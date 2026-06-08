package imageupdate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func indexServer(t *testing.T) *httptest.Server {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "index", "podinfo-index.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index.yaml" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestHTTPHelmIndexClient_Versions(t *testing.T) {
	srv := indexServer(t)
	versions, err := NewHTTPHelmIndexClient(nil).Versions(context.Background(), srv.URL, "podinfo")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"6.6.0", "6.5.4", "6.5.0", "6.6.0-rc.1"}
	if len(versions) != len(want) {
		t.Fatalf("got %v, want %v", versions, want)
	}
	for i := range want {
		if versions[i] != want[i] {
			t.Errorf("versions[%d] = %q, want %q", i, versions[i], want[i])
		}
	}
}

func TestHTTPHelmIndexClient_ChartNotFound(t *testing.T) {
	srv := indexServer(t)
	_, err := NewHTTPHelmIndexClient(nil).Versions(context.Background(), srv.URL, "ghost")
	if err == nil {
		t.Fatal("expected error for missing chart")
	}
}

type fakeCreds struct{ user, pass string }

func (f fakeCreds) Resolve(ctx context.Context, host string) (string, string, error) {
	return f.user, f.pass, nil
}

func TestHTTPHelmIndexClient_SendsBasicAuthWhenCredsPresent(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "index", "podinfo-index.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	client := NewHTTPHelmIndexClient(fakeCreds{user: "u", pass: "p"})
	if _, err := client.Versions(context.Background(), srv.URL, "podinfo"); err != nil {
		t.Fatal(err)
	}
	// base64("u:p") == "dTpw"
	if gotAuth != "Basic dTpw" {
		t.Errorf("Authorization = %q, want Basic dTpw", gotAuth)
	}
}

func TestHTTPHelmIndexClient_NoAuthWhenAnonymous(t *testing.T) {
	body, _ := os.ReadFile(filepath.Join("testdata", "index", "podinfo-index.yaml"))
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	client := NewHTTPHelmIndexClient(fakeCreds{}) // empty creds
	if _, err := client.Versions(context.Background(), srv.URL, "podinfo"); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "" {
		t.Errorf("expected no Authorization header, got %q", gotAuth)
	}
}

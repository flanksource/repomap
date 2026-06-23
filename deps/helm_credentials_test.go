package deps

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHelmCredentialsMatchLongestPrefix(t *testing.T) {
	creds := &helmCredentials{repos: []helmRepoCreds{
		{Name: "broad", URL: "https://charts.example.com"},
		{Name: "narrow", URL: "https://charts.example.com/team/", Username: "u"},
	}}
	got, ok := creds.match("https://charts.example.com/team/index.yaml")
	if !ok || got.Name != "narrow" {
		t.Fatalf("expected longest-prefix match 'narrow', got %q (ok=%v)", got.Name, ok)
	}
	if other, ok := creds.match("https://unrelated.example.com/index.yaml"); ok {
		t.Fatalf("unrelated URL should not match, got %q", other.Name)
	}
}

func TestCacheFetchSendsHelmBasicAuth(t *testing.T) {
	var gotUser, gotPass string
	var hadAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, hadAuth = r.BasicAuth()
		if !hadAuth {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte("index-body"))
	}))
	defer srv.Close()

	now, _ := testClock(time.Unix(1000, 0))
	c := newTestCache(t, now)
	c.get = c.httpGet // exercise the real HTTP path
	c.helmAuth = &helmCredentials{repos: []helmRepoCreds{
		{Name: "private", URL: srv.URL, Username: "alice", Password: "s3cret"},
	}}

	data, err := c.Fetch(context.Background(), srv.URL+"/index.yaml", ttlIndex)
	if err != nil {
		t.Fatalf("fetch with credentials failed: %v", err)
	}
	if string(data) != "index-body" {
		t.Fatalf("body = %q, want index-body", data)
	}
	if !hadAuth || gotUser != "alice" || gotPass != "s3cret" {
		t.Fatalf("server did not receive expected basic auth (user=%q pass set=%v hadAuth=%v)", gotUser, gotPass != "", hadAuth)
	}
}

func TestCacheFetchNoAuthForUnmatchedRepo(t *testing.T) {
	var hadAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _, hadAuth = r.BasicAuth()
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	now, _ := testClock(time.Unix(1000, 0))
	c := newTestCache(t, now)
	c.get = c.httpGet
	c.helmAuth = &helmCredentials{repos: []helmRepoCreds{
		{Name: "other", URL: "https://charts.other.com", Username: "x", Password: "y"},
	}}

	if _, err := c.Fetch(context.Background(), srv.URL+"/index.yaml", ttlIndex); err != nil {
		t.Fatal(err)
	}
	if hadAuth {
		t.Fatalf("credentials must not be sent to a non-matching repository")
	}
}

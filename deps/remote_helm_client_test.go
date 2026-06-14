package deps

import (
	"context"
	"strings"
	"testing"
)

func TestPickChartVersion(t *testing.T) {
	entries := []helmIndexEntry{
		{Version: "16.1.0", URLs: []string{"u-16.1.0"}},
		{Version: "16.7.27", URLs: []string{"u-16.7.27"}},
		{Version: "17.0.0", URLs: []string{"u-17.0.0"}},
	}
	cases := []struct{ constraint, wantVer, wantURL string }{
		{"16.x.x", "16.7.27", "u-16.7.27"},  // newest satisfying the range
		{"16.7.27", "16.7.27", "u-16.7.27"}, // exact
		{">=17.0.0", "17.0.0", "u-17.0.0"},
		{"99.x", "", ""}, // nothing satisfies
	}
	for _, tc := range cases {
		ver, url := pickChartVersion(entries, tc.constraint)
		if ver != tc.wantVer || url != tc.wantURL {
			t.Fatalf("pickChartVersion(%q) = (%q,%q), want (%q,%q)", tc.constraint, ver, url, tc.wantVer, tc.wantURL)
		}
	}
}

func TestFetchChartOCIDegrades(t *testing.T) {
	const repo = "https://charts.bitnami.com/bitnami"
	cache := &memCache{blobs: map[string][]byte{
		repo + "/index.yaml": []byte("entries:\n  postgresql:\n    - version: 16.7.27\n      urls:\n        - oci://registry-1.docker.io/bitnamicharts/postgresql:16.7.27\n"),
	}}
	h := &helmClient{cache: cache}
	_, _, err := h.fetchChart(context.Background(), chartDepEntry{Name: "postgresql", Version: "16.x.x", Repository: repo})
	if err == nil {
		t.Fatal("expected OCI chart to degrade with an error")
	}
	if got := err.Error(); !strings.Contains(got, "OCI artifact") {
		t.Fatalf("error should explain the OCI limitation, got %q", got)
	}
}

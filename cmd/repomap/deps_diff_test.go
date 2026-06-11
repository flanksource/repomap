package main

import (
	"strings"
	"testing"
)

func TestDepsDiffCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"deps", "diff", "HEAD~1"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd == nil || !strings.HasPrefix(cmd.Use, "diff") {
		t.Fatalf("expected deps diff command, got %#v", cmd)
	}
	for _, name := range []string{"depth", "manager", "filter"} {
		if flag := cmd.Flags().Lookup(name); flag == nil {
			t.Fatalf("%s flag not registered on deps diff", name)
		}
	}
	if depth := cmd.Flags().Lookup("depth"); depth.DefValue != "1" {
		t.Fatalf("depth default = %q, want 1", depth.DefValue)
	}
}

func TestParseRefRange(t *testing.T) {
	cases := []struct {
		arg      string
		wantBase string
		wantHead string
		wantErr  bool
	}{
		{"HEAD", "HEAD", "", false},
		{"v1.0.0..v1.1.0", "v1.0.0", "v1.1.0", false},
		{"..head", "", "", true},
		{"base..", "", "", true},
	}
	for _, tc := range cases {
		base, head, err := parseRefRange(tc.arg)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("parseRefRange(%q) expected error", tc.arg)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parseRefRange(%q) unexpected error: %v", tc.arg, err)
		}
		if base != tc.wantBase || head != tc.wantHead {
			t.Fatalf("parseRefRange(%q) = (%q, %q), want (%q, %q)", tc.arg, base, head, tc.wantBase, tc.wantHead)
		}
	}
}

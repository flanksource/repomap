package manifest

import "testing"

func TestSplitSpec(t *testing.T) {
	cases := []struct {
		spec        string
		wantName    string
		wantVersion string
		wantErr     bool
	}{
		{spec: "github.com/acme/lib@v1.2.3", wantName: "github.com/acme/lib", wantVersion: "v1.2.3"},
		// No version means "whatever the manager considers current"; the concrete
		// version is read back after warming.
		{spec: "github.com/acme/lib", wantName: "github.com/acme/lib", wantVersion: "latest"},
		{spec: "left-pad@1.3.0", wantName: "left-pad", wantVersion: "1.3.0"},
		{spec: "left-pad@^1.3.0", wantName: "left-pad", wantVersion: "^1.3.0"},
		{spec: "left-pad@latest", wantName: "left-pad", wantVersion: "latest"},
		// A scoped npm name leads with @, so splitting must use the last @ and
		// ignore one at index 0.
		{spec: "@scope/pkg@1.0.0", wantName: "@scope/pkg", wantVersion: "1.0.0"},
		{spec: "@scope/pkg", wantName: "@scope/pkg", wantVersion: "latest"},
		// A Go branch or commit reference must survive untouched.
		{spec: "github.com/acme/lib@main", wantName: "github.com/acme/lib", wantVersion: "main"},
		{spec: "", wantErr: true},
		{spec: "   ", wantErr: true},
		{spec: "left-pad@", wantErr: true},
		{spec: "@", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.spec, func(t *testing.T) {
			name, version, err := SplitSpec(tc.spec)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("SplitSpec(%q) = (%q, %q), want an error", tc.spec, name, version)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if name != tc.wantName || version != tc.wantVersion {
				t.Fatalf("SplitSpec(%q) = (%q, %q), want (%q, %q)", tc.spec, name, version, tc.wantName, tc.wantVersion)
			}
		})
	}
}

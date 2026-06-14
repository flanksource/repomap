package deps

import "testing"

func refsToStrings(refs []imageRef) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		s := r.Name
		if r.Version != "" {
			s += ":" + r.Version
		}
		if r.Digest != "" {
			s += "@" + r.Digest
		}
		out = append(out, s)
	}
	return out
}

func TestParseDockerfileFrom(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "single FROM with tag",
			content: "FROM nginx:1.25.3\nRUN echo hi\n",
			want:    []string{"nginx:1.25.3"},
		},
		{
			name:    "bare image no tag",
			content: "FROM alpine\n",
			want:    []string{"alpine"},
		},
		{
			name: "multi-stage excludes internal stage reference",
			content: "FROM golang:1.22 AS build\n" +
				"RUN go build\n" +
				"FROM gcr.io/distroless/static:nonroot\n" +
				"COPY --from=build /app /app\n",
			want: []string{"golang:1.22", "gcr.io/distroless/static:nonroot"},
		},
		{
			name: "final FROM referencing a prior stage is internal",
			content: "FROM golang:1.22 AS build\n" +
				"FROM build\n",
			want: []string{"golang:1.22"},
		},
		{
			name:    "platform flag is stripped",
			content: "FROM --platform=linux/amd64 ubuntu:22.04\n",
			want:    []string{"ubuntu:22.04"},
		},
		{
			name:    "ARG substitution in tag",
			content: "ARG GO_VERSION=1.22\nFROM golang:${GO_VERSION}\n",
			want:    []string{"golang:1.22"},
		},
		{
			name:    "ARG substitution in registry and unbraced var",
			content: "ARG REG=docker.io\nFROM $REG/library/busybox:1.36\n",
			want:    []string{"docker.io/library/busybox:1.36"},
		},
		{
			name:    "scratch is terminal and excluded",
			content: "FROM scratch\nCOPY x /\n",
			want:    nil,
		},
		{
			name:    "digest pinned",
			content: "FROM nginx@sha256:abc123\n",
			want:    []string{"nginx@sha256:abc123"},
		},
		{
			name:    "tag and digest",
			content: "FROM nginx:1.25@sha256:abc123\n",
			want:    []string{"nginx:1.25@sha256:abc123"},
		},
		{
			name:    "case-insensitive directives and comments",
			content: "# base\nfrom Ubuntu:22.04 as Base\n",
			want:    []string{"Ubuntu:22.04"},
		},
		{
			name:    "registry with port keeps host colon",
			content: "FROM localhost:5000/team/app:2.0\n",
			want:    []string{"localhost:5000/team/app:2.0"},
		},
		{
			name:    "duplicate external bases collapse",
			content: "FROM alpine:3.20 AS a\nFROM alpine:3.20 AS b\n",
			want:    []string{"alpine:3.20"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := parseDockerfileFrom(tc.content)
			gotStrings := refsToStrings(got)
			if len(gotStrings) != len(tc.want) {
				t.Fatalf("bases = %v, want %v", gotStrings, tc.want)
			}
			for i := range tc.want {
				if gotStrings[i] != tc.want[i] {
					t.Fatalf("base[%d] = %q, want %q (all: %v)", i, gotStrings[i], tc.want[i], gotStrings)
				}
			}
		})
	}
}

func TestParseDockerfileUnresolvedArgWarns(t *testing.T) {
	bases, warnings := parseDockerfileFrom("FROM $UNDEFINED_BASE\n")
	if len(bases) != 0 {
		t.Fatalf("unresolved ARG FROM should yield no base, got %v", refsToStrings(bases))
	}
	if len(warnings) == 0 {
		t.Fatalf("expected a warning for the unresolved ARG reference")
	}
}

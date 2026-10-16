package gitspec

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var githubSlug = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
var gitlabSlug = regexp.MustCompile(`^[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)+$`)
var scpGitURL = regexp.MustCompile(`^[^/@:\s]+@[^/:\s]+:[^\s]+$`)

type Spec struct {
	Remote string
	Base   string
	Ref    string
}

func Parse(value string) (Spec, bool, error) {
	value = strings.TrimSpace(value)
	base, ref, _ := strings.Cut(value, "#")
	if base == "" {
		return Spec{}, false, nil
	}
	remote := base
	switch {
	case strings.HasPrefix(base, "github:"), strings.HasPrefix(base, "gitlab:"), strings.HasPrefix(base, "bitbucket:"):
		provider, slug, _ := strings.Cut(base, ":")
		if !githubSlug.MatchString(slug) && (provider != "gitlab" || !gitlabSlug.MatchString(slug)) {
			return Spec{}, false, fmt.Errorf("invalid Git dependency source %q", base)
		}
		remote = "https://" + provider + ".com/" + slug
		if !strings.HasSuffix(remote, ".git") {
			remote += ".git"
		}
	case githubSlug.MatchString(base):
		remote = "https://github.com/" + base
		if !strings.HasSuffix(remote, ".git") {
			remote += ".git"
		}
	case strings.HasPrefix(base, "git+"):
		remote = strings.TrimPrefix(base, "git+")
		if !strings.Contains(remote, "://") {
			return Spec{}, false, fmt.Errorf("invalid Git dependency source %q", base)
		}
	case scpGitURL.MatchString(base):
	case strings.Contains(base, "://"):
		parsed, err := url.Parse(base)
		if err != nil {
			return Spec{}, false, fmt.Errorf("invalid Git dependency source %q: %w", base, err)
		}
		if !strings.HasSuffix(parsed.Path, ".git") && parsed.Hostname() != "github.com" && parsed.Hostname() != "gitlab.com" && parsed.Hostname() != "bitbucket.org" {
			return Spec{}, false, nil
		}
	default:
		return Spec{}, false, nil
	}
	if strings.ContainsAny(remote, " \t\r\n") || strings.HasPrefix(remote, "-") {
		return Spec{}, false, fmt.Errorf("invalid Git dependency source %q", base)
	}
	if strings.Contains(remote, "://") {
		parsed, err := url.Parse(remote)
		if err != nil || parsed.Scheme == "" || parsed.Path == "" {
			return Spec{}, false, fmt.Errorf("invalid Git dependency source %q", base)
		}
		if parsed.User != nil {
			if _, hasPassword := parsed.User.Password(); hasPassword {
				return Spec{}, false, fmt.Errorf("git dependency URL contains embedded credentials; use a Git credential helper")
			}
		}
		if parsed.Scheme != "file" && parsed.Hostname() == "" {
			return Spec{}, false, fmt.Errorf("invalid Git dependency source %q", base)
		}
		switch parsed.Scheme {
		case "https", "http", "ssh", "git", "file":
		default:
			return Spec{}, false, fmt.Errorf("unsupported Git dependency URL scheme %q", parsed.Scheme)
		}
	}
	return Spec{Remote: remote, Base: base, Ref: ref}, true, nil
}

func (s Spec) Pin(sha string) string {
	return s.Base + "#" + sha
}

func ValidCommitSHA(sha string) bool {
	if len(sha) != 40 && len(sha) != 64 {
		return false
	}
	for _, ch := range sha {
		if ch < '0' || ch > '9' {
			if ch < 'a' || ch > 'f' {
				return false
			}
		}
	}
	return true
}

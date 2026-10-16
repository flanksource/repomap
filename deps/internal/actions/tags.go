package actions

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
)

var exactReleaseTag = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

type TagSet struct {
	tags map[string]string
}

func ParseTags(output string) (TagSet, error) {
	tags := map[string]string{}
	peeled := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		sha, ref, ok := strings.Cut(line, "\t")
		if !ok || !fullSHA.MatchString(sha) || !strings.HasPrefix(ref, "refs/tags/") {
			return TagSet{}, fmt.Errorf("invalid remote tag line %q", line)
		}
		name := strings.TrimPrefix(ref, "refs/tags/")
		if strings.HasSuffix(name, "^{}") {
			peeled[strings.TrimSuffix(name, "^{}")] = strings.ToLower(sha)
		} else {
			tags[name] = strings.ToLower(sha)
		}
	}
	for name, sha := range peeled {
		if _, ok := tags[name]; !ok {
			return TagSet{}, fmt.Errorf("peeled tag %q has no tag object", name)
		}
		tags[name] = sha
	}
	return TagSet{tags: tags}, nil
}

func (t TagSet) Versions() []string {
	versions := make([]string, 0, len(t.tags))
	for tag := range t.tags {
		if exactReleaseTag.MatchString(tag) {
			if _, err := semver.NewVersion(tag); err != nil {
				continue
			}
			versions = append(versions, tag)
		}
	}
	sort.Slice(versions, func(i, j int) bool {
		left, _ := semver.NewVersion(versions[i])
		right, _ := semver.NewVersion(versions[j])
		if left.Equal(right) {
			return versions[i] < versions[j]
		}
		return left.GreaterThan(right)
	})
	return versions
}

func (t TagSet) Resolve(tag string) (string, error) {
	sha, ok := t.tags[tag]
	if !ok {
		return "", fmt.Errorf("tag %q is not advertised by the action repository", tag)
	}
	return sha, nil
}

func (t TagSet) CurrentVersion(ref, comment string) (string, error) {
	if !fullSHA.MatchString(ref) {
		if _, err := semver.NewVersion(ref); err != nil {
			return "", fmt.Errorf("reference %q is not a version tag", ref)
		}
		return ref, nil
	}
	ref = strings.ToLower(ref)
	if comment != "" && t.tags[comment] != ref {
		return "", fmt.Errorf("version comment %q does not resolve to pinned SHA %s", comment, ref)
	}
	for _, version := range t.Versions() {
		if t.tags[version] == ref {
			return version, nil
		}
	}
	var majors []string
	for tag, sha := range t.tags {
		if sha == ref {
			if _, err := semver.NewVersion(tag); err == nil {
				majors = append(majors, tag)
			}
		}
	}
	sort.Strings(majors)
	if len(majors) > 0 {
		return majors[len(majors)-1], nil
	}
	return "", fmt.Errorf("pinned SHA %s does not match a version tag", ref)
}

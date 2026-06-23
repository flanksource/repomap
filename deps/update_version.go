package deps

import (
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
)

func sortDependencyVersions(versions []string) []string {
	type parsed struct {
		orig string
		ver  *semver.Version
	}
	seen := map[string]bool{}
	var parsedVersions []parsed
	for _, version := range versions {
		version = strings.TrimSpace(version)
		if version == "" || seen[version] {
			continue
		}
		seen[version] = true
		sv, err := semver.NewVersion(version)
		if err != nil {
			continue
		}
		parsedVersions = append(parsedVersions, parsed{orig: version, ver: sv})
	}
	sort.Slice(parsedVersions, func(i, j int) bool {
		return parsedVersions[i].ver.GreaterThan(parsedVersions[j].ver)
	})
	out := make([]string, len(parsedVersions))
	for i, item := range parsedVersions {
		out[i] = item.orig
	}
	return out
}

func latestStableVersion(versions []string) string {
	for _, version := range versions {
		if !isPrerelease(version) {
			return version
		}
	}
	return ""
}

func latestPrereleaseVersion(versions []string) string {
	for _, version := range versions {
		if isPrerelease(version) {
			return version
		}
	}
	return ""
}

func isPrerelease(version string) bool {
	sv, err := semver.NewVersion(version)
	return err == nil && strings.TrimSpace(sv.Prerelease()) != ""
}

func updateableVersions(current string, versions []string) []string {
	current = normalizeCurrentVersion(current)
	currentSemver, currentErr := semver.NewVersion(current)
	out := make([]string, 0, len(versions))
	for _, version := range versions {
		if version == "" || version == current {
			continue
		}
		versionSemver, versionErr := semver.NewVersion(version)
		if currentErr != nil || versionErr != nil {
			out = append(out, version)
			continue
		}
		if versionSemver.GreaterThan(currentSemver) {
			out = append(out, version)
		}
	}
	return out
}

func selectedVersionIsCurrent(current, selected string) bool {
	return normalizeCurrentVersion(current) == selected
}

func normalizeCurrentVersion(current string) string {
	current = strings.TrimSpace(current)
	current = strings.TrimPrefix(current, "npm:")
	current = strings.TrimLeft(current, "^~<>= ")
	if idx := strings.IndexAny(current, " |,"); idx >= 0 {
		current = current[:idx]
	}
	return strings.TrimSpace(current)
}

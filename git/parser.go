package git

import (
	"regexp"
	"strings"

	"github.com/flanksource/repomap"
)

var commitTypeScopeRegex = regexp.MustCompile(`^(\w+)(\(([^)]+)\))?:\s*(.+)$`)
var commitTypeRegex = regexp.MustCompile(`^(\w+)\s+:\s*(.+)$`)

func ParseCommitTypeAndScope(subject string) (repomap.CommitType, repomap.ScopeType, string) {
	matches := commitTypeScopeRegex.FindStringSubmatch(subject)
	if len(matches) == 5 {
		commitType := repomap.CommitType(matches[1])
		scope := repomap.ScopeType(matches[3])
		subject := matches[4]
		return commitType, scope, subject
	}
	matches = commitTypeRegex.FindStringSubmatch(subject)
	if len(matches) == 3 {
		commitType := repomap.CommitType(matches[1])
		subject := matches[2]
		return commitType, repomap.ScopeTypeUnknown, subject
	}
	return repomap.CommitTypeUnknown, repomap.ScopeTypeUnknown, subject
}

var refRegex = regexp.MustCompile(`#(\d+)`)
var refWithParansRegex = regexp.MustCompile(`\(#(\d+)\)`)

func ParseReference(subject string) (string, string) {
	var ref string
	matches := refWithParansRegex.FindStringSubmatch(subject)
	if len(matches) == 2 {
		ref = matches[1]
		subject = strings.ReplaceAll(subject, matches[0], "")
	} else {
		matches = refRegex.FindStringSubmatch(subject)
		if len(matches) == 2 {
			ref = matches[1]
			subject = strings.ReplaceAll(subject, matches[0], "")
		}
	}
	return strings.TrimSpace(subject), strings.TrimSpace(ref)
}

var trailerKeys = []string{
	"Signed-off-by",
	"Co-authored-by",
	"Reviewed-by",
	"Acked-by",
	"Reported-by",
	"Tested-by",
}

func ParseTrailers(message string) (string, map[string]string) {
	trailers := make(map[string]string)
	lines := strings.Split(message, "\n")
	out := ""
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, ":") {
			out += line + "\n"
			continue
		}
		found := false
		for _, key := range trailerKeys {
			if strings.HasPrefix(line, key+":") {
				value := strings.TrimSpace(strings.TrimPrefix(line, key+":"))
				trailers[key] = value
				found = true
				break
			}
		}
		if !found {
			out += line + "\n"
		}
	}
	return out, trailers
}

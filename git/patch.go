package git

import (
	"fmt"
	"strings"

	"github.com/flanksource/repomap"
)

// ParsePatch takes a git patch string and returns a slice of CommitChange
func ParsePatch(patch string) ([]repomap.CommitChange, error) {
	if patch == "" {
		return nil, nil
	}

	lines := strings.Split(patch, "\n")
	var changes []repomap.CommitChange
	var currentFile string
	var adds, dels int
	var changeType repomap.SourceChangeType
	var currentLine int

	linesChanged := make([]int, 0, len(lines))

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		if strings.HasPrefix(line, "diff --git") {
			if currentFile != "" {
				changes = append(changes, repomap.CommitChange{
					File:         currentFile,
					Type:         changeType,
					Adds:         adds,
					Dels:         dels,
					LinesChanged: repomap.NewLineRanges(linesChanged),
				})
			}

			idx := strings.Index(line, ` "b/`)
			if idx != -1 {
				path := line[idx+4:]
				if endQuote := strings.Index(path, `"`); endQuote != -1 {
					currentFile = path[:endQuote]
				}
			} else {
				idx = strings.Index(line, " b/")
				if idx != -1 {
					currentFile = line[idx+3:]
				}
			}
			adds, dels = 0, 0
			linesChanged = linesChanged[:0]
			currentLine = 0
			changeType = repomap.SourceChangeTypeModified
		} else if strings.HasPrefix(line, "new file") {
			changeType = repomap.SourceChangeTypeAdded
		} else if strings.HasPrefix(line, "deleted file") {
			changeType = repomap.SourceChangeTypeDeleted
		} else if strings.HasPrefix(line, "rename from") {
			changeType = repomap.SourceChangeTypeRenamed
		} else if strings.HasPrefix(line, "@@") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				newRange := strings.TrimPrefix(parts[2], "+")
				fmt.Sscanf(newRange, "%d", &currentLine)
			}
		} else if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			adds++
			linesChanged = append(linesChanged, currentLine)
			currentLine++
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			dels++
		} else if !strings.HasPrefix(line, "diff") && !strings.HasPrefix(line, "index") &&
			!strings.HasPrefix(line, "---") && !strings.HasPrefix(line, "+++") &&
			!strings.HasPrefix(line, "new file") && !strings.HasPrefix(line, "deleted file") &&
			!strings.HasPrefix(line, "rename") && !strings.HasPrefix(line, "similarity") &&
			!strings.HasPrefix(line, "Binary files") && currentLine > 0 {
			currentLine++
		}
	}

	if currentFile != "" {
		changes = append(changes, repomap.CommitChange{
			File:         currentFile,
			Type:         changeType,
			Adds:         adds,
			Dels:         dels,
			LinesChanged: repomap.NewLineRanges(linesChanged),
		})
	}

	return changes, nil
}

package deps

import "github.com/flanksource/repomap/deps/internal/gitspec"

type nodeGitSpec = gitspec.Spec

func parseNodeGitSpec(value string) (nodeGitSpec, bool, error) { return gitspec.Parse(value) }

func validCommitSHA(sha string) bool { return gitspec.ValidCommitSHA(sha) }

# Feature: Repomap CLI

## Overview

A cobra-based CLI for the repomap library that exposes repository analysis capabilities through terminal commands with rich clicky-based Pretty() output. The CLI enables developers to scan files, analyze commits, diff changes, inspect configuration, and evaluate CEL severity rules directly from the command line.

**Problem**: Repomap is currently library-only. Users need a CLI to quickly scan repos, analyze commits, and evaluate rules without writing Go code.

**Target Users**: Developers, DevOps engineers, and CI/CD pipelines analyzing repository structure and commit history.

## Functional Requirements

### FR-1: Scan Command
**Description**: Scan a directory or file to produce FileMap output showing scopes, technologies, language, and Kubernetes references.
**User Story**: As a developer, I want to scan my repository to see how files are classified by scope and technology.
**Acceptance Criteria**:
- [ ] `repomap scan [path]` accepts file or directory path (defaults to `.`)
- [ ] Uses `git ls-files` to enumerate tracked files, respecting `.gitignore`
- [ ] Outputs FileMap with scopes, tech, language for each file
- [ ] Shows Kubernetes resource refs for YAML files
- [ ] Supports `--format` flag for pretty/json/yaml output
- [ ] Pretty output uses `FileMap.Pretty()` with clicky icons and colors

### FR-2: Commits/Log Command
**Description**: Parse and analyze git commit history with conventional commit detection, type/scope classification, and filtering.
**User Story**: As a developer, I want to analyze commit history with conventional commit parsing and filtering by author, date, type, and scope.
**Acceptance Criteria**:
- [ ] `repomap commits [path]` shows analyzed commit history
- [ ] Parses conventional commit types and scopes using `git.ParseCommitTypeAndScope`
- [ ] `--author` filter using `collections.MatchItem` wildcard patterns
- [ ] `--since` / `--until` date range filters
- [ ] `--type` filter by commit type (feat, fix, chore, etc.)
- [ ] `--scope` filter by commit scope
- [ ] Pretty output uses `Commit.Pretty()` / `CommitAnalysis.Pretty()`
- [ ] Short mode (`--short`) uses `PrettyShort()` for compact listing

### FR-3: Diff/Changes Command
**Description**: Analyze a specific commit or commit range for file changes, Kubernetes changes, and severity evaluation.
**User Story**: As a developer, I want to see detailed change analysis for commits including K8s resource changes and severity.
**Acceptance Criteria**:
- [ ] `repomap diff [commit]` analyzes a single commit
- [ ] `repomap diff [from]..[to]` analyzes a commit range
- [ ] Shows file changes with adds/dels using `CommitChange.Pretty()`
- [ ] K8s changes shown inline using `KubernetesChange.Pretty()`
- [ ] `--kubernetes` flag for K8s-only filtered view
- [ ] CEL severity rules evaluated automatically when configured in arch.yaml
- [ ] Accepts git diff/patch content via stdin for analysis without a repo
- [ ] Supports `--format` flag for pretty/json output

### FR-4: Config Command
**Description**: Display the merged architecture configuration (embedded defaults + user arch.yaml).
**User Story**: As a developer, I want to see my effective configuration to debug scope/tech detection rules.
**Acceptance Criteria**:
- [ ] `repomap config [path]` shows merged config for a path
- [ ] Shows effective scope rules (user + defaults merged)
- [ ] Shows effective tech rules
- [ ] Shows severity CEL rules if configured
- [ ] YAML output by default, JSON with `--format json`

### FR-5: Eval Command
**Description**: Evaluate CEL expressions against repository state for severity rule testing.
**User Story**: As a developer, I want to test CEL expressions against my commits/files to develop severity rules.
**Acceptance Criteria**:
- [ ] `repomap eval '<expr>'` evaluates inline CEL expression
- [ ] `repomap eval --file rules.yaml` evaluates rules from file
- [ ] Shows evaluation result with matched severity
- [ ] Context includes commit, change, kubernetes, file data
- [ ] Pretty output shows matched/unmatched rules with severity badges

## CLI Structure

```
cmd/repomap/main.go          # Entry point
cmd/repomap/scan.go           # scan command
cmd/repomap/commits.go        # commits command
cmd/repomap/diff.go           # diff command
cmd/repomap/config.go         # config command
cmd/repomap/eval.go           # eval command
```

## User Interactions

### Command Registration
Use `clicky.AddCommand()` wrapper pattern (matching gavel) for command registration with built-in output handling. Each command returns `api.Text` or structured data that clicky renders.

### Global Flags
- `--format / -o` — Output format: `pretty` (default), `json`, `yaml`
- `--config / -c` — Path to repomap.yaml (default: auto-detect via `GetConfForFile`)
- `--path / -p` — Target repo/directory (default: current directory)
- `--verbose / -v` — Verbose output with debug logging

### Output Flow
1. Command executes library function (e.g., `GetFileMap`, `ParseCommitTypeAndScope`)
2. Based on `--format`:
   - `pretty`: Call `.Pretty()` on result, pipe through `clicky.Print()`
   - `json`: Marshal to JSON, write to stdout
   - `yaml`: Marshal to YAML, write to stdout

### Stdin Support
- `repomap diff` accepts patch content via stdin: `git diff | repomap diff -`
- Detected via `-` argument or `!isatty(stdin)`

## Technical Considerations

- **Framework**: cobra via `clicky.AddCommand()` wrapper
- **Output**: clicky `api.Text` with Pretty() methods, `clicky.Print()` for terminal rendering
- **Config**: Uses existing `GetConf()` / `GetConfForFile()` for config loading
- **Git**: Uses `clicky.Exec("git")` for git operations (already in conf.go)
- **CEL**: Uses `cel.NewEngine()` for rule evaluation
- **Kubernetes**: Uses `kubernetes.ExtractKubernetesRefsFromContent()` for K8s detection

## Success Criteria
- [ ] All 5 commands (scan, commits, diff, config, eval) work with pretty output
- [ ] JSON output produces valid, parseable JSON for all commands
- [ ] `git ls-files` scanning respects .gitignore
- [ ] All existing Pretty() methods render correctly in terminal
- [ ] CEL evaluation integrates with diff command when severity rules configured
- [ ] Stdin piping works for diff command
- [ ] `go build ./cmd/repomap/` produces working binary

## Testing Requirements
- Unit tests: Command flag parsing, output format selection
- Integration tests: End-to-end command execution against test repos
- Test fixtures: Sample arch.yaml, git repos with known commits

## Implementation Checklist

### Phase 1: Setup
- [ ] Create `cmd/repomap/main.go` with root cobra command via clicky
- [ ] Add global flags (--format, --config, --path, --verbose)
- [ ] Set up output format switching (pretty/json/yaml)

### Phase 2: Core Commands
- [ ] Implement `scan` command with git-aware file walking
- [ ] Implement `commits` command with filtering flags
- [ ] Implement `diff` command with commit/range/stdin support
- [ ] Implement `config` command
- [ ] Implement `eval` command with inline + file input

### Phase 3: Testing
- [ ] Write command tests
- [ ] Test Pretty output rendering
- [ ] Test JSON/YAML output formats
- [ ] Test stdin piping for diff

### Phase 4: Polish
- [ ] Add --help text with examples for each command
- [ ] Verify all Pretty() methods render correctly
- [ ] Add Makefile target for building CLI

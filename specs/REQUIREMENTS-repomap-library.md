# Feature: Standalone Repomap Library

## Overview

Extract and generalize the repomap, change detection, and commit analysis functionality from `flanksource/gavel` and `flanksource/arch-unit-todos` into a standalone, extensible Go library. The library classifies repository files, analyzes git commits and their changes (including Kubernetes resource diffs, version tracking, and scaling detection), evaluates CEL-based severity rules, and provides pluggable content parsing.

**Problem**: The repomap logic and git change analysis are currently embedded in two separate projects with duplicated code. They need to be extracted into a reusable library with a cleaner API and more extensible rule system.

**Target users**: Go developers building repository analysis tools, CI/CD pipelines, change management systems, or architecture testing frameworks.

## Source Code References

- **gavel repomap**: `/Users/moshe/go/src/github.com/flanksource/gavel/repomap/`
- **gavel git analysis**: `/Users/moshe/go/src/github.com/flanksource/gavel/git/` (analyzer, patch parsing, kubernetes change detection, rules engine)
- **gavel models**: `/Users/moshe/go/src/github.com/flanksource/gavel/models/` (Commit, CommitChange, KubernetesChange, Severity)
- **arch-unit-todos**: `/Users/moshe/go/src/github.com/flanksource/arch-unit-todos/repomap/`

## Scope Decisions

- **Change detection and commit analysis are IN scope**. The library provides commit parsing, patch analysis, Kubernetes change detection (version tracking, scaling, environment changes), and CEL-based severity evaluation.
- **AI-based commit analysis is OUT of scope**. The `AnalyzeWithAI` functionality and LLM agent integration remain in gavel.
- **Repository management is OUT of scope**. `GitRepositoryManager`, `CloneManager`, worktree management, and remote operations remain in gavel.
- **`arch.yaml` backwards compatibility**: The library supports `scopes`, `tech`, `severity`, and `version_field_patterns` sections. The `build` and `golang` sections are consumer-specific and not loaded by this library. The `git` section (commit message validation) is loaded.
- **Scope and technology values are arbitrary strings**. No `allowed_scopes` validation in the library. Consumers can validate against their own allow-lists.
- **Pretty-printing is OUT of scope**. The library provides data structures only; rendering (clicky, text formatting) remains in consumers.

---

## Functional Requirements

### FR-1: File Classification

**Description**: Classify files by language, organizational scope, and technology stack using configurable path-based pattern matching.

**User Story**: As a developer, I want to classify any file in a repository by its language, scope, and technology so that I can build tools that reason about repository structure.

**Acceptance Criteria**:
- [ ] Detect programming language from file extension (go, python, java, typescript, javascript, rust, ruby, markdown, xml, and more)
- [ ] Classify files into scopes using glob patterns (ci, dependency, docs, test, app, and arbitrary user-defined scopes)
- [ ] Detect technology stack using glob patterns (kubernetes, helm, terraform, docker, go, nodejs, and arbitrary user-defined technologies)
- [ ] Support negation patterns (`!` or `-` prefix) for exclusions
- [ ] Support doublestar (`**`) glob matching via `bmatcuk/doublestar`
- [ ] Path matching order: full path match first, then basename fallback, results sorted by specificity (longer pattern, fewer wildcards)
- [ ] Paths are normalized to forward slashes (`filepath.ToSlash`) before matching
- [ ] Input paths are repo-relative (relative to config root or git root). The API normalizes absolute paths when a root is known
- [ ] Return `FileMap` struct with: Path, Language, Scopes, Tech, KubernetesRefs, Ignored, Tags
- [ ] Lenient behavior: missing files return an empty FileMap with the path set (no error), matching current gavel behavior

### FR-2: Kubernetes YAML Parsing

**Description**: Parse multi-document YAML files and extract Kubernetes resource metadata with source line tracking.

**User Story**: As a developer, I want to extract structured Kubernetes resource references from YAML files so that I can analyze infrastructure-as-code changes.

**Acceptance Criteria**:
- [ ] Parse multi-document YAML files (separated by `---`) with line number tracking per document
- [ ] Detect Kubernetes resources by presence of both `apiVersion` and `kind` fields
- [ ] Extract `KubernetesRef` with: apiVersion, kind, namespace, name, labels, annotations, jsonPath, startLine, endLine
- [ ] Only trigger for `.yaml` / `.yml` files
- [ ] Malformed documents are skipped silently (logged at debug level), not treated as errors
- [ ] Empty documents between `---` separators are skipped
- [ ] Non-Kubernetes YAML documents are skipped (included in parse results but not in KubernetesRefs)
- [ ] Parsing works from content string (no file I/O dependency) — callers provide content via `FileReader` or directly

### FR-3: CEL Rule Engine

**Description**: Evaluate CEL (Common Expression Language) expressions against structured context for flexible, user-defined rule evaluation.

**User Story**: As a developer, I want to write CEL expressions that evaluate against file metadata and Kubernetes resource properties so that I can define custom classification, tagging, and severity rules.

**Acceptance Criteria**:
- [ ] Compile CEL expressions at initialization time for performance
- [ ] Evaluate against file metadata context (see CEL Context Variables below)
- [ ] Evaluate against Kubernetes resource context per-document — for multi-resource files, rules are evaluated once per KubernetesRef with both `file.*` and `kubernetes.*` populated
- [ ] User-defined variables are passed under a `vars` map key (e.g., `vars.myfield`), not as top-level keys, to avoid CEL compilation failures on undefined variables
- [ ] Rules are defined as an **ordered list** (not a map) with explicit priority. YAML config uses a list of `{expr, result}` objects. Legacy map format is accepted and converted to a list with stable sorted order (alphabetical by expression)
- [ ] Two evaluation modes:
  - **first-match**: Return the result of the first matching rule (default for severity evaluation)
  - **collect-all**: Return results of all matching rules (default for tagging/violation detection)
- [ ] `RuleResult` struct:
  ```go
  type RuleResult struct {
      Severity string         `yaml:"severity" json:"severity"`
      Message  string         `yaml:"message"  json:"message"`
      Tags     []string       `yaml:"tags"     json:"tags"`
      Fields   map[string]any `yaml:"fields"   json:"fields,omitempty"`
  }
  ```
- [ ] Allow registering custom Go functions callable from CEL expressions via `cel.Function`
- [ ] Provide sensible default rules embedded in the binary
- [ ] CEL compilation errors are returned at initialization time, not at evaluation time

### FR-4: Configuration System

**Description**: Support YAML-based configuration with hierarchical directory lookup, embedded defaults, programmatic API, and explicit merge semantics.

**User Story**: As a developer, I want to configure repomap via `arch.yaml` files with sensible defaults and also build configurations programmatically in Go code.

**Acceptance Criteria**:
- [ ] Load configuration from `arch.yaml` found by walking up the directory tree, stopping at the root dir (or git root when available)
- [ ] Return zero-value config (defaults only) when no `arch.yaml` is found — not an error
- [ ] Embed `defaults.yaml` with default scope/tech/severity rules and `version_field_patterns`
- [ ] Merge semantics for all sections:
  - **scopes**: User rules prepended to defaults for the same scope name. User-only scopes added as-is.
  - **tech**: Same merge as scopes.
  - **severity**: User rules prepended to default rules list. User `default` severity overrides default.
  - Sections not present in user config fall through to defaults.
- [ ] Hybrid rule support: glob patterns for simple path matching + CEL for complex conditional rules
- [ ] Programmatic Go API to build/modify config:
  ```go
  conf := repomap.NewConf().
      WithScope("custom", repomap.PathRule{Path: "custom/**"}).
      WithTech("custom-tech", repomap.PathRule{Path: "*.custom"}).
      WithCELRule(repomap.Rule{Expr: "...", Result: repomap.RuleResult{...}}).
      WithCustomFunc("hasAnnotation", hasAnnotationFunc)
  ```
- [ ] Programmatic API can modify a loaded config (load then customize)
- [ ] Config schema documented in `arch.yaml.schema.json` or equivalent

### FR-5: File Access Abstraction

**Description**: Core library operates on file paths and content via a `FileReader` interface. Git operations provided as a separate optional package.

**User Story**: As a developer, I want to use repomap without requiring a git repository, but optionally use git features when available.

**Acceptance Criteria**:
- [ ] `FileReader` interface defined in core:
  ```go
  type FileReader interface {
      ReadFile(path string) (string, error)
      FileExists(path string) bool
      Walk(root string, fn filepath.WalkFunc) error
  }
  ```
- [ ] Default `OSFileReader` implementation uses `os.ReadFile` / `os.Stat` / `filepath.Walk`
- [ ] Core classification uses `FileReader` for all file I/O
- [ ] Separate `git` package provides `GitFileReader` implementing `FileReader` with:
  - `ReadFileAtCommit(path, commit)` — reads from a specific commit
  - `FileExistsAtCommit(path, commit)`
  - `FindGitRoot(path)` — walks up to find `.git`
- [ ] Git package shells out to `git` CLI (matching current gavel behavior) rather than using `go-git`, to avoid parity issues with worktrees, submodules, and LFS
- [ ] Core package has zero dependency on git package

### FR-6: Directory Tree Scanning

**Description**: Support both single-file classification and full directory tree scanning with ignore-pattern awareness.

**User Story**: As a developer, I want to scan an entire repository and get FileMaps for all files, respecting ignore patterns.

**Acceptance Criteria**:
- [ ] `ClassifyFile(path string) (*FileMap, error)` — classify a single file
- [ ] `Scan(rootPath string) (*ScanResult, error)` — scan directory tree
- [ ] `ScanResult` contains:
  ```go
  type ScanResult struct {
      Files  []FileMap
      Errors []ScanError // per-file errors (path + error), scan continues
  }
  ```
- [ ] Respect `.gitignore` patterns when scanning (via `FileReader.Walk` or gitignore parser)
- [ ] Support configurable ignore patterns in `arch.yaml` beyond `.gitignore`
- [ ] Skip ignored directories early (don't descend into `node_modules/`, `.git/`, etc.)
- [ ] Concurrent file processing for large repos (configurable worker count, default to `runtime.NumCPU()`)

### FR-7: Pluggable Content Parsers

**Description**: Support extensible content parsing beyond Kubernetes YAML via a `Parser` interface.

**User Story**: As a developer, I want to register custom parsers for file types (Dockerfiles, Terraform HCL, Helm charts) so that repomap can extract structured metadata from any format.

**Acceptance Criteria**:
- [ ] `Parser` interface:
  ```go
  type Parser interface {
      // Name returns the parser identifier
      Name() string
      // Matches returns true if this parser handles the given file path
      Matches(path string) bool
      // Parse extracts structured metadata from file content
      Parse(path string, content string) ([]ParseResult, error)
  }
  type ParseResult struct {
      Kind        string            // e.g., "KubernetesResource", "DockerStage", "TerraformResource"
      Name        string
      Fields      map[string]any
      StartLine   int
      EndLine     int
  }
  ```
- [ ] Kubernetes YAML parser is the built-in default parser (registered automatically)
- [ ] Custom parsers registered via `conf.WithParser(parser)`
- [ ] Parse results are available in CEL context under `resource.*` (kind, name, fields)
- [ ] CEL evaluation iterates over each ParseResult in a file, similar to per-KubernetesRef evaluation

### FR-8: Commit Parsing

**Description**: Parse git commits into structured data including conventional commit type/scope extraction, patch parsing into per-file changes with line-level granularity, and commit quality scoring.

**User Story**: As a developer, I want to parse git commits into structured change data so that I can analyze what changed, where, and how.

**Acceptance Criteria**:
- [ ] Parse unified diff patches into per-file `CommitChange` structs with: file path, change type (added/modified/deleted/renamed), lines added, lines deleted, changed line ranges
- [ ] Extract conventional commit type and scope from subject line (e.g., `feat(auth): add login` → type=feat, scope=auth)
- [ ] Extract git trailers (Signed-off-by, Co-authored-by, etc.) from commit body
- [ ] Extract GitHub issue references (`#123`) from subject
- [ ] Calculate commit quality score (0-100) based on: has conventional type (+5), has scope (+5), subject length (scaled), body length (scaled), has trailers (+5)
- [ ] `Commit` struct:
  ```go
  type Commit struct {
      Hash         string
      Author       Author    // Name, Email, Date
      Committer    Author
      Subject      string
      Body         string
      Patch        string    // raw unified diff
      CommitType   string    // feat, fix, chore, docs, etc.
      Scope        string    // conventional commit scope
      Tags         []string
      Reference    string    // e.g., "#123"
      Trailers     map[string]string
      QualityScore int
  }
  ```
- [ ] `CommitChange` struct:
  ```go
  type CommitChange struct {
      File              string
      Type              ChangeType        // added, modified, deleted, renamed
      Adds              int
      Dels              int
      LinesChanged      LineRanges        // compressed "1-5,7-9,12" format
      Scope             []string          // from FileMap classification
      Tech              []string          // from FileMap classification
      KubernetesChanges []KubernetesChange
      Severity          Severity
  }
  ```
- [ ] `LineRanges` type with compressed representation and `String()` method

### FR-9: Kubernetes Change Detection

**Description**: Detect and analyze changes to Kubernetes resources across commits, including JSON patch generation, version tracking, scaling changes, and environment variable changes.

**User Story**: As a developer, I want to see exactly what changed in Kubernetes manifests between commits — which resources were modified, what fields changed, whether versions were upgraded/downgraded, and if replicas or resources were scaled.

**Acceptance Criteria**:
- [ ] Compare before/after YAML content for changed files to detect per-resource changes
- [ ] Match before/after Kubernetes resources by kind + name + namespace
- [ ] Generate RFC 6902 JSON patches with old values preserved (`ExtendedPatch`)
- [ ] Detect change type per resource: added, modified, deleted
- [ ] Detect source type: kustomize, helm, yaml, flux, argocd (from file path and content heuristics)
- [ ] `KubernetesChange` struct:
  ```go
  type KubernetesChange struct {
      KubernetesRef                       // embedded
      ChangeType        ChangeType
      SourceType        KubernetesSourceType
      Patches           []ExtendedPatch   // RFC 6902 with OldValue
      Scaling           *Scaling
      VersionChanges    []VersionChange
      EnvironmentChange *EnvironmentChange
      Severity          Severity
      FieldsChanged     []string          // dot-notation field paths
      FieldChangeCount  int
      Before            map[string]any    // resource snapshot before
      After             map[string]any    // resource snapshot after
  }
  ```
- [ ] Find affected YAML documents by intersecting changed line numbers with document line ranges
- [ ] Extract changed field paths from JSON patches (JSON pointer → dot notation)

### FR-10: Version Change Detection

**Description**: Detect and classify version changes in Kubernetes resources including semver upgrades/downgrades, container image tag changes, and SHA digest changes.

**User Story**: As a developer, I want to know when a Kubernetes resource has a version upgrade, downgrade, or SHA change so that I can assess the risk of the change.

**Acceptance Criteria**:
- [ ] Detect version changes using configurable field patterns (default: `**.image`, `**.tag`, `**.version`, `**.appVersion`, `**.imageTag`)
- [ ] Classify version value types: semver, sha256, git-sha, combined (tag+digest)
- [ ] For semver values, classify change type: major, minor, patch upgrade or downgrade
- [ ] Parse container images to extract tag and digest separately (`image:tag@sha256:...`)
- [ ] `VersionChange` struct:
  ```go
  type VersionChange struct {
      OldVersion string
      NewVersion string
      ChangeType string    // major, minor, patch, unknown
      FieldPath  string    // dot-notation path to the changed field
      ValueType  string    // semver, sha256, git-sha, combined
      Digest     string    // extracted digest if present
  }
  ```
- [ ] Detect scaling changes: replica count, CPU requests/limits, memory requests/limits
- [ ] `Scaling` struct:
  ```go
  type Scaling struct {
      OldCPU      string
      NewCPU      string
      OldMemory   string
      NewMemory   string
      Replicas    *int
      NewReplicas *int
  }
  ```
- [ ] Detect environment variable changes (added, removed, modified env vars in containers)
- [ ] `EnvironmentChange` struct with Old/New `map[string]string`

### FR-11: Commit Analysis

**Description**: Analyze a commit by enriching each file change with FileMap classification, Kubernetes change detection, and severity evaluation.

**User Story**: As a developer, I want to analyze a full commit and get a structured result showing all changes with their scopes, technologies, Kubernetes resource impacts, and severity levels.

**Acceptance Criteria**:
- [ ] `AnalyzeCommit(commit, options)` performs full analysis:
  1. Parse patch into per-file CommitChanges
  2. Classify each changed file via `ClassifyFile` (scope, tech, language)
  3. For YAML files, run Kubernetes change detection (before/after comparison)
  4. Evaluate severity for each change using CEL rules engine
  5. Aggregate scopes and technologies across all changes
  6. Calculate quality score
- [ ] `CommitAnalysis` struct:
  ```go
  type CommitAnalysis struct {
      Commit                              // embedded
      Changes            []CommitChange
      Tech               []string         // aggregated from all changes
      TotalLineChanges   int
      TotalResourceCount int
  }
  ```
- [ ] Support `AnalyzeOptions` for filtering:
  - `ScopeTypes` — include only changes matching these scopes
  - `CommitTypes` — include only these conventional commit types
  - `Technologies` — include only changes matching these technologies
  - `MaxConcurrent` — worker count for batch analysis
- [ ] `AnalyzeCommitHistory(commits, options)` for batch analysis with concurrent processing
- [ ] CEL context for severity evaluation includes all four domains:
  ```
  commit.*      — hash, author, subject, type, scope, file_count, line_changes, resource_count
  change.*      — type, file, adds, dels, fields_changed, field_count
  kubernetes.*  — is_kubernetes, kind, api_version, namespace, name,
                  version_upgrade, version_downgrade, has_sha_change,
                  replica_delta, has_env_change, has_resource_change
  file.*        — extension, directory, is_test, is_config, tech
  ```
- [ ] Default severity rules (embedded):
  - File deletions → critical
  - K8s Secrets → critical
  - RBAC resources (Role, RoleBinding, ServiceAccount) → high
  - Network resources (Service, Ingress, NetworkPolicy) → high
  - Large changes (>500 lines or >20 files) → critical
  - Version downgrades → high
  - Major version upgrades → high
  - SHA changes → high
  - Replica scaling >10 or <-5 → high
  - Env/resource changes → medium
  - `.env` files → high
  - Modified config files → medium

### FR-12: Severity Model

**Description**: Provide a severity type with ordering, comparison, and distribution tracking.

**User Story**: As a developer, I want a consistent severity model (critical/high/medium/low/info) with comparison operators so that I can aggregate and report on change risk.

**Acceptance Criteria**:
- [ ] `Severity` type as string with constants: Critical, High, Medium, Low, Info
- [ ] `Value()` returns numeric ordering (5=Critical, 1=Info)
- [ ] `Max(a, b)` returns higher severity
- [ ] `SeverityDistribution` struct with per-level counters and `Add(s)`, `Total()`, `Max()` methods
- [ ] `ChangeSeverity` type (string alias for Kubernetes-specific severity)

---

## Public API Design

**Primary API style**: Config-driven

```go
// --- File Classification ---

// Load configuration (finds arch.yaml, merges with defaults)
conf, err := repomap.LoadConf(path)

// Load with explicit FileReader
conf, err := repomap.LoadConf(path, repomap.WithFileReader(gitReader))

// Classify a single file
fileMap, err := conf.ClassifyFile(path)

// Scan directory tree
result, err := conf.Scan(rootPath)
// result.Files contains []FileMap
// result.Errors contains []ScanError for files that failed

// Evaluate CEL rules against a file
violations := conf.Evaluate(fileMap) // returns []RuleResult

// Programmatic configuration
conf := repomap.NewConf().
    WithScope("custom-scope", repomap.PathRule{Path: "custom/**"}).
    WithCELRule(repomap.Rule{
        Expr:   "file.extension == 'go' && file.path.contains('generated')",
        Result: repomap.RuleResult{Tags: []string{"generated"}},
    }).
    WithParser(myTerraformParser).
    WithCustomFunc("hasAnnotation", hasAnnotationFunc)

// --- Change Detection & Commit Analysis ---

// Parse a raw patch into per-file changes
changes := repomap.ParsePatch(patchString) // returns []CommitChange

// Analyze Kubernetes changes in a file (before/after content)
k8sChanges, err := repomap.AnalyzeKubernetesChanges(conf, beforeContent, afterContent, changedLines)

// Analyze a full commit (classifies files, detects K8s changes, evaluates severity)
ctx := repomap.NewAnalyzerContext(conf, repomap.WithFileReader(gitReader))
analysis, err := ctx.AnalyzeCommit(commit, repomap.AnalyzeOptions{
    ScopeTypes:    []string{"app", "infrastructure"},
    Technologies:  []string{"kubernetes"},
    MaxConcurrent: 4,
})

// Batch analyze commit history
analyses, err := ctx.AnalyzeCommitHistory(commits, repomap.AnalyzeOptions{})

// Severity evaluation with full commit/change context
severity := ctx.EvaluateSeverity(commit, change, k8sChange) // returns Severity
```

---

## Technical Considerations

### Package Structure

```
repomap/                  # Public API: LoadConf, ClassifyFile, Scan, Evaluate
  ├── types.go            # FileMap, ArchConf, ScanResult, FileReader, Severity
  ├── config.go           # LoadConf, config merging, defaults
  ├── classify.go         # ClassifyFile, language detection, scope/tech matching
  ├── scan.go             # Scan with concurrency
  ├── pattern.go          # PathPattern, PathRule, glob matching
  ├── defaults.yaml       # Embedded default config
  │
  ├── cel/                # CEL rule engine
  │   ├── engine.go       # Compile, Evaluate, rule ordering, eval modes
  │   ├── functions.go    # Custom function registration
  │   ├── context.go      # Context variable declarations (file, resource, commit, change, kubernetes)
  │   └── types.go        # Rule, RuleResult, EvalMode
  │
  ├── parser/             # Content parser interface and built-in parsers
  │   ├── parser.go       # Parser interface, ParseResult
  │   └── kubernetes.go   # Kubernetes YAML parser (default)
  │
  ├── changes/            # Change detection and commit analysis
  │   ├── commit.go       # Commit, CommitAnalysis, CommitChange types
  │   ├── patch.go        # ParsePatch — unified diff → per-file changes
  │   ├── parser.go       # Conventional commit parsing (type, scope, trailers, refs)
  │   ├── quality.go      # Commit quality scoring
  │   ├── analyzer.go     # AnalyzeCommit, AnalyzeCommitHistory, AnalyzerContext
  │   ├── options.go      # AnalyzeOptions, filtering
  │   ├── kubernetes.go   # Kubernetes change detection (before/after diff)
  │   ├── version.go      # Version change detection (semver, SHA, images)
  │   ├── scaling.go      # Scaling detection (replicas, CPU, memory)
  │   ├── environment.go  # Environment variable change detection
  │   ├── severity.go     # Default severity rules, context building
  │   └── jsonpatch.go    # RFC 6902 JSON patch generation with old values
  │
  └── git/                # Optional git operations
      ├── reader.go       # GitFileReader (shells out to git CLI)
      └── discover.go     # FindGitRoot, arch.yaml discovery
```

**Layering rules**:
- `repomap/` depends on `repomap/cel/` and `repomap/parser/`
- `repomap/cel/` has no dependency on `repomap/` (only shares types defined in `cel/types.go`)
- `repomap/parser/` has no dependency on `repomap/`
- `repomap/changes/` depends on `repomap/` (for ClassifyFile, CEL engine, KubernetesRef) and `repomap/parser/`
- `repomap/git/` depends on `repomap/` (implements `FileReader` interface)
- No import cycles

### Dependencies

- `github.com/bmatcuk/doublestar/v4` — Glob pattern matching
- `github.com/google/cel-go` — CEL expression evaluation
- `github.com/goccy/go-yaml` — YAML parsing (chosen for line number tracking support and performance; standardize on this single library, replacing `ghodss/yaml` from gavel)
- `github.com/Masterminds/semver` — Semantic version parsing and comparison (for version change detection)
- `github.com/mattbaird/jsonpatch` — RFC 6902 JSON patch generation (for Kubernetes change detection)

**Not included** (avoiding heavyweight/UI deps from source projects):
- `go-git/go-git` — git package shells out to `git` CLI instead
- `clicky`, `commons/logger`, `samber` — replaced with stdlib `log/slog` or no logging
- `commons-db`, `gomplate` — AI/LLM integration remains in gavel

### Data Structures

**File Classification (repomap/)**:
```go
type FileMap struct {
    Path           string
    Language       string
    Scopes         []string
    Tech           []string
    KubernetesRefs []KubernetesRef
    ParseResults   []ParseResult    // from registered parsers
    Ignored        bool
    Tags           []string         // populated by CEL rules
}

type KubernetesRef struct {
    APIVersion  string
    Kind        string
    Namespace   string
    Name        string
    JSONPath    string             // path to field within resource
    Labels      map[string]string
    Annotations map[string]string
    StartLine   int
    EndLine     int
}

type ArchConf struct {
    Git      GitConfig            // commit message validation patterns
    Scopes   ScopesConfig
    Tech     TechnologyConfig
    Rules    []Rule               // ordered CEL rules
    Severity SeverityConfig       // default severity + severity rules
    VersionFieldPatterns []string // patterns for version field detection
    Parsers  []Parser             // registered content parsers
    reader   FileReader           // file access abstraction
}

type Severity string // critical, high, medium, low, info
// With Value(), Max(), comparison methods, SeverityDistribution
```

**Change Detection (repomap/changes/)**:
```go
type Commit struct {
    Hash         string
    Author       Author
    Committer    Author
    Subject      string
    Body         string
    Patch        string
    CommitType   string            // feat, fix, chore, docs, etc.
    Scope        string            // conventional commit scope
    Tags         []string
    Reference    string            // e.g., "#123"
    Trailers     map[string]string
    QualityScore int
}

type CommitAnalysis struct {
    Commit                                // embedded
    Changes            []CommitChange
    Tech               []string           // aggregated
    TotalLineChanges   int
    TotalResourceCount int
}

type CommitChange struct {
    File              string
    Type              ChangeType
    Adds              int
    Dels              int
    LinesChanged      LineRanges
    Scope             []string
    Tech              []string
    KubernetesChanges []KubernetesChange
    Severity          Severity
}

type KubernetesChange struct {
    KubernetesRef                         // embedded
    ChangeType        ChangeType
    SourceType        KubernetesSourceType // kustomize, helm, yaml, flux, argocd
    Patches           []ExtendedPatch
    Scaling           *Scaling
    VersionChanges    []VersionChange
    EnvironmentChange *EnvironmentChange
    Severity          Severity
    FieldsChanged     []string
    FieldChangeCount  int
    Before            map[string]any
    After             map[string]any
}

type VersionChange struct {
    OldVersion string
    NewVersion string
    ChangeType string              // major, minor, patch, unknown
    FieldPath  string
    ValueType  string              // semver, sha256, git-sha, combined
    Digest     string
}

type Scaling struct {
    OldCPU, NewCPU       string
    OldMemory, NewMemory string
    Replicas, NewReplicas *int
}

type EnvironmentChange struct {
    Old map[string]string
    New map[string]string
}

type ExtendedPatch struct {
    Operation string              // add, remove, replace
    Path      string
    Value     any
    OldValue  any
}
```

**Configuration types**:
```go
type Rule struct {
    Name   string     `yaml:"name"`
    Expr   string     `yaml:"expr"`
    Result RuleResult `yaml:"result"`
    Mode   EvalMode   `yaml:"mode"` // "first-match" or "collect-all"
}

type ScopesConfig struct {
    Rules PathRules // scope name → []PathRule
}

type TechnologyConfig struct {
    Rules PathRules // tech name → []PathRule
}

type PathRule struct {
    Path   string // glob pattern
    Prefix string // optional file content prefix match (preserved for compat)
}

type SeverityConfig struct {
    Default Severity                    // fallback severity
    Rules   []Rule                      // ordered CEL rules for severity
}

type GitConfig struct {
    VersionFieldPatterns []string       // patterns for version fields in YAML
}
```

### CEL Context Variables

The CEL context has multiple levels depending on what is being evaluated.

**File-level context** (used by `ClassifyFile` + `Evaluate`):
```
file.path        string    # repo-relative file path
file.extension   string    # file extension (e.g., "go", "yaml")
file.language    string    # detected language
file.scopes      []string  # detected scopes
file.tech        []string  # detected technologies
file.is_test     bool      # true if "test" in scopes
file.is_config   bool      # true if extension in [yaml, yml, json, toml, ini, cfg, conf]

vars             map       # user-defined variables (arbitrary keys under vars.*)
```

**Per-resource context** (in addition to `file.*`, used when iterating parsed resources):
```
resource.kind         string          # parser result kind (e.g., "KubernetesResource")
resource.name         string          # resource name
resource.fields       map[string]any  # parser-specific fields
resource.start_line   int
resource.end_line     int

kubernetes.kind        string          # K8s resource kind (shortcut for K8s parser)
kubernetes.api_version string          # K8s API version
kubernetes.namespace   string          # K8s namespace
kubernetes.name        string          # K8s resource name
kubernetes.labels      map[string]string
kubernetes.annotations map[string]string
```

**Commit/change severity context** (used by `AnalyzeCommit` severity evaluation):
```
commit.hash            string
commit.author          string
commit.author_email    string
commit.subject         string
commit.body            string
commit.type            string    # conventional commit type
commit.scope           string    # conventional commit scope
commit.file_count      int
commit.line_changes    int
commit.resource_count  int

change.type            string    # added, modified, deleted, renamed
change.file            string
change.adds            int
change.dels            int
change.fields_changed  []string  # dot-notation field paths (from K8s patches)
change.field_count     int

kubernetes.is_kubernetes    bool
kubernetes.kind             string
kubernetes.api_version      string
kubernetes.namespace        string
kubernetes.name             string
kubernetes.version_upgrade  string     # major, minor, patch, or ""
kubernetes.version_downgrade string    # major, minor, patch, or ""
kubernetes.has_sha_change   bool
kubernetes.replica_delta    int
kubernetes.has_env_change   bool
kubernetes.has_resource_change bool

file.extension         string
file.directory         string
file.is_test           bool
file.is_config         bool
file.tech              string
```

**Context building rules** (matching gavel behavior):
- No nil values — use empty strings, zeros, and false for missing data
- `kubernetes.*` fields are populated only when `kubernetes.is_kubernetes == true`
- `version_upgrade`/`version_downgrade` reflect the highest-severity version change in the resource

### arch.yaml Config Schema

```yaml
# Git configuration
git:
  version_field_patterns:
    - "**.image"
    - "**.tag"
    - "**.version"
    - "**.appVersion"
    - "**.imageTag"

# Scope classification rules (glob patterns)
scopes:
  ci:
    - path: "Makefile"
    - path: "Jenkinsfile"
    - path: "*.sh"
  test:
    - path: "*test*"
    - path: "**/*test/**"
  dependency:
    - path: "go.mod"
    - path: "package.json"
  # arbitrary user-defined scopes allowed
  my-custom-scope:
    - path: "custom/**"

# Technology detection rules (glob patterns)
tech:
  kubernetes:
    - path: "kustomization.yaml"
  helm:
    - path: "Chart.yaml"
    - path: "chart/**"
  go:
    - path: "*.go"
    - path: "go.mod"
  # arbitrary user-defined technologies allowed

# Severity rules (ordered list, evaluated in commit/change context)
severity:
  default: medium
  rules:
    - name: file-deletions
      expr: 'change.type == "deleted"'
      result:
        severity: critical
    - name: k8s-secrets
      expr: 'kubernetes.kind == "Secret"'
      result:
        severity: critical
        message: "Kubernetes Secret modified"
    - name: large-changes
      expr: 'commit.line_changes > 500'
      result:
        severity: critical
    - name: version-downgrade
      expr: 'kubernetes.version_downgrade != ""'
      result:
        severity: high

# General CEL rules (ordered list, evaluated in file/resource context)
rules:
  - name: tag-generated-go
    expr: 'file.extension == "go" && file.path.contains("generated")'
    result:
      tags: ["generated"]
  - name: tag-test-fixtures
    expr: 'file.path.contains("testdata") || file.path.contains("fixtures")'
    result:
      tags: ["test-fixture"]

# Legacy map format also accepted for severity (converted to sorted list):
# severity:
#   default: medium
#   rules:
#     'kubernetes.kind == "Secret"': critical
#     'change.type == "deleted"': critical
```

---

## Success Criteria

- [ ] Both gavel and arch-unit-todos can be refactored to use this library as a dependency
- [ ] All existing classification, parsing, and change detection tests from source projects pass when ported
- [ ] CEL rules can replace and extend current glob-only pattern matching
- [ ] Library works without git (core + OSFileReader) and with git (git subpackage)
- [ ] Configuration is backwards-compatible with existing arch.yaml `scopes`, `tech`, `severity`, and `git` sections
- [ ] Custom CEL functions can be registered by consumers
- [ ] Custom content parsers can be registered by consumers
- [ ] Commit analysis produces identical results to gavel for the same inputs
- [ ] Kubernetes change detection correctly identifies version upgrades/downgrades, scaling changes, and environment changes
- [ ] Severity evaluation with default rules matches gavel's default behavior
- [ ] Scan of a 10k-file repo completes in under 5 seconds on modern hardware

## Testing Requirements

- **Unit tests per package**: Test pattern matching, CEL engine, kubernetes parsing, config merging, patch parsing, version detection, scaling detection independently
- **Integration tests with fixtures**: Test against real repo fixtures in `testdata/` directories
- **Table-driven tests**: Parameterized test cases with input/expected pairs for classification, CEL evaluation, config merging, patch parsing, and Kubernetes change detection
- **Error handling tests**: Verify partial scan failures, malformed YAML, invalid CEL expressions, missing files
- **Concurrency tests**: Verify Scan and AnalyzeCommitHistory produce consistent results under concurrent execution
- **Backward compatibility tests**: Port key tests from gavel and arch-unit-todos to verify identical behavior
- **Change detection tests**: Test version change classification (semver upgrade/downgrade, SHA changes), scaling detection, environment change detection, JSON patch generation
- Tests colocated in `*_test.go` files per package (T-1)

---

## Implementation Checklist

### Phase 1: Core Types and Pattern Matching
- [ ] Initialize go.mod with dependencies
- [ ] Define core types: `FileMap`, `FileReader`, `OSFileReader`, `PathPattern`, `PathRule`, `PathRules`, `Severity`, `SeverityDistribution`
- [ ] Extract and adapt `pattern.go` (glob matching with full-path-then-basename, specificity sorting, negation, doublestar)
- [ ] Normalize paths with `filepath.ToSlash`
- [ ] Extract and adapt language detection from file extension
- [ ] Write table-driven unit tests for pattern matching and severity ordering

### Phase 2: Parser Interface and Kubernetes Parser
- [ ] Define `Parser` interface and `ParseResult` type
- [ ] Extract Kubernetes YAML parser: `ParseYAMLDocuments`, `IsKubernetesResource`, `ExtractKubernetesRef`
- [ ] Include `JSONPath` field in `KubernetesRef`
- [ ] Use `goccy/go-yaml` exclusively for all YAML parsing
- [ ] Write unit tests with multi-document YAML fixtures (empty docs, non-K8s docs, malformed docs)

### Phase 3: CEL Rule Engine
- [ ] Set up CEL environment with all context variable declarations (file, resource, kubernetes, commit, change, vars)
- [ ] Implement rule compilation at init time with error reporting
- [ ] Define `RuleResult` struct (severity, message, tags, fields)
- [ ] Implement ordered rule list with explicit priority
- [ ] Implement both first-match and collect-all evaluation modes
- [ ] Implement per-resource evaluation (iterate ParseResults, evaluate rules for each)
- [ ] Support custom function registration via `cel.Function`
- [ ] Embed default severity rules
- [ ] Support legacy map-format rules (convert to sorted list)
- [ ] Implement context builder functions: `BuildFileContext`, `BuildCommitContext`, `BuildChangeContext`, `BuildKubernetesContext`
- [ ] Write unit tests for compilation, evaluation, modes, custom functions, context building, error cases

### Phase 4: Configuration System
- [ ] Implement YAML config loading (`LoadArchConf` from file)
- [ ] Implement `defaults.yaml` embedding with default scope/tech/severity/version_field_patterns/git config
- [ ] Implement config merging with explicit semantics: scopes prepend, tech prepend, rules prepend, default severity override, severity rules merge
- [ ] Implement directory hierarchy walking for `arch.yaml` discovery (configurable stop-at root)
- [ ] Return defaults-only config when no `arch.yaml` found
- [ ] Implement programmatic config API (`NewConf`, `WithScope`, `WithTech`, `WithCELRule`, `WithParser`, `WithCustomFunc`)
- [ ] Allow programmatic modification of loaded configs
- [ ] Preserve `PathRule.Prefix` for backward compatibility
- [ ] Write tests for config merging (all section combinations), discovery, and programmatic API

### Phase 5: File Classification and Scanning
- [ ] Implement `ClassifyFile(path)` using patterns, language detection, and parsers
- [ ] Implement `Evaluate(fileMap)` to run CEL rules against classified file
- [ ] Implement `Scan(rootPath)` with concurrent file processing
- [ ] Return `ScanResult` with `Files` and `Errors` (partial failure tolerance)
- [ ] Implement ignore pattern support (.gitignore format + arch.yaml ignores)
- [ ] Skip ignored directories early during walk
- [ ] Write integration tests with directory fixtures

### Phase 6: Change Detection
- [ ] Extract and adapt `ParsePatch` — unified diff parsing into per-file CommitChanges with line ranges
- [ ] Extract conventional commit parser (type, scope, trailers, references)
- [ ] Extract commit quality scoring
- [ ] Implement JSON patch generation with old value preservation (`ExtendedPatch`)
- [ ] Implement Kubernetes change detection: before/after comparison, affected document detection, resource matching by kind+name+namespace
- [ ] Implement version change detection: semver parsing, upgrade/downgrade classification, SHA detection, container image parsing
- [ ] Implement scaling detection: replica count, CPU, memory changes
- [ ] Implement environment variable change detection
- [ ] Implement source type detection (kustomize, helm, yaml, flux, argocd)
- [ ] Implement field path extraction (JSON pointer → dot notation)
- [ ] Write unit tests for: patch parsing, version classification, scaling detection, env detection, JSON patch generation

### Phase 7: Commit Analysis
- [ ] Implement `AnalyzerContext` wrapping ArchConf + severity engine + FileReader
- [ ] Implement `AnalyzeCommit`: parse patch → classify files → detect K8s changes → evaluate severity
- [ ] Implement `AnalyzeCommitHistory` with concurrent processing (configurable MaxConcurrent)
- [ ] Implement `AnalyzeOptions` filtering (scope types, commit types, technologies)
- [ ] Implement severity context building from commit + change + K8s change data
- [ ] Wire default severity rules (file deletions, K8s secrets, RBAC, network, large changes, version changes, scaling, env)
- [ ] Write integration tests with commit fixtures

### Phase 8: Git Package
- [ ] Implement `GitFileReader` shelling out to `git` CLI
- [ ] Implement `FindGitRoot`, `ReadFileAtCommit`, `FileExistsAtCommit`
- [ ] Implement arch.yaml discovery that stops at git root
- [ ] Write integration tests with git fixtures (requires git repo in testdata)

### Phase 9: Testing and Polish
- [ ] Port key tests from gavel and arch-unit-todos (classification, change detection, severity evaluation)
- [ ] Add backward compatibility tests for arch.yaml format
- [ ] Add concurrency tests for Scan and AnalyzeCommitHistory
- [ ] Verify no import cycles between packages
- [ ] Verify zero dependency on git package from core
- [ ] Run `make lint` and `make build`

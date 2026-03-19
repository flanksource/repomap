# repomap

A repository structure analysis tool that classifies files by language, scope, and technology. It parses Kubernetes YAML manifests, analyzes git commits, detects version changes, and evaluates configurable severity rules using CEL expressions.

## Install

```bash
go install github.com/flanksource/repomap/cmd/repomap@latest
```

Or build from source:

```bash
make build    # outputs to .bin/repomap
make install  # installs to $GOPATH/bin
```

## Quick Start

```bash
# Scan current directory
repomap

# Scan a specific path
repomap scan ./my-repo

# Show all files including unclassified
repomap scan --all

# Flat table output
repomap scan --flat
```

## Commands

### `scan` (default)

Scan a repository and classify tracked files by language, scope, and Kubernetes references.

```bash
repomap scan [path]
```

| Flag | Description | Default |
|------|-------------|---------|
| `--commit` | Git commit to scan at | `HEAD` |
| `--all` | Show all files including those with no scopes | `false` |
| `--flat` | Output flat table instead of tree | `false` |
| `--cwd` | Override working directory | `.` |

When run without a subcommand, `repomap` defaults to `scan`.

### `config`

Display the merged configuration (user `repomap.yaml` + embedded defaults).

```bash
repomap config [--path ./my-repo]
```

### `eval`

Evaluate CEL expressions against a sample context for testing severity rules.

```bash
# Test an inline expression
repomap eval --expr 'change.type == "deleted"'

# Evaluate rules from a YAML file
repomap eval --file rules.yaml

# Test Kubernetes-specific rules
repomap eval --expr 'kubernetes.kind == "Secret"'
```

### `version`

Print version, commit hash, build date, and Go version.

### Global Flags

| Flag | Description | Default |
|------|-------------|---------|
| `--format` / `-o` | Output format: `pretty`, `json`, `yaml`, `csv`, `table` | `pretty` |

## Configuration

Repomap looks for `repomap.yaml` in the current directory, walking up to the git root. If none is found, embedded defaults are used. User rules are merged with defaults. See [`example.repomap.yaml`](example.repomap.yaml) for a comprehensive example.

### Scope Rules

Scope rules classify files into categories. Three rule types are supported:

#### 1. Path rules

Glob patterns matched against file paths (doublestar syntax: `*`, `?`, `**`, `{a,b}`).

```yaml
scopes:
  rules:
    ci:
      - path: "Makefile"
      - path: ".github/workflows/*.yml"
      - path: "*.sh"
    app:
      - path: "cmd/**"
      - path: "pkg/**"
```

#### 2. Resource rules

Match Kubernetes resources found in YAML files by `kind`, `name`, `namespace`, or `apiVersion`. Glob patterns are supported for all fields. When multiple fields are specified, all must match.

```yaml
scopes:
  rules:
    security:
      - kind: "Secret"
      - kind: "Role"
      - kind: "ClusterRole"
      - kind: "NetworkPolicy"

    networking:
      - kind: "Service"
      - kind: "Ingress"

    database:
      - kind: "*SQL*"                        # glob: matches MySQL, PostgreSQL, etc.

    secrets:
      - kind: "Secret"
        namespace: "production"              # combined: only production secrets

    infrastructure:
      - namespace: "kube-system"             # all resources in kube-system
```

#### 3. CEL rules

[CEL](https://github.com/google/cel-go) expressions evaluated per Kubernetes resource. Context variables: `kubernetes.kind`, `kubernetes.name`, `kubernetes.namespace`, `kubernetes.api_version`, `kubernetes.labels`, `kubernetes.annotations`.

```yaml
scopes:
  rules:
    flux:
      - when: 'kubernetes.api_version.startsWith("source.toolkit.fluxcd.io")'
      - when: 'kubernetes.api_version.startsWith("kustomize.toolkit.fluxcd.io")'

    argo:
      - when: 'kubernetes.api_version.startsWith("argoproj.io")'

    canary:
      - when: 'kubernetes.labels.tier == "canary"'
      - when: 'kubernetes.annotations.deployment_strategy == "canary"'
```

Rules can be mixed freely within a scope — path, resource, and CEL rules are all evaluated independently.

### Built-in Scopes

The following scopes are detected by default: `ci`, `dependency`, `docs`, `test`, `kubernetes`, `helm`, `terraform`, `bazel`, `go`, `nodejs`, `python`, `java`, `ruby`, `rust`, `php`, `shell`, `jenkins`, `docker`, `markdown`, `security`, `networking`.

## CEL Expression Context

Severity rules are evaluated as [CEL](https://github.com/google/cel-go) expressions with the following context variables:

### `commit.*`

| Variable | Type | Description |
|----------|------|-------------|
| `commit.hash` | `string` | Commit hash |
| `commit.author` | `string` | Author name |
| `commit.author_email` | `string` | Author email |
| `commit.type` | `string` | Conventional commit type (`feat`, `fix`, `chore`, etc.) |
| `commit.scope` | `string` | Conventional commit scope |
| `commit.line_changes` | `int` | Total lines changed |
| `commit.file_count` | `int` | Number of files changed |
| `commit.resource_count` | `int` | Number of K8s resources affected |

### `change.*`

| Variable | Type | Description |
|----------|------|-------------|
| `change.type` | `string` | `added`, `modified`, `deleted`, `renamed` |
| `change.file` | `string` | File path |
| `change.adds` | `int` | Lines added |
| `change.dels` | `int` | Lines deleted |
| `change.field_count` | `int` | K8s fields changed |

### `kubernetes.*`

| Variable | Type | Description |
|----------|------|-------------|
| `kubernetes.is_kubernetes` | `bool` | Whether the change involves a K8s resource |
| `kubernetes.kind` | `string` | Resource kind (`Deployment`, `Secret`, etc.) |
| `kubernetes.api_version` | `string` | API version |
| `kubernetes.namespace` | `string` | Resource namespace |
| `kubernetes.name` | `string` | Resource name |
| `kubernetes.version_upgrade` | `string` | `major`, `minor`, or `patch` |
| `kubernetes.version_downgrade` | `string` | `major`, `minor`, or `patch` |
| `kubernetes.has_sha_change` | `bool` | SHA digest changed |
| `kubernetes.replica_delta` | `int` | Replica count change |
| `kubernetes.has_env_change` | `bool` | Environment variables changed |
| `kubernetes.has_resource_change` | `bool` | CPU/memory resources changed |

### `file.*`

| Variable | Type | Description |
|----------|------|-------------|
| `file.extension` | `string` | File extension (`.go`, `.yaml`, etc.) |
| `file.directory` | `string` | Parent directory |
| `file.is_test` | `bool` | Whether the file is a test file |
| `file.is_config` | `bool` | Whether the file is a config file |
| `file.tech` | `string` | Detected technology |

## Default Severity Rules

These rules are applied automatically unless overridden:

| Rule | Severity |
|------|----------|
| File deletions | Critical |
| K8s Secrets | Critical |
| >500 line changes | Critical |
| >20 files changed | Critical |
| >25 K8s resources changed | Critical |
| RBAC resources (Role, ClusterRole, ServiceAccount, etc.) | High |
| Network resources (Service, Ingress, NetworkPolicy) | High |
| Version downgrades | High |
| Major version upgrades | High |
| SHA digest changes | High |
| >100 line changes | High |
| >10 files changed | High |
| `.env` file changes | High |
| Replica scaling (>10 or <-5) | High |
| Environment variable changes | Medium |
| CPU/memory resource changes | Medium |
| Config file modifications | Medium |
| PV/PVC changes | Medium |

## Library Usage

Repomap can also be used as a Go library:

```go
import "github.com/flanksource/repomap"

conf, _ := repomap.GetConf(".")
fm, _ := conf.GetFileMap("cmd/main.go", "HEAD")

fmt.Println(fm.Language)  // "go"
fmt.Println(fm.Scopes)    // [go ci]
```

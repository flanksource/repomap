---
codeBlocks: [bash]
---

# Path-based Scope Rules

### command: Go files match go scope

```bash
../.bin/repomap scope --config configs/path-rules.yaml --path main.go --json
```

- cel: json.scopes.exists(s, s == "go")

### command: Test files match both test and go

```bash
../.bin/repomap scope --config configs/path-rules.yaml --path main_test.go --json
```

- cel: json.scopes.exists(s, s == "go") && json.scopes.exists(s, s == "test")

### command: Makefile matches ci scope

```bash
../.bin/repomap scope --config configs/path-rules.yaml --path Makefile --json
```

- cel: json.scopes.exists(s, s == "ci")

### command: Shell scripts match ci

```bash
../.bin/repomap scope --config configs/path-rules.yaml --path deploy.sh --json
```

- cel: json.scopes.exists(s, s == "ci")

### command: Unknown files match nothing

```bash
../.bin/repomap scope --config configs/path-rules.yaml --path unknown.txt --json
```

- cel: json.scopes.size() == 0

### command: Doublestar glob matches nested

```bash
../.bin/repomap scope --config configs/path-rules.yaml --path cmd/server/main.go --json
```

- cel: json.scopes.exists(s, s == "app")

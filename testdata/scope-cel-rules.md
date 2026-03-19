---
codeBlocks: [bash]
---

# CEL-based Scope Rules

### command: CEL matches kind

```bash
../.bin/repomap scope --config configs/cel-workloads.yaml --kind Deployment --name web-app --json
```

- cel: json.scopes.exists(s, s == "workloads")

### command: CEL rejects non-matching kind

```bash
../.bin/repomap scope --config configs/cel-workloads.yaml --kind Service --name api --json
```

- cel: json.scopes.size() == 0

### command: CEL matches apiVersion prefix

```bash
../.bin/repomap scope --config configs/cel-flux.yaml --kind GitRepository --api-version source.toolkit.fluxcd.io/v1 --name my-repo --json
```

- cel: json.scopes.exists(s, s == "flux")

### command: CEL matches labels

```bash
../.bin/repomap scope --config configs/cel-labels.yaml --kind Deployment --name web-app --labels '{"tier":"canary","app":"web"}' --json
```

- cel: json.scopes.exists(s, s == "canary")

### command: CEL label mismatch

```bash
../.bin/repomap scope --config configs/cel-labels.yaml --kind Deployment --name web-app --labels '{"tier":"production"}' --json
```

- cel: json.scopes.size() == 0

### command: CEL matches annotations

```bash
../.bin/repomap scope --config configs/cel-annotations.yaml --kind Namespace --name staging --annotations '{"managed_by":"terraform"}' --json
```

- cel: json.scopes.exists(s, s == "managed")

### command: CEL with NetworkPolicy

```bash
../.bin/repomap scope --config configs/cel-mixed.yaml --kind NetworkPolicy --name deny-all --json
```

- cel: json.scopes.exists(s, s == "security")

### command: CEL namespace production match

```bash
../.bin/repomap scope --config configs/cel-namespace.yaml --kind Deployment --name api --namespace production --json
```

- cel: json.scopes.exists(s, s == "critical")

### command: CEL namespace rejects staging

```bash
../.bin/repomap scope --config configs/cel-namespace.yaml --kind Deployment --name api --namespace staging --json
```

- cel: json.scopes.size() == 0

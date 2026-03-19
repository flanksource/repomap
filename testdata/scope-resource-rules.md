---
codeBlocks: [bash]
---

# Resource-based Scope Rules

### command: Secret matches security scope

```bash
../.bin/repomap scope --config configs/resource-security.yaml --kind Secret --name db-password --json
```

- cel: json.scopes.exists(s, s == "security")

### command: Service matches networking scope

```bash
../.bin/repomap scope --config configs/resource-networking.yaml --kind Service --name api-gateway --json
```

- cel: json.scopes.exists(s, s == "networking")

### command: Deployment with no matching rule

```bash
../.bin/repomap scope --config configs/resource-security.yaml --kind Deployment --name web-app --json
```

- cel: json.scopes.size() == 0

### command: Glob pattern matches kind

```bash
../.bin/repomap scope --config configs/resource-database.yaml --kind PostgreSQL --name main-db --json
```

- cel: json.scopes.exists(s, s == "database")

### command: Namespace filter matches

```bash
../.bin/repomap scope --config configs/resource-namespace.yaml --kind ConfigMap --name coredns --namespace kube-system --json
```

- cel: json.scopes.exists(s, s == "infrastructure")

### command: Namespace filter rejects non-matching

```bash
../.bin/repomap scope --config configs/resource-namespace.yaml --kind ConfigMap --name app-config --namespace default --json
```

- cel: json.scopes.size() == 0

### command: Combined kind and namespace filter

```bash
../.bin/repomap scope --config configs/resource-combined.yaml --kind Secret --name api-key --namespace production --json
```

- cel: json.scopes.exists(s, s == "secrets")

### command: Combined filter rejects partial match

```bash
../.bin/repomap scope --config configs/resource-combined.yaml --kind Secret --name api-key --namespace staging --json
```

- cel: json.scopes.size() == 0

### command: Wildcard kind matches any resource

```bash
../.bin/repomap scope --config configs/resource-wildcard.yaml --kind CronJob --name cleanup --json
```

- cel: json.scopes.exists(s, s == "kubernetes")

### command: Name glob filter

```bash
../.bin/repomap scope --config configs/resource-name-glob.yaml --kind Deployment --name prometheus-monitoring --json
```

- cel: json.scopes.exists(s, s == "monitoring")

### command: APIVersion filter

```bash
../.bin/repomap scope --config configs/resource-apiversion.yaml --kind Ingress --api-version networking.k8s.io/v1 --json
```

- cel: json.scopes.exists(s, s == "networking")

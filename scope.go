package repomap

type Scopes []ScopeType

func (s Scopes) Contains(scope ScopeType) bool {
	for _, sc := range s {
		if sc == scope {
			return true
		}
	}
	return false
}

func (s Scopes) ToString() []string {
	var result []string
	for _, scope := range s {
		result = append(result, string(scope))
	}
	return result
}

type ScopeType string

const (
	ScopeTypeDocs            ScopeType = "docs"
	ScopeTypeGeneral         ScopeType = "general"
	ScopeTypeCI              ScopeType = "ci"
	ScopeTypeMicroservices   ScopeType = "microservices"
	ScopeTypeObservability   ScopeType = "observability"
	ScopeTypeNetworking      ScopeType = "networking"
	ScopeTypeSecurity        ScopeType = "security"
	ScopeTypeDatabase        ScopeType = "database"
	ScopeTypeInfrastructure  ScopeType = "infrastructure"
	ScopeTypeIaC             ScopeType = "iac"
	ScopeTypeApp             ScopeType = "app"
	ScopeTypeDeployment      ScopeType = "deployment"
	ScopeTypeCD              = ScopeTypeDeployment
	ScopeTypeScaling         ScopeType = "scaling"
	ScopeTypeTest            ScopeType = "test"
	ScopeTypeReliability     ScopeType = "reliability"
	ScopeTypePerformance     ScopeType = "performance"
	ScopeTypeCostOptimization ScopeType = "cost_optimization"
	ScopeTypeSecrets         ScopeType = "secrets"
	ScopeTypeConfig          ScopeType = "config"
	ScopeTypeDependency      ScopeType = "dependency"
	ScopeTypeML              ScopeType = "ml"
	ScopeTypeOther           ScopeType = "other"
	ScopeTypeUnknown         ScopeType = ""
)

type Technology []ScopeTechnology

func (t Technology) ToString() []string {
	var result []string
	for _, tech := range t {
		result = append(result, string(tech))
	}
	return result
}

type ScopeTechnology string

const (
	TechKubernetes    ScopeTechnology = "kubernetes"
	TechBazel         ScopeTechnology = "bazel"
	TechDocker        ScopeTechnology = "docker"
	TechTerraform     ScopeTechnology = "terraform"
	TechMarkdown      ScopeTechnology = "markdown"
	TechPrometheus    ScopeTechnology = "prometheus"
	TechGrafana       ScopeTechnology = "grafana"
	TechJenkins       ScopeTechnology = "jenkins"
	TechAnsible       ScopeTechnology = "ansible"
	TechHelm          ScopeTechnology = "helm"
	TechGitOps        ScopeTechnology = "gitops"
	TechAWS           ScopeTechnology = "aws"
	TechGCP           ScopeTechnology = "gcp"
	TechAzure         ScopeTechnology = "azure"
	TechLinux         ScopeTechnology = "linux"
	TechOpenshift     ScopeTechnology = "openshift"
	TechMongoDB       ScopeTechnology = "mongodb"
	TechPostgreSQL    ScopeTechnology = "postgresql"
	TechMySQL         ScopeTechnology = "mysql"
	TechRedis         ScopeTechnology = "redis"
	TechNginx         ScopeTechnology = "nginx"
	TechClickhouse    ScopeTechnology = "clickhouse"
	TechKafka         ScopeTechnology = "kafka"
	TechCassandra     ScopeTechnology = "cassandra"
	TechGitlab        ScopeTechnology = "gitlab"
	TechArgoCD        ScopeTechnology = "argocd"
	TechFluxCD        ScopeTechnology = "fluxcd"
	TechOpenTelemetry ScopeTechnology = "opentelemetry"
	TechGitHubActions ScopeTechnology = "github_actions"
	TechPython        ScopeTechnology = "python"
	TechJava          ScopeTechnology = "java"
	TechRuby          ScopeTechnology = "ruby"
	TechRust          ScopeTechnology = "rust"
	TechPHP           ScopeTechnology = "php"
	TechNodeJS        ScopeTechnology = "nodejs"
	TechGo            ScopeTechnology = "go"
	TechShell         ScopeTechnology = "shell"
	TechPowershell    ScopeTechnology = "powershell"
	TechReact         ScopeTechnology = "react"
	TechBash          ScopeTechnology = "bash"
	TechSQL           ScopeTechnology = "sql"
)

type CommitType string

const (
	CommitTypeFeat       CommitType = "feat"
	CommitTypeFix        CommitType = "fix"
	CommitTypeChore      CommitType = "chore"
	CommitTypeDocs       CommitType = "docs"
	CommitTypeStyle      CommitType = "style"
	CommitTypeRefactor   CommitType = "refactor"
	CommitTypePerf       CommitType = "perf"
	CommitTypeTest       CommitType = "test"
	CommitTypeCi         CommitType = "ci"
	CommitTypeBuild      CommitType = "build"
	CommitTypeRevert     CommitType = "revert"
	CommitTypeConfig     CommitType = "config"
	CommitTypeOther      CommitType = "other"
	CommitTypeSecurity   CommitType = "security"
	CommitTypeDependency CommitType = "dependency"
	CommitTypeUnknown    CommitType = ""
)

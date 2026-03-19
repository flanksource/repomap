package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/repomap"
	"github.com/flanksource/repomap/kubernetes"
	ghodss "github.com/ghodss/yaml"
)

type ScopeOptions struct {
	Config    string `json:"config" flag:"config" help:"Path to YAML config file with scope rules"`
	Path      string `json:"path" flag:"path" help:"File path to match against scope rules"`
	Kind      string `json:"kind" flag:"kind" help:"Kubernetes resource kind"`
	Name      string `json:"name" flag:"name" help:"Kubernetes resource name"`
	Namespace string `json:"namespace" flag:"namespace" help:"Kubernetes resource namespace"`
	API       string `json:"apiVersion" flag:"api-version" help:"Kubernetes apiVersion"`
	Labels    string `json:"labels" flag:"labels" help:"Kubernetes labels as JSON object"`
	Annot     string `json:"annotations" flag:"annotations" help:"Kubernetes annotations as JSON object"`
}

func (opts ScopeOptions) GetName() string { return "scope" }

func (opts ScopeOptions) Help() api.Text {
	return clicky.Text(`Evaluate scope rules against a file path or Kubernetes resource.

Reads scope rules from a YAML config file and outputs matching scopes
as a JSON array.

EXAMPLES:
  repomap scope -c config.yaml --path main.go
  repomap scope -c config.yaml --kind Deployment --name web-app
  repomap scope -c config.yaml --kind Secret --namespace production`)
}

type ScopeResult struct {
	Scopes []string `json:"scopes"`
}

func (r ScopeResult) Pretty() api.Text {
	data, _ := json.Marshal(r.Scopes)
	return clicky.Text(string(data))
}

func init() {
	cmd := clicky.AddCommand(rootCmd, ScopeOptions{}, runScope)
	cmd.Short = "Evaluate scope rules against a path or resource"
}

func runScope(opts ScopeOptions) (any, error) {
	if opts.Config == "" {
		return nil, fmt.Errorf("--config is required")
	}

	data, err := os.ReadFile(opts.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var config struct {
		Scopes repomap.ScopesConfig `json:"scopes"`
	}
	if err := ghodss.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	if err := config.Scopes.Validate(); err != nil {
		return nil, fmt.Errorf("invalid scopes config: %w", err)
	}

	var scopes repomap.Scopes

	if opts.Path != "" {
		pathScopes, _ := config.Scopes.GetScopesByPath(opts.Path)
		scopes = pathScopes
	}

	if opts.Kind != "" || opts.Name != "" || opts.Namespace != "" || opts.API != "" {
		ref := kubernetes.KubernetesRef{
			Kind:       opts.Kind,
			Name:       opts.Name,
			Namespace:  opts.Namespace,
			APIVersion: opts.API,
		}
		if opts.Labels != "" {
			if err := json.Unmarshal([]byte(opts.Labels), &ref.Labels); err != nil {
				return nil, fmt.Errorf("invalid labels JSON: %w", err)
			}
		}
		if opts.Annot != "" {
			if err := json.Unmarshal([]byte(opts.Annot), &ref.Annotations); err != nil {
				return nil, fmt.Errorf("invalid annotations JSON: %w", err)
			}
		}
		refScopes, _ := config.Scopes.GetScopesByRefs([]kubernetes.KubernetesRef{ref})
		scopes = scopes.Merge(refScopes)
	}

	result := scopes.ToString()
	if result == nil {
		result = []string{}
	}
	sort.Strings(result)

	return ScopeResult{Scopes: result}, nil
}

package repomap

import "testing"

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"main.go", "go"},
		{"app.py", "python"},
		{"Component.tsx", "typescript"},
		{"index.js", "javascript"},
		{"Cargo.toml", "toml"},
		{"deploy.yaml", "yaml"},
		{"deploy.yml", "yaml"},
		{"style.css", "css"},
		{"page.html", "html"},
		{"query.sql", "sql"},
		{"main.tf", "terraform"},
		{"script.sh", "shell"},
		{"Gemfile", "unknown"},
		{"binary.exe", "unknown"},
	}

	for _, tt := range tests {
		if got := DetectLanguage(tt.path); got != tt.expected {
			t.Errorf("DetectLanguage(%q) = %q, want %q", tt.path, got, tt.expected)
		}
	}
}

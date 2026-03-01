package repomap

import "path/filepath"

var extensionLanguages = map[string]string{
	".go":       "go",
	".py":       "python",
	".pyi":      "python",
	".java":     "java",
	".kt":       "java",
	".kts":      "java",
	".groovy":   "java",
	".ts":       "typescript",
	".tsx":      "typescript",
	".js":       "javascript",
	".jsx":      "javascript",
	".mjs":      "javascript",
	".cjs":      "javascript",
	".rs":       "rust",
	".rb":       "ruby",
	".md":       "markdown",
	".mdx":      "markdown",
	".markdown": "markdown",
	".xml":      "xml",
	".xsd":      "xml",
	".xslt":     "xml",
	".xsl":      "xml",
	".php":      "php",
	".sh":       "shell",
	".bash":     "shell",
	".zsh":      "shell",
	".bat":      "shell",
	".ps1":      "shell",
	".tf":       "terraform",
	".tfvars":   "terraform",
	".yaml":     "yaml",
	".yml":      "yaml",
	".json":     "json",
	".toml":     "toml",
	".sql":      "sql",
	".css":      "css",
	".scss":     "css",
	".html":     "html",
	".htm":      "html",
}

func DetectLanguage(path string) string {
	ext := filepath.Ext(path)
	if lang, ok := extensionLanguages[ext]; ok {
		return lang
	}
	return "unknown"
}

package deps

import (
	"fmt"
	"regexp"
	"strings"
)

// imageRef is a parsed container image reference split into name, tag, and digest.
type imageRef struct {
	Name    string
	Version string
	Digest  string
}

var argRefPattern = regexp.MustCompile(`\$\{(\w+)\}|\$(\w+)`)

// parseDockerfileFrom extracts the external base images referenced by FROM
// directives. It substitutes ARG/ENV defaults declared earlier in the file,
// excludes references to internal build stages (FROM ... AS <stage>) and the
// scratch terminal, and de-duplicates. Unresolved ARG references are warned.
func parseDockerfileFrom(content string) (bases []imageRef, warnings []string) {
	args := map[string]string{}
	stages := map[string]bool{}
	seen := map[string]bool{}

	for _, line := range dockerfileLines(content) {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		switch strings.ToUpper(fields[0]) {
		case "ARG", "ENV":
			if name, val, ok := parseArgAssign(fields[1:]); ok {
				args[name] = val
			}
		case "FROM":
			ref, stage, ok, warn := parseFromDirective(fields[1:], args, stages)
			if warn != "" {
				warnings = append(warnings, warn)
			}
			if stage != "" {
				stages[strings.ToLower(stage)] = true
			}
			if !ok {
				continue
			}
			key := ref.Name + ":" + ref.Version + "@" + ref.Digest
			if !seen[key] {
				seen[key] = true
				bases = append(bases, ref)
			}
		}
	}
	return bases, warnings
}

// parseFromDirective interprets the tokens after FROM: it strips flags
// (--platform=...), resolves the image token, and records an AS stage name.
// ok is false for internal stage references, scratch, and unresolved ARGs.
func parseFromDirective(tokens []string, args map[string]string, stages map[string]bool) (ref imageRef, stage string, ok bool, warn string) {
	var image string
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		switch {
		case strings.HasPrefix(tok, "--"):
			continue
		case image == "":
			image = tok
		case strings.EqualFold(tok, "AS") && i+1 < len(tokens):
			stage = tokens[i+1]
			i++
		}
	}
	if image == "" {
		return imageRef{}, stage, false, ""
	}
	resolved, complete := substituteArgs(image, args)
	if !complete {
		return imageRef{}, stage, false, fmt.Sprintf("unresolved build arg in FROM %q", image)
	}
	if strings.EqualFold(resolved, "scratch") || stages[strings.ToLower(resolved)] {
		return imageRef{}, stage, false, ""
	}
	return parseImageRef(resolved), stage, true, ""
}

// substituteArgs replaces ${VAR}/$VAR with known ARG/ENV defaults; complete is
// false when any reference cannot be resolved.
func substituteArgs(s string, args map[string]string) (out string, complete bool) {
	complete = true
	out = argRefPattern.ReplaceAllStringFunc(s, func(m string) string {
		name := strings.Trim(m, "${}")
		if v, ok := args[name]; ok {
			return v
		}
		complete = false
		return m
	})
	return out, complete
}

// parseImageRef splits an image reference into name, tag, and digest, leaving a
// registry host:port intact.
func parseImageRef(s string) imageRef {
	var digest string
	if at := strings.Index(s, "@"); at >= 0 {
		digest = s[at+1:]
		s = s[:at]
	}
	name, version := s, ""
	if i := imageTagSeparator(s); i >= 0 {
		name, version = s[:i], s[i+1:]
	}
	return imageRef{Name: name, Version: version, Digest: digest}
}

func parseArgAssign(tokens []string) (name, value string, ok bool) {
	if len(tokens) == 0 {
		return "", "", false
	}
	eq := strings.SplitN(tokens[0], "=", 2)
	if len(eq) != 2 {
		return "", "", false
	}
	return eq[0], strings.Trim(eq[1], `"'`), true
}

// dockerfileLines joins backslash line continuations and trims carriage returns.
func dockerfileLines(content string) []string {
	var lines []string
	var buf strings.Builder
	for _, ln := range strings.Split(content, "\n") {
		ln = strings.TrimRight(ln, "\r")
		trimmedRight := strings.TrimRight(ln, " \t")
		if strings.HasSuffix(trimmedRight, "\\") {
			buf.WriteString(strings.TrimSuffix(trimmedRight, "\\"))
			buf.WriteString(" ")
			continue
		}
		buf.WriteString(ln)
		lines = append(lines, strings.TrimSpace(buf.String()))
		buf.Reset()
	}
	if buf.Len() > 0 {
		lines = append(lines, strings.TrimSpace(buf.String()))
	}
	return lines
}

// Package haropenapi converts recorded HTTP samples into conservative OpenAPI drafts.
package haropenapi

import (
	"fmt"
	"io"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Title         string          `yaml:"title"`
	IncludeHosts  []string        `yaml:"include_hosts"`
	ExcludeHosts  []string        `yaml:"exclude_hosts"`
	IncludePaths  []string        `yaml:"include_paths"`
	ExcludePaths  []string        `yaml:"exclude_paths"`
	IncludeStatic bool            `yaml:"include_static"`
	PathTemplates []PathTemplate  `yaml:"path_templates"`
	Operations    []OperationRule `yaml:"operations"`
}

type PathTemplate struct {
	Host     string `yaml:"host"`
	Pattern  string `yaml:"pattern"`
	Template string `yaml:"template"`
	re       *regexp.Regexp
}

type OperationRule struct {
	Host        string   `yaml:"host"`
	Method      string   `yaml:"method"`
	Path        string   `yaml:"path"`
	Summary     string   `yaml:"summary"`
	Description string   `yaml:"description"`
	Tags        []string `yaml:"tags"`
}

// ReadConfig rejects misspelled options instead of silently ignoring them.
func ReadConfig(r io.Reader) (Config, error) {
	var c Config
	d := yaml.NewDecoder(r)
	d.KnownFields(true)
	if err := d.Decode(&c); err != nil {
		return c, fmt.Errorf("decode OpenAPI rules: %w", err)
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return c, fmt.Errorf("rules must contain one YAML document")
	}
	return c, c.prepare()
}

func (c *Config) prepare() error {
	for _, h := range append(append([]string{}, c.IncludeHosts...), c.ExcludeHosts...) {
		if h == "" || strings.ContainsAny(h, "/:@?# ") || strings.Contains(strings.TrimPrefix(h, "*."), "*") {
			return fmt.Errorf("host filters must be hostnames or *.hostname")
		}
	}
	for _, p := range append(append([]string{}, c.IncludePaths...), c.ExcludePaths...) {
		if _, err := regexp.Compile(p); err != nil {
			return fmt.Errorf("invalid path filter: %w", err)
		}
	}
	for i := range c.PathTemplates {
		t := &c.PathTemplates[i]
		if t.Host != "" && !validHostPattern(t.Host) {
			return fmt.Errorf("invalid path_templates host at index %d", i)
		}
		r, err := regexp.Compile(t.Pattern)
		if err != nil || t.Pattern == "" {
			return fmt.Errorf("invalid path_templates pattern at index %d", i)
		}
		if !validTemplate(t.Template) {
			return fmt.Errorf("invalid path_templates template at index %d", i)
		}
		t.re = r
	}
	for _, r := range c.Operations {
		if r.Host != "" && !validHostPattern(r.Host) {
			return fmt.Errorf("invalid operation rule host")
		}
		if !validTemplate(r.Path) || (r.Method != "" && !validMethod(strings.ToLower(r.Method))) {
			return fmt.Errorf("invalid operation rule path or method")
		}
	}
	return nil
}

var variable = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func validHostPattern(h string) bool {
	return h != "" && !strings.ContainsAny(h, "/:@?# ") && !strings.Contains(strings.TrimPrefix(h, "*."), "*")
}

func validTemplate(s string) bool {
	if !strings.HasPrefix(s, "/") || strings.ContainsAny(s, "?#\r\n") {
		return false
	}
	rest := variable.ReplaceAllString(s, "")
	if strings.ContainsAny(rest, "{}") {
		return false
	}
	seen := map[string]bool{}
	for _, m := range variable.FindAllStringSubmatch(s, -1) {
		if seen[m[1]] {
			return false
		}
		seen[m[1]] = true
	}
	return true
}

func hostMatches(pattern, host string) bool {
	pattern, host = strings.ToLower(pattern), strings.ToLower(host)
	if strings.HasPrefix(pattern, "*.") {
		return strings.HasSuffix(host, pattern[1:]) && host != pattern[2:]
	}
	return pattern == host
}
func hostIn(patterns []string, host string) bool {
	for _, p := range patterns {
		if hostMatches(p, host) {
			return true
		}
	}
	return false
}
func pathIn(patterns []string, path string) bool {
	for _, p := range patterns {
		if regexp.MustCompile(p).MatchString(path) {
			return true
		}
	}
	return false
}
func validMethod(m string) bool {
	switch m {
	case "get", "put", "post", "delete", "options", "head", "patch", "trace":
		return true
	}
	return false
}

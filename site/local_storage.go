package site

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/chromedp/chromedp"
)

// LocalStorageConfig maps JSON fields from one same-origin storage key to
// session aliases. Token mappings and auth query parameters reference aliases.
type LocalStorageConfig struct {
	Key    string            `yaml:"key"`
	Fields map[string]string `yaml:"fields"`
	Query  map[string]string `yaml:"query,omitempty"`
}

func (c Config) validateLocalStorage() error {
	source := c.Auth.LocalStorage
	if strings.TrimSpace(source.Key) == "" || len(source.Fields) == 0 {
		return fmt.Errorf("auth.local_storage requires a key and at least one field")
	}
	for alias, path := range source.Fields {
		if (&http.Cookie{Name: alias, Value: "x"}).Valid() != nil || strings.TrimSpace(path) == "" {
			return fmt.Errorf("auth.local_storage has an invalid field alias or path")
		}
	}
	for param, alias := range source.Query {
		if strings.TrimSpace(param) == "" || source.Fields[alias] == "" {
			return fmt.Errorf("auth.local_storage.query must reference configured field aliases")
		}
	}
	_, err := c.localStorageAuthURL(c.ResolvedBaseURL())
	return err
}

func (c Config) captureLocalStorage(ctx context.Context, cookies string) (string, error) {
	source := c.Auth.LocalStorage
	base, _ := json.Marshal(c.ResolvedBaseURL())
	key, _ := json.Marshal(source.Key)
	var raw string
	expression := fmt.Sprintf(`location.origin === new URL(%s).origin ? localStorage.getItem(%s) || "" : ""`, base, key)
	if err := chromedp.Run(ctx, chromedp.Evaluate(expression, &raw)); err != nil {
		return "", fmt.Errorf("waiting to read same-origin localStorage")
	}
	if raw == "" {
		return "", fmt.Errorf("waiting for the configured localStorage key on the site's origin")
	}
	return source.merge(cookies, raw)
}

func (s LocalStorageConfig) merge(cookies, raw string) (string, error) {
	var payload any
	if !json.Valid([]byte(raw)) {
		return "", fmt.Errorf("configured localStorage value is not valid JSON")
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return "", fmt.Errorf("could not decode configured localStorage value")
	}
	var parts []string
	if filtered := s.NetworkCookieHeader(cookies); filtered != "" {
		parts = append(parts, filtered)
	}
	aliases := make([]string, 0, len(s.Fields))
	for alias := range s.Fields {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	for _, alias := range aliases {
		value, _ := jsonPath(payload, s.Fields[alias])
		var text string
		switch v := value.(type) {
		case string:
			text = v
		case json.Number:
			text = v.String()
		}
		// Aliases share the existing cookie export format for token consumers.
		if text == "" || strings.ContainsAny(text, " ,") || (&http.Cookie{Name: alias, Value: text}).Valid() != nil {
			return "", fmt.Errorf("missing or invalid localStorage field %q", alias)
		}
		parts = append(parts, alias+"="+text)
	}
	return strings.Join(parts, "; "), nil
}

// NetworkCookieHeader removes session aliases before sending cookies to the site.
func (s *LocalStorageConfig) NetworkCookieHeader(raw string) string {
	if s == nil {
		return raw
	}
	var cookies []string
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		name, _, ok := strings.Cut(part, "=")
		if ok && s.Fields[strings.TrimSpace(name)] == "" {
			cookies = append(cookies, part)
		}
	}
	return strings.Join(cookies, "; ")
}

func (c Config) localStorageAuthURL(baseURL string) (*url.URL, error) {
	base, err := url.Parse(baseURL)
	if err != nil || base.Hostname() == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" ||
		(base.Scheme != "http" && base.Scheme != "https") {
		return nil, fmt.Errorf("auth.local_storage requires a valid HTTP(S) base_url")
	}
	path, err := url.Parse(c.Auth.Path)
	if err != nil || path.Host != "" || path.Scheme != "" || path.Fragment != "" ||
		!strings.HasPrefix(c.Auth.Path, "/") || strings.HasPrefix(c.Auth.Path, "//") {
		return nil, fmt.Errorf("auth.local_storage requires a same-origin auth.path")
	}
	return url.Parse(baseURL + c.Auth.Path)
}

func (c Config) authURL(baseURL, raw string) (string, error) {
	source := c.Auth.LocalStorage
	if source == nil {
		return baseURL + c.Auth.Path, nil
	}
	target, err := c.localStorageAuthURL(baseURL)
	if err != nil {
		return "", err
	}
	for alias := range source.Fields {
		if value, ok := cookieValue(raw, alias); !ok || value == "" {
			return "", fmt.Errorf("session is missing field %q; run login to refresh it", alias)
		}
	}
	query := target.Query()
	for param, alias := range source.Query {
		value, _ := cookieValue(raw, alias)
		query.Set(param, value)
	}
	target.RawQuery = query.Encode()
	return target.String(), nil
}

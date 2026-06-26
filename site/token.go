package site

import "strings"

// TokenConfig derives an HTTP header from a named cookie value.
type TokenConfig struct {
	Cookie string `yaml:"cookie"`
	Header string `yaml:"header"`
	Prefix string `yaml:"prefix"`
}

// AuthHeaders returns extra request headers derived from the cookie header.
func (a AuthConfig) AuthHeaders(cookieHeader string) map[string]string {
	return a.authHeaders(cookieHeader)
}

func (a AuthConfig) authHeaders(cookieHeader string) map[string]string {
	if a.Token == nil || a.Token.Cookie == "" {
		return nil
	}
	value, ok := cookieValue(cookieHeader, a.Token.Cookie)
	if !ok || value == "" {
		return nil
	}

	header := a.Token.Header
	if header == "" {
		header = "Authorization"
	}
	prefix := a.Token.Prefix
	if prefix == "" && header == "Authorization" {
		prefix = "Bearer "
	}

	return map[string]string{header: prefix + value}
}

func cookieValue(cookieHeader, name string) (string, bool) {
	for _, part := range strings.Split(cookieHeader, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(key) == name {
			return strings.TrimSpace(value), true
		}
	}
	return "", false
}

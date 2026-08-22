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
	tokens := a.Tokens
	if a.Token != nil {
		tokens = append([]TokenConfig{*a.Token}, tokens...)
	}
	if len(tokens) == 0 {
		return nil
	}

	headers := map[string]string{}
	for _, token := range tokens {
		if token.Cookie == "" {
			continue
		}
		value, ok := cookieValue(cookieHeader, token.Cookie)
		if !ok || value == "" {
			continue
		}

		header := token.Header
		if header == "" {
			header = "Authorization"
		}
		prefix := token.Prefix
		if prefix == "" && header == "Authorization" {
			prefix = "Bearer "
		}
		headers[header] = prefix + value
	}
	if len(headers) == 0 {
		return nil
	}
	return headers
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

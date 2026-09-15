package site

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// FetchSession validates cookies and returns session details.
func (c Config) FetchSession(cookieHeader string) (Session, error) {
	username, payload, err := c.validateHTTP(cookieHeader)
	if err != nil {
		return Session{}, err
	}
	session := Session{Username: username}
	if c.Auth.UserIDJSONPath != "" {
		if value, ok := jsonPath(payload, c.Auth.UserIDJSONPath); ok {
			session.UserID = toInt64(value)
		}
	}
	return session, nil
}

// ValidateSession checks cookies using the site auth config.
func (c Config) ValidateSession(baseURL, cookieHeader string) (string, error) {
	username, _, err := c.validateHTTPWithBase(baseURL, cookieHeader)
	return username, err
}

func (c Config) validateHTTP(cookieHeader string) (string, map[string]any, error) {
	return c.validateHTTPWithBase(c.ResolvedBaseURL(), cookieHeader)
}

func (c Config) validateHTTPWithBase(baseURL, cookieHeader string) (string, map[string]any, error) {
	if c.Auth.Path == "" {
		return "", nil, fmt.Errorf("site %q has no auth.path configured", c.ID)
	}

	method := c.Auth.Method
	if method == "" {
		method = http.MethodGet
	}
	okStatus := c.Auth.OKStatus
	if okStatus == 0 {
		okStatus = http.StatusOK
	}

	url := baseURL + c.Auth.Path
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return "", nil, fmt.Errorf("build auth request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cookie", cookieHeader)
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; BrowserAuth-CLI/1.0)")
	req.Header.Set("Referer", baseURL+"/")
	req.Header.Set("Origin", baseURL)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	for key, value := range c.Auth.authHeaders(cookieHeader) {
		req.Header.Set(key, value)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("auth request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, fmt.Errorf("read auth response: %w", err)
	}
	if resp.StatusCode != okStatus {
		return "", nil, fmt.Errorf("auth check failed with HTTP %d", resp.StatusCode)
	}

	var payload any
	if len(body) > 0 {
		if err := json.Unmarshal(body, &payload); err != nil {
			return "", nil, fmt.Errorf("decode auth response: %w", err)
		}
	}

	return c.validatePayload(payload)
}

func (c Config) validatePayload(payload any) (string, map[string]any, error) {
	for _, path := range c.Auth.IdentityJSONPaths {
		value, ok := jsonPath(payload, path)
		if !ok || isEmptyValue(value) {
			return "", nil, fmt.Errorf("auth check missing identity at %s", path)
		}
	}

	username := ""
	for _, path := range c.Auth.UsernameJSONPaths {
		value, ok := jsonPath(payload, path)
		if !ok {
			continue
		}
		text := strings.TrimSpace(fmt.Sprint(value))
		if text == "" {
			continue
		}
		if forbiddenUsername(text, c.Auth.UsernameForbidden) {
			return "", nil, fmt.Errorf("auth check returned forbidden username %q", text)
		}
		username = text
		break
	}

	if username == "" {
		for _, path := range c.Auth.IdentityJSONPaths {
			value, ok := jsonPath(payload, path)
			if ok && !isEmptyValue(value) {
				username = fmt.Sprintf("id:%v", value)
				break
			}
		}
	}
	if username == "" {
		return "", nil, fmt.Errorf("auth check could not resolve username")
	}

	root, _ := payload.(map[string]any)
	return username, root, nil
}

func toInt64(v any) int64 {
	switch typed := v.(type) {
	case float64:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	case string:
		var n int64
		fmt.Sscan(typed, &n)
		return n
	default:
		return 0
	}
}

func forbiddenUsername(name string, blocked []string) bool {
	for _, item := range blocked {
		if strings.EqualFold(name, item) {
			return true
		}
	}
	return false
}

func isEmptyValue(v any) bool {
	switch typed := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(typed) == ""
	case float64:
		return typed == 0
	case bool:
		return !typed
	default:
		return false
	}
}

func jsonPath(data any, path string) (any, bool) {
	current := data
	for _, part := range strings.Split(path, ".") {
		if part == "" {
			return nil, false
		}
		switch node := current.(type) {
		case map[string]any:
			next, ok := node[part]
			if !ok {
				return nil, false
			}
			current = next
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(node) {
				return nil, false
			}
			current = node[index]
		default:
			return nil, false
		}
	}
	return current, true
}

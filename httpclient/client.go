package httpclient

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"unicode/utf8"

	"skills-browserauth/store"
)

// AuthHeaderBuilder derives extra headers (e.g. Bearer token) from a cookie header.
type AuthHeaderBuilder func(cookieHeader string) map[string]string

// Client performs cookie-authenticated HTTP requests for a web app.
type Client struct {
	BaseURL     string
	Cookie      string
	AuthHeaders AuthHeaderBuilder
	NoFail      bool
	httpClient  *http.Client
}

// New builds a client from store config and optional no-fail env.
type NewOptions struct {
	BaseURL     string
	Names       store.AppNames
	AuthHeaders AuthHeaderBuilder
	NoFailEnv   string
}

// NewDirect creates a Client with explicit base URL, cookie, and HTTP client.
func NewDirect(baseURL, cookie string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{}
	}
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		Cookie:     cookie,
		httpClient: hc,
	}
}

// New creates a Client.
func New(opts NewOptions) (*Client, error) {
	cookieHeader, err := store.ResolveCookie(opts.Names)
	if err != nil {
		return nil, err
	}
	noFail := false
	if opts.NoFailEnv != "" && os.Getenv(opts.NoFailEnv) == "1" {
		noFail = true
	}
	return &Client{
		BaseURL:     strings.TrimRight(opts.BaseURL, "/"),
		Cookie:      cookieHeader,
		AuthHeaders: opts.AuthHeaders,
		NoFail:      noFail,
		httpClient:  &http.Client{},
	}, nil
}

// Do sends an HTTP request with browser-like headers and session cookie.
func (c *Client) Do(method, path string, body io.Reader, extraHeaders map[string]string) ([]byte, int, error) {
	req, err := http.NewRequest(method, c.BaseURL+path, body)
	if err != nil {
		return nil, 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cookie", c.Cookie)
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; BrowserAuth-CLI/1.0)")
	req.Header.Set("Referer", c.BaseURL+"/")
	req.Header.Set("Origin", c.BaseURL)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	if c.AuthHeaders != nil {
		for k, v := range c.AuthHeaders(c.Cookie) {
			req.Header.Set(k, v)
		}
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}
	if !c.NoFail && resp.StatusCode >= 400 {
		msg := fmt.Sprintf("API failed: %s %s (HTTP %d)", method, path, resp.StatusCode)
		if snippet := safeBodySnippet(respBody); snippet != "" {
			msg += "\n" + snippet
		}
		return respBody, resp.StatusCode, fmt.Errorf("%s", msg)
	}
	return respBody, resp.StatusCode, nil
}

func safeBodySnippet(body []byte) string {
	const max = 400
	if len(body) == 0 || !utf8.Valid(body) {
		return ""
	}
	s := strings.TrimSpace(string(body))
	if len(s) > max {
		s = s[:max] + "..."
	}
	if strings.Contains(strings.ToLower(s), "<html") {
		return "(HTML response omitted)"
	}
	return s
}

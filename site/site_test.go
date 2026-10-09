package site

import (
	"strings"
	"testing"
)

func TestJsonPathNested(t *testing.T) {
	payload := map[string]any{
		"data": map[string]any{
			"userId":   float64(410342),
			"nickName": "sichang824",
		},
	}
	v, ok := jsonPath(payload, "data.nickName")
	if !ok || v != "sichang824" {
		t.Fatalf("got %v ok=%v", v, ok)
	}
}

func TestForbiddenUsername(t *testing.T) {
	if !forbiddenUsername("anonymous", []string{"anonymous"}) {
		t.Fatal("expected forbidden")
	}
	if forbiddenUsername("alice", []string{"anonymous"}) {
		t.Fatal("expected allowed")
	}
}

func TestToInt64(t *testing.T) {
	if got := toInt64(float64(410342)); got != 410342 {
		t.Fatalf("got %d", got)
	}
}

func TestParseCookieHeader(t *testing.T) {
	pairs := parseCookieHeader("a=1; token=abc=def; malformed; =empty")
	if len(pairs) != 2 {
		t.Fatalf("got %d pairs", len(pairs))
	}
	if pairs[1].name != "token" || pairs[1].value != "abc=def" {
		t.Fatalf("unexpected pair: %#v", pairs[1])
	}
}

func TestValidateBrowserPathSameOriginOnly(t *testing.T) {
	if err := validateBrowserPath("/api/query?month=1"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"https://evil.example/x", "//evil.example/x", "relative/path"} {
		if err := validateBrowserPath(path); err == nil {
			t.Fatalf("expected %q to be rejected", path)
		}
	}
}

func TestBrowserConfigDefaults(t *testing.T) {
	cfg := Config{Auth: AuthConfig{Transport: "browser"}}
	if cfg.BrowserIdleTimeout() != 0 || cfg.BrowserMaxLifetime() != 0 {
		t.Fatalf("unexpected defaults: idle=%s lifetime=%s", cfg.BrowserIdleTimeout(), cfg.BrowserMaxLifetime())
	}
	if !cfg.BrowserInjectCookie() {
		t.Fatal("saved cookie injection should default to enabled")
	}
}

func TestBrowserConfigCanDisableCookieInjection(t *testing.T) {
	disabled := false
	cfg := Config{Browser: BrowserConfig{InjectCookie: &disabled}}
	if cfg.BrowserInjectCookie() {
		t.Fatal("saved cookie injection should be disabled")
	}
}

func TestParseConfigRejectsInvalidBrowserConfig(t *testing.T) {
	base := "id: bad\nbase_url: https://example.com\ncookie:\n  magic: BADENC\\x01\nbrowser:\n"
	for _, suffix := range []string{"  idle_timeout: soon\n", "  max_lifetime: 0s\n"} {
		if _, err := parseConfig([]byte(base + suffix)); err == nil || !strings.Contains(err.Error(), "browser.") {
			t.Fatalf("expected browser config error for %q, got %v", suffix, err)
		}
	}
}

func TestParseConfigRejectsEmptyHostCookieCleanupName(t *testing.T) {
	data := "id: bad\nbase_url: https://example.com\ncookie:\n  magic: BADENC\\x01\nbrowser:\n  inject_cookie: false\n  clear_host_cookies: [sessionid, '']\n"
	if _, err := parseConfig([]byte(data)); err == nil || !strings.Contains(err.Error(), "browser.clear_host_cookies") {
		t.Fatalf("expected host cookie cleanup config error, got %v", err)
	}
}

func TestParseConfigRequiresInjectionDisabledForHostCookieCleanup(t *testing.T) {
	data := "id: bad\nbase_url: https://example.com\ncookie:\n  magic: BADENC\\x01\nbrowser:\n  clear_host_cookies: [sessionid]\n"
	if _, err := parseConfig([]byte(data)); err == nil || !strings.Contains(err.Error(), "inject_cookie: false") {
		t.Fatalf("expected cookie injection conflict error, got %v", err)
	}
}

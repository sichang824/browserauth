package site

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCookieValue(t *testing.T) {
	header := "app_platform_token=abc123; other=val"
	got, ok := cookieValue(header, "app_platform_token")
	if !ok || got != "abc123" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
	if _, ok := cookieValue(header, "missing"); ok {
		t.Fatal("expected missing cookie")
	}
}

func TestAuthHeadersBearerFromCookie(t *testing.T) {
	auth := AuthConfig{
		Token: &TokenConfig{Cookie: "app_platform_token"},
	}
	headers := auth.authHeaders("app_platform_token=secret; foo=bar")
	if headers["Authorization"] != "Bearer secret" {
		t.Fatalf("got %q", headers["Authorization"])
	}
}

func TestAuthHeadersCustomHeaderAndPrefix(t *testing.T) {
	auth := AuthConfig{
		Token: &TokenConfig{
			Cookie: "session",
			Header: "X-Auth-Token",
			Prefix: "",
		},
	}
	headers := auth.authHeaders("session=tok")
	if headers["X-Auth-Token"] != "tok" {
		t.Fatalf("got %q", headers["X-Auth-Token"])
	}
}

func TestAuthHeadersMissingTokenCookie(t *testing.T) {
	auth := AuthConfig{
		Token: &TokenConfig{Cookie: "app_platform_token"},
	}
	if headers := auth.authHeaders("other=val"); len(headers) != 0 {
		t.Fatalf("expected no headers, got %v", headers)
	}
}

func TestValidateHTTPWithBearerFromCookie(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer my-token-123" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "alice"})
	}))
	defer srv.Close()

	cfg := Config{
		ID:      "test",
		BaseURL: srv.URL,
		Auth: AuthConfig{
			Path:              "/me",
			UsernameJSONPaths: []string{"name"},
			Token:             &TokenConfig{Cookie: "app_platform_token"},
		},
	}

	username, err := cfg.ValidateSession(srv.URL, "app_platform_token=my-token-123; other=val")
	if err != nil {
		t.Fatal(err)
	}
	if username != "alice" {
		t.Fatalf("got %q", username)
	}
}

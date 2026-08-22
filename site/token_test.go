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

func TestAuthHeadersMultipleTokens(t *testing.T) {
	auth := AuthConfig{
		Tokens: []TokenConfig{
			{Cookie: "access_token", Header: "Authorization", Prefix: "Bearer "},
			{Cookie: "csrf_token", Header: "x-csrf-token"},
		},
	}
	headers := auth.authHeaders("access_token=jwt; csrf_token=csrf; other=x")
	if headers["Authorization"] != "Bearer jwt" {
		t.Fatalf("Authorization got %q", headers["Authorization"])
	}
	if headers["x-csrf-token"] != "csrf" {
		t.Fatalf("x-csrf-token got %q", headers["x-csrf-token"])
	}
}

func TestAuthHeadersSingularAndListMerge(t *testing.T) {
	auth := AuthConfig{
		Token:  &TokenConfig{Cookie: "access_token"},
		Tokens: []TokenConfig{{Cookie: "csrf_token", Header: "x-csrf-token"}},
	}
	headers := auth.authHeaders("access_token=jwt; csrf_token=csrf")
	if headers["Authorization"] != "Bearer jwt" || headers["x-csrf-token"] != "csrf" {
		t.Fatalf("got %v", headers)
	}
}

func TestAuthHeadersPartialMissingCookies(t *testing.T) {
	auth := AuthConfig{
		Tokens: []TokenConfig{
			{Cookie: "access_token", Header: "Authorization", Prefix: "Bearer "},
			{Cookie: "csrf_token", Header: "x-csrf-token"},
		},
	}
	headers := auth.authHeaders("access_token=jwt")
	if len(headers) != 1 || headers["Authorization"] != "Bearer jwt" {
		t.Fatalf("got %v", headers)
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

func TestJSONPathArrayIndex(t *testing.T) {
	payload := map[string]any{
		"workspaces": []any{
			map[string]any{"id": "w1", "name": "Online Workspace"},
			map[string]any{"id": "w2", "name": "Other"},
		},
	}
	got, ok := jsonPath(payload, "workspaces.0.name")
	if !ok || got != "Online Workspace" {
		t.Fatalf("got %v ok=%v", got, ok)
	}
	if _, ok := jsonPath(payload, "workspaces.5.name"); ok {
		t.Fatal("expected out-of-range to fail")
	}
	if _, ok := jsonPath(payload, "workspaces.x"); ok {
		t.Fatal("expected non-numeric index on array to fail")
	}
}

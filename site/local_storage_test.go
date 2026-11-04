package site

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLocalStorageSession(t *testing.T) {
	const uid = "9007199254740993"
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/verify" ||
			r.URL.Query().Get("account") != uid ||
			r.URL.Query().Get("scope") != "current" ||
			r.Header.Get("Authorization") != "Bearer new-token" ||
			r.Header.Get("Cookie") != "theme=dark" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"name":"signed-in"}`)
	}))
	defer api.Close()
	cfg := Config{
		ID: "example", BaseURL: api.URL,
		Cookie: CookieConfig{Magic: "EXAMPLEENC"},
		Auth: AuthConfig{
			Path:              "/verify?account=old&scope=current",
			Token:             &TokenConfig{Cookie: "session_token"},
			UsernameJSONPaths: []string{"name"},
			LocalStorage: &LocalStorageConfig{
				Key:    "profile",
				Fields: map[string]string{"session_token": "auth.token", "session_account": "account.id"},
				Query:  map[string]string{"account": "session_account"},
			},
		},
	}
	data, err := MarshalYAML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = parseConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.LocalStorage == nil || cfg.Auth.LocalStorage.Key != "profile" {
		t.Fatal("site config lost localStorage settings")
	}
	raw, err := cfg.Auth.LocalStorage.merge("theme=dark; session_token=stale", `{"auth":{"token":"new-token"},"account":{"id":`+uid+`}}`)
	if err != nil {
		t.Fatal(err)
	}
	session, err := cfg.FetchSession(raw)
	if err != nil || session.Username != "signed-in" {
		t.Fatalf("FetchSession = %+v, %v", session, err)
	}
	if _, err := cfg.FetchSession("session_token=stale"); err == nil || !strings.Contains(err.Error(), "session_account") {
		t.Fatalf("expected a missing field error, got %v", err)
	}
	if _, err := cfg.Auth.LocalStorage.merge("", `{"auth":{"token":"new-token"},"account":{"id":"malformed;alias=injected"}}`); err == nil {
		t.Fatal("accepted a field that could inject another session alias")
	}
}

func TestLocalStorageAuthRejectsRedirect(t *testing.T) {
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("localStorage credentials must not be replayed after a redirect")
		w.WriteHeader(http.StatusOK)
	}))
	defer destination.Close()
	entry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusFound)
	}))
	defer entry.Close()

	cfg := Config{
		BaseURL: entry.URL,
		Auth: AuthConfig{
			Path:         "/verify",
			Token:        &TokenConfig{Cookie: "access"},
			LocalStorage: &LocalStorageConfig{Key: "session", Fields: map[string]string{"access": "token"}},
		},
	}
	if _, err := cfg.FetchSession("access=token"); err == nil || !strings.Contains(err.Error(), "HTTP 302") {
		t.Fatalf("expected HTTP 302, got %v", err)
	}
}

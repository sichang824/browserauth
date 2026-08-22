package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultSitesDirUnderDataDir(t *testing.T) {
	root := t.TempDir()
	t.Setenv("BROWSERAUTH_DATA_DIR", root)
	t.Setenv("BROWSERAUTH_SITES_DIR", "")
	got := DefaultSitesDir()
	want := filepath.Join(root, "sites")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestEnsureSitesDir(t *testing.T) {
	root := t.TempDir()
	t.Setenv("BROWSERAUTH_DATA_DIR", root)
	dir, err := EnsureSitesDir()
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(root, "sites") {
		t.Fatalf("dir %q", dir)
	}
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		t.Fatalf("stat: %v", err)
	}
}

func TestAddFromFileAndRemove(t *testing.T) {
	root := t.TempDir()
	t.Setenv("BROWSERAUTH_DATA_DIR", root)

	src := filepath.Join(t.TempDir(), "custom.yaml")
	custom := []byte(`id: demo
base_url: https://demo.example.com
login_url: /login
cookie:
  magic: "DEMOENC\x01"
auth:
  method: GET
  path: /api/me
  username_json_paths:
    - name
`)
	if err := os.WriteFile(src, custom, 0o600); err != nil {
		t.Fatal(err)
	}

	path, err := Add("demo", src, false)
	if err != nil {
		t.Fatal(err)
	}
	if !Exists("demo") {
		t.Fatal("expected demo site")
	}
	if path != SiteFilePath("demo") {
		t.Fatalf("path %q", path)
	}

	cfg, err := Load("demo")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ID != "demo" {
		t.Fatalf("got %q", cfg.ID)
	}

	if err := Remove("demo"); err != nil {
		t.Fatal(err)
	}
	if Exists("demo") {
		t.Fatal("expected removed")
	}
}

func TestAddRequiresFromFile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("BROWSERAUTH_DATA_DIR", root)
	if _, err := Add("jira", "", false); err == nil {
		t.Fatal("expected --from-file required")
	}
}

func TestAddRejectsDuplicate(t *testing.T) {
	root := t.TempDir()
	t.Setenv("BROWSERAUTH_DATA_DIR", root)

	src := filepath.Join(t.TempDir(), "jira.yaml")
	data := []byte(`id: jira
base_url: http://example.com
cookie:
  magic: "JIRAENC\x01"
auth:
  method: GET
  path: /me
  username_json_paths: [name]
`)
	if err := os.WriteFile(src, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Add("jira", src, false); err != nil {
		t.Fatal(err)
	}
	if _, err := Add("jira", src, false); err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestValidSiteID(t *testing.T) {
	valid := []string{"jira", "dify-console", "hub.local", "a_b-c.9"}
	for _, id := range valid {
		if !ValidSiteID(id) {
			t.Fatalf("%q should be valid", id)
		}
	}
	invalid := []string{"", "has space", "a/b", "../evil", "a:b", "中文"}
	for _, id := range invalid {
		if ValidSiteID(id) {
			t.Fatalf("%q should be invalid", id)
		}
	}
}

func TestDeriveEnvPrefix(t *testing.T) {
	cases := map[string]string{
		"jira":         "JIRA",
		"dify-console": "DIFY_CONSOLE",
		"hub.local":    "HUB_LOCAL",
		"Xhs":          "XHS",
	}
	for in, want := range cases {
		if got := DeriveEnvPrefix(in); got != want {
			t.Fatalf("DeriveEnvPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDeriveCookieMagic(t *testing.T) {
	cases := map[string]string{
		"jira":         "JIRAENC\x01",
		"hub-local":    "HUBLOCALENC\x01",
		"dify-console": "DIFYCONSOLEENC\x01",
	}
	for in, want := range cases {
		if got := DeriveCookieMagic(in); got != want {
			t.Fatalf("DeriveCookieMagic(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMarshalYAMLOmitsEmptyGroups(t *testing.T) {
	cfg := Config{
		ID:      "xhs",
		BaseURL: "https://www.xiaohongshu.com",
		Cookie:  CookieConfig{Magic: "XHSENC\x01", Env: "XHS_COOKIE"},
	}
	data, err := MarshalYAML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, absent := range []string{"auth:", "profile:", "login_url", "no_fail_env", "chrome_path_env"} {
		if strings.Contains(text, absent) {
			t.Fatalf("expected %q to be omitted, got:\n%s", absent, text)
		}
	}
	if !strings.Contains(text, "XHSENC") {
		t.Fatalf("magic missing:\n%s", text)
	}

	// Round-trip: parse back and confirm parseConfig defaults kick in.
	back, err := parseConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	if back.LoginURL != "/" || back.Cookie.Env != "XHS_COOKIE" {
		t.Fatalf("round-trip lost fields: %+v", back)
	}
}

func TestMarshalYAMLKeepsAuthAndToken(t *testing.T) {
	cfg := Config{
		ID:       "authz",
		BaseURL:  "https://api.example.com",
		LoginURL: "https://example.com/login",
		Cookie:   CookieConfig{Magic: "AUTHZENC\x01", Env: "AUTHZ_COOKIE"},
		Auth: AuthConfig{
			Method: "POST",
			Path:   "/users/me",
			Token:  &TokenConfig{Cookie: "access_token", Header: "Authorization", Prefix: "Bearer "},
		},
	}
	data, err := MarshalYAML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	back, err := parseConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	if back.Auth.Method != "POST" || back.Auth.Token == nil || back.Auth.Token.Prefix != "Bearer " {
		t.Fatalf("round-trip lost auth/token: %+v", back.Auth)
	}
	if back.LoginURL != "https://example.com/login" {
		t.Fatalf("absolute login_url lost: %q", back.LoginURL)
	}
}

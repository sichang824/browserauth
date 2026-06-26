package site

import (
	"os"
	"path/filepath"
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

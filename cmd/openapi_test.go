package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenAPISiteLatestAndRules(t *testing.T) {
	root := t.TempDir()
	t.Setenv("BROWSERAUTH_DATA_DIR", root)
	t.Setenv("BROWSERAUTH_COOKIE_KEY", "")
	recordings := filepath.Join(root, "recordings")
	os.MkdirAll(recordings, 0o700)
	old := filepath.Join(recordings, "demo-20260101-000000.har")
	latest := filepath.Join(recordings, "demo-20260102-000000.har")
	other := filepath.Join(recordings, "demo-other-20260103-000000.har")
	for _, p := range []string{old, latest, other} {
		if err := os.WriteFile(p, []byte(`{"log":{"entries":[{"request":{"method":"GET","url":"https://api.example/v1/info"},"response":{"status":200,"content":{"mimeType":"application/json","text":"{}"}}}]}}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	os.Chtimes(old, now.Add(-time.Hour), now.Add(-time.Hour))
	os.Chtimes(latest, now, now)
	os.Chtimes(other, now.Add(time.Hour), now.Add(time.Hour))
	got, err := latestRecording("demo")
	if err != nil || got != latest {
		t.Fatalf("latest recording = %q, %v", got, err)
	}
	rules := filepath.Join(root, "openapi-rules")
	os.MkdirAll(rules, 0o700)
	os.WriteFile(filepath.Join(rules, "demo.yaml"), []byte("title: Demo APIs\ninclude_hosts: [api.example]\n"), 0o600)
	if code := OpenAPI("demo", nil); code != 0 {
		t.Fatalf("export exit %d", code)
	}
	output := filepath.Join(root, "openapi/demo/demo-20260102-000000-openapi/openapi.yaml")
	if _, err := os.Stat(output); err != nil {
		t.Fatal(err)
	}
	if code := OpenAPI("", []string{"--input", latest, "--output", filepath.Join(root, "global")}); code != 0 {
		t.Fatal("global export failed")
	}
	if code := OpenAPI("", nil); code != 2 {
		t.Fatal("missing input accepted")
	}
}

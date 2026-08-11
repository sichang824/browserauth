package store

import (
	"os"
	"path/filepath"
	"testing"

	"skills-browserauth/cookie"
)

func TestDefaultDataDir(t *testing.T) {
	t.Setenv(dataDirEnv, "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	got := DefaultDataDir()
	want := filepath.Join(home, ".browserauth")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestCookieFilePathUsesHomeHiddenDir(t *testing.T) {
	t.Setenv(dataDirEnv, t.TempDir())
	names := AppNames{SiteID: "jira", Magic: "JIRAENC\x01"}
	got := CookieFilePath(names)
	want := filepath.Join(DefaultDataDir(), "cookies", "jira")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestGlobalProfileDirDefault(t *testing.T) {
	t.Setenv(dataDirEnv, t.TempDir())
	t.Setenv(globalProfileDirEnv, "")
	got := GlobalProfileDir()
	want := filepath.Join(DefaultDataDir(), "profile")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestProfileDirPathSharedByDefault(t *testing.T) {
	t.Setenv(dataDirEnv, t.TempDir())
	t.Setenv(isolatedProfileEnv, "")
	names := AppNames{SiteID: "tingwu"}
	got := ProfileDirPath(names, false)
	want := GlobalProfileDir()
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestProfileDirPathIsolated(t *testing.T) {
	t.Setenv(dataDirEnv, t.TempDir())
	names := AppNames{SiteID: "jira"}
	got := ProfileDirPath(names, true)
	want := filepath.Join(DefaultDataDir(), "profiles", "jira")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestIsolatedProfileEnabledGlobalEnv(t *testing.T) {
	t.Setenv(isolatedProfileEnv, "1")
	t.Setenv("JIRA_ISOLATED_PROFILE", "")
	if !IsolatedProfileEnabled(AppNames{SiteID: "jira"}) {
		t.Fatal("expected isolated")
	}
}

func TestIsolatedProfileEnabledPerSiteEnv(t *testing.T) {
	t.Setenv(isolatedProfileEnv, "")
	t.Setenv("TINGWU_ISOLATED_PROFILE", "true")
	if !IsolatedProfileEnabled(AppNames{SiteID: "tingwu"}) {
		t.Fatal("expected isolated")
	}
}

func TestResolveIsolatedProfileCLIOverrides(t *testing.T) {
	t.Setenv(isolatedProfileEnv, "")
	if !ResolveIsolatedProfile(AppNames{SiteID: "jira"}, true) {
		t.Fatal("expected cli flag to enable isolation")
	}
}

func TestResolvePassphrasePrefersUnifiedEnv(t *testing.T) {
	t.Setenv(defaultKeyEnv, "unified-key")
	t.Setenv("JIRA_COOKIE_KEY", "legacy-key")
	got, err := ResolvePassphrase(AppNames{CookieKeyEnv: "JIRA_COOKIE_KEY"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "unified-key" {
		t.Fatalf("got %q", got)
	}
}

func TestResolvePassphraseFallsBackToLegacyEnv(t *testing.T) {
	t.Setenv(defaultKeyEnv, "")
	t.Setenv("JIRA_COOKIE_KEY", "legacy-key")
	got, err := ResolvePassphrase(AppNames{CookieKeyEnv: "JIRA_COOKIE_KEY"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "legacy-key" {
		t.Fatalf("got %q", got)
	}
}

func TestResolvePassphraseMissing(t *testing.T) {
	t.Setenv(defaultKeyEnv, "")
	t.Setenv("JIRA_COOKIE_KEY", "")
	if _, err := ResolvePassphrase(AppNames{CookieKeyEnv: "JIRA_COOKIE_KEY"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveCookieKeepsTrailingNewlineInCiphertext(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(dataDirEnv, dir)
	t.Setenv(defaultKeyEnv, "test-passphrase")
	t.Setenv("CHATGPT_COOKIE", "")

	names := AppNames{
		SiteID:        "chatgpt",
		Magic:         "CHATGPTENC\x01",
		CookieEnv:     "CHATGPT_COOKIE",
		CookieFileEnv: "CHATGPT_COOKIE_FILE",
	}

	plain := "session-token=abc; __Secure-next-auth.session-token=xyz"
	var blob []byte
	var err error
	// Re-encrypt until natural ciphertext ends with 0x0a (exposes TrimRight bug).
	for i := 0; i < 10000; i++ {
		blob, err = cookie.Encrypt(plain, names.Magic, "test-passphrase")
		if err != nil {
			t.Fatal(err)
		}
		if blob[len(blob)-1] == '\n' {
			break
		}
	}
	if blob[len(blob)-1] != '\n' {
		t.Fatal("could not produce ciphertext ending with 0x0a")
	}

	path := CookieFilePath(names)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, blob, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ResolveCookie(names)
	if err != nil {
		t.Fatalf("ResolveCookie: %v", err)
	}
	want, err := cookie.Decrypt(blob, names.Magic, "test-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveCookieAllowsExtraTrailingNewlineAfterBlob(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(dataDirEnv, dir)
	t.Setenv(defaultKeyEnv, "test-passphrase")
	t.Setenv("CHATGPT_COOKIE", "")

	names := AppNames{
		SiteID:    "chatgpt",
		Magic:     "CHATGPTENC\x01",
		CookieEnv: "CHATGPT_COOKIE",
	}
	plain := "session=ok"
	blob, err := cookie.Encrypt(plain, names.Magic, "test-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	path := CookieFilePath(names)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(blob, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ResolveCookie(names)
	if err != nil {
		t.Fatalf("ResolveCookie: %v", err)
	}
	if got != plain {
		t.Fatalf("got %q want %q", got, plain)
	}
}

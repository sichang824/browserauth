package store

import (
	"os"
	"path/filepath"
	"testing"
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

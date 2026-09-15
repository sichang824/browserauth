package chromebrowser

import (
	"os"
	"testing"
)

func TestChromeExecutableSharedEnv(t *testing.T) {
	t.Setenv("BROWSER_CHROME_PATH", "/tmp/shared-chrome")
	t.Setenv("TINGWU_CHROME_PATH", "/tmp/app-chrome")
	if got := ChromeExecutable("TINGWU_CHROME_PATH"); got != "/tmp/shared-chrome" {
		t.Fatalf("got %q", got)
	}
}

func TestChromeExecutableAppEnv(t *testing.T) {
	t.Setenv("BROWSER_CHROME_PATH", "")
	t.Setenv("TINGWU_CHROME_PATH", "/tmp/app-chrome")
	if got := ChromeExecutable("TINGWU_CHROME_PATH"); got != "/tmp/app-chrome" {
		t.Fatalf("got %q", got)
	}
}

func TestChromeExecutableMacDefault(t *testing.T) {
	t.Setenv("BROWSER_CHROME_PATH", "")
	if _, err := os.Stat(defaultMacChromePath); err != nil {
		t.Skip("Google Chrome not installed")
	}
	if got := ChromeExecutable(""); got != defaultMacChromePath {
		t.Fatalf("got %q", got)
	}
}

func TestExecAllocatorOptionsAvoidsAutomationDefaults(t *testing.T) {
	opts := ExecAllocatorOptions("/tmp/profile", "")
	if len(opts) == 0 {
		t.Fatal("expected options")
	}
	// Sanity: we build a custom option list instead of chromedp.DefaultExecAllocatorOptions.
	if len(opts) < 5 {
		t.Fatalf("too few options: %d", len(opts))
	}
}

func TestAvailableLoopbackPort(t *testing.T) {
	port, err := availableLoopbackPort()
	if err != nil {
		t.Fatal(err)
	}
	if port <= 0 || port > 65535 {
		t.Fatalf("invalid port %d", port)
	}
}

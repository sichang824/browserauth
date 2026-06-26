package chromebrowser

import (
	"os"
	"runtime"

	"github.com/chromedp/chromedp"
)

const defaultMacChromePath = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"

// ChromeExecutable returns the Chrome binary for automation.
// Uses BROWSER_CHROME_PATH, then chromePathEnv (app-specific), then macOS Google Chrome.
func ChromeExecutable(chromePathEnv string) string {
	if path := os.Getenv("BROWSER_CHROME_PATH"); path != "" {
		return path
	}
	if chromePathEnv != "" {
		if path := os.Getenv(chromePathEnv); path != "" {
			return path
		}
	}
	if runtime.GOOS == "darwin" {
		if _, err := os.Stat(defaultMacChromePath); err == nil {
			return defaultMacChromePath
		}
	}
	return ""
}

// ExecAllocatorOptions builds chromedp options for a persistent, human-like Chrome session.
// Avoids chromedp.DefaultExecAllocatorOptions (enable-automation, headless defaults, etc.)
// that trigger CAPTCHA / bot detection on sites like tingwu.aliyun.com.
func ExecAllocatorOptions(profileDir, chromePathEnv string) []chromedp.ExecAllocatorOption {
	opts := []chromedp.ExecAllocatorOption{
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("headless", false),
		chromedp.UserDataDir(profileDir),
		chromedp.Flag("disable-extensions", false),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("exclude-switches", "enable-automation"),
		chromedp.Flag("disable-infobars", true),
		chromedp.WindowSize(1440, 900),
	}
	if chromePath := ChromeExecutable(chromePathEnv); chromePath != "" {
		opts = append(opts, chromedp.ExecPath(chromePath))
	}
	return opts
}

package chromebrowser

import (
	"net"
	"os"
	"runtime"
	"strconv"

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
		chromedp.Flag("exclude-switches", "enable-automation"),
		chromedp.WindowSize(1440, 900),
	}
	// chromedp defaults to --remote-debugging-port=0. Chromium treats that
	// exact flag as an automation signal and exposes navigator.webdriver,
	// which causes strict WAFs to reject an otherwise normal headed session.
	// A dynamically reserved non-zero loopback port keeps CDP available
	// without enabling the webdriver marker.
	if port, err := availableLoopbackPort(); err == nil {
		opts = append(opts, chromedp.Flag("remote-debugging-port", strconv.Itoa(port)))
	}
	if chromePath := ChromeExecutable(chromePathEnv); chromePath != "" {
		opts = append(opts, chromedp.ExecPath(chromePath))
	}
	return opts
}

func availableLoopbackPort() (int, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

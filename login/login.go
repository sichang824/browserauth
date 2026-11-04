package login

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"skills-browserauth/chromebrowser"
	"skills-browserauth/store"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// SessionValidator checks whether a cookie header represents an authenticated session.
type SessionValidator func(ctx context.Context, baseURL, cookieHeader string) (username string, err error)
type BrowserSessionValidator func(ctx context.Context, cookieHeader string) (username string, err error)

// Config drives browser login and cookie capture for one app.
type Config struct {
	BaseURL         string
	LoginURL        string
	Names           store.AppNames
	ChromePathEnv   string
	IsolatedProfile bool
	Validate        SessionValidator
	ValidateBrowser BrowserSessionValidator
	CaptureFields   func(ctx context.Context, cookies string) (string, error)
}

// Result is returned after a successful login capture.
type Result struct {
	Username    string
	CookiePath  string
	ProfileDir  string
	CookieCount int
}

// Capture opens a browser, waits for a valid session, and saves it encrypted.
func Capture(ctx context.Context, cfg Config, passphrase string) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if passphrase == "" {
		return Result{}, errors.New("BROWSERAUTH_COOKIE_KEY must be set to store the cookie (encrypted)")
	}

	profileDir := store.ProfileDirPath(cfg.Names, cfg.IsolatedProfile)
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		return Result{}, fmt.Errorf("prepare profile dir: %w", err)
	}

	// Keep Chrome alive when the work context is cancelled so it can flush the
	// persistent profile before the allocator terminates its process.
	opts := append(chromebrowser.ExecAllocatorOptions(profileDir, cfg.ChromePathEnv), chromedp.WSURLReadTimeout(10*time.Second))
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.WithoutCancel(ctx), opts...)
	defer allocCancel()
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()
	defer func() {
		if chromedp.FromContext(browserCtx).Browser != nil && browserCtx.Err() == nil {
			closeCtx, cancel := context.WithTimeout(browserCtx, 5*time.Second)
			defer cancel()
			_ = chromedp.Cancel(closeCtx)
		}
	}()

	// The first Run starts the process with the lifetime of browserCtx.
	if err := chromebrowser.InjectStealth(browserCtx); err != nil {
		return Result{}, fmt.Errorf("prepare browser: %w", err)
	}
	workCtx, cancelWork := context.WithCancel(browserCtx)
	defer cancelWork()
	stop := context.AfterFunc(ctx, cancelWork)
	defer stop()

	if err := chromedp.Run(workCtx, network.Enable()); err != nil {
		return Result{}, fmt.Errorf("enable network events: %w", err)
	}

	triggerCh := make(chan struct{}, 1)

	chromedp.ListenTarget(browserCtx, func(ev interface{}) {
		switch ev.(type) {
		case *network.EventResponseReceived, *network.EventLoadingFinished:
			select {
			case triggerCh <- struct{}{}:
			default:
			}
		}
	})

	if err := chromedp.Run(workCtx, chromedp.Navigate(cfg.LoginURL)); err != nil {
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		return Result{}, fmt.Errorf("open browser: %w", err)
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		res, waiting, err := tryPersist(workCtx, cfg, passphrase, profileDir)
		if err != nil {
			return Result{}, err
		}
		if waiting == nil {
			return res, nil
		}
		select {
		case <-ctx.Done():
			return Result{}, fmt.Errorf("%w (%v)", ctx.Err(), waiting)
		case <-browserCtx.Done():
			return Result{}, fmt.Errorf("browser closed while waiting for login (%v)", waiting)
		case <-triggerCh:
		case <-ticker.C:
		}
	}
}

// RefreshFromBrowser reads the session from an open browser and persists it if valid.
func RefreshFromBrowser(ctx context.Context, cfg Config, passphrase string) (Result, bool, error) {
	profileDir := store.ProfileDirPath(cfg.Names, cfg.IsolatedProfile)
	res, waiting, err := tryPersist(ctx, cfg, passphrase, profileDir)
	return res, err == nil && waiting == nil, err
}

// tryPersist separates retryable capture and auth failures from a failed write.
func tryPersist(ctx context.Context, cfg Config, passphrase, profileDir string) (result Result, waiting error, err error) {
	if ctx.Err() != nil {
		return Result{}, ctx.Err(), nil
	}
	cookies, err := getBrowserCookies(ctx, cfg)
	if err != nil {
		return Result{}, errors.New("waiting to read browser cookies"), nil
	}
	cookieLine := formatCookies(cookies)
	if cfg.CaptureFields != nil {
		cookieLine, err = cfg.CaptureFields(ctx, cookieLine)
		if err != nil {
			return Result{}, err, nil
		}
	}
	if cookieLine == "" {
		return Result{}, errors.New("waiting for browser cookies"), nil
	}
	var username string
	if cfg.ValidateBrowser != nil {
		username, err = cfg.ValidateBrowser(ctx, cookieLine)
	} else {
		username, err = cfg.Validate(ctx, cfg.BaseURL, cookieLine)
	}
	if err != nil {
		return Result{}, err, nil
	}
	if cfg.ValidateBrowser != nil && cfg.CaptureFields == nil {
		cookies, err = getBrowserCookies(ctx, cfg)
		if err != nil || len(cookies) == 0 {
			return Result{}, errors.New("waiting for validated browser cookies"), nil
		}
		cookieLine = formatCookies(cookies)
	}
	if ctx.Err() != nil {
		return Result{}, ctx.Err(), nil
	}

	cookiePath, err := store.WriteEncryptedCookie(cfg.Names, cookieLine, passphrase)
	if err != nil {
		return Result{}, nil, err
	}
	return Result{
		Username:    username,
		CookiePath:  cookiePath,
		ProfileDir:  profileDir,
		CookieCount: len(cookies),
	}, nil, nil
}

func getBrowserCookies(ctx context.Context, cfg Config) ([]*network.Cookie, error) {
	var cookies []*network.Cookie
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		urls := []string{cfg.BaseURL}
		// Keep legacy cookie-only sites that authenticate through a different host.
		if cfg.CaptureFields == nil && cfg.LoginURL != "" && !strings.HasPrefix(cfg.LoginURL, cfg.BaseURL) {
			urls = append(urls, cfg.LoginURL)
		}
		var getErr error
		cookies, getErr = network.GetCookies().WithURLs(urls).Do(ctx)
		return getErr
	}))
	return cookies, err
}

func formatCookies(cookies []*network.Cookie) string {
	parts := make([]string, 0, len(cookies))
	for _, c := range cookies {
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}

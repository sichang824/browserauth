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
type SessionValidator func(baseURL, cookieHeader string) (username string, err error)

// Config drives browser login and cookie capture for one app.
type Config struct {
	BaseURL         string
	LoginURL        string
	Names           store.AppNames
	ChromePathEnv   string
	IsolatedProfile bool
	Validate        SessionValidator
}

// Result is returned after a successful login capture.
type Result struct {
	Username   string
	CookiePath string
	ProfileDir string
	CookieCount int
}

// Capture opens a browser, waits for a valid session, and writes encrypted cookie.
func Capture(ctx context.Context, cfg Config, passphrase string) (Result, error) {
	if passphrase == "" {
		return Result{}, errors.New("BROWSERAUTH_COOKIE_KEY must be set to store the cookie (encrypted)")
	}

	profileDir := store.ProfileDirPath(cfg.Names, cfg.IsolatedProfile)
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		return Result{}, fmt.Errorf("prepare profile dir: %w", err)
	}

	opts := chromebrowser.ExecAllocatorOptions(profileDir, cfg.ChromePathEnv)
	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, opts...)
	defer allocCancel()

	browserCtx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	if err := chromebrowser.InjectStealth(browserCtx); err != nil {
		return Result{}, fmt.Errorf("prepare browser stealth: %w", err)
	}

	if err := chromedp.Run(browserCtx, network.Enable()); err != nil {
		return Result{}, fmt.Errorf("enable network events: %w", err)
	}

	triggerCh := make(chan struct{}, 1)
	resultCh := make(chan Result, 1)
	errCh := make(chan error, 1)

	chromedp.ListenTarget(browserCtx, func(ev interface{}) {
		switch ev.(type) {
		case *network.EventResponseReceived, *network.EventLoadingFinished:
			select {
			case triggerCh <- struct{}{}:
			default:
			}
		}
	})

	go watchCapture(browserCtx, cfg, passphrase, profileDir, triggerCh, resultCh, errCh)

	if err := chromedp.Run(browserCtx, chromedp.Navigate(cfg.LoginURL)); err != nil {
		return Result{}, fmt.Errorf("open browser: %w", err)
	}

	select {
	case res := <-resultCh:
		allocCancel()
		return res, nil
	case err := <-errCh:
		if errors.Is(err, context.Canceled) {
			return Result{}, err
		}
		return Result{}, err
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
}

// RefreshFromBrowser reads cookies from an open browser context and persists if valid.
func RefreshFromBrowser(ctx context.Context, cfg Config, passphrase string) (Result, bool, error) {
	profileDir := store.ProfileDirPath(cfg.Names, cfg.IsolatedProfile)
	return tryPersist(ctx, cfg, passphrase, profileDir)
}

func watchCapture(ctx context.Context, cfg Config, passphrase, profileDir string, triggerCh <-chan struct{}, resultCh chan<- Result, errCh chan<- error) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-triggerCh:
		}

		res, ok, err := tryPersist(ctx, cfg, passphrase, profileDir)
		if err != nil {
			select {
			case errCh <- err:
			default:
			}
			return
		}
		if !ok {
			continue
		}
		select {
		case resultCh <- res:
		default:
		}
		return
	}
}

func tryPersist(ctx context.Context, cfg Config, passphrase, profileDir string) (Result, bool, error) {
	var cookies []*network.Cookie
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		urls := []string{cfg.BaseURL}
		// Web login host may differ from API base (e.g. authz.zsclab.com vs api.authz.zsclab.com).
		if cfg.LoginURL != "" && !strings.HasPrefix(cfg.LoginURL, cfg.BaseURL) {
			urls = append(urls, cfg.LoginURL)
		}
		var getErr error
		cookies, getErr = network.GetCookies().WithURLs(urls).Do(ctx)
		return getErr
	}))
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return Result{}, false, err
		}
		return Result{}, false, nil
	}
	if len(cookies) == 0 {
		return Result{}, false, nil
	}

	cookieLine := formatCookies(cookies)
	username, err := cfg.Validate(cfg.BaseURL, cookieLine)
	if err != nil {
		return Result{}, false, nil
	}

	cookiePath, err := store.WriteEncryptedCookie(cfg.Names, cookieLine, passphrase)
	if err != nil {
		return Result{}, false, err
	}

	return Result{
		Username:    username,
		CookiePath:  cookiePath,
		ProfileDir:  profileDir,
		CookieCount: len(cookies),
	}, true, nil
}

func formatCookies(cookies []*network.Cookie) string {
	parts := make([]string, 0, len(cookies))
	for _, c := range cookies {
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}

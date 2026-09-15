package site

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"skills-browserauth/chromebrowser"
	"skills-browserauth/store"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

const (
	browserAuthTimeout    = 30 * time.Second
	browserRequestTimeout = 60 * time.Second
	browserEntryTimeout   = 5 * time.Minute
)

type browserFetchResult struct {
	Status int    `json:"status"`
	Body   string `json:"body"`
}

// BrowserSession owns one Chrome process and serializes all operations on it.
// It is safe to reuse across multiple requests and goroutines.
type BrowserSession struct {
	cfg           Config
	cookieHeader  string
	ctx           context.Context
	cleanup       func()
	mu            sync.Mutex
	authenticated bool
	session       Session
	closed        bool
	createdAt     time.Time
	lastUsed      time.Time
	requestCount  int64
}

// OpenBrowserSession starts one browser that remains alive until Close.
func (c Config) OpenBrowserSession(parent context.Context, cookieHeader string, isolated bool) (*BrowserSession, error) {
	browserCtx, cleanup, err := c.prepareBrowser(parent, cookieHeader, isolated)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	return &BrowserSession{cfg: c, cookieHeader: cookieHeader, ctx: browserCtx, cleanup: cleanup, createdAt: now, lastUsed: now}, nil
}

func (s *BrowserSession) authenticateLocked() (Session, error) {
	if s.closed {
		return Session{}, fmt.Errorf("browser session is closed")
	}
	if s.authenticated {
		return s.session, nil
	}
	ctx, cancel := context.WithTimeout(s.ctx, browserAuthTimeout)
	defer cancel()
	session, err := s.cfg.waitForBrowserSession(ctx, s.cookieHeader)
	if err != nil {
		return Session{}, err
	}
	s.authenticated = true
	s.session = session
	s.lastUsed = time.Now()
	return session, nil
}

func (s *BrowserSession) Authenticate() (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authenticateLocked()
}

// Request authenticates once, then sends the business request exactly once.
func (s *BrowserSession) Request(method, path, body string, extraHeaders map[string]string) ([]byte, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateBrowserPath(path); err != nil {
		return nil, 0, err
	}
	if _, err := s.authenticateLocked(); err != nil {
		return nil, 0, err
	}
	headers := map[string]string{"Accept": "application/json", "X-Requested-With": "XMLHttpRequest"}
	for key, value := range s.cfg.Auth.authHeaders(s.cookieHeader) {
		headers[key] = value
	}
	for key, value := range extraHeaders {
		headers[key] = value
	}
	ctx, cancel := context.WithTimeout(s.ctx, browserRequestTimeout)
	defer cancel()
	result, err := browserFetch(ctx, strings.ToUpper(method), s.cfg.ResolvedBaseURL()+path, headers, body, true)
	s.lastUsed = time.Now()
	s.requestCount++
	if err != nil {
		return nil, 0, fmt.Errorf("browser request failed: %w", err)
	}
	data := []byte(result.Body)
	if result.Status >= 400 && (s.cfg.NoFailEnv == "" || os.Getenv(s.cfg.NoFailEnv) != "1") {
		return data, result.Status, fmt.Errorf("API failed: %s %s (HTTP %d)", method, path, result.Status)
	}
	return data, result.Status, nil
}

func validateBrowserPath(path string) error {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.Contains(path, "://") {
		return fmt.Errorf("request path must be a same-origin absolute path (got %q)", path)
	}
	return nil
}

func (s *BrowserSession) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		s.cleanup()
	}
}

func (s *BrowserSession) Stats() (createdAt, lastUsed time.Time, requestCount int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createdAt, s.lastUsed, s.requestCount
}

// Alive reports whether the owned browser context is still connected.
func (s *BrowserSession) Alive() bool {
	select {
	case <-s.ctx.Done():
		return false
	default:
		return true
	}
}

// FetchConfiguredSession validates a cookie using the transport selected by
// auth.transport. The default remains the lightweight HTTP client.
func (c Config) FetchConfiguredSession(ctx context.Context, cookieHeader string) (Session, error) {
	switch c.Auth.Transport {
	case "", "http":
		return c.FetchSession(cookieHeader)
	case "browser":
		isolated := store.ResolveIsolatedProfile(c.StoreNames(), false)
		return c.fetchSessionBrowser(ctx, cookieHeader, isolated)
	default:
		return Session{}, fmt.Errorf("unsupported auth transport %q", c.Auth.Transport)
	}
}

func (c Config) fetchSessionBrowser(parent context.Context, cookieHeader string, isolated bool) (Session, error) {
	if c.Auth.Path == "" {
		return Session{}, fmt.Errorf("site %q has no auth.path configured", c.ID)
	}
	session, err := c.OpenBrowserSession(parent, cookieHeader, isolated)
	if err != nil {
		return Session{}, err
	}
	defer session.Close()
	return session.Authenticate()
}

// BrowserRequest performs one authenticated request in a real browser after
// the configured read-only auth check succeeds. The business request itself is
// never retried, which keeps POST/PUT/DELETE operations safe from duplication.
func (c Config) BrowserRequest(parent context.Context, cookieHeader, method, path, body string, extraHeaders map[string]string) ([]byte, int, error) {
	isolated := store.ResolveIsolatedProfile(c.StoreNames(), false)
	session, err := c.OpenBrowserSession(parent, cookieHeader, isolated)
	if err != nil {
		return nil, 0, err
	}
	defer session.Close()
	return session.Request(method, path, body, extraHeaders)
}

func (c Config) prepareBrowser(parent context.Context, cookieHeader string, isolated bool) (context.Context, func(), error) {

	profileDir := store.ProfileDirPath(c.StoreNames(), isolated)
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("prepare profile dir: %w", err)
	}

	opts := chromebrowser.ExecAllocatorOptions(profileDir, c.ChromePathEnv)
	allocCtx, allocCancel := chromedp.NewExecAllocator(parent, opts...)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	var activeTargetCancel context.CancelFunc
	cleanup := func() {
		if activeTargetCancel != nil {
			activeTargetCancel()
		}
		browserCancel()
		allocCancel()
	}
	// The first chromedp.Run owns the browser executor lifecycle, so it must use
	// the long-lived session context. Cancelling a temporary child here would
	// tear down Chrome immediately after setup.
	if err := chromebrowser.InjectStealth(browserCtx); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("prepare browser stealth: %w", err)
	}
	if err := injectRequestPanel(browserCtx); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("prepare browser request panel: %w", err)
	}
	if err := chromedp.Run(browserCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		if err := network.Enable().Do(ctx); err != nil {
			return err
		}
		for _, pair := range parseCookieHeader(cookieHeader) {
			if err := network.SetCookie(pair.name, pair.value).WithURL(c.ResolvedBaseURL()).Do(ctx); err != nil {
				return fmt.Errorf("set cookie %s: %w", pair.name, err)
			}
		}
		return nil
	})); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("prepare browser auth: %w", err)
	}

	if err := chromedp.Run(browserCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		_, _, _, _, err := page.Navigate(c.ResolvedLoginURL()).Do(ctx)
		return err
	}), chromedp.Sleep(1500*time.Millisecond)); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("open browser auth page: %w", err)
	}
	if entryText := strings.TrimSpace(c.Browser.EntryText); entryText != "" {
		newTargetID, err := waitAndClickBrowserEntry(browserCtx, entryText)
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("open browser business entry: %w", err)
		}
		if newTargetID != "" {
			activeCtx, cancel := chromedp.NewContext(browserCtx, chromedp.WithTargetID(newTargetID))
			activeTargetCancel = cancel
			if err := chromebrowser.InjectStealth(activeCtx); err != nil {
				cleanup()
				return nil, nil, fmt.Errorf("prepare business tab stealth: %w", err)
			}
			if err := injectRequestPanel(activeCtx); err != nil {
				cleanup()
				return nil, nil, fmt.Errorf("prepare business tab request panel: %w", err)
			}
			browserCtx = activeCtx
		}
	}
	return browserCtx, cleanup, nil
}

type browserPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

func waitAndClickBrowserEntry(browserCtx context.Context, entryText string) (target.ID, error) {
	ctx, cancel := context.WithTimeout(browserCtx, browserEntryTimeout)
	defer cancel()
	entryJSON, _ := json.Marshal(entryText)
	expression := fmt.Sprintf(`(()=>{const el=[...document.querySelectorAll("a")].find(e=>e.innerText.trim()===%s&&e.getClientRects().length);if(!el)return null;el.target="_self";const r=el.getBoundingClientRect();return{x:r.left+r.width/2,y:r.top+r.height/2}})()`, entryJSON)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		var point *browserPoint
		if err := chromedp.Run(ctx, chromedp.Evaluate(expression, &point)); err == nil && point != nil {
			currentTargetID := chromedp.FromContext(browserCtx).Target.TargetID
			newTarget := chromedp.WaitNewTarget(browserCtx, func(info *target.Info) bool {
				return info.Type == "page" && info.OpenerID == currentTargetID
			})
			if err := chromedp.Run(ctx, chromedp.MouseClickXY(point.X, point.Y), chromedp.Sleep(2*time.Second)); err != nil {
				return "", err
			}
			select {
			case targetID := <-newTarget:
				return targetID, nil
			default:
				return "", nil
			}
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("timed out waiting for browser entry %q", entryText)
		case <-ticker.C:
		}
	}
}

func (c Config) waitForBrowserSession(browserCtx context.Context, cookieHeader string) (Session, error) {
	method := strings.ToUpper(strings.TrimSpace(c.Auth.Method))
	if method == "" {
		method = "GET"
	}
	okStatus := c.Auth.OKStatus
	if okStatus == 0 {
		okStatus = 200
	}
	headers := map[string]string{
		"Accept":           "application/json",
		"X-Requested-With": "XMLHttpRequest",
	}
	for key, value := range c.Auth.authHeaders(cookieHeader) {
		headers[key] = value
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	var lastStatus int
	var lastErr error
	for {
		result, err := browserFetch(browserCtx, method, c.ResolvedBaseURL()+c.Auth.Path, headers, "")
		if err == nil {
			lastStatus = result.Status
			if result.Status == okStatus {
				var payload any
				if decodeErr := json.Unmarshal([]byte(result.Body), &payload); decodeErr == nil {
					username, root, validateErr := c.validatePayload(payload)
					if validateErr != nil {
						return Session{}, validateErr
					}
					session := Session{Username: username}
					if c.Auth.UserIDJSONPath != "" {
						if value, ok := jsonPath(root, c.Auth.UserIDJSONPath); ok {
							session.UserID = toInt64(value)
						}
					}
					return session, nil
				} else {
					lastErr = fmt.Errorf("decode auth response: %w", decodeErr)
				}
			} else if result.Status != 0 {
				lastErr = fmt.Errorf("auth check failed with HTTP %d", result.Status)
			}
		} else {
			lastErr = err
		}

		select {
		case <-browserCtx.Done():
			if lastErr != nil {
				return Session{}, fmt.Errorf("browser auth timed out (last status %d): %w", lastStatus, lastErr)
			}
			return Session{}, fmt.Errorf("browser auth timed out (last status %d)", lastStatus)
		case <-ticker.C:
		}
	}
}

func (c Config) validateBrowserSessionOnce(ctx context.Context, cookieHeader string) (Session, error) {
	method := strings.ToUpper(strings.TrimSpace(c.Auth.Method))
	if method == "" {
		method = "GET"
	}
	okStatus := c.Auth.OKStatus
	if okStatus == 0 {
		okStatus = 200
	}
	headers := map[string]string{"Accept": "application/json", "X-Requested-With": "XMLHttpRequest"}
	for key, value := range c.Auth.authHeaders(cookieHeader) {
		headers[key] = value
	}
	result, err := browserFetch(ctx, method, c.ResolvedBaseURL()+c.Auth.Path, headers, "")
	if err != nil {
		return Session{}, err
	}
	if result.Status != okStatus {
		return Session{}, fmt.Errorf("auth check failed with HTTP %d", result.Status)
	}
	var payload any
	if err := json.Unmarshal([]byte(result.Body), &payload); err != nil {
		return Session{}, fmt.Errorf("decode auth response: %w", err)
	}
	username, root, err := c.validatePayload(payload)
	if err != nil {
		return Session{}, err
	}
	session := Session{Username: username}
	if c.Auth.UserIDJSONPath != "" {
		if value, ok := jsonPath(root, c.Auth.UserIDJSONPath); ok {
			session.UserID = toInt64(value)
		}
	}
	return session, nil
}

func browserFetch(ctx context.Context, method, url string, headers map[string]string, body string, showInPanel ...bool) (browserFetchResult, error) {
	urlJSON, _ := json.Marshal(url)
	methodJSON, _ := json.Marshal(method)
	headersJSON, _ := json.Marshal(headers)
	bodyJSON := "null"
	if body != "" {
		encoded, _ := json.Marshal(body)
		bodyJSON = string(encoded)
	}
	panelStart := "null"
	if len(showInPanel) > 0 && showInPanel[0] {
		panelStart = fmt.Sprintf(`(() => { try { return window.__browserauthPanel && window.__browserauthPanel.start({method:%s,url:%s,body:%s}); } catch (_) { return null; } })()`, methodJSON, urlJSON, bodyJSON)
	}
	expression := fmt.Sprintf(`(async () => {
  const panelRequestId = %s;
  try {
    const response = await fetch(%s, {
      method: %s,
      credentials: "include",
      headers: %s,
      body: %s
    });
    const responseBody = await response.text();
    try { if (panelRequestId && window.__browserauthPanel) window.__browserauthPanel.finish(panelRequestId, {status: response.status, body: responseBody}); } catch (_) {}
    return {status: response.status, body: responseBody};
  } catch (error) {
    const message = String(error);
    try { if (panelRequestId && window.__browserauthPanel) window.__browserauthPanel.finish(panelRequestId, {status: 0, body: "", error: message}); } catch (_) {}
    return {status: 0, body: message};
  }
})()`, panelStart, urlJSON, methodJSON, headersJSON, bodyJSON)
	var result browserFetchResult
	err := chromedp.Run(ctx, chromedp.Evaluate(expression, &result, func(params *runtime.EvaluateParams) *runtime.EvaluateParams {
		return params.WithAwaitPromise(true)
	}))
	return result, err
}

type cookiePair struct {
	name  string
	value string
}

func parseCookieHeader(header string) []cookiePair {
	pairs := make([]cookiePair, 0)
	for _, item := range strings.Split(header, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(item), "=")
		if !ok || strings.TrimSpace(name) == "" {
			continue
		}
		pairs = append(pairs, cookiePair{name: strings.TrimSpace(name), value: value})
	}
	return pairs
}

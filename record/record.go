// Package record captures a browser session's network traffic to a HAR file.
// It launches the site's persistent-profile Chrome, records requests/responses
// of the managed tab, and stops when the user clicks the injected stop button,
// presses Ctrl+C, or closes the browser.
package record

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"time"

	"skills-browserauth/chromebrowser"
	"skills-browserauth/store"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

const (
	stopBinding     = "__browserauth_stop"
	maxBodyBytes    = 1 << 20 // per-response decoded body cap
	maxPostDataSize = 4 << 20 // Chrome drops request bodies above this unless raised
	bodyWorkers     = 4
	drainTimeout    = 2 * time.Second
)

// Config drives a recording session for one site.
type Config struct {
	SiteID          string
	StartURL        string
	Names           store.AppNames
	ChromePathEnv   string
	IsolatedProfile bool
}

// StopReason describes how the recording ended.
type StopReason string

const (
	StopButton        StopReason = "button"
	StopSignal        StopReason = "signal"
	StopBrowserClosed StopReason = "browser-closed"
)

// Result is returned after the HAR is written.
type Result struct {
	HARPath    string
	Entries    int
	Duration   time.Duration
	StopReason StopReason
}

// Run opens the browser, records until a stop trigger fires, and writes the
// HAR. The HAR is written before Chrome is killed and is kept for every stop
// reason, as long as at least one request was captured.
func Run(ctx context.Context, cfg Config) (Result, error) {
	profileDir := store.ProfileDirPath(cfg.Names, cfg.IsolatedProfile)
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		return Result{}, fmt.Errorf("prepare profile dir: %w", err)
	}

	opts := chromebrowser.ExecAllocatorOptions(profileDir, cfg.ChromePathEnv)
	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, opts...)
	defer allocCancel()

	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	defer browserCancel()

	if err := chromebrowser.InjectStealth(browserCtx); err != nil {
		return Result{}, fmt.Errorf("prepare browser stealth: %w", err)
	}

	r := newRunner(newCapture(), cfg, allocCancel)

	// Register capture hooks before navigating so the very first document is covered.
	if err := chromedp.Run(browserCtx, chromedp.ActionFunc(func(c context.Context) error {
		if err := network.Enable().WithMaxPostDataSize(maxPostDataSize).Do(c); err != nil {
			return fmt.Errorf("enable network events: %w", err)
		}
		if _, err := page.AddScriptToEvaluateOnNewDocument(buttonJS).Do(c); err != nil {
			return fmt.Errorf("inject stop button script: %w", err)
		}
		if err := runtime.AddBinding(stopBinding).Do(c); err != nil {
			return fmt.Errorf("register stop binding: %w", err)
		}
		return nil
	})); err != nil {
		return Result{}, err
	}

	chromedp.ListenTarget(browserCtx, r.handleEvent(browserCtx))

	for i := 0; i < bodyWorkers; i++ {
		r.wg.Add(1)
		go r.bodyWorker(browserCtx)
	}

	r.start = time.Now()
	if err := chromedp.Run(browserCtx, chromedp.Navigate(cfg.StartURL)); err != nil {
		r.closeJobs()
		r.wg.Wait()
		return Result{}, fmt.Errorf("open browser: %w", err)
	}

	// Stop triggers: button (via listener), Ctrl+C, window close. Signals stay on
	// their own channel so finalize controls when the browser context dies.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, stopSignals()...)
	defer signal.Stop(sigCh)

	go func() {
		select {
		case <-sigCh:
			r.finalize(StopSignal)
		case <-browserCtx.Done():
			r.finalize(StopBrowserClosed)
		}
	}()

	<-r.done
	return r.res, r.resErr
}

// runner owns the capture state, body workers, and the one-shot finalize.
type runner struct {
	cap         *capture
	cfg         Config
	allocCancel context.CancelFunc
	start       time.Time

	jobsMu     sync.Mutex
	jobs       chan *entryData
	jobsClosed bool

	wg sync.WaitGroup // body workers

	once   sync.Once
	res    Result
	resErr error
	done   chan struct{}
}

func newRunner(cap *capture, cfg Config, allocCancel context.CancelFunc) *runner {
	return &runner{
		cap:         cap,
		cfg:         cfg,
		allocCancel: allocCancel,
		jobs:        make(chan *entryData, 1024),
		done:        make(chan struct{}),
	}
}

// handleEvent builds the chromedp event listener. It must never block: body
// fetches run on workers, stop triggers hop to their own goroutine.
func (r *runner) handleEvent(browserCtx context.Context) func(ev interface{}) {
	return func(ev interface{}) {
		switch e := ev.(type) {
		case *network.EventRequestWillBeSent:
			r.cap.requestWillBeSent(e)
		case *network.EventResponseReceived:
			r.cap.responseReceived(e)
		case *network.EventRequestWillBeSentExtraInfo:
			r.cap.requestExtraInfo(e)
		case *network.EventResponseReceivedExtraInfo:
			r.cap.responseExtraInfo(e)
		case *network.EventLoadingFailed:
			r.cap.loadingFailed(e)
		case *network.EventLoadingFinished:
			if entry := r.cap.loadingFinished(e); entry != nil {
				r.enqueue(entry)
			}
		case *runtime.EventBindingCalled:
			if e.Name == stopBinding {
				go r.finalize(StopButton)
			}
		}
	}
}

func (r *runner) enqueue(entry *entryData) {
	r.jobsMu.Lock()
	defer r.jobsMu.Unlock()
	if r.jobsClosed {
		return
	}
	select {
	case r.jobs <- entry:
	default: // queue overflow: keep metadata, skip this body
	}
}

func (r *runner) closeJobs() {
	r.jobsMu.Lock()
	defer r.jobsMu.Unlock()
	if !r.jobsClosed {
		r.jobsClosed = true
		close(r.jobs)
	}
}

// bodyWorker fetches response bodies for finished requests.
func (r *runner) bodyWorker(browserCtx context.Context) {
	defer r.wg.Done()
	for entry := range r.jobs {
		if browserCtx.Err() != nil {
			r.cap.storeBodyErr(entry, browserCtx.Err())
			continue
		}
		if !r.cap.bodyEligible(entry) {
			continue
		}
		var body []byte
		err := chromedp.Run(browserCtx, chromedp.ActionFunc(func(ctx context.Context) error {
			var fetchErr error
			body, fetchErr = network.GetResponseBody(entry.requestID).Do(ctx)
			return fetchErr
		}))
		if err != nil {
			r.cap.storeBodyErr(entry, err)
			continue
		}
		r.cap.storeBody(entry, body)
	}
}

// finalize drains workers, writes the HAR, then kills Chrome. Runs exactly once.
func (r *runner) finalize(reason StopReason) {
	r.once.Do(func() {
		r.res.StopReason = reason
		r.res.Duration = time.Since(r.start)

		r.closeJobs()
		drained := make(chan struct{})
		go func() {
			r.wg.Wait()
			close(drained)
		}()
		select {
		case <-drained:
		case <-time.After(drainTimeout):
		}

		har := buildHAR(r.cap)
		r.res.Entries = len(har.Log.Entries)
		switch {
		case r.res.Entries == 0:
			r.resErr = errors.New("录制期间没有捕获到任何请求")
		default:
			path, err := writeHAR(r.cfg.SiteID, har)
			if err != nil {
				r.resErr = fmt.Errorf("write HAR: %w", err)
			} else {
				r.res.HARPath = path
			}
		}

		r.allocCancel()
		close(r.done)
	})
}

// writeHAR marshals and stores the HAR under the recordings dir (0600).
func writeHAR(siteID string, har HarLog) (string, error) {
	dir := store.RecordingsDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("prepare recordings dir: %w", err)
	}
	data, err := json.MarshalIndent(har, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal HAR: %w", err)
	}
	stamp := time.Now().Format("20060102-150405")
	path := filepath.Join(dir, fmt.Sprintf("%s-%s.har", siteID, stamp))
	for i := 1; fileExists(path); i++ {
		path = filepath.Join(dir, fmt.Sprintf("%s-%s-%d.har", siteID, stamp, i))
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// buttonJS injects the floating stop button (top frame only) and keeps
// navigation inside the recorded tab. Registered via
// page.AddScriptToEvaluateOnNewDocument so it survives all same-tab navigations.
const buttonJS = `
(function () {
  if (window.top !== window) return;
  if (window.__browserauthRecordHook) return;
  window.__browserauthRecordHook = true;

  // Keep the user inside the recorded tab (new tabs escape capture).
  document.addEventListener('click', function (ev) {
    try {
      var el = ev.target && ev.target.closest ? ev.target.closest('a[target="_blank"], a[target="_new"]') : null;
      if (el) el.removeAttribute('target');
    } catch (e) {}
  }, true);
  window.open = function (url) {
    if (url) { try { window.location.assign(url); } catch (e) {} }
    return window;
  };

  function install() {
    if (document.getElementById('browserauth-rec-btn')) return;
    var btn = document.createElement('button');
    btn.id = 'browserauth-rec-btn';
    btn.type = 'button';
    btn.textContent = '⏹ 结束录制';
    btn.style.cssText = 'position:fixed;top:12px;right:12px;z-index:2147483647;' +
      'padding:8px 14px;border:none;border-radius:8px;background:#e53935;color:#fff;' +
      'font:14px/1.2 -apple-system,system-ui,sans-serif;cursor:pointer;' +
      'box-shadow:0 2px 8px rgba(0,0,0,0.35);';
    btn.addEventListener('click', function () {
      btn.disabled = true;
      btn.textContent = '保存中…';
      if (typeof window.__browserauth_stop === 'function') {
        window.__browserauth_stop('user');
      }
    });
    (document.body || document.documentElement).appendChild(btn);
  }
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', install);
  } else {
    install();
  }
})();
`

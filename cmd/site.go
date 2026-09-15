package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"skills-browserauth/chromebrowser"
	"skills-browserauth/httpclient"
	"skills-browserauth/login"
	"skills-browserauth/record"
	"skills-browserauth/sessionmanager"
	"skills-browserauth/site"
	"skills-browserauth/store"

	"github.com/chromedp/chromedp"
)

// RunSite handles "browserauth <site> <command> ...".
func RunSite(args []string) int {
	if len(args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}

	siteID := args[0]
	sub := args[1]
	rest := args[2:]

	cfg, err := site.Load(siteID)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	switch sub {
	case "login":
		rest, isolated := parseIsolatedProfileFlag(rest)
		return siteLogin(cfg, isolated, rest)
	case "browser":
		rest, isolated := parseIsolatedProfileFlag(rest)
		return siteBrowser(cfg, isolated, rest)
	case "record":
		rest, isolated := parseIsolatedProfileFlag(rest)
		return siteRecord(cfg, isolated, rest)
	case "auth":
		if len(rest) > 0 && rest[0] == "set" {
			return siteAuthSet(cfg, rest[1:])
		}
		return siteAuth(cfg, rest)
	case "request":
		return siteRequest(cfg, rest)
	case "session":
		return siteSession(cfg, rest)
	case "cookie":
		return siteCookie(cfg, rest)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command for site %q: %s\n\n", siteID, sub)
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
}

func siteLogin(cfg site.Config, isolated bool, _ []string) int {
	passphrase, err := cfg.Passphrase()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	loginCfg := cfg.LoginConfig(isolated)
	chromePath := chromebrowser.ChromeExecutable(loginCfg.ChromePathEnv)
	if chromePath == "" {
		chromePath = "(chromedp default Chromium)"
	}
	fmt.Fprintf(os.Stderr, "浏览器已打开：%s\n", loginCfg.LoginURL)
	fmt.Fprintf(os.Stderr, "Chrome: %s\n", chromePath)
	fmt.Fprintf(os.Stderr, "Profile: %s\n", store.ProfileDirPath(cfg.StoreNames(), loginCfg.IsolatedProfile))
	fmt.Fprintf(os.Stderr, "请在打开的页面完成登录；检测到有效会话后会自动保存 Cookie 并关闭浏览器。\n")

	res, err := login.Capture(context.Background(), loginCfg, passphrase)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "登录流程已取消。")
			return 1
		}
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	fmt.Printf("已自动捕获 %d 个 Cookie，当前用户 %s\n", res.CookieCount, res.Username)
	fmt.Printf("Cookie 已加密保存至 %s\n", res.CookiePath)
	fmt.Printf("浏览器 profile 已持久化保存至 %s\n", res.ProfileDir)
	return 0
}

func siteBrowser(cfg site.Config, isolated bool, _ []string) int {
	loginCfg := cfg.LoginConfig(isolated)
	profileDir := store.ProfileDirPath(cfg.StoreNames(), loginCfg.IsolatedProfile)
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "Prepare profile dir: %v\n", err)
		return 2
	}

	opts := chromebrowser.ExecAllocatorOptions(profileDir, loginCfg.ChromePathEnv)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer allocCancel()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	if err := chromebrowser.InjectStealth(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Prepare browser stealth: %v\n", err)
		return 2
	}

	if err := chromedp.Run(ctx, chromedp.Navigate(loginCfg.LoginURL)); err != nil {
		fmt.Fprintf(os.Stderr, "Open browser: %v\n", err)
		return 2
	}

	chromePath := chromebrowser.ChromeExecutable(loginCfg.ChromePathEnv)
	if chromePath == "" {
		chromePath = "(chromedp default Chromium)"
	}
	fmt.Fprintf(os.Stderr, "已打开持久化浏览器：%s\n", loginCfg.LoginURL)
	fmt.Fprintf(os.Stderr, "Chrome: %s\n", chromePath)
	fmt.Fprintf(os.Stderr, "Profile: %s\n", profileDir)
	fmt.Fprintf(os.Stderr, "按 Ctrl+C 退出。\n")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	if key, err := cfg.Passphrase(); err == nil {
		if res, ok, err := login.RefreshFromBrowser(ctx, loginCfg, key); err == nil && ok {
			fmt.Printf("已刷新 Cookie，用户 %s\n", res.Username)
			fmt.Printf("Cookie 已加密保存至 %s\n", res.CookiePath)
		}
	}
	return 0
}

func siteRecord(cfg site.Config, isolated bool, rest []string) int {
	startURL := cfg.ResolvedLoginURL()
	if len(rest) > 1 {
		fmt.Fprintf(os.Stderr, "Usage: browserauth %s record [--isolated-profile] [url]\n", cfg.ID)
		return 2
	}
	if len(rest) == 1 {
		startURL = strings.TrimSpace(rest[0])
		if !strings.HasPrefix(startURL, "http://") && !strings.HasPrefix(startURL, "https://") {
			fmt.Fprintf(os.Stderr, "record url must start with http:// or https:// (got %q)\n", startURL)
			return 2
		}
	}

	loginCfg := cfg.LoginConfig(isolated)
	chromePath := chromebrowser.ChromeExecutable(loginCfg.ChromePathEnv)
	if chromePath == "" {
		chromePath = "(chromedp default Chromium)"
	}
	fmt.Fprintf(os.Stderr, "开始录制：%s\n", startURL)
	fmt.Fprintf(os.Stderr, "Chrome: %s\n", chromePath)
	fmt.Fprintf(os.Stderr, "Profile: %s\n", store.ProfileDirPath(cfg.StoreNames(), loginCfg.IsolatedProfile))
	fmt.Fprintf(os.Stderr, "请点击页面右上角「⏹ 结束录制」停止；Ctrl+C 或关闭浏览器窗口也会保存录制。\n")

	res, err := record.Run(context.Background(), record.Config{
		SiteID:          cfg.ID,
		StartURL:        startURL,
		Names:           cfg.StoreNames(),
		ChromePathEnv:   loginCfg.ChromePathEnv,
		IsolatedProfile: loginCfg.IsolatedProfile,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		if res.HARPath == "" {
			return 1
		}
	}

	reason := map[record.StopReason]string{
		record.StopButton:        "按钮",
		record.StopSignal:        "Ctrl+C",
		record.StopBrowserClosed: "浏览器关闭",
	}[res.StopReason]
	fmt.Fprintf(os.Stderr, "录制结束（%s），共 %d 条请求，用时 %s\n", reason, res.Entries, res.Duration.Round(time.Second))
	fmt.Printf("%s\n", res.HARPath)
	return 0
}

func siteCookie(cfg site.Config, _ []string) int {
	names := cfg.StoreNames()
	cookie, err := store.ResolveCookie(names)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintf(os.Stderr, "Try: browserauth %s login\n", cfg.ID)
		return 2
	}
	fmt.Print(cookie)
	return 0
}

func siteAuth(cfg site.Config, _ []string) int {
	names := cfg.StoreNames()
	cookie, err := store.ResolveCookie(names)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintf(os.Stderr, "Try: browserauth %s login\n", cfg.ID)
		return 2
	}

	session, err := cfg.FetchConfiguredSession(context.Background(), cookie)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintf(os.Stderr, "Try: browserauth %s login\n", cfg.ID)
		return 1
	}

	if session.UserID > 0 {
		fmt.Printf("OK: authenticated as %s (userId=%d)\n", session.Username, session.UserID)
	} else {
		fmt.Printf("OK: authenticated as %s\n", session.Username)
	}
	return 0
}

func siteAuthSet(cfg site.Config, args []string) int {
	raw := readCookieInput(args)
	if raw == "" {
		fmt.Fprintf(os.Stderr, "Usage: browserauth %s auth set <COOKIE_STRING>\n", cfg.ID)
		return 2
	}

	passphrase, err := cfg.Passphrase()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	cookiePath, err := store.WriteEncryptedCookie(cfg.StoreNames(), raw, passphrase)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fmt.Printf("Cookie stored (encrypted) at %s\n", cookiePath)
	return 0
}

func siteRequest(cfg site.Config, args []string) int {
	keepOpen := len(args) > 0 && args[0] == "--keep-open"
	if keepOpen {
		args = args[1:]
	}
	if len(args) > 0 && args[0] == "--requests" {
		return siteRequests(cfg, keepOpen, args[1:])
	}
	if len(args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: browserauth %s request [--keep-open] <METHOD> <PATH> [BODY]\n       browserauth %s request [--keep-open] --requests [JSON_ARRAY]\n", cfg.ID, cfg.ID)
		return 2
	}

	method := strings.ToUpper(strings.TrimSpace(args[0]))
	path := args[1]
	var bodyText string
	if len(args) > 2 {
		bodyText = strings.Join(args[2:], " ")
	} else if stat, _ := os.Stdin.Stat(); (stat.Mode() & os.ModeCharDevice) == 0 {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		if len(data) > 0 {
			bodyText = string(data)
		}
	}

	headers := map[string]string{}
	if bodyText != "" {
		headers["Content-Type"] = "application/json"
	}
	if cfg.Auth.Transport == "browser" {
		results, _ := executeBrowserRequests(cfg, []requestSpec{{Method: method, Path: path, rawBody: bodyText, Headers: headers}}, keepOpen)
		if len(results) == 0 {
			return 1
		}
		result := results[0]
		return writeRequestResult(result.data, result.Status, result.err)
	}

	var body io.Reader
	if bodyText != "" {
		body = strings.NewReader(bodyText)
	}

	client, err := httpclient.New(httpclient.NewOptions{
		BaseURL:     cfg.ResolvedBaseURL(),
		Names:       cfg.StoreNames(),
		AuthHeaders: cfg.Auth.AuthHeaders,
		NoFailEnv:   cfg.NoFailEnv,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	data, status, err := client.Do(method, path, body, headers)
	return writeRequestResult(data, status, err)
}

func hasRetainedSession(siteID string) bool {
	response, err := sessionmanager.Call(sessionmanager.Request{Action: "status"})
	if err != nil {
		return false
	}
	for _, item := range response.Sessions {
		if item.Site == siteID {
			return true
		}
	}
	return false
}

func responseError(response sessionmanager.Response) error {
	if response.OK {
		return nil
	}
	if response.Error == "" {
		return errors.New("retained browser request failed")
	}
	return errors.New(response.Error)
}

type requestSpec struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Body    json.RawMessage   `json:"body,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	rawBody string
}

type requestOutput struct {
	Index    int             `json:"index"`
	OK       bool            `json:"ok"`
	Status   int             `json:"status,omitempty"`
	Body     json.RawMessage `json:"body,omitempty"`
	BodyText string          `json:"body_text,omitempty"`
	Error    string          `json:"error,omitempty"`
}

type requestResult struct {
	requestOutput
	data []byte
	err  error
}

func (r requestSpec) bodyText() (string, error) {
	if r.rawBody != "" {
		return r.rawBody, nil
	}
	if len(r.Body) == 0 || string(r.Body) == "null" {
		return "", nil
	}
	if r.Body[0] == '"' {
		var value string
		if err := json.Unmarshal(r.Body, &value); err != nil {
			return "", err
		}
		return value, nil
	}
	return string(r.Body), nil
}

func siteRequests(cfg site.Config, keepOpen bool, args []string) int {
	var input []byte
	if len(args) > 0 {
		input = []byte(strings.Join(args, " "))
	} else {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		input = data
	}
	var requests []requestSpec
	if err := json.Unmarshal(input, &requests); err != nil {
		fmt.Fprintf(os.Stderr, "requests must be a JSON array: %v\n", err)
		return 2
	}
	if len(requests) == 0 {
		fmt.Fprintln(os.Stderr, "requests must contain at least one request")
		return 2
	}
	if cfg.Auth.Transport != "browser" {
		fmt.Fprintln(os.Stderr, "multiple requests currently require auth.transport: browser")
		return 2
	}
	results, failed := executeBrowserRequests(cfg, requests, keepOpen)
	output := make([]requestOutput, len(results))
	for index := range results {
		output[index] = results[index].requestOutput
	}
	if err := json.NewEncoder(os.Stdout).Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if failed {
		return 1
	}
	return 0
}

func executeBrowserRequests(cfg site.Config, requests []requestSpec, keepOpen bool) ([]requestResult, bool) {
	useRetained := keepOpen || hasRetainedSession(cfg.ID)
	var local *site.BrowserSession
	if useRetained {
		if err := sessionmanager.EnsureStarted(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return nil, true
		}
	} else {
		cookie, err := store.ResolveCookie(cfg.StoreNames())
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return nil, true
		}
		local, err = cfg.OpenBrowserSession(context.Background(), cookie, store.ResolveIsolatedProfile(cfg.StoreNames(), false))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return nil, true
		}
		defer local.Close()
	}
	results := make([]requestResult, 0, len(requests))
	failed := false
	for index, item := range requests {
		body, bodyErr := item.bodyText()
		if item.Headers == nil {
			item.Headers = map[string]string{}
		}
		if body != "" {
			if _, exists := item.Headers["Content-Type"]; !exists {
				item.Headers["Content-Type"] = "application/json"
			}
		}
		var data []byte
		var status int
		var err error
		if bodyErr != nil {
			err = bodyErr
		} else if strings.TrimSpace(item.Method) == "" || strings.TrimSpace(item.Path) == "" {
			err = errors.New("method and path are required")
		} else if local != nil {
			data, status, err = local.Request(item.Method, item.Path, body, item.Headers)
		} else {
			response, callErr := sessionmanager.Call(sessionmanager.Request{Action: "request", Site: cfg.ID, Method: item.Method, Path: item.Path, Body: body, Headers: item.Headers})
			if callErr != nil {
				err = callErr
			} else {
				data, status, err = []byte(response.Body), response.Status, responseError(response)
			}
		}
		out := requestResult{requestOutput: requestOutput{Index: index, OK: err == nil, Status: status}, data: data, err: err}
		if json.Valid(data) {
			out.Body = json.RawMessage(data)
		} else if len(data) > 0 {
			out.BodyText = string(data)
		}
		if err != nil {
			out.Error = err.Error()
			failed = true
		}
		results = append(results, out)
	}
	return results, failed
}

func siteSession(cfg site.Config, args []string) int {
	if len(args) != 1 {
		fmt.Fprintf(os.Stderr, "Usage: browserauth %s session start|status|stop\n", cfg.ID)
		return 2
	}
	switch args[0] {
	case "start":
		if err := sessionmanager.EnsureStarted(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		response, err := sessionmanager.Call(sessionmanager.Request{Action: "start", Site: cfg.ID})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if !response.OK {
			fmt.Fprintln(os.Stderr, response.Error)
			return 1
		}
		fmt.Printf("session ready: %s (%s)\n", cfg.ID, response.Username)
		return 0
	case "status":
		response, err := sessionmanager.Call(sessionmanager.Request{Action: "status"})
		if err != nil {
			fmt.Fprintln(os.Stderr, "no retained browser session")
			return 1
		}
		for _, item := range response.Sessions {
			if item.Site == cfg.ID {
				fmt.Printf("session ready: %s requests=%d idle=%s\n", cfg.ID, item.RequestCount, time.Since(item.LastUsed).Round(time.Second))
				return 0
			}
		}
		fmt.Printf("session not started: %s\n", cfg.ID)
		return 1
	case "stop":
		response, err := sessionmanager.Call(sessionmanager.Request{Action: "stop_session", Site: cfg.ID})
		if err != nil {
			fmt.Fprintln(os.Stderr, "no retained browser session")
			return 1
		}
		fmt.Printf("session stopped: %s (existed=%s)\n", cfg.ID, response.Body)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Usage: browserauth %s session start|status|stop\n", cfg.ID)
		return 2
	}
}

func writeRequestResult(data []byte, status int, err error) int {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if _, werr := os.Stdout.Write(data); werr != nil {
		fmt.Fprintln(os.Stderr, werr)
		return 2
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		fmt.Println()
	}
	if status >= 400 {
		return 1
	}
	return 0
}

func readCookieInput(args []string) string {
	if len(args) > 0 {
		return strings.TrimSpace(strings.Join(args, " "))
	}
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text())
	}
	return ""
}

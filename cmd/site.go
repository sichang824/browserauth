package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"skills-browserauth/chromebrowser"
	"skills-browserauth/httpclient"
	"skills-browserauth/login"
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
	case "auth":
		if len(rest) > 0 && rest[0] == "set" {
			return siteAuthSet(cfg, rest[1:])
		}
		return siteAuth(cfg, rest)
	case "request":
		return siteRequest(cfg, rest)
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

	session, err := cfg.FetchSession(cookie)
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
	if len(args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: browserauth %s request <METHOD> <PATH> [BODY]\n", cfg.ID)
		return 2
	}

	method := strings.ToUpper(strings.TrimSpace(args[0]))
	path := args[1]
	var body io.Reader
	if len(args) > 2 {
		body = strings.NewReader(strings.Join(args[2:], " "))
	} else if stat, _ := os.Stdin.Stat(); (stat.Mode() & os.ModeCharDevice) == 0 {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		if len(data) > 0 {
			body = strings.NewReader(string(data))
		}
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

	headers := map[string]string{}
	if body != nil {
		headers["Content-Type"] = "application/json"
	}

	data, status, err := client.Do(method, path, body, headers)
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

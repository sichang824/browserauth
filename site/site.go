package site

import (
	"context"
	"os"
	"strings"
	"time"

	"skills-browserauth/login"
	"skills-browserauth/store"
)

// Config describes one cookie-auth web app.
type Config struct {
	ID            string        `yaml:"id"`
	BaseURL       string        `yaml:"base_url"`
	LoginURL      string        `yaml:"login_url"`
	Cookie        CookieConfig  `yaml:"cookie"`
	Profile       ProfileConfig `yaml:"profile"`
	Browser       BrowserConfig `yaml:"browser"`
	ChromePathEnv string        `yaml:"chrome_path_env"`
	Auth          AuthConfig    `yaml:"auth"`
	NoFailEnv     string        `yaml:"no_fail_env"`
}

// BrowserConfig controls how browser-backed requests reuse Chrome.
type BrowserConfig struct {
	IdleTimeout string `yaml:"idle_timeout"`
	MaxLifetime string `yaml:"max_lifetime"`
	EntryText   string `yaml:"entry_text"`
}

func (c Config) BrowserIdleTimeout() time.Duration {
	if d, err := time.ParseDuration(strings.TrimSpace(c.Browser.IdleTimeout)); err == nil && d > 0 {
		return d
	}
	return 0
}

func (c Config) BrowserMaxLifetime() time.Duration {
	if d, err := time.ParseDuration(strings.TrimSpace(c.Browser.MaxLifetime)); err == nil && d > 0 {
		return d
	}
	return 0
}

type CookieConfig struct {
	Magic   string `yaml:"magic"`
	File    string `yaml:"file"`
	KeyEnv  string `yaml:"key_env"`
	Env     string `yaml:"env"`
	FileEnv string `yaml:"file_env"`
}

type ProfileConfig struct {
	Subdir string `yaml:"subdir"`
	DirEnv string `yaml:"dir_env"`
}

type AuthConfig struct {
	Transport         string        `yaml:"transport"`
	Method            string        `yaml:"method"`
	Path              string        `yaml:"path"`
	OKStatus          int           `yaml:"ok_status"`
	Token             *TokenConfig  `yaml:"token"`
	Tokens            []TokenConfig `yaml:"tokens"`
	UsernameJSONPaths []string      `yaml:"username_json_paths"`
	IdentityJSONPaths []string      `yaml:"identity_json_paths"`
	UserIDJSONPath    string        `yaml:"user_id_json_path"`
	UsernameForbidden []string      `yaml:"username_forbidden"`
}

// Session holds validated session details.
type Session struct {
	Username string
	UserID   int64
}

// StoreNames maps site config to store.AppNames.
func (c Config) StoreNames() store.AppNames {
	fileEnv := c.Cookie.FileEnv
	if fileEnv == "" {
		fileEnv = strings.ToUpper(c.ID) + "_COOKIE_FILE"
	}
	return store.AppNames{
		SiteID:         c.ID,
		Magic:          c.Cookie.Magic,
		CookieFileName: c.Cookie.File,
		ProfileSubdir:  c.Profile.Subdir,
		CookieKeyEnv:   c.Cookie.KeyEnv,
		CookieEnv:      c.Cookie.Env,
		CookieFileEnv:  fileEnv,
		ProfileDirEnv:  c.Profile.DirEnv,
	}
}

// ResolvedBaseURL returns trimmed base URL with env override support.
func (c Config) ResolvedBaseURL() string {
	if v := strings.TrimSpace(os.Getenv(strings.ToUpper(c.ID) + "_BASE_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	if v := strings.TrimSpace(os.Getenv(c.ID + "_BASE_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return strings.TrimRight(c.BaseURL, "/")
}

// ResolvedLoginURL returns the browser entry URL.
func (c Config) ResolvedLoginURL() string {
	envKey := strings.ToUpper(c.ID) + "_LOGIN_URL"
	if v := strings.TrimSpace(os.Getenv(envKey)); v != "" {
		return v
	}
	if strings.HasPrefix(c.LoginURL, "http://") || strings.HasPrefix(c.LoginURL, "https://") {
		return c.LoginURL
	}
	return c.ResolvedBaseURL() + "/" + strings.TrimLeft(c.LoginURL, "/")
}

// LoginConfig builds login capture settings for this site.
func (c Config) LoginConfig(isolatedProfileFlag bool) login.Config {
	names := c.StoreNames()
	cfg := login.Config{
		BaseURL:         c.ResolvedBaseURL(),
		LoginURL:        c.ResolvedLoginURL(),
		Names:           names,
		ChromePathEnv:   c.ChromePathEnv,
		IsolatedProfile: store.ResolveIsolatedProfile(names, isolatedProfileFlag),
		Validate:        c.ValidateSession,
	}
	if c.Auth.Transport == "browser" {
		cfg.ValidateBrowser = func(ctx context.Context, cookieHeader string) (string, error) {
			session, err := c.validateBrowserSessionOnce(ctx, cookieHeader)
			return session.Username, err
		}
	}
	return cfg
}

// Passphrase reads the encryption key for cookie files.
func (c Config) Passphrase() (string, error) {
	return store.ResolvePassphrase(c.StoreNames())
}

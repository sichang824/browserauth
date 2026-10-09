package site

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Load returns a site config by id.
func Load(id string) (Config, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Config{}, fmt.Errorf("site id is required")
	}

	path := SiteFilePath(id)
	cfg, err := loadFile(path)
	if err == nil {
		return cfg, nil
	}
	if os.IsNotExist(err) {
		return Config{}, fmt.Errorf("site %q not found (%s); run: browserauth sites add %s --from-file <yaml>", id, path, id)
	}
	return Config{}, err
}

// List returns all known site ids from the sites directory.
func List() ([]string, error) {
	dir := DefaultSitesDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	ids := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(entry.Name(), ".yaml"))
	}
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			if ids[j] < ids[i] {
				ids[i], ids[j] = ids[j], ids[i]
			}
		}
	}
	return ids, nil
}

func loadFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	return parseConfig(data)
}

func parseConfig(data []byte) (Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse site config: %w", err)
	}
	if cfg.ID == "" {
		return Config{}, fmt.Errorf("site config missing id")
	}
	if cfg.BaseURL == "" {
		return Config{}, fmt.Errorf("site %q missing base_url", cfg.ID)
	}
	if cfg.Cookie.Magic == "" {
		return Config{}, fmt.Errorf("site %q missing cookie.magic", cfg.ID)
	}
	if cfg.Cookie.Env == "" {
		cfg.Cookie.Env = strings.ToUpper(cfg.ID) + "_COOKIE"
	}
	if cfg.Profile.Subdir == "" {
		cfg.Profile.Subdir = cfg.ID + "/profile"
	}
	if cfg.LoginURL == "" {
		cfg.LoginURL = "/"
	}
	cfg.Auth.Transport = strings.ToLower(strings.TrimSpace(cfg.Auth.Transport))
	switch cfg.Auth.Transport {
	case "", "http", "browser":
	default:
		return Config{}, fmt.Errorf("site %q has unsupported auth.transport %q", cfg.ID, cfg.Auth.Transport)
	}
	for name, value := range map[string]string{"idle_timeout": cfg.Browser.IdleTimeout, "max_lifetime": cfg.Browser.MaxLifetime} {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if duration, err := time.ParseDuration(value); err != nil || duration <= 0 {
			return Config{}, fmt.Errorf("site %q has invalid browser.%s %q", cfg.ID, name, value)
		}
	}
	for _, name := range cfg.Browser.ClearHostCookies {
		if strings.TrimSpace(name) == "" {
			return Config{}, fmt.Errorf("site %q has empty browser.clear_host_cookies entry", cfg.ID)
		}
	}
	if len(cfg.Browser.ClearHostCookies) > 0 && cfg.BrowserInjectCookie() {
		return Config{}, fmt.Errorf("site %q requires browser.inject_cookie: false when browser.clear_host_cookies is configured", cfg.ID)
	}
	return cfg, nil
}

// MarshalYAML serializes a site config, emitting only non-empty fields in the
// conventional order (matches the hand-written reference YAMLs).
func MarshalYAML(cfg Config) ([]byte, error) {
	out := yamlConfig{
		ID:            cfg.ID,
		BaseURL:       cfg.BaseURL,
		LoginURL:      cfg.LoginURL,
		ChromePathEnv: cfg.ChromePathEnv,
		NoFailEnv:     cfg.NoFailEnv,
	}
	if out.LoginURL == "/" { // parseConfig default; keep files clean
		out.LoginURL = ""
	}
	cookie := yamlCookie{
		Magic:   cfg.Cookie.Magic,
		File:    cfg.Cookie.File,
		KeyEnv:  cfg.Cookie.KeyEnv,
		Env:     cfg.Cookie.Env,
		FileEnv: cfg.Cookie.FileEnv,
	}
	if cookie != (yamlCookie{}) {
		out.Cookie = &cookie
	}
	profile := yamlProfile{Subdir: cfg.Profile.Subdir, DirEnv: cfg.Profile.DirEnv}
	if profile != (yamlProfile{}) {
		out.Profile = &profile
	}
	browser := yamlBrowser{IdleTimeout: cfg.Browser.IdleTimeout, MaxLifetime: cfg.Browser.MaxLifetime, EntryText: cfg.Browser.EntryText, InjectCookie: cfg.Browser.InjectCookie, ClearHostCookies: cfg.Browser.ClearHostCookies}
	if browser.IdleTimeout != "" || browser.MaxLifetime != "" || browser.EntryText != "" || browser.InjectCookie != nil || len(browser.ClearHostCookies) > 0 {
		out.Browser = &browser
	}
	auth := yamlAuth{
		Transport:         cfg.Auth.Transport,
		Method:            cfg.Auth.Method,
		Path:              cfg.Auth.Path,
		OKStatus:          cfg.Auth.OKStatus,
		UsernameJSONPaths: cfg.Auth.UsernameJSONPaths,
		IdentityJSONPaths: cfg.Auth.IdentityJSONPaths,
		UserIDJSONPath:    cfg.Auth.UserIDJSONPath,
		UsernameForbidden: cfg.Auth.UsernameForbidden,
	}
	if cfg.Auth.Token != nil {
		auth.Token = &yamlToken{Cookie: cfg.Auth.Token.Cookie, Header: cfg.Auth.Token.Header, Prefix: cfg.Auth.Token.Prefix}
	}
	for _, tk := range cfg.Auth.Tokens {
		auth.Tokens = append(auth.Tokens, yamlToken{Cookie: tk.Cookie, Header: tk.Header, Prefix: tk.Prefix})
	}
	if auth.Transport != "" || auth.Method != "" || auth.Path != "" || auth.OKStatus != 0 || auth.Token != nil ||
		len(auth.Tokens) > 0 || len(auth.UsernameJSONPaths) > 0 || len(auth.IdentityJSONPaths) > 0 ||
		auth.UserIDJSONPath != "" || len(auth.UsernameForbidden) > 0 {
		out.Auth = &auth
	}
	return yaml.Marshal(out)
}

// yamlConfig mirrors Config with omitempty/pointer fields for clean output.
type yamlConfig struct {
	ID            string       `yaml:"id"`
	BaseURL       string       `yaml:"base_url"`
	LoginURL      string       `yaml:"login_url,omitempty"`
	Cookie        *yamlCookie  `yaml:"cookie,omitempty"`
	Profile       *yamlProfile `yaml:"profile,omitempty"`
	Browser       *yamlBrowser `yaml:"browser,omitempty"`
	ChromePathEnv string       `yaml:"chrome_path_env,omitempty"`
	Auth          *yamlAuth    `yaml:"auth,omitempty"`
	NoFailEnv     string       `yaml:"no_fail_env,omitempty"`
}

type yamlBrowser struct {
	IdleTimeout      string   `yaml:"idle_timeout,omitempty"`
	MaxLifetime      string   `yaml:"max_lifetime,omitempty"`
	EntryText        string   `yaml:"entry_text,omitempty"`
	InjectCookie     *bool    `yaml:"inject_cookie,omitempty"`
	ClearHostCookies []string `yaml:"clear_host_cookies,omitempty"`
}

type yamlCookie struct {
	Magic   string `yaml:"magic,omitempty"`
	File    string `yaml:"file,omitempty"`
	KeyEnv  string `yaml:"key_env,omitempty"`
	Env     string `yaml:"env,omitempty"`
	FileEnv string `yaml:"file_env,omitempty"`
}

type yamlProfile struct {
	Subdir string `yaml:"subdir,omitempty"`
	DirEnv string `yaml:"dir_env,omitempty"`
}

type yamlToken struct {
	Cookie string `yaml:"cookie"`
	Header string `yaml:"header"`
	Prefix string `yaml:"prefix"`
}

type yamlAuth struct {
	Transport         string      `yaml:"transport,omitempty"`
	Method            string      `yaml:"method,omitempty"`
	Path              string      `yaml:"path,omitempty"`
	OKStatus          int         `yaml:"ok_status,omitempty"`
	Token             *yamlToken  `yaml:"token,omitempty"`
	Tokens            []yamlToken `yaml:"tokens,omitempty"`
	UsernameJSONPaths []string    `yaml:"username_json_paths,omitempty"`
	IdentityJSONPaths []string    `yaml:"identity_json_paths,omitempty"`
	UserIDJSONPath    string      `yaml:"user_id_json_path,omitempty"`
	UsernameForbidden []string    `yaml:"username_forbidden,omitempty"`
}

// WriteFile writes a site config to the sites directory.
func WriteFile(cfg Config) (string, error) {
	data, err := MarshalYAML(cfg)
	if err != nil {
		return "", err
	}
	return WriteRawFile(cfg.ID, data)
}

// WriteRawFile writes raw YAML bytes for a site id.
func WriteRawFile(id string, data []byte) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("site id is required")
	}
	if _, err := parseConfig(data); err != nil {
		return "", err
	}
	dir := DefaultSitesDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("prepare sites dir: %w", err)
	}
	path := SiteFilePath(id)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("write site file: %w", err)
	}
	return path, nil
}

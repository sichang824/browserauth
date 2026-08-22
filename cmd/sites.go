package cmd

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"skills-browserauth/site"
	"skills-browserauth/store"
)

const sitesUsage = `browserauth sites - manage site YAML configs

Usage:
  browserauth sites list
  browserauth sites path
  browserauth sites show <id>
  browserauth sites init
  browserauth sites new <name> [options]          Create a site from flags
  browserauth sites add <id> --from-file PATH [--force]
  browserauth sites remove <id>

sites new options (all optional; defaults derived from <name>):
  --base-url URL          Base URL (the only field the format cannot derive)
  --login-url PATH|URL    Browser entry (default /)
  --cookie-magic/--cookie-env/--cookie-file/--cookie-key-env/--cookie-file-env
  --profile-subdir/--profile-dir-env/--chrome-path-env/--no-fail-env
  --auth-method/--auth-path/--ok-status
  --username-path (repeatable)  --identity-path (repeatable)
  --userid-path                 --forbidden-username (repeatable)
  --token-cookie/--token-header/--token-prefix
  --force                 Overwrite existing site file

Sites directory (default): ~/.browserauth/sites
Override: BROWSERAUTH_SITES_DIR or BROWSERAUTH_DATA_DIR
`

// Sites handles "browserauth sites ..." commands.
func Sites(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, sitesUsage)
		return 2
	}

	switch args[0] {
	case "list":
		return listSites()
	case "path":
		fmt.Println(site.DefaultSitesDir())
		return 0
	case "show":
		return showSite(args[1:])
	case "init":
		return initSites(args[1:])
	case "new":
		return newSite(args[1:])
	case "add":
		return addSite(args[1:])
	case "remove", "rm":
		return removeSite(args[1:])
	case "-h", "--help", "help":
		fmt.Print(sitesUsage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Unknown sites subcommand: %s\n\n", args[0])
		fmt.Fprint(os.Stderr, sitesUsage)
		return 2
	}
}

func listSites() int {
	ids, err := site.List()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if len(ids) == 0 {
		fmt.Fprintf(os.Stderr, "No sites in %s\n", site.DefaultSitesDir())
		fmt.Fprintln(os.Stderr, "Add one: browserauth sites add <id> --from-file <yaml>")
		return 0
	}
	for _, id := range ids {
		cfg, err := site.Load(id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", id, err)
			continue
		}
		fmt.Printf("%s\t%s\n", id, cfg.ResolvedBaseURL())
	}
	return 0
}

func showSite(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "Usage: browserauth sites show <id>")
		return 2
	}
	data, err := site.ReadRaw(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fmt.Print(string(data))
	if len(data) > 0 && data[len(data)-1] != '\n' {
		fmt.Println()
	}
	return 0
}

func initSites(_ []string) int {
	dir, err := site.EnsureSitesDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fmt.Printf("Sites directory ready: %s\n", dir)
	return 0
}

// stringSlice is a repeatable string flag (--username-path a --username-path b).
type stringSlice []string

func (s *stringSlice) String() string     { return strings.Join(*s, ",") }
func (s *stringSlice) Set(v string) error { *s = append(*s, v); return nil }

const newSiteUsage = "Usage: browserauth sites new <name> [--base-url URL] [options]  (see: browserauth sites help)"

func newSite(args []string) int {
	// flag.Parse stops at the first positional; rotate a leading site name to
	// the end so "sites new <name> --flags" works as documented.
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		args = append(append([]string{}, args[1:]...), args[0])
	}
	fs := flag.NewFlagSet("sites new", flag.ExitOnError)
	baseURL := fs.String("base-url", "", "site base URL, e.g. https://www.example.com")
	loginURL := fs.String("login-url", "", "browser entry path or absolute URL (default /)")
	cookieMagic := fs.String("cookie-magic", "", "cookie magic marker (default <NAME>ENC)")
	cookieEnv := fs.String("cookie-env", "", "inline cookie env var (default <NAME>_COOKIE)")
	cookieFile := fs.String("cookie-file", "", "cookie file name override")
	cookieKeyEnv := fs.String("cookie-key-env", "", "legacy per-site passphrase env")
	cookieFileEnv := fs.String("cookie-file-env", "", "cookie file path env override")
	profileSubdir := fs.String("profile-subdir", "", "isolated profile subdir")
	profileDirEnv := fs.String("profile-dir-env", "", "profile dir env override")
	chromePathEnv := fs.String("chrome-path-env", "", "Chrome binary env var (default <NAME>_CHROME_PATH)")
	noFailEnv := fs.String("no-fail-env", "", "no-fail env var (default <NAME>_NO_FAIL)")
	authMethod := fs.String("auth-method", "", "auth check method (default GET)")
	authPath := fs.String("auth-path", "", "auth check path (JSON API)")
	okStatus := fs.Int("ok-status", 0, "auth check expected status (default 200)")
	var usernamePaths, identityPaths, forbiddenUsernames stringSlice
	fs.Var(&usernamePaths, "username-path", "username JSON path (repeatable)")
	fs.Var(&identityPaths, "identity-path", "identity JSON path (repeatable)")
	userIDPath := fs.String("userid-path", "", "user id JSON path")
	fs.Var(&forbiddenUsernames, "forbidden-username", "forbidden username value (repeatable)")
	tokenCookie := fs.String("token-cookie", "", "token source cookie name")
	tokenHeader := fs.String("token-header", "", "token destination header")
	tokenPrefix := fs.String("token-prefix", "", "token header prefix (e.g. 'Bearer ')")
	force := fs.Bool("force", false, "overwrite existing site file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, newSiteUsage)
		return 2
	}

	name := strings.TrimSpace(fs.Arg(0))
	if !site.ValidSiteID(name) {
		fmt.Fprintf(os.Stderr, "invalid site name %q: use letters, digits, '.', '-', '_' (no spaces or '/'; it becomes the file name)\n", name)
		return 2
	}
	if strings.TrimSpace(*baseURL) == "" {
		fmt.Fprintf(os.Stderr, "--base-url is required: the site format cannot work without it\n%s\n", newSiteUsage)
		return 2
	}
	if site.Exists(name) && !*force {
		fmt.Fprintf(os.Stderr, "site %q already exists at %s (use --force)\n", name, site.SiteFilePath(name))
		return 2
	}

	prefix := site.DeriveEnvPrefix(name)
	cfg := site.Config{
		ID:       name,
		BaseURL:  strings.TrimSpace(*baseURL),
		LoginURL: strings.TrimSpace(*loginURL),
		Cookie: site.CookieConfig{
			Magic:   firstNonEmpty(*cookieMagic, site.DeriveCookieMagic(name)),
			Env:     firstNonEmpty(*cookieEnv, prefix+"_COOKIE"),
			File:    *cookieFile,
			KeyEnv:  *cookieKeyEnv,
			FileEnv: *cookieFileEnv,
		},
		Profile:       site.ProfileConfig{Subdir: *profileSubdir, DirEnv: *profileDirEnv},
		ChromePathEnv: firstNonEmpty(*chromePathEnv, prefix+"_CHROME_PATH"),
		NoFailEnv:     firstNonEmpty(*noFailEnv, prefix+"_NO_FAIL"),
		Auth: site.AuthConfig{
			Method:            *authMethod,
			Path:              *authPath,
			OKStatus:          *okStatus,
			UsernameJSONPaths: usernamePaths,
			IdentityJSONPaths: identityPaths,
			UserIDJSONPath:    *userIDPath,
			UsernameForbidden: forbiddenUsernames,
		},
	}
	if *tokenCookie != "" {
		cfg.Auth.Token = &site.TokenConfig{Cookie: *tokenCookie, Header: *tokenHeader, Prefix: *tokenPrefix}
	}

	path, err := site.WriteFile(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	loaded, err := site.Load(name)
	if err != nil {
		loaded = cfg
	}
	fmt.Printf("Site %q created\n", name)
	fmt.Printf("  base_url:     %s\n", loaded.ResolvedBaseURL())
	fmt.Printf("  login_url:    %s\n", loaded.ResolvedLoginURL())
	fmt.Printf("  cookie env:   %s\n", loaded.Cookie.Env)
	fmt.Printf("  cookie file:  %s\n", store.CookieFilePath(loaded.StoreNames()))
	if loaded.Auth.Path != "" {
		method := loaded.Auth.Method
		if method == "" {
			method = "GET"
		}
		fmt.Printf("  auth:         %s %s\n", method, loaded.Auth.Path)
	} else {
		fmt.Printf("  auth:         not configured (set --auth-path before login)\n")
	}
	fmt.Printf("Site file: %s\n", path)
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func addSite(args []string) int {
	// flag.Parse stops at the first positional; rotate a leading site id to
	// the end so "sites add <id> --from-file PATH" works as documented.
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		args = append(append([]string{}, args[1:]...), args[0])
	}
	fs := flag.NewFlagSet("sites add", flag.ExitOnError)
	fromFile := fs.String("from-file", "", "YAML file to install (required)")
	force := fs.Bool("force", false, "overwrite existing site file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "Usage: browserauth sites add <id> --from-file PATH [--force]")
		return 2
	}
	if *fromFile == "" {
		fmt.Fprintln(os.Stderr, "Usage: browserauth sites add <id> --from-file PATH [--force]")
		return 2
	}
	id := strings.TrimSpace(fs.Arg(0))
	path, err := site.Add(id, *fromFile, *force)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fmt.Printf("Site %q saved to %s\n", id, path)
	return 0
}

func removeSite(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "Usage: browserauth sites remove <id>")
		return 2
	}
	id := strings.TrimSpace(args[0])
	if err := site.Remove(id); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fmt.Printf("Removed site %q from %s\n", id, site.DefaultSitesDir())
	return 0
}

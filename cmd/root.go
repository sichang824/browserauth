package cmd

import (
	"fmt"
	"os"
)

const usage = `browserauth - cookie-auth CLI for any configured web site

Usage:
  browserauth sites list|path|show|init|add|remove
  browserauth <site> login [--isolated-profile]
  browserauth <site> browser [--isolated-profile]
  browserauth <site> record [--isolated-profile] [url]   Record traffic to HAR
  browserauth <site> auth
  browserauth <site> auth set [COOKIE]
  browserauth <site> request <METHOD> <PATH> [BODY]
  browserauth <site> cookie          Print resolved cookie (for shell scripts)
  browserauth help

Sites are YAML files in ~/.browserauth/sites/<id>.yaml.
Manage with: browserauth sites init|new|add --from-file|remove|show
Override dir: BROWSERAUTH_SITES_DIR (or BROWSERAUTH_DATA_DIR)

Global env:
  BROWSERAUTH_COOKIE_KEY   Encryption passphrase for all sites (required)
  BROWSERAUTH_DATA_DIR     Data root (default: ~/.browserauth)
  BROWSER_CHROME_PATH      Chrome binary for all sites
  BROWSERAUTH_SITES_DIR    Extra directory for site YAML configs

Default paths (under ~/.browserauth):
  cookies/<site>           Encrypted cookie per site
  profile/                 Shared Chrome profile (all sites)
  recordings/<site>-<ts>.har  Recorded traffic (contains Cookie headers!)

Isolated profile (optional):
  --isolated-profile       Per-site profile at profiles/<site>
  BROWSERAUTH_ISOLATED_PROFILE=1
  {SITE}_ISOLATED_PROFILE=1

Per-site env (optional overrides):
  {SITE}_BASE_URL, {SITE}_LOGIN_URL, {SITE}_COOKIE
  {SITE}_COOKIE_FILE, {SITE}_PROFILE_DIR, BROWSERAUTH_PROFILE_DIR

Examples:
  export BROWSERAUTH_COOKIE_KEY='your-passphrase'
  browserauth sites add jira --from-file references/sites/jira.yaml
  browserauth sites list
  browserauth tingwu login
  browserauth tingwu auth
  browserauth jira request GET /rest/api/2/myself
  browserauth tingwu request POST '/api/trans/request?getTransStatus&c=web' '{"action":"getTransStatus"}'
  browserauth tingwu record
`

// Run is the main entry point for the browserauth CLI.
func Run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}

	switch args[0] {
	case "-h", "--help", "help":
		fmt.Print(usage)
		return 0
	case "sites":
		return Sites(args[1:])
	default:
		return RunSite(args)
	}
}

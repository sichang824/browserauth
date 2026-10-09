---
name: browserauth
description: >-
  Universal cookie-auth CLI for any web site. YAML site configs drive login,
  browser profile, auth verify, and authenticated HTTP requests. Use for
  browserauth tingwu login, browserauth jira auth, adding new sites without Go code.
---

# BrowserAuth (universal cookie-auth CLI)

One CLI for browser login, session verify, and cookie-authenticated HTTP — **no per-site login code**.

## Commands

| Command | Purpose |
|---------|---------|
| `browserauth sites list` | List configured sites |
| `browserauth <site> login` | Browser login → encrypt cookie |
| `browserauth <site> browser` | Persistent Chrome (extensions OK) |
| `browserauth <site> auth` | Verify session |
| `browserauth <site> auth set` | Store encrypted cookie manually |
| `browserauth <site> request <METHOD> <PATH> [BODY]` | Authenticated HTTP |
| `browserauth <site> request --requests '[…]'` | One or more requests over one browser session |
| `browserauth <site> session start [--observe]\|status\|stop` | Manage one site's retained browser session |
| `browserauth <site> xhr status\|list\|get\|wait\|clear` | Inspect page-native XHR/Fetch captured by the session observer |
| `browserauth <site> page reload\|scroll` | Trigger page behavior without replaying signed requests |
| `browserauth session status\|stop` | Inspect or close retained browser sessions |
| `browserauth <site> record [url]` | Record tab traffic to HAR (stop via injected ⏹ button) |
| `browserauth <site> cookie` | Print cookie for shell/`oapi --cookie` (do not log) |

## Setup (once)

```bash
# ~/.zshrc — one passphrase for all sites
export BROWSERAUTH_COOKIE_KEY='your-passphrase'

browserauth jira login
browserauth tingwu login
```

Legacy per-site keys (`JIRA_COOKIE_KEY`, `TINGWU_COOKIE_KEY`) still work as fallback during migration.

## Default storage

All data under **`~/.browserauth/`** (override with `BROWSERAUTH_DATA_DIR`):

| Path | Content |
|------|---------|
| `cookies/<site>` | Encrypted cookie per site |
| `profile/` | **Shared** Chrome profile (default) |
| `profiles/<site>` | Per-site profile (when isolated) |
| `recordings/<site>-<ts>.har` | Recorded traffic (0600, contains Cookie headers — do not commit/share) |

**Isolated profile** (optional — separate login state per site):

```bash
browserauth jira login --isolated-profile
export BROWSERAUTH_ISOLATED_PROFILE=1          # all sites
export JIRA_ISOLATED_PROFILE=1                 # one site
```

Overrides: `BROWSERAUTH_PROFILE_DIR`, `{SITE}_PROFILE_DIR`, `{SITE}_COOKIE_FILE`, `{SITE}_COOKIE`.

## Recording (site discovery / onboarding)

```bash
browserauth <site> record              # opens the site login_url, records traffic
browserauth <site> record <url>        # start from another URL
```

Opens the persistent-profile browser, captures the tab's network traffic
(requests, wire headers incl. Cookie, post bodies, response bodies ≤1MB for
XHR/Fetch/Document) and injects a floating **⏹ 结束录制** button. Clicking it
writes `recordings/<site>-<ts>.har` and closes the browser; Ctrl+C or closing
the window saves the recording too. Use the HAR to pick the auth endpoint /
headers / JSON paths when writing a new site YAML. Notes:

- Only the managed tab is captured; links that would open a new tab are
  rewritten to stay in-tab.
- Bodies >1MB are truncated (`_bodyTruncated`); binary types keep metadata only.
- Redirect chains become one HAR entry per hop.

## Site configs

Directory: **`~/.browserauth/sites/{id}.yaml`**

```bash
browserauth sites init
browserauth sites new myapp --base-url https://myapp.example.com   # create from flags
browserauth sites add --from-file ./myapp.yaml myapp  # install your own YAML
browserauth sites list
browserauth sites show jira
browserauth sites remove myapp
browserauth sites path
```

Flags go **before** the site id (Go `flag` stops at the first positional):
`browserauth sites add --force --from-file <yaml> <id>`.

Site YAML `auth.transport` defaults to `http`; set it to `browser` for WAF-protected
sites that require a real browser/TLS fingerprint. `request` accepts either the legacy
single-request arguments or `--requests` with a JSON array containing one or more
requests. It opens one browser, reuses it for every request in the array, then closes it.
Add `--keep-open` when later commands should retain and reuse that browser. Once
retained, ordinary later `request` commands reuse it without repeating the flag and
do not close it. It remains available until the browser window is closed or
`browserauth <site> session stop` is run. Optional `browser.idle_timeout` and
`browser.max_lifetime` values can impose explicit limits. The local session socket is mode 0600 and
business URLs must be same-origin paths. The configured read-only auth check may poll,
but each business request is sent once and never retried.

### Observe page-native XHR/Fetch

Use observation when a site signs its own requests in JavaScript and direct replay is
invalid. The observer belongs to the retained browser session, so one login can support
many page actions and log queries until `session stop`:

```bash
browserauth douyin session start --observe
browserauth douyin page reload
browserauth douyin xhr list --match '/feed/' --limit 10
browserauth douyin xhr get xhr-12
browserauth douyin xhr get xhr-12 --response
browserauth douyin xhr wait --match '/feed/' --after 12 --timeout 30s
browserauth douyin page scroll --times 3 --wait-xhr '/feed/'
browserauth douyin xhr clear
browserauth douyin session stop
```

`request --keep-open --observe` can enable the same observer while making a
browser-backed API call. `xhr` commands only inspect an existing retained session and
never open a browser themselves. `page reload` and `page scroll` make the page create
fresh signed requests; they do not replay captured URLs. Only XHR/Fetch metadata and
bodies are retained: authorization headers and cookies are not collected. The in-memory
log keeps at most 200 entries per session and response bodies are capped at 1 MiB.

`browser.inject_cookie` controls whether the encrypted saved Cookie header is copied
into a newly opened browser. It defaults to `true` for compatibility. Set it to
`false` for sites such as Douyin whose QR login writes authoritative cookies into the
persistent profile; this avoids stale host cookies conflicting with fresh domain cookies.
For profiles already polluted by an older version, `browser.clear_host_cookies` accepts
a list of exact cookie names to delete from the `base_url` host on every startup while
leaving parent-domain and unrelated device cookies intact.

Browser-backed business requests also appear in a fixed monitor panel at the top of
the managed tab. The compact list shows method, path, HTTP status, and duration. Click
a row to inspect its request/response body, or use **全屏** for the complete panel.
The newest 100 entries survive same-tab navigation through `sessionStorage`; auth
headers and cookies are deliberately excluded from the panel. Drag the title bar to
move the panel away from page controls; its position survives same-tab navigation,
and double-clicking the title bar restores the default top-center position.
`auth` also supports cookie→header token mapping: `token: {cookie, header, prefix}`
or a `tokens:` list when an API needs several derived headers (e.g. dify-console sends
both `Authorization: Bearer` and `x-csrf-token`). `username_json_paths` /
`identity_json_paths` support dotted paths with array indices (`workspaces.0.name`).

Site configs are local files under `~/.browserauth/sites/`; this repository does not bundle site templates.

### Add a site

From flags — only `<name>` is required; every other field is optional
(`magic`, env names are derived from the name; `--base-url` is the one field
the format cannot derive):

```bash
browserauth sites new xiaohongshu --base-url https://www.xiaohongshu.com \
  --login-url "/explore?channel_id=homefeed.love_v3"
# optional: --auth-transport http|browser/--auth-method/--auth-path/--ok-status/--username-path (repeatable)
#   --identity-path/--userid-path/--token-cookie+--token-header+--token-prefix
#   --cookie-env/--cookie-magic/--chrome-path-env/--no-fail-env/--force ...
# prints the created site's resolved info + YAML path
```

Or install a hand-written YAML:

```bash
browserauth sites add myapp --from-file ./myapp.yaml
```

Cookie file: `~/.browserauth/cookies/myapp`

## Local site configurations

This repository does not bundle site-specific configurations. Create your own with
`browserauth sites new`, or install an authorized YAML with `browserauth sites add`.
Existing configurations remain under `~/.browserauth/sites/` and can be inspected
with `browserauth sites list`, `browserauth sites show <id>`, and `browserauth sites path`.
The `references/` directory contains only an empty `.gitkeep` placeholder.

## Env

| Variable | Purpose |
|----------|---------|
| `BROWSERAUTH_COOKIE_KEY` | **Required** encryption passphrase (all sites) |
| `BROWSERAUTH_DATA_DIR` | Data root (default `~/.browserauth`) |
| `BROWSER_CHROME_PATH` | Chrome binary |
| `BROWSERAUTH_SITES_DIR` | Extra site YAML directory |
| `{SITE}_COOKIE` | Inline cookie override |
| `{SITE}_COOKIE_FILE` | Cookie file path override |

## Agent recovery

```
1. browserauth <site> auth
2. browserauth <site> login   (needs BROWSERAUTH_COOKIE_KEY)
3. browserauth <site> auth
4. Run business command
```

## Related

- [browser skill](../browser/SKILL.md) — Chrome profile / extensions
- [login skill](../login/SKILL.md) — cookie-auth pattern

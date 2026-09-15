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
| `browserauth <site> session start\|status\|stop` | Manage one site's retained browser session |
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
browserauth sites add --from-file references/sites/jira.yaml jira  # install a YAML
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

Example YAML templates (not auto-installed): `browserauth/references/sites/`

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

## Built-in sites

| Site | Business | Auth |
|------|----------|------|
| authz | AuthZ Admin + API (`authz.zsclab.com` / `api.authz.zsclab.com`) | `browserauth authz …` → cookie `access_token` as Bearer; see [authz skill](../../.cursor/skills/authz/SKILL.md) |
| tingwu | `scripts/transcribe.sh` + `oapi` | `browserauth tingwu ...` |
| jira | `oapi call` + [jira/specs/jira.openapi.yaml](../jira/specs/jira.openapi.yaml) | `browserauth jira ...` |
| chatgpt | [chatgpt skill](../chatgpt/SKILL.md) headed Chrome send | `browserauth chatgpt ...` |
| dify-console | Dify console API (`yai.dhb168.com/console/api`); see [dify skill](../dify/SKILL.md) | `browserauth dify-console …` → `tokens:` sends Bearer + x-csrf-token |
| xiaohongshu | 小红书：login 在 `www.xiaohongshu.com`，auth 用 creator 平台 galaxy API | 主站 API 需 x-s/x-t 签名无法裸 Cookie 调用；galaxy `/api/galaxy/user/info` 免签名且认 www 的 `web_session` |
| etax | 自然人电子税务局 WEB 端扣缴 | `auth.transport: browser` 绕过严格 WAF；校验 `/web/zh/loginstatus/check` |
| douyin | 抖音：login 在 `www.douyin.com/jingxuan` | auth 用 `/aweme/v1/web/user/profile/self/?aid=6383`（HAR 验证可裸 Cookie；feed 等接口仍常带 a_bogus） |

Site YAML templates: `references/sites/` (includes `authz.yaml`, `chatgpt.yaml`, `dify-console.yaml`, `xiaohongshu.yaml`, `douyin.yaml`, `etax.yaml`).

### AuthZ notes

- Login UI and cookies: `https://authz.zsclab.com`
- REST / OIDC issuer: `https://api.authz.zsclab.com`
- `auth.token.cookie: access_token` → `Authorization: Bearer …`
- Cookie capture uses both `base_url` and absolute `login_url` when they differ

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

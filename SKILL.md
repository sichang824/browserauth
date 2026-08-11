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

**Isolated profile** (optional — separate login state per site):

```bash
browserauth jira login --isolated-profile
export BROWSERAUTH_ISOLATED_PROFILE=1          # all sites
export JIRA_ISOLATED_PROFILE=1                 # one site
```

Overrides: `BROWSERAUTH_PROFILE_DIR`, `{SITE}_PROFILE_DIR`, `{SITE}_COOKIE_FILE`, `{SITE}_COOKIE`.

## Site configs

Directory: **`~/.browserauth/sites/{id}.yaml`**

```bash
browserauth sites init
browserauth sites add jira --from-file references/sites/jira.yaml
browserauth sites add tingwu --from-file references/sites/tingwu.yaml
browserauth sites list
browserauth sites show jira
browserauth sites remove myapp
browserauth sites path
```

Example YAML templates (not auto-installed): `browserauth/references/sites/`

### Add a site

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

Site YAML templates: `references/sites/` (includes `authz.yaml`, `chatgpt.yaml`).

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

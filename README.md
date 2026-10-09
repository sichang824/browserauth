# BrowserAuth

BrowserAuth 是一个用 Go 编写的浏览器登录与 Cookie 认证命令行工具，也可作为 AI 代理的技能使用。通过 YAML 配置站点，统一完成浏览器登录、会话校验、带身份认证的 HTTP 请求、浏览器会话管理和网络流量观察。

**本项目采用 GNU AGPL v3.0（`AGPL-3.0-only`）。允许商业使用和二次开发；分发及修改后提供网络交互服务时，须按许可证履行相应的源码提供义务。**详见下方[授权许可](#授权许可)和 [LICENSE](LICENSE)。

本工具可以以已登录账号的身份访问数据和执行操作。使用前请阅读[免责声明](DISCLAIMER.md)，确认你对目标账号、系统、数据和具体操作拥有合法授权。

## 功能

- 在可见的 Chrome / Chromium 中完成登录，将捕获的 Cookie 加密保存。
- 使用 YAML 描述站点、登录入口、会话校验接口及 Cookie 到认证请求头的映射。
- 通过 HTTP 或浏览器发送认证请求，支持批量请求和保留浏览器会话。
- 观察页面自身产生的 XHR / Fetch，通过刷新、滚动触发页面行为。
- 将受管理标签页的流量录制为 HAR，辅助理解和配置获授权站点。
- 使用持久化浏览器配置；默认共享，也可按站点隔离。

完整命令和配置说明见 [SKILL.md](SKILL.md)。站点模板位于 [references/sites](references/sites)，需检查并调整目标地址后安装，不会自动安装。

## 安装

需要 Go 1.25.1 或更新版本，以及本机可用的 Chrome / Chromium。以下示例使用 macOS / Linux 的 shell。

```bash
git clone https://github.com/sichang824/browserauth.git
cd browserauth
make install
export PATH="$HOME/.local/bin:$PATH"
browserauth help
```

`make install` 默认安装到 `~/.local/bin`，可用 `make install BIN=/your/bin` 更改。仅编译可执行 `make build`，运行单元测试可执行 `make test`。

## 快速开始

仅在你拥有授权的站点上使用。先创建配置，将示例域名和校验接口替换为实际值；会话校验接口应只读，并能识别当前用户。

```bash
# 在 zsh 中交互输入强随机口令，避免把口令写入命令历史
read -rs 'BROWSERAUTH_COOKIE_KEY?Cookie 加密口令: '
printf '\n'
export BROWSERAUTH_COOKIE_KEY

browserauth sites new myapp \
  --base-url https://app.example.com \
  --login-url /login \
  --auth-path /api/me \
  --username-path username

browserauth myapp login --isolated-profile
browserauth myapp auth
browserauth myapp request GET /api/me
```

在打开的浏览器中自行完成登录。后续会话过期时重新执行 `login`。加密口令应从密码管理器或受控的环境注入，后续解密需使用相同口令。

也可以安装已审核的配置：

```bash
browserauth sites add --from-file ./myapp.yaml myapp
browserauth sites list
browserauth sites show myapp
```

对于需要浏览器环境的站点，将 YAML 中的 `auth.transport` 配置为 `browser`。此模式支持保留会话与页面流量观察：

```bash
browserauth myapp session start --observe
browserauth myapp page reload
browserauth myapp xhr list --match '/api/' --limit 10
browserauth myapp session stop
```

## 数据与凭据

默认数据目录为 `~/.browserauth/`，可通过 `BROWSERAUTH_DATA_DIR` 覆盖。

| 路径 | 内容 |
| --- | --- |
| `sites/<id>.yaml` | 站点配置 |
| `cookies/<id>` | 登录或 `auth set` 保存的加密 Cookie |
| `profile/` | 默认共享的持久化浏览器配置 |
| `profiles/<id>/` | 使用 `--isolated-profile` 时的站点浏览器配置 |
| `recordings/<id>-<时间戳>.har` | 网络流量录制，可能含明文凭据与业务数据 |
| `run/` | 保留会话的运行数据和日志 |

常用环境变量：

| 变量 | 用途 |
| --- | --- |
| `BROWSERAUTH_COOKIE_KEY` | 保存和解密 Cookie 的口令 |
| `BROWSERAUTH_DATA_DIR` | 数据根目录 |
| `BROWSER_CHROME_PATH` | 浏览器可执行文件路径 |
| `BROWSERAUTH_SITES_DIR` | 额外站点配置目录 |
| `BROWSERAUTH_ISOLATED_PROFILE=1` | 默认按站点隔离浏览器配置 |

Cookie 文件的加密不覆盖浏览器配置、HAR、请求输出或业务响应。`cookie` 命令会输出明文 Cookie；HAR 可能包含 Cookie、Authorization、请求体和响应体。XHR 观察器不收集认证请求头，但 URL、请求体和响应体仍可能含敏感信息。

不要将凭据、浏览器配置或未脱敏的录制与输出提交到仓库、公开 Issue 或提供给无权访问的服务。共享机器上使用独立的操作系统账号并保护数据目录；站点隔离不能替代账号权限隔离。结束使用后关闭保留的会话。凭据泄露时应在目标网站撤销会话，而不只是删除本地文件。

## 使用授权与 AI 代理

软件许可证授予的是使用、修改和分发本项目的权利，不授予访问任何第三方网站、账号或数据的权限。使用者须遵守适用法律、服务条款和组织授权范围。

把本工具交给 AI 代理时，应明确允许访问的站点、账号、数据及操作范围。登录成功或能够读取 Cookie，不代表获得了执行所有业务操作的授权。对删除、提交、付款、发布和权限变更等操作，应先取得针对具体操作的授权。

本工具不能保证任意网站接口始终可用，也不保证所有接口只凭 Cookie 即可调用。网站可能要求额外的签名、CSRF 校验或交互验证。

## 授权许可

除另有标注的第三方材料外，本仓库的项目代码、技能说明、文档和站点配置示例采用 **GNU Affero General Public License, version 3 only**，标识为 **`AGPL-3.0-only`**。完整条款见 [LICENSE](LICENSE)，第三方依赖仍适用各自的许可证。

- 允许个人、组织和企业使用，允许收费、修改及二次开发；遵守 AGPL 的这些行为无需另行申请作者授权。
- 分发本项目或受 AGPL 覆盖的衍生作品时，须按许可证保留相关声明、提供许可证，并提供对应源码或许可证允许的源码获取方式；修改版本还须标明修改情况。
- 如果修改本程序，并让用户通过计算机网络与修改版本交互，须按第 13 条向这些用户显著提供免费获取该版本对应源码的机会。
- 个人或组织内部的私下修改，不会仅因发生修改或属于商业用途，就自动产生向全社会公开源码的义务；通过网络与修改版本交互的用户仍受第 13 条保护。
- 对应源码及覆盖范围以许可证定义为准；调用本工具不自动意味着调用方的全部独立代码都必须采用 AGPL。业务数据、Cookie 和密钥不应当作为源码一并泄露。

以上是便于理解的摘要，不替代许可证正文。本 README 和[免责声明](DISCLAIMER.md)不对 AGPL 授予的权利附加非商业或禁止修改限制。

## 免责声明与反馈

本项目按现状提供，不保证适销性、特定用途适用性或不侵权；责任限制以适用法律允许的范围为限。详见 [DISCLAIMER.md](DISCLAIMER.md) 及 LICENSE 第 15–17 条。

一般问题可提交到[项目 Issues](https://github.com/sichang824/browserauth/issues)。反馈请包含版本、复现步骤和脱敏后的错误信息，不要附带真实凭据或未经脱敏的 HAR。

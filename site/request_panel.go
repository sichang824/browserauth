package site

import (
	"context"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

const requestPanelScript = `(() => {
  if (window.__browserauthPanel && window.__browserauthPanel.version === 2) return;

  const storageKey = "__browserauth_request_log_v1";
  const uiStorageKey = "__browserauth_request_panel_ui_v1";
  const maxEntries = 100;
  const state = { entries: [], selected: null, fullscreen: false, collapsed: false, position: null };
  try {
    const saved = JSON.parse(sessionStorage.getItem(storageKey) || "[]");
    if (Array.isArray(saved)) state.entries = saved.slice(-maxEntries);
  } catch (_) {}
  try {
    const savedUI = JSON.parse(sessionStorage.getItem(uiStorageKey) || "null");
    if (savedUI && Number.isFinite(savedUI.left) && Number.isFinite(savedUI.top)) state.position = savedUI;
  } catch (_) {}

  let host = null;
  let shadow = null;

  const escapeHTML = (value) => String(value == null ? "" : value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");

  const pretty = (value) => {
    if (value == null || value === "") return "（空）";
    const text = String(value);
    try { return JSON.stringify(JSON.parse(text), null, 2); } catch (_) { return text; }
  };

  const pathOf = (url) => {
    try {
      const parsed = new URL(url, location.href);
      return parsed.pathname + parsed.search;
    } catch (_) { return String(url); }
  };

  const save = () => {
    try { sessionStorage.setItem(storageKey, JSON.stringify(state.entries)); } catch (_) {}
  };

  const saveUI = () => {
    try { sessionStorage.setItem(uiStorageKey, JSON.stringify(state.position)); } catch (_) {}
  };

  const applyPosition = () => {
    if (!shadow) return;
    const shell = shadow.querySelector(".shell");
    if (!shell) return;
    if (state.fullscreen || !state.position) {
      shell.style.removeProperty("left");
      shell.style.removeProperty("top");
      shell.style.removeProperty("transform");
      return;
    }
    const rect = shell.getBoundingClientRect();
    state.position.left = Math.max(0, Math.min(state.position.left, Math.max(0, innerWidth - rect.width)));
    state.position.top = Math.max(0, Math.min(state.position.top, Math.max(0, innerHeight - Math.min(rect.height, 42))));
    shell.style.left = state.position.left + "px";
    shell.style.top = state.position.top + "px";
    shell.style.transform = "none";
  };

  const bindDrag = () => {
    const header = shadow && shadow.querySelector("header");
    const shell = shadow && shadow.querySelector(".shell");
    if (!header || !shell) return;
    header.addEventListener("pointerdown", (event) => {
      if (state.fullscreen || event.button !== 0 || event.target.closest("button")) return;
      event.preventDefault();
      const rect = shell.getBoundingClientRect();
      const origin = { pointerX: event.clientX, pointerY: event.clientY, left: rect.left, top: rect.top };
      header.classList.add("dragging");
      header.setPointerCapture(event.pointerId);
      const move = (moveEvent) => {
        const left = origin.left + moveEvent.clientX - origin.pointerX;
        const top = origin.top + moveEvent.clientY - origin.pointerY;
        state.position = {
          left: Math.max(0, Math.min(left, Math.max(0, innerWidth - rect.width))),
          top: Math.max(0, Math.min(top, Math.max(0, innerHeight - 42)))
        };
        applyPosition();
      };
      const finish = () => {
        header.classList.remove("dragging");
        header.removeEventListener("pointermove", move);
        header.removeEventListener("pointerup", finish);
        header.removeEventListener("pointercancel", finish);
        saveUI();
      };
      header.addEventListener("pointermove", move);
      header.addEventListener("pointerup", finish);
      header.addEventListener("pointercancel", finish);
    });
    header.addEventListener("dblclick", (event) => {
      if (state.fullscreen || event.target.closest("button")) return;
      state.position = null;
      saveUI();
      applyPosition();
    });
  };

  const statusInfo = (entry) => {
    if (entry.state === "running") return { label: "请求中", cls: "running" };
    if (entry.error || entry.status === 0 || entry.status >= 400) return { label: entry.status ? "HTTP " + entry.status : "失败", cls: "failed" };
    return { label: "HTTP " + entry.status, cls: "success" };
  };

  const render = () => {
    if (!shadow) return;
    const entries = [...state.entries].reverse();
    const active = state.entries.find((entry) => entry.id === state.selected) || state.entries[state.entries.length - 1];
    const running = state.entries.filter((entry) => entry.state === "running").length;
    const failed = state.entries.filter((entry) => entry.state === "done" && (entry.error || entry.status === 0 || entry.status >= 400)).length;
    host.classList.toggle("fullscreen", state.fullscreen);
    host.classList.toggle("collapsed", state.collapsed);

    const rows = entries.length ? entries.map((entry) => {
      const info = statusInfo(entry);
      const duration = entry.durationMs == null ? "…" : entry.durationMs + " ms";
      return '<button class="row ' + (active && active.id === entry.id ? "selected" : "") + '" data-id="' + escapeHTML(entry.id) + '">' +
        '<span class="method">' + escapeHTML(entry.method) + '</span>' +
        '<span class="path" title="' + escapeHTML(pathOf(entry.url)) + '">' + escapeHTML(pathOf(entry.url)) + '</span>' +
        '<span class="duration">' + escapeHTML(duration) + '</span>' +
        '<span class="status ' + info.cls + '">' + escapeHTML(info.label) + '</span>' +
      '</button>';
    }).join("") : '<div class="empty">暂无 browserauth 业务请求</div>';

    const details = active ? (() => {
      const info = statusInfo(active);
      return '<section class="details">' +
        '<div class="detail-head"><strong>' + escapeHTML(active.method + " " + pathOf(active.url)) + '</strong><span class="status ' + info.cls + '">' + escapeHTML(info.label) + '</span></div>' +
        '<div class="meta">开始：' + escapeHTML(new Date(active.startedAt).toLocaleString()) + '　耗时：' + escapeHTML(active.durationMs == null ? "进行中" : active.durationMs + " ms") + '</div>' +
        '<div class="payloads"><div><h3>请求体</h3><pre>' + escapeHTML(pretty(active.requestBody)) + '</pre></div>' +
        '<div><h3>响应体</h3><pre>' + escapeHTML(pretty(active.responseBody || active.error)) + '</pre></div></div>' +
      '</section>';
    })() : "";

    shadow.querySelector(".app").innerHTML =
      '<header title="拖动面板；双击恢复顶部居中"><div class="brand"><span class="drag-grip">⠿</span><span class="pulse ' + (running ? "busy" : failed ? "bad" : "ok") + '"></span><strong>browserauth</strong><span class="summary">' +
      state.entries.length + ' 条 · ' + running + ' 进行中 · ' + failed + ' 失败</span></div>' +
      '<div class="actions"><button data-action="collapse">' + (state.collapsed ? "展开" : "收起") + '</button><button data-action="fullscreen">' + (state.fullscreen ? "退出全屏" : "全屏") + '</button></div></header>' +
      (state.collapsed ? "" : '<main><div class="list">' + rows + '</div>' + details + '</main>');

    shadow.querySelectorAll("[data-id]").forEach((button) => button.addEventListener("click", () => {
      state.selected = button.dataset.id;
      render();
    }));
    shadow.querySelector('[data-action="collapse"]').addEventListener("click", () => {
      state.collapsed = !state.collapsed;
      if (state.collapsed) state.fullscreen = false;
      render();
    });
    shadow.querySelector('[data-action="fullscreen"]').addEventListener("click", () => {
      state.fullscreen = !state.fullscreen;
      state.collapsed = false;
      render();
    });
    applyPosition();
    bindDrag();
  };

  const mount = () => {
    if (!document.documentElement) {
      addEventListener("DOMContentLoaded", mount, { once: true });
      return;
    }
    host = document.getElementById("__browserauth-request-panel");
    if (!host) {
      host = document.createElement("div");
      host.id = "__browserauth-request-panel";
      document.documentElement.appendChild(host);
    }
    shadow = host.shadowRoot || host.attachShadow({ mode: "open" });
    shadow.innerHTML = '<style>' +
      ':host{all:initial}*{box-sizing:border-box}button{font:inherit}' +
      '.shell{position:fixed;top:10px;left:50%;transform:translateX(-50%);z-index:2147483647;width:min(860px,calc(100vw - 24px));max-height:min(520px,calc(100vh - 20px));overflow:hidden;color:#e8eef8;background:rgba(14,20,31,.96);border:1px solid rgba(148,163,184,.28);border-radius:12px;box-shadow:0 18px 55px rgba(0,0,0,.38);font:13px/1.45 ui-monospace,SFMono-Regular,Menlo,monospace;backdrop-filter:blur(14px)}' +
      ':host(.fullscreen) .shell{top:0;left:0;transform:none;width:100vw;height:100vh;max-height:none;border:0;border-radius:0}:host(.collapsed) .shell{width:auto;min-width:360px}' +
      'header{height:42px;padding:0 12px;display:flex;align-items:center;justify-content:space-between;border-bottom:1px solid rgba(148,163,184,.2);cursor:grab;user-select:none;touch-action:none}header.dragging{cursor:grabbing}.brand,.actions{display:flex;align-items:center;gap:8px}.drag-grip{color:#64748b;font-size:16px}.summary{color:#9aa9bd;font-size:12px}' +
      '.pulse{width:8px;height:8px;border-radius:50%;background:#22c55e}.pulse.busy{background:#f59e0b;box-shadow:0 0 0 4px rgba(245,158,11,.15)}.pulse.bad{background:#ef4444}' +
      '.actions button{color:#cbd5e1;background:#1e293b;border:1px solid #334155;border-radius:6px;padding:4px 9px;cursor:pointer}.actions button:hover{background:#334155}' +
      'main{display:grid;grid-template-columns:minmax(280px,42%) 1fr;height:460px;max-height:calc(100vh - 62px)}:host(.fullscreen) main{height:calc(100vh - 42px);max-height:none}' +
      '.list{overflow:auto;border-right:1px solid rgba(148,163,184,.2);padding:6px}.row{width:100%;display:grid;grid-template-columns:58px minmax(0,1fr) 72px 78px;gap:7px;align-items:center;color:#dbe4f0;background:transparent;border:1px solid transparent;border-radius:7px;padding:8px;text-align:left;cursor:pointer}.row:hover{background:#182235}.row.selected{background:#1d2a40;border-color:#3b82f6}' +
      '.method{font-weight:700;color:#93c5fd}.path{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.duration{color:#94a3b8;text-align:right}.status{justify-self:end;border-radius:999px;padding:2px 7px;font-size:11px;white-space:nowrap}.status.running{color:#fbbf24;background:#422006}.status.success{color:#86efac;background:#052e16}.status.failed{color:#fca5a5;background:#450a0a}' +
      '.details{min-width:0;overflow:auto;padding:14px}.detail-head{display:flex;justify-content:space-between;gap:12px;align-items:flex-start}.detail-head strong{word-break:break-all}.meta{margin-top:6px;color:#94a3b8;font-size:12px}.payloads{display:grid;grid-template-columns:1fr;gap:12px;margin-top:14px}.payloads h3{font-size:12px;color:#93a4ba;margin:0 0 5px}.payloads pre{margin:0;padding:11px;min-height:80px;max-height:260px;overflow:auto;color:#dce6f4;background:#090f1a;border:1px solid #263248;border-radius:8px;white-space:pre-wrap;word-break:break-word}:host(.fullscreen) .payloads{grid-template-columns:1fr 1fr}:host(.fullscreen) .payloads pre{max-height:calc(100vh - 180px)}' +
      '.empty{padding:30px 12px;text-align:center;color:#64748b}' +
      '</style><div class="shell"><div class="app"></div></div>';
    render();
    addEventListener("resize", applyPosition);
  };

  window.__browserauthPanel = {
    version: 2,
    start(meta) {
      const id = Date.now().toString(36) + Math.random().toString(36).slice(2, 8);
      state.entries.push({ id, method: meta.method, url: meta.url, requestBody: meta.body || "", responseBody: "", status: null, error: "", state: "running", startedAt: Date.now(), durationMs: null });
      if (state.entries.length > maxEntries) state.entries.splice(0, state.entries.length - maxEntries);
      state.selected = id;
      save();
      render();
      return id;
    },
    finish(id, result) {
      const entry = state.entries.find((item) => item.id === id);
      if (!entry) return;
      entry.status = Number(result.status || 0);
      entry.responseBody = result.body || "";
      entry.error = result.error || "";
      entry.state = "done";
      entry.durationMs = Date.now() - entry.startedAt;
      save();
      render();
    }
  };
  mount();
})();`

func injectRequestPanel(ctx context.Context) error {
	var ignored any
	return chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(requestPanelScript).Do(ctx)
			return err
		}),
		chromedp.Evaluate(requestPanelScript, &ignored),
	)
}

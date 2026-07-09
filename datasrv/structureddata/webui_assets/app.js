// MaClawDataSrv MIS Admin Console - Core App Logic
"use strict";

const App = {
  endpoint: localStorage.getItem("mis_endpoint") || location.origin,
  token: localStorage.getItem("mis_token") || "",
  tenant: localStorage.getItem("mis_tenant") || "default",
  currentPage: "overview",
  _navGen: 0, // navigation generation — increments on each page switch

  // === Initialization ===
  init() {
    this.bindLogin();
    this.bindNav();
    this.bindLangSwitch();
    if (this.token) {
      this.showApp();
      this.checkConnection();
    } else {
      this.showLogin();
    }
  },

  // === Auth ===
  showLogin() {
    document.getElementById("loginScreen").classList.remove("hidden");
    document.getElementById("appShell").classList.add("hidden");
  },

  showApp() {
    document.getElementById("loginScreen").classList.add("hidden");
    document.getElementById("appShell").classList.remove("hidden");
    this.navigate(this.currentPage);
  },

  bindLogin() {
    const btn = document.getElementById("loginBtn");
    const status = document.getElementById("loginStatus");
    btn.addEventListener("click", async () => {
      const u = document.getElementById("loginUsername").value.trim();
      const p = document.getElementById("loginPassword").value;
      if (!u || !p) { status.textContent = "请输入用户名和密码"; return; }
      status.textContent = "正在登录...";
      try {
        const res = await this.api("/api/v1/login", { method: "POST", body: JSON.stringify({ username: u, password: p }) });
        if (res.token) {
          this.token = res.token;
          localStorage.setItem("mis_token", res.token);
          this.showApp();
          this.checkConnection();
        } else {
          status.textContent = res.error || "登录失败";
        }
      } catch (e) {
        status.textContent = "连接失败: " + e.message;
      }
    });
    // Enter key
    document.getElementById("loginPassword").addEventListener("keydown", e => { if (e.key === "Enter") btn.click(); });
  },

  logout() {
    this.token = "";
    localStorage.removeItem("mis_token");
    this.showLogin();
  },

  // === Navigation ===
  bindNav() {
    document.getElementById("sidebar").addEventListener("click", e => {
      const btn = e.target.closest("[data-page]");
      if (!btn) return;
      this.navigate(btn.dataset.page);
    });
    document.getElementById("logoutBtn").addEventListener("click", () => this.logout());
  },

  navigate(page) {
    this.currentPage = page;
    this._navGen++;
    // Update nav active state
    document.querySelectorAll(".nav-item").forEach(el => {
      el.classList.toggle("active", el.dataset.page === page);
    });
    // Render page
    const container = document.getElementById("pageContainer");
    const renderer = PageModules[page];
    if (renderer) {
      container.innerHTML = "";
      renderer.render(container);
    } else {
      container.innerHTML = `<div class="empty-state">模块 "${page}" 开发中...</div>`;
    }
  },

  /** Returns a guard function: call it to check if the page is still active. */
  navGuard() {
    const gen = this._navGen;
    return () => this._navGen === gen;
  },

  // === Language ===
  bindLangSwitch() {
    document.querySelector(".lang-switch").addEventListener("click", e => {
      const btn = e.target.closest("[data-lang]");
      if (!btn) return;
      I18N.lang = btn.dataset.lang;
      document.querySelectorAll(".lang-switch button").forEach(b => b.classList.toggle("active", b.dataset.lang === I18N.lang));
      document.documentElement.lang = I18N.lang === "zh" ? "zh-CN" : "en";
      // Re-render current page
      this.navigate(this.currentPage);
    });
  },

  // === API ===
  async api(path, options = {}) {
    const url = this.endpoint.replace(/\/$/, "") + path;
    const headers = { ...(options.headers || {}) };
    if (options.body) headers["Content-Type"] = "application/json";
    if (this.token) headers["Authorization"] = "Bearer " + this.token;
    if (this.tenant) headers["X-Tenant"] = this.tenant;
    const resp = await fetch(url, { ...options, headers });
    if (resp.status === 401) { this.logout(); throw new Error("会话过期，请重新登录"); }
    const ct = resp.headers.get("content-type") || "";
    if (!ct.includes("application/json")) {
      if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
      return { ok: true, text: await resp.text() };
    }
    const data = await resp.json().catch(() => ({}));
    if (!resp.ok) throw new Error(data.error || `HTTP ${resp.status}`);
    return data;
  },

  async checkConnection() {
    try {
      await this.api("/health");
      const el = document.getElementById("serviceStatus");
      el.textContent = "● 服务在线";
      el.style.color = "var(--ok)";
    } catch (e) {
      const el = document.getElementById("serviceStatus");
      el.textContent = "● 连接失败";
      el.style.color = "var(--danger)";
    }
  },

  // === Toast ===
  toast(msg, type = "ok") {
    const stack = document.getElementById("toastStack");
    const el = document.createElement("div");
    el.className = "toast " + type;
    el.textContent = msg;
    stack.appendChild(el);
    setTimeout(() => el.remove(), 4000);
  },

  // === Helpers ===
  $(id) { return document.getElementById(id); },

  html(tag, attrs = {}, ...children) {
    const el = document.createElement(tag);
    for (const [k, v] of Object.entries(attrs)) {
      if (k === "class") el.className = v;
      else if (k === "style" && typeof v === "object") Object.assign(el.style, v);
      else if (k.startsWith("on") && typeof v === "function") el.addEventListener(k.slice(2), v);
      else if (k === "checked" || k === "disabled" || k === "readonly") { if (v) el[k] = true; }
      else el.setAttribute(k, v);
    }
    for (const child of children) {
      if (typeof child === "string") el.appendChild(document.createTextNode(child));
      else if (child) el.appendChild(child);
    }
    return el;
  },

  collapsible(title, defaultOpen, contentFn) {
    const h = this.html;
    const section = h("div", { class: "collapsible" });
    let rendered = false;
    const body = h("div", { class: "collapsible-body" + (defaultOpen ? " open" : "") });

    const ensureContent = () => {
      if (!rendered) {
        rendered = true;
        body.appendChild(contentFn());
      }
    };

    const trigger = h("button", {
      class: "collapsible-trigger",
      "aria-expanded": String(defaultOpen),
      onclick: () => {
        ensureContent();
        const isOpen = body.classList.toggle("open");
        trigger.setAttribute("aria-expanded", String(isOpen));
      }
    }, h("span", {}, title), h("span", { class: "chevron" }, "▼"));

    // Render immediately only if default open
    if (defaultOpen) ensureContent();

    section.appendChild(trigger);
    section.appendChild(body);
    return section;
  },

  field(id, label, type = "text", placeholder = "") {
    const h = this.html;
    return h("div", { class: "form-field" },
      h("label", { for: id }, label),
      h("input", { id, type, placeholder })
    );
  },

  selectField(id, label, options) {
    const h = this.html;
    const select = h("select", { id });
    options.forEach(([val, text]) => {
      select.appendChild(h("option", { value: val }, text));
    });
    return h("div", { class: "form-field" },
      h("label", { for: id }, label),
      select
    );
  },

  /** Build a safe DOM table from headers and row data. No innerHTML. */
  table(headers, rows, opts = {}) {
    const h = this.html;
    const tbl = document.createElement("table");
    const thead = h("thead", {}, h("tr", {}, ...headers.map(hdr => h("th", {}, hdr))));
    tbl.appendChild(thead);
    const tbody = document.createElement("tbody");
    rows.forEach(cells => {
      const tr = document.createElement("tr");
      if (opts.onRowClick && cells._id) {
        tr.style.cursor = "pointer";
        tr.addEventListener("click", () => opts.onRowClick(cells._id));
      }
      (cells._cells || cells).forEach(cell => {
        if (cell instanceof HTMLElement) {
          const td = document.createElement("td");
          td.appendChild(cell);
          tr.appendChild(td);
        } else {
          const td = h("td", cell?.attrs || {}, String(cell?.text ?? cell ?? "-"));
          tr.appendChild(td);
        }
      });
      tbody.appendChild(tr);
    });
    tbl.appendChild(tbody);
    const wrap = h("div", { class: "table-wrap" });
    wrap.appendChild(tbl);
    return wrap;
  }
};

// Page module registry
const PageModules = {};

// Boot
document.addEventListener("DOMContentLoaded", () => App.init());

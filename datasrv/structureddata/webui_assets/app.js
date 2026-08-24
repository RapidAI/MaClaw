// MaClawDataSrv MIS Admin Console - Core App Logic
"use strict";

const App = {
  endpoint: localStorage.getItem("mis_endpoint") || location.origin,
  token: localStorage.getItem("mis_token") || "",
  tenant: localStorage.getItem("mis_tenant") || "default",
  datasetId: localStorage.getItem("mis_dataset") || "",
  currentPage: "overview",
  _navGen: 0,
  _sidebarOpen: false,
  _cmdOpen: false,
  _cmdIndex: 0,
  _cmdItems: [],
  _setupInitialized: null,
  _caps: null,
  _datasets: null,

  PAGE_META: {
    overview: { title: "用户视图", group: "工作" },
    inbox: { title: "待办", group: "工作" },
    records: { title: "记录", group: "工作" },
    actions: { title: "业务动作", group: "工作" },
    domains: { title: "业务应用", group: "应用" },
    quickstart: { title: "开通应用", group: "应用" },
    views: { title: "视图", group: "分析" },
    dashboards: { title: "仪表盘", group: "分析" },
    reports: { title: "报表", group: "分析" },
    datasets: { title: "数据集", group: "建模" },
    fields: { title: "字段", group: "建模" },
    relationships: { title: "关联", group: "建模" },
    connectors: { title: "连接器", group: "集成" },
    apikeys: { title: "API 密钥", group: "治理" },
    admins: { title: "管理员", group: "治理" },
    quality: { title: "质量检查", group: "治理" },
    backups: { title: "备份", group: "治理" },
    audit: { title: "审计", group: "治理" },
    ops: { title: "运维", group: "治理" },
  },

  // === Initialization ===
  init() {
    this.bindLogin();
    this.bindInitAdmin();
    this.bindNav();
    this.bindLangSwitch();
    this.bindMobileNav();
    this.bindCommandPalette();
    this.bindHashRoute();
    this.bindDatasetPicker();
    this.syncTenantChip();
    this.syncLangButtons();
    this.applyHashRoute();

    // Prefill tenant input
    const tenantInput = document.getElementById("loginTenant");
    if (tenantInput) tenantInput.value = this.tenant || "default";

    this.refreshSetupStatus().finally(() => {
      if (this.token) {
        this.showApp();
        this.checkConnection();
      } else {
        this.showLogin();
      }
    });
  },

  // === Setup / Auth ===
  async refreshSetupStatus() {
    const statusEl = document.getElementById("setupStatusText");
    const policyEl = document.getElementById("adminPasswordPolicy");
    try {
      const res = await fetch(this.endpoint.replace(/\/$/, "") + "/api/v1/setup/status");
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`);

      this._setupInitialized = !!data.initialized;
      if (statusEl) {
        statusEl.textContent = data.initialized
          ? (data.mode === "hub_tenant_admin" ? "已初始化 · Hub 模式" : "已初始化 · 本地管理员")
          : "待初始化";
        statusEl.className = "chip" + (data.initialized ? " brand" : " warn");
      }
      if (policyEl && data.password_policy) {
        const p = data.password_policy;
        const parts = [`最少 ${p.min_length || 8} 位`];
        if (p.lockout_enabled) parts.push(`失败 ${p.login_max_failures || 5} 次锁定`);
        if (p.offline_reset_available) parts.push("支持离线重置");
        policyEl.textContent = "密码策略：" + parts.join(" · ");
      } else if (policyEl) {
        policyEl.textContent = "密码策略：最少 8 位";
      }

      // Tenant options
      const list = document.getElementById("tenantOptions");
      if (list && Array.isArray(data.tenants)) {
        list.innerHTML = "";
        data.tenants.forEach(t => {
          const opt = document.createElement("option");
          opt.value = t.id || t.slug || "";
          opt.label = t.name || opt.value;
          if (opt.value) list.appendChild(opt);
        });
      }

      this.applySetupMode(this._setupInitialized);
      return data;
    } catch (e) {
      if (statusEl) {
        statusEl.textContent = "无法连接服务";
        statusEl.className = "chip danger";
      }
      if (policyEl) policyEl.textContent = "密码策略不可用";
      this.applySetupMode(true); // show login form so user can still try
      return null;
    }
  },

  applySetupMode(initialized) {
    const initBox = document.getElementById("adminInitBox");
    const loginBox = document.getElementById("adminLoginBox");
    const title = document.getElementById("authTitle");
    const sub = document.getElementById("authSubtitle");
    if (!initBox || !loginBox) return;
    if (!initialized) {
      initBox.classList.remove("hidden");
      loginBox.classList.add("hidden");
      if (title) title.textContent = "首次初始化";
      if (sub) sub.textContent = "创建全局管理员，完成控制台启用";
    } else {
      initBox.classList.add("hidden");
      loginBox.classList.remove("hidden");
      if (title) title.textContent = "管理员登录";
      if (sub) sub.textContent = "企业结构化数据管理控制台";
    }
  },

  bindInitAdmin() {
    const btn = document.getElementById("initializeAdmin");
    if (!btn) return;
    btn.addEventListener("click", async () => {
      const status = document.getElementById("loginStatus");
      const setStatus = (msg, isError) => {
        if (!status) return;
        status.textContent = msg || "";
        status.classList.toggle("is-error", !!isError);
      };
      const username = (document.getElementById("initUsername")?.value || "").trim();
      const displayName = (document.getElementById("initDisplayName")?.value || "").trim();
      const password = document.getElementById("initPassword")?.value || "";
      if (!username || !password) { setStatus("请填写用户名和密码", true); return; }
      btn.disabled = true;
      setStatus("正在初始化…");
      try {
        const res = await this.publicApi("/api/v1/setup/admin", {
          method: "POST",
          body: JSON.stringify({
            username,
            password,
            display_name: displayName || username,
            tenant_id: "default",
          }),
        });
        if (res.token) {
          this.token = res.token;
          localStorage.setItem("mis_token", res.token);
          const nameEl = document.getElementById("adminName");
          if (nameEl) nameEl.textContent = res.display_name || res.username || username;
          setStatus("");
          this._setupInitialized = true;
          this.showApp();
          this.checkConnection();
          this.toast("管理员已初始化");
        } else {
          setStatus(res.error || "初始化失败", true);
        }
      } catch (e) {
        setStatus(e.message || "初始化失败", true);
      } finally {
        btn.disabled = false;
      }
    });
  },

  showLogin() {
    document.getElementById("loginScreen").classList.remove("hidden");
    document.getElementById("appShell").classList.add("hidden");
    this.closeSidebar();
    this.closeCommandPalette();
  },

  showApp() {
    document.getElementById("loginScreen").classList.add("hidden");
    document.getElementById("appShell").classList.remove("hidden");
    this.refreshDatasetPicker();
    this.navigate(this.currentPage, this.hashState().params);
  },

  bindLogin() {
    const btn = document.getElementById("loginBtn");
    const status = document.getElementById("loginStatus");
    if (!btn) return;
    const setStatus = (msg, isError) => {
      status.textContent = msg || "";
      status.classList.toggle("is-error", !!isError);
    };
    btn.addEventListener("click", async () => {
      const u = document.getElementById("loginUsername").value.trim();
      const p = document.getElementById("loginPassword").value;
      const tenant = (document.getElementById("loginTenant")?.value || "default").trim() || "default";
      if (!u || !p) { setStatus("请输入用户名和密码", true); return; }
      btn.disabled = true;
      setStatus("正在登录…");
      try {
        this.tenant = tenant;
        localStorage.setItem("mis_tenant", tenant);
        this.syncTenantChip();
        const res = await this.publicApi("/api/v1/login", {
          method: "POST",
          body: JSON.stringify({ username: u, password: p, tenant_id: tenant }),
        });
        if (res.token) {
          this.token = res.token;
          localStorage.setItem("mis_token", res.token);
          const nameEl = document.getElementById("adminName");
          if (nameEl) nameEl.textContent = res.display_name || res.username || u;
          setStatus("");
          this.showApp();
          this.checkConnection();
        } else {
          setStatus(res.error || "登录失败", true);
        }
      } catch (e) {
        setStatus("连接失败: " + e.message, true);
      } finally {
        btn.disabled = false;
      }
    });
    document.getElementById("loginPassword")?.addEventListener("keydown", e => {
      if (e.key === "Enter") btn.click();
    });
    document.getElementById("loginUsername")?.addEventListener("keydown", e => {
      if (e.key === "Enter") document.getElementById("loginPassword")?.focus();
    });
  },

  logout() {
    this.token = "";
    localStorage.removeItem("mis_token");
    this.showLogin();
    this.refreshSetupStatus();
  },

  // === Navigation ===
  bindNav() {
    document.getElementById("sidebar")?.addEventListener("click", e => {
      const btn = e.target.closest("[data-page]");
      if (!btn) return;
      this.navigate(btn.dataset.page);
      this.closeSidebar();
    });
    document.getElementById("logoutBtn")?.addEventListener("click", () => this.logout());
  },

  bindMobileNav() {
    const toggle = document.getElementById("menuToggle");
    const overlay = document.getElementById("sidebarOverlay");
    if (toggle) {
      toggle.addEventListener("click", () => {
        if (this._sidebarOpen) this.closeSidebar();
        else this.openSidebar();
      });
    }
    if (overlay) overlay.addEventListener("click", () => this.closeSidebar());
    document.addEventListener("keydown", e => {
      if (e.key === "Escape") {
        if (this._cmdOpen) { this.closeCommandPalette(); return; }
        if (this._sidebarOpen) this.closeSidebar();
      }
    });
    window.addEventListener("resize", () => {
      if (window.innerWidth > 900 && this._sidebarOpen) this.closeSidebar();
    });
  },

  openSidebar() {
    const sidebar = document.getElementById("sidebar");
    const overlay = document.getElementById("sidebarOverlay");
    const toggle = document.getElementById("menuToggle");
    if (!sidebar) return;
    sidebar.classList.add("open");
    if (overlay) overlay.classList.add("open");
    if (toggle) toggle.setAttribute("aria-expanded", "true");
    this._sidebarOpen = true;
  },

  closeSidebar() {
    const sidebar = document.getElementById("sidebar");
    const overlay = document.getElementById("sidebarOverlay");
    const toggle = document.getElementById("menuToggle");
    if (sidebar) sidebar.classList.remove("open");
    if (overlay) overlay.classList.remove("open");
    if (toggle) toggle.setAttribute("aria-expanded", "false");
    this._sidebarOpen = false;
  },

  navigate(page, params) {
    if (params && params.dataset) this.setDataset(params.dataset);
    this.currentPage = page;
    this._navGen++;
    document.querySelectorAll(".nav-item[data-page]").forEach(el => {
      el.classList.toggle("active", el.dataset.page === page);
    });
    const meta = this.PAGE_META[page];
    const ctx = document.getElementById("pageContext");
    if (ctx) ctx.textContent = meta ? (meta.group + " / " + meta.title) : page;
    document.title = (meta ? meta.title + " · " : "") + "MaClawDataSrv MIS";
    this.writeHash(page, Object.assign({}, params || {}, this.datasetId ? { dataset: this.datasetId } : {}));

    const container = document.getElementById("pageContainer");
    const renderer = PageModules[page];
    if (renderer) {
      container.innerHTML = "";
      renderer.render(container);
    } else {
      container.innerHTML = "";
      container.appendChild(this.comingSoon(page));
    }
    try { container.focus({ preventScroll: true }); } catch (_) { /* ignore */ }
  },

  bindHashRoute() {
    window.addEventListener("hashchange", () => {
      if (document.getElementById("appShell")?.classList.contains("hidden")) return;
      const state = this.hashState();
      if (!state.page) {
        if (state.params.dataset && state.params.dataset !== this.datasetId) {
          this.setDataset(state.params.dataset);
        }
        return;
      }
      if (this.PAGE_META[state.page]) this.navigate(state.page, state.params);
    });
  },

  applyHashRoute() {
    const state = this.hashState();
    if (state.params.dataset) this.setDataset(state.params.dataset);
    if (state.page && this.PAGE_META[state.page]) this.currentPage = state.page;
  },

  hashState() {
    const raw = (location.hash || "").replace(/^#/, "");
    const qIndex = raw.indexOf("?");
    const page = (qIndex >= 0 ? raw.slice(0, qIndex) : raw).trim();
    const query = qIndex >= 0 ? raw.slice(qIndex + 1) : "";
    const params = {};
    if (query) new URLSearchParams(query).forEach((v, k) => { params[k] = v; });
    return { page, params };
  },

  writeHash(page, params) {
    const qs = this.qs(params);
    const next = "#" + page + qs;
    if (location.hash !== next) history.replaceState(null, "", next);
  },

  navGuard() {
    const gen = this._navGen;
    return () => this._navGen === gen;
  },

  // === Command palette ===
  bindCommandPalette() {
    const open = () => this.openCommandPalette();
    document.getElementById("cmdOpenBtn")?.addEventListener("click", open);
    document.getElementById("cmdTopBtn")?.addEventListener("click", open);
    document.getElementById("cmdOverlay")?.addEventListener("click", e => {
      if (e.target.id === "cmdOverlay") this.closeCommandPalette();
    });
    const input = document.getElementById("cmdInput");
    input?.addEventListener("input", () => this.renderCommandList(input.value));
    input?.addEventListener("keydown", e => this.onCommandKeydown(e));

    document.addEventListener("keydown", e => {
      const tag = (e.target && e.target.tagName) || "";
      const typing = tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || e.target?.isContentEditable;
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        if (document.getElementById("appShell")?.classList.contains("hidden")) return;
        this.openCommandPalette();
        return;
      }
      if (!typing && e.key === "/" && !e.ctrlKey && !e.metaKey && !e.altKey) {
        if (document.getElementById("appShell")?.classList.contains("hidden")) return;
        e.preventDefault();
        this.openCommandPalette();
      }
    });
  },

  openCommandPalette() {
    const overlay = document.getElementById("cmdOverlay");
    if (!overlay) return;
    overlay.classList.remove("hidden");
    this._cmdOpen = true;
    this._cmdIndex = 0;
    this.renderCommandList("");
    const input = document.getElementById("cmdInput");
    if (input) {
      input.value = "";
      setTimeout(() => input.focus(), 0);
    }
  },

  closeCommandPalette() {
    document.getElementById("cmdOverlay")?.classList.add("hidden");
    this._cmdOpen = false;
  },

  renderCommandList(query) {
    const q = (query || "").trim().toLowerCase();
    const items = Object.entries(this.PAGE_META)
      .map(([id, meta]) => ({ id, ...meta, hay: (meta.title + " " + meta.group + " " + id).toLowerCase() }))
      .filter(it => !q || it.hay.includes(q));
    this._cmdItems = items;
    if (this._cmdIndex >= items.length) this._cmdIndex = Math.max(0, items.length - 1);
    const list = document.getElementById("cmdList");
    if (!list) return;
    list.innerHTML = "";
    if (!items.length) {
      list.innerHTML = '<li class="cmd-empty">无匹配模块</li>';
      return;
    }
    items.forEach((it, i) => {
      const li = document.createElement("li");
      li.className = "cmd-item" + (i === this._cmdIndex ? " active" : "");
      li.setAttribute("role", "option");
      li.innerHTML = `<span class="cmd-item-title"></span><span class="cmd-item-group"></span>`;
      li.querySelector(".cmd-item-title").textContent = it.title;
      li.querySelector(".cmd-item-group").textContent = it.group;
      li.addEventListener("click", () => {
        this.closeCommandPalette();
        this.navigate(it.id);
      });
      list.appendChild(li);
    });
  },

  onCommandKeydown(e) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      this._cmdIndex = Math.min(this._cmdIndex + 1, this._cmdItems.length - 1);
      this.renderCommandList(document.getElementById("cmdInput")?.value || "");
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      this._cmdIndex = Math.max(this._cmdIndex - 1, 0);
      this.renderCommandList(document.getElementById("cmdInput")?.value || "");
    } else if (e.key === "Enter") {
      e.preventDefault();
      const it = this._cmdItems[this._cmdIndex];
      if (it) {
        this.closeCommandPalette();
        this.navigate(it.id);
      }
    } else if (e.key === "Escape") {
      e.preventDefault();
      this.closeCommandPalette();
    }
  },

  // === Language ===
  bindLangSwitch() {
    document.querySelectorAll(".lang-switch").forEach(sw => {
      sw.addEventListener("click", e => {
        const btn = e.target.closest("[data-lang]");
        if (!btn) return;
        I18N.lang = btn.dataset.lang;
        this.syncLangButtons();
        document.documentElement.lang = I18N.lang === "zh" ? "zh-CN" : "en";
        if (!document.getElementById("appShell")?.classList.contains("hidden")) {
          this.navigate(this.currentPage);
        }
      });
    });
  },

  syncLangButtons() {
    document.querySelectorAll(".lang-switch button").forEach(b => {
      b.classList.toggle("active", b.dataset.lang === I18N.lang);
    });
  },

  syncTenantChip() {
    const el = document.getElementById("tenantChip");
    if (el) el.textContent = "租户 " + (this.tenant || "default");
  },

  // === API ===
  async publicApi(path, options = {}) {
    const url = this.endpoint.replace(/\/$/, "") + path;
    const headers = { ...(options.headers || {}) };
    if (options.body) headers["Content-Type"] = "application/json";
    const resp = await fetch(url, { ...options, headers });
    const ct = resp.headers.get("content-type") || "";
    if (!ct.includes("application/json")) {
      if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
      return { ok: true, text: await resp.text() };
    }
    const data = await resp.json().catch(() => ({}));
    if (!resp.ok) throw new Error(data.error || `HTTP ${resp.status}`);
    return data;
  },

  async api(path, options = {}) {
    const url = this.endpoint.replace(/\/$/, "") + path;
    const headers = { ...(options.headers || {}) };
    if (options.body) headers["Content-Type"] = "application/json";
    if (this.token) headers["Authorization"] = "Bearer " + this.token;
    if (this.tenant) {
      headers["X-MaClaw-Tenant-ID"] = this.tenant;
      headers["X-Tenant"] = this.tenant;
    }
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
    const el = document.getElementById("serviceStatus");
    if (!el) return;
    try {
      await this.publicApi("/health");
      el.textContent = "服务在线";
      el.className = "status-dot online";
    } catch (e) {
      el.textContent = "连接失败";
      el.className = "status-dot offline";
    }
  },

  // === Toast ===
  toast(msg, type = "ok") {
    const stack = document.getElementById("toastStack");
    const el = document.createElement("div");
    if (type === "danger" || type === "warn") type = "err";
    el.className = "toast " + type;
    el.textContent = msg;
    if (stack) stack.appendChild(el);
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
      else if (v === false || v == null) { /* skip */ }
      else el.setAttribute(k, v);
    }
    for (const child of children) {
      if (typeof child === "string") el.appendChild(document.createTextNode(child));
      else if (child) el.appendChild(child);
    }
    return el;
  },

  emptyState(title, desc, actions) {
    const h = this.html;
    const wrap = h("div", { class: "empty-state" });
    if (title) wrap.appendChild(h("span", { class: "empty-title" }, title));
    if (desc) wrap.appendChild(h("span", { class: "empty-desc" }, desc));
    if (actions && actions.length) {
      const btns = h("div", { class: "btn-group" });
      actions.forEach(a => {
        btns.appendChild(h("button", {
          class: a.primary ? "primary sm" : "sm",
          type: "button",
          onclick: a.onclick
        }, a.label));
      });
      wrap.appendChild(btns);
    }
    return wrap;
  },

  comingSoon(page) {
    const h = this.html;
    return h("div", { class: "coming-soon card" },
      h("h2", {}, "模块准备中"),
      h("p", {}, `「${page}」功能即将上线。请从侧栏选择其他已就绪的模块继续操作。`)
    );
  },

  badge(text, variant) {
    return this.html("span", { class: "badge" + (variant ? " " + variant : "") }, text);
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
      type: "button",
      "aria-expanded": String(defaultOpen),
      onclick: () => {
        ensureContent();
        const isOpen = body.classList.toggle("open");
        trigger.setAttribute("aria-expanded", String(isOpen));
      }
    }, h("span", {}, title), h("span", { class: "chevron", "aria-hidden": "true" }, "▼"));

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

  table(headers, rows, opts = {}) {
    const h = this.html;
    const tbl = document.createElement("table");
    const thead = h("thead", {}, h("tr", {}, ...headers.map(hdr => h("th", {}, hdr))));
    tbl.appendChild(thead);
    const tbody = document.createElement("tbody");
    rows.forEach(cells => {
      const tr = document.createElement("tr");
      if (opts.onRowClick && cells._id) {
        tr.dataset.clickable = "1";
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
  },

  listItems(data, ...keys) {
    if (!data) return [];
    if (Array.isArray(data.items)) return data.items;
    for (const key of keys) {
      if (Array.isArray(data[key])) return data[key];
    }
    if (Array.isArray(data)) return data;
    return [];
  },

  qs(params) {
    const u = new URLSearchParams();
    Object.entries(params || {}).forEach(([k, v]) => {
      if (v === undefined || v === null) return;
      const s = String(v).trim();
      if (s !== "") u.set(k, s);
    });
    const out = u.toString();
    return out ? "?" + out : "";
  },

  setDataset(id) {
    this.datasetId = String(id || "").trim();
    if (this.datasetId) localStorage.setItem("mis_dataset", this.datasetId);
    else localStorage.removeItem("mis_dataset");
    this.syncDatasetPicker();
  },

  async loadDatasets(force) {
    if (this._datasets && !force) return this._datasets;
    const data = await this.api("/api/v1/data/datasets?limit=200");
    this._datasets = this.listItems(data, "datasets");
    return this._datasets;
  },

  async loadFields(datasetId) {
    const id = datasetId || this.datasetId;
    if (!id) return [];
    const data = await this.api("/api/v1/data/datasets/" + encodeURIComponent(id) + "/fields?limit=200");
    return this.listItems(data, "fields");
  },

  async capabilities(force) {
    if (this._caps && !force) return this._caps;
    this._caps = await this.api("/api/v1/data/capabilities");
    return this._caps;
  },

  invalidateCache() {
    this._datasets = null;
    this._caps = null;
  },

  datasetLabel(ds) {
    if (!ds) return "-";
    return ds.title || ds.name || ds.id || "-";
  },

  fieldLabel(field) {
    if (!field) return "-";
    return field.title || field.key || field.name || "-";
  },

  fieldKey(field) {
    return (field && (field.key || field.name || field.id)) || "";
  },

  fieldEnum(field) {
    const cfg = (field && field.config) || {};
    const raw = cfg.values || cfg.enum || cfg.options || [];
    return Array.isArray(raw) ? raw.map(v => (typeof v === "object" ? (v.value || v.id || "") : String(v))).filter(Boolean) : [];
  },

  fmtTime(v) {
    if (!v) return "-";
    const d = new Date(v);
    return Number.isNaN(d.getTime()) ? String(v) : d.toLocaleString();
  },

  fmtBytes(n) {
    const num = Number(n);
    if (!Number.isFinite(num) || num <= 0) return "-";
    if (num < 1024) return num + " B";
    if (num < 1048576) return (num / 1024).toFixed(1) + " KB";
    return (num / 1048576).toFixed(1) + " MB";
  },

  preview(value, max) {
    if (value == null || value === "") return "-";
    if (typeof value === "object") {
      try { value = JSON.stringify(value); } catch (_) { value = String(value); }
    }
    const s = String(value);
    const limit = max || 48;
    return s.length > limit ? s.slice(0, limit) + "…" : s;
  },

  parseJSON(text, fallback) {
    const raw = String(text || "").trim();
    if (!raw) return fallback;
    try {
      return JSON.parse(raw);
    } catch (_) {
      if (fallback !== undefined) return fallback;
      throw new Error("JSON 无效");
    }
  },

  val(id) {
    const el = document.getElementById(id);
    return el ? String(el.value || "").trim() : "";
  },

  setVal(id, value) {
    const el = document.getElementById(id);
    if (el) el.value = value == null ? "" : value;
  },

  async refreshDatasetPicker() {
    const select = document.getElementById("datasetPicker");
    if (!select) return;
    try {
      const items = await this.loadDatasets();
      const current = this.datasetId;
      select.innerHTML = "";
      select.appendChild(this.html("option", { value: "" }, "选择当前表…"));
      items.forEach(ds => {
        const opt = this.html("option", { value: ds.id }, this.datasetLabel(ds) + " · " + ds.id);
        if (ds.id === current) opt.selected = true;
        select.appendChild(opt);
      });
      if (current && !items.some(ds => ds.id === current)) {
        const opt = this.html("option", { value: current }, current);
        opt.selected = true;
        select.appendChild(opt);
      }
    } catch (_) {
      /* picker stays empty until auth/network recovers */
    }
  },

  syncDatasetPicker() {
    const select = document.getElementById("datasetPicker");
    if (select && select.value !== this.datasetId) select.value = this.datasetId || "";
  },

  bindDatasetPicker() {
    const select = document.getElementById("datasetPicker");
    if (!select || select.dataset.bound) return;
    select.dataset.bound = "1";
    select.addEventListener("change", () => {
      this.setDataset(select.value);
      if (this.currentPage === "records" || this.currentPage === "fields" || this.currentPage === "relationships") {
        this.navigate(this.currentPage, { dataset: this.datasetId });
      }
    });
  },

  datasetSelect(id, label) {
    const h = this.html;
    const select = h("select", { id });
    select.appendChild(h("option", { value: "" }, "选择业务表…"));
    (this._datasets || []).forEach(ds => {
      const opt = h("option", { value: ds.id }, this.datasetLabel(ds) + " · " + ds.id);
      if (ds.id === this.datasetId) opt.selected = true;
      select.appendChild(opt);
    });
    const alive = this.navGuard();
    this.loadDatasets().then(items => {
      if (!alive()) return;
      const el = document.getElementById(id);
      if (!el) return;
      const keep = el.value || this.datasetId;
      el.innerHTML = "";
      el.appendChild(h("option", { value: "" }, "选择业务表…"));
      items.forEach(ds => {
        const opt = h("option", { value: ds.id }, this.datasetLabel(ds) + " · " + ds.id);
        if (ds.id === keep) opt.selected = true;
        el.appendChild(opt);
      });
    }).catch(() => {});
    return h("div", { class: "form-field" },
      h("label", { for: id }, label || "业务表"),
      select
    );
  },

  fieldControls(fields, values, prefix) {
    const h = this.html;
    const wrap = h("div", { class: "field-grid", id: (prefix || "rec") + "FieldGrid" });
    (fields || []).forEach(field => {
      const key = this.fieldKey(field);
      if (!key) return;
      const fid = (prefix || "rec") + "__" + key;
      const value = values && values[key] != null ? values[key] : "";
      const enums = this.fieldEnum(field);
      let control;
      if (enums.length) {
        control = h("select", { id: fid });
        control.appendChild(h("option", { value: "" }, field.required ? "请选择" : "（空）"));
        enums.forEach(opt => control.appendChild(h("option", { value: opt }, opt)));
        control.value = String(value);
      } else if (field.type === "boolean") {
        control = h("select", { id: fid },
          h("option", { value: "" }, "（空）"),
          h("option", { value: "true" }, "是"),
          h("option", { value: "false" }, "否")
        );
        if (value === true || value === "true") control.value = "true";
        else if (value === false || value === "false") control.value = "false";
      } else if (field.type === "number" || field.type === "integer") {
        control = h("input", { id: fid, type: "number", step: field.type === "integer" ? "1" : "any", value: value === "" ? "" : String(value) });
      } else if (field.type === "date") {
        control = h("input", { id: fid, type: "date", value: String(value).slice(0, 10) });
      } else if (field.type === "datetime") {
        control = h("input", { id: fid, type: "datetime-local", value: String(value).slice(0, 16) });
      } else {
        const ph = field.type === "record_ref" ? "关联记录 ID" : "";
        control = h("input", { id: fid, type: field.sensitive ? "password" : "text", value: String(value), placeholder: ph, spellcheck: "false" });
      }
      const hint = [field.type || "string", field.required ? "必填" : "", field.sensitive ? "敏感" : ""].filter(Boolean).join(" · ");
      wrap.appendChild(h("div", { class: "form-field" },
        h("label", { for: fid }, this.fieldLabel(field)),
        control,
        h("div", { class: "field-hint" }, hint)
      ));
    });
    return wrap;
  },

  collectFieldValues(fields, prefix) {
    const out = {};
    (fields || []).forEach(field => {
      const key = this.fieldKey(field);
      if (!key) return;
      const el = document.getElementById((prefix || "rec") + "__" + key);
      if (!el) return;
      const raw = String(el.value || "").trim();
      if (raw === "") return;
      if (field.type === "number" || field.type === "integer") {
        const n = Number(raw);
        if (!Number.isNaN(n)) out[key] = n;
        return;
      }
      if (field.type === "boolean") {
        out[key] = raw === "true";
        return;
      }
      out[key] = raw;
    });
    return out;
  },

  resultCard(title, data) {
    const h = this.html;
    const pre = h("pre", { class: "result-json" }, typeof data === "string" ? data : JSON.stringify(data, null, 2));
    return h("div", { class: "card result-card" },
      h("div", { class: "card-header" }, h("h3", {}, title)),
      pre
    );
  },

  async download(path, options) {
    const opts = options || {};
    const url = this.endpoint.replace(/\/$/, "") + path;
    const headers = {};
    if (this.token) headers.Authorization = "Bearer " + this.token;
    if (this.tenant) headers["X-MaClaw-Tenant-ID"] = this.tenant;
    if (opts.body) headers["Content-Type"] = "application/json";
    const resp = await fetch(url, { method: opts.method || "GET", headers, body: opts.body });
    if (resp.status === 401) { this.logout(); throw new Error("会话过期，请重新登录"); }
    if (!resp.ok) {
      const data = await resp.json().catch(() => ({}));
      throw new Error(data.error || ("HTTP " + resp.status));
    }
    const blob = await resp.blob();
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = opts.filename || "download";
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(a.href);
  }
};

const PageModules = {};

document.addEventListener("DOMContentLoaded", () => App.init());

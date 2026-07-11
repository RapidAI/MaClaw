// MaClawDataSrv - API Keys Module (Logical Restructure)
"use strict";

PageModules.apikeys = {
  render(container) {
    const h = App.html;
    const t = (k) => I18N.t(k);

    // === Page Header ===
    const header = h("div", { class: "page-header" },
      h("div", {},
        h("h1", {}, t("API Key Management")),
        h("div", { class: "subtitle" }, t("Manage agent and service access credentials. Create scoped keys, review permissions, and rotate expired credentials."))
      ),
      h("button", { class: "ghost sm", onclick: () => this.refreshAll() }, t("Refresh"))
    );

    // === Status Banner ===
    const statusBanner = h("div", { class: "status-banner", id: "apikeysStatus" },
      this.statusItem(t("Total keys"), "0", "totalKeys"),
      this.statusItem(t("Active"), "0", "activeKeys"),
      this.statusItem(t("Expiring soon"), "0", "expiringKeys"),
      this.statusItem(t("Expired"), "0", "expiredKeys"),
      this.statusItem(t("High risk"), "0", "riskKeys")
    );

    // === Section 1: Create Key (Step-by-Step Guide) ===
    const createSection = this.buildCollapsible("createKey", t("Create New Key"), true, () => {
      const body = h("div", {});

      // Step guide
      const steps = h("div", { class: "steps-guide" },
        this.stepCard("1", t("Step 1: Choose Role"), t("Select a preset role that matches the agent's business function.")),
        this.stepCard("2", t("Step 2: Configure Permissions"), t("Fine-tune which operations, datasets, and fields the key can access.")),
        this.stepCard("3", t("Step 3: Set Expiry & Create"), t("Set an expiration date and create the managed key."))
      );
      body.appendChild(steps);

      // Form
      const form = h("div", { class: "card" });

      // Row 1: Key identity
      const row1 = h("div", { class: "form-row" },
        this.field("accessKeyId", t("API key ID"), "text", "sales-agent"),
        this.field("accessUserId", t("User / Agent"), "text", "agent_sales"),
        this.selectField("accessRole", t("Role"), [
          ["data_user", t("data_user") + " - 数据用户"],
          ["data_auditor", t("data_auditor") + " - 审计员"],
          ["data_admin", t("data_admin") + " - 管理员"]
        ])
      );
      form.appendChild(row1);

      // Row 2: Preset
      const row2 = h("div", { class: "form-row" },
        this.selectField("accessPreset", t("Authorization preset"), [["", t("Custom")]]),
        this.field("accessPurpose", t("Agent purpose"), "text", "如：财务报表 Agent")
      );
      form.appendChild(row2);

      const presetBtns = h("div", { class: "btn-group mt-sm" },
        h("button", { class: "sm", onclick: () => this.applyPreset() }, t("Apply preset")),
        h("button", { class: "sm ghost", onclick: () => this.recommend() }, t("Recommend"))
      );
      form.appendChild(presetBtns);

      // Permissions
      const perms = h("div", { class: "checkbox-row mt-md" },
        this.checkbox("allowReports", t("Allow views/reports/dashboards"), true),
        this.checkbox("allowRawData", t("Allow raw dataset API"), false),
        this.checkbox("allowSensitive", t("Allow sensitive fields"), false),
        this.checkbox("allowAdmin", t("Allow admin operations"), false)
      );
      form.appendChild(perms);

      // Expiry + Create
      const row3 = h("div", { class: "form-row mt-md" },
        this.field("accessExpiry", t("Expires at"), "text", "2026-12-31")
      );
      form.appendChild(row3);

      const actions = h("div", { class: "btn-group mt-md" },
        h("button", { class: "primary", onclick: () => this.createKey() }, t("Create key")),
        h("button", { onclick: () => this.generatePolicy() }, t("Generate policy"))
      );
      form.appendChild(actions);

      // Recommendation result area
      form.appendChild(h("div", { id: "accessRecommendation", class: "mt-sm" }));

      body.appendChild(form);
      return body;
    });

    // === Section 2: Key List ===
    const listSection = this.buildCollapsible("keyList", t("Key List"), true, () => {
      const body = h("div", {});

      // Filters
      const filters = h("div", { class: "form-row" },
        this.selectField("keyStatusFilter", t("Status"), [
          ["", t("All")],
          ["active", t("Active")],
          ["expiring_soon", t("Expiring soon")],
          ["expired", t("Expired")],
          ["disabled", t("Disabled")]
        ]),
        this.field("keySearch", t("Search keys..."), "text", "ID、用户、备注"),
        h("div", { class: "form-field", style: { alignSelf: "end" } },
          h("button", { onclick: () => this.loadKeys() }, t("Refresh"))
        )
      );
      body.appendChild(filters);

      // Key table
      body.appendChild(h("div", { class: "table-wrap mt-sm", id: "keyTable" },
        h("div", { class: "empty-state" }, t("Loading..."))
      ));

      // Selected key actions
      const keyActions = h("div", { class: "btn-group mt-sm", id: "selectedKeyActions", style: { display: "none" } },
        h("button", { class: "sm", onclick: () => this.rotateKey() }, "轮换密钥"),
        h("button", { class: "sm", onclick: () => this.previewAccess() }, "预览权限"),
        h("button", { class: "sm danger", onclick: () => this.disableKey() }, "禁用")
      );
      body.appendChild(keyActions);

      return body;
    });

    // === Section 3: Agent Onboarding (Collapsed) ===
    const agentSection = this.buildCollapsible("agentOnboard", t("Agent Onboarding"), false, () => {
      const body = h("div", {});
      body.appendChild(h("p", { class: "card-desc" }, "创建密钥后，生成交接文档和接入包分享给 Agent。"));

      const btns = h("div", { class: "btn-group" },
        h("button", { onclick: () => this.generateHandoff() }, t("Generate handoff document")),
        h("button", { onclick: () => this.runReadiness() }, t("Run readiness check")),
        h("button", { onclick: () => this.generatePacket() }, t("Generate onboarding packet"))
      );
      body.appendChild(btns);

      body.appendChild(h("div", { class: "form-field mt-md" },
        h("label", {}, "交接文档"),
        h("textarea", { id: "agentHandoff", placeholder: "创建密钥后生成...", readonly: true })
      ));

      body.appendChild(h("div", { id: "readinessResult", class: "mt-sm" }));

      return body;
    });

    // === Section 4: Compliance & Review (Collapsed) ===
    const complianceSection = this.buildCollapsible("compliance", t("Compliance & Review"), false, () => {
      const body = h("div", {});
      body.appendChild(h("p", { class: "card-desc" }, "定期复核 API 密钥权限，确保符合最小权限原则。"));

      const btns = h("div", { class: "btn-group" },
        h("button", { onclick: () => this.reviewAccess() }, t("Review access")),
        h("button", { onclick: () => this.exportEvidence() }, t("Export evidence")),
        h("button", { class: "ghost", onclick: () => this.refreshEvidence() }, t("Refresh evidence"))
      );
      body.appendChild(btns);

      body.appendChild(h("div", { id: "complianceResult", class: "mt-sm" }));

      return body;
    });

    // Assemble page
    container.appendChild(header);
    container.appendChild(statusBanner);
    container.appendChild(createSection);
    container.appendChild(listSection);
    container.appendChild(agentSection);
    container.appendChild(complianceSection);

    // Load initial data
    this.refreshAll();
  },

  // === UI Helpers ===
  statusItem(label, value, id) {
    const h = App.html;
    return h("div", { class: "status-item" },
      h("div", { class: "status-label" }, label),
      h("div", { class: "status-value", id: "stat_" + id }, value)
    );
  },

  stepCard(num, title, desc) {
    const h = App.html;
    return h("div", { class: "step-card" },
      h("span", { class: "step-num" }, num),
      h("strong", {}, title),
      h("p", {}, desc)
    );
  },

  buildCollapsible(id, title, defaultOpen, contentFn) {
    return App.collapsible(title, defaultOpen, contentFn);
  },

  field(id, label, type = "text", placeholder = "") {
    return App.field(id, label, type, placeholder);
  },

  selectField(id, label, options) {
    return App.selectField(id, label, options);
  },

  checkbox(id, label, checked = false) {
    const h = App.html;
    const input = h("input", { type: "checkbox", id });
    if (checked) input.checked = true;
    return h("label", {},
      input,
      label
    );
  },

  // === Data Operations ===
  async refreshAll() {
    try {
      const data = await App.api("/api/v1/data/access/api-keys");
      if (data && data.keys) {
        this.updateStats(data.keys);
        this.renderKeyTable(data.keys);
      }
    } catch (e) {
      // silently fail on initial load
    }
  },

  updateStats(keys) {
    const now = new Date();
    const weekLater = new Date(now.getTime() + 7 * 86400000);
    let active = 0, expiring = 0, expired = 0;
    keys.forEach(k => {
      if (k.disabled) return;
      if (!k.expires_at) { active++; return; }
      const exp = new Date(k.expires_at);
      if (exp < now) expired++;
      else if (exp < weekLater) expiring++;
      else active++;
    });
    const set = (id, val) => { const el = document.getElementById("stat_" + id); if (el) el.textContent = val; };
    set("totalKeys", keys.length);
    set("activeKeys", active);
    set("expiringKeys", expiring);
    set("expiredKeys", expired);
  },

  renderKeyTable(keys) {
    const table = document.getElementById("keyTable");
    if (!table) return;
    if (!keys.length) {
      table.innerHTML = "";
      table.appendChild(App.emptyState(
        "暂无 API 密钥",
        "使用上方表单为 Agent 或服务创建首个作用域密钥。"
      ));
      return;
    }
    // Build table via DOM to avoid XSS from key IDs
    const h = App.html;
    const tbl = document.createElement("table");
    const thead = h("thead", {}, h("tr", {},
      h("th", {}, "ID"), h("th", {}, "用户"), h("th", {}, "角色"),
      h("th", {}, "状态"), h("th", {}, "过期时间"), h("th", {}, "操作")
    ));
    tbl.appendChild(thead);
    const tbody = document.createElement("tbody");
    keys.forEach(k => {
      const statusEl = k.disabled ? App.badge("已停用") : App.badge("有效", "ok");
      const expiryText = k.expires_at ? new Date(k.expires_at).toLocaleDateString() : "永不过期";
      const selectBtn = h("button", { class: "sm ghost", type: "button", onclick: () => this.selectKey(k.id) }, "选择");
      const row = h("tr", {},
        h("td", { class: "mono" }, k.id || "-"),
        h("td", {}, k.user || "-"),
        h("td", {}, k.role || "-"),
        h("td", {}, statusEl),
        h("td", {}, expiryText),
        h("td", {}, selectBtn)
      );
      tbody.appendChild(row);
    });
    tbl.appendChild(tbody);
    table.innerHTML = "";
    table.appendChild(tbl);
  },

  // === Actions (stubs - connect to real API) ===
  selectKey(id) { App.toast("已选择密钥: " + id); },
  createKey() { App.toast("创建密钥..."); },
  generatePolicy() { App.toast("正在生成策略..."); },
  applyPreset() { App.toast("应用预设..."); },
  recommend() { App.toast("正在生成推荐方案..."); },
  rotateKey() { App.toast("轮换密钥..."); },
  previewAccess() { App.toast("预览权限..."); },
  disableKey() { App.toast("禁用密钥..."); },
  loadKeys() { this.refreshAll(); },
  generateHandoff() { App.toast("生成交接文档..."); },
  runReadiness() { App.toast("运行就绪检查..."); },
  generatePacket() { App.toast("生成接入包..."); },
  reviewAccess() { App.toast("复核访问..."); },
  exportEvidence() { App.toast("导出证据..."); },
  refreshEvidence() { App.toast("刷新证据..."); },
};

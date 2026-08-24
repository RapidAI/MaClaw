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
    this.loadPresets();
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
    const alive = App.navGuard();
    try {
      const data = await App.api("/api/v1/data/access/api-keys");
      if (!alive()) return;
      const keys = App.listItems(data, "keys");
      this._keys = keys;
      this.updateStats(keys);
      this.renderKeyTable(keys);
    } catch (e) {
      if (!alive()) return;
      const table = document.getElementById("keyTable");
      if (table) {
        table.innerHTML = "";
        table.appendChild(App.emptyState("加载密钥失败", e.message || "需要管理员权限。"));
      }
    }
  },

  async loadPresets() {
    const alive = App.navGuard();
    try {
      const data = await App.api("/api/v1/data/access/presets?limit=100");
      if (!alive()) return;
      this._presets = App.listItems(data, "presets");
      const sel = document.getElementById("accessPreset");
      if (!sel) return;
      const keep = sel.value;
      sel.innerHTML = "";
      sel.appendChild(App.html("option", { value: "" }, "自定义"));
      this._presets.forEach(p => {
        sel.appendChild(App.html("option", { value: p.id }, p.title || p.id));
      });
      if (keep) sel.value = keep;
    } catch (_) {
      this._presets = this._presets || [];
    }
  },

  updateStats(keys) {
    const now = new Date();
    const weekLater = new Date(now.getTime() + 7 * 86400000);
    let active = 0, expiring = 0, expired = 0, risk = 0;
    keys.forEach(k => {
      if (k.allow_admin || k.allow_raw_data || k.allow_sensitive) risk++;
      if (k.enabled === false || k.disabled) return;
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
    set("riskKeys", risk);
  },

  renderKeyTable(allKeys) {
    const table = document.getElementById("keyTable");
    if (!table) return;
    const status = App.val("keyStatusFilter");
    const q = App.val("keySearch").toLowerCase();
    const now = new Date();
    const weekLater = new Date(now.getTime() + 7 * 86400000);
    const keys = (allKeys || []).filter(k => {
      if (q) {
        const hay = [k.id, k.user_id, k.user, k.role, k.note].join(" ").toLowerCase();
        if (!hay.includes(q)) return false;
      }
      if (!status) return true;
      const disabled = k.enabled === false || k.disabled;
      const exp = k.expires_at ? new Date(k.expires_at) : null;
      if (status === "disabled") return disabled;
      if (disabled) return false;
      if (status === "expired") return !!(exp && exp < now);
      if (status === "expiring_soon") return !!(exp && exp >= now && exp < weekLater);
      if (status === "active") return !exp || exp >= weekLater;
      return true;
    });
    if (!keys.length) {
      table.innerHTML = "";
      table.appendChild(App.emptyState(
        allKeys && allKeys.length ? "没有匹配的密钥" : "暂无 API 密钥",
        allKeys && allKeys.length ? "调整状态或搜索条件后再试。" : "使用上方表单为 Agent 或服务创建首个作用域密钥。"
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
      const statusEl = (k.enabled === false || k.disabled) ? App.badge("已停用") : App.badge("有效", "ok");
      const expiryText = k.expires_at ? new Date(k.expires_at).toLocaleDateString() : "永不过期";
      const selectBtn = h("button", { class: "sm ghost", type: "button", onclick: () => this.selectKey(k.id) }, "选择");
      const row = h("tr", {},
        h("td", { class: "mono" }, k.id || "-"),
        h("td", {}, k.user_id || k.user || "-"),
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

  selectKey(id) {
    this._selectedKey = id;
    const actions = document.getElementById("selectedKeyActions");
    if (actions) actions.style.display = "flex";
    App.toast("已选择密钥: " + id);
  },

  async createKey() {
    const id = App.val("accessKeyId");
    if (!id) { App.toast("请填写密钥 ID", "warn"); return; }
    const preset = this._preset || {};
    const allowReports = document.getElementById("allowReports")?.checked !== false;
    const body = {
      id,
      user_id: App.val("accessUserId"),
      role: App.val("accessRole") || preset.role || "data_user",
      note: App.val("accessPurpose"),
      allow_raw_data: document.getElementById("allowRawData")?.checked === true,
      allow_sensitive: document.getElementById("allowSensitive")?.checked === true,
      allow_admin: document.getElementById("allowAdmin")?.checked === true,
      expires_at: App.val("accessExpiry") || undefined,
    };
    if (preset.allowed_domains) body.allowed_domains = preset.allowed_domains;
    if (preset.allowed_datasets) body.allowed_datasets = preset.allowed_datasets;
    if (preset.allowed_actions) body.allowed_actions = preset.allowed_actions;
    if (allowReports) {
      body.allowed_views = preset.allowed_views || [];
      body.allowed_reports = preset.allowed_reports || [];
      body.allowed_dashboards = preset.allowed_dashboards || [];
    }
    try {
      const out = await App.api("/api/v1/data/access/api-keys", {
        method: "POST",
        body: JSON.stringify(body),
      });
      const key = out.key || "";
      const keyId = (out.policy && out.policy.id) || id;
      const lines = [
        "MaClawDataSrv agent key (shown once)",
        "endpoint: " + App.endpoint,
        "key_id: " + keyId,
        "key: " + key,
        "tool: mis_data",
        "first call: GET /api/v1/data/capabilities",
      ];
      const rec = document.getElementById("accessRecommendation");
      if (rec) {
        rec.innerHTML = "";
        rec.appendChild(App.resultCard("密钥只显示一次，请立即复制", {
          endpoint: App.endpoint,
          key_id: keyId,
          key: key || "(服务未返回明文)",
          first_call: "GET /api/v1/data/capabilities",
        }));
      }
      const handoff = document.getElementById("agentHandoff");
      if (handoff) handoff.value = lines.join("\n");
      App.toast(key ? "密钥已创建，请立即复制明文" : "密钥已创建");
      this.refreshAll();
    } catch (e) {
      App.toast(e.message || "创建失败", "danger");
    }
  },

  async generatePolicy() {
    if (!this._presets || !this._presets.length) await this.loadPresets();
    const el = document.getElementById("accessRecommendation");
    if (el) {
      el.innerHTML = "";
      el.appendChild(App.resultCard("授权预设", this._presets || []));
    }
  },

  applyPreset() {
    const id = App.val("accessPreset");
    const preset = (this._presets || []).find(p => p.id === id);
    if (!preset) { App.toast("请先选择授权预设", "warn"); return; }
    this._preset = preset;
    App.setVal("accessRole", preset.role || "data_user");
    const setChk = (fid, v) => { const el = document.getElementById(fid); if (el) el.checked = !!v; };
    setChk("allowRawData", preset.allow_raw_data);
    setChk("allowSensitive", preset.allow_sensitive);
    setChk("allowAdmin", preset.allow_admin);
    setChk("allowReports", true);
    App.toast("已应用预设 " + (preset.title || preset.id));
  },

  async recommend() {
    if (!this._presets || !this._presets.length) await this.loadPresets();
    const purpose = App.val("accessPurpose").toLowerCase();
    const presets = this._presets || [];
    if (!purpose || !presets.length) {
      App.toast("请先填写 Agent 用途，并确认预设已加载", "warn");
      return;
    }
    const tokens = purpose.split(/[^a-z0-9_\u4e00-\u9fa5-]+/).filter(t => t.length > 1);
    let best = null, bestScore = 0;
    presets.forEach(p => {
      const hay = [p.id, p.title, p.description].concat(p.allowed_domains || []).join(" ").toLowerCase();
      const score = tokens.filter(t => hay.includes(t)).length;
      if (score > bestScore) { best = p; bestScore = score; }
    });
    if (!best) { App.toast("没有匹配的预设，请手动选择", "warn"); return; }
    App.setVal("accessPreset", best.id);
    this.applyPreset();
  },

  async rotateKey() {
    if (!this._selectedKey) { App.toast("请先选择密钥", "warn"); return; }
    try {
      const out = await App.api("/api/v1/data/access/api-keys/" + encodeURIComponent(this._selectedKey) + "/rotate", {
        method: "POST",
      });
      const handoff = document.getElementById("agentHandoff");
      if (handoff && out.key) handoff.value = "rotated key_id=" + this._selectedKey + "\nkey=" + out.key;
      App.toast("已轮换，请复制新密钥");
      this.refreshAll();
    } catch (e) {
      App.toast(e.message || "轮换失败", "danger");
    }
  },

  async previewAccess() {
    if (!this._selectedKey) { App.toast("请先选择密钥", "warn"); return; }
    try {
      const data = await App.api("/api/v1/data/access/api-keys/" + encodeURIComponent(this._selectedKey) + "/capabilities");
      const el = document.getElementById("accessRecommendation");
      if (el) {
        el.innerHTML = "";
        el.appendChild(App.resultCard("权限预览", data));
      }
    } catch (e) {
      App.toast(e.message || "预览失败", "danger");
    }
  },

  async disableKey() {
    if (!this._selectedKey) { App.toast("请先选择密钥", "warn"); return; }
    if (!confirm("确定禁用该密钥？")) return;
    try {
      await App.api("/api/v1/data/access/api-keys/" + encodeURIComponent(this._selectedKey), { method: "DELETE" });
      App.toast("已禁用");
      this.refreshAll();
    } catch (e) {
      App.toast(e.message || "禁用失败", "danger");
    }
  },

  loadKeys() {
    if (this._keys) this.renderKeyTable(this._keys);
    else this.refreshAll();
  },

  generateHandoff() {
    const el = document.getElementById("agentHandoff");
    if (!el) return;
    el.value = [
      "# Agent onboarding",
      "1. GET /api/v1/data/capabilities",
      "2. POST /api/v1/data/intent/resolve  {query}",
      "3. follow next_steps.tool_call_template",
      "4. dry_run before execute_business_action",
    ].join("\n");
    App.toast("已生成交接提纲");
  },

  async runReadiness() {
    try {
      const data = await App.api("/api/v1/data/access/review");
      this.putCompliance(data);
    } catch (e) {
      App.toast(e.message || "就绪检查失败", "danger");
    }
  },

  generatePacket() { this.generateHandoff(); },

  async reviewAccess() {
    try {
      const data = await App.api("/api/v1/data/access/review");
      this.putCompliance(data);
    } catch (e) {
      App.toast(e.message || "复核失败", "danger");
    }
  },

  async exportEvidence() {
    try {
      await App.download("/api/v1/data/governance/evidence-summary.txt", { filename: "evidence-summary.txt" });
      App.toast("已导出证据摘要");
    } catch (e) {
      App.toast(e.message || "导出失败", "danger");
    }
  },

  async refreshEvidence() {
    try {
      const data = await App.api("/api/v1/data/governance/evidence-pack");
      this.putCompliance(data);
    } catch (e) {
      App.toast(e.message || "刷新证据失败", "danger");
    }
  },

  putCompliance(data) {
    const el = document.getElementById("complianceResult") || document.getElementById("readinessResult");
    if (!el) return;
    el.innerHTML = "";
    el.appendChild(App.resultCard("治理结果", data));
  },
};

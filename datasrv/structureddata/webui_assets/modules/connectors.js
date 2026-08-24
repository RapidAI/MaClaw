// MaClawDataSrv - Connectors Module
"use strict";

PageModules.connectors = {
  render(container) {
    const h = App.html;
    const t = (k) => I18N.t(k);

    container.appendChild(h("div", { class: "page-header" },
      h("div", {},
        h("h1", {}, t("External Connectors")),
        h("div", { class: "subtitle" }, t("Manage integrations with CRM, ERP, HR, and other external systems."))
      ),
      h("button", { class: "ghost sm", type: "button", onclick: () => this.refresh() }, t("Refresh"))
    ));

    container.appendChild(h("div", { class: "card" },
      h("div", { class: "card-header" },
        h("h3", {}, t("Connector List")),
        h("div", { class: "text-muted text-sm" }, "选择连接器查看详情和管理配置")
      ),
      h("div", { id: "connectorTable", class: "table-wrap mt-sm" },
        h("div", { class: "empty-state" }, "加载中...")
      )
    ));

    container.appendChild(this.buildCollapsible("connConfig", t("Connector Configuration"), true, () => {
      const body = h("div", {});
      body.appendChild(h("div", { class: "form-row" },
        this.field("connId", t("Connector ID"), "text", "sales.crm"),
        this.field("connName", t("Name"), "text", "Sales CRM"),
        this.field("connDomain", t("Domain"), "text", "sales")
      ));
      body.appendChild(h("div", { class: "form-row" },
        this.field("connKind", t("Kind"), "text", "crm, erp, hris"),
        this.field("connAuth", t("Auth type"), "text", "bearer, api_key"),
        this.field("connToken", t("Token ref"), "text", "MIS_CRM_TOKEN")
      ));
      body.appendChild(this.field("connBaseUrl", t("Base URL"), "text", "https://crm.example.local"));
      body.appendChild(h("div", { class: "form-field mt-sm" },
        h("label", {}, t("Subscribed actions")),
        h("textarea", { id: "connActions", style: { minHeight: "60px" }, placeholder: '["sales.order_upsert"]' })
      ));
      body.appendChild(h("div", { class: "form-field mt-sm" },
        h("label", {}, t("Config JSON")),
        h("textarea", { id: "connConfig", placeholder: "{}" })
      ));
      body.appendChild(h("div", { class: "checkbox-row mt-sm" },
        h("label", {},
          h("input", { type: "checkbox", id: "connEnabled", checked: true }),
          t("Enabled")
        )
      ));
      body.appendChild(h("div", { class: "btn-group mt-md" },
        h("button", { class: "primary", type: "button", onclick: () => this.save() }, t("Save connector")),
        h("button", { class: "sm ghost", type: "button", onclick: () => this.formatJSON() }, "格式化 JSON")
      ));
      return body;
    }));

    container.appendChild(this.buildCollapsible("connHealth", t("Health & Diagnostics"), false, () => {
      const body = h("div", {});
      body.appendChild(h("p", { class: "card-desc" }, "检查连接器与外部系统的连通性和配置正确性。"));
      body.appendChild(h("div", { class: "btn-group" },
        h("button", { type: "button", onclick: () => this.testBindings() }, t("Test bindings")),
        h("button", { type: "button", onclick: () => this.checkReadiness() }, t("Check readiness")),
        h("button", { type: "button", onclick: () => this.checkHealth() }, t("Check health"))
      ));
      body.appendChild(h("div", { id: "healthResult", class: "mt-sm" }));
      return body;
    }));

    container.appendChild(this.buildCollapsible("connSync", t("Sync Management"), false, () => {
      const body = h("div", {});
      body.appendChild(h("p", { class: "card-desc" }, "查看同步状态和历史，运行手动同步批次。"));
      body.appendChild(h("div", { class: "btn-group" },
        h("button", { type: "button", onclick: () => this.syncState() }, t("Sync state")),
        h("button", { class: "primary", type: "button", onclick: () => this.runBatch() }, t("Run sync batch")),
        h("button", { class: "sm ghost", type: "button", onclick: () => this.syncHistory() }, t("Sync history"))
      ));
      body.appendChild(h("div", { id: "syncResult", class: "table-wrap mt-sm" }));
      return body;
    }));

    this.refresh();
  },

  field(id, label, type, placeholder) {
    return App.field(id, label, type, placeholder);
  },

  buildCollapsible(id, title, defaultOpen, contentFn) {
    return App.collapsible(title, defaultOpen, contentFn);
  },

  currentId() {
    return App.val("connId");
  },

  async refresh() {
    const alive = App.navGuard();
    try {
      const data = await App.api("/api/v1/data/connectors");
      if (!alive()) return;
      const items = App.listItems(data, "connectors");
      const el = document.getElementById("connectorTable");
      if (!el) return;
      if (!items.length) {
        el.innerHTML = "";
        el.appendChild(App.emptyState(
          "暂无连接器",
          "使用下方「连接器配置」创建第一个外部系统集成。"
        ));
        return;
      }
      const rows = items.map(c => ({
        _id: c.id,
        _cells: [
          { text: c.id || "-", attrs: { class: "mono" } },
          c.name || "-",
          c.kind || "-",
          c.domain || "-",
          c.enabled !== false ? App.badge("启用", "ok") : App.badge("停用")
        ]
      }));
      el.innerHTML = "";
      el.appendChild(App.table(["ID", "名称", "类型", "域", "状态"], rows, {
        onRowClick: (id) => this.select(id)
      }));
    } catch (e) {
      if (!alive()) return;
      const el = document.getElementById("connectorTable");
      if (el) {
        el.innerHTML = "";
        el.appendChild(App.emptyState("加载失败", e.message));
      }
    }
  },

  async select(id) {
    try {
      const c = await App.api("/api/v1/data/connectors/" + encodeURIComponent(id));
      App.setVal("connId", c.id || id);
      App.setVal("connName", c.name || "");
      App.setVal("connDomain", c.domain || "");
      App.setVal("connKind", c.kind || "");
      App.setVal("connAuth", c.auth_type || "");
      App.setVal("connToken", c.token_ref || "");
      App.setVal("connBaseUrl", c.base_url || "");
      App.setVal("connActions", JSON.stringify(c.subscribed_actions || [], null, 2));
      const cfg = Object.assign({}, c.config || {});
      delete cfg.token;
      delete cfg.secret;
      delete cfg.password;
      App.setVal("connConfig", JSON.stringify(cfg, null, 2));
      const enabled = document.getElementById("connEnabled");
      if (enabled) enabled.checked = c.enabled !== false;
      App.toast("已选择: " + id);
    } catch (e) {
      App.toast(e.message || "读取连接器失败", "danger");
    }
  },

  connectorBody() {
    const actionsRaw = document.getElementById("connActions")?.value || "";
    const configRaw = document.getElementById("connConfig")?.value || "";
    let actions = [];
    let config = {};
    if (actionsRaw.trim()) {
      actions = App.parseJSON(actionsRaw);
      if (!Array.isArray(actions)) throw new Error("订阅动作需要 JSON 数组");
    }
    if (configRaw.trim()) {
      config = App.parseJSON(configRaw);
      if (!config || typeof config !== "object" || Array.isArray(config)) throw new Error("Config JSON 需要对象");
    }
    return {
      id: App.val("connId"),
      name: App.val("connName"),
      domain: App.val("connDomain"),
      kind: App.val("connKind"),
      auth_type: App.val("connAuth"),
      token_ref: App.val("connToken"),
      base_url: App.val("connBaseUrl"),
      subscribed_actions: Array.isArray(actions) ? actions : [],
      config,
      enabled: document.getElementById("connEnabled")?.checked !== false,
    };
  },

  async save() {
    let body;
    try {
      body = this.connectorBody();
    } catch (e) {
      App.toast(e.message || "JSON 无效", "danger");
      return;
    }
    if (!body.name) { App.toast("请填写名称", "warn"); return; }
    try {
      if (body.id) {
        await App.api("/api/v1/data/connectors/" + encodeURIComponent(body.id), {
          method: "PUT",
          body: JSON.stringify(body),
        });
      } else {
        const created = await App.api("/api/v1/data/connectors", {
          method: "POST",
          body: JSON.stringify(body),
        });
        if (created.id) App.setVal("connId", created.id);
      }
      App.toast("连接器已保存");
      this.refresh();
    } catch (e) {
      App.toast(e.message || "保存失败", "danger");
    }
  },

  formatJSON() {
    try {
      const parsed = App.parseJSON(document.getElementById("connConfig")?.value, {});
      App.setVal("connConfig", JSON.stringify(parsed, null, 2));
      App.toast("已格式化");
    } catch (e) {
      App.toast("JSON 无效", "danger");
    }
  },

  async testBindings() { await this.call("/test", "测试绑定"); },
  async checkReadiness() {
    const id = this.currentId();
    if (!id) { App.toast("请先选择连接器", "warn"); return; }
    try {
      const out = await App.api("/api/v1/data/connectors/" + encodeURIComponent(id) + "/readiness", { method: "POST" });
      this.putResult("healthResult", "就绪检查", out);
    } catch (e) {
      App.toast(e.message || "就绪检查失败", "danger");
    }
  },
  async checkHealth() {
    try {
      const data = await App.api("/api/v1/data/connectors/health");
      this.putResult("healthResult", "健康检查", data);
    } catch (e) {
      App.toast(e.message || "健康检查失败", "danger");
    }
  },
  async syncState() {
    const id = this.currentId();
    if (!id) { App.toast("请先选择连接器", "warn"); return; }
    await this.show("/api/v1/data/connectors/" + encodeURIComponent(id) + "/sync-state", "同步状态", "syncResult");
  },
  async runBatch() {
    const id = this.currentId();
    if (!id) { App.toast("请先选择连接器", "warn"); return; }
    try {
      const out = await App.api("/api/v1/data/connectors/" + encodeURIComponent(id) + "/sync-batch", {
        method: "POST",
        body: JSON.stringify({}),
      });
      this.putResult("syncResult", "同步批次", out);
    } catch (e) {
      App.toast(e.message || "同步失败", "danger");
    }
  },
  async syncHistory() {
    const id = this.currentId();
    if (!id) { App.toast("请先选择连接器", "warn"); return; }
    await this.show("/api/v1/data/connectors/" + encodeURIComponent(id) + "/sync-runs", "同步历史", "syncResult");
  },

  async call(suffix, title) {
    const id = this.currentId();
    if (!id) { App.toast("请先选择连接器", "warn"); return; }
    try {
      const out = await App.api("/api/v1/data/connectors/" + encodeURIComponent(id) + suffix, { method: "POST" });
      this.putResult("healthResult", title, out);
    } catch (e) {
      App.toast(e.message || (title + "失败"), "danger");
    }
  },

  async show(path, title, target) {
    try {
      const data = await App.api(path);
      this.putResult(target || "healthResult", title, data);
    } catch (e) {
      App.toast(e.message || (title + "失败"), "danger");
    }
  },

  putResult(id, title, data) {
    const el = document.getElementById(id);
    if (!el) return;
    el.innerHTML = "";
    el.appendChild(App.resultCard(title, data));
  }
};

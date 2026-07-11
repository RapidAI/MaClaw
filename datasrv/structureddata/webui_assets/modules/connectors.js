// MaClawDataSrv - Connectors Module
"use strict";

PageModules.connectors = {
  render(container) {
    const h = App.html;
    const t = (k) => I18N.t(k);

    // === Page Header ===
    container.appendChild(h("div", { class: "page-header" },
      h("div", {},
        h("h1", {}, t("External Connectors")),
        h("div", { class: "subtitle" }, t("Manage integrations with CRM, ERP, HR, and other external systems."))
      ),
      h("button", { class: "ghost sm", onclick: () => this.refresh() }, t("Refresh"))
    ));

    // === Section 1: Connector List ===
    container.appendChild(h("div", { class: "card" },
      h("div", { class: "card-header" },
        h("h3", {}, t("Connector List")),
        h("div", { class: "text-muted text-sm" }, "选择连接器查看详情和管理配置")
      ),
      h("div", { id: "connectorTable", class: "table-wrap mt-sm" },
        h("div", { class: "empty-state" }, "加载中...")
      )
    ));

    // === Section 2: Configuration (Default Open) ===
    container.appendChild(this.buildCollapsible("connConfig", t("Connector Configuration"), true, () => {
      const body = h("div", {});

      // Basic info
      const row1 = h("div", { class: "form-row" },
        this.field("connId", t("Connector ID"), "text", "sales.crm"),
        this.field("connName", t("Name"), "text", "Sales CRM"),
        this.field("connDomain", t("Domain"), "text", "sales")
      );
      body.appendChild(row1);

      const row2 = h("div", { class: "form-row" },
        this.field("connKind", t("Kind"), "text", "crm, erp, hris"),
        this.field("connAuth", t("Auth type"), "text", "bearer, api_key"),
        this.field("connToken", t("Token ref"), "text", "MIS_CRM_TOKEN")
      );
      body.appendChild(row2);

      body.appendChild(this.field("connBaseUrl", t("Base URL"), "text", "https://crm.example.local"));

      body.appendChild(h("div", { class: "form-field mt-sm" },
        h("label", {}, t("Subscribed actions")),
        h("textarea", { id: "connActions", style: { minHeight: "60px" }, placeholder: '["sales.order_upsert"]' })
      ));

      body.appendChild(h("div", { class: "form-field mt-sm" },
        h("label", {}, t("Config JSON")),
        h("textarea", { id: "connConfig", placeholder: "{}" })
      ));

      const enabledRow = h("div", { class: "checkbox-row mt-sm" },
        h("label", {},
          h("input", { type: "checkbox", id: "connEnabled", checked: true }),
          t("Enabled")
        )
      );
      body.appendChild(enabledRow);

      const btns = h("div", { class: "btn-group mt-md" },
        h("button", { class: "primary", onclick: () => this.save() }, t("Save connector")),
        h("button", { class: "sm ghost", onclick: () => this.formatJSON() }, "格式化 JSON")
      );
      body.appendChild(btns);

      return body;
    }));

    // === Section 3: Health & Diagnostics (Collapsed) ===
    container.appendChild(this.buildCollapsible("connHealth", t("Health & Diagnostics"), false, () => {
      const body = h("div", {});
      body.appendChild(h("p", { class: "card-desc" }, "检查连接器与外部系统的连通性和配置正确性。"));

      const btns = h("div", { class: "btn-group" },
        h("button", { onclick: () => this.testBindings() }, t("Test bindings")),
        h("button", { onclick: () => this.checkReadiness() }, t("Check readiness")),
        h("button", { onclick: () => this.checkHealth() }, t("Check health"))
      );
      body.appendChild(btns);

      body.appendChild(h("div", { id: "healthResult", class: "mt-sm" }));
      return body;
    }));

    // === Section 4: Sync Management (Collapsed) ===
    container.appendChild(this.buildCollapsible("connSync", t("Sync Management"), false, () => {
      const body = h("div", {});
      body.appendChild(h("p", { class: "card-desc" }, "查看同步状态和历史，运行手动同步批次。"));

      const btns = h("div", { class: "btn-group" },
        h("button", { onclick: () => this.syncState() }, t("Sync state")),
        h("button", { class: "primary", onclick: () => this.runBatch() }, t("Run sync batch")),
        h("button", { class: "sm ghost", onclick: () => this.syncHistory() }, t("Sync history"))
      );
      body.appendChild(btns);

      body.appendChild(h("div", { id: "syncResult", class: "table-wrap mt-sm" }));
      return body;
    }));

    this.refresh();
  },

  // === Helpers ===
  field(id, label, type, placeholder) {
    return App.field(id, label, type, placeholder);
  },

  buildCollapsible(id, title, defaultOpen, contentFn) {
    return App.collapsible(title, defaultOpen, contentFn);
  },

  // === Actions ===
  async refresh() {
    try {
      const data = await App.api("/api/v1/data/connectors");
      const el = document.getElementById("connectorTable");
      if (!el) return;
      if (!data.connectors || !data.connectors.length) {
        el.innerHTML = "";
        el.appendChild(App.emptyState(
          "暂无连接器",
          "使用下方「连接器配置」创建第一个外部系统集成。"
        ));
        return;
      }
      const rows = data.connectors.map(c => ({
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
      const el = document.getElementById("connectorTable");
      if (el) {
        el.innerHTML = "";
        el.appendChild(App.emptyState("加载失败", e.message));
      }
    }
  },

  select(id) { App.toast("已选择: " + id); },
  save() { App.toast("保存连接器..."); },
  formatJSON() { App.toast("已格式化"); },
  testBindings() { App.toast("测试绑定..."); },
  checkReadiness() { App.toast("检查就绪..."); },
  checkHealth() { App.toast("检查健康..."); },
  syncState() { App.toast("查询同步状态..."); },
  runBatch() { App.toast("运行同步批次..."); },
  syncHistory() { App.toast("加载同步历史..."); },
};

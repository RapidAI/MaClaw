// MaClawDataSrv - Records Module
"use strict";

PageModules.records = {
  render(container) {
    const h = App.html;
    const t = (k) => I18N.t(k);

    // === Page Header ===
    container.appendChild(h("div", { class: "page-header" },
      h("div", {},
        h("h1", {}, t("Data Records")),
        h("div", { class: "subtitle" }, t("Search and manage structured business records."))
      )
    ));

    // === Section 1: Search & Browse (Default Open) ===
    container.appendChild(this.buildCollapsible("searchBrowse", t("Search & Browse"), true, () => {
      const body = h("div", {});

      // Search form
      const searchRow = h("div", { class: "form-row" },
        this.field("queryText", t("Keyword"), "text", "客户名、金额..."),
        this.field("queryTag", t("Tag"), "text", "q1"),
        this.field("queryLimit", t("Limit"), "number", "50")
      );
      body.appendChild(searchRow);

      // Filter (optional, collapsible in future)
      body.appendChild(h("div", { class: "form-field" },
        h("label", {}, "筛选条件 (JSON)"),
        h("textarea", { id: "queryFilter", placeholder: '{"field":"amount","op":"gte","value":1000}', style: { minHeight: "60px" } })
      ));

      // Action buttons
      const actions = h("div", { class: "btn-group mt-sm" },
        h("button", { class: "primary", onclick: () => this.query() }, t("Query")),
        h("button", { class: "sm", onclick: () => this.exportCSV() }, t("Export CSV")),
        h("button", { class: "sm", onclick: () => this.exportJSONL() }, t("Export JSONL")),
        h("button", { class: "ghost sm", onclick: () => this.clearQuery() }, t("Clear"))
      );
      body.appendChild(actions);

      // Results
      body.appendChild(h("div", { id: "recordTable", class: "mt-md" },
        App.emptyState("等待查询", "输入关键词或筛选条件后点击「查询」。")
      ));

      return body;
    }));

    // === Section 2: Record Editor (Default Open) ===
    container.appendChild(this.buildCollapsible("recordEdit", t("Record Editor"), true, () => {
      const body = h("div", {});

      const row1 = h("div", { class: "form-row" },
        this.field("recordId", t("Record ID"), "text", "留空自动创建"),
        this.field("recordTitle", t("Title"), "text", "")
      );
      body.appendChild(row1);

      body.appendChild(this.field("recordTags", t("Tags") + "（逗号分隔）", "text", "q1, imported"));

      body.appendChild(h("div", { class: "form-field mt-sm" },
        h("label", {}, t("Data JSON")),
        h("textarea", { id: "recordData", placeholder: '{\n  "customer": "Acme",\n  "amount": 8800\n}' })
      ));

      const editActions = h("div", { class: "btn-group mt-sm" },
        h("button", { class: "sm", onclick: () => this.validate() }, t("Validate")),
        h("button", { class: "primary", onclick: () => this.save() }, t("Save")),
        h("button", { class: "sm", onclick: () => this.newRecord() }, t("New record")),
        h("button", { class: "danger sm", onclick: () => this.deleteRecord() }, t("Delete"))
      );
      body.appendChild(editActions);

      return body;
    }));

    // === Section 3: Batch Operations (Collapsed) ===
    container.appendChild(this.buildCollapsible("batchOps", t("Batch Operations"), false, () => {
      const body = h("div", {});
      body.appendChild(h("p", { class: "card-desc" }, "批量导入、更新或删除记录。适用于初始数据加载和定期同步。"));

      const btns = h("div", { class: "btn-group" },
        h("button", { onclick: () => this.importCSV() }, t("Import CSV")),
        h("button", { onclick: () => this.importJSONL() }, t("Import JSONL")),
        h("button", { onclick: () => this.bulkUpdate() }, t("Bulk update")),
        h("button", { class: "danger", onclick: () => this.bulkDelete() }, t("Bulk delete"))
      );
      body.appendChild(btns);

      body.appendChild(h("div", { class: "form-field mt-md" },
        h("label", {}, "批量数据 (JSON Array)"),
        h("textarea", { id: "batchData", placeholder: '[{"customer":"A","amount":100}, ...]' })
      ));

      return body;
    }));
  },

  // === Helpers ===
  field(id, label, type, placeholder) {
    return App.field(id, label, type, placeholder);
  },

  buildCollapsible(id, title, defaultOpen, contentFn) {
    return App.collapsible(title, defaultOpen, contentFn);
  },

  // === Actions (stubs) ===
  query() { App.toast("正在查询..."); },
  exportCSV() { App.toast("导出 CSV..."); },
  exportJSONL() { App.toast("导出 JSONL..."); },
  clearQuery() {
    const q = document.getElementById("queryText");
    if (q) q.value = "";
    const table = document.getElementById("recordTable");
    if (table) {
      table.innerHTML = "";
      table.appendChild(App.emptyState("等待查询", "输入关键词或筛选条件后点击「查询」。"));
    }
  },
  validate() { App.toast("校验中..."); },
  save() { App.toast("保存中..."); },
  newRecord() { ["recordId", "recordTitle", "recordTags", "recordData"].forEach(id => { const el = document.getElementById(id); if (el) el.value = ""; }); },
  deleteRecord() { if (confirm("确定删除此记录？")) App.toast("已删除"); },
  importCSV() { App.toast("导入 CSV..."); },
  importJSONL() { App.toast("导入 JSONL..."); },
  bulkUpdate() { App.toast("批量更新..."); },
  bulkDelete() { if (confirm("确定批量删除匹配的记录？")) App.toast("批量删除中..."); },
};

// MaClawDataSrv - Records workbench
"use strict";

PageModules.records = {
  _fields: [],
  _selected: null,

  render(container) {
    const h = App.html;
    const t = (k) => I18N.t(k);

    container.appendChild(h("div", { class: "page-header" },
      h("div", {},
        h("h1", {}, t("Data Records")),
        h("div", { class: "subtitle" }, "按字段查、改、导入。默认不用写 JSON。")
      )
    ));

    const split = h("div", { class: "record-split" });

    const list = h("div", { class: "card" });
    list.appendChild(h("div", { class: "card-header" }, h("h3", {}, t("Search & Browse"))));
    list.appendChild(h("div", { class: "form-row" },
      App.datasetSelect("recordDataset", "业务表"),
      this.field("queryText", t("Keyword"), "text", "客户名、单号、金额"),
      this.field("queryTag", t("Tag"), "text", "q1"),
      this.field("queryLimit", t("Limit"), "number", "50")
    ));
    list.appendChild(h("div", { class: "btn-group mt-sm" },
      h("button", { class: "primary", type: "button", onclick: () => this.query() }, t("Query")),
      h("button", { class: "sm", type: "button", onclick: () => this.exportCSV() }, t("Export CSV")),
      h("button", { class: "sm", type: "button", onclick: () => this.exportJSONL() }, t("Export JSONL")),
      h("button", { class: "ghost sm", type: "button", onclick: () => this.clearQuery() }, t("Clear"))
    ));
    list.appendChild(App.collapsible("高级筛选 (JSON)", false, () =>
      h("div", { class: "form-field" },
        h("label", { for: "queryFilter" }, "筛选条件 (JSON)"),
        h("textarea", { id: "queryFilter", placeholder: '{"field":"amount","op":"gte","value":1000}', style: { minHeight: "60px" } })
      )
    ));
    list.appendChild(h("div", { id: "recordTable", class: "mt-md" },
      App.emptyState("等待查询", "选择业务表后点「查询」。")
    ));

    const editor = h("div", { class: "card" });
    editor.appendChild(h("div", { class: "card-header" }, h("h3", {}, t("Record Editor"))));
    editor.appendChild(h("div", { class: "form-row" },
      this.field("recordId", t("Record ID"), "text", "留空自动创建"),
      this.field("recordTitle", t("Title"), "text", ""),
      this.field("recordTags", t("Tags") + "（逗号分隔）", "text", "q1, imported")
    ));
    editor.appendChild(h("div", { id: "recordFieldForm", class: "mt-sm" },
      h("p", { class: "card-desc" }, "选择业务表后，这里会变成字段表单。")
    ));
    editor.appendChild(App.collapsible("Data JSON", false, () =>
      h("div", { class: "form-field" },
        h("label", { for: "recordData" }, t("Data JSON")),
        h("textarea", { id: "recordData", placeholder: '{\n  "customer": "Acme",\n  "amount": 8800\n}' })
      )
    ));
    editor.appendChild(h("div", { class: "btn-group mt-sm" },
      h("button", { class: "sm", type: "button", onclick: () => this.validate() }, t("Validate")),
      h("button", { class: "primary", type: "button", onclick: () => this.save() }, t("Save")),
      h("button", { class: "sm", type: "button", onclick: () => this.newRecord() }, t("New record")),
      h("button", { class: "danger sm", type: "button", onclick: () => this.deleteRecord() }, t("Delete"))
    ));
    editor.appendChild(h("div", { id: "recordSaveResult", class: "mt-sm" }));

    split.appendChild(list);
    split.appendChild(editor);
    container.appendChild(split);

    container.appendChild(this.buildCollapsible("batchOps", t("Batch Operations"), false, () => {
      const body = h("div", {});
      body.appendChild(h("p", { class: "card-desc" }, "批量导入、更新或删除记录。适用于初始数据加载和定期同步。"));
      body.appendChild(h("div", { class: "btn-group" },
        h("button", { type: "button", onclick: () => this.importCSV() }, t("Import CSV")),
        h("button", { type: "button", onclick: () => this.importJSONL() }, t("Import JSONL")),
        h("button", { type: "button", onclick: () => this.bulkUpdate() }, t("Bulk update")),
        h("button", { class: "danger", type: "button", onclick: () => this.bulkDelete() }, t("Bulk delete"))
      ));
      body.appendChild(h("div", { class: "form-field mt-md" },
        h("label", { for: "batchData" }, "批量数据 (JSON Array)"),
        h("textarea", { id: "batchData", placeholder: '[{"customer":"A","amount":100}, ...]' })
      ));
      return body;
    }));

    const ds = document.getElementById("recordDataset");
    if (ds) {
      ds.addEventListener("change", () => {
        App.setDataset(ds.value);
        this._fields = [];
        this.loadForm().then(() => this.query());
      });
    }
    const recId = App.hashState().params.record;
    const openDs = App.datasetId;
    if (openDs) {
      this.loadForm().then(() => this.query()).then(() => {
        if (recId && this.dataset() === openDs) this.loadOne(recId);
      });
    }
  },

  field(id, label, type, placeholder) {
    return App.field(id, label, type, placeholder);
  },

  buildCollapsible(id, title, defaultOpen, contentFn) {
    return App.collapsible(title, defaultOpen, contentFn);
  },

  dataset() {
    return App.val("recordDataset") || App.datasetId;
  },

  async loadForm() {
    const ds = this.dataset();
    const box = document.getElementById("recordFieldForm");
    if (!box) return;
    this._formGen = (this._formGen || 0) + 1;
    const gen = this._formGen;
    if (!ds) {
      box.innerHTML = "";
      box.appendChild(App.html("p", { class: "card-desc" }, "选择业务表后显示字段表单。"));
      this._fields = [];
      return;
    }
    try {
      const fields = await App.loadFields(ds);
      if (gen !== this._formGen) return;
      this._fields = fields;
      box.innerHTML = "";
      if (!this._fields.length) {
        box.appendChild(App.html("p", { class: "card-desc" }, "该表还没有字段定义，请用右侧 JSON 或先去字段页检查。"));
        return;
      }
      box.appendChild(App.fieldControls(this._fields, {}, "rec"));
    } catch (e) {
      if (gen !== this._formGen) return;
      box.innerHTML = "";
      box.appendChild(App.emptyState("字段加载失败", e.message));
    }
  },

  collectData() {
    const fromFields = App.collectFieldValues(this._fields, "rec");
    const raw = document.getElementById("recordData")?.value || "";
    if (!raw.trim()) return fromFields;
    try {
      return Object.assign({}, App.parseJSON(raw), fromFields);
    } catch (_) {
      throw new Error("Data JSON 无效");
    }
  },

  fillEditor(rec) {
    this._selected = rec;
    App.setVal("recordId", rec.id || "");
    App.setVal("recordTitle", rec.title || "");
    App.setVal("recordTags", (rec.tags || []).join(", "));
    const data = rec.data || {};
    App.setVal("recordData", JSON.stringify(data, null, 2));
    const box = document.getElementById("recordFieldForm");
    if (box && this._fields.length) {
      box.innerHTML = "";
      box.appendChild(App.fieldControls(this._fields, data, "rec"));
    }
  },

  async query() {
    const ds = this.dataset();
    const table = document.getElementById("recordTable");
    if (!table) return;
    if (!ds) {
      table.innerHTML = "";
      table.appendChild(App.emptyState("请先选择业务表", "从顶部下拉框或开通应用后进入。"));
      return;
    }
    App.setDataset(ds);
    this._queryGen = (this._queryGen || 0) + 1;
    const gen = this._queryGen;
    if (!this._fields.length) await this.loadForm();
    if (gen !== this._queryGen) return;
    table.innerHTML = '<div class="loading-state">查询中…</div>';
    try {
      let filter;
      const filterText = document.getElementById("queryFilter")?.value || "";
      if (filterText.trim()) {
        try {
          filter = App.parseJSON(filterText);
        } catch (_) {
          table.innerHTML = "";
          table.appendChild(App.emptyState("筛选 JSON 无效", "高级筛选需要合法 JSON 对象。"));
          return;
        }
        if (!filter || typeof filter !== "object" || Array.isArray(filter)) {
          table.innerHTML = "";
          table.appendChild(App.emptyState("筛选 JSON 无效", "请使用对象，例如 {\"field\":\"amount\",\"op\":\"gte\",\"value\":1000}。"));
          return;
        }
      }
      const limit = Number(App.val("queryLimit") || 50) || 50;
      const body = {
        q: App.val("queryText"),
        tag: App.val("queryTag"),
        limit,
      };
      if (filter) body.filter = filter;
      const data = await App.api("/api/v1/data/datasets/" + encodeURIComponent(ds) + "/records/query", {
        method: "POST",
        body: JSON.stringify(body),
      });
      if (gen !== this._queryGen) return;
      const items = App.listItems(data);
      if (!items.length) {
        table.innerHTML = "";
        table.appendChild(App.emptyState("没有匹配记录", "换关键词，或新建一条。"));
        return;
      }
      const fieldKeys = this._fields.slice(0, 5).map(f => App.fieldKey(f)).filter(Boolean);
      const headers = ["ID", "标题"].concat(fieldKeys.map(k => {
        const f = this._fields.find(x => App.fieldKey(x) === k);
        return f ? App.fieldLabel(f) : k;
      })).concat(["更新时间"]);
      const rows = items.map(rec => ({
        _id: rec.id,
        _cells: [
          { text: rec.id, attrs: { class: "mono" } },
          rec.title || "-",
          ...fieldKeys.map(k => App.preview((rec.data || {})[k])),
          App.fmtTime(rec.updated_at)
        ]
      }));
      table.innerHTML = "";
      table.appendChild(App.table(headers, rows, { onRowClick: (id) => this.open(id, items) }));
    } catch (e) {
      if (gen !== this._queryGen) return;
      table.innerHTML = "";
      table.appendChild(App.emptyState("查询失败", e.message));
    }
  },

  open(id, items) {
    const rec = (items || []).find(r => r.id === id);
    if (rec) this.fillEditor(rec);
    else this.loadOne(id);
  },

  async loadOne(id) {
    const ds = this.dataset();
    if (!ds || !id) return;
    this._loadOneGen = (this._loadOneGen || 0) + 1;
    const gen = this._loadOneGen;
    try {
      const rec = await App.api("/api/v1/data/datasets/" + encodeURIComponent(ds) + "/records/" + encodeURIComponent(id));
      if (gen !== this._loadOneGen || this.dataset() !== ds) return;
      this.fillEditor(rec);
    } catch (e) {
      if (gen !== this._loadOneGen || this.dataset() !== ds) return;
      App.toast(e.message || "读取记录失败", "danger");
    }
  },

  exportCSV() { this.exportFile("csv", "records.csv"); },
  exportJSONL() { this.exportFile("jsonl", "records.jsonl"); },

  async exportFile(kind, filename) {
    const ds = this.dataset();
    if (!ds) { App.toast("请先选择业务表", "warn"); return; }
    try {
      await App.download("/api/v1/data/datasets/" + encodeURIComponent(ds) + "/records/export." + kind, {
        method: "POST",
        body: JSON.stringify({ q: App.val("queryText"), tag: App.val("queryTag"), limit: 5000 }),
        filename,
      });
      App.toast("已开始下载");
    } catch (e) {
      App.toast(e.message || "导出失败", "danger");
    }
  },

  clearQuery() {
    ["queryText", "queryTag", "queryFilter"].forEach(id => App.setVal(id, ""));
    const table = document.getElementById("recordTable");
    if (table) {
      table.innerHTML = "";
      table.appendChild(App.emptyState("等待查询", "输入关键词或筛选条件后点击「查询」。"));
    }
  },

  payload() {
    return {
      id: App.val("recordId") || undefined,
      title: App.val("recordTitle"),
      tags: App.val("recordTags").split(",").map(s => s.trim()).filter(Boolean),
      data: this.collectData(),
    };
  },

  async validate() {
    const ds = this.dataset();
    if (!ds) { App.toast("请先选择业务表", "warn"); return; }
    try {
      const out = await App.api("/api/v1/data/datasets/" + encodeURIComponent(ds) + "/records/validate", {
        method: "POST",
        body: JSON.stringify(this.payload()),
      });
      const el = document.getElementById("recordSaveResult");
      if (el) {
        el.innerHTML = "";
        el.appendChild(App.resultCard(out.valid ? "校验通过" : "校验未通过", out));
      }
    } catch (e) {
      App.toast(e.message || "校验失败", "danger");
    }
  },

  async save() {
    const ds = this.dataset();
    if (!ds) { App.toast("请先选择业务表", "warn"); return; }
    if (this._busy) return;
    let body;
    try {
      body = this.payload();
    } catch (e) {
      App.toast(e.message || "保存失败", "danger");
      return;
    }
    this._busy = true;
    try {
      let out;
      if (body.id) {
        out = await App.api("/api/v1/data/datasets/" + encodeURIComponent(ds) + "/records/" + encodeURIComponent(body.id), {
          method: "PATCH",
          body: JSON.stringify({ title: body.title, tags: body.tags, data: body.data }),
        });
      } else {
        out = await App.api("/api/v1/data/datasets/" + encodeURIComponent(ds) + "/records", {
          method: "POST",
          body: JSON.stringify(body),
        });
        if (out.id) App.setVal("recordId", out.id);
      }
      if (this.dataset() !== ds) return;
      const savedId = out.id || body.id;
      if (savedId) App.writeHash("records", { dataset: ds, record: savedId });
      App.toast("已保存 " + (savedId || ""));
      this.query();
    } catch (e) {
      App.toast(e.message || "保存失败", "danger");
    } finally {
      this._busy = false;
    }
  },

  newRecord() {
    this._selected = null;
    ["recordId", "recordTitle", "recordTags", "recordData"].forEach(id => App.setVal(id, ""));
    const box = document.getElementById("recordFieldForm");
    if (box && this._fields.length) {
      box.innerHTML = "";
      box.appendChild(App.fieldControls(this._fields, {}, "rec"));
    }
  },

  async deleteRecord() {
    const ds = this.dataset();
    const id = App.val("recordId");
    if (!ds || !id) { App.toast("请先打开一条记录", "warn"); return; }
    if (!confirm("确定删除此记录？")) return;
    try {
      await App.api("/api/v1/data/datasets/" + encodeURIComponent(ds) + "/records/" + encodeURIComponent(id), {
        method: "DELETE",
      });
      App.toast("已删除");
      this.newRecord();
      this.query();
    } catch (e) {
      App.toast(e.message || "删除失败", "danger");
    }
  },

  async importCSV() { await this.importKind("csv"); },
  async importJSONL() { await this.importKind("jsonl"); },

  async importKind(kind) {
    const ds = this.dataset();
    if (!ds) { App.toast("请先选择业务表", "warn"); return; }
    const raw = document.getElementById("batchData")?.value || "";
    if (!raw.trim()) { App.toast("请粘贴导入内容", "warn"); return; }
    const path = kind === "csv"
      ? "/api/v1/data/datasets/" + encodeURIComponent(ds) + "/records/import.csv"
      : "/api/v1/data/datasets/" + encodeURIComponent(ds) + "/records/import.jsonl";
    try {
      const body = kind === "csv" ? { csv: raw } : { jsonl: raw };
      const out = await App.api(path, { method: "POST", body: JSON.stringify(body) });
      App.toast("导入完成");
      const el = document.getElementById("recordSaveResult");
      if (el) {
        el.innerHTML = "";
        el.appendChild(App.resultCard("导入结果", out));
      }
      this.query();
    } catch (e) {
      App.toast(e.message || "导入失败", "danger");
    }
  },

  async bulkUpdate() {
    const ds = this.dataset();
    if (!ds) { App.toast("请先选择业务表", "warn"); return; }
    let records;
    try { records = App.parseJSON(document.getElementById("batchData")?.value, null); }
    catch (_) { App.toast("批量数据不是合法 JSON", "danger"); return; }
    if (!Array.isArray(records)) { App.toast("批量更新需要 JSON 数组", "warn"); return; }
    const payload = records.map(item => {
      if (item && item.data && typeof item.data === "object") return item;
      return { data: item };
    });
    try {
      const out = await App.api("/api/v1/data/datasets/" + encodeURIComponent(ds) + "/records/batch", {
        method: "POST",
        body: JSON.stringify({ records: payload }),
      });
      App.toast("批量写入完成");
      const el = document.getElementById("recordSaveResult");
      if (el) {
        el.innerHTML = "";
        el.appendChild(App.resultCard("批量更新", out));
      }
      this.query();
    } catch (e) {
      App.toast(e.message || "批量更新失败", "danger");
    }
  },

  async bulkDelete() {
    const ds = this.dataset();
    if (!ds) { App.toast("请先选择业务表", "warn"); return; }
    if (!confirm("确定批量删除匹配的记录？")) return;
    try {
      const out = await App.api("/api/v1/data/datasets/" + encodeURIComponent(ds) + "/records/bulk-delete", {
        method: "POST",
        body: JSON.stringify({
          query: { q: App.val("queryText"), tag: App.val("queryTag") },
          confirm: true,
        }),
      });
      App.toast("批量删除已提交");
      const el = document.getElementById("recordSaveResult");
      if (el) {
        el.innerHTML = "";
        el.appendChild(App.resultCard("批量删除", out));
      }
      this.query();
    } catch (e) {
      App.toast(e.message || "批量删除失败", "danger");
    }
  },
};

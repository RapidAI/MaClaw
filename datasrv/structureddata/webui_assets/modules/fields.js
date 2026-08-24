// MaClawDataSrv - Fields Module
"use strict";

PageModules.fields = {
  render(container) {
    const h = App.html;

    container.appendChild(h("div", { class: "page-header" },
      h("div", {},
        h("h1", {}, "字段管理"),
        h("div", { class: "subtitle" }, "按字段定义看表结构。改结构请走提案，不要直接改生产字段。")
      ),
      h("div", { class: "header-actions" },
        h("button", { class: "ghost sm", type: "button", onclick: () => this.refresh() }, "刷新")
      )
    ));

    container.appendChild(h("div", { class: "card" },
      h("div", { class: "card-header" },
        h("h3", {}, "当前字段"),
        h("div", { class: "text-muted text-sm" }, "选择数据集后查看其字段定义")
      ),
      h("div", { class: "form-row" },
        App.datasetSelect("fieldDataset", "数据集")
      ),
      h("div", { class: "btn-group mt-sm" },
        h("button", { class: "primary", type: "button", onclick: () => this.refresh() }, "加载字段")
      ),
      h("div", { id: "fieldTable", class: "mt-md" },
        App.emptyState(
          "尚未选择数据集",
          "在顶部选择当前表，或从记录页点进来。",
          [{ label: "前往数据集", onclick: () => App.navigate("datasets") }]
        )
      )
    ));

    container.appendChild(App.collapsible("结构改进提案", false, () => {
      const body = h("div", {});
      body.appendChild(h("p", { class: "card-desc" }, "Agent 观察到新字段时会生成提案，管理员确认后再应用。"));
      body.appendChild(h("div", { class: "btn-group" },
        h("button", { type: "button", onclick: () => this.loadProposals() }, "加载提案"),
        h("button", { class: "sm ghost", type: "button", onclick: () => this.proposeSchema() }, "从样例生成提案")
      ));
      body.appendChild(h("div", { class: "form-field mt-sm" },
        h("label", { for: "proposalSample" }, "样例 JSON（用于生成提案）"),
        h("textarea", { id: "proposalSample", placeholder: '{"new_field": "value"}' })
      ));
      body.appendChild(h("div", { id: "proposalList", class: "mt-sm" }));
      return body;
    }));

    const ds = document.getElementById("fieldDataset");
    if (ds) {
      ds.addEventListener("change", () => {
        App.setDataset(ds.value);
        this.refresh();
      });
    }
    this.refresh();
  },

  currentDataset() {
    return App.val("fieldDataset") || App.datasetId;
  },

  async refresh() {
    const alive = App.navGuard();
    const ds = this.currentDataset();
    const el = document.getElementById("fieldTable");
    if (!el) return;
    if (!ds) {
      el.innerHTML = "";
      el.appendChild(App.emptyState("尚未选择数据集", "请先选择业务表。"));
      return;
    }
    App.setDataset(ds);
    this._loadGen = (this._loadGen || 0) + 1;
    const gen = this._loadGen;
    el.innerHTML = '<div class="loading-state">加载中…</div>';
    try {
      const data = await App.api("/api/v1/data/datasets/" + encodeURIComponent(ds) + "/fields?limit=200");
      if (!alive() || gen !== this._loadGen) return;
      const fields = App.listItems(data, "fields");
      if (!fields.length) {
        el.innerHTML = "";
        el.appendChild(App.emptyState("无字段定义", "该数据集尚未配置字段结构。"));
        return;
      }
      const rows = fields.map(f => ({
        _cells: [
          { text: App.fieldKey(f), attrs: { class: "mono" } },
          App.fieldLabel(f),
          f.type || "-",
          f.required ? App.badge("必填", "warn") : App.badge("可选"),
          f.sensitive ? App.badge("敏感", "danger") : App.badge("普通")
        ]
      }));
      el.innerHTML = "";
      el.appendChild(App.table(["字段", "标题", "类型", "约束", "敏感"], rows));
    } catch (e) {
      if (!alive() || gen !== this._loadGen) return;
      el.innerHTML = "";
      el.appendChild(App.emptyState("加载失败", e.message || "请确认数据集 ID 是否正确。"));
    }
  },

  async loadProposals() {
    const ds = this.currentDataset();
    const el = document.getElementById("proposalList");
    if (!el) return;
    if (!ds) { App.toast("请先选择数据集", "warn"); return; }
    try {
      const data = await App.api("/api/v1/data/datasets/" + encodeURIComponent(ds) + "/schema-proposals?limit=50");
      const items = App.listItems(data);
      if (!items.length) {
        el.innerHTML = "";
        el.appendChild(App.emptyState("暂无结构提案", "可以从样例 JSON 生成一条提案。"));
        return;
      }
      const rows = items.map(p => ({
        _cells: [
          p.id || "-",
          p.status || "-",
          String((p.fields || p.proposed_fields || []).length),
          App.fmtTime(p.created_at)
        ]
      }));
      el.innerHTML = "";
      el.appendChild(App.table(["提案", "状态", "字段数", "时间"], rows));
    } catch (e) {
      el.innerHTML = "";
      el.appendChild(App.emptyState("加载提案失败", e.message));
    }
  },

  async proposeSchema() {
    const ds = this.currentDataset();
    if (!ds) { App.toast("请先选择数据集", "warn"); return; }
    let sample;
    try {
      sample = App.parseJSON(App.val("proposalSample") || document.getElementById("proposalSample")?.value, null);
    } catch (e) {
      App.toast("样例 JSON 无效", "danger");
      return;
    }
    if (!sample || typeof sample !== "object") { App.toast("请提供样例 JSON", "warn"); return; }
    try {
      const out = await App.api("/api/v1/data/datasets/" + encodeURIComponent(ds) + "/schema-proposals", {
        method: "POST",
        body: JSON.stringify({ sample_data: sample }),
      });
      App.toast("已生成提案 " + (out.id || ""));
      this.loadProposals();
    } catch (e) {
      App.toast(e.message || "生成提案失败", "danger");
    }
  }
};

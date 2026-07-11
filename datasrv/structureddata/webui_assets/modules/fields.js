// MaClawDataSrv - Fields Module
"use strict";

PageModules.fields = {
  render(container) {
    const h = App.html;

    container.appendChild(h("div", { class: "page-header" },
      h("div", {},
        h("h1", {}, "字段管理"),
        h("div", { class: "subtitle" }, "维护数据集字段定义和受控的结构改进提案")
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
        h("div", { class: "form-field" },
          h("label", { for: "fieldDataset" }, "数据集"),
          h("input", { id: "fieldDataset", placeholder: "sales.orders", spellcheck: "false" })
        ),
        h("div", { class: "form-field", style: { alignSelf: "end" } },
          h("button", { class: "primary", type: "button", onclick: () => this.refresh() }, "加载字段")
        )
      ),
      h("div", { id: "fieldTable", class: "mt-md" },
        App.emptyState(
          "尚未选择数据集",
          "输入数据集 ID 后点击「加载字段」，或从数据集页面选择后返回。",
          [{ label: "前往数据集", onclick: () => App.navigate("datasets") }]
        )
      )
    ));

    container.appendChild(App.collapsible("结构改进提案", false, () => {
      const body = h("div", {});
      body.appendChild(h("p", { class: "card-desc" }, "当 Agent 建议修改数据结构时，会生成提案供管理员审批。"));
      body.appendChild(h("div", { class: "btn-group" },
        h("button", { type: "button", onclick: () => this.loadProposals() }, "加载提案"),
        h("button", { class: "sm ghost", type: "button", onclick: () => this.proposeSchema() }, "生成新提案")
      ));
      body.appendChild(h("div", { id: "proposalList", class: "mt-sm" }));
      return body;
    }));
  },

  async refresh() {
    const ds = document.getElementById("fieldDataset")?.value?.trim();
    const el = document.getElementById("fieldTable");
    if (!el) return;
    if (!ds) {
      el.innerHTML = "";
      el.appendChild(App.emptyState("尚未选择数据集", "请先填写数据集 ID。"));
      return;
    }
    el.innerHTML = '<div class="loading-state">加载中…</div>';
    try {
      const data = await App.api("/api/v1/data/datasets/" + encodeURIComponent(ds));
      const fields = data.fields || data.schema?.fields || data.dataset?.fields || [];
      if (!fields.length) {
        el.innerHTML = "";
        el.appendChild(App.emptyState("无字段定义", "该数据集尚未配置字段结构。"));
        return;
      }
      const rows = fields.map(f => ({
        _cells: [
          { text: f.name || f.id || "-", attrs: { class: "mono" } },
          f.type || f.data_type || "-",
          f.required ? App.badge("必填", "warn") : App.badge("可选"),
          f.description || f.label || "-"
        ]
      }));
      el.innerHTML = "";
      el.appendChild(App.table(["字段名", "类型", "约束", "说明"], rows));
    } catch (e) {
      el.innerHTML = "";
      el.appendChild(App.emptyState("加载失败", e.message || "请确认数据集 ID 是否正确。"));
    }
  },

  loadProposals() { App.toast("加载提案…"); },
  proposeSchema() { App.toast("生成提案…"); },
};

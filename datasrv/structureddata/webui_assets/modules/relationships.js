// MaClawDataSrv - Relationships Module
"use strict";

PageModules.relationships = {
  render(container) {
    const h = App.html;
    container.appendChild(h("div", { class: "page-header" },
      h("div", {},
        h("h1", {}, "数据关联"),
        h("div", { class: "subtitle" }, "表与表之间的引用。日常录入时在记录页用关联字段即可。")
      ),
      h("div", { class: "header-actions" },
        h("button", { class: "ghost sm", type: "button", onclick: () => this.refresh() }, "刷新")
      )
    ));

    container.appendChild(h("div", { class: "card" },
      h("div", { class: "form-row" },
        App.datasetSelect("relDataset", "数据集筛选")
      ),
      h("div", { class: "btn-group mt-sm" },
        h("button", { class: "primary", type: "button", onclick: () => this.refresh() }, "查询")
      )
    ));

    container.appendChild(h("div", { id: "relTable", class: "mt-sm" },
      h("div", { class: "loading-state" }, "加载中…")
    ));
    const ds = document.getElementById("relDataset");
    if (ds) {
      ds.addEventListener("change", () => {
        App.setDataset(ds.value);
        this.refresh();
      });
    }
    this.refresh();
  },

  async refresh() {
    const alive = App.navGuard();
    try {
      const ds = App.val("relDataset") || App.datasetId;
      const data = await App.api("/api/v1/data/relationships" + App.qs({ dataset_id: ds, limit: 200 }));
      if (!alive()) return;
      const items = App.listItems(data, "relationships");
      const el = document.getElementById("relTable");
      if (!el) return;
      if (!items.length) {
        el.innerHTML = "";
        el.appendChild(App.emptyState(
          "暂无关联关系",
          "带 record_ref 的字段会在这里显示引用路径。"
        ));
        return;
      }
      const rows = items.map(r => ({
        _cells: [
          { text: r.source_dataset_id || r.source_dataset || "-", attrs: { class: "mono" } },
          r.source_field || r.source_title || "-",
          { text: r.target_dataset_id || r.target_dataset || "-", attrs: { class: "mono" } },
          r.field_type || r.type || "-"
        ]
      }));
      el.innerHTML = "";
      el.appendChild(App.table(["源表", "字段", "目标表", "类型"], rows));
    } catch (e) {
      if (!alive()) return;
      const el = document.getElementById("relTable");
      if (el) {
        el.innerHTML = "";
        el.appendChild(App.emptyState("加载失败", e.message));
      }
    }
  }
};

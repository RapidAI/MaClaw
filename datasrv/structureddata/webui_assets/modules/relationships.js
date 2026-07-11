// MaClawDataSrv - Relationships Module
"use strict";

PageModules.relationships = {
  render(container) {
    const h = App.html;
    container.appendChild(h("div", { class: "page-header" },
      h("div", {},
        h("h1", {}, "数据关联"),
        h("div", { class: "subtitle" }, "查看业务数据集和记录之间的受控关联关系")
      ),
      h("div", { class: "header-actions" },
        h("button", { class: "ghost sm", type: "button", onclick: () => this.refresh() }, "刷新")
      )
    ));

    container.appendChild(h("div", { class: "card" },
      h("div", { class: "form-row" },
        h("div", { class: "form-field" },
          h("label", { for: "relDataset" }, "数据集筛选"),
          h("input", { id: "relDataset", placeholder: "如：sales.orders" })
        ),
        h("div", { class: "form-field", style: { alignSelf: "end" } },
          h("button", { class: "primary", type: "button", onclick: () => this.refresh() }, "查询")
        )
      )
    ));

    container.appendChild(h("div", { id: "relTable", class: "mt-sm" },
      h("div", { class: "loading-state" }, "加载中…")
    ));
    this.refresh();
  },

  async refresh() {
    try {
      const ds = document.getElementById("relDataset")?.value || "";
      const data = await App.api("/api/v1/data/relationships" + (ds ? "?dataset=" + encodeURIComponent(ds) : ""));
      const el = document.getElementById("relTable");
      if (!el) return;
      if (!data.relationships || !data.relationships.length) {
        el.innerHTML = "";
        el.appendChild(App.emptyState(
          "暂无关联关系",
          "关联定义数据集之间的引用路径。创建带外键语义的字段后可在此查看。"
        ));
        return;
      }
      const rows = data.relationships.map(r => ({
        _cells: [
          { text: r.source_dataset || "-", attrs: { class: "mono" } },
          { text: r.target_dataset || "-", attrs: { class: "mono" } },
          r.type || "-",
          r.direction || "-"
        ]
      }));
      el.innerHTML = "";
      el.appendChild(App.table(["源数据集", "目标数据集", "关系类型", "方向"], rows));
    } catch (e) {
      const el = document.getElementById("relTable");
      if (el) {
        el.innerHTML = "";
        el.appendChild(App.emptyState("加载失败", e.message));
      }
    }
  }
};

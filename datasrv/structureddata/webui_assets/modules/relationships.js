// MaClawDataSrv - Relationships Module
"use strict";

PageModules.relationships = {
  render(container) {
    const h = App.html;
    container.appendChild(h("div", { class: "page-header" },
      h("div", {}, h("h1", {}, "数据关联"), h("div", { class: "subtitle" }, "查看业务数据集和记录之间的受控关联关系")),
      h("button", { class: "ghost sm", onclick: () => this.refresh() }, "刷新")
    ));

    // Filters
    container.appendChild(h("div", { class: "card" },
      h("div", { class: "form-row" },
        h("div", { class: "form-field" }, h("label", {}, "数据集筛选"), h("input", { id: "relDataset", placeholder: "如：sales.orders" })),
        h("div", { class: "form-field", style: { alignSelf: "end" } },
          h("button", { class: "primary", onclick: () => this.refresh() }, "查询")
        )
      )
    ));

    container.appendChild(h("div", { id: "relTable" }, h("div", { class: "empty-state" }, "加载中...")));
    this.refresh();
  },

  async refresh() {
    try {
      const ds = document.getElementById("relDataset")?.value || "";
      const data = await App.api("/api/v1/data/relationships" + (ds ? "?dataset=" + encodeURIComponent(ds) : ""));
      const el = document.getElementById("relTable");
      if (!el) return;
      if (!data.relationships || !data.relationships.length) { el.innerHTML = '<div class="empty-state">暂无关联关系</div>'; return; }
      const h = App.html;
      const tbl = document.createElement("table");
      tbl.appendChild(h("thead", {}, h("tr", {},
        h("th", {}, "源数据集"), h("th", {}, "目标数据集"), h("th", {}, "关系类型"), h("th", {}, "方向")
      )));
      const tbody = document.createElement("tbody");
      data.relationships.forEach(r => {
        tbody.appendChild(h("tr", {},
          h("td", { class: "mono" }, r.source_dataset || "-"),
          h("td", { class: "mono" }, r.target_dataset || "-"),
          h("td", {}, r.type || "-"),
          h("td", {}, r.direction || "-")
        ));
      });
      tbl.appendChild(tbody);
      el.innerHTML = "";
      const wrap = h("div", { class: "table-wrap" });
      wrap.appendChild(tbl);
      el.appendChild(wrap);
    } catch(e) { document.getElementById("relTable").innerHTML = '<div class="empty-state">加载失败</div>'; }
  }
};

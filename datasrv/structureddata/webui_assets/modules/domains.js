// MaClawDataSrv - Business Domains Module
"use strict";

PageModules.domains = {
  render(container) {
    const h = App.html;

    container.appendChild(h("div", { class: "page-header" },
      h("div", {},
        h("h1", {}, "业务域"),
        h("div", { class: "subtitle" }, "发现和管理业务能力域（销售、财务、人事、法务、采购等）")
      ),
      h("button", { class: "ghost sm", onclick: () => this.refresh() }, "刷新")
    ));

    // Domain readiness grid
    container.appendChild(h("div", { class: "card" },
      h("div", { class: "card-header" }, h("h3", {}, "域就绪度")),
      h("p", { class: "card-desc" }, "每个业务域的模板覆盖、数据集和能力状态"),
      h("div", { id: "domainGrid", class: "status-banner" },
        h("div", { class: "loading-state" }, "加载中…")
      )
    ));

    // Domain list
    container.appendChild(h("div", { class: "card" },
      h("div", { class: "card-header" }, h("h3", {}, "域列表")),
      h("div", { id: "domainList", class: "mt-sm" },
        h("div", { class: "loading-state" }, "加载中…")
      )
    ));

    this.refresh();
  },

  async refresh() {
    try {
      const data = await App.api("/api/v1/data/domains");
      const el = document.getElementById("domainList");
      if (!el) return;
      if (!data.domains || !data.domains.length) {
        el.innerHTML = "";
        el.appendChild(App.emptyState(
          "暂无业务域",
          "请先在「数据集」模块创建数据集，系统将按域 ID 自动归类。",
          [{ label: "前往数据集", primary: true, onclick: () => App.navigate("datasets") }]
        ));
        return;
      }
      const h = App.html;
      const tbl = document.createElement("table");
      tbl.appendChild(h("thead", {}, h("tr", {},
        h("th", {}, "域 ID"), h("th", {}, "名称"), h("th", {}, "数据集数"), h("th", {}, "能力数"), h("th", {}, "状态")
      )));
      const tbody = document.createElement("tbody");
      data.domains.forEach(d => {
        tbody.appendChild(h("tr", {},
          h("td", { class: "mono" }, d.id || "-"),
          h("td", {}, d.name || d.id || "-"),
          h("td", {}, String(d.dataset_count || 0)),
          h("td", {}, String(d.capability_count || 0)),
          h("td", {}, App.badge("就绪", "ok"))
        ));
      });
      tbl.appendChild(tbody);
      el.innerHTML = "";
      const wrap = h("div", { class: "table-wrap" });
      wrap.appendChild(tbl);
      el.appendChild(wrap);
    } catch (e) {
      const el = document.getElementById("domainList");
      if (el) {
        el.innerHTML = "";
        el.appendChild(App.emptyState("加载失败", e.message || "请检查网络或权限后重试。"));
      }
    }
  }
};

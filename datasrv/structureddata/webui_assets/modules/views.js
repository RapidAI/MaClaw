// MaClawDataSrv - Views, Dashboards, Reports Module
"use strict";

function renderResourceList(el, items, emptyTitle, emptyDesc, onClick) {
  if (!el) return;
  if (!items || !items.length) {
    el.innerHTML = "";
    el.appendChild(App.emptyState(emptyTitle, emptyDesc));
    return;
  }
  const h = App.html;
  el.innerHTML = "";
  items.forEach(item => {
    const id = item.id || "-";
    const desc = item.description || item.name || "";
    el.appendChild(h("div", {
      class: "list-row",
      role: "button",
      tabindex: "0",
      onclick: () => onClick(id),
      onkeydown: (e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); onClick(id); } }
    },
      h("div", {},
        h("div", { class: "list-title mono" }, id),
        h("div", { class: "list-meta" }, desc)
      ),
      App.badge("打开", "brand")
    ));
  });
}

PageModules.views = {
  render(container) {
    const h = App.html;
    container.appendChild(h("div", { class: "page-header" },
      h("div", {},
        h("h1", {}, "业务视图"),
        h("div", { class: "subtitle" }, "查询受控业务视图，不暴露原始数据集细节")
      ),
      h("div", { class: "header-actions" },
        h("button", { class: "ghost sm", type: "button", onclick: () => this.refresh() }, "刷新")
      )
    ));
    container.appendChild(h("div", { class: "card" },
      h("div", { class: "card-header" }, h("h3", {}, "可用视图")),
      h("div", { id: "viewList" }, h("div", { class: "loading-state" }, "加载中…"))
    ));
    this.refresh();
  },
  async refresh() {
    try {
      const data = await App.api("/api/v1/data/business-views");
      renderResourceList(
        document.getElementById("viewList"),
        data.views,
        "暂无业务视图",
        "视图由业务域模板或管理员配置生成，用于安全的只读分析。",
        (id) => this.query(id)
      );
    } catch (e) {
      const el = document.getElementById("viewList");
      if (el) { el.innerHTML = ""; el.appendChild(App.emptyState("加载失败", e.message)); }
    }
  },
  query(id) { App.toast("查询视图: " + id); }
};

PageModules.dashboards = {
  render(container) {
    const h = App.html;
    container.appendChild(h("div", { class: "page-header" },
      h("div", {},
        h("h1", {}, "仪表盘"),
        h("div", { class: "subtitle" }, "运行运营仪表盘摘要，按公司或业务域维度聚合")
      ),
      h("div", { class: "header-actions" },
        h("button", { class: "ghost sm", type: "button", onclick: () => this.refresh() }, "刷新")
      )
    ));
    container.appendChild(h("div", { class: "card" },
      h("div", { class: "card-header" }, h("h3", {}, "可用仪表盘")),
      h("div", { id: "dashList" }, h("div", { class: "loading-state" }, "加载中…"))
    ));
    this.refresh();
  },
  async refresh() {
    try {
      const data = await App.api("/api/v1/data/dashboards");
      renderResourceList(
        document.getElementById("dashList"),
        data.dashboards,
        "暂无仪表盘",
        "仪表盘提供跨数据集的运营摘要，可从快速操作手册了解配置方式。",
        (id) => this.run(id)
      );
    } catch (e) {
      const el = document.getElementById("dashList");
      if (el) { el.innerHTML = ""; el.appendChild(App.emptyState("加载失败", e.message)); }
    }
  },
  run(id) { App.toast("运行仪表盘: " + id); }
};

PageModules.reports = {
  render(container) {
    const h = App.html;
    container.appendChild(h("div", { class: "page-header" },
      h("div", {},
        h("h1", {}, "报表"),
        h("div", { class: "subtitle" }, "运行内置报表和受控聚合分析")
      ),
      h("div", { class: "header-actions" },
        h("button", { class: "ghost sm", type: "button", onclick: () => this.refresh() }, "刷新")
      )
    ));
    container.appendChild(h("div", { class: "card" },
      h("div", { class: "card-header" }, h("h3", {}, "可用报表")),
      h("div", { id: "reportList" }, h("div", { class: "loading-state" }, "加载中…"))
    ));
    this.refresh();
  },
  async refresh() {
    try {
      const data = await App.api("/api/v1/data/reports");
      renderResourceList(
        document.getElementById("reportList"),
        data.reports,
        "暂无报表",
        "报表用于周期性业务分析。创建数据集并初始化 MIS 后可获得内置报表。",
        (id) => this.run(id)
      );
    } catch (e) {
      const el = document.getElementById("reportList");
      if (el) { el.innerHTML = ""; el.appendChild(App.emptyState("加载失败", e.message)); }
    }
  },
  run(id) { App.toast("运行报表: " + id); }
};

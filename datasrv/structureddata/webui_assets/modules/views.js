// MaClawDataSrv - Views, Dashboards, Reports Module
"use strict";

PageModules.views = {
  render(container) {
    const h = App.html;
    container.appendChild(h("div", { class: "page-header" },
      h("div", {}, h("h1", {}, "业务视图"), h("div", { class: "subtitle" }, "查询受控业务视图，不暴露原始数据集细节")),
      h("button", { class: "ghost sm", onclick: () => this.refresh() }, "刷新")
    ));
    container.appendChild(h("div", { class: "card" },
      h("div", { class: "card-header" }, h("h3", {}, "可用视图")),
      h("div", { id: "viewList" }, h("div", { class: "empty-state" }, "加载中..."))
    ));
    this.refresh();
  },
  async refresh() {
    try {
      const data = await App.api("/api/v1/data/business-views");
      const el = document.getElementById("viewList");
      if (!el) return;
      if (!data.views || !data.views.length) { el.innerHTML = '<div class="empty-state">暂无业务视图</div>'; return; }
      const h = App.html;
      el.innerHTML = "";
      data.views.forEach(v => {
        el.appendChild(h("div", { class: "card", style: { marginBottom: "6px", cursor: "pointer" }, onclick: () => this.query(v.id) },
          h("strong", {}, v.id),
          h("span", { class: "text-muted text-sm", style: { marginLeft: "12px" } }, v.description || "")
        ));
      });
    } catch(e) { document.getElementById("viewList").innerHTML = '<div class="empty-state">加载失败</div>'; }
  },
  query(id) { App.toast("查询视图: " + id); }
};

PageModules.dashboards = {
  render(container) {
    const h = App.html;
    container.appendChild(h("div", { class: "page-header" },
      h("div", {}, h("h1", {}, "仪表盘"), h("div", { class: "subtitle" }, "运行运营仪表盘摘要，按公司或业务域维度聚合")),
      h("button", { class: "ghost sm", onclick: () => this.refresh() }, "刷新")
    ));
    container.appendChild(h("div", { class: "card" },
      h("div", { class: "card-header" }, h("h3", {}, "可用仪表盘")),
      h("div", { id: "dashList" }, h("div", { class: "empty-state" }, "加载中..."))
    ));
    this.refresh();
  },
  async refresh() {
    try {
      const data = await App.api("/api/v1/data/dashboards");
      const el = document.getElementById("dashList");
      if (!el) return;
      if (!data.dashboards || !data.dashboards.length) { el.innerHTML = '<div class="empty-state">暂无仪表盘</div>'; return; }
      const h = App.html;
      el.innerHTML = "";
      data.dashboards.forEach(d => {
        el.appendChild(h("div", { class: "card", style: { marginBottom: "6px", cursor: "pointer" }, onclick: () => this.run(d.id) },
          h("strong", {}, d.id),
          h("span", { class: "text-muted text-sm", style: { marginLeft: "12px" } }, d.description || "")
        ));
      });
    } catch(e) { document.getElementById("dashList").innerHTML = '<div class="empty-state">加载失败</div>'; }
  },
  run(id) { App.toast("运行仪表盘: " + id); }
};

PageModules.reports = {
  render(container) {
    const h = App.html;
    container.appendChild(h("div", { class: "page-header" },
      h("div", {}, h("h1", {}, "报表"), h("div", { class: "subtitle" }, "运行内置报表和受控聚合分析")),
      h("button", { class: "ghost sm", onclick: () => this.refresh() }, "刷新")
    ));
    container.appendChild(h("div", { class: "card" },
      h("div", { class: "card-header" }, h("h3", {}, "可用报表")),
      h("div", { id: "reportList" }, h("div", { class: "empty-state" }, "加载中..."))
    ));
    this.refresh();
  },
  async refresh() {
    try {
      const data = await App.api("/api/v1/data/reports");
      const el = document.getElementById("reportList");
      if (!el) return;
      if (!data.reports || !data.reports.length) { el.innerHTML = '<div class="empty-state">暂无报表</div>'; return; }
      const h = App.html;
      el.innerHTML = "";
      data.reports.forEach(r => {
        el.appendChild(h("div", { class: "card", style: { marginBottom: "6px", cursor: "pointer" }, onclick: () => this.run(r.id) },
          h("strong", {}, r.id),
          h("span", { class: "text-muted text-sm", style: { marginLeft: "12px" } }, r.description || "")
        ));
      });
    } catch(e) { document.getElementById("reportList").innerHTML = '<div class="empty-state">加载失败</div>'; }
  },
  run(id) { App.toast("运行报表: " + id); }
};

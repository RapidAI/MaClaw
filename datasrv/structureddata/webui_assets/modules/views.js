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
    const desc = item.description || item.title || item.name || "";
    el.appendChild(h("div", {
      class: "list-row",
      role: "button",
      tabindex: "0",
      onclick: () => onClick(id, item),
      onkeydown: (e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); onClick(id, item); } }
    },
      h("div", {},
        h("div", { class: "list-title" }, item.title || id),
        h("div", { class: "list-meta" }, id + (desc ? " · " + desc : ""))
      ),
      App.badge("打开", "brand")
    ));
  });
}

function renderRecords(el, records, title) {
  if (!el) return;
  el.innerHTML = "";
  if (!records || !records.length) {
    el.appendChild(App.emptyState("没有数据", title || "查询结果为空"));
    return;
  }
  const keys = [];
  records.forEach(rec => Object.keys(rec.data || {}).forEach(k => {
    if (!keys.includes(k) && keys.length < 6) keys.push(k);
  }));
  const rows = records.map(rec => ({
    _cells: [
      rec.id || "-",
      rec.title || "-",
      ...keys.map(k => App.preview((rec.data || {})[k])),
    ]
  }));
  el.appendChild(App.table(["ID", "标题"].concat(keys), rows));
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
    container.appendChild(h("div", { id: "viewResult", class: "mt-sm" }));
    this.refresh();
  },
  async refresh() {
    const alive = App.navGuard();
    try {
      const data = await App.api("/api/v1/data/views");
      if (!alive()) return;
      renderResourceList(
        document.getElementById("viewList"),
        App.listItems(data, "views"),
        "暂无业务视图",
        "开通应用后会生成只读视图。",
        (id) => this.query(id)
      );
    } catch (e) {
      if (!alive()) return;
      const el = document.getElementById("viewList");
      if (el) { el.innerHTML = ""; el.appendChild(App.emptyState("加载失败", e.message)); }
    }
  },
  async query(id) {
    const alive = App.navGuard();
    const el = document.getElementById("viewResult");
    if (el) el.innerHTML = '<div class="loading-state">查询中…</div>';
    try {
      const out = await App.api("/api/v1/data/views/" + encodeURIComponent(id) + "/query", {
        method: "POST",
        body: JSON.stringify({ limit: 50 }),
      });
      if (!alive()) return;
      renderRecords(el, out.records || App.listItems(out), id);
    } catch (e) {
      if (!alive()) return;
      if (el) { el.innerHTML = ""; el.appendChild(App.emptyState("查询失败", e.message)); }
    }
  }
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
    container.appendChild(h("div", { id: "dashResult", class: "mt-sm" }));
    this.refresh();
  },
  async refresh() {
    const alive = App.navGuard();
    try {
      const data = await App.api("/api/v1/data/dashboards");
      if (!alive()) return;
      renderResourceList(
        document.getElementById("dashList"),
        App.listItems(data, "dashboards"),
        "暂无仪表盘",
        "开通应用后可获得域级运营摘要。",
        (id) => this.run(id)
      );
    } catch (e) {
      if (!alive()) return;
      const el = document.getElementById("dashList");
      if (el) { el.innerHTML = ""; el.appendChild(App.emptyState("加载失败", e.message)); }
    }
  },
  async run(id) {
    const alive = App.navGuard();
    const el = document.getElementById("dashResult");
    if (el) el.innerHTML = '<div class="loading-state">运行中…</div>';
    try {
      const out = await App.api("/api/v1/data/dashboards/" + encodeURIComponent(id) + "/run", { method: "POST" });
      if (!alive()) return;
      if (el) {
        el.innerHTML = "";
        el.appendChild(App.resultCard(id, out));
      }
    } catch (e) {
      if (!alive()) return;
      if (el) { el.innerHTML = ""; el.appendChild(App.emptyState("运行失败", e.message)); }
    }
  }
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
    container.appendChild(h("div", { id: "reportResult", class: "mt-sm" }));
    this.refresh();
  },
  async refresh() {
    const alive = App.navGuard();
    try {
      const data = await App.api("/api/v1/data/reports");
      if (!alive()) return;
      renderResourceList(
        document.getElementById("reportList"),
        App.listItems(data, "reports"),
        "暂无报表",
        "开通应用后可获得内置报表。",
        (id) => this.run(id)
      );
    } catch (e) {
      if (!alive()) return;
      const el = document.getElementById("reportList");
      if (el) { el.innerHTML = ""; el.appendChild(App.emptyState("加载失败", e.message)); }
    }
  },
  async run(id) {
    const alive = App.navGuard();
    const el = document.getElementById("reportResult");
    if (el) el.innerHTML = '<div class="loading-state">运行中…</div>';
    try {
      const out = await App.api("/api/v1/data/reports/" + encodeURIComponent(id) + "/run", {
        method: "POST",
        body: JSON.stringify({}),
      });
      if (!alive()) return;
      if (el) {
        el.innerHTML = "";
        el.appendChild(App.resultCard(id, out));
      }
    } catch (e) {
      if (!alive()) return;
      if (el) { el.innerHTML = ""; el.appendChild(App.emptyState("运行失败", e.message)); }
    }
  }
};

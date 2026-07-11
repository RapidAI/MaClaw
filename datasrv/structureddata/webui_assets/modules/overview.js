// MaClawDataSrv - Overview Module
"use strict";

PageModules.overview = {
  render(container) {
    const h = App.html;
    const t = (k) => I18N.t(k);

    container.appendChild(h("div", { class: "page-header" },
      h("div", {},
        h("h1", {}, t("Overview")),
        h("div", { class: "subtitle" }, "服务状态、业务就绪度和待处理工作一览")
      ),
      h("div", { class: "header-actions" },
        h("button", { class: "ghost sm", type: "button", onclick: () => this.refresh() }, t("Refresh"))
      )
    ));

    const banner = h("div", { class: "status-banner", "data-testid": "overview-health" },
      this.stat("数据集", "-", "ovDatasets"),
      this.stat("记录总数", "-", "ovRecords"),
      this.stat("待处理项", "-", "ovPending"),
      this.stat("有效密钥", "-", "ovKeys"),
      this.stat("连接器", "-", "ovConnectors")
    );
    container.appendChild(banner);

    const quickGrid = h("div", { class: "quick-grid" });

    quickGrid.appendChild(this.quickCard("日常运营", "记录查询、业务动作和待办事项", [
      { label: "打开记录", page: "records" },
      { label: "业务动作", page: "actions" },
      { label: "收件箱", page: "inbox" },
    ]));

    quickGrid.appendChild(this.quickCard("分析", "报表、仪表盘和业务视图", [
      { label: "报表", page: "reports" },
      { label: "仪表盘", page: "dashboards" },
      { label: "视图", page: "views" },
    ]));

    quickGrid.appendChild(this.quickCard("治理", "密钥管理、质量检查和备份", [
      { label: "API 密钥", page: "apikeys" },
      { label: "质量检查", page: "quality" },
      { label: "备份", page: "backups" },
    ]));

    container.appendChild(quickGrid);

    container.appendChild(this.buildSection("监控详情", false, () => {
      const body = h("div", {});
      body.appendChild(h("div", { id: "ovHealthGrid", class: "status-banner" },
        this.stat("业务域就绪", "-", "ovDomains"),
        this.stat("集成健康", "-", "ovIntegration"),
        this.stat("访问风险", "-", "ovRisk"),
        this.stat("治理就绪", "-", "ovGovernance")
      ));
      body.appendChild(h("div", { id: "ovActivity", class: "mt-md" },
        h("div", { class: "loading-state" }, "加载中…")
      ));
      return body;
    }));

    this.refresh();
  },

  stat(label, value, id) {
    const h = App.html;
    return h("div", { class: "status-item" },
      h("div", { class: "status-label" }, label),
      h("div", { class: "status-value", id }, value)
    );
  },

  quickCard(title, desc, links) {
    const h = App.html;
    const card = h("div", { class: "card quick-card" },
      h("div", { class: "card-header" }, h("h3", {}, title)),
      h("p", { class: "card-desc" }, desc)
    );
    const btns = h("div", { class: "btn-group" });
    links.forEach(l => {
      btns.appendChild(h("button", { class: "sm", type: "button", onclick: () => App.navigate(l.page) }, l.label));
    });
    card.appendChild(btns);
    return card;
  },

  buildSection(title, defaultOpen, contentFn) {
    return App.collapsible(title, defaultOpen, contentFn);
  },

  async refresh() {
    try {
      const stats = await App.api("/api/v1/data/stats");
      const set = (id, v) => { const el = document.getElementById(id); if (el) el.textContent = v; };
      set("ovDatasets", stats.datasets ?? stats.dataset_count ?? 0);
      set("ovRecords", stats.total_records ?? 0);
      set("ovPending", stats.pending_items ?? stats.inbox_pending ?? "-");
      set("ovKeys", stats.active_keys ?? stats.api_keys ?? "-");
      set("ovConnectors", stats.connectors ?? stats.connector_count ?? "-");
    } catch (e) { /* silent on overview */ }
  }
};

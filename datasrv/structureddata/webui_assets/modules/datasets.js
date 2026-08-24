// MaClawDataSrv - Datasets Module
"use strict";

PageModules.datasets = {
  render(container) {
    const h = App.html;

    container.appendChild(h("div", { class: "page-header" },
      h("div", {},
        h("h1", {}, "数据集"),
        h("div", { class: "subtitle" }, "业务表清单。日常录入请走「记录」；这里只做建表和生命周期。")
      ),
      h("div", { class: "header-actions" },
        h("button", { class: "ghost sm", type: "button", onclick: () => this.refresh() }, "刷新")
      )
    ));

    container.appendChild(h("div", { class: "card" },
      h("div", { class: "card-header" }, h("h3", {}, "数据集列表")),
      h("div", { id: "datasetList", class: "mt-sm" },
        h("div", { class: "loading-state" }, "加载中…")
      )
    ));

    container.appendChild(this.collapsible("createDs", "创建数据集", false, () => {
      const body = h("div", {});
      body.appendChild(h("p", { class: "card-desc" }, "优先从模板创建。自定义 ID 建议使用 domain.name，例如 sales.orders。"));
      body.appendChild(h("div", { class: "form-row" },
        this.field("dsId", "数据集 ID", "text", "sales.orders"),
        this.field("dsName", "名称", "text", "orders"),
        this.field("dsTitle", "显示名称", "text", "销售订单"),
        this.field("dsDomain", "所属业务域", "text", "sales")
      ));
      body.appendChild(h("div", { class: "btn-group mt-md" },
        h("button", { class: "primary", type: "button", onclick: () => this.create() }, "创建数据集"),
        h("button", { class: "sm", type: "button", onclick: () => this.fromTemplate() }, "从模板创建")
      ));
      return body;
    }));

    container.appendChild(this.collapsible("bootstrap", "初始化 MIS（Bootstrap）", false, () => {
      const body = h("div", {});
      body.appendChild(h("p", { class: "card-desc" }, "用内置模板初始化常见业务域。已存在的表会跳过。"));
      body.appendChild(h("div", { class: "btn-group" },
        h("button", { type: "button", onclick: () => this.previewBootstrap() }, "预览初始化内容"),
        h("button", { class: "primary", type: "button", onclick: () => this.bootstrap() }, "执行初始化")
      ));
      body.appendChild(h("div", { id: "bootstrapPreview", class: "mt-sm" }));
      return body;
    }));

    this.refresh();
  },

  field(id, label, type, placeholder) {
    return App.field(id, label, type, placeholder);
  },

  collapsible(id, title, open, contentFn) {
    return App.collapsible(title, open, contentFn);
  },

  async refresh() {
    const alive = App.navGuard();
    try {
      const data = await App.api("/api/v1/data/datasets");
      if (!alive()) return;
      const items = App.listItems(data, "datasets");
      const el = document.getElementById("datasetList");
      if (!el) return;
      if (!items.length) {
        el.innerHTML = "";
        el.appendChild(App.emptyState(
          "暂无数据集",
          "使用「开通应用」生成业务包，或在下方创建一张表。",
          [{ label: "开通应用", onclick: () => App.navigate("quickstart") }]
        ));
        return;
      }
      App._datasets = items;
      const rows = items.map(d => ({
        _id: d.id,
        _cells: [
          { text: d.id, attrs: { class: "mono" } },
          App.datasetLabel(d),
          d.domain || "-",
          "v" + (d.schema_version || 1)
        ]
      }));
      el.innerHTML = "";
      el.appendChild(App.table(["ID", "名称", "域", "结构版本"], rows, {
        onRowClick: (id) => this.select(id)
      }));
    } catch (e) {
      if (!alive()) return;
      const el = document.getElementById("datasetList");
      if (el) {
        el.innerHTML = "";
        el.appendChild(App.emptyState("加载失败", e.message || "请检查网络或权限后重试。"));
      }
    }
  },

  select(id) {
    App.setDataset(id);
    App.toast("已选择 " + id);
    App.navigate("records", { dataset: id });
  },

  async create() {
    const id = App.val("dsId");
    const name = App.val("dsName");
    const domain = App.val("dsDomain");
    if (!name || !domain) { App.toast("请填写名称和业务域", "warn"); return; }
    try {
      const ds = await App.api("/api/v1/data/datasets", {
        method: "POST",
        body: JSON.stringify({
          id: id || undefined,
          domain,
          name,
          title: App.val("dsTitle") || name,
        }),
      });
      App.invalidateCache();
      App.refreshDatasetPicker();
      App.toast("已创建 " + (ds.id || id));
      this.refresh();
    } catch (e) {
      App.toast(e.message || "创建失败", "danger");
    }
  },

  async fromTemplate() {
    const id = App.val("dsId");
    if (!id) { App.toast("请先填写模板/数据集 ID，例如 sales.orders", "warn"); return; }
    try {
      const data = await App.api("/api/v1/data/templates/" + encodeURIComponent(id) + "/create", {
        method: "POST",
        body: JSON.stringify({}),
      });
      const ds = data.dataset || data;
      App.invalidateCache();
      App.refreshDatasetPicker();
      App.toast("已从模板创建 " + (ds.id || id));
      this.refresh();
    } catch (e) {
      App.toast(e.message || "从模板创建失败", "danger");
    }
  },

  async previewBootstrap() {
    await this.runBootstrap(true);
  },

  async bootstrap() {
    if (!confirm("确定执行 MIS 初始化？已存在的数据集会被跳过。")) return;
    await this.runBootstrap(false);
  },

  async runBootstrap(dry) {
    const el = document.getElementById("bootstrapPreview");
    if (el) el.innerHTML = '<div class="loading-state">处理中…</div>';
    try {
      const data = await App.api("/api/v1/data/templates/bootstrap", {
        method: "POST",
        body: JSON.stringify({ dry_run: !!dry, skip_existing: true }),
      });
      if (!dry) {
        App.invalidateCache();
        App.refreshDatasetPicker();
        this.refresh();
      }
      const would = data.would_create || [];
      const created = data.created || [];
      const skipped = data.skipped || [];
      const rows = would.map(t => ({ _cells: [t.id, t.domain || "-", "将创建"] }))
        .concat(created.map(c => ({ _cells: [(c.dataset || c).id || "-", (c.dataset || c).domain || "-", "已创建"] })))
        .concat(skipped.map(id => ({ _cells: [id, "-", "跳过"] })));
      if (el) {
        el.innerHTML = "";
        el.appendChild(rows.length
          ? App.table(["表", "域", "结果"], rows)
          : App.emptyState("没有变更", "模板表都已存在。"));
      }
    } catch (e) {
      if (el) {
        el.innerHTML = "";
        el.appendChild(App.emptyState("失败", e.message));
      }
    }
  }
};

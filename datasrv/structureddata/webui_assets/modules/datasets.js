// MaClawDataSrv - Datasets Module
"use strict";

PageModules.datasets = {
  render(container) {
    const h = App.html;

    container.appendChild(h("div", { class: "page-header" },
      h("div", {},
        h("h1", {}, "数据集"),
        h("div", { class: "subtitle" }, "创建和管理数据集元数据、结构定义和生命周期")
      ),
      h("div", { class: "header-actions" },
        h("button", { class: "ghost sm", type: "button", onclick: () => this.refresh() }, "刷新")
      )
    ));

    container.appendChild(h("div", { class: "card" },
      h("div", { class: "card-header" },
        h("h3", {}, "数据集列表")
      ),
      h("div", { id: "datasetList", class: "mt-sm" },
        h("div", { class: "loading-state" }, "加载中…")
      )
    ));

    container.appendChild(this.collapsible("createDs", "创建数据集", true, () => {
      const body = h("div", {});

      body.appendChild(h("p", { class: "card-desc" }, "从模板快速创建，或自定义数据集。ID 建议使用 domain.name 形式，例如 sales.orders。"));

      const row = h("div", { class: "form-row" },
        this.field("dsId", "数据集 ID", "text", "sales.orders"),
        this.field("dsName", "显示名称", "text", "销售订单"),
        this.field("dsDomain", "所属业务域", "text", "sales")
      );
      body.appendChild(row);

      body.appendChild(h("div", { class: "form-field mt-sm" },
        h("label", { for: "dsSchema" }, "初始结构 (JSON Schema)"),
        h("textarea", { id: "dsSchema", placeholder: '{\n  "fields": [\n    {"name": "customer", "type": "string"},\n    {"name": "amount", "type": "number"}\n  ]\n}' })
      ));

      body.appendChild(h("div", { class: "btn-group mt-md" },
        h("button", { class: "primary", type: "button", onclick: () => this.create() }, "创建数据集"),
        h("button", { class: "sm", type: "button", onclick: () => this.fromTemplate() }, "从模板创建")
      ));

      return body;
    }));

    container.appendChild(this.collapsible("bootstrap", "初始化 MIS（Bootstrap）", false, () => {
      const body = h("div", {});
      body.appendChild(h("p", { class: "card-desc" }, "用内置模板一键初始化常见业务域（销售、财务、人事、库存等）。仅建议首次部署时使用。"));
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
    try {
      const data = await App.api("/api/v1/data/datasets");
      const el = document.getElementById("datasetList");
      if (!el) return;
      if (!data.datasets || !data.datasets.length) {
        el.innerHTML = "";
        el.appendChild(App.emptyState(
          "暂无数据集",
          "使用下方表单创建，或执行「初始化 MIS」以快速生成业务域模板。",
          [{ label: "前往快速操作", onclick: () => App.navigate("quickstart") }]
        ));
        return;
      }
      const h = App.html;
      el.innerHTML = "";
      data.datasets.forEach(d => {
        const row = h("div", {
          class: "list-row",
          role: "button",
          tabindex: "0",
          onclick: () => this.select(d.id),
          onkeydown: (e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); this.select(d.id); } }
        },
          h("div", {},
            h("div", { class: "list-title mono" }, d.id),
            h("div", { class: "list-meta" }, d.name || d.display_name || "")
          ),
          h("span", { class: "badge" }, String(d.record_count || 0) + " 条")
        );
        el.appendChild(row);
      });
    } catch (e) {
      const el = document.getElementById("datasetList");
      if (el) {
        el.innerHTML = "";
        el.appendChild(App.emptyState("加载失败", e.message || "请检查网络或权限后重试。"));
      }
    }
  },

  select(id) { App.toast("已选择: " + id); },
  create() { App.toast("创建数据集..."); },
  fromTemplate() { App.toast("加载模板..."); },
  previewBootstrap() { App.toast("预览初始化..."); },
  bootstrap() { if (confirm("确定执行 MIS 初始化？这将创建多个数据集。")) App.toast("初始化中..."); },
};

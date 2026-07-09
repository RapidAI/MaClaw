// MaClawDataSrv - Fields Module
"use strict";

PageModules.fields = {
  render(container) {
    const h = App.html;

    container.appendChild(h("div", { class: "page-header" },
      h("div", {},
        h("h1", {}, "字段管理"),
        h("div", { class: "subtitle" }, "维护数据集字段定义和受控的结构改进提案")
      ),
      h("button", { class: "ghost sm", onclick: () => this.refresh() }, "刷新")
    ));

    // Current fields
    container.appendChild(h("div", { class: "card" },
      h("div", { class: "card-header" },
        h("h3", {}, "当前字段"),
        h("div", { class: "text-muted text-sm" }, "选择数据集后查看其字段定义")
      ),
      h("div", { id: "fieldTable", class: "table-wrap mt-sm" },
        h("div", { class: "empty-state" }, "请先在"数据集"页面选择一个数据集")
      )
    ));

    // Schema proposals (collapsed)
    container.appendChild(this.collapsible("proposals", "📝 结构改进提案", false, () => {
      const body = h("div", {});
      body.appendChild(h("p", { class: "card-desc" }, "当 Agent 建议修改数据结构时，会生成提案供管理员审批。"));
      body.appendChild(h("div", { class: "btn-group" },
        h("button", { onclick: () => this.loadProposals() }, "加载提案"),
        h("button", { class: "sm ghost", onclick: () => this.proposeSchema() }, "生成新提案")
      ));
      body.appendChild(h("div", { id: "proposalList", class: "mt-sm" }));
      return body;
    }));
  },

  collapsible(id, title, open, contentFn) {
    return App.collapsible(title, open, contentFn);
  },

  refresh() { App.toast("刷新字段..."); },
  loadProposals() { App.toast("加载提案..."); },
  proposeSchema() { App.toast("生成提案..."); },
};

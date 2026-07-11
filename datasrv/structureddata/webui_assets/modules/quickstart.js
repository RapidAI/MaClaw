// MaClawDataSrv - Quick Start Module
"use strict";

PageModules.quickstart = {
  render(container) {
    const h = App.html;

    container.appendChild(h("div", { class: "page-header" },
      h("div", {},
        h("h1", {}, "快速操作手册"),
        h("div", { class: "subtitle" }, "面向管理员：先理解核心概念，再按安全顺序操作")
      )
    ));

    const steps = h("div", { class: "steps-guide", "data-testid": "quickstart-panel" });
    const stepData = [
      ["1", "检查总览", "确认服务在线、模板和数据集状态", "overview"],
      ["2", "建数据集", "用模板初始化业务域，或自定义创建", "datasets"],
      ["3", "操作记录", "选数据集后增删改查、导入导出", "records"],
      ["4", "业务动作", "用受控动作替代原始 CRUD 写入", "actions"],
      ["5", "做治理", "检查质量、审计、密钥和备份", "apikeys"],
    ];
    stepData.forEach(([num, title, desc, page]) => {
      const card = h("div", { class: "step-card" },
        h("span", { class: "step-num" }, num),
        h("strong", {}, title),
        h("p", {}, desc),
        h("button", { class: "sm", type: "button", onclick: () => App.navigate(page) }, "前往")
      );
      steps.appendChild(card);
    });
    container.appendChild(steps);

    container.appendChild(h("h2", { class: "section-title" }, "核心概念"));

    const concepts = h("div", { class: "concept-grid" });
    const conceptData = [
      ["数据集", "一类业务对象的受控存储，如客户、订单、费用", "datasets"],
      ["记录", "数据集中的一条业务事实", "records"],
      ["业务域", "业务能力分组：销售、财务、人事、法务", "domains"],
      ["业务动作", "带规则检查和幂等保护的安全写入操作", "actions"],
      ["视图 / 报表 / 仪表盘", "受控的分析入口，避免暴露原始数据", "views"],
      ["收件箱", "审批、失败、质量问题的统一处理入口", "inbox"],
      ["连接器", "连接 CRM、ERP、HR 等外部系统", "connectors"],
      ["API 密钥与审计", "权限管控、审计证据和灾备恢复", "apikeys"],
    ];
    conceptData.forEach(([title, desc, page]) => {
      concepts.appendChild(h("button", {
        class: "concept-card",
        type: "button",
        onclick: () => App.navigate(page)
      },
        h("strong", {}, title),
        h("p", {}, desc)
      ));
    });
    container.appendChild(concepts);
  }
};

// MaClawDataSrv - Inbox Module
"use strict";

PageModules.inbox = {
  render(container) {
    const h = App.html;

    container.appendChild(h("div", { class: "page-header" },
      h("div", {},
        h("h1", {}, "收件箱"),
        h("div", { class: "subtitle" }, "审批、失败任务、质量问题和待处理工作的统一入口")
      ),
      h("button", { class: "ghost sm", onclick: () => this.refresh() }, "刷新")
    ));

    // Status summary
    container.appendChild(h("div", { class: "status-banner", id: "inboxStats" },
      this.stat("待审批", "0", "inboxApproval"),
      this.stat("失败任务", "0", "inboxFailed"),
      this.stat("质量问题", "0", "inboxQuality"),
      this.stat("逾期", "0", "inboxOverdue")
    ));

    // Filters
    container.appendChild(h("div", { class: "card" },
      h("div", { class: "form-row" },
        h("div", { class: "form-field" },
          h("label", {}, "类型"),
          h("select", { id: "inboxType" },
            h("option", { value: "" }, "全部"),
            h("option", { value: "approval" }, "审批"),
            h("option", { value: "operation_plan" }, "操作计划"),
            h("option", { value: "import_job" }, "导入任务"),
            h("option", { value: "quality" }, "质量问题")
          )
        ),
        h("div", { class: "form-field" },
          h("label", {}, "状态"),
          h("select", { id: "inboxStatus" },
            h("option", { value: "" }, "待处理"),
            h("option", { value: "pending" }, "待审批"),
            h("option", { value: "failed" }, "失败"),
            h("option", { value: "issue" }, "有问题")
          )
        ),
        h("div", { class: "form-field", style: { alignSelf: "end" } },
          h("button", { class: "primary", onclick: () => this.refresh() }, "筛选")
        )
      )
    ));

    // Items list
    container.appendChild(h("div", { id: "inboxList", class: "mt-sm" },
      h("div", { class: "empty-state" }, "加载中...")
    ));

    this.refresh();
  },

  stat(label, value, id) {
    return App.html("div", { class: "status-item" },
      App.html("div", { class: "status-label" }, label),
      App.html("div", { class: "status-value", id }, value)
    );
  },

  async refresh() {
    try {
      const data = await App.api("/api/v1/data/inbox?limit=50");
      const el = document.getElementById("inboxList");
      if (!el) return;
      if (!data.items || !data.items.length) {
        el.innerHTML = '<div class="empty-state">🎉 暂无待处理事项</div>';
        return;
      }
      const h = App.html;
      const tbl = document.createElement("table");
      tbl.appendChild(h("thead", {}, h("tr", {},
        h("th", {}, "类型"), h("th", {}, "状态"), h("th", {}, "摘要"), h("th", {}, "创建时间")
      )));
      const tbody = document.createElement("tbody");
      data.items.forEach(item => {
        tbody.appendChild(h("tr", {},
          h("td", {}, item.type || "-"),
          h("td", {}, item.status || "-"),
          h("td", {}, item.summary || "-"),
          h("td", {}, item.created_at ? new Date(item.created_at).toLocaleString() : "-")
        ));
      });
      tbl.appendChild(tbody);
      el.innerHTML = "";
      const wrap = h("div", { class: "table-wrap" });
      wrap.appendChild(tbl);
      el.appendChild(wrap);
    } catch(e) { document.getElementById("inboxList").innerHTML = '<div class="empty-state">加载失败</div>'; }
  }
};

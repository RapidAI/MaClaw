// MaClawDataSrv - Inbox Module
"use strict";

PageModules.inbox = {
  render(container) {
    const h = App.html;

    container.appendChild(h("div", { class: "page-header" },
      h("div", {},
        h("h1", {}, "待办"),
        h("div", { class: "subtitle" }, "审批、失败任务、质量问题。先处理这里，再去改表。")
      ),
      h("button", { class: "ghost sm", type: "button", onclick: () => this.refresh() }, "刷新")
    ));

    container.appendChild(h("div", { class: "status-banner", id: "inboxStats" },
      this.stat("待处理", "0", "inboxTotal"),
      this.stat("审批", "0", "inboxApproval"),
      this.stat("失败", "0", "inboxFailed"),
      this.stat("质量", "0", "inboxQuality"),
      this.stat("逾期", "0", "inboxOverdue")
    ));

    container.appendChild(h("div", { class: "card" },
      h("div", { class: "form-row" },
        h("div", { class: "form-field" },
          h("label", {}, "类型"),
          h("select", { id: "inboxType" },
            h("option", { value: "" }, "全部"),
            h("option", { value: "approval" }, "审批"),
            h("option", { value: "operation_plan" }, "操作计划"),
            h("option", { value: "import_job" }, "导入任务"),
            h("option", { value: "export_job" }, "导出任务"),
            h("option", { value: "event_dead_letter" }, "事件死信"),
            h("option", { value: "quality" }, "质量问题")
          )
        ),
        h("div", { class: "form-field" },
          h("label", {}, "状态"),
          h("select", { id: "inboxStatus" },
            h("option", { value: "" }, "全部状态"),
            h("option", { value: "pending" }, "待审批"),
            h("option", { value: "failed" }, "失败"),
            h("option", { value: "issue" }, "有问题")
          )
        ),
        h("div", { class: "form-field", style: { alignSelf: "end" } },
          h("button", { class: "primary", type: "button", onclick: () => this.refresh() }, "筛选")
        )
      )
    ));

    container.appendChild(h("div", { id: "inboxList", class: "mt-sm" },
      h("div", { class: "loading-state" }, "加载中…")
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
    const alive = App.navGuard();
    const type = App.val("inboxType");
    const status = App.val("inboxStatus");
    try {
      const summary = await App.api("/api/v1/data/inbox/summary");
      if (!alive()) return;
      const set = (id, v) => { const el = document.getElementById(id); if (el) el.textContent = v; };
      set("inboxTotal", summary.total ?? 0);
      set("inboxOverdue", summary.overdue ?? 0);
      const byType = summary.by_type || {};
      set("inboxApproval", byType.approval ?? byType.record_approval ?? 0);
      set("inboxFailed", (byType.import_job || 0) + (byType.export_job || 0) + (byType.event_dead_letter || 0));
      set("inboxQuality", byType.quality ?? 0);
    } catch (_) { /* banner stays */ }

    try {
      const data = await App.api("/api/v1/data/inbox" + App.qs({ limit: 50, type, status }));
      if (!alive()) return;
      const items = App.listItems(data);
      const el = document.getElementById("inboxList");
      if (!el) return;
      if (!items.length) {
        el.innerHTML = "";
        el.appendChild(App.emptyState("收件箱为空", "当前没有待审批、失败任务或质量问题。"));
        return;
      }
      const rows = items.map(item => ({
        _id: item.id,
        _item: item,
        _cells: [
          item.type || "-",
          item.status || "-",
          item.summary || item.title || "-",
          item.dataset_id || "-",
          App.fmtTime(item.created_at)
        ]
      }));
      el.innerHTML = "";
      el.appendChild(App.table(["类型", "状态", "摘要", "表", "时间"], rows, {
        onRowClick: (id) => this.openItem(items.find(it => it.id === id))
      }));
    } catch (e) {
      if (!alive()) return;
      const el = document.getElementById("inboxList");
      if (el) {
        el.innerHTML = "";
        el.appendChild(App.emptyState("加载失败", e.message || "请检查网络或权限后重试。"));
      }
    }
  },

  openItem(item) {
    if (!item) return;
    if (item.type === "quality" && item.dataset_id) {
      App.navigate("quality", { dataset: item.dataset_id });
      return;
    }
    if (item.dataset_id) {
      const params = { dataset: item.dataset_id };
      if (item.record_id) params.record = item.record_id;
      App.navigate("records", params);
      return;
    }
    App.toast(item.recommended_action || item.summary || "这条待办没有关联业务表", "warn");
  }
};

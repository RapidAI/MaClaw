// MaClawDataSrv - Security Modules (Admins, Quality, Backups, Audit, Ops)
"use strict";

// === Admins ===
PageModules.admins = {
  render(container) {
    const h = App.html;
    container.appendChild(h("div", { class: "page-header" },
      h("div", {}, h("h1", {}, "管理员"), h("div", { class: "subtitle" }, "创建管理员账号、管理会话并配置 Hub 注册")),
      h("button", { class: "ghost sm", onclick: () => this.refresh() }, "刷新")
    ));

    // Account list
    container.appendChild(h("div", { class: "card" },
      h("div", { class: "card-header" }, h("h3", {}, "管理员账号")),
      h("div", { id: "adminList" }, h("div", { class: "loading-state" }, "加载中…"))
    ));

    // Create account (collapsed)
    container.appendChild(this.collapsible("createAdmin", "创建管理员", false, () => {
      const body = h("div", {});
      const row = h("div", { class: "form-row" },
        this.field("newAdminUser", "用户名", "text", "new_admin"),
        this.field("newAdminName", "显示名", "text", ""),
        this.field("newAdminPwd", "临时密码", "password", "")
      );
      body.appendChild(row);
      body.appendChild(h("div", { class: "btn-group mt-sm" },
        h("button", { class: "primary", onclick: () => this.createAdmin() }, "创建")
      ));
      return body;
    }));

    // Sessions (collapsed)
    container.appendChild(this.collapsible("sessions", "活跃会话", false, () => {
      const body = h("div", {});
      body.appendChild(h("div", { id: "sessionList" }, h("div", { class: "empty-state" }, "点击刷新加载")));
      body.appendChild(h("div", { class: "btn-group mt-sm" }, h("button", { class: "sm", onclick: () => this.loadSessions() }, "加载会话")));
      return body;
    }));

    this.refresh();
  },

  field(id, label, type, placeholder) {
    return App.field(id, label, type, placeholder);
  },
  collapsible(id, title, open, fn) {
    return App.collapsible(title, open, fn);
  },

  async refresh() {
    try {
      const data = await App.api("/api/v1/data/admin/accounts");
      const el = document.getElementById("adminList");
      if (!el) return;
      if (!data.accounts || !data.accounts.length) {
        el.innerHTML = "";
        el.appendChild(App.emptyState("暂无管理员账号", "使用下方表单创建首个管理员，或完成首次初始化。"));
        return;
      }
      const rows = data.accounts.map(a => ({
        _cells: [
          a.username || "-",
          a.display_name || "-",
          a.scope || "global",
          a.enabled !== false ? App.badge("启用", "ok") : App.badge("停用"),
          a.last_login ? new Date(a.last_login).toLocaleString() : "从未"
        ]
      }));
      el.innerHTML = "";
      el.appendChild(App.table(["用户名", "显示名", "角色", "状态", "上次登录"], rows));
    } catch (e) {
      const el = document.getElementById("adminList");
      if (el) {
        el.innerHTML = "";
        el.appendChild(App.emptyState("加载失败", e.message || "需要 allow_admin 权限。"));
      }
    }
  },
  createAdmin() { App.toast("创建管理员..."); },
  loadSessions() { App.toast("加载会话..."); },
};

// === Quality ===
PageModules.quality = {
  render(container) {
    const h = App.html;
    container.appendChild(h("div", { class: "page-header" },
      h("div", {}, h("h1", {}, "数据质量"), h("div", { class: "subtitle" }, "运行质量检查，查看历史扫描结果和问题")),
      h("button", { class: "ghost sm", onclick: () => this.refresh() }, "刷新")
    ));
    container.appendChild(h("div", { class: "card" },
      h("div", { class: "card-header" }, h("h3", {}, "质量检查"), h("button", { class: "primary sm", onclick: () => this.runCheck() }, "运行检查")),
      h("div", { id: "qualityTable" }, h("div", { class: "loading-state" }, "加载中…"))
    ));
    this.refresh();
  },
  async refresh() {
    try {
      const data = await App.api("/api/v1/data/quality/checks");
      const el = document.getElementById("qualityTable");
      if (!el) return;
      if (!data.checks || !data.checks.length) {
        el.innerHTML = "";
        el.appendChild(App.emptyState("暂无质量检查记录", "点击「运行检查」开始扫描数据集。"));
        return;
      }
      const rows = data.checks.map(c => ({
        _cells: [
          c.dataset || "-",
          String(c.scanned || 0),
          String(c.issues || 0),
          c.finished_at ? new Date(c.finished_at).toLocaleString() : "-"
        ]
      }));
      el.innerHTML = "";
      el.appendChild(App.table(["数据集", "已扫描", "问题数", "时间"], rows));
    } catch (e) {
      const el = document.getElementById("qualityTable");
      if (el) { el.innerHTML = ""; el.appendChild(App.emptyState("加载失败", e.message)); }
    }
  },
  runCheck() { App.toast("运行质量检查..."); },
};

// === Backups ===
PageModules.backups = {
  render(container) {
    const h = App.html;
    container.appendChild(h("div", { class: "page-header" },
      h("div", {}, h("h1", {}, "备份管理"), h("div", { class: "subtitle" }, "创建、下载和恢复数据库快照")),
      h("div", { class: "btn-group" },
        h("button", { class: "primary", onclick: () => this.create() }, "创建备份"),
        h("button", { class: "ghost sm", onclick: () => this.refresh() }, "刷新")
      )
    ));
    container.appendChild(h("div", { class: "card" },
      h("div", { class: "card-header" }, h("h3", {}, "备份列表")),
      h("div", { id: "backupList" }, h("div", { class: "loading-state" }, "加载中…"))
    ));
    this.refresh();
  },
  async refresh() {
    try {
      const data = await App.api("/api/v1/data/backups");
      const el = document.getElementById("backupList");
      if (!el) return;
      if (!data.backups || !data.backups.length) {
        el.innerHTML = "";
        el.appendChild(App.emptyState("暂无备份", "建议在重要操作前创建备份快照。"));
        return;
      }
      const h = App.html;
      const rows = data.backups.map(b => {
        const name = b.name || b.filename || "-";
        const size = b.size_bytes ? (b.size_bytes / 1024).toFixed(0) + " KB" : "-";
        const time = b.created_at ? new Date(b.created_at).toLocaleString() : "-";
        const actions = h("div", { class: "btn-group" },
          h("button", { class: "sm ghost", type: "button", onclick: () => this.download(name) }, "下载"),
          h("button", { class: "sm danger", type: "button", onclick: () => this.restore(name) }, "恢复")
        );
        return {
          _cells: [
            { text: name, attrs: { class: "mono" } },
            size,
            time,
            actions
          ]
        };
      });
      el.innerHTML = "";
      el.appendChild(App.table(["文件名", "大小", "创建时间", "操作"], rows));
    } catch (e) {
      const el = document.getElementById("backupList");
      if (el) { el.innerHTML = ""; el.appendChild(App.emptyState("加载失败", e.message)); }
    }
  },
  create() { App.toast("创建备份..."); },
  download(name) { App.toast("下载: " + name); },
  restore(name) { if (confirm("确定恢复备份？当前数据将被替换，此操作不可撤销。")) App.toast("恢复中..."); },
};

// === Audit ===
PageModules.audit = {
  render(container) {
    const h = App.html;
    container.appendChild(h("div", { class: "page-header" },
      h("div", {}, h("h1", {}, "审计日志"), h("div", { class: "subtitle" }, "搜索和导出审计轨迹用于合规复核")),
      h("button", { class: "ghost sm", onclick: () => this.refresh() }, "刷新")
    ));

    // Filters
    container.appendChild(h("div", { class: "card" },
      h("div", { class: "form-row" },
        h("div", { class: "form-field" }, h("label", {}, "关键词"), h("input", { id: "auditKeyword", placeholder: "操作、用户或资源" })),
        h("div", { class: "form-field" }, h("label", {}, "数量"), h("input", { id: "auditLimit", type: "number", value: "100" })),
        h("div", { class: "form-field", style: { alignSelf: "end" } },
          h("button", { class: "primary", onclick: () => this.refresh() }, "搜索"),
          h("button", { class: "sm ghost", style: { marginLeft: "8px" }, onclick: () => this.exportCSV() }, "导出 CSV")
        )
      )
    ));

    container.appendChild(h("div", { id: "auditTable", class: "mt-sm" }, h("div", { class: "loading-state" }, "加载中…")));
    this.refresh();
  },
  async refresh() {
    try {
      const kw = document.getElementById("auditKeyword")?.value || "";
      const limit = document.getElementById("auditLimit")?.value || "100";
      const data = await App.api(`/api/v1/data/audit?keyword=${encodeURIComponent(kw)}&limit=${limit}`);
      const el = document.getElementById("auditTable");
      if (!el) return;
      if (!data.entries || !data.entries.length) {
        el.innerHTML = "";
        el.appendChild(App.emptyState("暂无审计记录", "调整关键词后重新搜索，或确认系统已产生操作日志。"));
        return;
      }
      const rows = data.entries.map(e => ({
        _cells: [
          e.timestamp ? new Date(e.timestamp).toLocaleString() : "-",
          e.user || "-",
          e.action || "-",
          { text: e.resource || "-", attrs: { class: "mono" } }
        ]
      }));
      el.innerHTML = "";
      el.appendChild(App.table(["时间", "用户", "操作", "资源"], rows));
    } catch (e) {
      const el = document.getElementById("auditTable");
      if (el) { el.innerHTML = ""; el.appendChild(App.emptyState("加载失败", e.message)); }
    }
  },
  exportCSV() { App.toast("导出审计 CSV..."); },
};

// === Ops ===
PageModules.ops = {
  render(container) {
    const h = App.html;
    container.appendChild(h("div", { class: "page-header" },
      h("div", {}, h("h1", {}, "服务运维"), h("div", { class: "subtitle" }, "查看服务统计、运行数据库维护")),
      h("button", { class: "ghost sm", onclick: () => this.refresh() }, "刷新")
    ));

    container.appendChild(h("div", { class: "status-banner", id: "opsStats" },
      this.stat("数据库大小", "-", "opsDbSize"),
      this.stat("总记录数", "-", "opsRecords"),
      this.stat("总事件数", "-", "opsEvents"),
      this.stat("服务运行时间", "-", "opsUptime")
    ));

    container.appendChild(h("div", { class: "card" },
      h("div", { class: "card-header" }, h("h3", {}, "数据库维护")),
      h("p", { class: "card-desc" }, "VACUUM 回收磁盘空间，期间数据库短暂锁定。建议在低峰时段执行。"),
      h("div", { class: "btn-group" },
        h("button", { onclick: () => this.vacuum() }, "运行 VACUUM"),
        h("button", { class: "ghost sm", onclick: () => this.refresh() }, "刷新统计")
      )
    ));

    this.refresh();
  },

  stat(label, value, id) {
    return App.html("div", { class: "status-item" }, App.html("div", { class: "status-label" }, label), App.html("div", { class: "status-value", id }, value));
  },

  async refresh() {
    try {
      const data = await App.api("/api/v1/data/stats");
      const set = (id, v) => { const el = document.getElementById(id); if (el) el.textContent = v; };
      set("opsDbSize", data.db_size_bytes ? (data.db_size_bytes / 1048576).toFixed(1) + " MB" : "-");
      set("opsRecords", data.total_records || "-");
      set("opsEvents", data.total_events || "-");
      set("opsUptime", data.uptime_seconds ? Math.floor(data.uptime_seconds / 3600) + " 小时" : "-");
    } catch(e) { /* silent */ }
  },
  vacuum() { if (confirm("确定运行 VACUUM？期间数据库将短暂锁定。")) App.toast("VACUUM 中..."); },
};

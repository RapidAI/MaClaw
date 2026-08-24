// MaClawDataSrv - Security Modules (Admins, Quality, Backups, Audit, Ops)
"use strict";

PageModules.admins = {
  render(container) {
    const h = App.html;
    container.appendChild(h("div", { class: "page-header" },
      h("div", {}, h("h1", {}, "管理员"), h("div", { class: "subtitle" }, "创建管理员账号、管理会话并配置 Hub 注册")),
      h("button", { class: "ghost sm", type: "button", onclick: () => this.refresh() }, "刷新")
    ));

    container.appendChild(h("div", { class: "card" },
      h("div", { class: "card-header" }, h("h3", {}, "管理员账号")),
      h("div", { id: "adminList" }, h("div", { class: "loading-state" }, "加载中…"))
    ));

    container.appendChild(this.collapsible("createAdmin", "创建管理员", false, () => {
      const body = h("div", {});
      body.appendChild(h("div", { class: "form-row" },
        this.field("newAdminUser", "用户名", "text", "new_admin"),
        this.field("newAdminName", "显示名", "text", ""),
        this.field("newAdminPwd", "临时密码", "password", "")
      ));
      body.appendChild(h("div", { class: "btn-group mt-sm" },
        h("button", { class: "primary", type: "button", onclick: () => this.createAdmin() }, "创建")
      ));
      return body;
    }));

    container.appendChild(this.collapsible("sessions", "活跃会话", false, () => {
      const body = h("div", {});
      body.appendChild(h("div", { id: "sessionList" }, h("div", { class: "empty-state" }, "点击刷新加载")));
      body.appendChild(h("div", { class: "btn-group mt-sm" },
        h("button", { class: "sm", type: "button", onclick: () => this.loadSessions() }, "加载会话")
      ));
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
      const items = App.listItems(data, "accounts");
      const el = document.getElementById("adminList");
      if (!el) return;
      if (!items.length) {
        el.innerHTML = "";
        el.appendChild(App.emptyState("暂无管理员账号", "使用下方表单创建管理员。"));
        return;
      }
      const rows = items.map(a => ({
        _cells: [
          a.username || "-",
          a.display_name || "-",
          a.admin_scope || a.scope || "global",
          a.enabled !== false ? App.badge("启用", "ok") : App.badge("停用"),
          a.last_login_at ? App.fmtTime(a.last_login_at) : "从未"
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

  async createAdmin() {
    const username = App.val("newAdminUser");
    const password = document.getElementById("newAdminPwd")?.value || "";
    if (!username || !password) { App.toast("请填写用户名和密码", "warn"); return; }
    try {
      await App.api("/api/v1/data/admin/accounts", {
        method: "POST",
        body: JSON.stringify({
          username,
          password,
          display_name: App.val("newAdminName") || username,
        }),
      });
      App.toast("管理员已创建");
      this.refresh();
    } catch (e) {
      App.toast(e.message || "创建失败", "danger");
    }
  },

  async loadSessions() {
    const el = document.getElementById("sessionList");
    if (!el) return;
    try {
      const data = await App.api("/api/v1/data/admin/sessions");
      const items = App.listItems(data);
      if (!items.length) {
        el.innerHTML = "";
        el.appendChild(App.emptyState("没有活跃会话", "登录后会显示在这里。"));
        return;
      }
      const rows = items.map(s => ({
        _cells: [
          s.id || s.session_id || "-",
          s.username || s.user_id || "-",
          App.fmtTime(s.expires_at),
          s.current ? App.badge("当前", "ok") : App.badge("其他")
        ]
      }));
      el.innerHTML = "";
      el.appendChild(App.table(["会话", "用户", "过期", "标记"], rows));
    } catch (e) {
      el.innerHTML = "";
      el.appendChild(App.emptyState("加载失败", e.message));
    }
  }
};

PageModules.quality = {
  render(container) {
    const h = App.html;
    container.appendChild(h("div", { class: "page-header" },
      h("div", {}, h("h1", {}, "数据质量"), h("div", { class: "subtitle" }, "选择业务表后运行质量检查")),
      h("button", { class: "ghost sm", type: "button", onclick: () => this.refresh() }, "刷新")
    ));
    container.appendChild(h("div", { class: "card" },
      h("div", { class: "form-row" }, App.datasetSelect("qualityDataset", "业务表")),
      h("div", { class: "btn-group mt-sm" },
        h("button", { class: "primary sm", type: "button", onclick: () => this.runCheck() }, "运行检查"),
        h("button", { class: "sm", type: "button", onclick: () => this.refresh() }, "查看检查定义")
      ),
      h("div", { id: "qualityTable" }, h("div", { class: "loading-state" }, "加载中…"))
    ));
    const ds = document.getElementById("qualityDataset");
    if (ds) {
      ds.addEventListener("change", () => {
        App.setDataset(ds.value);
        if (ds.value) this.showLatest();
        else this.refresh();
      });
    }
    if (App.datasetId) this.showLatest();
    else this.refresh();
  },
  async refresh() {
    const alive = App.navGuard();
    try {
      const data = await App.api("/api/v1/data/quality-checks");
      if (!alive()) return;
      const items = App.listItems(data, "checks");
      const el = document.getElementById("qualityTable");
      if (!el) return;
      if (!items.length) {
        el.innerHTML = "";
        el.appendChild(App.emptyState("暂无质量检查定义", "选择业务表后点击「运行检查」。"));
        return;
      }
      const rows = items.map(c => ({
        _cells: [
          c.id || "-",
          c.title || "-",
          c.severity || "-",
          c.description || "-"
        ]
      }));
      el.innerHTML = "";
      el.appendChild(App.table(["检查", "标题", "级别", "说明"], rows));
    } catch (e) {
      if (!alive()) return;
      const el = document.getElementById("qualityTable");
      if (el) { el.innerHTML = ""; el.appendChild(App.emptyState("加载失败", e.message)); }
    }
  },
  async showLatest() {
    const alive = App.navGuard();
    const ds = App.val("qualityDataset") || App.datasetId;
    const el = document.getElementById("qualityTable");
    if (!ds) { this.refresh(); return; }
    if (el) el.innerHTML = '<div class="loading-state">加载最近一次检查…</div>';
    try {
      const data = await App.api("/api/v1/data/datasets/" + encodeURIComponent(ds) + "/quality/runs?limit=1");
      if (!alive()) return;
      const items = App.listItems(data);
      if (!items.length) {
        this.refresh();
        return;
      }
      if (el) {
        el.innerHTML = "";
        el.appendChild(App.resultCard("最近一次检查 · 扫描 " + (items[0].scanned || 0) + " · 问题 " + (items[0].issue_count || 0), items[0]));
      }
    } catch (_) {
      if (alive()) this.refresh();
    }
  },
  async runCheck() {
    const alive = App.navGuard();
    const ds = App.val("qualityDataset") || App.datasetId;
    if (!ds) { App.toast("请先选择业务表", "warn"); return; }
    App.setDataset(ds);
    const el = document.getElementById("qualityTable");
    if (el) el.innerHTML = '<div class="loading-state">检查中…</div>';
    try {
      const out = await App.api("/api/v1/data/datasets/" + encodeURIComponent(ds) + "/quality/run", {
        method: "POST",
        body: JSON.stringify({ include_warnings: true }),
      });
      if (!alive()) return;
      if (el) {
        el.innerHTML = "";
        el.appendChild(App.resultCard("检查结果 · 扫描 " + (out.scanned || 0) + " · 问题 " + (out.issue_count || 0), out));
      }
    } catch (e) {
      if (!alive()) return;
      App.toast(e.message || "运行失败", "danger");
    }
  }
};

PageModules.backups = {
  render(container) {
    const h = App.html;
    container.appendChild(h("div", { class: "page-header" },
      h("div", {}, h("h1", {}, "备份管理"), h("div", { class: "subtitle" }, "创建、下载和恢复数据库快照")),
      h("div", { class: "btn-group" },
        h("button", { class: "primary", type: "button", onclick: () => this.create() }, "创建备份"),
        h("button", { class: "ghost sm", type: "button", onclick: () => this.refresh() }, "刷新")
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
      const items = App.listItems(data, "backups");
      const el = document.getElementById("backupList");
      if (!el) return;
      if (!items.length) {
        el.innerHTML = "";
        el.appendChild(App.emptyState("暂无备份", "建议在重要操作前创建备份快照。"));
        return;
      }
      const h = App.html;
      const rows = items.map(b => {
        const actions = h("div", { class: "btn-group" },
          h("button", { class: "sm ghost", type: "button", onclick: () => this.download(b.id, b.name) }, "下载"),
          h("button", { class: "sm danger", type: "button", onclick: () => this.restore(b.id) }, "恢复")
        );
        return {
          _cells: [
            { text: b.id || "-", attrs: { class: "mono" } },
            b.name || "-",
            App.fmtBytes(b.size_bytes),
            App.fmtTime(b.created_at),
            actions
          ]
        };
      });
      el.innerHTML = "";
      el.appendChild(App.table(["ID", "名称", "大小", "创建时间", "操作"], rows));
    } catch (e) {
      const el = document.getElementById("backupList");
      if (el) { el.innerHTML = ""; el.appendChild(App.emptyState("加载失败", e.message)); }
    }
  },
  async create() {
    try {
      await App.api("/api/v1/data/backups", {
        method: "POST",
        body: JSON.stringify({ name: "manual-" + new Date().toISOString().slice(0, 19) }),
      });
      App.toast("备份已创建");
      this.refresh();
    } catch (e) {
      App.toast(e.message || "创建失败", "danger");
    }
  },
  async download(id, name) {
    try {
      await App.download("/api/v1/data/backups/" + encodeURIComponent(id) + "/download", {
        filename: (name || id || "backup") + ".db",
      });
    } catch (e) {
      App.toast(e.message || "下载失败", "danger");
    }
  },
  async restore(id) {
    if (!confirm("确定恢复备份？当前数据将被替换，此操作不可撤销。")) return;
    try {
      await App.api("/api/v1/data/backups/" + encodeURIComponent(id) + "/restore", {
        method: "POST",
        body: JSON.stringify({ confirm: true, reason: "console restore" }),
      });
      App.toast("恢复完成");
    } catch (e) {
      App.toast(e.message || "恢复失败", "danger");
    }
  }
};

PageModules.audit = {
  render(container) {
    const h = App.html;
    container.appendChild(h("div", { class: "page-header" },
      h("div", {}, h("h1", {}, "审计日志"), h("div", { class: "subtitle" }, "搜索和导出审计轨迹用于合规复核")),
      h("button", { class: "ghost sm", type: "button", onclick: () => this.refresh() }, "刷新")
    ));

    container.appendChild(h("div", { class: "card" },
      h("div", { class: "form-row" },
        h("div", { class: "form-field" }, h("label", {}, "关键词"), h("input", { id: "auditKeyword", placeholder: "操作、用户或资源" })),
        h("div", { class: "form-field" }, h("label", {}, "数量"), h("input", { id: "auditLimit", type: "number", value: "100" })),
        h("div", { class: "form-field", style: { alignSelf: "end" } },
          h("button", { class: "primary", type: "button", onclick: () => this.refresh() }, "搜索"),
          h("button", { class: "sm ghost", type: "button", style: { marginLeft: "8px" }, onclick: () => this.exportCSV() }, "导出 CSV")
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
      const data = await App.api("/api/v1/data/audit" + App.qs({ q: kw, limit }));
      const items = App.listItems(data, "entries");
      const el = document.getElementById("auditTable");
      if (!el) return;
      if (!items.length) {
        el.innerHTML = "";
        el.appendChild(App.emptyState("暂无审计记录", "调整关键词后重新搜索。"));
        return;
      }
      const rows = items.map(e => ({
        _cells: [
          App.fmtTime(e.created_at || e.timestamp),
          e.user_id || e.user || "-",
          e.action || "-",
          { text: e.dataset_id || e.target_id || e.resource || "-", attrs: { class: "mono" } },
          e.summary || "-"
        ]
      }));
      el.innerHTML = "";
      el.appendChild(App.table(["时间", "用户", "操作", "资源", "摘要"], rows));
    } catch (e) {
      const el = document.getElementById("auditTable");
      if (el) { el.innerHTML = ""; el.appendChild(App.emptyState("加载失败", e.message)); }
    }
  },
  async exportCSV() {
    try {
      const kw = document.getElementById("auditKeyword")?.value || "";
      await App.download("/api/v1/data/audit/export.csv" + App.qs({ q: kw }), { filename: "audit.csv" });
      App.toast("已导出审计 CSV");
    } catch (e) {
      App.toast(e.message || "导出失败", "danger");
    }
  }
};

PageModules.ops = {
  render(container) {
    const h = App.html;
    container.appendChild(h("div", { class: "page-header" },
      h("div", {}, h("h1", {}, "服务运维"), h("div", { class: "subtitle" }, "查看服务统计、运行数据库维护")),
      h("button", { class: "ghost sm", type: "button", onclick: () => this.refresh() }, "刷新")
    ));

    container.appendChild(h("div", { class: "status-banner", id: "opsStats" },
      this.stat("数据库大小", "-", "opsDbSize"),
      this.stat("总记录数", "-", "opsRecords"),
      this.stat("字段数", "-", "opsFields"),
      this.stat("审计条数", "-", "opsAudit")
    ));

    container.appendChild(h("div", { class: "card" },
      h("div", { class: "card-header" }, h("h3", {}, "数据库维护")),
      h("p", { class: "card-desc" }, "VACUUM 回收磁盘空间，期间数据库短暂锁定。建议在低峰时段执行。"),
      h("div", { class: "btn-group" },
        h("button", { type: "button", onclick: () => this.vacuum() }, "运行 VACUUM"),
        h("button", { class: "ghost sm", type: "button", onclick: () => this.refresh() }, "刷新统计")
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
      set("opsDbSize", App.fmtBytes(data.database_bytes || data.db_size_bytes));
      set("opsRecords", data.record_count ?? data.total_records ?? "-");
      set("opsFields", data.field_count ?? "-");
      set("opsAudit", data.audit_log_count ?? "-");
    } catch (e) { /* silent */ }
  },
  async vacuum() {
    if (!confirm("确定运行 VACUUM？期间数据库将短暂锁定。")) return;
    try {
      const out = await App.api("/api/v1/data/maintenance/run", {
        method: "POST",
        body: JSON.stringify({ tasks: ["vacuum"] }),
      });
      App.toast("维护完成");
      const banner = document.getElementById("opsStats");
      if (banner && banner.parentNode) {
        banner.parentNode.appendChild(App.resultCard("维护结果", out));
      }
      this.refresh();
    } catch (e) {
      App.toast(e.message || "维护失败", "danger");
    }
  }
};

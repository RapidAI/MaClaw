// MaClawDataSrv - Business apps (domains)
"use strict";

PageModules.domains = {
  render(container) {
    const h = App.html;

    container.appendChild(h("div", { class: "page-header" },
      h("div", {},
        h("h1", {}, "业务应用"),
        h("div", { class: "subtitle" }, "一个应用是一组表、动作和视图。点进去办事，不要先学数据建模。")
      ),
      h("button", { class: "ghost sm", type: "button", onclick: () => this.refresh() }, "刷新")
    ));

    container.appendChild(h("div", { id: "domainGrid", class: "app-card-grid" },
      h("div", { class: "loading-state" }, "加载中…")
    ));
    container.appendChild(h("div", { id: "domainDetail", class: "mt-md" }));

    this.refresh();
    const wanted = App.hashState().params.domain;
    if (wanted) this.open(wanted);
  },

  async refresh() {
    const alive = App.navGuard();
    const el = document.getElementById("domainGrid");
    if (!el) return;
    try {
      const data = await App.api("/api/v1/data/domains?limit=50");
      if (!alive()) return;
      const items = App.listItems(data, "domains");
      if (!items.length) {
        el.innerHTML = "";
        el.appendChild(App.emptyState(
          "暂无业务域",
          "请先在「开通应用」里选择 CRM、进销存或财务。",
          [{ label: "开通应用", primary: true, onclick: () => App.navigate("quickstart") }]
        ));
        return;
      }
      el.innerHTML = "";
      items.forEach(d => {
        const id = d.domain || d.id;
        const missing = (d.missing_templates || []).length;
        el.appendChild(App.html("button", {
          class: "card app-card",
          type: "button",
          onclick: () => this.open(id)
        },
          App.html("strong", {}, d.title || id || "-"),
          App.html("div", { class: "app-meta" },
            (d.initialized ? "已开通" : "未初始化") +
            " · " + ((d.datasets || []).length || d.dataset_count || 0) + " 张表" +
            (missing ? " · 缺 " + missing + " 个模板" : "")
          )
        ));
      });
    } catch (e) {
      if (!alive()) return;
      el.innerHTML = "";
      el.appendChild(App.emptyState("加载失败", e.message || "请检查网络或权限后重试。"));
    }
  },

  async open(domain) {
    const alive = App.navGuard();
    const el = document.getElementById("domainDetail");
    if (!el || !domain) return;
    el.innerHTML = '<div class="loading-state">打开应用…</div>';
    try {
      const d = await App.api("/api/v1/data/domains/" + encodeURIComponent(domain));
      if (!alive()) return;
      const h = App.html;
      const wrap = h("div", { class: "card" });
      wrap.appendChild(h("div", { class: "card-header" },
        h("h3", {}, d.title || d.domain || domain),
        d.initialized ? App.badge("已开通", "ok") : App.badge("未初始化", "warn")
      ));
      if ((d.missing_templates || []).length) {
        wrap.appendChild(h("p", { class: "card-desc" },
          "还缺：" + d.missing_templates.join("、") + "。先预览再开通，避免重复建主数据。"
        ));
        wrap.appendChild(h("div", { class: "btn-group" },
          h("button", { class: "primary sm", type: "button", onclick: () => this.bootstrap(domain, true) }, "预览补齐"),
          h("button", { class: "sm", type: "button", onclick: () => this.bootstrap(domain, false) }, "补齐缺失表")
        ));
      }
      const datasets = d.datasets || [];
      if (datasets.length) {
        const rows = datasets.map(item => {
          const ds = item.dataset || item;
          return {
            _id: ds.id,
            _cells: [
              { text: ds.id, attrs: { class: "mono" } },
              App.datasetLabel(ds),
              String((item.fields || []).length || ds.schema_version || "-")
            ]
          };
        });
        wrap.appendChild(h("div", { class: "mt-sm" },
          App.table(["表 ID", "名称", "字段"], rows, {
            onRowClick: (id) => App.navigate("records", { dataset: id })
          })
        ));
      }
      const actions = d.business_actions || [];
      if (actions.length) {
        const btns = h("div", { class: "btn-group mt-sm" });
        actions.slice(0, 6).forEach(a => {
          btns.appendChild(h("button", {
            class: "sm",
            type: "button",
            onclick: () => App.navigate("actions", { action: a.id })
          }, a.title || a.id));
        });
        wrap.appendChild(btns);
      }
      wrap.appendChild(h("div", { id: "domainPlan", class: "mt-sm" }));
      el.innerHTML = "";
      el.appendChild(wrap);
    } catch (e) {
      if (!alive()) return;
      el.innerHTML = "";
      el.appendChild(App.emptyState("打开应用失败", e.message));
    }
  },

  async bootstrap(domain, dry) {
    if (!dry && !confirm("确定补齐「" + domain + "」缺失的表？已存在的表会跳过。")) return;
    const el = document.getElementById("domainPlan");
    if (el) el.innerHTML = '<div class="loading-state">处理中…</div>';
    try {
      const data = await App.api("/api/v1/data/templates/bootstrap", {
        method: "POST",
        body: JSON.stringify({ domains: [domain], dry_run: !!dry, skip_existing: true }),
      });
      if (!dry) {
        App.invalidateCache();
        App.refreshDatasetPicker();
        await this.open(domain);
        this.refresh();
      }
      const created = (data.would_create || []).map(t => t.id).concat(
        (data.created || []).map(c => (c.dataset || c).id).filter(Boolean)
      );
      const skipped = data.skipped || [];
      const errors = Object.keys(data.errors || {});
      const box = document.getElementById("domainPlan");
      if (box) {
        box.innerHTML = "";
        box.appendChild(App.html("p", { class: "card-desc" },
          (dry ? "将创建 " : "已创建 ") + created.length + " 张表，复用 " + skipped.length + " 张已有表" +
          (errors.length ? "，失败 " + errors.length + " 张" : "") + "。"
        ));
      }
    } catch (e) {
      const box = document.getElementById("domainPlan") || el;
      if (box) {
        box.innerHTML = "";
        box.appendChild(App.emptyState("失败", e.message));
      }
    }
  }
};

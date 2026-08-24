// MaClawDataSrv - User view: process + principle
"use strict";

PageModules.overview = {
  render(container) {
    const h = App.html;
    const t = (k) => I18N.t(k);

    container.appendChild(h("div", { class: "page-header" },
      h("div", {},
        h("h1", {}, "用户视图"),
        h("div", { class: "subtitle" }, "先看懂原理，再按快捷流程办事。人和 Agent 走同一条路。")
      ),
      h("div", { class: "header-actions" },
        h("button", { class: "ghost sm", type: "button", onclick: () => this.refresh() }, t("Refresh"))
      )
    ));

    container.appendChild(this.principleCard());
    container.appendChild(this.flowCard());

    container.appendChild(h("div", { class: "status-banner", "data-testid": "overview-health" },
      this.stat("业务表", "-", "ovDatasets"),
      this.stat("记录", "-", "ovRecords"),
      this.stat("待办", "-", "ovPending"),
      this.stat("逾期", "-", "ovOverdue"),
      this.stat("备份", "-", "ovBackups")
    ));

    container.appendChild(h("div", { id: "ovNext", class: "card next-action", "data-testid": "user-guide-next" },
      h("div", { class: "loading-state" }, "正在判断你该先做什么…")
    ));

    container.appendChild(h("h2", { class: "section-title" }, "已开通的业务应用"));
    container.appendChild(h("div", { id: "ovApps", class: "app-card-grid" },
      h("div", { class: "loading-state" }, "加载中…")
    ));

    container.appendChild(h("h2", { class: "section-title" }, "待办摘要"));
    container.appendChild(h("div", { id: "ovInbox", class: "mt-sm" },
      h("div", { class: "loading-state" }, "加载中…")
    ));

    container.appendChild(h("div", { class: "card mt-md" },
      h("div", { class: "card-header" }, h("h3", {}, "用一句话找下一步")),
      h("p", { class: "card-desc" }, "例如：报销、缺货、客户订单。系统会匹配业务场景，并给出人和 Agent 都能执行的步骤。"),
      h("div", { class: "form-row" },
        App.field("ovIntentQ", "你想做什么", "text", "提交报销、查库存、新建销售订单"),
        h("div", { class: "form-field", style: { alignSelf: "end" } },
          h("button", { class: "primary", type: "button", onclick: () => this.resolveIntent() }, "按快捷流程解析")
        )
      ),
      h("div", { id: "ovIntent", class: "mt-sm" })
    ));

    document.getElementById("ovIntentQ")?.addEventListener("keydown", e => {
      if (e.key === "Enter") {
        e.preventDefault();
        this.resolveIntent();
      }
    });
    this.refresh();
  },

  principleCard() {
    const h = App.html;
    return h("section", { class: "card principle-card", "data-testid": "user-guide" },
      h("div", { class: "card-header" },
        h("h3", {}, "整个系统怎么工作"),
        h("span", { class: "chip brand" }, "原理")
      ),
      h("p", { class: "card-desc" },
        "MaClawDataSrv 是企业信息的数据底座。你面对的是「业务应用」，不是数据库。应用下面才是表、记录和受控动作。"
      ),
      h("ol", { class: "principle-map", "data-testid": "user-guide-principle" },
        this.principleStep("1", "你或 Agent", "用人话办事，或让 Agent 调同一套 API。不要直接写 SQL。"),
        this.principleStep("2", "业务应用", "CRM、进销存、财务是业务包。开通时系统装配需要的表，已有客户/商品会复用。"),
        this.principleStep("3", "业务表 + 记录", "表是一类单据（订单、发票）。记录是一张具体单据。日常在「记录」里用字段表单录入。"),
        this.principleStep("4", "业务动作", "生产写入优先走动作：带校验、幂等和规则，比直接改记录更安全。"),
        this.principleStep("5", "待办 / 视图 / 审计", "审批和失败进待办；看数用视图报表；谁改了什么在审计里。Agent 密钥按最小权限发放。")
      )
    );
  },

  principleStep(num, title, desc) {
    const h = App.html;
    return h("li", { class: "principle-step" },
      h("span", { class: "step-num" }, num),
      h("div", {},
        h("strong", {}, title),
        h("p", {}, desc)
      )
    );
  },

  flowCard() {
    const h = App.html;
    const steps = [
      ["1", "看懂原理", "先知道应用、表、记录、动作的关系", "overview", "本页"],
      ["2", "开通应用", "选 CRM / 进销存 / 财务 → 预览 → 一键开通", "quickstart", "去开通"],
      ["3", "办事", "待办处理审批；记录里录入和查询", "records", "打开记录"],
      ["4", "受控写入", "生产变更用业务动作，先试运行再提交", "actions", "打开动作"],
      ["5", "授权 Agent", "发最小权限密钥，Agent 先 capabilities 再 intent", "apikeys", "发密钥"],
    ];
    const flow = h("div", { class: "user-flow", "data-testid": "user-guide-flow" });
    steps.forEach(([num, title, desc, page, label]) => {
      flow.appendChild(h("article", { class: "user-flow-step" },
        h("span", { class: "step-num" }, num),
        h("strong", {}, title),
        h("p", {}, desc),
        h("button", { class: "sm", type: "button", onclick: () => this.go(page) }, label)
      ));
    });
    return h("section", { class: "card" },
      h("div", { class: "card-header" },
        h("h3", {}, "快捷流程"),
        h("span", { class: "chip" }, "怎么用")
      ),
      h("p", { class: "card-desc" }, "按顺序做即可。不要先去建模或填 JSON。"),
      flow
    );
  },

  go(page) {
    if (page === "overview") {
      document.querySelector("[data-testid='user-guide']")?.scrollIntoView({ behavior: "smooth", block: "start" });
      return;
    }
    App.navigate(page);
  },

  stat(label, value, id) {
    return App.html("div", { class: "status-item" },
      App.html("div", { class: "status-label" }, label),
      App.html("div", { class: "status-value", id }, value)
    );
  },

  async refresh() {
    const alive = App.navGuard();
    const set = (id, v) => { const el = document.getElementById(id); if (el) el.textContent = v; };
    let datasets = 0;
    let pending = 0;
    const [statsRes, inboxRes] = await Promise.allSettled([
      App.api("/api/v1/data/stats"),
      App.api("/api/v1/data/inbox/summary"),
    ]);
    if (!alive()) return;
    if (statsRes.status === "fulfilled") {
      const stats = statsRes.value;
      datasets = stats.dataset_count ?? 0;
      set("ovDatasets", datasets);
      set("ovRecords", stats.record_count ?? 0);
      set("ovBackups", stats.backup_count ?? 0);
    }
    if (inboxRes.status === "fulfilled") {
      const summary = inboxRes.value;
      pending = summary.total ?? 0;
      set("ovPending", pending);
      set("ovOverdue", summary.overdue ?? 0);
    } else {
      set("ovPending", "-");
    }
    this.renderNext(datasets, pending);
    this.loadApps();
    this.loadInbox();
  },

  renderNext(datasets, pending) {
    const el = document.getElementById("ovNext");
    if (!el) return;
    let title = "下一步：开通一个业务应用";
    let desc = "还没有业务表。按快捷流程第 2 步：选 CRM / 进销存 / 财务，先预览再开通。";
    let label = "按流程开通应用";
    let page = "quickstart";
    if (pending > 0) {
      title = "下一步：处理待办";
      desc = "有 " + pending + " 件待处理（审批、失败任务或质量问题）。先清待办，再改数据。";
      label = "打开待办";
      page = "inbox";
    } else if (datasets > 0) {
      title = "下一步：在业务表里办事";
      desc = "应用已就绪。用字段表单录入或查询记录；生产写入请走业务动作。";
      label = "打开记录";
      page = "records";
    }
    el.innerHTML = "";
    el.appendChild(App.html("div", { class: "card-header" },
      App.html("h3", {}, title),
      App.html("span", { class: "chip brand" }, "推荐")
    ));
    el.appendChild(App.html("p", { class: "card-desc" }, desc));
    el.appendChild(App.html("div", { class: "btn-group" },
      App.html("button", { class: "primary", type: "button", onclick: () => App.navigate(page) }, label),
      App.html("button", { class: "sm", type: "button", onclick: () => App.navigate("quickstart") }, "再看一遍开通步骤")
    ));
  },

  async loadApps() {
    const alive = App.navGuard();
    const el = document.getElementById("ovApps");
    if (!el) return;
    try {
      const data = await App.api("/api/v1/data/domains?limit=50");
      if (!alive()) return;
      const items = App.listItems(data, "domains");
      if (!items.length) {
        el.innerHTML = "";
        el.appendChild(App.emptyState(
          "还没有业务应用",
          "从「开通应用」选择 CRM、进销存或财务。系统会预览并创建表。",
          [{ label: "开通应用", primary: true, onclick: () => App.navigate("quickstart") }]
        ));
        return;
      }
      el.innerHTML = "";
      const ready = items.filter(d => d.initialized || (d.datasets || []).length);
      if (!ready.length) {
        el.innerHTML = "";
        el.appendChild(App.emptyState(
          "还没有开通业务应用",
          "目录里虽有销售/财务等模板，但本租户还没有表。先按快捷流程开通。",
          [{ label: "开通应用", primary: true, onclick: () => App.navigate("quickstart") }]
        ));
        return;
      }
      el.innerHTML = "";
      ready.forEach(d => {
        const missing = (d.missing_templates || []).length;
        el.appendChild(App.html("button", {
          class: "card app-card",
          type: "button",
          onclick: () => App.navigate("domains", { domain: d.domain || d.id })
        },
          App.html("strong", {}, d.title || d.domain || d.id || "-"),
          App.html("div", { class: "app-meta" },
            (d.initialized ? "已开通" : "未初始化") +
            " · " + ((d.datasets || []).length) + " 张表" +
            (missing ? " · 缺 " + missing + " 个模板" : "")
          )
        ));
      });
    } catch (e) {
      if (!alive()) return;
      el.innerHTML = "";
      el.appendChild(App.emptyState("应用列表加载失败", e.message));
    }
  },

  async loadInbox() {
    const alive = App.navGuard();
    const el = document.getElementById("ovInbox");
    if (!el) return;
    try {
      const data = await App.api("/api/v1/data/inbox?limit=8");
      if (!alive()) return;
      const items = App.listItems(data);
      if (!items.length) {
        el.innerHTML = "";
        el.appendChild(App.emptyState("暂无待办", "审批、失败任务和质量问题会汇总到这里。"));
        return;
      }
      const rows = items.map(item => ({
        _id: item.id,
        _cells: [
          item.type || "-",
          item.status || "-",
          item.summary || item.title || "-",
          App.fmtTime(item.created_at)
        ]
      }));
      el.innerHTML = "";
      el.appendChild(App.table(["类型", "状态", "摘要", "时间"], rows, {
        onRowClick: (id) => PageModules.inbox.openItem(items.find(it => it.id === id))
      }));
    } catch (e) {
      if (!alive()) return;
      el.innerHTML = "";
      el.appendChild(App.emptyState("待办加载失败", e.message));
    }
  },

  async resolveIntent() {
    const el = document.getElementById("ovIntent");
    const q = App.val("ovIntentQ");
    if (!q) { App.toast("请输入你想做什么", "warn"); return; }
    if (el) el.innerHTML = '<div class="loading-state">按快捷流程解析…</div>';
    const alive = App.navGuard();
    try {
      const data = await App.api("/api/v1/data/intent/resolve", {
        method: "POST",
        body: JSON.stringify({ query: q, limit: 5 }),
      });
      if (!alive()) return;
      const matches = data.matches || [];
      if (!el) return;
      if (!matches.length) {
        el.innerHTML = "";
        el.appendChild(App.emptyState("没有匹配的业务场景", "换一个更具体的说法，例如「提交报销」或「查看库存」。"));
        return;
      }
      el.innerHTML = "";
      matches.forEach(m => {
        const steps = m.next_steps || [];
        const card = App.html("div", { class: "card mt-sm" },
          App.html("div", { class: "card-header" },
            App.html("h3", {}, (m.title || m.domain || "场景") + " · " + ((m.use_case && (m.use_case.title || m.use_case.id)) || "-")),
            App.html("span", { class: "chip" }, m.confidence || m.decision || "")
          )
        );
        if (steps.length) {
          const ol = App.html("ol", { class: "intent-steps" });
          steps.forEach(s => {
            ol.appendChild(App.html("li", {},
              (s.order ? s.order + ". " : "") + (s.description || s.action || "") +
              (s.dry_run ? "（试运行）" : "")
            ));
          });
          card.appendChild(ol);
        }
        const actionId = m.business_action_id || (m.use_case && m.use_case.preferred_action);
        if (actionId) {
          card.appendChild(App.html("div", { class: "btn-group mt-sm" },
            App.html("button", {
              class: "primary sm",
              type: "button",
              onclick: () => App.navigate("actions", { action: actionId })
            }, "按流程打开业务动作")
          ));
        }
        el.appendChild(card);
      });
    } catch (e) {
      if (!alive()) return;
      if (el) {
        el.innerHTML = "";
        el.appendChild(App.emptyState("解析失败", e.message));
      }
    }
  }
};

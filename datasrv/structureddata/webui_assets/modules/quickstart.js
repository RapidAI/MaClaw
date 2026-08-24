// MaClawDataSrv - Guided onboarding (select -> preview -> create -> authorize -> enter)
"use strict";

PageModules.quickstart = {
  _step: 1,
  _domain: "sales",
  _plan: null,

  render(container) {
    const h = App.html;

    container.appendChild(h("div", { class: "page-header" },
      h("div", {},
        h("h1", {}, "开通应用"),
        h("div", { class: "subtitle" }, "按快捷流程：选应用 → 预览 → 开通 → 授权 → 进入办事。不要从空白表开始。")
      )
    ));

    const steps = h("div", { class: "wizard-steps", "data-testid": "quickstart-panel" });
    [
      ["1", "选应用", "选 CRM、进销存或财务"],
      ["2", "预览", "看清会新建和复用哪些表"],
      ["3", "开通", "一键创建表、视图和动作"],
      ["4", "授权", "给人 / Agent 发最小权限"],
      ["5", "办事", "进入记录或待办"],
    ].forEach(([num, title, desc]) => {
      const n = Number(num);
      steps.appendChild(h("button", {
        class: "wizard-step",
        id: "qsStep" + num,
        type: "button",
        onclick: () => this.jumpStep(n)
      },
        h("span", { class: "step-num" }, num),
        h("strong", {}, title),
        h("p", {}, desc)
      ));
    });
    container.appendChild(steps);

    container.appendChild(h("div", { class: "guide-split" },
      this.principleAside(),
      h("div", { id: "qsMain" })
    ));

    container.appendChild(App.collapsible("单表模板（高级）", false, () => {
      const body = h("div", {});
      body.appendChild(h("p", { class: "card-desc" }, "只在已有应用上补一张表时使用。新手请走上面的业务包。"));
      body.appendChild(h("div", { id: "qsTemplates", class: "app-card-grid" },
        h("div", { class: "loading-state" }, "加载模板…")
      ));
      this.loadTemplates();
      return body;
    }));

    if (!this._step) this._step = 1;
    this._busy = false;
    this._previewGen = this._previewGen || 0;
    this.renderStep();
  },

  principleAside() {
    const h = App.html;
    return h("aside", { class: "card guide-aside" },
      h("h3", {}, "为什么这样开通"),
      h("p", { class: "card-desc" }, "用户看到的是应用，不是表。开通时后端按业务域装配模板，已存在的主数据会跳过。"),
      h("ul", { class: "guide-list" },
        h("li", {}, "CRM：客户、联系人、商机、订单"),
        h("li", {}, "进销存：供应商、商品、仓库、采购/销售"),
        h("li", {}, "财务：发票、收付款、报销；客户表会复用"),
        h("li", {}, "人和 Agent 之后都走同一套 capabilities / intent")
      )
    );
  },

  jumpStep(n) {
    if (n === this._step) return;
    if (n > 3 && !this._opened) return;
    if (n === 3 && !this._plan) {
      this.setStep(2);
      return;
    }
    if (n <= 3 || this._opened || n < this._step) this.setStep(n);
  },

  setStep(n) {
    if (n === 3 && !this._plan) n = 2;
    if (n > 3 && !this._opened) n = 3;
    this._step = n;
    this.renderStep();
  },

  renderStep() {
    const main = document.getElementById("qsMain");
    if (!main) return;
    main.innerHTML = "";
    if (this._step === 1) main.appendChild(this.stepChoose());
    else if (this._step === 2) main.appendChild(this.stepPreview());
    else if (this._step === 3) main.appendChild(this.stepCreate());
    else if (this._step === 4) main.appendChild(this.stepAuth());
    else main.appendChild(this.stepEnter());
    for (let i = 1; i <= 5; i++) {
      const el = document.getElementById("qsStep" + i);
      if (!el) continue;
      el.classList.toggle("active", i === this._step);
      const locked = i > 3 && !this._opened;
      el.classList.toggle("locked", locked);
      el.setAttribute("aria-disabled", locked ? "true" : "false");
    }
  },

  stepChoose() {
    const h = App.html;
    const card = h("div", { class: "card" },
      h("div", { class: "card-header" }, h("h3", {}, "第 1 步 · 选择业务应用")),
      h("p", { class: "card-desc" }, "选一个你马上要办的业务。系统会装配一组相关表，而不是让你从零建库。")
    );
    const apps = [
      ["sales", "CRM / 销售", "客户、联系人、商机、销售订单"],
      ["procurement", "采购", "供应商、采购单"],
      ["inventory", "库存", "商品、仓库、出入库"],
      ["finance", "财务", "发票、收付款、报销；复用已有客户"],
      ["hr", "人事", "员工、部门"],
      ["company", "组织主数据", "部门等共享基础表"],
    ];
    const grid = h("div", { class: "app-card-grid" });
    apps.forEach(([id, title, desc]) => {
      const selected = this._domain === id;
      grid.appendChild(h("button", {
        class: "card app-card" + (selected ? " selected" : ""),
        type: "button",
        onclick: () => this.selectDomain(id)
      },
        h("strong", {}, title),
        h("div", { class: "app-meta" }, desc)
      ));
    });
    card.appendChild(grid);
    card.appendChild(h("div", { class: "btn-group mt-md" },
      h("button", { class: "primary", type: "button", onclick: () => this.goPreview() }, "下一步：预览将创建的表")
    ));
    return card;
  },

  stepPreview() {
    const h = App.html;
    const card = h("div", { class: "card" },
      h("div", { class: "card-header" }, h("h3", {}, "第 2 步 · 预览")),
      h("p", { class: "card-desc" }, "开通前先看结果：将创建的是新表，已存在的主数据会复用，不会复制一份客户或商品。")
    );
    card.appendChild(h("div", { id: "qsPreview", class: "mt-sm" },
      h("div", { class: "loading-state" }, "预览中…")
    ));
    card.appendChild(h("div", { class: "btn-group mt-md" },
      h("button", { class: "sm", type: "button", onclick: () => this.setStep(1) }, "返回重选"),
      h("button", {
        class: "primary",
        id: "qsPreviewNext",
        type: "button",
        disabled: !this._plan,
        onclick: () => this.jumpStep(3)
      }, "下一步：确认开通")
    ));
    if (this._plan && this._planDomain === this._domain) {
      this.renderPlan(card.querySelector("#qsPreview"), this._plan, !this._opened);
      const next = card.querySelector("#qsPreviewNext");
      if (next) next.disabled = false;
    } else {
      this.previewInto("qsPreview").then(() => {
        const btn = document.getElementById("qsPreviewNext");
        if (btn && this._plan) btn.disabled = false;
      });
    }
    return card;
  },

  stepCreate() {
    const h = App.html;
    const card = h("div", { class: "card" },
      h("div", { class: "card-header" }, h("h3", {}, "第 3 步 · 开通")),
      h("p", { class: "card-desc" }, "确认后系统创建缺失的表、默认视图和业务动作。已存在的表会跳过。")
    );
    const preview = h("div", { id: "qsCreate", class: "mt-sm" });
    card.appendChild(preview);
    if (this._plan) this.renderPlan(preview, this._plan, !this._opened);
    const actions = h("div", { class: "btn-group mt-md" },
      h("button", { class: "sm", type: "button", onclick: () => this.setStep(2) }, "返回预览")
    );
    if (this._opened) {
      actions.appendChild(h("button", { class: "primary", type: "button", onclick: () => this.setStep(4) }, "下一步：授权"));
    } else {
      actions.appendChild(h("button", {
        class: "primary",
        id: "qsBootstrapBtn",
        type: "button",
        onclick: () => this.bootstrap()
      }, "一键开通"));
    }
    card.appendChild(actions);
    return card;
  },

  stepAuth() {
    const h = App.html;
    return h("div", { class: "card" },
      h("div", { class: "card-header" }, h("h3", {}, "第 4 步 · 授权")),
      h("p", { class: "card-desc" }, "人用管理员会话即可办事。Agent 需要一把最小权限密钥：先 GET /api/v1/data/capabilities，再 POST /api/v1/data/intent/resolve。"),
      h("div", { class: "btn-group mt-md" },
        h("button", { class: "sm", type: "button", onclick: () => this.setStep(3) }, "返回开通结果"),
        h("button", { class: "primary", type: "button", onclick: () => App.navigate("apikeys") }, "去发 Agent 密钥"),
        h("button", { type: "button", onclick: () => this.setStep(5) }, "跳过，直接办事")
      )
    );
  },

  stepEnter() {
    const h = App.html;
    const first = this.firstCreatedId();
    return h("div", { class: "card" },
      h("div", { class: "card-header" }, h("h3", {}, "第 5 步 · 进入办事")),
      h("p", { class: "card-desc" }, "应用已可用。日常在记录里用字段表单录入；生产写入走业务动作；审批和失败看待办。"),
      h("div", { class: "btn-group mt-md" },
        h("button", {
          class: "primary",
          type: "button",
          onclick: () => App.navigate("records", first ? { dataset: first } : {})
        }, "打开记录"),
        h("button", { type: "button", onclick: () => App.navigate("inbox") }, "打开待办"),
        h("button", { type: "button", onclick: () => App.navigate("actions") }, "打开业务动作"),
        h("button", { class: "ghost sm", type: "button", onclick: () => App.navigate("overview") }, "回到用户视图")
      )
    );
  },

  firstCreatedId() {
    const plan = this._plan || {};
    const created = plan.created || [];
    if (created.length) return (created[0].dataset || created[0] || {}).id || "";
    const skipped = plan.skipped || [];
    if (skipped.length) return skipped[0] || "";
    const would = plan.would_create || [];
    if (would.length) return would[0].id || "";
    return "";
  },

  domainTitle() {
    const hit = [
      ["sales", "CRM / 销售"],
      ["procurement", "采购"],
      ["inventory", "库存"],
      ["finance", "财务"],
      ["hr", "人事"],
      ["company", "组织主数据"],
    ].find(row => row[0] === this._domain);
    return hit ? hit[1] : this._domain;
  },

  selectDomain(id) {
    if (this._domain !== id) {
      this._domain = id;
      this._plan = null;
      this._planDomain = "";
      this._opened = false;
      this._previewGen = (this._previewGen || 0) + 1;
    }
    this.renderStep();
  },

  async goPreview() {
    this.setStep(2);
  },

  async previewInto(id) {
    const el = document.getElementById(id);
    if (!el) return;
    this._previewGen = (this._previewGen || 0) + 1;
    const gen = this._previewGen;
    const domain = this._domain;
    el.innerHTML = '<div class="loading-state">预览中…</div>';
    try {
      const data = await App.api("/api/v1/data/templates/bootstrap", {
        method: "POST",
        body: JSON.stringify({ domains: [domain], dry_run: true, skip_existing: true }),
      });
      if (gen !== this._previewGen) return;
      this._plan = data;
      this._planDomain = domain;
      const box = document.getElementById(id);
      if (box) this.renderPlan(box, data, true);
    } catch (e) {
      if (gen !== this._previewGen) return;
      const box = document.getElementById(id);
      if (!box) return;
      box.innerHTML = "";
      box.appendChild(App.emptyState("预览失败", e.message));
    }
  },

  async preview() {
    await this.goPreview();
  },

  async bootstrap() {
    if (this._busy) return;
    if (!confirm("确定开通「" + this.domainTitle() + "」？已存在的表会跳过。")) return;
    const el = document.getElementById("qsCreate") || document.getElementById("qsPreview");
    const btn = document.getElementById("qsBootstrapBtn");
    if (btn) btn.disabled = true;
    this._busy = true;
    if (el) el.innerHTML = '<div class="loading-state">开通中…</div>';
    try {
      const data = await App.api("/api/v1/data/templates/bootstrap", {
        method: "POST",
        body: JSON.stringify({ domains: [this._domain], dry_run: false, skip_existing: true }),
      });
      this._plan = data;
      this._planDomain = this._domain;
      const failedOnly = this.planFailedOnly(data);
      this._opened = !failedOnly;
      App.invalidateCache();
      App.refreshDatasetPicker();
      const first = this.firstCreatedId();
      if (first) App.setDataset(first);
      if (el) this.renderPlan(el, data, false);
      App.toast(failedOnly ? "开通未完成，请查看失败项" : "应用已开通", failedOnly ? "danger" : "ok");
      this.renderStep();
    } catch (e) {
      if (el) {
        el.innerHTML = "";
        el.appendChild(App.emptyState("开通失败", e.message));
      }
      if (btn) btn.disabled = false;
    } finally {
      this._busy = false;
    }
  },

  async loadTemplates() {
    const el = document.getElementById("qsTemplates");
    if (!el) return;
    try {
      const data = await App.api("/api/v1/data/templates?limit=200");
      const items = App.listItems(data, "templates");
      if (!items.length) {
        el.innerHTML = "";
        el.appendChild(App.emptyState("没有可用模板", "服务内置模板未加载。"));
        return;
      }
      el.innerHTML = "";
      items.forEach(tmpl => {
        el.appendChild(App.html("div", { class: "card app-card" },
          App.html("strong", {}, tmpl.title || tmpl.id),
          App.html("div", { class: "app-meta" }, (tmpl.domain || "") + " · " + tmpl.id),
          App.html("div", { class: "btn-group mt-sm" },
            App.html("button", { class: "sm", type: "button", onclick: () => this.createOne(tmpl.id) }, "只创建这一张表")
          )
        ));
      });
    } catch (e) {
      el.innerHTML = "";
      el.appendChild(App.emptyState("模板加载失败", e.message));
    }
  },

  async createOne(templateId) {
    if (this._busy) return;
    this._busy = true;
    try {
      const data = await App.api("/api/v1/data/templates/" + encodeURIComponent(templateId) + "/create", {
        method: "POST",
        body: JSON.stringify({}),
      });
      App.invalidateCache();
      App.refreshDatasetPicker();
      const ds = data.dataset || data;
      if (ds && ds.id) {
        App.setDataset(ds.id);
        App.toast("已创建 " + ds.id);
        App.navigate("records", { dataset: ds.id });
      } else {
        App.toast("已创建模板表");
      }
    } catch (e) {
      App.toast(e.message || "创建失败", "danger");
    } finally {
      this._busy = false;
    }
  },

  planFailedOnly(data) {
    const created = (data.created || []).length;
    const skipped = (data.skipped || []).length;
    const errors = Object.keys(data.errors || {}).length;
    return errors > 0 && created === 0 && skipped === 0;
  },

  renderPlan(el, data, dry) {
    if (!el) return;
    const created = data.created || [];
    const would = data.would_create || [];
    const skipped = data.skipped || [];
    const errors = data.errors || {};
    const rows = [];
    would.forEach(t => rows.push({ _cells: [t.id || t.title || "-", t.domain || "-", "将创建"] }));
    created.forEach(c => {
      const ds = c.dataset || c;
      rows.push({ _cells: [ds.id || "-", ds.domain || "-", dry ? "将创建" : "已创建"] });
    });
    skipped.forEach(id => rows.push({ _cells: [id, "-", "已存在，复用"] }));
    Object.keys(errors).forEach(id => rows.push({ _cells: [id, "-", "失败：" + errors[id]] }));
    el.innerHTML = "";
    if (!rows.length) {
      el.appendChild(App.emptyState(dry ? "没有需要新建的表" : "没有变更", "该业务域的表已经存在，可以直接办事。"));
      return;
    }
    el.appendChild(App.table(["表", "域", "结果"], rows));
  }
};

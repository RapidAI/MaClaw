// MaClawDataSrv - Business Actions Module
"use strict";

PageModules.actions = {
  _selected: null,

  render(container) {
    const h = App.html;
    const t = (k) => I18N.t(k);

    container.appendChild(h("div", { class: "page-header" },
      h("div", {},
        h("h1", {}, t("Business Actions")),
        h("div", { class: "subtitle" }, t("Execute governed business operations with rule checks and audit trails."))
      ),
      h("button", { class: "ghost sm", type: "button", onclick: () => this.refreshActions() }, t("Refresh"))
    ));

    container.appendChild(h("div", { class: "card" },
      h("div", { class: "card-header" },
        h("h3", {}, t("Action List")),
        h("div", { class: "text-muted text-sm" }, "选择一个业务动作，先试运行再提交")
      ),
      h("div", { id: "actionList", class: "mt-sm" },
        h("div", { class: "empty-state" }, "加载中...")
      )
    ));

    container.appendChild(this.buildCollapsible("execAction", t("Execute Action"), true, () => {
      const body = h("div", {});
      body.appendChild(h("div", { class: "form-row" },
        h("div", { class: "form-field" },
          h("label", {}, t("Action ID")),
          h("input", { id: "actionId", readonly: true, class: "mono", style: { background: "var(--panel-2)" } })
        ),
        h("div", { class: "form-field" },
          h("label", {}, t("Target Dataset")),
          h("input", { id: "actionDataset", readonly: true, style: { background: "var(--panel-2)" } })
        )
      ));
      body.appendChild(h("div", { class: "form-field" },
        h("label", {}, t("Description")),
        h("textarea", { id: "actionDesc", readonly: true, style: { minHeight: "60px", background: "var(--panel-2)" } })
      ));
      body.appendChild(h("div", { id: "actionFieldForm", class: "mt-md" }));
      body.appendChild(h("div", { class: "form-field mt-md" },
        h("label", {}, t("Input JSON")),
        h("textarea", { id: "actionInput", placeholder: '{\n  "field": "value"\n}' })
      ));
      body.appendChild(h("div", { class: "form-row" },
        h("div", { class: "form-field" },
          h("label", {}, t("Record ID") + "（可选）"),
          h("input", { id: "actionRecordId", placeholder: "外部业务 ID" })
        ),
        h("div", { class: "form-field" },
          h("label", {}, t("Idempotency Key") + "（可选）"),
          h("input", { id: "actionIdempotencyKey", placeholder: "source:object:id:version" })
        )
      ));
      body.appendChild(h("div", { class: "btn-group mt-md" },
        h("button", { type: "button", onclick: () => this.dryRun() }, t("Dry-run") + "（试运行）"),
        h("button", { class: "primary", type: "button", onclick: () => this.execute() }, t("Execute") + "（执行）"),
        h("button", { class: "ghost sm", type: "button", onclick: () => this.checkRules() }, t("Check rules"))
      ));
      body.appendChild(h("div", { id: "actionResult", class: "table-wrap mt-md" }));
      return body;
    }));

    container.appendChild(this.buildCollapsible("eventContracts", t("Event Contracts"), false, () => {
      const body = h("div", {});
      body.appendChild(h("p", { class: "card-desc" }, "查看业务动作触发的事件契约定义。"));
      body.appendChild(h("div", { class: "form-row" },
        h("div", { class: "form-field" },
          h("label", {}, "域筛选"),
          h("input", { id: "contractDomain", placeholder: "如：sales" })
        ),
        h("div", { class: "form-field", style: { alignSelf: "end" } },
          h("button", { class: "sm", type: "button", onclick: () => this.refreshContracts() }, "刷新契约")
        )
      ));
      body.appendChild(h("div", { id: "contractTable", class: "table-wrap mt-sm" }));
      return body;
    }));

    this.refreshActions();
  },

  buildCollapsible(id, title, defaultOpen, contentFn) {
    return App.collapsible(title, defaultOpen, contentFn);
  },

  async refreshActions() {
    const alive = App.navGuard();
    try {
      const data = await App.api("/api/v1/data/business-actions");
      if (!alive()) return;
      const items = App.listItems(data, "actions");
      const el = document.getElementById("actionList");
      if (!el) return;
      if (!items.length) {
        el.innerHTML = '<div class="empty-state">暂无业务动作。请先通过「开通应用」初始化业务域。</div>';
        return;
      }
      el.innerHTML = "";
      items.forEach(a => {
        const card = App.html("button", {
          class: "card",
          type: "button",
          style: { textAlign: "left", width: "100%", marginBottom: "6px", cursor: "pointer" },
          onclick: () => this.selectAction(a)
        },
          App.html("strong", {}, a.title || a.id),
          App.html("div", { class: "text-muted text-sm" }, a.id + " · " + (a.dataset_id || a.dataset || ""))
        );
        el.appendChild(card);
      });
      const wanted = App.hashState().params.action;
      if (wanted) {
        const hit = items.find(a => a.id === wanted);
        if (hit) this.selectAction(hit);
      }
    } catch (e) {
      if (!alive()) return;
      const el = document.getElementById("actionList");
      if (el) el.innerHTML = '<div class="empty-state">加载失败</div>';
    }
  },

  async selectAction(actionOrId) {
    const id = typeof actionOrId === "string" ? actionOrId : (actionOrId && actionOrId.id);
    if (!id) return;
    this._selectGen = (this._selectGen || 0) + 1;
    const gen = this._selectGen;
    App.setVal("actionId", id);
    let action = actionOrId && typeof actionOrId === "object" ? actionOrId : null;
    try {
      action = await App.api("/api/v1/data/business-actions/" + encodeURIComponent(id));
    } catch (_) { /* list payload is enough */ }
    if (gen !== this._selectGen) return;
    if (!action) {
      App.toast("无法加载业务动作 " + id, "danger");
      return;
    }
    this._selected = action;
    App.setVal("actionDataset", action.dataset_id || action.dataset || "");
    App.setVal("actionDesc", action.description || action.title || "");
    const fields = action.input_fields || [];
    const box = document.getElementById("actionFieldForm");
    if (box) {
      box.innerHTML = "";
      if (fields.length) box.appendChild(App.fieldControls(fields, {}, "act"));
    }
    if (action.dataset_id) App.setDataset(action.dataset_id);
  },

  collectInput() {
    const fromFields = this._selected ? App.collectFieldValues(this._selected.input_fields || [], "act") : {};
    const raw = document.getElementById("actionInput")?.value || "";
    if (!raw.trim()) return fromFields;
    try {
      return Object.assign({}, App.parseJSON(raw), fromFields);
    } catch (_) {
      throw new Error("Input JSON 无效");
    }
  },

  async run(dry) {
    const id = App.val("actionId");
    if (!id) { App.toast("请先选择业务动作", "warn"); return; }
    if (this._busy) return;
    this._busy = true;
    const el = document.getElementById("actionResult");
    if (el) el.innerHTML = '<div class="loading-state">执行中…</div>';
    try {
      const out = await App.api("/api/v1/data/business-actions/" + encodeURIComponent(id) + "/execute", {
        method: "POST",
        body: JSON.stringify({
          record_id: App.val("actionRecordId") || undefined,
          idempotency_key: App.val("actionIdempotencyKey") || undefined,
          data: this.collectInput(),
          dry_run: !!dry,
        }),
      });
      if (el) {
        el.innerHTML = "";
        el.appendChild(App.resultCard(dry ? "试运行结果" : "执行结果", out));
      }
      if (!dry) App.toast("执行完成");
    } catch (e) {
      if (el) {
        el.innerHTML = "";
        el.appendChild(App.emptyState(dry ? "试运行失败" : "执行失败", e.message));
      }
    } finally {
      this._busy = false;
    }
  },

  dryRun() { this.run(true); },
  execute() {
    if (!confirm("确定执行此业务动作？")) return;
    this.run(false);
  },

  async checkRules() {
    const action = this._selected;
    if (!action) { App.toast("请先选择业务动作", "warn"); return; }
    let input;
    try {
      input = this.collectInput();
    } catch (e) {
      App.toast(e.message || "Input JSON 无效", "danger");
      return;
    }
    try {
      const out = await App.api("/api/v1/data/business-rules/evaluate", {
        method: "POST",
        body: JSON.stringify({
          business_action_id: action.id,
          dataset_id: action.dataset_id,
          data: input,
        }),
      });
      const el = document.getElementById("actionResult");
      if (el) {
        el.innerHTML = "";
        el.appendChild(App.resultCard("规则检查", out));
      }
    } catch (e) {
      App.toast(e.message || "规则检查失败", "danger");
    }
  },

  async refreshContracts() {
    try {
      const data = await App.api("/api/v1/data/event-contracts" + App.qs({ domain: App.val("contractDomain"), limit: 100 }));
      const items = App.listItems(data);
      const el = document.getElementById("contractTable");
      if (!el) return;
      if (!items.length) {
        el.innerHTML = "";
        el.appendChild(App.emptyState("暂无事件契约", "业务动作执行后会产生对应事件。"));
        return;
      }
      const rows = items.map(c => ({
        _cells: [c.action_id || c.id || "-", c.event_type || "-", c.domain || "-"]
      }));
      el.innerHTML = "";
      el.appendChild(App.table(["动作", "事件", "域"], rows));
    } catch (e) {
      const el = document.getElementById("contractTable");
      if (el) {
        el.innerHTML = "";
        el.appendChild(App.emptyState("加载失败", e.message));
      }
    }
  },
};

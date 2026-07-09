// MaClawDataSrv - Business Actions Module
"use strict";

PageModules.actions = {
  render(container) {
    const h = App.html;
    const t = (k) => I18N.t(k);

    // === Page Header ===
    container.appendChild(h("div", { class: "page-header" },
      h("div", {},
        h("h1", {}, t("Business Actions")),
        h("div", { class: "subtitle" }, t("Execute governed business operations with rule checks and audit trails."))
      ),
      h("button", { class: "ghost sm", onclick: () => this.refreshActions() }, t("Refresh"))
    ));

    // === Section 1: Action List (Always Visible) ===
    container.appendChild(h("div", { class: "card" },
      h("div", { class: "card-header" },
        h("h3", {}, t("Action List")),
        h("div", { class: "text-muted text-sm" }, "选择一个业务动作，然后在下方执行")
      ),
      h("div", { id: "actionList", class: "mt-sm" },
        h("div", { class: "empty-state" }, "加载中...")
      )
    ));

    // === Section 2: Execute Action (Shown after selecting) ===
    container.appendChild(this.buildCollapsible("execAction", "▶️ " + t("Execute Action"), true, () => {
      const body = h("div", {});

      // Read-only info
      const info = h("div", { class: "form-row" },
        h("div", { class: "form-field" },
          h("label", {}, t("Action ID")),
          h("input", { id: "actionId", readonly: true, class: "mono", style: { background: "var(--panel-2)" } })
        ),
        h("div", { class: "form-field" },
          h("label", {}, t("Target Dataset")),
          h("input", { id: "actionDataset", readonly: true, style: { background: "var(--panel-2)" } })
        )
      );
      body.appendChild(info);

      body.appendChild(h("div", { class: "form-field" },
        h("label", {}, t("Description")),
        h("textarea", { id: "actionDesc", readonly: true, style: { minHeight: "60px", background: "var(--panel-2)" } })
      ));

      // Input area
      body.appendChild(h("div", { class: "form-field mt-md" },
        h("label", {}, t("Input JSON")),
        h("textarea", { id: "actionInput", placeholder: '{\n  "field": "value"\n}' })
      ));

      const extraRow = h("div", { class: "form-row" },
        h("div", { class: "form-field" },
          h("label", {}, t("Record ID") + "（可选）"),
          h("input", { id: "actionRecordId", placeholder: "外部业务 ID" })
        ),
        h("div", { class: "form-field" },
          h("label", {}, t("Idempotency Key") + "（可选）"),
          h("input", { id: "actionIdempotencyKey", placeholder: "source:object:id:version" })
        )
      );
      body.appendChild(extraRow);

      // Action buttons - clear hierarchy: Dry-run (safe) → Execute (production)
      const btns = h("div", { class: "btn-group mt-md" },
        h("button", { onclick: () => this.dryRun() }, t("Dry-run") + "（试运行）"),
        h("button", { class: "primary", onclick: () => this.execute() }, t("Execute") + "（执行）"),
        h("button", { class: "ghost sm", onclick: () => this.checkRules() }, t("Check rules"))
      );
      body.appendChild(btns);

      // Result
      body.appendChild(h("div", { id: "actionResult", class: "table-wrap mt-md" }));

      return body;
    }));

    // === Section 3: Event Contracts (Collapsed) ===
    container.appendChild(this.buildCollapsible("eventContracts", "📜 " + t("Event Contracts"), false, () => {
      const body = h("div", {});
      body.appendChild(h("p", { class: "card-desc" }, "查看业务动作触发的事件契约定义。"));

      const filterRow = h("div", { class: "form-row" },
        h("div", { class: "form-field" },
          h("label", {}, "域筛选"),
          h("input", { id: "contractDomain", placeholder: "如：sales" })
        ),
        h("div", { class: "form-field", style: { alignSelf: "end" } },
          h("button", { class: "sm", onclick: () => this.refreshContracts() }, "刷新契约")
        )
      );
      body.appendChild(filterRow);
      body.appendChild(h("div", { id: "contractTable", class: "table-wrap mt-sm" }));

      return body;
    }));

    this.refreshActions();
  },

  // === Helpers ===
  buildCollapsible(id, title, defaultOpen, contentFn) {
    return App.collapsible(title, defaultOpen, contentFn);
  },

  // === Actions ===
  async refreshActions() {
    try {
      const data = await App.api("/api/v1/data/business-actions");
      const el = document.getElementById("actionList");
      if (!el) return;
      if (!data.actions || !data.actions.length) {
        el.innerHTML = '<div class="empty-state">暂无业务动作。请先通过"数据集"模块创建数据集。</div>';
        return;
      }
      const h = App.html;
      el.innerHTML = "";
      data.actions.forEach(a => {
        const card = h("button", { class: "card", style: { textAlign: "left", width: "100%", marginBottom: "6px", cursor: "pointer" }, onclick: () => this.selectAction(a.id) },
          h("strong", { class: "mono" }, a.id),
          h("div", { class: "text-muted text-sm" }, a.description || a.dataset || "")
        );
        el.appendChild(card);
      });
    } catch (e) {
      const el = document.getElementById("actionList");
      if (el) el.innerHTML = '<div class="empty-state">加载失败</div>';
    }
  },

  selectAction(id) {
    const el = document.getElementById("actionId");
    if (el) el.value = id;
    App.toast("已选择: " + id);
  },

  dryRun() { App.toast("试运行中..."); },
  execute() { if (confirm("确定执行此业务动作？")) App.toast("执行中..."); },
  checkRules() { App.toast("检查规则..."); },
  refreshContracts() { App.toast("刷新事件契约..."); },
};

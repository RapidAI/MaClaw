/*
 * Shared admin UI helpers.
 * ASCII only.
 */
(function(global) {
  function attrsToString(attrs) {
    return Object.keys(attrs || {}).map(function(key) {
      const value = attrs[key];
      if (value === null || typeof value === 'undefined' || value === false) return '';
      if (value === true) return ' ' + key;
      return ' ' + key + '="' + escapeHtml(String(value)) + '"';
    }).join('');
  }
  function hint(message, extraClass) {
    return '<div class="hint' + (extraClass ? ' ' + extraClass : '') + '">' + escapeHtml(message || '') + '</div>';
  }
  function meta(text, extraClass, inlineStyle) {
    const cls = 'item-meta' + (extraClass ? ' ' + extraClass : '');
    const style = inlineStyle ? ' style="' + escapeHtml(inlineStyle) + '"' : '';
    return '<div class="' + cls + '"' + style + '>' + escapeHtml(text || '') + '</div>';
  }
  function badge(text, type, attrs) {
    return '<span class="badge ' + escapeHtml(type || 'info') + '"' + attrsToString(attrs) + '>' + escapeHtml(text || '') + '</span>';
  }
  function actionButton(text, className, onclick, attrs) {
    const nextAttrs = Object.assign({}, attrs || {});
    if (!nextAttrs.type) nextAttrs.type = 'button';
    if (onclick) nextAttrs.onclick = onclick;
    return '<button class="' + escapeHtml(className || 'btn-secondary') + '"' + attrsToString(nextAttrs) + '>' + escapeHtml(text || '') + '</button>';
  }
  function enhanceButtonTypes(root) {
    const scope = root || global.document;
    if (!scope || !scope.querySelectorAll) return;
    scope.querySelectorAll('button:not([type])').forEach(function(button) {
      button.type = 'button';
    });
  }
  function enhanceLanguageSwitchStates(root) {
    const scope = root || global.document;
    if (!scope || !scope.querySelectorAll) return;
    scope.querySelectorAll('.lang-switch button').forEach(function(button) {
      button.setAttribute('aria-pressed', button.classList.contains('active') ? 'true' : 'false');
    });
  }
  function enhanceAdminNavigation(root) {
    const scope = root || global.document;
    if (!scope || !scope.querySelectorAll) return;
    scope.querySelectorAll('.nav button[data-tab]').forEach(function(button) {
      const active = button.classList.contains('active');
      button.setAttribute('aria-current', active ? 'page' : 'false');
      var group = button.closest('.nav-group');
      const hidden = button.classList.contains('hidden') || !!(group && group.classList.contains('hidden'));
      button.tabIndex = (active && !hidden) ? 0 : -1;
    });
  }
  function bindAdminNavigationKeyboard() {
    if (!global.document || bindAdminNavigationKeyboard.done) return;
    bindAdminNavigationKeyboard.done = true;
    global.document.addEventListener('keydown', function(event) {
      const nav = event.target && event.target.closest ? event.target.closest('.nav') : null;
      if (!nav) return;
      const keys = ['ArrowDown', 'ArrowRight', 'ArrowUp', 'ArrowLeft', 'Home', 'End'];
      if (keys.indexOf(event.key) === -1) return;
      const buttons = Array.prototype.slice.call(nav.querySelectorAll('button[data-tab]')).filter(function(btn) {
        if (btn.classList.contains('hidden')) return false;
        var group = btn.closest('.nav-group');
        return !(group && group.classList.contains('hidden'));
      });
      const index = buttons.indexOf(global.document.activeElement);
      if (index < 0) return;
      event.preventDefault();
      let next = index;
      if (event.key === 'Home') next = 0;
      else if (event.key === 'End') next = buttons.length - 1;
      else next = (index + (event.key === 'ArrowUp' || event.key === 'ArrowLeft' ? -1 : 1) + buttons.length) % buttons.length;
      buttons[next].focus();
      buttons[next].click();
    });
  }
  bindAdminNavigationKeyboard.done = false;
  function installButtonTypeNormalizer() {
    if (!global.document || installButtonTypeNormalizer.done) return;
    installButtonTypeNormalizer.done = true;
    enhanceButtonTypes(global.document);
    enhanceLanguageSwitchStates(global.document);
    enhanceAdminNavigation(global.document);
    if (!global.MutationObserver) return;
    const observer = new global.MutationObserver(function(records) {
      records.forEach(function(record) {
        record.addedNodes.forEach(function(node) {
          if (!node || node.nodeType !== 1) return;
          if (node.matches && node.matches('button:not([type])')) node.type = 'button';
          enhanceButtonTypes(node);
          enhanceLanguageSwitchStates(node);
          enhanceAdminNavigation(node);
        });
        if (record.type === 'attributes' && record.target && record.target.matches && record.target.matches('.lang-switch button')) {
          enhanceLanguageSwitchStates(record.target.parentElement || global.document);
        }
        if (record.type === 'attributes' && record.target && record.target.matches && record.target.matches('.nav button[data-tab]')) {
          enhanceAdminNavigation(record.target.closest('.nav') || global.document);
        }
      });
    });
    observer.observe(global.document.documentElement, { childList: true, subtree: true, attributes: true, attributeFilter: ['class'] });
  }
  installButtonTypeNormalizer.done = false;
  function simpleCard(options) {
    const opts = options || {};
    const style = opts.style ? ' style="' + escapeHtml(opts.style) + '"' : '';
    const attrs = attrsToString(opts.attrs || {});
    const title = opts.title ? '<div class="item-title">' + escapeHtml(opts.title) + '</div>' : '';
    const titleMeta = opts.titleMeta ? '<div class="item-meta' + (opts.titleMetaClass ? ' ' + opts.titleMetaClass : '') + '">' + escapeHtml(opts.titleMeta) + '</div>' : '';
    const headRight = opts.headRight || '';
    const body = Array.isArray(opts.body) ? opts.body.join('') : (opts.body || '');
    return '<div class="item"' + style + attrs + '><div class="item-head"><div>' + title + titleMeta + '</div>' + headRight + '</div>' + body + '</div>';
  }
  function renderList(items, renderItem, emptyMessage) {
    const list = Array.isArray(items) ? items : [];
    if (!list.length) return hint(emptyMessage || '');
    return list.map(function(item, index) {
      return renderItem(item, index);
    }).join('');
  }
  function rememberBackdropPointer(event, overlay) {
    if (!overlay) return;
    overlay.__adminBackdropPointerStarted = !!(event && event.target === overlay);
  }
  function dismissBackdropClick(event, overlay, closeFn) {
    if (!overlay || !event || event.target !== overlay) return;
    const startedOnBackdrop = !!overlay.__adminBackdropPointerStarted;
    overlay.__adminBackdropPointerStarted = false;
    if (!startedOnBackdrop) return;
    if (typeof closeFn === 'function') closeFn();
  }
  function bindModalOverlayDismiss(overlay, closeFn) {
    if (!overlay) return;
    overlay.onclick = null;
    overlay.onpointerdown = function(event) { rememberBackdropPointer(event, overlay); };
    overlay.onmousedown = function(event) { rememberBackdropPointer(event, overlay); };
    overlay.onclick = function(event) { dismissBackdropClick(event, overlay, closeFn); };
  }
  function installModalBackdropGuard() {
    if (!global.document || installModalBackdropGuard.done) return;
    installModalBackdropGuard.done = true;
    const markStart = function(event) {
      const overlay = event && event.target && event.target.classList && event.target.classList.contains('session-modal-overlay') ? event.target : null;
      if (overlay) rememberBackdropPointer(event, overlay);
    };
    const guardClick = function(event) {
      const overlay = event && event.target && event.target.classList && event.target.classList.contains('session-modal-overlay') ? event.target : null;
      if (!overlay) return;
      if (!overlay.__adminBackdropPointerStarted) {
        overlay.__adminBackdropPointerStarted = false;
        event.stopImmediatePropagation();
        event.preventDefault();
      }
    };
    global.document.addEventListener('pointerdown', markStart, true);
    global.document.addEventListener('mousedown', markStart, true);
    global.document.addEventListener('click', guardClick, true);
  }
  installModalBackdropGuard.done = false;
  installModalBackdropGuard();
  bindAdminNavigationKeyboard();
  installButtonTypeNormalizer();

  function dialogEsc(s) {
    if (typeof global.escapeHtml === 'function') return global.escapeHtml(s);
    return String(s == null ? '' : s)
      .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }
  function dialogLang() {
    if (typeof global.getAdminLang === 'function') {
      return global.getAdminLang() || 'zh';
    }
    return 'zh';
  }
  function dialogTrFallback(en, zh) {
    return dialogLang() === 'zh' ? zh : en;
  }
  function dialogCancelLabel(options) {
    if (options && options.cancelText) return options.cancelText;
    if (typeof global.tr === 'function') {
      var t = global.tr('closeDialog');
      if (t && t !== 'closeDialog') return t;
    }
    return dialogTrFallback('Cancel', '\u53d6\u6d88');
  }
  function dialogOkLabel(options) {
    if (options && options.confirmText) return options.confirmText;
    return dialogTrFallback('OK', '\u786e\u5b9a');
  }
  function dialogConfirmTitle(options) {
    if (options && options.title) return options.title;
    return dialogTrFallback('Confirm', '\u786e\u8ba4');
  }
  function dialogPromptTitle(options) {
    if (options && options.title) return options.title;
    return dialogTrFallback('Input', '\u8f93\u5165');
  }
  function dialogRequiredHint(options) {
    if (options && options.requiredText) return options.requiredText;
    return dialogTrFallback('This field is required.', '\u6b64\u9879\u4e3a\u5fc5\u586b\u3002');
  }
  // Must sit above feature overlays (digital-assets content=11000, progress=12000).
  var DIALOG_Z_INDEX = 20000;
  var activeDialogSession = null;
  var bodyScrollLockCount = 0;
  var bodyScrollLockPrev = '';

  function removeDialogOverlay(overlay) {
    if (overlay && overlay.parentNode) overlay.parentNode.removeChild(overlay);
  }

  function isNodeConnected(node) {
    if (!node) return false;
    if (typeof node.isConnected === 'boolean') return node.isConnected;
    return !!(global.document && global.document.contains && global.document.contains(node));
  }

  function lockBodyScroll() {
    if (!global.document || !global.document.body) return;
    if (bodyScrollLockCount === 0) {
      bodyScrollLockPrev = global.document.body.style.overflow || '';
      global.document.body.style.overflow = 'hidden';
    }
    bodyScrollLockCount += 1;
  }

  function unlockBodyScroll() {
    if (!global.document || !global.document.body) return;
    if (bodyScrollLockCount <= 0) {
      bodyScrollLockCount = 0;
      return;
    }
    bodyScrollLockCount -= 1;
    if (bodyScrollLockCount === 0) {
      global.document.body.style.overflow = bodyScrollLockPrev;
      bodyScrollLockPrev = '';
    }
  }

  function createDialogOverlay(id) {
    // Drop orphan DOM with the same id (e.g. interrupted previous paint). Active
    // sessions are closed via dismissActiveDialog before mount, not here.
    var existing = global.document.getElementById(id);
    if (existing && existing.parentNode) {
      if (!activeDialogSession || activeDialogSession.overlay !== existing) {
        existing.parentNode.removeChild(existing);
      }
    }
    var overlay = global.document.createElement('div');
    overlay.id = id;
    overlay.className = 'session-modal-overlay show admin-ui-dialog-overlay';
    overlay.style.cssText = 'position:fixed;inset:0;z-index:' + DIALOG_Z_INDEX
      + ';background:rgba(15,23,42,.42);padding:18px;display:flex;align-items:center;justify-content:center;box-sizing:border-box';
    return overlay;
  }

  function dialogPanelStyle() {
    return 'width:min(420px,100%);max-height:min(90vh,720px);overflow:auto;border:1px solid var(--border,#d8dee9);border-radius:12px;padding:16px;box-shadow:0 18px 60px rgba(15,23,42,.22);background:var(--panel-strong,var(--panel,#fff));box-sizing:border-box';
  }

  function focusableIn(root) {
    if (!root || !root.querySelectorAll) return [];
    var nodes = root.querySelectorAll('button:not([disabled]), [href], input:not([disabled]):not([type="hidden"]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])');
    return Array.prototype.slice.call(nodes).filter(function(el) {
      if (el.getAttribute && el.getAttribute('aria-hidden') === 'true') return false;
      return !!(el.offsetWidth || el.offsetHeight || el.getClientRects().length);
    });
  }

  function isDialogOpen() {
    return !!(activeDialogSession && activeDialogSession.overlay
      && activeDialogSession.overlay.parentNode);
  }

  // Cancel any open AdminUI dialog so its Promise settles (avoids leaks when reopening).
  // Always uses the previous session's own cancelValue (confirm=false, prompt=null).
  function dismissActiveDialog() {
    if (!activeDialogSession || typeof activeDialogSession.close !== 'function') return;
    var session = activeDialogSession;
    activeDialogSession = null;
    session.close(session.cancelValue);
  }

  function isImeComposing(event) {
    // CJK IME: do not treat Enter/Esc as dialog actions while composing.
    return !!(event && (event.isComposing || event.keyCode === 229 || event.which === 229));
  }

  // Shared lifecycle: previous dialog cancel, key handlers, focus restore/trap.
  // cancelValue: value used for Esc / backdrop / superseded dialog.
  // opts.skipDismiss: when caller already dismissed (confirm/prompt pre-dismiss).
  function mountDialogSession(overlay, opts) {
    opts = opts || {};
    var cancelValue = Object.prototype.hasOwnProperty.call(opts, 'cancelValue') ? opts.cancelValue : null;
    var onEnter = typeof opts.onEnter === 'function' ? opts.onEnter : null;
    var focusEl = opts.focusEl || null;
    var selectOnFocus = !!opts.selectOnFocus;
    var settled = false;

    // Supersede previous dialog first, THEN capture focus restore target.
    // Capturing before dismiss would point at controls inside the dying dialog.
    if (!opts.skipDismiss) dismissActiveDialog();
    var previousFocus = global.document.activeElement;

    var close = function(value) {
      if (settled) return;
      settled = true;
      if (activeDialogSession && activeDialogSession.overlay === overlay) {
        activeDialogSession = null;
      }
      global.document.removeEventListener('keydown', onKey, true);
      removeDialogOverlay(overlay);
      unlockBodyScroll();
      if (previousFocus && typeof previousFocus.focus === 'function' && isNodeConnected(previousFocus)) {
        try { previousFocus.focus(); } catch (_) {}
      }
      if (typeof opts.resolve === 'function') opts.resolve(value);
    };

    var onKey = function(event) {
      if (!overlay || !overlay.parentNode || settled) return;
      // Ignore events that another dialog already handled.
      if (event.defaultPrevented) return;
      if (isImeComposing(event)) return;
      if (event.key === 'Escape') {
        event.preventDefault();
        if (typeof event.stopImmediatePropagation === 'function') event.stopImmediatePropagation();
        else event.stopPropagation();
        close(cancelValue);
        return;
      }
      // Confirm Enter only when not typing in multiline fields.
      if (event.key === 'Enter' && onEnter) {
        var tag = event.target && event.target.tagName ? String(event.target.tagName).toLowerCase() : '';
        if (tag === 'textarea') return;
        // Let focused Cancel activate natively (do not force confirm).
        if (tag === 'button' && event.target.id && /Cancel/i.test(event.target.id)) return;
        event.preventDefault();
        if (typeof event.stopImmediatePropagation === 'function') event.stopImmediatePropagation();
        else event.stopPropagation();
        onEnter();
        return;
      }
      // Basic focus trap inside the dialog.
      if (event.key === 'Tab') {
        var list = focusableIn(overlay);
        if (!list.length) return;
        var first = list[0];
        var last = list[list.length - 1];
        var active = global.document.activeElement;
        if (event.shiftKey) {
          if (active === first || !overlay.contains(active)) {
            event.preventDefault();
            last.focus();
          }
        } else if (active === last || !overlay.contains(active)) {
          event.preventDefault();
          first.focus();
        }
      }
    };

    activeDialogSession = { overlay: overlay, close: close, cancelValue: cancelValue };
    global.document.body.appendChild(overlay);
    lockBodyScroll();
    bindModalOverlayDismiss(overlay, function() { close(cancelValue); });
    global.document.addEventListener('keydown', onKey, true);

    // Prevent wheel/touch scroll chaining to the page behind the dialog (desktop + iOS).
    var blockBackdropScroll = function(event) {
      if (event.target === overlay) event.preventDefault();
    };
    overlay.addEventListener('wheel', blockBackdropScroll, { passive: false });
    overlay.addEventListener('touchmove', blockBackdropScroll, { passive: false });

    // Clicks inside the panel must not be treated as backdrop dismiss.
    var panel = overlay.firstElementChild;
    if (panel && panel.addEventListener) {
      panel.addEventListener('click', function(event) {
        event.stopPropagation();
      });
    }

    if (focusEl) {
      try {
        focusEl.focus();
        if (selectOnFocus && typeof focusEl.select === 'function' && focusEl.value) focusEl.select();
      } catch (_) {}
    }

    return { close: close, settled: function() { return settled; } };
  }

  // Shared custom confirm (never uses window.confirm).
  // options: { title, confirmText, cancelText, danger:boolean }
  // resolves true/false
  function confirmDialog(message, options) {
    options = options || {};
    return new Promise(function(resolve) {
      if (!global.document || !global.document.body) {
        resolve(false);
        return;
      }
      // Close any prior dialog before creating DOM (avoids same-id orphans).
      dismissActiveDialog();
      var danger = options.danger === true || options.variant === 'danger';
      var overlay = createDialogOverlay('adminUiConfirmDialogOverlay');
      var titleText = dialogConfirmTitle(options);
      var confirmText = dialogOkLabel(options);
      var cancelText = dialogCancelLabel(options);
      var okClass = danger ? 'btn-danger' : 'btn-primary';
      var msgId = 'adminUiConfirmDialogMessage';
      overlay.innerHTML = '<div class="session-modal" role="dialog" aria-modal="true" aria-labelledby="adminUiConfirmDialogTitle" aria-describedby="' + msgId + '" style="' + dialogPanelStyle() + '">'
        + '<div class="item-title" id="adminUiConfirmDialogTitle" style="margin-bottom:8px'
        + (danger ? ';color:var(--danger,#e53935)' : '') + '">' + dialogEsc(titleText) + '</div>'
        + '<div class="item-meta" id="' + msgId + '" style="margin-bottom:16px;white-space:pre-wrap">' + dialogEsc(message || '') + '</div>'
        + '<div class="actions" style="display:flex;justify-content:flex-end;gap:8px;flex-wrap:wrap">'
        + '<button type="button" class="btn-ghost" id="adminUiConfirmCancelBtn">' + dialogEsc(cancelText) + '</button>'
        + '<button type="button" class="' + okClass + '" id="adminUiConfirmOkBtn">' + dialogEsc(confirmText) + '</button>'
        + '</div></div>';

      var cancel = overlay.querySelector('#adminUiConfirmCancelBtn');
      var ok = overlay.querySelector('#adminUiConfirmOkBtn');
      var session = mountDialogSession(overlay, {
        cancelValue: false,
        skipDismiss: true,
        // Match native confirm: primary action focused (Enter confirms).
        focusEl: ok,
        resolve: function(v) { resolve(!!v); },
        onEnter: function() { session.close(true); }
      });
      if (cancel) cancel.addEventListener('click', function() { session.close(false); });
      if (ok) ok.addEventListener('click', function() { session.close(true); });
    });
  }

  // Shared custom prompt (never uses window.prompt).
  // options: { title, defaultValue, placeholder, confirmText, cancelText, required, requiredText, inputType, multiline }
  // resolves trimmed string, or null when cancelled
  function promptDialog(message, options) {
    options = options || {};
    return new Promise(function(resolve) {
      if (!global.document || !global.document.body) {
        resolve(null);
        return;
      }
      dismissActiveDialog();
      var overlay = createDialogOverlay('adminUiPromptDialogOverlay');
      var titleText = dialogPromptTitle(options);
      var confirmText = dialogOkLabel(options);
      var cancelText = dialogCancelLabel(options);
      var required = !!options.required;
      var multiline = !!options.multiline;
      // Restrict type to safe allowlist (avoid attribute injection / unexpected controls).
      var allowedTypes = { text: 1, password: 1, email: 1, url: 1, search: 1, number: 1 };
      var inputType = allowedTypes[options.inputType] ? options.inputType : 'text';
      var defaultValue = options.defaultValue != null ? String(options.defaultValue) : '';
      var placeholder = options.placeholder != null ? String(options.placeholder) : '';
      var msgId = 'adminUiPromptDialogMessage';
      var inputHtml = multiline
        ? '<textarea id="adminUiPromptInput" rows="4" style="width:100%;box-sizing:border-box;resize:vertical;font-size:13px;padding:8px 10px;border-radius:8px;border:1px solid rgba(31,34,48,.12);margin-bottom:4px" placeholder="' + dialogEsc(placeholder) + '">' + dialogEsc(defaultValue) + '</textarea>'
        : '<input id="adminUiPromptInput" type="' + dialogEsc(inputType) + '" value="' + dialogEsc(defaultValue) + '" placeholder="' + dialogEsc(placeholder) + '" autocomplete="off" style="width:100%;height:36px;box-sizing:border-box;margin-bottom:4px">';

      overlay.innerHTML = '<div class="session-modal" role="dialog" aria-modal="true" aria-labelledby="adminUiPromptDialogTitle"'
        + (message ? ' aria-describedby="' + msgId + '"' : '')
        + ' style="' + dialogPanelStyle() + '">'
        + '<div class="item-title" id="adminUiPromptDialogTitle" style="margin-bottom:8px">' + dialogEsc(titleText) + '</div>'
        + (message ? '<div class="item-meta" id="' + msgId + '" style="margin-bottom:12px;white-space:pre-wrap">' + dialogEsc(message) + '</div>' : '')
        + inputHtml
        + '<div id="adminUiPromptError" role="alert" style="color:var(--danger,#e53935);font-size:12px;min-height:18px;margin-bottom:8px"></div>'
        + '<div class="actions" style="display:flex;justify-content:flex-end;gap:8px;flex-wrap:wrap">'
        + '<button type="button" class="btn-ghost" id="adminUiPromptCancelBtn">' + dialogEsc(cancelText) + '</button>'
        + '<button type="button" class="btn-primary" id="adminUiPromptOkBtn">' + dialogEsc(confirmText) + '</button>'
        + '</div></div>';

      var input = overlay.querySelector('#adminUiPromptInput');
      var errorEl = overlay.querySelector('#adminUiPromptError');
      var cancel = overlay.querySelector('#adminUiPromptCancelBtn');
      var ok = overlay.querySelector('#adminUiPromptOkBtn');
      var session = null;

      var submit = function() {
        if (!session) return;
        var val = input ? String(input.value || '') : '';
        if (required && !val.trim()) {
          if (errorEl) errorEl.textContent = dialogRequiredHint(options);
          if (input) {
            try { input.focus(); } catch (_) {}
          }
          return;
        }
        if (errorEl) errorEl.textContent = '';
        session.close(val.trim());
      };

      session = mountDialogSession(overlay, {
        cancelValue: null,
        skipDismiss: true,
        focusEl: input,
        selectOnFocus: !!defaultValue,
        resolve: resolve,
        onEnter: multiline ? null : submit
      });
      if (cancel) cancel.addEventListener('click', function() { session.close(null); });
      if (ok) ok.addEventListener('click', submit);
      if (input) {
        input.addEventListener('input', function() {
          if (errorEl && errorEl.textContent) errorEl.textContent = '';
        });
      }
    });
  }

  global.AdminUI = {
    hint: hint,
    meta: meta,
    badge: badge,
    actionButton: actionButton,
    simpleCard: simpleCard,
    renderList: renderList,
    enhanceButtonTypes: enhanceButtonTypes,
    enhanceLanguageSwitchStates: enhanceLanguageSwitchStates,
    enhanceAdminNavigation: enhanceAdminNavigation,
    bindAdminNavigationKeyboard: bindAdminNavigationKeyboard,
    installButtonTypeNormalizer: installButtonTypeNormalizer,
    rememberBackdropPointer: rememberBackdropPointer,
    dismissBackdropClick: dismissBackdropClick,
    bindModalOverlayDismiss: bindModalOverlayDismiss,
    installModalBackdropGuard: installModalBackdropGuard,
    confirmDialog: confirmDialog,
    promptDialog: promptDialog,
    dismissActiveDialog: dismissActiveDialog,
    isDialogOpen: isDialogOpen,
    dialogZIndex: DIALOG_Z_INDEX
  };
})(window);

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
    installModalBackdropGuard: installModalBackdropGuard
  };
})(window);

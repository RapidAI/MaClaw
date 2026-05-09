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
    if (onclick) nextAttrs.onclick = onclick;
    return '<button class="' + escapeHtml(className || 'btn-secondary') + '"' + attrsToString(nextAttrs) + '>' + escapeHtml(text || '') + '</button>';
  }
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
  global.AdminUI = {
    hint: hint,
    meta: meta,
    badge: badge,
    actionButton: actionButton,
    simpleCard: simpleCard,
    renderList: renderList,
    rememberBackdropPointer: rememberBackdropPointer,
    dismissBackdropClick: dismissBackdropClick,
    bindModalOverlayDismiss: bindModalOverlayDismiss,
    installModalBackdropGuard: installModalBackdropGuard
  };
})(window);

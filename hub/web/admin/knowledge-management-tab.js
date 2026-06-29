/*
 * Knowledge share admin module.
 * ASCII only.
 */
(function(global) {
  if (typeof I18N !== 'undefined') {
    I18N.en = Object.assign({}, I18N.en, {
      knowledgeTabTitle: 'Knowledge Management',
      knowledgeTabSubtitle: 'Review shared knowledge export records by user, time, views, and imports.',
      knowledgeNavDesc: 'Shared knowledge export records',
      knowledgeReload: 'Reload Knowledge',
      knowledgeTenantLabel: 'Tenant ID',
      knowledgeUserLabel: 'User',
      knowledgeSortLabel: 'Sort',
      knowledgeSortNewest: 'Newest first',
      knowledgeSortUpdated: 'Recently updated',
      knowledgeSortViews: 'Most viewed',
      knowledgeSortImports: 'Most imported',
      knowledgeSearch: 'Search',
      knowledgeKeywordLabel: 'Keyword',
      knowledgeKeywordPlaceholder: 'Search by title or description',
      knowledgePrivacyHint: 'This admin view shows only descriptions and metadata. It never displays exported knowledge body, cards, or facts.',
      knowledgeEmpty: 'No shared knowledge records match the current filters.',
      knowledgeLoadFailed: 'Load knowledge shares failed: {error}',
      knowledgePageSummary: 'Showing {start}-{end} of {total} knowledge shares.',
      knowledgePageSingle: 'Showing {total} knowledge shares.',
      knowledgePrev: 'Previous',
      knowledgeNext: 'Next',
      knowledgeID: 'Knowledge ID',
      knowledgeOwner: 'Owner',
      knowledgeTenant: 'Tenant',
      knowledgeVisibility: 'Visibility',
      knowledgeViews: 'Views',
      knowledgeImports: 'Imports',
      knowledgePublishedAt: 'Published',
      knowledgeUpdatedAt: 'Updated',
      knowledgeDescription: 'Description',
      knowledgeShareLink: 'Share link',
      knowledgeForceDelete: 'Force delete',
      knowledgeDeleteReasonPrompt: 'Enter a delete reason. The share will be removed from discovery and import links.',
      knowledgeDeleteReasonRequired: 'Delete reason is required.',
      knowledgeDeleteDone: 'Knowledge share deleted.',
      knowledgeDeleteFailed: 'Delete knowledge share failed: {error}',
      knowledgeNoDescription: 'No description provided.'
    });
    I18N.zh = Object.assign({}, I18N.zh, {
      knowledgeTabTitle: '\u77e5\u8bc6\u7ba1\u7406',
      knowledgeTabSubtitle: '\u6309\u7528\u6237\u3001\u65f6\u95f4\u3001\u8bbf\u95ee\u91cf\u548c\u5bfc\u5165\u91cf\u67e5\u770b\u5df2\u5206\u4eab\u7684\u77e5\u8bc6\u5bfc\u51fa\u8bb0\u5f55\u3002',
      knowledgeNavDesc: '\u77e5\u8bc6\u5206\u4eab\u5bfc\u51fa\u8bb0\u5f55',
      knowledgeReload: '\u5237\u65b0\u77e5\u8bc6',
      knowledgeTenantLabel: '\u79df\u6237 ID',
      knowledgeUserLabel: '\u7528\u6237',
      knowledgeSortLabel: '\u6392\u5e8f',
      knowledgeSortNewest: '\u6700\u65b0\u53d1\u5e03',
      knowledgeSortUpdated: '\u6700\u8fd1\u66f4\u65b0',
      knowledgeSortViews: '\u8bbf\u95ee\u91cf\u6700\u9ad8',
      knowledgeSortImports: '\u5bfc\u5165\u91cf\u6700\u9ad8',
      knowledgeSearch: '\u641c\u7d22',
      knowledgeKeywordLabel: '\u5173\u952e\u5b57',
      knowledgeKeywordPlaceholder: '\u6309\u6807\u9898\u6216\u63cf\u8ff0\u641c\u7d22',
      knowledgePrivacyHint: '\u6b64\u7ba1\u7406\u89c6\u56fe\u53ea\u663e\u793a\u63cf\u8ff0\u548c\u5143\u6570\u636e\uff0c\u4e0d\u663e\u793a\u5bfc\u51fa\u77e5\u8bc6\u7684\u6b63\u6587\u3001\u5361\u7247\u6216\u4e8b\u5b9e\u5185\u5bb9\u3002',
      knowledgeEmpty: '\u5f53\u524d\u8fc7\u6ee4\u6761\u4ef6\u4e0b\u6682\u65e0\u77e5\u8bc6\u5206\u4eab\u8bb0\u5f55\u3002',
      knowledgeLoadFailed: '\u52a0\u8f7d\u77e5\u8bc6\u5206\u4eab\u5931\u8d25\uff1a{error}',
      knowledgePageSummary: '\u663e\u793a\u7b2c {start}-{end} \u6761\uff0c\u5171 {total} \u6761\u77e5\u8bc6\u5206\u4eab\u3002',
      knowledgePageSingle: '\u5171 {total} \u6761\u77e5\u8bc6\u5206\u4eab\u3002',
      knowledgePrev: '\u4e0a\u4e00\u9875',
      knowledgeNext: '\u4e0b\u4e00\u9875',
      knowledgeID: '\u77e5\u8bc6 ID',
      knowledgeOwner: '\u6240\u6709\u8005',
      knowledgeTenant: '\u79df\u6237',
      knowledgeVisibility: '\u53ef\u89c1\u8303\u56f4',
      knowledgeViews: '\u8bbf\u95ee',
      knowledgeImports: '\u5bfc\u5165',
      knowledgePublishedAt: '\u53d1\u5e03\u65f6\u95f4',
      knowledgeUpdatedAt: '\u66f4\u65b0\u65f6\u95f4',
      knowledgeDescription: '\u77e5\u8bc6\u63cf\u8ff0',
      knowledgeShareLink: '\u5206\u4eab\u94fe\u63a5',
      knowledgeForceDelete: '\u5f3a\u5236\u5220\u9664',
      knowledgeDeleteReasonPrompt: '\u8bf7\u8f93\u5165\u5220\u9664\u539f\u56e0\u3002\u8be5\u5206\u4eab\u5c06\u4ece\u6d4f\u89c8\u548c\u5bfc\u5165\u94fe\u63a5\u4e2d\u79fb\u9664\u3002',
      knowledgeDeleteReasonRequired: '\u9700\u8981\u586b\u5199\u5220\u9664\u539f\u56e0\u3002',
      knowledgeDeleteDone: '\u77e5\u8bc6\u5206\u4eab\u5df2\u5220\u9664\u3002',
      knowledgeDeleteFailed: '\u5220\u9664\u77e5\u8bc6\u5206\u4eab\u5931\u8d25\uff1a{error}',
      knowledgeNoDescription: '\u672a\u586b\u5199\u63cf\u8ff0\u3002'
    });
  }

  const state = {
    items: [],
    total: 0,
    page: 1,
    pageSize: 20,
    tenantID: '',
    user: '',
    keyword: '',
    sort: 'published_at_desc'
  };

  function byID(id) { return global.document.getElementById(id); }
  function tenantScoped() {
    const profile = typeof global.adminProfile === 'function' ? global.adminProfile() : null;
    return !!(profile && String(profile.scope || '').toLowerCase() === 'tenant');
  }
  function pageCount() { return Math.max(1, Math.ceil((state.total || 0) / state.pageSize)); }
  function formatTime(value) {
    if (!value) return '-';
    try { return new Date(value).toLocaleString(); } catch (_) { return String(value); }
  }
  function queryString() {
    const params = new URLSearchParams();
    params.set('offset', String((state.page - 1) * state.pageSize));
    params.set('limit', String(state.pageSize));
    params.set('sort', state.sort || 'published_at_desc');
    if (state.user) params.set('user', state.user);
    if (state.keyword) params.set('keyword', state.keyword);
    if (!tenantScoped() && state.tenantID) params.set('tenant_id', state.tenantID);
    return params.toString();
  }
  function syncFiltersFromInputs() {
    const tenant = byID('knowledgeTenantFilter');
    const user = byID('knowledgeUserFilter');
    const sort = byID('knowledgeSortFilter');
    const keyword = byID('knowledgeKeywordFilter');
    state.tenantID = tenant ? tenant.value.trim() : '';
    state.user = user ? user.value.trim() : '';
    state.sort = sort ? sort.value : 'published_at_desc';
    state.keyword = keyword ? keyword.value.trim() : '';
  }
  function meta(label, value) {
    if (value === undefined || value === null || value === '') return '';
    return '<span><strong>' + escapeHtml(label) + ':</strong> ' + escapeHtml(String(value)) + '</span>';
  }
  function renderKnowledgeShares() {
    const root = byID('knowledgeSharesList');
    const pager = byID('knowledgeSharesPager');
    const pagerMeta = byID('knowledgeSharesPagerMeta');
    const prevButton = byID('knowledgeSharesPrevButton');
    const nextButton = byID('knowledgeSharesNextButton');
    if (!root || !pager || !pagerMeta || !prevButton || !nextButton) return;
    if (!state.items.length) {
      root.innerHTML = '<div class="hint">' + escapeHtml(tr('knowledgeEmpty')) + '</div>';
      pager.classList.add('hidden');
      return;
    }
    root.innerHTML = '<div class="table" style="gap:8px;grid-template-columns:repeat(2,minmax(0,1fr));align-items:stretch">' + state.items.map(function(item) {
      const desc = item.description || tr('knowledgeNoDescription');
      const metrics = [
        meta(tr('knowledgeTenant'), item.tenant_id),
        meta(tr('knowledgeOwner'), item.owner_user_email || item.owner_user_id),
        meta(tr('knowledgeVisibility'), item.visibility_scope),
        meta(tr('knowledgeViews'), item.view_count || 0),
        meta(tr('knowledgeImports'), item.import_count || 0)
      ].filter(Boolean).join('<span style="color:rgba(31,34,48,.16)">|</span>');
      const timeLine = [
        meta(tr('knowledgePublishedAt'), formatTime(item.published_at)),
        meta(tr('knowledgeUpdatedAt'), formatTime(item.updated_at))
      ].filter(Boolean).join('<span style="color:rgba(31,34,48,.16)">|</span>');
      return '<div class="item" style="gap:6px;padding:10px 12px;margin-top:0">'
        + '<div class="item-head"><div style="min-width:0"><div class="item-title" style="font-size:13px">' + escapeHtml(item.title || item.knowledge_id || '-') + '</div></div><div class="actions"><button class="btn-danger" type="button" style="height:28px;font-size:11px;padding:0 10px" onclick="forceDeleteKnowledgeShare(\'' + escapeHtml(String(item.knowledge_id || '').replace(/'/g, "\\'")) + '\')">' + escapeHtml(tr('knowledgeForceDelete')) + '</button></div></div>'
        + '<div class="desc" style="font-size:11px;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden"><strong>' + escapeHtml(tr('knowledgeDescription')) + ':</strong> ' + escapeHtml(desc) + '</div>'
        + '<div class="item-meta" style="display:flex;gap:4px;flex-wrap:wrap;font-size:11px">' + metrics + '</div>'
        + (timeLine ? '<div class="item-meta" style="display:flex;gap:4px;flex-wrap:wrap;font-size:11px">' + timeLine + '</div>' : '')
        + (item.share_url ? '<div class="mono" style="font-size:10px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">' + escapeHtml(tr('knowledgeShareLink')) + ': ' + escapeHtml(item.share_url) + '</div>' : '')
        + '</div>';
    }).join('') + '</div>';
    const totalPages = pageCount();
    const start = ((state.page - 1) * state.pageSize) + 1;
    const end = ((state.page - 1) * state.pageSize) + state.items.length;
    pagerMeta.textContent = state.total > state.pageSize
      ? tr('knowledgePageSummary', { start: String(start), end: String(end), total: String(state.total) })
      : tr('knowledgePageSingle', { total: String(state.total) });
    prevButton.textContent = tr('knowledgePrev');
    nextButton.textContent = tr('knowledgeNext');
    prevButton.disabled = state.page <= 1;
    nextButton.disabled = state.page >= totalPages;
    pager.classList.toggle('hidden', state.total <= state.pageSize);
  }
  function applyKnowledgeScopeUI() {
    const wrap = byID('knowledgeTenantFilterWrap');
    if (!wrap) return;
    const isTenantScope = tenantScoped();
    const input = byID('knowledgeTenantFilter');
    if (input) input.disabled = isTenantScope;
    wrap.style.display = isTenantScope ? 'none' : '';
  }
  function applyKnowledgeI18n() {
    const options = global.document.querySelectorAll('#knowledgeSortFilter option[data-i18n]');
    options.forEach(function(option) { option.textContent = tr(option.getAttribute('data-i18n')); });
    const keywordInput = byID('knowledgeKeywordFilter');
    if (keywordInput) keywordInput.placeholder = tr('knowledgeKeywordPlaceholder');
    applyKnowledgeScopeUI();
    renderKnowledgeShares();
  }

  global.loadKnowledgeShares = async function loadKnowledgeShares() {
    try {
      syncFiltersFromInputs();
      const data = await api('/api/admin/knowledge/shares?' + queryString());
      state.items = Array.isArray(data.items) ? data.items : [];
      state.total = Number(data.total || 0);
      renderKnowledgeShares();
    } catch (err) {
      const msg = tr('knowledgeLoadFailed', { error: err.message });
      setOutput(msg);
      showToast(msg, 'error');
      const root = byID('knowledgeSharesList');
      if (root) root.innerHTML = '<div class="hint" style="color:var(--danger)">' + escapeHtml(msg) + '</div>';
    }
  };
  global.searchKnowledgeShares = async function searchKnowledgeShares() {
    state.page = 1;
    await global.loadKnowledgeShares();
  };
  global.resetKnowledgeShareFilters = async function resetKnowledgeShareFilters() {
    ['knowledgeTenantFilter', 'knowledgeUserFilter', 'knowledgeKeywordFilter'].forEach(function(id) { const el = byID(id); if (el) el.value = ''; });
    const sort = byID('knowledgeSortFilter');
    if (sort) sort.value = 'published_at_desc';
    state.page = 1;
    await global.loadKnowledgeShares();
  };
  global.changeKnowledgeSharesPage = async function changeKnowledgeSharesPage(step) {
    state.page = Math.min(pageCount(), Math.max(1, state.page + step));
    await global.loadKnowledgeShares();
  };
  function showKnowledgeDeleteReasonDialog() {
    return new Promise(function(resolve) {
      var overlayId = 'knowledgeDeleteReasonDialogOverlay';
      var existing = global.document.getElementById(overlayId);
      if (existing && existing.parentNode) existing.parentNode.removeChild(existing);
      var overlay = global.document.createElement('div');
      overlay.id = overlayId;
      overlay.className = 'session-modal-overlay show';
      overlay.style.cssText = 'z-index:9999;background:rgba(15,23,42,.42);padding:18px';
      var titleText = tr('knowledgeForceDelete');
      var promptText = tr('knowledgeDeleteReasonPrompt');
      var emptyHint = tr('knowledgeDeleteReasonRequired');
      var cancelText = typeof global.tr === 'function' ? global.tr('closeDialog') : 'Cancel';
      var confirmText = tr('knowledgeForceDelete');
      overlay.innerHTML = '<div class="session-modal" role="dialog" aria-modal="true" aria-labelledby="knowledgeDeleteReasonDialogTitle" style="width:min(420px,100%);max-height:none;overflow:visible;border:1px solid var(--border,#d8dee9);border-radius:12px;padding:16px;box-shadow:0 18px 60px rgba(15,23,42,.22)">'
        + '<div class="item-title" id="knowledgeDeleteReasonDialogTitle" style="margin-bottom:8px;color:var(--danger,#e53935)">' + escapeHtml(titleText) + '</div>'
        + '<div class="item-meta" style="margin-bottom:12px">' + escapeHtml(promptText) + '</div>'
        + '<input id="knowledgeDeleteReasonInput" type="text" style="width:100%;height:36px;margin-bottom:4px">'
        + '<div id="knowledgeDeleteReasonError" style="color:var(--danger,#e53935);font-size:12px;min-height:18px;margin-bottom:8px"></div>'
        + '<div class="actions" style="justify-content:flex-end;gap:8px">'
        + '<button type="button" class="btn-ghost" id="knowledgeDeleteReasonCancelBtn">' + escapeHtml(cancelText) + '</button>'
        + '<button type="button" class="btn-danger" id="knowledgeDeleteReasonConfirmBtn">' + escapeHtml(confirmText) + '</button>'
        + '</div></div>';
      var done = function(value) { if (overlay && overlay.parentNode) overlay.parentNode.removeChild(overlay); resolve(value); };
      global.document.body.appendChild(overlay);
      if (global.AdminUI && typeof global.AdminUI.bindModalOverlayDismiss === 'function') {
        global.AdminUI.bindModalOverlayDismiss(overlay, function() { done(null); });
      } else {
        overlay.onclick = function(event) { if (event && event.target === overlay) done(null); };
      }
      var input = overlay.querySelector('#knowledgeDeleteReasonInput');
      var errorEl = overlay.querySelector('#knowledgeDeleteReasonError');
      var cancel = overlay.querySelector('#knowledgeDeleteReasonCancelBtn');
      var ok = overlay.querySelector('#knowledgeDeleteReasonConfirmBtn');
      if (cancel) cancel.addEventListener('click', function() { done(null); });
      if (ok) ok.addEventListener('click', function() {
        var val = (input ? input.value : '').trim();
        if (!val) { if (errorEl) errorEl.textContent = emptyHint; if (input) input.focus(); return; }
        done(val);
      });
      if (input) {
        input.addEventListener('keydown', function(event) {
          if (event.key === 'Enter') { event.preventDefault(); if (ok) ok.click(); }
          if (event.key === 'Escape') { event.preventDefault(); done(null); }
        });
        input.focus();
      }
    });
  }
  global.forceDeleteKnowledgeShare = async function forceDeleteKnowledgeShare(id) {
    const reason = await showKnowledgeDeleteReasonDialog();
    if (!reason) {
      return;
    }
    try {
      await api('/api/admin/knowledge/shares/' + encodeURIComponent(id), { method: 'DELETE', body: JSON.stringify({ reason: reason }) });
      showToast(tr('knowledgeDeleteDone'), 'success');
      setOutput(tr('knowledgeDeleteDone'));
      await global.loadKnowledgeShares();
    } catch (err) {
      const msg = tr('knowledgeDeleteFailed', { error: err.message });
      setOutput(msg);
      showToast(msg, 'error');
    }
  };

  if (typeof global.tabMeta === 'object') global.tabMeta.knowledge = ['knowledgeTabTitle', 'knowledgeTabSubtitle'];
  if (global.AdminTabRegistry && typeof global.AdminTabRegistry.registerTab === 'function') {
    global.AdminTabRegistry.registerTab({
      id: 'knowledge',
      title: function() { return tr('knowledgeTabTitle'); },
      subtitle: function() { return tr('knowledgeTabSubtitle'); },
      onOpen: function() { applyKnowledgeScopeUI(); global.loadKnowledgeShares(); }
    });
  }
  if (global.AdminTabRegistry && typeof global.AdminTabRegistry.onLanguageChange === 'function') {
    global.AdminTabRegistry.onLanguageChange(applyKnowledgeI18n);
  }
  global.addEventListener('keydown', function(event) {
    if (event.key !== 'Enter') return;
    if (event.target && (event.target.id === 'knowledgeTenantFilter' || event.target.id === 'knowledgeUserFilter' || event.target.id === 'knowledgeKeywordFilter')) global.searchKnowledgeShares();
  });
  applyKnowledgeI18n();
})(window);

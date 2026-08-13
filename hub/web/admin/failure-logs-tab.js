/*
 * Failure logs admin module.
 * ASCII only.
 */
(function(global) {
  if (typeof I18N !== 'undefined') {
    I18N.en = Object.assign({}, I18N.en, {
      navFailureLogs: 'Failure Logs',
      navFailureLogsDesc: 'Search failed registration and sync events',
      failureLogsTabTitle: 'Failure Logs',
      failureLogsTabSubtitle: 'Review failed registration, heartbeat, and related diagnostic events.',
      failureLogsSearchLabel: 'Keyword',
      failureLogsSearchPlaceholder: 'email, event code, ip, entity id',
      failureLogsCategoryLabel: 'Category',
      failureLogsCategoryAll: 'All Categories',
      failureLogsCategoryRegistration: 'Registration',
      failureLogsCategoryHeartbeat: 'Heartbeat',
      failureLogsCategorySync: 'Sync',
      failureLogsCategoryRouting: 'Routing',
      failureLogsCategoryUserReferral: 'User referrals',
      failureLogsCategoryOther: 'Other',
      failureLogsReload: 'Reload Logs',
      failureLogsSearchAction: 'Search',
      failureLogsEmpty: 'No failure logs match the current filters.',
      failureLogsLoadFailed: 'Load failure logs failed: {error}',
      failureLogsPageSummary: 'Showing {start}-{end} of {total} failure logs.',
      failureLogsPageSingle: 'Showing {total} failure logs.',
      failureLogsPrev: 'Previous',
      failureLogsNext: 'Next',
      failureLogsDetails: 'Details JSON',
      failureLogsMeta: 'Diagnostic Fields',
      failureLogsTime: 'Occurred At',
      failureLogsEvent: 'Event',
      failureLogsMessage: 'Message',
      failureLogsEmail: 'Email',
      failureLogsIP: 'Client IP',
      failureLogsEntity: 'Entity ID',
      failureLogsCategory: 'Category'
    });
    I18N.zh = Object.assign({}, I18N.zh, {
      navFailureLogs: '\u5931\u8d25\u65e5\u5fd7',
      navFailureLogsDesc: '\u641c\u7d22\u6ce8\u518c\u3001\u5fc3\u8df3\u548c\u540c\u6b65\u5931\u8d25\u4e8b\u4ef6',
      failureLogsTabTitle: '\u5931\u8d25\u65e5\u5fd7',
      failureLogsTabSubtitle: '\u67e5\u770b\u6ce8\u518c\u3001\u5fc3\u8df3\u548c\u76f8\u5173\u8bca\u65ad\u5931\u8d25\u4e8b\u4ef6\u3002',
      failureLogsSearchLabel: '\u5173\u952e\u5b57',
      failureLogsSearchPlaceholder: '\u90ae\u7bb1\u3001\u4e8b\u4ef6\u7801\u3001IP\u3001\u5b9e\u4f53 ID',
      failureLogsCategoryLabel: '\u5206\u7c7b',
      failureLogsCategoryAll: '\u5168\u90e8\u5206\u7c7b',
      failureLogsCategoryRegistration: '\u6ce8\u518c',
      failureLogsCategoryHeartbeat: '\u5fc3\u8df3',
      failureLogsCategorySync: '\u540c\u6b65',
      failureLogsCategoryRouting: '\u8def\u7531',
      failureLogsCategoryUserReferral: '\u7528\u6237\u9080\u8bf7',
      failureLogsCategoryOther: '\u5176\u5b83',
      failureLogsReload: '\u5237\u65b0\u65e5\u5fd7',
      failureLogsSearchAction: '\u641c\u7d22',
      failureLogsEmpty: '\u5f53\u524d\u8fc7\u6ee4\u6761\u4ef6\u4e0b\u6682\u65e0\u5931\u8d25\u65e5\u5fd7\u3002',
      failureLogsLoadFailed: '\u52a0\u8f7d\u5931\u8d25\u65e5\u5fd7\u5931\u8d25\uff1a{error}',
      failureLogsPageSummary: '\u663e\u793a\u7b2c {start}-{end} \u6761\uff0c\u5171 {total} \u6761\u5931\u8d25\u65e5\u5fd7\u3002',
      failureLogsPageSingle: '\u5171 {total} \u6761\u5931\u8d25\u65e5\u5fd7\u3002',
      failureLogsPrev: '\u4e0a\u4e00\u9875',
      failureLogsNext: '\u4e0b\u4e00\u9875',
      failureLogsDetails: '\u8be6\u7ec6 JSON',
      failureLogsMeta: '\u8bca\u65ad\u5b57\u6bb5',
      failureLogsTime: '\u53d1\u751f\u65f6\u95f4',
      failureLogsEvent: '\u4e8b\u4ef6',
      failureLogsMessage: '\u63cf\u8ff0',
      failureLogsEmail: '\u90ae\u7bb1',
      failureLogsIP: '\u5ba2\u6237\u7aef IP',
      failureLogsEntity: '\u5b9e\u4f53 ID',
      failureLogsCategory: '\u5206\u7c7b'
    });
  }

  const state = {
    items: [],
    total: 0,
    page: 1,
    pageSize: 20,
    keyword: '',
    category: ''
  };

  function failurePageCount() {
    return Math.max(1, Math.ceil((state.total || 0) / state.pageSize));
  }

  function failureQuery() {
    const params = new URLSearchParams();
    params.set('offset', String((state.page - 1) * state.pageSize));
    params.set('limit', String(state.pageSize));
    if (state.keyword) params.set('keyword', state.keyword);
    if (state.category) params.set('category', state.category);
    return params.toString();
  }

  function detailBlock(details) {
    const text = details && Object.keys(details).length ? JSON.stringify(details, null, 2) : '{}';
    return '<details><summary>' + escapeHtml(tr('failureLogsDetails')) + '</summary><pre class="console" style="min-height:auto;max-height:220px;margin-top:10px">' + escapeHtml(text) + '</pre></details>';
  }

  function metaLine(label, value) {
    if (!value) return '';
    return '<div class="item-meta"><strong>' + escapeHtml(label) + ':</strong> ' + escapeHtml(String(value)) + '</div>';
  }

  function renderFailureLogs() {
    const root = document.getElementById('failureLogsList');
    const pager = document.getElementById('failureLogsPager');
    const pagerMeta = document.getElementById('failureLogsPagerMeta');
    const prevButton = document.getElementById('failureLogsPrevButton');
    const nextButton = document.getElementById('failureLogsNextButton');
    if (!root || !pager || !pagerMeta || !prevButton || !nextButton) return;
    if (!state.items.length) {
      root.innerHTML = '<div class="hint">' + escapeHtml(tr('failureLogsEmpty')) + '</div>';
      pager.classList.add('hidden');
      return;
    }
    const header = '<div class="row header" style="grid-template-columns:.82fr .82fr 1.45fr .82fr .82fr .72fr;padding:8px 10px"><div>' + escapeHtml(tr('failureLogsTime')) + '</div><div>' + escapeHtml(tr('failureLogsCategory')) + '</div><div>' + escapeHtml(tr('failureLogsEvent')) + '</div><div>' + escapeHtml(tr('failureLogsEmail')) + '</div><div>' + escapeHtml(tr('failureLogsIP')) + '</div><div></div></div>';
    const rows = state.items.map(function(item) {
      const details = item && item.details && typeof item.details === 'object' ? item.details : {};
      const detailsText = details && Object.keys(details).length ? JSON.stringify(details, null, 2) : '{}';
      const metaBits = [
        item.entity_id ? ('<span><strong>' + escapeHtml(tr('failureLogsEntity')) + ':</strong> ' + escapeHtml(String(item.entity_id)) + '</span>') : '',
        item.message ? ('<span><strong>' + escapeHtml(tr('failureLogsMessage')) + ':</strong> ' + escapeHtml(String(item.message)) + '</span>') : ''
      ].filter(Boolean).join('<span style="color:rgba(31,34,48,.16)">|</span>');
      return '<details class="item" style="padding:0;overflow:hidden;border-radius:12px">'
        + '<summary class="row" style="grid-template-columns:.82fr .82fr 1.45fr .82fr .82fr .72fr;list-style:none;cursor:pointer;border:none;border-radius:0;background:#fff;padding:8px 10px">'
        + '<div class="item-meta">' + escapeHtml(item.created_at || '-') + '</div>'
        + '<div><span class="badge warn">' + escapeHtml(item.category || 'other') + '</span></div>'
        + '<div style="min-width:0"><div class="mono" style="font-size:11px;font-weight:700;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(item.event_code || '-') + '</div></div>'
        + '<div class="item-meta mono" style="white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(item.email || '-') + '</div>'
        + '<div class="item-meta mono" style="white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(item.client_ip || '-') + '</div>'
        + '<div style="display:flex;justify-content:flex-end"><button class="btn-ghost" type="button" style="height:28px;font-size:11px;padding:0 10px" onclick="event.preventDefault(); const d=this.closest(\'details\'); d.open=!d.open;">' + escapeHtml(tr('failureLogsDetails')) + '</button></div>'
        + '</summary>'
        + '<div style="padding:8px 10px 10px;border-top:1px solid rgba(31,34,48,.06);background:#fffdfd">'
        + '<div class="item-meta" style="display:flex;gap:6px;flex-wrap:wrap;font-size:11px">' + metaBits + '</div>'
        + '<pre class="console" style="min-height:auto;max-height:200px;margin-top:6px;border-radius:10px;padding:12px 14px">' + escapeHtml(detailsText) + '</pre>'
        + '</div>'
        + '</details>';
    }).join('');
    root.innerHTML = '<div class="table" style="gap:4px">' + header + rows + '</div>';
    const totalPages = failurePageCount();
    const start = ((state.page - 1) * state.pageSize) + 1;
    const end = ((state.page - 1) * state.pageSize) + state.items.length;
    pagerMeta.textContent = state.total > state.pageSize
      ? tr('failureLogsPageSummary', { start: String(start), end: String(end), total: String(state.total) })
      : tr('failureLogsPageSingle', { total: String(state.total) });
    prevButton.textContent = tr('failureLogsPrev');
    nextButton.textContent = tr('failureLogsNext');
    prevButton.disabled = state.page <= 1;
    nextButton.disabled = state.page >= totalPages;
    pager.classList.toggle('hidden', state.total <= state.pageSize);
  }

  global.loadFailureLogs = async function loadFailureLogs() {
    try {
      const keywordInput = document.getElementById('failureLogsKeyword');
      const categoryInput = document.getElementById('failureLogsCategory');
      if (keywordInput) state.keyword = keywordInput.value.trim();
      if (categoryInput) state.category = categoryInput.value.trim();
      const data = await api('/api/admin/failure-logs?' + failureQuery());
      state.items = Array.isArray(data.logs) ? data.logs : [];
      state.total = Number(data.total || 0);
      renderFailureLogs();
    } catch (err) {
      const msg = tr('failureLogsLoadFailed', { error: err.message });
      setOutput(msg);
      showToast(msg, 'error');
      const root = document.getElementById('failureLogsList');
      if (root) root.innerHTML = '<div class="hint" style="color:var(--danger)">' + escapeHtml(msg) + '</div>';
    }
  };

  global.searchFailureLogs = async function searchFailureLogs() {
    state.page = 1;
    await global.loadFailureLogs();
  };

  global.resetFailureLogsFilters = async function resetFailureLogsFilters() {
    const keywordInput = document.getElementById('failureLogsKeyword');
    const categoryInput = document.getElementById('failureLogsCategory');
    if (keywordInput) keywordInput.value = '';
    if (categoryInput) categoryInput.value = '';
    state.keyword = '';
    state.category = '';
    state.page = 1;
    await global.loadFailureLogs();
  };

  global.changeFailureLogsPage = async function changeFailureLogsPage(step) {
    state.page = Math.min(failurePageCount(), Math.max(1, state.page + step));
    await global.loadFailureLogs();
  };

  function applyFailureLogsI18n() {
    const keyword = document.getElementById('failureLogsKeyword');
    if (keyword) keyword.placeholder = tr('failureLogsSearchPlaceholder');
    const category = document.getElementById('failureLogsCategory');
    if (category) {
      const options = category.querySelectorAll('option[data-i18n]');
      options.forEach(function(option) { option.textContent = tr(option.getAttribute('data-i18n')); });
    }
    renderFailureLogs();
  }

  if (global.AdminTabRegistry && typeof global.AdminTabRegistry.registerTab === 'function') {
    global.AdminTabRegistry.registerTab({
      id: 'failurelogs',
      title: function() { return tr('failureLogsTabTitle'); },
      subtitle: function() { return tr('failureLogsTabSubtitle'); },
      onOpen: function() { global.loadFailureLogs(); }
    });
  }

  if (global.AdminTabRegistry && typeof global.AdminTabRegistry.onLanguageChange === 'function') {
    global.AdminTabRegistry.onLanguageChange(function() { applyFailureLogsI18n(); });
  }

  global.addEventListener('keydown', function(event) {
    if (event.key !== 'Enter') return;
    if (event.target && event.target.id === 'failureLogsKeyword') global.searchFailureLogs();
  });

  applyFailureLogsI18n();
})(window);

// AI Expert Market is an operator-only surface. Consumer browse, purchase and
// installation remain in the MaClaw GUI dialog; this script exposes only the
// moderation lifecycle and safe private-expert administration.
let expertMarketAdminPage = 1;
let expertMarketAdminInFlight = null;
let expertMarketAdminSequence = 0;
let expertMarketAdminRequestKey = '';
let expertMarketAdminStatusTimer = null;
let expertMarketAdminLoadController = null;
let expertMarketOwnerDialogState = null;
let expertMarketOwnerLoadController = null;
let expertMarketOwnerLoadSequence = 0;
let expertMarketOwnerDialogOpener = null;

const EXPERT_MARKET_TEXT = {
  en: { nav: 'AI Expert Market', navDesc: 'Expert listing management', title: 'AI Expert Market', desc: 'Review public submissions and operate private experts safely.', filter: 'Status', all: 'All statuses', private: 'Private', pending: 'Pending review', listed: 'Listed', unlisted: 'Unlisted', rejected: 'Rejected', deleted: 'Deleted', purged: 'Purged', refresh: 'Refresh', search: 'Search', searchPlaceholder: 'Search expert, ID, publisher email', loading: 'Loading...', empty: 'No expert listings match this filter.', author: 'Publisher', version: 'Version', price: 'Credits', downloads: 'Downloads', approve: 'Approve & publish', reject: 'Reject', unlist: 'Unlist', remove: 'Delete', purge: 'Permanently delete', transfer: 'Change owner', publish: 'Make public', reason: 'Moderation note (optional)', reasonHint: 'Retained with the review record', operationReason: 'Operation reason', operationReasonLabel: 'Operation reason (required)', operationReasonHint: 'Required for this lifecycle action', operationReasonRequired: 'Enter an operation reason before continuing.', publishConfirm: 'Submit this private expert for public review? It will not be listed until approved.', privateDeleteConfirm: 'Delete this private expert? The publisher will no longer be able to access it.', purgeConfirm: 'Permanently delete this package? This is allowed only when no active buyer entitlement exists.', deleteConfirm: 'Delete this unlisted package? Buyers who already own it keep their download access.', actionFailed: 'Action failed: {error}', approvedOk: 'Expert listing approved and published.', rejectedOk: 'Expert listing rejected.', unlistedOk: 'Expert listing unlisted.', deletedOk: 'Expert listing deleted.', purgedOk: 'Expert package permanently deleted.', privateDeletedOk: 'Private expert deleted.', publishSubmittedOk: 'Private expert submitted for public review.', transferredOk: 'Expert owner changed.', ownerDialogTitle: 'Change owner', ownerDialogDesc: 'Select a verified user to own “{name}”.', ownerSearchLabel: 'Find user', ownerSearch: 'User ID or email', ownerSearchAction: 'Search users', close: 'Close', cancel: 'Cancel', ownerSearchRequired: 'Enter at least 2 characters to search users.', ownerEmpty: 'No verified users found.', ownerCurrent: 'Current owner', ownerSelect: 'Select', ownerConfirm: 'Change owner', ownerConfirmText: 'Transfer “{name}” from {from} to {to}? The previous owner will lose management access.', page: '{start}-{end} / {total}' },
  zh: { nav: 'AI 专家市场', navDesc: '专家审核与上架管理', title: 'AI 专家市场', desc: '审核公开提交，并安全运营私有专家。', filter: '状态', all: '全部状态', private: '私有', pending: '待审核', listed: '已上架', unlisted: '已下架', rejected: '已驳回', deleted: '已删除', purged: '已彻底清理', refresh: '刷新', search: '搜索', searchPlaceholder: '搜索专家、ID、发布者邮箱', loading: '加载中...', empty: '没有符合条件的专家条目。', author: '发布者', version: '版本', price: 'Credits', downloads: '下载', approve: '通过并上架', reject: '驳回', unlist: '下架', remove: '删除', purge: '彻底删除', transfer: '修改属主', publish: '公开', reason: '审核说明（选填）', reasonHint: '用于留存审核记录', operationReason: '操作原因', operationReasonLabel: '操作原因（必填）', operationReasonHint: '执行此生命周期操作前必填', operationReasonRequired: '请先填写操作原因。', publishConfirm: '确认将此私有专家提交公开审核？审核通过后才会上架。', privateDeleteConfirm: '确认删除此私有专家？原属主将无法继续访问它。', purgeConfirm: '确认彻底删除该专家包？仅当没有有效买家权益时允许。', deleteConfirm: '确认删除这个已下架专家包？已购买用户仍可下载。', actionFailed: '操作失败：{error}', approvedOk: '专家条目已通过审核并上架。', rejectedOk: '专家条目已驳回。', unlistedOk: '专家条目已下架。', deletedOk: '专家条目已删除。', purgedOk: '专家包已彻底删除。', privateDeletedOk: '私有专家已删除。', publishSubmittedOk: '私有专家已提交公开审核。', transferredOk: '专家属主已修改。', ownerDialogTitle: '修改属主', ownerDialogDesc: '为“{name}”选择一位已验证的新属主。', ownerSearchLabel: '查找用户', ownerSearch: '用户 ID 或邮箱', ownerSearchAction: '搜索用户', close: '关闭', cancel: '取消', ownerSearchRequired: '请输入至少 2 个字符后搜索用户。', ownerEmpty: '没有找到已验证用户。', ownerCurrent: '当前属主', ownerSelect: '选择', ownerConfirm: '修改属主', ownerConfirmText: '确认将“{name}”从 {from} 转移给 {to}？原属主将失去管理权限。', page: '{start}-{end} / {total}' }
};

function expertMarketText(key, vars = {}) { const lang = window.currentLang === 'zh' ? 'zh' : 'en'; return (EXPERT_MARKET_TEXT[lang][key] || EXPERT_MARKET_TEXT.en[key] || key).replace(/\{(\w+)\}/g, (_, n) => vars[n] ?? ''); }
function expertMarketEsc(v) { return (window.escapeHtml ? window.escapeHtml(String(v ?? '')) : String(v ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c])); }
function expertMarketStatus(v) { return expertMarketText(String(v || '').toLowerCase()); }
function expertMarketAPI(path, options) { if (typeof window.api !== 'function') throw new Error('Admin API is not ready.'); return window.api(path, options || {}); }
function expertMarketError(err) { return err?.message || String(err || expertMarketText('actionFailed')); }
function expertMarketIsAbort(err) { return err?.name === 'AbortError'; }
function expertMarketIsPrivate(item) { return item && item.visibility === 'private' && item.status === 'private'; }

function ensureExpertMarketAdminNav() {
  if (document.querySelector('.nav button[data-tab="expertmarket"]')) return;
  const anchor = document.querySelector('.nav button[data-tab="petstore"]') || document.querySelector('.nav button[data-tab="skillmarket"]');
  if (!anchor || !anchor.parentElement) return;
  const button = document.createElement('button'); button.type = 'button'; button.dataset.tab = 'expertmarket';
  button.innerHTML = '<span class="nav-icon" aria-hidden="true">AI</span><span></span><small></small>';
  button.addEventListener('click', () => window.openTab('expertmarket'));
  anchor.parentElement.insertBefore(button, anchor.nextSibling);
  applyExpertMarketAdminI18n();
}

function applyExpertMarketAdminI18n() {
  const nav = document.querySelector('.nav button[data-tab="expertmarket"]');
  if (nav) { const title = nav.querySelector('span:not(.nav-icon)'), desc = nav.querySelector('small'); if (title) title.textContent = expertMarketText('nav'); if (desc) desc.textContent = expertMarketText('navDesc'); }
  [['expertMarketAdminTitle', 'title'], ['expertMarketAdminDesc', 'desc'], ['expertMarketAdminFilterLabel', 'filter'], ['expertMarketAdminRefresh', 'refresh'], ['expertMarketAdminSearch', 'search']].forEach(([id, key]) => { const el = document.getElementById(id); if (el) el.textContent = expertMarketText(key); });
  const search = document.getElementById('expertMarketAdminKeyword'); if (search) search.placeholder = expertMarketText('searchPlaceholder');
  const filter = document.getElementById('expertMarketAdminStatus'); if (filter) Array.from(filter.options).forEach(option => option.textContent = expertMarketText(option.value || 'all'));
  [['expertMarketOwnerSearchLabel', 'ownerSearchLabel'], ['expertMarketOwnerSearchButton', 'ownerSearchAction'], ['expertMarketOwnerReasonLabel', 'operationReasonLabel'], ['expertMarketOwnerCancel', 'cancel'], ['expertMarketOwnerConfirm', 'ownerConfirm']].forEach(([id, key]) => { const el = document.getElementById(id); if (el) el.textContent = expertMarketText(key); });
  const close = document.getElementById('expertMarketOwnerDialogClose'); if (close) close.setAttribute('aria-label', expertMarketText('close'));
  const ownerSearch = document.getElementById('expertMarketOwnerKeyword'); if (ownerSearch) ownerSearch.placeholder = expertMarketText('ownerSearch');
  const ownerReason = document.getElementById('expertMarketOwnerReason'); if (ownerReason) ownerReason.placeholder = expertMarketText('operationReasonHint');
}

function expertMarketActionButton(item) {
  const status = String(item.status || '').toLowerCase();
  if (expertMarketIsPrivate(item)) return `<button class="mp-card-btn mp-card-btn-secondary" type="button" data-expert-action="transfer">${expertMarketEsc(expertMarketText('transfer'))}</button><button class="mp-card-btn mp-card-btn-primary" type="button" data-expert-action="publish">${expertMarketEsc(expertMarketText('publish'))}</button><button class="mp-card-btn mp-card-btn-danger" type="button" data-expert-action="private-delete">${expertMarketEsc(expertMarketText('remove'))}</button>`;
  if (status === 'pending_review') return `<button class="mp-card-btn mp-card-btn-primary" type="button" data-expert-action="approve">${expertMarketEsc(expertMarketText('approve'))}</button><button class="mp-card-btn mp-card-btn-secondary" type="button" data-expert-action="reject">${expertMarketEsc(expertMarketText('reject'))}</button>`;
  if (status === 'listed') return `<button class="mp-card-btn mp-card-btn-secondary" type="button" data-expert-action="unlist">${expertMarketEsc(expertMarketText('unlist'))}</button>`;
  if (status === 'unlisted') return `<button class="mp-card-btn mp-card-btn-danger" type="button" data-expert-action="delete">${expertMarketEsc(expertMarketText('remove'))}</button>`;
  if (status === 'deleted' && item.visibility === 'public') return `<button class="mp-card-btn mp-card-btn-danger" type="button" data-expert-action="purge">${expertMarketEsc(expertMarketText('purge'))}</button>`;
  if (status === 'deleted' && item.visibility === 'private') return `<button class="mp-card-btn mp-card-btn-danger" type="button" data-expert-action="private-purge">${expertMarketEsc(expertMarketText('purge'))}</button>`;
  return '';
}

function expertMarketRenderCard(item) {
  const id = String(item.id || ''), status = String(item.status || '').toLowerCase();
  const reviewNote = status === 'pending_review' ? `<label class="expert-market-note"><span>${expertMarketEsc(expertMarketText('reason'))}</span><input type="text" maxlength="2048" data-expert-reason placeholder="${expertMarketEsc(expertMarketText('reasonHint'))}"></label>` : '';
  const actions = expertMarketActionButton(item);
  const footer = reviewNote || actions ? `<div class="expert-market-footer">${reviewNote}<div class="expert-market-actions">${actions}</div></div>` : '';
  return `<article class="expert-market-card" data-expert-id="${expertMarketEsc(id)}" data-expert-name="${expertMarketEsc(item.name || id)}" data-expert-owner-id="${expertMarketEsc(item.owner_id || '')}" data-expert-owner-email="${expertMarketEsc(item.owner_email || '')}">
    <div class="expert-market-card-head"><div class="expert-market-avatar" aria-hidden="true">${expertMarketEsc(item.icon || 'AI')}</div><div class="expert-market-identity"><strong title="${expertMarketEsc(item.name || id)}">${expertMarketEsc(item.name || id)}</strong><span title="${expertMarketEsc(id)}">${expertMarketEsc(id)}</span></div><span class="expert-market-status expert-market-status-${expertMarketEsc(status)}">${expertMarketEsc(expertMarketStatus(status))}</span></div>
    <p class="expert-market-description" title="${expertMarketEsc(item.description || '')}">${expertMarketEsc(item.description || '—')}</p>
    <dl class="expert-market-details"><div><dt>${expertMarketEsc(expertMarketText('author'))}</dt><dd title="${expertMarketEsc(item.owner_email || '')}">${expertMarketEsc(item.owner_email || '—')}</dd></div><div><dt>${expertMarketEsc(expertMarketText('version'))}</dt><dd>${expertMarketEsc(item.version || '—')}</dd></div><div><dt>${expertMarketEsc(expertMarketText('price'))}</dt><dd>${expertMarketEsc(Number(item.price || 0).toLocaleString())}</dd></div><div><dt>${expertMarketEsc(expertMarketText('downloads'))}</dt><dd>${expertMarketEsc(Number(item.download_count || 0).toLocaleString())}</dd></div></dl>
    ${footer}
  </article>`;
}

function expertMarketEnsureActionReason(card) {
  const existing = card.querySelector('[data-expert-reason]'); if (existing) return existing;
  let footer = card.querySelector('.expert-market-footer');
  if (!footer) { footer = document.createElement('div'); footer.className = 'expert-market-footer'; footer.innerHTML = '<div class="expert-market-actions"></div>'; card.append(footer); }
  const label = document.createElement('label'); label.className = 'expert-market-note expert-market-operation-reason';
  label.innerHTML = `<span>${expertMarketEsc(expertMarketText('operationReason'))}</span><input type="text" maxlength="2048" data-expert-reason placeholder="${expertMarketEsc(expertMarketText('operationReasonHint'))}">`;
  footer.insertBefore(label, footer.firstChild); return label.querySelector('[data-expert-reason]');
}

function expertMarketSetStatus(kind, message, dismissAfter = 0) {
  const status = document.getElementById('expertMarketAdminStatusMessage'); if (!status) return;
  if (expertMarketAdminStatusTimer) { window.clearTimeout(expertMarketAdminStatusTimer); expertMarketAdminStatusTimer = null; }
  status.className = 'sm-status' + (kind ? ' show ' + kind : ''); status.textContent = message || '';
  if (dismissAfter > 0) expertMarketAdminStatusTimer = window.setTimeout(() => expertMarketSetStatus(), dismissAfter);
}

async function loadExpertMarketAdmin(page, force = false) {
  if (Number.isFinite(page)) expertMarketAdminPage = Math.max(1, page);
  const grid = document.getElementById('expertMarketAdminGrid'), filter = document.getElementById('expertMarketAdminStatus'), search = document.getElementById('expertMarketAdminKeyword'), refresh = document.getElementById('expertMarketAdminRefresh'); if (!grid) return;
  const query = new URLSearchParams({ page: String(expertMarketAdminPage) }); if (filter?.value) query.set('status', filter.value); if (search?.value.trim()) query.set('keyword', search.value.trim());
  const requestKey = query.toString(); if (!force && expertMarketAdminInFlight && expertMarketAdminRequestKey === requestKey) return expertMarketAdminInFlight;
  if (expertMarketAdminLoadController) expertMarketAdminLoadController.abort(); const controller = new AbortController(), sequence = ++expertMarketAdminSequence; expertMarketAdminLoadController = controller; expertMarketAdminRequestKey = requestKey;
  grid.setAttribute('aria-busy', 'true'); grid.innerHTML = `<div class="hint">${expertMarketEsc(expertMarketText('loading'))}</div>`; expertMarketSetStatus('loading', expertMarketText('loading')); if (refresh) { refresh.disabled = true; refresh.textContent = expertMarketText('loading'); }
  expertMarketAdminInFlight = (async () => { try {
    const result = await expertMarketAPI(`/api/v1/admin/expert-market/experts?${query}`, { signal: controller.signal }); if (sequence !== expertMarketAdminSequence) return;
    const experts = Array.isArray(result?.experts) ? result.experts : [], total = Number(result?.total || 0), pages = Math.max(1, Number(result?.total_pages || 1));
    if (!experts.length && total > 0 && expertMarketAdminPage > pages) { expertMarketAdminPage = pages; expertMarketAdminInFlight = null; expertMarketAdminRequestKey = ''; return loadExpertMarketAdmin(pages); }
    grid.innerHTML = experts.length ? experts.map(expertMarketRenderCard).join('') : `<div class="hint">${expertMarketEsc(expertMarketText('empty'))}</div>`; grid.setAttribute('aria-busy', 'false'); expertMarketSetStatus();
    const info = document.getElementById('expertMarketAdminPageInfo'), prev = document.getElementById('expertMarketAdminPrev'), next = document.getElementById('expertMarketAdminNext'); if (info) info.textContent = expertMarketText('page', { start: total ? ((expertMarketAdminPage - 1) * 20 + 1) : 0, end: Math.min(expertMarketAdminPage * 20, total), total }); if (prev) prev.disabled = expertMarketAdminPage <= 1; if (next) next.disabled = expertMarketAdminPage >= pages;
  } catch (err) { if (expertMarketIsAbort(err) || sequence !== expertMarketAdminSequence) return; const message = expertMarketText('actionFailed', { error: expertMarketError(err) }); grid.innerHTML = `<div class="hint">${expertMarketEsc(message)}</div>`; grid.setAttribute('aria-busy', 'false'); expertMarketSetStatus('error', message); } finally { if (expertMarketAdminLoadController === controller) expertMarketAdminLoadController = null; if (sequence === expertMarketAdminSequence && refresh) { refresh.disabled = false; refresh.textContent = expertMarketText('refresh'); } } })();
  try { return await expertMarketAdminInFlight; } finally { if (sequence === expertMarketAdminSequence) { expertMarketAdminInFlight = null; expertMarketAdminRequestKey = ''; } }
}

function openExpertMarketOwnerDialog(card) {
  const dialog = document.getElementById('expertMarketOwnerDialog'); if (!dialog) return;
  expertMarketOwnerDialogOpener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
  expertMarketOwnerDialogState = { id: card.dataset.expertId, name: card.dataset.expertName, ownerID: card.dataset.expertOwnerId, ownerEmail: card.dataset.expertOwnerEmail, selected: null, page: 1 };
  dialog.hidden = false; dialog.setAttribute('aria-hidden', 'false'); document.getElementById('expertMarketOwnerDialogTitle').textContent = expertMarketText('ownerDialogTitle'); document.getElementById('expertMarketOwnerDialogDesc').textContent = expertMarketText('ownerDialogDesc', { name: expertMarketOwnerDialogState.name }); const input = document.getElementById('expertMarketOwnerKeyword'), reason = document.getElementById('expertMarketOwnerReason'); input.value = ''; if (reason) reason.value = ''; applyExpertMarketAdminI18n(); document.getElementById('expertMarketOwnerConfirm').disabled = true; renderExpertMarketOwnerSearchRequired(); window.setTimeout(() => input.focus(), 0);
}
function closeExpertMarketOwnerDialog() { if (expertMarketOwnerLoadController) expertMarketOwnerLoadController.abort(); expertMarketOwnerLoadController = null; ++expertMarketOwnerLoadSequence; const dialog = document.getElementById('expertMarketOwnerDialog'); if (dialog) { dialog.hidden = true; dialog.setAttribute('aria-hidden', 'true'); } expertMarketOwnerDialogState = null; const opener = expertMarketOwnerDialogOpener; expertMarketOwnerDialogOpener = null; if (opener?.isConnected) window.setTimeout(() => opener.focus(), 0); }
function renderExpertMarketOwnerSearchRequired() {
  const results = document.getElementById('expertMarketOwnerResults'), info = document.getElementById('expertMarketOwnerPageInfo'), prev = document.getElementById('expertMarketOwnerPrev'), next = document.getElementById('expertMarketOwnerNext');
  if (results) results.innerHTML = `<div class="hint">${expertMarketEsc(expertMarketText('ownerSearchRequired'))}</div>`;
  if (info) info.textContent = '';
  if (prev) prev.disabled = true;
  if (next) next.disabled = true;
}
async function loadExpertMarketOwnerUsers(page) {
  if (!expertMarketOwnerDialogState) return;
  const state = expertMarketOwnerDialogState, keyword = document.getElementById('expertMarketOwnerKeyword')?.value.trim() || '';
  if (keyword !== state.keyword) { state.keyword = keyword; state.selected = null; state.page = 1; document.getElementById('expertMarketOwnerConfirm').disabled = true; } else state.page = Math.max(1, Number(page) || 1);
  if (expertMarketOwnerLoadController) expertMarketOwnerLoadController.abort(); const controller = new AbortController(), sequence = ++expertMarketOwnerLoadSequence;
  expertMarketOwnerLoadController = controller;
  if ([...keyword].length < 2) { renderExpertMarketOwnerSearchRequired(); if (expertMarketOwnerLoadController === controller) expertMarketOwnerLoadController = null; return; }
  const results = document.getElementById('expertMarketOwnerResults');
  results.innerHTML = `<div class="hint">${expertMarketEsc(expertMarketText('loading'))}</div>`;
  try {
    const data = await expertMarketAPI(`/api/v1/admin/expert-market/users?${new URLSearchParams({ page: String(state.page), keyword })}`, { signal: controller.signal });
    if (expertMarketOwnerDialogState !== state || sequence !== expertMarketOwnerLoadSequence) return;
    const users = (Array.isArray(data?.users) ? data.users : []).filter(user => user.id !== state.ownerID);
    results.innerHTML = users.length ? users.map(user => `<button type="button" class="expert-market-owner-row${state.selected?.id === user.id ? ' selected' : ''}" data-expert-owner-id="${expertMarketEsc(user.id)}" data-expert-owner-email="${expertMarketEsc(user.email)}"><span><strong>${expertMarketEsc(user.email)}</strong><small>${expertMarketEsc(user.id)}</small></span><em>${expertMarketEsc(expertMarketText('ownerSelect'))}</em></button>`).join('') : `<div class="hint">${expertMarketEsc(expertMarketText('ownerEmpty'))}</div>`;
    const total = Number(data?.total || 0), pages = Math.max(1, Number(data?.total_pages || 1)), info = document.getElementById('expertMarketOwnerPageInfo'), prev = document.getElementById('expertMarketOwnerPrev'), next = document.getElementById('expertMarketOwnerNext');
    if (info) info.textContent = expertMarketText('page', { start: total ? ((state.page - 1) * 20 + 1) : 0, end: Math.min(state.page * 20, total), total });
    if (prev) prev.disabled = state.page <= 1; if (next) next.disabled = state.page >= pages;
  } catch (err) { if (expertMarketIsAbort(err) || expertMarketOwnerDialogState !== state || sequence !== expertMarketOwnerLoadSequence) return; results.innerHTML = `<div class="hint">${expertMarketEsc(expertMarketText('actionFailed', { error: expertMarketError(err) }))}</div>`; } finally { if (expertMarketOwnerLoadController === controller) expertMarketOwnerLoadController = null; }
}
function changeExpertMarketOwnerPage(delta) { if (expertMarketOwnerDialogState) void loadExpertMarketOwnerUsers(expertMarketOwnerDialogState.page + delta); }
function selectExpertMarketOwner(button) { if (!expertMarketOwnerDialogState) return; expertMarketOwnerDialogState.selected = { id: button.dataset.expertOwnerId, email: button.dataset.expertOwnerEmail }; document.querySelectorAll('.expert-market-owner-row').forEach(row => row.classList.toggle('selected', row === button)); document.getElementById('expertMarketOwnerConfirm').disabled = false; }
async function confirmExpertMarketOwnerTransfer() {
  const state = expertMarketOwnerDialogState, reasonInput = document.getElementById('expertMarketOwnerReason'), reason = reasonInput?.value.trim() || ''; if (!state?.selected) return; if (!reason) { reasonInput?.setCustomValidity(expertMarketText('operationReasonRequired')); reasonInput?.reportValidity(); reasonInput?.focus(); return; } reasonInput?.setCustomValidity(''); if (!window.confirm(expertMarketText('ownerConfirmText', { name: state.name, from: state.ownerEmail || state.ownerID, to: state.selected.email || state.selected.id }))) return;
  const confirm = document.getElementById('expertMarketOwnerConfirm'); confirm.disabled = true; try { await expertMarketAPI(`/api/v1/admin/expert-market/experts/${encodeURIComponent(state.id)}/transfer-owner`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ target_user_id: state.selected.id, expected_owner_id: state.ownerID, reason }) }); closeExpertMarketOwnerDialog(); await loadExpertMarketAdmin(undefined, true); expertMarketSetStatus('success', expertMarketText('transferredOk'), 3200); } catch (err) { confirm.disabled = false; expertMarketSetStatus('error', expertMarketText('actionFailed', { error: expertMarketError(err) })); }
}

async function expertMarketAdminAction(card, button) {
  const action = button.dataset.expertAction, id = String(card.dataset.expertId || ''); if (!id || !action || button.disabled) return; if (action === 'transfer') { openExpertMarketOwnerDialog(card); return; }
  const requiresReason = action === 'unlist' || action === 'delete' || action === 'purge' || action === 'private-purge' || action === 'publish' || action === 'private-delete'; let reasonInput = card.querySelector('[data-expert-reason]'); if (requiresReason && !reasonInput) { reasonInput = expertMarketEnsureActionReason(card); reasonInput?.focus(); expertMarketSetStatus('error', expertMarketText('operationReasonRequired')); return; }
  const reason = String(reasonInput?.value || '').trim(); if (requiresReason && !reason) { reasonInput?.focus(); expertMarketSetStatus('error', expertMarketText('operationReasonRequired')); return; }
  if (action === 'delete' && !window.confirm(expertMarketText('deleteConfirm'))) { reasonInput?.focus(); return; }
  if (action === 'purge' && !window.confirm(expertMarketText('purgeConfirm'))) { reasonInput?.focus(); return; }
  if (action === 'private-purge' && !window.confirm(expertMarketText('purgeConfirm'))) { reasonInput?.focus(); return; }
  if (action === 'publish' && !window.confirm(expertMarketText('publishConfirm'))) { reasonInput?.focus(); return; }
  if (action === 'private-delete' && !window.confirm(expertMarketText('privateDeleteConfirm'))) { reasonInput?.focus(); return; }
  const route = { approve: 'approve', reject: 'reject', unlist: 'unlist', publish: 'submit-publication', 'private-delete': 'private' }[action]; const method = (action === 'delete' || action === 'purge' || action === 'private-delete' || action === 'private-purge') ? 'DELETE' : 'POST'; const suffix = action === 'delete' ? '' : action === 'purge' ? '/purge' : action === 'private-purge' ? '/private/purge' : '/' + route;
  const buttons = Array.from(card.querySelectorAll('[data-expert-action]')), labels = buttons.map(item => item.textContent); buttons.forEach(item => { item.disabled = true; }); if (reasonInput) reasonInput.disabled = true; button.textContent = expertMarketText('loading');
  try { await expertMarketAPI(`/api/v1/admin/expert-market/experts/${encodeURIComponent(id)}${suffix}`, { method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ reason }) }); const successKey = { approve: 'approvedOk', reject: 'rejectedOk', unlist: 'unlistedOk', delete: 'deletedOk', purge: 'purgedOk', 'private-purge': 'purgedOk', publish: 'publishSubmittedOk', 'private-delete': 'privateDeletedOk' }[action]; await loadExpertMarketAdmin(undefined, true); expertMarketSetStatus('success', expertMarketText(successKey), 3200); } catch (err) { expertMarketSetStatus('error', expertMarketText('actionFailed', { error: expertMarketError(err) })); buttons.forEach((item, index) => { item.disabled = false; item.textContent = labels[index]; }); if (reasonInput) reasonInput.disabled = false; }
}

function bindExpertMarketActions() {
  const grid = document.getElementById('expertMarketAdminGrid'); if (!grid || grid.dataset.expertMarketBound === 'true') return; grid.dataset.expertMarketBound = 'true'; grid.addEventListener('click', event => { const button = event.target.closest('[data-expert-action]'), card = button?.closest('.expert-market-card'); if (button && card) void expertMarketAdminAction(card, button); });
  document.getElementById('expertMarketOwnerResults')?.addEventListener('click', event => { const button = event.target.closest('[data-expert-owner-id]'); if (button) selectExpertMarketOwner(button); });
  const dialog = document.getElementById('expertMarketOwnerDialog'); dialog?.addEventListener('click', event => { if (event.target === dialog) closeExpertMarketOwnerDialog(); });
  document.addEventListener('keydown', event => { if (!expertMarketOwnerDialogState) return; if (event.key === 'Escape') { event.preventDefault(); closeExpertMarketOwnerDialog(); return; } if (event.key !== 'Tab') return; const focusable = Array.from(document.getElementById('expertMarketOwnerDialog')?.querySelectorAll('button:not([disabled]), input:not([disabled])') || []).filter(el => el.offsetParent !== null); if (!focusable.length) return; const first = focusable[0], last = focusable[focusable.length - 1]; if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); } else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); } });
}

document.addEventListener('DOMContentLoaded', () => { ensureExpertMarketAdminNav(); applyExpertMarketAdminI18n(); bindExpertMarketActions(); if (document.getElementById('tab-expertmarket')?.classList.contains('active')) void loadExpertMarketAdmin(); });
window.loadExpertMarketAdmin = loadExpertMarketAdmin; window.changeExpertMarketAdminPage = delta => loadExpertMarketAdmin(expertMarketAdminPage + delta); window.closeExpertMarketOwnerDialog = closeExpertMarketOwnerDialog; window.loadExpertMarketOwnerUsers = loadExpertMarketOwnerUsers; window.changeExpertMarketOwnerPage = changeExpertMarketOwnerPage; window.confirmExpertMarketOwnerTransfer = confirmExpertMarketOwnerTransfer;

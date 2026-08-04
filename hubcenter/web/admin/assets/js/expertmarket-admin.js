// AI Expert Market is an operator-only surface. Consumer browse, purchase and
// installation remain in the MaClaw GUI dialog; this script exposes only the
// moderation lifecycle and its server-backed status.
let expertMarketAdminPage = 1;
let expertMarketAdminInFlight = null;
let expertMarketAdminSequence = 0;
let expertMarketAdminRequestKey = '';
let expertMarketAdminStatusTimer = null;
let expertMarketAdminLoadController = null;

const EXPERT_MARKET_TEXT = {
  en: { nav: 'AI Expert Market', navDesc: 'Expert listing management', title: 'AI Expert Market', desc: 'Approve submissions to publish them immediately, then manage their visibility.', filter: 'Status', all: 'All statuses', pending: 'Pending review', listed: 'Listed', unlisted: 'Unlisted', rejected: 'Rejected', deleted: 'Deleted', refresh: 'Refresh', loading: 'Loading...', empty: 'No expert listings match this filter.', author: 'Author', version: 'Version', price: 'Credits', downloads: 'Downloads', sales: 'Sales', updated: 'Updated', approve: 'Approve & publish', reject: 'Reject', unlist: 'Unlist', remove: 'Delete', purge: 'Permanently delete', reason: 'Moderation note (optional)', reasonHint: 'Retained with the review record', operationReason: 'Operation reason', operationReasonHint: 'Required for this lifecycle action', operationReasonRequired: 'Enter an operation reason before continuing.', purgeConfirm: 'Permanently delete this package? This is allowed only when no active buyer entitlement exists.', deleteConfirm: 'Delete this unlisted package? Buyers who already own it keep their download access.', actionFailed: 'Action failed: {error}', approvedOk: 'Expert listing approved and published.', rejectedOk: 'Expert listing rejected.', unlistedOk: 'Expert listing unlisted.', deletedOk: 'Expert listing deleted.', purgedOk: 'Expert listing permanently deleted.', page: '{start}-{end} / {total}' },
  zh: { nav: 'AI 专家市场', navDesc: '专家审核与上架管理', title: 'AI 专家市场', desc: '审核通过即上架；下架后可删除。', filter: '状态', all: '全部状态', pending: '待审核', listed: '已上架', unlisted: '已下架', rejected: '已驳回', deleted: '已删除', refresh: '刷新', loading: '加载中...', empty: '没有符合条件的专家条目。', author: '作者', version: '版本', price: 'Credits', downloads: '下载', sales: '销售额', updated: '更新时间', approve: '通过并上架', reject: '驳回', unlist: '下架', remove: '删除', purge: '彻底删除', reason: '审核说明（选填）', reasonHint: '用于留存审核记录', operationReason: '操作原因', operationReasonHint: '执行此生命周期操作前必填', operationReasonRequired: '请先填写操作原因。', purgeConfirm: '确认彻底删除该专家包？仅当没有有效买家权益时允许。', deleteConfirm: '确认删除这个已下架专家包？已购买用户仍可下载。', actionFailed: '操作失败：{error}', approvedOk: '专家条目已通过审核并上架。', rejectedOk: '专家条目已驳回。', unlistedOk: '专家条目已下架。', deletedOk: '专家条目已删除。', purgedOk: '专家条目已彻底删除。', page: '{start}-{end} / {total}' }
};

function expertMarketText(key, vars = {}) { const lang = window.currentLang === 'zh' ? 'zh' : 'en'; return (EXPERT_MARKET_TEXT[lang][key] || EXPERT_MARKET_TEXT.en[key] || key).replace(/\{(\w+)\}/g, (_, n) => vars[n] ?? ''); }
function expertMarketEsc(v) { return (window.escapeHtml ? window.escapeHtml(String(v ?? '')) : String(v ?? '').replace(/[&<>"']/g, c => ({ '&':'&amp;', '<':'&lt;', '>':'&gt;', '"':'&quot;', "'":'&#39;' })[c])); }
function expertMarketStatus(v) { return expertMarketText(String(v || '').toLowerCase()); }
function expertMarketAPI(path, options) { if (typeof window.api !== 'function') throw new Error('Admin API is not ready.'); return window.api(path, options || {}); }
function expertMarketError(err) { return err?.message || String(err || expertMarketText('actionFailed')); }
function expertMarketIsAbort(err) { return err?.name === 'AbortError'; }

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
  [['expertMarketAdminTitle', 'title'], ['expertMarketAdminDesc', 'desc'], ['expertMarketAdminFilterLabel', 'filter'], ['expertMarketAdminRefresh', 'refresh']].forEach(([id, key]) => { const el = document.getElementById(id); if (el) el.textContent = expertMarketText(key); });
  const filter = document.getElementById('expertMarketAdminStatus'); if (filter) Array.from(filter.options).forEach(option => option.textContent = expertMarketText(option.value || 'all'));
}

function expertMarketActionButton(status) {
  if (status === 'pending_review') return `<button class="mp-card-btn mp-card-btn-primary" type="button" data-expert-action="approve">${expertMarketEsc(expertMarketText('approve'))}</button><button class="mp-card-btn mp-card-btn-secondary" type="button" data-expert-action="reject">${expertMarketEsc(expertMarketText('reject'))}</button>`;
  if (status === 'listed') return `<button class="mp-card-btn mp-card-btn-secondary" type="button" data-expert-action="unlist">${expertMarketEsc(expertMarketText('unlist'))}</button>`;
  if (status === 'unlisted') return `<button class="mp-card-btn mp-card-btn-danger" type="button" data-expert-action="delete">${expertMarketEsc(expertMarketText('remove'))}</button>`;
  if (status === 'deleted') return `<button class="mp-card-btn mp-card-btn-danger" type="button" data-expert-action="purge">${expertMarketEsc(expertMarketText('purge'))}</button>`;
  return '';
}

function expertMarketRenderCard(item) {
  const id = String(item.id || ''), status = String(item.status || '').toLowerCase();
  const reviewNote = status === 'pending_review' ? `<label class="expert-market-note"><span>${expertMarketEsc(expertMarketText('reason'))}</span><input type="text" maxlength="2048" data-expert-reason placeholder="${expertMarketEsc(expertMarketText('reasonHint'))}"></label>` : '';
  const actions = expertMarketActionButton(status);
  const footer = reviewNote || actions ? `<div class="expert-market-footer">${reviewNote}<div class="expert-market-actions">${actions}</div></div>` : '';
  return `<article class="expert-market-card" data-expert-id="${expertMarketEsc(id)}">
    <div class="expert-market-card-head"><div class="expert-market-avatar" aria-hidden="true">${expertMarketEsc(item.icon || 'AI')}</div><div class="expert-market-identity"><strong title="${expertMarketEsc(item.name || id)}">${expertMarketEsc(item.name || id)}</strong><span title="${expertMarketEsc(id)}">${expertMarketEsc(id)}</span></div><span class="expert-market-status expert-market-status-${expertMarketEsc(status)}">${expertMarketEsc(expertMarketStatus(status))}</span></div>
    <p class="expert-market-description" title="${expertMarketEsc(item.description || '')}">${expertMarketEsc(item.description || '—')}</p>
    <dl class="expert-market-details"><div><dt>${expertMarketEsc(expertMarketText('author'))}</dt><dd title="${expertMarketEsc(item.owner_email || '')}">${expertMarketEsc(item.owner_email || '—')}</dd></div><div><dt>${expertMarketEsc(expertMarketText('version'))}</dt><dd>${expertMarketEsc(item.version || '—')}</dd></div><div><dt>${expertMarketEsc(expertMarketText('price'))}</dt><dd>${expertMarketEsc(Number(item.price || 0).toLocaleString())}</dd></div><div><dt>${expertMarketEsc(expertMarketText('downloads'))}</dt><dd>${expertMarketEsc(Number(item.download_count || 0).toLocaleString())}</dd></div></dl>
    ${footer}
  </article>`;
}

function expertMarketEnsureActionReason(card) {
  const existing = card.querySelector('[data-expert-reason]');
  if (existing) return existing;
  const footer = card.querySelector('.expert-market-footer');
  if (!footer) return null;
  const label = document.createElement('label');
  label.className = 'expert-market-note expert-market-operation-reason';
  label.innerHTML = `<span>${expertMarketEsc(expertMarketText('operationReason'))}</span><input type="text" maxlength="2048" data-expert-reason placeholder="${expertMarketEsc(expertMarketText('operationReasonHint'))}">`;
  footer.insertBefore(label, footer.firstChild);
  return label.querySelector('[data-expert-reason]');
}

function expertMarketSetStatus(kind, message, dismissAfter = 0) {
  const status = document.getElementById('expertMarketAdminStatusMessage');
  if (!status) return;
  if (expertMarketAdminStatusTimer) { window.clearTimeout(expertMarketAdminStatusTimer); expertMarketAdminStatusTimer = null; }
  status.className = 'sm-status' + (kind ? ' show ' + kind : '');
  status.textContent = message || '';
  if (dismissAfter > 0) expertMarketAdminStatusTimer = window.setTimeout(() => expertMarketSetStatus(), dismissAfter);
}

async function loadExpertMarketAdmin(page, force = false) {
  if (Number.isFinite(page)) expertMarketAdminPage = Math.max(1, page);
  const grid = document.getElementById('expertMarketAdminGrid'), filter = document.getElementById('expertMarketAdminStatus'), refresh = document.getElementById('expertMarketAdminRefresh');
  if (!grid) return;
  const query = new URLSearchParams({ page: String(expertMarketAdminPage) }); if (filter?.value) query.set('status', filter.value);
  const requestKey = query.toString();
  if (!force && expertMarketAdminInFlight && expertMarketAdminRequestKey === requestKey) return expertMarketAdminInFlight;
  if (expertMarketAdminLoadController) expertMarketAdminLoadController.abort();
  const controller = new AbortController();
  expertMarketAdminLoadController = controller;
  const sequence = ++expertMarketAdminSequence;
  expertMarketAdminRequestKey = requestKey;
  grid.setAttribute('aria-busy', 'true'); grid.innerHTML = `<div class="hint">${expertMarketEsc(expertMarketText('loading'))}</div>`; expertMarketSetStatus('loading', expertMarketText('loading')); if (refresh) { refresh.disabled = true; refresh.textContent = expertMarketText('loading'); }
  expertMarketAdminInFlight = (async () => {
    try {
      const result = await expertMarketAPI(`/api/v1/admin/expert-market/experts?${query}`, { signal: controller.signal }); if (sequence !== expertMarketAdminSequence) return;
      const experts = Array.isArray(result?.experts) ? result.experts : [], total = Number(result?.total || 0), pages = Math.max(1, Number(result?.total_pages || 1));
      if (!experts.length && total > 0 && expertMarketAdminPage > pages) { expertMarketAdminPage = pages; expertMarketAdminInFlight = null; expertMarketAdminRequestKey = ''; return loadExpertMarketAdmin(pages); }
      grid.innerHTML = experts.length ? experts.map(expertMarketRenderCard).join('') : `<div class="hint">${expertMarketEsc(expertMarketText('empty'))}</div>`;
      grid.setAttribute('aria-busy', 'false'); expertMarketSetStatus();
      const info = document.getElementById('expertMarketAdminPageInfo'), prev = document.getElementById('expertMarketAdminPrev'), next = document.getElementById('expertMarketAdminNext');
      if (info) info.textContent = expertMarketText('page', { start: total ? ((expertMarketAdminPage - 1) * 20 + 1) : 0, end: Math.min(expertMarketAdminPage * 20, total), total });
      if (prev) prev.disabled = expertMarketAdminPage <= 1; if (next) next.disabled = expertMarketAdminPage >= pages;
    } catch (err) {
      if (expertMarketIsAbort(err)) return;
      if (sequence !== expertMarketAdminSequence) return;
      const message = expertMarketText('actionFailed', { error: expertMarketError(err) }); grid.innerHTML = `<div class="hint">${expertMarketEsc(message)}</div>`; grid.setAttribute('aria-busy', 'false'); expertMarketSetStatus('error', message);
    } finally { if (expertMarketAdminLoadController === controller) expertMarketAdminLoadController = null; if (sequence === expertMarketAdminSequence && refresh) { refresh.disabled = false; refresh.textContent = expertMarketText('refresh'); } }
  })();
  try { return await expertMarketAdminInFlight; } finally { if (sequence === expertMarketAdminSequence) { expertMarketAdminInFlight = null; expertMarketAdminRequestKey = ''; } }
}

async function expertMarketAdminAction(card, button) {
  const action = button.dataset.expertAction, id = String(card.dataset.expertId || '');
  if (!id || !action || button.disabled) return;
  const requiresReason = action === 'unlist' || action === 'delete' || action === 'purge';
  let reasonInput = card.querySelector('[data-expert-reason]');
  if (requiresReason && !reasonInput) { reasonInput = expertMarketEnsureActionReason(card); reasonInput?.focus(); expertMarketSetStatus('error', expertMarketText('operationReasonRequired')); return; }
  const reason = String(reasonInput?.value || '').trim();
  if (requiresReason && !reason) { reasonInput?.focus(); expertMarketSetStatus('error', expertMarketText('operationReasonRequired')); return; }
  if (action === 'purge' && !window.confirm(expertMarketText('purgeConfirm'))) { reasonInput?.focus(); return; }
  if (action === 'delete' && !window.confirm(expertMarketText('deleteConfirm'))) { reasonInput?.focus(); return; }
  const route = { approve: 'approve', reject: 'reject', unlist: 'unlist' }[action], method = (action === 'delete' || action === 'purge') ? 'DELETE' : 'POST';
  const buttons = Array.from(card.querySelectorAll('[data-expert-action]')), labels = buttons.map(item => item.textContent); buttons.forEach(item => { item.disabled = true; }); if (reasonInput) reasonInput.disabled = true; button.textContent = expertMarketText('loading');
  try {
    await expertMarketAPI(`/api/v1/admin/expert-market/experts/${encodeURIComponent(id)}${action === 'delete' ? '' : action === 'purge' ? '/purge' : '/' + route}`, { method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ reason }) });
    const successKey = { approve: 'approvedOk', reject: 'rejectedOk', unlist: 'unlistedOk', delete: 'deletedOk', purge: 'purgedOk' }[action];
    // A list refresh may have started before this moderation request finished.
    // Force a new read so a stale response cannot leave the card in its old state.
    await loadExpertMarketAdmin(undefined, true);
    expertMarketSetStatus('success', expertMarketText(successKey), 3200);
  } catch (err) { expertMarketSetStatus('error', expertMarketText('actionFailed', { error: expertMarketError(err) })); buttons.forEach((item, index) => { item.disabled = false; item.textContent = labels[index]; }); if (reasonInput) reasonInput.disabled = false; }
}

function bindExpertMarketActions() {
  const grid = document.getElementById('expertMarketAdminGrid'); if (!grid || grid.dataset.expertMarketBound === 'true') return;
  grid.dataset.expertMarketBound = 'true';
  grid.addEventListener('click', event => { const button = event.target.closest('[data-expert-action]'), card = button?.closest('.expert-market-card'); if (button && card) void expertMarketAdminAction(card, button); });
}

document.addEventListener('DOMContentLoaded', () => {
  ensureExpertMarketAdminNav();
  applyExpertMarketAdminI18n();
  bindExpertMarketActions();
  // admin-core restores the last tab before this split script is evaluated.
  // If Expert Market was the saved tab, load it once this module is ready.
  if (document.getElementById('tab-expertmarket')?.classList.contains('active')) void loadExpertMarketAdmin();
});
window.loadExpertMarketAdmin = loadExpertMarketAdmin;
window.changeExpertMarketAdminPage = delta => loadExpertMarketAdmin(expertMarketAdminPage + delta);

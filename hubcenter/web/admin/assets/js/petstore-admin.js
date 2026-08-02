// Pet Store moderation is intentionally independent from the capability
// marketplace review queue: pet packages publish immediately, then operators
// can inspect, pause, restore, or remove them here.
let petAdminPage = 1;
let petAdminInFlight = null;
let petAdminRequestKey = '';
let petAdminRequestSequence = 0;
let petAdminLanguageWatcher = null;

const PET_ADMIN_TEXT = {
  en: {
    nav: 'Pet Store', navDesc: 'Pet package management', title: 'Pet Store',
    desc: 'Inspect packages published directly to the market, then pause, restore, or remove them.',
    status: 'Status', all: 'All except deleted', refresh: 'Refresh', loading: 'Loading...',
    empty: 'No pet packages match this filter.', page: '{start}-{end} / {total}',
    preview: 'Preview images', hidePreview: 'Hide preview', pause: 'Pause', resume: 'Restore', remove: 'Delete', purge: 'Permanently delete',
    confirmDelete: 'Delete this pet package? Existing buyers will no longer be able to download it. This cannot be undone.', confirmPurge: 'Permanently delete this already removed pet package? Its package file will be cleared and it cannot be recovered.',
    active: 'Active', paused: 'Paused', withdrawn: 'Withdrawn', deleted: 'Deleted', purged: 'Permanently deleted',
    free: 'Free', credits: 'credits', owner: 'Publisher', source: 'Source package', version: 'Version',
    downloads: 'Downloads', purchases: 'Purchases', sales: 'Sales', updated: 'Updated',
    loadFailed: 'Could not load pet packages: {error}', actionFailed: 'Action failed: {error}',
    pausedOk: 'Pet package paused.', resumedOk: 'Pet package restored.', deletedOk: 'Pet package deleted.', purgedOk: 'Pet package permanently deleted.',
    noImages: 'No PNG, JPEG, or WebP preview images in this package.', previewFailed: 'Could not load preview: {error}'
  },
  zh: {
    nav: '\u5ba0\u7269\u5e02\u573a', navDesc: '\u5ba0\u7269\u5305\u7ba1\u7406', title: '\u5ba0\u7269\u5e02\u573a',
    desc: '\u67e5\u770b\u76f4\u63a5\u53d1\u5e03\u5230\u5e02\u573a\u7684\u5ba0\u7269\u5305\uff0c\u5e76\u53ef\u6682\u505c\u3001\u6062\u590d\u6216\u5220\u9664\u3002',
    status: '\u72b6\u6001', all: '\u5168\u90e8\uff08\u4e0d\u542b\u5df2\u5220\u9664\uff09', refresh: '\u5237\u65b0', loading: '\u52a0\u8f7d\u4e2d...',
    empty: '\u6ca1\u6709\u7b26\u5408\u7b5b\u9009\u6761\u4ef6\u7684\u5ba0\u7269\u5305\u3002', page: '{start}-{end} / {total}',
    preview: '\u67e5\u770b\u9884\u89c8\u56fe', hidePreview: '\u6536\u8d77\u9884\u89c8', pause: '\u6682\u505c', resume: '\u6062\u590d', remove: '\u5220\u9664', purge: '\u5f7b\u5e95\u5220\u9664',
    confirmDelete: '\u786e\u5b9a\u5220\u9664\u8be5\u5ba0\u7269\u5305\uff1f\u5df2\u8d2d\u4e70\u7528\u6237\u5c06\u65e0\u6cd5\u518d\u4e0b\u8f7d\uff0c\u6b64\u64cd\u4f5c\u4e0d\u53ef\u64a4\u9500\u3002', confirmPurge: '\u786e\u5b9a\u5f7b\u5e95\u5220\u9664\u5df2\u5220\u9664\u7684\u5ba0\u7269\u5305\uff1f\u5305\u6587\u4ef6\u5c06\u88ab\u6e05\u9664\uff0c\u4e14\u65e0\u6cd5\u6062\u590d\u3002',
    active: '\u5df2\u4e0a\u67b6', paused: '\u5df2\u6682\u505c', withdrawn: '\u5df2\u4e0b\u67b6', deleted: '\u5df2\u5220\u9664', purged: '\u5df2\u5f7b\u5e95\u5220\u9664',
    free: '\u514d\u8d39', credits: '\u79ef\u5206', owner: '\u53d1\u5e03\u8005\uff08\u90ae\u7bb1\uff09', source: '\u6e90\u5ba0\u7269\u5305', version: '\u7248\u672c',
    downloads: '\u4e0b\u8f7d', purchases: '\u8d2d\u4e70', sales: '\u9500\u552e\u989d', updated: '\u66f4\u65b0\u65f6\u95f4',
    loadFailed: '\u52a0\u8f7d\u5ba0\u7269\u5305\u5931\u8d25\uff1a{error}', actionFailed: '\u64cd\u4f5c\u5931\u8d25\uff1a{error}',
    pausedOk: '\u5ba0\u7269\u5305\u5df2\u6682\u505c\u3002', resumedOk: '\u5ba0\u7269\u5305\u5df2\u6062\u590d\u3002', deletedOk: '\u5ba0\u7269\u5305\u5df2\u5220\u9664\u3002', purgedOk: '\u5ba0\u7269\u5305\u5df2\u5f7b\u5e95\u5220\u9664\u3002',
    noImages: '\u8be5\u5305\u5185\u6ca1\u6709 PNG\u3001JPEG \u6216 WebP \u9884\u89c8\u56fe\u3002', previewFailed: '\u52a0\u8f7d\u9884\u89c8\u5931\u8d25\uff1a{error}'
  }
};

function petAdminText(key, vars = {}) {
  const lang = window.currentLang === 'zh' ? 'zh' : 'en';
  return (PET_ADMIN_TEXT[lang][key] || PET_ADMIN_TEXT.en[key] || key).replace(/\{(\w+)\}/g, (_, name) => vars[name] ?? '');
}

function petAdminStatusText(status) { return petAdminText(String(status || '').toLowerCase()) || status || '-'; }

// Keep this script safe even if it is loaded before admin-core, or if a future
// admin-core refactor stops publishing escapeHtml on window.
function petAdminEscape(value) {
  const text = String(value ?? '');
  if (typeof window.escapeHtml === 'function') return window.escapeHtml(text);
  return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}
function petAdminNumber(value) { return new Intl.NumberFormat(window.currentLang === 'zh' ? 'zh-CN' : 'en-US').format(Number(value || 0)); }
function petAdminDate(value) { return value && typeof fmtDate === 'function' ? fmtDate(value) : (value || '-'); }
function petAdminErrorMessage(err) { return err && err.message ? err.message : String(err || petAdminText('actionFailed')); }
function petAdminDataURL(value) {
  const url = String(value || '').trim();
  return /^data:image\/(?:png|jpe?g|webp);base64,[a-z0-9+/=]+$/i.test(url) ? url : '';
}

// admin-core owns the authenticated request helper inside its own module
// scope. Pet Store is a separate script, so use the deliberately exported
// window API instead of relying on that lexical binding.
function petAdminAPI(path, options) {
  if (typeof window.api !== 'function') {
    throw new Error('Admin API is not ready. Please refresh and try again.');
  }
  return window.api(path, options || {});
}

function ensurePetStoreAdminNav() {
  if (document.querySelector('.nav button[data-tab="petstore"]')) return;
  const capabilityButton = document.querySelector('.nav button[data-tab="skillmarket"]');
  if (!capabilityButton || !capabilityButton.parentElement) return;
  const button = document.createElement('button');
  button.type = 'button';
  button.dataset.tab = 'petstore';
  button.innerHTML = '<span class="nav-icon" aria-hidden="true"><svg viewBox="0 0 24 24"><path d="M5 15.5c1.5-2.1 3.2-2.1 4.7 0"></path><path d="M14.3 15.5c1.5-2.1 3.2-2.1 4.7 0"></path><circle cx="8" cy="10" r="2"></circle><circle cx="16" cy="10" r="2"></circle><circle cx="12" cy="6" r="2"></circle><path d="M12 14.2c-2.1 0-3.8 1.3-3.8 3 0 1.5 1.5 2.8 3.8 2.8s3.8-1.3 3.8-2.8c0-1.7-1.7-3-3.8-3Z"></path></svg></span><span></span><small></small>';
  button.addEventListener('click', () => window.openTab('petstore'));
  capabilityButton.insertAdjacentElement('afterend', button);
  applyPetStoreAdminI18n();
}

function applyPetStoreAdminI18n() {
  const nav = document.querySelector('.nav button[data-tab="petstore"]');
  if (nav) {
    const title = nav.querySelector('span:not(.nav-icon)');
    const desc = nav.querySelector('small');
    if (title) title.textContent = petAdminText('nav');
    if (desc) desc.textContent = petAdminText('navDesc');
  }
  const update = (id, text) => { const el = document.getElementById(id); if (el) el.textContent = text; };
  update('petAdminTitle', petAdminText('title'));
  update('petAdminDesc', petAdminText('desc'));
  update('petAdminStatusLabel', petAdminText('status'));
  update('petAdminRefreshBtn', petAdminText('refresh'));
  const filter = document.getElementById('petAdminStatusFilter');
  if (filter) {
    Array.from(filter.options).forEach(option => { option.textContent = option.value ? petAdminStatusText(option.value) : petAdminText('all'); });
  }
}

function setPetAdminState(state, message = '') {
  const el = document.getElementById('petAdminStatus');
  if (!el) return;
  el.className = 'sm-status' + (state ? ' show ' + state : '');
  el.textContent = message;
}

function petAdminRequestIsCurrent(sequence) {
  return sequence === petAdminRequestSequence;
}

function petAdminRenderCard(pack) {
  const id = String(pack.id || '');
  const status = String(pack.status || '');
  const price = Number(pack.price || 0);
  const priceText = price === 0 ? petAdminText('free') : petAdminNumber(price) + ' ' + petAdminText('credits');
  const action = status === 'active'
    ? `<button class="mp-card-btn mp-card-btn-secondary" type="button" data-pet-admin-action="pause">${petAdminEscape(petAdminText('pause'))}</button>`
    : status === 'paused'
      ? `<button class="mp-card-btn mp-card-btn-secondary" type="button" data-pet-admin-action="resume">${petAdminEscape(petAdminText('resume'))}</button>`
      : '';
  const destructiveAction = status === 'deleted'
    ? `<button class="mp-card-btn mp-card-btn-danger" type="button" data-pet-admin-action="purge">${petAdminEscape(petAdminText('purge'))}</button>`
    : status === 'purged'
      ? ''
      : `<button class="mp-card-btn mp-card-btn-danger" type="button" data-pet-admin-action="delete">${petAdminEscape(petAdminText('remove'))}</button>`;
  const preview = petAdminDataURL(pack.preview_data_url);
  const cover = preview
    ? `<img class="pet-admin-cover" loading="lazy" src="${petAdminEscape(preview)}" alt="${petAdminEscape(pack.name || id)}">`
    : `<div class="pet-admin-cover pet-admin-cover-fallback" role="img" aria-label="${petAdminEscape(pack.name || id)}">${petAdminEscape((pack.name || id || '?').slice(0, 1).toUpperCase())}</div>`;
  return `<article class="pet-admin-card" data-pack-id="${petAdminEscape(id)}">
    <div class="pet-admin-card-head"><div class="pet-admin-card-title"><strong title="${petAdminEscape(pack.name || id)}">${petAdminEscape(pack.name || id)}</strong><span class="badge pet-admin-status-${petAdminEscape(status)}">${petAdminEscape(petAdminStatusText(status))}</span></div><span class="pet-admin-price">${petAdminEscape(priceText)}</span></div>
    ${cover}
    <p class="pet-admin-description">${petAdminEscape(pack.description || '—')}</p>
    <dl class="pet-admin-details"><div><dt>${petAdminEscape(petAdminText('owner'))}</dt><dd title="${petAdminEscape(pack.owner_email || '')}">${petAdminEscape(pack.owner_email || '—')}</dd></div><div><dt>${petAdminEscape(petAdminText('version'))}</dt><dd>${petAdminEscape(pack.version || '—')}</dd></div><div><dt>${petAdminEscape(petAdminText('source'))}</dt><dd class="mono" title="${petAdminEscape(pack.source_pack_id || '')}">${petAdminEscape(pack.source_pack_id || '—')}</dd></div><div><dt>${petAdminEscape(petAdminText('updated'))}</dt><dd>${petAdminEscape(petAdminDate(pack.updated_at))}</dd></div></dl>
    <div class="pet-admin-stats"><span>${petAdminEscape(petAdminText('downloads'))}<strong>${petAdminEscape(petAdminNumber(pack.download_count))}</strong></span><span>${petAdminEscape(petAdminText('purchases'))}<strong>${petAdminEscape(petAdminNumber(pack.purchase_count))}</strong></span><span>${petAdminEscape(petAdminText('sales'))}<strong>${petAdminEscape(petAdminNumber(pack.sales_amount))}</strong></span></div>
    <div class="pet-admin-actions"><button class="mp-card-btn mp-card-btn-ghost" type="button" data-pet-admin-action="preview">${petAdminEscape(petAdminText('preview'))}</button>${action}${destructiveAction}</div>
    <div class="pet-admin-preview" hidden aria-hidden="true"></div>
  </article>`;
}

async function loadPetStoreAdmin(page) {
  if (page) petAdminPage = Math.max(1, Number(page) || 1);
  const filter = document.getElementById('petAdminStatusFilter');
  const status = filter ? filter.value : '';
  const requestKey = `${petAdminPage}|${status}`;
  if (petAdminInFlight && petAdminRequestKey === requestKey) return petAdminInFlight;
  const requestSequence = ++petAdminRequestSequence;
  petAdminRequestKey = requestKey;
  petAdminInFlight = (async () => {
    const root = document.getElementById('petAdminGrid');
    const refresh = document.getElementById('petAdminRefreshBtn');
    const pager = document.getElementById('petAdminPager');
    if (!root) return;
    if (refresh) { refresh.disabled = true; refresh.textContent = petAdminText('loading'); }
    root.setAttribute('aria-busy', 'true');
    root.innerHTML = '<div class="pet-admin-loading">' + petAdminEscape(petAdminText('loading')) + '</div>';
    if (pager) pager.classList.remove('is-visible');
    setPetAdminState('loading', petAdminText('loading'));
    try {
      const params = new URLSearchParams({page: String(petAdminPage)});
      if (status) params.set('status', status);
      const payload = await petAdminAPI('/api/v1/admin/pet-store/packs?' + params.toString());
      // A response can arrive after the operator has changed page or status.
      // Ignore it rather than replacing the newer view with stale cards.
      if (!petAdminRequestIsCurrent(requestSequence)) return;
      const packs = Array.isArray(payload.packs) ? payload.packs : [];
      const total = Number(payload.total || 0);
      const totalPages = Math.max(1, Number(payload.total_pages || 1));
      if (!packs.length && total > 0 && petAdminPage > totalPages) { petAdminPage = totalPages; return loadPetStoreAdmin(petAdminPage); }
      root.setAttribute('aria-busy', 'false');
      if (!packs.length) { root.innerHTML = '<div class="hint pet-admin-empty">' + petAdminEscape(petAdminText('empty')) + '</div>'; setPetAdminState(); return; }
      root.innerHTML = packs.map(petAdminRenderCard).join('');
      setPetAdminState();
      if (pager) pager.classList.toggle('is-visible', totalPages > 1);
      const start = (petAdminPage - 1) * 20 + 1;
      const end = Math.min(start + packs.length - 1, total);
      const info = document.getElementById('petAdminPageInfo');
      if (info) info.textContent = petAdminText('page', {start, end, total});
      const prev = document.getElementById('petAdminPrevBtn'), next = document.getElementById('petAdminNextBtn');
      if (prev) prev.disabled = petAdminPage <= 1;
      if (next) next.disabled = petAdminPage >= totalPages;
    } catch (err) {
      if (!petAdminRequestIsCurrent(requestSequence)) return;
      root.setAttribute('aria-busy', 'false');
      const message = petAdminText('loadFailed', {error: petAdminErrorMessage(err)});
      root.innerHTML = '<div class="hint pet-admin-empty">' + petAdminEscape(message) + '</div>';
      setPetAdminState('error', message);
      showToast(message, 'error');
    } finally {
      if (petAdminRequestIsCurrent(requestSequence) && refresh) {
        refresh.disabled = false;
        refresh.textContent = petAdminText('refresh');
      }
    }
  })();
  try { return await petAdminInFlight; }
  finally {
    if (petAdminRequestIsCurrent(requestSequence)) {
      petAdminInFlight = null;
      petAdminRequestKey = '';
    }
  }
}

function changePetStoreAdminPage(delta) { loadPetStoreAdmin(petAdminPage + delta); }

function bindPetStoreAdminActions() {
  const root = document.getElementById('petAdminGrid');
  if (!root || root.dataset.petAdminActionsBound === 'true') return;
  root.dataset.petAdminActionsBound = 'true';
  root.addEventListener('click', event => {
    const target = event.target;
    const button = target && target.closest ? target.closest('button[data-pet-admin-action]') : null;
    if (!button || !root.contains(button) || button.disabled) return;
    const card = button.closest('.pet-admin-card');
    const id = card && String(card.dataset.packId || '');
    if (!id) return;
    const action = button.dataset.petAdminAction;
    if (action === 'preview') {
      togglePetStorePreview(id, button);
    } else if (action === 'pause' || action === 'resume') {
      petAdminSetStatus(id, action, button);
    } else if (action === 'delete') {
      deletePetStorePack(id, button);
    } else if (action === 'purge') {
      purgePetStorePack(id, button);
    }
  });
}

async function petAdminSetStatus(id, action, button) {
  const prior = button ? button.textContent : '';
  if (button) { button.disabled = true; button.textContent = petAdminText('loading'); }
  try {
    await petAdminAPI('/api/v1/admin/pet-store/packs/' + encodeURIComponent(id) + '/' + action, {method: 'POST'});
    showToast(action === 'pause' ? petAdminText('pausedOk') : petAdminText('resumedOk'), 'success');
    await loadPetStoreAdmin();
  } catch (err) { showToast(petAdminText('actionFailed', {error: petAdminErrorMessage(err)}), 'error'); }
  finally { if (button && document.body.contains(button)) { button.disabled = false; button.textContent = prior; } }
}

async function deletePetStorePack(id, button) {
  if (!window.confirm(petAdminText('confirmDelete'))) return;
  const prior = button ? button.textContent : '';
  if (button) { button.disabled = true; button.textContent = petAdminText('loading'); }
  try {
    await petAdminAPI('/api/v1/admin/pet-store/packs/' + encodeURIComponent(id), {method: 'DELETE'});
    showToast(petAdminText('deletedOk'), 'success');
    await loadPetStoreAdmin();
  } catch (err) { showToast(petAdminText('actionFailed', {error: petAdminErrorMessage(err)}), 'error'); }
  finally { if (button && document.body.contains(button)) { button.disabled = false; button.textContent = prior; } }
}

async function purgePetStorePack(id, button) {
  if (!window.confirm(petAdminText('confirmPurge'))) return;
  const prior = button ? button.textContent : '';
  if (button) { button.disabled = true; button.textContent = petAdminText('loading'); }
  try {
    await petAdminAPI('/api/v1/admin/pet-store/packs/' + encodeURIComponent(id) + '/purge', {method: 'DELETE'});
    showToast(petAdminText('purgedOk'), 'success');
    await loadPetStoreAdmin();
  } catch (err) { showToast(petAdminText('actionFailed', {error: petAdminErrorMessage(err)}), 'error'); }
  finally { if (button && document.body.contains(button)) { button.disabled = false; button.textContent = prior; } }
}

// `hidden` is the semantic source of truth. The explicit inline fallback
// keeps the control reliable if an old cached stylesheet still declares the
// preview container as a grid.
function setPetStorePreviewVisibility(target, visible) {
  target.hidden = !visible;
  target.setAttribute('aria-hidden', String(!visible));
  target.style.display = visible ? '' : 'none';
}

async function togglePetStorePreview(id, button) {
  const card = button && button.closest('.pet-admin-card');
  const target = card && card.querySelector('.pet-admin-preview');
  if (!target) return;
  if (!target.hidden) { setPetStorePreviewVisibility(target, false); button.textContent = petAdminText('preview'); return; }
  if (target.dataset.loaded === 'true') { setPetStorePreviewVisibility(target, true); button.textContent = petAdminText('hidePreview'); return; }
  const prior = button.textContent;
  button.disabled = true; button.textContent = petAdminText('loading'); setPetStorePreviewVisibility(target, true); target.innerHTML = '<div class="pet-admin-preview-state">' + petAdminEscape(petAdminText('loading')) + '</div>';
  try {
    const data = await petAdminAPI('/api/v1/admin/pet-store/packs/' + encodeURIComponent(id) + '/preview');
    const images = Array.isArray(data.images) ? data.images : [];
    target.dataset.loaded = 'true';
    const safeImages = images.map(image => ({...image, dataURL: petAdminDataURL(image && image.data_url)})).filter(image => image.dataURL);
    target.innerHTML = safeImages.length ? safeImages.map(image => `<figure><img loading="lazy" src="${petAdminEscape(image.dataURL)}" alt="${petAdminEscape(image.name || '')}"><figcaption title="${petAdminEscape(image.name || '')}">${petAdminEscape(image.name || '')}</figcaption></figure>`).join('') : '<div class="pet-admin-preview-state">' + petAdminEscape(petAdminText('noImages')) + '</div>';
    button.textContent = petAdminText('hidePreview');
  } catch (err) { target.innerHTML = '<div class="pet-admin-preview-state error">' + petAdminEscape(petAdminText('previewFailed', {error: petAdminErrorMessage(err)})) + '</div>'; button.textContent = prior; }
  finally { button.disabled = false; }
}

document.addEventListener('DOMContentLoaded', () => {
  ensurePetStoreAdminNav();
  applyPetStoreAdminI18n();
  bindPetStoreAdminActions();
  if (!petAdminLanguageWatcher) {
    petAdminLanguageWatcher = new MutationObserver(() => applyPetStoreAdminI18n());
    petAdminLanguageWatcher.observe(document.documentElement, {attributes: true, attributeFilter: ['lang']});
  }
});

window.loadPetStoreAdmin = loadPetStoreAdmin;
window.changePetStoreAdminPage = changePetStoreAdminPage;
window.petAdminSetStatus = petAdminSetStatus;
window.deletePetStorePack = deletePetStorePack;
window.purgePetStorePack = purgePetStorePack;
window.togglePetStorePreview = togglePetStorePreview;

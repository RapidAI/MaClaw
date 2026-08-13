// Industry management is intentionally separate from the consumer expert
// market: it turns author-authorized listings into immutable platform assets
// and controls tenant visibility without granting a user purchase entitlement.
let industryManagementState = { industries: [], assets: [], acquired: [], audit: [], bindings: new Map() };
let industryManagementLoading = null;
const industryBindingLoads = new Map();

const IM_TEXT = {
  en: {
    nav: 'Industry Management', navDesc: 'Industry default AI experts', title: 'Industry Management', desc: 'Create industries, bind immutable expert assets, and distribute them by Hub tenant.', refresh: 'Refresh', loading: 'Loading…', create: 'Create industry', created: 'Industry created.', acquire: 'Acquire as platform asset', acquired: 'Asset acquired.', noIndustries: 'No industries have been created.', noEligible: 'No eligible expert listings. Authors must enable platform distribution.', eligibleTitle: 'Eligible marketplace experts', eligibleDesc: 'Only listings whose author permits platform distribution can be acquired.', industriesTitle: 'Industries', industriesDesc: 'Bind fixed assets and manage distribution status.', assetsTitle: 'Acquired immutable assets', assetsDesc: 'Revoking an asset removes it from every assigned tenant catalogue.', auditTitle: 'Recent industry audit', noAssets: 'No acquired assets.', bindings: 'Bound assets', saveBindings: 'Save bindings', bindingsLoading: 'Loading current bindings…', enable: 'Enable', disable: 'Disable', saved: 'Industry bindings saved.', failed: 'Industry management failed: {error}', status: 'Status', version: 'Version', price: 'Credits', revoke: 'Revoke asset', revokeConfirm: 'Revoke this asset? It will be removed from every tenant catalogue that uses it.', reasonPrompt: 'Enter a reason for revocation:', revoked: 'Asset revoked.', active: 'Active', disabled: 'Disabled', ready: 'Ready', revokedStatus: 'Revoked', none: 'None', noDescription: 'No description', codePlaceholder: 'e.g. finance', namePlaceholder: 'e.g. Financial services', auditActionCreated: 'Industry created', auditActionUpdated: 'Industry updated', auditActionAssetAcquired: 'Platform asset acquired', auditActionBindingsReplaced: 'Industry asset bindings updated', auditActionAssetRevoked: 'Platform asset revoked', auditActionTenantIndustriesReplaced: 'Tenant industries updated', auditTargetIndustry: 'Industry', auditTargetAsset: 'Platform asset', auditTargetHubTenant: 'Hub tenant', generalDefaultNote: 'System default: tenants without explicit industries use this industry automatically.', tenantSettingsTitle: 'Industry settings', tenantSettingsLoadingDesc: 'Select one or more industries for this tenant; their default experts synchronize through the Hub.', tenantSettingsDesc: 'Choose one or more industries. Saving automatically refreshes this tenant catalogue.', tenantDefaultHint: 'No explicit industry is configured. This tenant uses General by default. Saving selections replaces it; clearing all selections restores General.', catalogueRevision: 'Catalogue revision', experts: 'Experts', saveTenantSettings: 'Save industry settings', tenantSettingsSaved: 'Tenant industries saved; catalogue revision {revision}.', bindingLoadFailed: 'Some current bindings could not be loaded. Refresh and try again.', code: 'Code', name: 'Name', description: 'Description', icon: 'Icon', order: 'Order', unknownError: 'Unknown error'
  },
  zh: {
    nav: '行业管理', navDesc: '行业默认 AI 专家', title: '行业管理', desc: '创建行业、绑定不可变专家资产，并按 Hub 租户分发。', refresh: '刷新', loading: '正在加载…', create: '创建行业', created: '行业已创建。', acquire: '获取为平台资产', acquired: '资产已获取。', noIndustries: '尚未创建行业。', noEligible: '暂无符合条件的专家。作者须允许平台按行业分发。', eligibleTitle: '可获取的市场专家', eligibleDesc: '仅作者允许平台分发的专家上架项可获取为平台资产。', industriesTitle: '行业列表', industriesDesc: '绑定不可变资产并管理分发状态。', assetsTitle: '已获取的不可变资产', assetsDesc: '撤销资产会将其从所有已分配租户的专家目录中移除。', auditTitle: '最近行业审计记录', noAssets: '尚未获取平台资产。', bindings: '已绑定资产', saveBindings: '保存绑定', bindingsLoading: '正在加载当前绑定…', enable: '启用', disable: '停用', saved: '行业资产绑定已保存。', failed: '行业管理操作失败：{error}', status: '状态', version: '版本', price: '积分', revoke: '撤销资产', revokeConfirm: '确定撤销这个资产吗？它会从所有使用它的租户目录中移除。', reasonPrompt: '请输入撤销原因：', revoked: '资产已撤销。', active: '启用', disabled: '停用', ready: '可用', revokedStatus: '已撤销', none: '无', noDescription: '暂无描述', codePlaceholder: '例如：finance', namePlaceholder: '例如：金融服务', auditActionCreated: '已创建行业', auditActionUpdated: '已更新行业', auditActionAssetAcquired: '已获取平台资产', auditActionBindingsReplaced: '已更新行业资产绑定', auditActionAssetRevoked: '已撤销平台资产', auditActionTenantIndustriesReplaced: '已更新租户行业', auditTargetIndustry: '行业', auditTargetAsset: '平台资产', auditTargetHubTenant: '节点租户', generalDefaultNote: '系统内置：未设置行业的租户自动使用此行业。', tenantSettingsTitle: '行业设置', tenantSettingsLoadingDesc: '为该租户选择一个或多个行业；其默认专家会随 Hub 同步。', tenantSettingsDesc: '选择一个或多个行业，保存后自动更新该租户的专家目录。', tenantDefaultHint: '当前未配置行业，正在使用“通用行业”。勾选并保存后将改用所选行业；全部取消后恢复通用行业。', catalogueRevision: '目录版本', experts: '专家数', saveTenantSettings: '保存行业设置', tenantSettingsSaved: '租户行业设置已保存，目录版本 {revision}。', bindingLoadFailed: '部分已绑定资产加载失败，请刷新后重试。', code: '编码', name: '名称', description: '描述', icon: '图标', order: '排序', unknownError: '未知错误'
  }
};
function imLang() { return window.currentLang === 'zh' ? 'zh' : 'en'; }
function imT(key, vars = {}) { return (IM_TEXT[imLang()][key] || IM_TEXT.en[key] || key).replace(/\{(\w+)\}/g, (_, k) => vars[k] ?? ''); }
function imEsc(value) { return window.escapeHtml ? window.escapeHtml(String(value ?? '')) : String(value ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]); }
function imAPI(path, options) { if (typeof window.api !== 'function') throw new Error('Admin API is not ready.'); return window.api(path, options || {}); }
function imError(error) { return error?.message || String(error || imT('unknownError')); }
function imSetStatus(kind, message) { const el = document.getElementById('industryManagementStatus'); if (!el) return; el.className = 'sm-status' + (kind ? ' show ' + kind : ''); el.textContent = message || ''; }
function imStatus(status) { const key = String(status || '').toLowerCase(); return imT(key === 'active' ? 'active' : key === 'disabled' ? 'disabled' : key === 'ready' ? 'ready' : key === 'revoked' ? 'revokedStatus' : key); }
function imNumber(value) { return Number(value || 0).toLocaleString(imLang() === 'zh' ? 'zh-CN' : 'en-US'); }
function imAuditAction(action) { return imT({ 'industry.created': 'auditActionCreated', 'industry.updated': 'auditActionUpdated', 'asset.acquired': 'auditActionAssetAcquired', 'industry.bindings.replaced': 'auditActionBindingsReplaced', 'asset.revoked': 'auditActionAssetRevoked', 'tenant.industries.replaced': 'auditActionTenantIndustriesReplaced' }[String(action || '')] || 'none'); }
function imAuditTarget(type) { return imT({ industry: 'auditTargetIndustry', asset: 'auditTargetAsset', hub_tenant: 'auditTargetHubTenant' }[String(type || '')] || 'none'); }
function imFormatDate(value) { const date = new Date(value); return Number.isNaN(date.getTime()) ? String(value || '') : new Intl.DateTimeFormat(imLang() === 'zh' ? 'zh-CN' : 'en-US', { dateStyle: 'medium', timeStyle: 'short' }).format(date); }

function ensureIndustryManagementNav() {
  if (document.querySelector('.nav button[data-tab="industrymanagement"]')) return;
  const anchor = document.querySelector('.nav button[data-tab="expertmarket"]') || document.querySelector('.nav button[data-tab="skillmarket"]');
  if (!anchor?.parentElement) return;
  const button = document.createElement('button'); button.type = 'button'; button.dataset.tab = 'industrymanagement';
  button.innerHTML = '<span class="nav-icon" aria-hidden="true">⌘</span><span></span><small></small>';
  button.addEventListener('click', () => window.openTab('industrymanagement'));
  anchor.parentElement.insertBefore(button, anchor.nextSibling);
  applyIndustryManagementI18n();
}
function applyIndustryManagementI18n() {
  const nav = document.querySelector('.nav button[data-tab="industrymanagement"]');
  if (nav) { const title = nav.querySelector('span:not(.nav-icon)'), desc = nav.querySelector('small'); if (title) title.textContent = imT('nav'); if (desc) desc.textContent = imT('navDesc'); }
  const labels = [['industryManagementTitle','title'],['industryManagementDesc','desc'],['industryManagementRefresh','refresh'],['industryCreateTitle','create'],['industryCreateCodeLabel','code'],['industryCreateNameLabel','name'],['industryCreateIconLabel','icon'],['industryCreateSortLabel','order'],['industryCreateDescriptionLabel','description'],['industryCreateSave','create'],['industryEligibleTitle','eligibleTitle'],['industryEligibleDesc','eligibleDesc'],['industryListTitle','industriesTitle'],['industryListDesc','industriesDesc'],['industryAssetsTitle','assetsTitle'],['industryAssetsDesc','assetsDesc'],['industryAuditTitle','auditTitle']];
  labels.forEach(([id,key]) => { const el = document.getElementById(id); if (el) el.textContent = imT(key); });
  const placeholders = [['industryCreateCode','codePlaceholder'],['industryCreateName','namePlaceholder']];
  placeholders.forEach(([id,key]) => { const el = document.getElementById(id); if (el) el.placeholder = imT(key); });
}

function imAssetLine(asset, controls = '') {
  return `<div class="item"><div class="head meta-spaced"><div><strong>${imEsc(asset.icon || '🤖')} ${imEsc(asset.name || asset.id)}</strong><div class="item-meta mono">${imEsc(asset.id || asset.listing_id || '')}</div></div><span class="badge ${String(asset.status || '').toLowerCase() === 'revoked' ? 'danger' : 'ok'}">${imEsc(imStatus(asset.status || 'ready'))}</span></div><div class="item-meta">${imEsc(asset.description || imT('noDescription'))}</div><div class="item-meta">${imEsc(imT('version'))}: ${imEsc(asset.version || '—')} · ${imEsc(imT('price'))}: ${imNumber(asset.price)}</div>${controls ? `<div class="actions section-gap">${controls}</div>` : ''}</div>`;
}
function renderIndustryManagement() {
  const eligible = document.getElementById('industryEligibleAssets'), industries = document.getElementById('industryManagementIndustries'), acquired = document.getElementById('industryAcquiredAssets');
  if (eligible) eligible.innerHTML = industryManagementState.assets.length ? industryManagementState.assets.map(item => imAssetLine(item, `<button class="btn-secondary" type="button" data-im-acquire="${imEsc(item.listing_id)}">${imEsc(imT('acquire'))}</button>`)).join('') : `<div class="hint">${imEsc(imT('noEligible'))}</div>`;
  if (acquired) acquired.innerHTML = industryManagementState.acquired.length ? industryManagementState.acquired.map(item => imAssetLine(item, String(item.status) === 'revoked' ? '' : `<button class="btn-danger" type="button" data-im-revoke="${imEsc(item.id)}">${imEsc(imT('revoke'))}</button>`)).join('') : `<div class="hint">${imEsc(imT('noAssets'))}</div>`;
  if (industries) industries.innerHTML = industryManagementState.industries.length ? industryManagementState.industries.map(industry => {
    const bound = industryManagementState.bindings.get(String(industry.id));
    const bindingReady = bound instanceof Set;
    const options = industryManagementState.acquired.filter(asset => String(asset.status) === 'ready').map(asset => `<label class="inline-check"><input type="checkbox" data-im-binding="${imEsc(asset.id)}"${bindingReady && bound.has(String(asset.id)) ? ' checked' : ''}> <span>${imEsc(asset.icon || '🤖')} ${imEsc(asset.name)} <small class="mono">${imEsc(asset.version)}</small></span></label>`).join('') || `<div class="hint">${imEsc(imT('noAssets'))}</div>`;
    const isGeneral = String(industry.code) === 'general';
    const systemNote = isGeneral ? `<div class="item-meta section-gap-sm">${imEsc(imT('generalDefaultNote'))}</div>` : '';
    const toggle = isGeneral ? '' : `<button class="btn-ghost" type="button" data-im-toggle-industry="${imEsc(industry.id)}" data-im-status="${imEsc(industry.status)}">${imEsc(String(industry.status) === 'active' ? imT('disable') : imT('enable'))}</button>`;
    return `<div class="item" data-im-industry="${imEsc(industry.id)}"><div class="head meta-spaced"><div><strong>${imEsc(industry.icon || '🏷️')} ${imEsc(industry.name)}</strong><div class="item-meta mono">${imEsc(industry.code)}</div></div><span class="badge ${String(industry.status) === 'active' ? 'ok' : 'warn'}">${imEsc(imStatus(industry.status))}</span></div><div class="item-meta">${imEsc(industry.description || imT('noDescription'))}</div>${systemNote}<details class="section-gap"><summary>${imEsc(imT('bindings'))}</summary><div class="stack-gap-sm section-gap" data-im-binding-list>${options}</div><div class="actions section-gap"><button class="btn-primary" type="button"${bindingReady ? '' : ' disabled'} data-im-save-bindings="${imEsc(industry.id)}">${imEsc(bindingReady ? imT('saveBindings') : imT('bindingsLoading'))}</button>${toggle}</div></details></div>`;
  }).join('') : `<div class="hint">${imEsc(imT('noIndustries'))}</div>`;
  const audit = document.getElementById('industryAuditEvents');
  if (audit) audit.innerHTML = industryManagementState.audit.length ? industryManagementState.audit.map(event => `<div class="data-row"><div class="data-row-main"><div>${imEsc(imAuditAction(event.action))}</div><div class="data-row-meta">${imEsc(imAuditTarget(event.target_type))}: ${imEsc(event.target_id || '—')}${event.reason ? ` · ${imEsc(event.reason)}` : ''}</div></div><div class="data-row-value"><small>${imEsc(event.actor_id || '')}</small>${imEsc(imFormatDate(event.created_at))}</div></div>`).join('') : `<div class="hint">${imEsc(imT('none'))}</div>`;
  industryManagementState.industries.filter(industry => !(industryManagementState.bindings.get(String(industry.id)) instanceof Set)).forEach(industry => void loadIndustryBindings(industry.id));
}

function imHubConfigContext() {
  const overlay = document.getElementById('hubConfigModalOverlay');
  const select = overlay?.querySelector('select[id^="hubTenantSelect-"]');
  const hubID = String(overlay?.querySelector('.hub-config-head .item-meta')?.textContent || '').trim();
  return { overlay, select, hubID, tenantID: String(select?.value || '').trim() };
}
async function renderTenantIndustrySettings() {
  const { overlay, select, hubID, tenantID } = imHubConfigContext();
  if (!overlay || !select || !hubID || !tenantID) return;
  const tenantPanel = select.closest('.hub-tenant-panel'); if (!tenantPanel) return;
  let root = tenantPanel.querySelector('[data-im-tenant-settings]');
  if (!root) { root = document.createElement('div'); root.dataset.imTenantSettings = 'true'; root.className = 'item section-gap-sm'; tenantPanel.appendChild(root); }
  root.innerHTML = `<div class="item-title">${imEsc(imT('tenantSettingsTitle'))}</div><div class="item-meta">${imEsc(imT('tenantSettingsLoadingDesc'))}</div><div class="hint section-gap">${imEsc(imT('loading'))}</div>`;
  try {
    const [industryData, assignment] = await Promise.all([
      imAPI('/api/admin/industry-management/industries'),
      imAPI(`/api/admin/hubs/${encodeURIComponent(hubID)}/tenants/${encodeURIComponent(tenantID)}/industries`),
    ]);
    // The modal may have been re-rendered while its requests were in flight.
    const live = imHubConfigContext(); if (live.overlay !== overlay || live.hubID !== hubID || live.tenantID !== tenantID || !document.contains(root)) return;
    const assigned = new Set((assignment?.industry_ids || []).map(String));
    // General is a system fallback, not a selectable tenant industry. It is
    // shown by the hint above when no explicit industries are configured.
    const items = (industryData?.industries || []).filter(item => String(item.status) === 'active' && String(item.code) !== 'general');
    const status = await imAPI(`/api/admin/hubs/${encodeURIComponent(hubID)}/tenants/${encodeURIComponent(tenantID)}/industry-expert-status`);
    const defaultHint = assignment?.using_default ? `<div class="hint section-gap-sm">${imEsc(imT('tenantDefaultHint'))}</div>` : '';
    root.innerHTML = `<div class="item-title">${imEsc(imT('tenantSettingsTitle'))}</div><div class="item-meta">${imEsc(imT('tenantSettingsDesc'))}</div>${defaultHint}<div class="item-meta section-gap-sm">${imEsc(imT('catalogueRevision'))}: ${imNumber(status?.revision)} · ${imEsc(imT('experts'))}: ${imNumber(status?.expert_count)}</div><div class="stack-gap-sm section-gap">${items.length ? items.map(item => `<label class="inline-check"><input type="checkbox" data-im-tenant-industry="${imEsc(item.id)}"${assigned.has(String(item.id)) ? ' checked' : ''}> <span>${imEsc(item.icon || '🏷️')} ${imEsc(item.name)}</span></label>`).join('') : `<div class="hint">${imEsc(imT('noIndustries'))}</div>`}</div><div class="actions section-gap"><button class="btn-primary" type="button" data-im-save-tenant-industries>${imEsc(imT('saveTenantSettings'))}</button></div>`;
  } catch (error) { root.innerHTML = `<div class="item-title">${imEsc(imT('tenantSettingsTitle'))}</div><div class="hint">${imEsc(imT('failed', { error: imError(error) }))}</div>`; }
}
async function saveTenantIndustrySettings(button) {
  const root = button.closest('[data-im-tenant-settings]'); const { hubID, tenantID } = imHubConfigContext();
  if (!root || !hubID || !tenantID) return;
  const industryIDs = Array.from(root.querySelectorAll('[data-im-tenant-industry]:checked')).map(box => String(box.dataset.imTenantIndustry || ''));
  button.disabled = true;
  try { const result = await imAPI(`/api/admin/hubs/${encodeURIComponent(hubID)}/tenants/${encodeURIComponent(tenantID)}/industries`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ industry_ids: industryIDs }) }); imSetStatus('success', imT('tenantSettingsSaved', { revision: imNumber(result?.revision) })); }
  catch (error) { imSetStatus('error', imT('failed', { error: imError(error) })); }
  finally { button.disabled = false; }
}
let imHubConfigTimer = null;
function watchTenantIndustrySettings() {
  const observer = new MutationObserver(() => { if (imHubConfigTimer) window.clearTimeout(imHubConfigTimer); imHubConfigTimer = window.setTimeout(() => { void renderTenantIndustrySettings(); }, 80); });
  observer.observe(document.body, { childList: true, subtree: true });
}

async function loadIndustryBindings(industryID) {
  const key = String(industryID);
  if (industryManagementState.bindings.get(key) instanceof Set) return;
  if (industryBindingLoads.has(key)) return industryBindingLoads.get(key);
  const card = document.querySelector(`[data-im-industry="${CSS.escape(String(industryID))}"]`); const root = card?.querySelector('[data-im-binding-list]'); const save = card?.querySelector('[data-im-save-bindings]'); if (!root || !save) return;
  const request = (async () => {
    try { const data = await imAPI(`/api/admin/industry-management/industries/${encodeURIComponent(industryID)}/bindings`); const bound = new Set((data?.bindings || []).map(item => String(item.asset_id))); industryManagementState.bindings.set(key, bound); if (!document.contains(root)) return; root.querySelectorAll('[data-im-binding]').forEach(box => { box.checked = bound.has(String(box.dataset.imBinding || '')); }); save.disabled = false; save.textContent = imT('saveBindings'); } catch (_) { if (document.contains(save)) { save.disabled = true; save.textContent = imT('bindingsLoading'); } imSetStatus('error', imT('bindingLoadFailed')); } finally { industryBindingLoads.delete(key); }
  })();
  industryBindingLoads.set(key, request);
  return request;
}

async function loadIndustryManagement(force = false) {
  if (!force && industryManagementLoading) return industryManagementLoading;
  imSetStatus('loading', imT('refresh'));
  industryManagementLoading = (async () => {
    try {
      const [industries, assetData, auditData] = await Promise.all([imAPI('/api/admin/industry-management/industries'), imAPI('/api/admin/industry-management/assets'), imAPI('/api/admin/industry-management/audit-events?limit=20')]);
      industryBindingLoads.clear();
      industryManagementState = { industries: Array.isArray(industries?.industries) ? industries.industries : [], assets: Array.isArray(assetData?.assets) ? assetData.assets : [], acquired: Array.isArray(assetData?.acquired_assets) ? assetData.acquired_assets : [], audit: Array.isArray(auditData?.events) ? auditData.events : [], bindings: new Map() };
      renderIndustryManagement(); imSetStatus();
    } catch (error) { imSetStatus('error', imT('failed', { error: imError(error) })); }
    finally { industryManagementLoading = null; }
  })();
  return industryManagementLoading;
}

async function createIndustryManagementIndustry() {
  const value = id => String(document.getElementById(id)?.value || '').trim();
  const body = { code: value('industryCreateCode'), name: value('industryCreateName'), icon: value('industryCreateIcon'), description: value('industryCreateDescription'), sort_order: Number(value('industryCreateSort') || 0) };
  if (!body.code || !body.name) { imSetStatus('error', imT('failed', { error: `${imT('code')} / ${imT('name')}` })); return; }
  try { await imAPI('/api/admin/industry-management/industries', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }); ['industryCreateCode','industryCreateName','industryCreateIcon','industryCreateDescription'].forEach(id => { const el = document.getElementById(id); if (el) el.value = ''; }); await loadIndustryManagement(true); imSetStatus('success', imT('created')); } catch (error) { imSetStatus('error', imT('failed', { error: imError(error) })); }
}
async function acquireIndustryAsset(listingID) { try { await imAPI('/api/admin/industry-management/assets/acquire', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ listing_id: listingID }) }); await loadIndustryManagement(true); imSetStatus('success', imT('acquired')); } catch (error) { imSetStatus('error', imT('failed', { error: imError(error) })); } }
async function saveIndustryBindings(industryID, card) { const assetIDs = Array.from(card.querySelectorAll('[data-im-binding]:checked')).map(box => box.dataset.imBinding); try { await imAPI(`/api/admin/industry-management/industries/${encodeURIComponent(industryID)}/bindings`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ asset_ids: assetIDs }) }); await loadIndustryManagement(true); imSetStatus('success', imT('saved')); } catch (error) { imSetStatus('error', imT('failed', { error: imError(error) })); } }
async function toggleIndustry(industryID, current) { try { await imAPI(`/api/admin/industry-management/industries/${encodeURIComponent(industryID)}`, { method: 'PATCH', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ status: String(current) === 'active' ? 'disabled' : 'active' }) }); await loadIndustryManagement(true); } catch (error) { imSetStatus('error', imT('failed', { error: imError(error) })); } }
async function revokeIndustryAsset(assetID) { if (!window.confirm(imT('revokeConfirm'))) return; const reason = window.prompt(imT('reasonPrompt')); if (!String(reason || '').trim()) return; try { await imAPI(`/api/admin/industry-management/assets/${encodeURIComponent(assetID)}/revoke`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ reason: String(reason).trim() }) }); await loadIndustryManagement(true); imSetStatus('success', imT('revoked')); } catch (error) { imSetStatus('error', imT('failed', { error: imError(error) })); } }

document.addEventListener('DOMContentLoaded', () => {
  ensureIndustryManagementNav(); applyIndustryManagementI18n();
  const panel = document.getElementById('tab-industrymanagement'); if (panel && panel.dataset.imBound !== 'true') { panel.dataset.imBound = 'true'; panel.addEventListener('click', event => { const target = event.target.closest('button'); if (!target) return; if (target.dataset.imAcquire) void acquireIndustryAsset(target.dataset.imAcquire); if (target.dataset.imSaveBindings) void saveIndustryBindings(target.dataset.imSaveBindings, target.closest('[data-im-industry]')); if (target.dataset.imToggleIndustry) void toggleIndustry(target.dataset.imToggleIndustry, target.dataset.imStatus); if (target.dataset.imRevoke) void revokeIndustryAsset(target.dataset.imRevoke); }); }
  document.addEventListener('click', event => { const target = event.target.closest('[data-im-save-tenant-industries]'); if (target) void saveTenantIndustrySettings(target); });
  watchTenantIndustrySettings();
  if (panel?.classList.contains('active')) void loadIndustryManagement();
});
window.loadIndustryManagement = loadIndustryManagement;
window.createIndustryManagementIndustry = createIndustryManagementIndustry;
window.applyIndustryManagementI18n = () => { applyIndustryManagementI18n(); renderIndustryManagement(); void renderTenantIndustrySettings(); };

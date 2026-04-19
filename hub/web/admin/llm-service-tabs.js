const LLM_SERVICE_I18N = {
  en: {
    adminTitle: 'Model Service Groups',
    adminDesc: 'Authorize models by security group, user, or redeem card. Public API and exposed model names are shown above.',
    groups: 'Service Groups',
    bindings: 'Security Group Bindings',
    users: 'User Bindings',
    cards: 'Redeem Cards',
    grants: 'Active Grants',
    addGroup: 'Add / Update Group',
    saveAll: 'Save Service Config',
    issueCard: 'Issue Card',
    systemDefaults: 'New User Defaults',
    newUserGroups: 'Default Service Groups',
    newUserDays: 'Validity (Days)',
    saveDefaults: 'Save Defaults', tokensPerCredit: 'Tokens per Credit', credits: 'Credits', diagnoseTitle: 'Entitlement Diagnostic', diagnoseDesc: 'Explain why a user can or cannot access model services.', diagnoseEmail: 'Email', diagnoseBtn: 'Diagnose', diagnoseEmpty: 'Enter an email to inspect effective entitlements.', diagnoseLoadFailed: 'Load entitlement diagnostic failed: {error}',
    loadFailed: 'Load model services failed: {error}',
    saveDone: 'Model service configuration saved.',
    saveFailed: 'Save model services failed: {error}',
    issueDone: 'Redeem card created: {code}',
    issueFailed: 'Create redeem card failed: {error}'
  },
  zh: {
    adminTitle: '\u6a21\u578b\u670d\u52a1\u7ec4',
    adminDesc: '\u901a\u8fc7\u5b89\u5168\u7ec4\u3001\u7528\u6237\u6216\u5145\u503c\u5361\u6388\u6743\u6a21\u578b\u670d\u52a1\u3002\u5bf9\u5916 API \u5730\u5740\u4e0e\u66b4\u9732\u6a21\u578b\u540d\u79f0\u5df2\u5728\u4e0a\u65b9\u663e\u793a\u3002',
    groups: '\u670d\u52a1\u7ec4',
    bindings: '\u5b89\u5168\u7ec4\u6388\u6743',
    users: '\u7528\u6237\u76f4\u6388\u6743',
    cards: '\u5145\u503c\u5361',
    grants: '\u751f\u6548\u4e2d\u6388\u6743',
    addGroup: '\u65b0\u589e / \u66f4\u65b0\u670d\u52a1\u7ec4',
    saveAll: '\u4fdd\u5b58\u670d\u52a1\u914d\u7f6e',
    issueCard: '\u53d1\u884c\u5145\u503c\u5361',
    systemDefaults: '\u65b0\u7528\u6237\u9ed8\u8ba4\u6388\u6743',
    newUserGroups: '\u9ed8\u8ba4\u670d\u52a1\u7ec4',
    newUserDays: '\u6709\u6548\u671f(\u5929)',
    saveDefaults: '\u4fdd\u5b58\u9ed8\u8ba4\u6388\u6743', tokensPerCredit: '\u6bcf credit \u5bf9\u5e94 token \u6570', credits: 'Credits', diagnoseTitle: '\u6743\u9650\u8bca\u65ad', diagnoseDesc: '\u89e3\u91ca\u67d0\u4e2a\u7528\u6237\u4e3a\u4ec0\u4e48\u80fd\u6216\u4e0d\u80fd\u4f7f\u7528\u6a21\u578b\u670d\u52a1\u3002', diagnoseEmail: '\u90ae\u7bb1', diagnoseBtn: '\u5f00\u59cb\u8bca\u65ad', diagnoseEmpty: '\u8f93\u5165\u90ae\u7bb1\u540e\u53ef\u67e5\u770b\u6700\u7ec8\u751f\u6548\u6743\u9650\u3002', diagnoseLoadFailed: '\u52a0\u8f7d\u6743\u9650\u8bca\u65ad\u5931\u8d25: {error}',
    loadFailed: '\u52a0\u8f7d\u6a21\u578b\u670d\u52a1\u5931\u8d25: {error}',
    saveDone: '\u6a21\u578b\u670d\u52a1\u914d\u7f6e\u5df2\u4fdd\u5b58\u3002',
    saveFailed: '\u4fdd\u5b58\u6a21\u578b\u670d\u52a1\u5931\u8d25: {error}',
    issueDone: '\u5145\u503c\u5361\u5df2\u521b\u5efa: {code}',
    issueFailed: '\u521b\u5efa\u5145\u503c\u5361\u5931\u8d25: {error}'
  }
};
const lsx = (key, vars = {}) => ((LLM_SERVICE_I18N[currentLang] || LLM_SERVICE_I18N.en)[key] || LLM_SERVICE_I18N.en[key] || key).replace(/\{(\w+)\}/g, (_, name) => vars[name] ?? '');
let llmServiceAdminCache = null;
let llmServiceSelectedGroupID = '';
const BUILTIN_DEFAULT_LLM_SERVICE_GROUP_ID = 'default';
function isBuiltinLLMServiceGroup(id) { return String(id || '').trim().toLowerCase() === BUILTIN_DEFAULT_LLM_SERVICE_GROUP_ID; }
function parseCSV(value) { return String(value || '').split(',').map(function(v) { return v.trim(); }).filter(Boolean); }
function parseModelDefs(value) { return String(value || '').split(/\r?\n/).map(function(line) { return line.trim(); }).filter(Boolean).map(function(line) { const segments = line.split(';').map(function(part) { return part.trim(); }).filter(Boolean); const main = segments.shift() || ''; const parts = main.split('='); const name = (parts.shift() || '').trim(); const providers = parts.join('=').split(',').map(function(v) { return v.trim(); }).filter(Boolean); const item = { name: name, provider_ids: providers, capability_tags: [], priority: 0, resolution_tier: 0, credit_multiplier: 1 }; segments.forEach(function(segment) { const kv = segment.split('='); const key = (kv.shift() || '').trim().toLowerCase(); const raw = kv.join('=').trim(); if (!key || !raw) return; if (key === 'features' || key === 'capabilities') item.capability_tags = raw.split(',').map(function(v) { return v.trim(); }).filter(Boolean); else if (key === 'priority') item.priority = Number(raw) || 0; else if (key === 'resolution' || key === 'resolution_tier') item.resolution_tier = Number(raw) || 0; else if (key === 'multiplier' || key === 'credit_multiplier') item.credit_multiplier = Number(raw) || 1; }); return item; }).filter(function(item) { return item.name && item.provider_ids.length; }); }
function modelDefsText(models) { return (models || []).map(function(m) { const parts = [(m.name || '') + '=' + ((m.provider_ids || []).join(','))]; if (m.capability_tags && m.capability_tags.length) parts.push('features=' + m.capability_tags.join(',')); if (m.priority) parts.push('priority=' + String(m.priority)); if (m.resolution_tier) parts.push('resolution=' + String(m.resolution_tier)); if (m.credit_multiplier && Number(m.credit_multiplier) !== 1) parts.push('multiplier=' + String(m.credit_multiplier)); return parts.join('; '); }).join('\n'); }
function aui() { return window.AdminUI; }
function ensureLLMServiceAdminUI() {
  if (document.getElementById('llmServiceAdminRoot')) return;
  const tab = document.getElementById('tab-modelservices');
  if (!tab) return;
  const host = document.createElement('div');
  host.id = 'llmServiceAdminRoot';
  host.className = 'grid2';
  host.style.marginTop = '16px';
  host.innerHTML = '' +
    '<div class="item"><div class="item-head"><div><div class="item-title" id="llmServiceAdminTitle"></div><div class="item-meta" id="llmServiceAdminDesc"></div></div><div class="actions"><button class="btn-secondary" onclick="saveLLMServiceAdmin()" id="llmServiceSaveBtn"></button></div></div>' +
    '<div class="grid2" style="margin-top:12px">' +
    '<div><label>API Base URL</label><div id="llmServiceExposeApiBase" class="mono" style="padding:10px 12px;border:1px solid var(--line);border-radius:12px;min-height:42px">-</div></div>' +
    '<div><label>Chat Completions URL</label><div id="llmServiceExposeChatUrl" class="mono" style="padding:10px 12px;border:1px solid var(--line);border-radius:12px;min-height:42px">-</div></div>' +
    '<div><label>Models URL</label><div id="llmServiceExposeModelsUrl" class="mono" style="padding:10px 12px;border:1px solid var(--line);border-radius:12px;min-height:42px">-</div></div>' +
    '<div><label>Available Models</label><div id="llmServiceExposeModels" class="mono" style="padding:10px 12px;border:1px solid var(--line);border-radius:12px;min-height:42px">-</div></div>' +
    '</div><div class="grid2">' +
    '<div><label>ID</label><input id="llmServiceGroupID" placeholder="coding-basic"></div>' +
    '<div><label>Name</label><input id="llmServiceGroupName" placeholder="Coding Basic"></div>' +
    '<div style="grid-column:1 / -1"><label>Description</label><input id="llmServiceGroupDesc" placeholder="Exposed models for basic coding"></div>' +
    '<div style="grid-column:1 / -1"><label>Models</label><textarea id="llmServiceGroupModels" style="width:100%;min-height:100px;padding:10px;border-radius:12px;border:1px solid var(--line);font:inherit;resize:vertical" placeholder="auto=provider-a,provider-b; features=reasoning,tools; priority=50; resolution=1; multiplier=1&#10;doc=provider-c; features=document; priority=80; resolution=1; multiplier=1.2"></textarea><div class="hint">One line per exposed model. Format: <span class="mono">model=provider1,provider2; features=document,reasoning,tools; priority=50; resolution=1; multiplier=1.2</span>. Provider order is failover priority. Auto scheduling prefers the highest matched capability score, then the lowest resolution tier, then the lowest multiplier.</div></div>' +
    '</div><div class="actions" style="margin-top:12px"><button class="btn-primary" onclick="upsertLLMServiceGroup()" id="llmServiceAddGroupBtn"></button><button class="btn-danger" onclick="removeSelectedLLMServiceGroup()" id="llmServiceRemoveGroupBtn">Remove Group</button></div><div id="llmServiceGroupsList" style="margin-top:14px"></div></div>' +
    '<div class="item"><div class="item-head"><div><div class="item-title" id="llmServiceBindingsTitle"></div><div class="item-meta">Reuse the security groups already created in Security.</div></div></div>' +
    '<div class="grid2"><div><label>Security Group ID</label><input id="llmServiceBindingGroupID" placeholder="engineering"></div><div><label>Service Groups</label><input id="llmServiceBindingServiceGroups" placeholder="coding-basic,coding-pro"></div></div><div class="actions"><button class="btn-secondary" onclick="addLLMServiceGroupBinding()">Add Group Binding</button></div><div id="llmServiceGroupBindingsList"></div>' +
    '<div style="margin-top:16px" class="item-title" id="llmServiceUsersTitle"></div><div class="grid2"><div><label>Email</label><input id="llmServiceUserEmail" placeholder="user@example.com"></div><div><label>Service Groups</label><input id="llmServiceUserServiceGroups" placeholder="coding-pro"></div></div><div class="actions"><button class="btn-secondary" onclick="addLLMServiceUserBinding()">Add User Binding</button></div><div id="llmServiceUserBindingsList"></div>' +
    '<div style="margin-top:16px" class="item-title" id="llmServiceCardsTitle"></div><div class="grid2"><div><label>Label</label><input id="llmServiceCardLabel" placeholder="April campaign"></div><div><label>Service Groups</label><input id="llmServiceCardGroups" placeholder="coding-basic"></div></div><div class="grid2" style="margin-top:12px"><div><label>Days</label><input id="llmServiceCardDays" type="number" min="1" value="30"></div><div><label id="llmServiceCardCreditsLabel">Credits</label><input id="llmServiceCardCredits" type="number" min="0" step="1" value="1000"></div></div><div class="actions"><button class="btn-primary" onclick="issueLLMServiceCard()" id="llmServiceIssueBtn"></button></div><div id="llmServiceCardsList"></div>' +
    '<div style="margin-top:16px" class="item-title" id="llmServiceGrantsTitle"></div><div id="llmServiceGrantsList"></div>' +
    '<div style="margin-top:16px" class="item-title" id="llmServiceDiagnoseTitle"></div><div class="item-meta" id="llmServiceDiagnoseDesc" style="margin-bottom:10px"></div><div class="grid2"><div><label id="llmServiceDiagnoseEmailLabel"></label><input id="llmServiceDiagnoseEmail" placeholder="user@example.com"></div><div style="display:flex;align-items:flex-end"><button class="btn-secondary" onclick="diagnoseLLMServiceUser()" id="llmServiceDiagnoseBtn"></button></div></div><div id="llmServiceDiagnoseResult" style="margin-top:12px"></div></div>';
  tab.appendChild(host);
  applyLLMServiceI18n();
}
function applyLLMServiceI18n() {
  _s('llmServiceAdminTitle', 'textContent', lsx('adminTitle'));
  _s('llmServiceAdminDesc', 'textContent', lsx('adminDesc'));
  _s('llmServiceBindingsTitle', 'textContent', lsx('bindings'));
  _s('llmServiceUsersTitle', 'textContent', lsx('users'));
  _s('llmServiceCardsTitle', 'textContent', lsx('cards'));
  _s('llmServiceGrantsTitle', 'textContent', lsx('grants'));
  _s('llmServiceAddGroupBtn', 'textContent', lsx('addGroup'));
  _s('llmServiceSaveBtn', 'textContent', lsx('saveAll'));
  _s('llmServiceIssueBtn', 'textContent', lsx('issueCard'));
  _s('llmServiceCardCreditsLabel', 'textContent', lsx('credits'));
  _s('llmServiceDiagnoseTitle', 'textContent', lsx('diagnoseTitle'));
  _s('llmServiceDiagnoseDesc', 'textContent', lsx('diagnoseDesc'));
  _s('llmServiceDiagnoseEmailLabel', 'textContent', lsx('diagnoseEmail'));
  _s('llmServiceDiagnoseBtn', 'textContent', lsx('diagnoseBtn'));
}
async function loadLLMServiceAdmin() {
  try {
    const data = await api('/api/admin/llm/services');
    llmServiceAdminCache = data || { model_service_groups: [], group_bindings: [], user_bindings: [], cards: [], grants: [] };
    if (llmServiceSelectedGroupID && !(llmServiceAdminCache.model_service_groups || []).some(function(g) { return g.id === llmServiceSelectedGroupID; })) llmServiceSelectedGroupID = '';
    renderLLMServiceAdmin();
  } catch (err) {
    const msg = lsx('loadFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
  }
}
function renderLLMServiceAdmin() {
  if (!llmServiceAdminCache) return;
  applyLLMServiceI18n();
  const groups = llmServiceAdminCache.model_service_groups || [];
  _s('llmServiceExposeApiBase', 'textContent', llmServiceAdminCache.expose_api_base_url || '-');
  _s('llmServiceExposeChatUrl', 'textContent', llmServiceAdminCache.expose_base_url || '-');
  _s('llmServiceExposeModelsUrl', 'textContent', llmServiceAdminCache.expose_models_url || '-');
  _s('llmServiceExposeModels', 'textContent', (llmServiceAdminCache.available_models || []).length ? llmServiceAdminCache.available_models.join(', ') : '-');
  const selected = groups.find(function(g) { return g.id === llmServiceSelectedGroupID; }) || null;  if (selected) {
    const builtin = isBuiltinLLMServiceGroup(selected.id);
    llmServiceSelectedGroupID = selected.id;
    _s('llmServiceGroupID', 'value', selected.id || '');
    _s('llmServiceGroupName', 'value', selected.name || '');
    _s('llmServiceGroupDesc', 'value', selected.description || '');
    _s('llmServiceGroupModels', 'value', modelDefsText(selected.models || []));
    const idEl = document.getElementById('llmServiceGroupID');
    const nameEl = document.getElementById('llmServiceGroupName');
    const descEl = document.getElementById('llmServiceGroupDesc');
    const modelsEl = document.getElementById('llmServiceGroupModels');
    const addBtn = document.getElementById('llmServiceAddGroupBtn');
    const removeBtn = document.getElementById('llmServiceRemoveGroupBtn');
    if (idEl) idEl.disabled = builtin;
    if (nameEl) nameEl.disabled = builtin;
    if (descEl) descEl.disabled = builtin;
    if (modelsEl) modelsEl.disabled = builtin;
    if (addBtn) addBtn.textContent = builtin ? 'Built-in Default (No Model Access)' : lsx('addGroup');
    if (removeBtn) { removeBtn.disabled = builtin; removeBtn.textContent = builtin ? 'Built-in Default' : 'Remove Group'; }
  } else {
    _s('llmServiceGroupID', 'value', '');
    _s('llmServiceGroupName', 'value', '');
    _s('llmServiceGroupDesc', 'value', '');
    _s('llmServiceGroupModels', 'value', '');
    const idEl = document.getElementById('llmServiceGroupID');
    const nameEl = document.getElementById('llmServiceGroupName');
    const descEl = document.getElementById('llmServiceGroupDesc');
    const modelsEl = document.getElementById('llmServiceGroupModels');
    const addBtn = document.getElementById('llmServiceAddGroupBtn');
    const removeBtn = document.getElementById('llmServiceRemoveGroupBtn');
    if (idEl) idEl.disabled = false;
    if (nameEl) nameEl.disabled = false;
    if (descEl) descEl.disabled = false;
    if (modelsEl) modelsEl.disabled = false;
    if (addBtn) addBtn.textContent = lsx('addGroup');
    if (removeBtn) { removeBtn.disabled = true; removeBtn.textContent = currentLang === 'zh' ? '\u5220\u9664\u670d\u52a1\u7ec4' : 'Remove Group'; }
  }
  const ui = aui();
  const groupsRoot = document.getElementById('llmServiceGroupsList');
  if (groupsRoot) groupsRoot.innerHTML = ui.renderList(groups, function(g) {
    const active = g.id === llmServiceSelectedGroupID;
    return ui.simpleCard({
      title: g.name || g.id,
      titleMeta: g.id || '',
      titleMetaClass: 'mono',
      headRight: ui.badge(String((g.models || []).length) + ' models', 'info'),
      style: 'margin-top:10px;border:' + (active ? '1px solid rgba(47,128,237,.36)' : '1px solid var(--line)') + ';cursor:pointer',
      attrs: { onclick: 'selectLLMServiceGroup(\'' + String(g.id).replace(/'/g, "\\'") + '\')' },
      body: [ui.meta(g.description || ''), ui.meta(modelDefsText(g.models || []), 'mono', 'margin-top:8px')]
    });
  }, 'No service groups yet.');
  const gbRoot = document.getElementById('llmServiceGroupBindingsList');
  if (gbRoot) gbRoot.innerHTML = ui.renderList(llmServiceAdminCache.group_bindings || [], function(b, idx) {
    return ui.simpleCard({
      title: b.group_id || '',
      titleMeta: (b.service_group_ids || []).join(', '),
      titleMetaClass: 'mono',
      style: 'margin-top:10px',
      headRight: ui.actionButton('Remove', 'btn-danger', 'removeLLMServiceGroupBinding(' + idx + ')')
    });
  }, 'No security-group bindings yet.');
  const ubRoot = document.getElementById('llmServiceUserBindingsList');
  if (ubRoot) ubRoot.innerHTML = ui.renderList(llmServiceAdminCache.user_bindings || [], function(b, idx) {
    return ui.simpleCard({
      title: b.email || '',
      titleMeta: (b.service_group_ids || []).join(', '),
      titleMetaClass: 'mono',
      style: 'margin-top:10px',
      headRight: ui.actionButton('Remove', 'btn-danger', 'removeLLMServiceUserBinding(' + idx + ')')
    });
  }, 'No direct user bindings yet.');
  const cardsRoot = document.getElementById('llmServiceCardsList');
  if (cardsRoot) cardsRoot.innerHTML = ui.renderList(llmServiceAdminCache.cards || [], function(c) {
    return ui.simpleCard({
      title: c.label || c.id || 'card',
      titleMeta: (c.service_group_ids || []).join(', ') + ' | ' + String(c.duration_days || 0) + ' days | ' + String(c.credits || 0) + ' credits',
      titleMetaClass: 'mono',
      style: 'margin-top:10px',
      headRight: ui.badge(c.redeemed_at ? 'redeemed' : 'unused', c.redeemed_at ? 'warn' : 'ok'),
      body: ui.meta((c.redeemed_by_email || '') + (c.redeemed_at ? (' | ' + String(c.redeemed_at)) : ''))
    });
  }, 'No redeem cards issued yet.');
  const grantsRoot = document.getElementById('llmServiceGrantsList');
  if (grantsRoot) grantsRoot.innerHTML = ui.renderList(llmServiceAdminCache.grants || [], function(g) {
    const total = Number(g.credits_total || 0);
    const used = Number(g.credits_used || 0);
    const remaining = total > 0 ? Math.max(0, total - used) : 0;
    const creditsText = total > 0 ? (' | credits ' + remaining.toFixed(3).replace(/\.000$/, '') + '/' + total.toFixed(3).replace(/\.000$/, '')) : '';
    return ui.simpleCard({
      title: g.email || '',
      titleMeta: (g.service_group_id || '') + ' | ' + (g.source || '') + creditsText,
      titleMetaClass: 'mono',
      style: 'margin-top:10px',
      headRight: ui.badge(String(g.expires_at || ''), 'info')
    });
  }, 'No active grants yet.');
  const diagnoseRoot = document.getElementById('llmServiceDiagnoseResult');
  if (diagnoseRoot) {
    const diag = llmServiceAdminCache.user_diagnostic || null;
    if (!diag || !diag.email) {
      diagnoseRoot.innerHTML = ui.hint(lsx('diagnoseEmpty'));
    } else {
      const status = diag.service_status || {};
      const securityGroups = (diag.resolved_security_group_ids || []).join(', ') || '-';
      const effectiveGroups = (status.service_group_ids || []).join(', ') || '-';
      const models = (status.available_models || []).join(', ') || '-';
      const userBindings = (diag.direct_user_bindings || []).length ? diag.direct_user_bindings.map(function(b) { return ui.meta((b.service_group_ids || []).join(', '), 'mono'); }).join('') : ui.hint('-');
      const groupBindings = (diag.matched_group_bindings || []).length ? diag.matched_group_bindings.map(function(b) { return ui.meta((b.group_id || '') + ' ' + ((b.service_group_ids || []).join(', '))); }).join('') : ui.hint('-');
      const grants = (diag.active_grants || []).length ? diag.active_grants.map(function(g) { return ui.meta((g.service_group_id || '') + ' | ' + (g.source || '') + ' | ' + String(g.expires_at || '')); }).join('') : ui.hint('-');
      diagnoseRoot.innerHTML = '<div class="item"><div class="item-head"><div><div class="item-title">' + escapeHtml(diag.email || '') + '</div><div class="item-meta">' + (status.active ? 'active' : 'inactive') + ' | default model: <span class="mono">' + escapeHtml(status.default_model || 'auto') + '</span></div></div></div><div class="grid2" style="margin-top:12px"><div><label>Security Groups</label><div class="mono">' + escapeHtml(securityGroups) + '</div></div><div><label>Effective Service Groups</label><div class="mono">' + escapeHtml(effectiveGroups) + '</div></div><div><label>Available Models</label><div class="mono">' + escapeHtml(models) + '</div></div><div><label>Credits</label><div class="mono">' + escapeHtml(String(status.credits_available || 0)) + '</div></div></div><div style="margin-top:12px"><label>Direct User Bindings</label>' + userBindings + '</div><div style="margin-top:12px"><label>Matched Group Bindings</label>' + groupBindings + '</div><div style="margin-top:12px"><label>Active Grants</label>' + grants + '</div></div>';
    }
  }
}
function selectLLMServiceGroup(id) { llmServiceSelectedGroupID = id; renderLLMServiceAdmin(); }
function upsertLLMServiceGroup() {
  if (!llmServiceAdminCache) llmServiceAdminCache = { model_service_groups: [], group_bindings: [], user_bindings: [], cards: [], grants: [] };
  const next = { id: (document.getElementById('llmServiceGroupID').value || '').trim(), name: (document.getElementById('llmServiceGroupName').value || '').trim(), description: (document.getElementById('llmServiceGroupDesc').value || '').trim(), models: parseModelDefs(document.getElementById('llmServiceGroupModels').value || '') };
  if (!next.id || !next.name) { showToast('Service group id and name are required.', 'error'); return false; }
  if (isBuiltinLLMServiceGroup(next.id) || isBuiltinLLMServiceGroup(llmServiceSelectedGroupID)) { showToast('The built-in Default group is read-only.', 'info'); return false; }
  const idx = (llmServiceAdminCache.model_service_groups || []).findIndex(function(g) { return g.id === next.id; });
  if (idx >= 0) llmServiceAdminCache.model_service_groups[idx] = next; else llmServiceAdminCache.model_service_groups.push(next);
  llmServiceSelectedGroupID = next.id;
  renderLLMServiceAdmin();
  return true;
}
function removeSelectedLLMServiceGroup() {
  if (!llmServiceAdminCache || !llmServiceSelectedGroupID) return;
  if (isBuiltinLLMServiceGroup(llmServiceSelectedGroupID)) { showToast('The built-in Default group cannot be removed.', 'info'); return; }
  llmServiceAdminCache.model_service_groups = (llmServiceAdminCache.model_service_groups || []).filter(function(g) { return g.id !== llmServiceSelectedGroupID; });
  llmServiceAdminCache.group_bindings = (llmServiceAdminCache.group_bindings || []).map(function(b) { b.service_group_ids = (b.service_group_ids || []).filter(function(id) { return id !== llmServiceSelectedGroupID; }); return b; }).filter(function(b) { return (b.service_group_ids || []).length; });
  llmServiceAdminCache.user_bindings = (llmServiceAdminCache.user_bindings || []).map(function(b) { b.service_group_ids = (b.service_group_ids || []).filter(function(id) { return id !== llmServiceSelectedGroupID; }); return b; }).filter(function(b) { return (b.service_group_ids || []).length; });
  llmServiceSelectedGroupID = llmServiceAdminCache.model_service_groups[0] && llmServiceAdminCache.model_service_groups[0].id || '';
  renderLLMServiceAdmin();
}
function addLLMServiceGroupBinding() {
  if (!llmServiceAdminCache) return;
  const groupID = (document.getElementById('llmServiceBindingGroupID').value || '').trim();
  const serviceGroupIDs = parseCSV(document.getElementById('llmServiceBindingServiceGroups').value || '');
  if (!groupID || !serviceGroupIDs.length) return;
  llmServiceAdminCache.group_bindings = llmServiceAdminCache.group_bindings || [];
  llmServiceAdminCache.group_bindings.push({ group_id: groupID, service_group_ids: serviceGroupIDs });
  document.getElementById('llmServiceBindingGroupID').value = '';
  document.getElementById('llmServiceBindingServiceGroups').value = '';
  renderLLMServiceAdmin();
}
function removeLLMServiceGroupBinding(idx) { if (!llmServiceAdminCache) return; llmServiceAdminCache.group_bindings.splice(idx, 1); renderLLMServiceAdmin(); }
function addLLMServiceUserBinding() {
  if (!llmServiceAdminCache) return;
  const email = (document.getElementById('llmServiceUserEmail').value || '').trim();
  const serviceGroupIDs = parseCSV(document.getElementById('llmServiceUserServiceGroups').value || '');
  if (!email || !serviceGroupIDs.length) return;
  llmServiceAdminCache.user_bindings = llmServiceAdminCache.user_bindings || [];
  llmServiceAdminCache.user_bindings.push({ email: email, service_group_ids: serviceGroupIDs });
  document.getElementById('llmServiceUserEmail').value = '';
  document.getElementById('llmServiceUserServiceGroups').value = '';
  renderLLMServiceAdmin();
}
function removeLLMServiceUserBinding(idx) { if (!llmServiceAdminCache) return; llmServiceAdminCache.user_bindings.splice(idx, 1); renderLLMServiceAdmin(); }
async function saveLLMServiceAdmin() {
  if (!llmServiceAdminCache) return;
  const draftId = (document.getElementById('llmServiceGroupID') && document.getElementById('llmServiceGroupID').value || '').trim();
  const draftName = (document.getElementById('llmServiceGroupName') && document.getElementById('llmServiceGroupName').value || '').trim();
  if (draftId || draftName) {
    if (!upsertLLMServiceGroup()) return;
    if (!llmServiceAdminCache) return;
  }
  try {
    const data = await api('/api/admin/llm/services', { method: 'PUT', body: JSON.stringify(llmServiceAdminCache) });
    llmServiceAdminCache = data || llmServiceAdminCache;
    renderLLMServiceAdmin();
    setOutput(lsx('saveDone'));
    showToast(lsx('saveDone'), 'success');
  } catch (err) {
    const msg = lsx('saveFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
  }
}
async function diagnoseLLMServiceUser() {
  const email = (document.getElementById('llmServiceDiagnoseEmail').value || '').trim();
  if (!email) {
    showToast(lsx('diagnoseEmpty'), 'info');
    return;
  }
  try {
    const data = await api('/api/admin/llm/services/diagnose?email=' + encodeURIComponent(email));
    if (!llmServiceAdminCache) llmServiceAdminCache = { model_service_groups: [], group_bindings: [], user_bindings: [], cards: [], grants: [] };
    llmServiceAdminCache.user_diagnostic = data || null;
    renderLLMServiceAdmin();
  } catch (err) {
    const msg = lsx('diagnoseLoadFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
  }
}
async function issueLLMServiceCard() {
  try {
    const data = await api('/api/admin/llm/service-cards', { method: 'POST', body: JSON.stringify({ label: (document.getElementById('llmServiceCardLabel').value || '').trim(), service_group_ids: parseCSV(document.getElementById('llmServiceCardGroups').value || ''), duration_days: Number(document.getElementById('llmServiceCardDays').value || 30), credits: Number(document.getElementById('llmServiceCardCredits').value || 0) }) });
    const msg = lsx('issueDone', { code: data.card && data.card.code || '' });
    setOutput(msg);
    showToast(msg, 'success');
    await loadLLMServiceAdmin();
  } catch (err) {
    const msg = lsx('issueFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
  }
}
const baseLLMProvidersSetLanguage = typeof setLanguage === 'function' ? setLanguage : null;
if (baseLLMProvidersSetLanguage) {
  setLanguage = function(lang) {
    baseLLMProvidersSetLanguage(lang);
    applyLLMProvidersI18n();
    applyLLMServiceI18n();
  };
}
ensureLLMServiceAdminUI();

function ensureLLMServiceSystemUI() {
  if (document.getElementById('llmServiceSystemRoot')) return;
  const tab = document.getElementById('tab-system');
  if (!tab) return;
  const host = document.createElement('div');
  host.id = 'llmServiceSystemRoot';
  host.className = 'item';
  host.style.marginTop = '18px';
  host.innerHTML = '' +
    '<div class="head" style="margin-bottom:12px"><div><div class="item-title" id="llmServiceSystemTitle"></div><div class="item-meta">Grant model-service groups automatically when a new email user is created.</div></div><div class="actions"><button class="btn-primary" onclick="saveLLMServiceSystemSettings()" id="llmServiceSystemSaveBtn"></button></div></div>' +
    '<div class="grid2">' +
    '<div><label id="llmServiceSystemGroupsLabel"></label><input id="llmServiceSystemGroups" placeholder="default"></div>' +
    '<div><label id="llmServiceSystemDaysLabel"></label><input id="llmServiceSystemDays" type="number" min="1" value="30"></div>' +
    '</div><div class="grid2" style="margin-top:12px"><div><label id="llmServiceSystemTokensLabel"></label><input id="llmServiceSystemTokensPerCredit" type="number" min="1" value="10000"></div><div></div></div><div class="hint" style="margin-top:12px">Use service-group IDs. The built-in <span class="mono">default</span> group is Default (No Model Access) and is used as the fallback for new users.</div>'; 
  tab.appendChild(host);
  applyLLMServiceSystemI18n();
}
function applyLLMServiceSystemI18n() {
  _s('llmServiceSystemTitle', 'textContent', lsx('systemDefaults'));
  _s('llmServiceSystemGroupsLabel', 'textContent', lsx('newUserGroups'));
  _s('llmServiceSystemDaysLabel', 'textContent', lsx('newUserDays'));
  _s('llmServiceSystemSaveBtn', 'textContent', lsx('saveDefaults'));
  _s('llmServiceSystemTokensLabel', 'textContent', lsx('tokensPerCredit')); 
}
function renderLLMServiceSystemSettings() {
  applyLLMServiceSystemI18n();
  if (!llmServiceAdminCache) return;
  _s('llmServiceSystemGroups', 'value', (llmServiceAdminCache.default_new_user_service_groups || []).join(', '));
  _s('llmServiceSystemDays', 'value', String(llmServiceAdminCache.default_new_user_duration_days || 30));
  _s('llmServiceSystemTokensPerCredit', 'value', String(llmServiceAdminCache.tokens_per_credit || 10000));
}
async function saveLLMServiceSystemSettings() {
  ensureLLMServiceSystemUI();
  if (!llmServiceAdminCache) await loadLLMServiceAdmin();
  llmServiceAdminCache.default_new_user_service_groups = parseCSV(document.getElementById('llmServiceSystemGroups').value || '') || [];
  if (!llmServiceAdminCache.default_new_user_service_groups.length) llmServiceAdminCache.default_new_user_service_groups = ['default'];
  llmServiceAdminCache.default_new_user_duration_days = Number(document.getElementById('llmServiceSystemDays').value || 30);
  if (!(llmServiceAdminCache.default_new_user_duration_days > 0)) llmServiceAdminCache.default_new_user_duration_days = 30;
  llmServiceAdminCache.tokens_per_credit = Number(document.getElementById('llmServiceSystemTokensPerCredit').value || 10000);
  if (!(llmServiceAdminCache.tokens_per_credit > 0)) llmServiceAdminCache.tokens_per_credit = 10000;
  try {
    const data = await api('/api/admin/llm/services', { method: 'PUT', body: JSON.stringify(llmServiceAdminCache) });
    llmServiceAdminCache = data || llmServiceAdminCache;
    renderLLMServiceAdmin();
    renderLLMServiceSystemSettings();
    setOutput(lsx('saveDone'));
    showToast(lsx('saveDone'), 'success');
  } catch (err) {
    const msg = lsx('saveFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
  }
}
const baseLLMSystemSetLanguage = typeof setLanguage === 'function' ? setLanguage : null;
if (baseLLMSystemSetLanguage) {
  setLanguage = function(lang) {
    baseLLMSystemSetLanguage(lang);
    applyLLMServiceSystemI18n();
  };
}
ensureLLMServiceSystemUI();







function ensureLLMServiceCardsTab() {
  ensureLLMServiceAdminUI();
  const tab = document.getElementById('tab-servicecards');
  const title = document.getElementById('llmServiceCardsTitle');
  const grantsTitle = document.getElementById('llmServiceGrantsTitle');
  if (!tab || !title || !grantsTitle || document.getElementById('llmServiceCardsStandalone')) return;
  const first = title.nextElementSibling;
  const second = first && first.nextElementSibling;
  const third = second && second.nextElementSibling;
  const list = third && third.nextElementSibling;
  if (!first || !second || !third || !list) return;
  const host = document.createElement('div');
  host.id = 'llmServiceCardsStandalone';
  host.className = 'grid2';
  const formItem = document.createElement('div');
  formItem.className = 'item';
  const listItem = document.createElement('div');
  listItem.className = 'item';
  host.appendChild(formItem);
  host.appendChild(listItem);
  formItem.appendChild(title);
  formItem.appendChild(first);
  formItem.appendChild(second);
  formItem.appendChild(third);
  const head = document.createElement('div');
  head.className = 'item-title';
  head.id = 'llmServiceCardsListTitle';
  listItem.appendChild(head);
  listItem.appendChild(list);
  tab.appendChild(host);
}
function applyLLMServiceTabI18n() {
  if (typeof tabMeta === 'object') {
    tabMeta.modelservices = ['modelServicesTabTitle', 'modelServicesTabSubtitle'];
    tabMeta.servicecards = ['serviceCardsTabTitle', 'serviceCardsTabSubtitle'];
  }
  _s('navModelServices', 'textContent', currentLang === 'zh' ? '\u6a21\u578b\u670d\u52a1' : 'Model Services');
  _s('navModelServicesDesc', 'textContent', currentLang === 'zh' ? '\u6a21\u578b\u670d\u52a1\u7ec4\u4e0e\u6388\u6743' : 'Model service groups and grants');
  _s('navServiceCards', 'textContent', currentLang === 'zh' ? '\u5145\u503c\u5361\u7ba1\u7406' : 'Service Cards');
  _s('navServiceCardsDesc', 'textContent', currentLang === 'zh' ? '\u53d1\u5361\u4e0e\u5151\u6362\u72b6\u6001' : 'Issue and review redeem cards');
  _s('modelServicesTabTitle', 'textContent', currentLang === 'zh' ? '\u6a21\u578b\u670d\u52a1' : 'Model Services');
  _s('modelServicesTabSubtitle', 'textContent', currentLang === 'zh' ? '\u6a21\u578b\u670d\u52a1\u7ec4\u3001\u6388\u6743\u4e0e\u751f\u6548\u6743\u9650' : 'Model service groups, bindings, and active grants');
  _s('modelServicesReloadBtn', 'textContent', currentLang === 'zh' ? '\u91cd\u65b0\u52a0\u8f7d' : 'Reload');
  _s('serviceCardsTabTitle', 'textContent', currentLang === 'zh' ? '\u5145\u503c\u5361\u7ba1\u7406' : 'Service Cards');
  _s('serviceCardsTabSubtitle', 'textContent', currentLang === 'zh' ? '\u53d1\u884c\u5145\u503c\u5361\u5e76\u67e5\u770b\u5151\u6362\u60c5\u51b5' : 'Issue cards and review redemption status');
  _s('serviceCardsReloadBtn', 'textContent', currentLang === 'zh' ? '\u91cd\u65b0\u52a0\u8f7d' : 'Reload');
  _s('llmServiceCardsListTitle', 'textContent', lsx('cards'));
}
function registerLLMServiceTabs() {
  if (!window.AdminTabRegistry || typeof window.AdminTabRegistry.registerTab !== 'function') return;
  window.AdminTabRegistry.registerTab({
    id: 'modelservices',
    title: function() { return currentLang === 'zh' ? '\u6a21\u578b\u670d\u52a1' : 'Model Services'; },
    subtitle: function() { return currentLang === 'zh' ? '\u6a21\u578b\u670d\u52a1\u7ec4\u3001\u6388\u6743\u4e0e\u751f\u6548\u6743\u9650' : 'Model service groups, bindings, and active grants'; },
    onOpen: function() { ensureLLMServiceCardsTab(); applyLLMServiceTabI18n(); loadLLMServiceAdmin(); }
  });
  window.AdminTabRegistry.registerTab({
    id: 'servicecards',
    title: function() { return currentLang === 'zh' ? '\u5145\u503c\u5361\u7ba1\u7406' : 'Service Cards'; },
    subtitle: function() { return currentLang === 'zh' ? '\u53d1\u884c\u5145\u503c\u5361\u5e76\u67e5\u770b\u5151\u6362\u60c5\u51b5' : 'Issue cards and review redemption status'; },
    onOpen: function() { ensureLLMServiceCardsTab(); applyLLMServiceTabI18n(); loadLLMServiceAdmin(); }
  });
}
if (window.AdminTabRegistry && typeof window.AdminTabRegistry.onLanguageChange === 'function') {
  window.AdminTabRegistry.onLanguageChange(function() {
    applyLLMServiceTabI18n();
    ensureLLMServiceCardsTab();
  });
}
registerLLMServiceTabs();
ensureLLMServiceCardsTab();
applyLLMServiceTabI18n();
function ensureLLMServiceNewGroupButton() {
  const addBtn = document.getElementById('llmServiceAddGroupBtn');
  if (!addBtn || document.getElementById('llmServiceNewGroupBtnInline')) return;
  const btn = document.createElement('button');
  btn.id = 'llmServiceNewGroupBtnInline';
  btn.className = 'btn-ghost';
  btn.textContent = currentLang === 'zh' ? '\u65b0\u589e\u670d\u52a1\u7ec4' : 'Add New Group';
  btn.onclick = function() { llmServiceSelectedGroupID = ''; renderLLMServiceAdmin(); };
  addBtn.parentNode.insertBefore(btn, addBtn);
}
ensureLLMServiceNewGroupButton();
(function() {
  const active = typeof localStorage !== 'undefined' ? localStorage.getItem(activeTabKey) : '';
  if (typeof token === 'function' && token() && (active === 'modelservices' || active === 'servicecards')) {
    openTab(active);
  }
})();




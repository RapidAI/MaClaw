const LLM_SERVICE_I18N = {
  en: {
    adminTitle: 'Model Service Groups',
    adminDesc: 'Authorize models by security group, user, or redeem card. Public API and exposed model names are shown above.',
    groups: 'Service Groups',
    bindings: 'Security Group Bindings',
    bindingsDesc: 'Reuse the security groups already created in Security.',
    users: 'User Bindings',
    cards: 'Redeem Cards',
    grants: 'Active Grants',
    navLabel: 'Model Services',
    navDesc: 'Model service groups and grants',
    serviceCardsNavLabel: 'Service Cards',
    serviceCardsNavDesc: 'Issue and review redeem cards',
    tabTitle: 'Model Services',
    tabSubtitle: 'Model service groups, bindings, and active grants',
    serviceCardsTabTitle: 'Service Cards',
    serviceCardsTabSubtitle: 'Issue cards and review redemption status',
    reload: 'Reload',
    emptyValue: '-',
    addGroup: 'Add / Update Group',
    saveAll: 'Save Service Config',
    issueCard: 'Issue Card',
    systemDefaults: 'New User Defaults',
    systemDesc: 'Grant model-service groups automatically when a new email user is created.',
    systemHint: 'Use service-group IDs. The built-in default group is Default (No Model Access) and is used as the fallback for new users.',
    newUserGroups: 'Default Service Groups',
    newUserDays: 'Validity (Days)',
    saveDefaults: 'Save Defaults',
    tokensPerCredit: 'Tokens per Credit',
    credits: 'Credits',
    diagnoseTitle: 'Entitlement Diagnostic',
    diagnoseDesc: 'Explain why a user can or cannot access model services.',
    diagnoseEmail: 'Email',
    diagnoseBtn: 'Diagnose',
    diagnoseEmpty: 'Enter an email to inspect effective entitlements.',
    diagnoseLoadFailed: 'Load entitlement diagnostic failed: {error}',
    loadFailed: 'Load model services failed: {error}',
    saveDone: 'Model service configuration saved.',
    saveFailed: 'Save model services failed: {error}',
    issueDone: 'Redeem card created: {code}',
    issueFailed: 'Create redeem card failed: {error}',
    apiBaseUrl: 'API Base URL',
    chatCompletionsUrl: 'Chat Completions URL',
    modelsUrl: 'Models URL',
    availableModels: 'Available Models',
    id: 'ID',
    name: 'Name',
    description: 'Description',
    modelsLabel: 'Models',
    modelsHint: 'One line per exposed model. Format: model=provider1,provider2; features=document,reasoning,tools; priority=50; resolution=1; multiplier=1.2. Provider order is failover priority. Auto scheduling prefers the highest matched capability score, then the lowest resolution tier, then the lowest multiplier.',
    removeGroup: 'Remove Group',
    securityGroupId: 'Security Group ID',
    serviceGroups: 'Service Groups',
    addGroupBinding: 'Add Group Binding',
    email: 'Email',
    addUserBinding: 'Add User Binding',
    label: 'Label',
    days: 'Days',
    groupIdPlaceholder: 'coding-basic',
    groupNamePlaceholder: 'Coding Basic',
    groupDescPlaceholder: 'Exposed models for basic coding',
    bindingGroupPlaceholder: 'engineering',
    bindingServiceGroupsPlaceholder: 'coding-basic,coding-pro',
    userEmailPlaceholder: 'user@example.com',
    userServiceGroupsPlaceholder: 'coding-pro',
    cardLabelPlaceholder: 'April campaign',
    cardGroupsPlaceholder: 'coding-basic',
    diagnoseEmailPlaceholder: 'user@example.com',
    builtInDefaultNoAccess: 'Built-in Default (No Model Access)',
    builtInDefault: 'Built-in Default',
    noServiceGroups: 'No service groups yet.',
    noSecurityGroupBindings: 'No security-group bindings yet.',
    noDirectUserBindings: 'No direct user bindings yet.',
    noRedeemCards: 'No redeem cards issued yet.',
    noActiveGrants: 'No active grants yet.',
    remove: 'Remove',
    modelsCount: '{count} models',
    daysCount: '{count} days',
    creditsCount: '{count} credits',
    creditsRemaining: 'credits {remaining}/{total}',
    card: 'card',
    redeemed: 'redeemed',
    unused: 'unused',
    active: 'active',
    inactive: 'inactive',
    defaultModel: 'default model',
    securityGroups: 'Security Groups',
    effectiveServiceGroups: 'Effective Service Groups',
    directUserBindings: 'Direct User Bindings',
    matchedGroupBindings: 'Matched Group Bindings',
    activeGrants: 'Active Grants',
    groupIdNameRequired: 'Service group id and name are required.',
    builtInDefaultReadOnly: 'The built-in Default group is read-only.',
    builtInDefaultCannotRemove: 'The built-in Default group cannot be removed.',
    addNewGroup: 'Add New Group',
    modelsPlaceholder: 'auto=provider-a,provider-b; features=reasoning,tools; priority=50; resolution=1; multiplier=1\\ndoc=provider-c; features=document; priority=80; resolution=1; multiplier=1.2',
    systemGroupsPlaceholder: 'default'
  },
  zh: {
    adminTitle: '\u6a21\u578b\u670d\u52a1\u7ec4',
    adminDesc: '\u6309\u5b89\u5168\u7ec4\u3001\u7528\u6237\u6216\u5145\u503c\u5361\u6388\u6743\u6a21\u578b\u670d\u52a1\u3002\u9876\u90e8\u5c55\u793a\u5bf9\u5916 API \u4e0e\u53ef\u7528\u6a21\u578b\u5217\u8868\u3002',
    groups: '\u670d\u52a1\u7ec4',
    bindings: '\u5b89\u5168\u7ec4\u7ed1\u5b9a',
    bindingsDesc: '\u590d\u7528\u5728\u5b89\u5168\u7ba1\u7406\u4e2d\u5df2\u521b\u5efa\u7684\u7528\u6237\u7ec4\u3002',
    users: '\u7528\u6237\u7ed1\u5b9a',
    cards: '\u5145\u503c\u5361',
    grants: '\u751f\u6548\u6388\u6743',
    navLabel: '\u6a21\u578b\u670d\u52a1',
    navDesc: '\u6a21\u578b\u670d\u52a1\u7ec4\u4e0e\u6388\u6743',
    serviceCardsNavLabel: '\u5145\u503c\u5361\u7ba1\u7406',
    serviceCardsNavDesc: '\u53d1\u5361\u4e0e\u5151\u6362\u72b6\u6001',
    tabTitle: '\u6a21\u578b\u670d\u52a1',
    tabSubtitle: '\u6a21\u578b\u670d\u52a1\u7ec4\u3001\u6388\u6743\u4e0e\u751f\u6548\u6743\u9650',
    serviceCardsTabTitle: '\u5145\u503c\u5361\u7ba1\u7406',
    serviceCardsTabSubtitle: '\u53d1\u884c\u5145\u503c\u5361\u5e76\u67e5\u770b\u5151\u6362\u60c5\u51b5',
    reload: '\u91cd\u65b0\u52a0\u8f7d',
    emptyValue: '-',
    addGroup: '\u65b0\u5efa / \u66f4\u65b0\u670d\u52a1\u7ec4',
    saveAll: '\u4fdd\u5b58\u670d\u52a1\u914d\u7f6e',
    issueCard: '\u53d1\u884c\u5145\u503c\u5361',
    systemDefaults: '\u65b0\u7528\u6237\u9ed8\u8ba4\u6388\u6743',
    systemDesc: '\u65b0\u90ae\u7bb1\u7528\u6237\u521b\u5efa\u65f6\u81ea\u52a8\u6388\u4e88\u6a21\u578b\u670d\u52a1\u7ec4\u3002',
    systemHint: '\u8bf7\u586b\u5199\u670d\u52a1\u7ec4 ID\u3002\u5185\u7f6e\u9ed8\u8ba4\u7ec4\u4e3a Default\uff08\u65e0\u6a21\u578b\u6743\u9650\uff09\uff0c\u4f1a\u4f5c\u4e3a\u65b0\u7528\u6237\u7684\u56de\u9000\u7ec4\u3002',
    newUserGroups: '\u9ed8\u8ba4\u670d\u52a1\u7ec4',
    newUserDays: '\u6709\u6548\u671f\uff08\u5929\uff09',
    saveDefaults: '\u4fdd\u5b58\u9ed8\u8ba4\u503c',
    tokensPerCredit: '\u6bcf Credit \u5bf9\u5e94 Token \u6570',
    credits: 'Credits',
    diagnoseTitle: '\u6388\u6743\u8bca\u65ad',
    diagnoseDesc: '\u8bf4\u660e\u67d0\u4e2a\u7528\u6237\u4e3a\u4ec0\u4e48\u80fd\u6216\u4e0d\u80fd\u8bbf\u95ee\u6a21\u578b\u670d\u52a1\u3002',
    diagnoseEmail: '\u90ae\u7bb1',
    diagnoseBtn: '\u5f00\u59cb\u8bca\u65ad',
    diagnoseEmpty: '\u8bf7\u8f93\u5165\u90ae\u7bb1\u4ee5\u67e5\u770b\u5b9e\u9645\u751f\u6548\u7684\u6743\u9650\u3002',
    diagnoseLoadFailed: '\u52a0\u8f7d\u6388\u6743\u8bca\u65ad\u5931\u8d25: {error}',
    loadFailed: '\u52a0\u8f7d\u6a21\u578b\u670d\u52a1\u5931\u8d25: {error}',
    saveDone: '\u6a21\u578b\u670d\u52a1\u914d\u7f6e\u5df2\u4fdd\u5b58\u3002',
    saveFailed: '\u4fdd\u5b58\u6a21\u578b\u670d\u52a1\u5931\u8d25: {error}',
    issueDone: '\u5145\u503c\u5361\u5df2\u521b\u5efa: {code}',
    issueFailed: '\u521b\u5efa\u5145\u503c\u5361\u5931\u8d25: {error}',
    apiBaseUrl: 'API \u57fa\u5730\u5740',
    chatCompletionsUrl: 'Chat Completions \u5730\u5740',
    modelsUrl: 'Models \u5730\u5740',
    availableModels: '\u53ef\u7528\u6a21\u578b',
    id: 'ID',
    name: '\u540d\u79f0',
    description: '\u63cf\u8ff0',
    modelsLabel: '\u6a21\u578b',
    modelsHint: '\u6bcf\u884c\u5b9a\u4e49\u4e00\u4e2a\u5bf9\u5916\u66b4\u9732\u7684\u6a21\u578b\u3002\u683c\u5f0f\uff1amodel=provider1,provider2; features=document,reasoning,tools; priority=50; resolution=1; multiplier=1.2\u3002provider \u987a\u5e8f\u4ee3\u8868\u5931\u8d25\u5207\u6362\u4f18\u5148\u7ea7\uff0c\u81ea\u52a8\u8c03\u5ea6\u4f1a\u4f18\u5148\u9009\u62e9\u80fd\u529b\u5339\u914d\u5ea6\u6700\u9ad8\u3001\u5206\u8fa8\u7387\u5c42\u7ea7\u6700\u4f4e\u3001\u500d\u7387\u6700\u4f4e\u7684\u6a21\u578b\u3002',
    removeGroup: '\u5220\u9664\u670d\u52a1\u7ec4',
    securityGroupId: '\u5b89\u5168\u7ec4 ID',
    serviceGroups: '\u670d\u52a1\u7ec4',
    addGroupBinding: '\u65b0\u589e\u7ec4\u7ed1\u5b9a',
    email: '\u90ae\u7bb1',
    addUserBinding: '\u65b0\u589e\u7528\u6237\u7ed1\u5b9a',
    label: '\u6807\u7b7e',
    days: '\u5929\u6570',
    groupIdPlaceholder: 'coding-basic',
    groupNamePlaceholder: '\u57fa\u7840\u7f16\u7801\u670d\u52a1',
    groupDescPlaceholder: '\u57fa\u7840\u7f16\u7801\u573a\u666f\u7684\u5bf9\u5916\u6a21\u578b',
    bindingGroupPlaceholder: 'engineering',
    bindingServiceGroupsPlaceholder: 'coding-basic,coding-pro',
    userEmailPlaceholder: 'user@example.com',
    userServiceGroupsPlaceholder: 'coding-pro',
    cardLabelPlaceholder: '\u56db\u6708\u6d3b\u52a8',
    cardGroupsPlaceholder: 'coding-basic',
    diagnoseEmailPlaceholder: 'user@example.com',
    builtInDefaultNoAccess: '\u5185\u7f6e Default\uff08\u65e0\u6a21\u578b\u6743\u9650\uff09',
    builtInDefault: '\u5185\u7f6e Default',
    noServiceGroups: '\u6682\u65e0\u670d\u52a1\u7ec4\u3002',
    noSecurityGroupBindings: '\u6682\u65e0\u5b89\u5168\u7ec4\u7ed1\u5b9a\u3002',
    noDirectUserBindings: '\u6682\u65e0\u76f4\u63a5\u7528\u6237\u7ed1\u5b9a\u3002',
    noRedeemCards: '\u6682\u65e0\u5df2\u53d1\u884c\u7684\u5145\u503c\u5361\u3002',
    noActiveGrants: '\u6682\u65e0\u751f\u6548\u6388\u6743\u3002',
    remove: '\u79fb\u9664',
    modelsCount: '{count} \u4e2a\u6a21\u578b',
    daysCount: '{count} \u5929',
    creditsCount: '{count} Credits',
    creditsRemaining: 'Credits {remaining}/{total}',
    card: '\u5361',
    redeemed: '\u5df2\u5151\u6362',
    unused: '\u672a\u4f7f\u7528',
    active: '\u751f\u6548\u4e2d',
    inactive: '\u672a\u751f\u6548',
    defaultModel: '\u9ed8\u8ba4\u6a21\u578b',
    securityGroups: '\u5b89\u5168\u7ec4',
    effectiveServiceGroups: '\u751f\u6548\u670d\u52a1\u7ec4',
    directUserBindings: '\u76f4\u63a5\u7528\u6237\u7ed1\u5b9a',
    matchedGroupBindings: '\u5339\u914d\u5230\u7684\u7ec4\u7ed1\u5b9a',
    activeGrants: '\u751f\u6548\u6388\u6743',
    groupIdNameRequired: '\u670d\u52a1\u7ec4 ID \u548c\u540d\u79f0\u4e3a\u5fc5\u586b\u9879\u3002',
    builtInDefaultReadOnly: '\u5185\u7f6e Default \u7ec4\u4e3a\u53ea\u8bfb\u3002',
    builtInDefaultCannotRemove: '\u5185\u7f6e Default \u7ec4\u4e0d\u80fd\u5220\u9664\u3002',
    addNewGroup: '\u65b0\u5efa\u670d\u52a1\u7ec4',
    modelsPlaceholder: 'auto=provider-a,provider-b; features=reasoning,tools; priority=50; resolution=1; multiplier=1\\ndoc=provider-c; features=document; priority=80; resolution=1; multiplier=1.2',
    systemGroupsPlaceholder: 'default'
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
    '<div><label id="llmServiceExposeApiBaseLabel"></label><div id="llmServiceExposeApiBase" class="mono" style="padding:10px 12px;border:1px solid var(--line);border-radius:12px;min-height:42px">-</div></div>' +
    '<div><label id="llmServiceExposeChatUrlLabel"></label><div id="llmServiceExposeChatUrl" class="mono" style="padding:10px 12px;border:1px solid var(--line);border-radius:12px;min-height:42px">-</div></div>' +
    '<div><label id="llmServiceExposeModelsUrlLabel"></label><div id="llmServiceExposeModelsUrl" class="mono" style="padding:10px 12px;border:1px solid var(--line);border-radius:12px;min-height:42px">-</div></div>' +
    '<div><label id="llmServiceExposeModelsLabel"></label><div id="llmServiceExposeModels" class="mono" style="padding:10px 12px;border:1px solid var(--line);border-radius:12px;min-height:42px">-</div></div>' +
    '</div><div class="grid2">' +
    '<div><label id="llmServiceGroupIDLabel"></label><input id="llmServiceGroupID"></div>' +
    '<div><label id="llmServiceGroupNameLabel"></label><input id="llmServiceGroupName"></div>' +
    '<div style="grid-column:1 / -1"><label id="llmServiceGroupDescLabel"></label><input id="llmServiceGroupDesc"></div>' +
    '<div style="grid-column:1 / -1"><label id="llmServiceGroupModelsLabel"></label><textarea id="llmServiceGroupModels" style="width:100%;min-height:100px;padding:10px;border-radius:12px;border:1px solid var(--line);font:inherit;resize:vertical" placeholder=""></textarea><div class="hint" id="llmServiceGroupModelsHint"></div></div>' +
    '</div><div class="actions" style="margin-top:12px"><button class="btn-primary" onclick="upsertLLMServiceGroup()" id="llmServiceAddGroupBtn"></button><button class="btn-danger" onclick="removeSelectedLLMServiceGroup()" id="llmServiceRemoveGroupBtn"></button></div><div id="llmServiceGroupsList" style="margin-top:14px"></div></div>' +
    '<div class="item"><div class="item-head"><div><div class="item-title" id="llmServiceBindingsTitle"></div><div class="item-meta" id="llmServiceBindingsDesc"></div></div></div>' +
    '<div class="grid2"><div><label id="llmServiceBindingGroupIDLabel"></label><input id="llmServiceBindingGroupID"></div><div><label id="llmServiceBindingServiceGroupsLabel"></label><input id="llmServiceBindingServiceGroups"></div></div><div class="actions"><button class="btn-secondary" onclick="addLLMServiceGroupBinding()" id="llmServiceAddGroupBindingBtn"></button></div><div id="llmServiceGroupBindingsList"></div>' +
    '<div style="margin-top:16px" class="item-title" id="llmServiceUsersTitle"></div><div class="grid2"><div><label id="llmServiceUserEmailLabel"></label><input id="llmServiceUserEmail"></div><div><label id="llmServiceUserServiceGroupsLabel"></label><input id="llmServiceUserServiceGroups"></div></div><div class="actions"><button class="btn-secondary" onclick="addLLMServiceUserBinding()" id="llmServiceAddUserBindingBtn"></button></div><div id="llmServiceUserBindingsList"></div>' +
    '<div style="margin-top:16px" class="item-title" id="llmServiceCardsTitle"></div><div class="grid2"><div><label id="llmServiceCardLabelLabel"></label><input id="llmServiceCardLabel"></div><div><label id="llmServiceCardGroupsLabel"></label><input id="llmServiceCardGroups"></div></div><div class="grid2" style="margin-top:12px"><div><label id="llmServiceCardDaysLabel"></label><input id="llmServiceCardDays" type="number" min="1" value="30"></div><div><label id="llmServiceCardCreditsLabel"></label><input id="llmServiceCardCredits" type="number" min="0" step="1" value="1000"></div></div><div class="actions"><button class="btn-primary" onclick="issueLLMServiceCard()" id="llmServiceIssueBtn"></button></div><div id="llmServiceCardsList"></div>' +
    '<div style="margin-top:16px" class="item-title" id="llmServiceGrantsTitle"></div><div id="llmServiceGrantsList"></div>' +
    '<div style="margin-top:16px" class="item-title" id="llmServiceDiagnoseTitle"></div><div class="item-meta" id="llmServiceDiagnoseDesc" style="margin-bottom:10px"></div><div class="grid2"><div><label id="llmServiceDiagnoseEmailLabel"></label><input id="llmServiceDiagnoseEmail"></div><div style="display:flex;align-items:flex-end"><button class="btn-secondary" onclick="diagnoseLLMServiceUser()" id="llmServiceDiagnoseBtn"></button></div></div><div id="llmServiceDiagnoseResult" style="margin-top:12px"></div></div>';
  tab.appendChild(host);
  applyLLMServiceI18n();
}
function applyLLMServiceI18n() {
  _s('llmServiceAdminTitle', 'textContent', lsx('adminTitle'));
  _s('llmServiceAdminDesc', 'textContent', lsx('adminDesc'));
  _s('llmServiceExposeApiBaseLabel', 'textContent', lsx('apiBaseUrl'));
  _s('llmServiceExposeChatUrlLabel', 'textContent', lsx('chatCompletionsUrl'));
  _s('llmServiceExposeModelsUrlLabel', 'textContent', lsx('modelsUrl'));
  _s('llmServiceExposeModelsLabel', 'textContent', lsx('availableModels'));
  _s('llmServiceGroupIDLabel', 'textContent', lsx('id'));
  _s('llmServiceGroupNameLabel', 'textContent', lsx('name'));
  _s('llmServiceGroupDescLabel', 'textContent', lsx('description'));
  _s('llmServiceGroupModelsLabel', 'textContent', lsx('modelsLabel'));
  _s('llmServiceGroupModelsHint', 'textContent', lsx('modelsHint'));
  _s('llmServiceBindingsTitle', 'textContent', lsx('bindings'));
  _s('llmServiceBindingsDesc', 'textContent', lsx('bindingsDesc'));
  _s('llmServiceBindingGroupIDLabel', 'textContent', lsx('securityGroupId'));
  _s('llmServiceBindingServiceGroupsLabel', 'textContent', lsx('serviceGroups'));
  _s('llmServiceAddGroupBindingBtn', 'textContent', lsx('addGroupBinding'));
  _s('llmServiceUsersTitle', 'textContent', lsx('users'));
  _s('llmServiceUserEmailLabel', 'textContent', lsx('email'));
  _s('llmServiceUserServiceGroupsLabel', 'textContent', lsx('serviceGroups'));
  _s('llmServiceAddUserBindingBtn', 'textContent', lsx('addUserBinding'));
  _s('llmServiceCardsTitle', 'textContent', lsx('cards'));
  _s('llmServiceCardLabelLabel', 'textContent', lsx('label'));
  _s('llmServiceCardGroupsLabel', 'textContent', lsx('serviceGroups'));
  _s('llmServiceCardDaysLabel', 'textContent', lsx('days'));
  _s('llmServiceGrantsTitle', 'textContent', lsx('grants'));
  _s('llmServiceAddGroupBtn', 'textContent', lsx('addGroup'));
  _s('llmServiceSaveBtn', 'textContent', lsx('saveAll'));
  _s('llmServiceIssueBtn', 'textContent', lsx('issueCard'));
  _s('llmServiceCardCreditsLabel', 'textContent', lsx('credits'));
  _s('llmServiceDiagnoseTitle', 'textContent', lsx('diagnoseTitle'));
  _s('llmServiceDiagnoseDesc', 'textContent', lsx('diagnoseDesc'));
  _s('llmServiceDiagnoseEmailLabel', 'textContent', lsx('diagnoseEmail'));
  _s('llmServiceDiagnoseBtn', 'textContent', lsx('diagnoseBtn'));
  _s('llmServiceGroupID', 'placeholder', lsx('groupIdPlaceholder'));
  _s('llmServiceGroupName', 'placeholder', lsx('groupNamePlaceholder'));
  _s('llmServiceGroupDesc', 'placeholder', lsx('groupDescPlaceholder'));
  _s('llmServiceBindingGroupID', 'placeholder', lsx('bindingGroupPlaceholder'));
  _s('llmServiceBindingServiceGroups', 'placeholder', lsx('bindingServiceGroupsPlaceholder'));
  _s('llmServiceUserEmail', 'placeholder', lsx('userEmailPlaceholder'));
  _s('llmServiceUserServiceGroups', 'placeholder', lsx('userServiceGroupsPlaceholder'));
  _s('llmServiceCardLabel', 'placeholder', lsx('cardLabelPlaceholder'));
  _s('llmServiceCardGroups', 'placeholder', lsx('cardGroupsPlaceholder'));
  _s('llmServiceDiagnoseEmail', 'placeholder', lsx('diagnoseEmailPlaceholder'));
  _s('llmServiceGroupModels', 'placeholder', lsx('modelsPlaceholder'));
}
async function loadLLMServiceAdmin() {
  ensureLLMServiceAdminUI();
  ensureLLMServiceSystemUI();
  try {
    const data = await api('/api/admin/llm/services');
    llmServiceAdminCache = data || { model_service_groups: [], group_bindings: [], user_bindings: [], cards: [], grants: [] };
    if (llmServiceSelectedGroupID && !(llmServiceAdminCache.model_service_groups || []).some(function(g) { return g.id === llmServiceSelectedGroupID; })) llmServiceSelectedGroupID = '';
    renderLLMServiceAdmin();
    renderLLMServiceSystemSettings();
  } catch (err) {
    const msg = lsx('loadFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
  }
}
function renderLLMServiceAdmin() {
  if (!llmServiceAdminCache) return;
  ensureLLMServiceAdminUI();
  const ui = aui();
  if (!ui || typeof ui.renderList !== 'function' || typeof ui.simpleCard !== 'function' || typeof ui.hint !== 'function' || typeof ui.actionButton !== 'function' || typeof ui.badge !== 'function' || typeof ui.meta !== 'function') return;
  applyLLMServiceI18n();
  const groups = llmServiceAdminCache.model_service_groups || [];
  _s('llmServiceExposeApiBase', 'textContent', llmServiceAdminCache.expose_api_base_url || lsx('emptyValue'));
  _s('llmServiceExposeChatUrl', 'textContent', llmServiceAdminCache.expose_base_url || lsx('emptyValue'));
  _s('llmServiceExposeModelsUrl', 'textContent', llmServiceAdminCache.expose_models_url || lsx('emptyValue'));
  _s('llmServiceExposeModels', 'textContent', (llmServiceAdminCache.available_models || []).length ? llmServiceAdminCache.available_models.join(', ') : lsx('emptyValue'));
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
    if (addBtn) addBtn.textContent = builtin ? lsx('builtInDefaultNoAccess') : lsx('addGroup');
    if (removeBtn) { removeBtn.disabled = builtin; removeBtn.textContent = builtin ? lsx('builtInDefault') : lsx('removeGroup'); }
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
    if (removeBtn) { removeBtn.disabled = true; removeBtn.textContent = lsx('removeGroup'); }
  }
  const groupsRoot = document.getElementById('llmServiceGroupsList');
  if (groupsRoot) groupsRoot.innerHTML = ui.renderList(groups, function(g) {
    const active = g.id === llmServiceSelectedGroupID;
    return ui.simpleCard({
      title: g.name || g.id,
      titleMeta: g.id || '',
      titleMetaClass: 'mono',
      headRight: ui.badge(lsx('modelsCount', { count: String((g.models || []).length) }), 'info'),
      style: 'margin-top:10px;border:' + (active ? '1px solid rgba(47,128,237,.36)' : '1px solid var(--line)') + ';cursor:pointer',
      attrs: { onclick: 'selectLLMServiceGroup(\'' + String(g.id).replace(/'/g, "\\'") + '\')' },
      body: [ui.meta(g.description || ''), ui.meta(modelDefsText(g.models || []), 'mono', 'margin-top:8px')]
    });
  }, lsx('noServiceGroups'));
  const gbRoot = document.getElementById('llmServiceGroupBindingsList');
  if (gbRoot) gbRoot.innerHTML = ui.renderList(llmServiceAdminCache.group_bindings || [], function(b, idx) {
    return ui.simpleCard({
      title: b.group_id || '',
      titleMeta: (b.service_group_ids || []).join(', '),
      titleMetaClass: 'mono',
      style: 'margin-top:10px',
      headRight: ui.actionButton(lsx('remove'), 'btn-danger', 'removeLLMServiceGroupBinding(' + idx + ')')
    });
  }, lsx('noSecurityGroupBindings'));
  const ubRoot = document.getElementById('llmServiceUserBindingsList');
  if (ubRoot) ubRoot.innerHTML = ui.renderList(llmServiceAdminCache.user_bindings || [], function(b, idx) {
    return ui.simpleCard({
      title: b.email || '',
      titleMeta: (b.service_group_ids || []).join(', '),
      titleMetaClass: 'mono',
      style: 'margin-top:10px',
      headRight: ui.actionButton(lsx('remove'), 'btn-danger', 'removeLLMServiceUserBinding(' + idx + ')')
    });
  }, lsx('noDirectUserBindings'));
  const cardsRoot = document.getElementById('llmServiceCardsList');
  if (cardsRoot) cardsRoot.innerHTML = ui.renderList(llmServiceAdminCache.cards || [], function(c) {
    return ui.simpleCard({
      title: c.label || c.id || lsx('card'),
      titleMeta: (c.service_group_ids || []).join(', ') + ' | ' + lsx('daysCount', { count: String(c.duration_days || 0) }) + ' | ' + lsx('creditsCount', { count: String(c.credits || 0) }),
      titleMetaClass: 'mono',
      style: 'margin-top:10px',
      headRight: ui.badge(c.redeemed_at ? lsx('redeemed') : lsx('unused'), c.redeemed_at ? 'warn' : 'ok'),
      body: ui.meta((c.redeemed_by_email || '') + (c.redeemed_at ? (' | ' + String(c.redeemed_at)) : ''))
    });
  }, lsx('noRedeemCards'));
  const grantsRoot = document.getElementById('llmServiceGrantsList');
  if (grantsRoot) grantsRoot.innerHTML = ui.renderList(llmServiceAdminCache.grants || [], function(g) {
    const total = Number(g.credits_total || 0);
    const used = Number(g.credits_used || 0);
    const remaining = total > 0 ? Math.max(0, total - used) : 0;
    const creditsText = total > 0 ? (' | ' + lsx('creditsRemaining', { remaining: remaining.toFixed(3).replace(/\.000$/, ''), total: total.toFixed(3).replace(/\.000$/, '') })) : '';
    return ui.simpleCard({
      title: g.email || '',
      titleMeta: (g.service_group_id || '') + ' | ' + (g.source || '') + creditsText,
      titleMetaClass: 'mono',
      style: 'margin-top:10px',
      headRight: ui.badge(String(g.expires_at || ''), 'info')
    });
  }, lsx('noActiveGrants'));
  const diagnoseRoot = document.getElementById('llmServiceDiagnoseResult');
  if (diagnoseRoot) {
    const diag = llmServiceAdminCache.user_diagnostic || null;
    if (!diag || !diag.email) {
      diagnoseRoot.innerHTML = ui.hint(lsx('diagnoseEmpty'));
    } else {
      const status = diag.service_status || {};
      const securityGroups = (diag.resolved_security_group_ids || []).join(', ') || lsx('emptyValue');
      const effectiveGroups = (status.service_group_ids || []).join(', ') || lsx('emptyValue');
      const models = (status.available_models || []).join(', ') || lsx('emptyValue');
      const userBindings = (diag.direct_user_bindings || []).length ? diag.direct_user_bindings.map(function(b) { return ui.meta((b.service_group_ids || []).join(', '), 'mono'); }).join('') : ui.hint(lsx('emptyValue'));
      const groupBindings = (diag.matched_group_bindings || []).length ? diag.matched_group_bindings.map(function(b) { return ui.meta((b.group_id || '') + ' ' + ((b.service_group_ids || []).join(', '))); }).join('') : ui.hint(lsx('emptyValue'));
      const grants = (diag.active_grants || []).length ? diag.active_grants.map(function(g) { return ui.meta((g.service_group_id || '') + ' | ' + (g.source || '') + ' | ' + String(g.expires_at || '')); }).join('') : ui.hint(lsx('emptyValue'));
      diagnoseRoot.innerHTML = '<div class="item"><div class="item-head"><div><div class="item-title">' + escapeHtml(diag.email || '') + '</div><div class="item-meta">' + (status.active ? lsx('active') : lsx('inactive')) + ' | ' + lsx('defaultModel') + ': <span class="mono">' + escapeHtml(status.default_model || 'auto') + '</span></div></div></div><div class="grid2" style="margin-top:12px"><div><label>' + lsx('securityGroups') + '</label><div class="mono">' + escapeHtml(securityGroups) + '</div></div><div><label>' + lsx('effectiveServiceGroups') + '</label><div class="mono">' + escapeHtml(effectiveGroups) + '</div></div><div><label>' + lsx('availableModels') + '</label><div class="mono">' + escapeHtml(models) + '</div></div><div><label>' + lsx('credits') + '</label><div class="mono">' + escapeHtml(String(status.credits_available || 0)) + '</div></div></div><div style="margin-top:12px"><label>' + lsx('directUserBindings') + '</label>' + userBindings + '</div><div style="margin-top:12px"><label>' + lsx('matchedGroupBindings') + '</label>' + groupBindings + '</div><div style="margin-top:12px"><label>' + lsx('activeGrants') + '</label>' + grants + '</div></div>';
    }
  }
}
function selectLLMServiceGroup(id) { llmServiceSelectedGroupID = id; renderLLMServiceAdmin(); }
function upsertLLMServiceGroup() {
  if (!llmServiceAdminCache) llmServiceAdminCache = { model_service_groups: [], group_bindings: [], user_bindings: [], cards: [], grants: [] };
  const next = { id: (document.getElementById('llmServiceGroupID').value || '').trim(), name: (document.getElementById('llmServiceGroupName').value || '').trim(), description: (document.getElementById('llmServiceGroupDesc').value || '').trim(), models: parseModelDefs(document.getElementById('llmServiceGroupModels').value || '') };
  if (!next.id || !next.name) { showToast(lsx('groupIdNameRequired'), 'error'); return false; }
  if (isBuiltinLLMServiceGroup(next.id) || isBuiltinLLMServiceGroup(llmServiceSelectedGroupID)) { showToast(lsx('builtInDefaultReadOnly'), 'info'); return false; }
  const idx = (llmServiceAdminCache.model_service_groups || []).findIndex(function(g) { return g.id === next.id; });
  if (idx >= 0) llmServiceAdminCache.model_service_groups[idx] = next; else llmServiceAdminCache.model_service_groups.push(next);
  llmServiceSelectedGroupID = next.id;
  renderLLMServiceAdmin();
  return true;
}
function removeSelectedLLMServiceGroup() {
  if (!llmServiceAdminCache || !llmServiceSelectedGroupID) return;
  if (isBuiltinLLMServiceGroup(llmServiceSelectedGroupID)) { showToast(lsx('builtInDefaultCannotRemove'), 'info'); return; }
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
  const emailEl = document.getElementById('llmServiceDiagnoseEmail');
  const email = (emailEl && emailEl.value || '').trim();
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
    const labelEl = document.getElementById('llmServiceCardLabel');
    const groupsEl = document.getElementById('llmServiceCardGroups');
    const daysEl = document.getElementById('llmServiceCardDays');
    const creditsEl = document.getElementById('llmServiceCardCredits');
    if (!labelEl || !groupsEl || !daysEl || !creditsEl) return;
    const data = await api('/api/admin/llm/service-cards', { method: 'POST', body: JSON.stringify({ label: (labelEl.value || '').trim(), service_group_ids: parseCSV(groupsEl.value || ''), duration_days: Number(daysEl.value || 30), credits: Number(creditsEl.value || 0) }) });
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
    ensureLLMServiceNewGroupButton();
    if (llmServiceAdminCache) renderLLMServiceAdmin();
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
    '<div class="head" style="margin-bottom:12px"><div><div class="item-title" id="llmServiceSystemTitle"></div><div class="item-meta" id="llmServiceSystemDesc"></div></div><div class="actions"><button class="btn-primary" onclick="saveLLMServiceSystemSettings()" id="llmServiceSystemSaveBtn"></button></div></div>' +
    '<div class="grid2">' +
    '<div><label id="llmServiceSystemGroupsLabel"></label><input id="llmServiceSystemGroups" placeholder=""></div>' +
    '<div><label id="llmServiceSystemDaysLabel"></label><input id="llmServiceSystemDays" type="number" min="1" value="30"></div>' +
    '</div><div class="grid2" style="margin-top:12px"><div><label id="llmServiceSystemTokensLabel"></label><input id="llmServiceSystemTokensPerCredit" type="number" min="1" value="10000"></div><div></div></div><div class="hint" id="llmServiceSystemHint" style="margin-top:12px"></div>';
  tab.appendChild(host);
  applyLLMServiceSystemI18n();
}
function applyLLMServiceSystemI18n() {
  _s('llmServiceSystemTitle', 'textContent', lsx('systemDefaults'));
  _s('llmServiceSystemDesc', 'textContent', lsx('systemDesc'));
  _s('llmServiceSystemGroupsLabel', 'textContent', lsx('newUserGroups'));
  _s('llmServiceSystemDaysLabel', 'textContent', lsx('newUserDays'));
  _s('llmServiceSystemSaveBtn', 'textContent', lsx('saveDefaults'));
  _s('llmServiceSystemTokensLabel', 'textContent', lsx('tokensPerCredit'));
  _s('llmServiceSystemHint', 'textContent', lsx('systemHint'));
  _s('llmServiceSystemGroups', 'placeholder', lsx('systemGroupsPlaceholder'));
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
    ensureLLMServiceNewGroupButton();
    if (llmServiceAdminCache) { renderLLMServiceAdmin(); renderLLMServiceSystemSettings(); }
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
  _s('navModelServices', 'textContent', lsx('navLabel'));
  _s('navModelServicesDesc', 'textContent', lsx('navDesc'));
  _s('navServiceCards', 'textContent', lsx('serviceCardsNavLabel'));
  _s('navServiceCardsDesc', 'textContent', lsx('serviceCardsNavDesc'));
  _s('modelServicesTabTitle', 'textContent', lsx('tabTitle'));
  _s('modelServicesTabSubtitle', 'textContent', lsx('tabSubtitle'));
  _s('modelServicesReloadBtn', 'textContent', lsx('reload'));
  _s('serviceCardsTabTitle', 'textContent', lsx('serviceCardsTabTitle'));
  _s('serviceCardsTabSubtitle', 'textContent', lsx('serviceCardsTabSubtitle'));
  _s('serviceCardsReloadBtn', 'textContent', lsx('reload'));
  _s('llmServiceCardsListTitle', 'textContent', lsx('cards'));
}
function registerLLMServiceTabs() {
  if (!window.AdminTabRegistry || typeof window.AdminTabRegistry.registerTab !== 'function') return;
  window.AdminTabRegistry.registerTab({
    id: 'modelservices',
    title: function() { return lsx('tabTitle'); },
    subtitle: function() { return lsx('tabSubtitle'); },
    onOpen: function() { ensureLLMServiceCardsTab(); applyLLMServiceTabI18n(); loadLLMServiceAdmin(); }
  });
  window.AdminTabRegistry.registerTab({
    id: 'servicecards',
    title: function() { return lsx('serviceCardsTabTitle'); },
    subtitle: function() { return lsx('serviceCardsTabSubtitle'); },
    onOpen: function() { ensureLLMServiceCardsTab(); applyLLMServiceTabI18n(); loadLLMServiceAdmin(); }
  });
}
if (window.AdminTabRegistry && typeof window.AdminTabRegistry.onLanguageChange === 'function') {
  window.AdminTabRegistry.onLanguageChange(function() {
    applyLLMServiceTabI18n();
    ensureLLMServiceCardsTab();
    ensureLLMServiceNewGroupButton();
    if (llmServiceAdminCache) {
      renderLLMServiceAdmin();
      renderLLMServiceSystemSettings();
    }
  });
}
window.loadLlmServiceGroups = loadLLMServiceAdmin;
window.openLlmServiceGroupTab = function() { if (typeof openTab === 'function') openTab('modelservices'); };
registerLLMServiceTabs();
ensureLLMServiceCardsTab();
applyLLMServiceTabI18n();
function ensureLLMServiceNewGroupButton() {
  const addBtn = document.getElementById('llmServiceAddGroupBtn');
  if (!addBtn) return;
  const existing = document.getElementById('llmServiceNewGroupBtnInline');
  if (existing) {
    existing.textContent = lsx('addNewGroup');
    return;
  }
  const btn = document.createElement('button');
  btn.id = 'llmServiceNewGroupBtnInline';
  btn.className = 'btn-ghost';
  btn.textContent = lsx('addNewGroup');
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






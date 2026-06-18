/**
 * HubCenter Admin: LLM Service Tab
 * - LLM 接入 (Provider CRUD with form dialog)
 * - 模型服务 (Service Group CRUD with form dialog)
 * ASCII only. Chinese via \uXXXX or data-i18n.
 */

// Register i18n keys for the main admin-core applyI18n system
if (typeof I18N_EN !== 'undefined') {
  Object.assign(I18N_EN, {llmServiceTitle:'LLM Service',llmServiceDesc:'Manage LLM providers, compute agents, and model service groups.',llmServiceProviders:'Providers',llmServiceGroups:'Service Groups',llmServiceProvidersTitle:'LLM Providers',llmServiceProvidersDesc:'Backend LLM API endpoints for model routing.',llmServiceGroupsTitle:'Model Service Groups',llmServiceGroupsDesc:'Route models to providers with dispatch policies.',llmServiceAddProvider:'Add Provider',llmServiceNoProviders:'No providers configured.',llmServiceProviderName:'Name',llmServiceProviderURL:'API URL',llmServiceProviderKey:'API Key',llmServiceProviderProtocol:'Protocol',llmServiceProviderModels:'Models',llmServiceProviderPriority:'Priority',llmServiceProviderConcurrency:'Max Concurrency',llmServiceProviderCapabilities:'Capabilities',llmServiceAddGroup:'Add Service Group',llmServiceNoGroups:'No service groups.',llmServiceGroupName:'Group Name',llmServiceGroupDesc:'Description',llmServiceGroupModels:'Models',llmServiceSave:'Save',llmServiceCancel:'Cancel',sgRouteHint:'Exposed model alias with provider failover',sgRemoveRoute:'Remove',sgExposedModel:'Exposed Model',sgNoProviders:'No providers assigned. Add a provider above.',sgAccessPolicy:'Access Policy',sgPolicyFreeHint:'no grant needed',sgPolicyGrantHint:'needs card/grant',sgRoutes:'Provider Routes',sgAddRoute:'+ Add Route',sgProviderAlreadyAdded:'Provider already added to this route.',sgProviderConfigTitle:'Provider Config',sgCapabilityTags:'Capability Tags',sgExtraTags:'Extra Tags (custom)',sgPriority:'Priority',sgResolutionTier:'Resolution Tier',sgCreditMultiplier:'Credit Multiplier',sgIDNameRequired:'ID and Name are required.',sgRouteNeedsProvider:'Each route needs at least one provider.',sgAvailableProviders:'Available providers',chooseProvider:'Choose Provider'});
  Object.assign(I18N_ZH, {llmServiceTitle:'LLM \u63a5\u5165',llmServiceDesc:'\u7ba1\u7406 LLM \u670d\u52a1\u5546\u3001\u7b97\u529b\u4ee3\u7406\u5546\u548c\u6a21\u578b\u670d\u52a1\u7ec4\u3002',llmServiceProviders:'\u670d\u52a1\u5546',llmServiceGroups:'\u670d\u52a1\u7ec4',llmServiceProvidersTitle:'LLM \u670d\u52a1\u5546\u7ba1\u7406',llmServiceProvidersDesc:'\u540e\u7aef LLM API \u7aef\u70b9\u914d\u7f6e\u3002',llmServiceGroupsTitle:'\u6a21\u578b\u670d\u52a1\u7ec4',llmServiceGroupsDesc:'\u5c06\u6a21\u578b\u8def\u7531\u5230\u670d\u52a1\u5546\uff0c\u914d\u7f6e\u8c03\u5ea6\u7b56\u7565\u3002',llmServiceAddProvider:'\u6dfb\u52a0\u670d\u52a1\u5546',llmServiceNoProviders:'\u672a\u914d\u7f6e\u670d\u52a1\u5546\u3002',llmServiceProviderName:'\u540d\u79f0',llmServiceProviderURL:'API \u5730\u5740',llmServiceProviderKey:'API \u5bc6\u94a5',llmServiceProviderProtocol:'\u534f\u8bae',llmServiceProviderModels:'\u6a21\u578b',llmServiceProviderPriority:'\u4f18\u5148\u7ea7',llmServiceProviderConcurrency:'\u6700\u5927\u5e76\u53d1',llmServiceProviderCapabilities:'\u80fd\u529b\u6807\u7b7e',llmServiceAddGroup:'\u6dfb\u52a0\u670d\u52a1\u7ec4',llmServiceNoGroups:'\u672a\u914d\u7f6e\u670d\u52a1\u7ec4\u3002',llmServiceGroupName:'\u7ec4\u540d\u79f0',llmServiceGroupDesc:'\u63cf\u8ff0',llmServiceGroupModels:'\u6a21\u578b\u914d\u7f6e',llmServiceSave:'\u4fdd\u5b58',llmServiceCancel:'\u53d6\u6d88',sgRouteHint:'\u66b4\u9732\u6a21\u578b\u522b\u540d\uff0c\u6309\u670d\u52a1\u5546\u4f18\u5148\u7ea7\u5b9e\u73b0\u6545\u969c\u8f6c\u79fb',sgRemoveRoute:'\u5220\u9664',sgExposedModel:'\u66b4\u9732\u6a21\u578b\u540d',sgNoProviders:'\u672a\u5206\u914d\u670d\u52a1\u5546\u3002\u8bf7\u5728\u4e0a\u65b9\u6dfb\u52a0\u3002',sgAccessPolicy:'\u8bbf\u95ee\u7b56\u7565',sgPolicyFreeHint:'\u65e0\u9700\u6388\u6743',sgPolicyGrantHint:'\u9700\u8981\u5361/\u6388\u6743',sgRoutes:'\u670d\u52a1\u5546\u8def\u7531',sgAddRoute:'+ \u6dfb\u52a0\u8def\u7531',sgProviderAlreadyAdded:'\u8be5\u670d\u52a1\u5546\u5df2\u6dfb\u52a0\u3002',sgProviderConfigTitle:'\u670d\u52a1\u5546\u914d\u7f6e',sgCapabilityTags:'\u80fd\u529b\u6807\u7b7e',sgExtraTags:'\u989d\u5916\u6807\u7b7e\uff08\u81ea\u5b9a\u4e49\uff09',sgPriority:'\u4f18\u5148\u7ea7',sgResolutionTier:'\u89e3\u6790\u5c42\u7ea7',sgCreditMultiplier:'\u989d\u5ea6\u500d\u7387',sgIDNameRequired:'ID \u548c\u540d\u79f0\u4e0d\u80fd\u4e3a\u7a7a\u3002',sgRouteNeedsProvider:'\u6bcf\u4e2a\u8def\u7531\u81f3\u5c11\u9700\u8981\u4e00\u4e2a\u670d\u52a1\u5546\u3002',sgAvailableProviders:'\u53ef\u7528\u670d\u52a1\u5546',chooseProvider:'\u9009\u62e9\u670d\u52a1\u5546'});
  Object.assign(I18N_EN, {providerProbeModels:'Probe',providerProbing:'Probing models...',providerProbeEmpty:'No models returned.',providerProbeFailed:'Probe failed',providerCapabilityPreset:'Preset capabilities',testProvider:'Test',providerTesting:'Testing...',providerTestOK:'Available',providerTestFailed:'Unavailable',providerTestLatency:'Latency',providerTestModels:'Models'});
  Object.assign(I18N_ZH, {providerProbeModels:'\u63a2\u6d4b',providerProbing:'\u6b63\u5728\u63a2\u6d4b\u6a21\u578b...',providerProbeEmpty:'\u672a\u8fd4\u56de\u6a21\u578b\u5217\u8868\u3002',providerProbeFailed:'\u63a2\u6d4b\u5931\u8d25',providerCapabilityPreset:'\u9884\u7f6e\u80fd\u529b',testProvider:'\u6d4b\u8bd5',providerTesting:'\u6d4b\u8bd5\u4e2d...',providerTestOK:'\u53ef\u7528',providerTestFailed:'\u5f02\u5e38',providerTestLatency:'\u8017\u65f6',providerTestModels:'\u6a21\u578b'});
  Object.assign(I18N_EN, {llmServiceAgents:'Agents',llmServiceAgentsTitle:'Compute Agents',llmServiceAgentsDesc:'Manage upstream compute resellers for settlement.',llmServiceAddAgent:'Add Agent',llmServiceNoAgents:'No agents configured.'});
  Object.assign(I18N_ZH, {llmServiceAgents:'\u4ee3\u7406\u5546',llmServiceAgentsTitle:'\u7b97\u529b\u4ee3\u7406\u5546',llmServiceAgentsDesc:'\u7ba1\u7406\u4e0a\u6e38\u7b97\u529b\u4ee3\u7406\u4e0e\u7ed3\u7b97\u5f52\u5c5e\u3002',llmServiceAddAgent:'\u6dfb\u52a0\u4ee3\u7406\u5546',llmServiceNoAgents:'\u672a\u914d\u7f6e\u4ee3\u7406\u5546\u3002'});
}

(function() {
  'use strict';

  // ---------------------------------------------------------------------------
  // I18N
  // ---------------------------------------------------------------------------
  var I18N = {
    en: {
      llmTabTitle: 'LLM Service', llmTabDesc: 'Manage LLM providers, compute agents, and model service groups.',
      // Providers
      providersTitle: 'LLM Providers', providersDesc: 'Backend LLM API endpoints for model routing.',
      addProvider: 'Add Provider', editProvider: 'Edit', deleteProvider: 'Delete', noProviders: 'No providers configured.',
      providerDialogTitleNew: 'New Provider', providerDialogTitleEdit: 'Edit Provider',
      fieldID: 'Provider ID', fieldName: 'Name', fieldURL: 'API URL', fieldKey: 'API Key',
      fieldProtocol: 'Protocol', fieldModels: 'Models (comma-separated)', fieldCapabilities: 'Capabilities',
      fieldPriority: 'Priority', fieldConcurrency: 'Max Concurrency', fieldTimeout: 'Timeout (sec)',
      agentsTitle: 'Compute Agents', agentsDesc: 'Upstream compute resellers for settlement and customer-facing attribution.',
      addAgent: 'Add Agent', editAgent: 'Edit', deleteAgent: 'Delete', noAgents: 'No agents configured.',
      agentDialogTitleNew: 'New Compute Agent', agentDialogTitleEdit: 'Edit Compute Agent',
      fieldAgentID: 'Agent ID', fieldAgentName: 'Agent Name', fieldAgentContact: 'Contact', fieldAgentSettlement: 'Settlement',
      fieldAgentDesc: 'Description', fieldGroupAgent: 'Compute Agent', sgAgentRequired: 'Please select a compute agent.',
      // Service Groups
      groupsTitle: 'Model Service Groups', groupsDesc: 'Route models to providers with dispatch policies.',
      addGroup: 'Add Service Group', editGroup: 'Edit', deleteGroup: 'Delete', noGroups: 'No service groups.',
      groupDialogTitleNew: 'New Service Group', groupDialogTitleEdit: 'Edit Service Group',
      fieldGroupID: 'Group ID', fieldGroupName: 'Group Name', fieldGroupDesc: 'Description',
      fieldGroupModels: 'Models (JSON)', modelNamePlaceholder: 'e.g. gpt-4, claude-3.5',
      // Service Group Dialog
      sgRouteHint: 'Exposed model alias with provider failover',
      sgRemoveRoute: 'Remove', sgExposedModel: 'Exposed Model', sgNoProviders: 'No providers assigned. Add a provider above.',
      sgAccessPolicy: 'Access Policy', sgPolicyFreeHint: 'no grant needed', sgPolicyGrantHint: 'needs card/grant',
      sgRoutes: 'Provider Routes', sgAddRoute: '+ Add Route',
      sgProviderAlreadyAdded: 'Provider already added to this route.',
      sgProviderConfigTitle: 'Provider Config', sgCapabilityTags: 'Capability Tags',
      sgExtraTags: 'Extra Tags (custom)', sgPriority: 'Priority',
      sgResolutionTier: 'Resolution Tier', sgCreditMultiplier: 'Credit Multiplier',
      sgIDNameRequired: 'ID and Name are required.', sgRouteNeedsProvider: 'Each route needs at least one provider.',
      chooseProvider: 'Choose Provider',
      fieldHubID: 'Hub ID', fieldTenantID: 'Tenant ID',
      fieldHubRequired: 'Select a Hub', fieldTenantRequired: 'Select a tenant', noHubs: 'No registered Hubs.', defaultTenant: 'Default tenant',
      // Common
      save: 'Save', cancel: 'Cancel', confirm: 'Confirm', delete: 'Delete',
      saved: 'Saved successfully.', deleted: 'Deleted.', error: 'Error',
      status: 'Status', credits: 'Credits', expires: 'Expires', active: 'Active',
    },
    zh: {
      llmTabTitle: 'LLM \u670d\u52a1', llmTabDesc: '\u7ba1\u7406 LLM \u670d\u52a1\u5546\u3001\u7b97\u529b\u4ee3\u7406\u5546\u548c\u6a21\u578b\u670d\u52a1\u7ec4\u3002',
      providersTitle: 'LLM \u63a5\u5165', providersDesc: '\u540e\u7aef LLM API \u7aef\u70b9\u914d\u7f6e\u3002',
      addProvider: '\u6dfb\u52a0\u670d\u52a1\u5546', editProvider: '\u7f16\u8f91', deleteProvider: '\u5220\u9664', noProviders: '\u672a\u914d\u7f6e\u670d\u52a1\u5546\u3002',
      providerDialogTitleNew: '\u65b0\u5efa\u670d\u52a1\u5546', providerDialogTitleEdit: '\u7f16\u8f91\u670d\u52a1\u5546',
      fieldID: '\u670d\u52a1\u5546 ID', fieldName: '\u540d\u79f0', fieldURL: 'API \u5730\u5740', fieldKey: 'API \u5bc6\u94a5',
      fieldProtocol: '\u534f\u8bae', fieldModels: '\u6a21\u578b\uff08\u9017\u53f7\u5206\u9694\uff09', fieldCapabilities: '\u80fd\u529b\u6807\u7b7e',
      fieldPriority: '\u4f18\u5148\u7ea7', fieldConcurrency: '\u6700\u5927\u5e76\u53d1', fieldTimeout: '\u8d85\u65f6\uff08\u79d2\uff09',
      agentsTitle: '\u7b97\u529b\u4ee3\u7406\u5546', agentsDesc: '\u7528\u4e8e\u7ed3\u7b97\u548c\u5ba2\u6237\u4fa7\u5c55\u793a\u7684\u4e0a\u6e38\u7b97\u529b\u4ee3\u7406\u3002',
      addAgent: '\u6dfb\u52a0\u4ee3\u7406\u5546', editAgent: '\u7f16\u8f91', deleteAgent: '\u5220\u9664', noAgents: '\u672a\u914d\u7f6e\u4ee3\u7406\u5546\u3002',
      agentDialogTitleNew: '\u65b0\u5efa\u7b97\u529b\u4ee3\u7406\u5546', agentDialogTitleEdit: '\u7f16\u8f91\u7b97\u529b\u4ee3\u7406\u5546',
      fieldAgentID: '\u4ee3\u7406\u5546 ID', fieldAgentName: '\u4ee3\u7406\u5546\u540d\u79f0', fieldAgentContact: '\u8054\u7cfb\u65b9\u5f0f', fieldAgentSettlement: '\u7ed3\u7b97\u5907\u6ce8',
      fieldAgentDesc: '\u63cf\u8ff0', fieldGroupAgent: '\u7b97\u529b\u4ee3\u7406\u5546', sgAgentRequired: '\u8bf7\u9009\u62e9\u7b97\u529b\u4ee3\u7406\u5546\u3002',
      groupsTitle: '\u6a21\u578b\u670d\u52a1\u7ec4', groupsDesc: '\u5c06\u6a21\u578b\u8def\u7531\u5230\u670d\u52a1\u5546\u3002',
      addGroup: '\u6dfb\u52a0\u670d\u52a1\u7ec4', editGroup: '\u7f16\u8f91', deleteGroup: '\u5220\u9664', noGroups: '\u672a\u914d\u7f6e\u670d\u52a1\u7ec4\u3002',
      groupDialogTitleNew: '\u65b0\u5efa\u670d\u52a1\u7ec4', groupDialogTitleEdit: '\u7f16\u8f91\u670d\u52a1\u7ec4',
      fieldGroupID: '\u7ec4 ID', fieldGroupName: '\u7ec4\u540d\u79f0', fieldGroupDesc: '\u63cf\u8ff0',
      fieldGroupModels: '\u6a21\u578b\u914d\u7f6e (JSON)', modelNamePlaceholder: '\u5982 gpt-4, claude-3.5',
      // Service Group Dialog
      sgRouteHint: '\u66b4\u9732\u6a21\u578b\u522b\u540d\uff0c\u6309\u670d\u52a1\u5546\u4f18\u5148\u7ea7\u5b9e\u73b0\u6545\u969c\u8f6c\u79fb',
      sgRemoveRoute: '\u5220\u9664', sgExposedModel: '\u66b4\u9732\u6a21\u578b\u540d', sgNoProviders: '\u672a\u5206\u914d\u670d\u52a1\u5546\u3002\u8bf7\u5728\u4e0a\u65b9\u6dfb\u52a0\u3002',
      sgAccessPolicy: '\u8bbf\u95ee\u7b56\u7565', sgPolicyFreeHint: '\u65e0\u9700\u6388\u6743', sgPolicyGrantHint: '\u9700\u8981\u5361/\u6388\u6743',
      sgRoutes: '\u670d\u52a1\u5546\u8def\u7531', sgAddRoute: '+ \u6dfb\u52a0\u8def\u7531',
      sgProviderAlreadyAdded: '\u8be5\u670d\u52a1\u5546\u5df2\u6dfb\u52a0\u3002',
      sgProviderConfigTitle: '\u670d\u52a1\u5546\u914d\u7f6e', sgCapabilityTags: '\u80fd\u529b\u6807\u7b7e',
      sgExtraTags: '\u989d\u5916\u6807\u7b7e\uff08\u81ea\u5b9a\u4e49\uff09', sgPriority: '\u4f18\u5148\u7ea7',
      sgResolutionTier: '\u89e3\u6790\u5c42\u7ea7', sgCreditMultiplier: '\u989d\u5ea6\u500d\u7387',
      sgIDNameRequired: 'ID \u548c\u540d\u79f0\u4e0d\u80fd\u4e3a\u7a7a\u3002', sgRouteNeedsProvider: '\u6bcf\u4e2a\u8def\u7531\u81f3\u5c11\u9700\u8981\u4e00\u4e2a\u670d\u52a1\u5546\u3002',
      chooseProvider: '\u9009\u62e9\u670d\u52a1\u5546',
      fieldHubID: 'Hub ID', fieldTenantID: '\u79df\u6237 ID',
      fieldHubRequired: '\u8bf7\u9009\u62e9 Hub', fieldTenantRequired: '\u8bf7\u9009\u62e9\u79df\u6237', noHubs: '\u6682\u65e0\u5df2\u6ce8\u518c Hub\u3002', defaultTenant: '\u9ed8\u8ba4\u79df\u6237',
      save: '\u4fdd\u5b58', cancel: '\u53d6\u6d88', confirm: '\u786e\u8ba4', delete: '\u5220\u9664',
      saved: '\u4fdd\u5b58\u6210\u529f\u3002', deleted: '\u5df2\u5220\u9664\u3002', error: '\u9519\u8bef',
      status: '\u72b6\u6001', credits: '\u989d\u5ea6', expires: '\u6709\u6548\u671f', active: '\u6d3b\u8dc3',
    }
  };
  function t(k) { var l = (window.currentLang || 'en').startsWith('zh') ? 'zh' : 'en'; return (I18N[l]||I18N.en)[k] || I18N.en[k] || k; }
  function isZh() { return (window.currentLang || 'en').startsWith('zh'); }
  function esc(s) { return String(s||'').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;'); }

  // ---------------------------------------------------------------------------
  // API
  // ---------------------------------------------------------------------------
  function adminToken() { return (typeof window.token === 'function' ? window.token() : '') || localStorage.getItem('maclawHubCenterAdminToken') || sessionStorage.getItem('maclawHubCenterAdminToken') || ''; }
  function apiErrorMessage(e, fallback) {
    if (e && typeof e.error === 'object' && e.error && e.error.message) return e.error.message;
    if (e && typeof e.error === 'string') return e.error;
    return (e && e.message) || fallback;
  }
  async function api(path, opts) {
    if (typeof window.api === 'function') return window.api(path, opts || {});
    var token = adminToken();
    var headers = { 'Content-Type': 'application/json' };
    if (token) headers.Authorization = 'Bearer ' + token;
    var resp = await fetch(path, Object.assign({ headers: headers }, opts));
    if (!resp.ok) { var e = await resp.json().catch(function(){return{};}); throw new Error(apiErrorMessage(e, resp.statusText)); }
    return resp.json();
  }

  // ---------------------------------------------------------------------------
  // State
  // ---------------------------------------------------------------------------
  var providers = [], agents = [], serviceGroups = [];
  var providerTestStates = {};
  var providerDialogID = '';
  var providerCapabilityOptions = ['chat','streaming','json','tools','reasoning','vision','document','code','search','audio','embedding','rerank'];
  var llmInitInFlight = null;

  // ---------------------------------------------------------------------------
  // Tab Init
  // ---------------------------------------------------------------------------
  window.initLLMServiceTab = async function() {
    if (llmInitInFlight) return llmInitInFlight;
    llmInitInFlight = (async function() {
    // Re-apply i18n for dynamically registered keys
    if (typeof applyI18n === 'function') applyI18n();
    await Promise.all([loadProviders(), loadAgents(), loadServiceGroups()]);
    })();
    try { return await llmInitInFlight; }
    finally { llmInitInFlight = null; }
  };

  // ---------------------------------------------------------------------------
  // PROVIDERS
  // ---------------------------------------------------------------------------
  async function loadProviders() {
    try { var data = await api('/api/admin/llm/providers'); providers = data.providers || []; } catch(e) { providers = []; }
    renderProviders();
  }

  function renderProviders() {
    var el = document.getElementById('llmProvidersList');
    if (!el) return;
    if (!providers.length) { el.innerHTML = '<div class="hint">' + esc(t('noProviders')) + '</div>'; return; }
    el.innerHTML = providers.map(function(p) {
      var testState = providerTestStates[p.id];
      var testHTML = renderProviderTestState(testState);
      var testDisabled = testState && testState.status === 'testing' ? ' disabled' : '';
      return '<div class="data-row"><div class="data-row-main"><strong>' + esc(p.name||p.id) + '</strong>'
        + '<span class="data-row-meta">' + esc(p.api_url) + ' \u00b7 ' + esc(p.protocol||'openai')
        + (p.has_api_key ? ' \u00b7 \u{1f511}' : '') + '</span>'
        + testHTML + '</div>'
        + '<div class="data-row-actions">'
        + '<button class="btn-ghost provider-test-btn" onclick="testLLMProvider(\'' + esc(p.id) + '\')"' + testDisabled + '>' + esc(testState && testState.status === 'testing' ? t('providerTesting') : t('testProvider')) + '</button>'
        + '<button class="btn-ghost" onclick="editLLMProvider(\'' + esc(p.id) + '\')">' + esc(t('editProvider')) + '</button>'
        + '<button class="btn-danger-ghost" onclick="deleteLLMProvider(\'' + esc(p.id) + '\')">' + esc(t('deleteProvider')) + '</button>'
        + '</div></div>';
    }).join('');
  }

  function renderProviderTestState(state) {
    if (!state) return '';
    if (state.status === 'testing') {
      return '<span class="provider-test-status is-testing">' + esc(t('providerTesting')) + '</span>';
    }
    if (state.status === 'ok') {
      return '<span class="provider-test-status is-ok"><span class="badge ok">' + esc(t('providerTestOK')) + '</span> '
        + esc(t('providerTestLatency')) + ': ' + esc(state.latency_ms) + 'ms \u00b7 '
        + esc(t('providerTestModels')) + ': ' + esc(state.model_count) + '</span>';
    }
    return '<span class="provider-test-status is-error"><span class="badge danger">' + esc(t('providerTestFailed')) + '</span> ' + esc(state.message || '') + '</span>';
  }

  window.testLLMProvider = async function(id) {
    var provider = providers.find(function(p){ return p.id === id; });
    if (!provider) return;
    providerTestStates[id] = { status: 'testing' };
    renderProviders();
    var started = (typeof performance !== 'undefined' && performance.now) ? performance.now() : Date.now();
    try {
      var data = await api('/api/admin/llm/providers/probe-models', { method: 'POST', body: JSON.stringify({
        provider_id: provider.id,
        api_url: provider.api_url,
        protocol: provider.protocol || 'openai'
      }) });
      var ended = (typeof performance !== 'undefined' && performance.now) ? performance.now() : Date.now();
      providerTestStates[id] = { status: 'ok', latency_ms: Math.max(1, Math.round(ended - started)), model_count: (data.models || []).length };
      toast(t('providerTestOK') + ': ' + (provider.name || provider.id), 'success');
    } catch(e) {
      providerTestStates[id] = { status: 'error', message: e.message };
      toast(t('providerTestFailed') + ': ' + e.message, 'error');
    }
    renderProviders();
  };

  window.showProviderDialog = function(mode, id) {
    var p = mode === 'edit' ? providers.find(function(x){return x.id===id;}) : null;
    providerDialogID = mode === 'edit' ? (id || '') : '';
    var title = mode === 'edit' ? t('providerDialogTitleEdit') : t('providerDialogTitleNew');
    var html = '<h3>' + esc(title) + '</h3><div class="grid2">'
      + field('llmPrvID', t('fieldID'), p ? p.id : '', mode==='edit')
      + field('llmPrvName', t('fieldName'), p ? p.name : '')
      + '</div><div class="grid2">'
      + field('llmPrvURL', t('fieldURL'), p ? p.api_url : '')
      + field('llmPrvKey', t('fieldKey'), '', false, 'password')
      + '</div><div class="grid2">'
      + '<div><label>' + esc(t('fieldProtocol')) + '</label><select id="llmPrvProtocol"><option value="openai"' + ((!p||p.protocol==='openai')?' selected':'') + '>OpenAI</option><option value="anthropic"' + (p&&p.protocol==='anthropic'?' selected':'') + '>Anthropic</option></select></div>'
      + providerModelsField(p ? (p.models||[]).join(', ') : '')
      + '</div><div class="grid2">'
      + providerCapabilitiesField(p ? (p.capability_tags||[]).join(', ') : '')
      + field('llmPrvPriority', t('fieldPriority'), p ? String(p.priority||0) : '0', false, 'number')
      + '</div><div class="grid2">'
      + field('llmPrvConc', t('fieldConcurrency'), p ? String(p.max_concurrency||10) : '10', false, 'number')
      + field('llmPrvTimeout', t('fieldTimeout'), p ? String(p.upstream_timeout_sec||120) : '120', false, 'number')
      + '</div><div class="actions section-gap">'
      + '<button class="btn-primary" onclick="saveProvider(\'' + (mode==='edit'?esc(id):'') + '\')">' + esc(t('save')) + '</button>'
      + '<button class="btn-ghost" onclick="closeDialog()">' + esc(t('cancel')) + '</button></div>';
    openDialog(html);
    window.renderProviderCapabilityChips();
  };
  window.editLLMProvider = function(id) { window.showProviderDialog('edit', id); };

  function providerModelsField(value) {
    return '<div><label for="llmPrvModels">' + esc(t('fieldModels')) + '</label>'
      + '<div class="provider-model-tools"><input id="llmPrvModels" list="llmPrvModelOptions" value="' + esc(value || '') + '" placeholder="gpt-4o, deepseek-chat"><button class="btn-ghost provider-probe-btn" type="button" onclick="probeProviderModels()">' + esc(t('providerProbeModels')) + '</button></div>'
      + '<datalist id="llmPrvModelOptions"></datalist><div id="llmPrvModelChoices" class="provider-model-results"></div><div id="llmPrvProbeStatus" class="provider-probe-status"></div></div>';
  }

  function providerCapabilitiesField(value) {
    return '<div class="provider-cap-field"><label for="llmPrvCaps">' + esc(t('fieldCapabilities')) + '</label>'
      + '<div id="llmPrvCapChips" class="provider-cap-picker" aria-label="' + esc(t('providerCapabilityPreset')) + '"></div>'
      + '<input id="llmPrvCaps" value="' + esc(value || '') + '" placeholder="tools, vision, reasoning" oninput="renderProviderCapabilityChips()"></div>';
  }

  function csvValues(id) {
    return csv(id).map(function(v){return v.trim();}).filter(Boolean);
  }

  function setCSVValues(id, values) {
    var el = document.getElementById(id);
    if (!el) return;
    var seen = {};
    el.value = (values || []).map(function(v){return String(v || '').trim();}).filter(function(v){if(!v || seen[v]) return false; seen[v] = true; return true;}).join(', ');
  }

  function addCSVValue(id, value) {
    value = String(value || '').trim();
    if (!value) return;
    var values = csvValues(id);
    if (values.indexOf(value) < 0) values.push(value);
    setCSVValues(id, values);
  }

  window.addProviderModel = function(model) {
    addCSVValue('llmPrvModels', model);
  };

  window.toggleProviderCapability = function(cap) {
    var values = csvValues('llmPrvCaps');
    var idx = values.indexOf(cap);
    if (idx >= 0) values.splice(idx, 1);
    else values.push(cap);
    setCSVValues('llmPrvCaps', values);
    renderProviderCapabilityChips();
  };

  window.renderProviderCapabilityChips = function() {
    var root = document.getElementById('llmPrvCapChips');
    if (!root) return;
    var active = csvValues('llmPrvCaps');
    root.innerHTML = providerCapabilityOptions.map(function(cap) {
      var on = active.indexOf(cap) >= 0;
      return '<button type="button" class="provider-cap-chip' + (on ? ' is-active' : '') + '" onclick="toggleProviderCapability(\'' + esc(cap) + '\')">' + esc(cap) + '</button>';
    }).join('');
  };

  window.probeProviderModels = async function() {
    var status = document.getElementById('llmPrvProbeStatus');
    var choices = document.getElementById('llmPrvModelChoices');
    var list = document.getElementById('llmPrvModelOptions');
    if (status) status.textContent = t('providerProbing');
    if (choices) choices.innerHTML = '';
    try {
      var data = await api('/api/admin/llm/providers/probe-models', { method: 'POST', body: JSON.stringify({
        provider_id: providerDialogID,
        api_url: val('llmPrvURL'),
        api_key: val('llmPrvKey'),
        protocol: val('llmPrvProtocol') || 'openai'
      }) });
      var models = data.models || [];
      if (list) list.innerHTML = models.map(function(m){ return '<option value="' + esc(m) + '"></option>'; }).join('');
      if (choices) choices.innerHTML = models.map(function(m){ return '<button type="button" class="provider-model-choice" onclick="addProviderModel(\'' + esc(m) + '\')">' + esc(m) + '</button>'; }).join('');
      if (status) status.textContent = models.length ? '' : t('providerProbeEmpty');
    } catch(e) {
      if (status) status.textContent = t('providerProbeFailed') + ': ' + e.message;
    }
  };

  window.saveProvider = async function(editID) {
    var payload = {
      id: val('llmPrvID'), name: val('llmPrvName'), api_url: val('llmPrvURL'),
      protocol: val('llmPrvProtocol'),
      models: csv('llmPrvModels'), capability_tags: csv('llmPrvCaps'),
      priority: num('llmPrvPriority'), max_concurrency: num('llmPrvConc'),
      upstream_timeout_sec: num('llmPrvTimeout'),
    };
    var key = val('llmPrvKey');
    if (key) payload.api_key = key;
    try {
      if (editID) { await api('/api/admin/llm/providers/' + editID, { method: 'PUT', body: JSON.stringify(payload) }); }
      else { await api('/api/admin/llm/providers', { method: 'POST', body: JSON.stringify(payload) }); }
      closeDialog(); toast(t('saved'), 'success'); loadProviders();
    } catch(e) { toast(e.message, 'error'); }
  };

  window.deleteLLMProvider = async function(id) {
    if (!confirm(t('deleteProvider') + ': ' + id + '?')) return;
    try { await api('/api/admin/llm/providers/' + id, { method: 'DELETE' }); toast(t('deleted'), 'success'); loadProviders(); }
    catch(e) { toast(e.message, 'error'); }
  };

  // ---------------------------------------------------------------------------
  // COMPUTE AGENTS
  // ---------------------------------------------------------------------------
  async function loadAgents() {
    try { var data = await api('/api/admin/llm/agents'); agents = data.agents || []; } catch(e) { agents = []; }
    renderAgents();
  }

  function renderAgents() {
    var el = document.getElementById('llmAgentsList');
    if (!el) return;
    if (!agents.length) { el.innerHTML = '<div class="hint">' + esc(t('noAgents')) + '</div>'; return; }
    el.innerHTML = agents.map(function(a) {
      var locked = a.id === 'maclaw_official';
      var status = a.enabled === false ? '<span class="badge warn">Disabled</span>' : '<span class="badge ok">Enabled</span>';
      return '<div class="data-row llm-agent-row"><div class="data-row-main"><strong>' + esc(a.name || a.id) + '</strong> ' + status
        + '<span class="data-row-meta">' + esc(a.id) + (a.contact ? ' · ' + esc(a.contact) : '') + (a.description ? ' · ' + esc(a.description) : '') + '</span></div>'
        + '<div class="data-row-actions">'
        + '<button class="btn-ghost" onclick="showLLMAgentDialog(\'edit\',\'' + esc(a.id) + '\')">' + esc(t('editAgent')) + '</button>'
        + (locked ? '' : '<button class="btn-danger-ghost" onclick="deleteLLMAgent(\'' + esc(a.id) + '\')">' + esc(t('deleteAgent')) + '</button>')
        + '</div></div>';
    }).join('');
  }

  window.showLLMAgentDialog = function(mode, id) {
    var a = mode === 'edit' ? agents.find(function(x){return x.id===id;}) : null;
    var html = '<h3>' + esc(mode === 'edit' ? t('agentDialogTitleEdit') : t('agentDialogTitleNew')) + '</h3>'
      + '<div class="grid2">'
      + field('llmAgentID', t('fieldAgentID'), a ? a.id : '', mode === 'edit')
      + field('llmAgentName', t('fieldAgentName'), a ? a.name : '')
      + '</div><div class="grid2">'
      + field('llmAgentContact', t('fieldAgentContact'), a ? a.contact : '')
      + field('llmAgentSettlement', t('fieldAgentSettlement'), a ? a.settlement : '')
      + '</div>'
      + '<div class="sg-block-xs"><label for="llmAgentDesc">' + esc(t('fieldAgentDesc')) + '</label><textarea id="llmAgentDesc" rows="3">' + esc(a ? a.description : '') + '</textarea></div>'
      + '<div class="actions section-gap"><button class="btn-primary" onclick="saveLLMAgent(\'' + (mode === 'edit' ? esc(id) : '') + '\')">' + esc(t('save')) + '</button><button class="btn-ghost" onclick="closeDialog()">' + esc(t('cancel')) + '</button></div>';
    openDialog(html);
  };

  window.saveLLMAgent = async function(editID) {
    var payload = { id: val('llmAgentID'), name: val('llmAgentName'), contact: val('llmAgentContact'), settlement: val('llmAgentSettlement'), description: val('llmAgentDesc'), enabled: true };
    try {
      if (editID) await api('/api/admin/llm/agents/' + editID, { method: 'PUT', body: JSON.stringify(payload) });
      else await api('/api/admin/llm/agents', { method: 'POST', body: JSON.stringify(payload) });
      closeDialog(); toast(t('saved'), 'success'); await loadAgents(); await loadServiceGroups();
    } catch(e) { toast(e.message, 'error'); }
  };

  window.deleteLLMAgent = async function(id) {
    if (!confirm(t('deleteAgent') + ': ' + id + '?')) return;
    try { await api('/api/admin/llm/agents/' + id, { method: 'DELETE' }); toast(t('deleted'), 'success'); await loadAgents(); await loadServiceGroups(); }
    catch(e) { toast(e.message, 'error'); }
  };

  // ---------------------------------------------------------------------------
  // SERVICE GROUPS
  // ---------------------------------------------------------------------------
  async function loadServiceGroups() {
    try { var data = await api('/api/admin/llm/service-groups'); serviceGroups = data.service_groups || []; } catch(e) { serviceGroups = []; }
    renderServiceGroups();
  }

  function renderServiceGroups() {
    var el = document.getElementById('llmServiceGroupsList');
    if (!el) return;
    if (!serviceGroups.length) { el.innerHTML = '<div class="hint">' + esc(t('noGroups')) + '</div>'; return; }
    el.innerHTML = serviceGroups.map(function(g) {
      var modelNames = (g.models||[]).map(function(m){return m.name;}).join(', ');
      var policyBadge = g.access_policy === 'grant_required' ? '<span class="badge warn">'+esc(sgPolicyLabel('grant_required'))+'</span>' : '<span class="badge ok">'+esc(sgPolicyLabel('free'))+'</span>';
      var agentName = g.agent_name || agentNameByID(g.agent_id) || '-';
      return '<div class="data-row"><div class="data-row-main"><strong>' + esc(g.name||g.id) + '</strong> ' + policyBadge
        + '<span class="data-row-meta">' + esc(agentName) + ' \u00b7 ' + esc(g.description||'') + ' \u00b7 ' + esc(modelNames||'no models')
        + ' \u00b7 ' + (g.models||[]).length + ' route(s)</span></div>'
        + '<div class="data-row-actions">'
        + '<button class="btn-ghost" onclick="editLLMServiceGroup(\'' + esc(g.id) + '\')">' + esc(t('editGroup')) + '</button>'
        + '<button class="btn-danger-ghost" onclick="deleteLLMServiceGroup(\'' + esc(g.id) + '\')">' + esc(t('deleteGroup')) + '</button>'
        + '</div></div>';
    }).join('');
  }

  // --- Service Group Dialog (Hub-style routes + provider configs) ---
  var sgDraft = null, sgMode = 'create', sgProviderDraft = null;
  var sgCapabilityOptions = ['reasoning','tools','document','vision','audio','code','search'];
  var sgPriorityOptions = [0,10,20,30,40,50,60,70,80,90,100];
  var sgResolutionOptions = [0,1,2,3,4,5];
  var sgMultiplierOptions = [0.25,0.5,0.75,1,1.5,2,3,5,10];
  function agentNameByID(id){var a=agents.find(function(x){return x.id===id;});return a&&(a.name||a.id);}
  function sgPolicyLabel(policy){return policy==='grant_required'?(isZh()?'\u9700\u5151\u6362\u5361':'Card Required'):(isZh()?'\u514d\u8d39\u901a\u884c':'Free Access');}

  function sgProviderIDsFromModel(m) {
    var ids = [];
    (m && m.provider_ids || []).forEach(function(id){ if (id && ids.indexOf(id) < 0) ids.push(id); });
    (m && m.provider_configs || []).forEach(function(c){ if (c && c.provider_id && ids.indexOf(c.provider_id) < 0) ids.push(c.provider_id); });
    return ids;
  }

  function sgCloneGroup(g) {
    return {id:(g&&g.id||'').trim(),name:(g&&g.name||'').trim(),description:(g&&g.description||'').trim(),
      agent_id:(g&&g.agent_id)||'maclaw_official',agent_name:(g&&g.agent_name)||agentNameByID(g&&g.agent_id)||'',
      access_policy:g&&g.access_policy||'free',
      models:(g&&g.models||[]).map(function(m){return{name:m.name||'auto',provider_ids:sgProviderIDsFromModel(m),provider_configs:(m.provider_configs||[]).map(function(c){return{provider_id:c.provider_id,capability_tags:(c.capability_tags||[]).slice(),priority:c.priority||0,resolution_tier:c.resolution_tier||0,credit_multiplier:c.credit_multiplier||1};}),capability_tags:(m.capability_tags||[]).slice(),priority:m.priority||50,resolution_tier:m.resolution_tier||0,credit_multiplier:m.credit_multiplier||1};})};
  }
  function sgEmptyGroup(){return{id:'',name:'',description:'',agent_id:'maclaw_official',agent_name:agentNameByID('maclaw_official')||'MaClaw官方',access_policy:'free',models:[{name:'auto',provider_ids:[],provider_configs:[],capability_tags:[],priority:50,resolution_tier:0,credit_multiplier:1}]};}
  function sgProviderName(id){var p=providers.find(function(x){return x.id===id;});return p?(p.name||p.id):id;}
  function sgGetProviderConfig(model,providerID){if(!model)return null;model.provider_configs=model.provider_configs||[];var existing=model.provider_configs.find(function(c){return c.provider_id===providerID;});if(existing)return existing;var cfg={provider_id:providerID,capability_tags:[],priority:0,resolution_tier:0,credit_multiplier:1};model.provider_configs.push(cfg);return cfg;}

  function sgRenderProviderCard(rowIndex,providerID,providerIndex,total){
    var model=sgDraft&&sgDraft.models&&sgDraft.models[rowIndex];
    var cfg=sgGetProviderConfig(model,providerID);
    var features=(cfg&&cfg.capability_tags||[]).join(', ')||'-';
    return '<div class="sg-provider-card">'
      +'<div class="sg-row-head">'
      +'<strong>'+esc(sgProviderName(providerID))+' #'+(providerIndex+1)+'</strong>'
      +'<div class="sg-actions">'
      +'<button class="btn-ghost sg-tiny-btn" onclick="sgEditProviderConfig('+rowIndex+',\''+esc(providerID)+'\')">'+esc(t('editGroup'))+'</button>'
      +(providerIndex>0?'<button class="btn-ghost sg-icon-btn" onclick="sgMoveProvider('+rowIndex+',\''+esc(providerID)+'\',-1)">\u2191</button>':'')
      +(providerIndex<total-1?'<button class="btn-ghost sg-icon-btn" onclick="sgMoveProvider('+rowIndex+',\''+esc(providerID)+'\',1)">\u2193</button>':'')
      +'<button class="btn-danger-ghost sg-tiny-btn" onclick="sgRemoveProvider('+rowIndex+',\''+esc(providerID)+'\')">\u2715</button>'
      +'</div></div>'
      +'<div class="sg-provider-meta">'+esc(t('fieldCapabilities'))+': '+esc(features)+' \u00b7 P:'+(cfg&&cfg.priority||0)+' \u00b7 T:'+(cfg&&cfg.resolution_tier||0)+' \u00b7 x'+(cfg&&cfg.credit_multiplier||1)+'</div>'
      +'</div>';
  }

  function sgRenderRouteRow(model,rowIndex){
    var providerCards=(model.provider_ids||[]).map(function(pid,pi){return sgRenderProviderCard(rowIndex,pid,pi,(model.provider_ids||[]).length);}).join('');
    var providerOptions=!providers.length?'<option value="">('+esc(t('noProviders'))+')</option>'
      :'<option value="">-- '+esc(t('chooseProvider'))+' --</option>'+providers.map(function(p){return'<option value="'+esc(p.id)+'">'+esc(p.name||p.id)+'</option>';}).join('');
    return '<div class="sg-route-card">'
      +'<div class="sg-row-head">'
      +'<div><strong>Route #'+(rowIndex+1)+'</strong><span class="sg-route-hint">'+esc(t('sgRouteHint'))+'</span></div>'
      +'<button class="btn-danger-ghost sg-remove-route" onclick="sgRemoveRoute('+rowIndex+')">'+esc(t('sgRemoveRoute'))+'</button>'
      +'</div>'
      +'<div class="sg-route-grid">'
      +'<div><label class="sg-label-sm">'+esc(t('sgExposedModel'))+'</label><input class="sg-field-full" value="'+esc(model.name||'auto')+'" placeholder="auto" oninput="sgSetRouteField('+rowIndex+',\'name\',this.value)"></div>'
      +'<div class="sg-provider-add"><select id="sgProviderAdd'+rowIndex+'">'+providerOptions+'</select><button class="btn-ghost" onclick="sgAddProviderToRoute('+rowIndex+')">+</button></div>'
      +'</div>'
      +(providerCards||'<div class="sg-empty-provider">'+esc(t('sgNoProviders'))+'</div>')
      +'</div>';
  }

  function sgRenderGroupDialog(){
    var d=sgDraft||sgEmptyGroup();
    var rows=(d.models||[]).map(function(m,i){return sgRenderRouteRow(m,i);}).join('');
    var title=sgMode==='edit'?t('groupDialogTitleEdit'):t('groupDialogTitleNew');
    var agentOptions = agents.map(function(a){return '<option value="'+esc(a.id)+'"'+(d.agent_id===a.id?' selected':'')+'>'+esc(a.name||a.id)+'</option>';}).join('');
    var html='<h3>'+esc(title)+'</h3>'
      +'<div class="grid2 sg-block-sm">'
      +'<div><label>'+esc(t('fieldGroupID'))+'</label><input id="sgFieldID" value="'+esc(d.id)+'" placeholder="e.g. coding-pro"'+(sgMode==='edit'?' readonly class="sg-readonly"':'')+' oninput="sgSetField(\'id\',this.value)"></div>'
      +'<div><label>'+esc(t('fieldGroupName'))+'</label><input value="'+esc(d.name)+'" placeholder="e.g. Coding Pro" oninput="sgSetField(\'name\',this.value)"></div>'
      +'</div>'
      +'<div class="sg-block-xs"><label>'+esc(t('fieldGroupAgent'))+'</label><select class="sg-field-full" onchange="sgSetField(\'agent_id\',this.value)"><option value="">--</option>'+agentOptions+'</select></div>'
      +'<div class="sg-block-xs"><label>'+esc(t('fieldGroupDesc'))+'</label><input class="sg-field-full" value="'+esc(d.description)+'" oninput="sgSetField(\'description\',this.value)"></div>'
      +'<div class="sg-block-xs"><label>'+esc(t('sgAccessPolicy'))+'</label><select onchange="sgSetField(\'access_policy\',this.value)"><option value="free"'+(d.access_policy!=='grant_required'?' selected':'')+'>'+esc(sgPolicyLabel('free'))+' ('+esc(t('sgPolicyFreeHint'))+')</option><option value="grant_required"'+(d.access_policy==='grant_required'?' selected':'')+'>'+esc(sgPolicyLabel('grant_required'))+' ('+esc(t('sgPolicyGrantHint'))+')</option></select></div>'
      +'<div class="sg-block-md"><div class="sg-flex-between"><strong>'+esc(t('sgRoutes'))+'</strong><button class="btn-ghost" onclick="sgAddRoute()">'+esc(t('sgAddRoute'))+'</button></div>'
      +rows+'</div>'
      +'<div class="actions sg-block-md"><button class="btn-primary" onclick="sgSaveGroup()">'+esc(t('save'))+'</button><button class="btn-ghost" onclick="closeDialog()">'+esc(t('cancel'))+'</button></div>';
    openDialog(html);
  }

  window.showGroupDialog=function(mode,id){var g=mode==='edit'?serviceGroups.find(function(x){return x.id===id;}):null;sgMode=mode==='edit'?'edit':'create';sgDraft=g?sgCloneGroup(g):sgEmptyGroup();sgRenderGroupDialog();};
  window.editLLMServiceGroup=function(id){window.showGroupDialog('edit',id);};
  window.showLLMGroupEditor=function(){window.showGroupDialog('create');};
  window.sgSetField=function(k,v){if(sgDraft)sgDraft[k]=v.trim();};
  window.sgSetRouteField=function(i,k,v){if(sgDraft&&sgDraft.models&&sgDraft.models[i])sgDraft.models[i][k]=v.trim();};
  window.sgAddRoute=function(){if(!sgDraft)sgDraft=sgEmptyGroup();sgDraft.models.push({name:'auto',provider_ids:[],provider_configs:[],capability_tags:[],priority:50,resolution_tier:0,credit_multiplier:1});sgRenderGroupDialog();};
  window.sgRemoveRoute=function(i){if(sgDraft&&sgDraft.models){sgDraft.models.splice(i,1);sgRenderGroupDialog();}};
  window.sgAddProviderToRoute=function(i){var sel=document.getElementById('sgProviderAdd'+i);var id=sel&&sel.value;if(!id){toast(t('chooseProvider'),'info');return;}var m=sgDraft&&sgDraft.models&&sgDraft.models[i];if(!m)return;if((m.provider_ids||[]).indexOf(id)>=0){toast(t('sgProviderAlreadyAdded'),'info');return;}m.provider_ids=(m.provider_ids||[]).concat([id]);sgGetProviderConfig(m,id);sgRenderGroupDialog();};
  window.sgMoveProvider=function(i,pid,delta){var m=sgDraft&&sgDraft.models&&sgDraft.models[i];if(!m)return;var list=m.provider_ids||[];var from=list.indexOf(pid);if(from<0)return;var to=from+delta;if(to<0||to>=list.length)return;list.splice(from,1);list.splice(to,0,pid);sgRenderGroupDialog();};
  window.sgRemoveProvider=function(i,pid){var m=sgDraft&&sgDraft.models&&sgDraft.models[i];if(!m)return;m.provider_ids=(m.provider_ids||[]).filter(function(v){return v!==pid;});m.provider_configs=(m.provider_configs||[]).filter(function(c){return c.provider_id!==pid;});sgRenderGroupDialog();};

  // Provider config sub-dialog
  window.sgEditProviderConfig=function(rowIndex,providerID){var model=sgDraft&&sgDraft.models&&sgDraft.models[rowIndex];var cfg=sgGetProviderConfig(model,providerID);sgProviderDraft={rowIndex:rowIndex,providerID:providerID,draft:{capability_tags:(cfg&&cfg.capability_tags||[]).slice(),priority:cfg&&cfg.priority||0,resolution_tier:cfg&&cfg.resolution_tier||0,credit_multiplier:cfg&&cfg.credit_multiplier||1}};sgRenderProviderDialog();};

  function sgRenderProviderDialog(){
    if(!sgProviderDraft)return;var d=sgProviderDraft.draft;
    var featureChecks=sgCapabilityOptions.map(function(f){var checked=(d.capability_tags||[]).indexOf(f)>=0?' checked':'';return'<label class="sg-feature-check"><input type="checkbox"'+checked+' onchange="sgToggleFeature(\''+f+'\',this.checked)">'+esc(f)+'</label>';}).join('');
    var extraTags=(d.capability_tags||[]).filter(function(v){return sgCapabilityOptions.indexOf(v)<0;}).join(', ');
    var html='<h3>'+esc(t('sgProviderConfigTitle'))+': '+esc(sgProviderName(sgProviderDraft.providerID))+'</h3>'
      +'<div class="sg-block-sm"><label class="sg-label-strong">'+esc(t('sgCapabilityTags'))+'</label><div class="sg-block-xs">'+featureChecks+'</div></div>'
      +'<div class="sg-block-xs"><label class="sg-label-sm">'+esc(t('sgExtraTags'))+'</label><input class="sg-field-full" value="'+esc(extraTags)+'" placeholder="custom1, custom2" oninput="sgSetExtraTags(this.value)"></div>'
      +'<div class="grid2 sg-block-sm">'
      +'<div><label class="sg-label-sm">'+esc(t('sgPriority'))+'</label><select onchange="sgSetProviderField(\'priority\',Number(this.value))">'+sgPriorityOptions.map(function(v){return'<option value="'+v+'"'+(d.priority===v?' selected':'')+'>'+v+'</option>';}).join('')+'</select></div>'
      +'<div><label class="sg-label-sm">'+esc(t('sgResolutionTier'))+'</label><select onchange="sgSetProviderField(\'resolution_tier\',Number(this.value))">'+sgResolutionOptions.map(function(v){return'<option value="'+v+'"'+(d.resolution_tier===v?' selected':'')+'>'+v+'</option>';}).join('')+'</select></div>'
      +'</div>'
      +'<div class="sg-block-xs"><label class="sg-label-sm">'+esc(t('sgCreditMultiplier'))+'</label><select onchange="sgSetProviderField(\'credit_multiplier\',Number(this.value))">'+sgMultiplierOptions.map(function(v){return'<option value="'+v+'"'+(d.credit_multiplier===v?' selected':'')+'>'+v+'</option>';}).join('')+'</select></div>'
      +'<div class="actions sg-block-actions"><button class="btn-primary" onclick="sgSaveProviderConfig()">'+esc(t('save'))+'</button><button class="btn-ghost" onclick="sgCancelProviderConfig()">'+esc(t('cancel'))+'</button></div>';
    openDialog(html);
  }

  window.sgToggleFeature=function(f,on){if(!sgProviderDraft)return;var s=new Set(sgProviderDraft.draft.capability_tags||[]);if(on)s.add(f);else s.delete(f);sgProviderDraft.draft.capability_tags=Array.from(s);};
  window.sgSetExtraTags=function(v){if(!sgProviderDraft)return;var keep=(sgProviderDraft.draft.capability_tags||[]).filter(function(x){return sgCapabilityOptions.indexOf(x)>=0;});var extra=v.split(/[,;\s]+/).map(function(x){return x.trim();}).filter(Boolean);sgProviderDraft.draft.capability_tags=Array.from(new Set(keep.concat(extra)));};
  window.sgSetProviderField=function(k,v){if(sgProviderDraft)sgProviderDraft.draft[k]=v;};
  window.sgSaveProviderConfig=function(){if(!sgProviderDraft||!sgDraft)return;var model=sgDraft.models&&sgDraft.models[sgProviderDraft.rowIndex];if(!model)return;var cfg=sgGetProviderConfig(model,sgProviderDraft.providerID);cfg.capability_tags=(sgProviderDraft.draft.capability_tags||[]).slice();cfg.priority=sgProviderDraft.draft.priority||0;cfg.resolution_tier=sgProviderDraft.draft.resolution_tier||0;cfg.credit_multiplier=sgProviderDraft.draft.credit_multiplier||1;sgProviderDraft=null;sgRenderGroupDialog();};
  window.sgCancelProviderConfig=function(){sgProviderDraft=null;sgRenderGroupDialog();};

  window.sgSaveGroup=async function(){
    if(!sgDraft||!sgDraft.id||!sgDraft.name){toast(t('sgIDNameRequired'),'error');return;}
    if(!sgDraft.agent_id){toast(t('sgAgentRequired'),'error');return;}
    if(sgMode!=='edit'){
      for(var r=0;r<(sgDraft.models||[]).length;r++){if(!(sgProviderIDsFromModel(sgDraft.models[r])||[]).length){toast(t('sgRouteNeedsProvider'),'error');return;}}
    }
    var payload=sgCloneGroup(sgDraft);
    for(var i=0;i<(payload.models||[]).length;i++){
      var model=payload.models[i];
      if(!(model.provider_ids||[]).length&&(model.provider_configs||[]).length){
        model.provider_ids=sgProviderIDsFromModel(model);
      }
      if((model.provider_ids||[]).length&&!model.provider_configs.length){
        model.provider_configs=model.provider_ids.map(function(pid){return{provider_id:pid,capability_tags:[],priority:0,resolution_tier:0,credit_multiplier:1};});
      }
    }
    try{
      if(sgMode==='edit'){await api('/api/admin/llm/service-groups/'+payload.id,{method:'PUT',body:JSON.stringify(payload)});}
      else{await api('/api/admin/llm/service-groups',{method:'POST',body:JSON.stringify(payload)});}
      closeDialog();toast(t('saved'),'success');loadServiceGroups();
    }catch(e){toast(e.message,'error');}
  };

  window.deleteLLMServiceGroup = async function(id) {
    if (!confirm(t('deleteGroup') + ': ' + id + '?')) return;
    try { await api('/api/admin/llm/service-groups/' + id, { method: 'DELETE' }); toast(t('deleted'), 'success'); loadServiceGroups(); }
    catch(e) { toast(e.message, 'error'); }
  };

  // ---------------------------------------------------------------------------
  // Dialog helpers
  // ---------------------------------------------------------------------------
  function openDialog(html) {
    var overlay = document.getElementById('llmDialogOverlay');
    if (!overlay) {
      overlay = document.createElement('div');
      overlay.id = 'llmDialogOverlay';
      overlay.className = 'session-modal-overlay';
      overlay.innerHTML = '<div class="session-modal cm-dialog-lg" id="llmDialogContent"></div>';
      if (typeof window.installOverlayDismiss === 'function') {
        window.installOverlayDismiss(overlay, closeDialog);
      } else {
        var startedOnOverlay = false;
        overlay.addEventListener('pointerdown', function(e) { startedOnOverlay = e.target === overlay; });
        overlay.addEventListener('click', function(e) { if (startedOnOverlay && e.target === overlay) closeDialog(); startedOnOverlay = false; });
      }
      document.body.appendChild(overlay);
    }
    document.getElementById('llmDialogContent').innerHTML = html;
    overlay.classList.add('show');
  }
  function closeDialog() { var o = document.getElementById('llmDialogOverlay'); if (o) o.classList.remove('show'); }
  window.closeDialog = closeDialog;

  function field(id, label, value, readonly, type) {
    return '<div><label for="' + id + '">' + esc(label) + '</label><input id="' + id + '" type="' + (type||'text') + '" value="' + esc(value||'') + '"' + (readonly ? ' readonly class="sg-readonly"' : '') + '></div>';
  }
  // ---------------------------------------------------------------------------
  // Sub-tab switching
  // ---------------------------------------------------------------------------

  window.switchLLMSubTab = function(tab) {
    ['providers', 'agents', 'groups'].forEach(function(t) {
      var view = document.getElementById('llmSubView' + t.charAt(0).toUpperCase() + t.slice(1));
      var btn = document.getElementById('llmSubTab' + t.charAt(0).toUpperCase() + t.slice(1));
      var active = (t === tab);
      if (view) view.classList.toggle('hidden-view', !active);
      if (btn) {
        btn.className = active ? 'btn-secondary' : 'btn-ghost';
        btn.setAttribute('aria-pressed', String(active));
      }
    });
  };

  // ---------------------------------------------------------------------------
  // Inline editor functions (called by HTML onclick)
  // ---------------------------------------------------------------------------

  // Legacy inline editor stubs (now handled via modal dialogs)
  window.showLLMProviderEditor = function() { window.showProviderDialog('create'); };
  window.hideLLMProviderEditor = function() { closeDialog(); };
  window.showLLMGroupEditor = window.showLLMGroupEditor || function() { window.showGroupDialog('create'); };
  window.hideLLMGroupEditor = function() { closeDialog(); };

  function val(id) { var el = document.getElementById(id); return el ? el.value.trim() : ''; }
  function num(id) { return Number(val(id)) || 0; }
  function csv(id) { return val(id).split(/[,\uff0c]+/).map(function(s){return s.trim();}).filter(Boolean); }
  function toast(msg, type) { if (window.showToast) window.showToast(msg, type); else alert(msg); }

  if (document.getElementById('tab-llmservice')?.classList.contains('active')) {
    setTimeout(window.initLLMServiceTab, 0);
  }

})();

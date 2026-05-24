/* * LLM provider admin extension. * ASCII only. Chinese text must use \uXXXX escapes. */const LLM_PROVIDER_I18N = {  en: {    navLabel: 'LLM EndPoint', navDesc: 'Endpoint routing and unified API', tabTitle: 'LLM EndPoint', tabSubtitle: 'Configure LLM endpoints, token usage, connection tests, and the unified OpenAI v1 endpoint.', reload: 'Reload', enabled: 'Enable unified LLM service', smartRoute: 'Smart route single-device LLM', defaultProvider: 'Default provider', exposeTitle: 'Unified OpenAI v1 Endpoint', exposeDesc: 'Select provider by `model`, `X-LLM-Provider`, or `?provider=`.', apiBaseUrl: 'API Base URL', exposeUrl: 'Chat Completions URL', modelsUrl: 'Models URL', availableModels: 'Available Models', authLabel: 'Authentication', hints: 'Hints', editorTitle: 'EndPoint Editor', editorDesc: 'Create or update provider credentials and model mapping.', listTitle: 'Configured EndPoints', listDesc: 'Token usage accumulates from calls sent through the unified OpenAI v1 endpoint.', providerId: 'Provider ID', providerName: 'Display Name', apiUrl: 'API Base URL', apiKey: 'API Key', model: 'Upstream Model', protocol: 'Protocol', wireApi: 'Wire API', wireChat: 'Chat Completions', wireResponses: 'Responses API', wireResponsesWS: 'Responses WS', agentType: 'User-Agent / Agent Type', agentTypeHint: 'Examples: openclaw, claude-code/2.0.0, cline', maxConcurrency: 'Upstream Concurrency', maxQueueWaiters: 'Max Queue Length', queueTimeoutMs: 'Queue Timeout (ms)', concurrencyUnlimited: 'Unlimited', queueUnlimited: 'Unlimited', queueTimeoutUnlimited: 'Wait with request timeout', inFlight: 'In Flight', queueWaiters: 'Queued', add: 'New Provider', remove: 'Remove', save: 'Save', createAction: 'Create EndPoint', updateAction: 'Update EndPoint', test: 'Test Connection', cancel: 'Cancel', createTitle: 'New EndPoint', editTitle: 'Edit EndPoint', createDone: 'EndPoint created.', updateDone: 'EndPoint updated.', noSelection: 'No provider selected', selected: 'Selected', defaultBadge: 'Default', hasKey: 'API key saved', noKey: 'No API key', usageInput: 'Input', usageOutput: 'Output', usageTotal: 'Total', saveDone: 'LLM EndPoint registry saved.', saveDoneEmpty: 'Global LLM settings saved. No provider is configured yet.', nothingToSave: 'No provider to save yet. Click New Provider first.', edit: 'Edit', providerDraftMissing: 'Enter provider ID and display name before saving.', providerDraftSaved: 'Provider draft prepared.', saveFailed: 'Save LLM EndPoints failed: {error}', loadFailed: 'Load LLM EndPoints failed: {error}', addDone: 'Provider draft added.', removeDone: 'EndPoint removed.', removeConfirm: 'Remove provider {id}?', providerRequired: 'Add a provider first.', duplicateId: 'Provider ID already exists: {id}', apiKeyKeep: 'Configured (leave empty to keep)', apiKeyEnter: 'Enter API key', testRunning: 'Testing...', testOk: 'Connection ok ({ms}ms): {reply}', testFail: 'Connection failed: {error}', emptyList: 'No providers configured yet.', hintEmpty: 'Use model=<provider id> to select a provider on the unified endpoint.', authEmpty: 'Use Authorization: Bearer <viewer access token> from hub email sign-in.', generateTestKey: 'Generate Test API Key', testKeyRunning: 'Generating test API key...', testKeyDone: 'Test API key ready for {email}.', testKeyFail: 'Generate test API key failed: {error}', testKeyResult: 'Email: {email}\nAuthorization: Bearer {token}\nExpires: {days} days', modelsEmpty: '-', searchPlaceholder: 'Search by provider ID, name, or model', clearSearch: 'Clear', emptyFilter: 'No matching providers.', allProtocols: 'All Protocols', allKeyStates: 'All Key States', withKey: 'With Key', withoutKey: 'Without Key', countSummary: 'Total {total}, filtered {filtered}', uaShort: 'UA', providerIdPlaceholder: 'provider-id', providerNamePlaceholder: 'Provider Name', apiUrlPlaceholder: 'https://api.example.com/v1', modelPlaceholder: 'gpt-4.1', agentTypePlaceholder: 'openclaw', export: 'Export JSON', import: 'Import JSON', exportDone: 'LLM EndPoint JSON exported.', exportEmpty: 'No LLM EndPoint configuration to export yet.', importDone: 'LLM EndPoint JSON imported.', importFailed: 'Import LLM EndPoint JSON failed: {error}', importInvalid: 'Import JSON must include a providers array.', importBusy: 'Please finish the current editor changes before importing.'  },  zh: {    navLabel: 'LLM\u63a5\u5165', navDesc: '\u7aef\u70b9\u8def\u7531\u4e0e\u7edf\u4e00 API', tabTitle: 'LLM\u63a5\u5165', tabSubtitle: '\u914d\u7f6e LLM EndPoint\u3001token \u7528\u91cf\u3001\u8fde\u63a5\u6d4b\u8bd5\u4e0e\u7edf\u4e00 OpenAI v1 \u63a5\u53e3\u3002', reload: '\u91cd\u65b0\u52a0\u8f7d', enabled: '\u542f\u7528\u7edf\u4e00 LLM \u670d\u52a1', smartRoute: '\u5355\u8bbe\u5907\u667a\u80fd\u8def\u7531 LLM', defaultProvider: '\u9ed8\u8ba4\u670d\u52a1\u5546', exposeTitle: '\u7edf\u4e00 OpenAI v1 \u5bf9\u5916\u63a5\u53e3', exposeDesc: '\u53ef\u901a\u8fc7 `model`\u3001`X-LLM-Provider` \u6216 `?provider=` \u9009\u62e9\u670d\u52a1\u5546\u3002', apiBaseUrl: 'API Base URL', exposeUrl: 'Chat Completions URL', modelsUrl: 'Models URL', availableModels: '\u53ef\u7528\u6a21\u578b', authLabel: '\u9274\u6743\u65b9\u5f0f', hints: '\u63d0\u793a', editorTitle: 'EndPoint \u7f16\u8f91\u5668', editorDesc: '\u521b\u5efa\u6216\u66f4\u65b0\u670d\u52a1\u5546\u914d\u7f6e\u3001\u5bc6\u94a5\u548c\u6a21\u578b\u6620\u5c04\u3002', listTitle: '\u5df2\u914d\u7f6e EndPoint', listDesc: 'token \u7528\u91cf\u4ece\u7edf\u4e00 OpenAI v1 \u7aef\u70b9\u7684\u8bf7\u6c42\u4e2d\u7d2f\u79ef\u7edf\u8ba1\u3002', providerId: '\u670d\u52a1\u5546 ID', providerName: '\u663e\u793a\u540d\u79f0', apiUrl: 'API \u57fa\u5730\u5740', apiKey: 'API Key', model: '\u4e0a\u6e38\u6a21\u578b', protocol: '\u534f\u8bae', wireApi: '\u4f20\u8f93 API', wireChat: 'Chat Completions', wireResponses: 'Responses API', wireResponsesWS: 'Responses WS', agentType: 'User-Agent / \u5ba2\u6237\u7aef\u7c7b\u578b', agentTypeHint: '\u793a\u4f8b\uff1aopenclaw\u3001claude-code/2.0.0\u3001cline', maxConcurrency: '\u4e0a\u6e38\u5e76\u53d1\u6570', maxQueueWaiters: '\u6700\u5927\u6392\u961f\u957f\u5ea6', queueTimeoutMs: '\u6392\u961f\u8d85\u65f6\uff08ms\uff09', concurrencyUnlimited: '\u4e0d\u9650\u5236', queueUnlimited: '\u4e0d\u9650\u5236', queueTimeoutUnlimited: '\u8ddf\u968f\u8bf7\u6c42\u8d85\u65f6\u7b49\u5f85', inFlight: '\u6267\u884c\u4e2d', queueWaiters: '\u6392\u961f\u4e2d', add: '\u65b0\u5efa\u670d\u52a1\u5546', remove: '\u5220\u9664', save: '\u4fdd\u5b58', createAction: '\u521b\u5efa EndPoint', updateAction: '\u66f4\u65b0 EndPoint', test: '\u6d4b\u8bd5\u8fde\u63a5', cancel: '\u53d6\u6d88', createTitle: '\u65b0\u5efa EndPoint', editTitle: '\u7f16\u8f91 EndPoint', createDone: 'EndPoint \u5df2\u65b0\u5efa\u3002', updateDone: 'EndPoint \u5df2\u66f4\u65b0\u3002', noSelection: '\u672a\u9009\u62e9\u670d\u52a1\u5546', selected: '\u5df2\u9009\u4e2d', defaultBadge: '\u9ed8\u8ba4', hasKey: '\u5df2\u4fdd\u5b58 API Key', noKey: '\u672a\u914d\u7f6e API Key', usageInput: '\u8f93\u5165', usageOutput: '\u8f93\u51fa', usageTotal: '\u603b\u8ba1', saveDone: 'LLM EndPoint \u914d\u7f6e\u5df2\u4fdd\u5b58\u3002', saveDoneEmpty: '\u5df2\u4fdd\u5b58 LLM \u5168\u5c40\u8bbe\u7f6e\uff0c\u4f46\u5f53\u524d\u8fd8\u6ca1\u6709\u914d\u7f6e\u670d\u52a1\u5546\u3002', nothingToSave: '\u8fd8\u6ca1\u6709\u53ef\u4fdd\u5b58\u7684\u670d\u52a1\u5546\uff0c\u8bf7\u5148\u70b9\u51fb\u65b0\u5efa\u670d\u52a1\u5546\u3002', edit: '\u4fee\u6539', providerDraftMissing: '\u4fdd\u5b58\u524d\u8bf7\u5148\u586b\u5199\u670d\u52a1\u5546 ID \u548c\u663e\u793a\u540d\u79f0\u3002', providerDraftSaved: '\u5f53\u524d\u670d\u52a1\u5546\u8349\u7a3f\u5df2\u5199\u5165\u3002', saveFailed: '\u4fdd\u5b58 LLM EndPoint \u5931\u8d25: {error}', loadFailed: '\u52a0\u8f7d LLM EndPoint \u5931\u8d25: {error}', addDone: '\u5df2\u65b0\u589e\u670d\u52a1\u5546\u8349\u7a3f\u3002', removeDone: 'EndPoint \u5df2\u5220\u9664\u3002', removeConfirm: '\u786e\u8ba4\u5220\u9664\u670d\u52a1\u5546 {id} \u5417\uff1f', providerRequired: '\u8bf7\u5148\u65b0\u589e\u670d\u52a1\u5546\u3002', duplicateId: '\u670d\u52a1\u5546 ID \u5df2\u5b58\u5728: {id}', apiKeyKeep: '\u5df2\u914d\u7f6e\uff08\u7559\u7a7a\u4fdd\u6301\u4e0d\u53d8\uff09', apiKeyEnter: '\u8bf7\u8f93\u5165 API Key', testRunning: '\u6d4b\u8bd5\u4e2d...', testOk: '\u8fde\u63a5\u6210\u529f ({ms}ms): {reply}', testFail: '\u8fde\u63a5\u5931\u8d25: {error}', emptyList: '\u6682\u672a\u914d\u7f6e\u670d\u52a1\u5546\u3002', hintEmpty: '\u53ef\u4f7f\u7528 model=<provider id> \u5728\u7edf\u4e00\u7aef\u70b9\u9009\u62e9\u670d\u52a1\u5546\u3002', authEmpty: '\u4f7f\u7528 HUB \u90ae\u7bb1\u767b\u5f55\u540e\u8fd4\u56de\u7684 viewer access token\uff0c\u901a\u8fc7 Authorization: Bearer <token> \u8c03\u7528\u3002', generateTestKey: '\u751f\u6210\u6d4b\u8bd5 API Key', testKeyRunning: '\u6b63\u5728\u751f\u6210\u6d4b\u8bd5 API Key...', testKeyDone: '\u5df2\u4e3a {email} \u751f\u6210\u6d4b\u8bd5 API Key\u3002', testKeyFail: '\u751f\u6210\u6d4b\u8bd5 API Key \u5931\u8d25: {error}', testKeyResult: 'Email: {email}\nAuthorization: Bearer {token}\n\u6709\u6548\u671f: {days} \u5929', modelsEmpty: '-', searchPlaceholder: '\u6309\u670d\u52a1\u5546 ID\u3001\u540d\u79f0\u6216\u6a21\u578b\u641c\u7d22', clearSearch: '\u6e05\u9664', emptyFilter: '\u6682\u65e0\u5339\u914d\u7684\u670d\u52a1\u5546\u3002', allProtocols: '\u5168\u90e8\u534f\u8bae', allKeyStates: '\u5168\u90e8 Key \u72b6\u6001', withKey: '\u5df2\u914d\u7f6e Key', withoutKey: '\u672a\u914d\u7f6e Key', countSummary: '\u603b\u5171 {total} \u4e2a\uff0c\u5f53\u524d {filtered} \u4e2a', uaShort: 'UA', providerIdPlaceholder: 'provider-id', providerNamePlaceholder: '\u670d\u52a1\u5546\u540d\u79f0', apiUrlPlaceholder: 'https://api.example.com/v1', modelPlaceholder: 'gpt-4.1', agentTypePlaceholder: 'openclaw', export: '\u5bfc\u51fa JSON', import: '\u5bfc\u5165 JSON', exportDone: 'LLM EndPoint JSON \u5df2\u5bfc\u51fa\u3002', exportEmpty: '\u5f53\u524d\u6ca1\u6709\u53ef\u5bfc\u51fa\u7684 LLM EndPoint \u914d\u7f6e\u3002', importDone: 'LLM EndPoint JSON \u5df2\u5bfc\u5165\u3002', importFailed: '\u5bfc\u5165 LLM EndPoint JSON \u5931\u8d25: {error}', importInvalid: '\u5bfc\u5165 JSON \u5fc5\u987b\u5305\u542b providers \u6570\u7ec4\u3002', importBusy: '\u8bf7\u5148\u5904\u7406\u5f53\u524d\u7f16\u8f91\u4e2d\u7684\u53d8\u66f4\uff0c\u518d\u6267\u884c\u5bfc\u5165\u3002'  }};const lp = (key, vars = {}) => ((LLM_PROVIDER_I18N[currentLang] || LLM_PROVIDER_I18N.en)[key] || LLM_PROVIDER_I18N.en[key] || key).replace(/\{(\w+)\}/g, (_, name) => vars[name] ?? '');let llmProviderRegistryCache = null;let llmProviderSelectedId = '';let llmProviderDialogMode = 'create';let llmProviderIdManuallyEdited = false;let llmProviderLastSuggestedId = '';let llmProviderPage = 1;let llmProviderFilter = '';let llmProviderProtocolFilter = '';let llmProviderKeyFilter = 'all';let llmProviderCardTestState = {};const llmProviderPageSize = 12;function lpUsage(usage) { return { input_tokens: Number(usage && usage.input_tokens || 0), output_tokens: Number(usage && usage.output_tokens || 0), total_tokens: Number(usage && usage.total_tokens || 0), cached_input_tokens: Number(usage && usage.cached_input_tokens || 0), cache_write_tokens: Number(usage && usage.cache_write_tokens || 0), requests: Number(usage && usage.requests || 0), cached_requests: Number(usage && usage.cached_requests || 0) }; }function lpMetricLabel(kind) { var zh = { requests: '\u8bf7\u6c42\u6570', cacheRate: 'Prompt \u7f13\u5b58\u7387', cacheReuseRate: '\u7f13\u5b58\u590d\u7528\u7387', cacheRead: 'Cache Read', cacheWrite: 'Cache Write' }; var en = { requests: 'Requests', cacheRate: 'Prompt Cache Rate', cacheReuseRate: 'Cache Reuse', cacheRead: 'Cache Read', cacheWrite: 'Cache Write' }; return ((currentLang === 'zh' ? zh : en)[kind]) || kind; }function lpRatePercent(hit,total){ hit=Number(hit||0); total=Number(total||0); if(!total) return '0%'; return ((hit*100/total).toFixed(1).replace(/\.0$/,'')) + '%'; }function lpClone(provider) { return { id: provider && provider.id || '', name: provider && provider.name || '', api_url: provider && provider.api_url || '', api_key: provider && provider.api_key || '', has_api_key: !!(provider && provider.has_api_key), model: provider && provider.model || '', protocol: provider && provider.protocol || 'openai', wire_api: provider && provider.wire_api || 'chat', agent_type: provider && provider.agent_type || '', max_concurrency: Number(provider && provider.max_concurrency || 0), max_queue_waiters: Number(provider && provider.max_queue_waiters || 0), queue_timeout_ms: Number(provider && provider.queue_timeout_ms || 0), in_flight: Number(provider && provider.in_flight || 0), queue_waiters: Number(provider && provider.queue_waiters || 0), usage: lpUsage(provider && provider.usage) }; }function lpById(id) { return (llmProviderRegistryCache && llmProviderRegistryCache.providers || []).find(function(p) { return p.id === id; }) || null; }function lpNormalizeId(value) { return String(value || '').trim().toLowerCase().replace(/[^a-z0-9._-]+/g, '-').replace(/^-+|-+$/g, ''); }function lpNextId() { const used = new Set((llmProviderRegistryCache && llmProviderRegistryCache.providers || []).map(function(p) { return p.id; })); let i = 1; while (used.has('provider-' + i)) i++; return 'provider-' + i; }function lpEnsureSelection() { const providers = llmProviderRegistryCache && llmProviderRegistryCache.providers || []; if (!providers.length) { llmProviderSelectedId = ''; return; } if (!lpById(llmProviderSelectedId)) llmProviderSelectedId = llmProviderRegistryCache.current_provider_id || providers[0].id; if (!llmProviderRegistryCache.current_provider_id || !lpById(llmProviderRegistryCache.current_provider_id)) llmProviderRegistryCache.current_provider_id = llmProviderSelectedId; }function lpApiKeyPlaceholder(provider) { return provider && provider.has_api_key ? lp('apiKeyKeep') : lp('apiKeyEnter'); }function lpSuggestIdFromName(name) {  const normalized = lpNormalizeId(name || '');  return normalized || lpNextId();}function resetLLMProviderIdSuggestionState() {  llmProviderIdManuallyEdited = false;  llmProviderLastSuggestedId = '';}function syncLLMProviderIdSuggestion() {  if (llmProviderDialogMode !== 'create') return;  const idInput = document.getElementById('llmProviderId');  const nameInput = document.getElementById('llmProviderName');  if (!idInput || !nameInput) return;  const currentId = String(idInput.value || '').trim();  if (llmProviderIdManuallyEdited && currentId && currentId !== llmProviderLastSuggestedId) return;  const suggested = lpSuggestIdFromName(nameInput.value);  llmProviderLastSuggestedId = suggested;  idInput.value = suggested;}function initLLMProviderFormBindings() {  if (initLLMProviderFormBindings.done) return;  initLLMProviderFormBindings.done = true;  const idInput = document.getElementById('llmProviderId');  const nameInput = document.getElementById('llmProviderName');  if (nameInput) {    nameInput.addEventListener('input', function() {      syncLLMProviderIdSuggestion();    });  }  if (idInput) {    idInput.addEventListener('input', function() {      if (llmProviderDialogMode !== 'create') return;      const currentId = lpNormalizeId(idInput.value);      if (!currentId || currentId === llmProviderLastSuggestedId) {        llmProviderIdManuallyEdited = false;        if (currentId) llmProviderLastSuggestedId = currentId;        return;      }      llmProviderIdManuallyEdited = true;    });  }}initLLMProviderFormBindings.done = false;function validateLLMProvider(provider, opts) {  const requireKey = !!(opts && opts.requireKey);  if (!provider || !provider.id || !provider.name || !provider.api_url || !provider.model) return { ok: false, message: lp('providerDraftMissing') };  if (requireKey && !String(provider.api_key || '').trim()) return { ok: false, message: lp('apiKeyEnter') };  return { ok: true, message: '' };}function scrollLLMProviderCardIntoView(id) {  if (!id) return;  const card = document.querySelector('[data-provider-id="' + String(id).replace(/"/g, '\"') + '"]');  if (card && typeof card.scrollIntoView === 'function') card.scrollIntoView({ behavior: 'smooth', block: 'nearest' });}function ensureLLMProviderPageInRange(total) {  const totalPages = Math.max(1, Math.ceil(Number(total || 0) / llmProviderPageSize));  llmProviderPage = Math.min(totalPages, Math.max(1, Number(llmProviderPage || 1)));  return totalPages;}function syncLLMProviderPageWithSelection(providers) {  const list = providers || [];  const selectedIndex = list.findIndex(function(p) { return p.id === llmProviderSelectedId; });  const totalPages = ensureLLMProviderPageInRange(list.length);  if (selectedIndex >= 0) llmProviderPage = Math.floor(selectedIndex / llmProviderPageSize) + 1;  return totalPages;}function changeLLMProviderPage(step) {  const filteredCount = filterLLMProviders(llmProviderRegistryCache && llmProviderRegistryCache.providers || []).length;  const totalPages = ensureLLMProviderPageInRange(filteredCount);  llmProviderPage = Math.min(totalPages, Math.max(1, llmProviderPage + step));  renderLLMProviders();}function setLLMProviderFilter(value) {  llmProviderFilter = String(value || '').trim().toLowerCase();  llmProviderPage = 1;  renderLLMProviders();}function setLLMProviderProtocolFilter(value) {  llmProviderProtocolFilter = String(value || '').trim().toLowerCase();  llmProviderPage = 1;  renderLLMProviders();}function setLLMProviderKeyFilter(value) {  llmProviderKeyFilter = String(value || 'all').trim().toLowerCase() || 'all';  llmProviderPage = 1;  renderLLMProviders();}function filterLLMProviders(providers) {  const keyword = String(llmProviderFilter || '').trim().toLowerCase();  const protocolFilter = String(llmProviderProtocolFilter || '').trim().toLowerCase();  const keyFilter = String(llmProviderKeyFilter || 'all').trim().toLowerCase();  return (providers || []).filter(function(p) {    const matchesKeyword = !keyword || [p.id, p.name, p.model].some(function(v) { return String(v || '').toLowerCase().indexOf(keyword) >= 0; });    const matchesProtocol = !protocolFilter || String(p.protocol || 'openai').toLowerCase() === protocolFilter;    const hasKey = !!p.has_api_key;    const matchesKey = keyFilter === 'all' || (keyFilter === 'with_key' && hasKey) || (keyFilter === 'without_key' && !hasKey);    return matchesKeyword && matchesProtocol && matchesKey;  });}function applyLLMProviderScopeUI() {  var enabled = document.getElementById('llmProvidersEnabled');  if (enabled && enabled.parentElement && enabled.parentElement.parentElement) enabled.parentElement.parentElement.style.display = 'none';  var smart = document.getElementById('llmProvidersSmartRouteSingle');  if (smart && smart.parentElement && smart.parentElement.parentElement) smart.parentElement.parentElement.style.display = 'none';}function ensureLLMProviderModalUI() {  var overlay = document.getElementById('llmProviderModalOverlay');  if (overlay && window.AdminUI && typeof AdminUI.bindModalOverlayDismiss === 'function') {    AdminUI.bindModalOverlayDismiss(overlay, closeLLMProviderDialog);  }  var testResult = document.getElementById('llmProviderTestResult');  if (testResult) testResult.classList.add('hidden');  initLLMProviderFormBindings();  initLLMProviderGlobalBindings();}function updateLLMProviderEditorCopy(mode) {  const editing = mode === 'edit';  _s('llmProviderEditorTitle', 'textContent', editing ? lp('editTitle') : lp('createTitle'));  _s('llmProviderEditorDesc', 'textContent', lp('editorDesc'));  _s('llmProviderSaveBtn', 'textContent', editing ? lp('updateAction') : lp('createAction'));  _s('llmProviderCancelBtn', 'textContent', lp('cancel'));  _s('llmProviderTestBtn', 'textContent', lp('test'));}function applyLLMProviderEditorMode() {  var idInput = document.getElementById('llmProviderId');  if (idInput) idInput.readOnly = llmProviderDialogMode === 'edit';}function openLLMProviderDialog(mode) {  ensureLLMProviderModalUI();  var overlay = document.getElementById('llmProviderModalOverlay');  if (!overlay) return;  llmProviderDialogMode = mode === 'edit' ? 'edit' : 'create';  if (llmProviderDialogMode === 'edit') {    llmProviderIdManuallyEdited = true;    llmProviderLastSuggestedId = document.getElementById('llmProviderId') && document.getElementById('llmProviderId').value || '';  }  updateLLMProviderEditorCopy(llmProviderDialogMode);  applyLLMProviderEditorMode();  var overlay = document.getElementById('llmProviderModalOverlay');  if (overlay) overlay.classList.add('show');  var testResult = document.getElementById('llmProviderTestResult');  if (testResult) { testResult.classList.add('hidden'); testResult.textContent = ''; }  var first = document.getElementById('llmProviderId');  if (first && typeof first.focus === 'function') first.focus();}function llmProviderDialogOpen() {  var overlay = document.getElementById('llmProviderModalOverlay');  return !!(overlay && overlay.classList.contains('show'));}function closeLLMProviderDialog() {  var overlay = document.getElementById('llmProviderModalOverlay');  if (overlay) overlay.classList.remove('show');  var testResult = document.getElementById('llmProviderTestResult');  if (testResult) { testResult.classList.add('hidden'); testResult.textContent = ''; }}function setLLMProviderTestKeyResult(message) {  var result = document.getElementById('llmProvidersTestKeyResult');  if (!result) return;  if (!message) { result.classList.add('hidden'); result.textContent = ''; return; }  result.classList.remove('hidden');  result.textContent = message;}function applyLLMProvidersI18n() {  _s('navLLMProviders', 'textContent', lp('navLabel'));  _s('navLLMProvidersDesc', 'textContent', lp('navDesc'));  _s('llmProvidersTabTitle', 'textContent', lp('tabTitle'));  _s('llmProvidersTabSubtitle', 'textContent', lp('tabSubtitle'));  _s('llmProvidersReloadBtn', 'textContent', lp('reload'));  _s('llmProvidersExportBtn', 'textContent', lp('export'));  _s('llmProvidersImportBtn', 'textContent', lp('import'));  _s('llmProvidersEnabledLabel', 'textContent', lp('enabled'));  _s('llmProvidersSmartRouteSingleLabel', 'textContent', lp('smartRoute'));  _s('llmProvidersCurrentLabel', 'textContent', lp('defaultProvider'));  _s('llmProvidersExposeTitle', 'textContent', lp('exposeTitle'));  _s('llmProvidersExposeDesc', 'textContent', lp('exposeDesc'));  _s('llmProvidersAPIBaseURLLabel', 'textContent', lp('apiBaseUrl'));  _s('llmProvidersExposeURLLabel', 'textContent', lp('exposeUrl'));  _s('llmProvidersModelsURLLabel', 'textContent', lp('modelsUrl'));  _s('llmProvidersAvailableModelsLabel', 'textContent', lp('availableModels'));  _s('llmProvidersAuthLabel', 'textContent', lp('authLabel'));  _s('llmProvidersGenerateTestKeyBtn', 'textContent', lp('generateTestKey'));  _s('llmProvidersHintsLabel', 'textContent', lp('hints'));  _s('llmProviderEditorTitle', 'textContent', lp('editorTitle'));  _s('llmProviderEditorDesc', 'textContent', lp('editorDesc'));  _s('llmProviderListTitle', 'textContent', lp('listTitle'));  _s('llmProviderListDesc', 'textContent', lp('listDesc'));  _s('llmProviderIdLabel', 'textContent', lp('providerId'));  _s('llmProviderNameLabel', 'textContent', lp('providerName'));  _s('llmProviderApiUrlLabel', 'textContent', lp('apiUrl'));  _s('llmProviderApiKeyLabel', 'textContent', lp('apiKey'));  _s('llmProviderModelLabel', 'textContent', lp('model'));  _s('llmProviderProtocolLabel', 'textContent', lp('protocol'));  _s('llmProviderWireApiLabel', 'textContent', lp('wireApi'));  _s('llmProviderAgentTypeLabel', 'textContent', lp('agentType'));  _s('llmProviderAgentTypeHint', 'textContent', lp('agentTypeHint'));  _s('llmProviderWireApiChat', 'textContent', lp('wireChat'));  _s('llmProviderWireApiResponses', 'textContent', lp('wireResponses'));  _s('llmProviderWireApiResponsesWS', 'textContent', lp('wireResponsesWS'));  _s('llmProviderMaxConcurrencyLabel', 'textContent', lp('maxConcurrency'));  _s('llmProviderCreateBtn', 'textContent', lp('add'));  _s('llmProvidersSaveBtn', 'textContent', lp('save'));  _s('llmProviderSaveBtn', 'textContent', lp('save'));  _s('llmProviderTestBtn', 'textContent', lp('test'));  _s('llmProviderCancelBtn', 'textContent', lp('cancel'));  _s('llmProviderId', 'placeholder', lp('providerIdPlaceholder'));  _s('llmProviderName', 'placeholder', lp('providerNamePlaceholder'));  _s('llmProviderApiUrl', 'placeholder', lp('apiUrlPlaceholder'));  _s('llmProviderApiKey', 'placeholder', lp('apiKeyEnter'));  _s('llmProviderModel', 'placeholder', lp('modelPlaceholder'));  _s('llmProviderAgentType', 'placeholder', lp('agentTypePlaceholder'));  _s('llmProviderModalCloseBtn', 'ariaLabel', tr('closeDialog'));  updateLLMProviderEditorCopy(llmProviderDialogMode === 'edit' ? 'edit' : 'create');  applyLLMProviderEditorMode();  applyLLMProviderScopeUI();  renderLLMProviders();}function renderLLMProviders() {
  if (!document.getElementById('llmProviderList')) return;
  if (!llmProviderRegistryCache) {
    _s('llmProviderList', 'innerHTML', '<div class="hint">' + lp('emptyList') + '</div>');
    _s('llmProviderSelectionBadge', 'textContent', lp('noSelection'));
    return;
  }
  lpEnsureSelection();
  const providers = llmProviderRegistryCache.providers || [];
  const selected = lpById(llmProviderSelectedId);
  const filteredProviders = filterLLMProviders(providers);
  const totalPages = syncLLMProviderPageWithSelection(filteredProviders);
  _s('llmProvidersEnabled', 'checked', !!llmProviderRegistryCache.enabled);
  _s('llmProvidersSmartRouteSingle', 'checked', !!llmProviderRegistryCache.smart_route_single_device);
  const currentEl = document.getElementById('llmProvidersCurrent');
  if (currentEl) currentEl.innerHTML = providers.length ? providers.map(function(p) {
    return '<option value="' + escapeHtml(p.id) + '"' + (p.id === llmProviderRegistryCache.current_provider_id ? ' selected' : '') + '>' + escapeHtml((p.name || p.id) + ' (' + p.id + ')') + '</option>';
  }).join('') : '<option value="">-</option>';
  _s('llmProvidersAPIBaseURL', 'textContent', llmProviderRegistryCache.expose_api_base_url || '-');
  _s('llmProvidersExposeURL', 'textContent', llmProviderRegistryCache.expose_base_url || '-');
  _s('llmProvidersModelsURL', 'textContent', llmProviderRegistryCache.expose_models_url || '-');
  _s('llmProvidersAvailableModels', 'textContent', (llmProviderRegistryCache.available_models || []).length ? llmProviderRegistryCache.available_models.join(', ') : lp('modelsEmpty'));
  _s('llmProvidersAuthHint', 'textContent', llmProviderRegistryCache.auth_hint || lp('authEmpty'));
  const hints = llmProviderRegistryCache.hints && llmProviderRegistryCache.hints.length ? llmProviderRegistryCache.hints : [lp('hintEmpty')];
  _s('llmProvidersHints', 'innerHTML', hints.map(function(h) { return '<div>' + escapeHtml(h) + '</div>'; }).join(''));
  _s('llmProviderSelectionBadge', 'textContent', selected ? (lp('selected') + ': ' + (selected.name || selected.id)) : lp('noSelection'));
  const shouldSyncForm = !llmProviderDialogOpen();
  if (shouldSyncForm) {
    _s('llmProviderId', 'value', selected ? selected.id : '');
    _s('llmProviderName', 'value', selected ? selected.name : '');
    _s('llmProviderApiUrl', 'value', selected ? selected.api_url : '');
    _s('llmProviderModel', 'value', selected ? selected.model : '');
    _s('llmProviderApiKey', 'value', '');
    _s('llmProviderApiKey', 'placeholder', lpApiKeyPlaceholder(selected));
    _s('llmProviderProtocol', 'value', selected ? selected.protocol : 'openai');
    _s('llmProviderWireApi', 'value', selected ? selected.wire_api : 'chat');
    _s('llmProviderAgentType', 'value', selected ? selected.agent_type : '');
    _s('llmProviderMaxConcurrency', 'value', selected ? String(Number(selected.max_concurrency || 0)) : '0');  _s('llmProviderMaxQueueWaiters', 'value', selected ? String(Number(selected.max_queue_waiters || 0)) : '0');  _s('llmProviderQueueTimeoutMs', 'value', selected ? String(Number(selected.queue_timeout_ms || 0)) : '0');
  }
  const root = document.getElementById('llmProviderList');
  const isZh = currentLang === 'zh';
  const protocolOptions = ['openai', 'anthropic'].map(function(v) {
    return '<option value="' + escapeHtml(v) + '"' + (llmProviderProtocolFilter === v ? ' selected' : '') + '>' + escapeHtml(v) + '</option>';
  }).join('');
  const actionsRow = '<div class="actions" style="margin:0 0 8px 0;justify-content:flex-end">'
    + '<button class="btn-primary" type="button" id="llmProviderCreateInlineBtn" style="height:34px;padding:0 14px;font-size:12px;white-space:nowrap" onclick="addLLMProvider()">' + escapeHtml(lp('add')) + '</button>'
    + '<button class="btn-ghost" type="button" id="llmProvidersExportBtn" onclick="exportLLMProvidersJSON()">' + escapeHtml(lp('export')) + '</button>'
    + '<button class="btn-ghost" type="button" id="llmProvidersImportBtn" onclick="triggerLLMProvidersImport()">' + escapeHtml(lp('import')) + '</button>'
    + '<input id="llmProvidersImportInput" type="file" accept="application/json,.json" class="hidden" onchange="importLLMProvidersJSON(event)">'
    + '</div>';
  const searchRow = '<div class="row" style="grid-template-columns:minmax(0,1.35fr) minmax(120px,.72fr) minmax(136px,.76fr) auto;gap:8px;margin-bottom:8px;padding:0;border:none;background:transparent;align-items:center">'
    + '<input id="llmProviderSearchInput" value="' + escapeHtml(llmProviderFilter) + '" placeholder="' + escapeHtml(lp('searchPlaceholder')) + '" style="height:34px" oninput="setLLMProviderFilter(this.value)">'
    + '<select id="llmProviderProtocolFilter" style="height:34px" onchange="setLLMProviderProtocolFilter(this.value)"><option value="">' + escapeHtml(lp('allProtocols')) + '</option>' + protocolOptions + '</select>'
    + '<select id="llmProviderKeyFilter" style="height:34px" onchange="setLLMProviderKeyFilter(this.value)"><option value="all"' + (llmProviderKeyFilter === 'all' ? ' selected' : '') + '>' + escapeHtml(lp('allKeyStates')) + '</option><option value="with_key"' + (llmProviderKeyFilter === 'with_key' ? ' selected' : '') + '>' + escapeHtml(lp('withKey')) + '</option><option value="without_key"' + (llmProviderKeyFilter === 'without_key' ? ' selected' : '') + '>' + escapeHtml(lp('withoutKey')) + '</option></select>'
    + '<button class="btn-ghost" style="height:34px;padding:0 10px;font-size:11px" onclick="window.__resetLLMProviderFilters && window.__resetLLMProviderFilters()">' + escapeHtml(lp('clearSearch')) + '</button>'
    + '</div>';
  const searchMeta = '<div class="item-meta" style="margin:0 0 8px 0;font-size:11px">' + lp('countSummary', { total: String(providers.length), filtered: String(filteredProviders.length) }) + '</div>';
  if (!providers.length) {
    llmProviderPage = 1;
    root.innerHTML = actionsRow + searchRow + searchMeta + '<div class="hint">' + lp('emptyList') + '</div>';
    return;
  }
  if (!filteredProviders.length) {
    llmProviderPage = 1;
    root.innerHTML = actionsRow + searchRow + searchMeta + '<div class="hint">' + lp('emptyFilter') + '</div>';
    return;
  }
  const startIndex = (llmProviderPage - 1) * llmProviderPageSize;
  const pageProviders = filteredProviders.slice(startIndex, startIndex + llmProviderPageSize);
  const header = '<div class="row header" style="grid-template-columns:1.1fr 1fr .55fr .68fr .58fr .74fr .62fr 1.72fr;padding:8px 10px"><div>' + escapeHtml(lp('providerName')) + '</div><div>' + escapeHtml(lp('model')) + '</div><div>' + escapeHtml(lp('protocol')) + '</div><div>' + escapeHtml(lp('apiKey')) + '</div><div>' + escapeHtml(lp('maxConcurrency')) + '</div><div>' + escapeHtml(lp('usageTotal')) + '</div><div>' + escapeHtml(lpMetricLabel('cacheRate')) + '</div><div></div></div>';
  const rows = pageProviders.map(function(p) {
    const usage = lpUsage(p.usage);
    const isSelected = p.id === llmProviderSelectedId;
    const isDefault = p.id === llmProviderRegistryCache.current_provider_id;
    const keyBadge = p.has_api_key ? '<span class="badge ok" style="font-size:10px;padding:4px 8px">' + escapeHtml(lp('hasKey')) + '</span>' : '<span class="badge warn" style="font-size:10px;padding:4px 8px">' + escapeHtml(lp('noKey')) + '</span>';
    const defaultBadge = isDefault ? '<span class="badge info" style="font-size:10px;padding:4px 8px">' + escapeHtml(lp('defaultBadge')) + '</span>' : '';
    const concurrency = Number(p.max_concurrency || 0);
    const concurrencyText = concurrency > 0 ? String(concurrency) : lp('concurrencyUnlimited');

    const testState = llmProviderCardTestState[p.id] || null;
    const testButtonTitle = testState && testState.running ? lp('testRunning') : lp('test');
    const testButtonText = testState && testState.running ? '...' : (isZh ? '\u6d4b' : 'T');
    const editButtonTitle = lp('edit');
    const editButtonText = isZh ? '\u6539' : 'E';
    const removeButtonTitle = lp('remove');
    const removeButtonText = isZh ? '\u5220' : 'D';
    const testButtonDisabled = testState && testState.running ? ' disabled' : '';
    const testColor = testState ? (testState.success ? 'var(--ok,#1f9d55)' : (testState.running ? 'var(--muted)' : 'var(--danger,#d64545)')) : 'var(--muted)';
    const testText = testState && testState.message ? escapeHtml(testState.message) : '-';
    return '<div class="item" data-provider-id="' + escapeHtml(p.id) + '" style="margin-bottom:6px;padding:0;overflow:hidden;border:' + (isSelected ? '1px solid rgba(47,128,237,.3)' : '1px solid var(--line)') + ';box-shadow:none;cursor:pointer" onclick="selectLLMProvider(this.dataset.providerId)">'
      + '<div class="row" style="grid-template-columns:1.1fr 1fr .55fr .68fr .58fr .74fr .62fr 1.72fr;gap:8px;padding:9px 10px;border:none;background:' + (isSelected ? '#f8fbff' : '#fff') + '">'
      + '<div style="min-width:0"><div style="display:flex;align-items:center;gap:5px;min-width:0"><div class="mono" style="font-size:11px;font-weight:700;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(p.name || p.id) + '</div>' + defaultBadge + '</div><div class="item-meta mono" style="margin-top:2px;font-size:10px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(p.id) + '</div></div>'
      + '<div style="min-width:0"><div class="mono" style="font-size:11px;font-weight:700;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(p.model || '-') + '</div><div class="item-meta mono" style="margin-top:2px;font-size:10px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(p.api_url || '-') + '</div></div>'
      + '<div><div class="mono" style="font-size:11px">' + escapeHtml(p.protocol || 'openai') + '</div><div class="item-meta" style="margin-top:2px;font-size:10px">' + escapeHtml(p.wire_api || 'chat') + '</div></div>'
      + '<div>' + keyBadge + '</div>'
      + '<div><div class="mono" style="font-size:11px">' + escapeHtml(concurrencyText) + '</div><div class="item-meta" style="margin-top:2px;font-size:10px">' + escapeHtml(lp('inFlight')) + ': ' + escapeHtml(String(Number(p.in_flight || 0))) + '</div></div>'
      + '<div><div class="mono" style="font-size:11px">' + escapeHtml(lpFormatInt(usage.total_tokens)) + '</div><div class="item-meta" style="margin-top:2px;font-size:10px">' + escapeHtml(lp('usageInput')) + ': ' + escapeHtml(lpFormatInt(usage.input_tokens)) + ' / ' + escapeHtml(lp('usageOutput')) + ': ' + escapeHtml(lpFormatInt(usage.output_tokens)) + '</div></div>'
      + '<div><div class="mono" style="font-size:11px;color:var(--ok,#1f9d55);font-weight:700">' + escapeHtml(lpRatePercent(usage.cached_requests, usage.requests)) + '</div><div class="item-meta" style="margin-top:2px;font-size:10px">' + escapeHtml(lpFormatInt(usage.cached_requests)) + ' / ' + escapeHtml(lpFormatInt(usage.requests)) + '</div></div>'
      + '<div class="llm-provider-actions" style="display:flex;justify-content:flex-end;gap:8px;flex-wrap:nowrap;align-items:center;min-width:0">' + llmProviderPriceChipHTML(p) + '<button class="btn-secondary" title="' + escapeHtml(testButtonTitle) + '" aria-label="' + escapeHtml(testButtonTitle) + '" style="flex:0 0 34px;width:34px;min-width:0;height:24px;font-size:10px;padding:0;white-space:nowrap;overflow:hidden;text-align:center" data-provider-id="' + escapeHtml(p.id) + '" onclick="event.stopPropagation(); testLLMProviderCard(this.dataset.providerId)"' + testButtonDisabled + '>' + escapeHtml(testButtonText) + '</button><button class="btn-ghost" title="' + escapeHtml(editButtonTitle) + '" aria-label="' + escapeHtml(editButtonTitle) + '" style="flex:0 0 34px;width:34px;min-width:0;height:24px;font-size:10px;padding:0;white-space:nowrap;overflow:hidden;text-align:center" data-provider-id="' + escapeHtml(p.id) + '" onclick="event.stopPropagation(); editLLMProvider(this.dataset.providerId)">' + escapeHtml(editButtonText) + '</button><button class="btn-danger" title="' + escapeHtml(removeButtonTitle) + '" aria-label="' + escapeHtml(removeButtonTitle) + '" style="flex:0 0 34px;width:34px;min-width:0;height:24px;font-size:10px;padding:0;white-space:nowrap;overflow:hidden;text-align:center" data-provider-id="' + escapeHtml(p.id) + '" onclick="event.stopPropagation(); removeLLMProviderById(this.dataset.providerId)">' + escapeHtml(removeButtonText) + '</button></div>'
      + '</div>'
      + '<div style="padding:0 10px 8px;border-top:1px solid rgba(31,34,48,.05);background:' + (isSelected ? '#f8fbff' : '#fff') + '"><div class="item-meta llm-provider-meta-row" style="display:flex;gap:8px;flex-wrap:wrap;align-items:center;font-size:10px"><span class="mono">' + escapeHtml(lp('uaShort')) + ': ' + escapeHtml(p.agent_type || 'openclaw') + '</span><span>' + escapeHtml(lp('queueWaiters')) + ': ' + escapeHtml(String(Number(p.queue_waiters || 0))) + '</span><span>' + escapeHtml(lpMetricLabel('cacheReuseRate')) + ': <strong style="color:#4b82d8">' + escapeHtml(lpRatePercent(usage.cached_input_tokens, usage.input_tokens)) + '</strong></span><span>' + escapeHtml(lpMetricLabel('cacheRead')) + ': ' + escapeHtml(lpFormatInt(usage.cached_input_tokens)) + '</span><span>' + escapeHtml(lpMetricLabel('cacheWrite')) + ': ' + escapeHtml(lpFormatInt(usage.cache_write_tokens)) + '</span><span style="color:' + testColor + '">' + testText + '</span></div></div>'
      + '</div>';
  }).join('');
  root.innerHTML = actionsRow + searchRow + searchMeta + '<div class="table" style="gap:4px">' + header + rows + '</div>';
  if (filteredProviders.length > llmProviderPageSize) {
    const end = startIndex + pageProviders.length;
    const pageSummary = isZh ? ('\\u7b2c ' + llmProviderPage + ' / ' + totalPages + ' \\u9875\\uff0c\\u663e\\u793a ' + (startIndex + 1) + '-' + end + ' / ' + filteredProviders.length) : ('Page ' + llmProviderPage + ' / ' + totalPages + ', showing ' + (startIndex + 1) + '-' + end + ' / ' + filteredProviders.length);
    const prevLabel = isZh ? '\\u4e0a\\u4e00\\u9875' : 'Previous';
    const nextLabel = isZh ? '\\u4e0b\\u4e00\\u9875' : 'Next';
    root.innerHTML += '<div class="pager" style="margin-top:8px"><div class="pager-meta">' + pageSummary + '</div><div class="pager-actions"><button class="btn-ghost" style="height:28px;font-size:11px;padding:0 10px" onclick="changeLLMProviderPage(-1)"' + (llmProviderPage <= 1 ? ' disabled' : '') + '>' + prevLabel + '</button><button class="btn-ghost" style="height:28px;font-size:11px;padding:0 10px" onclick="changeLLMProviderPage(1)"' + (llmProviderPage >= totalPages ? ' disabled' : '') + '>' + nextLabel + '</button></div></div>';
  }
}
window.__resetLLMProviderFilters = function() { setLLMProviderFilter(''); setLLMProviderProtocolFilter(''); setLLMProviderKeyFilter('all'); var search = document.getElementById('llmProviderSearchInput'); if (search) search.value = ''; var protocol = document.getElementById('llmProviderProtocolFilter'); if (protocol) protocol.value = ''; var key = document.getElementById('llmProviderKeyFilter'); if (key) key.value = 'all'; };
function clearLLMProviderForm() {  resetLLMProviderIdSuggestionState();  _s('llmProviderId', 'value', '');  _s('llmProviderName', 'value', '');  _s('llmProviderApiUrl', 'value', '');  _s('llmProviderApiKey', 'value', '');  _s('llmProviderModel', 'value', '');  _s('llmProviderProtocol', 'value', 'openai');  _s('llmProviderWireApi', 'value', 'chat');  _s('llmProviderAgentType', 'value', '');  _s('llmProviderMaxConcurrency', 'value', '0');  _s('llmProviderMaxQueueWaiters', 'value', '0');  _s('llmProviderQueueTimeoutMs', 'value', '0');  _s('llmProviderApiKey', 'placeholder', lp('apiKeyEnter'));}function formHasLLMProviderDraft() {  const form = readSelectedLLMProviderForm();  return !!(form.id || form.name || form.api_url || form.api_key || form.model || form.agent_type || Number(form.max_concurrency || 0) > 0 || Number(form.max_queue_waiters || 0) > 0 || Number(form.queue_timeout_ms || 0) > 0 || form.protocol !== 'openai' || form.wire_api !== 'chat');}function upsertLLMProviderFromForm(requireIdentity) {  if (!llmProviderRegistryCache) llmProviderRegistryCache = { enabled: false, current_provider_id: '', smart_route_single_device: false, providers: [], expose_api_base_url: '', expose_base_url: '', expose_models_url: '', available_models: [], auth_mode: '', auth_hint: '', hints: [], downstream_max_concurrency: 100, user_rate_limit_per_minute: 120, user_rate_limit_burst: 20 };  const next = readSelectedLLMProviderForm();  if (!next.id || !next.name) {    if (requireIdentity) {      const msg = lp('providerDraftMissing');      setOutput(msg);      showToast(msg, 'info');      return false;    }    return true;  }  const providers = llmProviderRegistryCache.providers || [];  let idx = llmProviderSelectedId ? providers.findIndex(function(p) { return p.id === llmProviderSelectedId; }) : -1;  const duplicate = providers.find(function(p, i) { return i !== idx && p.id === next.id; });  if (duplicate) {    const msg = lp('duplicateId', { id: next.id });    setOutput(msg);    showToast(msg, 'error');    return false;  }  const prev = idx >= 0 ? providers[idx] : providers.find(function(p) { return p.id === next.id; }) || null;  if (idx < 0 && prev) idx = providers.findIndex(function(p) { return p.id === prev.id; });  next.has_api_key = (prev && prev.has_api_key) || !!next.api_key;  next.usage = lpUsage(prev && prev.usage);  next.in_flight = Number(prev && prev.in_flight || 0);  next.queue_waiters = Number(prev && prev.queue_waiters || 0);  if (idx >= 0) providers[idx] = next; else providers.push(next);  llmProviderRegistryCache.providers = providers;  llmProviderSelectedId = next.id;  if (!llmProviderRegistryCache.current_provider_id) llmProviderRegistryCache.current_provider_id = next.id;  return true;}function readSelectedLLMProviderForm() {  const idEl = document.getElementById('llmProviderId');  const nameEl = document.getElementById('llmProviderName');  const apiUrlEl = document.getElementById('llmProviderApiUrl');  const apiKeyEl = document.getElementById('llmProviderApiKey');  const modelEl = document.getElementById('llmProviderModel');  const protocolEl = document.getElementById('llmProviderProtocol');  const wireApiEl = document.getElementById('llmProviderWireApi');  const agentTypeEl = document.getElementById('llmProviderAgentType');  const concurrencyEl = document.getElementById('llmProviderMaxConcurrency');  const maxQueueWaitersEl = document.getElementById('llmProviderMaxQueueWaiters');  const queueTimeoutEl = document.getElementById('llmProviderQueueTimeoutMs');  const wireAPI = String(wireApiEl && wireApiEl.value || 'chat').trim().replace(/_/g, '-');  return { id: lpNormalizeId(idEl && idEl.value || ''), name: (nameEl && nameEl.value || '').trim(), api_url: (apiUrlEl && apiUrlEl.value || '').trim(), api_key: apiKeyEl && apiKeyEl.value || '', model: (modelEl && modelEl.value || '').trim(), protocol: (protocolEl && protocolEl.value || 'openai').trim(), wire_api: wireAPI, agent_type: (agentTypeEl && agentTypeEl.value || '').trim(), max_concurrency: Math.max(0, Number(concurrencyEl && concurrencyEl.value || 0) || 0), max_queue_waiters: Math.max(0, Number(maxQueueWaitersEl && maxQueueWaitersEl.value || 0) || 0), queue_timeout_ms: Math.max(0, Number(queueTimeoutEl && queueTimeoutEl.value || 0) || 0) };}function syncSelectedLLMProviderFromForm() {  if (!llmProviderRegistryCache || !llmProviderSelectedId) return true;  const idx = (llmProviderRegistryCache.providers || []).findIndex(function(p) { return p.id === llmProviderSelectedId; });  if (idx < 0) return true;  const prev = llmProviderRegistryCache.providers[idx];  const next = readSelectedLLMProviderForm();  next.id = next.id || prev.id;  const duplicate = (llmProviderRegistryCache.providers || []).find(function(p, i) { return i !== idx && p.id === next.id; });  if (duplicate) { const msg = lp('duplicateId', { id: next.id }); setOutput(msg); showToast(msg, 'error'); return false; }  next.name = next.name || next.id;  next.has_api_key = prev.has_api_key || !!next.api_key;  next.usage = lpUsage(prev.usage);  next.in_flight = Number(prev.in_flight || 0);  next.queue_waiters = Number(prev.queue_waiters || 0);  llmProviderRegistryCache.providers[idx] = next;  if (llmProviderRegistryCache.current_provider_id === llmProviderSelectedId) llmProviderRegistryCache.current_provider_id = next.id;  llmProviderSelectedId = next.id;  return true;}function buildLLMProviderPayload() {  return { enabled: !!(llmProviderRegistryCache && llmProviderRegistryCache.enabled), current_provider_id: document.getElementById('llmProvidersCurrent') && document.getElementById('llmProvidersCurrent').value || llmProviderRegistryCache.current_provider_id || '', smart_route_single_device: !!(llmProviderRegistryCache && llmProviderRegistryCache.smart_route_single_device), providers: (llmProviderRegistryCache.providers || []).map(function(p) { return { id: p.id, name: p.name || p.id, api_url: p.api_url || '', api_key: p.api_key || '', model: p.model || '', protocol: p.protocol || 'openai', wire_api: p.wire_api || 'chat', agent_type: p.agent_type || '', max_concurrency: Math.max(0, Number(p.max_concurrency || 0)), max_queue_waiters: Math.max(0, Number(p.max_queue_waiters || 0)), queue_timeout_ms: Math.max(0, Number(p.queue_timeout_ms || 0)) }; }) };}function llmProviderImportedPayload(raw) {  raw = raw || {};  var providers = Array.isArray(raw.providers) ? raw.providers : null;  if (!providers) throw new Error(lp('importInvalid'));  return { enabled: !!raw.enabled, current_provider_id: String(raw.current_provider_id || '').trim(), smart_route_single_device: !!raw.smart_route_single_device, downstream_max_concurrency: Math.max(1, Number(raw.downstream_max_concurrency || 100) || 100), user_rate_limit_per_minute: Math.max(1, Number(raw.user_rate_limit_per_minute || 120) || 120), user_rate_limit_burst: Math.max(1, Number(raw.user_rate_limit_burst || 20) || 20), providers: providers.map(function(p) { return { id: lpNormalizeId(p && p.id || ''), name: String(p && p.name || p && p.id || '').trim(), api_url: String(p && p.api_url || '').trim(), api_key: String(p && p.api_key || ''), model: String(p && p.model || '').trim(), protocol: String(p && p.protocol || 'openai').trim() || 'openai', wire_api: String(p && p.wire_api || 'chat').trim().replace(/_/g, '-'), agent_type: String(p && p.agent_type || '').trim(), max_concurrency: Math.max(0, Number(p && p.max_concurrency || 0) || 0), max_queue_waiters: Math.max(0, Number(p && p.max_queue_waiters || 0) || 0), queue_timeout_ms: Math.max(0, Number(p && p.queue_timeout_ms || 0) || 0), circuit_breaker_threshold: Math.max(1, Number(p && p.circuit_breaker_threshold || 3) || 3), circuit_breaker_cooldown_ms: Math.max(1, Number(p && p.circuit_breaker_cooldown_ms || 30000) || 30000), failure_backoff_base_ms: Math.max(1, Number(p && p.failure_backoff_base_ms || 500) || 500), failure_backoff_max_ms: Math.max(1, Number(p && p.failure_backoff_max_ms || 10000) || 10000) }; }).filter(function(p) { return p.id; }) };}function exportLLMProvidersJSON() {  try {    if (llmProviderDialogOpen()) {      if (llmProviderSelectedId) {        if (!syncSelectedLLMProviderFromForm()) return;      } else if (formHasLLMProviderDraft()) {        var draft = readSelectedLLMProviderForm();        var validation = validateLLMProvider(draft, { requireKey: false });        if (!validation.ok) { setOutput(validation.message); showToast(validation.message, 'info'); return; }        if (!upsertLLMProviderFromForm(true)) return;      }    }    if (!llmProviderRegistryCache) { showToast(lp('exportEmpty'), 'info'); return; }    var payload = buildLLMProviderPayload();    if (!payload || !Array.isArray(payload.providers) || !payload.providers.length) { showToast(lp('exportEmpty'), 'info'); return; }    var blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json;charset=utf-8' });    var url = URL.createObjectURL(blob);    var a = document.createElement('a');    a.href = url;    a.download = 'llm-endpoints-' + new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19) + '.json';    document.body.appendChild(a);    a.click();    document.body.removeChild(a);    URL.revokeObjectURL(url);    setOutput(lp('exportDone'));    showToast(lp('exportDone'), 'success');  } catch (err) {    var msg = lp('importFailed', { error: err && err.message || 'export failed' });    setOutput(msg);    showToast(msg, 'error');  }}function triggerLLMProvidersImport() {  if (llmProviderDialogOpen() && formHasLLMProviderDraft()) {    showToast(lp('importBusy'), 'info');    return;  }  var input = document.getElementById('llmProvidersImportInput');  if (!input) return;  input.value = '';  input.click();}async function importLLMProvidersJSON(event) {  var input = event && event.target;  var file = input && input.files && input.files[0];  if (!file) return;  try {    var text = await file.text();    var imported = llmProviderImportedPayload(JSON.parse(text));    if (!imported.providers.length) throw new Error(lp('importInvalid'));    llmProviderRegistryCache = { enabled: imported.enabled, current_provider_id: imported.current_provider_id, smart_route_single_device: imported.smart_route_single_device, providers: imported.providers.map(lpClone), expose_api_base_url: '', expose_base_url: '', expose_models_url: '', available_models: [], auth_mode: '', auth_hint: '', hints: [], downstream_max_concurrency: imported.downstream_max_concurrency, user_rate_limit_per_minute: imported.user_rate_limit_per_minute, user_rate_limit_burst: imported.user_rate_limit_burst };    lpEnsureSelection();    var data = await api('/api/admin/llm/providers', { method: 'PUT', body: JSON.stringify(buildLLMProviderPayload()) });    llmProviderRegistryCache = { enabled: !!data.enabled, current_provider_id: data.current_provider_id || '', smart_route_single_device: !!data.smart_route_single_device, providers: (data.providers || []).map(lpClone), expose_api_base_url: data.expose_api_base_url || '', expose_base_url: data.expose_base_url || '', expose_models_url: data.expose_models_url || '', available_models: data.available_models || [], auth_mode: data.auth_mode || '', auth_hint: data.auth_hint || '', hints: data.hints || [], downstream_max_concurrency: llmProviderNormalizeDownstreamConcurrency(data.downstream_max_concurrency), user_rate_limit_per_minute: llmProviderNormalizeUserRateLimit(data.user_rate_limit_per_minute, 120), user_rate_limit_burst: llmProviderNormalizeUserRateLimit(data.user_rate_limit_burst, 20) };    lpEnsureSelection();    renderLLMProviders();    closeLLMProviderDialog();    setOutput(lp('importDone'));    showToast(lp('importDone'), 'success');  } catch (err) {    var msg = lp('importFailed', { error: err && err.message || 'invalid json' });    setOutput(msg);    showToast(msg, 'error');  } finally {    if (input) input.value = '';  }}async function loadLLMProviders() {  if (!llmProviderTenantScopedRefresh()) return;  if (typeof token === 'function' && !token()) return;  if (!document.getElementById('tab-llmproviders') && !document.getElementById('llmProviderList')) return;  ensureLLMProviderModalUI();  try {    const data = await api('/api/admin/llm/providers');    llmProviderRegistryCache = { enabled: !!data.enabled, current_provider_id: data.current_provider_id || '', smart_route_single_device: !!data.smart_route_single_device, providers: (data.providers || []).map(lpClone), expose_api_base_url: data.expose_api_base_url || '', expose_base_url: data.expose_base_url || '', expose_models_url: data.expose_models_url || '', available_models: data.available_models || [], auth_mode: data.auth_mode || '', auth_hint: data.auth_hint || '', hints: data.hints || [], downstream_max_concurrency: llmProviderNormalizeDownstreamConcurrency(data.downstream_max_concurrency), user_rate_limit_per_minute: llmProviderNormalizeUserRateLimit(data.user_rate_limit_per_minute, 120), user_rate_limit_burst: llmProviderNormalizeUserRateLimit(data.user_rate_limit_burst, 20) };    lpEnsureSelection();    renderLLMProviders();  } catch (err) {    const msg = lp('loadFailed', { error: err.message });    setOutput(msg);    showToast(msg, 'error');  }}function selectLLMProvider(id) { if (llmProviderDialogOpen() && !syncSelectedLLMProviderFromForm()) return; llmProviderSelectedId = id; renderLLMProviders(); }function editLLMProvider(id) {  llmProviderSelectedId = id;  llmProviderDialogMode = 'edit';  renderLLMProviders();  llmProviderIdManuallyEdited = true;  llmProviderLastSuggestedId = id || '';  openLLMProviderDialog('edit');}function setCurrentLLMProvider(id) { if (!llmProviderRegistryCache) return; llmProviderRegistryCache.current_provider_id = id || ''; renderLLMProviders(); }function addLLMProvider() {  if (!llmProviderRegistryCache) llmProviderRegistryCache = { enabled: false, current_provider_id: '', smart_route_single_device: false, providers: [], expose_api_base_url: '', expose_base_url: '', expose_models_url: '', available_models: [], auth_mode: '', auth_hint: '', hints: [], downstream_max_concurrency: 100, user_rate_limit_per_minute: 120, user_rate_limit_burst: 20 };  llmProviderSelectedId = '';  llmProviderDialogMode = 'create';  clearLLMProviderForm();  const suggestedID = lpNextId();  _s('llmProviderId', 'value', suggestedID);  _s('llmProviderName', 'value', 'Provider ' + suggestedID.split('-').pop());  llmProviderLastSuggestedId = suggestedID;  openLLMProviderDialog('create');}async function removeLLMProviderById(id) {  if (!llmProviderRegistryCache || !id) { showToast(lp('providerRequired'), 'info'); return; }  if (!confirm(lp('removeConfirm', { id: id }))) return;  const snapshot = JSON.stringify(llmProviderRegistryCache);  const selectedSnapshot = llmProviderSelectedId;  const modeSnapshot = llmProviderDialogMode;  llmProviderRegistryCache.providers = (llmProviderRegistryCache.providers || []).filter(function(p) { return p.id !== id; });  if (llmProviderRegistryCache.current_provider_id === id) llmProviderRegistryCache.current_provider_id = llmProviderRegistryCache.providers[0] && llmProviderRegistryCache.providers[0].id || '';  if (llmProviderSelectedId === id) {    llmProviderSelectedId = llmProviderRegistryCache.providers[0] && llmProviderRegistryCache.providers[0].id || '';    llmProviderDialogMode = 'create';    closeLLMProviderDialog();    clearLLMProviderForm();  }  renderLLMProviders();  try {    const data = await api('/api/admin/llm/providers', { method: 'PUT', body: JSON.stringify(buildLLMProviderPayload()) });    llmProviderRegistryCache = { enabled: !!data.enabled, current_provider_id: data.current_provider_id || '', smart_route_single_device: !!data.smart_route_single_device, providers: (data.providers || []).map(lpClone), expose_api_base_url: data.expose_api_base_url || '', expose_base_url: data.expose_base_url || '', expose_models_url: data.expose_models_url || '', available_models: data.available_models || [], auth_mode: data.auth_mode || '', auth_hint: data.auth_hint || '', hints: data.hints || [], downstream_max_concurrency: llmProviderNormalizeDownstreamConcurrency(data.downstream_max_concurrency), user_rate_limit_per_minute: llmProviderNormalizeUserRateLimit(data.user_rate_limit_per_minute, 120), user_rate_limit_burst: llmProviderNormalizeUserRateLimit(data.user_rate_limit_burst, 20) };    lpEnsureSelection();    renderLLMProviders();    setOutput(lp('removeDone'));    showToast(lp('removeDone'), 'success');  } catch (err) {    llmProviderRegistryCache = JSON.parse(snapshot);    llmProviderSelectedId = selectedSnapshot;    llmProviderDialogMode = modeSnapshot;    renderLLMProviders();    const msg = lp('saveFailed', { error: err.message });    setOutput(msg);    showToast(msg, 'error');  }}function llmProviderRegistrySnapshot(data) {  return { enabled: !!data.enabled, current_provider_id: data.current_provider_id || '', smart_route_single_device: !!data.smart_route_single_device, providers: (data.providers || []).map(lpClone), expose_api_base_url: data.expose_api_base_url || '', expose_base_url: data.expose_base_url || '', expose_models_url: data.expose_models_url || '', available_models: data.available_models || [], auth_mode: data.auth_mode || '', auth_hint: data.auth_hint || '', hints: data.hints || [], downstream_max_concurrency: llmProviderNormalizeDownstreamConcurrency(data.downstream_max_concurrency), user_rate_limit_per_minute: llmProviderNormalizeUserRateLimit(data.user_rate_limit_per_minute, 120), user_rate_limit_burst: llmProviderNormalizeUserRateLimit(data.user_rate_limit_burst, 20) };}async function saveLLMProviderGlobals() {  if (!llmProviderRegistryCache) llmProviderRegistryCache = { enabled: false, current_provider_id: '', smart_route_single_device: false, providers: [], expose_api_base_url: '', expose_base_url: '', expose_models_url: '', available_models: [], auth_mode: '', auth_hint: '', hints: [], downstream_max_concurrency: 100, user_rate_limit_per_minute: 120, user_rate_limit_burst: 20 };  llmProviderRegistryCache.current_provider_id = document.getElementById('llmProvidersCurrent') && document.getElementById('llmProvidersCurrent').value || llmProviderRegistryCache.current_provider_id || '';  llmProviderRegistryCache.enabled = !!(document.getElementById('llmProvidersEnabled') && document.getElementById('llmProvidersEnabled').checked);  llmProviderRegistryCache.smart_route_single_device = !!(document.getElementById('llmProvidersSmartRouteSingle') && document.getElementById('llmProvidersSmartRouteSingle').checked);  try {    const data = await api('/api/admin/llm/providers', { method: 'PUT', body: JSON.stringify(buildLLMProviderPayload()) });    llmProviderRegistryCache = llmProviderRegistrySnapshot(data);    lpEnsureSelection();    renderLLMProviders();    const saveMsg = (llmProviderRegistryCache.providers || []).length ? lp('saveDone') : lp('saveDoneEmpty');    setOutput(saveMsg);    showToast(saveMsg, 'success');  } catch (err) {    const msg = lp('saveFailed', { error: err.message });    setOutput(msg);    showToast(msg, 'error');  }}async function saveLLMProviders() {  const wasEditing = llmProviderDialogMode === 'edit' && !!llmProviderSelectedId;  const createMode = llmProviderDialogMode === 'create';  if (!llmProviderRegistryCache) llmProviderRegistryCache = { enabled: false, current_provider_id: '', smart_route_single_device: false, providers: [], expose_api_base_url: '', expose_base_url: '', expose_models_url: '', available_models: [], auth_mode: '', auth_hint: '', hints: [], downstream_max_concurrency: 100, user_rate_limit_per_minute: 120, user_rate_limit_burst: 20 };  const draft = readSelectedLLMProviderForm();  const hasDraft = formHasLLMProviderDraft();  const providers = llmProviderRegistryCache.providers || [];  if (!providers.length && !hasDraft) {    const msg = lp('nothingToSave');    setOutput(msg);    showToast(msg, 'info');    return;  }  if (hasDraft) {    const validation = validateLLMProvider(draft, { requireKey: false });    if (!validation.ok) {      setOutput(validation.message);      showToast(validation.message, 'info');      return;    }  }  if (llmProviderSelectedId) {    if (!syncSelectedLLMProviderFromForm()) return;  } else if (hasDraft) {    if (!upsertLLMProviderFromForm(true)) return;  }  try {    const data = await api('/api/admin/llm/providers', { method: 'PUT', body: JSON.stringify(buildLLMProviderPayload()) });    llmProviderRegistryCache = llmProviderRegistrySnapshot(data);    lpEnsureSelection();    renderLLMProviders();    if (llmProviderSelectedId) scrollLLMProviderCardIntoView(llmProviderSelectedId);    const saveMsg = (llmProviderRegistryCache.providers || []).length ? (wasEditing ? lp('updateDone') : lp('createDone')) : lp('saveDoneEmpty');    setOutput(saveMsg);    showToast(saveMsg, 'success');    closeLLMProviderDialog();    if (createMode) resetLLMProviderIdSuggestionState();  } catch (err) {    const msg = lp('saveFailed', { error: err.message });    setOutput(msg);    showToast(msg, 'error');  }}async function runLLMProviderTest(provider) {  const data = await api('/api/admin/llm/providers/test', { method: 'POST', body: JSON.stringify({ id: provider.id, name: provider.name, api_url: provider.api_url, api_key: provider.api_key || '', model: provider.model, protocol: provider.protocol, wire_api: provider.wire_api || 'chat', agent_type: provider.agent_type || '' }) });  return { success: !!data.success, message: data.success ? lp('testOk', { ms: String(data.latency_ms || 0), reply: data.reply || '' }) : lp('testFail', { error: data.error || 'unknown' }) };}async function generateLLMProviderTestKey() {  const btn = document.getElementById('llmProvidersGenerateTestKeyBtn');  if (btn) { btn.disabled = true; btn.textContent = lp('testKeyRunning'); }  setLLMProviderTestKeyResult('');  try {    const data = await api('/api/admin/llm/providers/test-key', { method: 'POST', body: JSON.stringify({}) });    const token = String(data.access_token || '');    const message = lp('testKeyResult', { email: String(data.email || ''), token: token, days: String(data.expires_in_days || 30) });    setLLMProviderTestKeyResult(message);    const done = lp('testKeyDone', { email: String(data.email || '') });    setOutput(done);    showToast(done, 'success');  } catch (err) {    const msg = lp('testKeyFail', { error: err.message });    setLLMProviderTestKeyResult(msg);    setOutput(msg);    showToast(msg, 'error');  } finally {    if (btn) { btn.disabled = false; btn.textContent = lp('generateTestKey'); }  }}async function testSelectedLLMProvider() {  let provider = null;  if (llmProviderSelectedId) {    if (!syncSelectedLLMProviderFromForm()) return;    provider = lpById(llmProviderSelectedId);  } else if (formHasLLMProviderDraft()) {    provider = readSelectedLLMProviderForm();  }  if (!provider) { showToast(lp('providerRequired'), 'info'); return; }  const validation = validateLLMProvider(provider, { requireKey: !(provider.has_api_key || provider.api_key) });  if (!validation.ok) {    setOutput(validation.message);    showToast(validation.message, 'info');    return;  }  const btn = document.getElementById('llmProviderTestBtn');  const result = document.getElementById('llmProviderTestResult');  if (btn) { btn.disabled = true; btn.textContent = lp('testRunning'); }  try {    const testResult = await runLLMProviderTest(provider);    if (result) { result.classList.remove('hidden'); result.textContent = testResult.message; }    setOutput(testResult.message);    showToast(testResult.message, testResult.success ? 'success' : 'error');  } catch (err) {    const msg = lp('testFail', { error: err.message });    if (result) { result.classList.remove('hidden'); result.textContent = msg; }    setOutput(msg);    showToast(msg, 'error');  } finally {    if (btn) { btn.disabled = false; btn.textContent = lp('test'); }  }}async function testLLMProviderCard(id) {  const provider = lpById(id);  if (!provider) { showToast(lp('providerRequired'), 'info'); return; }  const validation = validateLLMProvider(provider, { requireKey: !(provider.has_api_key || provider.api_key) });  if (!validation.ok) {    llmProviderCardTestState[id] = { success: false, running: false, message: validation.message };    renderLLMProviders();    setOutput(validation.message);    showToast(validation.message, 'info');    return;  }  llmProviderCardTestState[id] = { success: false, running: true, message: lp('testRunning') };  renderLLMProviders();  try {    const testResult = await runLLMProviderTest(provider);    llmProviderCardTestState[id] = { success: testResult.success, running: false, message: testResult.message };    renderLLMProviders();    setOutput(testResult.message);    showToast(testResult.message, testResult.success ? 'success' : 'error');  } catch (err) {    const msg = lp('testFail', { error: err.message });    llmProviderCardTestState[id] = { success: false, running: false, message: msg };    renderLLMProviders();    setOutput(msg);    showToast(msg, 'error');  }}function registerLLMProviderTab() {  if (!window.AdminTabRegistry || typeof window.AdminTabRegistry.registerTab !== 'function') return;  window.AdminTabRegistry.registerTab({    id: 'llmproviders',    title: function() { return lp('tabTitle'); },    subtitle: function() { return lp('tabSubtitle'); },    onOpen: function() { ensureLLMProviderModalUI(); loadLLMProviders(); }  });}if (window.AdminTabRegistry && typeof window.AdminTabRegistry.onLanguageChange === 'function') {  window.AdminTabRegistry.onLanguageChange(function() {    applyLLMProvidersI18n();  });}function llmProviderTenantScopedRefresh() {  const profile = typeof adminProfile === 'function' ? adminProfile() : null;  return !!(profile && String(profile.scope || '').toLowerCase() === 'tenant');}window.loadLlmProviders = loadLLMProviders;window.changeLLMProviderPage = changeLLMProviderPage;window.setLLMProviderFilter = setLLMProviderFilter;window.setLLMProviderProtocolFilter = setLLMProviderProtocolFilter;window.setLLMProviderKeyFilter = setLLMProviderKeyFilter;window.testLLMProviderCard = testLLMProviderCard;window.generateLLMProviderTestKey = generateLLMProviderTestKey;window.exportLLMProvidersJSON = exportLLMProvidersJSON;window.triggerLLMProvidersImport = triggerLLMProvidersImport;window.importLLMProvidersJSON = importLLMProvidersJSON;window.openLlmProviderTab = function() { if (typeof openTab === 'function') openTab('llmproviders'); };window.saveLLMProviderGlobals = saveLLMProviderGlobals;window.saveLLMProviders = saveLLMProviders;
Object.assign(LLM_PROVIDER_I18N.en, {
  inputPricePerM: 'Input Price / 1M Tokens (RMB)',
  outputPricePerM: 'Output Price / 1M Tokens (RMB)',
  costRMB: 'Charge (RMB)',
  usageCost: 'Charge',
  pricePerMShort: 'Price / 1M',
  inputPriceShort: 'In',
  outputPriceShort: 'Out'
});
Object.assign(LLM_PROVIDER_I18N.zh, {
  inputPricePerM: '\u8f93\u5165\u4ef7\u683c / 100\u4e07 Tokens\uff08\u5143\uff09',
  outputPricePerM: '\u8f93\u51fa\u4ef7\u683c / 100\u4e07 Tokens\uff08\u5143\uff09',
  costRMB: '\u8ba1\u8d39\uff08\u5143\uff09',
  usageCost: '\u8ba1\u8d39',
  pricePerMShort: 'M Token \u5355\u4ef7',
  inputPriceShort: '\u8f93\u5165',
  outputPriceShort: '\u8f93\u51fa'
});
function llmProviderNormalizePricePerM(value, fallback) {
  fallback = Number(fallback || 0);
  if (value === undefined || value === null || value === '') return fallback >= 0 ? fallback : 0;
  value = Number(value);
  return Number.isFinite(value) && value >= 0 ? value : (fallback >= 0 ? fallback : 0);
}
function llmProviderFormatMoney(value) {
  var n = Number(value || 0);
  if (!Number.isFinite(n)) n = 0;
  return n.toFixed(n >= 100 ? 2 : 4).replace(/0+$/, '').replace(/\.$/, '') || '0';
}
function llmProviderPriceChipHTML(provider) {
  var input = llmProviderNormalizePricePerM(provider && provider.input_price_per_m_tokens_rmb, 1);
  var output = llmProviderNormalizePricePerM(provider && provider.output_price_per_m_tokens_rmb, 2);
  return '<span data-role="llm-provider-price-chip" style="display:inline-flex;flex-direction:column;justify-content:center;gap:2px;min-width:136px;max-width:164px;height:38px;padding:4px 10px;border:1px solid var(--line);border-radius:12px;background:rgba(255,255,255,.78);box-shadow:0 1px 0 rgba(16,24,40,.04);white-space:nowrap">'
    + '<span style="font-size:9px;line-height:1;color:var(--muted);font-weight:800;text-transform:uppercase;letter-spacing:0">' + escapeHtml(lp('pricePerMShort')) + '</span>'
    + '<span class="mono" style="font-size:10px;line-height:1.1;color:var(--ink);font-weight:800">' + escapeHtml(lp('inputPriceShort')) + ' \u00a5' + escapeHtml(llmProviderFormatMoney(input)) + ' / ' + escapeHtml(lp('outputPriceShort')) + ' \u00a5' + escapeHtml(llmProviderFormatMoney(output)) + '</span>'
    + '</span>';
}
function llmProviderInstallPriceChip(card, provider) {
  if (!card || !provider || card.querySelector('[data-role="llm-provider-price-chip"]')) return;
  var actions = null;
  Array.prototype.slice.call(card.querySelectorAll('.actions')).some(function(candidate) {
    var html = candidate.innerHTML || '';
    if (html.indexOf('testLLMProviderCard') >= 0 || html.indexOf('editLLMProvider') >= 0 || html.indexOf('removeLLMProviderById') >= 0) {
      actions = candidate;
      return true;
    }
    return false;
  });
  if (actions) {
    actions.insertAdjacentHTML('afterbegin', llmProviderPriceChipHTML(provider));
    return;
  }
  var metaRow = card.querySelector('.llm-provider-meta-row');
  if (metaRow) metaRow.insertAdjacentHTML('beforeend', llmProviderPriceChipHTML(provider));
}
const baseLpUsagePricing = typeof lpUsage === 'function' ? lpUsage : null;
if (baseLpUsagePricing) {
  lpUsage = function(usage) {
    var out = baseLpUsagePricing(usage);
    out.input_cost_rmb = Number(usage && usage.input_cost_rmb || 0);
    out.output_cost_rmb = Number(usage && usage.output_cost_rmb || 0);
    out.total_cost_rmb = Number(usage && usage.total_cost_rmb || 0);
    return out;
  };
}
const baseLpClonePricing = typeof lpClone === 'function' ? lpClone : null;
if (baseLpClonePricing) {
  lpClone = function(provider) {
    var next = baseLpClonePricing(provider);
    next.input_price_per_m_tokens_rmb = llmProviderNormalizePricePerM(provider && provider.input_price_per_m_tokens_rmb, 1);
    next.output_price_per_m_tokens_rmb = llmProviderNormalizePricePerM(provider && provider.output_price_per_m_tokens_rmb, 2);
    return next;
  };
}
function llmProviderEnsurePricingInputs() {
  var model = document.getElementById('llmProviderModel');
  if (!model || document.getElementById('llmProviderInputPricePerM')) return;
  var anchor = model.parentElement;
  if (!anchor || !anchor.parentElement) return;
  var inputWrap = document.createElement('div');
  inputWrap.innerHTML = '<label id="llmProviderInputPricePerMLabel"></label><input id="llmProviderInputPricePerM" type="number" min="0" step="0.0001" placeholder="1">';
  var outputWrap = document.createElement('div');
  outputWrap.innerHTML = '<label id="llmProviderOutputPricePerMLabel"></label><input id="llmProviderOutputPricePerM" type="number" min="0" step="0.0001" placeholder="2">';
  anchor.parentElement.insertBefore(outputWrap, anchor.nextSibling);
  anchor.parentElement.insertBefore(inputWrap, outputWrap);
}
function llmProviderWritePricingForm(provider) {
  llmProviderEnsurePricingInputs();
  provider = provider || {};
  _s('llmProviderInputPricePerM', 'value', String(llmProviderNormalizePricePerM(provider.input_price_per_m_tokens_rmb, 1)));
  _s('llmProviderOutputPricePerM', 'value', String(llmProviderNormalizePricePerM(provider.output_price_per_m_tokens_rmb, 2)));
}
function llmProviderReadPricingForm() {
  llmProviderEnsurePricingInputs();
  return {
    input_price_per_m_tokens_rmb: llmProviderNormalizePricePerM(document.getElementById('llmProviderInputPricePerM') && document.getElementById('llmProviderInputPricePerM').value, 1),
    output_price_per_m_tokens_rmb: llmProviderNormalizePricePerM(document.getElementById('llmProviderOutputPricePerM') && document.getElementById('llmProviderOutputPricePerM').value, 2)
  };
}
const baseApplyLLMProvidersI18nPricing = typeof applyLLMProvidersI18n === 'function' ? applyLLMProvidersI18n : null;
if (baseApplyLLMProvidersI18nPricing) {
  applyLLMProvidersI18n = function() {
    baseApplyLLMProvidersI18nPricing();
    llmProviderEnsurePricingInputs();
    _s('llmProviderInputPricePerMLabel', 'textContent', lp('inputPricePerM'));
    _s('llmProviderOutputPricePerMLabel', 'textContent', lp('outputPricePerM'));
  };
  applyLLMProvidersI18n();
}
const baseReadSelectedLLMProviderFormPricing = typeof readSelectedLLMProviderForm === 'function' ? readSelectedLLMProviderForm : null;
if (baseReadSelectedLLMProviderFormPricing) {
  readSelectedLLMProviderForm = function() {
    var next = baseReadSelectedLLMProviderFormPricing();
    var pricing = llmProviderReadPricingForm();
    next.input_price_per_m_tokens_rmb = pricing.input_price_per_m_tokens_rmb;
    next.output_price_per_m_tokens_rmb = pricing.output_price_per_m_tokens_rmb;
    return next;
  };
}
const baseClearLLMProviderFormPricing = typeof clearLLMProviderForm === 'function' ? clearLLMProviderForm : null;
if (baseClearLLMProviderFormPricing) {
  clearLLMProviderForm = function() {
    baseClearLLMProviderFormPricing();
    llmProviderWritePricingForm({});
  };
}
const baseOpenLLMProviderDialogPricing = typeof openLLMProviderDialog === 'function' ? openLLMProviderDialog : null;
if (baseOpenLLMProviderDialogPricing) {
  openLLMProviderDialog = function(mode) {
    baseOpenLLMProviderDialogPricing(mode);
    llmProviderWritePricingForm(mode === 'edit' ? lpById(llmProviderSelectedId) : {});
  };
}
const baseBuildLLMProviderPayloadPricing = typeof buildLLMProviderPayload === 'function' ? buildLLMProviderPayload : null;
if (baseBuildLLMProviderPayloadPricing) {
  buildLLMProviderPayload = function() {
    var payload = baseBuildLLMProviderPayloadPricing();
    payload.providers = (payload.providers || []).map(function(p) {
      var src = lpById(p.id) || p;
      p.input_price_per_m_tokens_rmb = llmProviderNormalizePricePerM(src && src.input_price_per_m_tokens_rmb, 1);
      p.output_price_per_m_tokens_rmb = llmProviderNormalizePricePerM(src && src.output_price_per_m_tokens_rmb, 2);
      return p;
    });
    return payload;
  };
}
const baseImportedPayloadPricing = typeof llmProviderImportedPayload === 'function' ? llmProviderImportedPayload : null;
if (baseImportedPayloadPricing) {
  llmProviderImportedPayload = function(raw) {
    var sourceProviders = raw && Array.isArray(raw.providers) ? raw.providers : [];
    var sourceByID = {};
    sourceProviders.forEach(function(src) {
      var id = lpNormalizeId(src && src.id || '');
      if (id) sourceByID[id] = src;
    });
    var payload = baseImportedPayloadPricing(raw);
    payload.providers = (payload.providers || []).map(function(p, idx) {
      var src = sourceByID[p && p.id || ''] || sourceProviders[idx] || p;
      p.input_price_per_m_tokens_rmb = llmProviderNormalizePricePerM(src && src.input_price_per_m_tokens_rmb, 1);
      p.output_price_per_m_tokens_rmb = llmProviderNormalizePricePerM(src && src.output_price_per_m_tokens_rmb, 2);
      return p;
    });
    return payload;
  };
}
const baseRenderLLMProvidersPricing = typeof renderLLMProviders === 'function' ? renderLLMProviders : null;
if (baseRenderLLMProvidersPricing) {
  renderLLMProviders = function() {
    baseRenderLLMProvidersPricing();
    document.querySelectorAll('.llm-provider-meta-row').forEach(function(row) {
      var card = row.closest('[data-provider-id]');
      var id = card && card.getAttribute('data-provider-id') || '';
      var provider = lpById(id);
      if (!provider || row.querySelector('[data-role="llm-provider-cost"]')) return;
      var usage = lpUsage(provider.usage);
      row.insertAdjacentHTML('beforeend', '<span data-role="llm-provider-cost">' + escapeHtml(lp('usageCost')) + ': \u00a5' + escapeHtml(llmProviderFormatMoney(usage.total_cost_rmb)) + '</span>');
    });
    document.querySelectorAll('.item[data-provider-id]').forEach(function(card) {
      var provider = lpById(card.getAttribute('data-provider-id') || '');
      llmProviderInstallPriceChip(card, provider);
    });
  };
}
function lpFormatInt(value) { const locale = currentLang === 'zh' ? 'zh-CN' : 'en-US'; return Number(value || 0).toLocaleString(locale); }
function lpCacheSummaryLabels() { return currentLang === 'zh' ? { title: 'Prompt \u7f13\u5b58\uff08\u63d0\u4f9b\u5546\u4fa7\uff09', desc: '\u57fa\u4e8e\u63d0\u4f9b\u5546\u7528\u91cf\u7edf\u8ba1\uff0c\u5c55\u793a Prompt \u7f13\u5b58\u547d\u4e2d\u7387\u4e0e Token \u590d\u7528\u60c5\u51b5\u3002', cacheRate: 'Prompt \u7f13\u5b58\u7387', reuseRate: '\u7f13\u5b58\u590d\u7528\u7387', readTokens: '\u7f13\u5b58\u8bfb\u53d6 Token', writeTokens: '\u7f13\u5b58\u5199\u5165 Token', requests: '\u7f13\u5b58\u8bf7\u6c42\u6570', reuseHint: '\u7f13\u5b58\u8bfb\u53d6 / \u8f93\u5165', readHint: '\u4ece\u7f13\u5b58\u8bfb\u53d6', writeHint: '\u5199\u5165\u7f13\u5b58', requestHint: 'Prompt \u7f13\u5b58\u547d\u4e2d', byProviderTitle: '\u6309\u63d0\u4f9b\u5546\u5206\u7c7b', tableHead: ['\u63d0\u4f9b\u5546', '\u8f93\u5165 Token \u603b\u8ba1', '\u7f13\u5b58\u8bfb\u53d6', '\u7f13\u5b58\u5199\u5165', '\u7f13\u5b58\u590d\u7528\u7387', '\u7f13\u5b58\u7387', '\u7f13\u5b58\u8bf7\u6c42\u6570'] } : { title: 'Prompt Cache (Provider Side)', desc: 'Summarizes provider-side prompt cache hits and token reuse from usage data.', cacheRate: 'Prompt Cache Rate', reuseRate: 'Cache Reuse Rate', readTokens: 'Cache Read Tokens', writeTokens: 'Cache Write Tokens', requests: 'Cached Requests', reuseHint: 'cache read / input', readHint: 'read from cache', writeHint: 'written to cache', requestHint: 'prompt cache hits', byProviderTitle: 'By Provider', tableHead: ['Provider', 'Input Tokens', 'Cache Read', 'Cache Write', 'Cache Reuse', 'Cache Rate', 'Cache Requests'] }; }
function ensureLLMProviderCacheSummary() { var list = document.getElementById('llmProviderList'); if (!list || !list.parentElement || !list.parentElement.parentElement) return null; var root = document.getElementById('llmProviderCacheSummary'); if (root) return root; root = document.createElement('div'); root.id = 'llmProviderCacheSummary'; root.style.marginTop = '18px'; list.parentElement.parentElement.insertBefore(root, list.parentElement); return root; }
function renderLLMProviderCacheSummary() { var root = ensureLLMProviderCacheSummary(); if (!root) return; var labels = lpCacheSummaryLabels(); var providers = llmProviderRegistryCache && llmProviderRegistryCache.providers || []; var total = { input_tokens: 0, cached_input_tokens: 0, cache_write_tokens: 0, requests: 0, cached_requests: 0 }; providers.forEach(function(p) { var u = lpUsage(p && p.usage); total.input_tokens += u.input_tokens; total.cached_input_tokens += u.cached_input_tokens; total.cache_write_tokens += u.cache_write_tokens; total.requests += u.requests; total.cached_requests += u.cached_requests; }); var cacheRate = lpRatePercent(total.cached_requests, total.requests); var reuseRate = lpRatePercent(total.cached_input_tokens, total.input_tokens); root.innerHTML = '<div class="item"><div class="item-head"><div><div class="item-title">' + escapeHtml(labels.title) + '</div><div class="item-meta">' + escapeHtml(labels.desc) + '</div></div></div><div class="metrics" style="max-width:none;margin:8px 0 0;grid-template-columns:repeat(auto-fit,minmax(170px,1fr))"><div class="metric"><label>' + escapeHtml(labels.cacheRate) + '</label><strong style="color:var(--ok)">' + escapeHtml(cacheRate) + '</strong><span>' + lpFormatInt(total.cached_requests) + ' / ' + lpFormatInt(total.requests) + '</span></div><div class="metric"><label>' + escapeHtml(labels.reuseRate) + '</label><strong style="color:#4b82d8">' + escapeHtml(reuseRate) + '</strong><span>' + escapeHtml(labels.reuseHint) + '</span></div><div class="metric"><label>' + escapeHtml(labels.readTokens) + '</label><strong style="color:#10aeca">' + escapeHtml(lpFormatInt(total.cached_input_tokens)) + '</strong><span>' + escapeHtml(labels.readHint) + '</span></div><div class="metric"><label>' + escapeHtml(labels.writeTokens) + '</label><strong style="color:#9b5de5">' + escapeHtml(lpFormatInt(total.cache_write_tokens)) + '</strong><span>' + escapeHtml(labels.writeHint) + '</span></div><div class="metric"><label>' + escapeHtml(labels.requests) + '</label><strong>' + escapeHtml(lpFormatInt(total.cached_requests)) + '</strong><span>' + escapeHtml(labels.requestHint) + '</span></div></div></div>'; }
const baseRenderLLMProvidersForCache = typeof renderLLMProviders === 'function' ? renderLLMProviders : null;
if (baseRenderLLMProvidersForCache) { renderLLMProviders = function() { baseRenderLLMProvidersForCache(); renderLLMProviderCacheSummary(); }; }renderLLMProviderCacheSummary = function() { var root = ensureLLMProviderCacheSummary(); if (!root) return; var labels = lpCacheSummaryLabels(); var providers = llmProviderRegistryCache && llmProviderRegistryCache.providers || []; var total = { input_tokens: 0, cached_input_tokens: 0, cache_write_tokens: 0, requests: 0, cached_requests: 0 }; providers.forEach(function(p) { var u = lpUsage(p && p.usage); total.input_tokens += u.input_tokens; total.cached_input_tokens += u.cached_input_tokens; total.cache_write_tokens += u.cache_write_tokens; total.requests += u.requests; total.cached_requests += u.cached_requests; }); var cacheRate = lpRatePercent(total.cached_requests, total.requests); var reuseRate = lpRatePercent(total.cached_input_tokens, total.input_tokens); var byProviderTitle = labels.byProviderTitle; var tableHead = labels.tableHead; var rows = providers.map(function(p) { var u = lpUsage(p && p.usage); return '<div class="row" style="grid-template-columns:1.2fr 1fr 1fr 1fr .9fr .9fr 1fr;padding:8px 10px"><div><strong style="font-size:12px">' + escapeHtml(p.name || p.id || '-') + '</strong><div class="item-meta" style="font-size:10px">' + escapeHtml(String(p.id || '')) + '</div></div><div>' + escapeHtml(lpFormatInt(u.input_tokens)) + '</div><div style="color:#10aeca">' + escapeHtml(lpFormatInt(u.cached_input_tokens)) + '</div><div style="color:#9b5de5">' + escapeHtml(lpFormatInt(u.cache_write_tokens)) + '</div><div style="color:#4b82d8;font-weight:800">' + escapeHtml(lpRatePercent(u.cached_input_tokens, u.input_tokens)) + '</div><div style="color:var(--ok);font-weight:800">' + escapeHtml(lpRatePercent(u.cached_requests, u.requests)) + '</div><div>' + escapeHtml(lpFormatInt(u.cached_requests)) + ' / ' + escapeHtml(lpFormatInt(u.requests)) + '</div></div>'; }).join(''); if (!rows) rows = '<div class="hint">' + escapeHtml(lp('emptyList')) + '</div>'; var header = '<div class="row header" style="grid-template-columns:1.2fr 1fr 1fr 1fr .9fr .9fr 1fr;padding:8px 10px">' + tableHead.map(function(h) { return '<div>' + escapeHtml(h) + '</div>'; }).join('') + '</div>'; root.innerHTML = '<div class="item" style="padding:12px 14px"><div class="item-head" style="align-items:flex-start;gap:10px"><div><div class="item-title" style="font-size:14px">' + escapeHtml(labels.title) + '</div><div class="item-meta">' + escapeHtml(labels.desc) + '</div></div><div class="actions" style="margin-left:auto"><button class="btn-ghost" type="button" id="llmProvidersReloadBtn" onclick="loadLLMProviders()">' + escapeHtml(lp('reload')) + '</button></div></div><div class="metrics" style="max-width:none;margin:8px 0 0;grid-template-columns:repeat(auto-fit,minmax(150px,1fr));gap:8px"><div class="metric" style="padding:12px 13px"><label>' + escapeHtml(labels.cacheRate) + '</label><strong style="color:var(--ok)">' + escapeHtml(cacheRate) + '</strong><span>' + lpFormatInt(total.cached_requests) + ' / ' + lpFormatInt(total.requests) + '</span></div><div class="metric" style="padding:12px 13px"><label>' + escapeHtml(labels.reuseRate) + '</label><strong style="color:#4b82d8">' + escapeHtml(reuseRate) + '</strong><span>' + escapeHtml(labels.reuseHint) + '</span></div><div class="metric" style="padding:12px 13px"><label>' + escapeHtml(labels.readTokens) + '</label><strong style="color:#10aeca">' + escapeHtml(lpFormatInt(total.cached_input_tokens)) + '</strong><span>' + escapeHtml(labels.readHint) + '</span></div><div class="metric" style="padding:12px 13px"><label>' + escapeHtml(labels.writeTokens) + '</label><strong style="color:#9b5de5">' + escapeHtml(lpFormatInt(total.cache_write_tokens)) + '</strong><span>' + escapeHtml(labels.writeHint) + '</span></div><div class="metric" style="padding:12px 13px"><label>' + escapeHtml(labels.requests) + '</label><strong>' + escapeHtml(lpFormatInt(total.cached_requests)) + '</strong><span>' + escapeHtml(labels.requestHint) + '</span></div></div><div style="margin-top:12px"><div class="item-title" style="margin-bottom:6px;font-size:13px">' + escapeHtml(byProviderTitle) + '</div><div class="item-meta" style="margin-bottom:8px">' + escapeHtml(labels.desc) + '</div>' + header + rows + '</div></div>'; };registerLLMProviderTab();ensureLLMProviderModalUI();applyLLMProvidersI18n();if (typeof token === 'function' && token() && llmProviderTenantScopedRefresh() && localStorage.getItem(activeTabKey) === 'llmproviders') {  openTab('llmproviders');}

const baseApplyLLMProvidersI18nConcurrency = typeof applyLLMProvidersI18n === 'function' ? applyLLMProvidersI18n : null;
if (baseApplyLLMProvidersI18nConcurrency) { applyLLMProvidersI18n = function() { baseApplyLLMProvidersI18nConcurrency(); _s('llmProviderMaxQueueWaitersLabel', 'textContent', lp('maxQueueWaiters')); _s('llmProviderQueueTimeoutMsLabel', 'textContent', lp('queueTimeoutMs')); }; applyLLMProvidersI18n(); }




const baseApplyLLMProvidersI18nDownstream = typeof applyLLMProvidersI18n === 'function' ? applyLLMProvidersI18n : null;
function llmProviderNormalizeDownstreamConcurrency(value) {
  value = Number(value || 0);
  return Number.isFinite(value) && value > 0 ? Math.floor(value) : 100;
}
function ensureLLMProviderDownstreamState() {
  if (!llmProviderRegistryCache) return 100;
  llmProviderRegistryCache.downstream_max_concurrency = llmProviderNormalizeDownstreamConcurrency(llmProviderRegistryCache.downstream_max_concurrency);
  return llmProviderRegistryCache.downstream_max_concurrency;
}
function llmProviderSyncGlobalControls() {
  var active = document.activeElement;
  var downstream = document.getElementById('llmProvidersDownstreamConcurrency');
  var rateLimit = document.getElementById('llmProvidersUserRateLimitPerMinute');
  var burst = document.getElementById('llmProvidersUserRateLimitBurst');
  if (downstream && active !== downstream) downstream.value = String(llmProviderRegistryCache ? ensureLLMProviderDownstreamState() : 100);
  if (rateLimit && active !== rateLimit) rateLimit.value = String(llmProviderRegistryCache ? llmProviderNormalizeUserRateLimit(llmProviderRegistryCache.user_rate_limit_per_minute, 120) : 120);
  if (burst && active !== burst) burst.value = String(llmProviderRegistryCache ? llmProviderNormalizeUserRateLimit(llmProviderRegistryCache.user_rate_limit_burst, 20) : 20);
}
function initLLMProviderGlobalBindings() {
  if (initLLMProviderGlobalBindings.done) return;
  initLLMProviderGlobalBindings.done = true;
  var downstream = document.getElementById('llmProvidersDownstreamConcurrency');
  var rateLimit = document.getElementById('llmProvidersUserRateLimitPerMinute');
  var burst = document.getElementById('llmProvidersUserRateLimitBurst');
  if (downstream) downstream.addEventListener('input', function() {
    if (!llmProviderRegistryCache) return;
    llmProviderRegistryCache.downstream_max_concurrency = llmProviderNormalizeDownstreamConcurrency(downstream.value);
  });
  if (rateLimit) rateLimit.addEventListener('input', function() {
    if (!llmProviderRegistryCache) return;
    llmProviderRegistryCache.user_rate_limit_per_minute = llmProviderNormalizeUserRateLimit(rateLimit.value, 120);
  });
  if (burst) burst.addEventListener('input', function() {
    if (!llmProviderRegistryCache) return;
    llmProviderRegistryCache.user_rate_limit_burst = llmProviderNormalizeUserRateLimit(burst.value, 20);
  });
}
initLLMProviderGlobalBindings.done = false;if (baseApplyLLMProvidersI18nDownstream) {
  applyLLMProvidersI18n = function() {
    baseApplyLLMProvidersI18nDownstream();
    _s('llmProvidersDownstreamConcurrencyLabel', 'textContent', currentLang === 'zh' ? '\u5bf9\u5916\u670d\u52a1\u5e76\u53d1\u6570' : 'Downstream Concurrency');
    _s('llmProvidersDownstreamConcurrencyHintLabel', 'textContent', currentLang === 'zh' ? '\u7acb\u5373\u751f\u6548' : 'Applies Immediately');
    _s('llmProvidersDownstreamConcurrencyHint', 'textContent', currentLang === 'zh' ? '\u9650\u5236 LLM EndPoint \u5bf9\u5916\u5165\u7ad9\u8bf7\u6c42\u7684\u5e76\u53d1\u6570\u3002\u4fdd\u5b58\u540e\u7acb\u5373\u751f\u6548\uff0c\u9ed8\u8ba4 100\u3002' : 'Limits public inbound concurrency for the LLM EndPoint. Saving applies immediately. Default is 100.');
  };
  applyLLMProvidersI18n();
}

const baseRenderLLMProvidersDownstream = typeof renderLLMProviders === 'function' ? renderLLMProviders : null;
if (baseRenderLLMProvidersDownstream) {
  renderLLMProviders = function() {
    if (llmProviderRegistryCache) ensureLLMProviderDownstreamState();
    baseRenderLLMProvidersDownstream();
    llmProviderSyncGlobalControls();
  };
}

const baseBuildLLMProviderPayloadDownstream = typeof buildLLMProviderPayload === 'function' ? buildLLMProviderPayload : null;
if (baseBuildLLMProviderPayloadDownstream) {
  buildLLMProviderPayload = function() {
    var payload = baseBuildLLMProviderPayloadDownstream();
    var input = document.getElementById('llmProvidersDownstreamConcurrency');
    var value = llmProviderNormalizeDownstreamConcurrency(input && input.value);
    if (llmProviderRegistryCache) llmProviderRegistryCache.downstream_max_concurrency = value;
    payload.downstream_max_concurrency = value;
    return payload;
  };
}

const baseLoadLLMProvidersDownstream = typeof loadLLMProviders === 'function' ? loadLLMProviders : null;
if (baseLoadLLMProvidersDownstream) {
  loadLLMProviders = async function() {
    await baseLoadLLMProvidersDownstream();
    if (llmProviderRegistryCache) {
      llmProviderRegistryCache.downstream_max_concurrency = llmProviderNormalizeDownstreamConcurrency(llmProviderRegistryCache.downstream_max_concurrency);
    }
    llmProviderSyncGlobalControls();
  };
}
Object.assign(LLM_PROVIDER_I18N.en, {
  accessLogTitle: 'Access Logs',
  accessLogDesc: 'Recent public MaClaw requests to this LLM EndPoint.',
  accessLogBadge: 'MaClaw',
  accessLogIPs: 'Unique IPs',
  accessLogRequests: 'Requests',
  accessLogLatest: 'Latest Request',
  accessLogEmpty: 'No access requests recorded yet.',
  accessLogLatestEmpty: 'No recent request yet.',
  viewAccessLogs: 'View Access Logs',
  accessLogDialogTitle: 'LLM EndPoint Access Logs',
  accessLogReload: 'Reload Logs',
  accessLogColIP: 'IP',
  accessLogColTime: 'Time',
  accessLogColTokens: 'Tokens',
  accessLogColStatus: 'Status',
  accessLogColModel: 'Model',
  accessLogColProvider: 'Provider',
  accessLogColEmail: 'User',
  accessLogColRequest: 'Raw Request',
  accessLogLoadFailed: 'Load access logs failed: {error}'
});
Object.assign(LLM_PROVIDER_I18N.zh, {
  accessLogTitle: '\u8bbf\u95ee\u65e5\u5fd7',
  accessLogDesc: '\u67e5\u770b MaClaw \u901a\u8fc7\u8fd9\u4e2a LLM EndPoint \u53d1\u8d77\u7684\u8bf7\u6c42\u3002',
  accessLogBadge: 'MaClaw',
  accessLogIPs: '\u8bbf\u95ee IP \u6570',
  accessLogRequests: '\u8bf7\u6c42\u6b21\u6570',
  accessLogLatest: '\u6700\u65b0\u8bf7\u6c42',
  accessLogEmpty: '\u6682\u65e0\u8bbf\u95ee\u65e5\u5fd7\u3002',
  accessLogLatestEmpty: '\u6682\u65e0\u6700\u65b0\u8bf7\u6c42\u3002',
  viewAccessLogs: '\u67e5\u770b\u8bbf\u95ee\u65e5\u5fd7',
  accessLogDialogTitle: 'LLM EndPoint \u8bbf\u95ee\u65e5\u5fd7',
  accessLogReload: '\u91cd\u65b0\u52a0\u8f7d',
  accessLogColIP: 'IP',
  accessLogColTime: '\u65f6\u95f4',
  accessLogColTokens: 'Token',
  accessLogColStatus: '\u72b6\u6001',
  accessLogColModel: '\u6a21\u578b',
  accessLogColProvider: '\u4e0a\u6e38',
  accessLogColEmail: '\u7528\u6237',
  accessLogColRequest: '\u539f\u59cb\u8bf7\u6c42',
  accessLogLoadFailed: '\u52a0\u8f7d\u8bbf\u95ee\u65e5\u5fd7\u5931\u8d25: {error}'
});

let llmProviderAccessLogsCache = null;

function llmProviderAccessLocaleTime(value) {
  if (!value) return lp('accessLogLatestEmpty');
  const dt = new Date(value);
  if (isNaN(dt.getTime())) return String(value || '');
  return dt.toLocaleString(currentLang === 'zh' ? 'zh-CN' : 'en-US');
}

function ensureLLMEndpointAccessLogDialog() {
  var overlay = document.getElementById('llmEndpointAccessLogOverlay');
  if (overlay) return overlay;
  overlay = document.createElement('div');
  overlay.id = 'llmEndpointAccessLogOverlay';
  overlay.className = 'session-modal-overlay';
  if (window.AdminUI && typeof AdminUI.bindModalOverlayDismiss === 'function') AdminUI.bindModalOverlayDismiss(overlay, function() { overlay.classList.remove('show'); });
  overlay.innerHTML = '' +
    '<div class="session-modal" style="width:min(980px,calc(100% - 48px))">' +
    '<button class="close-btn" type="button" onclick="document.getElementById(\'llmEndpointAccessLogOverlay\').classList.remove(\'show\')">&times;</button>' +
    '<div class="head" style="margin-bottom:10px"><div><h3 id="llmEndpointAccessLogDialogTitle"></h3><div class="desc" id="llmEndpointAccessLogDialogDesc"></div></div><div class="actions"><button class="btn-ghost" type="button" id="llmEndpointAccessLogReloadBtn" onclick="reloadLLMEndpointAccessLogs()"></button></div></div>' +
    '<div id="llmEndpointAccessLogDialogBody"></div>' +
    '</div>';
  document.body.appendChild(overlay);
  return overlay;
}

function renderLLMEndpointAccessSummary() {
  var summary = llmProviderAccessLogsCache && llmProviderAccessLogsCache.summary || null;
  _s('llmProvidersAccessLogTitle', 'textContent', lp('accessLogTitle'));
  _s('llmProvidersAccessLogDesc', 'textContent', lp('accessLogDesc'));
  _s('llmProvidersAccessLogBadge', 'textContent', lp('accessLogBadge'));
  _s('llmProvidersViewAccessLogsBtn', 'textContent', lp('viewAccessLogs'));
  var metrics = document.getElementById('llmProvidersAccessLogMetrics');
  var latest = document.getElementById('llmProvidersAccessLogLatest');
  if (!metrics || !latest) return;
  if (!summary) {
    metrics.innerHTML = '<div class="hint">' + escapeHtml(lp('accessLogEmpty')) + '</div>';
    latest.textContent = lp('accessLogLatestEmpty');
    return;
  }
  metrics.innerHTML = '' +
    '<div class="metric" style="padding:12px 13px"><label>' + escapeHtml(lp('accessLogIPs')) + '</label><strong>' + escapeHtml(String(summary.unique_ip_count || 0)) + '</strong><span>IP</span></div>' +
    '<div class="metric" style="padding:12px 13px"><label>' + escapeHtml(lp('accessLogRequests')) + '</label><strong>' + escapeHtml(String(summary.total_requests || 0)) + '</strong><span>HTTP</span></div>';
  latest.textContent = lp('accessLogLatest') + ': ' + llmProviderAccessLocaleTime(summary.latest_request_at);
}

function renderLLMEndpointAccessLogDialog() {
  ensureLLMEndpointAccessLogDialog();
  _s('llmEndpointAccessLogDialogTitle', 'textContent', lp('accessLogDialogTitle'));
  _s('llmEndpointAccessLogDialogDesc', 'textContent', lp('accessLogDesc'));
  _s('llmEndpointAccessLogReloadBtn', 'textContent', lp('accessLogReload'));
  var root = document.getElementById('llmEndpointAccessLogDialogBody');
  if (!root) return;
  var logs = llmProviderAccessLogsCache && llmProviderAccessLogsCache.logs || [];
  if (!logs.length) {
    root.innerHTML = '<div class="hint">' + escapeHtml(lp('accessLogEmpty')) + '</div>';
    return;
  }
  root.innerHTML = logs.map(function(item) {
    var tokens = [Number(item.input_tokens || 0), Number(item.output_tokens || 0), Number(item.total_tokens || 0)].join(' / ') + ' | \u00a5' + llmProviderFormatMoney(item.total_cost_rmb);
    var model = [item.requested_model || '-', item.authorized_model || '-'].join(' -> ');
    var status = String(item.status_code || 0) + (item.error_code ? ' / ' + item.error_code : '');
    return '<div class="item" style="padding:12px 14px;margin-bottom:10px">'
      + '<div class="grid3" style="gap:8px">'
      + '<div><label>' + escapeHtml(lp('accessLogColIP')) + '</label><div class="mono">' + escapeHtml(item.client_ip || '-') + '</div></div>'
      + '<div><label>' + escapeHtml(lp('accessLogColTime')) + '</label><div class="mono">' + escapeHtml(llmProviderAccessLocaleTime(item.created_at)) + '</div></div>'
      + '<div><label>' + escapeHtml(lp('accessLogColEmail')) + '</label><div class="mono">' + escapeHtml(item.email || '-') + '</div></div>'
      + '<div><label>' + escapeHtml(lp('accessLogColTokens')) + '</label><div class="mono">' + escapeHtml(tokens) + '</div></div>'
      + '<div><label>' + escapeHtml(lp('accessLogColStatus')) + '</label><div class="mono">' + escapeHtml(status) + '</div></div>'
      + '<div><label>' + escapeHtml(lp('accessLogColProvider')) + '</label><div class="mono">' + escapeHtml(item.provider_id || '-') + '</div></div>'
      + '<div style="grid-column:1 / -1"><label>' + escapeHtml(lp('accessLogColModel')) + '</label><div class="mono">' + escapeHtml(model) + '</div></div>'
      + '<div style="grid-column:1 / -1"><label>' + escapeHtml(lp('accessLogColRequest')) + '</label><pre class="console" style="min-height:120px;max-height:260px;margin:0">' + escapeHtml(item.request_body || '-') + '</pre></div>'
      + '</div>'
      + '</div>';
  }).join('');
}

async function loadLLMEndpointAccessLogs(limit) {
  try {
    llmProviderAccessLogsCache = await api('/api/admin/llm/access-logs?limit=' + encodeURIComponent(String(limit || 50)));
    renderLLMEndpointAccessSummary();
    renderLLMEndpointAccessLogDialog();
  } catch (err) {
    const msg = lp('accessLogLoadFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
  }
}

async function reloadLLMEndpointAccessLogs() {
  await loadLLMEndpointAccessLogs(50);
}

async function showLLMEndpointAccessLogs() {
  var overlay = ensureLLMEndpointAccessLogDialog();
  if (!llmProviderAccessLogsCache) await loadLLMEndpointAccessLogs(50);
  renderLLMEndpointAccessLogDialog();
  overlay.classList.add('show');
}

const baseApplyLLMProvidersI18nAccessLogs = typeof applyLLMProvidersI18n === 'function' ? applyLLMProvidersI18n : null;
if (baseApplyLLMProvidersI18nAccessLogs) {
  applyLLMProvidersI18n = function() {
    baseApplyLLMProvidersI18nAccessLogs();
    renderLLMEndpointAccessSummary();
    renderLLMEndpointAccessLogDialog();
  };
}

const baseLoadLLMProvidersAccessLogs = typeof loadLLMProviders === 'function' ? loadLLMProviders : null;
if (baseLoadLLMProvidersAccessLogs) {
  loadLLMProviders = async function() {
    await baseLoadLLMProvidersAccessLogs();
    await loadLLMEndpointAccessLogs(20);
  };
}

const baseRenderLLMProvidersAccessLogs = typeof renderLLMProviders === 'function' ? renderLLMProviders : null;
if (baseRenderLLMProvidersAccessLogs) {
  renderLLMProviders = function() {
    baseRenderLLMProvidersAccessLogs();
    renderLLMEndpointAccessSummary();
  };
}

window.showLLMEndpointAccessLogs = showLLMEndpointAccessLogs;
window.reloadLLMEndpointAccessLogs = reloadLLMEndpointAccessLogs;
renderLLMEndpointAccessSummary();
Object.assign(LLM_PROVIDER_I18N.en, {
  userRateLimitPerMinute: 'User Rate Limit / Min',
  userRateLimitBurst: 'User Burst',
  userRateLimitHint: 'Applies immediately after save. Each signed-in user is throttled independently.',
  circuitBreakerThreshold: 'Circuit Breaker Failures',
  circuitBreakerCooldownMs: 'Circuit Cooldown (ms)',
  failureBackoffBaseMs: 'Backoff Base (ms)',
  failureBackoffMaxMs: 'Backoff Max (ms)',
  resilienceHint: 'Provider failures trigger backoff and open the circuit when repeated.',
  resilienceState: 'Resilience',
  circuitOpenBadge: 'Circuit Open',
  backoffBadge: 'Backing Off',
  failuresBadge: 'Failures'
});
Object.assign(LLM_PROVIDER_I18N.zh, {
  userRateLimitPerMinute: '\u7528\u6237\u6bcf\u5206\u949f\u9650\u6d41',
  userRateLimitBurst: '\u7528\u6237\u77ac\u65f6\u7a81\u53d1',
  userRateLimitHint: '\u4fdd\u5b58\u540e\u7acb\u5373\u751f\u6548\uff0c\u6309\u5df2\u767b\u5f55\u7528\u6237\u5355\u72ec\u9650\u6d41\u3002',
  circuitBreakerThreshold: '\u7194\u65ad\u89e6\u53d1\u6b21\u6570',
  circuitBreakerCooldownMs: '\u7194\u65ad\u51b7\u5374\u65f6\u95f4(ms)',
  failureBackoffBaseMs: '\u9000\u907f\u8d77\u59cb(ms)',
  failureBackoffMaxMs: '\u9000\u907f\u4e0a\u9650(ms)',
  resilienceHint: '\u4e0a\u6e38\u8fde\u7eed\u5931\u8d25\u65f6\u4f1a\u5148\u9000\u907f\uff0c\u518d\u89e6\u53d1\u7194\u65ad\u3002',
  resilienceState: '\u97e7\u6027\u72b6\u6001',
  circuitOpenBadge: '\u7194\u65ad\u5df2\u6253\u5f00',
  backoffBadge: '\u9000\u907f\u4e2d',
  failuresBadge: '\u8fde\u7eed\u5931\u8d25'
});
function llmProviderNormalizePositiveInt(value, fallback) {
  value = Number(value || 0);
  return Number.isFinite(value) && value > 0 ? Math.floor(value) : fallback;
}
function llmProviderNormalizeResilience(provider) {
  provider = provider || {};
  provider.circuit_breaker_threshold = llmProviderNormalizePositiveInt(provider.circuit_breaker_threshold, 3);
  provider.circuit_breaker_cooldown_ms = llmProviderNormalizePositiveInt(provider.circuit_breaker_cooldown_ms, 30000);
  provider.failure_backoff_base_ms = llmProviderNormalizePositiveInt(provider.failure_backoff_base_ms, 500);
  provider.failure_backoff_max_ms = llmProviderNormalizePositiveInt(provider.failure_backoff_max_ms, 10000);
  if (provider.failure_backoff_max_ms < provider.failure_backoff_base_ms) provider.failure_backoff_max_ms = provider.failure_backoff_base_ms;
  provider.consecutive_failures = Number(provider.consecutive_failures || 0);
  provider.circuit_open = !!provider.circuit_open;
  provider.circuit_open_until = provider.circuit_open_until || '';
  provider.backoff_until = provider.backoff_until || '';
  return provider;
}
function llmProviderNormalizeUserRateLimit(value, fallback) {
  return llmProviderNormalizePositiveInt(value, fallback);
}
const baseLpCloneResilience = typeof lpClone === 'function' ? lpClone : null;
if (baseLpCloneResilience) {
  lpClone = function(provider) {
    var next = baseLpCloneResilience(provider);
    next.circuit_breaker_threshold = Number(provider && provider.circuit_breaker_threshold || 0);
    next.circuit_breaker_cooldown_ms = Number(provider && provider.circuit_breaker_cooldown_ms || 0);
    next.failure_backoff_base_ms = Number(provider && provider.failure_backoff_base_ms || 0);
    next.failure_backoff_max_ms = Number(provider && provider.failure_backoff_max_ms || 0);
    next.consecutive_failures = Number(provider && provider.consecutive_failures || 0);
    next.circuit_open = !!(provider && provider.circuit_open);
    next.circuit_open_until = provider && provider.circuit_open_until || '';
    next.backoff_until = provider && provider.backoff_until || '';
    return llmProviderNormalizeResilience(next);
  };
}
function llmProviderReadResilienceForm() {
  return llmProviderNormalizeResilience({
    circuit_breaker_threshold: document.getElementById('llmProviderCircuitBreakerThreshold') && document.getElementById('llmProviderCircuitBreakerThreshold').value || 3,
    circuit_breaker_cooldown_ms: document.getElementById('llmProviderCircuitBreakerCooldownMs') && document.getElementById('llmProviderCircuitBreakerCooldownMs').value || 30000,
    failure_backoff_base_ms: document.getElementById('llmProviderFailureBackoffBaseMs') && document.getElementById('llmProviderFailureBackoffBaseMs').value || 500,
    failure_backoff_max_ms: document.getElementById('llmProviderFailureBackoffMaxMs') && document.getElementById('llmProviderFailureBackoffMaxMs').value || 10000
  });
}
function llmProviderWriteResilienceForm(provider) {
  provider = llmProviderNormalizeResilience(provider || {});
  _s('llmProviderCircuitBreakerThreshold', 'value', String(provider.circuit_breaker_threshold));
  _s('llmProviderCircuitBreakerCooldownMs', 'value', String(provider.circuit_breaker_cooldown_ms));
  _s('llmProviderFailureBackoffBaseMs', 'value', String(provider.failure_backoff_base_ms));
  _s('llmProviderFailureBackoffMaxMs', 'value', String(provider.failure_backoff_max_ms));
}
const baseReadSelectedLLMProviderFormResilience = typeof readSelectedLLMProviderForm === 'function' ? readSelectedLLMProviderForm : null;
if (baseReadSelectedLLMProviderFormResilience) {
  readSelectedLLMProviderForm = function() {
    var next = baseReadSelectedLLMProviderFormResilience();
    var resilience = llmProviderReadResilienceForm();
    next.circuit_breaker_threshold = resilience.circuit_breaker_threshold;
    next.circuit_breaker_cooldown_ms = resilience.circuit_breaker_cooldown_ms;
    next.failure_backoff_base_ms = resilience.failure_backoff_base_ms;
    next.failure_backoff_max_ms = resilience.failure_backoff_max_ms;
    return next;
  };
}
const baseClearLLMProviderFormResilience = typeof clearLLMProviderForm === 'function' ? clearLLMProviderForm : null;
if (baseClearLLMProviderFormResilience) {
  clearLLMProviderForm = function() {
    baseClearLLMProviderFormResilience();
    llmProviderWriteResilienceForm({});
  };
}
const baseOpenLLMProviderDialogResilience = typeof openLLMProviderDialog === 'function' ? openLLMProviderDialog : null;
if (baseOpenLLMProviderDialogResilience) {
  openLLMProviderDialog = function(mode) {
    baseOpenLLMProviderDialogResilience(mode);
    var provider = mode === 'edit' ? lpById(llmProviderSelectedId) : null;
    llmProviderWriteResilienceForm(provider || {});
  };
}
const baseBuildLLMProviderPayloadResilience = typeof buildLLMProviderPayload === 'function' ? buildLLMProviderPayload : null;
if (baseBuildLLMProviderPayloadResilience) {
  buildLLMProviderPayload = function() {
    var payload = baseBuildLLMProviderPayloadResilience();
    payload.user_rate_limit_per_minute = llmProviderNormalizeUserRateLimit(document.getElementById('llmProvidersUserRateLimitPerMinute') && document.getElementById('llmProvidersUserRateLimitPerMinute').value, 120);
    payload.user_rate_limit_burst = llmProviderNormalizeUserRateLimit(document.getElementById('llmProvidersUserRateLimitBurst') && document.getElementById('llmProvidersUserRateLimitBurst').value, 20);
    payload.providers = (payload.providers || []).map(function(p, idx) {
      var cached = llmProviderRegistryCache && llmProviderRegistryCache.providers && llmProviderRegistryCache.providers[idx] || {};
      var normalized = llmProviderNormalizeResilience(cached);
      p.circuit_breaker_threshold = normalized.circuit_breaker_threshold;
      p.circuit_breaker_cooldown_ms = normalized.circuit_breaker_cooldown_ms;
      p.failure_backoff_base_ms = normalized.failure_backoff_base_ms;
      p.failure_backoff_max_ms = normalized.failure_backoff_max_ms;
      return p;
    });
    if (llmProviderRegistryCache) {
      llmProviderRegistryCache.user_rate_limit_per_minute = payload.user_rate_limit_per_minute;
      llmProviderRegistryCache.user_rate_limit_burst = payload.user_rate_limit_burst;
    }
    return payload;
  };
}
const baseLoadLLMProvidersResilience = typeof loadLLMProviders === 'function' ? loadLLMProviders : null;
if (baseLoadLLMProvidersResilience) {
  loadLLMProviders = async function() {
    await baseLoadLLMProvidersResilience();
    if (!llmProviderRegistryCache) return;
    llmProviderRegistryCache.user_rate_limit_per_minute = llmProviderNormalizeUserRateLimit(llmProviderRegistryCache.user_rate_limit_per_minute, 120);
    llmProviderRegistryCache.user_rate_limit_burst = llmProviderNormalizeUserRateLimit(llmProviderRegistryCache.user_rate_limit_burst, 20);
    llmProviderRegistryCache.providers = (llmProviderRegistryCache.providers || []).map(function(p) { return llmProviderNormalizeResilience(p); });
    llmProviderSyncGlobalControls();
  };
}
function llmProviderResilienceSummary(provider) {
  provider = llmProviderNormalizeResilience(provider || {});
  var parts = [lp('failuresBadge') + ': ' + String(provider.consecutive_failures || 0)];
  if (provider.circuit_open) parts.push(lp('circuitOpenBadge'));
  else if (provider.backoff_until) parts.push(lp('backoffBadge'));
  return parts.join(' | ');
}
const baseRenderLLMProvidersResilience = typeof renderLLMProviders === 'function' ? renderLLMProviders : null;
if (baseRenderLLMProvidersResilience) {
  renderLLMProviders = function() {
    baseRenderLLMProvidersResilience();
    if (llmProviderRegistryCache) {
      llmProviderSyncGlobalControls();
    }
    var selected = lpById(llmProviderSelectedId);
    if (llmProviderDialogOpen()) llmProviderWriteResilienceForm(selected || {});
    var cards = document.querySelectorAll('.item[data-provider-id]');
    cards.forEach(function(card) {
      var id = card.getAttribute('data-provider-id');
      var provider = lpById(id);
      if (!provider) return;
      var marker = card.querySelector('.llm-provider-resilience');
      if (!marker) {
        marker = document.createElement('span');
        marker.className = 'llm-provider-resilience';
      }
      marker.style.display = 'inline-flex';
      marker.style.alignItems = 'center';
      marker.style.whiteSpace = 'nowrap';
      var metaRow = card.querySelector('.llm-provider-meta-row');
      if (metaRow && marker.parentElement !== metaRow) metaRow.appendChild(marker);
      else if (!marker.parentElement) card.appendChild(marker);
      var resilienceText = (currentLang === 'zh' ? '\u5931\u8d25: ' : 'Fail: ') + String(provider.consecutive_failures || 0); if (provider.circuit_open) resilienceText += currentLang === 'zh' ? ' | \u7194\u65ad' : ' | Open'; else if (provider.backoff_until) resilienceText += currentLang === 'zh' ? ' | \u9000\u907f' : ' | Backoff'; marker.textContent = resilienceText;
    });
  };
}
const baseApplyLLMProvidersI18nResilience = typeof applyLLMProvidersI18n === 'function' ? applyLLMProvidersI18n : null;
if (baseApplyLLMProvidersI18nResilience) {
  applyLLMProvidersI18n = function() {
    baseApplyLLMProvidersI18nResilience();
    _s('llmProvidersUserRateLimitPerMinuteLabel', 'textContent', lp('userRateLimitPerMinute'));
    _s('llmProvidersUserRateLimitBurstLabel', 'textContent', lp('userRateLimitBurst'));
    _s('llmProvidersUserRateLimitHint', 'textContent', lp('userRateLimitHint'));
    _s('llmProviderCircuitBreakerThresholdLabel', 'textContent', lp('circuitBreakerThreshold'));
    _s('llmProviderCircuitBreakerCooldownMsLabel', 'textContent', lp('circuitBreakerCooldownMs'));
    _s('llmProviderFailureBackoffBaseMsLabel', 'textContent', lp('failureBackoffBaseMs'));
    _s('llmProviderFailureBackoffMaxMsLabel', 'textContent', lp('failureBackoffMaxMs'));
    _s('llmProviderResilienceHint', 'textContent', lp('resilienceHint'));
  };
  applyLLMProvidersI18n();
}


Object.assign(LLM_PROVIDER_I18N.en, {
  accessLogFilterProvider: 'Upstream / Provider',
  accessLogFilterClientIP: 'Downstream IP',
  accessLogFilterEmail: 'Client Email',
  accessLogFilterKeyword: 'Keyword',
  accessLogFilterApply: 'Filter',
  accessLogFilterReset: 'Reset',
  accessLogFilterProviderPlaceholder: 'provider id',
  accessLogFilterClientIPPlaceholder: 'client ip',
  accessLogFilterEmailPlaceholder: 'user@example.com',
  accessLogFilterKeywordPlaceholder: 'model or error code',
  accessLogRequestHidden: 'Hidden by default. Click to view raw request.',
  accessLogShowRequest: 'Show Request',
  accessLogHideRequest: 'Hide Request',
  accessLogFilteredEmpty: 'No access logs match the current filter.'
});
Object.assign(LLM_PROVIDER_I18N.zh, {
  accessLogFilterProvider: '\u4e0a\u6e38 / Provider',
  accessLogFilterClientIP: '\u4e0b\u6e38IP',
  accessLogFilterEmail: '\u5ba2\u6237\u7aef\u90ae\u7bb1',
  accessLogFilterKeyword: '\u5173\u952e\u5b57',
  accessLogFilterApply: '\u67e5\u8be2',
  accessLogFilterReset: '\u91cd\u7f6e',
  accessLogFilterProviderPlaceholder: 'provider id',
  accessLogFilterClientIPPlaceholder: 'client ip',
  accessLogFilterEmailPlaceholder: 'user@example.com',
  accessLogFilterKeywordPlaceholder: '\u6a21\u578b\u6216\u9519\u8bef\u7801',
  accessLogRequestHidden: '\u9ed8\u8ba4\u4e0d\u5c55\u5f00\u5185\u5bb9\uff0c\u70b9\u51fb\u540e\u67e5\u770b\u539f\u59cb\u8bf7\u6c42\u3002',
  accessLogShowRequest: '\u67e5\u770b\u8bf7\u6c42',
  accessLogHideRequest: '\u6536\u8d77\u8bf7\u6c42',
  accessLogFilteredEmpty: '\u5f53\u524d\u8fc7\u6ee4\u6761\u4ef6\u4e0b\u6ca1\u6709\u8bbf\u95ee\u65e5\u5fd7\u3002'
});
let llmProviderAccessLogFilterState = { provider: '', client_ip: '', email: '', q: '' };
function llmProviderAccessLogQuery(limit) {
  var params = new URLSearchParams();
  params.set('limit', String(limit || 50));
  Object.keys(llmProviderAccessLogFilterState || {}).forEach(function(key) {
    var value = String(llmProviderAccessLogFilterState[key] || '').trim();
    if (value) params.set(key, value);
  });
  return '/api/admin/llm/access-logs?' + params.toString();
}
function llmProviderReadAccessLogFilters() {
  llmProviderAccessLogFilterState = {
    provider: document.getElementById('llmEndpointAccessLogFilterProvider') && document.getElementById('llmEndpointAccessLogFilterProvider').value || '',
    client_ip: document.getElementById('llmEndpointAccessLogFilterClientIP') && document.getElementById('llmEndpointAccessLogFilterClientIP').value || '',
    email: document.getElementById('llmEndpointAccessLogFilterEmail') && document.getElementById('llmEndpointAccessLogFilterEmail').value || '',
    q: document.getElementById('llmEndpointAccessLogFilterKeyword') && document.getElementById('llmEndpointAccessLogFilterKeyword').value || ''
  };
}
function llmProviderWriteAccessLogFilters() {
  _s('llmEndpointAccessLogFilterProvider', 'value', llmProviderAccessLogFilterState.provider || '');
  _s('llmEndpointAccessLogFilterClientIP', 'value', llmProviderAccessLogFilterState.client_ip || '');
  _s('llmEndpointAccessLogFilterEmail', 'value', llmProviderAccessLogFilterState.email || '');
  _s('llmEndpointAccessLogFilterKeyword', 'value', llmProviderAccessLogFilterState.q || '');
}
function llmProviderToggleAccessLogRequest(id) {
  var body = document.getElementById('llmEndpointAccessLogRequest_' + id);
  var btn = document.getElementById('llmEndpointAccessLogToggle_' + id);
  if (!body || !btn) return;
  var opening = body.classList.contains('hidden');
  body.classList.toggle('hidden', !opening);
  btn.textContent = opening ? lp('accessLogHideRequest') : lp('accessLogShowRequest');
}
ensureLLMEndpointAccessLogDialog = function() {
  var overlay = document.getElementById('llmEndpointAccessLogOverlay');
  if (overlay) return overlay;
  overlay = document.createElement('div');
  overlay.id = 'llmEndpointAccessLogOverlay';
  overlay.className = 'session-modal-overlay';
  if (window.AdminUI && typeof AdminUI.bindModalOverlayDismiss === 'function') AdminUI.bindModalOverlayDismiss(overlay, function() { overlay.classList.remove('show'); });
  overlay.innerHTML = '' +
    '<div class="session-modal" style="width:min(1120px,calc(100% - 48px))">' +
    '<button class="close-btn" type="button" onclick="document.getElementById(\'llmEndpointAccessLogOverlay\').classList.remove(\'show\')">&times;</button>' +
    '<div class="head" style="margin-bottom:10px"><div><h3 id="llmEndpointAccessLogDialogTitle"></h3><div class="desc" id="llmEndpointAccessLogDialogDesc"></div></div><div class="actions"><button class="btn-ghost" type="button" id="llmEndpointAccessLogReloadBtn" onclick="reloadLLMEndpointAccessLogs()"></button></div></div>' +
    '<div class="item" style="padding:12px 14px;margin-bottom:10px"><div class="grid4" style="gap:8px"><div><label id="llmEndpointAccessLogFilterProviderLabel"></label><input id="llmEndpointAccessLogFilterProvider" placeholder=""></div><div><label id="llmEndpointAccessLogFilterClientIPLabel"></label><input id="llmEndpointAccessLogFilterClientIP" placeholder=""></div><div><label id="llmEndpointAccessLogFilterEmailLabel"></label><input id="llmEndpointAccessLogFilterEmail" placeholder=""></div><div><label id="llmEndpointAccessLogFilterKeywordLabel"></label><input id="llmEndpointAccessLogFilterKeyword" placeholder=""></div></div><div class="actions" style="margin-top:10px"><button class="btn-primary" type="button" id="llmEndpointAccessLogFilterApplyBtn" onclick="reloadLLMEndpointAccessLogs()"></button><button class="btn-ghost" type="button" id="llmEndpointAccessLogFilterResetBtn" onclick="resetLLMEndpointAccessLogsFilter()"></button></div></div>' +
    '<div id="llmEndpointAccessLogDialogBody"></div>' +
    '</div>';
  document.body.appendChild(overlay);
  return overlay;
};
renderLLMEndpointAccessLogDialog = function() {
  ensureLLMEndpointAccessLogDialog();
  llmProviderWriteAccessLogFilters();
  _s('llmEndpointAccessLogDialogTitle', 'textContent', lp('accessLogDialogTitle'));
  _s('llmEndpointAccessLogDialogDesc', 'textContent', lp('accessLogDesc'));
  _s('llmEndpointAccessLogReloadBtn', 'textContent', lp('accessLogReload'));
  _s('llmEndpointAccessLogFilterProviderLabel', 'textContent', lp('accessLogFilterProvider'));
  _s('llmEndpointAccessLogFilterClientIPLabel', 'textContent', lp('accessLogFilterClientIP'));
  _s('llmEndpointAccessLogFilterEmailLabel', 'textContent', lp('accessLogFilterEmail'));
  _s('llmEndpointAccessLogFilterKeywordLabel', 'textContent', lp('accessLogFilterKeyword'));
  _s('llmEndpointAccessLogFilterApplyBtn', 'textContent', lp('accessLogFilterApply'));
  _s('llmEndpointAccessLogFilterResetBtn', 'textContent', lp('accessLogFilterReset'));
  _s('llmEndpointAccessLogFilterProvider', 'placeholder', lp('accessLogFilterProviderPlaceholder'));
  _s('llmEndpointAccessLogFilterClientIP', 'placeholder', lp('accessLogFilterClientIPPlaceholder'));
  _s('llmEndpointAccessLogFilterEmail', 'placeholder', lp('accessLogFilterEmailPlaceholder'));
  _s('llmEndpointAccessLogFilterKeyword', 'placeholder', lp('accessLogFilterKeywordPlaceholder'));
  var root = document.getElementById('llmEndpointAccessLogDialogBody');
  if (!root) return;
  var logs = llmProviderAccessLogsCache && llmProviderAccessLogsCache.logs || [];
  if (!logs.length) {
    root.innerHTML = '<div class="hint">' + escapeHtml((llmProviderAccessLogsCache && llmProviderAccessLogsCache.total === 0 && (llmProviderAccessLogFilterState.provider || llmProviderAccessLogFilterState.client_ip || llmProviderAccessLogFilterState.email || llmProviderAccessLogFilterState.q)) ? lp('accessLogFilteredEmpty') : lp('accessLogEmpty')) + '</div>';
    return;
  }
  root.innerHTML = logs.map(function(item, idx) {
    var tokens = [Number(item.input_tokens || 0), Number(item.output_tokens || 0), Number(item.total_tokens || 0)].join(' / ') + ' | \u00a5' + llmProviderFormatMoney(item.total_cost_rmb);
    var model = [item.requested_model || '-', item.authorized_model || '-'].join(' -> ');
    var status = String(item.status_code || 0) + (item.error_code ? ' / ' + item.error_code : '');
    var reqId = String(item.id || idx).replace(/[^a-zA-Z0-9_-]+/g, '_');
    return '<div class="item" style="padding:12px 14px;margin-bottom:10px">'
      + '<div class="grid3" style="gap:8px">'
      + '<div><label>' + escapeHtml(lp('accessLogColProvider')) + '</label><div class="mono">' + escapeHtml(item.provider_id || '-') + '</div></div>'
      + '<div><label>' + escapeHtml(lp('accessLogColIP')) + '</label><div class="mono">' + escapeHtml(item.client_ip || '-') + '</div></div>'
      + '<div><label>' + escapeHtml(lp('accessLogColEmail')) + '</label><div class="mono">' + escapeHtml(item.email || '-') + '</div></div>'
      + '<div><label>' + escapeHtml(lp('accessLogColTime')) + '</label><div class="mono">' + escapeHtml(llmProviderAccessLocaleTime(item.created_at)) + '</div></div>'
      + '<div><label>' + escapeHtml(lp('accessLogColTokens')) + '</label><div class="mono">' + escapeHtml(tokens) + '</div></div>'
      + '<div><label>' + escapeHtml(lp('accessLogColStatus')) + '</label><div class="mono">' + escapeHtml(status) + '</div></div>'
      + '<div style="grid-column:1 / -1"><label>' + escapeHtml(lp('accessLogColModel')) + '</label><div class="mono">' + escapeHtml(model) + '</div></div>'
      + '<div style="grid-column:1 / -1"><label>' + escapeHtml(lp('accessLogColRequest')) + '</label><div class="hint" style="margin-bottom:8px">' + escapeHtml(lp('accessLogRequestHidden')) + '</div><div class="actions" style="margin:0 0 8px"><button class="btn-ghost" type="button" id="llmEndpointAccessLogToggle_' + reqId + '" onclick="llmProviderToggleAccessLogRequest(\'' + reqId + '\')">' + escapeHtml(lp('accessLogShowRequest')) + '</button></div><pre id="llmEndpointAccessLogRequest_' + reqId + '" class="console hidden" style="min-height:120px;max-height:260px;margin:0">' + escapeHtml(item.request_body || '-') + '</pre></div>'
      + '</div>'
      + '</div>';
  }).join('');
};
loadLLMEndpointAccessLogs = async function(limit) {
  try {
    llmProviderAccessLogsCache = await api(llmProviderAccessLogQuery(limit || 50));
    renderLLMEndpointAccessSummary();
    renderLLMEndpointAccessLogDialog();
  } catch (err) {
    const msg = lp('accessLogLoadFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
  }
};
reloadLLMEndpointAccessLogs = async function() {
  llmProviderReadAccessLogFilters();
  await loadLLMEndpointAccessLogs(50);
};
window.resetLLMEndpointAccessLogsFilter = async function() {
  llmProviderAccessLogFilterState = { provider: '', client_ip: '', email: '', q: '' };
  llmProviderWriteAccessLogFilters();
  await loadLLMEndpointAccessLogs(50);
};
showLLMEndpointAccessLogs = async function() {
  var overlay = ensureLLMEndpointAccessLogDialog();
  if (!llmProviderAccessLogsCache) await loadLLMEndpointAccessLogs(50);
  renderLLMEndpointAccessLogDialog();
  overlay.classList.add('show');
};
window.showLLMEndpointAccessLogs = showLLMEndpointAccessLogs;
window.reloadLLMEndpointAccessLogs = reloadLLMEndpointAccessLogs;
window.llmProviderToggleAccessLogRequest = llmProviderToggleAccessLogRequest;

Object.assign(LLM_PROVIDER_I18N.en, {
  accessLogFilterUpstreamHost: 'Upstream Host',
  accessLogFilterUpstreamHostPlaceholder: 'api.provider.example',
  accessLogColUpstreamHost: 'Upstream Host'
});
Object.assign(LLM_PROVIDER_I18N.zh, {
  accessLogFilterUpstreamHost: '\u4e0a\u6e38Host',
  accessLogFilterUpstreamHostPlaceholder: 'api.provider.example',
  accessLogColUpstreamHost: '\u4e0a\u6e38Host'
});
llmProviderAccessLogFilterState = Object.assign({ upstream_host: '' }, llmProviderAccessLogFilterState || {});
const baseLlmProviderReadAccessLogFiltersUpstream = llmProviderReadAccessLogFilters;
llmProviderReadAccessLogFilters = function() {
  baseLlmProviderReadAccessLogFiltersUpstream();
  llmProviderAccessLogFilterState.upstream_host = document.getElementById('llmEndpointAccessLogFilterUpstreamHost') && document.getElementById('llmEndpointAccessLogFilterUpstreamHost').value || '';
};
const baseLlmProviderWriteAccessLogFiltersUpstream = llmProviderWriteAccessLogFilters;
llmProviderWriteAccessLogFilters = function() {
  baseLlmProviderWriteAccessLogFiltersUpstream();
  _s('llmEndpointAccessLogFilterUpstreamHost', 'value', llmProviderAccessLogFilterState.upstream_host || '');
};
ensureLLMEndpointAccessLogDialog = function() {
  var overlay = document.getElementById('llmEndpointAccessLogOverlay');
  if (overlay) return overlay;
  overlay = document.createElement('div');
  overlay.id = 'llmEndpointAccessLogOverlay';
  overlay.className = 'session-modal-overlay';
  if (window.AdminUI && typeof AdminUI.bindModalOverlayDismiss === 'function') AdminUI.bindModalOverlayDismiss(overlay, function() { overlay.classList.remove('show'); });
  overlay.innerHTML = '' +
    '<div class="session-modal" style="width:min(1180px,calc(100% - 48px))">' +
    '<button class="close-btn" type="button" onclick="document.getElementById(\'llmEndpointAccessLogOverlay\').classList.remove(\'show\')">&times;</button>' +
    '<div class="head" style="margin-bottom:10px"><div><h3 id="llmEndpointAccessLogDialogTitle"></h3><div class="desc" id="llmEndpointAccessLogDialogDesc"></div></div><div class="actions"><button class="btn-ghost" type="button" id="llmEndpointAccessLogReloadBtn" onclick="reloadLLMEndpointAccessLogs()"></button></div></div>' +
    '<div class="item" style="padding:12px 14px;margin-bottom:10px"><div class="grid4" style="gap:8px"><div><label id="llmEndpointAccessLogFilterProviderLabel"></label><input id="llmEndpointAccessLogFilterProvider" placeholder=""></div><div><label id="llmEndpointAccessLogFilterUpstreamHostLabel"></label><input id="llmEndpointAccessLogFilterUpstreamHost" placeholder=""></div><div><label id="llmEndpointAccessLogFilterClientIPLabel"></label><input id="llmEndpointAccessLogFilterClientIP" placeholder=""></div><div><label id="llmEndpointAccessLogFilterEmailLabel"></label><input id="llmEndpointAccessLogFilterEmail" placeholder=""></div></div><div class="grid4" style="gap:8px;margin-top:8px"><div><label id="llmEndpointAccessLogFilterKeywordLabel"></label><input id="llmEndpointAccessLogFilterKeyword" placeholder=""></div></div><div class="actions" style="margin-top:10px"><button class="btn-primary" type="button" id="llmEndpointAccessLogFilterApplyBtn" onclick="reloadLLMEndpointAccessLogs()"></button><button class="btn-ghost" type="button" id="llmEndpointAccessLogFilterResetBtn" onclick="resetLLMEndpointAccessLogsFilter()"></button></div></div>' +
    '<div id="llmEndpointAccessLogDialogBody"></div>' +
    '</div>';
  document.body.appendChild(overlay);
  return overlay;
};
const baseRenderLLMEndpointAccessLogDialogUpstream = renderLLMEndpointAccessLogDialog;
renderLLMEndpointAccessLogDialog = function() {
  baseRenderLLMEndpointAccessLogDialogUpstream();
  _s('llmEndpointAccessLogFilterUpstreamHostLabel', 'textContent', lp('accessLogFilterUpstreamHost'));
  _s('llmEndpointAccessLogFilterUpstreamHost', 'placeholder', lp('accessLogFilterUpstreamHostPlaceholder'));
  var root = document.getElementById('llmEndpointAccessLogDialogBody');
  if (!root || !llmProviderAccessLogsCache || !llmProviderAccessLogsCache.logs || !llmProviderAccessLogsCache.logs.length) return;
  Array.prototype.forEach.call(root.querySelectorAll('.item'), function(card, idx) {
    var item = llmProviderAccessLogsCache.logs[idx];
    if (!item) return;
    var meta = item.metadata || {};
    var upstreamHost = meta.upstream_host || '-';
    var grid = card.querySelector('.grid3');
    if (!grid) return;
    var existing = card.querySelector('.llm-accesslog-upstream-host');
    if (existing) existing.remove();
    var block = document.createElement('div');
    block.className = 'llm-accesslog-upstream-host';
    block.innerHTML = '<label>' + escapeHtml(lp('accessLogColUpstreamHost')) + '</label><div class="mono">' + escapeHtml(upstreamHost) + '</div>';
    grid.insertBefore(block, grid.children[1] || null);
  });
};
window.resetLLMEndpointAccessLogsFilter = async function() {
  llmProviderAccessLogFilterState = { provider: '', upstream_host: '', client_ip: '', email: '', q: '' };
  llmProviderWriteAccessLogFilters();
  await loadLLMEndpointAccessLogs(50);
};

Object.assign(LLM_PROVIDER_I18N.en, {
  accessLogFilterStartAt: 'Start Time',
  accessLogFilterEndAt: 'End Time',
  accessLogPagePrev: 'Prev',
  accessLogPageNext: 'Next',
  accessLogPageSummary: 'Showing {from}-{to} / {total}'
});
Object.assign(LLM_PROVIDER_I18N.zh, {
  accessLogFilterStartAt: '\u5f00\u59cb\u65f6\u95f4',
  accessLogFilterEndAt: '\u7ed3\u675f\u65f6\u95f4',
  accessLogPagePrev: '\u4e0a\u4e00\u9875',
  accessLogPageNext: '\u4e0b\u4e00\u9875',
  accessLogPageSummary: '\u5f53\u524d\u663e\u793a {from}-{to} / {total}'
});
let llmProviderAccessLogPageState = { page: 1, page_size: 50 };
llmProviderAccessLogFilterState = Object.assign({ start_at: '', end_at: '' }, llmProviderAccessLogFilterState || {});
llmProviderAccessLogQuery = function(limit) {
  var pageSize = Number(limit || llmProviderAccessLogPageState.page_size || 50);
  llmProviderAccessLogPageState.page_size = pageSize;
  var params = new URLSearchParams();
  params.set('limit', String(pageSize));
  params.set('offset', String(Math.max(0, ((Number(llmProviderAccessLogPageState.page || 1) - 1) * pageSize))));
  Object.keys(llmProviderAccessLogFilterState || {}).forEach(function(key) {
    var value = String(llmProviderAccessLogFilterState[key] || '').trim();
    if (value) params.set(key, value);
  });
  return '/api/admin/llm/access-logs?' + params.toString();
};
const baseLlmProviderReadAccessLogFiltersPaged = llmProviderReadAccessLogFilters;
llmProviderReadAccessLogFilters = function() {
  baseLlmProviderReadAccessLogFiltersPaged();
  llmProviderAccessLogFilterState.start_at = document.getElementById('llmEndpointAccessLogFilterStartAt') && document.getElementById('llmEndpointAccessLogFilterStartAt').value || '';
  llmProviderAccessLogFilterState.end_at = document.getElementById('llmEndpointAccessLogFilterEndAt') && document.getElementById('llmEndpointAccessLogFilterEndAt').value || '';
};
const baseLlmProviderWriteAccessLogFiltersPaged = llmProviderWriteAccessLogFilters;
llmProviderWriteAccessLogFilters = function() {
  baseLlmProviderWriteAccessLogFiltersPaged();
  _s('llmEndpointAccessLogFilterStartAt', 'value', llmProviderAccessLogFilterState.start_at || '');
  _s('llmEndpointAccessLogFilterEndAt', 'value', llmProviderAccessLogFilterState.end_at || '');
};
ensureLLMEndpointAccessLogDialog = function() {
  var overlay = document.getElementById('llmEndpointAccessLogOverlay');
  if (overlay) return overlay;
  overlay = document.createElement('div');
  overlay.id = 'llmEndpointAccessLogOverlay';
  overlay.className = 'session-modal-overlay';
  if (window.AdminUI && typeof AdminUI.bindModalOverlayDismiss === 'function') AdminUI.bindModalOverlayDismiss(overlay, function() { overlay.classList.remove('show'); });
  overlay.innerHTML = '' +
    '<div class="session-modal" style="width:min(1180px,calc(100% - 48px))">' +
    '<button class="close-btn" type="button" onclick="document.getElementById(\'llmEndpointAccessLogOverlay\').classList.remove(\'show\')">&times;</button>' +
    '<div class="head" style="margin-bottom:10px"><div><h3 id="llmEndpointAccessLogDialogTitle"></h3><div class="desc" id="llmEndpointAccessLogDialogDesc"></div></div><div class="actions"><button class="btn-ghost" type="button" id="llmEndpointAccessLogReloadBtn" onclick="reloadLLMEndpointAccessLogs()"></button></div></div>' +
    '<div class="item" style="padding:12px 14px;margin-bottom:10px"><div class="grid4" style="gap:8px"><div><label id="llmEndpointAccessLogFilterProviderLabel"></label><input id="llmEndpointAccessLogFilterProvider" placeholder=""></div><div><label id="llmEndpointAccessLogFilterUpstreamHostLabel"></label><input id="llmEndpointAccessLogFilterUpstreamHost" placeholder=""></div><div><label id="llmEndpointAccessLogFilterClientIPLabel"></label><input id="llmEndpointAccessLogFilterClientIP" placeholder=""></div><div><label id="llmEndpointAccessLogFilterEmailLabel"></label><input id="llmEndpointAccessLogFilterEmail" placeholder=""></div></div><div class="grid4" style="gap:8px;margin-top:8px"><div><label id="llmEndpointAccessLogFilterKeywordLabel"></label><input id="llmEndpointAccessLogFilterKeyword" placeholder=""></div><div><label id="llmEndpointAccessLogFilterStartAtLabel"></label><input id="llmEndpointAccessLogFilterStartAt" type="datetime-local"></div><div><label id="llmEndpointAccessLogFilterEndAtLabel"></label><input id="llmEndpointAccessLogFilterEndAt" type="datetime-local"></div></div><div class="actions" style="margin-top:10px"><button class="btn-primary" type="button" id="llmEndpointAccessLogFilterApplyBtn" onclick="reloadLLMEndpointAccessLogs()"></button><button class="btn-ghost" type="button" id="llmEndpointAccessLogFilterResetBtn" onclick="resetLLMEndpointAccessLogsFilter()"></button></div></div>' +
    '<div id="llmEndpointAccessLogDialogBody"></div>' +
    '<div class="actions" style="margin-top:10px;justify-content:space-between"><div id="llmEndpointAccessLogPageSummary" class="hint"></div><div><button class="btn-ghost" type="button" id="llmEndpointAccessLogPrevBtn" onclick="changeLLMEndpointAccessLogsPage(-1)"></button><button class="btn-ghost" type="button" id="llmEndpointAccessLogNextBtn" onclick="changeLLMEndpointAccessLogsPage(1)"></button></div></div>' +
    '</div>';
  document.body.appendChild(overlay);
  return overlay;
};
const baseRenderLLMEndpointAccessLogDialogPaged = renderLLMEndpointAccessLogDialog;
renderLLMEndpointAccessLogDialog = function() {
  baseRenderLLMEndpointAccessLogDialogPaged();
  _s('llmEndpointAccessLogFilterStartAtLabel', 'textContent', lp('accessLogFilterStartAt'));
  _s('llmEndpointAccessLogFilterEndAtLabel', 'textContent', lp('accessLogFilterEndAt'));
  _s('llmEndpointAccessLogPrevBtn', 'textContent', lp('accessLogPagePrev'));
  _s('llmEndpointAccessLogNextBtn', 'textContent', lp('accessLogPageNext'));
  var total = Number(llmProviderAccessLogsCache && llmProviderAccessLogsCache.total || 0);
  var offset = Number(llmProviderAccessLogsCache && llmProviderAccessLogsCache.offset || 0);
  var logs = llmProviderAccessLogsCache && llmProviderAccessLogsCache.logs || [];
  var from = total ? offset + 1 : 0;
  var to = logs.length ? offset + logs.length : 0;
  _s('llmEndpointAccessLogPageSummary', 'textContent', lp('accessLogPageSummary', { from: from, to: to, total: total }));
  var prevBtn = document.getElementById('llmEndpointAccessLogPrevBtn');
  var nextBtn = document.getElementById('llmEndpointAccessLogNextBtn');
  if (prevBtn) prevBtn.disabled = Number(llmProviderAccessLogPageState.page || 1) <= 1;
  if (nextBtn) nextBtn.disabled = to >= total;
};
loadLLMEndpointAccessLogs = async function(limit) {
  try {
    llmProviderAccessLogsCache = await api(llmProviderAccessLogQuery(limit || llmProviderAccessLogPageState.page_size || 50));
    renderLLMEndpointAccessSummary();
    renderLLMEndpointAccessLogDialog();
  } catch (err) {
    const msg = lp('accessLogLoadFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
  }
};
reloadLLMEndpointAccessLogs = async function() {
  llmProviderAccessLogPageState.page = 1;
  llmProviderReadAccessLogFilters();
  await loadLLMEndpointAccessLogs(llmProviderAccessLogPageState.page_size || 50);
};
window.changeLLMEndpointAccessLogsPage = async function(step) {
  llmProviderAccessLogPageState.page = Math.max(1, Number(llmProviderAccessLogPageState.page || 1) + Number(step || 0));
  await loadLLMEndpointAccessLogs(llmProviderAccessLogPageState.page_size || 50);
};
window.resetLLMEndpointAccessLogsFilter = async function() {
  llmProviderAccessLogPageState.page = 1;
  llmProviderAccessLogFilterState = { provider: '', upstream_host: '', client_ip: '', email: '', q: '', start_at: '', end_at: '' };
  llmProviderWriteAccessLogFilters();
  await loadLLMEndpointAccessLogs(llmProviderAccessLogPageState.page_size || 50);
};

Object.assign(LLM_PROVIDER_I18N.en, {
  accessLogExport: 'Export CSV',
  accessLogTableTime: 'Time',
  accessLogTableProvider: 'Provider',
  accessLogTableUpstreamHost: 'Upstream Host',
  accessLogTableClientIP: 'Client IP',
  accessLogTableEmail: 'Email',
  accessLogTableModel: 'Model',
  accessLogTableTokens: 'Tokens',
  accessLogTableStatus: 'Status',
  accessLogTableRequest: 'Request',
  accessLogExportEmpty: 'No filtered access logs to export.'
});
Object.assign(LLM_PROVIDER_I18N.zh, {
  accessLogExport: '\u5bfc\u51fa CSV',
  accessLogTableTime: '\u65f6\u95f4',
  accessLogTableProvider: 'Provider',
  accessLogTableUpstreamHost: '\u4e0a\u6e38Host',
  accessLogTableClientIP: '\u5ba2\u6237\u7aefIP',
  accessLogTableEmail: '\u90ae\u7bb1',
  accessLogTableModel: '\u6a21\u578b',
  accessLogTableTokens: 'Token',
  accessLogTableStatus: '\u72b6\u6001',
  accessLogTableRequest: '\u8bf7\u6c42',
  accessLogExportEmpty: '\u5f53\u524d\u6ca1\u6709\u53ef\u5bfc\u51fa\u7684\u8fc7\u6ee4\u65e5\u5fd7\u3002'
});
function llmProviderAccessLogCSVValue(value) {
  value = String(value == null ? '' : value);
  if (/[,"\r\n]/.test(value)) return '"' + value.replace(/"/g, '""') + '"';
  return value;
}
function exportLLMEndpointAccessLogsCSV() {
  var logs = llmProviderAccessLogsCache && llmProviderAccessLogsCache.logs || [];
  if (!logs.length) {
    showToast(lp('accessLogExportEmpty'), 'info');
    return;
  }
  var rows = [['time','provider','upstream_host','client_ip','email','requested_model','authorized_model','input_tokens','output_tokens','total_tokens','input_cost_rmb','output_cost_rmb','total_cost_rmb','status','error_code','request_body']];
  logs.forEach(function(item) {
    var meta = item.metadata || {};
    rows.push([
      item.created_at || '',
      item.provider_id || '',
      meta.upstream_host || '',
      item.client_ip || '',
      item.email || '',
      item.requested_model || '',
      item.authorized_model || '',
      Number(item.input_tokens || 0),
      Number(item.output_tokens || 0),
      Number(item.total_tokens || 0),
      llmProviderFormatMoney(item.input_cost_rmb),
      llmProviderFormatMoney(item.output_cost_rmb),
      llmProviderFormatMoney(item.total_cost_rmb),
      Number(item.status_code || 0),
      item.error_code || '',
      item.request_body || ''
    ]);
  });
  var csv = rows.map(function(row) { return row.map(llmProviderAccessLogCSVValue).join(','); }).join('\r\n');
  var blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' });
  var url = URL.createObjectURL(blob);
  var a = document.createElement('a');
  a.href = url;
  a.download = 'llm-endpoint-access-logs.csv';
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(function() { URL.revokeObjectURL(url); }, 1000);
}
ensureLLMEndpointAccessLogDialog = function() {
  var overlay = document.getElementById('llmEndpointAccessLogOverlay');
  if (overlay) return overlay;
  overlay = document.createElement('div');
  overlay.id = 'llmEndpointAccessLogOverlay';
  overlay.className = 'session-modal-overlay';
  if (window.AdminUI && typeof AdminUI.bindModalOverlayDismiss === 'function') AdminUI.bindModalOverlayDismiss(overlay, function() { overlay.classList.remove('show'); });
  overlay.innerHTML = '' +
    '<div class="session-modal" style="width:min(1280px,calc(100% - 36px))">' +
    '<button class="close-btn" type="button" onclick="document.getElementById(\'llmEndpointAccessLogOverlay\').classList.remove(\'show\')">&times;</button>' +
    '<div class="head" style="margin-bottom:10px"><div><h3 id="llmEndpointAccessLogDialogTitle"></h3><div class="desc" id="llmEndpointAccessLogDialogDesc"></div></div><div class="actions"><button class="btn-ghost" type="button" id="llmEndpointAccessLogExportBtn" onclick="exportLLMEndpointAccessLogsCSV()"></button><button class="btn-ghost" type="button" id="llmEndpointAccessLogReloadBtn" onclick="reloadLLMEndpointAccessLogs()"></button></div></div>' +
    '<div class="item" style="padding:12px 14px;margin-bottom:10px"><div class="grid4" style="gap:8px"><div><label id="llmEndpointAccessLogFilterProviderLabel"></label><input id="llmEndpointAccessLogFilterProvider" placeholder=""></div><div><label id="llmEndpointAccessLogFilterUpstreamHostLabel"></label><input id="llmEndpointAccessLogFilterUpstreamHost" placeholder=""></div><div><label id="llmEndpointAccessLogFilterClientIPLabel"></label><input id="llmEndpointAccessLogFilterClientIP" placeholder=""></div><div><label id="llmEndpointAccessLogFilterEmailLabel"></label><input id="llmEndpointAccessLogFilterEmail" placeholder=""></div></div><div class="grid4" style="gap:8px;margin-top:8px"><div><label id="llmEndpointAccessLogFilterKeywordLabel"></label><input id="llmEndpointAccessLogFilterKeyword" placeholder=""></div><div><label id="llmEndpointAccessLogFilterStartAtLabel"></label><input id="llmEndpointAccessLogFilterStartAt" type="datetime-local"></div><div><label id="llmEndpointAccessLogFilterEndAtLabel"></label><input id="llmEndpointAccessLogFilterEndAt" type="datetime-local"></div></div><div class="actions" style="margin-top:10px"><button class="btn-primary" type="button" id="llmEndpointAccessLogFilterApplyBtn" onclick="reloadLLMEndpointAccessLogs()"></button><button class="btn-ghost" type="button" id="llmEndpointAccessLogFilterResetBtn" onclick="resetLLMEndpointAccessLogsFilter()"></button></div></div>' +
    '<div id="llmEndpointAccessLogDialogBody"></div>' +
    '<div class="actions" style="margin-top:10px;justify-content:space-between"><div id="llmEndpointAccessLogPageSummary" class="hint"></div><div><button class="btn-ghost" type="button" id="llmEndpointAccessLogPrevBtn" onclick="changeLLMEndpointAccessLogsPage(-1)"></button><button class="btn-ghost" type="button" id="llmEndpointAccessLogNextBtn" onclick="changeLLMEndpointAccessLogsPage(1)"></button></div></div>' +
    '</div>';
  document.body.appendChild(overlay);
  return overlay;
};
renderLLMEndpointAccessLogDialog = function() {
  ensureLLMEndpointAccessLogDialog();
  llmProviderWriteAccessLogFilters();
  _s('llmEndpointAccessLogDialogTitle', 'textContent', lp('accessLogDialogTitle'));
  _s('llmEndpointAccessLogDialogDesc', 'textContent', lp('accessLogDesc'));
  _s('llmEndpointAccessLogReloadBtn', 'textContent', lp('accessLogReload'));
  _s('llmEndpointAccessLogExportBtn', 'textContent', lp('accessLogExport'));
  _s('llmEndpointAccessLogFilterProviderLabel', 'textContent', lp('accessLogFilterProvider'));
  _s('llmEndpointAccessLogFilterUpstreamHostLabel', 'textContent', lp('accessLogFilterUpstreamHost'));
  _s('llmEndpointAccessLogFilterClientIPLabel', 'textContent', lp('accessLogFilterClientIP'));
  _s('llmEndpointAccessLogFilterEmailLabel', 'textContent', lp('accessLogFilterEmail'));
  _s('llmEndpointAccessLogFilterKeywordLabel', 'textContent', lp('accessLogFilterKeyword'));
  _s('llmEndpointAccessLogFilterStartAtLabel', 'textContent', lp('accessLogFilterStartAt'));
  _s('llmEndpointAccessLogFilterEndAtLabel', 'textContent', lp('accessLogFilterEndAt'));
  _s('llmEndpointAccessLogFilterApplyBtn', 'textContent', lp('accessLogFilterApply'));
  _s('llmEndpointAccessLogFilterResetBtn', 'textContent', lp('accessLogFilterReset'));
  _s('llmEndpointAccessLogFilterProvider', 'placeholder', lp('accessLogFilterProviderPlaceholder'));
  _s('llmEndpointAccessLogFilterUpstreamHost', 'placeholder', lp('accessLogFilterUpstreamHostPlaceholder'));
  _s('llmEndpointAccessLogFilterClientIP', 'placeholder', lp('accessLogFilterClientIPPlaceholder'));
  _s('llmEndpointAccessLogFilterEmail', 'placeholder', lp('accessLogFilterEmailPlaceholder'));
  _s('llmEndpointAccessLogFilterKeyword', 'placeholder', lp('accessLogFilterKeywordPlaceholder'));
  _s('llmEndpointAccessLogPrevBtn', 'textContent', lp('accessLogPagePrev'));
  _s('llmEndpointAccessLogNextBtn', 'textContent', lp('accessLogPageNext'));
  var root = document.getElementById('llmEndpointAccessLogDialogBody');
  if (!root) return;
  var logs = llmProviderAccessLogsCache && llmProviderAccessLogsCache.logs || [];
  if (!logs.length) {
    root.innerHTML = '<div class="hint">' + escapeHtml((llmProviderAccessLogsCache && llmProviderAccessLogsCache.total === 0 && (llmProviderAccessLogFilterState.provider || llmProviderAccessLogFilterState.upstream_host || llmProviderAccessLogFilterState.client_ip || llmProviderAccessLogFilterState.email || llmProviderAccessLogFilterState.q || llmProviderAccessLogFilterState.start_at || llmProviderAccessLogFilterState.end_at)) ? lp('accessLogFilteredEmpty') : lp('accessLogEmpty')) + '</div>';
  } else {
    var header = ['accessLogTableTime','accessLogTableProvider','accessLogTableUpstreamHost','accessLogTableClientIP','accessLogTableEmail','accessLogTableModel','accessLogTableTokens','accessLogTableStatus','accessLogTableRequest'];
    var headHtml = '<div class="row header" style="grid-template-columns:1.2fr .9fr 1fr .9fr 1fr 1.2fr .8fr .8fr .8fr;padding:8px 10px">' + header.map(function(key){ return '<div>' + escapeHtml(lp(key)) + '</div>'; }).join('') + '</div>';
    var rows = logs.map(function(item, idx) {
      var meta = item.metadata || {};
      var reqId = String(item.id || idx).replace(/[^a-zA-Z0-9_-]+/g, '_');
      var model = [item.requested_model || '-', item.authorized_model || '-'].join(' -> ');
      var tokens = [Number(item.input_tokens || 0), Number(item.output_tokens || 0), Number(item.total_tokens || 0)].join(' / ') + ' | \u00a5' + llmProviderFormatMoney(item.total_cost_rmb);
      var status = String(item.status_code || 0) + (item.error_code ? ' / ' + item.error_code : '');
      return '<div class="item" style="padding:0 0 10px;margin-bottom:10px">'
        + '<div class="row" style="grid-template-columns:1.2fr .9fr 1fr .9fr 1fr 1.2fr .8fr .8fr .8fr;padding:10px">'
        + '<div class="mono">' + escapeHtml(llmProviderAccessLocaleTime(item.created_at)) + '</div>'
        + '<div class="mono">' + escapeHtml(item.provider_id || '-') + '</div>'
        + '<div class="mono">' + escapeHtml(meta.upstream_host || '-') + '</div>'
        + '<div class="mono">' + escapeHtml(item.client_ip || '-') + '</div>'
        + '<div class="mono">' + escapeHtml(item.email || '-') + '</div>'
        + '<div class="mono">' + escapeHtml(model) + '</div>'
        + '<div class="mono">' + escapeHtml(tokens) + '</div>'
        + '<div class="mono">' + escapeHtml(status) + '</div>'
        + '<div><button class="btn-ghost" type="button" id="llmEndpointAccessLogToggle_' + reqId + '" onclick="llmProviderToggleAccessLogRequest(\'' + reqId + '\')">' + escapeHtml(lp('accessLogShowRequest')) + '</button></div>'
        + '</div>'
        + '<div class="hint" style="padding:0 10px 8px">' + escapeHtml(lp('accessLogRequestHidden')) + '</div>'
        + '<pre id="llmEndpointAccessLogRequest_' + reqId + '" class="console hidden" style="min-height:120px;max-height:260px;margin:0 10px 0">' + escapeHtml(item.request_body || '-') + '</pre>'
        + '</div>';
    }).join('');
    root.innerHTML = '<div class="item" style="padding:12px 14px">' + headHtml + rows + '</div>';
  }
  var total = Number(llmProviderAccessLogsCache && llmProviderAccessLogsCache.total || 0);
  var offset = Number(llmProviderAccessLogsCache && llmProviderAccessLogsCache.offset || 0);
  var from = total ? offset + 1 : 0;
  var to = logs.length ? offset + logs.length : 0;
  _s('llmEndpointAccessLogPageSummary', 'textContent', lp('accessLogPageSummary', { from: from, to: to, total: total }));
  var prevBtn = document.getElementById('llmEndpointAccessLogPrevBtn');
  var nextBtn = document.getElementById('llmEndpointAccessLogNextBtn');
  if (prevBtn) prevBtn.disabled = Number(llmProviderAccessLogPageState.page || 1) <= 1;
  if (nextBtn) nextBtn.disabled = to >= total;
};
window.exportLLMEndpointAccessLogsCSV = exportLLMEndpointAccessLogsCSV;





Object.assign(LLM_PROVIDER_I18N.en, {
  cacheInspectTitle: 'Cached Request Records',
  cacheInspectDesc: 'Show concrete provider-side cache-hit requests for the selected LLM EndPoint.',
  cacheInspectReload: 'Reload Cache Records',
  cacheInspectSearchEmail: 'Filter Email',
  cacheInspectSearchIP: 'Filter IP',
  cacheInspectSearchModel: 'Filter Model',
  cacheInspectClear: 'Clear Filters',
  cacheInspectProvider: 'Current EndPoint',
  cacheInspectScope: 'Cache Scope',
  cacheInspectScopeCurrent: 'Current EndPoint',
  cacheInspectScopeAll: 'All EndPoints',
  cacheInspectAll: 'All EndPoints',
  cacheInspectEmpty: 'No cached request records were found for the current filter.',
  cacheInspectLoading: 'Loading cached request records...',
  cacheInspectLoadFailed: 'Load cached request records failed: {error}',
  cacheInspectView: 'View Details',
  cacheInspectTime: 'Time',
  cacheInspectUser: 'User',
  cacheInspectIP: 'Client IP',
  cacheInspectModel: 'Model',
  cacheInspectProviderCol: 'Provider',
  cacheInspectStatus: 'Status',
  cacheInspectHits: 'Cache read {count}',
  cacheInspectWrites: 'Cache write {count}',
  cacheInspectAccessed: 'Time: {value}',
  cacheInspectDetailTitle: 'Cached Request Detail',
  cacheInspectDetailEmpty: 'Choose a cached request above to inspect its payload and token data.',
  cacheInspectDetailLoading: 'Loading cached request detail...',
  cacheInspectDetailModalTitle: 'Cached Request Detail',
  cacheInspectDetailClose: 'Close',
  cacheInspectCopyRequest: 'Copy Request',
  cacheInspectExportJSON: 'Export JSON',
  cacheInspectCopyDone: 'Request body copied.',
  cacheInspectCopyFailed: 'Copy request body failed: {error}',
  cacheInspectExportDone: 'Cached request JSON exported.',
  cacheInspectDetailProviders: 'Upstream provider',
  cacheInspectDetailAuthorized: 'Authorized model',
  cacheInspectDetailRequested: 'Requested model',
  cacheInspectDetailNormalized: 'Raw request',
  cacheInspectPrev: 'Previous',
  cacheInspectNext: 'Next',
  cacheInspectPage: 'Page {page}',
  cacheInspectPagerMeta: 'Showing {shown} / {total}'
});
Object.assign(LLM_PROVIDER_I18N.zh, {
  cacheInspectTitle: '\u7f13\u5b58\u8bf7\u6c42\u8bb0\u5f55',
  cacheInspectDesc: '\u76f4\u63a5\u67e5\u770b\u5f53\u524d\u9009\u4e2d LLM EndPoint \u4e0b\u53d1\u751f\u7f13\u5b58\u8bfb\u5199\u7684\u5177\u4f53\u8bf7\u6c42\u8bb0\u5f55\u3002',
  cacheInspectReload: '\u5237\u65b0\u7f13\u5b58\u8bb0\u5f55',
  cacheInspectSearchEmail: '\u7b5b\u9009\u90ae\u7bb1',
  cacheInspectSearchIP: '\u7b5b\u9009 IP',
  cacheInspectSearchModel: '\u7b5b\u9009\u6a21\u578b',
  cacheInspectClear: '\u6e05\u7a7a\u7b5b\u9009',
  cacheInspectProvider: '\u5f53\u524d EndPoint',
  cacheInspectScope: '\u67e5\u770b\u8303\u56f4',
  cacheInspectScopeCurrent: '\u5f53\u524d EndPoint',
  cacheInspectScopeAll: '\u5168\u90e8 EndPoint',
  cacheInspectAll: '\u5168\u90e8 EndPoint',
  cacheInspectEmpty: '\u5f53\u524d\u7b5b\u9009\u4e0b\u6682\u65e0\u7f13\u5b58\u8bf7\u6c42\u8bb0\u5f55\u3002',
  cacheInspectLoading: '\u6b63\u5728\u52a0\u8f7d\u7f13\u5b58\u8bf7\u6c42\u8bb0\u5f55...',
  cacheInspectLoadFailed: '\u52a0\u8f7d\u7f13\u5b58\u8bf7\u6c42\u8bb0\u5f55\u5931\u8d25: {error}',
  cacheInspectView: '\u67e5\u770b\u8be6\u60c5',
  cacheInspectTime: '\u65f6\u95f4',
  cacheInspectUser: '\u7528\u6237',
  cacheInspectIP: 'IP',
  cacheInspectModel: '\u6a21\u578b',
  cacheInspectProviderCol: '\u4e0a\u6e38',
  cacheInspectStatus: '\u72b6\u6001',
  cacheInspectHits: '\u7f13\u5b58\u8bfb\u53d6 {count}',
  cacheInspectWrites: '\u7f13\u5b58\u5199\u5165 {count}',
  cacheInspectAccessed: '\u65f6\u95f4: {value}',
  cacheInspectDetailTitle: '\u7f13\u5b58\u8bf7\u6c42\u8be6\u60c5',
  cacheInspectDetailEmpty: '\u8bf7\u5148\u4ece\u4e0a\u65b9\u9009\u4e00\u6761\u7f13\u5b58\u8bf7\u6c42\uff0c\u67e5\u770b\u8bf7\u6c42\u4f53\u548c token \u6570\u636e\u3002',
  cacheInspectDetailLoading: '\u6b63\u5728\u52a0\u8f7d\u7f13\u5b58\u8bf7\u6c42\u8be6\u60c5...',
  cacheInspectDetailModalTitle: '\u7f13\u5b58\u8bf7\u6c42\u8be6\u60c5',
  cacheInspectDetailClose: '\u5173\u95ed',
  cacheInspectCopyRequest: '\u590d\u5236\u8bf7\u6c42\u4f53',
  cacheInspectExportJSON: '\u5bfc\u51fa JSON',
  cacheInspectCopyDone: '\u8bf7\u6c42\u4f53\u5df2\u590d\u5236\u3002',
  cacheInspectCopyFailed: '\u590d\u5236\u8bf7\u6c42\u4f53\u5931\u8d25: {error}',
  cacheInspectExportDone: '\u7f13\u5b58\u8bf7\u6c42 JSON \u5df2\u5bfc\u51fa\u3002',
  cacheInspectDetailProviders: '\u4e0a\u6e38 provider',
  cacheInspectDetailAuthorized: '\u6388\u6743\u6a21\u578b',
  cacheInspectDetailRequested: '\u8bf7\u6c42\u6a21\u578b',
  cacheInspectDetailNormalized: '\u539f\u59cb\u8bf7\u6c42',
  cacheInspectPrev: '\u4e0a\u4e00\u9875',
  cacheInspectNext: '\u4e0b\u4e00\u9875',
  cacheInspectPage: '\u7b2c {page} \u9875',
  cacheInspectPagerMeta: '\u5df2\u663e\u793a {shown} / {total}'
});

let llmProviderPromptCacheState = {
  provider_id: '',
  scope: 'current',
  page: 1,
  total: 0,
  has_more: false,
  filter_email: '',
  filter_ip: '',
  filter_model: '',
  entries: [],
  detail: null,
  loading: false,
  detailLoading: false,
  loaded: false,
  loadError: '',
  detailError: ''
};

function llmProviderPromptCacheActiveProviderId() {
  if (llmProviderPromptCacheState.scope === 'all') return '';
  return String(llmProviderSelectedId || (llmProviderRegistryCache && llmProviderRegistryCache.current_provider_id) || '').trim();
}

function setLLMProviderPromptCacheScope(scope) {
  llmProviderPromptCacheState.scope = scope === 'all' ? 'all' : 'current';
  llmProviderPromptCacheState.page = 1;
  loadLLMProviderPromptCacheEntries(true);
}

function changeLLMProviderPromptCachePage(delta) {
  var next = Math.max(1, Number(llmProviderPromptCacheState.page || 1) + Number(delta || 0));
  if (next === Number(llmProviderPromptCacheState.page || 1)) return;
  llmProviderPromptCacheState.page = next;
  loadLLMProviderPromptCacheEntries(true);
}


function setLLMProviderPromptCacheFilter(kind, value) {
  if (kind === 'email') llmProviderPromptCacheState.filter_email = String(value || '').trim();
  if (kind === 'ip') llmProviderPromptCacheState.filter_ip = String(value || '').trim();
  if (kind === 'model') llmProviderPromptCacheState.filter_model = String(value || '').trim();
  llmProviderPromptCacheState.page = 1;
  loadLLMProviderPromptCacheEntries(true);
}

function clearLLMProviderPromptCacheFilters() {
  llmProviderPromptCacheState.filter_email = '';
  llmProviderPromptCacheState.filter_ip = '';
  llmProviderPromptCacheState.filter_model = '';
  var emailInput = document.getElementById('llmProviderPromptCacheFilterEmail');
  var ipInput = document.getElementById('llmProviderPromptCacheFilterIP');
  var modelInput = document.getElementById('llmProviderPromptCacheFilterModel');
  if (emailInput) emailInput.value = '';
  if (ipInput) ipInput.value = '';
  if (modelInput) modelInput.value = '';
  llmProviderPromptCacheState.page = 1;
  loadLLMProviderPromptCacheEntries(true);
}

function ensureLLMProviderPromptCacheRoot() {
  var summary = ensureLLMProviderCacheSummary();
  if (!summary || !summary.parentElement) return null;
  var root = document.getElementById('llmProviderPromptCacheInspect');
  if (root) return root;
  root = document.createElement('div');
  root.id = 'llmProviderPromptCacheInspect';
  root.style.marginTop = '10px';
  if (summary.nextSibling) summary.parentElement.insertBefore(root, summary.nextSibling);
  else summary.parentElement.appendChild(root);
  return root;
}


function llmProviderPromptCachePrettyRequest(value) {
  if (value == null || value === '') return '-';
  if (typeof value === 'string') {
    try {
      return JSON.stringify(JSON.parse(value), null, 2);
    } catch (_) {
      return value;
    }
  }
  try {
    return JSON.stringify(value, null, 2);
  } catch (_) {
    return String(value);
  }
}

function ensureLLMProviderPromptCacheDetailDialog() {
  var overlay = document.getElementById('llmProviderPromptCacheDetailOverlay');
  if (overlay) return overlay;
  overlay = document.createElement('div');
  overlay.id = 'llmProviderPromptCacheDetailOverlay';
  overlay.className = 'session-modal-overlay';
  if (window.AdminUI && typeof AdminUI.bindModalOverlayDismiss === 'function') AdminUI.bindModalOverlayDismiss(overlay, function() { overlay.classList.remove('show'); });
  overlay.innerHTML = ''
    + '<div class="session-modal" style="width:min(980px,calc(100% - 48px))">'
    + '<button class="close-btn" type="button" onclick="closeLLMProviderPromptCacheDetailDialog()">&times;</button>'
    + '<div class="head" style="margin-bottom:10px"><div><h3 id="llmProviderPromptCacheDetailTitle"></h3><div class="desc" id="llmProviderPromptCacheDetailDesc"></div></div><div class="actions"><button class="btn-ghost" type="button" id="llmProviderPromptCacheDetailCopyBtn" onclick="copyLLMProviderPromptCacheRequest()"></button><button class="btn-ghost" type="button" id="llmProviderPromptCacheDetailExportBtn" onclick="exportLLMProviderPromptCacheDetail()"></button><button class="btn-ghost" type="button" id="llmProviderPromptCacheDetailCloseBtn" onclick="closeLLMProviderPromptCacheDetailDialog()"></button></div></div>'
    + '<div id="llmProviderPromptCacheDetailBody"></div>'
    + '</div>';
  document.body.appendChild(overlay);
  return overlay;
}

function closeLLMProviderPromptCacheDetailDialog() {
  var overlay = document.getElementById('llmProviderPromptCacheDetailOverlay');
  if (overlay) overlay.classList.remove('show');
}

function renderLLMProviderPromptCacheDetailDialog() {
  ensureLLMProviderPromptCacheDetailDialog();
  _s('llmProviderPromptCacheDetailTitle', 'textContent', lp('cacheInspectDetailModalTitle'));
  _s('llmProviderPromptCacheDetailDesc', 'textContent', lp('cacheInspectDesc'));
  _s('llmProviderPromptCacheDetailCopyBtn', 'textContent', lp('cacheInspectCopyRequest'));
  _s('llmProviderPromptCacheDetailExportBtn', 'textContent', lp('cacheInspectExportJSON'));
  _s('llmProviderPromptCacheDetailCloseBtn', 'textContent', lp('cacheInspectDetailClose'));
  var body = document.getElementById('llmProviderPromptCacheDetailBody');
  if (!body) return;
  if (llmProviderPromptCacheState.detailLoading) {
    body.innerHTML = '<div class="hint">' + escapeHtml(lp('cacheInspectDetailLoading')) + '</div>';
    return;
  }
  if (llmProviderPromptCacheState.detailError) {
    body.innerHTML = '<div class="hint">' + escapeHtml(llmProviderPromptCacheState.detailError) + '</div>';
    return;
  }
  if (!llmProviderPromptCacheState.detail) {
    body.innerHTML = '<div class="hint">' + escapeHtml(lp('cacheInspectDetailEmpty')) + '</div>';
    return;
  }
  var detail = llmProviderPromptCacheState.detail;
  var rawBody = llmProviderPromptCachePrettyRequest(detail.request_body);
  var statusText = String(detail.status_code || 0) + (detail.error_code ? ' / ' + detail.error_code : '');
  body.innerHTML = ''
    + '<div class="grid2" style="gap:10px">'
    + '<div class="item" style="min-height:auto;padding:12px 14px"><div class="item-title" style="font-size:13px">' + escapeHtml(lp('cacheInspectDetailRequested')) + '</div><div class="mono" style="margin-top:6px">' + escapeHtml(detail.requested_model || detail.authorized_model || '-') + '</div></div>'
    + '<div class="item" style="min-height:auto;padding:12px 14px"><div class="item-title" style="font-size:13px">' + escapeHtml(lp('cacheInspectDetailAuthorized')) + '</div><div class="mono" style="margin-top:6px">' + escapeHtml(detail.authorized_model || detail.requested_model || '-') + '</div></div>'
    + '<div class="item" style="min-height:auto;padding:12px 14px"><div class="item-title" style="font-size:13px">' + escapeHtml(lp('cacheInspectDetailProviders')) + '</div><div class="mono" style="margin-top:6px">' + escapeHtml(detail.provider_id || '-') + '</div></div>'
    + '<div class="item" style="min-height:auto;padding:12px 14px"><div class="item-title" style="font-size:13px">' + escapeHtml(lp('cacheInspectStatus')) + '</div><div class="mono" style="margin-top:6px">' + escapeHtml(statusText) + '</div></div>'
    + '<div class="item" style="min-height:auto;padding:12px 14px"><div class="item-title" style="font-size:13px">' + escapeHtml(lpMetricLabel('cacheRead')) + '</div><div class="mono" style="margin-top:6px;color:#10aeca">' + escapeHtml(lpFormatInt(detail.cached_input_tokens || 0)) + '</div></div>'
    + '<div class="item" style="min-height:auto;padding:12px 14px"><div class="item-title" style="font-size:13px">' + escapeHtml(lpMetricLabel('cacheWrite')) + '</div><div class="mono" style="margin-top:6px;color:#9b5de5">' + escapeHtml(lpFormatInt(detail.cache_write_tokens || 0)) + '</div></div>'
    + '<div class="item" style="min-height:auto;padding:12px 14px"><div class="item-title" style="font-size:13px">' + escapeHtml(lp('accessLogColEmail')) + '</div><div class="mono" style="margin-top:6px">' + escapeHtml(detail.email || '-') + '</div></div>'
    + '<div class="item" style="min-height:auto;padding:12px 14px"><div class="item-title" style="font-size:13px">' + escapeHtml(lp('accessLogColIP')) + '</div><div class="mono" style="margin-top:6px">' + escapeHtml(detail.client_ip || '-') + '</div></div>'
    + '<div class="item" style="min-height:auto;padding:12px 14px"><div class="item-title" style="font-size:13px">' + escapeHtml(lp('accessLogColTime')) + '</div><div class="mono" style="margin-top:6px">' + escapeHtml(llmProviderAccessLocaleTime(detail.created_at)) + '</div></div>'
    + '</div>'
    + '<div class="item" style="margin-top:10px;padding:12px 14px"><div class="item-title" style="font-size:13px">' + escapeHtml(lp('cacheInspectDetailNormalized')) + '</div><pre class="mono" style="margin-top:8px;white-space:pre-wrap;word-break:break-word;background:rgba(31,34,48,.04);border:1px solid var(--line);border-radius:10px;padding:12px;max-height:420px;overflow:auto">' + escapeHtml(rawBody) + '</pre></div>';
}

function renderLLMProviderPromptCachePanel() {
  var root = ensureLLMProviderPromptCacheRoot();
  if (!root) return;
  var providerID = llmProviderPromptCacheActiveProviderId();
  var scopeLabel = providerID ? providerID : lp('cacheInspectAll');
  var listHTML = '';
  if (llmProviderPromptCacheState.loading && !llmProviderPromptCacheState.loaded) {
    listHTML = '<div class="hint">' + escapeHtml(lp('cacheInspectLoading')) + '</div>';
  } else if (llmProviderPromptCacheState.loadError) {
    listHTML = '<div class="hint">' + escapeHtml(llmProviderPromptCacheState.loadError) + '</div>';
  } else if (!(llmProviderPromptCacheState.entries || []).length) {
    listHTML = '<div class="hint">' + escapeHtml(lp('cacheInspectEmpty')) + '</div>';
  } else {
    var header = '<div class="row header" style="grid-template-columns:1fr 1fr .95fr 1.05fr .95fr .8fr .8fr auto;padding:8px 10px">'
      + '<div>' + escapeHtml(lp('accessLogColTime')) + '</div>'
      + '<div>' + escapeHtml(lp('accessLogColEmail')) + '</div>'
      + '<div>' + escapeHtml(lp('accessLogColIP')) + '</div>'
      + '<div>' + escapeHtml(lp('cacheInspectModel')) + '</div>'
      + '<div>' + escapeHtml(lp('cacheInspectProviderCol')) + '</div>'
      + '<div>' + escapeHtml(lp('cacheInspectStatus')) + '</div>'
      + '<div>' + escapeHtml(lpMetricLabel('cacheRead')) + '</div>'
      + '<div>' + escapeHtml(lpMetricLabel('cacheWrite')) + '</div>'
      + '<div></div>'
      + '</div>';
    var rows = llmProviderPromptCacheState.entries.map(function(item) {
      var detailId = String(item.id || '');
      var statusText = String(item.status_code || 0) + (item.error_code ? ' / ' + item.error_code : '');
      return '<div class="row" style="grid-template-columns:1fr 1fr .95fr 1.05fr .95fr .8fr .8fr auto;padding:8px 10px;align-items:center">'
        + '<div class="mono" style="font-size:11px">' + escapeHtml(llmProviderAccessLocaleTime(item.created_at)) + '</div>'
        + '<div class="mono" style="font-size:11px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(item.email || '-') + '</div>'
        + '<div class="mono" style="font-size:11px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(item.client_ip || '-') + '</div>'
        + '<div class="mono" style="font-size:11px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(item.requested_model || item.authorized_model || '-') + '</div>'
        + '<div class="mono" style="font-size:11px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + escapeHtml(item.provider_id || scopeLabel || '-') + '</div>'
        + '<div class="mono" style="font-size:11px">' + escapeHtml(statusText) + '</div>'
        + '<div class="mono" style="font-size:11px;color:#10aeca">' + escapeHtml(lpFormatInt(item.cached_input_tokens || 0)) + '</div>'
        + '<div class="mono" style="font-size:11px;color:#9b5de5">' + escapeHtml(lpFormatInt(item.cache_write_tokens || 0)) + '</div>'
        + '<div style="display:flex;justify-content:flex-end"><button class="btn-secondary" type="button" style="height:24px;padding:0 8px;font-size:11px;line-height:1;white-space:nowrap" onclick="viewLLMProviderPromptCacheEntry(' + "'" + encodeURIComponent(detailId) + "'" + ')">' + escapeHtml(lp('cacheInspectView')) + '</button></div>'
        + '</div>';
    }).join('');
    listHTML = '<div class="table" style="gap:4px">' + header + rows + '</div>';
    var total = Number(llmProviderPromptCacheState.total || 0);
    var shown = Array.isArray(llmProviderPromptCacheState.entries) ? llmProviderPromptCacheState.entries.length : 0;
    var page = Number(llmProviderPromptCacheState.page || 1);
    var hasMore = !!llmProviderPromptCacheState.has_more;
    if (total > 0 || page > 1 || hasMore) {
      listHTML += '<div class="pager" style="margin-top:8px"><div class="pager-meta">' + escapeHtml(lp('cacheInspectPage', { page: String(page) })) + ' | ' + escapeHtml(lp('cacheInspectPagerMeta', { shown: String(shown), total: String(total) })) + '</div><div class="pager-actions"><button class="btn-ghost" type="button" style="height:28px;padding:0 10px" onclick="changeLLMProviderPromptCachePage(-1)"' + (page <= 1 ? ' disabled' : '') + '>' + escapeHtml(lp('cacheInspectPrev')) + '</button><button class="btn-ghost" type="button" style="height:28px;padding:0 10px" onclick="changeLLMProviderPromptCachePage(1)"' + (!hasMore ? ' disabled' : '') + '>' + escapeHtml(lp('cacheInspectNext')) + '</button></div></div>';
    }
  }
  var detailHTML = '';
  if (llmProviderPromptCacheState.detailLoading) {
    detailHTML = '<div class="hint">' + escapeHtml(lp('cacheInspectDetailLoading')) + '</div>';
  } else if (llmProviderPromptCacheState.detailError) {
    detailHTML = '<div class="hint">' + escapeHtml(llmProviderPromptCacheState.detailError) + '</div>';
  } else if (!llmProviderPromptCacheState.detail) {
    detailHTML = '<div class="hint">' + escapeHtml(lp('cacheInspectDetailEmpty')) + '</div>';
  } else {
    var detail = llmProviderPromptCacheState.detail;
    detailHTML = '<div class="item" style="padding:12px 14px"><div class="item-title" style="font-size:13px">' + escapeHtml((detail.requested_model || detail.authorized_model || '-') + ' | ' + (detail.provider_id || '-')) + '</div><div class="item-meta mono" style="margin-top:6px">' + escapeHtml((detail.email || '-') + ' | ' + (detail.client_ip || '-')) + '</div><div class="item-meta" style="margin-top:10px">' + escapeHtml(lp('cacheInspectStatus')) + ': ' + escapeHtml(String(detail.status_code || 0) + (detail.error_code ? ' / ' + detail.error_code : '')) + '</div><div class="actions" style="margin-top:10px"><button class="btn-secondary" type="button" style="height:32px;padding:0 12px" onclick="openLLMProviderPromptCacheDetailDialog()">' + escapeHtml(lp('cacheInspectView')) + '</button></div></div>';
  }
  root.innerHTML = '<div class="item" style="padding:12px 14px">'
    + '<div class="item-head" style="align-items:flex-start;gap:10px"><div><div class="item-title" style="font-size:14px">' + escapeHtml(lp('cacheInspectTitle')) + '</div><div class="item-meta">' + escapeHtml(lp('cacheInspectDesc')) + '</div></div><div class="actions" style="margin-left:auto;gap:8px;flex-wrap:wrap"><label class="item-meta" style="display:flex;align-items:center;gap:6px">' + escapeHtml(lp('cacheInspectScope')) + '<select style="height:32px" onchange="setLLMProviderPromptCacheScope(this.value)"><option value="current"' + (llmProviderPromptCacheState.scope === 'current' ? ' selected' : '') + '>' + escapeHtml(lp('cacheInspectScopeCurrent')) + '</option><option value="all"' + (llmProviderPromptCacheState.scope === 'all' ? ' selected' : '') + '>' + escapeHtml(lp('cacheInspectScopeAll')) + '</option></select></label><span class="badge info">' + escapeHtml(lp('cacheInspectProvider')) + ': ' + escapeHtml(scopeLabel) + '</span><button class="btn-ghost" type="button" style="height:32px;padding:0 12px" onclick="reloadLLMProviderPromptCacheEntries()">' + escapeHtml(lp('cacheInspectReload')) + '</button></div></div>'
    + '<div class="row" style="grid-template-columns:1fr 1fr 1fr auto;gap:8px;margin-top:10px;padding:0;border:none;background:transparent"><input id="llmProviderPromptCacheFilterEmail" style="height:34px" placeholder="' + escapeHtml(lp('cacheInspectSearchEmail')) + '" value="' + escapeHtml(llmProviderPromptCacheState.filter_email || '') + '" oninput="setLLMProviderPromptCacheFilter(\'email\', this.value)"><input id="llmProviderPromptCacheFilterIP" style="height:34px" placeholder="' + escapeHtml(lp('cacheInspectSearchIP')) + '" value="' + escapeHtml(llmProviderPromptCacheState.filter_ip || '') + '" oninput="setLLMProviderPromptCacheFilter(\'ip\', this.value)"><input id="llmProviderPromptCacheFilterModel" style="height:34px" placeholder="' + escapeHtml(lp('cacheInspectSearchModel')) + '" value="' + escapeHtml(llmProviderPromptCacheState.filter_model || '') + '" oninput="setLLMProviderPromptCacheFilter(\'model\', this.value)"><button class="btn-ghost" type="button" style="height:34px;padding:0 12px" onclick="clearLLMProviderPromptCacheFilters()">' + escapeHtml(lp('cacheInspectClear')) + '</button></div>'
    + '<div class="grid2" style="margin-top:10px;gap:10px"><div>' + listHTML + '</div><div><div class="item-title" style="margin-bottom:8px;font-size:13px">' + escapeHtml(lp('cacheInspectDetailTitle')) + '</div>' + detailHTML + '</div></div>'
    + '</div>';
}

async function loadLLMProviderPromptCacheEntries(force) {
  var providerID = llmProviderPromptCacheActiveProviderId();
  var page = Math.max(1, Number(llmProviderPromptCacheState.page || 1));
  var limit = 6;
  if (!force && llmProviderPromptCacheState.loaded && llmProviderPromptCacheState.provider_id === providerID && Number(llmProviderPromptCacheState.page || 1) === page) {
    renderLLMProviderPromptCachePanel();
    return;
  }
  llmProviderPromptCacheState.provider_id = providerID;
  llmProviderPromptCacheState.page = page;
  llmProviderPromptCacheState.loading = true;
  llmProviderPromptCacheState.loadError = '';
  if (force) {
    llmProviderPromptCacheState.entries = [];
    llmProviderPromptCacheState.total = 0;
    llmProviderPromptCacheState.has_more = false;
    llmProviderPromptCacheState.detail = null;
    llmProviderPromptCacheState.detailError = '';
  }
  renderLLMProviderPromptCachePanel();
  try {
    var params = new URLSearchParams({ limit: String(limit), offset: String(Math.max(0, (page - 1) * limit)), cached_only: '1' });
    if (providerID) params.set('provider', providerID);
    if (llmProviderPromptCacheState.filter_email) params.set('email', llmProviderPromptCacheState.filter_email);
    if (llmProviderPromptCacheState.filter_ip) params.set('client_ip', llmProviderPromptCacheState.filter_ip);
    if (llmProviderPromptCacheState.filter_model) params.set('q', llmProviderPromptCacheState.filter_model);
    var data = await api('/api/admin/llm/access-logs?' + params.toString());
    llmProviderPromptCacheState.entries = Array.isArray(data && data.logs) ? data.logs : [];
    llmProviderPromptCacheState.total = Number(data && data.total || 0);
    llmProviderPromptCacheState.has_more = (((page - 1) * limit) + llmProviderPromptCacheState.entries.length) < llmProviderPromptCacheState.total;
    llmProviderPromptCacheState.page = Math.floor(Number(data && data.offset || 0) / limit) + 1;
    llmProviderPromptCacheState.loaded = true;
  } catch (err) {
    llmProviderPromptCacheState.loadError = lp('cacheInspectLoadFailed', { error: err.message });
  } finally {
    llmProviderPromptCacheState.loading = false;
    renderLLMProviderPromptCachePanel();
  }
}

async function reloadLLMProviderPromptCacheEntries() {
  await loadLLMProviderPromptCacheEntries(true);
}

function openLLMProviderPromptCacheDetailDialog() {
  var overlay = ensureLLMProviderPromptCacheDetailDialog();
  renderLLMProviderPromptCacheDetailDialog();
  overlay.classList.add('show');
}


async function copyLLMProviderPromptCacheRequest() {
  var detail = llmProviderPromptCacheState.detail;
  if (!detail) return;
  var payload = llmProviderPromptCachePrettyRequest(detail.request_body);
  try {
    if (navigator && navigator.clipboard && typeof navigator.clipboard.writeText === 'function') {
      await navigator.clipboard.writeText(payload);
    } else {
      var textarea = document.createElement('textarea');
      textarea.value = payload;
      textarea.style.position = 'fixed';
      textarea.style.opacity = '0';
      document.body.appendChild(textarea);
      textarea.focus();
      textarea.select();
      document.execCommand('copy');
      document.body.removeChild(textarea);
    }
    setOutput(lp('cacheInspectCopyDone'));
    showToast(lp('cacheInspectCopyDone'), 'success');
  } catch (err) {
    var msg = lp('cacheInspectCopyFailed', { error: err && err.message || 'copy failed' });
    setOutput(msg);
    showToast(msg, 'error');
  }
}

function exportLLMProviderPromptCacheDetail() {
  var detail = llmProviderPromptCacheState.detail;
  if (!detail) return;
  var blob = new Blob([JSON.stringify(detail, null, 2)], { type: 'application/json;charset=utf-8' });
  var url = URL.createObjectURL(blob);
  var a = document.createElement('a');
  a.href = url;
  a.download = 'cached-request-' + String(detail.id || detail.created_at || 'record').replace(/[^a-zA-Z0-9._-]+/g, '-') + '.json';
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
  setOutput(lp('cacheInspectExportDone'));
  showToast(lp('cacheInspectExportDone'), 'success');
}

async function viewLLMProviderPromptCacheEntry(entryId) {
  var key = decodeURIComponent(String(entryId || ''));
  if (!key) return;
  llmProviderPromptCacheState.detailLoading = true;
  llmProviderPromptCacheState.detailError = '';
  llmProviderPromptCacheState.detail = null;
  renderLLMProviderPromptCachePanel();
  renderLLMProviderPromptCacheDetailDialog();
  openLLMProviderPromptCacheDetailDialog();
  try {
    llmProviderPromptCacheState.detail = (llmProviderPromptCacheState.entries || []).find(function(item) {
      return String(item && item.id || '') === key;
    }) || null;
    if (!llmProviderPromptCacheState.detail) throw new Error(lp('cacheInspectDetailEmpty'));
  } catch (err) {
    llmProviderPromptCacheState.detailError = lp('cacheInspectLoadFailed', { error: err.message });
  } finally {
    llmProviderPromptCacheState.detailLoading = false;
    renderLLMProviderPromptCachePanel();
    renderLLMProviderPromptCacheDetailDialog();
  }
}
const baseRenderLLMProviderCacheSummaryInspect = typeof renderLLMProviderCacheSummary === 'function' ? renderLLMProviderCacheSummary : null;
if (baseRenderLLMProviderCacheSummaryInspect) {
  renderLLMProviderCacheSummary = function() {
    baseRenderLLMProviderCacheSummaryInspect();
    renderLLMProviderPromptCachePanel();
  };
}

const baseLoadLLMProvidersPromptCacheInspect = typeof loadLLMProviders === 'function' ? loadLLMProviders : null;
if (baseLoadLLMProvidersPromptCacheInspect) {
  loadLLMProviders = async function() {
    await baseLoadLLMProvidersPromptCacheInspect();
    await loadLLMProviderPromptCacheEntries(true);
  };
}

const baseSelectLLMProviderPromptCacheInspect = typeof selectLLMProvider === 'function' ? selectLLMProvider : null;
if (baseSelectLLMProviderPromptCacheInspect) {
  selectLLMProvider = function(id) {
    baseSelectLLMProviderPromptCacheInspect(id);
    loadLLMProviderPromptCacheEntries(true);
  };
}

const baseSetCurrentLLMProviderPromptCacheInspect = typeof setCurrentLLMProvider === 'function' ? setCurrentLLMProvider : null;
if (baseSetCurrentLLMProviderPromptCacheInspect) {
  setCurrentLLMProvider = function(id) {
    baseSetCurrentLLMProviderPromptCacheInspect(id);
    loadLLMProviderPromptCacheEntries(true);
  };
}

window.reloadLLMProviderPromptCacheEntries = reloadLLMProviderPromptCacheEntries;
window.viewLLMProviderPromptCacheEntry = viewLLMProviderPromptCacheEntry;
window.openLLMProviderPromptCacheDetailDialog = openLLMProviderPromptCacheDetailDialog;
window.closeLLMProviderPromptCacheDetailDialog = closeLLMProviderPromptCacheDetailDialog;
window.copyLLMProviderPromptCacheRequest = copyLLMProviderPromptCacheRequest;
window.exportLLMProviderPromptCacheDetail = exportLLMProviderPromptCacheDetail;
window.setLLMProviderPromptCacheScope = setLLMProviderPromptCacheScope;
window.changeLLMProviderPromptCachePage = changeLLMProviderPromptCachePage;
window.setLLMProviderPromptCacheFilter = setLLMProviderPromptCacheFilter;
window.clearLLMProviderPromptCacheFilters = clearLLMProviderPromptCacheFilters;
renderLLMProviderPromptCachePanel();
renderLLMProviderPromptCacheDetailDialog();


Object.assign(LLM_PROVIDER_I18N.en, {
  upstreamTimeoutSec: 'Upstream Timeout (sec)',
  upstreamTimeoutHint: 'Default 900. Increase for long reasoning or slow providers.',
  upstreamTimeoutBadge: 'Upstream timeout'
});
Object.assign(LLM_PROVIDER_I18N.zh, {
  upstreamTimeoutSec: '\u4e0a\u6e38\u8d85\u65f6\uff08\u79d2\uff09',
  upstreamTimeoutHint: '\u9ed8\u8ba4 900\u3002\u957f\u63a8\u7406\u6216\u6162\u901f\u4e0a\u6e38\u53ef\u8c03\u5927\u3002',
  upstreamTimeoutBadge: '\u4e0a\u6e38\u8d85\u65f6'
});
function llmProviderNormalizeUpstreamTimeoutSec(value) {
  value = Number(value || 0);
  return Number.isFinite(value) && value > 0 ? Math.floor(value) : 900;
}
function llmProviderEnsureUpstreamTimeoutField() {
  if (document.getElementById('llmProviderUpstreamTimeoutSec')) return;
  var queue = document.getElementById('llmProviderQueueTimeoutMs');
  if (!queue || !queue.parentElement) return;
  var field = document.createElement('div');
  field.className = queue.parentElement.className || '';
  field.innerHTML = '<label id="llmProviderUpstreamTimeoutSecLabel" for="llmProviderUpstreamTimeoutSec"></label><input id="llmProviderUpstreamTimeoutSec" type="number" min="1" step="1" value="900"><div class="hint" id="llmProviderUpstreamTimeoutSecHint"></div>';
  if (queue.parentElement.nextSibling) queue.parentElement.parentElement.insertBefore(field, queue.parentElement.nextSibling);
  else queue.parentElement.parentElement.appendChild(field);
}
function llmProviderReadUpstreamTimeoutSec() {
  var el = document.getElementById('llmProviderUpstreamTimeoutSec');
  return llmProviderNormalizeUpstreamTimeoutSec(el && el.value || 900);
}
function llmProviderWriteUpstreamTimeoutSec(provider) {
  llmProviderEnsureUpstreamTimeoutField();
  _s('llmProviderUpstreamTimeoutSec', 'value', String(llmProviderNormalizeUpstreamTimeoutSec(provider && provider.upstream_timeout_sec || 900)));
}
const baseEnsureLLMProviderModalUIUpstreamTimeout = typeof ensureLLMProviderModalUI === 'function' ? ensureLLMProviderModalUI : null;
if (baseEnsureLLMProviderModalUIUpstreamTimeout) {
  ensureLLMProviderModalUI = function() {
    baseEnsureLLMProviderModalUIUpstreamTimeout();
    llmProviderEnsureUpstreamTimeoutField();
  };
}
const baseLpCloneUpstreamTimeout = typeof lpClone === 'function' ? lpClone : null;
if (baseLpCloneUpstreamTimeout) {
  lpClone = function(provider) {
    var next = baseLpCloneUpstreamTimeout(provider);
    next.upstream_timeout_sec = llmProviderNormalizeUpstreamTimeoutSec(provider && provider.upstream_timeout_sec || 900);
    return next;
  };
}
const baseReadSelectedLLMProviderFormUpstreamTimeout = typeof readSelectedLLMProviderForm === 'function' ? readSelectedLLMProviderForm : null;
if (baseReadSelectedLLMProviderFormUpstreamTimeout) {
  readSelectedLLMProviderForm = function() {
    var next = baseReadSelectedLLMProviderFormUpstreamTimeout();
    next.upstream_timeout_sec = llmProviderReadUpstreamTimeoutSec();
    return next;
  };
}
const baseClearLLMProviderFormUpstreamTimeout = typeof clearLLMProviderForm === 'function' ? clearLLMProviderForm : null;
if (baseClearLLMProviderFormUpstreamTimeout) {
  clearLLMProviderForm = function() {
    baseClearLLMProviderFormUpstreamTimeout();
    llmProviderWriteUpstreamTimeoutSec({ upstream_timeout_sec: 900 });
  };
}
const baseOpenLLMProviderDialogUpstreamTimeout = typeof openLLMProviderDialog === 'function' ? openLLMProviderDialog : null;
if (baseOpenLLMProviderDialogUpstreamTimeout) {
  openLLMProviderDialog = function(mode) {
    baseOpenLLMProviderDialogUpstreamTimeout(mode);
    llmProviderWriteUpstreamTimeoutSec(mode === 'edit' ? lpById(llmProviderSelectedId) : { upstream_timeout_sec: 900 });
  };
}
const baseBuildLLMProviderPayloadUpstreamTimeout = typeof buildLLMProviderPayload === 'function' ? buildLLMProviderPayload : null;
if (baseBuildLLMProviderPayloadUpstreamTimeout) {
  buildLLMProviderPayload = function() {
    var payload = baseBuildLLMProviderPayloadUpstreamTimeout();
    var providers = llmProviderRegistryCache && llmProviderRegistryCache.providers || [];
    var byID = {};
    providers.forEach(function(p) { if (p && p.id) byID[p.id] = p; });
    payload.providers = (payload.providers || []).map(function(p, idx) {
      var cached = byID[p.id] || providers[idx] || {};
      p.upstream_timeout_sec = llmProviderNormalizeUpstreamTimeoutSec(cached.upstream_timeout_sec || p.upstream_timeout_sec || 900);
      return p;
    });
    return payload;
  };
}
const baseLLMProviderImportedPayloadUpstreamTimeout = typeof llmProviderImportedPayload === 'function' ? llmProviderImportedPayload : null;
if (baseLLMProviderImportedPayloadUpstreamTimeout) {
  llmProviderImportedPayload = function(raw) {
    var imported = baseLLMProviderImportedPayloadUpstreamTimeout(raw);
    var source = {};
    (raw && raw.providers || []).forEach(function(p) { if (p && p.id) source[lpNormalizeId(p.id)] = p; });
    imported.providers = (imported.providers || []).map(function(p) {
      var original = source[p.id] || {};
      p.upstream_timeout_sec = llmProviderNormalizeUpstreamTimeoutSec(original.upstream_timeout_sec || p.upstream_timeout_sec || 900);
      return p;
    });
    return imported;
  };
}
const baseRenderLLMProvidersUpstreamTimeout = typeof renderLLMProviders === 'function' ? renderLLMProviders : null;
if (baseRenderLLMProvidersUpstreamTimeout) {
  renderLLMProviders = function() {
    baseRenderLLMProvidersUpstreamTimeout();
    var selected = lpById(llmProviderSelectedId);
    if (llmProviderDialogOpen()) llmProviderWriteUpstreamTimeoutSec(selected || { upstream_timeout_sec: 900 });
    var cards = document.querySelectorAll('.item[data-provider-id]');
    cards.forEach(function(card) {
      var id = card.getAttribute('data-provider-id');
      var provider = lpById(id);
      if (!provider) return;
      var marker = card.querySelector('.llm-provider-upstream-timeout');
      if (!marker) {
        marker = document.createElement('span');
        marker.className = 'llm-provider-upstream-timeout';
      }
      marker.style.display = 'inline-flex';
      marker.style.alignItems = 'center';
      marker.style.whiteSpace = 'nowrap';
      var metaRow = card.querySelector('.llm-provider-meta-row');
      if (metaRow && marker.parentElement !== metaRow) metaRow.appendChild(marker);
      else if (!marker.parentElement) card.appendChild(marker);
      marker.textContent = (currentLang === 'zh' ? '\u8d85\u65f6: ' : 'Timeout: ') + String(llmProviderNormalizeUpstreamTimeoutSec(provider.upstream_timeout_sec)) + 's';
    });
  };
}
const baseApplyLLMProvidersI18nUpstreamTimeout = typeof applyLLMProvidersI18n === 'function' ? applyLLMProvidersI18n : null;
if (baseApplyLLMProvidersI18nUpstreamTimeout) {
  applyLLMProvidersI18n = function() {
    baseApplyLLMProvidersI18nUpstreamTimeout();
    llmProviderEnsureUpstreamTimeoutField();
    _s('llmProviderUpstreamTimeoutSecLabel', 'textContent', lp('upstreamTimeoutSec'));
    _s('llmProviderUpstreamTimeoutSecHint', 'textContent', lp('upstreamTimeoutHint'));
  };
  ensureLLMProviderModalUI();
  applyLLMProvidersI18n();
}

function llmProviderOptionsSnapshot() {
  return (llmProviderRegistryCache && llmProviderRegistryCache.providers || []).map(function(p) {
    return { id: String(p && p.id || '').trim(), name: String(p && p.name || '').trim() };
  }).filter(function(p) { return p.id; });
}
window.getLlmProviderOptions = llmProviderOptionsSnapshot;

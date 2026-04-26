/*
 * Hub LLM admin module.
 * ASCII only.
 */
const HUB_LLM_I18N = {
  en: {
    apiKeyConfigured: 'Configured (leave empty to keep)',
    apiKeyEnter: 'Enter API Key',
    unknownError: 'Unknown',
    saved: 'Hub LLM config saved.',
    saveFailed: 'Save Hub LLM config failed: {error}',
    loadFailed: 'Load Hub LLM config failed: {error}',
    cacheSaved: 'Prompt cache config saved.',
    cacheSaveFailed: 'Save prompt cache config failed: {error}',
    cacheLoadFailed: 'Load prompt cache config failed: {error}',
    cacheRefresh: 'Refresh Cache Data',
    cacheClear: 'Clear Cache',
    cacheCleared: 'Prompt cache cleared ({count} entries).',
    cacheClearFailed: 'Clear prompt cache failed: {error}',
    testing: 'Testing...',
    testBtn: 'Test Connection',
    testOk: '\u2705 LLM connected ({ms}ms), reply: {reply}',
    testFail: '\u274c LLM connection failed: {error}',
    testError: '\u274c LLM test failed: {error}',
    statusHealthy: '\ud83d\udfe2 Healthy',
    statusHalfOpen: '\ud83d\udfe1 Recovering',
    statusOpen: '\ud83d\udd34 Circuit Open',
    statusNone: '\u26aa Not Configured',
    statusUnknown: '\u26aa Status: {status}',
    cacheRate: 'Cache rate {rate}%',
    cacheDisk: 'Disk cache {bytes}',
    cacheMemory: 'Memory {bytes}',
    cacheConfigTitle: 'Prompt Cache',
    cacheConfigDesc: 'Configure local prompt cache TTL and memory/disk budget.',
    cacheEnabled: 'Enable local cache',
    cacheTTL: 'TTL (seconds)',
    cacheMemoryEntries: 'Memory entries',
    cacheMemoryMB: 'Memory budget (MB)',
    cacheDiskMB: 'Disk budget (MB)',
    cacheNormalizeDeterministic: 'Normalize deterministic defaults',
    cacheIgnoreModel: 'Ignore request model field',
    cacheIgnoreUser: 'Ignore request user field',
    cacheIgnoreMetadata: 'Ignore request metadata field',
    cacheSingleflightTimeout: 'Singleflight wait timeout (ms)',
    cacheSave: 'Save Cache Settings',
    cacheSummary: 'Request cache {rate}% ({hits}/{requests}) | Token reuse {reuse}% | Memory {memUsed}/{memLimit} ({memPct}%) | Disk {diskUsed}/{diskLimit} ({diskPct}%) | Hits M/D {memHits}/{diskHits}',
    cacheSummaryEmpty: 'Prompt cache status will appear here after loading.',
    saveBtn: 'Save Hub LLM',
    paneTitle: 'Hub LLM',
    paneDesc: 'Configure the built-in LLM gateway and local prompt cache.',
    enabledLabel: 'Enable Hub LLM routing',
    smartRouteSingleLabel: 'Single-device smart route only',
    apiUrlLabel: 'API URL',
    apiKeyLabel: 'API Key',
    modelLabel: 'Model',
    protocolLabel: 'Protocol',
    guideTitle: 'Guide',
    guideContent: 'Use this section to configure the upstream Hub LLM endpoint. The local prompt cache only applies to non-stream requests with the same authorized model and request body.',
    cacheEntriesTitle: 'Recent Cache Entries',
    cacheEntriesDesc: 'Inspect recently stored prompt-cache responses and filter by provider/model.',
    cacheEntriesEmpty: 'No cache entries matched the current filter.',
    cacheEntriesLoadFailed: 'Load cache entries failed: {error}',
    cacheEntryMeta: 'Provider {provider} | {size} | hits {hits}',
    cacheEntriesProvider: 'Provider filter',
    cacheEntriesModel: 'Model filter',
    cacheEntriesRefresh: 'Refresh',
    cacheEntryCachedTokens: '{count} cached',
    cacheEntryLastAccess: 'Last access: {value}',
    cacheEntryExpires: 'Expires: {value}',
    cacheEntryDetails: 'Details',
    cacheEntryDelete: 'Delete',
    cacheEntryDeleted: 'Cache entry deleted.',
    cacheEntryDeleteFailed: 'Delete cache entry failed: {error}',
    cacheEntryDeleteConfirm: 'Delete this cache entry?',
    cacheEntryDetailTitle: 'Cache Entry Details',
    cacheEntryDetailDesc: 'Inspect the normalized request and routing factors behind a cached response.',
    cacheEntryDetailEmpty: 'Select a cache entry to inspect its normalized request and routing details.',
    cacheEntryDetailLoadFailed: 'Load cache entry details failed: {error}',
    cacheEntryDetailKey: 'Cache key',
    cacheEntryDetailAuthorizedModel: 'Authorized model',
    cacheEntryDetailRequestedModel: 'Requested model',
    cacheEntryDetailProviders: 'Ordered providers',
    cacheEntryDetailNormalizedRequest: 'Normalized request',
    cacheEntriesPage: 'Page {page}',
    cacheEntriesPagerMeta: 'Showing {shown} / {total}',
    cacheEntriesPrev: 'Previous',
    cacheEntriesNext: 'Next',
    cacheEntriesAllProviders: 'All providers',
    cacheEntriesAllModels: 'All models',
    cacheHitSplitTitle: 'Cache Hit Sources',
    cacheHitSplitEmpty: 'Hit source breakdown will appear after traffic arrives.',
    cacheHitMemory: 'Memory hits',
    cacheHitDisk: 'Disk hits',
    cacheHitShare: '{label} {count} ({pct}%)',
    cacheRuntimeTitle: 'Cache Runtime Signals',
    cacheRuntimeEmpty: 'Runtime cache signals will appear after requests arrive.',
    cacheRuntimeCacheable: 'Cacheable requests: {count}',
    cacheRuntimeSingleflight: 'Singleflight shared waits: {shared} | Saved upstream calls: {saved}',
    cacheRuntimeBypassTitle: 'Bypass reasons',
    cacheRuntimeBypassItem: '{reason}: {count}'
  },
  zh: {
    apiKeyConfigured: '\u5df2\u914d\u7f6e\uff08\u7559\u7a7a\u4fdd\u6301\u4e0d\u53d8\uff09',
    apiKeyEnter: '\u8bf7\u8f93\u5165 API Key',
    unknownError: '\u672a\u77e5\u9519\u8bef',
    saved: 'Hub LLM \u914d\u7f6e\u5df2\u4fdd\u5b58\u3002',
    saveFailed: '\u4fdd\u5b58 Hub LLM \u914d\u7f6e\u5931\u8d25\uff1a{error}',
    loadFailed: '\u52a0\u8f7d Hub LLM \u914d\u7f6e\u5931\u8d25\uff1a{error}',
    cacheSaved: '\u7f13\u5b58\u914d\u7f6e\u5df2\u4fdd\u5b58\u3002',
    cacheSaveFailed: '\u4fdd\u5b58\u7f13\u5b58\u914d\u7f6e\u5931\u8d25\uff1a{error}',
    cacheLoadFailed: '\u52a0\u8f7d\u7f13\u5b58\u914d\u7f6e\u5931\u8d25\uff1a{error}',
    cacheRefresh: '\u5237\u65b0\u7f13\u5b58\u6570\u636e',
    cacheClear: '\u6e05\u7a7a\u7f13\u5b58',
    cacheCleared: '\u7f13\u5b58\u5df2\u6e05\u7a7a\uff08{count} \u6761\uff09\u3002',
    cacheClearFailed: '\u6e05\u7a7a\u7f13\u5b58\u5931\u8d25\uff1a{error}',
    testing: '\u6d4b\u8bd5\u4e2d...',
    testBtn: '\u6d4b\u8bd5\u8fde\u63a5',
    testOk: '\u2705 LLM \u8fde\u63a5\u6210\u529f ({ms}ms)\uff0c\u56de\u590d: {reply}',
    testFail: '\u274c LLM \u8fde\u63a5\u5931\u8d25: {error}',
    testError: '\u274c LLM \u6d4b\u8bd5\u5931\u8d25: {error}',
    statusHealthy: '\ud83d\udfe2 \u6b63\u5e38',
    statusHalfOpen: '\ud83d\udfe1 \u6062\u590d\u4e2d',
    statusOpen: '\ud83d\udd34 \u7194\u65ad\u4e2d',
    statusNone: '\u26aa \u672a\u914d\u7f6e',
    statusUnknown: '\u26aa \u72b6\u6001\uff1a{status}',
    cacheRate: '\u7f13\u5b58\u7387 {rate}%',
    cacheDisk: '\u78c1\u76d8\u7f13\u5b58 {bytes}',
    cacheMemory: '\u5185\u5b58 {bytes}',
    cacheConfigTitle: 'Prompt \u7f13\u5b58',
    cacheConfigDesc: '\u914d\u7f6e\u672c\u5730 prompt \u7f13\u5b58\u7684 TTL \u548c\u5185\u5b58/\u78c1\u76d8\u989d\u5ea6\u3002',
    cacheEnabled: '\u542f\u7528\u672c\u5730\u7f13\u5b58',
    cacheTTL: 'TTL (\u79d2)',
    cacheMemoryEntries: '\u5185\u5b58\u6761\u76ee\u4e0a\u9650',
    cacheMemoryMB: '\u5185\u5b58\u9884\u7b97 (MB)',
    cacheDiskMB: '\u78c1\u76d8\u9884\u7b97 (MB)',
    cacheNormalizeDeterministic: '\u5f52\u4e00\u5316\u786e\u5b9a\u6027\u9ed8\u8ba4\u53c2\u6570',
    cacheIgnoreModel: '\u5ffd\u7565\u8bf7\u6c42\u4e2d\u7684 model \u5b57\u6bb5',
    cacheIgnoreUser: '\u5ffd\u7565\u8bf7\u6c42\u4e2d\u7684 user \u5b57\u6bb5',
    cacheIgnoreMetadata: '\u5ffd\u7565\u8bf7\u6c42\u4e2d\u7684 metadata \u5b57\u6bb5',
    cacheSingleflightTimeout: 'Singleflight \u7b49\u5f85\u8d85\u65f6 (ms)',
    cacheSave: '\u4fdd\u5b58\u7f13\u5b58\u8bbe\u7f6e',
    cacheSummary: '\u8bf7\u6c42\u7f13\u5b58\u7387 {rate}% ({hits}/{requests}) | Token \u590d\u7528\u7387 {reuse}% | \u5185\u5b58 {memUsed}/{memLimit} ({memPct}%) | \u78c1\u76d8 {diskUsed}/{diskLimit} ({diskPct}%) | \u547d\u4e2d M/D {memHits}/{diskHits}',
    cacheSummaryEmpty: '\u52a0\u8f7d\u540e\u8fd9\u91cc\u4f1a\u663e\u793a prompt \u7f13\u5b58\u72b6\u6001\u3002',
    saveBtn: '\u4fdd\u5b58 Hub LLM',
    paneTitle: 'Hub LLM',
    paneDesc: '\u914d\u7f6e\u5185\u7f6e LLM \u7f51\u5173\u548c\u672c\u5730 prompt \u7f13\u5b58\u3002',
    enabledLabel: '\u542f\u7528 Hub LLM \u8def\u7531',
    smartRouteSingleLabel: '\u4ec5\u5355\u8bbe\u5907\u667a\u80fd\u8def\u7531',
    apiUrlLabel: 'API URL',
    apiKeyLabel: 'API Key',
    modelLabel: '\u6a21\u578b',
    protocolLabel: '\u534f\u8bae',
    guideTitle: '\u6307\u5f15',
    guideContent: '\u8fd9\u91cc\u7528\u4e8e\u914d\u7f6e Hub LLM \u4e0a\u6e38\u7aef\u70b9\u3002\u672c\u5730 prompt \u7f13\u5b58\u53ea\u5bf9\u975e stream \u4e14\u6388\u6743\u6a21\u578b\u4e0e\u8bf7\u6c42\u4f53\u5b8c\u5168\u4e00\u81f4\u7684\u8bf7\u6c42\u751f\u6548\u3002',
    cacheEntriesTitle: '\u6700\u8fd1\u7f13\u5b58\u6761\u76ee',
    cacheEntriesDesc: '\u67e5\u770b\u6700\u8fd1\u5199\u5165\u7684 prompt \u7f13\u5b58\u54cd\u5e94\uff0c\u53ef\u6309 provider/model \u7b5b\u9009\u3002',
    cacheEntriesEmpty: '\u5f53\u524d\u7b5b\u9009\u6761\u4ef6\u4e0b\u6ca1\u6709\u5339\u914d\u7684\u7f13\u5b58\u6761\u76ee\u3002',
    cacheEntriesLoadFailed: '\u52a0\u8f7d\u7f13\u5b58\u6761\u76ee\u5931\u8d25\uff1a{error}',
    cacheEntryMeta: 'Provider {provider} | {size} | \u547d\u4e2d {hits}',
    cacheEntriesProvider: 'Provider \u7b5b\u9009',
    cacheEntriesModel: '\u6a21\u578b\u7b5b\u9009',
    cacheEntriesRefresh: '\u5237\u65b0',
    cacheEntryCachedTokens: '\u7f13\u5b58 token {count}',
    cacheEntryLastAccess: '\u6700\u540e\u8bbf\u95ee\uff1a{value}',
    cacheEntryExpires: '\u8fc7\u671f\u65f6\u95f4\uff1a{value}',
    cacheEntryDetails: '\u8be6\u60c5',
    cacheEntryDelete: '\u5220\u9664',
    cacheEntryDeleted: '\u7f13\u5b58\u6761\u76ee\u5df2\u5220\u9664\u3002',
    cacheEntryDeleteFailed: '\u5220\u9664\u7f13\u5b58\u6761\u76ee\u5931\u8d25\uff1a{error}',
    cacheEntryDeleteConfirm: '\u786e\u5b9a\u5220\u9664\u8fd9\u6761\u7f13\u5b58\u8bb0\u5f55\uff1f',
    cacheEntryDetailTitle: '\u7f13\u5b58\u6761\u76ee\u8be6\u60c5',
    cacheEntryDetailDesc: '\u67e5\u770b\u8fd9\u6761\u7f13\u5b58\u7684\u89c4\u8303\u5316\u8bf7\u6c42\u4e0e\u8def\u7531\u56e0\u7d20\u3002',
    cacheEntryDetailEmpty: '\u8bf7\u5148\u9009\u62e9\u4e00\u6761\u7f13\u5b58\u8bb0\u5f55\uff0c\u67e5\u770b\u5b83\u7684\u89c4\u8303\u5316\u8bf7\u6c42\u4e0e\u547d\u4e2d\u4e0a\u4e0b\u6587\u3002',
    cacheEntryDetailLoadFailed: '\u52a0\u8f7d\u7f13\u5b58\u6761\u76ee\u8be6\u60c5\u5931\u8d25\uff1a{error}',
    cacheEntryDetailKey: '\u7f13\u5b58 Key',
    cacheEntryDetailAuthorizedModel: '\u6388\u6743\u6a21\u578b',
    cacheEntryDetailRequestedModel: '\u8bf7\u6c42\u6a21\u578b',
    cacheEntryDetailProviders: '\u6392\u5e8f\u540e provider',
    cacheEntryDetailNormalizedRequest: '\u89c4\u8303\u5316\u8bf7\u6c42',
    cacheEntriesPage: '\u7b2c {page} \u9875',
    cacheEntriesPagerMeta: '\u5df2\u663e\u793a {shown} / {total}',
    cacheEntriesPrev: '\u4e0a\u4e00\u9875',
    cacheEntriesNext: '\u4e0b\u4e00\u9875',
    cacheEntriesAllProviders: '\u5168\u90e8 provider',
    cacheEntriesAllModels: '\u5168\u90e8\u6a21\u578b',
    cacheHitSplitTitle: '\u547d\u4e2d\u6765\u6e90',
    cacheHitSplitEmpty: '\u6709\u7f13\u5b58\u6d41\u91cf\u540e\u8fd9\u91cc\u4f1a\u663e\u793a\u5185\u5b58\u548c\u78c1\u76d8\u547d\u4e2d\u62c6\u5206\u3002',
    cacheHitMemory: '\u5185\u5b58\u547d\u4e2d',
    cacheHitDisk: '\u78c1\u76d8\u547d\u4e2d',
    cacheHitShare: '{label} {count} ({pct}%)',
    cacheRuntimeTitle: '\u7f13\u5b58\u8fd0\u884c\u4fe1\u53f7',
    cacheRuntimeEmpty: '\u6709\u8bf7\u6c42\u540e\u8fd9\u91cc\u4f1a\u663e\u793a\u7f13\u5b58\u547d\u4e2d\u4e0e\u7ed5\u5f00\u539f\u56e0\u3002',
    cacheRuntimeCacheable: '\u53ef\u7f13\u5b58\u8bf7\u6c42\uff1a{count}',
    cacheRuntimeSingleflight: 'Singleflight \u5171\u4eab\u7b49\u5f85\uff1a{shared} | \u8282\u7701\u4e0a\u6e38\u8c03\u7528\uff1a{saved}',
    cacheRuntimeBypassTitle: '\u7ed5\u5f00\u7f13\u5b58\u539f\u56e0',
    cacheRuntimeBypassItem: '{reason}\uff1a{count}'
  }
};
const hli = (key, vars = {}) => ((HUB_LLM_I18N[currentLang] || HUB_LLM_I18N.en)[key] || HUB_LLM_I18N.en[key] || key).replace(/\{(\w+)\}/g, (_, name) => vars[name] ?? '');
let hubLlmLastStatus = null;
let hubLlmCacheEntriesFilters = { provider: '', model: '' };
let hubLlmCacheEntriesPage = 1;
let hubLlmSelectedCacheEntryKey = '';
const HUB_LLM_CACHE_ENTRIES_LIMIT = 6;

function formatHubBytes(bytes) {
  const n = Number(bytes || 0);
  if (n >= 1073741824) return (n / 1073741824).toFixed(1).replace(/\.0$/, '') + ' GB';
  if (n >= 1048576) return (n / 1048576).toFixed(1).replace(/\.0$/, '') + ' MB';
  if (n >= 1024) return (n / 1024).toFixed(1).replace(/\.0$/, '') + ' KB';
  return String(Math.max(0, Math.round(n))) + ' B';
}

function bytesToMB(bytes) {
  const n = Number(bytes || 0);
  return Math.max(1, Math.round(n / 1048576));
}

function mbToBytes(value) {
  const n = Number(value || 0);
  return Math.max(1, Math.round(n)) * 1048576;
}

function percentOf(used, limit) {
  const a = Number(used || 0);
  const b = Number(limit || 0);
  if (b <= 0) return '0';
  return (Math.max(0, (a / b) * 100)).toFixed(1).replace(/\.0$/, '');
}

function escapeHubHtml(text) {
  return String(text == null ? '' : text)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

function readHubLlmCacheEntryFilters() {
  const providerInput = document.getElementById('hubLlmCacheEntriesProvider');
  const modelInput = document.getElementById('hubLlmCacheEntriesModel');
  hubLlmCacheEntriesFilters = {
    provider: providerInput ? providerInput.value.trim() : '',
    model: modelInput ? modelInput.value.trim() : ''
  };
  return hubLlmCacheEntriesFilters;
}

function renderHubLlmCacheEntryFilters(data) {
  const providerSelect = document.getElementById('hubLlmCacheEntriesProvider');
  const modelSelect = document.getElementById('hubLlmCacheEntriesModel');
  if (!providerSelect || !modelSelect) return;
  const providers = Array.isArray(data && data.providers) ? data.providers : [];
  const models = Array.isArray(data && data.models) ? data.models : [];
  const providerValue = hubLlmCacheEntriesFilters.provider || '';
  const modelValue = hubLlmCacheEntriesFilters.model || '';
  providerSelect.innerHTML = '<option value="">' + escapeHubHtml(hli('cacheEntriesAllProviders')) + '</option>' + providers.map(function(value) {
    return '<option value="' + escapeHubHtml(value) + '"' + (value === providerValue ? ' selected' : '') + '>' + escapeHubHtml(value) + '</option>';
  }).join('');
  modelSelect.innerHTML = '<option value="">' + escapeHubHtml(hli('cacheEntriesAllModels')) + '</option>' + models.map(function(value) {
    return '<option value="' + escapeHubHtml(value) + '"' + (value === modelValue ? ' selected' : '') + '>' + escapeHubHtml(value) + '</option>';
  }).join('');
}

function renderHubLlmCacheEntries(entries) {
  const root = document.getElementById('hubLlmCacheEntries');
  if (!root) return;
  const list = Array.isArray(entries) ? entries : [];
  if (!list.length) {
    root.innerHTML = '<div class="hint">' + escapeHubHtml(hli('cacheEntriesEmpty')) + '</div>';
    return;
  }
  root.innerHTML = list.map(function(item) {
    const title = escapeHubHtml((item.model || '-') + ' | ' + (item.kind || '-'));
    const meta = escapeHubHtml(hli('cacheEntryMeta', { provider: item.provider_id || '-', size: formatHubBytes(item.payload_bytes || 0), hits: String(item.hit_count || 0) }));
    const accessed = escapeHubHtml(hli('cacheEntryLastAccess', { value: item.accessed_at || '-' }));
    const expires = escapeHubHtml(hli('cacheEntryExpires', { value: item.expires_at || '-' }));
    const key = escapeHubHtml(item.cache_key || '-');
    const cached = escapeHubHtml(hli('cacheEntryCachedTokens', { count: String(item.cached_input_tokens || 0) }));
    const detailsBtn = '<button class="btn-secondary" type="button" style="height:28px;padding:0 10px" onclick="viewHubLlmPromptCacheEntry(' + "'" + encodeURIComponent(item.cache_key || '') + "'" + ')">' + escapeHubHtml(hli('cacheEntryDetails')) + '</button>';
    const deleteBtn = '<button class="btn-danger" type="button" style="height:28px;padding:0 10px" onclick="deleteHubLlmPromptCacheEntry(' + "'" + encodeURIComponent(item.cache_key || '') + "'" + ')">' + escapeHubHtml(hli('cacheEntryDelete')) + '</button>';
    return '<div class="item" style="margin-top:8px;padding:12px 14px"><div class="item-head"><div><div class="item-title">' + title + '</div><div class="item-meta">' + meta + '</div></div><div style="display:flex;gap:6px;align-items:center;flex-wrap:wrap"><span class="badge info">' + cached + '</span>' + detailsBtn + deleteBtn + '</div></div><div class="item-meta mono" style="margin-top:8px">' + key + '</div><div class="grid2" style="margin-top:10px"><div class="item-meta">' + accessed + '</div><div class="item-meta">' + expires + '</div></div></div>';
  }).join('');
}

function renderHubLlmCacheEntryDetail(data, errorMessage) {
  const root = document.getElementById('hubLlmCacheEntryDetail');
  const title = document.getElementById('hubLlmCacheEntryDetailTitle');
  const desc = document.getElementById('hubLlmCacheEntryDetailDesc');
  if (!root || !title || !desc) return;
  title.textContent = hli('cacheEntryDetailTitle');
  desc.textContent = hli('cacheEntryDetailDesc');
  if (errorMessage) {
    root.innerHTML = '<div class="hint">' + escapeHubHtml(errorMessage) + '</div>';
    return;
  }
  if (!data) {
    root.innerHTML = '<div class="hint">' + escapeHubHtml(hli('cacheEntryDetailEmpty')) + '</div>';
    return;
  }
  const providers = Array.isArray(data.ordered_providers) && data.ordered_providers.length ? data.ordered_providers.join(', ') : '-';
  const normalized = data.normalized_request == null ? '{}' : JSON.stringify(data.normalized_request, null, 2);
  const meta = [
    '<div class="item-meta"><strong>' + escapeHubHtml(hli('cacheEntryDetailKey')) + ':</strong> <span class="mono">' + escapeHubHtml(data.cache_key || '-') + '</span></div>',
    '<div class="item-meta"><strong>' + escapeHubHtml(hli('cacheEntryDetailAuthorizedModel')) + ':</strong> ' + escapeHubHtml(data.authorized_model || '-') + '</div>',
    '<div class="item-meta"><strong>' + escapeHubHtml(hli('cacheEntryDetailRequestedModel')) + ':</strong> ' + escapeHubHtml(data.requested_model || '-') + '</div>',
    '<div class="item-meta"><strong>' + escapeHubHtml(hli('cacheEntryDetailProviders')) + ':</strong> ' + escapeHubHtml(providers) + '</div>'
  ].join('');
  root.innerHTML = '' +
    '<div class="item" style="min-height:auto;padding:12px 14px">' +
      '<div class="item-title" style="font-size:14px">' + escapeHubHtml((data.model || '-') + ' | ' + (data.kind || '-')) + '</div>' +
      '<div style="margin-top:8px;display:grid;gap:6px">' + meta + '</div>' +
      '<div class="item-meta" style="margin-top:10px"><strong>' + escapeHubHtml(hli('cacheEntryDetailNormalizedRequest')) + ':</strong></div>' +
      '<pre class="mono" style="margin-top:8px;white-space:pre-wrap;word-break:break-word;background:rgba(31,34,48,.04);border:1px solid var(--line);border-radius:10px;padding:12px;max-height:320px;overflow:auto">' + escapeHubHtml(normalized) + '</pre>' +
    '</div>';
}

async function loadHubLlmPromptCacheEntries() {
  try {
    const filters = readHubLlmCacheEntryFilters();
    const params = new URLSearchParams({ limit: String(HUB_LLM_CACHE_ENTRIES_LIMIT), page: String(hubLlmCacheEntriesPage) });
    if (filters.provider) params.set('provider', filters.provider);
    if (filters.model) params.set('model', filters.model);
    const data = await api('/api/admin/hub_llm_prompt_cache_entries?' + params.toString());
    hubLlmCacheEntriesPage = Number(data && data.page || hubLlmCacheEntriesPage || 1);
    renderHubLlmCacheEntryFilters(data || {});
    renderHubLlmCacheEntries(data && data.entries || []);
    renderHubLlmCacheEntriesPager(data || {});
  } catch (err) {
    const root = document.getElementById('hubLlmCacheEntries');
    if (root) root.innerHTML = '<div class="hint">' + escapeHubHtml(hli('cacheEntriesLoadFailed', { error: err.message })) + '</div>';
    renderHubLlmCacheEntryFilters({ providers: [], models: [] });
    renderHubLlmCacheEntriesPager({ entries: [], total: 0, page: 1, has_more: false });
  }
}

function renderHubLlmCacheEntriesPager(data) {
  const pager = document.getElementById('hubLlmCacheEntriesPager');
  const meta = document.getElementById('hubLlmCacheEntriesPagerMeta');
  const prev = document.getElementById('hubLlmCacheEntriesPrevBtn');
  const next = document.getElementById('hubLlmCacheEntriesNextBtn');
  if (!pager || !meta || !prev || !next) return;
  const total = Number(data && data.total || 0);
  const page = Number(data && data.page || hubLlmCacheEntriesPage || 1);
  const shown = Array.isArray(data && data.entries) ? data.entries.length : 0;
  const hasMore = !!(data && data.has_more);
  if (total <= 0 && page <= 1) {
    pager.classList.add('hidden');
    meta.textContent = '';
    prev.disabled = true;
    next.disabled = true;
    return;
  }
  pager.classList.remove('hidden');
  meta.textContent = hli('cacheEntriesPage', { page: String(page) }) + ' | ' + hli('cacheEntriesPagerMeta', { shown: String(shown), total: String(total) });
  prev.textContent = hli('cacheEntriesPrev');
  next.textContent = hli('cacheEntriesNext');
  prev.disabled = page <= 1;
  next.disabled = !hasMore;
}

async function refreshHubLlmPromptCacheEntries() {
  hubLlmCacheEntriesPage = 1;
  await loadHubLlmPromptCacheEntries();
}

async function refreshHubLlmPromptCache() {
  await Promise.all([
    loadHubLlmStatus(),
    loadHubLlmPromptCacheConfig(),
    refreshHubLlmPromptCacheEntries()
  ]);
}

async function changeHubLlmCacheEntriesPage(delta) {
  const nextPage = Math.max(1, hubLlmCacheEntriesPage + Number(delta || 0));
  if (nextPage === hubLlmCacheEntriesPage) return;
  hubLlmCacheEntriesPage = nextPage;
  await loadHubLlmPromptCacheEntries();
}

async function viewHubLlmPromptCacheEntry(cacheKey) {
  const key = decodeURIComponent(String(cacheKey || ''));
  if (!key) return;
  hubLlmSelectedCacheEntryKey = key;
  renderHubLlmCacheEntryDetail(null, null);
  try {
    const data = await api('/api/admin/hub_llm_prompt_cache_entry?cache_key=' + encodeURIComponent(key));
    if (hubLlmSelectedCacheEntryKey !== key) return;
    renderHubLlmCacheEntryDetail(data || {}, null);
  } catch (err) {
    if (hubLlmSelectedCacheEntryKey !== key) return;
    renderHubLlmCacheEntryDetail(null, hli('cacheEntryDetailLoadFailed', { error: err.message }));
  }
}

async function deleteHubLlmPromptCacheEntry(cacheKey) {
  const key = decodeURIComponent(String(cacheKey || ''));
  if (!key) return;
  if (typeof window !== 'undefined' && typeof window.confirm === 'function' && !window.confirm(hli('cacheEntryDeleteConfirm'))) {
    return;
  }
  try {
    await api('/api/admin/hub_llm_prompt_cache_entry?cache_key=' + encodeURIComponent(key), { method: 'DELETE' });
    const msg = hli('cacheEntryDeleted');
    setOutput(msg);
    showToast(msg, 'success');
    if (hubLlmSelectedCacheEntryKey === key) {
      hubLlmSelectedCacheEntryKey = '';
      renderHubLlmCacheEntryDetail(null, null);
    }
    await Promise.all([loadHubLlmStatus(), loadHubLlmPromptCacheEntries()]);
  } catch (err) {
    const msg = hli('cacheEntryDeleteFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
  }
}

function hubLlmStatusMeta(status) {
  const map = {
    healthy: ['statusHealthy', 'ok'],
    half_open: ['statusHalfOpen', 'warn'],
    open: ['statusOpen', 'danger'],
    not_configured: ['statusNone', 'info']
  };
  const entry = map[status];
  return entry ? [hli(entry[0]), entry[1]] : [hli('statusUnknown', { status }), 'info'];
}


function renderHubLlmCacheHitBreakdown(data) {
  const root = document.getElementById('hubLlmCacheHitBreakdown');
  const title = document.getElementById('hubLlmCacheHitBreakdownTitle');
  if (!root || !title) return;
  title.textContent = hli('cacheHitSplitTitle');
  const storage = data && data.prompt_cache && data.prompt_cache.local_storage || {};
  const memoryHits = Number(storage.memory_hits || 0);
  const diskHits = Number(storage.disk_hits || 0);
  const totalHits = memoryHits + diskHits;
  if (totalHits <= 0) {
    root.innerHTML = '<div class="hint">' + escapeHubHtml(hli('cacheHitSplitEmpty')) + '</div>';
    return;
  }
  const memoryPct = totalHits > 0 ? ((memoryHits / totalHits) * 100).toFixed(1).replace(/\.0$/, '') : '0';
  const diskPct = totalHits > 0 ? ((diskHits / totalHits) * 100).toFixed(1).replace(/\.0$/, '') : '0';
  root.innerHTML = '' +
    '<div class="grid2" style="margin-top:10px">' +
      '<div class="item" style="min-height:auto;padding:12px 14px">' +
        '<div class="item-title" style="font-size:14px">' + escapeHubHtml(hli('cacheHitMemory')) + '</div>' +
        '<div class="item-meta">' + escapeHubHtml(hli('cacheHitShare', { label: hli('cacheHitMemory'), count: String(memoryHits), pct: memoryPct })) + '</div>' +
      '</div>' +
      '<div class="item" style="min-height:auto;padding:12px 14px">' +
        '<div class="item-title" style="font-size:14px">' + escapeHubHtml(hli('cacheHitDisk')) + '</div>' +
        '<div class="item-meta">' + escapeHubHtml(hli('cacheHitShare', { label: hli('cacheHitDisk'), count: String(diskHits), pct: diskPct })) + '</div>' +
      '</div>' +
    '</div>' +
    '<div style="margin-top:10px;border-radius:999px;overflow:hidden;height:12px;background:rgba(31,34,48,.08)">' +
      '<div style="display:flex;height:100%">' +
        '<div style="width:' + escapeHubHtml(memoryPct) + '%;background:linear-gradient(135deg,#57c59a,#23a26d)"></div>' +
        '<div style="width:' + escapeHubHtml(diskPct) + '%;background:linear-gradient(135deg,#ef6a7c,#e8566a)"></div>' +
      '</div>' +
    '</div>';
}
function renderHubLlmCacheRuntime(data) {
  const root = document.getElementById('hubLlmCacheRuntime');
  const title = document.getElementById('hubLlmCacheRuntimeTitle');
  if (!root || !title) return;
  title.textContent = hli('cacheRuntimeTitle');
  const runtime = data && data.prompt_cache && data.prompt_cache.runtime || {};
  const cacheable = Number(runtime.cacheable_requests || 0);
  const shared = Number(runtime.singleflight_shared_hits || 0);
  const saved = Number(runtime.singleflight_saved_calls || 0);
  const reasons = runtime.bypass_reasons || {};
  const entries = Object.keys(reasons).sort(function(a, b) { return Number(reasons[b] || 0) - Number(reasons[a] || 0); });
  if (cacheable <= 0 && shared <= 0 && saved <= 0 && !entries.length) {
    root.innerHTML = '<div class="hint">' + escapeHubHtml(hli('cacheRuntimeEmpty')) + '</div>';
    return;
  }
  const cards = [
    '<div class="item" style="min-height:auto;padding:12px 14px"><div class="item-title" style="font-size:14px">' + escapeHubHtml(hli('cacheRuntimeCacheable', { count: String(cacheable) })) + '</div><div class="item-meta">' + escapeHubHtml(hli('cacheRuntimeSingleflight', { shared: String(shared), saved: String(saved) })) + '</div></div>'
  ];
  if (entries.length) {
    const items = entries.map(function(key) {
      return '<div class="item-meta">' + escapeHubHtml(hli('cacheRuntimeBypassItem', { reason: key, count: String(reasons[key] || 0) })) + '</div>';
    }).join('');
    cards.push('<div class="item" style="min-height:auto;padding:12px 14px"><div class="item-title" style="font-size:14px">' + escapeHubHtml(hli('cacheRuntimeBypassTitle')) + '</div><div style="margin-top:8px;display:grid;gap:4px">' + items + '</div></div>');
  }
  root.innerHTML = cards.join('');
}

function renderHubLlmCacheSummary(data) {
  const summary = document.getElementById('hubLlmCacheSummary');
  if (!summary) return;
  if (!data || !data.prompt_cache) {
    summary.textContent = hli('cacheSummaryEmpty');
    return;
  }
  const cache = data.prompt_cache || {};
  const storage = cache.local_storage || {};
  const cfg = cache.config || {};
  const memoryLimit = storage.memory_max_bytes || cfg.memory_max_bytes || 0;
  const diskLimit = cfg.disk_max_bytes || 0;
  summary.textContent = hli('cacheSummary', {
    rate: Number(cache.cache_rate || 0).toFixed(1).replace(/\.0$/, ''),
    hits: String(cache.cached_requests || 0),
    requests: String(cache.requests || 0),
    reuse: Number(cache.cache_reuse_rate || 0).toFixed(1).replace(/\.0$/, ''),
    memUsed: formatHubBytes(storage.memory_bytes || 0),
    memLimit: formatHubBytes(memoryLimit),
    memPct: percentOf(storage.memory_bytes || 0, memoryLimit),
    diskUsed: formatHubBytes(storage.disk_bytes || 0),
    diskLimit: formatHubBytes(diskLimit),
    diskPct: percentOf(storage.disk_bytes || 0, diskLimit),
    memHits: String(storage.memory_hits || 0),
    diskHits: String(storage.disk_hits || 0)
  });
}

function renderHubLlmStatus(data) {
  const badge = document.getElementById('hubLlmStatusBadge');
  if (!badge) return;
  const status = (data && data.status) || badge.dataset.statusKey || 'not_configured';
  const [baseText, cls] = hubLlmStatusMeta(status);
  const cache = (data && data.prompt_cache) || {};
  const rate = Number(cache.cache_rate || 0).toFixed(1).replace(/\.0$/, '');
  const cacheText = hli('cacheRate', { rate });
  const storage = cache.local_storage || {};
  const diskText = hli('cacheDisk', { bytes: formatHubBytes(storage.disk_bytes || 0) });
  badge.dataset.statusKey = status;
  badge.textContent = baseText + ' | ' + cacheText;
  badge.title = cacheText + ' (' + String(cache.cached_requests || 0) + '/' + String(cache.requests || 0) + ') | ' + diskText + ' | ' + hli('cacheMemory', { bytes: formatHubBytes(storage.memory_bytes || 0) });
  badge.className = 'badge ' + cls;
  renderHubLlmCacheSummary(data);
  renderHubLlmCacheHitBreakdown(data);
  renderHubLlmCacheRuntime(data);
}

function fillHubLlmPromptCacheConfig(data) {
  document.getElementById('hubLlmPromptCacheEnabled').checked = !!data.enabled;
  document.getElementById('hubLlmPromptCacheTTL').value = Number(data.ttl_seconds || 1800);
  document.getElementById('hubLlmPromptCacheMemoryEntries').value = Number(data.memory_max_entries || 256);
  document.getElementById('hubLlmPromptCacheMemoryMB').value = bytesToMB(data.memory_max_bytes || (8 << 20));
  document.getElementById('hubLlmPromptCacheDiskMB').value = bytesToMB(data.disk_max_bytes || (64 << 20));
  document.getElementById('hubLlmPromptCacheNormalizeDeterministic').checked = data.normalize_deterministic_params !== false;
  document.getElementById('hubLlmPromptCacheIgnoreModel').checked = data.ignore_model_field !== false;
  document.getElementById('hubLlmPromptCacheIgnoreUser').checked = data.ignore_user_field !== false;
  document.getElementById('hubLlmPromptCacheIgnoreMetadata').checked = data.ignore_metadata_field !== false;
  document.getElementById('hubLlmPromptCacheSingleflightTimeoutMS').value = Number(data.singleflight_wait_timeout_ms || 15000);
}

function applyHubLlmRuntimeI18n() {
  const setText = (id, key) => { const el = document.getElementById(id); if (el) el.textContent = hli(key); };
  setText('hubLlmPaneTitle', 'paneTitle');
  setText('hubLlmPaneDesc', 'paneDesc');
  setText('hubLlmEnabledLabel', 'enabledLabel');
  setText('hubLlmSmartRouteSingleLabel', 'smartRouteSingleLabel');
  setText('hubLlmApiUrlLabel', 'apiUrlLabel');
  setText('hubLlmApiKeyLabel', 'apiKeyLabel');
  setText('hubLlmModelLabel', 'modelLabel');
  setText('hubLlmProtocolLabel', 'protocolLabel');
  const openai = document.getElementById('hubLlmProtocolOpenAI');
  if (openai) openai.textContent = 'OpenAI';
  const anthropic = document.getElementById('hubLlmProtocolAnthropic');
  if (anthropic) anthropic.textContent = 'Anthropic';
  setText('hubLlmSaveBtn', 'saveBtn');
  setText('hubLlmCacheConfigTitle', 'cacheConfigTitle');
  setText('hubLlmCacheConfigDesc', 'cacheConfigDesc');
  setText('hubLlmPromptCacheEnabledLabel', 'cacheEnabled');
  setText('hubLlmPromptCacheTTLLabel', 'cacheTTL');
  setText('hubLlmPromptCacheMemoryEntriesLabel', 'cacheMemoryEntries');
  setText('hubLlmPromptCacheMemoryMBLabel', 'cacheMemoryMB');
  setText('hubLlmPromptCacheDiskMBLabel', 'cacheDiskMB');
  setText('hubLlmPromptCacheNormalizeDeterministicLabel', 'cacheNormalizeDeterministic');
  setText('hubLlmPromptCacheIgnoreModelLabel', 'cacheIgnoreModel');
  setText('hubLlmPromptCacheIgnoreUserLabel', 'cacheIgnoreUser');
  setText('hubLlmPromptCacheIgnoreMetadataLabel', 'cacheIgnoreMetadata');
  setText('hubLlmPromptCacheSingleflightTimeoutMSLabel', 'cacheSingleflightTimeout');
  setText('hubLlmPromptCacheSaveBtn', 'cacheSave');
  setText('hubLlmPromptCacheRefreshBtn', 'cacheRefresh');
  setText('hubLlmPromptCacheClearBtn', 'cacheClear');
  setText('hubLlmCacheHitBreakdownTitle', 'cacheHitSplitTitle');
  setText('hubLlmCacheRuntimeTitle', 'cacheRuntimeTitle');
  setText('hubLlmCacheEntriesTitle', 'cacheEntriesTitle');
  setText('hubLlmCacheEntriesDesc', 'cacheEntriesDesc');
  setText('hubLlmCacheEntryDetailTitle', 'cacheEntryDetailTitle');
  setText('hubLlmCacheEntryDetailDesc', 'cacheEntryDetailDesc');
  setText('hubLlmCacheEntriesProviderLabel', 'cacheEntriesProvider');
  setText('hubLlmCacheEntriesModelLabel', 'cacheEntriesModel');
  setText('hubLlmCacheEntriesRefreshBtn', 'cacheEntriesRefresh');
  setText('hubLlmGuideTitle', 'guideTitle');
  const guide = document.getElementById('hubLlmGuideContent');
  if (guide) guide.textContent = hli('guideContent');
  const btn = document.getElementById('hubLlmTestBtn');
  if (btn && !btn.disabled) btn.textContent = hli('testBtn');
  const keyInput = document.getElementById('hubLlmApiKey');
  if (keyInput) {
    const hasApiKey = keyInput.dataset.hasApiKey === '1';
    keyInput.placeholder = hasApiKey ? hli('apiKeyConfigured') : hli('apiKeyEnter');
  }
  renderHubLlmStatus(hubLlmLastStatus);
  if (!hubLlmSelectedCacheEntryKey) renderHubLlmCacheEntryDetail(null, null);
}

async function loadHubLlmPromptCacheConfig() {
  try {
    const data = await api('/api/admin/hub_llm_prompt_cache_config');
    fillHubLlmPromptCacheConfig(data || {});
  } catch (err) {
    const msg = hli('cacheLoadFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
  }
}

async function loadHubLlmConfig() {
  try {
    const data = await api('/api/admin/hub_llm_config');
    document.getElementById('hubLlmEnabled').checked = !!data.enabled;
    document.getElementById('hubLlmSmartRouteSingle').checked = !!data.smart_route_single_device;
    document.getElementById('hubLlmApiUrl').value = data.api_url || '';
    const keyInput = document.getElementById('hubLlmApiKey');
    keyInput.value = '';
    keyInput.dataset.hasApiKey = data.has_api_key ? '1' : '0';
    keyInput.placeholder = data.has_api_key ? hli('apiKeyConfigured') : hli('apiKeyEnter');
    document.getElementById('hubLlmModel').value = data.model || '';
    document.getElementById('hubLlmProtocol').value = data.protocol || 'openai';
    await loadHubLlmPromptCacheConfig();
    await loadHubLlmPromptCacheEntries();
  } catch (err) {
    const msg = hli('loadFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
  }
}

async function saveHubLlmConfig() {
  try {
    const payload = {
      enabled: document.getElementById('hubLlmEnabled').checked,
      smart_route_single_device: document.getElementById('hubLlmSmartRouteSingle').checked,
      api_url: document.getElementById('hubLlmApiUrl').value.trim(),
      api_key: document.getElementById('hubLlmApiKey').value,
      model: document.getElementById('hubLlmModel').value.trim(),
      protocol: document.getElementById('hubLlmProtocol').value
    };
    const data = await api('/api/admin/hub_llm_config', { method: 'PUT', body: JSON.stringify(payload) });
    document.getElementById('hubLlmEnabled').checked = !!data.enabled;
    document.getElementById('hubLlmApiUrl').value = data.api_url || '';
    const keyInput = document.getElementById('hubLlmApiKey');
    keyInput.value = '';
    keyInput.dataset.hasApiKey = data.has_api_key ? '1' : '0';
    keyInput.placeholder = data.has_api_key ? hli('apiKeyConfigured') : hli('apiKeyEnter');
    document.getElementById('hubLlmModel').value = data.model || '';
    document.getElementById('hubLlmProtocol').value = data.protocol || 'openai';
    const msg = hli('saved');
    setOutput(msg);
    showToast(msg, 'success');
    await Promise.all([loadHubLlmStatus(), loadHubLlmPromptCacheEntries()]);
  } catch (err) {
    const msg = hli('saveFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
  }
}

async function saveHubLlmPromptCacheConfig() {
  try {
    const payload = {
      enabled: document.getElementById('hubLlmPromptCacheEnabled').checked,
      ttl_seconds: Math.max(1, Number(document.getElementById('hubLlmPromptCacheTTL').value || 0)),
      memory_max_entries: Math.max(1, Number(document.getElementById('hubLlmPromptCacheMemoryEntries').value || 0)),
      memory_max_bytes: mbToBytes(document.getElementById('hubLlmPromptCacheMemoryMB').value),
      disk_max_bytes: mbToBytes(document.getElementById('hubLlmPromptCacheDiskMB').value),
      normalize_deterministic_params: document.getElementById('hubLlmPromptCacheNormalizeDeterministic').checked,
      ignore_model_field: document.getElementById('hubLlmPromptCacheIgnoreModel').checked,
      ignore_user_field: document.getElementById('hubLlmPromptCacheIgnoreUser').checked,
      ignore_metadata_field: document.getElementById('hubLlmPromptCacheIgnoreMetadata').checked,
      singleflight_wait_timeout_ms: Math.max(1, Number(document.getElementById('hubLlmPromptCacheSingleflightTimeoutMS').value || 0))
    };
    const data = await api('/api/admin/hub_llm_prompt_cache_config', { method: 'PUT', body: JSON.stringify(payload) });
    fillHubLlmPromptCacheConfig(data || {});
    const msg = hli('cacheSaved');
    setOutput(msg);
    showToast(msg, 'success');
    await Promise.all([loadHubLlmStatus(), loadHubLlmPromptCacheEntries()]);
  } catch (err) {
    const msg = hli('cacheSaveFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
  }
}

async function clearHubLlmPromptCache() {
  try {
    const data = await api('/api/admin/hub_llm_prompt_cache_clear', { method: 'POST' });
    const msg = hli('cacheCleared', { count: data && typeof data.purged === 'number' ? data.purged : 0 });
    setOutput(msg);
    showToast(msg, 'success');
    await Promise.all([loadHubLlmStatus(), loadHubLlmPromptCacheEntries()]);
  } catch (err) {
    const msg = hli('cacheClearFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
  }
}

async function testHubLlm() {
  const btn = document.getElementById('hubLlmTestBtn');
  if (btn) {
    btn.disabled = true;
    btn.textContent = hli('testing');
  }
  try {
    const data = await api('/api/admin/hub_llm_test', { method: 'POST' });
    if (data.success) {
      const msg = hli('testOk', { ms: data.latency_ms, reply: data.reply || '' });
      setOutput(msg);
      showToast(msg, 'success');
    } else {
      const msg = hli('testFail', { error: data.error || hli('unknownError') });
      setOutput(msg);
      showToast(msg, 'error');
    }
  } catch (err) {
    const msg = hli('testError', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
  } finally {
    if (btn) {
      btn.disabled = false;
      btn.textContent = hli('testBtn');
    }
  }
}

async function loadHubLlmStatus() {
  try {
    hubLlmLastStatus = await api('/api/admin/hub_llm_status');
    renderHubLlmStatus(hubLlmLastStatus);
  } catch (_) {}
}

if (window.AdminTabRegistry && typeof window.AdminTabRegistry.onLanguageChange === 'function') {
  window.AdminTabRegistry.onLanguageChange(function() {
    applyHubLlmRuntimeI18n();
  });
}

applyHubLlmRuntimeI18n();








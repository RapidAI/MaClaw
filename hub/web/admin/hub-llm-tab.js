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
    cacheConfigTitle: 'Prompt Cache',
    cacheConfigDesc: 'Configure local prompt cache TTL and memory/disk budget.',
    cacheEnabled: 'Enable local cache',
    cacheTTL: 'TTL (seconds)',
    cacheMemoryEntries: 'Memory entries',
    cacheMemoryMB: 'Memory budget (MB)',
    cacheDiskMB: 'Disk budget (MB)',
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
    cacheEntryDelete: 'Delete',
    cacheEntryDeleted: 'Cache entry deleted.',
    cacheEntryDeleteFailed: 'Delete cache entry failed: {error}',
    cacheEntryDeleteConfirm: 'Delete this cache entry?',
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
    cacheHitShare: '{label} {count} ({pct}%)'
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
    cacheConfigTitle: 'Prompt \u7f13\u5b58',
    cacheConfigDesc: '\u914d\u7f6e\u672c\u5730 prompt \u7f13\u5b58\u7684 TTL \u548c\u5185\u5b58/\u78c1\u76d8\u989d\u5ea6\u3002',
    cacheEnabled: '\u542f\u7528\u672c\u5730\u7f13\u5b58',
    cacheTTL: 'TTL (\u79d2)',
    cacheMemoryEntries: '\u5185\u5b58\u6761\u76ee\u4e0a\u9650',
    cacheMemoryMB: '\u5185\u5b58\u9884\u7b97 (MB)',
    cacheDiskMB: '\u78c1\u76d8\u9884\u7b97 (MB)',
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
    cacheEntryDelete: '\u5220\u9664',
    cacheEntryDeleted: '\u7f13\u5b58\u6761\u76ee\u5df2\u5220\u9664\u3002',
    cacheEntryDeleteFailed: '\u5220\u9664\u7f13\u5b58\u6761\u76ee\u5931\u8d25\uff1a{error}',
    cacheEntryDeleteConfirm: '\u786e\u5b9a\u5220\u9664\u8fd9\u6761\u7f13\u5b58\u8bb0\u5f55\uff1f',
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
    cacheHitShare: '{label} {count} ({pct}%)'
  }
};
const hli = (key, vars = {}) => ((HUB_LLM_I18N[currentLang] || HUB_LLM_I18N.en)[key] || HUB_LLM_I18N.en[key] || key).replace(/\{(\w+)\}/g, (_, name) => vars[name] ?? '');
let hubLlmLastStatus = null;
let hubLlmCacheEntriesFilters = { provider: '', model: '' };
let hubLlmCacheEntriesPage = 1;
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
    const deleteBtn = '<button class="btn-danger" type="button" style="height:32px;padding:0 12px" onclick="deleteHubLlmPromptCacheEntry(' + "'" + encodeURIComponent(item.cache_key || '') + "'" + ')">' + escapeHubHtml(hli('cacheEntryDelete')) + '</button>';
    return '<div class="item" style="margin-top:8px;padding:14px 16px"><div class="item-head"><div><div class="item-title">' + title + '</div><div class="item-meta">' + meta + '</div></div><div style="display:flex;gap:8px;align-items:center;flex-wrap:wrap"><span class="badge info">' + cached + '</span>' + deleteBtn + '</div></div><div class="item-meta mono" style="margin-top:8px">' + key + '</div><div class="grid2" style="margin-top:10px"><div class="item-meta">' + accessed + '</div><div class="item-meta">' + expires + '</div></div></div>';
  }).join('');
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

async function changeHubLlmCacheEntriesPage(delta) {
  const nextPage = Math.max(1, hubLlmCacheEntriesPage + Number(delta || 0));
  if (nextPage === hubLlmCacheEntriesPage) return;
  hubLlmCacheEntriesPage = nextPage;
  await loadHubLlmPromptCacheEntries();
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
      '<div class="item" style="min-height:auto;padding:14px 16px">' +
        '<div class="item-title" style="font-size:14px">' + escapeHubHtml(hli('cacheHitMemory')) + '</div>' +
        '<div class="item-meta">' + escapeHubHtml(hli('cacheHitShare', { label: hli('cacheHitMemory'), count: String(memoryHits), pct: memoryPct })) + '</div>' +
      '</div>' +
      '<div class="item" style="min-height:auto;padding:14px 16px">' +
        '<div class="item-title" style="font-size:14px">' + escapeHubHtml(hli('cacheHitDisk')) + '</div>' +
        '<div class="item-meta">' + escapeHubHtml(hli('cacheHitShare', { label: hli('cacheHitDisk'), count: String(diskHits), pct: diskPct })) + '</div>' +
      '</div>' +
    '</div>' +
    '<div style="margin-top:12px;border-radius:999px;overflow:hidden;height:12px;background:rgba(31,34,48,.08)">' +
      '<div style="display:flex;height:100%">' +
        '<div style="width:' + escapeHubHtml(memoryPct) + '%;background:linear-gradient(135deg,#57c59a,#23a26d)"></div>' +
        '<div style="width:' + escapeHubHtml(diskPct) + '%;background:linear-gradient(135deg,#ef6a7c,#e8566a)"></div>' +
      '</div>' +
    '</div>';
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
  badge.title = cacheText + ' (' + String(cache.cached_requests || 0) + '/' + String(cache.requests || 0) + ') | ' + diskText + ' | memory ' + formatHubBytes(storage.memory_bytes || 0);
  badge.className = 'badge ' + cls;
  renderHubLlmCacheSummary(data);
  renderHubLlmCacheHitBreakdown(data);
}

function fillHubLlmPromptCacheConfig(data) {
  document.getElementById('hubLlmPromptCacheEnabled').checked = !!data.enabled;
  document.getElementById('hubLlmPromptCacheTTL').value = Number(data.ttl_seconds || 1800);
  document.getElementById('hubLlmPromptCacheMemoryEntries').value = Number(data.memory_max_entries || 256);
  document.getElementById('hubLlmPromptCacheMemoryMB').value = bytesToMB(data.memory_max_bytes || (8 << 20));
  document.getElementById('hubLlmPromptCacheDiskMB').value = bytesToMB(data.disk_max_bytes || (64 << 20));
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
  setText('hubLlmPromptCacheSaveBtn', 'cacheSave');
  setText('hubLlmPromptCacheClearBtn', 'cacheClear');
  setText('hubLlmCacheHitBreakdownTitle', 'cacheHitSplitTitle');
  setText('hubLlmCacheEntriesTitle', 'cacheEntriesTitle');
  setText('hubLlmCacheEntriesDesc', 'cacheEntriesDesc');
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
      disk_max_bytes: mbToBytes(document.getElementById('hubLlmPromptCacheDiskMB').value)
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








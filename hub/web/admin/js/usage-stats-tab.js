/*
 * Usage stats admin extension.
 * ASCII only. Chinese text must use \uXXXX escapes.
 */
const USAGE_STATS_I18N = {
  en: {
    navLabel: 'Usage Stats',
    navDesc: 'Daily and monthly LLM usage analytics',
    tabTitle: 'Usage Stats',
    tabSubtitle: 'View token usage by user or security group, with daily 24-hour trends.',
    reload: 'Reload',
    scope: 'Scope',
    scopeUser: 'By User',
    scopeGroup: 'By Security Group',
    period: 'Period',
    periodDaily: 'Daily',
    periodMonthly: 'Monthly',
    date: 'Date',
    month: 'Month',
    entity: 'Entity Filter',
    entityAll: 'All',
    summaryTokens: 'Total Tokens',
    summaryInput: 'Input Tokens',
    summaryOutput: 'Output Tokens',
    summaryRequests: 'Requests',
    summaryCredits: 'Credits',
    trendTitle: '24-Hour Trend',
    trendEmpty: 'No daily trend is available for the selected view.',
    rowsTitle: 'Usage Ranking',
    rowsEmpty: 'No usage data found for the current filter.',
    colName: 'Name',
    colTotal: 'Total',
    colInput: 'Input',
    colOutput: 'Output',
    colRequests: 'Requests',
    colCredits: 'Credits',
    loadFailed: 'Load usage stats failed: {error}',
    generatedAt: 'Generated at {time}'
  },
  zh: {
    navLabel: '\u4f7f\u7528\u7edf\u8ba1',
    navDesc: '\u6309\u65e5\u3001\u6309\u6708\u7684 LLM \u7528\u91cf\u62a5\u8868',
    tabTitle: '\u4f7f\u7528\u7edf\u8ba1',
    tabSubtitle: '\u67e5\u770b\u6309\u7528\u6237\u6216\u5b89\u5168\u7ec4\u7684 token \u7528\u91cf\uff0c\u5305\u542b\u6bcf\u65e5 24 \u5c0f\u65f6\u8d8b\u52bf\u3002',
    reload: '\u91cd\u65b0\u52a0\u8f7d',
    scope: '\u7edf\u8ba1\u7ef4\u5ea6',
    scopeUser: '\u6309\u7528\u6237',
    scopeGroup: '\u6309\u5b89\u5168\u7ec4',
    period: '\u5468\u671f',
    periodDaily: '\u6309\u65e5',
    periodMonthly: '\u6309\u6708',
    date: '\u65e5\u671f',
    month: '\u6708\u4efd',
    entity: '\u5bf9\u8c61\u7b5b\u9009',
    entityAll: '\u5168\u90e8',
    summaryTokens: '\u603b token',
    summaryInput: '\u8f93\u5165 token',
    summaryOutput: '\u8f93\u51fa token',
    summaryRequests: '\u8bf7\u6c42\u6570',
    summaryCredits: 'Credits',
    trendTitle: '24 \u5c0f\u65f6\u8d8b\u52bf',
    trendEmpty: '\u5f53\u524d\u7b5b\u9009\u4e0b\u65e0\u6bcf\u65e5\u8d8b\u52bf\u6570\u636e\u3002',
    rowsTitle: '\u7528\u91cf\u6392\u540d',
    rowsEmpty: '\u5f53\u524d\u7b5b\u9009\u4e0b\u6682\u65e0\u7528\u91cf\u6570\u636e\u3002',
    colName: '\u540d\u79f0',
    colTotal: '\u603b\u8ba1',
    colInput: '\u8f93\u5165',
    colOutput: '\u8f93\u51fa',
    colRequests: '\u8bf7\u6c42\u6570',
    colCredits: 'Credits',
    loadFailed: '\u52a0\u8f7d\u4f7f\u7528\u7edf\u8ba1\u5931\u8d25: {error}',
    generatedAt: '\u751f\u6210\u65f6\u95f4 {time}'
  }
};
const ust = (key, vars = {}) => ((USAGE_STATS_I18N[currentLang] || USAGE_STATS_I18N.en)[key] || USAGE_STATS_I18N.en[key] || key).replace(/\{(\w+)\}/g, (_, name) => vars[name] ?? '');
let usageStatsCache = null;
let usageStatsState = {
  scope: 'user',
  period: 'daily',
  date: '',
  month: '',
  entity: ''
};
function ensureUsageStatsDefaults() {
  const now = new Date();
  if (!usageStatsState.date) usageStatsState.date = now.toISOString().slice(0, 10);
  if (!usageStatsState.month) usageStatsState.month = now.toISOString().slice(0, 7);
}
function fmtInt(value) {
  return Number(value || 0).toLocaleString('en-US');
}
function fmtCredits(value) {
  const n = Number(value || 0);
  return Math.abs(n - Math.round(n)) < 0.000001 ? String(Math.round(n)) : n.toFixed(3).replace(/0+$/, '').replace(/\.$/, '');
}
function usageMetricCard(label, value) {
  return '<div class="metric"><label>' + escapeHtml(label) + '</label><strong>' + escapeHtml(value) + '</strong></div>';
}
function ensureUsageStatsUI() {
  if (document.getElementById('usageStatsRoot')) return;
  const tab = document.getElementById('tab-usagestats');
  if (!tab) return;
  const host = document.createElement('div');
  host.id = 'usageStatsRoot';
  host.innerHTML = '' +
    '<div class="item"><div class="grid2">' +
    '<div><label id="usageStatsScopeLabel"></label><select id="usageStatsScope" onchange="onUsageStatsFilterChange()"><option value="user" id="usageStatsScopeUser"></option><option value="group" id="usageStatsScopeGroup"></option></select></div>' +
    '<div><label id="usageStatsPeriodLabel"></label><select id="usageStatsPeriod" onchange="onUsageStatsFilterChange()"><option value="daily" id="usageStatsPeriodDaily"></option><option value="monthly" id="usageStatsPeriodMonthly"></option></select></div>' +
    '<div id="usageStatsDateWrap"><label id="usageStatsDateLabel"></label><input id="usageStatsDate" type="date" onchange="onUsageStatsFilterChange()"></div>' +
    '<div id="usageStatsMonthWrap"><label id="usageStatsMonthLabel"></label><input id="usageStatsMonth" type="month" onchange="onUsageStatsFilterChange()"></div>' +
    '<div style="grid-column:1 / -1"><label id="usageStatsEntityLabel"></label><select id="usageStatsEntity" onchange="onUsageStatsFilterChange()"></select></div>' +
    '</div><div id="usageStatsGeneratedAt" class="item-meta" style="margin-top:12px"></div></div>' +
    '<div id="usageStatsSummary" class="metrics" style="margin-top:18px"></div>' +
    '<div class="grid2" style="margin-top:18px">' +
    '<div class="item"><div class="item-title" id="usageStatsTrendTitle"></div><div id="usageStatsTrend" style="margin-top:12px"></div></div>' +
    '<div class="item"><div class="item-title" id="usageStatsRowsTitle"></div><div id="usageStatsRows" style="margin-top:12px"></div></div>' +
    '</div>';
  tab.appendChild(host);
}
function applyUsageStatsI18n() {
  if (typeof tabMeta === 'object') tabMeta.usagestats = ['usageStatsTabTitle', 'usageStatsTabSubtitle'];
  _s('navUsageStats', 'textContent', ust('navLabel'));
  _s('navUsageStatsDesc', 'textContent', ust('navDesc'));
  _s('usageStatsTabTitle', 'textContent', ust('tabTitle'));
  _s('usageStatsTabSubtitle', 'textContent', ust('tabSubtitle'));
  _s('usageStatsReloadBtn', 'textContent', ust('reload'));
  _s('usageStatsScopeLabel', 'textContent', ust('scope'));
  _s('usageStatsScopeUser', 'textContent', ust('scopeUser'));
  _s('usageStatsScopeGroup', 'textContent', ust('scopeGroup'));
  _s('usageStatsPeriodLabel', 'textContent', ust('period'));
  _s('usageStatsPeriodDaily', 'textContent', ust('periodDaily'));
  _s('usageStatsPeriodMonthly', 'textContent', ust('periodMonthly'));
  _s('usageStatsDateLabel', 'textContent', ust('date'));
  _s('usageStatsMonthLabel', 'textContent', ust('month'));
  _s('usageStatsEntityLabel', 'textContent', ust('entity'));
  _s('usageStatsTrendTitle', 'textContent', ust('trendTitle'));
  _s('usageStatsRowsTitle', 'textContent', ust('rowsTitle'));
  renderUsageStats();
}
function syncUsageStatsFiltersFromState() {
  ensureUsageStatsDefaults();
  _s('usageStatsScope', 'value', usageStatsState.scope);
  _s('usageStatsPeriod', 'value', usageStatsState.period);
  _s('usageStatsDate', 'value', usageStatsState.date);
  _s('usageStatsMonth', 'value', usageStatsState.month);
  const dateWrap = document.getElementById('usageStatsDateWrap');
  const monthWrap = document.getElementById('usageStatsMonthWrap');
  if (dateWrap) dateWrap.style.display = usageStatsState.period === 'daily' ? 'block' : 'none';
  if (monthWrap) monthWrap.style.display = usageStatsState.period === 'monthly' ? 'block' : 'none';
}
function buildUsageStatsEntityOptions() {
  const root = document.getElementById('usageStatsEntity');
  if (!root) return;
  const options = [];
  const first = usageStatsState.scope === 'group' ? (usageStatsCache && usageStatsCache.available_groups || []) : (usageStatsCache && usageStatsCache.entities || []);
  options.push('<option value="">' + escapeHtml(ust('entityAll')) + '</option>');
  first.forEach(function(item) {
    if (!item || !item.id) return;
    options.push('<option value="' + escapeHtml(item.id) + '"' + (item.id === usageStatsState.entity ? ' selected' : '') + '>' + escapeHtml(item.name || item.id) + '</option>');
  });
  root.innerHTML = options.join('');
}
function renderUsageTrend() {
  const root = document.getElementById('usageStatsTrend');
  if (!root) return;
  const items = usageStatsCache && usageStatsCache.trend || [];
  if (usageStatsState.period !== 'daily' || !items.length) {
    root.innerHTML = '<div class="hint">' + ust('trendEmpty') + '</div>';
    return;
  }
  let max = 0;
  items.forEach(function(item) { max = Math.max(max, Number(item && item.total_tokens || 0)); });
  if (!max) {
    root.innerHTML = '<div class="hint">' + ust('trendEmpty') + '</div>';
    return;
  }
  const width = 720;
  const height = 220;
  const left = 28;
  const bottom = 24;
  const chartWidth = width - left - 10;
  const chartHeight = height - bottom - 10;
  const barWidth = Math.max(8, Math.floor(chartWidth / 24) - 4);
  const bars = items.map(function(item, idx) {
    const value = Number(item && item.total_tokens || 0);
    const x = left + idx * (chartWidth / 24) + 2;
    const h = Math.round(chartHeight * value / max);
    const y = 10 + chartHeight - h;
    const hour = String(idx).padStart(2, '0') + ':00';
    return '<g><title>' + hour + ' | ' + fmtInt(value) + '</title><rect x="' + x + '" y="' + y + '" width="' + barWidth + '" height="' + h + '" rx="4" fill="#4b82d8"></rect><text x="' + (x + 1) + '" y="' + (height - 6) + '" font-size="9" fill="#5f7692">' + String(idx) + '</text></g>';
  }).join('');
  root.innerHTML = '<svg viewBox="0 0 ' + width + ' ' + height + '" style="width:100%;height:auto;background:linear-gradient(180deg,#f9fbff 0%,#eef4ff 100%);border:1px solid var(--line);border-radius:16px"><line x1="' + left + '" y1="10" x2="' + left + '" y2="' + (10 + chartHeight) + '" stroke="rgba(24,49,79,.2)"></line><line x1="' + left + '" y1="' + (10 + chartHeight) + '" x2="' + (left + chartWidth) + '" y2="' + (10 + chartHeight) + '" stroke="rgba(24,49,79,.2)"></line>' + bars + '</svg>';
}
function renderUsageRows() {
  const root = document.getElementById('usageStatsRows');
  if (!root) return;
  const rows = usageStatsCache && usageStatsCache.rows || [];
  if (!rows.length) {
    root.innerHTML = '<div class="hint">' + ust('rowsEmpty') + '</div>';
    return;
  }
  const header = '<div class="row header" style="grid-template-columns:1.5fr .9fr .9fr .9fr .8fr .8fr"><div>' + ust('colName') + '</div><div>' + ust('colTotal') + '</div><div>' + ust('colInput') + '</div><div>' + ust('colOutput') + '</div><div>' + ust('colRequests') + '</div><div>' + ust('colCredits') + '</div></div>';
  const body = rows.slice(0, 20).map(function(row) {
    return '<div class="row" style="grid-template-columns:1.5fr .9fr .9fr .9fr .8fr .8fr"><div class="mono" style="font-size:12px">' + escapeHtml(row.name || row.id || '-') + '</div><div>' + fmtInt(row.total_tokens) + '</div><div>' + fmtInt(row.input_tokens) + '</div><div>' + fmtInt(row.output_tokens) + '</div><div>' + fmtInt(row.requests) + '</div><div>' + fmtCredits(row.credits) + '</div></div>';
  }).join('');
  root.innerHTML = header + body;
}
function renderUsageSummary() {
  const root = document.getElementById('usageStatsSummary');
  if (!root) return;
  const s = usageStatsCache && usageStatsCache.summary || {};
  root.innerHTML = [
    usageMetricCard(ust('summaryTokens'), fmtInt(s.total_tokens)),
    usageMetricCard(ust('summaryInput'), fmtInt(s.input_tokens)),
    usageMetricCard(ust('summaryOutput'), fmtInt(s.output_tokens)),
    usageMetricCard(ust('summaryRequests'), fmtInt(s.requests)),
    usageMetricCard(ust('summaryCredits'), fmtCredits(s.credits))
  ].join('');
}
function renderUsageStats() {
  ensureUsageStatsUI();
  syncUsageStatsFiltersFromState();
  buildUsageStatsEntityOptions();
  renderUsageSummary();
  renderUsageTrend();
  renderUsageRows();
  const ts = usageStatsCache && usageStatsCache.generated_at ? new Date(usageStatsCache.generated_at).toLocaleString() : '-';
  _s('usageStatsGeneratedAt', 'textContent', ust('generatedAt', { time: ts }));
}
function onUsageStatsFilterChange() {
  usageStatsState.scope = document.getElementById('usageStatsScope').value || 'user';
  usageStatsState.period = document.getElementById('usageStatsPeriod').value || 'daily';
  usageStatsState.date = document.getElementById('usageStatsDate').value || usageStatsState.date;
  usageStatsState.month = document.getElementById('usageStatsMonth').value || usageStatsState.month;
  usageStatsState.entity = document.getElementById('usageStatsEntity').value || '';
  loadUsageStats();
}
async function loadUsageStats() {
  ensureUsageStatsDefaults();
  ensureUsageStatsUI();
  syncUsageStatsFiltersFromState();
  try {
    const params = new URLSearchParams();
    params.set('scope', usageStatsState.scope);
    params.set('period', usageStatsState.period);
    if (usageStatsState.period === 'daily') params.set('date', usageStatsState.date);
    if (usageStatsState.period === 'monthly') params.set('month', usageStatsState.month);
    if (usageStatsState.entity) params.set('entity', usageStatsState.entity);
    usageStatsCache = await api('/api/admin/llm/usage-report?' + params.toString());
    renderUsageStats();
  } catch (err) {
    const msg = ust('loadFailed', { error: err.message });
    setOutput(msg);
    showToast(msg, 'error');
  }
}
function registerUsageStatsTab() {
  if (!window.AdminTabRegistry || typeof window.AdminTabRegistry.registerTab !== 'function') return;
  window.AdminTabRegistry.registerTab({
    id: 'usagestats',
    title: function() { return ust('tabTitle'); },
    subtitle: function() { return ust('tabSubtitle'); },
    onOpen: function() { loadUsageStats(); }
  });
}
if (window.AdminTabRegistry && typeof window.AdminTabRegistry.onLanguageChange === 'function') {
  window.AdminTabRegistry.onLanguageChange(function() {
    applyUsageStatsI18n();
  });
}
registerUsageStatsTab();
ensureUsageStatsUI();
applyUsageStatsI18n();
if (typeof token === 'function' && token() && localStorage.getItem(activeTabKey) === 'usagestats') {
  openTab('usagestats');
}

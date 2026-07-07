/*
 * Usage stats admin extension.
 * ASCII only. Chinese text must use \uXXXX escapes.
 */
const USAGE_STATS_I18N = {
  en: {
    navLabel: 'Usage Stats',
    navDesc: 'Daily and monthly LLM usage analytics',
    tabTitle: 'Usage Stats',
    tabSubtitle: 'View token usage by user, security group, or LLM provider, with daily 24-hour trends.',
    reload: 'Reload',
    scope: 'Scope',
    scopeUser: 'By User',
    scopeGroup: 'By Security Group',
    scopeProvider: 'By LLM Provider',
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
    summaryCacheRate: 'Prompt Cache Rate',
    summaryCacheRead: 'Cache Read Tokens',
    summaryCacheWrite: 'Cache Write Tokens',
    summaryRequests: 'Requests',
    summaryCredits: 'Credits',
    summaryCostRMB: 'Charge (RMB)',
    trendTitle: '24-Hour Trend',
    trendEmpty: 'No daily trend is available for the selected view.',
    rowsTitle: 'Usage Ranking',
    rowsEmpty: 'No usage data found for the current filter.',
    colName: 'Name',
    colTotal: 'Total',
    colInput: 'Input',
    colOutput: 'Output',
    colCacheRead: 'Cache Read',
    colCacheRate: 'Cache Rate',
    colRequests: 'Requests',
    colCredits: 'Credits',
    colCostRMB: 'Charge',
    loadFailed: 'Load usage stats failed: {error}',
    generatedAt: 'Generated at {time}'
    , subtabUsage: 'Usage Report'
    , subtabRanking: 'Ranking'
    , rankingPeriodYearly: 'Yearly'
    , rankingDimension: 'Display'
    , rankingAll: 'All'
    , rankingTokens: 'Tokens'
    , rankingDuration: 'Online Time'
    , rankingYear: 'Year'
    , rankingEmpty: 'No ranking data found for the selected period.'
    , rankingTokenRank: 'Token Rank'
    , rankingDurationRank: 'Time Rank'
    , rankingPager: 'Showing {start}-{end} / {total}'
    , rankingPrev: 'Previous'
    , rankingNext: 'Next'
    , rankingLoadFailed: 'Load user rankings failed: {error}'
  },
  zh: {
    navLabel: '\u4f7f\u7528\u7edf\u8ba1',
    navDesc: '\u6309\u65e5\u3001\u6309\u6708\u7684 LLM \u7528\u91cf\u62a5\u8868',
    tabTitle: '\u4f7f\u7528\u7edf\u8ba1',
    tabSubtitle: '\u67e5\u770b\u6309\u7528\u6237\u3001\u5b89\u5168\u7ec4\u6216 LLM \u670d\u52a1\u5546\u7684 token \u7528\u91cf\uff0c\u5305\u542b\u6bcf\u65e5 24 \u5c0f\u65f6\u8d8b\u52bf\u3002',
    reload: '\u91cd\u65b0\u52a0\u8f7d',
    scope: '\u7edf\u8ba1\u7ef4\u5ea6',
    scopeUser: '\u6309\u7528\u6237',
    scopeGroup: '\u6309\u5b89\u5168\u7ec4',
    scopeProvider: '\u6309 LLM \u670d\u52a1\u5546',
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
    summaryCacheRate: 'Prompt \u7f13\u5b58\u7387',
    summaryCacheRead: '\u7f13\u5b58\u8bfb\u53d6 Token',
    summaryCacheWrite: '\u7f13\u5b58\u5199\u5165 Token',
    summaryRequests: '\u8bf7\u6c42\u6570',
    summaryCredits: '\u79ef\u5206',
    summaryCostRMB: '\u8ba1\u8d39\uff08\u5143\uff09',
    trendTitle: '24 \u5c0f\u65f6\u8d8b\u52bf',
    trendEmpty: '\u5f53\u524d\u7b5b\u9009\u4e0b\u65e0\u6bcf\u65e5\u8d8b\u52bf\u6570\u636e\u3002',
    rowsTitle: '\u7528\u91cf\u6392\u540d',
    rowsEmpty: '\u5f53\u524d\u7b5b\u9009\u4e0b\u6682\u65e0\u7528\u91cf\u6570\u636e\u3002',
    colName: '\u540d\u79f0',
    colTotal: '\u603b\u8ba1',
    colInput: '\u8f93\u5165',
    colOutput: '\u8f93\u51fa',
    colCacheRead: '\u7f13\u5b58\u8bfb\u53d6',
    colCacheRate: '\u7f13\u5b58\u7387',
    colRequests: '\u8bf7\u6c42\u6570',
    colCredits: '\u79ef\u5206',
    colCostRMB: '\u8ba1\u8d39',
    loadFailed: '\u52a0\u8f7d\u4f7f\u7528\u7edf\u8ba1\u5931\u8d25: {error}',
    generatedAt: '\u751f\u6210\u65f6\u95f4 {time}'
    , subtabUsage: '\u7528\u91cf\u7edf\u8ba1'
    , subtabRanking: '\u6392\u884c\u699c'
    , rankingPeriodYearly: '\u6309\u5e74'
    , rankingDimension: '\u663e\u793a\u7ef4\u5ea6'
    , rankingAll: '\u5168\u90e8'
    , rankingTokens: 'Token \u91cf'
    , rankingDuration: '\u5728\u7ebf\u65f6\u957f'
    , rankingYear: '\u5e74\u4efd'
    , rankingEmpty: '\u5f53\u524d\u5468\u671f\u6682\u65e0\u6392\u884c\u6570\u636e\u3002'
    , rankingTokenRank: 'Token \u6392\u540d'
    , rankingDurationRank: '\u65f6\u957f\u6392\u540d'
    , rankingPager: '\u663e\u793a {start}-{end} / {total}'
    , rankingPrev: '\u4e0a\u4e00\u9875'
    , rankingNext: '\u4e0b\u4e00\u9875'
    , rankingLoadFailed: '\u52a0\u8f7d\u7528\u6237\u6392\u884c\u5931\u8d25: {error}'
  }
};
const ust = (key, vars = {}) => ((USAGE_STATS_I18N[currentLang] || USAGE_STATS_I18N.en)[key] || USAGE_STATS_I18N.en[key] || key).replace(/\{(\w+)\}/g, (_, name) => vars[name] ?? '');
let usageStatsCache = null;
let usageStatsState = {
  subtab: 'usage',
  scope: 'user',
  period: 'daily',
  date: '',
  month: '',
  year: '',
  entity: '',
  rankingDimension: 'all',
  rankingPage: 1
};
let userRankingCache = null;
function usageStatsTenantScoped() {
  const profile = typeof adminProfile === 'function' ? adminProfile() : null;
  return !!(profile && String(profile.scope || '').toLowerCase() === 'tenant');
}
function syncUsageStatsScopeVisibility() {
  const root = document.getElementById('usageStatsRoot');
  if (root) root.classList.toggle('hidden', !usageStatsTenantScoped());
}
function ensureUsageStatsDefaults() {
  const now = new Date();
  if (!usageStatsState.date) usageStatsState.date = now.toISOString().slice(0, 10);
  if (!usageStatsState.month) usageStatsState.month = now.toISOString().slice(0, 7);
  if (!usageStatsState.year) usageStatsState.year = String(now.getUTCFullYear());
}
function fmtInt(value) {
  const locale = currentLang === 'zh' ? 'zh-CN' : 'en-US';
  return Number(value || 0).toLocaleString(locale);
}
function fmtPercent(part, total) {
  const locale = currentLang === 'zh' ? 'zh-CN' : 'en-US';
  const numerator = Number(part || 0);
  const denominator = Number(total || 0);
  if (!denominator || !Number.isFinite(numerator) || !Number.isFinite(denominator)) return '0%';
  return (numerator / denominator).toLocaleString(locale, {
    style: 'percent',
    minimumFractionDigits: 0,
    maximumFractionDigits: 1
  });
}
function fmtCredits(value) {
  const n = Number(value || 0);
  return Math.abs(n - Math.round(n)) < 0.000001 ? String(Math.round(n)) : n.toFixed(3).replace(/0+$/, '').replace(/\.$/, '');
}
function fmtRMB(value) {
  const n = Number(value || 0);
  if (!Number.isFinite(n)) return '0';
  return n.toFixed(n >= 100 ? 2 : 4).replace(/0+$/, '').replace(/\.$/, '') || '0';
}
function fmtDuration(seconds) {
  const total = Math.max(0, Number(seconds || 0));
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  if (hours > 0) return hours + 'h ' + minutes + ' Min';
  return minutes + ' Min';
}
function isRankingEmail(value) {
  const email = String(value || '').trim();
  return email.split('@').length === 2 && !/\s/.test(email) && !email.startsWith('@') && !email.endsWith('@');
}
function usageMetricCard(label, value, hint) {
  return '<div class="metric" style="padding:12px 13px"><label>' + escapeHtml(label) + '</label><strong>' + escapeHtml(value) + '</strong>' + (hint ? ('<span>' + escapeHtml(hint) + '</span>') : '') + '</div>';
}
function ensureUsageStatsUI() {
  if (document.getElementById('usageStatsRoot')) return;
  const tab = document.getElementById('tab-usagestats');
  if (!tab) return;
  if (!document.getElementById('userRankingStyles')) {
    const style = document.createElement('style');
    style.id = 'userRankingStyles';
    style.textContent = '#userRankingCards{align-items:stretch}.usage-stats-subtabs{display:inline-flex!important;gap:4px!important;padding:3px!important;border:1px solid #d7e2f2!important;border-radius:10px!important;background:#f5f8fd!important}.usage-stats-subtab{position:relative!important;height:34px!important;padding:0 15px!important;border:0!important;border-radius:7px!important;background:transparent!important;color:#5f7088!important;font-weight:700!important;box-shadow:none!important;cursor:pointer!important;transition:background .16s ease,color .16s ease,box-shadow .16s ease!important}.usage-stats-subtab:hover{background:#edf4ff!important;color:#263b59!important}.usage-stats-subtab:focus-visible{outline:2px solid rgba(31,111,235,.35)!important;outline-offset:2px!important}.usage-stats-subtab.is-active{background:#1f6feb!important;color:#fff!important;box-shadow:0 1px 2px rgba(23,70,130,.18)!important}.user-ranking-card{min-width:0;height:100%;padding:12px 13px!important;gap:8px!important;display:grid!important;grid-template-rows:22px 1fr}.user-ranking-card-title{display:flex;align-items:center;min-width:0;height:22px;font-size:13px;line-height:22px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.user-ranking-card-metrics{display:grid!important;grid-template-columns:repeat(2,minmax(0,1fr))!important;grid-auto-rows:44px;gap:6px!important;align-items:stretch}.user-ranking-card-metrics .usage-rank-chip{min-height:44px;display:flex;flex-direction:column;justify-content:center}.user-ranking-card-metrics .usage-rank-label,.user-ranking-card-metrics .usage-rank-value{line-height:1.15}@media(max-width:1180px){#userRankingCards{grid-template-columns:repeat(2,minmax(0,1fr))!important}}@media(max-width:760px){#userRankingCards{grid-template-columns:1fr!important}.usage-stats-subtabs{display:flex!important;width:100%!important}.usage-stats-subtab{flex:1 1 0!important}}';
    document.head.appendChild(style);
  }
  const host = document.createElement('div');
  host.id = 'usageStatsRoot';
  host.innerHTML = '' +
    '<div class="filter-group usage-stats-subtabs" style="margin-bottom:12px" role="tablist"><button id="usageStatsSubtabUsage" class="usage-stats-subtab" type="button" role="tab" aria-controls="usageStatsUsagePane" onclick="switchUsageStatsSubtab(\'usage\')" onkeydown="onUsageStatsSubtabKeydown(event)"></button><button id="usageStatsSubtabRanking" class="usage-stats-subtab" type="button" role="tab" aria-controls="usageStatsRankingPane" onclick="switchUsageStatsSubtab(\'ranking\')" onkeydown="onUsageStatsSubtabKeydown(event)"></button></div>' +
    '<div id="usageStatsUsagePane" role="tabpanel" aria-labelledby="usageStatsSubtabUsage">' +
    '<div class="item" style="padding:12px 14px"><div class="grid2" style="gap:8px">' +
    '<div><label id="usageStatsScopeLabel"></label><select id="usageStatsScope" style="height:36px" onchange="onUsageStatsFilterChange()"><option value="user" id="usageStatsScopeUser"></option><option value="group" id="usageStatsScopeGroup"></option><option value="provider" id="usageStatsScopeProvider"></option></select></div>' +
    '<div><label id="usageStatsPeriodLabel"></label><select id="usageStatsPeriod" style="height:36px" onchange="onUsageStatsFilterChange()"><option value="daily" id="usageStatsPeriodDaily"></option><option value="monthly" id="usageStatsPeriodMonthly"></option></select></div>' +
    '<div id="usageStatsDateWrap"><label id="usageStatsDateLabel"></label><input id="usageStatsDate" style="height:36px" type="date" onchange="onUsageStatsFilterChange()"></div>' +
    '<div id="usageStatsMonthWrap"><label id="usageStatsMonthLabel"></label><input id="usageStatsMonth" style="height:36px" type="month" onchange="onUsageStatsFilterChange()"></div>' +
    '<div style="grid-column:1 / -1"><label id="usageStatsEntityLabel"></label><select id="usageStatsEntity" style="height:36px;max-width:360px" onchange="onUsageStatsFilterChange()"></select></div>' +
    '</div><div id="usageStatsGeneratedAt" class="item-meta" style="margin-top:8px;font-size:11px"></div></div>' +
    '<div id="usageStatsSummary" class="metrics" style="margin-top:10px;max-width:none;grid-template-columns:repeat(auto-fit,minmax(145px,1fr));gap:8px"></div>' +
    '<div class="usage-stats-detail-grid">' +
    '<div class="item" style="padding:12px 14px"><div class="item-title" style="font-size:14px" id="usageStatsTrendTitle"></div><div id="usageStatsTrend" style="margin-top:8px"></div></div>' +
    '<div class="item" style="padding:12px 14px"><div class="item-title" style="font-size:14px" id="usageStatsRowsTitle"></div><div id="usageStatsRows" style="margin-top:8px"></div></div>' +
    '</div></div>' +
    '<div id="usageStatsRankingPane" class="hidden" role="tabpanel" aria-labelledby="usageStatsSubtabRanking">' +
    '<div class="item" style="padding:12px 14px"><div class="grid3" style="gap:8px">' +
    '<div><label id="userRankingPeriodLabel"></label><select id="userRankingPeriod" style="height:36px" onchange="onUserRankingFilterChange()"><option value="daily" id="userRankingPeriodDaily"></option><option value="monthly" id="userRankingPeriodMonthly"></option><option value="yearly" id="userRankingPeriodYearly"></option></select></div>' +
    '<div id="userRankingDateWrap"><label id="userRankingDateLabel"></label><input id="userRankingDate" style="height:36px" type="date" onchange="onUserRankingFilterChange()"></div>' +
    '<div id="userRankingMonthWrap"><label id="userRankingMonthLabel"></label><input id="userRankingMonth" style="height:36px" type="month" onchange="onUserRankingFilterChange()"></div>' +
    '<div id="userRankingYearWrap"><label id="userRankingYearLabel"></label><input id="userRankingYear" style="height:36px" type="number" min="1970" max="9999" onchange="onUserRankingFilterChange()"></div>' +
    '<div><label id="userRankingDimensionLabel"></label><select id="userRankingDimension" style="height:36px" onchange="onUserRankingFilterChange()"><option value="all" id="userRankingDimensionAll"></option><option value="tokens" id="userRankingDimensionTokens"></option><option value="duration" id="userRankingDimensionDuration"></option></select></div>' +
    '</div><div id="userRankingGeneratedAt" class="item-meta" style="margin-top:8px;font-size:11px"></div></div>' +
    '<div id="userRankingCards" style="display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:10px;margin-top:10px"></div>' +
    '<div id="userRankingPager" class="pager hidden" style="margin-top:12px"><div id="userRankingPagerMeta" class="pager-meta"></div><div class="pager-actions"><button id="userRankingPrev" class="btn-ghost" type="button" onclick="changeUserRankingPage(-1)"></button><button id="userRankingNext" class="btn-ghost" type="button" onclick="changeUserRankingPage(1)"></button></div></div>' +
    '</div>';
  tab.appendChild(host);
  syncUsageStatsScopeVisibility();
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
  _s('usageStatsScopeProvider', 'textContent', ust('scopeProvider'));
  _s('usageStatsPeriodLabel', 'textContent', ust('period'));
  _s('usageStatsPeriodDaily', 'textContent', ust('periodDaily'));
  _s('usageStatsPeriodMonthly', 'textContent', ust('periodMonthly'));
  _s('usageStatsDateLabel', 'textContent', ust('date'));
  _s('usageStatsMonthLabel', 'textContent', ust('month'));
  _s('usageStatsEntityLabel', 'textContent', ust('entity'));
  _s('usageStatsTrendTitle', 'textContent', ust('trendTitle'));
  _s('usageStatsRowsTitle', 'textContent', ust('rowsTitle'));
  _s('usageStatsSubtabUsage', 'textContent', ust('subtabUsage'));
  _s('usageStatsSubtabRanking', 'textContent', ust('subtabRanking'));
  _s('userRankingPeriodLabel', 'textContent', ust('period'));
  _s('userRankingPeriodDaily', 'textContent', ust('periodDaily'));
  _s('userRankingPeriodMonthly', 'textContent', ust('periodMonthly'));
  _s('userRankingPeriodYearly', 'textContent', ust('rankingPeriodYearly'));
  _s('userRankingDateLabel', 'textContent', ust('date'));
  _s('userRankingMonthLabel', 'textContent', ust('month'));
  _s('userRankingYearLabel', 'textContent', ust('rankingYear'));
  _s('userRankingDimensionLabel', 'textContent', ust('rankingDimension'));
  _s('userRankingDimensionAll', 'textContent', ust('rankingAll'));
  _s('userRankingDimensionTokens', 'textContent', ust('rankingTokens'));
  _s('userRankingDimensionDuration', 'textContent', ust('rankingDuration'));
  _s('userRankingPrev', 'textContent', ust('rankingPrev'));
  _s('userRankingNext', 'textContent', ust('rankingNext'));
  renderUsageStats();
}
function syncUsageStatsFiltersFromState() {
  ensureUsageStatsDefaults();
  const scopeEl = document.getElementById('usageStatsScope');
  const periodEl = document.getElementById('usageStatsPeriod');
  const dateEl = document.getElementById('usageStatsDate');
  const monthEl = document.getElementById('usageStatsMonth');
  if (scopeEl) scopeEl.value = usageStatsState.scope;
  if (periodEl) periodEl.value = usageStatsState.period;
  if (dateEl) dateEl.value = usageStatsState.date;
  if (monthEl) monthEl.value = usageStatsState.month;
  const dateWrap = document.getElementById('usageStatsDateWrap');
  const monthWrap = document.getElementById('usageStatsMonthWrap');
  if (dateWrap) dateWrap.style.display = usageStatsState.period === 'daily' ? 'block' : 'none';
  if (monthWrap) monthWrap.style.display = usageStatsState.period === 'monthly' ? 'block' : 'none';
}
function syncUserRankingFiltersFromState() {
  ensureUsageStatsDefaults();
  const periodEl = document.getElementById('userRankingPeriod');
  const dateEl = document.getElementById('userRankingDate');
  const monthEl = document.getElementById('userRankingMonth');
  const yearEl = document.getElementById('userRankingYear');
  const dimEl = document.getElementById('userRankingDimension');
  if (periodEl) periodEl.value = usageStatsState.period;
  if (dateEl) dateEl.value = usageStatsState.date;
  if (monthEl) monthEl.value = usageStatsState.month;
  if (yearEl) yearEl.value = usageStatsState.year;
  if (dimEl) dimEl.value = usageStatsState.rankingDimension;
  const dateWrap = document.getElementById('userRankingDateWrap');
  const monthWrap = document.getElementById('userRankingMonthWrap');
  const yearWrap = document.getElementById('userRankingYearWrap');
  if (dateWrap) dateWrap.style.display = usageStatsState.period === 'daily' ? 'block' : 'none';
  if (monthWrap) monthWrap.style.display = usageStatsState.period === 'monthly' ? 'block' : 'none';
  if (yearWrap) yearWrap.style.display = usageStatsState.period === 'yearly' ? 'block' : 'none';
}
function switchUsageStatsSubtab(tab) {
  usageStatsState.subtab = tab === 'ranking' ? 'ranking' : 'usage';
  usageStatsState.rankingPage = 1;
  renderUsageStats();
  if (usageStatsState.subtab === 'ranking') loadUserRankings();
  else loadUsageStats();
}
function onUsageStatsSubtabKeydown(event) {
  const key = event && event.key;
  if (key !== 'ArrowLeft' && key !== 'ArrowRight' && key !== 'Home' && key !== 'End') return;
  event.preventDefault();
  const next = key === 'ArrowLeft' || key === 'Home' ? 'usage' : 'ranking';
  switchUsageStatsSubtab(next);
  const btn = document.getElementById(next === 'usage' ? 'usageStatsSubtabUsage' : 'usageStatsSubtabRanking');
  if (btn) btn.focus();
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
  root.innerHTML = '<svg viewBox="0 0 ' + width + ' ' + height + '" style="width:100%;height:auto;background:linear-gradient(180deg,#f9fbff 0%,#eef4ff 100%);border:1px solid var(--line);border-radius:12px"><line x1="' + left + '" y1="10" x2="' + left + '" y2="' + (10 + chartHeight) + '" stroke="rgba(24,49,79,.2)"></line><line x1="' + left + '" y1="' + (10 + chartHeight) + '" x2="' + (left + chartWidth) + '" y2="' + (10 + chartHeight) + '" stroke="rgba(24,49,79,.2)"></line>' + bars + '</svg>';
}
function renderUsageRows() {
  const root = document.getElementById('usageStatsRows');
  if (!root) return;
  const rows = usageStatsCache && usageStatsCache.rows || [];
  if (!rows.length) {
    root.innerHTML = '<div class="usage-rank-empty">' + ust('rowsEmpty') + '</div>';
    return;
  }
  const body = rows.slice(0, 20).map(function(row, index) {
    const name = escapeHtml(row.name || row.id || '-');
    return '' +
      '<div class="usage-rank-row">' +
        '<div class="usage-rank-main">' +
          '<div class="usage-rank-name"><span class="usage-rank-index">' + (index + 1) + '</span><div style="min-width:0"><span class="usage-rank-label">' + ust('colName') + '</span><span class="usage-rank-value mono" title="' + name + '" style="overflow:hidden;text-overflow:ellipsis">' + name + '</span></div></div>' +
          '<div><span class="usage-rank-label">' + ust('colTotal') + '</span><span class="usage-rank-value">' + fmtInt(row.total_tokens) + '</span></div>' +
          '<div><span class="usage-rank-label">' + ust('colCacheRate') + '</span><span class="usage-rank-value ok">' + fmtPercent(row.cached_requests, row.requests) + '</span></div>' +
          '<div><span class="usage-rank-label">' + ust('colRequests') + '</span><span class="usage-rank-value">' + fmtInt(row.cached_requests) + ' / ' + fmtInt(row.requests) + '</span></div>' +
        '</div>' +
        '<div class="usage-rank-sub">' +
          '<div class="usage-rank-chip"><span class="usage-rank-label">' + ust('colInput') + '</span><span class="usage-rank-value">' + fmtInt(row.input_tokens) + '</span></div>' +
          '<div class="usage-rank-chip"><span class="usage-rank-label">' + ust('colOutput') + '</span><span class="usage-rank-value">' + fmtInt(row.output_tokens) + '</span></div>' +
          '<div class="usage-rank-chip"><span class="usage-rank-label">' + ust('colCacheRead') + '</span><span class="usage-rank-value cache">' + fmtInt(row.cached_input_tokens) + '</span></div>' +
          '<div class="usage-rank-chip"><span class="usage-rank-label">' + ust('colCredits') + '</span><span class="usage-rank-value">' + fmtCredits(row.credits) + '</span></div>' +
          '<div class="usage-rank-chip"><span class="usage-rank-label">' + ust('colCostRMB') + '</span><span class="usage-rank-value">\u00a5' + fmtRMB(row.total_cost_rmb) + '</span></div>' +
        '</div>' +
      '</div>';
  }).join('');
  root.innerHTML = '<div class="usage-rank-list">' + body + '</div>';
}
function renderUsageSummary() {
  const root = document.getElementById('usageStatsSummary');
  if (!root) return;
  const s = usageStatsCache && usageStatsCache.summary || {};
  root.innerHTML = [
    usageMetricCard(ust('summaryTokens'), fmtInt(s.total_tokens), ust('rowsTitle')),
    usageMetricCard(ust('summaryInput'), fmtInt(s.input_tokens), ust('colInput')),
    usageMetricCard(ust('summaryOutput'), fmtInt(s.output_tokens), ust('colOutput')),
    usageMetricCard(ust('summaryCacheRate'), fmtPercent(s.cached_requests, s.requests), fmtInt(s.cached_requests) + ' / ' + fmtInt(s.requests)),
    usageMetricCard(ust('summaryCacheRead'), fmtInt(s.cached_input_tokens), ust('colCacheRead')),
    usageMetricCard(ust('summaryCacheWrite'), fmtInt(s.cache_write_tokens), ust('summaryCacheWrite')),
    usageMetricCard(ust('summaryRequests'), fmtInt(s.cached_requests) + ' / ' + fmtInt(s.requests), ust('summaryRequests')),
    usageMetricCard(ust('summaryCredits'), fmtCredits(s.credits), ust('summaryCredits')),
    usageMetricCard(ust('summaryCostRMB'), '\u00a5' + fmtRMB(s.total_cost_rmb), ust('summaryCostRMB'))
  ].join('');
}
function renderUsageStats() {
  ensureUsageStatsUI();
  const usagePane = document.getElementById('usageStatsUsagePane');
  const rankingPane = document.getElementById('usageStatsRankingPane');
  const usageBtn = document.getElementById('usageStatsSubtabUsage');
  const rankingBtn = document.getElementById('usageStatsSubtabRanking');
  if (usagePane) {
    const active = usageStatsState.subtab === 'usage';
    usagePane.classList.toggle('hidden', !active);
    usagePane.setAttribute('aria-hidden', active ? 'false' : 'true');
  }
  if (rankingPane) {
    const active = usageStatsState.subtab === 'ranking';
    rankingPane.classList.toggle('hidden', !active);
    rankingPane.setAttribute('aria-hidden', active ? 'false' : 'true');
  }
  if (usageBtn) {
    const active = usageStatsState.subtab === 'usage';
    usageBtn.className = active ? 'usage-stats-subtab is-active' : 'usage-stats-subtab';
    usageBtn.setAttribute('aria-selected', active ? 'true' : 'false');
    usageBtn.setAttribute('tabindex', active ? '0' : '-1');
  }
  if (rankingBtn) {
    const active = usageStatsState.subtab === 'ranking';
    rankingBtn.className = active ? 'usage-stats-subtab is-active' : 'usage-stats-subtab';
    rankingBtn.setAttribute('aria-selected', active ? 'true' : 'false');
    rankingBtn.setAttribute('tabindex', active ? '0' : '-1');
  }
  syncUsageStatsFiltersFromState();
  syncUserRankingFiltersFromState();
  buildUsageStatsEntityOptions();
  renderUsageSummary();
  renderUsageTrend();
  renderUsageRows();
  renderUserRankings();
  const generatedAt = document.getElementById('usageStatsGeneratedAt');
  if (!generatedAt) return;
  if (usageStatsCache && usageStatsCache.generated_at) {
    const locale = currentLang === 'zh' ? 'zh-CN' : 'en-US';
    const ts = new Date(usageStatsCache.generated_at).toLocaleString(locale);
    generatedAt.textContent = ust('generatedAt', { time: ts });
  } else {
    generatedAt.textContent = '';
  }
}
function renderUserRankings() {
  const root = document.getElementById('userRankingCards');
  if (!root) return;
  const rows = (userRankingCache && userRankingCache.rows || []).filter(function(row) {
    return row && isRankingEmail(row.user_email);
  });
  if (!rows.length) {
    root.innerHTML = '<div class="hint" style="grid-column:1/-1">' + ust('rankingEmpty') + '</div>';
  } else {
    root.innerHTML = rows.map(function(row) {
      const email = String(row.user_email || '').trim();
      return '<div class="item user-ranking-card">' +
        '<div class="item-title mono user-ranking-card-title" title="' + escapeHtml(email) + '">' + escapeHtml(email) + '</div>' +
        '<div class="usage-rank-sub user-ranking-card-metrics">' +
        '<div class="usage-rank-chip"><span class="usage-rank-label">' + ust('rankingTokens') + '</span><span class="usage-rank-value">' + fmtInt(row.total_tokens) + '</span></div>' +
        '<div class="usage-rank-chip"><span class="usage-rank-label">' + ust('rankingDuration') + '</span><span class="usage-rank-value">' + fmtDuration(row.duration_seconds) + '</span></div>' +
        '<div class="usage-rank-chip"><span class="usage-rank-label">' + ust('rankingTokenRank') + '</span><span class="usage-rank-value">#' + fmtInt(row.token_rank) + '</span></div>' +
        '<div class="usage-rank-chip"><span class="usage-rank-label">' + ust('rankingDurationRank') + '</span><span class="usage-rank-value">#' + fmtInt(row.duration_rank) + '</span></div>' +
        '</div></div>';
    }).join('');
  }
  const generatedAt = document.getElementById('userRankingGeneratedAt');
  if (generatedAt && userRankingCache && userRankingCache.generated_at) {
    const locale = currentLang === 'zh' ? 'zh-CN' : 'en-US';
    generatedAt.textContent = ust('generatedAt', { time: new Date(userRankingCache.generated_at).toLocaleString(locale) });
  }
  const pager = document.getElementById('userRankingPager');
  const meta = document.getElementById('userRankingPagerMeta');
  const prev = document.getElementById('userRankingPrev');
  const next = document.getElementById('userRankingNext');
  const total = Number(userRankingCache && userRankingCache.total || 0);
  const page = Number(userRankingCache && userRankingCache.page || usageStatsState.rankingPage);
  const pageSize = Number(userRankingCache && userRankingCache.page_size || 100);
  if (pager) pager.classList.toggle('hidden', total <= pageSize && page <= 1);
  if (meta) meta.textContent = total ? ust('rankingPager', { start: String((page - 1) * pageSize + 1), end: String(Math.min(total, page * pageSize)), total: String(total) }) : '';
  if (prev) prev.disabled = page <= 1;
  if (next) next.disabled = page * pageSize >= total;
}
function onUsageStatsFilterChange() {
  if (!usageStatsTenantScoped()) return;
  const scopeEl = document.getElementById('usageStatsScope');
  const periodEl = document.getElementById('usageStatsPeriod');
  const dateEl = document.getElementById('usageStatsDate');
  const monthEl = document.getElementById('usageStatsMonth');
  const entityEl = document.getElementById('usageStatsEntity');
  const nextScope = scopeEl && scopeEl.value || 'user';
  const scopeChanged = usageStatsState.scope !== nextScope;
  usageStatsState.scope = nextScope;
  usageStatsState.period = periodEl && periodEl.value || 'daily';
  usageStatsState.date = dateEl && dateEl.value || usageStatsState.date;
  usageStatsState.month = monthEl && monthEl.value || usageStatsState.month;
  usageStatsState.entity = scopeChanged ? '' : (entityEl && entityEl.value || '');
  loadUsageStats();
}
function onUserRankingFilterChange() {
  const periodEl = document.getElementById('userRankingPeriod');
  const dateEl = document.getElementById('userRankingDate');
  const monthEl = document.getElementById('userRankingMonth');
  const yearEl = document.getElementById('userRankingYear');
  const dimEl = document.getElementById('userRankingDimension');
  usageStatsState.period = periodEl && periodEl.value || 'daily';
  usageStatsState.date = dateEl && dateEl.value || usageStatsState.date;
  usageStatsState.month = monthEl && monthEl.value || usageStatsState.month;
  usageStatsState.year = yearEl && yearEl.value || usageStatsState.year;
  usageStatsState.rankingDimension = dimEl && dimEl.value || 'all';
  usageStatsState.rankingPage = 1;
  loadUserRankings();
}
function changeUserRankingPage(delta) {
  const next = Math.max(1, usageStatsState.rankingPage + delta);
  if (next === usageStatsState.rankingPage) return;
  usageStatsState.rankingPage = next;
  loadUserRankings();
}
async function loadUsageStats() {
  if (!usageStatsTenantScoped()) {
    syncUsageStatsScopeVisibility();
    return;
  }
  if (usageStatsState.subtab === 'ranking') {
    loadUserRankings();
    return;
  }
  ensureUsageStatsDefaults();
  ensureUsageStatsUI();
  syncUsageStatsScopeVisibility();
  syncUsageStatsFiltersFromState();
  const root = document.getElementById('usageStatsRoot');
  if (!root) return;
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
async function loadUserRankings() {
  if (!usageStatsTenantScoped()) {
    syncUsageStatsScopeVisibility();
    return;
  }
  ensureUsageStatsDefaults();
  ensureUsageStatsUI();
  syncUserRankingFiltersFromState();
  try {
    const params = new URLSearchParams();
    params.set('period', usageStatsState.period);
    params.set('dimension', usageStatsState.rankingDimension);
    params.set('page', String(usageStatsState.rankingPage));
    params.set('page_size', '100');
    if (usageStatsState.period === 'daily') params.set('date', usageStatsState.date);
    if (usageStatsState.period === 'monthly') params.set('month', usageStatsState.month);
    if (usageStatsState.period === 'yearly') params.set('year', usageStatsState.year);
    userRankingCache = await api('/api/admin/user-rankings?' + params.toString());
    renderUsageStats();
  } catch (err) {
    const msg = ust('rankingLoadFailed', { error: err.message });
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
window.loadUsageStats = loadUsageStats;
window.loadUserRankings = loadUserRankings;
registerUsageStatsTab();
ensureUsageStatsUI();
applyUsageStatsI18n();
if (typeof token === 'function' && token() && usageStatsTenantScoped() && localStorage.getItem(activeTabKey) === 'usagestats') {
  openTab('usagestats');
}

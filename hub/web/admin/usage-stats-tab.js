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
    summaryCacheAnomalies: 'Cache usage anomalies',
    summaryRequests: 'Requests',
    summaryCredits: 'Credits',
    summaryCostRMB: 'RMB reference cost',
    reconciliationTitle: 'HubCenter reconciliation',
    reconciliationMatched: 'Matched',
    reconciliationMismatch: 'Mismatch',
    reconciliationUnavailable: 'Unavailable',
    reconciliationTotals: 'Hub {hubIn}/{hubOut} in/out · HubCenter {upIn}/{upOut} in/out · difference {diffIn}/{diffOut} in/out · requests {diffRequests} · Hub {hubCredits} Credits · HubCenter {upCredits} Credits · upstream Credits difference {diffUpstreamCredits}',
    reconciliationUnavailableDetail: 'HubCenter reconciliation is unavailable: {message}',
    reconciliationGroupsTitle: 'Service-group ledger',
    reconciliationGroupUnspecified: 'Unspecified service group',
    reconciliationGroupRow: '{group}: Hub {hubIn}/{hubOut} in/out · {hubCredits} Credits · HubCenter {upIn}/{upOut} in/out · {upCredits} Credits · difference {diffIn}/{diffOut} in/out · requests {diffRequests} · upstream Credits difference {diffUpstreamCredits}',
    reconciliationGroupUpstreamOnly: '{group}: HubCenter {upIn}/{upOut} in/out · {upCredits} Credits. Hub has no per-group ledger for this day, so this row is HubCenter-only.',
    reconciliationCreditsNote: 'Hub Credits include the Hub service-group multiplier. Upstream Credits strip that Hub markup so they can be compared with the HubCenter authorization debit. Day status follows tokens and requests; per-group Credits compare only when the Hub markup is uniform.',
    reconciliationCreditsNA: 'n/a',
    trendTitle: '24-Hour Trend',
    trendEmpty: 'No daily trend is available for the selected view.',
    rowsTitle: 'Usage Ranking',
    rowsEmpty: 'No usage data found for the current filter.',
    systemUserTitle: 'sys_user usage',
    colName: 'Name',
    colTotal: 'Total',
    colInput: 'Input',
    colOutput: 'Output',
    colCacheRead: 'Cache Read',
    colCacheWrite: 'Cache Write',
    colCacheRate: 'Cache Rate',
    colRequests: 'Requests',
    colCredits: 'Credits',
    colCostRMB: 'Charge',
    creditsTooltipTitle: 'How credits are deducted',
    creditsTooltipFormula: 'Per-request rule: max(((input tokens − cache read − cache write) × provider input credits/10K) + (cache read × provider cache-read credits/10K) + (cache write × provider cache-write credits/10K) + (output tokens × provider output credits/10K), minimum request credits), then apply the frozen HubCenter provider and Hub service-group multipliers once.',
    creditsTooltipScope: 'Calculated from settled requests that retain a frozen provider-route price. Each line uses only the Tokens that were settled with its directional component; legacy usage without a price snapshot is shown separately and is never used to derive a unit price. Provider prices and multipliers below are frozen settlement facts.',
    creditsTooltipInputLine: 'Non-cached input: {tokens} × {rate} Credits/10K = {credits} Credits',
    creditsTooltipCacheReadLine: 'Cache Read: {tokens} × {rate} Credits/10K = {credits} Credits',
    creditsTooltipCacheWriteLine: 'Cache Write: {tokens} × {rate} Credits/10K = {credits} Credits',
    creditsTooltipOutputLine: 'Output: {tokens} × {rate} Credits/10K = {credits} Credits',
    creditsTooltipAdjustments: 'Adjustments: minimum {minimum} + settlement/rounding {rounding} + unitemized {unitemized}',
    creditsTooltipUnitemizedLine: 'Unitemized: {credits} Credits from {requests} settled requests without a frozen price snapshot (legacy token-count billing), covering non-cached input {input} + cache read {cacheRead} + cache write {cacheWrite} + output {output} Tokens; together with the lines above this equals the row totals.',
    creditsTooltipUnitemizedDirectionalEstimate: 'The same unitemized Tokens at the frozen provider-route unit prices would be about {credits} Credits (non-cached input {input} + cache read {cacheRead} + cache write {cacheWrite} + output {output}). The remaining difference is the legacy tokens-per-credit charge, which does not discount cache reads.',
    creditsTooltipActualTotal: 'Actual credits deducted: {credits}',
    creditsTooltipMultiplier: '{provider}: {label} × {multiplier}',
    creditsTooltipServiceGroupMultiplier: 'service-group multiplier',
    creditsTooltipProviderMultiplier: 'provider multiplier',
    creditsTooltipMultiplierUnavailable: 'Settlement multiplier snapshot: unavailable for this legacy usage record. Current HubCenter provider settings cannot recreate the historical charge.',
    creditsTooltipCurrentProviderMultiplier: 'HubCenter current setting (reference only): {provider} provider multiplier × {multiplier}',
    creditsTooltipProviderBasePricing: 'Settled provider-route price ({provider}, used for this debit): input {inputCredits} Credits/10K · cache read {cacheReadCredits} · cache write {cacheWriteCredits} · output {outputCredits} Credits/10K; input ¥{inputRMB}/1M · cache read ¥{cacheReadRMB}/1M · cache write ¥{cacheWriteRMB}/1M · output ¥{outputRMB}/1M',
    creditsTooltipRMBRates: 'Effective RMB price: non-cached input ¥{input}/1M Tokens · cache read ¥{cacheRead}/1M Tokens · cache write ¥{cacheWrite}/1M Tokens · output ¥{output}/1M Tokens',
    creditsTooltipRMBRateUnavailable: 'sample insufficient',
    creditsTooltipRMB: 'RMB: non-cached input ¥{input} + cache read ¥{cacheRead} + cache write ¥{cacheWrite} + output ¥{output} = ¥{total}',
    creditsTooltipRMBCoverage: 'RMB reference-cost coverage: {pricedRequests}/{requests} settled requests · non-cached input {pricedNormalInputTokens}/{normalInputTokens} Tokens · cache read {pricedCacheReadTokens}/{cacheReadTokens} Tokens · cache write {pricedCacheWriteTokens}/{cacheWriteTokens} Tokens · output {pricedOutputTokens}/{outputTokens} Tokens · {pricedCredits}/{credits} Credits. {missingCredits} Credits lack a frozen RMB price; current HubCenter pricing is never used to rewrite historical settlement.',
    creditsTooltipRMBUnavailable: 'RMB reference cost is unavailable: none of the settled requests in this result retained a frozen RMB price.',
    rmbReferencePartial: '{value} (partial)',
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
    summaryCacheAnomalies: '\u7f13\u5b58\u7528\u91cf\u5f02\u5e38',
    summaryRequests: '\u8bf7\u6c42\u6570',
    summaryCredits: '\u79ef\u5206',
    summaryCostRMB: '\u4eba\u6c11\u5e01\u53c2\u8003\u6210\u672c',
    reconciliationTitle: 'HubCenter \u5bf9\u8d26',
    reconciliationMatched: '\u5df2\u5bf9\u4e0a',
    reconciliationMismatch: '\u4e0d\u4e00\u81f4',
    reconciliationUnavailable: '\u6682\u4e0d\u53ef\u7528',
    reconciliationTotals: 'Hub \u8f93\u5165/\u8f93\u51fa {hubIn}/{hubOut} \u00b7 HubCenter {upIn}/{upOut} \u00b7 \u5dee\u989d {diffIn}/{diffOut} \u00b7 \u8bf7\u6c42\u6570\u5dee\u989d {diffRequests} \u00b7 Hub {hubCredits} \u79ef\u5206 \u00b7 HubCenter {upCredits} \u79ef\u5206 \u00b7 \u4e0a\u6e38\u79ef\u5206\u5dee\u989d {diffUpstreamCredits}',
    reconciliationUnavailableDetail: 'HubCenter \u5bf9\u8d26\u6682\u4e0d\u53ef\u7528\uff1a{message}',
    reconciliationGroupsTitle: '\u670d\u52a1\u7ec4\u53f0\u8d26',
    reconciliationGroupUnspecified: '\u672a\u6807\u6ce8\u670d\u52a1\u7ec4',
    reconciliationGroupRow: '{group}\uff1aHub \u8f93\u5165/\u8f93\u51fa {hubIn}/{hubOut} \u00b7 {hubCredits} \u79ef\u5206 \u00b7 HubCenter {upIn}/{upOut} \u00b7 {upCredits} \u79ef\u5206 \u00b7 \u5dee\u989d {diffIn}/{diffOut} \u00b7 \u8bf7\u6c42\u6570\u5dee\u989d {diffRequests} \u00b7 \u4e0a\u6e38\u79ef\u5206\u5dee\u989d {diffUpstreamCredits}',
    reconciliationGroupUpstreamOnly: '{group}\uff1aHubCenter \u8f93\u5165/\u8f93\u51fa {upIn}/{upOut} \u00b7 {upCredits} \u79ef\u5206\u3002Hub \u8be5\u65e5\u5c1a\u65e0\u670d\u52a1\u7ec4\u53f0\u8d26\uff0c\u6b64\u884c\u4ec5\u5c55\u793a HubCenter \u5206\u7ec4\u3002',
    reconciliationCreditsNote: 'Hub \u79ef\u5206\u542b\u672c\u5730\u670d\u52a1\u7ec4\u500d\u7387\uff1b\u4e0a\u6e38\u79ef\u5206\u4f1a\u5265\u6389\u8be5\u500d\u7387\uff0c\u7528\u4ee5\u5bf9\u8d26 HubCenter \u6388\u6743\u6263\u8d39\u3002\u65e5\u72b6\u6001\u6309 token \u4e0e\u8bf7\u6c42\u6570\u5224\u5b9a\uff1b\u670d\u52a1\u7ec4\u79ef\u5206\u4ec5\u5728 Hub \u52a0\u4ef7\u4e00\u81f4\u65f6\u6bd4\u5bf9\u3002',
    reconciliationCreditsNA: '\u4e0d\u53ef\u6bd4',
    trendTitle: '24 \u5c0f\u65f6\u8d8b\u52bf',
    trendEmpty: '\u5f53\u524d\u7b5b\u9009\u4e0b\u65e0\u6bcf\u65e5\u8d8b\u52bf\u6570\u636e\u3002',
    rowsTitle: '\u7528\u91cf\u6392\u540d',
    rowsEmpty: '\u5f53\u524d\u7b5b\u9009\u4e0b\u6682\u65e0\u7528\u91cf\u6570\u636e\u3002',
    systemUserTitle: 'sys_user \u7528\u91cf',
    colName: '\u540d\u79f0',
    colTotal: '\u603b\u8ba1',
    colInput: '\u8f93\u5165',
    colOutput: '\u8f93\u51fa',
    colCacheRead: '\u7f13\u5b58\u8bfb\u53d6',
    colCacheWrite: '\u7f13\u5b58\u5199\u5165',
    colCacheRate: '\u7f13\u5b58\u7387',
    colRequests: '\u8bf7\u6c42\u6570',
    colCredits: '\u79ef\u5206',
    colCostRMB: '\u8ba1\u8d39',
    creditsTooltipTitle: '\u79ef\u5206\u6263\u9664\u8ba1\u7b97',
    creditsTooltipFormula: '\u5355\u6b21\u8bf7\u6c42\u89c4\u5219\uff1amax((\u8f93\u5165 Token \u2212 \u7f13\u5b58\u8bfb\u53d6 \u2212 \u7f13\u5b58\u5199\u5165) \u00d7 \u670d\u52a1\u5546\u8f93\u5165\u6bcf 1 \u4e07 Token \u79ef\u5206 + \u7f13\u5b58\u8bfb\u53d6 \u00d7 \u670d\u52a1\u5546\u7f13\u5b58\u8bfb\u53d6\u6bcf 1 \u4e07 Token \u79ef\u5206 + \u7f13\u5b58\u5199\u5165 \u00d7 \u670d\u52a1\u5546\u7f13\u5b58\u5199\u5165\u6bcf 1 \u4e07 Token \u79ef\u5206 + \u8f93\u51fa Token \u00d7 \u670d\u52a1\u5546\u8f93\u51fa\u6bcf 1 \u4e07 Token \u79ef\u5206\uff0c\u6700\u4f4e\u8bf7\u6c42\u79ef\u5206)\uff0c\u518d\u5404\u751f\u6548\u4e00\u6b21 HubCenter \u670d\u52a1\u5546\u500d\u7387\u4e0e Hub \u670d\u52a1\u7ec4\u500d\u7387\u3002',
    creditsTooltipScope: '\u660e\u7ec6\u4ec5\u6c47\u603b\u4fdd\u7559\u4e86\u56fa\u5316\u670d\u52a1\u5546\u8def\u7531\u5355\u4ef7\u7684\u5df2\u7ed3\u7b97\u8bf7\u6c42\u3002\u6bcf\u4e00\u884c\u4ec5\u4f7f\u7528\u5bf9\u5e94\u65b9\u5411\u8ba1\u8d39\u5206\u91cf\u5df2\u7ed3\u7b97\u7684 Token\uff1b\u7f3a\u5c11\u5355\u4ef7\u5feb\u7167\u7684\u5386\u53f2\u7528\u91cf\u4f1a\u5355\u72ec\u5c55\u793a\uff0c\u4e0d\u518d\u7528\u6765\u53cd\u63a8\u5355\u4ef7\u3002\u4e0b\u65b9\u670d\u52a1\u5546\u5355\u4ef7\u4e0e\u500d\u7387\u5747\u4e3a\u7ed3\u7b97\u65f6\u56fa\u5316\u7684\u4e8b\u5b9e\u3002',
    creditsTooltipInputLine: '\u975e\u7f13\u5b58\u8f93\u5165\uff1a{tokens} \u00d7 \u6bcf 1 \u4e07 Token {rate} \u79ef\u5206 = {credits} \u79ef\u5206',
    creditsTooltipCacheReadLine: '\u7f13\u5b58\u8bfb\u53d6\uff1a{tokens} \u00d7 \u6bcf 1 \u4e07 Token {rate} \u79ef\u5206 = {credits} \u79ef\u5206',
    creditsTooltipCacheWriteLine: '\u7f13\u5b58\u5199\u5165\uff1a{tokens} \u00d7 \u6bcf 1 \u4e07 Token {rate} \u79ef\u5206 = {credits} \u79ef\u5206',
    creditsTooltipOutputLine: '\u8f93\u51fa\uff1a{tokens} \u00d7 \u6bcf 1 \u4e07 Token {rate} \u79ef\u5206 = {credits} \u79ef\u5206',
    creditsTooltipAdjustments: '\u8c03\u6574\uff1a\u6700\u4f4e\u6d88\u8d39 {minimum} + \u7ed3\u7b97/\u56db\u820d\u4e94\u5165 {rounding} + \u672a\u62c6\u5206 {unitemized}',
    creditsTooltipUnitemizedLine: '\u672a\u62c6\u5206 {credits} \u79ef\u5206\uff1a{requests} \u7b14\u7ed3\u7b97\u7f3a\u5c11\u56fa\u5316\u5355\u4ef7\u5feb\u7167\uff08\u65e7\u7248\u6309\u91cf\u8ba1\u8d39\uff09\uff0c\u542b\u975e\u7f13\u5b58\u8f93\u5165 {input} + \u7f13\u5b58\u8bfb\u53d6 {cacheRead} + \u7f13\u5b58\u5199\u5165 {cacheWrite} + \u8f93\u51fa {output} Token\uff1b\u4e0e\u4e0a\u65b9\u660e\u7ec6\u76f8\u52a0\u5373\u7b49\u4e8e\u672c\u884c\u603b\u91cf\u3002',
    creditsTooltipUnitemizedDirectionalEstimate: '\u540c\u4e00\u7b14\u672a\u62c6\u5206 Token \u6309\u5df2\u7ed3\u7b97\u8def\u7531\u5355\u4ef7\u8ba1\u7b97\u7ea6 {credits} \u79ef\u5206\uff08\u975e\u7f13\u5b58\u8f93\u5165 {input} + \u7f13\u5b58\u8bfb\u53d6 {cacheRead} + \u7f13\u5b58\u5199\u5165 {cacheWrite} + \u8f93\u51fa {output}\uff09\u3002\u5dee\u989d\u6765\u81ea\u65e7\u7248\u6309\u91cf\u8ba1\u8d39\uff0c\u4e0d\u4f1a\u7ed9\u7f13\u5b58\u8bfb\u53d6\u6298\u6263\u3002',
    creditsTooltipActualTotal: '\u5b9e\u9645\u6263\u9664\uff1a{credits} \u79ef\u5206',
    creditsTooltipMultiplier: '{provider}\uff1a{label} \u00d7 {multiplier}',
    creditsTooltipServiceGroupMultiplier: '\u670d\u52a1\u7ec4\u500d\u7387',
    creditsTooltipProviderMultiplier: '\u670d\u52a1\u5546\u500d\u7387',
    creditsTooltipMultiplierUnavailable: '\u7ed3\u7b97\u500d\u7387\u5feb\u7167\uff1a\u8be5\u5386\u53f2\u7528\u91cf\u672a\u4fdd\u5b58\u3002\u4e0d\u80fd\u7528 HubCenter \u5f53\u524d\u670d\u52a1\u5546\u914d\u7f6e\u53cd\u63a8\u5386\u53f2\u6263\u8d39\u3002',
    creditsTooltipCurrentProviderMultiplier: 'HubCenter \u5f53\u524d\u914d\u7f6e\uff08\u4ec5\u4f9b\u53c2\u8003\uff09\uff1a{provider} \u670d\u52a1\u5546\u500d\u7387 \u00d7 {multiplier}',
    creditsTooltipProviderBasePricing: '\u5df2\u7ed3\u7b97\u670d\u52a1\u5546\u8def\u7531\u5355\u4ef7\uff08{provider}\uff0c\u672c\u6b21\u6263\u8d39\u4f7f\u7528\uff09\uff1a\u8f93\u5165\u6bcf 1 \u4e07 Token {inputCredits} \u79ef\u5206\u00b7\u7f13\u5b58\u8bfb\u53d6 {cacheReadCredits}\u00b7\u7f13\u5b58\u5199\u5165 {cacheWriteCredits}\u00b7\u8f93\u51fa\u6bcf 1 \u4e07 Token {outputCredits} \u79ef\u5206\uff1b\u8f93\u5165\u6bcf 100 \u4e07 Token \u00a5{inputRMB}\u00b7\u7f13\u5b58\u8bfb\u53d6 \u00a5{cacheReadRMB}\u00b7\u7f13\u5b58\u5199\u5165 \u00a5{cacheWriteRMB}\u00b7\u8f93\u51fa \u6bcf 100 \u4e07 Token \u00a5{outputRMB}',
    creditsTooltipRMBRates: '\u52a0\u6743\u6bcf 100 \u4e07 Token \u4eba\u6c11\u5e01\u5355\u4ef7\uff1a\u975e\u7f13\u5b58\u8f93\u5165 \u00a5{input}\uff0c\u7f13\u5b58\u8bfb\u53d6 \u00a5{cacheRead}\uff0c\u7f13\u5b58\u5199\u5165 \u00a5{cacheWrite}\uff0c\u8f93\u51fa \u00a5{output}',
    creditsTooltipRMBRateUnavailable: '\u6837\u672c\u4e0d\u8db3',
    creditsTooltipRMB: '\u4eba\u6c11\u5e01\uff1a\u975e\u7f13\u5b58\u8f93\u5165 \u00a5{input} + \u7f13\u5b58\u8bfb\u53d6 \u00a5{cacheRead} + \u7f13\u5b58\u5199\u5165 \u00a5{cacheWrite} + \u8f93\u51fa \u00a5{output} = \u00a5{total}',
    creditsTooltipRMBCoverage: '\u4eba\u6c11\u5e01\u53c2\u8003\u6210\u672c\u8986\u76d6\u5ea6\uff1a{pricedRequests}/{requests} \u7b14\u5df2\u7ed3\u7b97\u8bf7\u6c42\u00b7\u975e\u7f13\u5b58\u8f93\u5165 {pricedNormalInputTokens}/{normalInputTokens} Token\u00b7\u7f13\u5b58\u8bfb\u53d6 {pricedCacheReadTokens}/{cacheReadTokens} Token\u00b7\u7f13\u5b58\u5199\u5165 {pricedCacheWriteTokens}/{cacheWriteTokens} Token\u00b7\u8f93\u51fa {pricedOutputTokens}/{outputTokens} Token\u00b7{pricedCredits}/{credits} \u79ef\u5206\u3002{missingCredits} \u79ef\u5206\u7f3a\u5c11\u56fa\u5316\u7684\u4eba\u6c11\u5e01\u4ef7\u683c\uff1b\u4e0d\u4f7f\u7528 HubCenter \u5f53\u524d\u4ef7\u683c\u6539\u5199\u5386\u53f2\u7ed3\u7b97\u3002',
    creditsTooltipRMBUnavailable: '\u4eba\u6c11\u5e01\u53c2\u8003\u6210\u672c\u4e0d\u53ef\u7528\uff1a\u5f53\u524d\u7ed3\u679c\u4e2d\u6ca1\u6709\u5df2\u7ed3\u7b97\u8bf7\u6c42\u4fdd\u7559\u56fa\u5316\u7684\u4eba\u6c11\u5e01\u4ef7\u683c\u3002',
    rmbReferencePartial: '{value}\uff08\u90e8\u5206\uff09',
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
function fmtFormulaCredits(value) {
  const n = Number(value || 0);
  if (!Number.isFinite(n) || Math.abs(n) < 0.0000005) return '0';
  // A request is rounded only after its input and output components are
  // combined. Keep six decimals in the audit tooltip so small directional
  // components and the rounding residual remain visible and reconcile with
  // the settled three-decimal-credit debit.
  return n.toFixed(6).replace(/0+$/, '').replace(/\.$/, '');
}
function usageOptionalCreditRate(value, fallback) {
  const n = Number(value);
  return Number.isFinite(n) && n >= 0 ? n : fallback;
}
function unitemizedDirectionalCreditEstimate(data, inputTokens, cacheReadTokens, cacheWriteTokens, outputTokens) {
  const records = Array.isArray(data && data.provider_pricing) ? data.provider_pricing : [];
  if (!records.length) return null;
  const pricing = records[0];
  for (let i = 1; i < records.length; i++) {
    if (records[i].input_credits_per_10k !== pricing.input_credits_per_10k
        || records[i].output_credits_per_10k !== pricing.output_credits_per_10k
        || records[i].cache_read_credits_per_10k !== pricing.cache_read_credits_per_10k
        || records[i].cache_write_credits_per_10k !== pricing.cache_write_credits_per_10k) {
      return null;
    }
  }
  const inputRate = usageOptionalCreditRate(pricing.input_credits_per_10k, NaN);
  const outputRate = usageOptionalCreditRate(pricing.output_credits_per_10k, NaN);
  if (!(inputRate > 0) || !(outputRate >= 0)) return null;
  const cacheReadRate = usageOptionalCreditRate(pricing.cache_read_credits_per_10k, inputRate / 10);
  const cacheWriteRate = usageOptionalCreditRate(pricing.cache_write_credits_per_10k, inputRate);
  const providerID = String(pricing.provider_id || '');
  let providerMul = 1;
  let groupMul = 1;
  const multipliers = Array.isArray(data && data.provider_multipliers) ? data.provider_multipliers : [];
  multipliers.forEach(function(item) {
    if (!item) return;
    if (providerID && String(item.provider_id || '') && String(item.provider_id) !== providerID) return;
    const value = Number(item.multiplier);
    if (!Number.isFinite(value) || value <= 0) return;
    const source = String(item.multiplier_source || '').toLowerCase();
    if (source === 'provider') providerMul = value;
    if (source === 'service_group') groupMul = value;
  });
  const multiplier = providerMul * groupMul;
  const input = Number(inputTokens || 0) * inputRate / 10000 * multiplier;
  const cacheRead = Number(cacheReadTokens || 0) * cacheReadRate / 10000 * multiplier;
  const cacheWrite = Number(cacheWriteTokens || 0) * cacheWriteRate / 10000 * multiplier;
  const output = Number(outputTokens || 0) * outputRate / 10000 * multiplier;
  return {
    credits: input + cacheRead + cacheWrite + output,
    input: input,
    cacheRead: cacheRead,
    cacheWrite: cacheWrite,
    output: output
  };
}
function fmtEffectiveRate(component, tokens) {
  const count = Number(tokens || 0);
  if (!Number.isFinite(count) || count <= 0) return '0';
  return fmtFormulaCredits(Number(component || 0) * 10000 / count);
}
function fmtEffectiveRMBPricePerM(cost, tokens) {
  const count = Number(tokens || 0);
  if (!Number.isFinite(count) || count <= 0) return '0';
  return fmtRMB(Number(cost || 0) * 1000000 / count);
}
function fmtRMB(value) {
  const n = Number(value || 0);
  if (!Number.isFinite(n) || Math.abs(n) < 0.0000005) return '0';
  return n.toFixed(n >= 100 ? 2 : 4).replace(/0+$/, '').replace(/\.$/, '') || '0';
}
function coverageCredits(value) {
  const n = Number(value || 0);
  return Number.isFinite(n) ? n : 0;
}
function rmbCoverageDetails(usage) {
  const data = usage || {};
  const snapshotRequests = Number(data.rmb_pricing_snapshot_requests || 0);
  if (!Number.isFinite(snapshotRequests) || snapshotRequests <= 0) {
    return { available: false, partial: false, text: ust('creditsTooltipRMBUnavailable') };
  }
  const pricedCredits = coverageCredits(data.rmb_priced_credits);
  const totalCredits = coverageCredits(data.credits);
  const pricedNormalInputTokens = Math.max(0, Number(data.priced_normal_input_tokens || 0));
  const pricedCacheReadTokens = Math.max(0, Number(data.priced_cache_read_tokens || 0));
  const pricedCacheWriteTokens = Math.max(0, Number(data.priced_cache_write_tokens || 0));
  const pricedOutputTokens = Math.max(0, Number(data.rmb_priced_output_tokens || 0));
  const inputTokens = Math.max(0, Number(data.input_tokens || 0));
  const cacheReadTokens = Math.min(inputTokens, Math.max(0, Number(data.cached_input_tokens || 0)));
  const cacheWriteTokens = Math.min(inputTokens - cacheReadTokens, Math.max(0, Number(data.cache_write_tokens || 0)));
  const normalInputTokens = inputTokens - cacheReadTokens - cacheWriteTokens;
  const outputTokens = Math.max(0, Number(data.output_tokens || 0));
  const requests = Math.max(0, Number(data.requests || 0));
  const pricedRequests = Math.max(0, Number(data.rmb_priced_requests || 0));
  const partial = pricedRequests < requests || pricedCredits < totalCredits - 0.0000005;
  return {
    available: true,
    partial: partial,
    pricedNormalInputTokens: Number.isFinite(pricedNormalInputTokens) ? pricedNormalInputTokens : 0,
    pricedCacheReadTokens: Number.isFinite(pricedCacheReadTokens) ? pricedCacheReadTokens : 0,
    pricedCacheWriteTokens: Number.isFinite(pricedCacheWriteTokens) ? pricedCacheWriteTokens : 0,
    pricedOutputTokens: Number.isFinite(pricedOutputTokens) ? pricedOutputTokens : 0,
    text: ust('creditsTooltipRMBCoverage', {
      pricedRequests: fmtInt(pricedRequests),
      requests: fmtInt(requests),
      pricedNormalInputTokens: fmtInt(pricedNormalInputTokens),
      normalInputTokens: fmtInt(normalInputTokens),
      pricedCacheReadTokens: fmtInt(pricedCacheReadTokens),
      cacheReadTokens: fmtInt(cacheReadTokens),
      pricedCacheWriteTokens: fmtInt(pricedCacheWriteTokens),
      cacheWriteTokens: fmtInt(cacheWriteTokens),
      pricedOutputTokens: fmtInt(pricedOutputTokens),
      outputTokens: fmtInt(outputTokens),
      pricedCredits: fmtFormulaCredits(pricedCredits),
      credits: fmtFormulaCredits(totalCredits),
      missingCredits: fmtFormulaCredits(Math.max(0, totalCredits - pricedCredits))
    })
  };
}
// RMB is an auditable settlement reference.  It is always the four frozen
// directional components added together; legacy token-only records remain
// outside this total instead of being extrapolated from an unrelated sample.
function rmbCostDetails(usage) {
  const data = usage || {};
  const coverage = rmbCoverageDetails(data);
  const nonNegativeCost = function(value) {
    const amount = Number(value || 0);
    return Number.isFinite(amount) && amount > 0 ? amount : 0;
  };
  if (!coverage.available) {
    return { available: false, total: 0, text: coverage.text };
  }
  return {
    available: true,
    partial: coverage.partial,
    input: nonNegativeCost(data.input_cost_rmb),
    cacheRead: nonNegativeCost(data.cache_read_cost_rmb),
    cacheWrite: nonNegativeCost(data.cache_write_cost_rmb),
    output: nonNegativeCost(data.output_cost_rmb),
    total: nonNegativeCost(data.input_cost_rmb) + nonNegativeCost(data.cache_read_cost_rmb) + nonNegativeCost(data.cache_write_cost_rmb) + nonNegativeCost(data.output_cost_rmb),
    text: coverage.text
  };
}
function usageRMBValue(usage) {
  const cost = rmbCostDetails(usage);
  if (!cost.available) return '-';
  const value = '\u00a5' + fmtRMB(cost.total);
  return cost.partial ? ust('rmbReferencePartial', { value: value }) : value;
}
function creditMultiplierDetails(usage) {
  const records = Array.isArray(usage && usage.provider_multipliers) ? usage.provider_multipliers : [];
  const lines = records.map(function(record) {
    const source = String(record && record.multiplier_source || '').toLowerCase();
    return {
      kind: 'multiplier',
      text: ust('creditsTooltipMultiplier', {
        provider: String(record && (record.provider_name || record.provider_id) || '-'),
        label: ust(source === 'provider' ? 'creditsTooltipProviderMultiplier' : 'creditsTooltipServiceGroupMultiplier'),
        multiplier: fmtFormulaCredits(record && record.multiplier)
      })
    };
  });
  if (records.length) return lines;
  lines.push({ kind: 'note', text: ust('creditsTooltipMultiplierUnavailable') });
  const currentBilling = Array.isArray(usage && usage.current_provider_billing)
    ? usage.current_provider_billing
    : (Array.isArray(usageStatsCache && usageStatsCache.current_provider_billing) ? usageStatsCache.current_provider_billing : []);
  currentBilling.forEach(function(record) {
    const multiplier = Number(record && record.current_multiplier);
    if (!Number.isFinite(multiplier) || multiplier <= 0) return;
    lines.push({
      kind: 'multiplier-reference',
      text: ust('creditsTooltipCurrentProviderMultiplier', {
        provider: String(record && (record.provider_name || record.provider_id) || '-'),
        multiplier: fmtFormulaCredits(multiplier)
      })
    });
  });
  return lines;
}
function providerPricingDetails(usage) {
  const records = Array.isArray(usage && usage.provider_pricing) ? usage.provider_pricing : [];
  return records.map(function(record) {
    return {
      kind: 'provider-pricing',
      text: ust('creditsTooltipProviderBasePricing', {
        provider: String(record && (record.provider_name || record.provider_id) || '-'),
        inputCredits: fmtFormulaCredits(record && record.input_credits_per_10k),
        outputCredits: fmtFormulaCredits(record && record.output_credits_per_10k),
        inputRMB: fmtRMB(Number(record && record.input_rmb_per_10k || 0) * 100),
        outputRMB: fmtRMB(Number(record && record.output_rmb_per_10k || 0) * 100),
        cacheReadCredits: fmtFormulaCredits(record && record.cache_read_credits_per_10k),
        cacheWriteCredits: fmtFormulaCredits(record && record.cache_write_credits_per_10k),
        cacheReadRMB: fmtRMB(Number(record && record.cache_read_rmb_per_10k || 0) * 100),
        cacheWriteRMB: fmtRMB(Number(record && record.cache_write_rmb_per_10k || 0) * 100)
      })
    };
  });
}
function fmtDuration(seconds) {
  const total = Math.max(0, Number(seconds || 0));
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  if (hours > 0) return hours + 'h ' + minutes + ' Min';
  return minutes + ' Min';
}
function effectiveRMBRateLabel(recordedCost, pricedTokens) {
  if (!Number.isFinite(Number(pricedTokens)) || Number(pricedTokens) <= 0) return ust('creditsTooltipRMBRateUnavailable');
  return fmtEffectiveRMBPricePerM(recordedCost, pricedTokens);
}
// Shared by the credits tooltip and the RMB summary-card tooltip so both audit
// views quote identical effective rates and the same four-component breakdown.
function rmbRateBreakdownLines(rmbCoverage, rmbCost) {
  if (!rmbCoverage.available) return [];
  return [
    { kind: 'rmb-rate', text: ust('creditsTooltipRMBRates', {
      input: effectiveRMBRateLabel(rmbCost.input, rmbCoverage.pricedNormalInputTokens),
      cacheRead: effectiveRMBRateLabel(rmbCost.cacheRead, rmbCoverage.pricedCacheReadTokens),
      cacheWrite: effectiveRMBRateLabel(rmbCost.cacheWrite, rmbCoverage.pricedCacheWriteTokens),
      output: effectiveRMBRateLabel(rmbCost.output, rmbCoverage.pricedOutputTokens)
    }) },
    { kind: 'rmb', text: ust('creditsTooltipRMB', {
      input: fmtRMB(rmbCost.input),
      cacheRead: fmtRMB(rmbCost.cacheRead),
      cacheWrite: fmtRMB(rmbCost.cacheWrite),
      output: fmtRMB(rmbCost.output),
      total: fmtRMB(rmbCost.total)
    }) }
  ];
}
function creditCalculationDetails(usage) {
  const data = usage || {};
  const normalInputTokens = Number(data.priced_normal_input_tokens || 0);
  const cacheReadTokens = Number(data.priced_cache_read_tokens || 0);
  const cacheWriteTokens = Number(data.priced_cache_write_tokens || 0);
  const outputTokens = Number(data.priced_output_tokens || 0);
  const inputRate = fmtEffectiveRate(data.credit_normal_input_component, normalInputTokens);
  const cacheReadRate = fmtEffectiveRate(data.credit_cache_read_component, cacheReadTokens);
  const cacheWriteRate = fmtEffectiveRate(data.credit_cache_write_component, cacheWriteTokens);
  const outputRate = fmtEffectiveRate(data.credit_output_component, outputTokens);
  const rmbCoverage = rmbCoverageDetails(data);
  const rmbCost = rmbCostDetails(data);
  const adjustments = {
    minimum: Number(data.credit_minimum_adjustment || 0),
    rounding: Number(data.credit_rounding_adjustment || 0),
    unitemized: Number(data.credit_unitemized_component || 0)
  };
  const lines = [
    { kind: 'title', text: ust('creditsTooltipTitle') },
    { kind: 'note', text: ust('creditsTooltipScope') },
    ...(normalInputTokens > 0 ? [{ kind: 'line', text: ust('creditsTooltipInputLine', {
      tokens: fmtInt(normalInputTokens),
      inputRate: inputRate,
      rate: inputRate,
      credits: fmtFormulaCredits(data.credit_normal_input_component)
    }) }] : []),
    ...(cacheReadTokens > 0 ? [{ kind: 'line', text: ust('creditsTooltipCacheReadLine', { tokens: fmtInt(cacheReadTokens), rate: cacheReadRate, credits: fmtFormulaCredits(data.credit_cache_read_component) }) }] : []),
    ...(cacheWriteTokens > 0 ? [{ kind: 'line', text: ust('creditsTooltipCacheWriteLine', { tokens: fmtInt(cacheWriteTokens), rate: cacheWriteRate, credits: fmtFormulaCredits(data.credit_cache_write_component) }) }] : []),
    ...(outputTokens > 0 ? [{ kind: 'line', text: ust('creditsTooltipOutputLine', {
      tokens: fmtInt(outputTokens),
      rate: outputRate,
      credits: fmtFormulaCredits(data.credit_output_component)
    }) }] : [])
  ];
  if (Math.abs(adjustments.minimum) >= 0.0000005 || Math.abs(adjustments.rounding) >= 0.0000005 || Math.abs(adjustments.unitemized) >= 0.0000005) {
    lines.push({ kind: 'line', text: ust('creditsTooltipAdjustments', {
      minimum: fmtFormulaCredits(adjustments.minimum),
      rounding: fmtFormulaCredits(adjustments.rounding),
      unitemized: fmtFormulaCredits(adjustments.unitemized)
    }) });
  }
  // The unitemized share settles legacy token-count requests whose tokens are
  // part of the row totals but not of any directional line above. Show its
  // scope explicitly so the tooltip visibly reconciles with the row.
  if (adjustments.unitemized > 0.0000005) {
    // Rows can contain legacy settlements written before directional pricing
    // existed. Their token counters are still present, but the explicit
    // unitemized scope fields are absent in those historical records. Derive
    // the uncovered residual from the row totals and frozen-price coverage so
    // cache-read tokens are not silently omitted from the explanation.
    const totalInputTokens = Math.max(0, Number(data.input_tokens || 0));
    const totalCacheReadTokens = Math.min(totalInputTokens, Math.max(0, Number(data.cached_input_tokens || 0)));
    const totalCacheWriteTokens = Math.min(totalInputTokens - totalCacheReadTokens, Math.max(0, Number(data.cache_write_tokens || 0)));
    const pricedInputTokens = Math.max(0, Number(data.priced_normal_input_tokens || 0)) + Math.max(0, Number(data.priced_cache_read_tokens || 0)) + Math.max(0, Number(data.priced_cache_write_tokens || 0));
    const residualCacheRead = Math.max(0, totalCacheReadTokens - Math.max(0, Number(data.priced_cache_read_tokens || 0)));
    const residualCacheWrite = Math.max(0, totalCacheWriteTokens - Math.max(0, Number(data.priced_cache_write_tokens || 0)));
    const unitemizedCacheRead = Math.max(0, Number(data.unitemized_cached_input_tokens || 0)) || residualCacheRead;
    const unitemizedCacheWrite = Math.max(0, Number(data.unitemized_cache_write_tokens || 0)) || residualCacheWrite;
    const unitemizedInputTotal = Math.max(0, Number(data.unitemized_input_tokens || 0)) || Math.max(0, totalInputTokens - pricedInputTokens);
    const unitemizedInput = Math.max(0, unitemizedInputTotal - unitemizedCacheRead - unitemizedCacheWrite);
    const unitemizedOutput = Math.max(0, Number(data.unitemized_output_tokens || 0)) || Math.max(0, Number(data.output_tokens || 0) - Math.max(0, Number(data.priced_output_tokens || 0)));
    const unitemizedRequests = Math.max(0, Number(data.unitemized_requests || 0)) || Math.max(0, Number(data.requests || 0) - Number(data.rmb_priced_requests || 0));
    lines.push({ kind: 'note', text: ust('creditsTooltipUnitemizedLine', {
      credits: fmtFormulaCredits(adjustments.unitemized),
      requests: fmtInt(unitemizedRequests),
      input: fmtInt(unitemizedInput),
      cacheRead: fmtInt(unitemizedCacheRead),
      cacheWrite: fmtInt(unitemizedCacheWrite),
      output: fmtInt(unitemizedOutput)
    }) });
    const directional = unitemizedDirectionalCreditEstimate(data, unitemizedInput, unitemizedCacheRead, unitemizedCacheWrite, unitemizedOutput);
    if (directional && directional.credits > 0.0000005) {
      lines.push({ kind: 'note', text: ust('creditsTooltipUnitemizedDirectionalEstimate', {
        credits: fmtFormulaCredits(directional.credits),
        input: fmtFormulaCredits(directional.input),
        cacheRead: fmtFormulaCredits(directional.cacheRead),
        cacheWrite: fmtFormulaCredits(directional.cacheWrite),
        output: fmtFormulaCredits(directional.output)
      }) });
    }
  }
  Array.prototype.push.apply(lines, providerPricingDetails(data));
  Array.prototype.push.apply(lines, creditMultiplierDetails(data));
  lines.push({ kind: 'rmb-coverage', text: rmbCoverage.text });
  lines.push(
    // Keep the tooltip total at the same precision as every component above it.
    // The compact metric card still uses fmtCredits, while this audit view must
    // visibly reconcile even for sub-milli-credit settlements.
    { kind: 'total', text: ust('creditsTooltipActualTotal', { credits: fmtFormulaCredits(data.credits) }) },
    ...rmbRateBreakdownLines(rmbCoverage, rmbCost),
    { kind: 'rule', text: ust('creditsTooltipFormula') }
  );
  return lines;
}
function usageCreditsLabel(usage) {
  const calculation = creditCalculationDetails(usage);
  const details = calculation.map(function (line) { return line.text; }).join('. ');
  const encoded = encodeURIComponent(JSON.stringify(calculation));
  return '<span class="usage-credit-label">' + escapeHtml(ust('colCredits')) + '<button class="usage-credit-info" type="button" aria-expanded="false" aria-label="' + escapeHtml(details) + '" data-credit-details="' + encoded + '" onclick="toggleUsageCreditTooltip(event)" onkeydown="onUsageCreditTooltipKeydown(event)">i</button></span>';
}
function usageRMBDetails(usage) {
  const data = usage || {};
  const coverage = rmbCoverageDetails(data);
  return [
    { kind: 'title', text: ust('summaryCostRMB') },
    { kind: 'rmb-coverage', text: coverage.text },
    ...rmbRateBreakdownLines(coverage, rmbCostDetails(data))
  ];
}
function usageRMBLabel(usage) {
  const details = usageRMBDetails(usage);
  const ariaLabel = details.map(function (line) { return line.text; }).join('. ');
  const encoded = encodeURIComponent(JSON.stringify(details));
  return '<span class="usage-credit-label">' + escapeHtml(ust('summaryCostRMB')) + '<button class="usage-credit-info" type="button" aria-expanded="false" aria-label="' + escapeHtml(ariaLabel) + '" data-credit-details="' + encoded + '" onclick="toggleUsageCreditTooltip(event)" onkeydown="onUsageCreditTooltipKeydown(event)">i</button></span>';
}
function dismissUsageCreditTooltip() {
  const popover = document.getElementById('usageCreditPopover');
  if (popover) popover.remove();
  document.querySelectorAll('.usage-credit-info[aria-expanded="true"]').forEach(function (item) {
    item.setAttribute('aria-expanded', 'false');
    item.removeAttribute('aria-describedby');
  });
}
function positionUsageCreditTooltip(popover, trigger) {
  const rect = trigger.getBoundingClientRect();
  const gap = 8;
  const viewportPadding = 12;
  const maxWidth = Math.min(440, window.innerWidth - viewportPadding * 2);
  popover.style.width = maxWidth + 'px';
  const height = popover.offsetHeight;
  const placeBelow = rect.top - gap - height < viewportPadding && rect.bottom + gap + height <= window.innerHeight - viewportPadding;
  const top = placeBelow ? rect.bottom + gap : Math.max(viewportPadding, rect.top - gap - height);
  const left = Math.min(Math.max(viewportPadding, rect.right - maxWidth), window.innerWidth - maxWidth - viewportPadding);
  popover.style.top = top + 'px';
  popover.style.left = left + 'px';
}
function showUsageCreditTooltip(trigger) {
  dismissUsageCreditTooltip();
  let details = [];
  try { details = JSON.parse(decodeURIComponent(trigger.getAttribute('data-credit-details') || '[]')); } catch (_) { return; }
  if (!Array.isArray(details) || !details.length) return;
  const popover = document.createElement('section');
  popover.id = 'usageCreditPopover';
  popover.className = 'usage-credit-popover';
  popover.setAttribute('role', 'tooltip');
  popover.innerHTML = details.filter(function(line) { return String(line && line.text || '').trim(); }).map(function (line) {
    return '<div class="usage-credit-popover-' + escapeHtml(line.kind || 'line') + '">' + escapeHtml(line.text || '') + '</div>';
  }).join('');
  document.body.appendChild(popover);
  trigger.setAttribute('aria-expanded', 'true');
  trigger.setAttribute('aria-describedby', popover.id);
  positionUsageCreditTooltip(popover, trigger);
  requestAnimationFrame(function () { popover.classList.add('is-visible'); });
}
function toggleUsageCreditTooltip(event) {
  const trigger = event && event.currentTarget;
  if (!trigger) return;
  const shouldOpen = trigger.getAttribute('aria-expanded') !== 'true';
  dismissUsageCreditTooltip();
  if (shouldOpen) showUsageCreditTooltip(trigger);
}
function onUsageCreditTooltipKeydown(event) {
  if (!event) return;
  if (event.key === 'Escape') {
    dismissUsageCreditTooltip();
    event.currentTarget.focus();
    return;
  }
}
if (!window.__usageCreditTooltipDismissBound) {
  window.__usageCreditTooltipDismissBound = true;
  document.addEventListener('pointerdown', function (event) {
    const target = event.target;
    if (target && target.closest && (target.closest('#usageCreditPopover') || target.closest('.usage-credit-info'))) return;
    dismissUsageCreditTooltip();
  });
  window.addEventListener('resize', dismissUsageCreditTooltip);
  window.addEventListener('scroll', dismissUsageCreditTooltip, true);
}
function isRankingEmail(value) {
  const email = String(value || '').trim();
  return email.split('@').length === 2 && !/\s/.test(email) && !email.startsWith('@') && !email.endsWith('@');
}
function usageMetricCard(label, value, hint, labelHTML) {
  return '<div class="metric" style="padding:12px 13px"><label>' + (labelHTML || escapeHtml(label)) + '</label><strong>' + escapeHtml(value) + '</strong>' + (hint ? ('<span>' + escapeHtml(hint) + '</span>') : '') + '</div>';
}
function ensureUsageStatsUI() {
  if (document.getElementById('usageStatsRoot')) return;
  const tab = document.getElementById('tab-usagestats');
  if (!tab) return;
  if (!document.getElementById('userRankingStyles')) {
    const style = document.createElement('style');
    style.id = 'userRankingStyles';
    style.textContent = '#userRankingCards{align-items:stretch}.usage-stats-subtabs{display:inline-flex!important;gap:4px!important;padding:3px!important;border:1px solid #d7e2f2!important;border-radius:10px!important;background:#f5f8fd!important}.usage-stats-subtab{position:relative!important;height:34px!important;padding:0 15px!important;border:0!important;border-radius:7px!important;background:transparent!important;color:#5f7088!important;font-weight:700!important;box-shadow:none!important;cursor:pointer!important;transition:background .16s ease,color .16s ease,box-shadow .16s ease!important}.usage-stats-subtab:hover{background:#edf4ff!important;color:#263b59!important}.usage-stats-subtab:focus-visible{outline:2px solid rgba(31,111,235,.35)!important;outline-offset:2px!important}.usage-stats-subtab.is-active{background:#1f6feb!important;color:#fff!important;box-shadow:0 1px 2px rgba(23,70,130,.18)!important}.user-ranking-card{min-width:0;height:100%;padding:12px 13px!important;gap:8px!important;display:grid!important;grid-template-rows:22px 1fr}.user-ranking-card-title{display:flex;align-items:center;min-width:0;height:22px;font-size:13px;line-height:22px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.user-ranking-card-metrics{display:grid!important;grid-template-columns:repeat(2,minmax(0,1fr))!important;grid-auto-rows:44px;gap:6px!important;align-items:stretch}.user-ranking-card-metrics .usage-rank-chip{min-height:44px;display:flex;flex-direction:column;justify-content:center}.user-ranking-card-metrics .usage-rank-label,.user-ranking-card-metrics .usage-rank-value{line-height:1.15}.usage-credit-label{display:inline-flex;align-items:center;gap:5px}.usage-credit-info{display:inline-flex;align-items:center;justify-content:center;width:16px;height:16px;padding:0;border:1px solid #9aa8ba;border-radius:50%;background:#fff;color:#52657d;font:700 10px/1 system-ui,sans-serif;cursor:pointer}.usage-credit-info:hover{border-color:#1f6feb;color:#1f6feb}.usage-credit-info:focus-visible{outline:2px solid rgba(31,111,235,.45);outline-offset:2px}.usage-credit-popover{position:fixed;z-index:40;max-height:calc(100vh - 24px);overflow:auto;padding:12px;border:1px solid #cbd5e1;border-radius:10px;background:#fff;color:#243247;box-shadow:0 6px 8px rgba(15,23,42,.16);font-size:12px;line-height:1.5;opacity:0;transform:translateY(3px);transition:opacity .16s ease,transform .16s ease}.usage-credit-popover.is-visible{opacity:1;transform:translateY(0)}.usage-credit-popover-title{margin-bottom:5px;font-weight:700;color:#16243a}.usage-credit-popover-note{margin-bottom:8px;color:#52657d}.usage-credit-popover-line{padding:2px 0}.usage-credit-popover-multiplier{margin-top:4px;color:#345675;font-weight:600}.usage-credit-popover-total{margin-top:7px;padding-top:7px;border-top:1px solid #e2e8f0;font-weight:700;color:#16243a}.usage-credit-popover-rmb-rate{margin-top:7px;color:#345675;font-weight:600}.usage-credit-popover-rmb{margin-top:5px;padding:7px 8px;border-radius:7px;background:#f3f7fd;color:#23456f;font-weight:700}.usage-credit-popover-rule{margin-top:8px;color:#52657d;font-size:11px;line-height:1.45}@media(max-width:1180px){#userRankingCards{grid-template-columns:repeat(2,minmax(0,1fr))!important}}@media(max-width:760px){#userRankingCards{grid-template-columns:1fr!important}.usage-stats-subtabs{display:flex!important;width:100%!important}.usage-stats-subtab{flex:1 1 0!important}}@media(prefers-reduced-motion:reduce){.usage-credit-popover{transition:none}}';
    // The admin's general button rule has a 38px minimum height. Keep this
    // compact inline trigger immune to that rule while preserving focus styling.
    style.textContent += '.usage-credit-label{gap:4px}.usage-credit-info{box-sizing:border-box!important;flex:0 0 14px!important;align-self:center!important;width:14px!important;min-width:14px!important;height:14px!important;min-height:14px!important;max-height:14px!important;padding:0!important;font-size:9px!important;line-height:12px!important;vertical-align:middle!important}.usage-credit-popover-multiplier-reference{margin-top:4px;color:#52657d;font-weight:600}.usage-credit-popover-provider-pricing{margin-top:5px;padding:5px 7px;border-radius:7px;background:#f7fafc;color:#345675;font-weight:600}.usage-credit-popover-rmb-coverage{margin-top:7px;padding:6px 7px;border:1px solid #d7e2f2;border-radius:7px;background:#f8fbff;color:#345675;font-size:11px;line-height:1.45}.usage-credit-popover-rmb-estimate{margin-top:5px;padding:7px 8px;border-radius:7px;background:#fff8e6;color:#76500a;font-weight:700;line-height:1.45}';
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
    '<div id="usageStatsReconciliation" class="item hidden" style="margin-top:10px;padding:10px 14px"></div>' +
    '<div class="usage-stats-detail-grid">' +
    '<div class="usage-stats-detail-main">' +
    '<div class="item" style="padding:12px 14px"><div class="item-title" data-icon="chart" style="font-size:14px" id="usageStatsTrendTitle"></div><div id="usageStatsTrend" style="margin-top:8px"></div></div>' +
    '<div class="item usage-stats-system-user" id="usageStatsSystemUserWrap" style="padding:12px 14px"><div class="item-title" data-icon="list" style="font-size:14px" id="usageStatsSystemUserTitle"></div><div id="usageStatsSystemUser" style="margin-top:8px"></div></div>' +
    '</div>' +
    '<div class="item" style="padding:12px 14px"><div class="item-title" data-icon="list" style="font-size:14px" id="usageStatsRowsTitle"></div><div id="usageStatsRows" style="margin-top:8px"></div></div>' +
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
  _s('usageStatsSystemUserTitle', 'textContent', ust('systemUserTitle'));
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
  dismissUsageCreditTooltip();
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
function isSystemLLMUserRow(row) {
  const id = String(row && row.id || '').trim().toLowerCase();
  const name = String(row && row.name || '').trim().toLowerCase();
  return id === 'sys_user' || name === 'sys_user';
}
function usageRankRowHTML(row, index) {
  const name = escapeHtml((row && (row.name || row.id)) || '-');
  const indexHTML = index == null ? '' : '<span class="usage-rank-index">' + index + '</span>';
  return '' +
    '<div class="usage-rank-row">' +
      '<div class="usage-rank-main">' +
        '<div class="usage-rank-name">' + indexHTML + '<div style="min-width:0"><span class="usage-rank-label">' + ust('colName') + '</span><span class="usage-rank-value mono" title="' + name + '" style="overflow:hidden;text-overflow:ellipsis">' + name + '</span></div></div>' +
        '<div><span class="usage-rank-label">' + ust('colTotal') + '</span><span class="usage-rank-value">' + fmtInt(row && row.total_tokens) + '</span></div>' +
        '<div><span class="usage-rank-label">' + ust('colCacheRate') + '</span><span class="usage-rank-value ok">' + fmtPercent(row && row.cached_requests, row && row.requests) + '</span></div>' +
        '<div><span class="usage-rank-label">' + ust('colRequests') + '</span><span class="usage-rank-value">' + fmtInt(row && row.cached_requests) + ' / ' + fmtInt(row && row.requests) + '</span></div>' +
      '</div>' +
      '<div class="usage-rank-sub">' +
        '<div class="usage-rank-chip"><span class="usage-rank-label">' + ust('colInput') + '</span><span class="usage-rank-value">' + fmtInt(row && row.input_tokens) + '</span></div>' +
        '<div class="usage-rank-chip"><span class="usage-rank-label">' + ust('colOutput') + '</span><span class="usage-rank-value">' + fmtInt(row && row.output_tokens) + '</span></div>' +
        '<div class="usage-rank-chip"><span class="usage-rank-label">' + ust('colCacheRead') + '</span><span class="usage-rank-value cache">' + fmtInt(row && row.cached_input_tokens) + '</span></div>' +
        '<div class="usage-rank-chip"><span class="usage-rank-label">' + ust('colCacheWrite') + '</span><span class="usage-rank-value cache">' + fmtInt(row && row.cache_write_tokens) + '</span></div>' +
        '<div class="usage-rank-chip"><span class="usage-rank-label">' + usageCreditsLabel(row || {}) + '</span><span class="usage-rank-value">' + fmtCredits(row && row.credits) + '</span></div>' +
        '<div class="usage-rank-chip"><span class="usage-rank-label">' + ust('colCostRMB') + '</span><span class="usage-rank-value">' + usageRMBValue(row || {}) + '</span></div>' +
      '</div>' +
    '</div>';
}
function renderUsageSystemUser() {
  const wrap = document.getElementById('usageStatsSystemUserWrap');
  const root = document.getElementById('usageStatsSystemUser');
  if (!root) {
    if (wrap) wrap.classList.add('hidden');
    return;
  }
  const show = usageStatsState.scope === 'user' && !String(usageStatsState.entity || '').trim();
  if (wrap) wrap.classList.toggle('hidden', !show);
  if (!show) {
    root.innerHTML = '';
    return;
  }
  let row = usageStatsCache && usageStatsCache.system_user;
  if (!row) {
    const rows = usageStatsCache && usageStatsCache.rows || [];
    for (let i = 0; i < rows.length; i++) {
      if (isSystemLLMUserRow(rows[i])) {
        row = rows[i];
        break;
      }
    }
  }
  if (!row) row = { id: 'sys_user', name: 'sys_user' };
  root.innerHTML = '<div class="usage-rank-list">' + usageRankRowHTML(row, '') + '</div>';
}
function renderUsageRows() {
  const root = document.getElementById('usageStatsRows');
  if (!root) return;
  const filteredEntity = String(usageStatsState.entity || '').trim();
  const rows = (usageStatsCache && usageStatsCache.rows || []).filter(function(row) {
    if (usageStatsState.scope !== 'user' || filteredEntity) return true;
    return !isSystemLLMUserRow(row);
  });
  if (!rows.length) {
    root.innerHTML = '<div class="usage-rank-empty">' + ust('rowsEmpty') + '</div>';
    return;
  }
  const body = rows.slice(0, 20).map(function(row, index) {
    return usageRankRowHTML(row, index + 1);
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
    usageMetricCard(ust('summaryCacheAnomalies'), fmtInt(s.usage_anomaly_count), ust('summaryCacheAnomalies')),
    usageMetricCard(ust('summaryRequests'), fmtInt(s.cached_requests) + ' / ' + fmtInt(s.requests), ust('summaryRequests')),
    usageMetricCard(ust('summaryCredits'), fmtCredits(s.credits), ust('summaryCredits'), usageCreditsLabel(s)),
    usageMetricCard(ust('summaryCostRMB'), usageRMBValue(s), ust('summaryCostRMB'), usageRMBLabel(s))
  ].join('');
}
function renderUsageReconciliation() {
  const root = document.getElementById('usageStatsReconciliation');
  if (!root) return;
  const visible = usageStatsState.period === 'daily' && usageStatsState.scope === 'user' && !usageStatsState.entity;
  root.classList.toggle('hidden', !visible);
  if (!visible) return;
  const reconciliation = usageStatsCache && usageStatsCache.reconciliation;
  if (!reconciliation || reconciliation.status === 'unavailable' || !reconciliation.hubcenter) {
    root.innerHTML = '<div class="item-title" style="font-size:14px">' + escapeHtml(ust('reconciliationTitle')) + '</div><div class="hint" style="margin-top:5px">' + escapeHtml(ust('reconciliationUnavailableDetail', { message: String(reconciliation && reconciliation.message || ust('reconciliationUnavailable')) })) + '</div>';
    return;
  }
  const difference = reconciliation.difference || {};
  const status = String(reconciliation.status || 'unavailable');
  const statusLabel = status === 'matched' ? ust('reconciliationMatched') : (status === 'mismatch' ? ust('reconciliationMismatch') : ust('reconciliationUnavailable'));
  const statusClass = status === 'matched' ? 'ok' : (status === 'mismatch' ? 'warn' : '');
  function reconCredits(value) {
    const n = Number(value || 0);
    return Number.isFinite(n) ? fmtCredits(n) : fmtCredits(0);
  }
  function reconUpstreamCredits(diff) {
    if (!diff || !diff.upstream_credits_comparable) return ust('reconciliationCreditsNA');
    return reconCredits(diff.upstream_credits);
  }
  const groups = Array.isArray(reconciliation.service_groups) ? reconciliation.service_groups : [];
  const groupRows = groups.map(function(row) {
    const groupDiff = row && row.difference || {};
    const groupStatus = String(row && row.status || 'unavailable');
    const groupStatusLabel = groupStatus === 'matched' ? ust('reconciliationMatched') : (groupStatus === 'mismatch' ? ust('reconciliationMismatch') : ust('reconciliationUnavailable'));
    const groupName = String(row && row.service_group_id || '').trim() || ust('reconciliationGroupUnspecified');
    const key = groupStatus === 'unavailable' ? 'reconciliationGroupUpstreamOnly' : 'reconciliationGroupRow';
    return '<div class="hint" style="margin-top:4px">' + escapeHtml(ust(key, {
      group: groupName + ' (' + groupStatusLabel + ')',
      hubIn: fmtInt(row && row.hub && row.hub.input_tokens),
      hubOut: fmtInt(row && row.hub && row.hub.output_tokens),
      hubCredits: reconCredits(row && row.hub && row.hub.credits),
      upIn: fmtInt(row && row.hubcenter && row.hubcenter.input_tokens),
      upOut: fmtInt(row && row.hubcenter && row.hubcenter.output_tokens),
      upCredits: reconCredits(row && row.hubcenter && row.hubcenter.total_credits),
      diffIn: fmtInt(groupDiff.input_tokens),
      diffOut: fmtInt(groupDiff.output_tokens),
      diffRequests: fmtInt(groupDiff.requests),
      diffUpstreamCredits: reconUpstreamCredits(groupDiff)
    })) + '</div>';
  }).join('');
  root.innerHTML = '<div class="item-title" style="font-size:14px">' + escapeHtml(ust('reconciliationTitle')) + ' <span class="' + statusClass + '" style="font-size:12px">' + escapeHtml(statusLabel) + '</span></div><div class="hint" style="margin-top:5px">' + escapeHtml(ust('reconciliationTotals', {
    hubIn: fmtInt(reconciliation.hub && reconciliation.hub.input_tokens),
    hubOut: fmtInt(reconciliation.hub && reconciliation.hub.output_tokens),
    upIn: fmtInt(reconciliation.hubcenter.input_tokens),
    upOut: fmtInt(reconciliation.hubcenter.output_tokens),
    diffIn: fmtInt(difference.input_tokens),
    diffOut: fmtInt(difference.output_tokens),
    diffRequests: fmtInt(difference.requests),
    hubCredits: reconCredits(reconciliation.hub && reconciliation.hub.credits),
    upCredits: reconCredits(reconciliation.hubcenter.total_credits),
    diffUpstreamCredits: reconUpstreamCredits(difference)
  })) + '</div><div class="hint" style="margin-top:4px">' + escapeHtml(ust('reconciliationCreditsNote')) + '</div>' + (groupRows ? ('<div class="item-meta" style="margin-top:8px">' + escapeHtml(ust('reconciliationGroupsTitle')) + '</div>' + groupRows) : '');
}
function renderUsageStats() {
  dismissUsageCreditTooltip();
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
  renderUsageReconciliation();
  renderUsageTrend();
  renderUsageSystemUser();
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
    const reportPath = '/api/admin/llm/usage-report?' + params.toString();
    const requests = [
      api(reportPath),
      api('/api/admin/llm/maclaw-compute-status?refresh=1').catch(function() { return null; })
    ];
    const needsReconciliation = usageStatsState.period === 'daily' && usageStatsState.scope === 'user' && !usageStatsState.entity;
    if (needsReconciliation) requests.push(api('/api/admin/llm/usage-reconciliation?date=' + encodeURIComponent(usageStatsState.date)).catch(function() { return null; }));
    const results = await Promise.all(requests);
    usageStatsCache = results[0];
    if (usageStatsCache && results[1] && Array.isArray(results[1].provider_billing)) {
      // This is deliberately shown only as the live HubCenter configuration.
      // A current schedule must never be used to recreate a historic settlement.
      usageStatsCache.current_provider_billing = results[1].provider_billing;
    }
    if (usageStatsCache) usageStatsCache.reconciliation = results[2] || null;
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

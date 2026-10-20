import { sidebarProviderAliases } from '../config/providerCatalog';
import { contactedProfileModel, contactedProfileProviderName, type ContactedProfileSummary } from './contactedModelRoute';
import type { SidebarLLMProviderSummary, SidebarTokenUsageStat } from '../types/appShell';

export type SidebarTurnUsageProfile = '' | 'assistant' | 'coding' | 'foreground';

type SidebarTurnSummaries = {
    assistant?: ContactedProfileSummary | null;
    coding?: ContactedProfileSummary | null;
    foreground?: ContactedProfileSummary | null;
} | null | undefined;

/** Profile bucket the quick switcher edits. The foreground role is not one of these. */
export function sidebarEditorUsageProfile(activeProfile: string | undefined): '' | 'assistant' | 'coding' {
    if (activeProfile === 'coding') return 'coding';
    if (activeProfile === 'assistant') return 'assistant';
    return '';
}

/**
 * Profile bucket whose rows the workbench meter adds. Desktop chat records
 * usage on the foreground role even when that role uses the assistant
 * provider, so the meter follows the role whenever it is resolved.
 */
export function sidebarTurnUsageProfile(
    activeProfile: string | undefined,
    summaries: SidebarTurnSummaries,
): SidebarTurnUsageProfile {
    if (activeProfile === 'coding') return 'coding';
    if (summaries && (contactedProfileProviderName(summaries.foreground) || contactedProfileModel(summaries.foreground))) {
        return 'foreground';
    }
    if (activeProfile === 'assistant') return 'assistant';
    return '';
}

export const emptySidebarTokenUsage = () => ({
    input: 0,
    output: 0,
    total: 0,
    cachedInput: 0,
    cacheWrite: 0,
    requests: 0,
    cachedRequests: 0,
    localCacheRequests: 0,
    localCacheHits: 0,
    today: 0,
    week: 0,
    month: 0,
});

export const normalizeSidebarTokenUsage = (stat?: SidebarTokenUsageStat | null) => {
    const input = stat?.input_tokens ?? stat?.InputTokens ?? 0;
    const output = stat?.output_tokens ?? stat?.OutputTokens ?? 0;
    const total = stat?.total_tokens ?? stat?.TotalTokens ?? input + output;
    const cachedInput = stat?.cached_input_tokens ?? stat?.CachedInputTokens ?? 0;
    const cacheWrite = stat?.cache_write_tokens ?? stat?.CacheWriteTokens ?? 0;
    const requests = stat?.requests ?? stat?.Requests ?? 0;
    const cachedRequests = stat?.cached_requests ?? stat?.CachedRequests ?? 0;
    const localCacheRequests = stat?.local_cache_requests ?? stat?.LocalCacheRequests ?? 0;
    const localCacheHits = stat?.local_cache_hits ?? stat?.LocalCacheHits ?? 0;
    const today = stat?.today_tokens ?? stat?.TodayTokens ?? 0;
    const week = stat?.week_tokens ?? stat?.WeekTokens ?? 0;
    const month = stat?.month_tokens ?? stat?.MonthTokens ?? 0;
    return { input, output, total, cachedInput, cacheWrite, requests, cachedRequests, localCacheRequests, localCacheHits, today, week, month };
};

const addSidebarUsageRow = (
    total: ReturnType<typeof emptySidebarTokenUsage>,
    item: SidebarTokenUsageStat,
) => {
    const row = normalizeSidebarTokenUsage(item);
    total.input += row.input;
    total.output += row.output;
    total.total += row.total;
    total.cachedInput = (total.cachedInput ?? 0) + row.cachedInput;
    total.cacheWrite = (total.cacheWrite ?? 0) + row.cacheWrite;
    total.requests = (total.requests ?? 0) + row.requests;
    total.cachedRequests = (total.cachedRequests ?? 0) + row.cachedRequests;
    total.localCacheRequests = (total.localCacheRequests ?? 0) + row.localCacheRequests;
    total.localCacheHits = (total.localCacheHits ?? 0) + row.localCacheHits;
    total.today = (total.today ?? 0) + row.today;
    total.week = (total.week ?? 0) + row.week;
    total.month = (total.month ?? 0) + row.month;
    return total;
};

/** Sum post-upgrade rows for one profile and provider. Other profiles stay out. */
export const sumSidebarProfileUsage = (
    profileUsage: Record<string, SidebarTokenUsageStat> | null | undefined,
    profile: string,
    providerID: string,
) => sumSidebarProviderProfiles(profileUsage, providerID, [profile]);

/**
 * Sum rows for one provider across the given profiles. The foreground meter
 * includes assistant-tagged rows for that same provider: desktop calls were
 * stored on the assistant profile before the foreground role existed. Coding
 * rows stay on the coding meter.
 */
export const sumSidebarProviderProfiles = (
    profileUsage: Record<string, SidebarTokenUsageStat> | null | undefined,
    providerID: string,
    profiles: readonly string[],
) => {
    const wantProvider = providerID.trim();
    const wantProfiles = new Set(profiles.map((profile) => profile.trim()).filter(Boolean));
    return Object.values(profileUsage || {}).reduce((total, item) => {
        if (!item || !wantProfiles.has(String(item.profile || '').trim()) || item.provider_id !== wantProvider) return total;
        return addSidebarUsageRow(total, item);
    }, emptySidebarTokenUsage());
};

export const getSidebarUsageForProvider = (usageMap: Record<string, SidebarTokenUsageStat>, provider: string) => {
    if (!provider) return emptySidebarTokenUsage();
    const direct = usageMap[provider];
    if (direct) return normalizeSidebarTokenUsage(direct);
    for (const alias of sidebarProviderAliases[provider] || []) {
        const stat = usageMap[alias];
        if (stat) return normalizeSidebarTokenUsage(stat);
    }
    return emptySidebarTokenUsage();
};

export const hasSidebarUsage = (usageMap: Record<string, SidebarTokenUsageStat>, provider: string) => {
    return getSidebarUsageForProvider(usageMap, provider).total > 0;
};

export const selectSidebarCurrentProvider = (
    providerSummaries: SidebarLLMProviderSummary[],
    currentProviderName: string,
    usageMap: Record<string, SidebarTokenUsageStat>,
) => {
    const providerNames = providerSummaries.map((provider) => provider.name);
    if (currentProviderName && providerNames.includes(currentProviderName)) return currentProviderName;

    const providerWithUsage = providerNames.find((provider) => hasSidebarUsage(usageMap, provider));
    return providerWithUsage || currentProviderName || providerNames[0] || '';
};

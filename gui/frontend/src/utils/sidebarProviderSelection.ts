import { sidebarProviderAliases } from '../config/providerCatalog';
import type { SidebarLLMProviderSummary, SidebarTokenUsageStat } from '../types/appShell';

export const emptySidebarTokenUsage = () => ({
    input: 0,
    output: 0,
    total: 0,
    cachedInput: 0,
    cacheWrite: 0,
    requests: 0,
    cachedRequests: 0,
});

export const normalizeSidebarTokenUsage = (stat?: SidebarTokenUsageStat | null) => {
    const input = stat?.input_tokens ?? stat?.InputTokens ?? 0;
    const output = stat?.output_tokens ?? stat?.OutputTokens ?? 0;
    const total = stat?.total_tokens ?? stat?.TotalTokens ?? input + output;
    const cachedInput = stat?.cached_input_tokens ?? stat?.CachedInputTokens ?? 0;
    const cacheWrite = stat?.cache_write_tokens ?? stat?.CacheWriteTokens ?? 0;
    const requests = stat?.requests ?? stat?.Requests ?? 0;
    const cachedRequests = stat?.cached_requests ?? stat?.CachedRequests ?? 0;
    return { input, output, total, cachedInput, cacheWrite, requests, cachedRequests };
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

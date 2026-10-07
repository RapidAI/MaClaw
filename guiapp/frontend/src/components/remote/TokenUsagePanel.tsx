import { useState, useEffect, useCallback, useRef } from "react";
import {
    GetAllLLMTokenUsage,
    GetLLMTokenUsageByDay,
    ResetLLMTokenUsage,
    GetMaclawLLMProviders,
} from "../../../wailsjs/go/main/App";
import { EventsOn } from "../../../wailsjs/runtime";
import { TokenUsageTrend } from "./TokenUsageTrend";
import type { TokenUsageDayPoint } from "./tokenUsageSeries";

interface TokenUsageStat {
    input_tokens?: number;
    output_tokens?: number;
    total_tokens?: number;
    total_cost_rmb?: number;
    input_cost_rmb?: number;
    output_cost_rmb?: number;
    cache_read_cost_rmb?: number;
    cache_write_cost_rmb?: number;
    InputTokens?: number;
    OutputTokens?: number;
    TotalTokens?: number;
    TotalCostRMB?: number;
    InputCostRMB?: number;
    OutputCostRMB?: number;
    CacheReadCostRMB?: number;
    CacheWriteCostRMB?: number;
    cached_input_tokens?: number;
    cache_write_tokens?: number;
    requests?: number;
    cached_requests?: number;
    CachedInputTokens?: number;
    CacheWriteTokens?: number;
    Requests?: number;
    CachedRequests?: number;
}

const tokenUsageRefreshMs = 2000;

const providerAliases: Record<string, string[]> = {
    "智谱龙芯": ["智谱", "GLM(智谱)", "GLM (智谱)"],
    "智谱": ["智谱龙芯", "GLM(智谱)", "GLM (智谱)"],
    "GLM(智谱)": ["智谱", "智谱龙芯", "GLM (智谱)"],
    "GLM (智谱)": ["智谱", "智谱龙芯", "GLM(智谱)"],
};

type Props = { lang: string };

type ProviderState = {
    providers?: Array<{ name?: string; Name?: string }>;
    Providers?: Array<{ name?: string; Name?: string }>;
    current?: string;
    Current?: string;
} | null;

const emptyUsage = { input_tokens: 0, output_tokens: 0, total_tokens: 0, total_cost_rmb: 0, input_cost_rmb: 0, output_cost_rmb: 0, cache_read_cost_rmb: 0, cache_write_cost_rmb: 0, cached_input_tokens: 0, cache_write_tokens: 0, requests: 0, cached_requests: 0 };

const normalizeProviderState = (data?: ProviderState) => {
    const providers = (data?.providers ?? data?.Providers ?? [])
        .map((provider) => provider?.name ?? provider?.Name ?? "")
        .filter(Boolean);
    const current = data?.current ?? data?.Current ?? "";
    return { providers, current };
};

const normalizeUsage = (stat?: TokenUsageStat | null) => {
    if (!stat) return emptyUsage;
    const input = stat.input_tokens ?? stat.InputTokens ?? 0;
    const output = stat.output_tokens ?? stat.OutputTokens ?? 0;
    const total = stat.total_tokens ?? stat.TotalTokens ?? (input + output);
    const inputCost = stat.input_cost_rmb ?? stat.InputCostRMB;
    const outputCost = stat.output_cost_rmb ?? stat.OutputCostRMB;
    const cacheReadCost = stat.cache_read_cost_rmb ?? stat.CacheReadCostRMB;
    const cacheWriteCost = stat.cache_write_cost_rmb ?? stat.CacheWriteCostRMB;
    const hasDirectionalCost = [inputCost, outputCost, cacheReadCost, cacheWriteCost].some((value) => value !== undefined);
    const cost = hasDirectionalCost
        ? Number(inputCost ?? 0) + Number(cacheReadCost ?? 0) + Number(cacheWriteCost ?? 0) + Number(outputCost ?? 0)
        : stat.total_cost_rmb ?? stat.TotalCostRMB ?? 0;
    const cached = stat.cached_input_tokens ?? stat.CachedInputTokens ?? 0;
    const cacheWrite = stat.cache_write_tokens ?? stat.CacheWriteTokens ?? 0;
    const requests = stat.requests ?? stat.Requests ?? 0;
    const cachedRequests = stat.cached_requests ?? stat.CachedRequests ?? 0;
    return { input_tokens: input, output_tokens: output, total_tokens: total, total_cost_rmb: cost, input_cost_rmb: Number(inputCost ?? 0), output_cost_rmb: Number(outputCost ?? 0), cache_read_cost_rmb: Number(cacheReadCost ?? 0), cache_write_cost_rmb: Number(cacheWriteCost ?? 0), cached_input_tokens: cached, cache_write_tokens: cacheWrite, requests, cached_requests: cachedRequests };
};

const usageKeysForProvider = (provider: string) => {
    const keys: string[] = [];
    const seen = new Set<string>();
    for (const key of [provider, ...(providerAliases[provider] || [])]) {
        if (!key || seen.has(key)) continue;
        seen.add(key);
        keys.push(key);
    }
    return keys;
};

const addUsage = (sum: ReturnType<typeof normalizeUsage>, stat: ReturnType<typeof normalizeUsage>) => ({
    input_tokens: sum.input_tokens + stat.input_tokens,
    output_tokens: sum.output_tokens + stat.output_tokens,
    total_tokens: sum.total_tokens + stat.total_tokens,
    total_cost_rmb: sum.total_cost_rmb + stat.total_cost_rmb,
    input_cost_rmb: sum.input_cost_rmb + stat.input_cost_rmb,
    output_cost_rmb: sum.output_cost_rmb + stat.output_cost_rmb,
    cache_read_cost_rmb: sum.cache_read_cost_rmb + stat.cache_read_cost_rmb,
    cache_write_cost_rmb: sum.cache_write_cost_rmb + stat.cache_write_cost_rmb,
    cached_input_tokens: sum.cached_input_tokens + stat.cached_input_tokens,
    cache_write_tokens: sum.cache_write_tokens + stat.cache_write_tokens,
    requests: sum.requests + stat.requests,
    cached_requests: sum.cached_requests + stat.cached_requests,
});

const getUsageForProvider = (usageMap: Record<string, TokenUsageStat>, provider: string) => {
    if (!provider) return emptyUsage;
    const stats = usageKeysForProvider(provider)
        .map((key) => usageMap[key])
        .filter((stat): stat is TokenUsageStat => !!stat)
        .map((stat) => normalizeUsage(stat));
    if (stats.length === 0) return emptyUsage;
    return stats.reduce(addUsage);
};

const hasUsage = (usageMap: Record<string, TokenUsageStat>, provider: string) => {
    return getUsageForProvider(usageMap, provider).total_tokens > 0;
};

const getPreferredProvider = (providerNames: string[], currentProviderName: string, usageMap: Record<string, TokenUsageStat>) => {
    const providerWithUsage = providerNames.find((provider) => hasUsage(usageMap, provider));
    if (currentProviderName && providerNames.includes(currentProviderName) && (hasUsage(usageMap, currentProviderName) || !providerWithUsage)) {
        return currentProviderName;
    }
    return providerWithUsage || currentProviderName || providerNames[0] || "";
};

export function TokenUsagePanel({ lang }: Props) {
    const t = useCallback((en: string, zhHans: string, zhHant: string = zhHans) =>
        lang === "zh-Hans" ? zhHans : lang === "zh-Hant" ? zhHant : en, [lang]);

    const [providers, setProviders] = useState<string[]>([]);
    const [currentProvider, setCurrentProvider] = useState("");
    const [selectedProvider, setSelectedProvider] = useState("");
    const [usage, setUsage] = useState<typeof emptyUsage | null>(null);
    const [allUsage, setAllUsage] = useState<Record<string, TokenUsageStat>>({});
    const [dayPoints, setDayPoints] = useState<TokenUsageDayPoint[]>([]);
    const [loading, setLoading] = useState(false);
    const loadSeq = useRef(0);

    const loadData = useCallback(async (quiet = false) => {
        const seq = ++loadSeq.current;
        if (!quiet) setLoading(true);
        try {
            const [providerState, usageMap, daySeries] = await Promise.all([
                GetMaclawLLMProviders() as Promise<ProviderState>,
                GetAllLLMTokenUsage() as Promise<Record<string, TokenUsageStat> | null>,
                GetLLMTokenUsageByDay().catch(() => []) as Promise<TokenUsageDayPoint[] | null>,
            ]);
            if (seq !== loadSeq.current) return;
            const normalizedProviderState = normalizeProviderState(providerState);
            const normalizedUsageMap = usageMap || {};
            const nextProviders = normalizedProviderState.providers;
            const nextCurrent = normalizedProviderState.current || "";
            setProviders(nextProviders);
            setCurrentProvider(nextCurrent);
            setSelectedProvider((prev) => {
                const preferredProvider = getPreferredProvider(nextProviders, nextCurrent || prev, normalizedUsageMap);
                if (prev && nextProviders.includes(prev) && (hasUsage(normalizedUsageMap, prev) || !preferredProvider)) {
                    return prev;
                }
                return preferredProvider;
            });
            setAllUsage(normalizedUsageMap);
            setDayPoints(Array.isArray(daySeries) ? daySeries : []);
        } catch {
            if (seq !== loadSeq.current) return;
            setProviders([]);
            setCurrentProvider("");
            setSelectedProvider("");
            setAllUsage({});
            setDayPoints([]);
        } finally {
            if (seq === loadSeq.current) setLoading(false);
        }
    }, []);

    useEffect(() => {
        void loadData();
    }, [loadData]);

    useEffect(() => {
        let timer = 0;
        const onTokenUsageChanged = () => {
            window.clearTimeout(timer);
            timer = window.setTimeout(() => {
                void loadData(true);
            }, tokenUsageRefreshMs);
        };
        const unsubscribe = EventsOn("llm-token-usage-changed", onTokenUsageChanged);
        return () => {
            window.clearTimeout(timer);
            if (typeof unsubscribe === "function") unsubscribe();
        };
    }, [loadData]);

    useEffect(() => {
        if (!selectedProvider) {
            setUsage(emptyUsage);
            return;
        }
        setUsage(getUsageForProvider(allUsage, selectedProvider));
    }, [selectedProvider, allUsage]);

    const handleReset = async (provider: string) => {
        try {
            const keys = provider ? usageKeysForProvider(provider) : [""];
            for (const key of keys) {
                await ResetLLMTokenUsage(key);
            }
            await loadData();
        } catch { /* ignore */ }
    };

    const formatTokens = (n: number) => {
        if (n >= 1_000_000) return (n / 1_000_000).toFixed(2) + "M";
        if (n >= 1_000) return (n / 1_000).toFixed(1) + "K";
        return String(n);
    };

    const formatRMB = (n: number) => {
        if (!Number.isFinite(n)) return "0";
        return n.toFixed(n >= 100 ? 2 : 4).replace(/0+$/, "").replace(/\.$/, "") || "0";
    };

    return (
        <div className="token-usage-panel">
            <div className="token-usage-panel__header">
                <span className="token-usage-panel__title">
                    {t("Token Usage Stats", "Token 用量统计", "Token 用量統計")}
                </span>
                <div className="token-usage-panel__header-actions">
                    <button onClick={() => void loadData()} disabled={loading} className="token-usage-panel__secondary-button">
                        {loading ? "..." : t("Refresh", "刷新", "重新整理")}
                    </button>
                </div>
            </div>

            <div className="token-usage-panel__select-wrap">
                <select
                    value={selectedProvider}
                    onChange={e => setSelectedProvider(e.target.value)}
                    className="token-usage-panel__select"
                >
                    {providers.map(name => (
                        <option key={name} value={name}>
                            {name}{name === currentProvider ? t(" (current)", " (当前)", " (目前)") : ""}
                        </option>
                    ))}
                </select>
            </div>

            {usage && (
                <div className="token-usage-panel__stats">
                    <div className="token-usage-panel__stat-row">
                        <span>Input Tokens</span>
                        <strong className="is-primary">{formatTokens(usage.input_tokens)}</strong>
                    </div>
                    <div className="token-usage-panel__stat-row">
                        <span>Output Tokens</span>
                        <strong className="is-primary-strong">{formatTokens(usage.output_tokens)}</strong>
                    </div>
                    {usage.cached_input_tokens > 0 && (
                        <div className="token-usage-panel__stat-row">
                            <span>{t("Cache Read", "缓存读取", "快取讀取")}</span>
                            <strong className="is-success">{formatTokens(usage.cached_input_tokens)}</strong>
                        </div>
                    )}
                    {/* Cache write is only meaningful once a provider reports it; most report
                        reads alone, so an always-zero row would read as a real measurement. */}
                    {usage.cache_write_tokens > 0 && (
                        <div className="token-usage-panel__stat-row">
                            <span>{t("Cache Write", "缓存写入", "快取寫入")}</span>
                            <strong>{formatTokens(usage.cache_write_tokens)}</strong>
                        </div>
                    )}
                    {usage.requests > 0 && (
                        <div className="token-usage-panel__stat-row">
                            <span>{t("Cache Hit Rate", "缓存命中率", "快取命中率")}</span>
                            <strong>{Math.min(100, Math.round((usage.cached_requests / usage.requests) * 100))}%</strong>
                        </div>
                    )}
                    <div className="token-usage-panel__stat-row token-usage-panel__stat-row--total">
                        <span>{t("Total", "总计", "總計")}</span>
                        <strong>{formatTokens(usage.total_tokens)}</strong>
                    </div>
                    <div className="token-usage-panel__stat-row token-usage-panel__stat-row--total-text">
                        <span>{t("Cost (RMB)", "费用（元）", "費用（元）")}</span>
                        <strong>{"\u00a5"}{formatRMB(usage.total_cost_rmb)}</strong>
                    </div>
                </div>
            )}

            <TokenUsageTrend
                lang={lang}
                provider={selectedProvider}
                points={dayPoints}
                aliases={providerAliases}
                formatTokens={formatTokens}
            />

            <div className="token-usage-panel__actions">
                <button onClick={() => handleReset(selectedProvider)} className="token-usage-panel__danger-button">
                    {t("Reset Current", "重置当前", "重置目前")}
                </button>
                <button onClick={() => handleReset("")} className="token-usage-panel__danger-button">
                    {t("Reset All", "重置全部", "重置全部")}
                </button>
            </div>
        </div>
    );
}

import { useState, useEffect, useCallback } from "react";
import {
    GetAllLLMTokenUsage,
    ResetLLMTokenUsage,
    GetMaclawLLMProviders,
} from "../../../wailsjs/go/main/App";
import { EventsOff, EventsOn } from "../../../wailsjs/runtime";
import { colors } from "./styles";

interface TokenUsageStat {
    input_tokens?: number;
    output_tokens?: number;
    total_tokens?: number;
    total_cost_rmb?: number;
    InputTokens?: number;
    OutputTokens?: number;
    TotalTokens?: number;
    TotalCostRMB?: number;
    cached_input_tokens?: number;
    cache_write_tokens?: number;
    requests?: number;
    cached_requests?: number;
    CachedInputTokens?: number;
    CacheWriteTokens?: number;
    Requests?: number;
    CachedRequests?: number;
}

const providerAliases: Record<string, string[]> = {
    "智谱龙虾": ["智谱", "GLM(智谱)", "GLM (智谱)"],
    "智谱": ["智谱龙虾", "GLM(智谱)", "GLM (智谱)"],
    "GLM(智谱)": ["智谱", "智谱龙虾", "GLM (智谱)"],
    "GLM (智谱)": ["智谱", "智谱龙虾", "GLM(智谱)"],
};

type Props = { lang: string };

type ProviderState = {
    providers?: Array<{ name?: string; Name?: string }>;
    Providers?: Array<{ name?: string; Name?: string }>;
    current?: string;
    Current?: string;
} | null;

const emptyUsage = { input_tokens: 0, output_tokens: 0, total_tokens: 0, total_cost_rmb: 0, cached_input_tokens: 0, cache_write_tokens: 0, requests: 0, cached_requests: 0 };

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
    const cost = stat.total_cost_rmb ?? stat.TotalCostRMB ?? 0;
    const cached = stat.cached_input_tokens ?? stat.CachedInputTokens ?? 0;
    const cacheWrite = stat.cache_write_tokens ?? stat.CacheWriteTokens ?? 0;
    const requests = stat.requests ?? stat.Requests ?? 0;
    const cachedRequests = stat.cached_requests ?? stat.CachedRequests ?? 0;
    return { input_tokens: input, output_tokens: output, total_tokens: total, total_cost_rmb: cost, cached_input_tokens: cached, cache_write_tokens: cacheWrite, requests, cached_requests: cachedRequests };
};

const getUsageForProvider = (usageMap: Record<string, TokenUsageStat>, provider: string) => {
    if (!provider) return emptyUsage;
    const direct = usageMap[provider];
    if (direct) return normalizeUsage(direct);
    for (const alias of providerAliases[provider] || []) {
        const stat = usageMap[alias];
        if (stat) return normalizeUsage(stat);
    }
    return emptyUsage;
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
        lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en, [lang]);

    const [providers, setProviders] = useState<string[]>([]);
    const [currentProvider, setCurrentProvider] = useState("");
    const [selectedProvider, setSelectedProvider] = useState("");
    const [usage, setUsage] = useState<typeof emptyUsage | null>(null);
    const [allUsage, setAllUsage] = useState<Record<string, TokenUsageStat>>({});
    const [loading, setLoading] = useState(false);

    const loadData = useCallback(async () => {
        setLoading(true);
        try {
            const [providerState, usageMap] = await Promise.all([
                GetMaclawLLMProviders() as Promise<ProviderState>,
                GetAllLLMTokenUsage() as Promise<Record<string, TokenUsageStat> | null>,
            ]);
            const normalizedProviderState = normalizeProviderState(providerState);
            const normalizedUsageMap = usageMap || {};
            const nextProviders = normalizedProviderState.providers;
            const nextCurrent = normalizedProviderState.current || "";
            setProviders(nextProviders);
            setCurrentProvider(nextCurrent);
            setSelectedProvider((prev) => {
                const preferredProvider = getPreferredProvider(
                    nextProviders,
                    nextCurrent || prev,
                    normalizedUsageMap,
                );
                if (prev && nextProviders.includes(prev) && (hasUsage(normalizedUsageMap, prev) || !preferredProvider)) {
                    return prev;
                }
                return preferredProvider;
            });
            setAllUsage(normalizedUsageMap);
        } catch {
            setProviders([]);
            setCurrentProvider("");
            setSelectedProvider("");
            setAllUsage({});
        } finally {
            setLoading(false);
        }
    }, []);

    useEffect(() => {
        void loadData();
    }, [loadData]);

    useEffect(() => {
        const onTokenUsageChanged = () => {
            void loadData();
        };
        EventsOn("llm-token-usage-changed", onTokenUsageChanged);
        return () => {
            EventsOff("llm-token-usage-changed");
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
            await ResetLLMTokenUsage(provider);
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

    const statRowStyle: React.CSSProperties = {
        display: "flex", justifyContent: "space-between", alignItems: "center",
        padding: "4px 0", fontSize: "0.76rem",
    };

    return (
        <div style={{
            padding: "10px 14px", borderRadius: 8,
            border: `1px solid ${colors.border}`, background: colors.surface,
            marginTop: 12,
        }}>
            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: 8 }}>
                <span style={{ fontSize: "0.78rem", fontWeight: 600, color: colors.text }}>
                    {t("Token Usage Stats", "Token 用量统计")}
                </span>
                <div style={{ display: "flex", gap: 6, alignItems: "center" }}>
                    <button onClick={() => void loadData()} disabled={loading} style={{
                        fontSize: "0.7rem", padding: "2px 8px", borderRadius: 4,
                        background: colors.bg, color: colors.textSecondary,
                        border: `1px solid ${colors.border}`, cursor: loading ? "default" : "pointer",
                    }}>
                        {loading ? "..." : t("Refresh", "刷新")}
                    </button>
                </div>
            </div>

            <div style={{ marginBottom: 8 }}>
                <select
                    value={selectedProvider}
                    onChange={e => setSelectedProvider(e.target.value)}
                    style={{
                        width: "100%", padding: "5px 8px", fontSize: "0.76rem",
                        border: `1px solid ${colors.border}`, borderRadius: 4,
                        background: colors.bg, color: colors.text,
                    }}
                >
                    {providers.map(name => (
                        <option key={name} value={name}>
                            {name}{name === currentProvider ? t(" (current)", " (当前)") : ""}
                        </option>
                    ))}
                </select>
            </div>

            {usage && (
                <div>
                    <div style={statRowStyle}>
                        <span style={{ color: colors.textSecondary }}>Input Tokens</span>
                        <span style={{ fontWeight: 600, color: "var(--theme-primary)" }}>{formatTokens(usage.input_tokens)}</span>
                    </div>
                    <div style={statRowStyle}>
                        <span style={{ color: colors.textSecondary }}>Output Tokens</span>
                        <span style={{ fontWeight: 600, color: "var(--theme-primary-strong)" }}>{formatTokens(usage.output_tokens)}</span>
                    </div>
                    {(usage.cached_input_tokens > 0 || usage.cache_write_tokens > 0) && (
                        <>
                            <div style={statRowStyle}>
                                <span style={{ color: colors.textSecondary }}>{t("Cache Read", "缓存读取")}</span>
                                <span style={{ fontWeight: 600, color: colors.success }}>{formatTokens(usage.cached_input_tokens)}</span>
                            </div>
                            <div style={statRowStyle}>
                                <span style={{ color: colors.textSecondary }}>{t("Cache Write", "缓存写入")}</span>
                                <span style={{ fontWeight: 600, color: colors.textSecondary }}>{formatTokens(usage.cache_write_tokens)}</span>
                            </div>
                        </>
                    )}
                    {usage.requests > 0 && (
                        <div style={statRowStyle}>
                            <span style={{ color: colors.textSecondary }}>{t("Cache Hit Rate", "缓存命中率")}</span>
                            <span style={{ fontWeight: 600, color: colors.text }}>
                                {Math.round((usage.cached_requests / usage.requests) * 100)}%
                            </span>
                        </div>
                    )}
                    <div style={{
                        ...statRowStyle,
                        borderTop: `1px solid ${colors.border}`, paddingTop: 6, marginTop: 2,
                    }}>
                        <span style={{ color: colors.textSecondary, fontWeight: 600 }}>{t("Total", "总计")}</span>
                        <span style={{ fontWeight: 700, fontSize: "0.82rem", color: colors.text }}>
                            {formatTokens(usage.total_tokens)}
                        </span>
                    </div>
                    <div style={statRowStyle}>
                        <span style={{ color: colors.textSecondary, fontWeight: 600 }}>{t("Cost (RMB)", "费用（元）")}</span>
                        <span style={{ fontWeight: 700, fontSize: "0.82rem", color: colors.text }}>
                            ¥{formatRMB(usage.total_cost_rmb)}
                        </span>
                    </div>
                </div>
            )}

            <div style={{ display: "flex", gap: 8, marginTop: 8, justifyContent: "flex-end" }}>
                <button onClick={() => handleReset(selectedProvider)} style={{
                    fontSize: "0.7rem", padding: "3px 10px", borderRadius: 4,
                    background: "transparent", color: colors.danger,
                    border: `1px solid ${colors.danger}`, cursor: "pointer",
                }}>
                    {t("Reset Current", "重置当前")}
                </button>
                <button onClick={() => handleReset("")} style={{
                    fontSize: "0.7rem", padding: "3px 10px", borderRadius: 4,
                    background: "transparent", color: colors.danger,
                    border: `1px solid ${colors.danger}`, cursor: "pointer",
                }}>
                    {t("Reset All", "重置全部")}
                </button>
            </div>
        </div>
    );
}

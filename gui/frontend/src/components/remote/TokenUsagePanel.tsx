import { useState, useEffect, useCallback } from "react";
import {
    GetAllLLMTokenUsage,
    ResetLLMTokenUsage,
    GetMaclawLLMProviders,
} from "../../../wailsjs/go/main/App";
import { colors } from "./styles";

interface TokenUsageStat {
    input_tokens: number;
    output_tokens: number;
    total_tokens: number;
}

type Props = { lang: string };

export function TokenUsagePanel({ lang }: Props) {
    const t = useCallback((zh: string, en: string) => lang?.startsWith("zh") ? zh : en, [lang]);

    const [providers, setProviders] = useState<string[]>([]);
    const [currentProvider, setCurrentProvider] = useState("");
    const [selectedProvider, setSelectedProvider] = useState("");
    const [usage, setUsage] = useState<TokenUsageStat | null>(null);
    const [allUsage, setAllUsage] = useState<Record<string, TokenUsageStat>>({});
    const [loading, setLoading] = useState(false);

    const loadData = useCallback(async () => {
        setLoading(true);
        try {
            const data = await GetMaclawLLMProviders();
            if (data?.providers) {
                setProviders(data.providers.map((p: any) => p.name));
                setCurrentProvider(data.current || "");
                setSelectedProvider(prev => prev || data.current || "");
            }
            const all = await GetAllLLMTokenUsage();
            setAllUsage(all || {});
        } catch { /* ignore */ }
        setLoading(false);
    }, []);

    useEffect(() => { loadData(); }, []);

    useEffect(() => {
        if (!selectedProvider) return;
        const stat = allUsage[selectedProvider];
        setUsage(stat || { input_tokens: 0, output_tokens: 0, total_tokens: 0 });
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
                    {t("Token 用量统计", "Token Usage Stats")}
                </span>
                <div style={{ display: "flex", gap: 6, alignItems: "center" }}>
                    <button onClick={() => loadData()} disabled={loading} style={{
                        fontSize: "0.7rem", padding: "2px 8px", borderRadius: 4,
                        background: colors.bg, color: colors.textSecondary,
                        border: `1px solid ${colors.border}`, cursor: loading ? "default" : "pointer",
                    }}>
                        {loading ? "..." : t("刷新", "Refresh")}
                    </button>
                </div>
            </div>

            {/* Provider selector */}
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
                            {name}{name === currentProvider ? t(" (当前)", " (current)") : ""}
                        </option>
                    ))}
                </select>
            </div>

            {/* Stats display */}
            {usage && (
                <div>
                    <div style={statRowStyle}>
                        <span style={{ color: colors.textSecondary }}>Input Tokens</span>
                        <span style={{ fontWeight: 600, color: "#3b82f6" }}>{formatTokens(usage.input_tokens)}</span>
                    </div>
                    <div style={statRowStyle}>
                        <span style={{ color: colors.textSecondary }}>Output Tokens</span>
                        <span style={{ fontWeight: 600, color: "#8b5cf6" }}>{formatTokens(usage.output_tokens)}</span>
                    </div>
                    <div style={{
                        ...statRowStyle,
                        borderTop: `1px solid ${colors.border}`, paddingTop: 6, marginTop: 2,
                    }}>
                        <span style={{ color: colors.textSecondary, fontWeight: 600 }}>{t("总计", "Total")}</span>
                        <span style={{ fontWeight: 700, fontSize: "0.82rem", color: colors.text }}>
                            {formatTokens(usage.total_tokens)}
                        </span>
                    </div>
                </div>
            )}

            {/* Reset buttons */}
            <div style={{ display: "flex", gap: 8, marginTop: 8, justifyContent: "flex-end" }}>
                <button onClick={() => handleReset(selectedProvider)} style={{
                    fontSize: "0.7rem", padding: "3px 10px", borderRadius: 4,
                    background: "transparent", color: "#ef4444",
                    border: "1px solid #fca5a5", cursor: "pointer",
                }}>
                    {t("重置当前", "Reset Current")}
                </button>
                <button onClick={() => handleReset("")} style={{
                    fontSize: "0.7rem", padding: "3px 10px", borderRadius: 4,
                    background: "transparent", color: "#ef4444",
                    border: "1px solid #fca5a5", cursor: "pointer",
                }}>
                    {t("重置全部", "Reset All")}
                </button>
            </div>
        </div>
    );
}

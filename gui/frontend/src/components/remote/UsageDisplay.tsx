import { useState, useCallback } from "react";
import { GetOpenAIUsage } from "../../../wailsjs/go/main/App";
import { colors } from "./styles";

interface UsageData {
    total_granted: number;
    total_used: number;
    total_available: number;
}

type Props = {
    lang: string;
};

export function UsageDisplay({ lang }: Props) {
    const t = useCallback((en: string, zhHans: string, zhHant: string = zhHans) =>
        lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en, [lang]);

    const [usage, setUsage] = useState<UsageData | null>(null);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState("");

    const fetchUsage = async () => {
        setLoading(true);
        setError("");
        try {
            const data = await GetOpenAIUsage();
            setUsage(data);
        } catch (e) {
            setError(String(e));
        }
        setLoading(false);
    };

    return (
        <div style={{
            padding: "10px 14px", borderRadius: 8,
            border: `1px solid ${colors.border}`, background: colors.surfaceMuted,
        }}>
            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: 8 }}>
                <span style={{ fontSize: "0.78rem", fontWeight: 600, color: colors.text }}>
                    {t("OpenAI Usage", "OpenAI 用量")}
                </span>
                <button onClick={fetchUsage} disabled={loading} style={{
                    fontSize: "0.72rem", padding: "3px 10px", borderRadius: 4,
                    background: loading ? colors.surfaceMuted : colors.primary, color: loading ? colors.textMuted : "#fff",
                    border: "none", cursor: loading ? "default" : "pointer",
                }}>
                    {loading ? t("Loading...", "查询中...") : t("Check", "查询")}
                </button>
            </div>

            {error && (
                <div style={{ fontSize: "0.72rem", color: colors.danger, marginBottom: 6 }}>
                    {error}
                </div>
            )}

            {usage && (
                <div style={{ fontSize: "0.76rem", color: colors.text }}>
                    <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                        <span style={{ color: colors.textSecondary }}>{t("This Month", "本月花费")}</span>
                        <span style={{ fontWeight: 600, fontSize: "0.9rem" }}>
                            ${usage.total_used.toFixed(4)}
                        </span>
                    </div>
                    {usage.total_granted > 0 && (
                        <>
                            <div style={{
                                height: 6, borderRadius: 3, background: colors.border,
                                overflow: "hidden", margin: "6px 0",
                            }}>
                                <div style={{
                                    height: "100%", borderRadius: 3,
                                    background: (usage.total_used / usage.total_granted) > 0.8 ? colors.danger : colors.success,
                                    width: `${Math.min(Math.round((usage.total_used / usage.total_granted) * 100), 100)}%`,
                                    transition: "width 0.3s",
                                }} />
                            </div>
                            <div style={{ display: "flex", justifyContent: "space-between", fontSize: "0.7rem", color: colors.textSecondary }}>
                                <span>{t("Left", "剩余")}: ${usage.total_available.toFixed(2)}</span>
                                <span>{t("Total", "总额")}: ${usage.total_granted.toFixed(2)}</span>
                            </div>
                        </>
                    )}
                </div>
            )}
        </div>
    );
}

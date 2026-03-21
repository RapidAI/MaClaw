import { useState, useCallback } from "react";
import { GetOpenAIUsage } from "../../../wailsjs/go/main/App";

interface UsageData {
    total_granted: number;
    total_used: number;
    total_available: number;
}

type Props = {
    lang: string;
};

export function UsageDisplay({ lang }: Props) {
    const t = useCallback((zh: string, en: string) => lang?.startsWith("zh") ? zh : en, [lang]);

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

    const pct = usage && usage.total_granted > 0
        ? Math.round((usage.total_used / usage.total_granted) * 100)
        : 0;

    return (
        <div style={{
            padding: "10px 14px", borderRadius: 8,
            border: "1px solid #e2e8f0", background: "#f8fafc",
        }}>
            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: 8 }}>
                <span style={{ fontSize: "0.78rem", fontWeight: 600, color: "#334155" }}>
                    {t("OpenAI 用量", "OpenAI Usage")}
                </span>
                <button onClick={fetchUsage} disabled={loading} style={{
                    fontSize: "0.72rem", padding: "3px 10px", borderRadius: 4,
                    background: loading ? "#e2e8f0" : "#6366f1", color: loading ? "#94a3b8" : "#fff",
                    border: "none", cursor: loading ? "default" : "pointer",
                }}>
                    {loading ? t("查询中...", "Loading...") : t("查询", "Check")}
                </button>
            </div>

            {error && (
                <div style={{ fontSize: "0.72rem", color: "#ef4444", marginBottom: 6 }}>
                    {error}
                </div>
            )}

            {usage && (
                <div>
                    <div style={{
                        height: 6, borderRadius: 3, background: "#e2e8f0",
                        overflow: "hidden", marginBottom: 6,
                    }}>
                        <div style={{
                            height: "100%", borderRadius: 3,
                            background: pct > 80 ? "#ef4444" : "#22c55e",
                            width: `${Math.min(pct, 100)}%`,
                            transition: "width 0.3s",
                        }} />
                    </div>
                    <div style={{ display: "flex", justifyContent: "space-between", fontSize: "0.7rem", color: "#64748b" }}>
                        <span>{t("已用", "Used")}: ${usage.total_used.toFixed(2)}</span>
                        <span>{t("剩余", "Left")}: ${usage.total_available.toFixed(2)}</span>
                        <span>{t("总额", "Total")}: ${usage.total_granted.toFixed(2)}</span>
                    </div>
                </div>
            )}
        </div>
    );
}

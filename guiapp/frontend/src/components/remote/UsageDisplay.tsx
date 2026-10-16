import { useState, useCallback, useEffect, useRef } from "react";
import { GetOpenAIUsage } from "../../../wailsjs/go/main/App";
import { formatProviderTestError } from "./LLMConfigPanelShared";

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
    const fetchSeqRef = useRef(0);

    useEffect(() => () => { fetchSeqRef.current += 1; }, []);

    const fetchUsage = useCallback(async () => {
        const seq = ++fetchSeqRef.current;
        setLoading(true);
        setError("");
        try {
            const data = await GetOpenAIUsage();
            if (seq !== fetchSeqRef.current) return;
            setUsage({
                total_granted: Number(data?.total_granted) || 0,
                total_used: Number(data?.total_used) || 0,
                total_available: Number(data?.total_available) || 0,
            });
        } catch (e) {
            if (seq !== fetchSeqRef.current) return;
            setUsage(null);
            setError(formatProviderTestError(String(e), t) || String(e));
        } finally {
            if (seq === fetchSeqRef.current) setLoading(false);
        }
    }, [t]);

    return (
        <div className="usage-display">
            <div className="usage-display__header">
                <span className="usage-display__title">
                    {t("OpenAI organization costs", "OpenAI 组织账单")}
                </span>
                <button type="button" onClick={fetchUsage} disabled={loading} className="usage-display__button">
                    {loading ? t("Loading...", "查询中...") : t("Check", "查询")}
                </button>
            </div>

            {error && (
                <div className="usage-display__error">
                    {error}
                </div>
            )}

            {usage && (
                <div className="usage-display__body">
                    <div className="usage-display__metric-row">
                        <span>{t("This Month", "本月花费")}</span>
                        <strong>
                            ${usage.total_used.toFixed(4)}
                        </strong>
                    </div>
                    {usage.total_granted > 0 && (
                        <>
                            <div className="usage-display__progress">
                                <div
                                    className={(usage.total_used / usage.total_granted) > 0.8 ? "usage-display__progress-fill is-danger" : "usage-display__progress-fill is-success"}
                                    style={{ width: `${Math.min(Math.round((usage.total_used / usage.total_granted) * 100), 100)}%` }}
                                />
                            </div>
                            <div className="usage-display__footer">
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

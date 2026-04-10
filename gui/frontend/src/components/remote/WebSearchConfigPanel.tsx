import { useCallback, useEffect, useMemo, useState } from "react";
import { GetWebSearchProviders, SaveWebSearchProviders } from "../../../wailsjs/go/main/App";
import { colors } from "./styles";

interface WebSearchProvider {
    name: string;
    type: string;
    key?: string;
    base_url?: string;
}

type Props = { lang?: string };

const cardStyle: React.CSSProperties = {
    border: `1px solid ${colors.border}`,
    borderRadius: 6,
    padding: "12px 14px",
    background: colors.surface,
};

const inputStyle: React.CSSProperties = {
    width: "100%",
    padding: "8px 10px",
    fontSize: "0.8rem",
    border: `1px solid ${colors.border}`,
    borderRadius: 4,
    background: colors.surface,
    color: colors.text,
    boxSizing: "border-box",
};

export function WebSearchConfigPanel({ lang }: Props) {
    const t = useCallback((en: string, zhHans: string, zhHant: string = zhHans) =>
        lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en, [lang]);
    const [providers, setProviders] = useState<WebSearchProvider[]>([]);
    const [current, setCurrent] = useState("duckduckgo");
    const [saving, setSaving] = useState(false);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState("");
    const [saved, setSaved] = useState(false);

    const load = useCallback(async () => {
        setLoading(true);
        setError("");
        try {
            const data: any = await GetWebSearchProviders();
            setProviders(Array.isArray(data?.providers) ? data.providers : []);
            setCurrent(data?.current || "duckduckgo");
        } catch (e: any) {
            setError(String(e));
        } finally {
            setLoading(false);
        }
    }, []);

    useEffect(() => { void load(); }, [load]);

    const currentProvider = useMemo(
        () => providers.find((provider) => provider.type === current) || providers[0] || null,
        [providers, current],
    );

    const updateProviderKey = useCallback((type: string, key: string) => {
        setSaved(false);
        setProviders(prev => prev.map(provider => provider.type === type ? { ...provider, key } : provider));
    }, []);

    const save = useCallback(async () => {
        setSaving(true);
        setError("");
        setSaved(false);
        try {
            await SaveWebSearchProviders(providers, current);
            setSaved(true);
            setTimeout(() => {
                setSaved(false);
            }, 1500);
        } catch (e: any) {
            setError(String(e));
        } finally {
            setSaving(false);
        }
    }, [providers, current, t]);

    if (loading) {
        return <div style={{ padding: 16, color: colors.textMuted }}>{t("Loading...", "加载中...")}</div>;
    }

    return (
        <div style={{ padding: "0 4px" }}>
            <div style={{ marginBottom: 16 }}>
                <p style={{ fontSize: "0.75rem", color: colors.textSecondary, margin: 0, lineHeight: 1.6 }}>
                    {t(
                        "选择 AI 助手网页搜索使用的搜索引擎。Brave 和 Serper 需要 API Key；未填写时会回退到默认联网搜索。DuckDuckGo 免费，无需 API Key。",
                        "Choose which search engine AI Assistant uses for web search. Brave and Serper require API keys; without one, requests fall back to the default direct web search. DuckDuckGo is free and needs no API key.",
                    )}
                </p>
            </div>

            <div style={{ display: "grid", gridTemplateColumns: "260px 1fr", gap: 16, alignItems: "start" }}>
                <div style={{ ...cardStyle, display: "flex", flexDirection: "column", gap: 10 }}>
                    {providers.map((provider) => {
                        const active = provider.type === current;
                        return (
                            <button
                                key={provider.type}
                                type="button"
                                onClick={() => {
                                    setSaved(false);
                                    setCurrent(provider.type);
                                }}
                                style={{
                                    textAlign: "left",
                                    border: active ? `1px solid ${colors.primary}` : `1px solid ${colors.border}`,
                                    background: active ? colors.primaryLight : colors.surface,
                                    color: colors.text,
                                    borderRadius: 6,
                                    padding: "10px 12px",
                                    cursor: "pointer",
                                }}
                            >
                                <div style={{ fontSize: "0.82rem", fontWeight: 600 }}>{provider.name || provider.type}</div>
                                <div style={{ fontSize: "0.72rem", color: colors.textMuted, marginTop: 4 }}>
                                    {provider.type === "duckduckgo"
                                        ? t("Free, no key needed", "免费，无需 Key")
                                        : t("API key supported", "可配置 API Key")}
                                </div>
                            </button>
                        );
                    })}
                </div>

                <div style={cardStyle}>
                    {currentProvider && (
                        <>
                            <div style={{ fontSize: "0.88rem", fontWeight: 600, color: colors.text, marginBottom: 8 }}>
                                {currentProvider.name}
                            </div>
                            <div style={{ fontSize: "0.75rem", color: colors.textSecondary, marginBottom: 16, lineHeight: 1.6 }}>
                                {currentProvider.type === "brave" && t("Uses Brave Search API. Without an API key, runtime falls back to the default direct web search.", "使用 Brave Search API。未填写 API Key 时，运行时将回退到默认联网搜索。")}
                                {currentProvider.type === "serper" && t("Uses Serper Search API. Without an API key, runtime falls back to the default direct web search.", "使用 Serper Search API。未填写 API Key 时，运行时将回退到默认联网搜索。")}
                                {currentProvider.type === "duckduckgo" && t("DuckDuckGo is the free option and uses its own provider implementation with no API key required.", "DuckDuckGo 为免费选项，采用独立 provider 实现，无需 API Key。")}
                            </div>

                            {currentProvider.type !== "duckduckgo" ? (
                                <div>
                                    <label style={{ display: "block", fontSize: "0.74rem", color: colors.textSecondary, marginBottom: 6 }}>
                                        API Key
                                    </label>
                                    <input
                                        type="password"
                                        value={currentProvider.key || ""}
                                        onChange={(e) => updateProviderKey(currentProvider.type, e.target.value)}
                                        placeholder={t("Enter API Key", "输入 API Key")}
                                        style={inputStyle}
                                        autoComplete="new-password"
                                    />
                                </div>
                            ) : (
                                <div style={{ fontSize: "0.78rem", color: colors.textMuted }}>
                                    {t("No extra configuration is needed for this provider.", "当前 provider 无需额外配置。")}
                                </div>
                            )}
                        </>
                    )}
                </div>
            </div>

            {error && <div style={{ marginTop: 12, color: colors.danger, fontSize: "0.76rem" }}>{error}</div>}

            <div style={{ display: "flex", justifyContent: "flex-end", marginTop: 18 }}>
                <button
                    type="button"
                    onClick={save}
                    disabled={saving}
                    style={{
                        padding: "8px 16px",
                        borderRadius: 4,
                        border: "none",
                        background: saving ? colors.primaryLight : colors.primary,
                        color: "#fff",
                        cursor: saving ? "default" : "pointer",
                        fontSize: "0.8rem",
                    }}
                >
                    {saving ? t("Saving...", "保存中...") : saved ? t("Saved ✓", "已保存 ✓") : t("Save", "保存")}
                </button>
            </div>
        </div>
    );
}

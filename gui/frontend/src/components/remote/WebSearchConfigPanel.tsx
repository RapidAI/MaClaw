import { useCallback, useEffect, useMemo, useState } from "react";
import { GetWebSearchProviders, SaveWebSearchProviders } from "../../../wailsjs/go/main/App";

interface WebSearchProvider {
    name: string;
    type: string;
    key?: string;
    base_url?: string;
}

type Props = { lang?: string };

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
        return <div className="web-search-config__loading">{t("Loading...", "加载中...")}</div>;
    }

    return (
        <div className="web-search-config">
            <div className="web-search-config__intro">
                <p>
                    {t(
                        "Choose which search engine AI Assistant uses for web search. Brave and Serper require API keys; without one, requests fall back to the default direct web search. DuckDuckGo is free and needs no API key.",
                        "选择 AI 助手网页搜索使用的搜索引擎。Brave 和 Serper 需要 API Key；未填写时会回退到默认联网搜索。DuckDuckGo 免费，无需 API Key。",
                        "選擇 AI 助手網頁搜尋使用的搜尋引擎。Brave 和 Serper 需要 API Key；未填寫時會回退到預設聯網搜尋。DuckDuckGo 免費，無需 API Key。",
                    )}
                </p>
            </div>

            <div className="web-search-config__layout">
                <div className="web-search-config__provider-list">
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
                                className="web-search-config__provider"
                                data-active={active ? "true" : "false"}
                            >
                                <div className="web-search-config__provider-name">{provider.name || provider.type}</div>
                                <div className="web-search-config__provider-meta">
                                    {provider.type === "duckduckgo"
                                        ? t("Free, no key needed", "免费，无需 Key")
                                        : t("API key supported", "可配置 API Key")}
                                </div>
                            </button>
                        );
                    })}
                </div>

                <div className="web-search-config__detail-card">
                    {currentProvider && (
                        <>
                            <div className="web-search-config__detail-title">
                                {currentProvider.name}
                            </div>
                            <div className="web-search-config__detail-copy">
                                {currentProvider.type === "brave" && t("Uses Brave Search API. Without an API key, runtime falls back to the default direct web search.", "使用 Brave Search API。未填写 API Key 时，运行时将回退到默认联网搜索。")}
                                {currentProvider.type === "serper" && t("Uses Serper Search API. Without an API key, runtime falls back to the default direct web search.", "使用 Serper Search API。未填写 API Key 时，运行时将回退到默认联网搜索。")}
                                {currentProvider.type === "tinyfish" && t("Uses TinyFish Search & Fetch API. Provides web search and intelligent content extraction. Without an API key, runtime falls back to the default direct web search.", "使用 TinyFish Search & Fetch API。提供网页搜索和智能内容提取。未填写 API Key 时，运行时将回退到默认联网搜索。")}
                                {currentProvider.type === "duckduckgo" && t("DuckDuckGo is the free option and uses its own provider implementation with no API key required.", "DuckDuckGo 为免费选项，采用独立 provider 实现，无需 API Key。")}
                            </div>

                            {currentProvider.type !== "duckduckgo" ? (
                                <div>
                                    <label className="web-search-config__label">
                                        API Key
                                    </label>
                                    <input
                                        type="password"
                                        value={currentProvider.key || ""}
                                        onChange={(e) => updateProviderKey(currentProvider.type, e.target.value)}
                                        placeholder={t("Enter API Key", "输入 API Key")}
                                        className="web-search-config__input"
                                        autoComplete="new-password"
                                    />
                                </div>
                            ) : (
                                <div className="web-search-config__empty-note">
                                    {t("No extra configuration is needed for this provider.", "当前 provider 无需额外配置。")}
                                </div>
                            )}

                            <div className="web-search-config__actions">
                                <button
                                    type="button"
                                    onClick={save}
                                    disabled={saving}
                                    className="web-search-config__save"
                                >
                                    {saving ? t("Saving...", "保存中...") : saved ? t("Saved OK", "已保存 OK") : t("Save", "保存")}
                                </button>
                            </div>
                        </>
                    )}
                </div>
            </div>

            {error && <div className="web-search-config__error">{error}</div>}
        </div>
    );
}

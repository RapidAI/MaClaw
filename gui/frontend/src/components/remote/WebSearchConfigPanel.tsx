import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { GetWebSearchProviders, SaveWebSearchProviders, TestWebSearchProvider } from "../../../wailsjs/go/main/App";

interface WebSearchProvider {
    name: string;
    type: string;
    key?: string;
    base_url?: string;
}

type SavePhase = "idle" | "testing" | "saving";
type Props = { lang?: string };

function formatProviderTestError(message: string, provider: WebSearchProvider | null | undefined, t: (en: string, zhHans: string, zhHant?: string) => string) {
    if (provider?.type === "duckduckgo" && message.includes("human verification challenge")) {
        return t(
            "DuckDuckGo blocked this request with a human verification challenge. The provider is not usable from the current network/IP right now.",
            "DuckDuckGo 返回了人工验证挑战。当前网络/IP 下这个 provider 现在不可用。",
            "DuckDuckGo 回傳了人工驗證挑戰。目前網路/IP 下這個 provider 現在不可用。",
        );
    }
    return message;
}

export function WebSearchConfigPanel({ lang }: Props) {
    const t = useCallback((en: string, zhHans: string, zhHant: string = zhHans) =>
        lang === "zh-Hans" ? zhHans : lang === "zh-Hant" ? zhHant : en, [lang]);
    const [providers, setProviders] = useState<WebSearchProvider[]>([]);
    const [current, setCurrent] = useState("duckduckgo");
    const [saving, setSaving] = useState(false);
    const [savePhase, setSavePhase] = useState<SavePhase>("idle");
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState("");
    const [saved, setSaved] = useState(false);
    const savedResetTimerRef = useRef<number | null>(null);

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
    useEffect(() => () => {
        if (savedResetTimerRef.current !== null) {
            window.clearTimeout(savedResetTimerRef.current);
        }
    }, []);

    const currentProvider = useMemo(
        () => providers.find((provider) => provider.type === current) || providers[0] || null,
        [providers, current],
    );

    const updateProviderKey = useCallback((type: string, key: string) => {
        setSaved(false);
        setError("");
        setProviders(prev => prev.map(provider => provider.type === type ? { ...provider, key } : provider));
    }, []);

    const save = useCallback(async () => {
        setSaving(true);
        setSavePhase("testing");
        setError("");
        setSaved(false);
        try {
            const providerToTest = providers.find((provider) => provider.type === current);
            if (!providerToTest) {
                throw new Error(t("No search provider is selected.", "未选择搜索引擎。"));
            }
            await TestWebSearchProvider(providerToTest);
            setSavePhase("saving");
            await SaveWebSearchProviders(providers, current);
            setSaved(true);
            if (savedResetTimerRef.current !== null) {
                window.clearTimeout(savedResetTimerRef.current);
            }
            savedResetTimerRef.current = window.setTimeout(() => {
                setSaved(false);
                savedResetTimerRef.current = null;
            }, 1500);
        } catch (e: any) {
            const providerToTest = providers.find((provider) => provider.type === current);
            setError(
                t("Search provider test failed: ", "搜索引擎测试失败：") +
                formatProviderTestError(String(e), providerToTest, t),
            );
        } finally {
            setSavePhase("idle");
            setSaving(false);
        }
    }, [providers, current, t]);

    const saveButtonLabel = savePhase === "testing"
        ? t("Testing provider...", "正在测试搜索引擎...", "正在測試搜尋引擎...")
        : savePhase === "saving"
            ? t("Saving...", "保存中...")
            : saved
                ? t("Saved OK", "已保存 OK")
                : t("Save", "保存");

    if (loading) {
        return <div className="web-search-config__loading">{t("Loading...", "加载中...")}</div>;
    }

    return (
        <div className="web-search-config">
            <div className="web-search-config__intro">
                <p>
                    {t(
                        "Choose which search engine AI Assistant uses for web search. Brave, Serper, TinyFish, and Tavily require API keys; DuckDuckGo is free and needs no API key. When saving, MaClaw tests the selected provider with the configured key first.",
                        "选择 AI 助手网页搜索使用的搜索引擎。Brave、Serper、TinyFish 和 Tavily 需要 API Key；DuckDuckGo 免费且无需 API Key。保存时会先使用当前配置的 Key 测试所选搜索引擎。",
                        "選擇 AI 助手網頁搜尋使用的搜尋引擎。Brave、Serper、TinyFish 和 Tavily 需要 API Key；DuckDuckGo 免費且無需 API Key。儲存時會先使用目前設定的 Key 測試所選搜尋引擎。",
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
                                    setError("");
                                    setCurrent(provider.type);
                                }}
                                className="web-search-config__provider"
                                data-active={active ? "true" : "false"}
                            >
                                <div className="web-search-config__provider-name">{provider.name || provider.type}</div>
                                <div className="web-search-config__provider-meta">
                                    {provider.type === "duckduckgo"
                                        ? t("Free, no key needed", "免费，无需 Key", "免費，無需 Key")
                                        : t("API key will be tested", "保存前测试 API Key", "儲存前測試 API Key")}
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
                                {currentProvider.type === "brave" && t("Uses Brave Search API. Saving requires a successful test with this API key.", "使用 Brave Search API。保存前必须用该 API Key 测试通过。")}
                                {currentProvider.type === "serper" && t("Uses Serper Search API. Saving requires a successful test with this API key.", "使用 Serper Search API。保存前必须用该 API Key 测试通过。")}
                                {currentProvider.type === "tinyfish" && t("Uses TinyFish Search & Fetch API. Saving requires a successful test with this API key.", "使用 TinyFish Search & Fetch API。保存前必须用该 API Key 测试通过。")}
                                {currentProvider.type === "tavily" && t("Uses Tavily Search API. Saving requires a successful test with this API key.", "使用 Tavily Search API。保存前必须用该 API Key 测试通过。")}
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
                                        placeholder={t("Enter API Key", "输入 API Key", "輸入 API Key")}
                                        className="web-search-config__input"
                                        autoComplete="new-password"
                                    />
                                </div>
                            ) : (
                                <div className="web-search-config__empty-note">
                                    {t("No extra configuration is needed for this provider. It will still be tested before saving.", "当前 provider 无需额外配置，保存前仍会测试连通性。")}
                                </div>
                            )}

                            <div className="web-search-config__actions">
                                <button
                                    type="button"
                                    onClick={save}
                                    disabled={saving}
                                    className="web-search-config__save"
                                >
                                    {saveButtonLabel}
                                </button>
                            </div>

                            {(saving || saved) && (
                                <div className="web-search-config__status" data-state={saved ? "success" : "pending"}>
                                    {savePhase === "testing" && (currentProvider.type === "duckduckgo"
                                        ? t("Testing this provider before saving.", "正在测试该搜索引擎，测试通过后再保存。", "正在測試該搜尋引擎，測試通過後再儲存。")
                                        : t("Testing this provider with the configured key before saving.", "正在使用当前配置的 Key 测试该搜索引擎，测试通过后再保存。", "正在使用目前設定的 Key 測試該搜尋引擎，測試通過後再儲存。"))}
                                    {savePhase === "saving" && t("Test passed. Saving configuration.", "测试通过，正在保存配置。")}
                                    {saved && t("Test passed and configuration saved.", "测试通过，配置已保存。")}
                                </div>
                            )}
                        </>
                    )}
                </div>
            </div>

            {error && <div className="web-search-config__error">{error}</div>}
        </div>
    );
}

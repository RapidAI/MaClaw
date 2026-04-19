import { useState, useEffect, useCallback, useRef } from "react";

import {
    GetMaclawLLMProviders,
    SaveMaclawLLMProviders,
    TestMaclawLLM,
    GetMaclawAgentMaxIterations,
    SetMaclawAgentMaxIterations,
    StartOpenAIOAuth,
    CancelOpenAIOAuth,
    ImportCodexAuth,
    StartFreeProxy,
    StopFreeProxy,
    IsFreeProxyRunning,
    DetectBrowser,
    DangbeiLogin,
    DangbeiFinishLogin,
    DangbeiEnsureAuth,
    GetFreeProxyModels,
    GetFreeProxyModel,
    SetFreeProxyModel,
    FetchCodeGenModels,
    SaveCodeGenModelChoice,
    GetHubLLMServiceStatus,
} from "../../../wailsjs/go/main/App";
import { colors } from "./styles";
import { UsageDisplay } from "./UsageDisplay";
import { TokenUsagePanel } from "./TokenUsagePanel";
import { PROVIDER_LOGOS } from "./providerLogos";
import { useDialog } from "../CustomDialog";

interface LLMProvider {
    name: string;
    url: string;
    key: string;
    model: string;
    protocol?: string; // "openai" (default) or "anthropic"
    context_length?: number; // max context tokens (0 = default 128k)
    is_custom?: boolean;
    auth_type?: string;
    agent_type?: string; // "openclaw" (default) or "claude_code"
    supports_vision?: boolean; // whether the model supports image input
}

const NONE_PROVIDER = "__none__";
const LLM_CONFIG_LOAD_TIMEOUT_MS = 5000;

/** Known OpenAI-compatible providers for quick-fill in custom provider config. */
const KNOWN_OPENAI_ENDPOINTS: { name: string; url: string; model: string; context_length?: number; protocol?: string; agent_type?: string }[] = [
    { name: "OpenAI Official", url: "https://api.openai.com/v1", model: "gpt-5.4", context_length: 128000 },
    { name: "DeepSeek", url: "https://api.deepseek.com/v1", model: "deepseek-chat", context_length: 128000 },
    { name: "智谱龙虾", url: "https://open.bigmodel.cn/api/coding/paas/v4", model: "glm-5-turbo", context_length: 180000 },
    { name: "智谱编程", url: "https://open.bigmodel.cn/api/anthropic", model: "glm-5.1", context_length: 180000, protocol: "anthropic", agent_type: "claude-code/2.0.0" },
    { name: "Kimi (月之暗面)", url: "https://api.kimi.com/coding/v1", model: "kimi-k2-thinking", context_length: 128000 },
    { name: "讯飞星辰", url: "https://maas-coding-api.cn-huabei-1.xf-yun.com/v2", model: "astron-code-latest", context_length: 128000 },
    { name: "Doubao (豆包)", url: "https://ark.cn-beijing.volces.com/api/coding", model: "doubao-seed-code-preview-latest", context_length: 128000 },
    { name: "MiniMax", url: "https://api.minimaxi.com/v1", model: "MiniMax-M2.7", context_length: 128000 },
    { name: "腾讯云", url: "https://api.lkeap.cloud.tencent.com/coding/v3", model: "glm-5", context_length: 128000 },
    { name: "xAI (Grok)", url: "https://api.x.ai/v1", model: "grok-3", context_length: 131072 },
    { name: "OpenRouter", url: "https://openrouter.ai/api/v1", model: "openai/gpt-4o", context_length: 128000 },
    { name: "Together AI", url: "https://api.together.xyz/v1", model: "meta-llama/Llama-3-70b-chat-hf", context_length: 128000 },
    { name: "Groq", url: "https://api.groq.com/openai/v1", model: "llama-3.3-70b-versatile", context_length: 128000 },
    { name: "Perplexity", url: "https://api.perplexity.ai", model: "sonar-pro", context_length: 128000 },
    { name: "阿里云 (百炼)", url: "https://dashscope.aliyuncs.com/compatible-mode/v1", model: "qwen3.5-plus", context_length: 128000 },
    { name: "ChatFire", url: "https://api.chatfire.cn/v1", model: "gpt-4o", context_length: 128000 },
];

/* ── Hoisted style objects (avoid re-creation per render) ── */
const inputStyle: React.CSSProperties = {
    width: "100%", padding: "7px 10px", fontSize: "0.8rem",
    border: `1px solid ${colors.border}`, borderRadius: 4,
    background: colors.surface, color: colors.text, boxSizing: "border-box",
};
const labelStyle: React.CSSProperties = {
    fontSize: "0.76rem", color: colors.textSecondary, marginBottom: 4, display: "block",
};
const readonlyStyle: React.CSSProperties = {
    ...inputStyle, background: colors.bg, color: colors.textMuted, cursor: "default",
};

function withTimeout<T>(promise: Promise<T>, timeoutMs: number, label: string): Promise<T> {
    return new Promise<T>((resolve, reject) => {
        const timer = window.setTimeout(() => {
            reject(new Error(`${label} timeout`));
        }, timeoutMs);
        promise.then(
            value => {
                window.clearTimeout(timer);
                resolve(value);
            },
            error => {
                window.clearTimeout(timer);
                reject(error);
            },
        );
    });
}

interface HubLLMServiceStatus {
    active?: boolean;
    skip_llm_config?: boolean;
    hub_llm_base_url?: string;
    available_models?: string[];
    default_model?: string;
}

interface Props {
    lang?: string;
    codexModels?: unknown[];
    onStatusChange?: (online: boolean, configured: boolean) => void;
}

export function LLMConfigPanel({ lang, onStatusChange }: Props) {
    const { showAlert, showConfirm } = useDialog();
    const [providers, setProviders] = useState<LLMProvider[]>([]);
    const [currentName, setCurrentName] = useState(NONE_PROVIDER);
    const [loading, setLoading] = useState(false);
    const [maxIter, setMaxIter] = useState(0);
    const [hubServiceStatus, setHubServiceStatus] = useState<HubLLMServiceStatus | null>(null);

    // Dialog state — track selected provider by index (stable across renames)
    const [dlgOpen, setDlgOpen] = useState(false);
    const [dlgProviders, setDlgProviders] = useState<LLMProvider[]>([]);
    const [dlgSelectedIdx, setDlgSelectedIdx] = useState<number | null>(null); // null = "None"
    const [dlgSaving, setDlgSaving] = useState(false);
    const [dlgTestResult, setDlgTestResult] = useState<{ ok: boolean; msg: string } | null>(null);
    const [dlgDirty, setDlgDirty] = useState(false);
    const [dlgTested, setDlgTested] = useState(false); // true after successful test; allows save-only on subsequent saves
    const [oauthBusy, setOauthBusy] = useState(false);
    const [proxyRunning, setProxyRunning] = useState(false);
    const [proxyBusy, setProxyBusy] = useState(false);
    const [browserInfo, setBrowserInfo] = useState<{ found: string; name?: string; path?: string } | null>(null);
    const [dangbeiLoggedIn, setDangbeiLoggedIn] = useState(false);
    const [loginBusy, setLoginBusy] = useState(false);
    const [browserLaunched, setBrowserLaunched] = useState(false);
    const [authChecking, setAuthChecking] = useState(false);
    const [freeModels, setFreeModels] = useState<{id: string; name: string}[]>([]);
    const [freeSelectedModel, setFreeSelectedModel] = useState("");
    const [codegenModels, setCodegenModels] = useState<{id: string; name: string}[]>([]);
    const [codegenModelsFetching, setCodegenModelsFetching] = useState(false);
    const [loadError, setLoadError] = useState<string | null>(null);
    const loadSeqRef = useRef(0);

    const t = useCallback((en: string, zhHans: string, zhHant: string = zhHans) =>
        lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en, [lang]);

    const loadHubServiceStatus = useCallback(async () => {
        try {
            const status = await GetHubLLMServiceStatus() as HubLLMServiceStatus;
            setHubServiceStatus(status || null);
        } catch {
            setHubServiceStatus(null);
        }
    }, []);

    /** Shared OAuth login handler for both first-login and re-login scenarios. */
    const handleOAuthLogin = useCallback(async () => {
        setOauthBusy(true);
        setDlgTestResult(null);
        try {
            await StartOpenAIOAuth();
            const data = await GetMaclawLLMProviders();
            if (data?.providers) {
                const fresh = data.providers.map((p: LLMProvider) => ({ ...p }));
                setDlgProviders(fresh);
                setProviders(fresh.map((p: LLMProvider) => ({ ...p })));
                setCurrentName(data.current || NONE_PROVIDER);
                loadHubServiceStatus().catch(() => {});
                // Re-select the OAuth provider by name to keep dlgSelectedIdx stable
                const oaIdx = fresh.findIndex((p: LLMProvider) => p.auth_type === "oauth");
                if (oaIdx >= 0) setDlgSelectedIdx(oaIdx);
                setDlgDirty(false);
                onStatusChange?.(true, true);
                setDlgTestResult({ ok: true, msg: t("OAuth login successful", "OAuth 登录成功") });
                setTimeout(() => setDlgOpen(false), 1200);
            }
        } catch (e) {
            setDlgTestResult({ ok: false, msg: String(e) });
        }
        setOauthBusy(false);
    }, [t, onStatusChange, loadHubServiceStatus]);

    const loadProviders = useCallback(async () => {
        const loadSeq = ++loadSeqRef.current;
        setLoading(true);
        setLoadError(null);
        console.info("[LLMConfigPanel] load start");
        try {
            const [providersResult, iterResult] = await Promise.allSettled([
                withTimeout(GetMaclawLLMProviders(), LLM_CONFIG_LOAD_TIMEOUT_MS, "GetMaclawLLMProviders"),
                withTimeout(GetMaclawAgentMaxIterations(), LLM_CONFIG_LOAD_TIMEOUT_MS, "GetMaclawAgentMaxIterations"),
            ]);
            if (loadSeq !== loadSeqRef.current) return;

            let failed = false;

            if (providersResult.status === "fulfilled") {
                const data = providersResult.value;
                if (data?.providers) {
                    setProviders(data.providers);
                    setCurrentName(data.current || NONE_PROVIDER);
                    loadHubServiceStatus().catch(() => {});
                } else {
                    setProviders([]);
                    setCurrentName(NONE_PROVIDER);
                }
                console.info("[LLMConfigPanel] providers loaded");
            } else {
                failed = true;
                setProviders([]);
                setCurrentName(NONE_PROVIDER);
                console.warn("[LLMConfigPanel] providers load failed", providersResult.reason);
            }

            if (iterResult.status === "fulfilled") {
                const iter = iterResult.value;
                setMaxIter(typeof iter === "number" ? iter : 0);
                console.info("[LLMConfigPanel] max iterations loaded");
            } else {
                failed = true;
                setMaxIter(0);
                console.warn("[LLMConfigPanel] max iterations load failed", iterResult.reason);
            }

            if (failed) {
                setLoadError(t("Some LLM settings failed to load. Click retry.", "部分 LLM 配置加载失败，可点击重试。"));
            }
        } finally {
            if (loadSeq === loadSeqRef.current) {
                console.info("[LLMConfigPanel] load finished");
                setLoading(false);
            }
        }
    }, [t, loadHubServiceStatus]);

    useEffect(() => { loadProviders(); }, [loadProviders]);
    useEffect(() => { loadHubServiceStatus(); }, [loadHubServiceStatus]);

    const isNone = currentName === NONE_PROVIDER;
    const hasHubManagedService = !!hubServiceStatus?.active && !!hubServiceStatus?.skip_llm_config;
    const hubAvailableModels = (hubServiceStatus?.available_models || []).filter(Boolean);

    /* ── Dialog helpers ── */

    const openDialog = useCallback(() => {
        const snapshot = providers.map(p => ({ ...p }));
        setDlgProviders(snapshot);
        const idx = currentName === NONE_PROVIDER ? null : snapshot.findIndex(p => p.name === currentName);
        setDlgSelectedIdx(idx === -1 ? null : idx);
        setDlgSaving(false);
        setDlgTestResult(null);
        setDlgDirty(false);
        setDlgTested(false);
        setBrowserInfo(null);
        setBrowserLaunched(false);
        setDlgOpen(true);
    }, [providers, currentName]);

    const closeDialog = useCallback(async () => {
        if (oauthBusy) return;
        if (dlgSaving) return;
        setDlgOpen(false);
    }, [dlgSaving, oauthBusy]);

    // Escape key to close dialog
    useEffect(() => {
        if (!dlgOpen) return;
        const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") closeDialog(); };
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [dlgOpen, closeDialog]);

    // Poll free proxy status when dialog shows a "none" auth provider
    const dlgAuthType = dlgSelectedIdx !== null ? dlgProviders[dlgSelectedIdx]?.auth_type : undefined;
    useEffect(() => {
        if (!dlgOpen || dlgAuthType !== "none") return;
        let cancelled = false;
        const poll = () => { IsFreeProxyRunning().then(r => { if (!cancelled) setProxyRunning(r); }).catch(() => {}); };
        poll();
        const id = setInterval(poll, 3000);
        return () => { cancelled = true; clearInterval(id); };
    }, [dlgOpen, dlgAuthType]);

    // Fetch CodeGen models when dialog opens with an SSO provider (CodeGen)
    useEffect(() => {
        if (!dlgOpen || dlgAuthType !== "sso") {
            setCodegenModels([]);
            setCodegenModelsFetching(false);
            return;
        }
        let cancelled = false;
        setCodegenModelsFetching(true);
        FetchCodeGenModels().then(models => {
            if (cancelled) return;
            setCodegenModels(models || []);
        }).catch(() => {
            if (!cancelled) setCodegenModels([]);
        }).finally(() => {
            if (!cancelled) setCodegenModelsFetching(false);
        });
        return () => { cancelled = true; };
    }, [dlgOpen, dlgAuthType]);

    // Detect browser and check dangbei login when dialog opens with free provider.
    // If cookie is valid, auto-start proxy so user doesn't need to do anything.
    useEffect(() => {
        if (!dlgOpen || dlgAuthType !== "none") return;
        DetectBrowser().then((info: any) => setBrowserInfo(info || { found: "false" })).catch(() => setBrowserInfo({ found: "false" }));
        // Load available models and current selection
        GetFreeProxyModels().then((models: any) => setFreeModels(models || [])).catch(() => {});
        GetFreeProxyModel().then((m: string) => setFreeSelectedModel(m || "deepseek_r1")).catch(() => {});
        // Validate persisted cookie — if valid, skip browser login flow
        setAuthChecking(true);
        DangbeiEnsureAuth().then(async (result: string) => {
            const loggedIn = result === "authenticated";
            setDangbeiLoggedIn(loggedIn);
            setAuthChecking(false);
            // Auto-start proxy if logged in and not already running
            if (loggedIn) {
                try {
                    const running = await IsFreeProxyRunning();
                    if (!running) {
                        await StartFreeProxy();
                        setProxyRunning(true);
                    }
                } catch { /* proxy start failure is non-fatal here */ }
            }
        }).catch(() => {
            setDangbeiLoggedIn(false);
            setAuthChecking(false);
        });
    }, [dlgOpen, dlgAuthType]);

    const dlgIsNone = dlgSelectedIdx === null;
    const dlgProvider = dlgSelectedIdx !== null ? dlgProviders[dlgSelectedIdx] ?? null : null;

    const dlgUpdateField = useCallback((field: keyof LLMProvider, value: string) => {
        if (dlgSelectedIdx === null) return;
        setDlgProviders(prev => {
            const copy = [...prev];
            const parsed: string | number = field === "context_length" ? (parseInt(value, 10) || 0) : value;
            copy[dlgSelectedIdx] = { ...copy[dlgSelectedIdx], [field]: parsed };
            return copy;
        });
        setDlgDirty(true);
        setDlgTestResult(null);
        // Reset tested flag when core connection fields change so re-test is required
        if (["url", "key", "model", "protocol"].includes(field)) {
            setDlgTested(false);
        }
    }, [dlgSelectedIdx]);

    const dlgSelectProvider = useCallback((idx: number | null) => {
        setDlgSelectedIdx(idx);
        setDlgDirty(true);
        setDlgTestResult(null);
        setDlgTested(false);
    }, []);

    const dlgQuickFill = useCallback((epName: string) => {
        const ep = KNOWN_OPENAI_ENDPOINTS.find(x => x.name === epName);
        if (!ep || dlgSelectedIdx === null) return;
        setDlgProviders(prev => {
            const copy = [...prev];
            copy[dlgSelectedIdx] = {
                ...copy[dlgSelectedIdx],
                name: ep.name,
                url: ep.url,
                model: ep.model,
                protocol: ep.protocol || "openai",
                agent_type: ep.agent_type || copy[dlgSelectedIdx].agent_type,
                context_length: ep.context_length || 128000,
            };
            return copy;
        });
        setDlgDirty(true);
        setDlgTestResult(null);
    }, [dlgSelectedIdx]);

    const dlgHandleSave = async () => {
        if (dlgIsNone) {
            setDlgSaving(true);
            try {
                await SaveMaclawLLMProviders(dlgProviders, NONE_PROVIDER);
                setDlgDirty(false);
                setProviders(dlgProviders.map(p => ({ ...p })));
                setCurrentName(NONE_PROVIDER);
                onStatusChange?.(false, false);
                setDlgOpen(false);
            } catch (e) { showAlert(String(e)); }
            setDlgSaving(false);
            return;
        }
        const sp = dlgProviders[dlgSelectedIdx!];
        if (!sp) return;
        setDlgSaving(true);
        setDlgTestResult(null);

        // OAuth / SSO providers: save directly (token already obtained via OAuth/SSO flow)
        if (sp.auth_type === "oauth" || sp.auth_type === "sso") {
            try {
                const saveName = sp.name;
                await SaveMaclawLLMProviders(dlgProviders, saveName);
                // For SSO (CodeGen), sync model choice to Claude Code and other tool configs
                if (sp.auth_type === "sso" && sp.model) {
                    await SaveCodeGenModelChoice(sp.model, sp.model).catch(() => {});
                }
                setDlgDirty(false);
                setProviders(dlgProviders.map(p => ({ ...p })));
                setCurrentName(saveName);
                onStatusChange?.(!!sp.key, !!sp.key);
                setDlgTestResult({ ok: true, msg: t("Saved", "已保存") });
                setTimeout(() => setDlgOpen(false), 800);
            } catch (e) {
                setDlgTestResult({ ok: false, msg: String(e) });
            }
            setDlgSaving(false);
            return;
        }

        // Free proxy (auth_type "none"): save directly, no test needed
        if (sp.auth_type === "none") {
            try {
                const saveName = sp.name;
                await SaveMaclawLLMProviders(dlgProviders, saveName);
                setDlgDirty(false);
                setProviders(dlgProviders.map(p => ({ ...p })));
                setCurrentName(saveName);
                onStatusChange?.(proxyRunning, true);
                setDlgTestResult({ ok: true, msg: t("Saved", "已保存") });
                setTimeout(() => setDlgOpen(false), 800);
            } catch (e) {
                setDlgTestResult({ ok: false, msg: String(e) });
            }
            setDlgSaving(false);
            return;
        }

        try {
            // If already tested successfully, just save without re-testing.
            // This allows the user to toggle supports_vision after a test
            // without the probe overwriting their manual choice.
            if (dlgTested) {
                const saveName = sp.name;
                await SaveMaclawLLMProviders(dlgProviders, saveName);
                setDlgDirty(false);
                setProviders(dlgProviders.map(p => ({ ...p })));
                setCurrentName(saveName);
                onStatusChange?.(true, true);
                setDlgTestResult({ ok: true, msg: t("Saved", "已保存") });
                setTimeout(() => setDlgOpen(false), 800);
                setDlgSaving(false);
                return;
            }

            const testResult = await TestMaclawLLM({ url: sp.url, key: sp.key, model: sp.model, protocol: sp.protocol || "openai", agent_type: sp.agent_type || "openclaw" });
            const saveName = sp.name;
            const nextProviders = dlgProviders.map((provider, index) => index === dlgSelectedIdx
                ? { ...provider, supports_vision: testResult.supports_vision }
                : { ...provider });
            await SaveMaclawLLMProviders(nextProviders, saveName);

            // Refresh providers to pick up persisted supports_vision from backend
            try {
                const freshData = await GetMaclawLLMProviders();
                if (freshData?.providers) {
                    const fresh = freshData.providers.map((p: LLMProvider) => ({ ...p }));
                    setDlgProviders(fresh);
                    setProviders(fresh.map((p: LLMProvider) => ({ ...p })));
                } else {
                    setDlgProviders(nextProviders);
                    setProviders(nextProviders.map((p: LLMProvider) => ({ ...p })));
                }
            } catch {
                setDlgProviders(nextProviders);
                setProviders(nextProviders.map((p: LLMProvider) => ({ ...p })));
            }
            setDlgDirty(false);
            setCurrentName(saveName);

            setDlgTested(true);
            setDlgTestResult({
                ok: true,
                msg: `${testResult.message}\n${testResult.supports_vision
                    ? t("Vision support: enabled", "图片理解：支持")
                    : t("Vision support: disabled", "图片理解：不支持")}`,
            });
            onStatusChange?.(true, true);
            // Don't auto-close: let user review the vision probe result and
            // manually override supports_vision if needed before closing.
        } catch (e) {
            setDlgTestResult({ ok: false, msg: String(e) });
        }
        setDlgSaving(false);
    };

    if (loading) return <div style={{ padding: 16, color: colors.textMuted }}>{t("Loading...", "加载中...")}</div>;

    return (
        <div style={{ padding: "0 4px" }}>
            {hasHubManagedService && (
                <div style={{
                    marginBottom: 16,
                    padding: "12px 16px",
                    borderRadius: 8,
                    border: `1px solid ${colors.success}`,
                    background: colors.successBg,
                }}>
                    <div style={{ fontSize: "0.82rem", fontWeight: 700, color: colors.success, marginBottom: 8 }}>
                        {t("MaClaw Model Service", "MaClaw 模型服务")}
                    </div>
                    <div style={{ fontSize: "0.74rem", color: colors.textSecondary, lineHeight: 1.6 }}>
                        {t(
                            "This account already has Hub-managed MaClaw model service access. Tools can use the exposed OpenAI-compatible endpoint directly.",
                            "当前账号已开通 Hub 托管的 MaClaw 模型服务，可直接使用对外暴露的 OpenAI 兼容接口。"
                        )}
                    </div>
                    <div style={{ marginTop: 10, display: "grid", gap: 8 }}>
                        <div>
                            <label style={labelStyle}>{t("Exposed API URL", "对外 API 地址")}</label>
                            <div style={{ ...readonlyStyle, minHeight: 36, display: "flex", alignItems: "center" }}>{hubServiceStatus?.hub_llm_base_url || "-"}</div>
                        </div>
                        <div>
                            <label style={labelStyle}>{t("Available Models", "可用模型名")}</label>
                            <div style={{ fontSize: "0.8rem", color: colors.text }}>{hubAvailableModels.length ? hubAvailableModels.join(", ") : (hubServiceStatus?.default_model || "auto")}</div>
                        </div>
                    </div>
                </div>
            )}
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 16 }}>
                <p style={{ fontSize: "0.72rem", color: colors.textMuted, margin: 0, lineHeight: 1.5 }}>
                    {t(
                        "Select LLM provider (OpenAI / Anthropic supported)",
                        "选择 LLM 服务商（支持 OpenAI / Anthropic 协议）"
                    )}
                </p>
                <button onClick={openDialog} style={{
                    fontSize: "0.76rem", padding: "6px 18px", cursor: "pointer",
                    background: colors.primary, color: colors.onPrimary, border: "none", borderRadius: 4, flexShrink: 0, marginLeft: 12,
                }}>
                    {t("Configure", "配置")}
                </button>
            </div>

            {/* Current provider summary */}
            <div style={{
                marginBottom: 16, padding: "10px 16px", borderRadius: 6,
                border: `1px solid ${colors.border}`, background: colors.surface,
                display: "flex", justifyContent: "space-between", alignItems: "center",
            }}>
                <span style={{ fontSize: "0.76rem", color: colors.textSecondary }}>
                    {t("Provider", "当前服务商")}
                </span>
                <span style={{ fontSize: "0.76rem", fontWeight: 600, color: isNone ? colors.danger : colors.text }}>
                    {isNone ? t("None", "未配置") : currentName}
                </span>
            </div>

            {/* Usage display for OAuth providers */}
            {!isNone && providers.find(p => p.name === currentName)?.auth_type === "oauth" && (
                <div style={{ marginBottom: 16 }}>
                    <UsageDisplay lang={lang || ""} />
                </div>
            )}

            {/* Token usage statistics */}
            {!isNone && (
                <div style={{ marginBottom: 16 }}>
                    <TokenUsagePanel lang={lang || ""} />
                </div>
            )}

            {/* Max iterations — inline editable */}
            <div style={{
                marginBottom: 16, padding: "12px 16px", borderRadius: 6,
                border: `1px solid ${colors.border}`, background: colors.surface,
            }}>
                <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: 8 }}>
                    <label style={{ ...labelStyle, marginBottom: 0 }}>
                        {t("Agent Max Iterations", "Agent 最大推理轮数")}
                        <span style={{ fontSize: "0.68rem", color: colors.textMuted, fontWeight: 400, marginLeft: 6 }}>
                            {t("0=unlimited, default 12", "0=不限制，默认 12")}
                        </span>
                    </label>
                </div>
                <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
                    <input type="range" min={0} max={300} step={1} value={maxIter}
                        onChange={e => { const v = Number(e.target.value); setMaxIter(v); SetMaclawAgentMaxIterations(v).catch(() => {}); }}
                        style={{ flex: 1, accentColor: "var(--theme-primary)" }} />
                    <input type="number" min={0} max={300} value={maxIter}
                        onChange={e => { const v = Math.max(0, Math.min(300, Number(e.target.value) || 0)); setMaxIter(v); SetMaclawAgentMaxIterations(v).catch(() => {}); }}
                        style={{ ...inputStyle, width: 60, textAlign: "center" as const }} />
                    <span style={{ fontSize: "0.72rem", color: colors.textSecondary, whiteSpace: "nowrap" }}>
                        {maxIter === 0 ? t("Unlimited", "不限制") : `${maxIter} ${t("rounds", "轮")}`}
                    </span>
                </div>
            </div>

            {isNone && !hasHubManagedService && (
                <div style={{
                    padding: "8px 12px", borderRadius: 4, fontSize: "0.74rem", lineHeight: 1.5,
                    background: "rgba(239,68,68,0.08)", border: "1px solid rgba(239,68,68,0.25)", color: colors.danger,
                }}>
                    提示 {t("Without a provider, MaClaw remote will be disabled.", "未配置服务商时，MaClaw 远程能力将不可用。")}
                </div>
            )}

            {/* ── Config Dialog ── */}
            {dlgOpen && (
                <div style={{
                    position: "fixed", top: 0, left: 0, right: 0, bottom: 0,
                    background: "rgba(0,0,0,0.4)", display: "flex",
                    alignItems: "center", justifyContent: "center", zIndex: 9999,
                }} onMouseDown={closeDialog}>
                    <div style={{
                        background: colors.surface, borderRadius: 12, padding: "24px 28px",
                        maxWidth: 520, width: "92%", maxHeight: "85vh", overflowY: "auto",
                        boxShadow: "0 16px 48px rgba(0,0,0,0.22)",
                    }} onMouseDown={e => e.stopPropagation()}>

                        {/* Header */}
                        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 18 }}>
                            <span style={{ fontSize: "0.92rem", fontWeight: 700, color: colors.text }}>
                                {t("MaClaw LLM Configuration", "MaClaw LLM 配置")}
                            </span>
                            <button onClick={closeDialog} style={{
                                border: "none", background: "transparent", cursor: "pointer",
                                fontSize: "1.1rem", color: colors.textSecondary, padding: "0 4px",
                            }}>✕</button>
                        </div>

                        {/* Provider selection */}
                        <div style={{ marginBottom: 16 }}>
                            <label style={labelStyle}>{t("Select Provider", "选择服务商")}</label>
                            <div style={{ display: "flex", gap: 4, flexWrap: "wrap" }}>
                                {dlgProviders.map((p, i) => {
                                    const active = dlgSelectedIdx === i;
                                    const badge: Record<string, string> = { "免费": "白嫖党", "OpenAI": "富家小子", "智谱龙虾": "聪明伶俐", "智谱编程": "写码飞快", "MiniMax": "憨厚老实", "讯飞星辰": "星辰大海" };
                                    const tag = badge[p.name];
                                    return (
                                        <button key={i} onClick={() => dlgSelectProvider(i)} style={{
                                            fontSize: "0.76rem", padding: "5px 14px", cursor: "pointer",
                                            background: active ? colors.primary : colors.surface,
                                            color: active ? colors.onPrimary : colors.text,
                                            border: `1px solid ${active ? colors.primary : colors.border}`,
                                            borderRadius: 4, transition: "all 0.15s",
                                            position: "relative" as const,
                                            display: "inline-flex", alignItems: "center", gap: 5,
                                        }}>
                                            {PROVIDER_LOGOS[p.name] ?? null}{p.name}
                                            {tag && (
                                                <span style={{
                                                    position: "absolute", top: -8, right: -10,
                                                    fontSize: "0.56rem", lineHeight: 1, padding: "2px 5px",
                                                    borderRadius: 6, whiteSpace: "nowrap",
                                                    background: active ? "var(--theme-warning)" : colors.primary,
                                                    color: colors.onPrimary, fontWeight: 600, pointerEvents: "none",
                                                }}>{tag}</span>
                                            )}
                                        </button>
                                    );
                                })}
                                {/* "None" button */}
                                <button onClick={() => dlgSelectProvider(null)} style={{
                                    fontSize: "0.76rem", padding: "5px 14px", cursor: "pointer",
                                    background: dlgIsNone ? colors.primary : colors.surface,
                                    color: dlgIsNone ? colors.onPrimary : colors.text,
                                    border: `1px solid ${dlgIsNone ? colors.primary : colors.border}`,
                                    borderRadius: 4, transition: "all 0.15s",
                                }}>
                                    {t("None", "暂不配置")}
                                </button>
                            </div>
                        </div>

                        {/* None warning */}
                        {dlgIsNone && (
                            <div style={{
                                padding: "8px 12px", borderRadius: 4, fontSize: "0.74rem", lineHeight: 1.5,
                                background: "rgba(239,68,68,0.08)", border: "1px solid rgba(239,68,68,0.25)", color: colors.danger,
                                marginBottom: 16,
                            }}>
                                ⚠️ {t("Without a provider, MaClaw remote will be disabled.", "不配置服务商，MaClaw 远程将失效。")}
                            </div>
                        )}

                        {/* Provider config fields */}
                        {!dlgIsNone && dlgProvider && (
                            <div style={{
                                marginBottom: 16, padding: "14px", borderRadius: 6,
                                border: `1px solid ${colors.border}`, background: colors.bg,
                            }}>
                                <div style={{ fontSize: "0.78rem", fontWeight: 600, color: colors.text, marginBottom: 12 }}>
                                    {dlgProvider.is_custom
                                        ? t("Custom Provider Configuration", "自定义服务商配置")
                                        : `${dlgProvider.name} ${t("Configuration", "配置")}`}
                                </div>

                                {/* Custom: quick-fill from known endpoints */}
                                {dlgProvider.is_custom && (
                                    <div style={{ marginBottom: 12 }}>
                                        <label style={labelStyle}>{t("Quick-fill from known provider", "从已知服务商快速填充")}</label>
                                        <select
                                            style={{ ...inputStyle, cursor: "pointer" }}
                                            value=""
                                            onChange={e => dlgQuickFill(e.target.value)}
                                        >
                                            <option value="">{t("-- Select a known provider to auto-fill --", "-- 选择已知服务商自动填充 --")}</option>
                                            {KNOWN_OPENAI_ENDPOINTS.map(ep => (
                                                <option key={ep.name} value={ep.name}>{ep.name} — {ep.model}</option>
                                            ))}
                                        </select>
                                    </div>
                                )}

                                {/* Protocol selection — only for custom providers */}
                                {dlgProvider.is_custom && (
                                    <div style={{ marginBottom: 12 }}>
                                        <label style={labelStyle}>{t("API Protocol", "API 协议")}</label>
                                        <div style={{ display: "flex", gap: 6 }}>
                                            {(["openai", "anthropic"] as const).map(proto => {
                                                const active = (dlgProvider.protocol || "openai") === proto;
                                                return (
                                                    <button key={proto} onClick={() => dlgUpdateField("protocol", proto)} style={{
                                                        fontSize: "0.76rem", padding: "5px 16px", cursor: "pointer",
                                                        background: active ? colors.primary : colors.surface,
                                                        color: active ? colors.onPrimary : colors.text,
                                                        border: `1px solid ${active ? colors.primary : colors.border}`,
                                                        borderRadius: 4, transition: "all 0.15s",
                                                    }}>
                                                        {proto === "openai" ? "OpenAI" : "Anthropic"}
                                                    </button>
                                                );
                                            })}
                                        </div>
                                        <p style={{ fontSize: "0.68rem", color: colors.textMuted, margin: "4px 0 0 0", lineHeight: 1.4 }}>
                                            {(dlgProvider.protocol || "openai") === "anthropic"
                                                ? t("Uses Anthropic Messages API (x-api-key auth)", "使用 Anthropic Messages API（x-api-key 鉴权）")
                                                : t("Uses OpenAI-compatible API (Bearer token auth)", "使用 OpenAI 兼容 API（Bearer Token 鉴权）")}
                                        </p>
                                    </div>
                                )}

                                {/* User-Agent selection */}
                                <div style={{ marginBottom: 12 }}>
                                    <label style={labelStyle}>User-Agent</label>
                                    <div style={{ display: "flex", gap: 6 }}>
                                        {(["openclaw", "claude-code/2.0.0"] as const).map(ua => {
                                            const active = (dlgProvider.agent_type || "openclaw") === ua;
                                            return (
                                                <button key={ua} onClick={() => dlgUpdateField("agent_type", ua)} style={{
                                                    fontSize: "0.76rem", padding: "5px 16px", cursor: "pointer",
                                                    background: active ? colors.primary : colors.surface,
                                                    color: active ? colors.onPrimary : colors.text,
                                                    border: `1px solid ${active ? colors.primary : colors.border}`,
                                                    borderRadius: 4, transition: "all 0.15s",
                                                }}>
                                                    {ua}
                                                </button>
                                            );
                                        })}
                                    </div>
                                    <p style={{ fontSize: "0.68rem", color: colors.textMuted, margin: "4px 0 0 0", lineHeight: 1.4 }}>
                                        {(dlgProvider.agent_type || "openclaw") === "claude-code/2.0.0"
                                            ? t("For providers requiring Claude Coding Plan identity (e.g. Kimi)", "适用于需要 Claude Coding Plan 身份的服务商（如 Kimi）")
                                            : t("Most providers use OpenClaw identity (e.g. Zhipu Lobster)", "大多数服务商使用 OpenClaw 身份（如智谱龙虾）")}
                                    </p>
                                </div>

                                {/* Custom: editable name */}
                                {dlgProvider.is_custom && (
                                    <div style={{ marginBottom: 12 }}>
                                        <label style={labelStyle}>{t("Provider Name", "服务商名称")}</label>
                                        <input style={inputStyle} value={dlgProvider.name}
                                            onChange={e => dlgUpdateField("name", e.target.value)}
                                            placeholder={t("Custom name", "自定义名称")}
                                            autoCapitalize="off" autoCorrect="off" spellCheck={false} autoComplete="off" />
                                    </div>
                                )}

                                {/* URL */}
                                <div style={{ marginBottom: 12 }}>
                                    <label style={labelStyle}>
                                        {t("API URL", "API 地址 (URL)")}
                                        {!dlgProvider.is_custom && (
                                            <span style={{ fontSize: "0.68rem", color: colors.textMuted, marginLeft: 6 }}>
                                                {t("(preset)", "（预设，无需修改）")}
                                            </span>
                                        )}
                                    </label>
                                    {dlgProvider.is_custom ? (
                                        <input style={inputStyle} value={dlgProvider.url}
                                            onChange={e => dlgUpdateField("url", e.target.value)}
                                            placeholder="https://api.openai.com/v1"
                                            autoCapitalize="off" autoCorrect="off" spellCheck={false} autoComplete="off" />
                                    ) : (
                                        <input style={readonlyStyle} value={dlgProvider.url} readOnly tabIndex={-1} />
                                    )}
                                </div>

                                {/* Model */}
                                <div style={{ marginBottom: 12 }}>
                                    <label style={labelStyle}>
                                        {t("Model Name", "模型名称")}
                                        {!dlgProvider.is_custom && dlgProvider.auth_type !== "oauth" && dlgProvider.auth_type !== "sso" && (
                                            <span style={{ fontSize: "0.68rem", color: colors.textMuted, marginLeft: 6 }}>
                                                {t("(preset)", "（预设，无需修改）")}
                                            </span>
                                        )}
                                        {dlgProvider.auth_type === "oauth" && (
                                            <span style={{ fontSize: "0.68rem", color: colors.textMuted, marginLeft: 6 }}>
                                                {t("(editable)", "（可修改）")}
                                            </span>
                                        )}
                                        {dlgProvider.auth_type === "sso" && (
                                            <span style={{ fontSize: "0.68rem", color: colors.textMuted, marginLeft: 6 }}>
                                                {codegenModelsFetching ? t("(loading models...)", "（加载模型中...）") : t("(select model)", "（选择模型）")}
                                            </span>
                                        )}
                                    </label>
                                    {dlgProvider.is_custom ? (
                                        <input style={inputStyle} value={dlgProvider.model}
                                            onChange={e => dlgUpdateField("model", e.target.value)}
                                            placeholder="gpt-5.4"
                                            autoCapitalize="off" autoCorrect="off" spellCheck={false} autoComplete="off" />
                                    ) : dlgProvider.auth_type === "oauth" ? (
                                        <input style={inputStyle} value={dlgProvider.model}
                                            onChange={e => dlgUpdateField("model", e.target.value)}
                                            placeholder="gpt-5.4"
                                            autoCapitalize="off" autoCorrect="off" spellCheck={false} autoComplete="off" />
                                    ) : dlgProvider.auth_type === "sso" ? (
                                        codegenModels.length > 0 ? (
                                            <select style={inputStyle} value={dlgProvider.model}
                                                onChange={e => dlgUpdateField("model", e.target.value)}>
                                                {!codegenModels.some(m => m.id === dlgProvider.model) && dlgProvider.model && (
                                                    <option value={dlgProvider.model}>{dlgProvider.model}</option>
                                                )}
                                                {codegenModels.map(m => (
                                                    <option key={m.id} value={m.id}>{m.name !== m.id ? `${m.name} (${m.id})` : m.id}</option>
                                                ))}
                                            </select>
                                        ) : (
                                            <input style={inputStyle} value={dlgProvider.model}
                                                onChange={e => dlgUpdateField("model", e.target.value)}
                                                placeholder={codegenModelsFetching ? t("Loading...", "加载中...") : t("Enter model name", "输入模型名称")}
                                                autoCapitalize="off" autoCorrect="off" spellCheck={false} autoComplete="off" />
                                        )
                                    ) : (
                                        <input style={readonlyStyle} value={dlgProvider.model} readOnly tabIndex={-1} />
                                    )}
                                </div>

                                {/* Auth: OAuth login button / no-key hint / API Key input */}
                                {dlgProvider.auth_type === "oauth" ? (
                                    <div>
                                        <label style={labelStyle}>{t("Authentication", "认证方式")}</label>
                                        {dlgProvider.key ? (
                                            <div style={{
                                                display: "flex", alignItems: "center", gap: 10,
                                                padding: "8px 12px", borderRadius: 4,
                                                background: "rgba(34,197,94,0.08)", border: "1px solid rgba(34,197,94,0.25)",
                                            }}>
                                                <span style={{ fontSize: "0.76rem", color: colors.success, flex: 1 }}>
                                                    已认证 {t("OAuth authenticated", "OAuth 已认证")}
                                                </span>
                                                <button onClick={handleOAuthLogin} disabled={oauthBusy} style={{
                                                    fontSize: "0.72rem", padding: "4px 12px", cursor: "pointer",
                                                    background: "transparent", color: "var(--theme-primary)",
                                                    border: `1px solid ${colors.primary}`, borderRadius: 4,
                                                    opacity: oauthBusy ? 0.5 : 1,
                                                }}>
                                                    {oauthBusy ? t("Logging in...", "登录中...") : t("Re-login", "重新登录")}
                                                </button>
                                            </div>
                                        ) : (
                                            <>
                                            <button onClick={handleOAuthLogin} disabled={oauthBusy} style={{
                                                width: "100%", padding: "10px 0", fontSize: "0.8rem",
                                                cursor: oauthBusy ? "default" : "pointer",
                                                background: colors.primary, color: colors.onPrimary,
                                                border: "none", borderRadius: 4,
                                                opacity: oauthBusy ? 0.6 : 1,
                                            }}>
                                                {oauthBusy
                                                    ? `⏳ ${t("Waiting for browser authorization...", "等待浏览器授权...")}`
                                                    : t("Sign in with OpenAI", "使用 OpenAI 账号登录")}
                                            </button>
                                            {oauthBusy && (
                                                <button onClick={() => { CancelOpenAIOAuth(); setOauthBusy(false); }} style={{
                                                    width: "100%", padding: "8px 0", fontSize: "0.76rem",
                                                    cursor: "pointer", marginTop: 6,
                                                    background: "transparent", color: colors.textMuted,
                                                    border: `1px solid ${colors.border}`, borderRadius: 4,
                                                }}>
                                                    {t("Cancel", "取消")}
                                                </button>
                                            )}
                                            {dlgTestResult && !dlgTestResult.ok && !oauthBusy && (
                                                <button onClick={async () => {
                                                    try {
                                                        const msg = await ImportCodexAuth();
                                                        const data = await GetMaclawLLMProviders();
                                                        if (data?.providers) {
                                                            const fresh = data.providers.map((p: LLMProvider) => ({ ...p }));
                                                            setDlgProviders(fresh);
                                                            setProviders(fresh.map((p: LLMProvider) => ({ ...p })));
                                                            setCurrentName(data.current || NONE_PROVIDER);
                                                            setDlgDirty(false);
                                                            onStatusChange?.(true, true);
                                                        }
                                                        setDlgTestResult({ ok: true, msg: msg || "已从 Codex 导入" });
                                                    } catch (e) {
                                                        setDlgTestResult({ ok: false, msg: String(e) });
                                                    }
                                                }} style={{
                                                    width: "100%", padding: "8px 0", fontSize: "0.76rem",
                                                    cursor: "pointer", marginTop: 6,
                                                    background: "transparent", color: colors.primary,
                                                    border: `1px dashed ${colors.primary}`, borderRadius: 4,
                                                }}>
                                                    {t("Import from Codex CLI (if already logged in)", "从 Codex CLI 导入（如已在 Codex 中登录）")}
                                                </button>
                                            )}
                                            </>
                                        )}
                                    </div>
                                ) : dlgProvider.auth_type === "none" ? (
                                    <div>
                                        {/* 当贝 AI login status */}
                                        <label style={labelStyle}>{t("Dangbei AI Login", "当贝 AI 登录")}</label>
                                        {authChecking ? (
                                            <div style={{
                                                padding: "8px 12px", borderRadius: 4, marginBottom: 10,
                                                background: "rgba(99,102,241,0.08)", border: "1px solid rgba(99,102,241,0.25)",
                                            }}>
                                                <span style={{ fontSize: "0.76rem", color: "var(--theme-primary)" }}>
                                                    ⏳ {t("Validating login status...", "正在验证登录状态...")}
                                                </span>
                                            </div>
                                        ) : dangbeiLoggedIn ? (
                                            <div style={{
                                                display: "flex", alignItems: "center", gap: 8,
                                                padding: "8px 12px", borderRadius: 4, marginBottom: 10,
                                                background: "rgba(34,197,94,0.08)", border: "1px solid rgba(34,197,94,0.25)",
                                            }}>
                                                <span style={{ fontSize: "0.76rem", color: colors.success, flex: 1 }}>
                                                    ✅ {t("Logged in to Dangbei AI", "已登录当贝 AI")}
                                                </span>
                                                <button
                                                    disabled={loginBusy}
                                                    onClick={async () => {
                                                        setLoginBusy(true);
                                                        setDlgTestResult(null);
                                                        try {
                                                            await DangbeiLogin();
                                                            setBrowserLaunched(true);
                                                            setDlgTestResult({ ok: true, msg: t("Browser opened. Log in then click 'Finish Login'", "浏览器已打开，请登录后点击「完成登录」") });
                                                        } catch (e) {
                                                            setDlgTestResult({ ok: false, msg: String(e) });
                                                        }
                                                        setLoginBusy(false);
                                                    }}
                                                    style={{
                                                        fontSize: "0.72rem", padding: "4px 12px", cursor: loginBusy ? "default" : "pointer",
                                                        background: "transparent", color: "var(--theme-primary)",
                                                        border: `1px solid ${colors.primary}`, borderRadius: 4,
                                                        opacity: loginBusy ? 0.5 : 1,
                                                    }}
                                                >
                                                    {loginBusy ? "..." : t("Re-login", "重新登录")}
                                                </button>
                                            </div>
                                        ) : (
                                            <div style={{ marginBottom: 10 }}>
                                                {/* Browser detection */}
                                                {browserInfo === null ? (
                                                    <p style={{ fontSize: "0.72rem", color: colors.textMuted }}>{t("Detecting browser...", "检测浏览器...")}</p>
                                                ) : browserInfo.found === "true" ? (
                                                    <div style={{
                                                        display: "flex", alignItems: "center", gap: 8,
                                                        padding: "8px 12px", borderRadius: 4, marginBottom: 8,
                                                        background: "rgba(34,197,94,0.08)", border: "1px solid rgba(34,197,94,0.25)",
                                                    }}>
                                                        <span style={{ fontSize: "0.76rem", color: colors.success, flex: 1 }}>
                                                            ✅ {t(`已找到 ${browserInfo.name === "edge" ? "Edge" : "Chrome"}`, `${browserInfo.name === "edge" ? "Edge" : "Chrome"} found`)}
                                                        </span>
                                                    </div>
                                                ) : (
                                                    <div style={{
                                                        display: "flex", alignItems: "center", gap: 8,
                                                        padding: "8px 12px", borderRadius: 4, marginBottom: 8,
                                                        background: "rgba(239,68,68,0.08)", border: "1px solid rgba(239,68,68,0.25)",
                                                    }}>
                                                        <span style={{ fontSize: "0.76rem", color: colors.danger, flex: 1 }}>
                                                            ❌ {t("Chrome/Edge not found", "未找到 Chrome 或 Edge")}
                                                        </span>
                                                        <button onClick={() => window.open("https://www.google.com/chrome/", "_blank")} style={{
                                                            fontSize: "0.72rem", padding: "4px 12px", cursor: "pointer",
                                                            background: colors.primary, color: colors.onPrimary, border: "none", borderRadius: 4,
                                                        }}>
                                                            {t("Download Chrome", "下载 Chrome")}
                                                        </button>
                                                    </div>
                                                )}
                                                {/* Login button */}
                                                <button
                                                    disabled={loginBusy || browserInfo?.found !== "true"}
                                                    onClick={async () => {
                                                        setLoginBusy(true);
                                                        setDlgTestResult(null);
                                                        try {
                                                            await DangbeiLogin();
                                                            setBrowserLaunched(true);
                                                            setDlgTestResult({ ok: true, msg: t("Browser opened. Log in to Dangbei AI, then click 'Finish Login' below", "浏览器已打开，请在浏览器中登录当贝 AI，完成后点击下方「完成登录」按钮") });
                                                        } catch (e) {
                                                            setDlgTestResult({ ok: false, msg: String(e) });
                                                        }
                                                        setLoginBusy(false);
                                                    }}
                                                    style={{
                                                        width: "100%", padding: "10px 0", fontSize: "0.8rem",
                                                        cursor: loginBusy ? "default" : "pointer",
                                                        background: colors.primary, color: colors.onPrimary,
                                                        border: "none", borderRadius: 4,
                                                        opacity: (loginBusy || browserInfo?.found !== "true") ? 0.6 : 1,
                                                    }}
                                                >
                                                    {loginBusy ? `⏳ ${t("Launching browser...", "正在启动浏览器...")}` : t("Login to Dangbei AI", "登录当贝 AI")}
                                                </button>
                                            </div>
                                        )}

                                        {/* Finish login button — shown after browser is launched */}
                                        {browserLaunched && (
                                            <div style={{ marginBottom: 10 }}>
                                                <button
                                                    disabled={loginBusy}
                                                    onClick={async () => {
                                                        setLoginBusy(true);
                                                        setDlgTestResult(null);
                                                        try {
                                                            await DangbeiFinishLogin();
                                                            setDangbeiLoggedIn(true);
                                                            setBrowserLaunched(false);
                                                            // Auto-start proxy after successful login
                                                            try {
                                                                const running = await IsFreeProxyRunning();
                                                                if (!running) {
                                                                    await StartFreeProxy();
                                                                    setProxyRunning(true);
                                                                }
                                                            } catch { /* non-fatal */ }
                                                            setDlgTestResult({ ok: true, msg: t("Login successful, proxy auto-started", "登录成功，代理已自动启动") });
                                                        } catch (e) {
                                                            setDlgTestResult({ ok: false, msg: String(e) });
                                                        }
                                                        setLoginBusy(false);
                                                    }}
                                                    style={{
                                                        width: "100%", padding: "10px 0", fontSize: "0.8rem",
                                                        cursor: loginBusy ? "default" : "pointer",
                                                        background: colors.success, color: colors.onPrimary,
                                                        border: "none", borderRadius: 4,
                                                        opacity: loginBusy ? 0.6 : 1,
                                                    }}
                                                >
                                                    {loginBusy ? `⏳ ${t("Closing browser & extracting login info...", "正在关闭浏览器并提取登录信息...")}` : t("✅ I've logged in, finish login", "✅ 我已在浏览器中登录，完成登录")}
                                                </button>
                                                {dlgTestResult && (
                                                    <div style={{
                                                        marginTop: 8, padding: "8px 12px", borderRadius: 4, fontSize: "0.74rem",
                                                        lineHeight: 1.5, whiteSpace: "pre-wrap", wordBreak: "break-word",
                                                        background: dlgTestResult.ok ? "rgba(34,197,94,0.1)" : "rgba(239,68,68,0.1)",
                                                        border: `1px solid ${dlgTestResult.ok ? "rgba(34,197,94,0.3)" : "rgba(239,68,68,0.3)"}`,
                                                        color: dlgTestResult.ok ? colors.success : colors.danger,
                                                    }}>
                                                        {dlgTestResult.ok ? "✅ " : "❌ "}{dlgTestResult.msg}
                                                    </div>
                                                )}
                                            </div>
                                        )}

                                        <p style={{ fontSize: "0.68rem", color: colors.textMuted, margin: "0 0 12px 0", lineHeight: 1.5 }}>
                                            💡 {t(
                                                "已登录的 cookie 会自动保存，下次打开无需重复登录。如 cookie 失效会自动提示重新登录。",
                                                "Login cookies are saved automatically. If expired, you'll be prompted to re-login."
                                            )}
                                        </p>

                                        {/* Model selection */}
                                        <label style={labelStyle}>{t("Model Selection", "模型选择")}</label>
                                        <div style={{ display: "flex", gap: 4, flexWrap: "wrap", marginBottom: 12 }}>
                                            {freeModels.map(m => {
                                                const active = freeSelectedModel === m.id;
                                                return (
                                                    <button key={m.id} onClick={() => {
                                                        setFreeSelectedModel(m.id);
                                                        SetFreeProxyModel(m.id).catch(() => {});
                                                    }} style={{
                                                        fontSize: "0.72rem", padding: "4px 10px", cursor: "pointer",
                                                        background: active ? colors.primary : colors.surface,
                                                        color: active ? colors.onPrimary : colors.text,
                                                        border: `1px solid ${active ? colors.primary : colors.border}`,
                                                        borderRadius: 4, transition: "all 0.15s",
                                                    }}>
                                                        {m.name}
                                                    </button>
                                                );
                                            })}
                                        </div>

                                        {/* Proxy status */}
                                        <label style={labelStyle}>{t("Proxy Status", "代理状态")}</label>
                                        <div style={{
                                            display: "flex", alignItems: "center", gap: 10,
                                            padding: "8px 12px", borderRadius: 4,
                                            background: proxyRunning ? "rgba(34,197,94,0.08)" : "rgba(239,68,68,0.08)",
                                            border: `1px solid ${proxyRunning ? "rgba(34,197,94,0.25)" : "rgba(239,68,68,0.25)"}`,
                                        }}>
                                            <span style={{
                                                width: 8, height: 8, borderRadius: "50%",
                                                background: proxyRunning ? colors.success : colors.danger,
                                                display: "inline-block", flexShrink: 0,
                                            }} />
                                            <span style={{ fontSize: "0.76rem", color: proxyRunning ? colors.success : colors.danger, flex: 1 }}>
                                                {proxyRunning
                                                    ? t("Proxy running (localhost:18099)", "代理服务运行中 (localhost:18099)")
                                                    : t("Proxy not running", "代理服务未运行")}
                                            </span>
                                            <button
                                                disabled={proxyBusy}
                                                onClick={async () => {
                                                    setProxyBusy(true);
                                                    setDlgTestResult(null);
                                                    try {
                                                        if (proxyRunning) { await StopFreeProxy(); setProxyRunning(false); }
                                                        else { await StartFreeProxy(); setProxyRunning(true); }
                                                    } catch (e) {
                                                        setDlgTestResult({ ok: false, msg: String(e) });
                                                        // Refresh actual status
                                                        IsFreeProxyRunning().then(r => setProxyRunning(r)).catch(() => {});
                                                    }
                                                    setProxyBusy(false);
                                                }}
                                                style={{
                                                    fontSize: "0.72rem", padding: "4px 12px", cursor: proxyBusy ? "default" : "pointer",
                                                    background: proxyRunning ? "transparent" : colors.primary,
                                                    color: proxyRunning ? colors.danger : colors.onPrimary,
                                                    border: `1px solid ${proxyRunning ? colors.danger : colors.primary}`,
                                                    borderRadius: 4, opacity: proxyBusy ? 0.5 : 1,
                                                }}
                                            >
                                                {proxyBusy ? "..." : proxyRunning ? t("Stop", "停止") : t("Start", "启动")}
                                            </button>
                                        </div>
                                        <p style={{ fontSize: "0.68rem", color: colors.textMuted, margin: "6px 0 0 0", lineHeight: 1.4 }}>
                                            提示 {t(
                                                "Use multiple LLMs for free via Dangbei AI, no API key needed.",
                                                "通过当贝 AI 免费使用多种大模型，无需 API Key。"
                                            )}
                                        </p>
                                    </div>
                                ) : (
                                    <div>
                                        <label style={labelStyle}>{t("API Key", "API Key")} <span style={{ color: colors.danger }}>*</span></label>
                                        <input style={inputStyle} type="password" value={dlgProvider.key}
                                            onChange={e => dlgUpdateField("key", e.target.value)}
                                            placeholder={((dlgProvider.name === "智谱龙虾" || dlgProvider.name === "智谱编程") || (dlgProvider.protocol || "openai") === "anthropic") ? "xxxxxxxx.yyyyyyyy" : "sk-..."}
                                            autoCapitalize="off" autoCorrect="off" spellCheck={false} autoComplete="off" />
                                    </div>
                                )}

                                {/* Context Length */}
                                <div style={{ marginTop: 12 }}>
                                    <label style={labelStyle}>{t("Context Length (tokens)", "上下文长度 (tokens)")}</label>
                                    <input style={inputStyle} type="number" min={0} step={1000}
                                        autoCapitalize="off" autoCorrect="off" spellCheck={false} autoComplete="off"
                                        value={dlgProvider.context_length || ""}
                                        onChange={e => dlgUpdateField("context_length", e.target.value)}
                                        placeholder="128000" />
                                    <p style={{ fontSize: "0.68rem", color: colors.textMuted, margin: "4px 0 0 0", lineHeight: 1.4 }}>
                                        {t(
                                            "Max context window of the model. GLM supports 180000. Defaults to 128000 if empty.",
                                            "模型支持的最大上下文长度。GLM 可支持 180000，留空默认 128000。"
                                        )}
                                    </p>
                                </div>

                                {/* Vision Support Toggle */}
                                <div style={{
                                    marginTop: 12, display: "flex", alignItems: "center",
                                    justifyContent: "space-between", gap: 10,
                                }}>
                                    <div style={{ flex: 1 }}>
                                        <label style={{ ...labelStyle, marginBottom: 2 }}>
                                            {t("Vision Support", "视觉支持")}
                                        </label>
                                        <p style={{ fontSize: "0.68rem", color: colors.textMuted, margin: 0, lineHeight: 1.4 }}>
                                            {dlgProvider.supports_vision
                                                ? t("Supports image input (WeChat images understood by model)", "支持图片输入（微信发图可被模型理解）")
                                                : t("No vision (images saved as files, not sent to model)", "不支持视觉（图片会保存为文件，不发送给模型）")}
                                        </p>
                                        <p style={{ fontSize: "0.64rem", color: colors.textMuted, margin: "2px 0 0 0", lineHeight: 1.4 }}>
                                            {t(
                                                "Vision support is auto-detected during the initial test-and-save. If inaccurate, you can adjust it manually and save again.",
                                                "首次测试并保存时会自动检测视觉能力；如果不准确，可手动调整后再保存。"
                                            )}
                                        </p>
                                    </div>
                                    <input type="checkbox" checked={!!dlgProvider.supports_vision}
                                        onChange={e => {
                                            if (dlgSelectedIdx === null) return;
                                            setDlgProviders(prev => {
                                                const copy = [...prev];
                                                copy[dlgSelectedIdx] = { ...copy[dlgSelectedIdx], supports_vision: e.target.checked };
                                                return copy;
                                            });
                                            setDlgDirty(true);
                                            setDlgTestResult(null);
                                        }}
                                        style={{ width: 18, height: 18, accentColor: "var(--theme-primary)", cursor: "pointer", flexShrink: 0 }} />
                                </div>


                            </div>
                        )}


                        {/* Footer */}
                        <div style={{ display: "flex", gap: 10, alignItems: "center", justifyContent: "flex-end", marginTop: 20 }}>
                            {dlgDirty && <span style={{ fontSize: "0.68rem", color: colors.warning, marginRight: "auto" }}>{t("unsaved", "未保存")}</span>}
                            <button onClick={closeDialog} style={{
                                fontSize: "0.76rem", padding: "6px 18px", cursor: "pointer",
                                background: colors.bg, color: colors.text,
                                border: `1px solid ${colors.border}`, borderRadius: 4,
                            }}>
                                {t("Cancel", "取消")}
                            </button>
                            <button onClick={dlgHandleSave} disabled={dlgSaving || oauthBusy || (!dlgDirty && !dlgTested)} style={{
                                fontSize: "0.76rem", padding: "6px 18px", cursor: (dlgDirty || dlgTested) ? "pointer" : "default",
                                background: (dlgDirty || dlgTested) ? colors.primary : colors.bg, color: (dlgDirty || dlgTested) ? colors.onPrimary : colors.textMuted,
                                border: "none", borderRadius: 4, opacity: dlgSaving ? 0.6 : 1,
                            }}>
                                {dlgSaving ? t("Testing & Saving...", "检测并保存中...") : dlgTested ? t("Save Changes", "保存修改") : t("Test & Save", "检测并保存")}
                            </button>
                        </div>

                        {/* Test result */}
                        {dlgTestResult && (
                            <div style={{
                                marginTop: 12, padding: "8px 12px", borderRadius: 4, fontSize: "0.74rem",
                                lineHeight: 1.5, whiteSpace: "pre-wrap", wordBreak: "break-word",
                                background: dlgTestResult.ok ? "rgba(34,197,94,0.1)" : "rgba(239,68,68,0.1)",
                                border: `1px solid ${dlgTestResult.ok ? "rgba(34,197,94,0.3)" : "rgba(239,68,68,0.3)"}`,
                                color: dlgTestResult.ok ? colors.success : colors.danger,
                            }}>
                                {dlgTestResult.ok
                                    ? `✅ ${t("Connection OK, saved", "连接成功，已保存")}\n${dlgTestResult.msg}`
                                    : `❌ ${t("Connection failed, not saved", "连接失败，未保存")}\n${dlgTestResult.msg}`}
                            </div>
                        )}
                    </div>
                </div>
            )}
        </div>
    );
}

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
    FetchCodeGenModels,
    FetchProviderModels,
    SaveCodeGenModelChoice,
    GetHubLLMServiceStatus,
} from "../../../wailsjs/go/main/App";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";
import { colors } from "./styles";
import { HUB_SERVICE_PROVIDER_NAME, KNOWN_OPENAI_ENDPOINTS, LLM_CONFIG_LOAD_TIMEOUT_MS, NONE_PROVIDER, hubCreditGrants, hubOfficialStatus, inputStyle, labelStyle, readonlyStyle, withTimeout, type HubLLMServiceStatus, type LLMProvider } from "./LLMConfigPanelShared";
import { UsageDisplay } from "./UsageDisplay";
import { TokenUsagePanel } from "./TokenUsagePanel";
import { PROVIDER_LOGOS } from "./providerLogos";
import { useDialog } from "../CustomDialog";
import { KNOWN_USER_AGENTS, commitCustomAgentValue, customAgentSeedForProvider, editableCustomAgentValue, effectiveAgentType, isKnownUserAgent } from "./userAgent";

interface Props {
    lang?: string;
    codexModels?: unknown[];
    onStatusChange?: (online: boolean, configured: boolean) => void;
    onProviderChanged?: () => void;
}

export function LLMConfigPanel({ lang, onStatusChange, onProviderChanged }: Props) {
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
    const [dlgHubSelected, setDlgHubSelected] = useState(false); // true when "MaClaw 官方" is selected in dialog
    const [dlgSaving, setDlgSaving] = useState(false);
    const [dlgTestResult, setDlgTestResult] = useState<{ ok: boolean; msg: string } | null>(null);
    const [dlgToast, setDlgToast] = useState<{ ok: boolean; title: string; detail?: string } | null>(null);
    const [dlgDirty, setDlgDirty] = useState(false);
    const [dlgTested, setDlgTested] = useState(false); // true after successful test; allows save-only on subsequent saves
    const [oauthBusy, setOauthBusy] = useState(false);
    const [codegenModels, setCodegenModels] = useState<{id: string; name: string}[]>([]);
    const [codegenModelsFetching, setCodegenModelsFetching] = useState(false);
    // Generic provider model list (for non-SSO/non-OAuth providers)
    const [providerModels, setProviderModels] = useState<{id: string; name: string}[]>([]);
    const [providerModelsFetching, setProviderModelsFetching] = useState(false);
    const [providerModelsError, setProviderModelsError] = useState<string | null>(null);
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
                onProviderChanged?.();
                setDlgTestResult({ ok: true, msg: t("OAuth login successful", "OAuth 登录成功") });
                setTimeout(() => setDlgOpen(false), 1200);
            }
        } catch (e) {
            setDlgTestResult({ ok: false, msg: String(e) });
        }
        setOauthBusy(false);
    }, [t, onStatusChange, onProviderChanged, loadHubServiceStatus]);

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

    // Reload providers and hub service status when the Hub LLM service changes
    // (e.g. after a successful redemption in the HubServiceRedeemPanel tab).
    useEffect(() => {
        const handler = () => {
            console.info("[LLMConfigPanel] hub-llm-service-changed event received, reloading");
            loadProviders();
        };
        const cleanup = EventsOn("hub-llm-service-changed", handler);
        return () => { if (typeof cleanup === 'function') cleanup(); else EventsOff("hub-llm-service-changed"); };
    }, [loadProviders]);

    const isNone = currentName === NONE_PROVIDER;
    const hasHubEntitlement = !!hubServiceStatus?.active || hubCreditGrants(hubServiceStatus).length > 0;
    const hubOfficial = hubOfficialStatus(hubServiceStatus, lang, t);
    const hasHubProviderInDialog = dlgProviders.some(p => p.name === HUB_SERVICE_PROVIDER_NAME);
    const hubSelectionAlreadySynced = currentName === HUB_SERVICE_PROVIDER_NAME && hasHubProviderInDialog;
    const hubAvailableModels = (hubServiceStatus?.available_models || []).filter(Boolean);
    const hubModelLabel = hubAvailableModels.length ? hubAvailableModels.join(", ") : (hubServiceStatus?.default_model || "auto");

    /* ── Dialog helpers ── */

    const openDialog = useCallback(() => {
        const snapshot = providers.map(p => ({ ...p }));
        setDlgProviders(snapshot);
        // When the current provider is the Hub service, pre-select the dedicated Hub button
        if (currentName === HUB_SERVICE_PROVIDER_NAME && hasHubEntitlement) {
            setDlgSelectedIdx(null);
            setDlgHubSelected(true);
        } else {
            const idx = currentName === NONE_PROVIDER ? null : snapshot.findIndex(p => p.name === currentName);
            const shouldSelectHub = (idx === null || idx === -1) && hasHubEntitlement;
            setDlgSelectedIdx(shouldSelectHub ? null : (idx === -1 ? null : idx));
            setDlgHubSelected(shouldSelectHub);
        }
        setDlgSaving(false);
        setDlgTestResult(null);
        setDlgDirty(false);
        setDlgTested(false);
        setDlgOpen(true);
    }, [providers, currentName, hasHubEntitlement]);

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

    useEffect(() => {
        if (!dlgTestResult) {
            setDlgToast(null);
            return;
        }
        const raw = String(dlgTestResult.msg || "").trim();
        const saved = t("Saved", "\u5df2\u4fdd\u5b58");
        const isSavedOnly = raw === saved || raw === "Saved" || raw === "\u5df2\u4fdd\u5b58";
        const title = dlgHubSelected
            ? raw
            : dlgTestResult.ok
                ? (isSavedOnly ? saved : t("Connection OK, saved", "\u8fde\u63a5\u6210\u529f\uff0c\u5df2\u4fdd\u5b58"))
                : t("Connection failed, not saved", "\u8fde\u63a5\u5931\u8d25\uff0c\u672a\u4fdd\u5b58");
        const detail = dlgHubSelected || (dlgTestResult.ok && isSavedOnly) ? "" : raw;
        setDlgToast({ ok: dlgTestResult.ok, title, detail });
        const timeout = window.setTimeout(() => setDlgToast(null), dlgTestResult.ok ? 7000 : 10000);
        return () => window.clearTimeout(timeout);
    }, [dlgHubSelected, dlgTestResult, t]);

    // Determine auth type of selected provider for conditional effects
    const dlgAuthType = dlgSelectedIdx !== null ? dlgProviders[dlgSelectedIdx]?.auth_type : undefined;

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

    // Clear generic provider models when switching providers or closing dialog
    useEffect(() => {
        setProviderModels([]);
        setProviderModelsError(null);
    }, [dlgSelectedIdx, dlgOpen]);

    const dlgIsNone = dlgSelectedIdx === null && !dlgHubSelected;
    const dlgProvider = dlgSelectedIdx !== null ? dlgProviders[dlgSelectedIdx] ?? null : null;

    // Handler: fetch models from any provider's /models endpoint
    const handleFetchProviderModels = useCallback(async () => {
        if (!dlgProvider || !dlgProvider.url || !dlgProvider.key) return;
        setProviderModelsFetching(true);
        setProviderModelsError(null);
        setProviderModels([]);
        try {
            const models = await FetchProviderModels(dlgProvider.url, dlgProvider.key, dlgProvider.protocol || "openai", effectiveAgentType(dlgProvider));
            setProviderModels(models || []);
            if (!models || models.length === 0) {
                setProviderModelsError(t("No models returned", "服务商返回了空的模型列表"));
            }
        } catch (e) {
            setProviderModelsError(String(e));
            setProviderModels([]);
        } finally {
            setProviderModelsFetching(false);
        }
    }, [dlgProvider, t]);

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
        if (["url", "key", "model", "protocol", "agent_type"].includes(field)) {
            setDlgTested(false);
        }
    }, [dlgSelectedIdx]);

    const dlgSelectProvider = useCallback((idx: number | null) => {
        setDlgSelectedIdx(idx);
        setDlgHubSelected(false);
        setDlgDirty(true);
        setDlgTestResult(null);
        setDlgTested(false);
    }, []);

    const dlgSelectHubService = useCallback(() => {
        setDlgSelectedIdx(null);
        setDlgHubSelected(true);
        setDlgDirty(false);
        setDlgTestResult(null);
        setDlgTested(false);
    }, []);

    /** Save Hub service as the current LLM provider (no test needed — Hub-managed). */
    const dlgHandleSaveHubService = useCallback(async () => {
        if (hubSelectionAlreadySynced) return; // already active and synced
        setDlgSaving(true);
        setDlgTestResult(null);
        try {
            await SaveMaclawLLMProviders(dlgProviders, HUB_SERVICE_PROVIDER_NAME);
            try {
                const freshData = await GetMaclawLLMProviders();
                const fresh = (freshData?.providers || dlgProviders).map((p: LLMProvider) => ({ ...p }));
                setDlgProviders(fresh);
                setProviders(fresh.map((p: LLMProvider) => ({ ...p })));
                setCurrentName(freshData?.current || HUB_SERVICE_PROVIDER_NAME);
            } catch {
                setProviders(dlgProviders.map(p => ({ ...p })));
                setCurrentName(HUB_SERVICE_PROVIDER_NAME);
            }
            onStatusChange?.(true, true);
            onProviderChanged?.();
            setDlgTestResult({ ok: true, msg: t("Saved", "已保存") });
            setTimeout(() => setDlgOpen(false), 800);
        } catch (e) {
            setDlgTestResult({ ok: false, msg: String(e) });
        }
        setDlgSaving(false);
    }, [dlgProviders, hubSelectionAlreadySynced, t, onStatusChange, onProviderChanged]);

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
                onProviderChanged?.();
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
                onProviderChanged?.();
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
                onProviderChanged?.();
                setDlgTestResult({ ok: true, msg: t("Saved", "已保存") });
                setTimeout(() => setDlgOpen(false), 800);
                setDlgSaving(false);
                return;
            }

            const testResult = await TestMaclawLLM({ url: sp.url, key: sp.key, model: sp.model, protocol: sp.protocol || "openai", agent_type: effectiveAgentType(sp), wire_api: sp.wire_api || "" });
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
            onProviderChanged?.();
            // Don't auto-close: let user review the vision probe result and
            // manually override supports_vision if needed before closing.
        } catch (e) {
            setDlgTestResult({ ok: false, msg: String(e) });
        }
        setDlgSaving(false);
    };

    if (loading) return <div className="llm-config-loading">{t("Loading...", "加载中...")}</div>;

    return (
        <div className="llm-config-panel">
            {dlgToast && (
                <div role={dlgToast.ok ? "status" : "alert"} aria-live="polite" style={{
                    position: "fixed", top: 42, left: "50%", transform: "translateX(-50%)",
                    zIndex: 10000, width: "min(92vw, 380px)", padding: "8px 12px",
                    borderRadius: 6, fontSize: "0.74rem", lineHeight: 1.45,
                    boxShadow: "0 10px 24px rgba(0,0,0,0.22)",
                    background: colors.surface,
                    border: `1px solid ${colors.border}`,
                    borderLeft: `3px solid ${dlgToast.ok ? colors.success : colors.danger}`,
                    color: colors.text,
                }}>
                    <div style={{ fontWeight: 700, color: dlgToast.ok ? colors.success : colors.danger }}>
                        {dlgToast.ok ? "✓" : "!"} {dlgToast.title}
                    </div>
                    {dlgToast.detail && (
                        <div style={{ marginTop: 3, color: colors.textSecondary, whiteSpace: "pre-wrap", wordBreak: "break-word" }}>
                            {dlgToast.detail}
                        </div>
                    )}
                </div>
            )}
            <div className="llm-config-panel__intro">
                <p>
                    {t(
                        "Select LLM provider (OpenAI / Anthropic supported)",
                        "选择 LLM 服务商（支持 OpenAI / Anthropic 协议）"
                    )}
                </p>
                <button className="llm-config-primary-action" onClick={openDialog} style={{
                    fontSize: "0.76rem", padding: "6px 18px", cursor: "pointer",
                    background: colors.primaryLight, color: colors.primaryDark, border: `1px solid ${colors.primary}`, borderRadius: 4, flexShrink: 0, marginLeft: 12,
                }}>
                    {t("Configure", "配置")}
                </button>
            </div>

            {/* Current provider summary */}
            <div className="llm-config-summary" style={{
                marginBottom: 16, padding: "10px 16px", borderRadius: 6,
                border: `1px solid ${colors.border}`, background: colors.surface,
                display: "flex", justifyContent: "space-between", alignItems: "center",
            }}>
                <span className="llm-config-summary__label" style={{ fontSize: "0.76rem", color: colors.textSecondary }}>
                    {t("Provider", "当前服务商")}
                </span>
                <span className="llm-config-summary__value" style={{ fontSize: "0.76rem", fontWeight: 600, color: isNone ? (hasHubEntitlement ? colors.warning : colors.danger) : colors.text }}>
                    {isNone
                        ? (hasHubEntitlement ? t("MaClaw Official", "MaClaw 官方") : t("None", "未配置"))
                        : currentName}
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
            <div className="llm-config-card" style={{
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

            {isNone && !hasHubEntitlement && (
                <div className="llm-config-alert llm-config-alert--danger" style={{
                    padding: "8px 12px", borderRadius: 4, fontSize: "0.74rem", lineHeight: 1.5,
                    background: "rgba(239,68,68,0.08)", border: "1px solid rgba(239,68,68,0.25)", color: colors.danger,
                }}>
                    提示 {t("Without a provider, MaClaw remote will be disabled.", "未配置服务商时，MaClaw 远程能力将不可用。")}
                </div>
            )}

            {/* ── Config Dialog ── */}
            {dlgOpen && (
                <div className="llm-config-dialog-overlay" style={{
                    position: "fixed", top: 0, left: 0, right: 0, bottom: 0,
                    background: "rgba(0,0,0,0.4)", display: "flex",
                    alignItems: "center", justifyContent: "center", zIndex: 9999,
                }} onMouseDown={closeDialog}>
                    <div className="llm-config-dialog" style={{
                        background: colors.surface, borderRadius: 12, padding: "24px 28px",
                        maxWidth: 520, width: "92%", maxHeight: "85vh", overflowY: "auto",
                        boxShadow: "0 16px 48px rgba(0,0,0,0.22)",
                    }} onMouseDown={e => e.stopPropagation()}>

                        {/* Header */}
                        <div className="llm-config-dialog__header" style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 18 }}>
                            <span className="llm-config-dialog__title" style={{ fontSize: "0.92rem", fontWeight: 700, color: colors.text }}>
                                {t("MaClaw LLM Configuration", "MaClaw LLM 配置")}
                            </span>
                            <button className="llm-config-dialog__close" onClick={closeDialog} aria-label={t("Close", "关闭")} style={{
                                border: "none", background: "transparent", cursor: "pointer",
                                fontSize: "1.1rem", color: colors.textSecondary, padding: "0 4px",
                            }}>✕</button>
                        </div>

                        {/* Provider selection */}
                        <div className="llm-config-provider-section" style={{ marginBottom: 16 }}>
                            <label style={labelStyle}>{t("Select Provider", "选择服务商")}</label>
                            <div className="llm-config-provider-grid" style={{ display: "flex", gap: 4, flexWrap: "wrap" }}>
                                {hasHubEntitlement && !hasHubProviderInDialog && (
                                    <button className="llm-config-provider-chip" onClick={dlgSelectHubService} style={{
                                        fontSize: "0.76rem", padding: "5px 14px", cursor: "pointer",
                                        background: dlgHubSelected ? colors.primaryLight : colors.surface,
                                        color: dlgHubSelected ? colors.primaryDark : colors.text,
                                        border: `1px solid ${dlgHubSelected ? colors.primary : colors.success}`,
                                        borderRadius: 4, transition: "all 0.15s",
                                        display: "inline-flex", alignItems: "center", gap: 5,
                                    }}>
                                        {t("MaClaw Official", "MaClaw \u5b98\u65b9")}
                                        {hubOfficial.kind !== "active" && (
                                            <span style={{ fontSize: "0.6rem", lineHeight: 1, padding: "2px 5px", borderRadius: 6, background: colors.warningBg, color: colors.warning, border: `1px solid ${colors.warning}` }}>
                                                {hubOfficial.label}
                                            </span>
                                        )}
                                    </button>
                                )}
                                {dlgProviders.map((p, i) => {
                                    const isHubProvider = hasHubEntitlement && p.name === HUB_SERVICE_PROVIDER_NAME;
                                    const active = isHubProvider ? dlgHubSelected : dlgSelectedIdx === i;
                                    const badge: Record<string, string> = {};
                                    const tag = isHubProvider && hubOfficial.kind !== "active" ? hubOfficial.label : badge[p.name];
                                    return (
                                        <button className="llm-config-provider-chip" key={i} onClick={() => isHubProvider ? dlgSelectHubService() : dlgSelectProvider(i)} style={{
                                            fontSize: "0.76rem", padding: "5px 14px", cursor: "pointer",
                                            background: active ? colors.primaryLight : colors.surface,
                                            color: active ? colors.primaryDark : colors.text,
                                            border: `1px solid ${active ? colors.primary : isHubProvider ? colors.success : colors.border}`,
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
                                                    background: isHubProvider ? colors.warningBg : active ? colors.warningBg : colors.primaryLight,
                                                    color: isHubProvider ? colors.warning : active ? colors.warning : colors.primaryDark, border: `1px solid ${isHubProvider || active ? colors.warning : colors.primary}`, fontWeight: 600, pointerEvents: "none",
                                                }}>{tag}</span>
                                            )}
                                        </button>
                                    );
                                })}
                                {/* "None" button */}
                                <button className="llm-config-provider-chip" onClick={() => dlgSelectProvider(null)} style={{
                                    fontSize: "0.76rem", padding: "5px 14px", cursor: "pointer",
                                    background: dlgIsNone ? colors.primaryLight : colors.surface,
                                    color: dlgIsNone ? colors.primaryDark : colors.text,
                                    border: `1px solid ${dlgIsNone ? colors.primary : colors.border}`,
                                    borderRadius: 4, transition: "all 0.15s",
                                }}>
                                    {t("None", "暂不配置")}
                                </button>
                            </div>
                        </div>

                        {/* None warning */}
                        {dlgIsNone && (
                            <div className="llm-config-alert llm-config-alert--danger" style={{
                                padding: "8px 12px", borderRadius: 4, fontSize: "0.74rem", lineHeight: 1.5,
                                background: "rgba(239,68,68,0.08)", border: "1px solid rgba(239,68,68,0.25)", color: colors.danger,
                                marginBottom: 16,
                            }}>
                                {t("Without a provider, MaClaw remote will be disabled.", "不配置服务商，MaClaw 远程将失效。")}
                            </div>
                        )}

                        {/* Hub-managed MaClaw Official details */}
                        {dlgHubSelected && hasHubEntitlement && (
                            <div style={{
                                marginBottom: 16,
                                padding: "18px 20px",
                                borderRadius: 12,
                                border: "1px solid color-mix(in srgb, var(--theme-success) 52%, transparent)",
                                background: "linear-gradient(135deg, color-mix(in srgb, var(--theme-surface) 86%, white), color-mix(in srgb, var(--theme-success-bg) 78%, var(--theme-surface)))",
                                boxShadow: "0 18px 42px rgba(15, 23, 42, 0.18)",
                                position: "relative",
                                overflow: "hidden",
                            }}>
                                <div style={{
                                    position: "absolute",
                                    inset: 0,
                                    pointerEvents: "none",
                                    background: "radial-gradient(circle at 16% 0%, rgba(255,255,255,0.34), transparent 34%), linear-gradient(135deg, rgba(255,255,255,0.16), transparent 48%)",
                                }} />
                                <div style={{ position: "relative", display: "grid", gap: 14 }}>
                                    <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", gap: 12 }}>
                                        <div>
                                            <div style={{ fontSize: "0.92rem", fontWeight: 800, color: "color-mix(in srgb, var(--theme-success) 74%, var(--theme-text-primary))", marginBottom: 6 }}>
                                                {t("MaClaw Official", "MaClaw \u5b98\u65b9")}
                                            </div>
                                            <div style={{ fontSize: "0.76rem", color: colors.textSecondary, lineHeight: 1.7, maxWidth: 620 }}>
                                                {t("Your account is authorized for MaClaw official LLM service. Requests are routed through the managed Hub service without exposing external API details.", "\u5f53\u524d\u8d26\u53f7\u5df2\u5f00\u901a MaClaw \u5b98\u65b9\u6a21\u578b\u670d\u52a1\u3002\u8bf7\u6c42\u5c06\u901a\u8fc7 Hub \u6258\u7ba1\u670d\u52a1\u5b89\u5168\u8f6c\u53d1\uff0c\u65e0\u9700\u5c55\u793a\u5bf9\u5916\u63a5\u53e3\u4fe1\u606f\u3002")}
                                            </div>
                                        </div>
                                        <span style={{
                                            flex: "0 0 auto",
                                            padding: "4px 10px",
                                            borderRadius: 999,
                                            border: "1px solid color-mix(in srgb, var(--theme-success) 60%, transparent)",
                                            background: "rgba(255,255,255,0.16)",
                                            color: "color-mix(in srgb, var(--theme-success) 78%, var(--theme-text-primary))",
                                            fontSize: "0.68rem",
                                            fontWeight: 800,
                                        }}>
                                            {hubOfficial.label}
                                        </span>
                                    </div>

                                    <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(160px, 1fr))", gap: 10 }}>
                                        <div style={{ padding: "10px 12px", borderRadius: 10, background: "rgba(255,255,255,0.12)", border: "1px solid var(--theme-border-subtle)" }}>
                                            <div style={{ fontSize: "0.68rem", color: colors.textMuted, marginBottom: 5, fontWeight: 700 }}>{t("Service Status", "\u670d\u52a1\u72b6\u6001")}</div>
                                            <div style={{ fontSize: "0.82rem", color: colors.text, fontWeight: 800 }}>{hubOfficial.label}</div>
                                        </div>
                                        <div style={{ padding: "10px 12px", borderRadius: 10, background: "rgba(255,255,255,0.12)", border: "1px solid var(--theme-border-subtle)" }}>
                                            <div style={{ fontSize: "0.68rem", color: colors.textMuted, marginBottom: 5, fontWeight: 700 }}>{t("Model", "\u6a21\u578b")}</div>
                                            <div style={{ fontSize: "0.82rem", color: colors.text, fontWeight: 800, wordBreak: "break-word" }}>{hubModelLabel}</div>
                                        </div>
                                        <div style={{ padding: "10px 12px", borderRadius: 10, background: "rgba(255,255,255,0.12)", border: "1px solid var(--theme-border-subtle)" }}>
                                            <div style={{ fontSize: "0.68rem", color: colors.textMuted, marginBottom: 5, fontWeight: 700 }}>{t("Configuration", "\u914d\u7f6e\u65b9\u5f0f")}</div>
                                            <div style={{ fontSize: "0.82rem", color: colors.text, fontWeight: 800 }}>{t("No setup needed", "\u65e0\u9700\u914d\u7f6e")}</div>
                                        </div>
                                    </div>

                                    <div style={{ fontSize: "0.72rem", color: colors.textMuted, lineHeight: 1.6 }}>
                                        {hubOfficial.detail || t("Use this managed service directly, or select another provider above if you need to override it.", "可直接使用此托管服务；如需改用其他服务商，可在上方切换。")}
                                    </div>
                                </div>
                            </div>
                        )}

                        {/* Provider config fields */}
                        {!dlgIsNone && dlgProvider && (
                            <div className="llm-config-form-card" style={{
                                marginBottom: 16, padding: "14px", borderRadius: 6,
                                border: `1px solid ${colors.border}`, background: colors.bg,
                            }}>
                                <div className="llm-config-form-card__title" style={{ fontSize: "0.78rem", fontWeight: 600, color: colors.text, marginBottom: 12 }}>
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
                                                        background: active ? colors.primaryLight : colors.surface,
                                                        color: active ? colors.primaryDark : colors.text,
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
                                    {(() => {
                                        const currentAgent = effectiveAgentType(dlgProvider);
                                        const isCustom = !isKnownUserAgent(currentAgent);
                                        return (<>
                                            <div style={{ display: "flex", gap: 6, flexWrap: "wrap", alignItems: "center" }}>
                                                {KNOWN_USER_AGENTS.map(ua => (
                                                    <button key={ua} onClick={() => dlgUpdateField("agent_type", ua)} style={{
                                                        fontSize: "0.76rem", padding: "5px 16px", cursor: "pointer",
                                                        background: currentAgent === ua ? colors.primaryLight : colors.surface,
                                                        color: currentAgent === ua ? colors.primaryDark : colors.text,
                                                        border: `1px solid ${currentAgent === ua ? colors.primary : colors.border}`,
                                                        borderRadius: 4, transition: "all 0.15s", flexShrink: 0,
                                                    }}>
                                                        {ua}
                                                    </button>
                                                ))}
                                                <button
                                                    onClick={() => dlgUpdateField("agent_type", isCustom ? currentAgent : customAgentSeedForProvider(dlgProvider))}
                                                    style={{
                                                        fontSize: "0.76rem", padding: "5px 16px", cursor: "pointer",
                                                        background: isCustom ? colors.primaryLight : colors.surface,
                                                        color: isCustom ? colors.primaryDark : colors.text,
                                                        border: `1px solid ${isCustom ? colors.primary : colors.border}`,
                                                        borderRadius: 4, transition: "all 0.15s", flexShrink: 0,
                                                    }}>
                                                    {t("Custom", "\u81ea\u5b9a\u4e49", "\u81ea\u8a02")}
                                                </button>
                                                {isCustom && (
                                                    <input style={{ ...inputStyle, flex: 1, minWidth: 120, margin: 0 }}
                                                        value={editableCustomAgentValue(dlgProvider)}
                                                        onChange={e => dlgUpdateField("agent_type", e.target.value)}
                                                        onBlur={e => dlgUpdateField("agent_type", commitCustomAgentValue(dlgProvider, e.target.value))}
                                                        placeholder={t("Custom User-Agent", "\u81ea\u5b9a\u4e49 User-Agent", "\u81ea\u8a02 User-Agent")}
                                                        autoCapitalize="off" autoCorrect="off" spellCheck={false} autoComplete="off" />
                                                )}
                                            </div>
                                            <p style={{ fontSize: "0.68rem", color: colors.textMuted, margin: "4px 0 0 0", lineHeight: 1.4 }}>
                                                {currentAgent === "claude-code/2.0.0"
                                                    ? t("For providers requiring Claude Coding Plan identity (e.g. Kimi)", "\u9002\u7528\u4e8e\u9700\u8981 Claude Coding Plan \u8eab\u4efd\u7684\u670d\u52a1\u5546\uff08\u5982 Kimi\uff09")
                                                    : isCustom
                                                        ? t("Uses the custom client identity you enter.", "\u4f7f\u7528\u4f60\u8f93\u5165\u7684\u81ea\u5b9a\u4e49\u5ba2\u6237\u7aef\u8eab\u4efd\u3002", "\u4f7f\u7528\u4f60\u8f38\u5165\u7684\u81ea\u8a02\u5ba2\u6236\u7aef\u8eab\u5206\u3002")
                                                        : t("Most providers use OpenClaw identity (e.g. Zhipu Lobster)", "\u5927\u591a\u6570\u670d\u52a1\u5546\u4f7f\u7528 OpenClaw \u8eab\u4efd\uff08\u5982\u667a\u8c31\u9f99\u867e\uff09")}
                                            </p>
                                        </>);
                                    })()}
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
                                        {dlgProvider.auth_type === "sso" ? (
                                            <span style={{ fontSize: "0.68rem", color: colors.textMuted, marginLeft: 6 }}>
                                                {codegenModelsFetching ? t("(loading models...)", "（加载模型中...）") : t("(select model)", "（选择模型）")}
                                            </span>
                                        ) : dlgProvider.auth_type === "oauth" ? (
                                            <span style={{ fontSize: "0.68rem", color: colors.textMuted, marginLeft: 6 }}>
                                                {providerModels.length > 0
                                                    ? t("(select model)", "（选择模型）")
                                                    : t("(click Fetch to browse)", "（可点击“获取”浏览）", "（可點擊「獲取」瀏覽）")}
                                            </span>
                                        ) : dlgProvider.is_custom ? (
                                            <span style={{ fontSize: "0.68rem", color: colors.textMuted, marginLeft: 6 }}>
                                                {providerModels.length > 0
                                                    ? t("(select model)", "（选择模型）")
                                                    : t("(click Fetch to browse)", "（可点击“获取”浏览）", "（可點擊「獲取」瀏覽）")}
                                            </span>
                                        ) : (
                                            <span style={{ fontSize: "0.68rem", color: colors.textMuted, marginLeft: 6 }}>
                                                {providerModels.length > 0
                                                    ? t("(select model)", "（选择模型）")
                                                    : t("(preset, click Fetch to browse)", "（预设，可点击“获取”浏览可用模型）", "（預設，可點擊「獲取」瀏覽可用模型）")}
                                            </span>
                                        )}
                                    </label>
                                    {dlgProvider.auth_type === "sso" ? (
                                        /* SSO (CodeGen): auto-fetch model list */
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
                                        /* All other providers: fetch models, then choose from the dropdown. */
                                        <div style={{ position: "relative" }}>
                                            <div style={{ display: "flex", gap: 4, alignItems: "center" }}>
                                                <select
                                                    style={{ ...inputStyle, flex: 1 }}
                                                    value={dlgProvider.model || ""}
                                                    onChange={e => dlgUpdateField("model", e.target.value)}
                                                    disabled={providerModelsFetching || providerModels.length === 0}
                                                >
                                                    <option value="">
                                                        {providerModelsFetching
                                                            ? t("Loading...", "加载中...")
                                                            : providerModels.length === 0
                                                                ? t("Click Fetch to load models first", "请先点击“获取”加载模型", "請先點擊「獲取」載入模型")
                                                                : t("Select a model", "请选择模型")}
                                                    </option>
                                                    {!providerModels.some(m => m.id === dlgProvider.model) && dlgProvider.model && (
                                                        <option value={dlgProvider.model}>{t("Current", "当前配置")}：{dlgProvider.model}</option>
                                                    )}
                                                    {providerModels.map(m => (
                                                        <option key={m.id} value={m.id}>{m.name !== m.id ? `${m.name} (${m.id})` : m.id}</option>
                                                    ))}
                                                </select>
                                                {/* Fetch button — visible when URL and Key are available */}
                                                {dlgProvider.url && dlgProvider.key && (
                                                    <button
                                                        onClick={handleFetchProviderModels}
                                                        disabled={providerModelsFetching}
                                                        style={{
                                                            fontSize: "0.72rem", padding: "6px 10px", cursor: providerModelsFetching ? "wait" : "pointer",
                                                            background: colors.surface, color: colors.text,
                                                            border: `1px solid ${colors.border}`, borderRadius: 4,
                                                            whiteSpace: "nowrap", flexShrink: 0,
                                                            opacity: providerModelsFetching ? 0.6 : 1,
                                                        }}
                                                        title={t("Fetch available models from provider", "从服务商获取可用模型列表")}
                                                    >
                                                        {providerModelsFetching ? t("Loading...", "加载中...") : t("Fetch", "获取", "獲取")}
                                                    </button>
                                                )}
                                            </div>
                                            {providerModelsError && (
                                                <div style={{ fontSize: "0.68rem", color: colors.danger, marginTop: 4 }}>
                                                    {providerModelsError}
                                                </div>
                                            )}
                                        </div>
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
                                                background: colors.primaryLight, color: colors.primaryDark,
                                                border: `1px solid ${colors.primary}`, borderRadius: 4,
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
                                                            onProviderChanged?.();
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
                            {dlgDirty && !dlgHubSelected && <span style={{ fontSize: "0.68rem", color: colors.warning, marginRight: "auto" }}>{t("unsaved", "未保存")}</span>}
                            <button onClick={closeDialog} style={{
                                fontSize: "0.76rem", padding: "6px 18px", cursor: "pointer",
                                background: colors.bg, color: colors.text,
                                border: `1px solid ${colors.border}`, borderRadius: 4,
                            }}>
                                {t("Cancel", "取消")}
                            </button>
                            {dlgHubSelected ? (
                                <button onClick={dlgHandleSaveHubService} disabled={dlgSaving || hubSelectionAlreadySynced} style={{
                                    fontSize: "0.76rem", padding: "6px 18px",
                                    cursor: (dlgSaving || hubSelectionAlreadySynced) ? "default" : "pointer",
                                    background: hubSelectionAlreadySynced ? colors.bg : colors.primaryLight,
                                    color: hubSelectionAlreadySynced ? colors.textMuted : colors.primaryDark,
                                    border: `1px solid ${hubSelectionAlreadySynced ? colors.border : colors.primary}`, borderRadius: 4, opacity: dlgSaving ? 0.6 : 1,
                                }}>
                                    {dlgSaving ? t("Saving...", "保存中...")
                                        : hubSelectionAlreadySynced ? t("Currently Active", "当前已启用")
                                        : t("Use This Service", "使用此服务")}
                                </button>
                            ) : (
                                <button onClick={dlgHandleSave} disabled={dlgSaving || oauthBusy || (!dlgDirty && !dlgTested)} style={{
                                    fontSize: "0.76rem", padding: "6px 18px", cursor: (dlgDirty || dlgTested) ? "pointer" : "default",
                                    background: (dlgDirty || dlgTested) ? colors.primaryLight : colors.bg, color: (dlgDirty || dlgTested) ? colors.primaryDark : colors.textMuted,
                                    border: `1px solid ${(dlgDirty || dlgTested) ? colors.primary : colors.border}`, borderRadius: 4, opacity: dlgSaving ? 0.6 : 1,
                                }}>
                                    {dlgSaving ? t("Testing & Saving...", "检测并保存中...") : dlgTested ? t("Save Changes", "保存修改") : t("Test & Save", "检测并保存")}
                                </button>
                            )}
                        </div>
                    </div>
                </div>
            )}
        </div>
    );
}

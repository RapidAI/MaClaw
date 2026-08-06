import { useCallback, useEffect, useRef, useState } from 'react';
import { CancelGitHubCopilotOAuth, CancelOpenAIOAuth, CancelXAIOAuthURL, CompleteAnthropicOAuth, FetchCodeGenModels, FetchProviderModels, GetHubLLMServiceStatus, GetMaclawAgentMaxIterations, GetMaclawLLMProviders, GetMaclawLLMThinkingMode, GetSubAgentConcurrency, ImportCodexAuth, SaveCodeGenModelChoice, SaveMaclawLLMProviders, SetMaclawAgentMaxIterations, SetMaclawLLMThinkingMode, SetSubAgentConcurrency, StartAnthropicOAuth, StartGitHubCopilotOAuth, StartOpenAIOAuth, StartXAIOAuth, TestMaclawLLM, WaitGitHubCopilotOAuth } from '../../../wailsjs/go/main/App';
import { corelib } from '../../../wailsjs/go/models';
import { BrowserOpenURL, ClipboardSetText, EventsOn, EventsOff } from "../../../wailsjs/runtime";
import { colors } from "./styles";
import { HUB_SERVICE_PROVIDER_NAME, KNOWN_OPENAI_ENDPOINTS, LLM_CONFIG_LOAD_TIMEOUT_MS, NONE_PROVIDER, hubCreditGrants, hubOfficialStatus, inputStyle, labelStyle, readonlyStyle, withTimeout, type HubLLMServiceStatus, type LLMProvider } from "./LLMConfigPanelShared";
import { UsageDisplay } from "./UsageDisplay";
import { TokenUsagePanel } from "./TokenUsagePanel";
import { PROVIDER_LOGOS } from "./providerLogos";
import { useDialog } from "../CustomDialog";
import { KNOWN_USER_AGENTS, commitCustomAgentValue, customAgentSeedForProvider, editableCustomAgentValue, effectiveAgentType, isKnownUserAgent } from "./userAgent";
import { ProviderModelCombobox } from "./ProviderModelCombobox";
import { useSafeBackdropDismiss } from "../../hooks/useSafeBackdropDismiss";
import { MobileQRCodeDialog } from "./MobileQRCodeDialog";
import { MoAConfigSection } from "./MoAConfigSection";
import { LLMConfigToast, type LLMConfigToastData } from "./LLMConfigToast";

interface Props {
    lang?: string;
    codexModels?: unknown[];
    onStatusChange?: (online: boolean, configured: boolean) => void;
    onProviderChanged?: () => void;
}

export function LLMConfigPanel({ lang, onStatusChange, onProviderChanged }: Props) {
    const { showAlert, showConfirm, showPrompt } = useDialog();
    const [providers, setProviders] = useState<LLMProvider[]>([]);
    const [currentName, setCurrentName] = useState(NONE_PROVIDER);
    // An empty list is valid data. Track readiness separately so a slow local
    // config read is never rendered as if the user selected "None".
    const [providerListReady, setProviderListReady] = useState(false);
    const [providerListLoading, setProviderListLoading] = useState(true);
    const [providerListError, setProviderListError] = useState<string | null>(null);
    const [providerLoadSlow, setProviderLoadSlow] = useState(false);
    const [maxIter, setMaxIter] = useState(0);
    const [subAgentConc, setSubAgentConc] = useState(2);
    const [thinkingMode, setThinkingMode] = useState("");
    const [thinkingModeSaving, setThinkingModeSaving] = useState(false);
    const thinkingModeSavingRef = useRef(false);
    const [thinkingModeError, setThinkingModeError] = useState<string | null>(null);
    const [hubServiceStatus, setHubServiceStatus] = useState<HubLLMServiceStatus | null>(null);

    const [dlgOpen, setDlgOpen] = useState(false);
    const [dlgProviders, setDlgProviders] = useState<LLMProvider[]>([]);
    const [dlgSelectedIdx, setDlgSelectedIdx] = useState<number | null>(null);
    const [dlgHubSelected, setDlgHubSelected] = useState(false);
    const [dlgSaving, setDlgSaving] = useState(false);
    const [dlgTestResult, setDlgTestResult] = useState<{ ok: boolean; msg: string } | null>(null);
    const [dlgToast, setDlgToast] = useState<LLMConfigToastData | null>(null);
    const [dlgDirty, setDlgDirty] = useState(false);
    const [dlgTested, setDlgTested] = useState(false);
    const [oauthBusy, setOauthBusy] = useState(false);
    const [xaiOAuthURL, setXaiOAuthURL] = useState("");
    const xaiOAuthURLRef = useRef("");
    const xaiOAuthActiveRef = useRef(false);
    const xaiOAuthAttemptRef = useRef(0);
    const [codegenModels, setCodegenModels] = useState<{id: string; name: string}[]>([]);
    const [codegenModelsFetching, setCodegenModelsFetching] = useState(false);
    const [providerModels, setProviderModels] = useState<{id: string; name: string}[]>([]);
    const [providerModelsFetching, setProviderModelsFetching] = useState(false);
    const [providerModelsError, setProviderModelsError] = useState<string | null>(null);
    const [providerModelListOpen, setProviderModelListOpen] = useState(false);
    const [qrDialogOpen, setQrDialogOpen] = useState(false);
    const loadSeqRef = useRef(0);
    const hubStatusSeqRef = useRef(0);
    const fetchModelsSeqRef = useRef(0);

    const t = useCallback((en: string, zhHans: string, zhHant: string = zhHans) =>
        lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en, [lang]);

    const setActiveXaiOAuthURL = useCallback((authorizationURL: string) => {
        xaiOAuthURLRef.current = authorizationURL;
        setXaiOAuthURL(authorizationURL);
    }, []);

    const clearActiveXaiOAuthURL = useCallback(() => {
        xaiOAuthActiveRef.current = false;
        xaiOAuthURLRef.current = "";
        setXaiOAuthURL("");
    }, []);

    const cancelActiveXaiOAuth = useCallback(() => {
        // Invalidate both a fully prepared session and a StartXAIOAuth call
        // that is still resolving through the Wails bridge.
        xaiOAuthAttemptRef.current += 1;
        if (xaiOAuthURLRef.current) void CancelXAIOAuthURL(xaiOAuthURLRef.current);
        setOauthBusy(false);
        clearActiveXaiOAuthURL();
    }, [clearActiveXaiOAuthURL]);

    const copyXaiOAuthURL = useCallback(async () => {
        if (!xaiOAuthURL) return;
        try {
            if (navigator.clipboard?.writeText) {
                await navigator.clipboard.writeText(xaiOAuthURL);
                return;
            }
        } catch {
            // Wails' clipboard bridge is the fallback for restricted WebViews.
        }
        ClipboardSetText(xaiOAuthURL);
    }, [xaiOAuthURL]);

    const openXaiOAuthURL = useCallback((authorizationURL: string) => {
        try {
            BrowserOpenURL(authorizationURL);
        } catch {
            // The OAuth callback is still listening, so retain the manual
            // recovery controls instead of abandoning the active session.
            setDlgTestResult({
                ok: false,
                msg: t(
                    "Couldn't open the browser automatically. Use the link below.",
                    "无法自动打开浏览器，请使用下方链接继续登录。",
                ),
            });
        }
    }, [t]);

    const loadHubServiceStatus = useCallback(async () => {
        const statusSeq = ++hubStatusSeqRef.current;
        try {
            const status = await GetHubLLMServiceStatus() as HubLLMServiceStatus;
            if (statusSeq !== hubStatusSeqRef.current) return;
            setHubServiceStatus(status || null);
        } catch {
            if (statusSeq !== hubStatusSeqRef.current) return;
            setHubServiceStatus(null);
        }
    }, []);

    const applyProviderList = useCallback((data: unknown) => {
        const result = data as { providers?: LLMProvider[]; current?: string } | null | undefined;
        if (!Array.isArray(result?.providers)) {
            throw new Error("provider list is unavailable");
        }
        setProviders(result.providers.map(provider => ({ ...provider })));
        setCurrentName(result.current || NONE_PROVIDER);
        setProviderListReady(true);
        setProviderListLoading(false);
        setProviderListError(null);
        setProviderLoadSlow(false);
        void loadHubServiceStatus();
    }, [loadHubServiceStatus]);

    const saveThinkingMode = useCallback(async (mode: "" | "enabled" | "disabled") => {
        if (thinkingModeSavingRef.current || mode === thinkingMode) return;
        const previous = thinkingMode;
        thinkingModeSavingRef.current = true;
        setThinkingMode(mode);
        setThinkingModeSaving(true);
        setThinkingModeError(null);
        try {
            await SetMaclawLLMThinkingMode(mode);
        } catch {
            setThinkingMode(previous);
            setThinkingModeError(t(
                "Couldn't save the reasoning setting. Check the connection and try again.",
                "推理设置未能保存。请检查连接后重试。",
            ));
        } finally {
            thinkingModeSavingRef.current = false;
            setThinkingModeSaving(false);
        }
    }, [t, thinkingMode]);

    const handleOAuthLogin = useCallback(async () => {
        const xaiAttempt = ++xaiOAuthAttemptRef.current;
        const providerName = (dlgSelectedIdx !== null ? dlgProviders[dlgSelectedIdx]?.name : undefined) || "OpenAI";
        setOauthBusy(true);
        setDlgTestResult(null);
        clearActiveXaiOAuthURL();
        try {
            let loginMessage = "";

            if (providerName === "Anthropic") {
                const info = await StartAnthropicOAuth();
                window.open(info.auth_url, "_blank");
                const code = await showPrompt(
                    t(
                        "Please paste the Authorization Code shown in the browser page:",
                        "请粘贴浏览器页面中显示的授权码 (Authorization Code):",
                    ),
                    t("Authorization Code", "授权码"),
                    {
                        placeholder: t("Paste authorization code here", "在此粘贴授权码"),
                    },
                );
                if (!code?.trim()) {
                    setDlgTestResult({ ok: false, msg: t("Cancelled", "已取消") });
                    return;
                }
                loginMessage = await CompleteAnthropicOAuth(code.trim());
            } else if (providerName === "GitHub Copilot") {
                const deviceInfo = await StartGitHubCopilotOAuth();
                setDlgTestResult({
                    ok: true,
                    msg: `请打开 ${deviceInfo.verification_uri} 并输入代码: ${deviceInfo.user_code}`,
                });
                loginMessage = await WaitGitHubCopilotOAuth();
            } else if (providerName === "xAI-Grok") {
                const authorizationURL = await StartXAIOAuth();
                if (xaiAttempt !== xaiOAuthAttemptRef.current) return;
                // Keep this correlation key in a ref before the browser opens:
                // xAI may redirect back quickly, before React commits state.
                xaiOAuthActiveRef.current = true;
                setActiveXaiOAuthURL(authorizationURL);
                openXaiOAuthURL(authorizationURL);
                return;
            } else {
                loginMessage = await StartOpenAIOAuth();
            }

            const data = await GetMaclawLLMProviders();
            if (data?.providers) {
                const fresh = data.providers.map((p: LLMProvider) => ({ ...p }));
                setDlgProviders(fresh);
                setProviders(fresh.map((p: LLMProvider) => ({ ...p })));
                setCurrentName(data.current || NONE_PROVIDER);
                loadHubServiceStatus().catch(() => {});
                const oaIdx = fresh.findIndex((p: LLMProvider) => p.name === providerName);
                if (oaIdx >= 0) setDlgSelectedIdx(oaIdx);
                setDlgDirty(false);
                onStatusChange?.(true, true);
                onProviderChanged?.();
                setDlgTestResult({
                    ok: true,
                    msg: loginMessage || t("OAuth login successful", "OAuth 登录成功"),
                });
                setTimeout(() => setDlgOpen(false), 1200);
            }
        } catch (e) {
            if (providerName === "xAI-Grok" && xaiAttempt !== xaiOAuthAttemptRef.current) return;
            setDlgTestResult({ ok: false, msg: String(e) });
        } finally {
            if (providerName !== "xAI-Grok") setOauthBusy(false);
        }
    }, [clearActiveXaiOAuthURL, t, dlgProviders, dlgSelectedIdx, onStatusChange, onProviderChanged, loadHubServiceStatus, openXaiOAuthURL, setActiveXaiOAuthURL, showPrompt]);

    const loadProviders = useCallback(async () => {
        const loadSeq = ++loadSeqRef.current;
        setProviderListLoading(true);
        setProviderListError(null);
        setProviderLoadSlow(false);
        console.info("[LLMConfigPanel] load start");
        try {
            // Provider configuration is local, authoritative state. Do not
            // impose a UI deadline on it: on slower computers it can arrive
            // after the other settings calls and must still populate the UI.
            // Normalize a bridge-side synchronous throw into the same error
            // path as an asynchronous rejection. Without this, a failed Wails
            // bridge initialization could leave the Configure action disabled
            // forever on the first render.
            const providerRequest = Promise.resolve().then(() => GetMaclawLLMProviders());
            void providerRequest.then(data => {
                if (loadSeq !== loadSeqRef.current) return;
                try {
                    applyProviderList(data);
                    console.info("[LLMConfigPanel] providers loaded");
                } catch (error) {
                    setProviderListLoading(false);
                    setProviderListError(t("Couldn't read LLM providers. Click retry.", "无法读取大模型服务商，可点击重试。"));
                    setProviderLoadSlow(false);
                    console.warn("[LLMConfigPanel] provider response invalid", error);
                }
            }).catch(error => {
                if (loadSeq !== loadSeqRef.current) return;
                setProviderListLoading(false);
                setProviderListError(t("Couldn't read LLM providers. Click retry.", "无法读取大模型服务商，可点击重试。"));
                setProviderLoadSlow(false);
                console.warn("[LLMConfigPanel] providers load failed", error);
            });

            const [iterResult, concResult, thinkingResult] = await Promise.allSettled([
                withTimeout(GetMaclawAgentMaxIterations(), LLM_CONFIG_LOAD_TIMEOUT_MS, "GetMaclawAgentMaxIterations"),
                withTimeout(GetSubAgentConcurrency(), LLM_CONFIG_LOAD_TIMEOUT_MS, "GetSubAgentConcurrency"),
                withTimeout(GetMaclawLLMThinkingMode(), LLM_CONFIG_LOAD_TIMEOUT_MS, "GetMaclawLLMThinkingMode"),
            ]);
            if (loadSeq !== loadSeqRef.current) return;

            let failed = false;

            if (iterResult.status === "fulfilled") {
                const iter = iterResult.value;
                setMaxIter(typeof iter === "number" ? iter : 0);
                console.info("[LLMConfigPanel] max iterations loaded");
            } else {
                failed = true;
                setMaxIter(0);
                console.warn("[LLMConfigPanel] max iterations load failed", iterResult.reason);
            }

            if (concResult.status === "fulfilled") {
                const conc = concResult.value;
                setSubAgentConc(typeof conc === "number" && conc >= 1 ? conc : 2);
                console.info("[LLMConfigPanel] subagent concurrency loaded");
            } else {
                setSubAgentConc(2);
                console.warn("[LLMConfigPanel] subagent concurrency load failed", concResult.reason);
            }

            if (thinkingResult.status === "fulfilled") {
                const mode = thinkingResult.value;
                setThinkingMode(mode === "enabled" || mode === "disabled" ? mode : "");
                console.info("[LLMConfigPanel] thinking mode loaded");
            } else {
                setThinkingMode("");
                console.warn("[LLMConfigPanel] thinking mode load failed", thinkingResult.reason);
            }

            if (failed) {
                console.warn("[LLMConfigPanel] one or more supporting settings failed to load");
            }
        } finally {
            if (loadSeq === loadSeqRef.current) {
                console.info("[LLMConfigPanel] supporting settings load finished");
            }
        }
    }, [applyProviderList, t]);

    useEffect(() => { loadProviders(); }, [loadProviders]);
    useEffect(() => { loadHubServiceStatus(); }, [loadHubServiceStatus]);

    // Wails calls cannot be cancelled, so invalidate in-flight responses when
    // this panel unmounts. This prevents a late local-file read from updating
    // a remounted or already-disposed settings view.
    useEffect(() => () => {
        loadSeqRef.current += 1;
        hubStatusSeqRef.current += 1;
        fetchModelsSeqRef.current += 1;
    }, []);

    // Five seconds is only a feedback threshold. The provider read itself is
    // intentionally not cancelled: older machines may still complete it.
    useEffect(() => {
        if (!providerListLoading) return;
        const timer = window.setTimeout(() => setProviderLoadSlow(true), LLM_CONFIG_LOAD_TIMEOUT_MS);
        return () => window.clearTimeout(timer);
    }, [providerListLoading]);

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

    const isNone = providerListReady && currentName === NONE_PROVIDER;
    const canConfigureProviders = providerListReady;
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
    }, [providers, currentName, hasHubEntitlement, providerListReady]);

    const closeDialog = useCallback(async () => {
        if (oauthBusy) return;
        if (dlgSaving) return;
        setDlgOpen(false);
    }, [dlgSaving, oauthBusy]);
    const { backdropProps: configDialogBackdropProps, dialogProps: configDialogProps } = useSafeBackdropDismiss(closeDialog);

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

    const dlgAuthType = dlgSelectedIdx !== null ? dlgProviders[dlgSelectedIdx]?.auth_type : undefined;

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

    useEffect(() => {
        setProviderModels([]);
        setProviderModelsError(null);
        setProviderModelListOpen(false);
    }, [dlgSelectedIdx, dlgOpen]);

    const dlgIsNone = dlgSelectedIdx === null && !dlgHubSelected;
    const dlgProvider = dlgSelectedIdx !== null ? dlgProviders[dlgSelectedIdx] ?? null : null;
    const dlgNeedsOAuthLogin = !!dlgProvider &&
        (dlgProvider.auth_type === "oauth" || dlgProvider.auth_type === "sso") &&
        !dlgProvider.key;

    useEffect(() => {
        const cleanup = EventsOn("xai-oauth-complete", async (payload: { ok?: boolean; message?: string; error?: string; authorization_url?: string } = {}) => {
            // Do not rely on state captured by this effect: a quick loopback
            // callback can arrive before React has committed oauthBusy/provider.
            if (!xaiOAuthActiveRef.current || payload.authorization_url !== xaiOAuthURLRef.current) return;
            setOauthBusy(false);
            clearActiveXaiOAuthURL();
            if (!payload.ok) {
                setDlgTestResult({ ok: false, msg: payload.error || t("xAI OAuth login failed", "xAI OAuth 登录失败") });
                return;
            }
            try {
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
                setDlgTestResult({ ok: true, msg: payload.message || t("xAI OAuth login successful", "xAI OAuth 登录成功") });
                setTimeout(() => setDlgOpen(false), 1200);
            } catch (error) {
                setDlgTestResult({ ok: false, msg: String(error) });
            }
        });
        return () => { if (typeof cleanup === "function") cleanup(); else EventsOff("xai-oauth-complete"); };
    }, [clearActiveXaiOAuthURL, onProviderChanged, onStatusChange, t]);

    const handleFetchProviderModels = useCallback(async () => {
        if (!dlgProvider || !dlgProvider.url) return;
        const isManagedAuth = dlgProvider.auth_type === "oauth" || dlgProvider.auth_type === "sso";
        if (!isManagedAuth && !dlgProvider.key) {
            setProviderModelsError(t("Please fill in API Key first", "请先填写 API Key"));
            return;
        }
        const fetchSeq = ++fetchModelsSeqRef.current;
        const fetchUrl = dlgProvider.url;
        const fetchKey = dlgProvider.key || "";
        const fetchProtocol = dlgProvider.protocol || "openai";
        const fetchAgent = effectiveAgentType(dlgProvider);
        setProviderModelsFetching(true);
        setProviderModelsError(null);
        setProviderModels([]);
        setProviderModelListOpen(false);
        try {
            const models = await FetchProviderModels(fetchUrl, fetchKey, fetchProtocol, fetchAgent);
            if (fetchSeq !== fetchModelsSeqRef.current) return;
            setProviderModels(models || []);
            if (!models || models.length === 0) {
                setProviderModelsError(t("No models returned", "服务商返回了空的模型列表"));
                setProviderModelListOpen(false);
            } else {
                setProviderModelListOpen(true);
            }
        } catch (e) {
            if (fetchSeq !== fetchModelsSeqRef.current) return;
            const msg = String(e);
            if (isManagedAuth && /API Key 为空|API Key is empty|OAuth\/SSO/i.test(msg)) {
                setProviderModelsError(t(
                    "Please complete OAuth login first (token is managed internally)",
                    "请先完成 OAuth 登录（凭证由内部管理，无需填写 API Key）",
                ));
            } else {
                setProviderModelsError(msg);
            }
            setProviderModels([]);
            setProviderModelListOpen(false);
        } finally {
            if (fetchSeq === fetchModelsSeqRef.current) {
                setProviderModelsFetching(false);
            }
        }
    }, [dlgProvider, t]);

    const dlgUpdateField = useCallback((field: keyof LLMProvider, value: string) => {
        if (dlgSelectedIdx === null) return;
        setDlgProviders(prev => {
            const copy = [...prev];
            const parsed: string | number = (field === "context_length" || field === "max_output_tokens") ? (parseInt(value, 10) || 0) : value;
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
            await SaveMaclawLLMProviders(dlgProviders as corelib.MaclawLLMProvider[], HUB_SERVICE_PROVIDER_NAME);
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
            setDlgSaving(false);
        }
    }, [dlgProviders, hubSelectionAlreadySynced, t, onStatusChange, onProviderChanged]);

    const dlgQuickFill = useCallback((epName: string) => {
        const ep = KNOWN_OPENAI_ENDPOINTS.find(x => x.name === epName);
        if (!ep || dlgSelectedIdx === null) return;
        setDlgProviders(prev => {
            const copy = [...prev];
            const previous = copy[dlgSelectedIdx];
            if (!previous) return prev;
            const switchesManagedAuth = previous.auth_type === "oauth" || ep.auth_type === "oauth";
            copy[dlgSelectedIdx] = {
                ...previous,
                name: ep.name,
                url: ep.url,
                model: ep.model,
                protocol: ep.protocol || "openai",
                auth_type: ep.auth_type || "",
                // A provider switch must never reuse a token from a different
                // authentication scheme or show it as a completed OAuth login.
                key: switchesManagedAuth ? "" : previous.key,
                refresh_token: switchesManagedAuth ? "" : previous.refresh_token,
                oauth_access_token: switchesManagedAuth ? "" : previous.oauth_access_token,
                token_expires_at: switchesManagedAuth ? 0 : previous.token_expires_at,
                wire_api: ep.wire_api || "",
                agent_type: ep.agent_type || "",
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
                await SaveMaclawLLMProviders(dlgProviders as corelib.MaclawLLMProvider[], NONE_PROVIDER);
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
            if (!sp.key) {
                setDlgSaving(false);
                setDlgTestResult({
                    ok: false,
                    msg: t("Please complete OAuth login before saving", "请先完成 OAuth 登录再保存"),
                });
                return;
            }
            try {
                const saveName = sp.name;
                await SaveMaclawLLMProviders(dlgProviders as corelib.MaclawLLMProvider[], saveName);
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
                return;
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
                await SaveMaclawLLMProviders(dlgProviders as corelib.MaclawLLMProvider[], saveName);
                setDlgDirty(false);
                setProviders(dlgProviders.map(p => ({ ...p })));
                setCurrentName(saveName);
                onStatusChange?.(true, true);
                onProviderChanged?.();
                setDlgTestResult({ ok: true, msg: t("Saved", "已保存") });
                setTimeout(() => setDlgOpen(false), 800);
                return;
            }

            // Keep provider identity and auth mode on the probe request. Some
            // managed providers need provider-specific request headers; xAI's
            // OAuth endpoint is one such example.
            const testResult = await TestMaclawLLM({
                url: sp.url,
                key: sp.key,
                model: sp.model,
                protocol: sp.protocol || "openai",
                agent_type: effectiveAgentType(sp),
                wire_api: sp.wire_api || "",
                provider_name: sp.name,
                auth_type: sp.auth_type || "",
            } as corelib.MaclawLLMConfig); // Go marks supports_vision required; the probe result (not this flag) decides the persisted value.
            const saveName = sp.name;
            const visionProbeInconclusive = testResult.vision_probe_status === "inconclusive";
            const nextProviders = dlgProviders.map((provider, index) => index === dlgSelectedIdx
                ? {
                    ...provider,
                    // Preserve a previously confirmed value when the image
                    // request itself failed (for example due to a timeout).
                    supports_vision: visionProbeInconclusive ? provider.supports_vision : testResult.supports_vision,
                }
                : { ...provider });
            await SaveMaclawLLMProviders(nextProviders as corelib.MaclawLLMProvider[], saveName);

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
                msg: `${testResult.message}\n${visionProbeInconclusive
                    ? t("Vision support: not confirmed; please retry", "图片理解：未确认，请重试")
                    : testResult.supports_vision
                    ? t("Vision support: enabled", "图片理解：支持")
                    : t("Vision support: disabled", "图片理解：不支持")}`,
            });
            onStatusChange?.(true, true);
            onProviderChanged?.();
            // Auto-close on success: test passed and config saved, no need for extra click.
            // Keep dlgSaving=true during close animation to prevent double-click.
            setTimeout(() => setDlgOpen(false), 800);
            return;
        } catch (e) {
            setDlgTestResult({ ok: false, msg: String(e) });
        }
        setDlgSaving(false);
    };

    return (
        <div className="llm-config-panel">
            <LLMConfigToast toast={dlgToast} />
            <div className="llm-config-panel__intro">
                <p>
                    {t(
                        "Select LLM provider (OpenAI / Anthropic supported)",
                        "选择大模型服务商（支持 OpenAI / Anthropic 协议）"
                    )}
                </p>
                <div style={{ display: "flex", gap: 8, flexShrink: 0 }}>
                    <button className="llm-config-qr-action" onClick={() => setQrDialogOpen(true)} disabled={!canConfigureProviders} style={{
                        fontSize: "0.76rem", padding: "6px 14px", cursor: canConfigureProviders ? "pointer" : "default",
                        background: colors.surface, color: colors.primaryDark, border: `1px solid ${colors.primary}`, borderRadius: 4,
                        opacity: canConfigureProviders ? 1 : 0.65,
                    }}>
                        {t("Mobile QR", "移动端二维码")}
                    </button>
                    <button className="llm-config-primary-action" onClick={openDialog} disabled={!canConfigureProviders} style={{
                        fontSize: "0.76rem", padding: "6px 18px", cursor: canConfigureProviders ? "pointer" : "default",
                        background: colors.primaryLight, color: colors.primaryDark, border: `1px solid ${colors.primary}`, borderRadius: 4,
                        opacity: canConfigureProviders ? 1 : 0.65,
                    }}>
                        {t("Configure", "配置")}
                    </button>
                </div>
            </div>

            {/* Current provider summary */}
            <div className="llm-config-summary" aria-busy={providerListLoading} style={{
                marginBottom: 16, padding: "10px 16px", borderRadius: 6,
                border: `1px solid ${colors.border}`, background: colors.surface,
                display: "flex", justifyContent: "space-between", alignItems: "center",
            }}>
                <span className="llm-config-summary__label" style={{ fontSize: "0.76rem", color: colors.textSecondary }}>
                    {t("Provider", "当前服务商")}
                </span>
                <span className="llm-config-summary__value" style={{ fontSize: "0.76rem", fontWeight: 600, color: isNone ? (hasHubEntitlement ? colors.primaryDark : colors.danger) : colors.text }}>
                    {!providerListReady
                        ? (providerListLoading
                            ? (providerLoadSlow
                                ? t("Still reading saved providers…", "仍在读取已保存的服务商…")
                                : t("Reading saved providers…", "正在读取已保存的服务商…"))
                            : t("Provider list unavailable", "服务商列表不可用"))
                        : isNone
                        ? (hasHubEntitlement ? t("MaClaw Official", "MaClaw 官方") : t("None", "未配置"))
                        : currentName}
                </span>
            </div>

            {providerListError && (
                <div role="alert" style={{
                    marginBottom: 16, padding: "8px 12px", borderRadius: 4, fontSize: "0.74rem", lineHeight: 1.5,
                    background: colors.dangerBg, border: `1px solid color-mix(in srgb, ${colors.danger} 30%, transparent)`, color: colors.danger,
                    display: "flex", justifyContent: "space-between", alignItems: "center", gap: 12,
                }}>
                    <span>{providerListError}</span>
                    <button type="button" onClick={() => { void loadProviders(); }} style={{
                        fontSize: "0.72rem", padding: "4px 10px", cursor: "pointer", flexShrink: 0,
                        background: colors.surface, color: colors.danger, border: `1px solid ${colors.danger}`, borderRadius: 4,
                    }}>
                        {t("Retry", "重试")}
                    </button>
                </div>
            )}

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
                            {t("0=unlimited, default 300", "0=不限制，默认 300")}
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

            {/* SubAgent concurrency — controls parallel coding tasks */}
            <div className="llm-config-card" style={{
                marginBottom: 16, padding: "12px 16px", borderRadius: 6,
                border: `1px solid ${colors.border}`, background: colors.surface,
            }}>
                <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: 8 }}>
                    <label style={{ ...labelStyle, marginBottom: 0 }}>
                        {t("CodingSubAgent Concurrency", "CodingSubAgent 并发数")}
                        <span style={{ fontSize: "0.68rem", color: colors.textMuted, fontWeight: 400, marginLeft: 6 }}>
                            {t("Parallel tasks without dependencies, default 2", "无依赖任务并行数，默认 2")}
                        </span>
                    </label>
                </div>
                <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
                    <input type="range" min={1} max={10} step={1} value={subAgentConc}
                        onChange={e => { const v = Number(e.target.value); setSubAgentConc(v); SetSubAgentConcurrency(v).catch(() => {}); }}
                        style={{ flex: 1, accentColor: "var(--theme-primary)" }} />
                    <input type="number" min={1} max={10} value={subAgentConc}
                        onChange={e => { const v = Math.max(1, Math.min(10, Number(e.target.value) || 1)); setSubAgentConc(v); SetSubAgentConcurrency(v).catch(() => {}); }}
                        style={{ ...inputStyle, width: 60, textAlign: "center" as const }} />
                    <span style={{ fontSize: "0.72rem", color: colors.textSecondary, whiteSpace: "nowrap" }}>
                        {subAgentConc === 1 ? t("Sequential", "顺序执行") : `${subAgentConc} ${t("parallel", "路并行")}`}
                    </span>
                </div>
            </div>

            {/* Thinking (reasoning) mode — global, provider-native request controls */}
            <div className="llm-config-card" style={{
                marginBottom: 16, padding: "12px 16px", borderRadius: 6,
                border: `1px solid ${colors.border}`, background: colors.surface,
            }}>
                <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: 8 }}>
                    <label style={{ ...labelStyle, marginBottom: 0 }}>
                        {t("Thinking (Reasoning)", "推理（思考过程）")}
                        <span style={{ fontSize: "0.68rem", color: colors.textMuted, fontWeight: 400, marginLeft: 6 }}>
                            {t("Global; translated to each provider's supported control", "全局设置；会按服务商支持的参数转换")}
                        </span>
                    </label>
                </div>
                <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }} role="group" aria-label={t("Thinking mode", "推理模式")} aria-busy={thinkingModeSaving}>
                    {(["", "enabled", "disabled"] as const).map(mode => {
                        const active = thinkingMode === mode;
                        return (
                            <button key={mode || "auto"}
                                data-testid={`thinking-mode-${mode || "auto"}`}
                                type="button"
                                aria-pressed={active}
                                disabled={thinkingModeSaving}
                                onClick={() => { void saveThinkingMode(mode); }}
                                style={{
                                    fontSize: "0.76rem", padding: "5px 16px", cursor: thinkingModeSaving ? "wait" : "pointer",
                                    background: active ? colors.primaryLight : colors.surface,
                                    color: active ? colors.primaryDark : colors.textSecondary,
                                    border: `1px solid ${active ? colors.primary : colors.border}`,
                                    borderRadius: 4, transition: "all 0.15s", opacity: thinkingModeSaving ? 0.7 : 1,
                                }}>
                                {mode === "" ? t("Auto", "自动") : mode === "enabled" ? t("On", "开启") : t("Off", "关闭")}
                            </button>
                        );
                    })}
                </div>
                <p style={{ fontSize: "0.68rem", color: colors.textMuted, margin: "6px 0 0 0", lineHeight: 1.4 }}>
                    {thinkingMode === "enabled"
                        ? t("Enabled on new requests using the provider's native control. The chat panel shows reasoning only when the provider returns it.", "已在后续请求中按服务商原生参数开启；仅当服务商返回推理内容时，助手面板才会显示“思考过程”。")
                        : thinkingMode === "disabled"
                            ? t("Disabled on new requests using the provider's native control. Models without a hard off switch use their lowest reasoning level.", "已在后续请求中按服务商原生参数关闭；没有硬关闭能力的模型会使用最低推理强度。")
                            : t("Auto: uses the model default (DeepSeek thinking models are explicitly enabled when required).", "自动：沿用模型默认行为（需要显式开启的 DeepSeek 思考模型会自动开启）。")}
                </p>
                {thinkingModeError && <p role="alert" style={{ fontSize: "0.7rem", color: colors.danger, margin: "6px 0 0", lineHeight: 1.4 }}>{thinkingModeError}</p>}
            </div>

            {/* Multi-model council (MoA) presets — aggregator + reference models */}
            <MoAConfigSection lang={lang} providers={providers} />

            {isNone && !hasHubEntitlement && (
                <div className="llm-config-alert llm-config-alert--danger" style={{
                    padding: "8px 12px", borderRadius: 4, fontSize: "0.74rem", lineHeight: 1.5,
                    background: colors.dangerBg, border: `1px solid color-mix(in srgb, ${colors.danger} 30%, transparent)`, color: colors.danger,
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
                }}
                    {...configDialogBackdropProps}
                >
                    <div className="llm-config-dialog" style={{
                        background: colors.surface, borderRadius: 12, padding: "24px 28px",
                        maxWidth: 520, width: "92%", maxHeight: "85vh", overflowY: "auto",
                        boxShadow: "0 16px 48px rgba(0,0,0,0.22)",
                    }}
                        {...configDialogProps}
                    >

                        {/* Header */}
                        <div className="llm-config-dialog__header" style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 18 }}>
                            <span className="llm-config-dialog__title" style={{ fontSize: "0.92rem", fontWeight: 700, color: colors.text }}>
                                {t("MaClaw LLM Configuration", "MaClaw 大模型配置")}
                            </span>
                            <button className="llm-config-dialog__close" onClick={closeDialog} aria-label={t("Close", "关闭")} style={{
                                border: "none", background: "transparent", cursor: "pointer",
                                fontSize: "0.68rem", fontWeight: 700, color: colors.textSecondary, padding: "2px 6px",
                            }}>CLOSE</button>
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
                                            <span style={{ fontSize: "0.6rem", lineHeight: 1, padding: "2px 5px", borderRadius: 6, background: colors.infoBg, color: colors.primaryDark, border: `1px solid ${colors.primary}` }}>
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
                                        <button className="llm-config-provider-chip" key={i} aria-label={tag ? `${p.name} ${tag}` : p.name} onClick={() => isHubProvider ? dlgSelectHubService() : dlgSelectProvider(i)} style={{
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
                                                    background: isHubProvider ? colors.infoBg : active ? colors.infoBg : colors.primaryLight,
                                                    color: colors.primaryDark, border: `1px solid ${colors.primary}`, fontWeight: 600, pointerEvents: "none",
                                                }}>{tag}</span>
                                            )}
                                        </button>
                                    );
                                })}
                                {/* Never offer a destructive-looking empty state while the saved list is unresolved. */}
                                {providerListReady && (
                                    <button className="llm-config-provider-chip" onClick={() => dlgSelectProvider(null)} style={{
                                        fontSize: "0.76rem", padding: "5px 14px", cursor: "pointer",
                                        background: dlgIsNone ? colors.primaryLight : colors.surface,
                                        color: dlgIsNone ? colors.primaryDark : colors.text,
                                        border: `1px solid ${dlgIsNone ? colors.primary : colors.border}`,
                                        borderRadius: 4, transition: "all 0.15s",
                                    }}>
                                        {t("None", "暂不配置")}
                                    </button>
                                )}
                            </div>
                        </div>

                        {/* None warning */}
                        {dlgIsNone && (
                            <div className="llm-config-alert llm-config-alert--danger" style={{
                                padding: "8px 12px", borderRadius: 4, fontSize: "0.74rem", lineHeight: 1.5,
                                background: colors.dangerBg, border: `1px solid color-mix(in srgb, ${colors.danger} 30%, transparent)`, color: colors.danger,
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
                                borderRadius: 8,
                                border: "1px solid color-mix(in srgb, var(--theme-success) 28%, var(--theme-border))",
                                background: "var(--theme-surface-muted)",
                                boxShadow: "none",
                                position: "relative",
                                overflow: "hidden",
                            }}>
                                <div style={{ position: "absolute", inset: 0, pointerEvents: "none", background: "transparent" }} />
                                <div style={{ position: "relative", display: "grid", gap: 14 }}>
                                    <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", gap: 12 }}>
                                        <div>
                                            <div style={{ fontSize: "0.92rem", fontWeight: 800, color: "var(--theme-text-primary)", marginBottom: 6 }}>
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
                                            border: "1px solid color-mix(in srgb, var(--theme-success) 34%, var(--theme-border))",
                                            background: "var(--theme-success-bg)",
                                            color: "var(--theme-success)",
                                            fontSize: "0.68rem",
                                            fontWeight: 800,
                                        }}>
                                            {hubOfficial.label}
                                        </span>
                                    </div>

                                    <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(160px, 1fr))", gap: 10 }}>
                                        <div style={{ padding: "10px 12px", borderRadius: 8, background: "var(--theme-surface)", border: "1px solid var(--theme-border-subtle)" }}>
                                            <div style={{ fontSize: "0.68rem", color: colors.textMuted, marginBottom: 5, fontWeight: 700 }}>{t("Service Status", "\u670d\u52a1\u72b6\u6001")}</div>
                                            <div style={{ fontSize: "0.82rem", color: colors.text, fontWeight: 800 }}>{hubOfficial.label}</div>
                                        </div>
                                        <div style={{ padding: "10px 12px", borderRadius: 8, background: "var(--theme-surface)", border: "1px solid var(--theme-border-subtle)" }}>
                                            <div style={{ fontSize: "0.68rem", color: colors.textMuted, marginBottom: 5, fontWeight: 700 }}>{t("Model", "\u6a21\u578b")}</div>
                                            <div style={{ fontSize: "0.82rem", color: colors.text, fontWeight: 800, wordBreak: "break-word" }}>{hubModelLabel}</div>
                                        </div>
                                        <div style={{ padding: "10px 12px", borderRadius: 8, background: "var(--theme-surface)", border: "1px solid var(--theme-border-subtle)" }}>
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
                                                {currentAgent === "claude code 2.0"
                                                    ? t("For providers requiring Claude Coding Plan identity (e.g. Kimi, Zhipu Coding)", "\u9002\u7528\u4e8e\u9700\u8981 Claude Coding Plan \u8eab\u4efd\u7684\u670d\u52a1\u5546\uff08\u5982 Kimi\u3001\u667a\u8c31\u7f16\u7a0b\uff09")
                                                    : currentAgent === "Kilo Code"
                                                        ? t("For providers requiring Kilo Code client identity.", "\u9002\u7528\u4e8e\u9700\u8981 Kilo Code \u5ba2\u6237\u7aef\u8eab\u4efd\u7684\u670d\u52a1\u5546\u3002", "\u9069\u7528\u65bc\u9700\u8981 Kilo Code \u5ba2\u6236\u7aef\u8eab\u5206\u7684\u670d\u52d9\u5546\u3002")
                                                    : isCustom
                                                        ? t("Uses the custom client identity you enter.", "\u4f7f\u7528\u4f60\u8f93\u5165\u7684\u81ea\u5b9a\u4e49\u5ba2\u6237\u7aef\u8eab\u4efd\u3002", "\u4f7f\u7528\u4f60\u8f38\u5165\u7684\u81ea\u8a02\u5ba2\u6236\u7aef\u8eab\u5206\u3002")
                                                        : t("Most providers use OpenClaw identity (e.g. Zhipu Lobster)", "\u5927\u591a\u6570\u670d\u52a1\u5546\u4f7f\u7528 OpenClaw \u8eab\u4efd\uff08\u5982\u667a\u8c31\u9f99\u867e\uff09")}
                                            </p>
                                        </>);
                                    })()}
                                </div>

                                {dlgProvider.is_custom && (
                                    <div style={{ marginBottom: 12 }}>
                                        <label style={labelStyle}>{t("Provider Name", "服务商名称")}</label>
                                        <input style={inputStyle} value={dlgProvider.name}
                                            onChange={e => dlgUpdateField("name", e.target.value)}
                                            placeholder={t("Custom name", "自定义名称")}
                                            autoCapitalize="off" autoCorrect="off" spellCheck={false} autoComplete="off" />
                                    </div>
                                )}

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
                                                    : dlgProvider.name === "\u706b\u5c71\u5f15\u64ce Agent Plan"
                                                        ? t("(preset model, enter manually)", "（预设模型，请手动填写）", "（預設模型，請手動填寫）")
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
                                        <ProviderModelCombobox
                                            selectedIdx={dlgSelectedIdx}
                                            value={dlgProvider.model || ""}
                                            models={providerModels}
                                            fetching={providerModelsFetching}
                                            error={providerModelsError}
                                            open={providerModelListOpen}
                                            canFetch={!!dlgProvider.url && dlgProvider.name !== "\u706b\u5c71\u5f15\u64ce Agent Plan"}
                                            onOpenChange={setProviderModelListOpen}
                                            onChange={value => dlgUpdateField("model", value)}
                                            onFetch={handleFetchProviderModels}
                                            t={t}
                                        />
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
                                                background: colors.successBg, border: `1px solid color-mix(in srgb, ${colors.success} 30%, transparent)`,
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
                                                    ? `RUN ${t("Waiting for browser authorization...", "等待浏览器授权...")}`
                                                    : dlgProvider.name === "GitHub Copilot"
                                                        ? t("Sign in with GitHub", "使用 GitHub 账号登录")
                                                        : dlgProvider.name === "Anthropic"
                                                            ? t("Sign in with Claude.ai", "使用 Claude.ai 账号登录")
                                                            : dlgProvider.name === "xAI-Grok"
                                                                ? t("Sign in with xAI", "使用 xAI 账号登录")
                                                            : t("Sign in with OpenAI", "使用 OpenAI 账号登录")}
                                            </button>
                                            {oauthBusy && (
                                                <button aria-label={t("Cancel OAuth login", "取消 OAuth 登录")} onClick={() => {
                                                    const name = dlgSelectedIdx !== null ? dlgProviders[dlgSelectedIdx]?.name : undefined;
                                                    if (name === "GitHub Copilot") {
                                                        CancelGitHubCopilotOAuth();
                                                        setOauthBusy(false);
                                                    } else if (name === "xAI-Grok") {
                                                        cancelActiveXaiOAuth();
                                                    } else {
                                                        CancelOpenAIOAuth();
                                                        setOauthBusy(false);
                                                    }
                                                }} style={{
                                                    width: "100%", padding: "8px 0", fontSize: "0.76rem",
                                                    cursor: "pointer", marginTop: 6,
                                                    background: "transparent", color: colors.textMuted,
                                                    border: `1px solid ${colors.border}`, borderRadius: 4,
                                                }}>
                                                    {t("Cancel", "取消")}
                                                </button>
                                            )}
                                            {oauthBusy && dlgProvider.name === "xAI-Grok" && xaiOAuthURL && (
                                                <div style={{ display: "flex", gap: 8, marginTop: 6 }}>
                                                    <button onClick={() => openXaiOAuthURL(xaiOAuthURL)} style={{
                                                        flex: 1, padding: "8px 0", fontSize: "0.76rem", cursor: "pointer",
                                                        background: "transparent", color: colors.primary,
                                                        border: `1px solid ${colors.primary}`, borderRadius: 4,
                                                    }}>
                                                        {t("Open browser again", "再次打开浏览器")}
                                                    </button>
                                                    <button onClick={() => void copyXaiOAuthURL()} style={{
                                                        flex: 1, padding: "8px 0", fontSize: "0.76rem", cursor: "pointer",
                                                        background: "transparent", color: colors.textMuted,
                                                        border: `1px solid ${colors.border}`, borderRadius: 4,
                                                    }}>
                                                        {t("Copy sign-in link", "复制登录链接")}
                                                    </button>
                                                </div>
                                            )}
                                            {dlgProvider.name === "OpenAI" && dlgTestResult && !dlgTestResult.ok && !oauthBusy && (
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
                                            placeholder={((dlgProvider.name === "智谱编程") || (dlgProvider.protocol || "openai") === "anthropic") ? "xxxxxxxx.yyyyyyyy" : "sk-..."}
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

                                {/* Max Output Tokens */}
                                <div style={{ marginTop: 12 }}>
                                    <label style={labelStyle}>{t("Max Output Tokens", "最大输出长度 (tokens)")}</label>
                                    <input style={inputStyle} type="number" min={1024} step={1024}
                                        autoCapitalize="off" autoCorrect="off" spellCheck={false} autoComplete="off"
                                        value={dlgProvider.max_output_tokens || ""}
                                        onChange={e => dlgUpdateField("max_output_tokens", e.target.value)}
                                        placeholder="65536" />
                                    <p style={{ fontSize: "0.68rem", color: colors.textMuted, margin: "4px 0 0 0", lineHeight: 1.4 }}>
                                        {t(
                                            "Max tokens per LLM response. Defaults to 65536. For models with lower limits, the system auto-detects and adapts on first use.",
                                            "单次回复最大 token 数。默认 65536。对于限制较低的模型，系统在首次使用时自动检测并适配。"
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
                            {dlgDirty && !dlgHubSelected && <span style={{ fontSize: "0.68rem", color: colors.primaryDark, marginRight: "auto" }}>{t("unsaved", "未保存")}</span>}
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
                                <button onClick={dlgHandleSave} disabled={dlgSaving || oauthBusy || (!dlgDirty && !dlgTested && !dlgNeedsOAuthLogin)} style={{
                                    fontSize: "0.76rem", padding: "6px 18px", cursor: (dlgDirty || dlgTested || dlgNeedsOAuthLogin) ? "pointer" : "default",
                                    background: (dlgDirty || dlgTested || dlgNeedsOAuthLogin) ? colors.primaryLight : colors.bg, color: (dlgDirty || dlgTested || dlgNeedsOAuthLogin) ? colors.primaryDark : colors.textMuted,
                                    border: `1px solid ${(dlgDirty || dlgTested || dlgNeedsOAuthLogin) ? colors.primary : colors.border}`, borderRadius: 4, opacity: dlgSaving ? 0.6 : 1,
                                }}>
                                    {dlgSaving ? t("Testing & Saving...", "检测并保存中...") : dlgTested ? t("Save Changes", "保存修改") : t("Test & Save", "检测并保存")}
                                </button>
                            )}
                        </div>
                    </div>
                </div>
            )}

            {/* Mobile QR Code Dialog */}
            <MobileQRCodeDialog
                open={qrDialogOpen}
                onClose={() => setQrDialogOpen(false)}
                providers={providers}
                currentName={currentName}
                lang={lang}
            />
        </div>
    );
}

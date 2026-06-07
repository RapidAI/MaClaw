import { useState, useCallback, useEffect, useRef, useMemo } from "react";
import { createPortal } from "react-dom";
import { colors, radius } from "./styles";
import { QRCodeSVG } from "qrcode.react";
import {
    GetMaclawLLMProviders,
    SaveMaclawLLMProviders,
    TestMaclawLLM,
    ActivateRemote,
    ProbeRemoteHub,
    StartOpenAIOAuth,
    CancelOpenAIOAuth,
    StartCodeGenSSO,
    StartCodeGenSSOEmbedded,
    WaitCodeGenSSOResult,
    CancelCodeGenSSOPolling,
    FetchCodeGenModels,
    SaveCodeGenModelChoice,
    GetWeixinStatus,
    StartWeixinQRLogin,
    PollWeixinQRStatus,
    GetRemoteConnectionStatus,
    GetHubLLMServiceStatus,
    RedeemHubLLMService,
} from "../../../wailsjs/go/main/App";
import { PROVIDER_LOGOS } from "./providerLogos";
import { localizeHubServiceReason, localizeHubServiceRedeemError } from "../../utils/hubServiceI18n";
import { HubRegisterButtonContent } from "./HubConnectionStatus";
import { OnboardingOfflineModeOption } from "./OnboardingOfflineModeOption";
import { OfflineModeNoticeDialog } from "./OfflineModeNoticeDialog";
import {
    getOnboardingFlow,
    getOnboardingStepDone,
    getOnboardingStepLabels,
    isCurrentOnboardingStep,
    isOnboardingComplete,
} from "./onboardingFlow";
import {
    inputStyle,
    labelStyle,
    localizeText,
    readonlyInputStyle,
    wizardPrimaryButtonStyle,
    wizardSuccessButtonStyle,
    wizardDisabledButtonStyle,
    wizardGhostButtonStyle,
    wizardGhostButtonBlockStyle,
    wizardSelectableChipStyle,
    wizardBannerStyle,
    type HubLLMActiveGrant,
    type HubLLMServiceStatus,
    type LLMProvider,
    type Props,
} from "./OnboardingWizardShared";
import { KNOWN_USER_AGENTS, customAgentSeedForProvider, editableCustomAgentValue, effectiveAgentType, isKnownUserAgent, nextCustomAgentValue } from "./userAgent";

export function OnboardingWizard({ lang, hubUrl, email, brandId, brandDisplayName, onClose, onLLMConfigured, onRegistered, onSaveField }: Props) {
    const t = useCallback((zh: string, en: string, zhHant: string = zh) => localizeText(lang, en, zh, zhHant), [lang]);
    const hubT = useCallback((en: string, zhHans: string, zhHant?: string) => localizeText(lang, en, zhHans, zhHant ?? zhHans), [lang]);

    // Track the app-level theme mode so the portal (rendered outside #App) can
    // inherit dark-mode CSS variables. Uses MutationObserver for live updates.
    const [portalTheme, setPortalTheme] = useState<'light' | 'dark'>(() => {
        return (document.getElementById("App") as HTMLElement)?.dataset?.aiTheme as 'light' | 'dark' || "light";
    });
    useEffect(() => {
        const appEl = document.getElementById("App");
        if (!appEl) return;
        const sync = () => setPortalTheme((appEl.dataset.aiTheme as 'light' | 'dark') || "light");
        sync();
        const observer = new MutationObserver(sync);
        observer.observe(appEl, { attributes: true, attributeFilter: ["data-ai-theme"] });
        return () => observer.disconnect();
    }, []);

    // 品牌显示名称（动态替换硬编码的 "MaClaw"）
    const displayName = brandDisplayName || 'MaClaw';

    const [step, setStep] = useState(1);
    const [regEmail, setRegEmail] = useState(email || "");
    const [invCode, setInvCode] = useState("");
    const [invRequired, setInvRequired] = useState(false);
    const [invError, setInvError] = useState("");
    const [showConfirm, setShowConfirm] = useState(false);
    const [vipFlag, setVipFlag] = useState(false);
    const [redeemCode, setRedeemCode] = useState("");
    const [freeTrial, setFreeTrial] = useState(true);
    const [offlineMode, setOfflineMode] = useState(false);
    const [showOfflineModeNotice, setShowOfflineModeNotice] = useState(false);
    const onboardingFlow = useMemo(() => getOnboardingFlow({ brandId, freeTrial, offlineMode }), [brandId, freeTrial, offlineMode]);
    const isTigerclaw = onboardingFlow.isTigerclaw;
    const totalSteps = onboardingFlow.totalSteps;
    const wxStep = onboardingFlow.wxStep;
    const [freeTrialVerified, setFreeTrialVerified] = useState(false);
    const [regResult, setRegResult] = useState<{ ok: boolean; msg: string; tone?: "warning" } | null>(null);

    const [ssoBusy, setSsoBusy] = useState(false);
    const [ssoResult, setSsoResult] = useState<{ ok: boolean; msg: string } | null>(null);
    // regBusy/regDone 在两种流程中均使用：普通品牌=手动注册，tigerclaw=SSO后自动注册
    const [regBusy, setRegBusy] = useState(false);
    const [regDone, setRegDone] = useState(false);
    const [hubConnecting, setHubConnecting] = useState(false);
    const [qrCodeURL, setQrCodeURL] = useState("");
    const [embeddedSSOLoading, setEmbeddedSSOLoading] = useState(false);
    const [embeddedSSOError, setEmbeddedSSOError] = useState("");
    const [providers, setProviders] = useState<LLMProvider[]>([]);
    const [selectedIdx, setSelectedIdx] = useState<number | null>(null);
    const [llmSaving, setLlmSaving] = useState(false);
    const [llmResult, setLlmResult] = useState<{ ok: boolean; msg: string } | null>(null);
    const [llmDone, setLlmDone] = useState(false);
    const [oauthBusy, setOauthBusy] = useState(false);
    const [codegenModels, setCodegenModels] = useState<{ id: string; name: string }[]>([]);
    const [codegenModelsFetching, setCodegenModelsFetching] = useState(false);
    const [maclawModel, setMaclawModel] = useState("");        // MaClaw Agent 使用的模型
    const [claudeCodeModel, setClaudeCodeModel] = useState(""); // TigerClaw Code 使用的模型
    const [modelSaving, setModelSaving] = useState(false);
    const [modelSaved, setModelSaved] = useState(false);
    const [wxDone, setWxDone] = useState(false);
    const [wxSkipped, setWxSkipped] = useState(false);
    const [wxQrUrl, setWxQrUrl] = useState("");
    const [wxStatus, setWxStatus] = useState("");
    const [wxMsg, setWxMsg] = useState("");
    const [wxLoading, setWxLoading] = useState(false);
    const wxPollingRef = useRef(false);

    const wxCompleted = wxDone || wxSkipped;
    const effectiveRegDone = offlineMode || regDone;
    const stepDone = useMemo(() => getOnboardingStepDone(onboardingFlow, {
        regDone: effectiveRegDone,
        llmDone,
        wxCompleted,
    }), [onboardingFlow, effectiveRegDone, llmDone, wxCompleted]);

    const getPrevStep = useCallback((currentStep: number) => {
        return Math.max(1, currentStep - 1);
    }, []);

    const getNextStep = useCallback((currentStep: number) => {
        return Math.min(totalSteps, currentStep + 1);
    }, [totalSteps]);

    const canNext = step < totalSteps && stepDone[step];
    const canPrev = step > 1;
    const isLastStep = step === totalSteps;
    const lastStepCompleted = !!stepDone[step];
    const applyHubServiceStatus = useCallback((status?: HubLLMServiceStatus | null) => {
        const shouldSkipLLM = !!status?.active && !!status?.skip_llm_config;
        if (shouldSkipLLM) {
            setLlmDone(true);
            setLlmResult({
                ok: true,
                msg: t("MaClaw 官方模型服务已授权，LLM 配置步骤已自动跳过", "MaClaw model service is authorized. The LLM binding step has been skipped automatically."),
            });
            onLLMConfigured();
        }
        return shouldSkipLLM;
    }, [onLLMConfigured, t]);

    const formatHubRetryDuration = useCallback((seconds: number): string => {
        const safeSeconds = Math.max(0, Math.ceil(Number(seconds || 0)));
        if (safeSeconds < 60) return t(`${safeSeconds} 秒`, `${safeSeconds}s`);
        const minutes = Math.ceil(safeSeconds / 60);
        if (minutes < 60) return t(`${minutes} 分钟`, `${minutes}m`);
        const hours = Math.ceil(minutes / 60);
        if (hours < 24) return t(`${hours} 小时`, `${hours}h`);
        const days = Math.ceil(hours / 24);
        return t(`${days} 天`, `${days}d`);
    }, [t]);

    const hubGrantRetrySeconds = useCallback((grant?: HubLLMActiveGrant): number => {
        if (!grant) return 0;
        let seconds = Number(grant.retry_after_seconds || 0);
        if ((!Number.isFinite(seconds) || seconds <= 0) && grant.retry_after_at) {
            const retryAt = new Date(grant.retry_after_at).getTime();
            if (Number.isFinite(retryAt)) seconds = Math.ceil((retryAt - Date.now()) / 1000);
        }
        return Number.isFinite(seconds) && seconds > 0 ? seconds : 0;
    }, []);

    const hubInactiveRedeemMessage = useCallback((status?: HubLLMServiceStatus | null): string => {
        const grants = (status?.credit_grants?.length ? status.credit_grants : status?.active_grants) || [];
        const findGrant = (target: string) => grants.find(grant => String(grant.status || "").toLowerCase() === target);
        const limited = findGrant("period_limited");
        if (limited) {
            const retrySeconds = hubGrantRetrySeconds(limited);
            const retryText = retrySeconds > 0 ? formatHubRetryDuration(retrySeconds) : "";
            return retryText
                ? t(`服务兑换码已生效，但 MaClaw 官方当前周期限额，约 ${retryText} 后恢复。LLM 配置步骤暂不跳过。`, `Service code redeemed, but MaClaw Official is period limited and recovers in about ${retryText}. LLM setup is not skipped yet.`)
                : t("服务兑换码已生效，但 MaClaw 官方当前周期限额。LLM 配置步骤暂不跳过。", "Service code redeemed, but MaClaw Official is period limited. LLM setup is not skipped yet.");
        }
        const queued = findGrant("queued");
        if (queued) {
            const retrySeconds = hubGrantRetrySeconds(queued);
            const retryText = retrySeconds > 0 ? formatHubRetryDuration(retrySeconds) : "";
            return retryText
                ? t(`服务兑换码已生效，MaClaw 官方授权约 ${retryText} 后生效。LLM 配置步骤暂不跳过。`, `Service code redeemed. MaClaw Official authorization starts in about ${retryText}. LLM setup is not skipped yet.`)
                : t("服务兑换码已生效，但 MaClaw 官方授权尚未生效。LLM 配置步骤暂不跳过。", "Service code redeemed, but MaClaw Official authorization is not active yet. LLM setup is not skipped yet.");
        }
        if (findGrant("exhausted")) {
            return t("服务兑换码已生效，但 MaClaw 官方额度已用尽。请兑换更多额度或手动配置其它服务商。", "Service code redeemed, but MaClaw Official credits are exhausted. Redeem more credits or configure another provider manually.");
        }
        if (findGrant("expired")) {
            return t("服务兑换码已生效，但 MaClaw 官方授权已过期。请兑换新的授权或手动配置其它服务商。", "Service code redeemed, but MaClaw Official authorization has expired. Redeem a new grant or configure another provider manually.");
        }
        const reason = (status?.inactive_reasons || []).map(reason => localizeHubServiceReason(reason, lang)).filter(Boolean).join("; ");
        return reason
            ? t(`服务兑换码已生效，但 MaClaw 官方暂不可用：${reason}`, `Service code redeemed, but MaClaw Official is unavailable: ${reason}`)
            : t("服务兑换码已生效，但 MaClaw 官方暂不可用。请在服务状态中查看原因。", "Service code redeemed, but MaClaw Official is unavailable. Check Service Status for details.");
    }, [formatHubRetryDuration, hubGrantRetrySeconds, lang, t]);

    useEffect(() => {
        GetMaclawLLMProviders().then(data => {
            if (data?.providers) setProviders(data.providers);
        }).catch(() => {});
    }, []);

    const initialEmailRef = useRef(email);
    useEffect(() => {
        if (!hubUrl || !initialEmailRef.current) return;
        ProbeRemoteHub(hubUrl, initialEmailRef.current).then(r => {
            if (r?.invitation_code_required) setInvRequired(true);
        }).catch(() => {});
    }, [hubUrl]);

    // Check WeChat status on mount — only treat explicit "connected"/"confirmed" as bound
    useEffect(() => {
        GetWeixinStatus().then(s => {
            if (s === "connected" || s === "confirmed") setWxDone(true);
        }).catch(() => {});
    }, []);

    useEffect(() => {
        if (isTigerclaw || offlineMode) return;
        let cancelled = false;
        GetHubLLMServiceStatus().then(status => {
            if (cancelled) return;
            const applied = applyHubServiceStatus(status);
            if (applied) setFreeTrialVerified(true);
        }).catch(() => {});
        return () => { cancelled = true; };
    }, [applyHubServiceStatus, isTigerclaw, offlineMode]);

    useEffect(() => {
        if (!isTigerclaw && !offlineMode && freeTrial && freeTrialVerified && !llmDone) {
            setLlmDone(true);
            onLLMConfigured();
        }
    }, [isTigerclaw, offlineMode, freeTrial, freeTrialVerified, llmDone, onLLMConfigured]);

    useEffect(() => {
        if (!isTigerclaw && !offlineMode && !freeTrial && llmDone && step === 2) {
            setStep(3);
        }
    }, [isTigerclaw, offlineMode, freeTrial, llmDone, step]);

    useEffect(() => {
        if (!regDone || !hubConnecting || offlineMode) return;
        let cancelled = false;
        const poll = async () => {
            try {
                const status = await GetRemoteConnectionStatus();
                if (!cancelled && status?.connected) {
                    setHubConnecting(false);
                    setRegResult(prev => {
                        const baseMsg = t("注册成功，Hub 已连接，可直接继续下一步", "Registration successful. Hub connected — you can continue.");
                        if (prev?.ok && prev.msg && prev.msg.includes('\n')) {
                            const suffix = prev.msg.substring(prev.msg.indexOf('\n'));
                            return { ok: true, msg: baseMsg + suffix };
                        }
                        return { ok: true, msg: baseMsg };
                    });
                    GetHubLLMServiceStatus().then(svcStatus => {
                        if (!cancelled) {
                            const applied = applyHubServiceStatus(svcStatus);
                            if (applied) {
                                setFreeTrialVerified(true);
                            }
                        }
                    }).catch(() => {});
                }
            } catch {
                // Ignore transient polling errors.
            }
        };
        poll();
        const id = setInterval(poll, 1500);
        return () => {
            cancelled = true;
            clearInterval(id);
        };
    }, [regDone, hubConnecting, offlineMode, t, applyHubServiceStatus]);

    useEffect(() => {
        if (step !== wxStep) wxPollingRef.current = false;
        return () => { wxPollingRef.current = false; };
    }, [step, wxStep]);

    useEffect(() => {
        return () => { CancelCodeGenSSOPolling().catch(() => {}); };
    }, []);

    useEffect(() => {
        const onKey = (e: KeyboardEvent) => {
            if (e.key === "Escape") {
                if (showOfflineModeNotice) {
                    setShowOfflineModeNotice(false);
                    return;
                }
                if (!showConfirm) onClose();
            }
        };
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [onClose, showConfirm, showOfflineModeNotice]);

    useEffect(() => {
        const allDone = isOnboardingComplete(onboardingFlow, { regDone: effectiveRegDone, llmDone, wxCompleted });
        if (allDone) {
            onSaveField({ onboarding_done: true });
            const timer = setTimeout(onClose, 1500);
            return () => clearTimeout(timer);
        }
    }, [onboardingFlow, effectiveRegDone, llmDone, wxCompleted, onClose, onSaveField]);

    const selectedProvider = selectedIdx !== null ? providers[selectedIdx] : null;
    const regResultWarning = regResult?.tone === "warning";

    const handleOfflineModeToggle = useCallback((checked: boolean) => {
        setOfflineMode(checked);
        if (checked) {
            setShowOfflineModeNotice(true);
            setFreeTrial(false);
            setFreeTrialVerified(false);
            setLlmDone(false);
            setRegResult(null);
            setHubConnecting(false);
        } else {
            setFreeTrial(true);
            if (freeTrialVerified) {
                setLlmDone(true);
                onLLMConfigured();
            }
            setRegResult(null);
        }
    }, [freeTrialVerified, onLLMConfigured, t]);

    const handleFreeTrialChange = useCallback((checked: boolean) => {
        if (offlineMode) return;
        setFreeTrial(checked);
        if (!checked) {
            setLlmDone(false);
        } else if (freeTrialVerified) {
            setLlmDone(true);
            onLLMConfigured();
        }
    }, [freeTrialVerified, offlineMode, onLLMConfigured]);

    const updateField = useCallback((field: keyof LLMProvider, value: string) => {
        if (selectedIdx === null) return;
        setProviders(prev => {
            const copy = [...prev];
            copy[selectedIdx] = { ...copy[selectedIdx], [field]: value };
            return copy;
        });
        setLlmResult(null);
    }, [selectedIdx]);

    // ── LLM Save ──
    const handleLLMSave = async () => {
        if (selectedIdx === null || !selectedProvider) return;
        const sp = selectedProvider;
        if (!sp.key?.trim()) {
            setLlmResult({ ok: false, msg: t("请输入 API Key", "Please enter API Key") });
            return;
        }
        if (sp.is_custom && !sp.url?.trim()) {
            setLlmResult({ ok: false, msg: t("请输入 API URL", "Please enter API URL") });
            return;
        }
        setLlmSaving(true);
        setLlmResult(null);
        try {
            // SSO providers: save directly (token already validated via SSO flow)
            if (sp.auth_type === "sso") {
                await SaveMaclawLLMProviders(providers, sp.name);
                setLlmResult({ ok: true, msg: t("已保存", "Saved") });
                setLlmDone(true);
                onLLMConfigured();
            } else {
                const testResult = await TestMaclawLLM({ url: sp.url, key: sp.key, model: sp.model, protocol: sp.protocol || "openai", agent_type: effectiveAgentType(sp), wire_api: sp.wire_api || "" });
                const nextProviders = providers.map((provider, index) => index === selectedIdx
                    ? { ...provider, supports_vision: testResult.supports_vision }
                    : { ...provider });
                await SaveMaclawLLMProviders(nextProviders, sp.name);

                try {
                    const freshData = await GetMaclawLLMProviders();
                    if (freshData?.providers) {
                        setProviders(freshData.providers.map((p: LLMProvider) => ({ ...p })));
                    } else {
                        setProviders(nextProviders);
                    }
                } catch {
                    setProviders(nextProviders);
                }

                const visionMsg = testResult.supports_vision
                    ? t("图片理解：支持", "Vision support: enabled")
                    : t("图片理解：不支持", "Vision support: disabled");
                setLlmResult({ ok: true, msg: `${testResult.message}\n${visionMsg}` });
                setLlmDone(true);
                onLLMConfigured();
            }
        } catch (e) {
            setLlmResult({ ok: false, msg: String(e) });
        } finally {
            setLlmSaving(false);
        }
    };

    const handleOAuthLogin = async () => {
        setOauthBusy(true);
        setLlmResult(null);
        try {
            const msg = await StartOpenAIOAuth();
            setLlmResult({ ok: true, msg: msg || "OAuth 登录成功" });
            setLlmDone(true);
            onLLMConfigured();
        } catch (e) {
            setLlmResult({ ok: false, msg: String(e) });
        } finally {
            setOauthBusy(false);
        }
    };

    // ── TigerClaw SSO Login + 自动注册 Hub（内嵌扫码流程）──
    const handleEmbeddedSSOLogin = async () => {
        setSsoBusy(true);
        setSsoResult(null);
        setEmbeddedSSOError("");
        setCodegenModels([]);
        setModelSaved(false);
        setEmbeddedSSOLoading(true);
        try {
            // Step 1: 启动 SSO 流程（打开浏览器 + 后台轮询）
            await StartCodeGenSSOEmbedded();
            setEmbeddedSSOLoading(false);

            // Step 2: 等待扫码结果（阻塞直到浏览器中完成扫码）
            const info = await WaitCodeGenSSOResult();
            setLlmDone(true);
            onLLMConfigured();

            const userEmail = info.email || "";
            setSsoResult({ ok: true, msg: info.message + (userEmail ? `\n账号：${userEmail}` : "") });

            // Step 3: Fetch models (non-blocking)
            setCodegenModelsFetching(true);
            FetchCodeGenModels().then(models => {
                setCodegenModels(models || []);
                const preferredModel = (info as any)?.model_id || "";
                const firstModel = preferredModel || (models && models.length > 0 ? models[0].id : "");
                if (firstModel) {
                    setMaclawModel(firstModel);
                    setClaudeCodeModel(firstModel);
                    return SaveCodeGenModelChoice(firstModel, firstModel).then(() => {
                        setModelSaved(true);
                    });
                }
            }).catch(err => {
                console.warn("[TigerClaw] Auto-select first CodeGen model failed:", err);
            }).finally(() => {
                setCodegenModelsFetching(false);
            });

            // Step 4: Auto-register hub
            if (userEmail) {
                setRegBusy(true);
                onSaveField({ remote_email: userEmail });
                try {
                    const result = await ActivateRemote(userEmail, "", "");
                    if (result?.vip_flag) setVipFlag(true);
                    setRegDone(true);
                    onRegistered();
                } catch (regErr) {
                    console.warn("[TigerClaw] Hub 自动注册失败:", regErr);
                    // 注册失败仍标记完成，避免用户卡在此步骤无法继续
                    // SSO 认证已成功，Hub 注册可在后续自动重试（autoRegisterOnStartup）
                    setRegDone(true);
                    onRegistered();
                    setSsoResult({ ok: true, msg: info.message + (userEmail ? `\n账号：${userEmail}` : "") + "\n" + t("（Hub 注册暂时失败，将在下次启动时自动重试）", " (Hub registration failed, will auto-retry on next launch)") });
                } finally {
                    setRegBusy(false);
                }
            } else {
                setRegDone(true);
            }
        } catch (e) {
            setEmbeddedSSOLoading(false);
            const errMsg = String(e);
            setEmbeddedSSOError(errMsg);
            setSsoResult({ ok: false, msg: errMsg });
        } finally {
            setSsoBusy(false);
        }
    };

    // ── TigerClaw 模型选择保存 ──
    const handleModelSave = useCallback(async () => {
        setModelSaving(true);
        try {
            await SaveCodeGenModelChoice(maclawModel, claudeCodeModel);
            setModelSaved(true);
        } catch (e) {
            console.error("[TigerClaw] SaveCodeGenModelChoice failed:", e);
        } finally {
            setModelSaving(false);
        }
    }, [maclawModel, claudeCodeModel]);

    // ── Registration ──
    const handleRegisterClick = () => {
        if (!regEmail.trim()) {
            setRegResult({ ok: false, msg: t("请输入邮箱", "Please enter email") });
            return;
        }
        setShowConfirm(true);
    };

    const doRegister = async () => {
        setShowConfirm(false);
        setRegBusy(true);
        setRegResult(null);
        setInvError("");
        const trimmedRedeemCode = redeemCode.trim();
        onSaveField({ remote_email: regEmail.trim() });
        try {
            const result = await ActivateRemote(regEmail.trim(), invCode.trim().toUpperCase(), "");
            if (result?.vip_flag) setVipFlag(true);
            let redeemNote = "";
            if (trimmedRedeemCode) {
                try {
                    const serviceStatus = await RedeemHubLLMService(trimmedRedeemCode) as HubLLMServiceStatus;
                    const skippedByStatus = applyHubServiceStatus(serviceStatus);
                    setRedeemCode("");
                    if (!skippedByStatus) {
                        if (serviceStatus?.active) {
                            setLlmDone(true);
                            onLLMConfigured();
                        } else {
                            setFreeTrial(false);
                            setLlmDone(false);
                            redeemNote = `\n${hubInactiveRedeemMessage(serviceStatus)}`;
                        }
                    }
                    if (!redeemNote) {
                        redeemNote = `\n${t("✅ 服务兑换码已激活，LLM 配置步骤已自动跳过", "✅ Service code redeemed. LLM configuration step skipped automatically.", "✅ 服務兌換碼已啟用，LLM 配置步驟已自動跳過")}`;
                    }
                } catch (redeemError) {
                    const localizedRedeemError = localizeHubServiceRedeemError(redeemError, lang);
                    redeemNote = `\n${t("服务兑换码兑换失败，请稍后在服务状态中重试：", "Service redeem code failed. You can retry later in service status: ", "服務兌換碼兌換失敗，請稍後在服務狀態中重試：")}${localizedRedeemError}`;
                }
            }
            setHubConnecting(true);
            setRegResult({
                ok: true,
                msg: `${t("注册成功，正在后台连接 Hub，可直接继续下一步", "Registration successful. Connecting to Hub in the background - you can continue.", "註冊成功，正在後台連線 Hub，可直接繼續下一步")}${redeemNote}`,
            });
            setRegDone(true);
            onRegistered();
        } catch (e) {
            const errMsg = String(e);
            if (errMsg.includes("INVITATION_CODE_REQUIRED")) {
                setInvRequired(true);
                setRegResult({ ok: false, msg: t("请输入邀请码后重试", "Invitation code required") });
            } else if (errMsg.includes("INVALID_INVITATION_CODE")) {
                setInvRequired(true);
                setInvError(t("邀请码无效或已被使用", "Invalid or used invitation code"));
                setRegResult({ ok: false, msg: t("邀请码无效", "Invalid invitation code") });
            } else if (errMsg.includes("INVITATION_EXPIRED")) {
                setInvRequired(true);
                setInvError(t("用户已失效，请使用新的邀请码", "Expired, use a new invitation code"));
                setRegResult({ ok: false, msg: t("邀请码已过期", "Invitation code expired") });
            } else if (errMsg.includes("INVITATION_CODE_NOT_ROUTED")) {
                setInvError(t("邀请码无效或未注册，请检查后重试", "Invitation code not recognized. Please check and try again."));
                setRegResult({ ok: false, msg: t("邀请码无法路由", "Invitation code not routed") });
            } else if (errMsg.includes("INVITATION_CODE_HUB_OFFLINE")) {
                setInvError(t("邀请码对应的服务器当前不可用，请稍后重试", "The server for this invitation code is currently offline. Please try again later."));
                setRegResult({ ok: false, msg: t("目标服务器离线", "Target server offline") });
            } else {
                setRegResult({ ok: false, msg: errMsg });
            }
        } finally {
            setRegBusy(false);
        }
    };

    // ── WeChat QR Login ──
    const startWxQR = async () => {
        wxPollingRef.current = false;
        setWxLoading(true);
        setWxQrUrl("");
        setWxStatus("");
        setWxMsg(t("正在获取二维码...", "Fetching QR code..."));
        try {
            const res = await StartWeixinQRLogin();
            if (res.error) {
                setWxMsg("❌ " + res.error);
                setWxStatus("error");
                setWxLoading(false);
                return;
            }
            const qrUrl = res.qrcode_url || "";
            const token = res.qrcode_token || "";
            if (!qrUrl || !token) {
                setWxMsg(t("❌ 获取二维码失败，请重试", "❌ Failed to get QR code, please retry"));
                setWxStatus("error");
                setWxLoading(false);
                return;
            }
            setWxQrUrl(qrUrl);
            setWxStatus("wait");
            setWxMsg(t("请用微信扫描二维码", "Scan with WeChat"));
            setWxLoading(false);

            // Frontend-driven short polling (every 2s) instead of blocking call
            wxPollingRef.current = true;
            const pollStart = Date.now();
            const maxPollMs = 8 * 60 * 1000; // 8 min timeout

            const doPoll = async () => {
                if (!wxPollingRef.current) return;
                if (Date.now() - pollStart > maxPollMs) {
                    setWxStatus("expired");
                    setWxMsg(t("二维码已过期，请刷新", "QR expired, please refresh"));
                    wxPollingRef.current = false;
                    return;
                }
                try {
                    const poll = await PollWeixinQRStatus(token);
                    if (!wxPollingRef.current) return;
                    const st = poll.status || "";
                    if (st === "confirmed") {
                        if (!wxPollingRef.current) return;
                        if (poll.error) {
                            setWxStatus("error");
                            setWxMsg("❌ " + poll.error);
                        } else {
                            setWxStatus("confirmed");
                            setWxMsg(poll.message || t("✅ 微信绑定成功", "✅ WeChat connected"));
                            setWxDone(true);
                        }
                        wxPollingRef.current = false;
                        return;
                    } else if (st === "scaned") {
                        setWxMsg(t("已扫码，请在微信确认...", "Scanned, please confirm in WeChat..."));
                    } else if (st === "wait" || !st) {
                        setWxStatus("wait");
                        setWxMsg(t("请用微信扫描二维码", "Scan with WeChat"));
                    } else if (st === "expired") {
                        setWxStatus("expired");
                        setWxMsg(poll.message || t("二维码已过期，请刷新", "QR expired, please refresh"));
                        wxPollingRef.current = false;
                        return;
                    } else if (poll.error) {
                        if (poll.retryable === "true") {
                            setWxStatus("wait");
                            setWxMsg("❌ " + poll.error);
                            setTimeout(doPoll, 2000);
                            return;
                        }
                        setWxStatus("error");
                        setWxMsg("❌ " + poll.error);
                        wxPollingRef.current = false;
                        return;
                    }
                    // "wait" — schedule next poll
                    if (wxPollingRef.current) {
                        setTimeout(doPoll, 2000);
                    }
                } catch {
                    if (!wxPollingRef.current) return;
                    setWxStatus("error");
                    setWxMsg(t("连接失败，请重试", "Connection failed, please retry"));
                    wxPollingRef.current = false;
                }
            };
            doPoll();
        } catch (e) {
            setWxMsg("❌ " + String(e));
            setWxStatus("error");
            setWxLoading(false);
        }
    };

    // ── Step labels (memoized) ──
    const labels = useMemo(() => getOnboardingStepLabels(onboardingFlow, lang), [onboardingFlow, lang]);
    const showRegistrationToast = !!regResult && !showConfirm && !showOfflineModeNotice && (isCurrentOnboardingStep(onboardingFlow, step, 'register') || isCurrentOnboardingStep(onboardingFlow, step, 'mode'));
    const registrationToastTitle = regResultWarning ? t("需要处理", "Action needed", "需要處理") : regResult?.ok ? t("注册完成", "Registration successful", "註冊完成") : t("注册失败", "Registration failed", "註冊失敗");
    const registrationToastDetail = (() => {
        if (!regResult) return "";
        if (!regResult.ok || regResultWarning) return regResult.msg;
        const extraNote = regResult.msg.split("\n").slice(1).map(item => item.trim()).filter(Boolean).join(" ");
        if (extraNote) return extraNote.replace(/^✅\s*/, "");
        if (offlineMode) return t("已进入离线模式，可继续下一步", "Offline mode is ready. You can continue.", "已進入離線模式，可繼續下一步");
        if (hubConnecting) return t("正在连接 Hub，可继续下一步", "Connecting to Hub. You can continue.", "正在連線 Hub，可繼續下一步");
        if (freeTrial && freeTrialVerified && llmDone) return t("Hub 已连接，免费试用已激活，可继续下一步", "Hub connected. Free trial activated. You can continue.", "Hub 已連線，免費試用已啟用，可繼續下一步");
        if (freeTrial && !llmDone) return t("Hub 已连接，正在验证免费试用服务", "Hub connected. Verifying free trial service.", "Hub 已連線，正在驗證免費試用服務");
        return t("Hub 已连接，可继续下一步", "Hub connected. You can continue.", "Hub 已連線，可繼續下一步");
    })();
    const srOnlyStyle = { position: "absolute", width: 1, height: 1, padding: 0, margin: -1, overflow: "hidden", clip: "rect(0, 0, 0, 0)", whiteSpace: "nowrap", border: 0 } as const;

    return createPortal(
        <div style={{
            position: "fixed", top: 0, left: 0, right: 0, bottom: 0,
            background: "rgba(0,0,0,0.3)", backdropFilter: "blur(3px)",
            display: "flex", alignItems: "center", justifyContent: "center", zIndex: 20000,
        }} data-ai-theme={portalTheme}>
            {showRegistrationToast && regResult && (
                <div style={{ position: "absolute", top: 116, left: "50%", transform: "translateX(-50%)", zIndex: 20001, width: "min(420px, calc(100vw - 36px))", maxHeight: "calc(100vh - 148px)", overflowY: "auto", boxSizing: "border-box" }} role={regResult.ok ? "status" : "alert"} aria-live="polite">
                    <div style={{ ...wizardBannerStyle(regResultWarning ? "warning" : regResult.ok ? "success" : "error"), marginTop: 0, background: "var(--theme-surface)", border: `1px solid ${regResultWarning ? "rgba(245,158,11,0.65)" : regResult.ok ? "rgba(34,197,94,0.65)" : "rgba(239,68,68,0.65)"}`, borderLeft: `3px solid ${regResultWarning ? colors.warning : regResult.ok ? colors.success : colors.danger}`, boxShadow: "0 14px 36px rgba(0,0,0,0.28)" }}>
                        <div style={{ display: "flex", alignItems: "flex-start", gap: 10 }}>
                            <span aria-hidden="true" style={{ flexShrink: 0, fontSize: "1rem", lineHeight: 1.35 }}>
                                {regResultWarning ? "⚠️" : regResult.ok ? "✅" : "❌"}
                            </span>
                            <div style={{ minWidth: 0 }}>
                                <div style={{ color: colors.textPrimary, fontSize: "0.82rem", fontWeight: 800, lineHeight: 1.35 }}>
                                    {registrationToastTitle}
                                </div>
                                <div style={{ marginTop: 2, color: regResult.ok && !regResultWarning ? colors.success : colors.textSecondary, fontSize: "0.74rem", fontWeight: 600, lineHeight: 1.45 }}>
                                    {registrationToastDetail}
                                    {regResult.ok && hubConnecting && !offlineMode && (
                                        <>
                                            <span style={srOnlyStyle}>{t("正在后台连接 Hub", "Connecting to Hub in the background", "正在後台連線 Hub")}</span>
                                            <span style={srOnlyStyle}>{hubT("Hub connecting", "Hub 连接中", "Hub 連線中")}</span>
                                        </>
                                    )}
                                    {regResult.ok && !hubConnecting && !offlineMode && (
                                        <span style={srOnlyStyle}>{hubT("Hub connected", "Hub 已连接", "Hub 已連線")}</span>
                                    )}
                                </div>
                            </div>
                        </div>
                    </div>
                </div>
            )}
            <div style={{
                background: "var(--theme-surface)", borderRadius: 16, width: 460, maxHeight: "90vh",
                overflowY: "auto", boxShadow: "0 16px 48px rgba(15,23,42,0.22)",
                border: "1px solid var(--theme-border)", display: "flex", flexDirection: "column",
            }}>
                <div style={{
                    background: "linear-gradient(135deg, var(--theme-info-bg, #eef2ff) 0%, var(--theme-primary-soft, #e0e7ff) 100%)",
                    padding: "20px 22px 18px", position: "relative", flexShrink: 0,
                    borderBottom: "1px solid var(--theme-border)",
                }}>
                    <button onClick={onClose} style={{
                        position: "absolute", top: 12, right: 14, border: "none",
                        background: "transparent", cursor: "pointer", fontSize: "1.25rem",
                        color: colors.textMuted, lineHeight: 1,
                    }}>&times;</button>
                    <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
                        <div style={{
                            width: 40, height: 40, borderRadius: "50%", flexShrink: 0,
                            background: "var(--theme-surface)", boxShadow: "0 2px 6px rgba(15,23,42,0.12)",
                            display: "flex", alignItems: "center", justifyContent: "center", fontSize: "1.35rem",
                        }}>👋</div>
                        <div style={{ minWidth: 0 }}>
                            <h3 style={{ margin: 0, color: colors.primaryDark, fontSize: "1.05rem", fontWeight: 600, lineHeight: 1.25 }}>
                                {t(`来，配置一下 ${displayName} 吧`, `Let's get ${displayName} ready!`)}
                            </h3>
                            <p style={{ margin: "3px 0 0", fontSize: "0.74rem", color: colors.textSecondary, lineHeight: 1.4 }}>
                                {t("快速完成几步配置即可开始使用", "Just a few quick steps to get started")}
                            </p>
                        </div>
                    </div>
                </div>

                <div style={{
                    display: "flex", alignItems: "flex-start", justifyContent: "center",
                    gap: 0, padding: "16px 18px 8px", flexShrink: 0,
                }}>
                    {Array.from({ length: totalSteps }, (_, i) => {
                        const s = i + 1;
                        const done = stepDone[s];
                        const active = s === step;
                        // Last step (WeChat) skipped: show grey instead of green
                        const skippedStep = s === totalSteps && wxSkipped && !wxDone;
                        const circleColor = skippedStep ? colors.textMuted : done ? colors.success : active ? colors.primary : "var(--theme-border)";
                        const filled = skippedStep || done || active;
                        return (
                            <div key={s} style={{ display: "flex", alignItems: "center" }}>
                                <div style={{ display: "flex", flexDirection: "column", alignItems: "center", minWidth: 58 }}>
                                    <div style={{
                                        width: 28, height: 28, borderRadius: "50%",
                                        background: circleColor,
                                        color: filled ? colors.onPrimary : colors.textMuted,
                                        display: "flex", alignItems: "center", justifyContent: "center",
                                        fontSize: "0.74rem", fontWeight: 700,
                                        boxShadow: active ? `0 0 0 4px var(--theme-primary-soft)` : "none",
                                        transition: "background 0.2s, box-shadow 0.2s",
                                    }}>
                                        {skippedStep ? "—" : done ? "✓" : s}
                                    </div>
                                    <span style={{
                                        fontSize: "0.66rem", marginTop: 6, textAlign: "center",
                                        color: active ? colors.primaryDark : done ? colors.textSecondary : colors.textMuted,
                                        fontWeight: active ? 600 : 400,
                                    }}>
                                        {labels[i]}
                                    </span>
                                </div>
                                {s < totalSteps && (
                                    <div style={{
                                        width: 34, height: 2, background: stepDone[s] ? colors.success : "var(--theme-border)",
                                        margin: "0 2px", marginBottom: 18, borderRadius: 1, transition: "background 0.2s",
                                    }} />
                                )}
                            </div>
                        );
                    })}
                </div>

                <div style={{ padding: "12px 20px 4px", flex: 1, overflowY: "auto" }}>

                    <style>{`@keyframes spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }`}</style>

                    {isCurrentOnboardingStep(onboardingFlow, step, 'sso') && (
                        <div>
                            <p style={{ margin: "0 0 10px 0", fontSize: "0.76rem", color: colors.textSecondary, lineHeight: 1.4 }}>
                                {t(
                                    `使用企业账号一键登录，自动配置 ${displayName} 和 TigerClaw Code，并注册到 Hub。`,
                                    `Sign in with your enterprise account to configure ${displayName}, TigerClaw Code, and register to Hub.`
                                )}
                            </p>
                            <button onClick={handleEmbeddedSSOLogin} disabled={ssoBusy || (llmDone && regDone)} style={{
                                ...((llmDone && regDone) ? wizardSuccessButtonStyle : wizardPrimaryButtonStyle),
                                fontSize: "0.84rem",
                                cursor: (ssoBusy || regBusy || (llmDone && regDone)) ? "default" : "pointer",
                            }}>
                                {ssoBusy
                                    ? t("⏳ 等待浏览器授权...", "⏳ Waiting for browser auth...")
                                    : regBusy
                                        ? t("⏳ 注册中...", "⏳ Registering...")
                                        : (llmDone && regDone)
                                            ? t("✅ 认证并注册完成", "✅ Authenticated & Registered")
                                            : t("🏢 企业 SSO 登录", "🏢 Enterprise SSO Login")}
                            </button>
                            {embeddedSSOLoading && (
                                <div style={{ textAlign: "center", padding: "20px 0" }}>
                                    <p style={{ fontSize: "0.76rem", color: colors.textMuted }}>
                                        {t("正在打开登录页面...", "Opening login page...")}
                                    </p>
                                </div>
                            )}
                            {ssoBusy && !embeddedSSOLoading && (
                                <div style={{ textAlign: "center", padding: "16px 0" }}>
                                    <div style={{ fontSize: "2rem", marginBottom: 8 }}>🔐</div>
                                    <p style={{ fontSize: "0.76rem", color: colors.textSecondary, marginTop: 10 }}>
                                        {t("请在弹出的浏览器页面中扫码", "Please scan the QR code in the browser window")}
                                    </p>
                                    <p style={{ fontSize: "0.7rem", color: colors.textMuted }}>
                                        {t("扫码完成后将自动继续...", "Will continue automatically after scanning...")}
                                    </p>
                                </div>
                            )}
                            {embeddedSSOError && (
                                <div style={{ marginTop: 10 }}>
                                    <button onClick={() => {
                                        StartCodeGenSSO().then(async info => {
                                            setLlmDone(true);
                                            onLLMConfigured();
                                            const userEmail = (info as any)?.email || "";
                                            setSsoResult({ ok: true, msg: (info as any)?.message || "SSO OK" });
                                            try {
                                                const models = await FetchCodeGenModels();
                                                setCodegenModels(models || []);
                                                const preferredModel = (info as any)?.model_id || "";
                                                const firstModel = preferredModel || (models && models.length > 0 ? models[0].id : "");
                                                if (firstModel) {
                                                    setMaclawModel(firstModel);
                                                    setClaudeCodeModel(firstModel);
                                                    await SaveCodeGenModelChoice(firstModel, firstModel);
                                                    setModelSaved(true);
                                                }
                                            } catch (modelErr) {
                                                console.warn("[TigerClaw] Auto-select first CodeGen model failed:", modelErr);
                                            }
                                            if (userEmail) {
                                                try {
                                                    await ActivateRemote(userEmail, "", "");
                                                    onRegistered();
                                                } catch (regErr) {
                                                    console.warn("[TigerClaw] Hub fallback registration failed:", regErr);
                                                }
                                            }
                                            setRegDone(true);
                                        }).catch(e => setSsoResult({ ok: false, msg: String(e) }));
                                    }} style={{
                                        ...wizardGhostButtonBlockStyle, color: colors.primaryDark,
                                    }}>
                                        {t("🌐 在浏览器中打开", "🌐 Open in Browser")}
                                    </button>
                                    <button onClick={handleEmbeddedSSOLogin} style={{
                                        ...wizardGhostButtonBlockStyle, color: colors.primaryDark,
                                    }}>
                                        {t("🔄 重试", "🔄 Retry")}
                                    </button>
                                </div>
                            )}
                            {ssoResult && (
                                <div style={wizardBannerStyle(ssoResult.ok ? "success" : "error")}>
                                    {ssoResult.ok ? `✅ ${ssoResult.msg}` : `❌ ${ssoResult.msg}`}
                                </div>
                            )}
                            {!(llmDone && regDone) && !ssoBusy && !regBusy && !embeddedSSOLoading && !embeddedSSOError && (
                                <p style={{ marginTop: 8, fontSize: "0.7rem", color: colors.textMuted, textAlign: "center" }}>
                                    {t("点击后自动弹出企业登录页，扫码后自动完成所有配置", "Browser opens automatically; all config applied after login")}
                                </p>
                            )}

                            {/* ── 模型选择区域（SSO 成功后显示）── */}
                            {llmDone && (
                                <div style={{ marginTop: 14, padding: "12px 14px", borderRadius: 8, border: `1px solid ${colors.border}`, background: colors.surfaceMuted }}>
                                    <div style={{ fontSize: "0.76rem", fontWeight: 600, color: colors.text, marginBottom: 10 }}>
                                        {t("🔧 选择使用的模型（可选）", "🔧 Select Models (optional)")}
                                    </div>

                                    {codegenModelsFetching && (
                                        <p style={{ fontSize: "0.72rem", color: colors.textMuted }}>
                                            {t("正在获取可用模型列表...", "Fetching available models...")}
                                        </p>
                                    )}

                                    {!codegenModelsFetching && codegenModels.length > 0 && (
                                        <>
                                            {/* MaClaw 模型 */}
                                            <div style={{ marginBottom: 10 }}>
                                                <label style={{ fontSize: "0.72rem", color: colors.textSecondary, display: "block", marginBottom: 4 }}>
                                                    🤖 {t(`${displayName} Agent 模型`, `${displayName} Agent Model`)}
                                                </label>
                                                <div style={{ display: "flex", gap: 4, flexWrap: "wrap" }}>
                                                    {codegenModels.map(m => (
                                                        <button key={m.id} onClick={() => setMaclawModel(m.id)} style={wizardSelectableChipStyle(maclawModel === m.id, "sm")}>
                                                            {m.name}
                                                        </button>
                                                    ))}
                                                </div>
                                            </div>

                                            {/* TigerClaw Code 模型 */}
                                            <div style={{ marginBottom: 10 }}>
                                                <label style={{ fontSize: "0.72rem", color: colors.textSecondary, display: "block", marginBottom: 4 }}>
                                                    🐯 {t("TigerClaw Code 模型", "TigerClaw Code Model")}
                                                </label>
                                                <div style={{ display: "flex", gap: 4, flexWrap: "wrap" }}>
                                                    {codegenModels.map(m => (
                                                        <button key={m.id} onClick={() => setClaudeCodeModel(m.id)} style={wizardSelectableChipStyle(claudeCodeModel === m.id, "sm")}>
                                                            {m.name}
                                                        </button>
                                                    ))}
                                                </div>
                                            </div>

                                            {/* 保存按钮 */}
                                            <button onClick={handleModelSave} disabled={modelSaving || modelSaved} style={{
                                                ...(modelSaved ? wizardSuccessButtonStyle : wizardPrimaryButtonStyle),
                                                padding: "7px 0", fontSize: "0.76rem",
                                                cursor: modelSaving || modelSaved ? "default" : "pointer",
                                            }}>
                                                {modelSaved
                                                    ? t("✅ 模型已保存", "✅ Models Saved")
                                                    : modelSaving
                                                        ? t("保存中...", "Saving...")
                                                        : t("确认模型选择", "Confirm Model Selection")}
                                            </button>
                                        </>
                                    )}
                                </div>
                            )}
                        </div>
                    )}

                    {(isCurrentOnboardingStep(onboardingFlow, step, 'register') || isCurrentOnboardingStep(onboardingFlow, step, 'mode')) && (
                        <div>
                            <p style={{ margin: "0 0 10px 0", fontSize: "0.76rem", color: colors.textSecondary, lineHeight: 1.4 }}>
                                {offlineMode
                                    ? t("选择离网模式后，将跳过 Hub 注册并进入 LLM 配置。", "Offline mode skips Hub registration and continues to LLM setup.", "選擇離網模式後，將跳過 Hub 註冊並進入 LLM 配置。")
                                    : t("选择运行模式。正常联网模式下，注册邮箱到 Hub 后即可使用所有功能。", "Choose a run mode. In online mode, register your email to the Hub to unlock all features.", "選擇運行模式。正常聯網模式下，註冊郵箱到 Hub 後即可使用所有功能。")}
                            </p>
                            <OnboardingOfflineModeOption
                                offlineMode={offlineMode}
                                freeTrial={freeTrial}
                                onToggle={handleOfflineModeToggle}
                                onFreeTrialChange={handleFreeTrialChange}
                                t={t}
                            />
                            {!offlineMode && (
                                <>
                            <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 10 }}>
                                <label style={{ ...labelStyle, marginBottom: 0, flex: "0 0 112px", whiteSpace: "nowrap" }}>{t("邮箱", "Email")} <span style={{ color: colors.danger }}>*</span></label>
                                <input style={{ ...inputStyle, flex: 1, minWidth: 0 }} value={regEmail}
                                    onChange={e => setRegEmail(e.target.value)}
                                    placeholder="name@example.com" spellCheck={false} />
                            </div>
                            <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 10 }}>
                                <label style={{ ...labelStyle, marginBottom: 0, flex: "0 0 112px", whiteSpace: "nowrap" }}>
                                    {t("邀请码", "Invitation Code")}{" "}
                                    <span style={{ fontSize: "0.68rem", color: colors.textMuted }}>({t("可选", "optional")})</span>
                                </label>
                                <div style={{ flex: 1, minWidth: 0 }}>
                                    <input style={{ ...inputStyle, width: "100%", ...(invError ? { borderColor: colors.danger } : {}) }}
                                        value={invCode}
                                        onChange={e => { setInvCode(e.target.value.toUpperCase()); setInvError(""); }}
                                        placeholder={t("请输入邀请码（可选）", "Enter invitation code (optional)", "請輸入邀請碼（可選）")}
                                        maxLength={20} spellCheck={false} />
                                    {invError && <div style={{ fontSize: "0.72rem", color: colors.danger, marginTop: 4 }}>{invError}</div>}
                                </div>
                            </div>
                            <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 10 }}>
                                <label style={{ ...labelStyle, marginBottom: 0, flex: "0 0 112px", whiteSpace: "nowrap" }}>
                                    {t("服务兑换码", "Service redeem code", "服務兌換碼")} {" "}
                                    <span style={{ fontSize: "0.68rem", color: colors.textMuted }}>({t("可选", "optional", "可選")})</span>
                                </label>
                                <input style={{ ...inputStyle, flex: 1, minWidth: 0 }}
                                    value={redeemCode}
                                    onChange={e => setRedeemCode(e.target.value.trim().toUpperCase())}
                                    placeholder={t("请输入服务兑换码（可选）", "Enter service redeem code (optional)", "請輸入服務兌換碼（可選）")}
                                    spellCheck={false} />
                            </div>
                            <button onClick={handleRegisterClick} disabled={regBusy || regDone} style={{
                                ...((regDone && !hubConnecting) ? wizardSuccessButtonStyle : wizardPrimaryButtonStyle),
                                padding: "10px 0", fontSize: "0.8rem",
                                cursor: regBusy || regDone ? "default" : "pointer",
                            }}>
                                <HubRegisterButtonContent regBusy={regBusy} regDone={regDone} hubConnecting={hubConnecting} t={hubT} />
                            </button>
                                </>
                            )}
                        </div>
                    )}

                    {isCurrentOnboardingStep(onboardingFlow, step, 'llm') && (
                        <div>
                            <p style={{ margin: "0 0 8px 0", fontSize: "0.76rem", color: colors.textSecondary, lineHeight: 1.4 }}>
                                {t("选择一个 LLM 服务商，输入 API Key 后测试并保存。",
                                    "Pick a provider, enter your API Key, then test & save.")}
                            </p>
                            {/* Provider buttons */}
                            <div style={{ display: "flex", gap: 6, marginBottom: 10, flexWrap: "wrap" }}>
                                {providers.map((p, i) => {
                                    // Skip auth_type "none" providers
                                    if (p.auth_type === "none") return null;
                                    const active = selectedIdx === i;
                                    return (
                                        <div key={i} style={{ textAlign: "center" }}>
                                            <button onClick={() => {
                                                setSelectedIdx(active ? null : i); setLlmResult(null);
                                            }} style={{
                                                fontSize: "0.78rem", padding: "6px 16px", cursor: "pointer",
                                                background: active ? colors.primary : colors.surfaceMuted,
                                                color: active ? colors.onPrimary : colors.text,
                                                border: `1px solid ${active ? colors.primary : colors.border}`,
                                                borderRadius: 6, fontWeight: active ? 600 : 400,
                                                transition: "all 0.15s",
                                                display: "inline-flex", alignItems: "center", gap: 5,
                                            }}>
                                                {PROVIDER_LOGOS[p.name] ?? null}{p.name}
                                            </button>
                                            {p.auth_type === "oauth" && (
                                                <div style={{ fontSize: "0.62rem", color: colors.textMuted, marginTop: 2 }}>
                                                    {t("一键登录", "One-click")}
                                                </div>
                                            )}
                                        </div>
                                    );
                                })}
                            </div>
                            {/* LLM config form */}
                            {selectedProvider && (
                                <div style={{
                                    padding: 14, borderRadius: 8,
                                    border: `1px solid ${colors.border}`, background: colors.surfaceMuted,
                                }}>
                                    {selectedProvider.auth_type === "oauth" ? (
                                        <>
                                            <p style={{ fontSize: "0.76rem", color: colors.textSecondary, margin: "0 0 12px 0", lineHeight: 1.4 }}>
                                                {t("点击下方按钮，将在浏览器中完成 OpenAI 账号授权。",
                                                    "Click below to authorize with your OpenAI account in the browser.")}
                                            </p>
                                            <button onClick={handleOAuthLogin} disabled={oauthBusy} style={{
                                                ...wizardPrimaryButtonStyle, cursor: oauthBusy ? "default" : "pointer",
                                            }}>
                                                {oauthBusy ? t("等待浏览器授权...", "Waiting for browser auth...") : t("使用 OpenAI 账号登录", "Sign in with OpenAI")}
                                            </button>
                                            {oauthBusy && (
                                                <button onClick={() => { CancelOpenAIOAuth(); setOauthBusy(false); }} style={{
                                                    ...wizardGhostButtonBlockStyle, color: colors.textMuted,
                                                }}>
                                                    {t("取消", "Cancel")}
                                                </button>
                                            )}
                                        </>
                                    ) : (
                                        <>
                                            {selectedProvider.is_custom ? (
                                                <>
                                                    {/* Protocol selection */}
                                                    <div style={{ marginBottom: 10 }}>
                                                        <label style={labelStyle}>{t("协议", "Protocol")}</label>
                                                        <div style={{ display: "flex", gap: 6 }}>
                                                            {(["openai", "anthropic"] as const).map(proto => {
                                                                const active = (selectedProvider.protocol || "openai") === proto;
                                                                return (
                                                                    <button key={proto} onClick={() => updateField("protocol", proto)} style={wizardSelectableChipStyle(active, "md")}>
                                                                        {proto === "openai" ? "OpenAI" : "Anthropic"}
                                                                    </button>
                                                                );
                                                            })}
                                                        </div>
                                                        <p style={{ fontSize: "0.68rem", color: colors.textMuted, margin: "4px 0 0 0", lineHeight: 1.4 }}>
                                                            {(selectedProvider.protocol || "openai") === "anthropic"
                                                                ? t("使用 Anthropic Messages API（x-api-key 鉴权）", "Uses Anthropic Messages API (x-api-key auth)")
                                                                : t("使用 OpenAI 兼容接口（Bearer Token 鉴权）", "Uses OpenAI-compatible API (Bearer token auth)")}
                                                        </p>
                                                    </div>
                                                    {/* User-Agent selection */}
                                                    <div style={{ marginBottom: 10 }}>
                                                        <label style={labelStyle}>User-Agent</label>
                                                        <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
                                                            {KNOWN_USER_AGENTS.map(ua => {
                                                                const currentAgent = effectiveAgentType(selectedProvider);
                                                                const active = currentAgent === ua;
                                                                return (
                                                                    <button key={ua} onClick={() => updateField("agent_type", ua)} style={wizardSelectableChipStyle(active, "md")}>
                                                                        {ua}
                                                                    </button>
                                                                );
                                                            })}
                                                            {(() => {
                                                                const currentAgent = effectiveAgentType(selectedProvider);
                                                                const active = !isKnownUserAgent(currentAgent);
                                                                return (
                                                                    <button onClick={() => updateField("agent_type", active ? currentAgent : customAgentSeedForProvider(selectedProvider))} style={wizardSelectableChipStyle(active, "md")}>
                                                                        {t("\u81ea\u5b9a\u4e49", "Custom", "\u81ea\u8a02")}
                                                                    </button>
                                                                );
                                                            })()}
                                                        </div>
                                                        {!isKnownUserAgent(effectiveAgentType(selectedProvider)) && (
                                                            <input style={{ ...inputStyle, marginTop: 8 }} value={editableCustomAgentValue(selectedProvider)} onChange={e => updateField("agent_type", nextCustomAgentValue(selectedProvider, e.target.value))} placeholder={t("\u81ea\u5b9a\u4e49 User-Agent", "Custom User-Agent", "\u81ea\u8a02 User-Agent")} autoCapitalize="off" autoCorrect="off" spellCheck={false} autoComplete="off" />
                                                        )}
                                                    </div>
                                                    <div style={{ marginBottom: 10 }}>
                                                        <label style={labelStyle}>API URL <span style={{ color: colors.danger }}>*</span></label>
                                                        <input style={inputStyle} value={selectedProvider.url}
                                                            onChange={e => updateField("url", e.target.value)}
                                                            placeholder="https://api.openai.com/v1" />
                                                    </div>
                                                    <div style={{ marginBottom: 10 }}>
                                                        <label style={labelStyle}>{t("模型名称", "Model Name")}</label>
                                                        <input style={inputStyle} value={selectedProvider.model}
                                                            onChange={e => updateField("model", e.target.value)}
                                                            placeholder="gpt-4o" />
                                                    </div>
                                                </>
                                            ) : (
                                                <>
                                                    <div style={{ marginBottom: 10 }}>
                                                        <label style={labelStyle}>API URL <span style={{ fontSize: "0.68rem", color: colors.textMuted }}>({t("预设", "preset")})</span></label>
                                                        <input style={readonlyInputStyle} value={selectedProvider.url} readOnly tabIndex={-1} />
                                                    </div>
                                                    <div style={{ marginBottom: 10 }}>
                                                        <label style={labelStyle}>{t("模型名称", "Model Name")} <span style={{ fontSize: "0.68rem", color: colors.textMuted }}>({t("预设", "preset")})</span></label>
                                                        <input style={readonlyInputStyle} value={selectedProvider.model} readOnly tabIndex={-1} />
                                                    </div>
                                                </>
                                            )}
                                            <div style={{ marginBottom: 12 }}>
                                                <label style={labelStyle}>API Key <span style={{ color: colors.danger }}>*</span></label>
                                                <input style={inputStyle} type="password" value={selectedProvider.key}
                                                    onChange={e => updateField("key", e.target.value)}
                                                    placeholder={selectedProvider.is_custom ? "sk-..." : ((selectedProvider.name === "智谱龙虾" || selectedProvider.name === "智谱编程") ? "xxxxxxxx.yyyyyyyy" : "sk-...")}
                                                    autoComplete="off" />
                                            </div>
                                            <button onClick={handleLLMSave} disabled={llmSaving} style={{
                                                ...wizardPrimaryButtonStyle, padding: "8px 0", fontSize: "0.8rem",
                                                cursor: llmSaving ? "default" : "pointer",
                                            }}>
                                                {llmSaving ? t("测试并保存中...", "Testing & Saving...") : t("测试并保存", "Test & Save")}
                                            </button>
                                        </>
                                    )}
                                    {llmResult && (
                                        <div style={wizardBannerStyle(llmResult.ok ? "success" : "error")}>
                                            {llmResult.ok ? `✅ ${t("连接成功，已保存", "Connected & saved")}\n${llmResult.msg}` : `❌ ${llmResult.msg}`}
                                        </div>
                                    )}
                                </div>
                            )}
                        </div>
                    )}

                    {/* ═══ 微信绑定 ═══
                        TigerClaw: step === 2
                        普通品牌: step === 3
                    */}
                    {isCurrentOnboardingStep(onboardingFlow, step, 'wechat') && (
                        <div>
                            <p style={{ margin: "0 0 10px 0", fontSize: "0.76rem", color: colors.textSecondary, lineHeight: 1.4 }}>
                                {t(`扫码绑定微信，即可通过微信与 ${displayName} 交互。`,
                                    `Scan to bind WeChat for messaging with ${displayName}.`)}
                            </p>
                            {wxDone ? (
                                <div style={{
                                    padding: "16px", textAlign: "center", borderRadius: 8,
                                    background: "rgba(34,197,94,0.08)", border: "1px solid rgba(34,197,94,0.2)",
                                }}>
                                    <div style={{ fontSize: "1.4rem", marginBottom: 4 }}>✅</div>
                                    <div style={{ fontSize: "0.82rem", color: colors.success, fontWeight: 600 }}>
                                        {t("微信已绑定", "WeChat connected")}
                                    </div>
                                </div>
                            ) : wxSkipped ? (
                                <div style={{
                                    padding: "16px", textAlign: "center", borderRadius: 8,
                                    background: "rgba(148,163,184,0.08)", border: "1px solid rgba(148,163,184,0.2)",
                                }}>
                                    <div style={{ fontSize: "1.4rem", marginBottom: 4 }}>⏭️</div>
                                    <div style={{ fontSize: "0.82rem", color: colors.textMuted, fontWeight: 600 }}>
                                        {t("已跳过，可稍后在设置中绑定", "Skipped — you can bind later in settings")}
                                    </div>
                                </div>
                            ) : (
                                <div>
                                    {!wxQrUrl && wxStatus !== "error" && (
                                        <button onClick={startWxQR} disabled={wxLoading} style={{
                                            ...wizardPrimaryButtonStyle, cursor: wxLoading ? "default" : "pointer",
                                        }}>
                                            {wxLoading ? t("获取中...", "Loading...") : t("显示二维码", "Show QR Code")}
                                        </button>
                                    )}
                                    {wxQrUrl && wxStatus !== "expired" && wxStatus !== "error" && (
                                        <div style={{ textAlign: "center" }}>
                                            <QRCodeSVG value={wxQrUrl} size={200}
                                                bgColor="#ffffff"
                                                fgColor="#000000"
                                                style={{
                                                borderRadius: 8, border: `1px solid ${colors.border}`,
                                                padding: 8, background: "#ffffff",
                                            }} />
                                            <div style={{ marginTop: 8 }}>
                                                <button onClick={startWxQR} disabled={wxLoading} style={{
                                                    fontSize: "0.72rem", padding: "4px 14px",
                                                    background: "transparent", color: colors.primary,
                                                    border: `1px solid ${colors.border}`, borderRadius: 4,
                                                    cursor: wxLoading ? "default" : "pointer",
                                                    opacity: wxLoading ? 0.5 : 1,
                                                }}>
                                                    🔄 {t("刷新二维码", "Refresh QR Code")}
                                                </button>
                                            </div>
                                        </div>
                                    )}
                                    {(wxStatus === "expired" || wxStatus === "error") && (
                                        <button onClick={startWxQR} disabled={wxLoading} style={{
                                            ...wizardPrimaryButtonStyle, cursor: wxLoading ? "default" : "pointer",
                                        }}>
                                            {t("刷新二维码", "Refresh QR Code")}
                                        </button>
                                    )}
                                    {wxMsg && (
                                        <div style={{
                                            marginTop: 8, padding: "6px 10px", borderRadius: 4, fontSize: "0.74rem",
                                            textAlign: "center",
                                            color: wxStatus === "error" || wxStatus === "expired" ? colors.danger : wxStatus === "scaned" ? colors.warning : colors.textMuted,
                                        }}>
                                            {wxMsg}
                                        </div>
                                    )}
                                </div>
                            )}
                        </div>
                    )}
                </div>

                {/* ── Navigation bar ── */}
                <div style={{
                    display: "flex", justifyContent: "space-between", alignItems: "center",
                    padding: "14px 20px 16px", borderTop: `1px solid ${colors.border}`, flexShrink: 0,
                }}>
                    <button
                        onClick={() => setStep(s => getPrevStep(s))}
                        disabled={!canPrev}
                        style={{
                            ...wizardGhostButtonStyle,
                            color: canPrev ? colors.textSecondary : colors.textMuted,
                            cursor: canPrev ? "pointer" : "default",
                            opacity: canPrev ? 1 : 0.55,
                        }}
                    >
                        {t("上一步", "Back")}
                    </button>

                    <span style={{ fontSize: "0.72rem", color: colors.textMuted, fontWeight: 500 }}>
                        {step} / {totalSteps}
                    </span>

                    {isLastStep ? (
                        <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
                            {isCurrentOnboardingStep(onboardingFlow, step, 'wechat') && !wxDone && !wxSkipped && (
                                <button
                                    onClick={() => {
                                        setWxSkipped(true);
                                    }}
                                    style={{
                                        ...wizardGhostButtonStyle, padding: "8px 14px",
                                        fontSize: "0.75rem", color: colors.textMuted,
                                    }}
                                >
                                    {t("跳过", "Skip")}
                                </button>
                            )}
                            <button
                                onClick={() => {
                                    onSaveField({ onboarding_done: true });
                                    onClose();
                                }}
                                disabled={!lastStepCompleted}
                                style={{
                                    ...(lastStepCompleted ? wizardSuccessButtonStyle : wizardDisabledButtonStyle),
                                    width: "auto", padding: "8px 22px", fontSize: "0.8rem",
                                    cursor: lastStepCompleted ? "pointer" : "default",
                                }}
                            >
                                {t("完成", "Finish")}
                            </button>
                        </div>
                    ) : (
                        <button
                            onClick={() => setStep(s => getNextStep(s))}
                            disabled={!canNext}
                            style={{
                                ...(canNext ? wizardPrimaryButtonStyle : wizardDisabledButtonStyle),
                                width: "auto", padding: "8px 22px", fontSize: "0.8rem",
                                cursor: canNext ? "pointer" : "default",
                            }}
                        >
                            {t("下一步", "Next")}
                        </button>
                    )}
                </div>
            </div>

            {showOfflineModeNotice && (
                <OfflineModeNoticeDialog
                    t={t}
                    onClose={() => setShowOfflineModeNotice(false)}
                    onBackToOnline={() => { handleOfflineModeToggle(false); setShowOfflineModeNotice(false); }}
                />
            )}

            {/* ── Confirmation dialog ── */}
            {showConfirm && (
                <div style={{
                    position: "fixed", top: 0, left: 0, right: 0, bottom: 0,
                    background: "rgba(0,0,0,0.35)", display: "flex",
                    alignItems: "center", justifyContent: "center", zIndex: 10000,
                }} onClick={() => setShowConfirm(false)}>
                    <div style={{
                        background: colors.surface, borderRadius: 16, padding: "24px 28px",
                        maxWidth: 400, width: "90%", boxShadow: "0 16px 40px rgba(0,0,0,0.18)",
                    }} onClick={e => e.stopPropagation()}>
                        <div style={{ fontSize: 16, fontWeight: 700, marginBottom: 12 }}>
                            {t("确认注册信息", "Confirm Registration")}
                        </div>
                        <div style={{ fontSize: 14, color: colors.textSecondary, lineHeight: 1.6, marginBottom: 8 }}>
                            {t("请确认邮箱正确无误。填写错误会导致注册失败，且需要管理员手动处理。",
                                "Please confirm the email below is correct. Errors require admin intervention.")}
                        </div>
                        {freeTrial && (
                            <div style={{
                                fontSize: 13, color: colors.warning, lineHeight: 1.5, marginBottom: 8,
                                padding: "8px 10px", borderRadius: 8,
                                background: "rgba(245,158,11,0.10)", border: "1px solid rgba(245,158,11,0.25)",
                            }}>
                                {t("只有填写正确邮箱并完成邮件确认后，才可以获得剩余赠送额度。",
                                    "Only a correct email address and email confirmation can unlock the remaining bonus credits.",
                                    "只有填寫正確信箱並完成郵件確認後，才可以獲得剩餘贈送額度。")}
                            </div>
                        )}
                        <div style={{
                            padding: 14, margin: "12px 0", borderRadius: 10,
                            background: "var(--theme-info-bg)", fontSize: "0.88rem", lineHeight: 1.8,
                        }}>
                            <div>
                                <span style={{ color: colors.textSecondary }}>{t("邮箱", "Email")}:</span>{" "}
                                <span style={{ fontWeight: 600, color: colors.text }}>{regEmail}</span>
                            </div>
                        </div>
                        <div style={{ display: "flex", gap: 10, justifyContent: "flex-end", marginTop: 16 }}>
                            <button onClick={() => setShowConfirm(false)} style={{
                                ...wizardGhostButtonStyle, padding: "8px 18px", fontSize: "0.8rem", color: colors.text,
                            }}>
                                {t("返回修改", "Go Back")}
                            </button>
                            <button onClick={doRegister} style={{
                                ...wizardPrimaryButtonStyle, width: "auto", padding: "8px 18px", fontSize: "0.8rem",
                            }}>
                                {t("确认注册", "Confirm & Register")}
                            </button>
                        </div>
                    </div>
                </div>
            )}
        </div>,
        document.body,
    );
}

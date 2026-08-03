import { Fragment, useCallback, useEffect, useId, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { colors, radius } from './styles';
import { QRCodeSVG } from 'qrcode.react';
import { ActivateRemote, ActivateRemoteEmail, ActivateRemoteSMS, CancelCodeGenSSOPolling, CancelOpenAIOAuth, CancelXAIOAuth, FetchCodeGenModels, GetHubLLMServiceStatus, GetMaclawLLMProviders, GetRemoteConnectionStatus, GetRemoteRegistrationAuth, GetUserDataMigrationJob, GetWeixinStatus, PollWeixinQRStatus, ProbeRemoteHub, RedeemHubLLMService, ResolveRemoteRegistrationTarget, ResolveRemoteRegistrationTargetWithInvitation, SaveCodeGenModelChoice, SaveMaclawLLMProviders, SendRemoteRegistrationEmail, SendRemoteRegistrationSMS, StartCodeGenSSO, StartCodeGenSSOEmbedded, StartOpenAIOAuth, StartUserDataMigrationImport, StartWeixinQRLogin, StartXAIOAuth, TestMaclawLLM, UserDataMigrationInstances, UserDataMigrationStatus, WaitCodeGenSSOResult } from '../../../wailsjs/go/main/App';
import { corelib } from '../../../wailsjs/go/models';
import { PROVIDER_LOGOS } from "./providerLogos";
import { localizeHubServiceReason, localizeHubServiceRedeemError } from "../../utils/hubServiceI18n";
import { HubRegisterButtonContent } from "./HubConnectionStatus";
import { OnboardingOfflineModeOption } from "./OnboardingOfflineModeOption";
import { OfflineModeNoticeDialog } from "./OfflineModeNoticeDialog";
import { stripLeadingEmojiCluster } from "../ai/aiAssistantProgressUtils";
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
import { canSubmitEmailVerification, emailVerificationCooldownSeconds, normalizeEmailVerificationTarget, sanitizeEmailVerificationCode } from "./emailVerification";
import {
    completeOnboardingAfterMigration,
    findOnboardingMigrationPackage,
    isMigrationJobRunning,
    migrationErrorMessage,
    migrationJobId,
    migrationJobStatus,
    migrationProgressPercent,
    optimisticMigrationRunningJob,
    pollUntilMigrationJobTerminal,
    shouldShowMigrationPassword,
    type OnboardingMigrationPackage,
} from "./onboardingMigration";

function extractDailySMSLimit(message: string): number | null {
    const match = message.match(/max\s+(\d+)\s+per\s+day/i);
    if (!match) return null;
    const limit = Number(match[1]);
    return Number.isFinite(limit) && limit > 0 ? limit : null;
}

export function OnboardingWizard({ lang, hubUrl, email, brandId, brandDisplayName, onClose, onLLMConfigured, onRegistered, onMigrationCompleted, onOnboardingCompleted, onSaveField }: Props) {
    const t = useCallback((zh: string, en: string, zhHant: string = zh) => localizeText(lang, en, zh, zhHant), [lang]);
    const hubT = useCallback((en: string, zhHans: string, zhHant?: string) => localizeText(lang, en, zhHans, zhHant ?? zhHans), [lang]);
    const registrationIdentityInputId = useId();
    const registrationPhoneInputId = useId();
    const registrationSMSCodeInputId = useId();
    const onboardingDiagnostic = useCallback((stage: string, fields: Record<string, unknown> = {}, level: "info" | "warn" | "error" = "info") => {
        const payload = { tag: "onboarding", stage, level, ts: Date.now(), ...fields };
        if (level === "error") console.error("[onboarding]", stage, fields);
        else if (level === "warn") console.warn("[onboarding]", stage, fields);
        else console.info("[onboarding]", stage, fields);
        try {
            const logDiagnostic = (window as any).go?.main?.App?.LogFrontendDiagnostic;
            if (typeof logDiagnostic === "function") {
                void Promise.resolve(logDiagnostic(payload)).catch(() => {});
            }
        } catch {
            // Diagnostics must never affect onboarding.
        }
    }, []);

    // Track the app-level theme mode so the portal (rendered outside #App) can
    // inherit dark-mode CSS variables. Uses MutationObserver for live updates.
    const [portalTheme, setPortalTheme] = useState<'light' | 'dark'>(() => {
        return (document.getElementById("App") as HTMLElement)?.dataset?.aiTheme as 'light' | 'dark' || "light";
    });
    const [portalDarkScheme, setPortalDarkScheme] = useState<string | undefined>(() => {
        return (document.getElementById("App") as HTMLElement)?.dataset?.aiDarkScheme || undefined;
    });
    const [portalLightScheme, setPortalLightScheme] = useState<string | undefined>(() => {
        return (document.getElementById("App") as HTMLElement)?.dataset?.aiLightScheme || undefined;
    });
    useEffect(() => {
        const appEl = document.getElementById("App");
        if (!appEl) return;
        const sync = () => {
            setPortalTheme((appEl.dataset.aiTheme as 'light' | 'dark') || "light");
            setPortalDarkScheme(appEl.dataset.aiDarkScheme || undefined);
            setPortalLightScheme(appEl.dataset.aiLightScheme || undefined);
        };
        sync();
        const observer = new MutationObserver(sync);
        observer.observe(appEl, { attributes: true, attributeFilter: ["data-ai-theme", "data-ai-dark-scheme", "data-ai-light-scheme"] });
        return () => observer.disconnect();
    }, []);

    // 品牌显示名称（动态替换硬编码的 "MaClaw"）
    const displayName = brandDisplayName || 'MaClaw';

    const [step, setStep] = useState(1);
    const [regEmail, setRegEmail] = useState(email || "");
    const [registrationStage, setRegistrationStage] = useState<"identity" | "details">("identity");
    const [registrationAuthMethod, setRegistrationAuthMethod] = useState<"email" | "phone" | "mixed" | null>(() => hubUrl ? null : "email");
    // In mixed mode, choose the verification channel once the identity is
    // confirmed. Do not derive it from the editable field on later renders.
    const [registrationIdentityKind, setRegistrationIdentityKind] = useState<"email" | "phone" | null>(null);
    const [registrationAuthError, setRegistrationAuthError] = useState("");
    const [registrationHubUrl, setRegistrationHubUrl] = useState(hubUrl || "");
    const [registrationHubID, setRegistrationHubID] = useState("");
    const [registrationTenantID, setRegistrationTenantID] = useState("");
    const [registrationTargetResolving, setRegistrationTargetResolving] = useState(false);
    const [regPhone, setRegPhone] = useState("");
    const [smsCode, setSmsCode] = useState("");
    const [smsSending, setSmsSending] = useState(false);
    const [smsCountdown, setSmsCountdown] = useState(0);
    const [smsCodeLength, setSmsCodeLength] = useState(6);
    const [smsTargetPhone, setSmsTargetPhone] = useState("");
    const [smsPurpose, setSmsPurpose] = useState<"registration" | "verify_bound_phone">("registration");
    const registrationTargetVersionRef = useRef(0);
    const [invCode, setInvCode] = useState("");
    const [invRequired, setInvRequired] = useState(false);
    const [invError, setInvError] = useState("");
    const [showConfirm, setShowConfirm] = useState(false);
    const [emailCode, setEmailCode] = useState("");
    const [emailCodeLength, setEmailCodeLength] = useState(6);
    const [emailCodeSending, setEmailCodeSending] = useState(false);
    const [emailCodeCountdown, setEmailCodeCountdown] = useState(0);
    const [emailCodeTarget, setEmailCodeTarget] = useState("");
    const [emailCodeError, setEmailCodeError] = useState("");
    const emailCodeInputRef = useRef<HTMLInputElement>(null);
    const emailCodeRequestRef = useRef(0);
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
    const [migrationPackage, setMigrationPackage] = useState<OnboardingMigrationPackage | null>(null);
    const [migrationDecisionPending, setMigrationDecisionPending] = useState(false);
    const [migrationPassword, setMigrationPassword] = useState("");
    const [migrationJob, setMigrationJob] = useState<Record<string, any> | null>(null);
    const [migrationError, setMigrationError] = useState("");
    const [migrationStarting, setMigrationStarting] = useState(false);
    const [migrationPromptDismissed, setMigrationPromptDismissed] = useState(false);
    const migrationPollRef = useRef(0);
    const migrationStartingRef = useRef(false);
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
    const normalizeSMSPhone = useCallback((value: string) => value.replace(/\D/g, ""), []);
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
        const grants = ((status?.credit_grants?.length ? status.credit_grants : status?.active_grants) || [])
            .filter(grant => String(grant.source || "").trim().toLowerCase() !== "hubcenter_compute");
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

    useEffect(() => {
        if (regDone) return;
        if (!hubUrl) {
            setRegistrationHubUrl("");
            setRegistrationAuthMethod("email");
            return;
        }
        setRegistrationHubUrl(hubUrl);
        let cancelled = false;
        const authProbeVersion = registrationTargetVersionRef.current;
        GetRemoteRegistrationAuth(hubUrl, '').then((cfg: any) => {
            if (cancelled || authProbeVersion !== registrationTargetVersionRef.current) return;
            const rawMethod = String(cfg?.method || "email").toLowerCase();
            const method = rawMethod === "phone" || rawMethod === "mixed" ? rawMethod : "email";
            setRegistrationAuthMethod(method);
            setRegistrationAuthError("");
            const nextLength = Number(cfg?.code_length || 6);
            setSmsCodeLength(Number.isFinite(nextLength) && nextLength >= 4 && nextLength <= 8 ? nextLength : 6);
        }).catch(() => {
            if (!cancelled && authProbeVersion === registrationTargetVersionRef.current) {
                setRegistrationAuthMethod(null);
                setRegistrationAuthError(t("无法读取 Hub 注册方式，请稍后重试", "Unable to load the Hub registration method. Try again shortly."));
            }
        });
        return () => { cancelled = true; };
    }, [hubUrl, regDone, t]);

    useEffect(() => {
        if ((registrationAuthMethod !== "phone" && registrationAuthMethod !== "mixed") || regPhone.trim()) return;
        const seededPhone = normalizeSMSPhone(regEmail || email || "");
        if (seededPhone.length >= 6) setRegPhone(seededPhone);
    }, [email, normalizeSMSPhone, regEmail, regPhone, registrationAuthMethod]);

    useEffect(() => {
        if (smsCountdown <= 0) return;
        const timer = window.setInterval(() => {
            setSmsCountdown(prev => Math.max(0, prev - 1));
        }, 1000);
        return () => window.clearInterval(timer);
    }, [smsCountdown]);

    useEffect(() => {
        if (emailCodeCountdown <= 0) return;
        const timer = window.setInterval(() => setEmailCodeCountdown(prev => Math.max(0, prev - 1)), 1000);
        return () => window.clearInterval(timer);
    }, [emailCodeCountdown]);

    useEffect(() => {
        if (showConfirm) window.setTimeout(() => emailCodeInputRef.current?.focus(), 0);
    }, [showConfirm]);

    useEffect(() => () => { migrationPollRef.current += 1; }, []);

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
                if (migrationPackage && !migrationPromptDismissed) return;
                if (showOfflineModeNotice) {
                    setShowOfflineModeNotice(false);
                    return;
                }
                if (showConfirm) {
                    if (!regBusy) setShowConfirm(false);
                    return;
                }
                onClose();
            }
        };
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [migrationPackage, migrationPromptDismissed, onClose, regBusy, showConfirm, showOfflineModeNotice]);

    useEffect(() => {
        const allDone = isOnboardingComplete(onboardingFlow, { regDone: effectiveRegDone, llmDone, wxCompleted });
        if (allDone && !migrationPackage && !migrationDecisionPending) {
            onSaveField({ onboarding_done: true });
            const timer = setTimeout(onClose, 1500);
            return () => clearTimeout(timer);
        }
    }, [onboardingFlow, effectiveRegDone, llmDone, wxCompleted, migrationDecisionPending, migrationPackage, onClose, onSaveField]);

    const selectedProvider = selectedIdx !== null ? providers[selectedIdx] : null;
    const migrationStatus = migrationJobStatus(migrationJob?.status);
    const migrationImportSucceeded = migrationStatus === "succeeded";
    const migrationJobFailed = migrationStatus === "failed";
    const migrationJobRunning = isMigrationJobRunning(migrationJob?.status, migrationStarting);
    const showMigrationPassword = shouldShowMigrationPassword(migrationJob);
    const regResultWarning = regResult?.tone === "warning";
    const normalizedRegPhone = normalizeSMSPhone(regPhone);
    const trimmedRegistrationIdentity = regEmail.trim();
    const registrationIdentityDigits = normalizeSMSPhone(trimmedRegistrationIdentity);
    const registrationIdentityLooksPhone = registrationIdentityDigits.length >= 6 && /^[+\d\s().-]+$/.test(trimmedRegistrationIdentity);
    const isValidSMSPhone = normalizedRegPhone.length >= 6;
    const smsActionDisabled = smsSending || smsCountdown > 0 || regBusy || regDone || !isValidSMSPhone;
    const registrationUsesPhone = registrationAuthMethod === "phone" || (registrationAuthMethod === "mixed" && registrationIdentityKind === "phone");
    const smsCodeReady = !registrationUsesPhone || (smsTargetPhone === normalizedRegPhone && smsCode.trim().length >= smsCodeLength);
    const registerActionDisabled = regBusy || regDone || registrationAuthMethod === null || (registrationUsesPhone && !smsCodeReady);

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

    const returnToRegistrationIdentity = useCallback(() => {
        registrationTargetVersionRef.current += 1;
        emailCodeRequestRef.current += 1;
        setRegistrationStage("identity");
        setRegistrationIdentityKind(null);
        setRegResult(null);
        setRegistrationTargetResolving(false);
        setSmsCode("");
        setSmsTargetPhone("");
        setSmsPurpose("registration");
        setEmailCode("");
        setEmailCodeTarget("");
        setEmailCodeError("");
        setShowConfirm(false);
        setRedeemCode("");
    }, []);

    const handleRegistrationIdentityContinue = useCallback(async () => {
        if (registrationTargetResolving) return;
        if (!trimmedRegistrationIdentity) {
            setRegResult({ ok: false, msg: t("请输入用户ID", "Please enter user ID") });
            return;
        }
        setRegResult(null);
        setRegistrationTargetResolving(true);
        const targetVersion = registrationTargetVersionRef.current + 1;
        registrationTargetVersionRef.current = targetVersion;
        try {
            const target = await ResolveRemoteRegistrationTarget(trimmedRegistrationIdentity) as any;
            if (targetVersion !== registrationTargetVersionRef.current) return;
            const targetHubURL = String(target?.hub_url || target?.HubURL || "").trim();
            const targetHubID = String(target?.hub_id || target?.HubID || "").trim();
            const targetTenantID = String(target?.tenant_id || target?.TenantID || "").trim();
            const rawTargetMethod = String(target?.method || target?.Method || "email").toLowerCase();
            const targetMethod = rawTargetMethod === "phone" || rawTargetMethod === "mixed" ? rawTargetMethod : "email";
            const nextIdentityKind = registrationIdentityLooksPhone ? "phone" : "email";
            if (targetHubURL) setRegistrationHubUrl(targetHubURL);
            setRegistrationHubID(targetHubID);
            setRegistrationTenantID(targetTenantID);
            setRegistrationAuthMethod(targetMethod);
            setRegistrationIdentityKind(nextIdentityKind);
            setRegistrationAuthError("");
            const nextLength = Number(target?.code_length || target?.CodeLength || 0);
            if (Number.isFinite(nextLength) && nextLength > 0) setSmsCodeLength(nextLength);
            if (targetMethod === "phone" && !registrationIdentityLooksPhone) {
                setRegResult({ ok: false, msg: t("该 Hub 仅支持手机号注册或登录，请输入手机号后继续", "This Hub accepts phone registration and sign-in only. Enter a phone number to continue.") });
                return;
            }
            if (targetMethod === "phone" || (targetMethod === "mixed" && registrationIdentityLooksPhone)) {
                setRegPhone(registrationIdentityDigits);
                setSmsCode("");
                setSmsTargetPhone("");
                setSmsPurpose("registration");
            } else if (registrationIdentityLooksPhone) {
                setRegResult({ ok: false, msg: t("路由已命中该租户，但该租户当前验证方式是邮箱注册，请在 Hub 租户系统设置中切换为手机号注册后再继续", "The route matched this tenant, but its current verification method is email registration. Switch this Hub tenant to phone registration in System Settings, then continue.") });
                return;
            }
            setRegistrationStage("details");
        } catch (e) {
            if (targetVersion !== registrationTargetVersionRef.current) return;
            const errMsg = String(e || "");
            const authConfigFailed = /registration auth config|Hub registration method/i.test(errMsg);
            setRegResult({
                ok: false,
                msg: authConfigFailed
                    ? t("已找到可用 Hub，但无法确认该租户的注册验证方式。请检查 Hub 租户系统设置和 /api/enroll/registration-auth 接口后重试。", "A Hub route was found, but the tenant registration verification method could not be confirmed. Check the Hub tenant System Settings and /api/enroll/registration-auth, then try again.")
                    : errMsg,
            });
        } finally {
            if (targetVersion === registrationTargetVersionRef.current) setRegistrationTargetResolving(false);
        }
    }, [registrationIdentityDigits, registrationIdentityLooksPhone, registrationTargetResolving, t, trimmedRegistrationIdentity]);

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
                await SaveMaclawLLMProviders(providers as corelib.MaclawLLMProvider[], sp.name);
                setLlmResult({ ok: true, msg: t("已保存", "Saved") });
                setLlmDone(true);
                onLLMConfigured();
            } else {
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
                const visionProbeInconclusive = testResult.vision_probe_status === "inconclusive";
                const nextProviders = providers.map((provider, index) => index === selectedIdx
                    ? {
                        ...provider,
                        // Do not replace a confirmed capability with a transient
                        // probe failure while completing first-run setup.
                        supports_vision: visionProbeInconclusive ? provider.supports_vision : testResult.supports_vision,
                    }
                    : { ...provider });
                await SaveMaclawLLMProviders(nextProviders as corelib.MaclawLLMProvider[], sp.name);

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

                const visionMsg = visionProbeInconclusive
                    ? t("图片理解：未确认，请稍后在设置中重试", "Vision support: not confirmed; retry later in Settings")
                    : testResult.supports_vision
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
        if (!selectedProvider) return;
        setOauthBusy(true);
        setLlmResult(null);
        try {
            const msg = selectedProvider.name === "xAI-Grok"
                ? await StartXAIOAuth()
                : await StartOpenAIOAuth();
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
    const localizeRegistrationSMSError = useCallback((error: unknown): string => {
        const errMsg = String(error || "");
        if (errMsg.includes("SMS_DAILY_LIMIT_REACHED")) {
            const limit = extractDailySMSLimit(errMsg);
            return limit
                ? t(`今日短信验证码次数已达上限（每天最多 ${limit} 次），请明天再试或联系管理员。`, `Daily SMS verification limit reached (max ${limit} per day). Try again tomorrow or contact an administrator.`)
                : t("今日短信验证码次数已达上限，请明天再试或联系管理员。", "Daily SMS verification limit reached. Try again tomorrow or contact an administrator.");
        }
        if (errMsg.includes("PHONE_ALREADY_REGISTERED")) {
            return t("该手机号已注册，请不要重复注册。", "This phone number is already registered.");
        }
        if (errMsg.includes("INVALID_PHONE_NUMBER") || errMsg.includes("valid phone number is required")) {
            return t("请输入有效手机号", "Please enter a valid phone number");
        }
        if (errMsg.includes("SMS_PROVIDER_NOT_CONFIGURED")) {
            return t("短信验证服务尚未配置，请联系管理员。", "SMS verification is not configured. Contact an administrator.");
        }
        if (errMsg.includes("SMS_SEND_FAILED")) {
            return t("短信验证码发送失败，请稍后重试。", "Failed to send SMS verification code. Please try again later.");
        }
        if (errMsg.includes("INVALID_SMS_VERIFY_CODE")) {
            return t("短信验证码不正确，请重新输入。", "The SMS verification code is incorrect. Please enter it again.");
        }
        if (errMsg.includes("INVALID_SMS_VERIFY_REQUEST")) {
            return t("短信验证码格式不正确，请检查位数。", "The SMS verification code format is invalid. Check the code length.");
        }
        return errMsg;
    }, [t]);

    const handleSendSMSCode = async () => {
        const normalizedPhone = normalizeSMSPhone(regPhone);
        if (normalizedPhone.length < 6) {
            setRegResult({ ok: false, msg: t("请输入有效手机号", "Please enter a valid phone number") });
            return;
        }
        if (smsSending || smsCountdown > 0) return;
        setSmsSending(true);
        setRegResult(null);
        try {
            const smsResult = await SendRemoteRegistrationSMS(registrationHubUrl || hubUrl, normalizedPhone, registrationTenantID) as any;
            const nextCodeLength = Number(smsResult?.code_length || smsResult?.CodeLength || 0);
            const nextPurpose = String(smsResult?.purpose || smsResult?.Purpose || "registration") === "verify_bound_phone" ? "verify_bound_phone" : "registration";
            if (Number.isFinite(nextCodeLength) && nextCodeLength > 0) {
                setSmsCodeLength(nextCodeLength);
                setSmsCode(prev => prev.replace(/\D/g, "").slice(0, nextCodeLength));
            }
            setSmsTargetPhone(normalizedPhone);
            setSmsPurpose(nextPurpose);
            setSmsCountdown(60);
            setRegResult({ ok: true, msg: t("验证码已发送，请查收短信", "Verification code sent. Please check your SMS.") });
        } catch (e) {
            const errMsg = String(e);
            if (/SMS_|PHONE_ALREADY_REGISTERED|INVALID_PHONE_NUMBER/.test(errMsg)) {
                setRegResult({ ok: false, msg: localizeRegistrationSMSError(e) });
            } else {
                setRegResult({ ok: false, msg: errMsg });
            }
        } finally {
            setSmsSending(false);
        }
    };

    const handleRegisterClick = () => {
        if (registrationUsesPhone) {
            const normalizedPhone = normalizeSMSPhone(regPhone);
            if (normalizedPhone.length < 6) {
                setRegResult({ ok: false, msg: t("请输入有效手机号", "Please enter a valid phone number") });
                return;
            }
            if (smsTargetPhone !== normalizedPhone) {
                setRegResult({ ok: false, msg: t("请先发送当前手机号的短信验证码", "Send a verification code to this phone number first.") });
                return;
            }
            if (!smsCode.trim()) {
                setRegResult({ ok: false, msg: t("请输入短信验证码", "Please enter SMS verification code") });
                return;
            }
            doRegister();
            return;
        }
        if (!regEmail.trim()) {
            setRegResult({ ok: false, msg: t("请输入邮箱", "Please enter email") });
            return;
        }
        setEmailCode("");
        setEmailCodeError("");
        setShowConfirm(true);
        void handleSendEmailCode();
    };

    const handleSendEmailCode = async () => {
        const target = normalizeEmailVerificationTarget(regEmail);
        if (!target || emailCodeSending || emailCodeCountdown > 0) return;
        const requestID = emailCodeRequestRef.current + 1;
        emailCodeRequestRef.current = requestID;
        setEmailCodeSending(true);
        setEmailCodeTarget("");
        setEmailCode("");
        setEmailCodeError("");
        setRegResult(null);
        try {
            let targetHubURL = registrationHubUrl || hubUrl;
            let targetTenantID = registrationTenantID;
            let targetHubID = registrationHubID;
            if (invCode.trim()) {
                const route = await ResolveRemoteRegistrationTargetWithInvitation(target, invCode.trim().toUpperCase()) as any;
                if (emailCodeRequestRef.current !== requestID) return;
                targetHubURL = String(route?.hub_url || route?.HubURL || targetHubURL).trim();
                targetTenantID = String(route?.tenant_id || route?.TenantID || "").trim();
                targetHubID = String(route?.hub_id || route?.HubID || "").trim();
                setRegistrationHubUrl(targetHubURL);
                setRegistrationTenantID(targetTenantID);
                setRegistrationHubID(targetHubID);
            }
            const result = await SendRemoteRegistrationEmail(targetHubURL, target, targetTenantID) as any;
            if (emailCodeRequestRef.current !== requestID) return;
            const length = Number(result?.code_length || 6);
            setEmailCodeLength(Number.isFinite(length) && length >= 4 && length <= 8 ? length : 6);
            const confirmedTenantID = String(result?.tenant_id || result?.TenantID || targetTenantID).trim();
            if (confirmedTenantID) setRegistrationTenantID(confirmedTenantID);
            setEmailCodeTarget(target);
			setEmailCodeCountdown(emailVerificationCooldownSeconds(result?.resend_cooldown_seconds));
        } catch (e) {
            if (emailCodeRequestRef.current !== requestID) return;
			setEmailCodeError(localizeEmailVerificationError(e));
        } finally {
            if (emailCodeRequestRef.current === requestID) setEmailCodeSending(false);
        }
    };

    const localizeEmailVerificationError = (error: unknown) => {
        const message = String(error || "");
        if (/INVALID_VERIFY_CODE/i.test(message)) return t("验证码不正确或已过期，请重新输入", "The verification code is incorrect or expired.");
        if (/VERIFY_LOCKED/i.test(message)) return t("错误次数过多，请重新获取验证码", "Too many attempts. Request a new code.");
        if (/RATE_LIMITED/i.test(message)) return t("验证码发送过于频繁，请稍后重试", "Please wait before requesting another code.");
        if (/MAIL_NOT_CONFIGURED|MAIL_SEND_FAILED/i.test(message)) return t("Hub 邮件服务不可用，请联系管理员", "Hub email delivery is unavailable. Contact an administrator.");
        if (/INVITATION_CODE_NOT_ROUTED|INVALID_INVITATION_CODE/i.test(message)) return t("邀请码无效或无法找到对应 Hub", "The invitation code is invalid or cannot be routed to a Hub.");
        return message;
    };

    const formatMigrationBytes = (value: number) => {
        if (!Number.isFinite(value) || value <= 0) return "-";
        if (value >= 1024 * 1024 * 1024) return `${(value / (1024 * 1024 * 1024)).toFixed(1)} GB`;
        if (value >= 1024 * 1024) return `${(value / (1024 * 1024)).toFixed(1)} MB`;
        if (value >= 1024) return `${(value / 1024).toFixed(1)} KB`;
        return `${value} B`;
    };

    const formatMigrationTimestamp = (value: string) => {
        const raw = String(value || "").trim();
        if (!raw) return "-";
        const parsed = new Date(raw);
        return Number.isFinite(parsed.getTime()) ? parsed.toLocaleString() : "-";
    };

    const localizeMigrationProgress = (value: unknown) => {
        const raw = String(value || "").trim();
        const lower = raw.toLowerCase();
        if (lower === "claiming migration export") return t("正在认领迁移包", "Claiming migration package");
        if (lower === "downloading encrypted chunks") return t("正在下载加密数据", "Downloading encrypted data");
        if (lower.startsWith("downloaded ")) return t("正在下载迁移数据", raw);
        if (lower === "decrypting and verifying package") return t("正在解密并校验迁移包", "Decrypting and verifying package");
        if (lower === "restoring local memory and knowledge base") return t("正在恢复系统配置、记忆与知识库", "Restoring settings, memory, and knowledge base");
        if (/^local restore temporarily busy; retrying \(\d+\/\d+\)$/i.test(raw)) return t("本地数据暂时被占用，正在自动重试", raw);
        if (lower === "validating restored llm providers") return t("正在验证已恢复的模型配置", "Validating restored model configuration");
        if (lower === "import completed") return t("迁入完成", "Move-in completed");
        return raw || t("正在准备迁入", "Preparing move-in");
    };

    const localizeMigrationError = (error: unknown) => {
        const message = migrationErrorMessage(error);
        const withDetail = (zh: string, en: string) => `${t(zh, en)}\n${t(`详细原因：${message}`, `Details: ${message}`)}`;
        // Order matters: more specific backend messages first.
        if (/password is incorrect|package is corrupted/i.test(message)) {
            return withDetail("迁移密码不正确或数据包已损坏，请重新输入密码后重试", "The migration password is incorrect or the package is corrupted. Re-enter the password and retry.");
        }
        if (/migration password must be at least|must contain letters and numbers/i.test(message)) {
            return withDetail("迁移密码不符合要求，请重新输入", "The migration password does not meet requirements. Try again.");
        }
        if (/another migration job is already running/i.test(message)) {
            return withDetail("已有迁移任务正在执行，请稍后重试", "Another migration task is already running. Try again shortly.");
        }
        if (/encrypted package hash mismatch|downloaded package size mismatch|downloaded migration chunk/i.test(message)) {
            return withDetail("下载的迁移包校验失败，请检查网络后重试", "The downloaded migration package failed validation. Check the network and retry.");
        }
        if (/decrypted package|hash mismatch|integrity/i.test(message)) {
            return withDetail("迁移包校验失败，请在旧设备重新迁出", "The migration package failed validation. Move it out again from the old device.");
        }
        if (/migration job status unavailable|migration job .* not found|migration job id missing/i.test(message)) {
            return withDetail("暂时无法读取迁入进度，请稍后重试", "Unable to read move-in progress right now. Try again shortly.");
        }
        if (/restore local migration data/i.test(message)) {
            return withDetail("恢复本地数据失败。这通常不是网络问题，请根据下方原因处理后重试", "Restoring local data failed. This is usually not a network issue; resolve the detail below and retry.");
        }
        return withDetail("迁入未完成", "Move-in did not complete.");
    };

    const continueExistingOnboarding = () => {
        migrationPollRef.current += 1;
        migrationStartingRef.current = false;
        setMigrationStarting(false);
        setMigrationPromptDismissed(true);
        setMigrationDecisionPending(false);
        setMigrationPackage(null);
        setMigrationPassword("");
        setMigrationJob(null);
        setMigrationError("");
    };

    const checkForMigrationPackage = async (): Promise<boolean> => {
        const discoveryStartedAt = performance.now();
        onboardingDiagnostic("migration.discovery_started");
        setMigrationDecisionPending(true);
        setMigrationError("");
        const [statusResult, instancesResult] = await Promise.allSettled([
            UserDataMigrationStatus(),
            UserDataMigrationInstances(),
        ]);
        if (statusResult.status === "rejected" && instancesResult.status === "rejected") {
            onboardingDiagnostic("migration.discovery_failed", { elapsed_ms: Math.round(performance.now() - discoveryStartedAt), status_error: String(statusResult.reason), instances_error: String(instancesResult.reason) }, "warn");
            setMigrationDecisionPending(false);
            return false;
        }
        try {
            const status = statusResult.status === "fulfilled" ? statusResult.value : {};
            const instances = instancesResult.status === "fulfilled" ? instancesResult.value : {};
            const candidate = findOnboardingMigrationPackage(status, instances);
            if (!candidate) {
                onboardingDiagnostic("migration.discovery_completed", { elapsed_ms: Math.round(performance.now() - discoveryStartedAt), package_found: false });
                setMigrationDecisionPending(false);
                return false;
            }
            const signedInUserID = String((status as any)?.user_id || "").trim();
            const signedInEmail = normalizeEmailVerificationTarget(String((status as any)?.email || ""));
            const manifestUserID = String(candidate.manifest?.user_id || "").trim();
            const manifestEmail = normalizeEmailVerificationTarget(String(candidate.manifest?.email || ""));
            if ((manifestUserID && signedInUserID && manifestUserID !== signedInUserID)
                || (manifestEmail && signedInEmail && manifestEmail !== signedInEmail)) {
                onboardingDiagnostic("migration.package_identity_mismatch", { export_id: candidate.exportId, has_manifest_user_id: !!manifestUserID, has_manifest_email: !!manifestEmail }, "warn");
                setMigrationDecisionPending(false);
                return false;
            }
            setMigrationPackage(candidate);
            setMigrationPromptDismissed(false);
            onboardingDiagnostic("migration.discovery_completed", { elapsed_ms: Math.round(performance.now() - discoveryStartedAt), package_found: true, export_id: candidate.exportId, status: candidate.status, size: candidate.size, source_machine_id: candidate.sourceMachineId });
            return true;
        } catch (error) {
            // Migration discovery is optional. Malformed or transient responses
            // must not block an otherwise successful sign-in.
            onboardingDiagnostic("migration.discovery_response_unusable", { elapsed_ms: Math.round(performance.now() - discoveryStartedAt), error: String(error) }, "warn");
            setMigrationDecisionPending(false);
            return false;
        }
    };

    const finishMigrationOnboarding = async () => {
        await completeOnboardingAfterMigration({
            markComplete: () => onOnboardingCompleted
                ? onOnboardingCompleted()
                : onSaveField({ onboarding_done: true }),
            close: onClose,
            refresh: onMigrationCompleted,
            onRefreshError: error => console.warn("[onboarding] post-migration refresh failed", error),
        });
    };

    const startOnboardingMigration = async () => {
        const completingSuccessfulImport = migrationJobStatus(migrationJob?.status) === "succeeded";
        // migrationStartingRef is the mutex; status===running alone is not enough
        // because we optimistically mark running before Start returns.
        if (!migrationPackage || (!completingSuccessfulImport && !migrationPassword) || migrationStartingRef.current) return;
        if (!completingSuccessfulImport && migrationJobStatus(migrationJob?.status) === "running" && migrationJobId(migrationJob)) {
            // A real backend job is already in flight; avoid starting a second one.
            return;
        }
        migrationStartingRef.current = true;
        setMigrationStarting(true);
        setMigrationError("");
        // Leave the password form immediately on retry so a previous failure
        // does not look like the wizard jumped back mid-flight. Do not keep a
        // previous failed job id — that would look like an active backend job.
        if (!completingSuccessfulImport) {
            setMigrationJob(prev => optimisticMigrationRunningJob(prev));
        }
        const pollID = migrationPollRef.current + 1;
        migrationPollRef.current = pollID;
        let importSucceeded = completingSuccessfulImport;
        const migrationStartedAt = performance.now();
        onboardingDiagnostic("migration.restore_started", { export_id: migrationPackage.exportId, retry_completion_only: completingSuccessfulImport });
        try {
            if (completingSuccessfulImport) {
                await finishMigrationOnboarding();
                return;
            }
            let nextJob = await StartUserDataMigrationImport(migrationPackage.exportId, migrationPassword) as Record<string, any>;
            const jobID = migrationJobId(nextJob);
            onboardingDiagnostic("migration.job_created", { export_id: migrationPackage.exportId, job_id: jobID, status: nextJob?.status });
            if (!jobID) {
                const startError = nextJob?.error != null && String(nextJob.error).trim() !== ""
                    ? migrationErrorMessage(nextJob.error)
                    : "";
                throw new Error(startError || "migration job id missing");
            }
            // Keep the password until success so a post-download failure (wrong
            // password / integrity check) can be retried without retyping.
            setMigrationJob(nextJob);
            nextJob = await pollUntilMigrationJobTerminal(jobID, async (id) => {
                return await GetUserDataMigrationJob(id) as Record<string, any>;
            }, {
                initialJob: nextJob,
                isCancelled: () => migrationPollRef.current !== pollID,
                onUpdate: (job) => {
                    if (migrationPollRef.current === pollID) setMigrationJob(job);
                },
            });
            if (migrationPollRef.current !== pollID) return;
            if (migrationJobStatus(nextJob?.status) !== "succeeded") {
                throw new Error(migrationErrorMessage(nextJob?.error) || "migration failed");
            }
            importSucceeded = true;
            setMigrationPassword("");
            onboardingDiagnostic("migration.restore_succeeded", {
                export_id: migrationPackage.exportId,
                job_id: nextJob?.id,
                elapsed_ms: Math.round(performance.now() - migrationStartedAt),
                cleanup_pending: nextJob?.result?.cleanup_pending === true,
            });
            await finishMigrationOnboarding();
        } catch (error) {
            const detail = migrationErrorMessage(error);
            onboardingDiagnostic("migration.restore_or_completion_failed", {
                export_id: migrationPackage.exportId,
                elapsed_ms: Math.round(performance.now() - migrationStartedAt),
                import_succeeded: importSucceeded,
                error: detail,
            }, "error");
            if (migrationPollRef.current === pollID) {
                if (importSucceeded) {
                    setMigrationError(t(
                        "数据已恢复，但暂时无法保存设置完成状态。请点击“完成设置”重试；不会重复迁入数据。",
                        "Your data was restored, but setup completion could not be saved. Select Finish setup to retry; your data will not be imported again.",
                    ));
                    setMigrationJob(prev => prev
                        ? { ...prev, status: "succeeded", progress: 1 }
                        : { status: "succeeded", progress: 1 });
                } else {
                    setMigrationError(localizeMigrationError(error));
                    setMigrationJob(prev => prev
                        ? { ...prev, status: "failed", error: detail || String(prev.error || "") }
                        : { status: "failed", error: detail });
                }
            }
        } finally {
            if (migrationPollRef.current === pollID) {
                migrationStartingRef.current = false;
                setMigrationStarting(false);
            }
        }
    };

    const doRegister = async () => {
        if (!registrationUsesPhone) {
            const currentTarget = normalizeEmailVerificationTarget(regEmail);
            if (emailCodeTarget !== currentTarget || !canSubmitEmailVerification({ target: emailCodeTarget, code: emailCode, codeLength: emailCodeLength, sending: emailCodeSending, busy: regBusy })) {
                setEmailCodeError(t("请输入邮件中的完整验证码", "Enter the complete verification code from the email."));
                setShowConfirm(true);
                return;
            }
        }
        setShowConfirm(false);
        setRegBusy(true);
        setRegResult(null);
        setInvError("");
        const trimmedRedeemCode = redeemCode.trim();
        if (!registrationUsesPhone) {
            onSaveField({ remote_email: regEmail.trim() });
        }
        try {
            const result = registrationUsesPhone
                ? await ActivateRemoteSMS(registrationHubUrl || hubUrl, normalizeSMSPhone(regPhone), smsCode.trim(), invCode.trim().toUpperCase(), registrationTenantID, registrationHubID)
                : await ActivateRemoteEmail(registrationHubUrl || hubUrl, regEmail.trim(), emailCode.trim(), invCode.trim().toUpperCase(), registrationTenantID, registrationHubID);
            onboardingDiagnostic("registration.activation_succeeded", { method: registrationAuthMethod, tenant_id: registrationTenantID, hub_id: registrationHubID, has_phone_number: !!result?.phone_number, has_email: !!result?.email, vip: !!result?.vip_flag });
            if (!registrationUsesPhone) {
                const phoneNumber = normalizeSMSPhone(String(result?.phone_number || ""));
                if (phoneNumber) onSaveField({ remote_mobile: phoneNumber });
            }
            if (registrationUsesPhone) {
                const fields: Record<string, string> = { remote_mobile: normalizeSMSPhone(regPhone) };
                if (result?.email) fields.remote_email = result.email;
                onSaveField(fields);
            }
            if (result?.vip_flag) setVipFlag(true);
            if (!registrationUsesPhone) setMigrationDecisionPending(true);
            // Activation has already persisted the Hub credentials. Parent-panel
            // refresh is useful, but it must not delay migration discovery or
            // turn a successful sign-in into a blocked onboarding state.
            void Promise.resolve(onRegistered()).catch(error => {
                console.warn("[onboarding] parent registration refresh failed", error);
            });
            const migrationFound = !registrationUsesPhone && await checkForMigrationPackage();
            onboardingDiagnostic("registration.post_activation_route", { method: registrationAuthMethod, migration_found: migrationFound });
            setRegDone(true);
            if (migrationFound) {
                setHubConnecting(false);
                setRegResult({ ok: true, msg: t("邮箱验证成功。发现可恢复的迁出数据", "Email verified. Move-out data is available to restore.") });
                return;
            }
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
                        redeemNote = `\n${t("服务兑换码已激活，LLM 配置步骤已自动跳过", "Service code redeemed. LLM configuration step skipped automatically.", "服務兌換碼已啟用，LLM 配置步驟已自動跳過")}`;
                    }
                } catch (redeemError) {
                    const localizedRedeemError = localizeHubServiceRedeemError(redeemError, lang);
                    redeemNote = `\n${t("服务兑换码兑换失败，请稍后在服务状态中重试：", "Service redeem code failed. You can retry later in service status: ", "服務兌換碼兌換失敗，請稍後在服務狀態中重試：")}${localizedRedeemError}`;
                }
            }
            setHubConnecting(true);
            const isReboundPhoneUser = registrationUsesPhone && (smsPurpose === "verify_bound_phone" || result?.rebound_existing_user);
            const successMessage = isReboundPhoneUser
                ? t("Device binding complete. Phone verified. Connecting to Hub in the background - you can continue.", "Device binding complete. Phone verified. Connecting to Hub in the background - you can continue.")
                : t("注册成功，正在后台连接 Hub，可直接继续下一步", "Registration successful. Connecting to Hub in the background - you can continue.", "註冊成功，正在後台連線 Hub，可直接繼續下一步");
            setRegResult({
                ok: true,
                msg: `${successMessage}${redeemNote}`,
            });
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
            } else if (/REGISTRATION_DISABLED|PHONE_REGISTRATION_REQUIRED|EMAIL_DOMAIN_NOT_ALLOWED/.test(errMsg) && !registrationUsesPhone) {
                setRegResult({
                    ok: false,
                    msg: t(
                        "该租户不接受新的邮箱注册或当前邮箱域名。如果这是已注册邮箱，请确认输入的是原注册邮箱并重试；否则请按租户要求使用手机号或允许的邮箱继续。",
                        "This tenant does not accept new email registrations or this email domain. If this is an existing account, confirm the original email and try again; otherwise continue with a phone number or an allowed email domain.",
                    ),
                });
            } else if (registrationUsesPhone && /REGISTRATION_DISABLED/.test(errMsg)) {
                setRegResult({
                    ok: false,
                    msg: t(
                        "该租户当前不接受新的手机号注册。若这是已有账号，请确认手机号后重试；否则请联系管理员或使用邀请码。",
                        "This tenant does not accept new phone registrations. If this is an existing account, confirm the phone number and try again; otherwise contact an administrator or use an invitation code.",
                    ),
                });
            } else if (registrationUsesPhone && /SMS_|PHONE_ALREADY_REGISTERED|INVALID_PHONE_NUMBER/.test(errMsg)) {
                if (errMsg.includes("INVALID_SMS_VERIFY_CODE")) setSmsCode("");
                setRegResult({ ok: false, msg: localizeRegistrationSMSError(e) });
            } else if (!registrationUsesPhone && /INVALID_VERIFY_CODE|VERIFY_LOCKED/.test(errMsg)) {
                if (errMsg.includes("VERIFY_LOCKED")) setEmailCodeCountdown(0);
                setEmailCode("");
                setEmailCodeError(localizeEmailVerificationError(e));
                setShowConfirm(true);
            } else if (/MANUAL_BINDING_REQUIRED/.test(errMsg)) {
                setEmailCode("");
                setEmailCodeTarget("");
                setEmailCodeCountdown(0);
                setRegResult({ ok: false, msg: t("此 Hub 需要管理员先完成设备绑定，请联系管理员后重新获取验证码", "This Hub requires an administrator to bind the device first. Contact your administrator, then request a new code.") });
            } else if (/PENDING_APPROVAL/.test(errMsg)) {
                setEmailCode("");
                setEmailCodeTarget("");
                setEmailCodeCountdown(0);
                setRegResult({ ok: false, msg: t("注册申请正在等待管理员审批。审批通过后，请重新获取验证码继续", "Your registration is awaiting administrator approval. Once approved, request a new code to continue.") });
            } else {
                if (!registrationUsesPhone) {
                    setEmailCode("");
                    setEmailCodeTarget("");
                    setEmailCodeCountdown(0);
                }
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
                setWxMsg(res.error);
                setWxStatus("error");
                setWxLoading(false);
                return;
            }
            const qrUrl = res.qrcode_url || "";
            const token = res.qrcode_token || "";
            if (!qrUrl || !token) {
                setWxMsg(t("获取二维码失败，请重试", "Failed to get QR code, please retry"));
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
                            setWxMsg(poll.error);
                        } else {
                            setWxStatus("confirmed");
                            setWxMsg(poll.message || t("微信绑定成功", "WeChat connected"));
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
                            setWxMsg(poll.error);
                            setTimeout(doPoll, 2000);
                            return;
                        }
                        setWxStatus("error");
                        setWxMsg(poll.error);
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
            setWxMsg(String(e));
            setWxStatus("error");
            setWxLoading(false);
        }
    };

    // ── Step labels (memoized) ──
    const labels = useMemo(() => getOnboardingStepLabels(onboardingFlow, lang), [onboardingFlow, lang]);
    const showRegistrationToast = !!regResult && !showConfirm && !showOfflineModeNotice && (isCurrentOnboardingStep(onboardingFlow, step, 'register') || isCurrentOnboardingStep(onboardingFlow, step, 'mode'));
    const registrationToastTitle = regResultWarning ? t("需要处理", "Action needed", "需要處理") : regResult?.ok ? t("注册完成", "Registration complete", "註冊完成") : t("注册失败", "Registration failed", "註冊失敗");
    const registrationToastDetail = (() => {
        if (!regResult) return "";
        if (!regResult.ok || regResultWarning) return regResult.msg;
        // Strip legacy leading pictographs (shared helper — no emoji literals in source).
        if (/Device binding complete|Phone verified/i.test(regResult.msg)) return stripLeadingEmojiCluster(regResult.msg);
        const extraNote = regResult.msg.split("\n").slice(1).map(item => item.trim()).filter(Boolean).join(" ");
        if (extraNote) return stripLeadingEmojiCluster(extraNote);
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
        }} data-ai-theme={portalTheme} data-ai-dark-scheme={portalDarkScheme} data-ai-light-scheme={portalLightScheme}>
            {showRegistrationToast && regResult && (
                <div style={{ position: "absolute", top: 116, left: "50%", transform: "translateX(-50%)", zIndex: 20001, width: "min(420px, calc(100vw - 36px))", maxHeight: "calc(100vh - 148px)", overflowY: "auto", boxSizing: "border-box" }} role={regResult.ok ? "status" : "alert"} aria-live="polite">
                    <div style={{ ...wizardBannerStyle(regResultWarning ? "warning" : regResult.ok ? "success" : "error"), marginTop: 0, background: "var(--theme-surface)", border: `1px solid ${regResultWarning ? colors.primary : regResult.ok ? colors.success : colors.danger}`, boxShadow: "0 14px 36px rgba(15,23,42,0.24)" }}>
                        <div style={{ display: "flex", alignItems: "flex-start", gap: 10 }}>
                            <span aria-hidden="true" style={{ flexShrink: 0, fontSize: "1rem", lineHeight: 1.35 }}>
                                {regResultWarning ? "WARN" : regResult.ok ? "OK" : "ERR"}
                            </span>
                            <div style={{ minWidth: 0 }}>
                                <div style={{ color: colors.textPrimary, fontSize: "0.82rem", fontWeight: 800, lineHeight: 1.35 }}>
                                    {registrationToastTitle}
                                    {regResult.ok && !regResultWarning && (
                                        <span style={srOnlyStyle}>{t("注册成功", "Registration successful", "註冊成功")}</span>
                                    )}
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
                background: "var(--theme-surface)", borderRadius: 16, width: "min(460px, calc(100vw - 32px))", maxHeight: "90vh",
                overflowY: "auto", boxShadow: "0 16px 48px rgba(15,23,42,0.22)",
                border: "1px solid var(--theme-border)", display: "flex", flexDirection: "column",
            }}>
                <div style={{
                    background: "var(--theme-info-bg, #f3f7fb)",
                    padding: "20px 22px 18px", position: "relative", flexShrink: 0,
                    borderBottom: "1px solid var(--theme-border)",
                }}>
                    <button aria-label={t("关闭", "Close")} onClick={() => {
                        if (!migrationPackage || migrationPromptDismissed) onClose();
                    }} disabled={!!migrationPackage && !migrationPromptDismissed} style={{
                        position: "absolute", top: 12, right: 14, border: "none",
                        background: "transparent", cursor: "pointer", fontSize: "1.25rem",
                        color: colors.textMuted, lineHeight: 1,
                        opacity: migrationPackage && !migrationPromptDismissed ? 0.45 : 1,
                    }}>X</button>
                    <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
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
                                        {skippedStep ? "—" : done ? "OK" : s}
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
                                    ? t("RUN 等待浏览器授权...", "RUN Waiting for browser auth...")
                                    : regBusy
                                        ? t("RUN 注册中...", "RUN Registering...")
                                        : (llmDone && regDone)
                                            ? t("认证并注册完成", "Authenticated & Registered")
                                            : t("企业 SSO 登录", "Enterprise SSO Login")}
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
                                        {t("在浏览器中打开", "Open in Browser")}
                                    </button>
                                    <button onClick={handleEmbeddedSSOLogin} style={{
                                        ...wizardGhostButtonBlockStyle, color: colors.primaryDark,
                                    }}>
                                        {t("重试", "Retry")}
                                    </button>
                                </div>
                            )}
                            {ssoResult && (
                                <div style={wizardBannerStyle(ssoResult.ok ? "success" : "error")}>
                                    {ssoResult.msg}
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
                                        {t("选择使用的模型（可选）", "Select Models (optional)")}
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
                                                    AI {t(`${displayName} Agent 模型`, `${displayName} Agent Model`)}
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
                                                    {t("TigerClaw Code 模型", "TigerClaw Code Model")}
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
                                                    ? t("模型已保存", "Models Saved")
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
                            {registrationStage === "identity" && !offlineMode ? (
                                <div style={{ maxWidth: 396, margin: "0 auto" }}>
                                    <p style={{ margin: "0 0 12px 0", fontSize: "0.76rem", color: colors.textSecondary, lineHeight: 1.45, textAlign: "left" }}>
                                        {t("先输入用户ID，系统会根据邮箱或手机号进入对应的注册验证流程。", "First enter your user ID. The system will route email or phone IDs to the right verification flow.")}
                                    </p>
                                    <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 10 }}>
                                        <label htmlFor={registrationIdentityInputId} style={{ ...labelStyle, marginBottom: 0, flex: "0 0 112px", whiteSpace: "nowrap", textAlign: "left" }}>{t("用户ID", "User ID")} <span aria-hidden="true" style={{ color: colors.danger }}>*</span></label>
                                        <input id={registrationIdentityInputId} required aria-required="true" style={{ ...inputStyle, flex: 1, minWidth: 0 }} value={regEmail}
                                            onChange={e => { registrationTargetVersionRef.current += 1; setRegistrationTargetResolving(false); setRegEmail(e.target.value); setRegResult(null); setRedeemCode(""); }}
                                            placeholder={t("邮箱或手机号", "Email or phone")} autoComplete="username" spellCheck={false} />
                                    </div>
                                    <button onClick={handleRegistrationIdentityContinue} disabled={registrationTargetResolving || !trimmedRegistrationIdentity} style={{
                                        ...(registrationTargetResolving || !trimmedRegistrationIdentity ? wizardDisabledButtonStyle : wizardPrimaryButtonStyle),
                                        padding: "10px 0", fontSize: "0.8rem",
                                        cursor: registrationTargetResolving || !trimmedRegistrationIdentity ? "default" : "pointer",
                                    }}>
                                        {registrationTargetResolving ? t("检查中...", "Checking...") : t("继续", "Continue")}
                                    </button>
                                </div>
                            ) : (
                                <>
                                    <p style={{ margin: "0 0 10px 0", fontSize: "0.76rem", color: colors.textSecondary, lineHeight: 1.4 }}>
                                        {offlineMode
                                            ? t("选择离网模式后，将跳过 Hub 注册并进入 LLM 配置。", "Offline mode skips Hub registration and continues to LLM setup.", "選擇離網模式後，將跳過 Hub 註冊並進入 LLM 配置。")
                                            : registrationUsesPhone
                                                ? t("使用手机号短信验证并注册到 Hub 后即可使用所有功能。", "Verify your phone number and register it to the Hub to unlock all features.", "完成手機號簡訊驗證並註冊到 Hub 後即可使用所有功能。")
                                                : t("使用真实用户ID注册到 Hub 后即可使用所有功能。", "Register your real user ID to the Hub to unlock all features.", "使用真實使用者ID註冊到 Hub 後即可使用所有功能。")}
                                    </p>
                                    {registrationStage === "details" && !regDone && (
                                        <button type="button" onClick={returnToRegistrationIdentity} style={{ ...wizardGhostButtonStyle, padding: "6px 10px", marginBottom: 10, fontSize: "0.74rem" }}>
                                            {t("编辑", "Edit")}
                                        </button>
                                    )}
                                    <OnboardingOfflineModeOption
                                        offlineMode={offlineMode}
                                        freeTrial={freeTrial}
                                        onToggle={handleOfflineModeToggle}
                                        onFreeTrialChange={handleFreeTrialChange}
                                        t={t}
                                    />
                                    {!offlineMode && !regDone && (
                                        <>
                                            {registrationUsesPhone ? (
                                                <>
                                                    <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 10 }}>
                                                        <label htmlFor={registrationPhoneInputId} style={{ ...labelStyle, marginBottom: 0, flex: "0 0 112px", whiteSpace: "nowrap" }}>{t("手机号", "Phone")} <span aria-hidden="true" style={{ color: colors.danger }}>*</span></label>
                                                        <div style={{ display: "flex", flex: 1, minWidth: 0, gap: 8 }}>
                                                            <input id={registrationPhoneInputId} style={{ ...inputStyle, flex: 1, minWidth: 0 }} value={regPhone}
                                                                readOnly={registrationStage === "details"}
                                                                onChange={e => { if (registrationStage !== "details") { setRegPhone(e.target.value); setSmsCode(""); setRegResult(null); } }}
                                                                placeholder="13800138000" inputMode="tel" spellCheck={false} />
                                                            <button onClick={handleSendSMSCode} disabled={smsActionDisabled} style={{
                                                                ...wizardPrimaryButtonStyle,
                                                                flex: "0 0 108px",
                                                                padding: "0 10px",
                                                                fontSize: "0.76rem",
                                                                opacity: smsActionDisabled ? 0.72 : 1,
                                                                cursor: smsActionDisabled ? "default" : "pointer",
                                                            }}>
                                                                {smsCountdown > 0 ? String(smsCountdown) + "s" : smsSending ? t("发送中", "Sending") : t("验证码", "Code")}
                                                            </button>
                                                        </div>
                                                    </div>
                                                    <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 10 }}>
                                                        <label htmlFor={registrationSMSCodeInputId} style={{ ...labelStyle, marginBottom: 0, flex: "0 0 112px", whiteSpace: "nowrap" }}>{t("短信验证码", "SMS Code")} <span aria-hidden="true" style={{ color: colors.danger }}>*</span></label>
                                                        <input id={registrationSMSCodeInputId} style={{ ...inputStyle, flex: 1, minWidth: 0 }} value={smsCode}
                                                            onChange={e => setSmsCode(e.target.value.replace(/\D/g, "").slice(0, smsCodeLength))}
                                                            placeholder={t("请输入 " + smsCodeLength + " 位验证码", "Enter " + smsCodeLength + "-digit code")}
                                                            inputMode="numeric" spellCheck={false} />
                                                    </div>
                                                </>
                                            ) : (
                                                <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 10 }}>
                                                    <label htmlFor={registrationIdentityInputId} style={{ ...labelStyle, marginBottom: 0, flex: "0 0 112px", whiteSpace: "nowrap" }}>{t("用户ID", "User ID")} <span aria-hidden="true" style={{ color: colors.danger }}>*</span></label>
                                                    <input id={registrationIdentityInputId} style={{ ...inputStyle, flex: 1, minWidth: 0 }} value={regEmail}
                                                        readOnly={registrationStage === "details"}
                                                        placeholder={t("邮箱或手机号", "Email or phone")} spellCheck={false} />
                                                </div>
                                            )}
                                            <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 10 }}>
                                                <label style={{ ...labelStyle, marginBottom: 0, flex: "0 0 112px", whiteSpace: "nowrap" }}>
                                                    {t("邀请码", "Invitation Code")} {" "}
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
                                            <button onClick={handleRegisterClick} disabled={registerActionDisabled} style={{
                                                ...((regDone && !hubConnecting) ? wizardSuccessButtonStyle : registerActionDisabled ? wizardDisabledButtonStyle : wizardPrimaryButtonStyle),
                                                padding: "10px 0", fontSize: "0.8rem",
                                                cursor: registerActionDisabled ? "default" : "pointer",
                                            }}>
                                                <HubRegisterButtonContent regBusy={regBusy} regDone={regDone} hubConnecting={hubConnecting} t={hubT} />
                                            </button>
                                        </>
                                    )}
                                    {!offlineMode && regDone && (
                                        <button type="button" disabled style={{
                                            ...wizardSuccessButtonStyle,
                                            padding: "10px 0",
                                            fontSize: "0.8rem",
                                            cursor: "default",
                                        }}>
                                            <HubRegisterButtonContent regBusy={regBusy} regDone={regDone} hubConnecting={hubConnecting} t={hubT} />
                                        </button>
                                    )}
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
                                                {selectedProvider.name === "xAI-Grok"
                                                    ? t("点击下方按钮，将在浏览器中完成 xAI 账号授权。",
                                                        "Click below to authorize with your xAI account in the browser.")
                                                    : t("点击下方按钮，将在浏览器中完成 OpenAI 账号授权。",
                                                        "Click below to authorize with your OpenAI account in the browser.")}
                                            </p>
                                            <button onClick={handleOAuthLogin} disabled={oauthBusy} style={{
                                                ...wizardPrimaryButtonStyle, cursor: oauthBusy ? "default" : "pointer",
                                            }}>
                                                {oauthBusy
                                                    ? t("等待浏览器授权...", "Waiting for browser auth...")
                                                    : selectedProvider.name === "xAI-Grok"
                                                        ? t("使用 xAI 账号登录", "Sign in with xAI")
                                                        : t("使用 OpenAI 账号登录", "Sign in with OpenAI")}
                                            </button>
                                            {oauthBusy && (
                                                <button onClick={() => {
                                                    if (selectedProvider.name === "xAI-Grok") CancelXAIOAuth();
                                                    else CancelOpenAIOAuth();
                                                    setOauthBusy(false);
                                                }} style={{
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
                                                    placeholder={selectedProvider.is_custom ? "sk-..." : ((selectedProvider.name === "智谱编程") ? "xxxxxxxx.yyyyyyyy" : "sk-...")}
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
                                            {llmResult.ok ? `${t("连接成功，已保存", "Connected & saved")}\n${llmResult.msg}` : llmResult.msg}
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
                                    background: colors.successBg, border: `1px solid color-mix(in srgb, ${colors.success} 30%, transparent)`,
                                }}>
                                    <div style={{ fontSize: "0.72rem", marginBottom: 4, fontWeight: 700 }}>OK</div>
                                    <div style={{ fontSize: "0.82rem", color: colors.success, fontWeight: 600 }}>
                                        {t("微信已绑定", "WeChat connected")}
                                    </div>
                                </div>
                            ) : wxSkipped ? (
                                <div style={{
                                    padding: "16px", textAlign: "center", borderRadius: 8,
                                    background: "rgba(148,163,184,0.08)", border: "1px solid rgba(148,163,184,0.2)",
                                }}>
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
                                                    {t("刷新二维码", "Refresh QR Code")}
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
                                            color: wxStatus === "error" || wxStatus === "expired" ? colors.danger : wxStatus === "scaned" ? colors.primaryDark : colors.textMuted,
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

            {migrationPackage && !migrationPromptDismissed && (
                <div style={{
                    position: "fixed", inset: 0, background: colors.overlay,
                    display: "flex", alignItems: "center", justifyContent: "center", zIndex: 10010,
                }}>
                    <div role="dialog" aria-modal="true" aria-labelledby="onboarding-migration-title" style={{
                        background: colors.surface, borderRadius: 14, padding: "24px 26px",
                        maxWidth: 460, width: "calc(100% - 32px)", color: colors.text,
                        boxShadow: "0 12px 36px rgba(15,23,42,0.18)", boxSizing: "border-box",
                    }}>
                        <div id="onboarding-migration-title" style={{ fontSize: 17, fontWeight: 700, marginBottom: 7 }}>
                            {t("发现可恢复的数据", "Data from another device is available")}
                        </div>
                        <p style={{ margin: "0 0 16px", fontSize: 13, lineHeight: 1.55, color: colors.textSecondary }}>
                            {t("是否将旧设备的系统配置、模型配置、记忆、知识库和附件迁入当前设备？迁入成功后将直接完成设置。", "Restore settings, model configuration, memory, knowledge base, and attachments from your old device? Successful restore completes setup immediately.")}
                        </p>
                        <dl style={{
                            margin: "0 0 14px",
                            padding: "11px 13px",
                            borderRadius: 8,
                            background: "var(--theme-info-bg)",
                            border: `1px solid ${colors.borderLight}`,
                            display: "grid",
                            gridTemplateColumns: "max-content minmax(0, 1fr)",
                            columnGap: 16,
                            rowGap: 6,
                            alignItems: "baseline",
                            fontSize: 12,
                            lineHeight: 1.45,
                            color: colors.textSecondary,
                            textAlign: "left",
                        }}>
                            {([
                                {
                                    label: t("来源设备", "Source device"),
                                    value: migrationPackage.sourceMachineName || migrationPackage.sourceMachineId || "-",
                                },
                                {
                                    label: t("迁出时间", "Move-out time"),
                                    value: formatMigrationTimestamp(migrationPackage.updatedAt),
                                },
                                {
                                    label: t("数据大小", "Package size"),
                                    value: formatMigrationBytes(migrationPackage.size),
                                },
                            ] as const).map((row) => (
                                <Fragment key={row.label}>
                                    <dt style={{ margin: 0, color: colors.text, fontWeight: 600 }}>{row.label}</dt>
                                    <dd style={{ margin: 0, minWidth: 0, overflowWrap: "anywhere" }}>{row.value}</dd>
                                </Fragment>
                            ))}
                        </dl>

                        {showMigrationPassword ? (
                            <label style={{ ...labelStyle, display: "block", marginBottom: 12 }}>
                                <span style={{ display: "block", marginBottom: 6 }}>{t("迁出密码", "Move-out password")}</span>
                                <input
                                    type="password"
                                    autoComplete="current-password"
                                    autoFocus
                                    value={migrationPassword}
                                    onChange={event => { setMigrationPassword(event.target.value); setMigrationError(""); }}
                                    onKeyDown={event => { if (event.key === "Enter" && migrationPassword && !migrationJobRunning) void startOnboardingMigration(); }}
                                    placeholder={t("输入旧设备迁出时设置的密码", "Enter the password set on the old device")}
                                    style={inputStyle}
                                    disabled={migrationJobRunning}
                                />
                            </label>
                        ) : null}

                        {migrationJob ? (
                            <div aria-live="polite" style={{ marginBottom: 13 }}>
                                <div style={{ display: "flex", justifyContent: "space-between", fontSize: 12, color: migrationJobFailed ? colors.danger : colors.textSecondary, marginBottom: 6 }}>
                                    <span>
                                        {migrationJobFailed
                                            ? t("迁入失败", "Move-in failed")
                                            : localizeMigrationProgress(migrationJob.progress_text)}
                                    </span>
                                    <span>{migrationProgressPercent(migrationJob.progress)}%</span>
                                </div>
                                <div role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={migrationProgressPercent(migrationJob.progress)} style={{ height: 6, borderRadius: 999, background: colors.borderLight, overflow: "hidden" }}>
                                    <div style={{
                                        width: `${migrationProgressPercent(migrationJob.progress)}%`,
                                        height: "100%",
                                        background: migrationJobFailed ? colors.danger : colors.primary,
                                        transition: "width 180ms ease-out",
                                    }} />
                                </div>
                            </div>
                        ) : null}

                        {migrationError ? (
                            <div role="alert" style={{ ...wizardBannerStyle("error"), marginBottom: 12, whiteSpace: "pre-wrap" }}>{migrationError}</div>
                        ) : null}

                        <div style={{ display: "flex", justifyContent: "flex-end", gap: 10 }}>
                            <button type="button" onClick={continueExistingOnboarding} disabled={migrationJobRunning || migrationImportSucceeded} style={{
                                ...wizardGhostButtonStyle, padding: "8px 15px", fontSize: "0.8rem",
                            }}>
                                {t("暂不恢复，继续设置", "Not now, continue setup")}
                            </button>
                            <button type="button" onClick={() => void startOnboardingMigration()} disabled={(!migrationPassword && !migrationImportSucceeded) || migrationJobRunning} style={{
                                ...((!migrationPassword && !migrationImportSucceeded) || migrationJobRunning ? wizardDisabledButtonStyle : wizardPrimaryButtonStyle),
                                width: "auto", padding: "8px 16px", fontSize: "0.8rem",
                            }}>
                                {migrationJobRunning
                                    ? t("正在迁入...", "Restoring...")
                                    : migrationImportSucceeded
                                        ? t("完成设置", "Finish setup")
                                        : migrationJobFailed
                                            ? t("重试迁入", "Retry restore")
                                            : t("立即恢复", "Restore now")}
                            </button>
                        </div>
                    </div>
                </div>
            )}

            {/* ── Email verification dialog ── */}
            {showConfirm && (
                <div style={{
                    position: "fixed", top: 0, left: 0, right: 0, bottom: 0,
                    background: colors.overlay, display: "flex",
                    alignItems: "center", justifyContent: "center", zIndex: 10000,
                }} onClick={() => { if (!regBusy) setShowConfirm(false); }}>
                    <div style={{
                        background: colors.surface, borderRadius: 14, padding: "22px 26px",
                        maxWidth: 380, width: "90%", boxShadow: "0 12px 36px rgba(15,23,42,0.18)",
                        color: colors.text,
                    }} role="dialog" aria-modal="true" aria-labelledby="email-verification-title" onClick={e => e.stopPropagation()}>
                        <div id="email-verification-title" style={{ fontSize: 16, fontWeight: 700, marginBottom: 6 }}>
                            {t("验证邮箱", "Verify your email")}
                        </div>
                        <p style={{ margin: "0 0 14px", fontSize: 13, color: colors.textSecondary, lineHeight: 1.5 }} aria-live="polite">
                            {emailCodeSending
								? t(`正在向 ${regEmail.trim()} 发送验证码…`, `Sending a verification code to ${regEmail.trim()}…`)
								: t(`验证码已发送至 ${regEmail.trim()}，请输入邮件中的 ${emailCodeLength} 位数字。`, `We sent a code to ${regEmail.trim()}. Enter the ${emailCodeLength}-digit code from the email.`)}
                        </p>
                        <div style={{
                            display: "flex", gap: 8, alignItems: "stretch", marginBottom: 8,
                        }}>
                            <input ref={emailCodeInputRef} aria-label={t("邮箱验证码", "Email verification code")}
                                style={{ ...inputStyle, flex: 1, minWidth: 0, fontSize: 20, fontWeight: 700, letterSpacing: "0.22em", textAlign: "center" }}
                                value={emailCode}
                                onChange={e => setEmailCode(sanitizeEmailVerificationCode(e.target.value, emailCodeLength))}
                                onKeyDown={e => { if (e.key === "Enter" && emailCodeTarget === normalizeEmailVerificationTarget(regEmail) && canSubmitEmailVerification({ target: emailCodeTarget, code: emailCode, codeLength: emailCodeLength, sending: emailCodeSending, busy: regBusy })) void doRegister(); }}
                                placeholder={"•".repeat(emailCodeLength)} inputMode="numeric" autoComplete="one-time-code" disabled={regBusy} />
                            <button type="button" onClick={handleSendEmailCode} disabled={emailCodeSending || emailCodeCountdown > 0 || regBusy} style={{
                                ...wizardGhostButtonStyle, flex: "0 0 104px", padding: "0 10px", fontSize: "0.74rem",
                                opacity: emailCodeSending || emailCodeCountdown > 0 || regBusy ? 0.62 : 1,
                            }}>
                                {emailCodeSending ? t("发送中", "Sending") : emailCodeCountdown > 0 ? `${emailCodeCountdown}s` : t("重新发送", "Resend")}
                            </button>
                        </div>
                        {emailCodeError && (
                            <div role="alert" style={{ ...wizardBannerStyle("error"), marginTop: 8, marginBottom: 0 }}>
                                {emailCodeError}
                            </div>
                        )}
                        <div style={{ fontSize: 12, color: colors.textMuted, lineHeight: 1.45 }}>
                            {t("首次登录会自动创建账号；后续登录同样需要验证。", "First-time sign-in creates the account automatically. Returning sign-ins are verified too.")}
                        </div>
                        <div style={{ display: "flex", gap: 10, justifyContent: "flex-end", marginTop: 14 }}>
                            <button onClick={() => setShowConfirm(false)} disabled={regBusy} style={{
                                ...wizardGhostButtonStyle, padding: "8px 18px", fontSize: "0.8rem", color: colors.text,
                            }}>
                                {t("返回修改", "Go Back")}
                            </button>
                            <button onClick={doRegister} disabled={emailCodeTarget !== normalizeEmailVerificationTarget(regEmail) || !canSubmitEmailVerification({ target: emailCodeTarget, code: emailCode, codeLength: emailCodeLength, sending: emailCodeSending, busy: regBusy })} style={{
                                ...(emailCodeTarget !== normalizeEmailVerificationTarget(regEmail) || !canSubmitEmailVerification({ target: emailCodeTarget, code: emailCode, codeLength: emailCodeLength, sending: emailCodeSending, busy: regBusy }) ? wizardDisabledButtonStyle : wizardPrimaryButtonStyle),
                                width: "auto", padding: "8px 18px", fontSize: "0.8rem",
                            }}>
                                {regBusy ? t("验证中...", "Verifying...") : t("验证并继续", "Verify & continue")}
                            </button>
                        </div>
                    </div>
                </div>
            )}
        </div>,
        document.body,
    );
}

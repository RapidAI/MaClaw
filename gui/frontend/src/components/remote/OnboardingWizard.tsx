import { useState, useCallback, useEffect, useRef, useMemo } from "react";
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
import { HubRegisterButtonContent, HubStatusBadge } from "./HubConnectionStatus";

interface HubLLMServiceStatus {
    active?: boolean;
    skip_llm_config?: boolean;
}

interface LLMProvider {
    name: string;
    url: string;
    key: string;
    model: string;
    protocol?: string;
    context_length?: number;
    is_custom?: boolean;
    auth_type?: string;
    agent_type?: string;
    supports_vision?: boolean;
}

type Props = {
    lang: string;
    hubUrl: string;
    email: string;
    uiMode: string;
    brandId?: string;
    brandDisplayName?: string;
    onClose: () => void;
    onLLMConfigured: () => void;
    onRegistered: () => void;
    onSaveField: (patch: Record<string, any>) => void;
};

/* ── Hoisted style objects ── */
const inputStyle: React.CSSProperties = {
    width: "100%", padding: "7px 10px", fontSize: "0.8rem",
    border: `1px solid var(--theme-border)`, borderRadius: 4,
    background: "var(--theme-surface)", color: "var(--theme-text-primary)", boxSizing: "border-box",
};
const readonlyInputStyle: React.CSSProperties = {
    ...inputStyle, background: "var(--theme-surface-muted)", color: "var(--theme-text-muted)", cursor: "default",
};
const labelStyle: React.CSSProperties = {
    fontSize: "0.76rem", color: "var(--theme-text-muted)", marginBottom: 4, display: "block",
};

const localizeText = (lang: string | undefined, en: string, zhHans: string, zhHant: string = zhHans) => (
    lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en
);

// TigerClaw 品牌三步流程：SSO+注册 → 界面选择 → 绑定微信
const TIGERCLAW_TOTAL_STEPS = 3;
const STEP_LABELS = {
    tigerclaw: {
        en: ["SSO Auth", "UI Mode", "WeChat"],
        zhHans: ["企业认证", "界面模式", "绑定微信"],
        zhHant: ["企業認證", "介面模式", "綁定微信"],
    },
    standard: {
        en: ["Register", "UI Mode", "LLM", "WeChat"],
        zhHans: ["邮件注册", "界面模式", "配置 LLM", "绑定微信"],
        zhHant: ["郵件註冊", "介面模式", "配置 LLM", "綁定微信"],
    },
};

const TOTAL_STEPS = 4;

export function OnboardingWizard({ lang, hubUrl, email, uiMode, brandId, brandDisplayName, onClose, onLLMConfigured, onRegistered, onSaveField }: Props) {
    const t = useCallback((zh: string, en: string, zhHant: string = zh) => localizeText(lang, en, zh, zhHant), [lang]);
    const hubT = useCallback((en: string, zhHans: string, zhHant?: string) => localizeText(lang, en, zhHans, zhHant ?? zhHans), [lang]);

    // 是否为 TigerClaw 品牌（oem_qianxin）
    const isTigerclaw = brandId === 'qianxin';

    // 品牌显示名称（动态替换硬编码的 "MaClaw"）
    const displayName = brandDisplayName || 'MaClaw';

    // TigerClaw 两步流程；普通品牌四步流程
    const totalSteps = isTigerclaw ? TIGERCLAW_TOTAL_STEPS : TOTAL_STEPS;

    // ── Wizard step (1-based) ──
    const [step, setStep] = useState(1);

    // ── Step 1: Registration（普通品牌）──
    const [regEmail, setRegEmail] = useState(email || "");
    const [invCode, setInvCode] = useState("");
    const [invRequired, setInvRequired] = useState(false);
    const [invError, setInvError] = useState("");
    const [showConfirm, setShowConfirm] = useState(false);
    const [vipFlag, setVipFlag] = useState(false);
    const [redeemCode, setRedeemCode] = useState("");
    const [regResult, setRegResult] = useState<{ ok: boolean; msg: string } | null>(null);

    // ── SSO（TigerClaw Step 1）+ 注册状态（共用）──
    const [ssoBusy, setSsoBusy] = useState(false);
    const [ssoResult, setSsoResult] = useState<{ ok: boolean; msg: string } | null>(null);
    // regBusy/regDone 在两种流程中均使用：普通品牌=手动注册，tigerclaw=SSO后自动注册
    const [regBusy, setRegBusy] = useState(false);
    const [regDone, setRegDone] = useState(false);
    const [hubConnecting, setHubConnecting] = useState(false);

    // ── 内嵌扫码状态（TigerClaw 品牌）──
    const [qrCodeURL, setQrCodeURL] = useState("");
    const [embeddedSSOLoading, setEmbeddedSSOLoading] = useState(false);
    const [embeddedSSOError, setEmbeddedSSOError] = useState("");

    // ── Step 2: UI Mode（普通品牌 step2；tigerclaw step2）──
    const [selectedMode, setSelectedMode] = useState<'pro' | 'lite'>(uiMode === 'pro' ? 'pro' : 'lite');
    const [modeDone, setModeDone] = useState(!!uiMode && uiMode !== '');

    // 进入 UI Mode 步骤时，如果已有默认选择但尚未标记完成，自动保存并标记
    useEffect(() => {
        if (step === 2 && !modeDone && selectedMode) {
            onSaveField({ ui_mode: selectedMode });
            setModeDone(true);
        }
    }, [step, modeDone, selectedMode, onSaveField]);

    // ── Step 3: LLM（普通品牌 step3；tigerclaw 在 step1 SSO 后自动完成）──
    const [providers, setProviders] = useState<LLMProvider[]>([]);
    const [selectedIdx, setSelectedIdx] = useState<number | null>(null);
    const [llmSaving, setLlmSaving] = useState(false);
    const [llmResult, setLlmResult] = useState<{ ok: boolean; msg: string } | null>(null);
    const [llmDone, setLlmDone] = useState(false);
    const [oauthBusy, setOauthBusy] = useState(false);

    // ── TigerClaw 模型选择（SSO 成功后）──
    const [codegenModels, setCodegenModels] = useState<{ id: string; name: string }[]>([]);
    const [codegenModelsFetching, setCodegenModelsFetching] = useState(false);
    const [maclawModel, setMaclawModel] = useState("");        // MaClaw Agent 使用的模型
    const [claudeCodeModel, setClaudeCodeModel] = useState(""); // TigerClaw Code 使用的模型
    const [modelSaving, setModelSaving] = useState(false);
    const [modelSaved, setModelSaved] = useState(false);

    // ── Step 4: WeChat Binding ──
    const [wxDone, setWxDone] = useState(false);
    const [wxSkipped, setWxSkipped] = useState(false);
    const [wxQrUrl, setWxQrUrl] = useState("");
    const [wxStatus, setWxStatus] = useState("");
    const [wxMsg, setWxMsg] = useState("");
    const [wxLoading, setWxLoading] = useState(false);
    const wxPollingRef = useRef(false);

    // wxDone = actually bound; wxSkipped = user chose to skip
    const wxCompleted = wxDone || wxSkipped;

    // Step completion map (memoized to avoid array re-creation)
    // TigerClaw 三步流程：step1=SSO+注册(llmDone&&regDone), step2=界面模式(modeDone), step3=微信
    // 普通品牌四步：step1=注册, step2=UI模式, step3=LLM, step4=微信
    const stepDone = useMemo(() => {
        if (isTigerclaw) {
            return [false, llmDone && regDone, modeDone, wxCompleted];
        }
        return [false, regDone, modeDone, llmDone, wxCompleted];
    }, [regDone, modeDone, llmDone, wxCompleted, isTigerclaw]);

    // Navigation guards
    const getPrevStep = useCallback((currentStep: number) => {
        if (!isTigerclaw && llmDone && currentStep === 4) return 2;
        return Math.max(1, currentStep - 1);
    }, [isTigerclaw, llmDone]);

    const getNextStep = useCallback((currentStep: number) => {
        if (!isTigerclaw && llmDone && currentStep === 2) return 4;
        return Math.min(totalSteps, currentStep + 1);
    }, [isTigerclaw, llmDone, totalSteps]);

    const canNext = step < totalSteps && stepDone[step];
    const canPrev = step > 1;
    const isLastStep = step === totalSteps;

    const applyHubServiceStatus = useCallback((status?: HubLLMServiceStatus | null) => {
        const shouldSkipLLM = !!status?.active && !!status?.skip_llm_config;
        if (shouldSkipLLM) {
            setLlmDone(true);
            setLlmResult({
                ok: true,
                msg: t("???? MaClaw ???????????? LLM ?????", "MaClaw model service is authorized. The LLM binding step has been skipped automatically."),
            });
            onLLMConfigured();
        }
        return shouldSkipLLM;
    }, [onLLMConfigured, t]);

    // Load providers on mount
    useEffect(() => {
        GetMaclawLLMProviders().then(data => {
            if (data?.providers) setProviders(data.providers);
        }).catch(() => {});
    }, []);

    // Probe hub for invitation code requirement
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
        if (isTigerclaw) return;
        GetHubLLMServiceStatus().then(status => {
            applyHubServiceStatus(status);
        }).catch(() => {});
    }, [applyHubServiceStatus, isTigerclaw]);

    useEffect(() => {
        if (!isTigerclaw && llmDone && step === 3) {
            setStep(4);
        }
    }, [isTigerclaw, llmDone, step]);

    useEffect(() => {
        if (!regDone || !hubConnecting) return;
        let cancelled = false;
        const poll = async () => {
            try {
                const status = await GetRemoteConnectionStatus();
                if (!cancelled && status?.connected) {
                    setHubConnecting(false);
                    setRegResult({
                        ok: true,
                        msg: t("注册成功，Hub 已连接，可直接继续下一步", "Registration successful. Hub connected — you can continue."),
                    });
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
    }, [regDone, hubConnecting, t]);

    // Stop WeChat polling when leaving the WeChat step or unmounting
    const wxStep = isTigerclaw ? 3 : 4;
    useEffect(() => {
        if (step !== wxStep) wxPollingRef.current = false;
        return () => { wxPollingRef.current = false; };
    }, [step, wxStep]);

    // Cancel embedded SSO polling on unmount
    useEffect(() => {
        return () => { CancelCodeGenSSOPolling().catch(() => {}); };
    }, []);

    // Escape key to close (not if confirm dialog open)
    useEffect(() => {
        const onKey = (e: KeyboardEvent) => {
            if (e.key === "Escape") {
                if (!showConfirm) onClose();
            }
        };
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [onClose, showConfirm]);

    // Auto-close when all done
    useEffect(() => {
        const allDone = isTigerclaw ? (llmDone && regDone && modeDone && wxCompleted) : (regDone && modeDone && llmDone && wxCompleted);
        if (allDone) {
            onSaveField({ onboarding_done: true });
            const timer = setTimeout(onClose, 1500);
            return () => clearTimeout(timer);
        }
    }, [regDone, modeDone, llmDone, wxCompleted, isTigerclaw, onClose, onSaveField]);

    const selectedProvider = selectedIdx !== null ? providers[selectedIdx] : null;

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
                const testResult = await TestMaclawLLM({ url: sp.url, key: sp.key, model: sp.model, protocol: sp.protocol || "openai", agent_type: sp.agent_type || "openclaw" });
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
                if (models && models.length > 0) {
                    setMaclawModel(models[0].id);
                    setClaudeCodeModel(models[0].id);
                }
            }).catch(err => {
                console.warn("[TigerClaw] FetchCodeGenModels failed:", err);
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
            const result = await ActivateRemote(regEmail.trim(), invCode.trim(), "");
            if (result?.vip_flag) setVipFlag(true);
            let redeemWarning = "";
            if (trimmedRedeemCode) {
                try {
                    const serviceStatus = await RedeemHubLLMService(trimmedRedeemCode) as HubLLMServiceStatus;
                    applyHubServiceStatus(serviceStatus);
                    setRedeemCode("");
                } catch (redeemError) {
                    redeemWarning = `\n${t("服务兑换码兑换失败，请稍后在服务状态中重试：", "Service redeem code failed. You can retry later in service status: ", "服務兌換碼兌換失敗，請稍後在服務狀態中重試：")}${String(redeemError)}`;
                }
            }
            setHubConnecting(true);
            setRegResult({
                ok: true,
                msg: `${t("注册成功，正在后台连接 Hub，可直接继续下一步", "Registration successful. Connecting to Hub in the background - you can continue.", "註冊成功，正在後台連線 Hub，可直接繼續下一步")}${redeemWarning}`,
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
                    } else if (st === "expired") {
                        setWxStatus("expired");
                        setWxMsg(poll.message || t("二维码已过期，请刷新", "QR expired, please refresh"));
                        wxPollingRef.current = false;
                        return;
                    } else if (poll.error) {
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
    const labels = useMemo(() => {
        const labelSet = isTigerclaw ? STEP_LABELS.tigerclaw : STEP_LABELS.standard;
        return lang === 'zh-Hans'
            ? labelSet.zhHans
            : lang === 'zh-Hant'
                ? labelSet.zhHant
                : labelSet.en;
    }, [lang, isTigerclaw]);

    return (
        <div style={{
            position: "fixed", top: 0, left: 0, right: 0, bottom: 0,
            background: "rgba(0,0,0,0.3)", backdropFilter: "blur(3px)",
            display: "flex", alignItems: "center", justifyContent: "center", zIndex: 9999,
        }}>
            <div style={{
                background: "var(--theme-surface)", borderRadius: 14, width: 440, maxHeight: "90vh",
                overflowY: "auto", boxShadow: "0 8px 24px rgba(99,102,241,0.12)",
                border: "1px solid var(--theme-border)", display: "flex", flexDirection: "column",
            }}>
                {/* ── Header ── */}
                <div style={{
                    background: "linear-gradient(135deg, var(--theme-info-bg, #eef2ff) 0%, var(--theme-primary-soft, #e0e7ff) 100%)",
                    padding: "12px 18px 10px", position: "relative", flexShrink: 0,
                }}>
                    <button onClick={onClose} style={{
                        position: "absolute", top: 8, right: 12, border: "none",
                        background: "transparent", cursor: "pointer", fontSize: "1.1rem", color: colors.textMuted,
                    }}>&times;</button>
                    <div style={{ fontSize: "1.2rem", marginBottom: 2, lineHeight: 1 }}>👋</div>
                    <h3 style={{ margin: 0, color: colors.primary, fontSize: "0.88rem", fontWeight: 600 }}>
                        {t(`来，配置一下 ${displayName} 吧`, `Let's get ${displayName} ready!`)}
                    </h3>
                </div>

                {/* ── Progress bar ── */}
                <div style={{
                    display: "flex", alignItems: "center", justifyContent: "center",
                    gap: 0, padding: "14px 18px 6px", flexShrink: 0,
                }}>
                    {Array.from({ length: totalSteps }, (_, i) => {
                        const s = i + 1;
                        const done = stepDone[s];
                        const active = s === step;
                        // Last step (WeChat) skipped: show grey instead of green
                        const skippedStep = s === totalSteps && wxSkipped && !wxDone;
                        const circleColor = skippedStep ? colors.textMuted : done ? colors.success : active ? colors.primary : "var(--theme-border)";
                        return (
                            <div key={s} style={{ display: "flex", alignItems: "center" }}>
                                <div style={{ display: "flex", flexDirection: "column", alignItems: "center", minWidth: 54 }}>
                                    <div style={{
                                        width: 26, height: 26, borderRadius: "50%",
                                        background: circleColor, color: "var(--theme-text-primary)",
                                        display: "flex", alignItems: "center", justifyContent: "center",
                                        fontSize: "0.72rem", fontWeight: 700,
                                        transition: "background 0.2s",
                                    }}>
                                        {skippedStep ? "—" : done ? "✓" : s}
                                    </div>
                                    <span style={{
                                        fontSize: "0.62rem", marginTop: 3,
                                        color: active ? colors.primary : colors.textMuted,
                                        fontWeight: active ? 600 : 400,
                                    }}>
                                        {labels[i]}
                                    </span>
                                </div>
                                {s < totalSteps && (
                                    <div style={{
                                        width: 32, height: 2, background: stepDone[s] ? colors.success : "var(--theme-border)",
                                        margin: "0 2px", marginBottom: 14, transition: "background 0.2s",
                                    }} />
                                )}
                            </div>
                        );
                    })}
                </div>

                {/* ── Step content ── */}
                <div style={{ padding: "10px 18px 0", flex: 1, overflowY: "auto" }}>

                    <style>{`@keyframes spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }`}</style>

                    {/* ═══ Step 1 ═══
                        TigerClaw 品牌：企业 SSO 认证 + 自动注册 Hub（合并原 Step1+Step2）
                        普通品牌：邮件注册
                    */}
                    {step === 1 && isTigerclaw && (
                        <div>
                            <p style={{ margin: "0 0 10px 0", fontSize: "0.76rem", color: colors.textSecondary, lineHeight: 1.4 }}>
                                {t(
                                    `使用企业账号一键登录，自动配置 ${displayName} 和 TigerClaw Code，并注册到 Hub。`,
                                    `Sign in with your enterprise account to configure ${displayName}, TigerClaw Code, and register to Hub.`
                                )}
                            </p>
                            <button onClick={handleEmbeddedSSOLogin} disabled={ssoBusy || (llmDone && regDone)} style={{
                                width: "100%", padding: "12px 0", fontSize: "0.84rem", fontWeight: 600,
                                background: ssoBusy || regBusy ? colors.primaryLight : (llmDone && regDone) ? colors.successBg : colors.primary,
                                color: colors.onPrimary, border: "none", borderRadius: 6,
                                cursor: (ssoBusy || regBusy || (llmDone && regDone)) ? "default" : "pointer",
                                display: "flex", alignItems: "center", justifyContent: "center", gap: 8,
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
                                        StartCodeGenSSO().then(info => {
                                            setLlmDone(true);
                                            onLLMConfigured();
                                            setSsoResult({ ok: true, msg: info.message });
                                        }).catch(e => setSsoResult({ ok: false, msg: String(e) }));
                                    }} style={{
                                        width: "100%", padding: "8px 0", fontSize: "0.76rem",
                                        background: "var(--theme-surface-muted)", color: colors.primary, border: "1px solid var(--theme-border)",
                                        borderRadius: 6, cursor: "pointer", marginTop: 6,
                                    }}>
                                        {t("🌐 在浏览器中打开", "🌐 Open in Browser")}
                                    </button>
                                    <button onClick={handleEmbeddedSSOLogin} style={{
                                        width: "100%", padding: "8px 0", fontSize: "0.76rem",
                                        background: "var(--theme-surface-muted)", color: colors.primary, border: "1px solid var(--theme-border)",
                                        borderRadius: 6, cursor: "pointer", marginTop: 6,
                                    }}>
                                        {t("🔄 重试", "🔄 Retry")}
                                    </button>
                                </div>
                            )}
                            {ssoResult && (
                                <div style={{
                                    marginTop: 10, padding: "8px 12px", borderRadius: 6, fontSize: "0.74rem",
                                    lineHeight: 1.5, whiteSpace: "pre-wrap", wordBreak: "break-word",
                                    background: ssoResult.ok ? "rgba(34,197,94,0.1)" : "rgba(239,68,68,0.1)",
                                    border: `1px solid ${ssoResult.ok ? "rgba(34,197,94,0.3)" : "rgba(239,68,68,0.3)"}`,
                                    color: ssoResult.ok ? colors.success : colors.danger,
                                }}>
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
                                                        <button key={m.id} onClick={() => setMaclawModel(m.id)} style={{
                                                            fontSize: "0.7rem", padding: "4px 10px", cursor: "pointer",
                                                            background: maclawModel === m.id ? colors.primary : colors.surface,
                                                            color: maclawModel === m.id ? colors.onPrimary : colors.text,
                                                            border: `1px solid ${maclawModel === m.id ? colors.primary : colors.border}`,
                                                            borderRadius: 4, transition: "all 0.12s",
                                                        }}>
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
                                                        <button key={m.id} onClick={() => setClaudeCodeModel(m.id)} style={{
                                                            fontSize: "0.7rem", padding: "4px 10px", cursor: "pointer",
                                                            background: claudeCodeModel === m.id ? "var(--theme-primary-strong)" : colors.surface,
                                                            color: claudeCodeModel === m.id ? colors.onPrimary : colors.text,
                                                            border: `1px solid ${claudeCodeModel === m.id ? "var(--theme-primary-strong)" : colors.border}`,
                                                            borderRadius: 4, transition: "all 0.12s",
                                                        }}>
                                                            {m.name}
                                                        </button>
                                                    ))}
                                                </div>
                                            </div>

                                            {/* 保存按钮 */}
                                            <button onClick={handleModelSave} disabled={modelSaving || modelSaved} style={{
                                                width: "100%", padding: "7px 0", fontSize: "0.76rem", fontWeight: 600,
                                                background: modelSaved ? colors.successBg : modelSaving ? colors.primaryLight : colors.primary,
                                                color: colors.onPrimary, border: "none", borderRadius: 6,
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

                    {step === 1 && !isTigerclaw && (
                        <div>
                            <p style={{ margin: "0 0 10px 0", fontSize: "0.76rem", color: colors.textSecondary, lineHeight: 1.4 }}>
                                {t("注册设备邮箱到 Hub，即可通过移动端操控。",
                                    "Register your email to the Hub for remote control.")}
                            </p>
                            <div style={{ marginBottom: 10 }}>
                                <label style={labelStyle}>{t("邮箱", "Email")} <span style={{ color: colors.danger }}>*</span></label>
                                <input style={inputStyle} value={regEmail}
                                    onChange={e => setRegEmail(e.target.value)}
                                    placeholder="name@example.com" spellCheck={false} />
                            </div>
                            {invRequired && (
                                <div style={{ marginBottom: 10 }}>
                                    <label style={labelStyle}>
                                        {t("邀请码", "Invitation Code")}{" "}
                                        <span style={{ fontSize: "0.68rem", color: colors.textMuted }}>({t("可选", "optional")})</span>
                                    </label>
                                    <input style={{ ...inputStyle, ...(invError ? { borderColor: colors.danger } : {}) }}
                                        value={invCode}
                                        onChange={e => { setInvCode(e.target.value.toUpperCase()); setInvError(""); }}
                                        placeholder={t("请输入邀请码", "Enter invitation code")}
                                        maxLength={10} spellCheck={false} />
                                    {invError && <div style={{ fontSize: "0.72rem", color: colors.danger, marginTop: 4 }}>{invError}</div>}
                                </div>
                            )}
                            <div style={{ marginBottom: 10 }}>
                                <label style={labelStyle}>
                                    {t("服务兑换码", "Service redeem code", "服務兌換碼")} {" "}
                                    <span style={{ fontSize: "0.68rem", color: colors.textMuted }}>({t("可选", "optional", "可選")})</span>
                                </label>
                                <input style={inputStyle}
                                    value={redeemCode}
                                    onChange={e => setRedeemCode(e.target.value.trim().toUpperCase())}
                                    placeholder={t("请输入服务兑换码（可选）", "Enter service redeem code (optional)", "請輸入服務兌換碼（可選）")}
                                    spellCheck={false} />
                            </div>
                            <button onClick={handleRegisterClick} disabled={regBusy || regDone} style={{
                                width: "100%", padding: "8px 0", fontSize: "0.8rem", fontWeight: 600,
                                background: regBusy ? colors.primaryLight : regDone ? (hubConnecting ? "var(--theme-primary-soft)" : colors.successBg) : colors.primary,
                                color: colors.onPrimary, border: "none", borderRadius: 6,
                                cursor: regBusy || regDone ? "default" : "pointer",
                                display: "flex", alignItems: "center", justifyContent: "center", gap: 8,
                            }}>
                                <HubRegisterButtonContent regBusy={regBusy} regDone={regDone} hubConnecting={hubConnecting} t={hubT} />
                            </button>
                            {regResult && (
                                <div style={{
                                    marginTop: 8, padding: "6px 10px", borderRadius: 4, fontSize: "0.74rem",
                                    lineHeight: 1.5, whiteSpace: "pre-wrap", wordBreak: "break-word",
                                    background: regResult.ok ? "rgba(34,197,94,0.1)" : "rgba(239,68,68,0.1)",
                                    border: `1px solid ${regResult.ok ? "rgba(34,197,94,0.3)" : "rgba(239,68,68,0.3)"}`,
                                    color: regResult.ok ? colors.success : colors.danger,
                                }}>
                                    {regResult.ok ? `✅ ${regResult.msg}` : `❌ ${regResult.msg}`}
                                    {regResult.ok && (
                                        <div style={{ marginTop: 6, display: "flex", alignItems: "center", gap: 6, fontSize: "0.68rem", color: hubConnecting ? colors.primary : colors.success }}>
                                            <HubStatusBadge connecting={hubConnecting} t={hubT} />
                                        </div>
                                    )}
                                </div>
                            )}
                        </div>
                    )}

                    {/* ═══ Step 2 ═══
                        TigerClaw 品牌：界面模式选择
                        普通品牌：界面模式选择
                    */}
                    {step === 2 && (
                        <div>
                            <p style={{ margin: "0 0 10px 0", fontSize: "0.76rem", color: colors.textSecondary, lineHeight: 1.4 }}>
                                {t("选择适合你的界面模式。", "Choose the interface mode that suits you.")}
                            </p>
                            <div style={{ display: "flex", gap: 10 }}>
                                <div
                                    onClick={() => { setSelectedMode('pro'); onSaveField({ ui_mode: 'pro' }); setModeDone(true); }}
                                    style={{
                                        flex: 1, padding: "14px 14px", borderRadius: 10, cursor: "pointer",
                                        border: `2px solid ${selectedMode === 'pro' ? colors.primary : colors.border}`,
                                        background: selectedMode === 'pro' ? 'var(--theme-info-bg)' : colors.surfaceMuted,
                                        transition: "all 0.15s",
                                    }}
                                >
                                    <div style={{ fontSize: "0.88rem", fontWeight: 600, color: selectedMode === 'pro' ? colors.primary : colors.text, marginBottom: 4 }}>
                                        🛠️ {t("专业模式", "Pro")}
                                    </div>
                                    <div style={{ fontSize: "0.72rem", color: colors.textMuted, lineHeight: 1.4 }}>
                                        {t("包含完整编程工具链，适合开发者", "Full coding toolchain for developers")}
                                    </div>
                                </div>
                                <div
                                    onClick={() => { setSelectedMode('lite'); onSaveField({ ui_mode: 'lite' }); setModeDone(true); }}
                                    style={{
                                        flex: 1, padding: "14px 14px", borderRadius: 10, cursor: "pointer",
                                        border: `2px solid ${selectedMode === 'lite' ? colors.primary : colors.border}`,
                                        background: selectedMode === 'lite' ? 'var(--theme-info-bg)' : colors.surfaceMuted,
                                        transition: "all 0.15s",
                                    }}
                                >
                                    <div style={{ fontSize: "0.88rem", fontWeight: 600, color: selectedMode === 'lite' ? colors.primary : colors.text, marginBottom: 4 }}>
                                        ✨ {t("简洁模式", "Lite")}
                                    </div>
                                    <div style={{ fontSize: "0.72rem", color: colors.textMuted, lineHeight: 1.4 }}>
                                        {t("专注 AI 助手与技能扩展，隐藏编程工具", "AI assistant & skills, coding tools hidden")}
                                    </div>
                                </div>
                            </div>
                        </div>
                    )}

                    {/* ═══ Step 3 ═══
                        TigerClaw 品牌：绑定微信
                        普通品牌：LLM 配置
                    */}

                    {step === 3 && !isTigerclaw && (
                        <div>
                            <p style={{ margin: "0 0 8px 0", fontSize: "0.76rem", color: colors.textSecondary, lineHeight: 1.4 }}>
                                {t("选择一个 LLM 服务商，输入 API Key 后测试并保存。",
                                    "Pick a provider, enter your API Key, then test & save.")}
                            </p>
                            {/* Provider buttons */}
                            <div style={{ display: "flex", gap: 6, marginBottom: 10, flexWrap: "wrap" }}>
                                {providers.map((p, i) => {
                                    // Skip auth_type "none" providers (free proxy removed)
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
                                                width: "100%", padding: "10px 0", fontSize: "0.82rem", fontWeight: 600,
                                                background: oauthBusy ? colors.primaryLight : colors.primary, color: colors.onPrimary,
                                                border: "none", borderRadius: 6, cursor: oauthBusy ? "default" : "pointer",
                                            }}>
                                                {oauthBusy ? t("等待浏览器授权...", "Waiting for browser auth...") : t("使用 OpenAI 账号登录", "Sign in with OpenAI")}
                                            </button>
                                            {oauthBusy && (
                                                <button onClick={() => { CancelOpenAIOAuth(); setOauthBusy(false); }} style={{
                                                    width: "100%", padding: "8px 0", fontSize: "0.76rem", marginTop: 6,
                                                    cursor: "pointer", background: "transparent", color: colors.textMuted,
                                                    border: `1px solid ${colors.border}`, borderRadius: 6,
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
                                                                    <button key={proto} onClick={() => updateField("protocol", proto)} style={{
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
                                                            {(selectedProvider.protocol || "openai") === "anthropic"
                                                                ? t("使用 Anthropic Messages API（x-api-key 鉴权）", "Uses Anthropic Messages API (x-api-key auth)")
                                                                : t("使用 OpenAI 兼容接口（Bearer Token 鉴权）", "Uses OpenAI-compatible API (Bearer token auth)")}
                                                        </p>
                                                    </div>
                                                    {/* User-Agent selection */}
                                                    <div style={{ marginBottom: 10 }}>
                                                        <label style={labelStyle}>User-Agent</label>
                                                        <div style={{ display: "flex", gap: 6 }}>
                                                            {(["openclaw", "claude-code/2.0.0"] as const).map(ua => {
                                                                const active = (selectedProvider.agent_type || "openclaw") === ua;
                                                                return (
                                                                    <button key={ua} onClick={() => updateField("agent_type", ua)} style={{
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
                                                            {(selectedProvider.agent_type || "openclaw") === "claude-code/2.0.0"
                                                                ? t("Kimi 等需要编程套餐身份的服务商", "For providers requiring Claude Coding Plan identity (e.g. Kimi)")
                                                                : t("智谱龙虾等大多数服务商使用 OpenClaw 身份", "Most providers use OpenClaw identity (e.g. Zhipu Lobster)")}
                                                        </p>
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
                                                width: "100%", padding: "8px 0", fontSize: "0.8rem", fontWeight: 600,
                                                background: llmSaving ? colors.primaryLight : colors.primary, color: colors.onPrimary,
                                                border: "none", borderRadius: 6, cursor: llmSaving ? "default" : "pointer",
                                            }}>
                                                {llmSaving ? t("测试并保存中...", "Testing & Saving...") : t("测试并保存", "Test & Save")}
                                            </button>
                                        </>
                                    )}
                                    {llmResult && (
                                        <div style={{
                                            marginTop: 8, padding: "6px 10px", borderRadius: 4, fontSize: "0.74rem",
                                            lineHeight: 1.5, whiteSpace: "pre-wrap", wordBreak: "break-word",
                                            background: llmResult.ok ? "rgba(34,197,94,0.1)" : "rgba(239,68,68,0.1)",
                                            border: `1px solid ${llmResult.ok ? "rgba(34,197,94,0.3)" : "rgba(239,68,68,0.3)"}`,
                                            color: llmResult.ok ? colors.success : colors.danger,
                                        }}>
                                            {llmResult.ok ? `✅ ${t("连接成功，已保存", "Connected & saved")}\n${llmResult.msg}` : `❌ ${llmResult.msg}`}
                                        </div>
                                    )}
                                </div>
                            )}
                        </div>
                    )}

                    {/* ═══ 微信绑定 ═══
                        TigerClaw: step === 3
                        普通品牌: step === 4
                    */}
                    {((isTigerclaw && step === 3) || (!isTigerclaw && step === 4)) && (
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
                                            width: "100%", padding: "10px 0", fontSize: "0.82rem", fontWeight: 600,
                                            background: wxLoading ? colors.primaryLight : colors.primary, color: colors.onPrimary,
                                            border: "none", borderRadius: 6, cursor: wxLoading ? "default" : "pointer",
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
                                            width: "100%", padding: "10px 0", fontSize: "0.82rem", fontWeight: 600,
                                            background: wxLoading ? colors.primaryLight : colors.primary, color: colors.onPrimary,
                                            border: "none", borderRadius: 6, cursor: wxLoading ? "default" : "pointer",
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
                    padding: "12px 18px 14px", borderTop: `1px solid ${colors.border}`, flexShrink: 0,
                }}>
                    <button
                        onClick={() => setStep(s => getPrevStep(s))}
                        disabled={!canPrev}
                        style={{
                            padding: "7px 20px", fontSize: "0.8rem", borderRadius: 6,
                            background: canPrev ? colors.surfaceMuted : colors.surfaceMuted,
                            color: canPrev ? colors.text : colors.textMuted,
                            border: `1px solid ${canPrev ? colors.border : colors.surfaceMuted}`,
                            cursor: canPrev ? "pointer" : "default",
                            fontWeight: 500,
                        }}
                    >
                        {t("上一步", "Back")}
                    </button>

                    <span style={{ fontSize: "0.7rem", color: colors.textMuted }}>
                        {step} / {totalSteps}
                    </span>

                    {isLastStep ? (
                        <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
                            {!wxDone && !wxSkipped && (
                                <button
                                    onClick={() => {
                                        setWxSkipped(true);
                                    }}
                                    style={{
                                        padding: "7px 14px", fontSize: "0.75rem", fontWeight: 500, borderRadius: 6,
                                        background: colors.primary, color: colors.textMuted, border: `1px solid ${colors.border}`,
                                        cursor: "pointer",
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
                                disabled={!wxCompleted}
                                style={{
                                    padding: "7px 20px", fontSize: "0.8rem", fontWeight: 600, borderRadius: 6,
                                    background: wxCompleted ? colors.success : colors.border,
                                    color: wxCompleted ? colors.onPrimary : colors.text, border: "none",
                                    cursor: wxCompleted ? "pointer" : "default",
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
                                padding: "7px 20px", fontSize: "0.8rem", fontWeight: 600, borderRadius: 6,
                                background: canNext ? colors.primary : colors.border,
                                color: canNext ? colors.onPrimary : colors.text, border: "none",
                                cursor: canNext ? "pointer" : "default",
                            }}
                        >
                            {t("下一步", "Next")}
                        </button>
                    )}
                </div>
            </div>

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
                        <div style={{
                            padding: 14, margin: "12px 0", borderRadius: 10,
                            background: "var(--theme-info-bg)", fontSize: "0.88rem", lineHeight: 1.8,
                        }}>
                            <div>
                                <span style={{ color: colors.textSecondary }}>{t("邮箱", "Email")}:</span>{" "}
                                <span style={{ fontWeight: 600 }}>{regEmail}</span>
                            </div>
                        </div>
                        <div style={{ display: "flex", gap: 10, justifyContent: "flex-end", marginTop: 16 }}>
                            <button onClick={() => setShowConfirm(false)} style={{
                                padding: "6px 18px", fontSize: "0.8rem", borderRadius: 6,
                                background: colors.surfaceMuted, color: colors.text, border: `1px solid ${colors.border}`, cursor: "pointer",
                            }}>
                                {t("返回修改", "Go Back")}
                            </button>
                            <button onClick={doRegister} style={{
                                padding: "6px 18px", fontSize: "0.8rem", fontWeight: 600, borderRadius: 6,
                                background: colors.primary, color: colors.onPrimary, border: "none", cursor: "pointer",
                            }}>
                                {t("确认注册", "Confirm & Register")}
                            </button>
                        </div>
                    </div>
                </div>
            )}
        </div>
    );
}

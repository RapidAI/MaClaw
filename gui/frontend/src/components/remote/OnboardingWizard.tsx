import { useState, useCallback, useEffect, useRef, useMemo } from "react";
import { QRCodeSVG } from "qrcode.react";
import {
    GetMaclawLLMProviders,
    SaveMaclawLLMProviders,
    TestMaclawLLM,
    ActivateRemote,
    ProbeRemoteHub,
    StartOpenAIOAuth,
    StartCodeGenSSO,
    StartCodeGenSSOEmbedded,
    WaitCodeGenSSOResult,
    CancelCodeGenSSOPolling,
    FetchCodeGenModels,
    SaveCodeGenModelChoice,
    GetWeixinStatus,
    StartWeixinQRLogin,
    PollWeixinQRStatus,
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
} from "../../../wailsjs/go/main/App";
import { PROVIDER_LOGOS } from "./providerLogos";

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
    border: "1px solid #e2e8f0", borderRadius: 4,
    background: "#fff", color: "#1e293b", boxSizing: "border-box",
};
const readonlyInputStyle: React.CSSProperties = {
    ...inputStyle, background: "#f1f5f9", color: "#94a3b8", cursor: "default",
};
const labelStyle: React.CSSProperties = {
    fontSize: "0.76rem", color: "#64748b", marginBottom: 4, display: "block",
};

// TigerClaw 品牌三步流程：SSO+注册 → 界面选择 → 绑定微信
const TIGERCLAW_TOTAL_STEPS = 3;
const STEP_LABELS_ZH_TIGERCLAW = ["企业认证", "界面模式", "绑定微信"];
const STEP_LABELS_EN_TIGERCLAW = ["SSO Auth", "UI Mode", "WeChat"];

const TOTAL_STEPS = 4;
const STEP_LABELS_ZH = ["邮件注册", "界面模式", "配置 LLM", "绑定微信"];
const STEP_LABELS_EN = ["Register", "UI Mode", "LLM", "WeChat"];

export function OnboardingWizard({ lang, hubUrl, email, uiMode, brandId, brandDisplayName, onClose, onLLMConfigured, onRegistered, onSaveField }: Props) {
    const t = useCallback((zh: string, en: string) => lang?.startsWith("zh") ? zh : en, [lang]);

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
    const [regResult, setRegResult] = useState<{ ok: boolean; msg: string } | null>(null);

    // ── SSO（TigerClaw Step 1）+ 注册状态（共用）──
    const [ssoBusy, setSsoBusy] = useState(false);
    const [ssoResult, setSsoResult] = useState<{ ok: boolean; msg: string } | null>(null);
    // regBusy/regDone 在两种流程中均使用：普通品牌=手动注册，tigerclaw=SSO后自动注册
    const [regBusy, setRegBusy] = useState(false);
    const [regDone, setRegDone] = useState(false);

    // ── 内嵌扫码状态（TigerClaw 品牌）──
    const [qrCodeURL, setQrCodeURL] = useState("");
    const [embeddedSSOLoading, setEmbeddedSSOLoading] = useState(false);
    const [embeddedSSOError, setEmbeddedSSOError] = useState("");

    // ── Step 2: UI Mode（普通品牌 step2；tigerclaw step2）──
    const [selectedMode, setSelectedMode] = useState<'pro' | 'lite'>(uiMode === 'pro' ? 'pro' : 'lite');
    const [modeDone, setModeDone] = useState(!!uiMode && uiMode !== '');

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

    // ── Free proxy modal ──
    const [freeModalOpen, setFreeModalOpen] = useState(false);
    const [proxyRunning, setProxyRunning] = useState(false);
    const [proxyBusy, setProxyBusy] = useState(false);
    const [browserInfo, setBrowserInfo] = useState<{ found: string; name?: string } | null>(null);
    const [dangbeiLoggedIn, setDangbeiLoggedIn] = useState(false);
    const [loginBusy, setLoginBusy] = useState(false);
    const [browserLaunched, setBrowserLaunched] = useState(false);
    const [authChecking, setAuthChecking] = useState(false);
    const [freeModels, setFreeModels] = useState<{id: string; name: string}[]>([]);
    const [freeSelectedModel, setFreeSelectedModel] = useState("");
    const [freeResult, setFreeResult] = useState<{ ok: boolean; msg: string } | null>(null);

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
    const canNext = step < totalSteps && stepDone[step];
    const canPrev = step > 1;
    const isLastStep = step === totalSteps;

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

    // Escape key to close (not if confirm dialog or free modal open)
    useEffect(() => {
        const onKey = (e: KeyboardEvent) => {
            if (e.key === "Escape") {
                if (freeModalOpen) { setFreeModalOpen(false); return; }
                if (!showConfirm) onClose();
            }
        };
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [onClose, showConfirm, freeModalOpen]);

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

    // ── Free proxy modal: init browser/auth/models when opened ──
    useEffect(() => {
        if (!freeModalOpen) return;
        let cancelled = false;
        DetectBrowser().then((info: any) => { if (!cancelled) setBrowserInfo(info || { found: "false" }); }).catch(() => { if (!cancelled) setBrowserInfo({ found: "false" }); });
        GetFreeProxyModels().then((models: any) => { if (!cancelled) setFreeModels(models || []); }).catch(() => {});
        GetFreeProxyModel().then((m: string) => { if (!cancelled) setFreeSelectedModel(m || "deepseek_r1"); }).catch(() => {});
        setAuthChecking(true);
        DangbeiEnsureAuth().then(async (result: string) => {
            if (cancelled) return;
            const loggedIn = result === "authenticated";
            setDangbeiLoggedIn(loggedIn);
            setAuthChecking(false);
            if (loggedIn) {
                try {
                    const running = await IsFreeProxyRunning();
                    if (!cancelled && !running) { await StartFreeProxy(); setProxyRunning(true); }
                } catch { /* non-fatal */ }
            }
        }).catch(() => { if (!cancelled) { setDangbeiLoggedIn(false); setAuthChecking(false); } });
        return () => { cancelled = true; };
    }, [freeModalOpen]);

    // Poll proxy status while free modal is open
    useEffect(() => {
        if (!freeModalOpen) return;
        let cancelled = false;
        const poll = () => { IsFreeProxyRunning().then(r => { if (!cancelled) setProxyRunning(r); }).catch(() => {}); };
        poll();
        const id = setInterval(poll, 3000);
        return () => { cancelled = true; clearInterval(id); };
    }, [freeModalOpen]);

    const openFreeModal = useCallback(() => {
        setFreeResult(null);
        setBrowserLaunched(false);
        setBrowserInfo(null);
        setDangbeiLoggedIn(false);
        setAuthChecking(false);
        setFreeModalOpen(true);
    }, []);

    const closeFreeModal = useCallback(() => setFreeModalOpen(false), []);

    // Save free provider selection and close modal
    const handleFreeSave = async () => {
        const freeIdx = providers.findIndex(p => p.auth_type === "none");
        if (freeIdx < 0) return;
        if (!proxyRunning) {
            setFreeResult({ ok: false, msg: t("请先启动代理服务", "Please start the proxy first") });
            return;
        }
        setLlmSaving(true);
        try {
            await SaveMaclawLLMProviders(providers, providers[freeIdx].name);
            setLlmDone(true);
            onLLMConfigured();
            setFreeModalOpen(false);
        } catch (e) {
            setFreeResult({ ok: false, msg: String(e) });
        } finally {
            setLlmSaving(false);
        }
    };

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
                // Save first to set current provider, then test (which also probes vision
                // and persists supports_vision into the current provider entry).
                await SaveMaclawLLMProviders(providers, sp.name);
                const reply = await TestMaclawLLM({ url: sp.url, key: sp.key, model: sp.model, protocol: sp.protocol || "openai", agent_type: sp.agent_type || "openclaw" });

                // Refresh providers to pick up auto-detected supports_vision from backend
                try {
                    const freshData = await GetMaclawLLMProviders();
                    if (freshData?.providers) {
                        setProviders(freshData.providers.map((p: LLMProvider) => ({ ...p })));
                    }
                } catch { /* non-fatal */ }

                setLlmResult({ ok: true, msg: reply });
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
        onSaveField({ remote_email: regEmail.trim() });
        try {
            const result = await ActivateRemote(regEmail.trim(), invCode.trim(), "");
            if (result?.vip_flag) setVipFlag(true);
            setRegResult({ ok: true, msg: t("注册成功", "Registration successful") });
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
        if (isTigerclaw) {
            return lang?.startsWith("zh") ? STEP_LABELS_ZH_TIGERCLAW : STEP_LABELS_EN_TIGERCLAW;
        }
        return lang?.startsWith("zh") ? STEP_LABELS_ZH : STEP_LABELS_EN;
    }, [lang, isTigerclaw]);

    return (
        <div style={{
            position: "fixed", top: 0, left: 0, right: 0, bottom: 0,
            background: "rgba(0,0,0,0.3)", backdropFilter: "blur(3px)",
            display: "flex", alignItems: "center", justifyContent: "center", zIndex: 9999,
        }}>
            <div style={{
                background: "#fff", borderRadius: 14, width: 440, maxHeight: "90vh",
                overflowY: "auto", boxShadow: "0 8px 24px rgba(99,102,241,0.12)",
                border: "1px solid #e0e7ff", display: "flex", flexDirection: "column",
            }}>
                {/* ── Header ── */}
                <div style={{
                    background: "linear-gradient(135deg, #eef2ff 0%, #e0e7ff 100%)",
                    padding: "12px 18px 10px", position: "relative", flexShrink: 0,
                }}>
                    <button onClick={onClose} style={{
                        position: "absolute", top: 8, right: 12, border: "none",
                        background: "transparent", cursor: "pointer", fontSize: "1.1rem", color: "#a5b4fc",
                    }}>&times;</button>
                    <div style={{ fontSize: "1.2rem", marginBottom: 2, lineHeight: 1 }}>👋</div>
                    <h3 style={{ margin: 0, color: "#4338ca", fontSize: "0.88rem", fontWeight: 600 }}>
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
                        const circleColor = skippedStep ? "#94a3b8" : done ? "#22c55e" : active ? "#6366f1" : "#cbd5e1";
                        return (
                            <div key={s} style={{ display: "flex", alignItems: "center" }}>
                                <div style={{ display: "flex", flexDirection: "column", alignItems: "center", minWidth: 54 }}>
                                    <div style={{
                                        width: 26, height: 26, borderRadius: "50%",
                                        background: circleColor, color: "#fff",
                                        display: "flex", alignItems: "center", justifyContent: "center",
                                        fontSize: "0.72rem", fontWeight: 700,
                                        transition: "background 0.2s",
                                    }}>
                                        {skippedStep ? "—" : done ? "✓" : s}
                                    </div>
                                    <span style={{
                                        fontSize: "0.62rem", marginTop: 3,
                                        color: active ? "#4338ca" : "#94a3b8",
                                        fontWeight: active ? 600 : 400,
                                    }}>
                                        {labels[i]}
                                    </span>
                                </div>
                                {s < totalSteps && (
                                    <div style={{
                                        width: 32, height: 2, background: stepDone[s] ? "#22c55e" : "#e2e8f0",
                                        margin: "0 2px", marginBottom: 14, transition: "background 0.2s",
                                    }} />
                                )}
                            </div>
                        );
                    })}
                </div>

                {/* ── Step content ── */}
                <div style={{ padding: "10px 18px 0", flex: 1, overflowY: "auto" }}>

                    {/* ═══ Step 1 ═══
                        TigerClaw 品牌：企业 SSO 认证 + 自动注册 Hub（合并原 Step1+Step2）
                        普通品牌：邮件注册
                    */}
                    {step === 1 && isTigerclaw && (
                        <div>
                            <p style={{ margin: "0 0 10px 0", fontSize: "0.76rem", color: "#64748b", lineHeight: 1.4 }}>
                                {t(
                                    `使用企业账号一键登录，自动配置 ${displayName} 和 TigerClaw Code，并注册到 Hub。`,
                                    `Sign in with your enterprise account to configure ${displayName}, TigerClaw Code, and register to Hub.`
                                )}
                            </p>
                            <button onClick={handleEmbeddedSSOLogin} disabled={ssoBusy || (llmDone && regDone)} style={{
                                width: "100%", padding: "12px 0", fontSize: "0.84rem", fontWeight: 600,
                                background: ssoBusy || regBusy ? "#a5b4fc" : (llmDone && regDone) ? "#86efac" : "#6366f1",
                                color: "#fff", border: "none", borderRadius: 6,
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
                                    <p style={{ fontSize: "0.76rem", color: "#94a3b8" }}>
                                        {t("正在打开登录页面...", "Opening login page...")}
                                    </p>
                                </div>
                            )}
                            {ssoBusy && !embeddedSSOLoading && (
                                <div style={{ textAlign: "center", padding: "16px 0" }}>
                                    <div style={{ fontSize: "2rem", marginBottom: 8 }}>🔐</div>
                                    <p style={{ fontSize: "0.76rem", color: "#64748b", marginTop: 10 }}>
                                        {t("请在弹出的浏览器页面中扫码", "Please scan the QR code in the browser window")}
                                    </p>
                                    <p style={{ fontSize: "0.7rem", color: "#94a3b8" }}>
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
                                        background: "#f8fafc", color: "#6366f1", border: "1px solid #e2e8f0",
                                        borderRadius: 6, cursor: "pointer", marginTop: 6,
                                    }}>
                                        {t("🌐 在浏览器中打开", "🌐 Open in Browser")}
                                    </button>
                                    <button onClick={handleEmbeddedSSOLogin} style={{
                                        width: "100%", padding: "8px 0", fontSize: "0.76rem",
                                        background: "#f8fafc", color: "#6366f1", border: "1px solid #e2e8f0",
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
                                    color: ssoResult.ok ? "#22c55e" : "#ef4444",
                                }}>
                                    {ssoResult.ok ? `✅ ${ssoResult.msg}` : `❌ ${ssoResult.msg}`}
                                </div>
                            )}
                            {!(llmDone && regDone) && !ssoBusy && !regBusy && !embeddedSSOLoading && !embeddedSSOError && (
                                <p style={{ marginTop: 8, fontSize: "0.7rem", color: "#94a3b8", textAlign: "center" }}>
                                    {t("点击后自动弹出企业登录页，扫码后自动完成所有配置", "Browser opens automatically; all config applied after login")}
                                </p>
                            )}

                            {/* ── 模型选择区域（SSO 成功后显示）── */}
                            {llmDone && (
                                <div style={{ marginTop: 14, padding: "12px 14px", borderRadius: 8, border: "1px solid #e2e8f0", background: "#f8fafc" }}>
                                    <div style={{ fontSize: "0.76rem", fontWeight: 600, color: "#334155", marginBottom: 10 }}>
                                        {t("🔧 选择使用的模型（可选）", "🔧 Select Models (optional)")}
                                    </div>

                                    {codegenModelsFetching && (
                                        <p style={{ fontSize: "0.72rem", color: "#94a3b8" }}>
                                            {t("正在获取可用模型列表...", "Fetching available models...")}
                                        </p>
                                    )}

                                    {!codegenModelsFetching && codegenModels.length > 0 && (
                                        <>
                                            {/* MaClaw 模型 */}
                                            <div style={{ marginBottom: 10 }}>
                                                <label style={{ fontSize: "0.72rem", color: "#64748b", display: "block", marginBottom: 4 }}>
                                                    🤖 {t(`${displayName} Agent 模型`, `${displayName} Agent Model`)}
                                                </label>
                                                <div style={{ display: "flex", gap: 4, flexWrap: "wrap" }}>
                                                    {codegenModels.map(m => (
                                                        <button key={m.id} onClick={() => setMaclawModel(m.id)} style={{
                                                            fontSize: "0.7rem", padding: "4px 10px", cursor: "pointer",
                                                            background: maclawModel === m.id ? "#6366f1" : "#fff",
                                                            color: maclawModel === m.id ? "#fff" : "#334155",
                                                            border: `1px solid ${maclawModel === m.id ? "#6366f1" : "#e2e8f0"}`,
                                                            borderRadius: 4, transition: "all 0.12s",
                                                        }}>
                                                            {m.name}
                                                        </button>
                                                    ))}
                                                </div>
                                            </div>

                                            {/* TigerClaw Code 模型 */}
                                            <div style={{ marginBottom: 10 }}>
                                                <label style={{ fontSize: "0.72rem", color: "#64748b", display: "block", marginBottom: 4 }}>
                                                    🐯 {t("TigerClaw Code 模型", "TigerClaw Code Model")}
                                                </label>
                                                <div style={{ display: "flex", gap: 4, flexWrap: "wrap" }}>
                                                    {codegenModels.map(m => (
                                                        <button key={m.id} onClick={() => setClaudeCodeModel(m.id)} style={{
                                                            fontSize: "0.7rem", padding: "4px 10px", cursor: "pointer",
                                                            background: claudeCodeModel === m.id ? "#0ea5e9" : "#fff",
                                                            color: claudeCodeModel === m.id ? "#fff" : "#334155",
                                                            border: `1px solid ${claudeCodeModel === m.id ? "#0ea5e9" : "#e2e8f0"}`,
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
                                                background: modelSaved ? "#86efac" : modelSaving ? "#a5b4fc" : "#6366f1",
                                                color: "#fff", border: "none", borderRadius: 6,
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
                            <p style={{ margin: "0 0 10px 0", fontSize: "0.76rem", color: "#64748b", lineHeight: 1.4 }}>
                                {t("注册设备邮箱到 Hub，即可通过移动端操控。",
                                    "Register your email to the Hub for remote control.")}
                            </p>
                            <div style={{ marginBottom: 10 }}>
                                <label style={labelStyle}>{t("邮箱", "Email")} <span style={{ color: "#ef4444" }}>*</span></label>
                                <input style={inputStyle} value={regEmail}
                                    onChange={e => setRegEmail(e.target.value)}
                                    placeholder="name@example.com" spellCheck={false} />
                            </div>
                            {invRequired && (
                                <div style={{ marginBottom: 10 }}>
                                    <label style={labelStyle}>
                                        {t("邀请码", "Invitation Code")}{" "}
                                        <span style={{ fontSize: "0.68rem", color: "#94a3b8" }}>({t("可选", "optional")})</span>
                                    </label>
                                    <input style={{ ...inputStyle, ...(invError ? { borderColor: "#ef4444" } : {}) }}
                                        value={invCode}
                                        onChange={e => { setInvCode(e.target.value.toUpperCase()); setInvError(""); }}
                                        placeholder={t("请输入邀请码", "Enter invitation code")}
                                        maxLength={10} spellCheck={false} />
                                    {invError && <div style={{ fontSize: "0.72rem", color: "#ef4444", marginTop: 4 }}>{invError}</div>}
                                </div>
                            )}
                            <button onClick={handleRegisterClick} disabled={regBusy || regDone} style={{
                                width: "100%", padding: "8px 0", fontSize: "0.8rem", fontWeight: 600,
                                background: regBusy ? "#a5b4fc" : regDone ? "#86efac" : "#6366f1",
                                color: "#fff", border: "none", borderRadius: 6,
                                cursor: regBusy || regDone ? "default" : "pointer",
                            }}>
                                {regBusy ? t("注册中...", "Registering...") : regDone ? t("✅ 已注册", "✅ Registered") : t("注册", "Register")}
                            </button>
                            {regResult && (
                                <div style={{
                                    marginTop: 8, padding: "6px 10px", borderRadius: 4, fontSize: "0.74rem",
                                    lineHeight: 1.5, whiteSpace: "pre-wrap", wordBreak: "break-word",
                                    background: regResult.ok ? "rgba(34,197,94,0.1)" : "rgba(239,68,68,0.1)",
                                    border: `1px solid ${regResult.ok ? "rgba(34,197,94,0.3)" : "rgba(239,68,68,0.3)"}`,
                                    color: regResult.ok ? "#22c55e" : "#ef4444",
                                }}>
                                    {regResult.ok ? `✅ ${regResult.msg}` : `❌ ${regResult.msg}`}
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
                            <p style={{ margin: "0 0 10px 0", fontSize: "0.76rem", color: "#64748b", lineHeight: 1.4 }}>
                                {t("选择适合你的界面模式。", "Choose the interface mode that suits you.")}
                            </p>
                            <div style={{ display: "flex", gap: 10 }}>
                                <div
                                    onClick={() => { setSelectedMode('pro'); onSaveField({ ui_mode: 'pro' }); setModeDone(true); }}
                                    style={{
                                        flex: 1, padding: "14px 14px", borderRadius: 10, cursor: "pointer",
                                        border: `2px solid ${selectedMode === 'pro' ? '#6366f1' : '#e2e8f0'}`,
                                        background: selectedMode === 'pro' ? 'rgba(99,102,241,0.06)' : '#f8fafc',
                                        transition: "all 0.15s",
                                    }}
                                >
                                    <div style={{ fontSize: "0.88rem", fontWeight: 600, color: selectedMode === 'pro' ? '#4338ca' : '#334155', marginBottom: 4 }}>
                                        🛠️ {t("专业模式", "Pro")}
                                    </div>
                                    <div style={{ fontSize: "0.72rem", color: "#94a3b8", lineHeight: 1.4 }}>
                                        {t("包含完整编程工具链，适合开发者", "Full coding toolchain for developers")}
                                    </div>
                                </div>
                                <div
                                    onClick={() => { setSelectedMode('lite'); onSaveField({ ui_mode: 'lite' }); setModeDone(true); }}
                                    style={{
                                        flex: 1, padding: "14px 14px", borderRadius: 10, cursor: "pointer",
                                        border: `2px solid ${selectedMode === 'lite' ? '#6366f1' : '#e2e8f0'}`,
                                        background: selectedMode === 'lite' ? 'rgba(99,102,241,0.06)' : '#f8fafc',
                                        transition: "all 0.15s",
                                    }}
                                >
                                    <div style={{ fontSize: "0.88rem", fontWeight: 600, color: selectedMode === 'lite' ? '#4338ca' : '#334155', marginBottom: 4 }}>
                                        ✨ {t("简洁模式", "Lite")}
                                    </div>
                                    <div style={{ fontSize: "0.72rem", color: "#94a3b8", lineHeight: 1.4 }}>
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
                            <p style={{ margin: "0 0 8px 0", fontSize: "0.76rem", color: "#64748b", lineHeight: 1.4 }}>
                                {t("选择一个 LLM 服务商，输入 API Key 后测试并保存。",
                                    "Pick a provider, enter your API Key, then test & save.")}
                            </p>
                            {/* Provider buttons */}
                            <div style={{ display: "flex", gap: 6, marginBottom: 10, flexWrap: "wrap" }}>
                                {providers.map((p, i) => {
                                    // Hide free provider when no vipFlag
                                    if (p.auth_type === "none" && !vipFlag) return null;
                                    const isFree = p.auth_type === "none";
                                    const active = selectedIdx === i;
                                    return (
                                        <div key={i} style={{ textAlign: "center" }}>
                                            <button onClick={() => {
                                                if (isFree) { openFreeModal(); return; }
                                                setSelectedIdx(active ? null : i); setLlmResult(null);
                                            }} style={{
                                                fontSize: "0.78rem", padding: "6px 16px", cursor: "pointer",
                                                background: active ? "#6366f1" : "#f8fafc",
                                                color: active ? "#fff" : "#334155",
                                                border: `1px solid ${active ? "#6366f1" : "#e2e8f0"}`,
                                                borderRadius: 6, fontWeight: active ? 600 : 400,
                                                transition: "all 0.15s",
                                                display: "inline-flex", alignItems: "center", gap: 5,
                                            }}>
                                                {PROVIDER_LOGOS[p.name] ?? null}{p.name}
                                            </button>
                                            {p.auth_type === "oauth" && (
                                                <div style={{ fontSize: "0.62rem", color: "#94a3b8", marginTop: 2 }}>
                                                    {t("一键登录", "One-click")}
                                                </div>
                                            )}
                                            {isFree && (
                                                <div style={{ fontSize: "0.62rem", color: "#22c55e", marginTop: 2 }}>
                                                    🆓 {t("免费", "Free")}
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
                                    border: "1px solid #e2e8f0", background: "#f8fafc",
                                }}>
                                    {selectedProvider.auth_type === "oauth" ? (
                                        <>
                                            <p style={{ fontSize: "0.76rem", color: "#64748b", margin: "0 0 12px 0", lineHeight: 1.4 }}>
                                                {t("点击下方按钮，将在浏览器中完成 OpenAI 账号授权。",
                                                    "Click below to authorize with your OpenAI account in the browser.")}
                                            </p>
                                            <button onClick={handleOAuthLogin} disabled={oauthBusy} style={{
                                                width: "100%", padding: "10px 0", fontSize: "0.82rem", fontWeight: 600,
                                                background: oauthBusy ? "#a5b4fc" : "#6366f1", color: "#fff",
                                                border: "none", borderRadius: 6, cursor: oauthBusy ? "default" : "pointer",
                                            }}>
                                                {oauthBusy ? t("等待浏览器授权...", "Waiting for browser auth...") : t("使用 OpenAI 账号登录", "Sign in with OpenAI")}
                                            </button>
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
                                                                        background: active ? "#6366f1" : "#fff",
                                                                        color: active ? "#fff" : "#1e293b",
                                                                        border: `1px solid ${active ? "#6366f1" : "#e2e8f0"}`,
                                                                        borderRadius: 4, transition: "all 0.15s",
                                                                    }}>
                                                                        {proto === "openai" ? "OpenAI" : "Anthropic"}
                                                                    </button>
                                                                );
                                                            })}
                                                        </div>
                                                        <p style={{ fontSize: "0.68rem", color: "#94a3b8", margin: "4px 0 0 0", lineHeight: 1.4 }}>
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
                                                                        background: active ? "#6366f1" : "#fff",
                                                                        color: active ? "#fff" : "#1e293b",
                                                                        border: `1px solid ${active ? "#6366f1" : "#e2e8f0"}`,
                                                                        borderRadius: 4, transition: "all 0.15s",
                                                                    }}>
                                                                        {ua}
                                                                    </button>
                                                                );
                                                            })}
                                                        </div>
                                                        <p style={{ fontSize: "0.68rem", color: "#94a3b8", margin: "4px 0 0 0", lineHeight: 1.4 }}>
                                                            {(selectedProvider.agent_type || "openclaw") === "claude-code/2.0.0"
                                                                ? t("Kimi 等需要编程套餐身份的服务商", "For providers requiring Claude Coding Plan identity (e.g. Kimi)")
                                                                : t("智谱等大多数服务商使用 OpenClaw 身份", "Most providers use OpenClaw identity (e.g. Zhipu)")}
                                                        </p>
                                                    </div>
                                                    <div style={{ marginBottom: 10 }}>
                                                        <label style={labelStyle}>API URL <span style={{ color: "#ef4444" }}>*</span></label>
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
                                                        <label style={labelStyle}>API URL <span style={{ fontSize: "0.68rem", color: "#94a3b8" }}>({t("预设", "preset")})</span></label>
                                                        <input style={readonlyInputStyle} value={selectedProvider.url} readOnly tabIndex={-1} />
                                                    </div>
                                                    <div style={{ marginBottom: 10 }}>
                                                        <label style={labelStyle}>{t("模型名称", "Model Name")} <span style={{ fontSize: "0.68rem", color: "#94a3b8" }}>({t("预设", "preset")})</span></label>
                                                        <input style={readonlyInputStyle} value={selectedProvider.model} readOnly tabIndex={-1} />
                                                    </div>
                                                </>
                                            )}
                                            <div style={{ marginBottom: 12 }}>
                                                <label style={labelStyle}>API Key <span style={{ color: "#ef4444" }}>*</span></label>
                                                <input style={inputStyle} type="password" value={selectedProvider.key}
                                                    onChange={e => updateField("key", e.target.value)}
                                                    placeholder={selectedProvider.is_custom ? "sk-..." : (selectedProvider.name === "智谱" ? "xxxxxxxx.yyyyyyyy" : "sk-...")}
                                                    autoComplete="off" />
                                            </div>
                                            <button onClick={handleLLMSave} disabled={llmSaving} style={{
                                                width: "100%", padding: "8px 0", fontSize: "0.8rem", fontWeight: 600,
                                                background: llmSaving ? "#a5b4fc" : "#6366f1", color: "#fff",
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
                                            color: llmResult.ok ? "#22c55e" : "#ef4444",
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
                            <p style={{ margin: "0 0 10px 0", fontSize: "0.76rem", color: "#64748b", lineHeight: 1.4 }}>
                                {t(`扫码绑定微信，即可通过微信与 ${displayName} 交互。`,
                                    `Scan to bind WeChat for messaging with ${displayName}.`)}
                            </p>
                            {wxDone ? (
                                <div style={{
                                    padding: "16px", textAlign: "center", borderRadius: 8,
                                    background: "rgba(34,197,94,0.08)", border: "1px solid rgba(34,197,94,0.2)",
                                }}>
                                    <div style={{ fontSize: "1.4rem", marginBottom: 4 }}>✅</div>
                                    <div style={{ fontSize: "0.82rem", color: "#22c55e", fontWeight: 600 }}>
                                        {t("微信已绑定", "WeChat connected")}
                                    </div>
                                </div>
                            ) : wxSkipped ? (
                                <div style={{
                                    padding: "16px", textAlign: "center", borderRadius: 8,
                                    background: "rgba(148,163,184,0.08)", border: "1px solid rgba(148,163,184,0.2)",
                                }}>
                                    <div style={{ fontSize: "1.4rem", marginBottom: 4 }}>⏭️</div>
                                    <div style={{ fontSize: "0.82rem", color: "#94a3b8", fontWeight: 600 }}>
                                        {t("已跳过，可稍后在设置中绑定", "Skipped — you can bind later in settings")}
                                    </div>
                                </div>
                            ) : (
                                <div>
                                    {!wxQrUrl && wxStatus !== "error" && (
                                        <button onClick={startWxQR} disabled={wxLoading} style={{
                                            width: "100%", padding: "10px 0", fontSize: "0.82rem", fontWeight: 600,
                                            background: wxLoading ? "#a5b4fc" : "#6366f1", color: "#fff",
                                            border: "none", borderRadius: 6, cursor: wxLoading ? "default" : "pointer",
                                        }}>
                                            {wxLoading ? t("获取中...", "Loading...") : t("显示二维码", "Show QR Code")}
                                        </button>
                                    )}
                                    {wxQrUrl && wxStatus !== "expired" && wxStatus !== "error" && (
                                        <div style={{ textAlign: "center" }}>
                                            <QRCodeSVG value={wxQrUrl} size={200} style={{
                                                borderRadius: 8, border: "1px solid #e2e8f0",
                                            }} />
                                            <div style={{ marginTop: 8 }}>
                                                <button onClick={startWxQR} disabled={wxLoading} style={{
                                                    fontSize: "0.72rem", padding: "4px 14px",
                                                    background: "transparent", color: "#6366f1",
                                                    border: "1px solid #e2e8f0", borderRadius: 4,
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
                                            background: wxLoading ? "#a5b4fc" : "#6366f1", color: "#fff",
                                            border: "none", borderRadius: 6, cursor: wxLoading ? "default" : "pointer",
                                        }}>
                                            {t("刷新二维码", "Refresh QR Code")}
                                        </button>
                                    )}
                                    {wxMsg && (
                                        <div style={{
                                            marginTop: 8, padding: "6px 10px", borderRadius: 4, fontSize: "0.74rem",
                                            textAlign: "center",
                                            color: wxStatus === "error" || wxStatus === "expired" ? "#ef4444" : wxStatus === "scaned" ? "#b45309" : "#64748b",
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
                    padding: "12px 18px 14px", borderTop: "1px solid #e0e7ff", flexShrink: 0,
                }}>
                    <button
                        onClick={() => setStep(s => Math.max(1, s - 1))}
                        disabled={!canPrev}
                        style={{
                            padding: "7px 20px", fontSize: "0.8rem", borderRadius: 6,
                            background: canPrev ? "#f1f5f9" : "#f8fafc",
                            color: canPrev ? "#334155" : "#cbd5e1",
                            border: `1px solid ${canPrev ? "#e2e8f0" : "#f1f5f9"}`,
                            cursor: canPrev ? "pointer" : "default",
                            fontWeight: 500,
                        }}
                    >
                        {t("上一步", "Back")}
                    </button>

                    <span style={{ fontSize: "0.7rem", color: "#94a3b8" }}>
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
                                        background: "transparent", color: "#94a3b8", border: "1px solid #e2e8f0",
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
                                    background: wxCompleted ? "#22c55e" : "#cbd5e1",
                                    color: "#fff", border: "none",
                                    cursor: wxCompleted ? "pointer" : "default",
                                }}
                            >
                                {t("完成", "Finish")}
                            </button>
                        </div>
                    ) : (
                        <button
                            onClick={() => setStep(s => Math.min(totalSteps, s + 1))}
                            disabled={!canNext}
                            style={{
                                padding: "7px 20px", fontSize: "0.8rem", fontWeight: 600, borderRadius: 6,
                                background: canNext ? "#6366f1" : "#cbd5e1",
                                color: "#fff", border: "none",
                                cursor: canNext ? "pointer" : "default",
                            }}
                        >
                            {t("下一步", "Next")}
                        </button>
                    )}
                </div>
            </div>

            {/* ── Free proxy config modal ── */}
            {freeModalOpen && (
                <div style={{
                    position: "fixed", top: 0, left: 0, right: 0, bottom: 0,
                    background: "rgba(0,0,0,0.35)", display: "flex",
                    alignItems: "center", justifyContent: "center", zIndex: 10000,
                }} onClick={closeFreeModal}>
                    <div style={{
                        background: "#fff", borderRadius: 12, padding: "20px 24px",
                        maxWidth: 460, width: "90%", maxHeight: "80vh", overflowY: "auto",
                        boxShadow: "0 16px 40px rgba(0,0,0,0.18)",
                    }} onClick={e => e.stopPropagation()}>
                        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 14 }}>
                            <span style={{ fontSize: "0.88rem", fontWeight: 700, color: "#1e293b" }}>
                                🆓 {t("免费服务商配置", "Free Provider Setup")}
                            </span>
                            <button onClick={closeFreeModal} style={{
                                border: "none", background: "transparent", cursor: "pointer",
                                fontSize: "1.1rem", color: "#94a3b8", padding: "0 4px",
                            }}>✕</button>
                        </div>

                        {/* Dangbei login status */}
                        <label style={labelStyle}>{t("当贝 AI 登录", "Dangbei AI Login")}</label>
                        {authChecking ? (
                            <div style={{
                                padding: "8px 12px", borderRadius: 4, marginBottom: 10,
                                background: "rgba(99,102,241,0.08)", border: "1px solid rgba(99,102,241,0.25)",
                            }}>
                                <span style={{ fontSize: "0.76rem", color: "#6366f1" }}>
                                    ⏳ {t("正在验证登录状态...", "Validating login status...")}
                                </span>
                            </div>
                        ) : dangbeiLoggedIn ? (
                            <div style={{
                                display: "flex", alignItems: "center", gap: 8,
                                padding: "8px 12px", borderRadius: 4, marginBottom: 10,
                                background: "rgba(34,197,94,0.08)", border: "1px solid rgba(34,197,94,0.25)",
                            }}>
                                <span style={{ fontSize: "0.76rem", color: "#22c55e", flex: 1 }}>
                                    ✅ {t("已登录当贝 AI", "Logged in to Dangbei AI")}
                                </span>
                                <button disabled={loginBusy} onClick={async () => {
                                    setLoginBusy(true); setFreeResult(null);
                                    try { await DangbeiLogin(); setBrowserLaunched(true); setFreeResult({ ok: true, msg: t("浏览器已打开，请登录后点击「完成登录」", "Browser opened. Log in then click 'Finish Login'") }); }
                                    catch (e) { setFreeResult({ ok: false, msg: String(e) }); }
                                    setLoginBusy(false);
                                }} style={{
                                    fontSize: "0.72rem", padding: "4px 12px", cursor: loginBusy ? "default" : "pointer",
                                    background: "transparent", color: "#6366f1",
                                    border: "1px solid #6366f1", borderRadius: 4, opacity: loginBusy ? 0.5 : 1,
                                }}>
                                    {loginBusy ? "..." : t("重新登录", "Re-login")}
                                </button>
                            </div>
                        ) : (
                            <div style={{ marginBottom: 10 }}>
                                {browserInfo === null ? (
                                    <p style={{ fontSize: "0.72rem", color: "#94a3b8" }}>{t("检测浏览器...", "Detecting browser...")}</p>
                                ) : browserInfo.found === "true" ? (
                                    <div style={{
                                        display: "flex", alignItems: "center", gap: 8,
                                        padding: "8px 12px", borderRadius: 4, marginBottom: 8,
                                        background: "rgba(34,197,94,0.08)", border: "1px solid rgba(34,197,94,0.25)",
                                    }}>
                                        <span style={{ fontSize: "0.76rem", color: "#22c55e" }}>
                                            ✅ {t(`已找到 ${browserInfo.name === "edge" ? "Edge" : "Chrome"}`, `${browserInfo.name === "edge" ? "Edge" : "Chrome"} found`)}
                                        </span>
                                    </div>
                                ) : (
                                    <div style={{
                                        display: "flex", alignItems: "center", gap: 8,
                                        padding: "8px 12px", borderRadius: 4, marginBottom: 8,
                                        background: "rgba(239,68,68,0.08)", border: "1px solid rgba(239,68,68,0.25)",
                                    }}>
                                        <span style={{ fontSize: "0.76rem", color: "#ef4444", flex: 1 }}>
                                            ❌ {t("未找到 Chrome 或 Edge", "Chrome/Edge not found")}
                                        </span>
                                    </div>
                                )}
                                <button disabled={loginBusy || browserInfo?.found !== "true"} onClick={async () => {
                                    setLoginBusy(true); setFreeResult(null);
                                    try { await DangbeiLogin(); setBrowserLaunched(true); setFreeResult({ ok: true, msg: t("浏览器已打开，请在浏览器中登录当贝 AI，完成后点击下方「完成登录」按钮", "Browser opened. Log in to Dangbei AI, then click 'Finish Login' below") }); }
                                    catch (e) { setFreeResult({ ok: false, msg: String(e) }); }
                                    setLoginBusy(false);
                                }} style={{
                                    width: "100%", padding: "10px 0", fontSize: "0.8rem",
                                    cursor: loginBusy ? "default" : "pointer",
                                    background: "#6366f1", color: "#fff",
                                    border: "none", borderRadius: 4,
                                    opacity: (loginBusy || browserInfo?.found !== "true") ? 0.6 : 1,
                                }}>
                                    {loginBusy ? `⏳ ${t("正在启动浏览器...", "Launching browser...")}` : t("登录当贝 AI", "Login to Dangbei AI")}
                                </button>
                            </div>
                        )}

                        {/* Finish login button */}
                        {browserLaunched && (
                            <div style={{ marginBottom: 10 }}>
                                <button disabled={loginBusy} onClick={async () => {
                                    setLoginBusy(true); setFreeResult(null);
                                    try {
                                        await DangbeiFinishLogin();
                                        setDangbeiLoggedIn(true); setBrowserLaunched(false);
                                        try { const running = await IsFreeProxyRunning(); if (!running) { await StartFreeProxy(); setProxyRunning(true); } } catch {}
                                        setFreeResult({ ok: true, msg: t("登录成功，代理已自动启动", "Login successful, proxy auto-started") });
                                    } catch (e) { setFreeResult({ ok: false, msg: String(e) }); }
                                    setLoginBusy(false);
                                }} style={{
                                    width: "100%", padding: "10px 0", fontSize: "0.8rem",
                                    cursor: loginBusy ? "default" : "pointer",
                                    background: "#22c55e", color: "#fff",
                                    border: "none", borderRadius: 4, opacity: loginBusy ? 0.6 : 1,
                                }}>
                                    {loginBusy ? `⏳ ${t("正在关闭浏览器并提取登录信息...", "Closing browser & extracting login info...")}` : t("✅ 我已在浏览器中登录，完成登录", "✅ I've logged in, finish login")}
                                </button>
                            </div>
                        )}

                        {freeResult && (
                            <div style={{
                                marginBottom: 10, padding: "6px 10px", borderRadius: 4, fontSize: "0.74rem",
                                lineHeight: 1.5, whiteSpace: "pre-wrap", wordBreak: "break-word",
                                background: freeResult.ok ? "rgba(34,197,94,0.1)" : "rgba(239,68,68,0.1)",
                                border: `1px solid ${freeResult.ok ? "rgba(34,197,94,0.3)" : "rgba(239,68,68,0.3)"}`,
                                color: freeResult.ok ? "#22c55e" : "#ef4444",
                            }}>
                                {freeResult.ok ? "✅ " : "❌ "}{freeResult.msg}
                            </div>
                        )}

                        {/* Model selection */}
                        <label style={labelStyle}>{t("模型选择", "Model Selection")}</label>
                        <div style={{ display: "flex", gap: 4, flexWrap: "wrap", marginBottom: 12 }}>
                            {freeModels.map(m => {
                                const active = freeSelectedModel === m.id;
                                return (
                                    <button key={m.id} onClick={() => { setFreeSelectedModel(m.id); SetFreeProxyModel(m.id).catch(() => {}); }} style={{
                                        fontSize: "0.72rem", padding: "4px 10px", cursor: "pointer",
                                        background: active ? "#6366f1" : "#f8fafc",
                                        color: active ? "#fff" : "#334155",
                                        border: `1px solid ${active ? "#6366f1" : "#e2e8f0"}`,
                                        borderRadius: 4, transition: "all 0.15s",
                                    }}>
                                        {m.name}
                                    </button>
                                );
                            })}
                        </div>

                        {/* Proxy status */}
                        <label style={labelStyle}>{t("代理状态", "Proxy Status")}</label>
                        <div style={{
                            display: "flex", alignItems: "center", gap: 10, marginBottom: 12,
                            padding: "8px 12px", borderRadius: 4,
                            background: proxyRunning ? "rgba(34,197,94,0.08)" : "rgba(239,68,68,0.08)",
                            border: `1px solid ${proxyRunning ? "rgba(34,197,94,0.25)" : "rgba(239,68,68,0.25)"}`,
                        }}>
                            <span style={{
                                width: 8, height: 8, borderRadius: "50%",
                                background: proxyRunning ? "#22c55e" : "#ef4444",
                                display: "inline-block", flexShrink: 0,
                            }} />
                            <span style={{ fontSize: "0.76rem", color: proxyRunning ? "#22c55e" : "#ef4444", flex: 1 }}>
                                {proxyRunning
                                    ? t("代理服务运行中", "Proxy running")
                                    : t("代理服务未运行", "Proxy not running")}
                            </span>
                            <button disabled={proxyBusy} onClick={async () => {
                                setProxyBusy(true); setFreeResult(null);
                                try {
                                    if (proxyRunning) { await StopFreeProxy(); setProxyRunning(false); }
                                    else { await StartFreeProxy(); setProxyRunning(true); }
                                } catch (e) { setFreeResult({ ok: false, msg: String(e) }); IsFreeProxyRunning().then(r => setProxyRunning(r)).catch(() => {}); }
                                setProxyBusy(false);
                            }} style={{
                                fontSize: "0.72rem", padding: "4px 12px", cursor: proxyBusy ? "default" : "pointer",
                                background: proxyRunning ? "transparent" : "#6366f1",
                                color: proxyRunning ? "#ef4444" : "#fff",
                                border: `1px solid ${proxyRunning ? "#ef4444" : "#6366f1"}`,
                                borderRadius: 4, opacity: proxyBusy ? 0.5 : 1,
                            }}>
                                {proxyBusy ? "..." : proxyRunning ? t("停止", "Stop") : t("启动", "Start")}
                            </button>
                        </div>

                        {/* Footer */}
                        <div style={{ display: "flex", gap: 10, justifyContent: "flex-end" }}>
                            <button onClick={closeFreeModal} style={{
                                fontSize: "0.76rem", padding: "6px 18px", cursor: "pointer",
                                background: "#f1f5f9", color: "#334155",
                                border: "1px solid #e2e8f0", borderRadius: 4,
                            }}>
                                {t("取消", "Cancel")}
                            </button>
                            <button onClick={handleFreeSave} disabled={llmSaving || !dangbeiLoggedIn || !proxyRunning} style={{
                                fontSize: "0.76rem", padding: "6px 18px", cursor: (dangbeiLoggedIn && proxyRunning) ? "pointer" : "default",
                                background: (dangbeiLoggedIn && proxyRunning) ? "#6366f1" : "#cbd5e1",
                                color: "#fff", border: "none", borderRadius: 4,
                                opacity: llmSaving ? 0.6 : 1,
                            }}>
                                {llmSaving ? t("保存中...", "Saving...") : t("保存并继续", "Save & Continue")}
                            </button>
                        </div>
                    </div>
                </div>
            )}

            {/* ── Confirmation dialog ── */}
            {showConfirm && (
                <div style={{
                    position: "fixed", top: 0, left: 0, right: 0, bottom: 0,
                    background: "rgba(0,0,0,0.35)", display: "flex",
                    alignItems: "center", justifyContent: "center", zIndex: 10000,
                }} onClick={() => setShowConfirm(false)}>
                    <div style={{
                        background: "#fff", borderRadius: 16, padding: "24px 28px",
                        maxWidth: 400, width: "90%", boxShadow: "0 16px 40px rgba(0,0,0,0.18)",
                    }} onClick={e => e.stopPropagation()}>
                        <div style={{ fontSize: 16, fontWeight: 700, marginBottom: 12 }}>
                            {t("确认注册信息", "Confirm Registration")}
                        </div>
                        <div style={{ fontSize: 14, color: "#555", lineHeight: 1.6, marginBottom: 8 }}>
                            {t("请确认邮箱正确无误。填写错误会导致注册失败，且需要管理员手动处理。",
                                "Please confirm the email below is correct. Errors require admin intervention.")}
                        </div>
                        <div style={{
                            padding: 14, margin: "12px 0", borderRadius: 10,
                            background: "#f0f5ff", fontSize: "0.88rem", lineHeight: 1.8,
                        }}>
                            <div>
                                <span style={{ color: "#64748b" }}>{t("邮箱", "Email")}:</span>{" "}
                                <span style={{ fontWeight: 600 }}>{regEmail}</span>
                            </div>
                        </div>
                        <div style={{ display: "flex", gap: 10, justifyContent: "flex-end", marginTop: 16 }}>
                            <button onClick={() => setShowConfirm(false)} style={{
                                padding: "6px 18px", fontSize: "0.8rem", borderRadius: 6,
                                background: "#f1f5f9", color: "#334155", border: "1px solid #e2e8f0", cursor: "pointer",
                            }}>
                                {t("返回修改", "Go Back")}
                            </button>
                            <button onClick={doRegister} style={{
                                padding: "6px 18px", fontSize: "0.8rem", fontWeight: 600, borderRadius: 6,
                                background: "#6366f1", color: "#fff", border: "none", cursor: "pointer",
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

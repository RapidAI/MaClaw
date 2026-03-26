import { useState, useCallback, useEffect, useRef, useMemo } from "react";
import { QRCodeSVG } from "qrcode.react";
import {
    GetMaclawLLMProviders,
    SaveMaclawLLMProviders,
    TestMaclawLLM,
    ActivateRemote,
    ProbeRemoteHub,
    StartOpenAIOAuth,
    GetWeixinStatus,
    StartWeixinQRLogin,
    WaitWeixinQRLogin,
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
}

type Props = {
    lang: string;
    hubUrl: string;
    email: string;
    uiMode: string;
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

const TOTAL_STEPS = 4;
const STEP_LABELS_ZH = ["邮件注册", "界面模式", "配置 LLM", "绑定微信"];
const STEP_LABELS_EN = ["Register", "UI Mode", "LLM", "WeChat"];

export function OnboardingWizard({ lang, hubUrl, email, uiMode, onClose, onLLMConfigured, onRegistered, onSaveField }: Props) {
    const t = useCallback((zh: string, en: string) => lang?.startsWith("zh") ? zh : en, [lang]);

    // ── Wizard step (1-based) ──
    const [step, setStep] = useState(1);

    // ── Step 1: Registration ──
    const [regEmail, setRegEmail] = useState(email || "");
    const [invCode, setInvCode] = useState("");
    const [invRequired, setInvRequired] = useState(false);
    const [invError, setInvError] = useState("");
    const [regBusy, setRegBusy] = useState(false);
    const [regResult, setRegResult] = useState<{ ok: boolean; msg: string } | null>(null);
    const [regDone, setRegDone] = useState(false);
    const [showConfirm, setShowConfirm] = useState(false);
    const [vipFlag, setVipFlag] = useState(false);

    // ── Step 2: UI Mode ──
    const [selectedMode, setSelectedMode] = useState<'pro' | 'lite'>(uiMode === 'pro' ? 'pro' : 'lite');
    const [modeDone, setModeDone] = useState(!!uiMode && uiMode !== '');

    // ── Step 3: LLM ──
    const [providers, setProviders] = useState<LLMProvider[]>([]);
    const [selectedIdx, setSelectedIdx] = useState<number | null>(null);
    const [llmSaving, setLlmSaving] = useState(false);
    const [llmResult, setLlmResult] = useState<{ ok: boolean; msg: string } | null>(null);
    const [llmDone, setLlmDone] = useState(false);
    const [oauthBusy, setOauthBusy] = useState(false);

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
    const [wxQrUrl, setWxQrUrl] = useState("");
    const [wxStatus, setWxStatus] = useState("");
    const [wxMsg, setWxMsg] = useState("");
    const [wxLoading, setWxLoading] = useState(false);
    const wxPollingRef = useRef(false);

    // Step completion map (memoized to avoid array re-creation)
    const stepDone = useMemo(() => [false, regDone, modeDone, llmDone, wxDone], [regDone, modeDone, llmDone, wxDone]);

    // Navigation guards
    const canNext = step < TOTAL_STEPS && stepDone[step];
    const canPrev = step > 1;
    const isLastStep = step === TOTAL_STEPS;

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

    // Check WeChat status on mount
    useEffect(() => {
        GetWeixinStatus().then(s => {
            if (s && s !== "disconnected") setWxDone(true);
        }).catch(() => {});
    }, []);

    // Stop WeChat polling when leaving step 4 or unmounting
    useEffect(() => {
        if (step !== 4) wxPollingRef.current = false;
        return () => { wxPollingRef.current = false; };
    }, [step]);

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
        if (regDone && modeDone && llmDone && wxDone) {
            onSaveField({ onboarding_done: true });
            const timer = setTimeout(onClose, 1500);
            return () => clearTimeout(timer);
        }
    }, [regDone, modeDone, llmDone, wxDone, onClose]);

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
            const reply = await TestMaclawLLM({ url: sp.url, key: sp.key, model: sp.model, protocol: sp.protocol || "openai", agent_type: sp.agent_type || "openclaw" });
            await SaveMaclawLLMProviders(providers, sp.name);
            setLlmResult({ ok: true, msg: reply });
            setLlmDone(true);
            onLLMConfigured();
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

            // WaitWeixinQRLogin is a blocking call (up to 8 min) that returns
            // only the final result: "connected" on success, or error/expired.
            wxPollingRef.current = true;
            try {
                const poll = await WaitWeixinQRLogin(token);
                if (!wxPollingRef.current) return; // unmounted or left step 4
                const st = poll.status || "";
                if (st === "connected" || st === "confirmed") {
                    setWxStatus("confirmed");
                    setWxMsg(poll.message || t("✅ 微信绑定成功", "✅ WeChat connected"));
                    setWxDone(true);
                } else if (poll.error) {
                    setWxStatus("expired");
                    setWxMsg("❌ " + poll.error);
                } else {
                    setWxStatus("expired");
                    setWxMsg(poll.message || t("二维码已过期，请刷新", "QR expired, please refresh"));
                }
            } catch {
                if (!wxPollingRef.current) return;
                setWxStatus("error");
                setWxMsg(t("连接失败，请重试", "Connection failed, please retry"));
            } finally {
                wxPollingRef.current = false;
            }
        } catch (e) {
            setWxMsg("❌ " + String(e));
            setWxStatus("error");
            setWxLoading(false);
        }
    };

    // ── Step labels (memoized) ──
    const labels = useMemo(() => lang?.startsWith("zh") ? STEP_LABELS_ZH : STEP_LABELS_EN, [lang]);

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
                        {t("来，配置一下 MaClaw 吧", "Let's get MaClaw ready!")}
                    </h3>
                </div>

                {/* ── Progress bar ── */}
                <div style={{
                    display: "flex", alignItems: "center", justifyContent: "center",
                    gap: 0, padding: "14px 18px 6px", flexShrink: 0,
                }}>
                    {Array.from({ length: TOTAL_STEPS }, (_, i) => {
                        const s = i + 1;
                        const done = stepDone[s];
                        const active = s === step;
                        const circleColor = done ? "#22c55e" : active ? "#6366f1" : "#cbd5e1";
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
                                        {done ? "✓" : s}
                                    </div>
                                    <span style={{
                                        fontSize: "0.62rem", marginTop: 3,
                                        color: active ? "#4338ca" : "#94a3b8",
                                        fontWeight: active ? 600 : 400,
                                    }}>
                                        {labels[i]}
                                    </span>
                                </div>
                                {s < TOTAL_STEPS && (
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

                    {/* ═══ Step 1: Registration ═══ */}
                    {step === 1 && (
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

                    {/* ═══ Step 2: UI Mode ═══ */}
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

                    {/* ═══ Step 3: LLM ═══ */}
                    {step === 3 && (
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
                                            {llmResult.ok ? `✅ ${t("连接成功，已保存", "Connected & saved")}` : `❌ ${llmResult.msg}`}
                                        </div>
                                    )}
                                </div>
                            )}
                        </div>
                    )}

                    {/* ═══ Step 4: WeChat ═══ */}
                    {step === 4 && (
                        <div>
                            <p style={{ margin: "0 0 10px 0", fontSize: "0.76rem", color: "#64748b", lineHeight: 1.4 }}>
                                {t("扫码绑定微信，即可通过微信与 MaClaw 交互。",
                                    "Scan to bind WeChat for messaging with MaClaw.")}
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
                        {step} / {TOTAL_STEPS}
                    </span>

                    {isLastStep ? (
                        <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
                            {!wxDone && (
                                <button
                                    onClick={() => {
                                        setWxDone(true);
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
                                disabled={!wxDone}
                                style={{
                                    padding: "7px 20px", fontSize: "0.8rem", fontWeight: 600, borderRadius: 6,
                                    background: wxDone ? "#22c55e" : "#cbd5e1",
                                    color: "#fff", border: "none",
                                    cursor: wxDone ? "pointer" : "default",
                                }}
                            >
                                {t("完成", "Finish")}
                            </button>
                        </div>
                    ) : (
                        <button
                            onClick={() => setStep(s => Math.min(TOTAL_STEPS, s + 1))}
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

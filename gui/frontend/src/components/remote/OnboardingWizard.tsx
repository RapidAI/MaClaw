import { useState, useCallback, useEffect, useRef } from "react";
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
    RestartWeixin,
} from "../../../wailsjs/go/main/App";

interface LLMProvider {
    name: string;
    url: string;
    key: string;
    model: string;
    protocol?: string;
    context_length?: number;
    is_custom?: boolean;
    auth_type?: string;
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

/* ── Hoisted style objects (avoid re-creation per render) ── */
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
const stepBadge: React.CSSProperties = {
    display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
    width: '22px', height: '22px', borderRadius: '50%',
    background: '#6366f1', color: '#fff', fontSize: '0.72rem', fontWeight: 700, flexShrink: 0,
};
const doneBadge: React.CSSProperties = { ...stepBadge, background: '#22c55e' };

import { PROVIDER_LOGOS } from "./providerLogos";

export function OnboardingWizard({ lang, hubUrl, email, uiMode, onClose, onLLMConfigured, onRegistered, onSaveField }: Props) {
    const t = useCallback((zh: string, en: string) => lang?.startsWith("zh") ? zh : en, [lang]);

    // ── Step 1: UI Mode ──
    const [selectedMode, setSelectedMode] = useState<'pro' | 'lite'>(uiMode === 'pro' ? 'pro' : 'lite');
    const [modeDone, setModeDone] = useState(!!uiMode && uiMode !== '');

    // ── Step 2: LLM ──
    const [providers, setProviders] = useState<LLMProvider[]>([]);
    const [selectedIdx, setSelectedIdx] = useState<number | null>(null);
    const [llmSaving, setLlmSaving] = useState(false);
    const [llmResult, setLlmResult] = useState<{ ok: boolean; msg: string } | null>(null);
    const [llmDone, setLlmDone] = useState(false);
    const [llmFormVisible, setLlmFormVisible] = useState(true);
    const [oauthBusy, setOauthBusy] = useState(false);

    // ── Step 3: Registration ──
    const [regEmail, setRegEmail] = useState(email || "");
    const [invCode, setInvCode] = useState("");
    const [invRequired, setInvRequired] = useState(false);
    const [invError, setInvError] = useState("");
    const [regBusy, setRegBusy] = useState(false);
    const [regResult, setRegResult] = useState<{ ok: boolean; msg: string } | null>(null);
    const [regDone, setRegDone] = useState(false);
    const [showConfirm, setShowConfirm] = useState(false);

    // ── Step 4: WeChat Binding ──
    const [wxDone, setWxDone] = useState(false);
    const [wxQrUrl, setWxQrUrl] = useState("");
    const [wxStatus, setWxStatus] = useState(""); // "wait" | "scaned" | "confirmed" | "expired" | "error"
    const [wxMsg, setWxMsg] = useState("");
    const [wxLoading, setWxLoading] = useState(false);
    const wxPollingRef = useRef(false);

    // Load providers on mount
    useEffect(() => {
        GetMaclawLLMProviders().then(data => {
            if (data?.providers) setProviders(data.providers);
        }).catch(() => {});
    }, []);

    // Probe hub for invitation code requirement (only on mount / hubUrl change,
    // NOT on every regEmail keystroke — use the initial email prop).
    const initialEmailRef = useRef(email);
    useEffect(() => {
        if (!hubUrl || !initialEmailRef.current) return;
        ProbeRemoteHub(hubUrl, initialEmailRef.current).then(r => {
            if (r?.invitation_code_required) setInvRequired(true);
        }).catch(() => {});
    }, [hubUrl]);

    // Check WeChat status on mount — if already connected, mark done
    useEffect(() => {
        GetWeixinStatus().then(s => {
            if (s && s !== "disconnected") setWxDone(true);
        }).catch(() => {});
    }, []);

    // Cleanup polling on unmount
    useEffect(() => {
        return () => { wxPollingRef.current = false; };
    }, []);

    // Escape key to close wizard (but not if confirmation dialog is open)
    useEffect(() => {
        const onKey = (e: KeyboardEvent) => {
            if (e.key === "Escape" && !showConfirm) onClose();
        };
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [onClose, showConfirm]);

    // Auto-close when all steps are done
    const isLite = selectedMode === 'lite';
    useEffect(() => {
        const allDone = isLite ? (modeDone && llmDone && wxDone) : (modeDone && llmDone && regDone && wxDone);
        if (allDone) {
            const timer = setTimeout(onClose, 1500);
            return () => clearTimeout(timer);
        }
    }, [modeDone, llmDone, regDone, wxDone, isLite, onClose]);

    // Collapse the LLM form after successful test & save
    useEffect(() => {
        if (llmDone) {
            const timer = setTimeout(() => setLlmFormVisible(false), 1500);
            return () => clearTimeout(timer);
        }
    }, [llmDone]);

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

    // ── LLM Save (Test & Save) ──
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
            const reply = await TestMaclawLLM({ url: sp.url, key: sp.key, model: sp.model, protocol: sp.protocol || "openai" });
            await SaveMaclawLLMProviders(providers, sp.name);
            setLlmResult({ ok: true, msg: reply });
            setLlmDone(true);
            onLLMConfigured();
        } catch (e) {
            setLlmResult({ ok: false, msg: String(e) });
        }
        setLlmSaving(false);
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
        }
        setOauthBusy(false);
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
            await ActivateRemote(regEmail.trim(), invCode.trim(), "");
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
        }
        setRegBusy(false);
    };

    // ── WeChat QR Login ──
    const startWxQR = async () => {
        wxPollingRef.current = false; // stop any previous polling
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

            // Start long-polling loop
            wxPollingRef.current = true;
            while (wxPollingRef.current) {
                try {
                    const poll = await WaitWeixinQRLogin(token);
                    const st = poll.status || "";
                    if (st === "confirmed") {
                        setWxStatus("confirmed");
                        setWxMsg(poll.message || t("✅ 微信绑定成功", "✅ WeChat connected"));
                        setWxDone(true);
                        wxPollingRef.current = false;
                        // Auto-start WeChat process after successful QR scan
                        RestartWeixin().catch(() => {});
                        break;
                    } else if (st === "scaned") {
                        setWxStatus("scaned");
                        setWxMsg(poll.message || t("已扫码，请在微信确认", "Scanned, confirm in WeChat"));
                    } else if (st === "expired") {
                        setWxStatus("expired");
                        setWxMsg(poll.message || t("二维码已过期，请刷新", "QR expired, please refresh"));
                        wxPollingRef.current = false;
                        break;
                    }
                    // "wait" — continue polling
                } catch {
                    if (!wxPollingRef.current) break; // unmounted, don't update state
                    setWxStatus("error");
                    setWxMsg(t("轮询失败，请重试", "Polling failed, please retry"));
                    wxPollingRef.current = false;
                    break;
                }
            }
        } catch (e) {
            setWxMsg("❌ " + String(e));
            setWxStatus("error");
            setWxLoading(false);
        }
    };

    return (
        <div style={{
            position: "fixed", top: 0, left: 0, right: 0, bottom: 0,
            background: "rgba(0,0,0,0.3)", backdropFilter: "blur(3px)",
            display: "flex", alignItems: "center", justifyContent: "center", zIndex: 9999,
        }}>
            <div style={{
                background: "#fff", borderRadius: 14, width: 420, maxHeight: "90vh", overflowY: "auto",
                boxShadow: "0 8px 24px rgba(99,102,241,0.12)", border: "1px solid #e0e7ff",
            }}>
                {/* Header */}
                <div style={{
                    background: "linear-gradient(135deg, #eef2ff 0%, #e0e7ff 100%)",
                    padding: "12px 18px 10px", position: "relative",
                }}>
                    <button onClick={onClose} style={{
                        position: "absolute", top: 8, right: 12, border: "none", background: "transparent",
                        cursor: "pointer", fontSize: "1.1rem", color: "#a5b4fc",
                    }}>&times;</button>
                    <div style={{ fontSize: "1.2rem", marginBottom: 2, lineHeight: 1 }}>👋</div>
                    <h3 style={{ margin: 0, color: "#4338ca", fontSize: "0.88rem", fontWeight: 600 }}>
                        {t("来，配置一下 MaClaw 吧", "Let's get MaClaw ready!")}
                    </h3>
                    <p style={{ margin: "2px 0 0 0", fontSize: "0.7rem", color: "#6366f1" }}>
                        {isLite ? t("三步快速上手。", "Three quick steps to get started.") : t("四步快速上手。", "Four quick steps to get started.")}
                    </p>
                </div>

                <div style={{ padding: "10px 18px 14px" }}>
                    {/* ═══ Step 1: Choose UI Mode ═══ */}
                    <div style={{ marginBottom: 12 }}>
                        <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 6 }}>
                            <span style={modeDone ? doneBadge : stepBadge}>{modeDone ? "✓" : "1"}</span>
                            <span style={{ fontSize: "0.82rem", fontWeight: 600, color: "#1e293b" }}>
                                {t("选择界面模式", "Choose Interface Mode")}
                            </span>
                            {modeDone && <span style={{ fontSize: "0.7rem", color: "#22c55e", marginLeft: "auto" }}>
                                {t("已选择", "Selected")}
                            </span>}
                        </div>
                        <div style={{ display: "flex", gap: 8, marginLeft: 30 }}>
                            <div
                                onClick={() => { setSelectedMode('pro'); onSaveField({ ui_mode: 'pro' }); setModeDone(true); }}
                                style={{
                                    flex: 1, padding: "10px 12px", borderRadius: 8, cursor: "pointer",
                                    border: `2px solid ${selectedMode === 'pro' ? '#6366f1' : '#e2e8f0'}`,
                                    background: selectedMode === 'pro' ? 'rgba(99,102,241,0.06)' : '#f8fafc',
                                    transition: "all 0.15s",
                                }}
                            >
                                <div style={{ fontSize: "0.82rem", fontWeight: 600, color: selectedMode === 'pro' ? '#4338ca' : '#334155', marginBottom: 2 }}>
                                    🛠️ {t("专业模式", "Pro")}
                                </div>
                                <div style={{ fontSize: "0.68rem", color: "#94a3b8", lineHeight: 1.4 }}>
                                    {t("包含完整编程工具链，适合开发者", "Full coding toolchain for developers")}
                                </div>
                            </div>
                            <div
                                onClick={() => { setSelectedMode('lite'); onSaveField({ ui_mode: 'lite' }); setModeDone(true); }}
                                style={{
                                    flex: 1, padding: "10px 12px", borderRadius: 8, cursor: "pointer",
                                    border: `2px solid ${selectedMode === 'lite' ? '#6366f1' : '#e2e8f0'}`,
                                    background: selectedMode === 'lite' ? 'rgba(99,102,241,0.06)' : '#f8fafc',
                                    transition: "all 0.15s",
                                }}
                            >
                                <div style={{ fontSize: "0.82rem", fontWeight: 600, color: selectedMode === 'lite' ? '#4338ca' : '#334155', marginBottom: 2 }}>
                                    ✨ {t("简洁模式", "Lite")}
                                </div>
                                <div style={{ fontSize: "0.68rem", color: "#94a3b8", lineHeight: 1.4 }}>
                                    {t("专注 AI 助手与技能扩展，隐藏编程工具", "AI assistant & skills, coding tools hidden")}
                                </div>
                            </div>
                        </div>

                    </div>

                    {/* Divider */}
                    <div style={{ height: 1, background: "#e0e7ff", margin: "0 0 12px 0" }} />
                    {/* ═══ Step 2: Configure LLM ═══ */}
                    <div style={{ marginBottom: 12 }}>
                        <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 6 }}>
                            <span style={llmDone ? doneBadge : stepBadge}>{llmDone ? "✓" : "2"}</span>
                            <span style={{ fontSize: "0.82rem", fontWeight: 600, color: "#1e293b" }}>
                                {t("配置 LLM", "Configure LLM")}
                            </span>
                            {llmDone && <span style={{ fontSize: "0.7rem", color: "#22c55e", marginLeft: "auto" }}>
                                {t("已完成", "Done")}
                            </span>}
                        </div>
                        <p style={{ margin: "0 0 6px 30px", fontSize: "0.7rem", color: "#94a3b8", lineHeight: 1.3 }}>
                            {t("选择一个 LLM 服务商，输入 API Key 后测试并保存。", "Pick a provider, enter your API Key, then test & save.")}
                        </p>

                        {/* Provider buttons */}
                        <div style={{ display: "flex", gap: 6, marginLeft: 30, marginBottom: 8, flexWrap: "wrap" }}>
                            {providers.map((p, i) => {
                                const active = selectedIdx === i;
                                return (
                                    <div key={i} style={{ textAlign: "center" }}>
                                        <button onClick={() => { setSelectedIdx(active ? null : i); setLlmResult(null); }} style={{
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
                                    </div>
                                );
                            })}
                        </div>

                        {/* Inline config form */}
                        {selectedProvider && llmFormVisible && (
                            <div style={{
                                marginLeft: 30, padding: 14, borderRadius: 8,
                                border: "1px solid #e2e8f0", background: "#f8fafc",
                            }}>
                                {selectedProvider.auth_type === "oauth" ? (
                                    <>
                                        <p style={{ fontSize: "0.76rem", color: "#64748b", margin: "0 0 12px 0", lineHeight: 1.4 }}>
                                            {t("点击下方按钮，将在浏览器中完成 OpenAI 账号授权。", "Click below to authorize with your OpenAI account in the browser.")}
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

                                        {/* API Key */}
                                        <div style={{ marginBottom: 12 }}>
                                            <label style={labelStyle}>API Key <span style={{ color: "#ef4444" }}>*</span></label>
                                            <input style={inputStyle} type="password" value={selectedProvider.key}
                                                onChange={e => updateField("key", e.target.value)}
                                                placeholder={selectedProvider.is_custom ? "sk-..." : (selectedProvider.name === "智谱" ? "xxxxxxxx.yyyyyyyy" : "sk-...")}
                                                autoComplete="off" />
                                        </div>

                                        {/* Test & Save */}
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

                    {/* Divider */}
                    <div style={{ height: 1, background: "#e0e7ff", margin: "0 0 12px 0" }} />

                    {/* ═══ Step 3: Mobile Registration (pro mode only) ═══ */}
                    {!isLite && <div style={{ marginBottom: 10 }}>
                        <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 6 }}>
                            <span style={regDone ? doneBadge : stepBadge}>{regDone ? "✓" : "3"}</span>
                            <span style={{ fontSize: "0.82rem", fontWeight: 600, color: "#1e293b" }}>
                                {t("移动端注册", "Mobile Registration")}
                            </span>
                            {regDone && <span style={{ fontSize: "0.7rem", color: "#22c55e", marginLeft: "auto" }}>
                                {t("已完成", "Done")}
                            </span>}
                        </div>
                        <p style={{ margin: "0 0 8px 30px", fontSize: "0.7rem", color: "#94a3b8", lineHeight: 1.3 }}>
                            {t("注册设备到 Hub，即可通过移动端操控。", "Register your device to enable remote control.")}
                        </p>

                        <div style={{ marginLeft: 30 }}>
                            {/* Email */}
                            <div style={{ marginBottom: 10 }}>
                                <label style={labelStyle}>{t("邮箱", "Email")} <span style={{ color: "#ef4444" }}>*</span></label>
                                <input style={inputStyle} value={regEmail}
                                    onChange={e => setRegEmail(e.target.value)}
                                    placeholder="name@example.com" spellCheck={false} />
                            </div>

                            {/* Invitation code */}
                            {invRequired && (
                                <div style={{ marginBottom: 10 }}>
                                    <label style={labelStyle}>{t("邀请码", "Invitation Code")} <span style={{ fontSize: "0.68rem", color: "#94a3b8" }}>({t("可选", "optional")})</span></label>
                                    <input style={{ ...inputStyle, ...(invError ? { borderColor: "#ef4444" } : {}) }}
                                        value={invCode} onChange={e => { setInvCode(e.target.value.toUpperCase()); setInvError(""); }}
                                        placeholder={t("请输入邀请码", "Enter invitation code")} maxLength={10} spellCheck={false} />
                                    {invError && <div style={{ fontSize: "0.72rem", color: "#ef4444", marginTop: 4 }}>{invError}</div>}
                                </div>
                            )}

                            {/* Register button */}
                            <button onClick={handleRegisterClick} disabled={regBusy || regDone} style={{
                                width: "100%", padding: "8px 0", fontSize: "0.8rem", fontWeight: 600,
                                background: regBusy ? "#a5b4fc" : regDone ? "#86efac" : "#6366f1",
                                color: "#fff", border: "none", borderRadius: 6,
                                cursor: regBusy || regDone ? "default" : "pointer",
                            }}>
                                {regBusy ? t("注册中...", "Registering...") : regDone ? t("已注册", "Registered") : t("注册", "Register")}
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
                    </div>}

                    {/* Divider */}
                    {!isLite && <div style={{ height: 1, background: "#e0e7ff", margin: "0 0 12px 0" }} />}

                    {/* ═══ Step 3/4: WeChat Binding ═══ */}
                    <div style={{ marginBottom: 10 }}>
                        <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 6 }}>
                            <span style={wxDone ? doneBadge : stepBadge}>{wxDone ? "✓" : isLite ? "3" : "4"}</span>
                            <span style={{ fontSize: "0.82rem", fontWeight: 600, color: "#1e293b" }}>
                                {t("绑定微信", "Bind WeChat")}
                            </span>
                            {wxDone && <span style={{ fontSize: "0.7rem", color: "#22c55e", marginLeft: "auto" }}>
                                {t("已绑定", "Connected")}
                            </span>}
                        </div>
                        <p style={{ margin: "0 0 8px 30px", fontSize: "0.7rem", color: "#94a3b8", lineHeight: 1.3 }}>
                            {t("扫码绑定微信，即可通过微信与 MaClaw 交互。", "Scan to bind WeChat for messaging with MaClaw.")}
                        </p>

                        {!wxDone && (
                            <div style={{ marginLeft: 30 }}>
                                {!wxQrUrl && wxStatus !== "error" && (
                                    <button onClick={startWxQR} disabled={wxLoading} style={{
                                        width: "100%", padding: "8px 0", fontSize: "0.8rem", fontWeight: 600,
                                        background: wxLoading ? "#a5b4fc" : "#6366f1", color: "#fff",
                                        border: "none", borderRadius: 6, cursor: wxLoading ? "default" : "pointer",
                                    }}>
                                        {wxLoading ? t("获取中...", "Loading...") : t("显示二维码", "Show QR Code")}
                                    </button>
                                )}

                                {wxQrUrl && wxStatus !== "expired" && wxStatus !== "error" && (
                                    <div style={{ textAlign: "center" }}>
                                        <QRCodeSVG value={wxQrUrl} size={180} style={{
                                            borderRadius: 8,
                                            border: "1px solid #e2e8f0",
                                        }} />
                                    </div>
                                )}

                                {(wxStatus === "expired" || wxStatus === "error") && (
                                    <button onClick={startWxQR} disabled={wxLoading} style={{
                                        width: "100%", padding: "8px 0", fontSize: "0.8rem", fontWeight: 600,
                                        background: wxLoading ? "#a5b4fc" : "#6366f1", color: "#fff",
                                        border: "none", borderRadius: 6, cursor: wxLoading ? "default" : "pointer",
                                    }}>
                                        {t("刷新二维码", "Refresh QR Code")}
                                    </button>
                                )}

                                {wxMsg && (
                                    <div style={{
                                        marginTop: 8, padding: "6px 10px", borderRadius: 4, fontSize: "0.74rem",
                                        textAlign: "center", color: wxStatus === "error" || wxStatus === "expired" ? "#ef4444" : wxStatus === "scaned" ? "#b45309" : "#64748b",
                                    }}>
                                        {wxMsg}
                                    </div>
                                )}
                            </div>
                        )}
                    </div>

                    {/* Footer hint */}
                    <p style={{ margin: "6px 0 0 0", fontSize: "0.66rem", color: "#b0b8c9", lineHeight: 1.4 }}>
                        💡 {isLite
                        ? t("左侧 AI 助手圆圈全亮，说明一切就绪。三步都完成后，下次启动将不再显示此向导。",
                            "The AI assistant ring lights up once you're all set. This wizard won't appear again after all steps are done.")
                        : t("左侧 AI 助手圆圈全亮，说明一切就绪。四步都完成后，下次启动将不再显示此向导。",
                            "The AI assistant ring lights up once you're all set. This wizard won't appear again after all steps are done.")}
                    </p>
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
                            <div><span style={{ color: "#64748b" }}>{t("邮箱", "Email")}:</span> <span style={{ fontWeight: 600 }}>{regEmail}</span></div>
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

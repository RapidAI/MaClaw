import { useState, useEffect, useCallback } from "react";
import {
    GetMaclawLLMProviders,
    SaveMaclawLLMProviders,
    TestMaclawLLM,
    GetMaclawAgentMaxIterations,
    SetMaclawAgentMaxIterations,
} from "../../../wailsjs/go/main/App";
import { colors } from "./styles";

interface LLMProvider {
    name: string;
    url: string;
    key: string;
    model: string;
    protocol?: string; // "openai" (default) or "anthropic"
    context_length?: number; // max context tokens (0 = default 128k)
    is_custom?: boolean;
}

const NONE_PROVIDER = "__none__";

/** Known OpenAI-compatible providers for quick-fill in custom provider config. */
const KNOWN_OPENAI_ENDPOINTS: { name: string; url: string; model: string; context_length?: number }[] = [
    { name: "OpenAI Official", url: "https://api.openai.com/v1", model: "gpt-4o", context_length: 128000 },
    { name: "DeepSeek", url: "https://api.deepseek.com/v1", model: "deepseek-chat", context_length: 128000 },
    { name: "GLM (智谱)", url: "https://open.bigmodel.cn/api/paas/v4", model: "glm-4.7", context_length: 200000 },
    { name: "Kimi (月之暗面)", url: "https://api.kimi.com/coding/v1", model: "kimi-k2-thinking", context_length: 128000 },
    { name: "Doubao (豆包)", url: "https://ark.cn-beijing.volces.com/api/coding", model: "doubao-seed-code-preview-latest", context_length: 128000 },
    { name: "MiniMax", url: "https://api.minimaxi.com/v1", model: "MiniMax-M2.1", context_length: 128000 },
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

type Props = { lang: string; codexModels?: any[]; onStatusChange?: (online: boolean, configured: boolean) => void };

export function LLMConfigPanel({ lang, onStatusChange }: Props) {
    const [providers, setProviders] = useState<LLMProvider[]>([]);
    const [currentName, setCurrentName] = useState(NONE_PROVIDER);
    const [loading, setLoading] = useState(false);
    const [maxIter, setMaxIter] = useState(12);

    // Dialog state — track selected provider by index (stable across renames)
    const [dlgOpen, setDlgOpen] = useState(false);
    const [dlgProviders, setDlgProviders] = useState<LLMProvider[]>([]);
    const [dlgSelectedIdx, setDlgSelectedIdx] = useState<number | null>(null); // null = "None"
    const [dlgSaving, setDlgSaving] = useState(false);
    const [dlgTestResult, setDlgTestResult] = useState<{ ok: boolean; msg: string } | null>(null);
    const [dlgDirty, setDlgDirty] = useState(false);

    const t = useCallback((zh: string, en: string) => lang?.startsWith("zh") ? zh : en, [lang]);

    const loadProviders = useCallback(async () => {
        setLoading(true);
        try {
            const data = await GetMaclawLLMProviders();
            if (data?.providers) { setProviders(data.providers); setCurrentName(data.current || NONE_PROVIDER); }
            const iter = await GetMaclawAgentMaxIterations();
            setMaxIter(typeof iter === "number" ? iter : 12);
        } catch { /* ignore */ }
        setLoading(false);
    }, []);

    useEffect(() => { loadProviders(); }, [loadProviders]);

    const isNone = currentName === NONE_PROVIDER;

    /* ── Dialog helpers ── */

    const openDialog = useCallback(() => {
        const snapshot = providers.map(p => ({ ...p }));
        setDlgProviders(snapshot);
        const idx = currentName === NONE_PROVIDER ? null : snapshot.findIndex(p => p.name === currentName);
        setDlgSelectedIdx(idx === -1 ? null : idx);
        setDlgSaving(false);
        setDlgTestResult(null);
        setDlgDirty(false);
        setDlgOpen(true);
    }, [providers, currentName]);

    const closeDialog = useCallback(() => {
        if (dlgDirty && !dlgSaving) {
            if (!window.confirm(t("有未保存的更改，确定关闭？", "Unsaved changes. Close anyway?"))) return;
        }
        setDlgOpen(false);
    }, [dlgDirty, dlgSaving, t]);

    // Escape key to close dialog
    useEffect(() => {
        if (!dlgOpen) return;
        const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") closeDialog(); };
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [dlgOpen, closeDialog]);

    const dlgIsNone = dlgSelectedIdx === null;
    const dlgProvider = dlgSelectedIdx !== null ? dlgProviders[dlgSelectedIdx] ?? null : null;

    const dlgUpdateField = useCallback((field: keyof LLMProvider, value: string) => {
        if (dlgSelectedIdx === null) return;
        setDlgProviders(prev => {
            const copy = [...prev];
            copy[dlgSelectedIdx] = { ...copy[dlgSelectedIdx], [field]: value };
            return copy;
        });
        setDlgDirty(true);
        setDlgTestResult(null);
    }, [dlgSelectedIdx]);

    const dlgSelectProvider = useCallback((idx: number | null) => {
        setDlgSelectedIdx(idx);
        setDlgDirty(true);
        setDlgTestResult(null);
    }, []);

    const dlgQuickFill = useCallback((epName: string) => {
        const ep = KNOWN_OPENAI_ENDPOINTS.find(x => x.name === epName);
        if (!ep || dlgSelectedIdx === null) return;
        setDlgProviders(prev => {
            const copy = [...prev];
            copy[dlgSelectedIdx] = { ...copy[dlgSelectedIdx], name: ep.name, url: ep.url, model: ep.model, protocol: "openai", context_length: ep.context_length || 128000 };
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
            } catch (e) { alert(String(e)); }
            setDlgSaving(false);
            return;
        }
        const sp = dlgProviders[dlgSelectedIdx!];
        if (!sp) return;
        setDlgSaving(true);
        setDlgTestResult(null);
        try {
            const reply = await TestMaclawLLM({ url: sp.url, key: sp.key, model: sp.model, protocol: sp.protocol || "openai" });
            try {
                const saveName = sp.name;
                await SaveMaclawLLMProviders(dlgProviders, saveName);
                setDlgDirty(false);
                setDlgTestResult({ ok: true, msg: reply });
                setProviders(dlgProviders.map(p => ({ ...p })));
                setCurrentName(saveName);
                onStatusChange?.(true, true);
                // Auto-close after brief delay so user sees the success message
                setTimeout(() => setDlgOpen(false), 1200);
            } catch (e) {
                setDlgTestResult({ ok: false, msg: t("连接正常但保存失败: ", "Connection OK but save failed: ") + String(e) });
            }
        } catch (e) {
            setDlgTestResult({ ok: false, msg: String(e) });
        }
        setDlgSaving(false);
    };

    if (loading) return <div style={{ padding: 16, color: "#888" }}>{t("加载中...", "Loading...")}</div>;

    return (
        <div style={{ padding: "0 4px" }}>
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 12 }}>
                <h4 style={{ fontSize: "0.8rem", color: "#6366f1", margin: 0, textTransform: "uppercase", letterSpacing: "0.025em" }}>
                    {t("MaClaw LLM 配置", "MaClaw LLM Configuration")}
                </h4>
                <button onClick={openDialog} style={{
                    fontSize: "0.76rem", padding: "6px 18px", cursor: "pointer",
                    background: "#6366f1", color: "#fff", border: "none", borderRadius: 4, flexShrink: 0,
                }}>
                    {t("配置", "Configure")}
                </button>
            </div>
            <p style={{ fontSize: "0.72rem", color: "#888", marginBottom: 16, lineHeight: 1.5 }}>
                {t(
                    "选择 MaClaw 桌面代理使用的 LLM 服务商（支持 OpenAI 兼容接口和 Anthropic 协议）。",
                    "Select the LLM provider for the MaClaw desktop agent (OpenAI-compatible and Anthropic protocols supported)."
                )}
            </p>

            {/* Current provider summary */}
            <div style={{
                marginBottom: 16, padding: "10px 16px", borderRadius: 6,
                border: `1px solid ${colors.border}`, background: colors.surface,
                display: "flex", justifyContent: "space-between", alignItems: "center",
            }}>
                <span style={{ fontSize: "0.76rem", color: colors.textSecondary }}>
                    {t("当前服务商", "Provider")}
                </span>
                <span style={{ fontSize: "0.76rem", fontWeight: 600, color: isNone ? "#ef4444" : colors.text }}>
                    {isNone ? t("暂不配置", "None") : currentName}
                </span>
            </div>

            {/* Max iterations — inline editable */}
            <div style={{
                marginBottom: 16, padding: "12px 16px", borderRadius: 6,
                border: `1px solid ${colors.border}`, background: colors.surface,
            }}>
                <label style={{ ...labelStyle, marginBottom: 2 }}>{t("Agent 最大推理轮数", "Agent Max Iterations")}</label>
                <p style={{ fontSize: "0.7rem", color: "#888", margin: "0 0 8px 0", lineHeight: 1.4 }}>
                    {t(
                        "Agent 每次对话最多执行的推理轮数。设为 0 则由 Agent 自行判断（不限制）。默认 12。",
                        "Max reasoning rounds per conversation. Set to 0 for unlimited (agent decides). Default 12."
                    )}
                </p>
                <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
                    <input type="range" min={0} max={300} step={1} value={maxIter}
                        onChange={e => { const v = Number(e.target.value); setMaxIter(v); SetMaclawAgentMaxIterations(v).catch(() => {}); }}
                        style={{ flex: 1, accentColor: "#6366f1" }} />
                    <input type="number" min={0} max={300} value={maxIter}
                        onChange={e => { const v = Math.max(0, Math.min(300, Number(e.target.value) || 0)); setMaxIter(v); SetMaclawAgentMaxIterations(v).catch(() => {}); }}
                        style={{ ...inputStyle, width: 60, textAlign: "center" as const }} />
                    <span style={{ fontSize: "0.72rem", color: colors.textSecondary, whiteSpace: "nowrap" }}>
                        {maxIter === 0 ? t("不限制", "Unlimited") : `${maxIter} ${t("轮", "rounds")}`}
                    </span>
                </div>
            </div>

            {isNone && (
                <div style={{
                    padding: "8px 12px", borderRadius: 4, fontSize: "0.74rem", lineHeight: 1.5,
                    background: "rgba(239,68,68,0.08)", border: "1px solid rgba(239,68,68,0.25)", color: "#ef4444",
                }}>
                    ⚠️ {t("不配置服务商，MaClaw 远程将失效。", "Without a provider, MaClaw remote will be disabled.")}
                </div>
            )}

            {/* ── Config Dialog ── */}
            {dlgOpen && (
                <div style={{
                    position: "fixed", top: 0, left: 0, right: 0, bottom: 0,
                    background: "rgba(0,0,0,0.4)", display: "flex",
                    alignItems: "center", justifyContent: "center", zIndex: 9999,
                }} onClick={closeDialog}>
                    <div style={{
                        background: colors.surface, borderRadius: 12, padding: "24px 28px",
                        maxWidth: 520, width: "92%", maxHeight: "85vh", overflowY: "auto",
                        boxShadow: "0 16px 48px rgba(0,0,0,0.22)",
                    }} onClick={e => e.stopPropagation()}>

                        {/* Header */}
                        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 18 }}>
                            <span style={{ fontSize: "0.92rem", fontWeight: 700, color: colors.text }}>
                                {t("MaClaw LLM 配置", "MaClaw LLM Configuration")}
                            </span>
                            <button onClick={closeDialog} style={{
                                border: "none", background: "transparent", cursor: "pointer",
                                fontSize: "1.1rem", color: colors.textSecondary, padding: "0 4px",
                            }}>✕</button>
                        </div>

                        {/* Provider selection */}
                        <div style={{ marginBottom: 16 }}>
                            <label style={labelStyle}>{t("选择服务商", "Select Provider")}</label>
                            <div style={{ display: "flex", gap: 4, flexWrap: "wrap" }}>
                                {dlgProviders.map((p, i) => {
                                    const active = dlgSelectedIdx === i;
                                    return (
                                        <button key={i} onClick={() => dlgSelectProvider(i)} style={{
                                            fontSize: "0.76rem", padding: "5px 14px", cursor: "pointer",
                                            background: active ? "#6366f1" : colors.surface,
                                            color: active ? "#fff" : colors.text,
                                            border: `1px solid ${active ? "#6366f1" : colors.border}`,
                                            borderRadius: 4, transition: "all 0.15s",
                                        }}>
                                            {p.name}
                                        </button>
                                    );
                                })}
                                {/* "None" button */}
                                <button onClick={() => dlgSelectProvider(null)} style={{
                                    fontSize: "0.76rem", padding: "5px 14px", cursor: "pointer",
                                    background: dlgIsNone ? "#6366f1" : colors.surface,
                                    color: dlgIsNone ? "#fff" : colors.text,
                                    border: `1px solid ${dlgIsNone ? "#6366f1" : colors.border}`,
                                    borderRadius: 4, transition: "all 0.15s",
                                }}>
                                    {t("暂不配置", "None")}
                                </button>
                            </div>
                        </div>

                        {/* None warning */}
                        {dlgIsNone && (
                            <div style={{
                                padding: "8px 12px", borderRadius: 4, fontSize: "0.74rem", lineHeight: 1.5,
                                background: "rgba(239,68,68,0.08)", border: "1px solid rgba(239,68,68,0.25)", color: "#ef4444",
                                marginBottom: 16,
                            }}>
                                ⚠️ {t("不配置服务商，MaClaw 远程将失效。", "Without a provider, MaClaw remote will be disabled.")}
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
                                        ? t("自定义服务商配置", "Custom Provider Configuration")
                                        : `${dlgProvider.name} ${t("配置", "Configuration")}`}
                                </div>

                                {/* Custom: quick-fill from known endpoints */}
                                {dlgProvider.is_custom && (
                                    <div style={{ marginBottom: 12 }}>
                                        <label style={labelStyle}>{t("从已知服务商快速填充", "Quick-fill from known provider")}</label>
                                        <select
                                            style={{ ...inputStyle, cursor: "pointer" }}
                                            value=""
                                            onChange={e => dlgQuickFill(e.target.value)}
                                        >
                                            <option value="">{t("-- 选择已知服务商自动填充 --", "-- Select a known provider to auto-fill --")}</option>
                                            {KNOWN_OPENAI_ENDPOINTS.map(ep => (
                                                <option key={ep.name} value={ep.name}>{ep.name} — {ep.model}</option>
                                            ))}
                                        </select>
                                    </div>
                                )}

                                {/* Protocol selection — only for custom providers */}
                                {dlgProvider.is_custom && (
                                    <div style={{ marginBottom: 12 }}>
                                        <label style={labelStyle}>{t("API 协议", "API Protocol")}</label>
                                        <div style={{ display: "flex", gap: 6 }}>
                                            {(["openai", "anthropic"] as const).map(proto => {
                                                const active = (dlgProvider.protocol || "openai") === proto;
                                                return (
                                                    <button key={proto} onClick={() => dlgUpdateField("protocol", proto)} style={{
                                                        fontSize: "0.76rem", padding: "5px 16px", cursor: "pointer",
                                                        background: active ? "#6366f1" : colors.surface,
                                                        color: active ? "#fff" : colors.text,
                                                        border: `1px solid ${active ? "#6366f1" : colors.border}`,
                                                        borderRadius: 4, transition: "all 0.15s",
                                                    }}>
                                                        {proto === "openai" ? "OpenAI" : "Anthropic"}
                                                    </button>
                                                );
                                            })}
                                        </div>
                                        <p style={{ fontSize: "0.68rem", color: colors.textMuted, margin: "4px 0 0 0", lineHeight: 1.4 }}>
                                            {(dlgProvider.protocol || "openai") === "anthropic"
                                                ? t("使用 Anthropic Messages API（x-api-key 鉴权）", "Uses Anthropic Messages API (x-api-key auth)")
                                                : t("使用 OpenAI 兼容接口（Bearer Token 鉴权）", "Uses OpenAI-compatible API (Bearer token auth)")}
                                        </p>
                                    </div>
                                )}

                                {/* Custom: editable name */}
                                {dlgProvider.is_custom && (
                                    <div style={{ marginBottom: 12 }}>
                                        <label style={labelStyle}>{t("服务商名称", "Provider Name")}</label>
                                        <input style={inputStyle} value={dlgProvider.name}
                                            onChange={e => dlgUpdateField("name", e.target.value)}
                                            placeholder={t("自定义名称", "Custom name")} />
                                    </div>
                                )}

                                {/* URL */}
                                <div style={{ marginBottom: 12 }}>
                                    <label style={labelStyle}>
                                        {t("API 地址 (URL)", "API URL")}
                                        {!dlgProvider.is_custom && (
                                            <span style={{ fontSize: "0.68rem", color: colors.textMuted, marginLeft: 6 }}>
                                                {t("（预设，无需修改）", "(preset)")}
                                            </span>
                                        )}
                                    </label>
                                    {dlgProvider.is_custom ? (
                                        <input style={inputStyle} value={dlgProvider.url}
                                            onChange={e => dlgUpdateField("url", e.target.value)}
                                            placeholder="https://api.openai.com/v1" />
                                    ) : (
                                        <input style={readonlyStyle} value={dlgProvider.url} readOnly tabIndex={-1} />
                                    )}
                                </div>

                                {/* Model */}
                                <div style={{ marginBottom: 12 }}>
                                    <label style={labelStyle}>
                                        {t("模型名称", "Model Name")}
                                        {!dlgProvider.is_custom && (
                                            <span style={{ fontSize: "0.68rem", color: colors.textMuted, marginLeft: 6 }}>
                                                {t("（预设，无需修改）", "(preset)")}
                                            </span>
                                        )}
                                    </label>
                                    {dlgProvider.is_custom ? (
                                        <input style={inputStyle} value={dlgProvider.model}
                                            onChange={e => dlgUpdateField("model", e.target.value)}
                                            placeholder="gpt-4o" />
                                    ) : (
                                        <input style={readonlyStyle} value={dlgProvider.model} readOnly tabIndex={-1} />
                                    )}
                                </div>

                                {/* API Key — always editable */}
                                <div>
                                    <label style={labelStyle}>{t("API 密钥", "API Key")} <span style={{ color: "#ef4444" }}>*</span></label>
                                    <input style={inputStyle} type="password" value={dlgProvider.key}
                                        onChange={e => dlgUpdateField("key", e.target.value)}
                                        placeholder={(dlgProvider.protocol || "openai") === "anthropic" ? "sk-ant-..." : "sk-..."} autoComplete="off" />
                                </div>

                                {/* Context Length */}
                                <div style={{ marginTop: 12 }}>
                                    <label style={labelStyle}>{t("上下文长度 (tokens)", "Context Length (tokens)")}</label>
                                    <input style={inputStyle} type="number" min={0} step={1000}
                                        value={dlgProvider.context_length || ""}
                                        onChange={e => dlgUpdateField("context_length", e.target.value ? String(parseInt(e.target.value, 10) || 0) : "0")}
                                        placeholder="128000" />
                                    <p style={{ fontSize: "0.68rem", color: colors.textMuted, margin: "4px 0 0 0", lineHeight: 1.4 }}>
                                        {t(
                                            "模型支持的最大上下文长度。智谱 GLM 为 200000，留空默认 128000。",
                                            "Max context window of the model. GLM supports 200000. Defaults to 128000 if empty."
                                        )}
                                    </p>
                                </div>
                            </div>
                        )}


                        {/* Footer */}
                        <div style={{ display: "flex", gap: 10, alignItems: "center", justifyContent: "flex-end", marginTop: 20 }}>
                            {dlgDirty && <span style={{ fontSize: "0.68rem", color: "#f59e0b", marginRight: "auto" }}>{t("未保存", "unsaved")}</span>}
                            <button onClick={closeDialog} style={{
                                fontSize: "0.76rem", padding: "6px 18px", cursor: "pointer",
                                background: colors.bg, color: colors.text,
                                border: `1px solid ${colors.border}`, borderRadius: 4,
                            }}>
                                {t("取消", "Cancel")}
                            </button>
                            <button onClick={dlgHandleSave} disabled={dlgSaving || !dlgDirty} style={{
                                fontSize: "0.76rem", padding: "6px 18px", cursor: dlgDirty ? "pointer" : "default",
                                background: dlgDirty ? "#6366f1" : colors.bg, color: dlgDirty ? "#fff" : colors.textMuted,
                                border: "none", borderRadius: 4, opacity: dlgSaving ? 0.6 : 1,
                            }}>
                                {dlgSaving ? t("测试并保存中...", "Testing & Saving...") : t("保存", "Save")}
                            </button>
                        </div>

                        {/* Test result */}
                        {dlgTestResult && (
                            <div style={{
                                marginTop: 12, padding: "8px 12px", borderRadius: 4, fontSize: "0.74rem",
                                lineHeight: 1.5, whiteSpace: "pre-wrap", wordBreak: "break-word",
                                background: dlgTestResult.ok ? "rgba(34,197,94,0.1)" : "rgba(239,68,68,0.1)",
                                border: `1px solid ${dlgTestResult.ok ? "rgba(34,197,94,0.3)" : "rgba(239,68,68,0.3)"}`,
                                color: dlgTestResult.ok ? "#22c55e" : "#ef4444",
                            }}>
                                {dlgTestResult.ok
                                    ? `✅ ${t("连接成功，已保存", "Connection OK, saved")}\n${dlgTestResult.msg}`
                                    : `❌ ${t("连接失败，未保存", "Connection failed, not saved")}\n${dlgTestResult.msg}`}
                            </div>
                        )}
                    </div>
                </div>
            )}
        </div>
    );
}

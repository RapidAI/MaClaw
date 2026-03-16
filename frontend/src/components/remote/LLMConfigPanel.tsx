import { useState, useEffect, useCallback } from "react";
import {
    GetMaclawLLMConfig,
    SaveMaclawLLMConfig,
    TestMaclawLLM,
} from "../../../wailsjs/go/main/App";
import { colors } from "./styles";

interface LLMConfig {
    url: string;
    key: string;
    model: string;
}

// OpenAI-compatible providers that can be copied into the LLM config.
const codexProviders = [
    { name: "OpenAI Official", url: "https://api.openai.com/v1", models: ["gpt-4o", "gpt-4o-mini", "gpt-4.1-mini"] },
    { name: "xAI (Grok)", url: "https://api.x.ai/v1", models: ["grok-3-mini"] },
    { name: "GLM", url: "https://open.bigmodel.cn/api/paas/v4", models: ["glm-4.7"] },
    { name: "Kimi", url: "https://api.kimi.com/coding/v1", models: ["kimi-k2-thinking"] },
    { name: "Doubao", url: "https://ark.cn-beijing.volces.com/api/coding", models: ["doubao-seed-code-preview-latest"] },
    { name: "DeepSeek", url: "https://api.deepseek.com/v1", models: ["deepseek-chat"] },
    { name: "腾讯云", url: "https://api.lkeap.cloud.tencent.com/coding/v3", models: ["glm-5", "hunyuan-2.0-instruct"] },
    { name: "OpenRouter", url: "https://openrouter.ai/api/v1", models: ["openai/gpt-4o"] },
    { name: "Together AI", url: "https://api.together.xyz/v1", models: [] },
    { name: "Groq", url: "https://api.groq.com/openai/v1", models: [] },
];

type Props = {
    lang: string;
};

export function LLMConfigPanel({ lang }: Props) {
    const [config, setConfig] = useState<LLMConfig>({ url: "", key: "", model: "" });
    const [loading, setLoading] = useState(false);
    const [saving, setSaving] = useState(false);
    const [testing, setTesting] = useState(false);
    const [testResult, setTestResult] = useState<{ ok: boolean; msg: string } | null>(null);
    const [dirty, setDirty] = useState(false);
    const [showProviders, setShowProviders] = useState(false);
    // When a provider with multiple models is selected, show a picker.
    const [pendingProvider, setPendingProvider] = useState<typeof codexProviders[0] | null>(null);

    const t = useCallback((zh: string, en: string) => lang?.startsWith("zh") ? zh : en, [lang]);

    const loadConfig = useCallback(async () => {
        setLoading(true);
        try {
            const cfg = await GetMaclawLLMConfig();
            if (cfg) setConfig({ url: cfg.url || "", key: cfg.key || "", model: cfg.model || "" });
        } catch { /* ignore */ }
        setLoading(false);
    }, []);

    useEffect(() => { loadConfig(); }, [loadConfig]);

    const handleSave = async () => {
        setSaving(true);
        try {
            await SaveMaclawLLMConfig(config);
            setDirty(false);
        } catch (e) {
            alert(String(e));
        }
        setSaving(false);
    };

    const handleTest = async () => {
        setTesting(true);
        setTestResult(null);
        try {
            const reply = await TestMaclawLLM(config);
            setTestResult({ ok: true, msg: reply });
        } catch (e) {
            setTestResult({ ok: false, msg: String(e) });
        }
        setTesting(false);
    };

    const applyProvider = (provider: typeof codexProviders[0], model?: string) => {
        setConfig(prev => ({
            ...prev,
            url: provider.url,
            model: model || provider.models[0] || prev.model,
        }));
        setDirty(true);
        setShowProviders(false);
        setPendingProvider(null);
    };

    const handleProviderClick = (provider: typeof codexProviders[0]) => {
        if (provider.models.length > 1) {
            setPendingProvider(provider);
        } else {
            applyProvider(provider);
        }
    };

    const update = (field: keyof LLMConfig, value: string) => {
        setConfig(prev => ({ ...prev, [field]: value }));
        setDirty(true);
        setTestResult(null);
    };

    if (loading) return <div style={{ padding: 16, color: "#888" }}>{t("加载中...", "Loading...")}</div>;

    const inputStyle: React.CSSProperties = {
        width: "100%", padding: "7px 10px", fontSize: "0.8rem",
        border: `1px solid ${colors.border}`, borderRadius: 4,
        background: colors.surface, color: colors.text,
        boxSizing: "border-box",
    };
    const labelStyle: React.CSSProperties = {
        fontSize: "0.76rem", color: colors.textSecondary, marginBottom: 4, display: "block",
    };
    const sectionStyle: React.CSSProperties = { marginBottom: 16 };
    const rowHover = {
        onMouseEnter: (e: React.MouseEvent<HTMLDivElement>) => (e.currentTarget.style.background = "rgba(99,102,241,0.1)"),
        onMouseLeave: (e: React.MouseEvent<HTMLDivElement>) => (e.currentTarget.style.background = "transparent"),
    };

    return (
        <div style={{ padding: "0 4px" }}>
            <h4 style={{ fontSize: "0.8rem", color: "#6366f1", marginBottom: 12, marginTop: 0, textTransform: "uppercase", letterSpacing: "0.025em" }}>
                {t("MaClaw LLM 配置", "MaClaw LLM Configuration")}
            </h4>
            <p style={{ fontSize: "0.72rem", color: "#888", marginBottom: 16, lineHeight: 1.5 }}>
                {t(
                    "配置 MaClaw 桌面代理使用的 LLM（OpenAI 兼容接口）。IM 消息经 Hub 透传到桌面端，由 MaClaw 进行意图分析和命令分发。",
                    "Configure the LLM (OpenAI-compatible) used by the MaClaw desktop agent. IM messages pass through the Hub to the desktop, where MaClaw handles intent analysis and command dispatch."
                )}
            </p>

            {/* Provider quick-fill */}
            <div style={sectionStyle}>
                <button
                    onClick={() => { setShowProviders(!showProviders); setPendingProvider(null); }}
                    style={{
                        fontSize: "0.74rem", padding: "5px 12px", cursor: "pointer",
                        background: colors.bg, color: colors.text,
                        border: `1px solid ${colors.border}`, borderRadius: 4,
                    }}
                >
                    {t("从已知服务商复制", "Copy from Provider")} {showProviders ? "▲" : "▼"}
                </button>

                {showProviders && !pendingProvider && (
                    <div style={{
                        marginTop: 8, border: `1px solid ${colors.border}`, borderRadius: 4,
                        background: colors.surface, maxHeight: 200, overflowY: "auto",
                    }}>
                        {codexProviders.map((p) => (
                            <div
                                key={p.name}
                                onClick={() => handleProviderClick(p)}
                                style={{
                                    padding: "6px 12px", cursor: "pointer", fontSize: "0.74rem",
                                    borderBottom: `1px solid ${colors.border}`,
                                    display: "flex", justifyContent: "space-between", alignItems: "center",
                                }}
                                {...rowHover}
                            >
                                <span style={{ fontWeight: 500 }}>{p.name}</span>
                                <span style={{ color: "#888", fontSize: "0.68rem" }}>
                                    {p.models.length > 1 ? `${p.models.length} models` : p.url}
                                </span>
                            </div>
                        ))}
                    </div>
                )}

                {/* Model picker for multi-model providers */}
                {showProviders && pendingProvider && (
                    <div style={{
                        marginTop: 8, border: `1px solid ${colors.border}`, borderRadius: 4,
                        background: colors.surface,
                    }}>
                        <div style={{ padding: "6px 12px", fontSize: "0.72rem", color: colors.textMuted, borderBottom: `1px solid ${colors.border}` }}>
                            <span style={{ cursor: "pointer", marginRight: 8 }} onClick={() => setPendingProvider(null)}>← </span>
                            {pendingProvider.name} — {t("选择模型", "Select Model")}
                        </div>
                        {pendingProvider.models.map((m) => (
                            <div
                                key={m}
                                onClick={() => applyProvider(pendingProvider, m)}
                                style={{
                                    padding: "6px 12px", cursor: "pointer", fontSize: "0.74rem",
                                    borderBottom: `1px solid ${colors.border}`,
                                }}
                                {...rowHover}
                            >
                                {m}
                            </div>
                        ))}
                    </div>
                )}
            </div>

            {/* URL */}
            <div style={sectionStyle}>
                <label style={labelStyle}>{t("API 地址 (URL)", "API URL")}</label>
                <input
                    style={inputStyle}
                    value={config.url}
                    onChange={e => update("url", e.target.value)}
                    placeholder="https://api.openai.com/v1"
                />
            </div>

            {/* API Key */}
            <div style={sectionStyle}>
                <label style={labelStyle}>{t("API 密钥", "API Key")}</label>
                <input
                    style={inputStyle}
                    type="password"
                    value={config.key}
                    onChange={e => update("key", e.target.value)}
                    placeholder="sk-..."
                />
            </div>

            {/* Model */}
            <div style={sectionStyle}>
                <label style={labelStyle}>{t("模型名称", "Model Name")}</label>
                <input
                    style={inputStyle}
                    value={config.model}
                    onChange={e => update("model", e.target.value)}
                    placeholder="gpt-4o"
                />
            </div>

            {/* Action buttons */}
            <div style={{ display: "flex", gap: 10, alignItems: "center", marginTop: 20 }}>
                <button
                    onClick={handleSave}
                    disabled={saving || !dirty}
                    style={{
                        fontSize: "0.76rem", padding: "6px 18px", cursor: dirty ? "pointer" : "default",
                        background: dirty ? "#6366f1" : colors.bg,
                        color: dirty ? "#fff" : colors.textMuted,
                        border: "none", borderRadius: 4, opacity: saving ? 0.6 : 1,
                    }}
                >
                    {saving ? t("保存中...", "Saving...") : t("保存", "Save")}
                </button>
                <button
                    onClick={handleTest}
                    disabled={testing || !config.url || !config.model}
                    style={{
                        fontSize: "0.76rem", padding: "6px 18px", cursor: "pointer",
                        background: colors.bg, color: colors.text,
                        border: `1px solid ${colors.border}`, borderRadius: 4,
                        opacity: (testing || !config.url || !config.model) ? 0.6 : 1,
                    }}
                >
                    {testing ? t("测试中...", "Testing...") : t("测试连接", "Test Connection")}
                </button>
                {dirty && (
                    <span style={{ fontSize: "0.68rem", color: "#f59e0b" }}>
                        {t("未保存", "unsaved")}
                    </span>
                )}
            </div>

            {/* Test result */}
            {testResult && (
                <div style={{
                    marginTop: 12, padding: "8px 12px", borderRadius: 4, fontSize: "0.74rem",
                    lineHeight: 1.5, whiteSpace: "pre-wrap", wordBreak: "break-word",
                    background: testResult.ok ? "rgba(34,197,94,0.1)" : "rgba(239,68,68,0.1)",
                    border: `1px solid ${testResult.ok ? "rgba(34,197,94,0.3)" : "rgba(239,68,68,0.3)"}`,
                    color: testResult.ok ? "#22c55e" : "#ef4444",
                }}>
                    {testResult.ok
                        ? `✅ ${t("连接成功", "Connection OK")}\n${testResult.msg}`
                        : `❌ ${t("连接失败", "Connection Failed")}\n${testResult.msg}`}
                </div>
            )}
        </div>
    );
}

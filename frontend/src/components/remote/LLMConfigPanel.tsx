import { useState, useEffect, useCallback } from "react";
import {
    GetMaclawLLMProviders,
    SaveMaclawLLMProviders,
    TestMaclawLLM,
} from "../../../wailsjs/go/main/App";
import { colors } from "./styles";

interface LLMProvider {
    name: string;
    url: string;
    key: string;
    model: string;
    is_custom?: boolean;
}

const NONE_PROVIDER = "__none__";

type Props = { lang: string; codexModels?: any[] };

export function LLMConfigPanel({ lang }: Props) {
    const [providers, setProviders] = useState<LLMProvider[]>([]);
    const [currentName, setCurrentName] = useState(NONE_PROVIDER);
    const [showConfig, setShowConfig] = useState(false);
    const [configIdx, setConfigIdx] = useState(-1);
    const [loading, setLoading] = useState(false);
    const [saving, setSaving] = useState(false);
    const [testing, setTesting] = useState(false);
    const [testResult, setTestResult] = useState<{ ok: boolean; msg: string } | null>(null);
    const [dirty, setDirty] = useState(false);

    const t = useCallback((zh: string, en: string) => lang?.startsWith("zh") ? zh : en, [lang]);

    const loadProviders = useCallback(async () => {
        setLoading(true);
        try {
            const data = await GetMaclawLLMProviders();
            if (data?.providers) {
                setProviders(data.providers);
                setCurrentName(data.current || NONE_PROVIDER);
            }
        } catch { /* ignore */ }
        setLoading(false);
    }, []);

    useEffect(() => { loadProviders(); }, [loadProviders]);

    const isNone = currentName === NONE_PROVIDER;
    const selectedProvider = providers.find(p => p.name === currentName);
    const providerConfigured = selectedProvider && selectedProvider.url && selectedProvider.model;

    const updateProviderField = (idx: number, field: keyof LLMProvider, value: string) => {
        setProviders(prev => {
            const copy = [...prev];
            copy[idx] = { ...copy[idx], [field]: value };
            return copy;
        });
        setDirty(true);
        setTestResult(null);
    };

    const handleSelectProvider = (name: string) => {
        setCurrentName(name);
        setDirty(true);
        setTestResult(null);
        if (name !== NONE_PROVIDER) {
            const idx = providers.findIndex(p => p.name === name);
            const p = providers[idx];
            if (idx >= 0 && (!p.url || !p.model)) {
                setConfigIdx(idx);
                setShowConfig(true);
            }
        }
    };

    const openConfig = (idx: number) => {
        setConfigIdx(idx);
        setShowConfig(true);
    };

    const handleSave = async () => {
        setSaving(true);
        try {
            await SaveMaclawLLMProviders(providers, currentName);
            setDirty(false);
        } catch (e) { alert(String(e)); }
        setSaving(false);
    };

    const handleTest = async () => {
        if (!selectedProvider) return;
        setTesting(true);
        setTestResult(null);
        try {
            const reply = await TestMaclawLLM({ url: selectedProvider.url, key: selectedProvider.key, model: selectedProvider.model });
            setTestResult({ ok: true, msg: reply });
        } catch (e) { setTestResult({ ok: false, msg: String(e) }); }
        setTesting(false);
    };

    if (loading) return <div style={{ padding: 16, color: "#888" }}>{t("加载中...", "Loading...")}</div>;

    const inputStyle: React.CSSProperties = {
        width: "100%", padding: "7px 10px", fontSize: "0.8rem",
        border: `1px solid ${colors.border}`, borderRadius: 4,
        background: colors.surface, color: colors.text, boxSizing: "border-box",
    };
    const labelStyle: React.CSSProperties = {
        fontSize: "0.76rem", color: colors.textSecondary, marginBottom: 4, display: "block",
    };

    // All selectable options: real providers + "暂不配置"
    const options = [
        ...providers.map(p => ({ name: p.name, isNone: false })),
        { name: NONE_PROVIDER, isNone: true },
    ];

    return (
        <div style={{ padding: "0 4px" }}>
            <h4 style={{ fontSize: "0.8rem", color: "#6366f1", marginBottom: 12, marginTop: 0, textTransform: "uppercase", letterSpacing: "0.025em" }}>
                {t("MaClaw LLM 配置", "MaClaw LLM Configuration")}
            </h4>
            <p style={{ fontSize: "0.72rem", color: "#888", marginBottom: 16, lineHeight: 1.5 }}>
                {t(
                    "选择 MaClaw 桌面代理使用的 LLM 服务商（OpenAI 兼容接口）。",
                    "Select the LLM provider (OpenAI-compatible) for the MaClaw desktop agent."
                )}
            </p>

            {/* Provider selection row */}
            <div style={{ marginBottom: 16 }}>
                <label style={labelStyle}>{t("选择服务商", "Select Provider")}</label>
                <div style={{ display: "flex", gap: 4, flexWrap: "wrap", alignItems: "center" }}>
                    {options.map(opt => {
                        const isActive = currentName === opt.name;
                        const displayName = opt.isNone ? t("暂不配置", "None") : opt.name;
                        return (
                            <button
                                key={opt.name}
                                onClick={() => handleSelectProvider(opt.name)}
                                style={{
                                    fontSize: "0.76rem", padding: "5px 14px", cursor: "pointer",
                                    background: isActive ? "#6366f1" : colors.surface,
                                    color: isActive ? "#fff" : colors.text,
                                    border: `1px solid ${isActive ? "#6366f1" : colors.border}`,
                                    borderRadius: 4, transition: "all 0.15s",
                                }}
                            >
                                {displayName}
                            </button>
                        );
                    })}
                </div>
            </div>

            {/* "暂不配置" warning */}
            {isNone && (
                <div style={{
                    padding: "8px 12px", borderRadius: 4, fontSize: "0.74rem", lineHeight: 1.5,
                    background: "rgba(239,68,68,0.08)", border: "1px solid rgba(239,68,68,0.25)", color: "#ef4444",
                    marginBottom: 16,
                }}>
                    ⚠️ {t("不配置服务商，MaClaw 远程将失效。", "Without a provider, MaClaw remote will be disabled.")}
                </div>
            )}

            {/* Current provider summary + config button (when not "暂不配置") */}
            {!isNone && selectedProvider && (
                <div style={{
                    marginBottom: 16, padding: "10px 14px", borderRadius: 6,
                    border: `1px solid ${colors.border}`, background: colors.surface,
                    display: "flex", justifyContent: "space-between", alignItems: "center",
                }}>
                    <div style={{ fontSize: "0.76rem", color: colors.text, lineHeight: 1.6 }}>
                        <div><span style={{ color: colors.textSecondary }}>URL: </span>{selectedProvider.url || <span style={{ color: "#f59e0b" }}>{t("未配置", "Not set")}</span>}</div>
                        <div><span style={{ color: colors.textSecondary }}>Model: </span>{selectedProvider.model || <span style={{ color: "#f59e0b" }}>{t("未配置", "Not set")}</span>}</div>
                        <div><span style={{ color: colors.textSecondary }}>Key: </span>{selectedProvider.key ? "••••••" : <span style={{ color: "#f59e0b" }}>{t("未配置", "Not set")}</span>}</div>
                    </div>
                    <button
                        onClick={() => openConfig(providers.findIndex(p => p.name === currentName))}
                        style={{
                            fontSize: "0.74rem", padding: "5px 14px", cursor: "pointer",
                            background: colors.bg, color: colors.text,
                            border: `1px solid ${colors.border}`, borderRadius: 4, flexShrink: 0,
                        }}
                    >
                        {t("配置", "Configure")}
                    </button>
                </div>
            )}

            {/* Config modal */}
            {showConfig && configIdx >= 0 && providers[configIdx] && (
                <div style={{
                    marginBottom: 16, padding: "14px", borderRadius: 6,
                    border: `1px solid #6366f1`, background: colors.surface,
                }}>
                    <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 12 }}>
                        <span style={{ fontSize: "0.8rem", fontWeight: 600, color: colors.text }}>
                            {t("配置服务商", "Configure Provider")}: {providers[configIdx].name}
                        </span>
                        <button onClick={() => setShowConfig(false)} style={{
                            border: "none", background: "transparent", cursor: "pointer",
                            fontSize: "1rem", color: colors.textSecondary, padding: "0 4px",
                        }}>✕</button>
                    </div>
                    {providers[configIdx].is_custom && (
                        <div style={{ marginBottom: 12 }}>
                            <label style={labelStyle}>{t("服务商名称", "Provider Name")}</label>
                            <input style={inputStyle} value={providers[configIdx].name}
                                onChange={e => updateProviderField(configIdx, "name", e.target.value)}
                                placeholder={t("自定义名称", "Custom name")} />
                        </div>
                    )}
                    <div style={{ marginBottom: 12 }}>
                        <label style={labelStyle}>{t("API 地址 (URL)", "API URL")}</label>
                        <input style={inputStyle} value={providers[configIdx].url}
                            onChange={e => updateProviderField(configIdx, "url", e.target.value)}
                            placeholder="https://api.openai.com/v1" />
                    </div>
                    <div style={{ marginBottom: 12 }}>
                        <label style={labelStyle}>{t("模型名称", "Model Name")}</label>
                        <input style={inputStyle} value={providers[configIdx].model}
                            onChange={e => updateProviderField(configIdx, "model", e.target.value)}
                            placeholder="gpt-4o" />
                    </div>
                    <div style={{ marginBottom: 12 }}>
                        <label style={labelStyle}>{t("API 密钥", "API Key")}</label>
                        <input style={inputStyle} type="password" value={providers[configIdx].key}
                            onChange={e => updateProviderField(configIdx, "key", e.target.value)}
                            placeholder="sk-..." />
                    </div>
                    <button onClick={() => setShowConfig(false)} style={{
                        fontSize: "0.76rem", padding: "5px 16px", cursor: "pointer",
                        background: "#6366f1", color: "#fff", border: "none", borderRadius: 4,
                    }}>{t("完成", "Done")}</button>
                </div>
            )}

            {/* Action buttons */}
            <div style={{ display: "flex", gap: 10, alignItems: "center", marginTop: 20 }}>
                <button onClick={handleSave} disabled={saving || !dirty}
                    style={{
                        fontSize: "0.76rem", padding: "6px 18px", cursor: dirty ? "pointer" : "default",
                        background: dirty ? "#6366f1" : colors.bg, color: dirty ? "#fff" : colors.textMuted,
                        border: "none", borderRadius: 4, opacity: saving ? 0.6 : 1,
                    }}>
                    {saving ? t("保存中...", "Saving...") : t("保存", "Save")}
                </button>
                <button onClick={handleTest}
                    disabled={testing || isNone || !providerConfigured}
                    style={{
                        fontSize: "0.76rem", padding: "6px 18px", cursor: (isNone || !providerConfigured) ? "default" : "pointer",
                        background: colors.bg, color: (isNone || !providerConfigured) ? colors.textMuted : colors.text,
                        border: `1px solid ${colors.border}`, borderRadius: 4,
                        opacity: (testing || isNone || !providerConfigured) ? 0.45 : 1,
                    }}>
                    {testing ? t("测试中...", "Testing...") : t("测试连接", "Test Connection")}
                </button>
                {dirty && <span style={{ fontSize: "0.68rem", color: "#f59e0b" }}>{t("未保存", "unsaved")}</span>}
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

import { useState, useEffect } from "react";
import { main } from "../../../wailsjs/go/models";
import { GetHubSecurityPolicy, IsHubSecurityReadOnly } from "../../../wailsjs/go/main/App";
import { EventsOn } from "../../../wailsjs/runtime/runtime";
import { colors } from "./styles";

type SecurityPolicyMode = "relaxed" | "standard" | "strict";

const SECURITY_MODES: { value: SecurityPolicyMode; labelZh: string; labelEn: string; descZh: string; descEn: string }[] = [
    { value: "relaxed", labelZh: "宽松", labelEn: "Relaxed", descZh: "low/medium/high 放行，critical 需确认", descEn: "low/medium/high allowed, critical requires confirmation" },
    { value: "standard", labelZh: "标准", labelEn: "Standard", descZh: "low 放行，medium 记录，high/critical 需确认", descEn: "low allowed, medium audited, high/critical requires confirmation" },
    { value: "strict", labelZh: "严格", labelEn: "Strict", descZh: "low 放行，medium 及以上均需确认，critical 拒绝", descEn: "low allowed, medium+ requires confirmation, critical denied" },
];

const SANDBOX_OPTIONS = ["none", "os", "docker"] as const;
const NETWORK_OPTIONS = ["none", "intranet", "full"] as const;

type Props = {
    config: main.AppConfig | null;
    saveRemoteConfigField: (patch: Partial<main.AppConfig>) => void;
    lang: string;
};

export function SecurityPolicyPanel({ config, saveRemoteConfigField, lang }: Props) {
    const [readOnly, setReadOnly] = useState(false);
    const [hubPolicy, setHubPolicy] = useState<any>(null);

    const t = (en: string, zhHans: string, zhHant: string = zhHans) =>
        lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en;

    // Load Hub security state
    useEffect(() => {
        let mounted = true;
        const refresh = () => {
            IsHubSecurityReadOnly().then((v) => { if (mounted) setReadOnly(v); }).catch(() => {});
            GetHubSecurityPolicy().then((p: any) => {
                if (!mounted) return;
                if (p && p.centralized_security && p.policy) {
                    setHubPolicy(p.policy);
                } else {
                    setHubPolicy(null);
                }
            }).catch(() => {});
        };
        refresh();
        // Listen for changes but do NOT call EventsOff on cleanup —
        // App.tsx also listens on the same event name and Wails EventsOff
        // removes ALL listeners for the event, not just ours.
        EventsOn("hub-security-policy-changed", refresh);
        return () => { mounted = false; };
    }, []);

    // Helpers to get effective value: Hub override > local config
    // Note: Hub EffectivePolicy uses "guardrail_mode" but AppConfig uses "security_policy_mode"
    const getStr = (key: string, hubKey: string, fallback: string): string => {
        if (readOnly && hubPolicy && hubPolicy[hubKey] !== undefined) return hubPolicy[hubKey];
        return (config as any)?.[key] || fallback;
    };
    const getBool = (key: string, fallback: boolean): boolean => {
        if (readOnly && hubPolicy && hubPolicy[key] !== undefined) return hubPolicy[key];
        const v = (config as any)?.[key];
        return v === undefined ? fallback : v;
    };

    const securityMode = getStr("security_policy_mode", "guardrail_mode", "standard") as SecurityPolicyMode;
    const sandboxMode = getStr("sandbox_mode", "sandbox_mode", "none");
    const networkLevel = getStr("network_level", "network_level", "full");
    const yoloAllowed = getBool("yolo_mode_allowed", true);
    const gossipOn = getBool("gossip_enabled", true);
    const fileOut = getBool("file_outbound_enabled", true);
    const imageOut = getBool("image_outbound_enabled", true);

    const currentMode = SECURITY_MODES.find((m) => m.value === securityMode);

    const disabledStyle: React.CSSProperties = readOnly ? { opacity: 0.6, pointerEvents: "none" } : {};

    return (
        <div style={{ padding: "2px 0" }}>
            {readOnly && (
                <div style={{ padding: "8px 12px", marginBottom: "12px", background: colors.warningBg, borderRadius: "8px", border: `1px solid ${colors.warning}`, fontSize: "0.78rem", color: colors.warning }}>
                    🔒 {t("当前由 Hub 集中管控，以下设置为只读", "Managed by Hub centralized security — settings are read-only")}
                </div>
            )}

            <div style={{ fontSize: "0.9rem", fontWeight: 600, marginBottom: "12px" }}>
                🛡️ {t("安全策略", "Security Policy")}
            </div>

            {/* 1. Guardrail mode */}
            <div className="form-group" style={{ marginBottom: "14px", ...disabledStyle }}>
                <label className="form-label" style={{ fontSize: "0.82rem" }}>
                    {t("安全护栏", "Guardrail Mode")}
                </label>
                <div style={{ display: "flex", gap: "6px" }}>
                    {SECURITY_MODES.map((mode) => (
                        <button
                            key={mode.value}
                            className={securityMode === mode.value ? "btn-primary" : "btn-secondary"}
                            style={{ flex: 1, fontSize: "0.8rem", padding: "6px 10px", height: "32px" }}
                            disabled={readOnly}
                            onClick={() => saveRemoteConfigField({ security_policy_mode: mode.value } as any)}
                        >
                            {t(mode.labelEn, mode.labelZh)}
                        </button>
                    ))}
                </div>
                <div style={{ fontSize: "0.75rem", color: colors.textMuted, marginTop: "4px" }}>
                    {currentMode ? t(currentMode.descEn, currentMode.descZh) : ""}
                </div>
            </div>

            {/* Risk level reference table */}
            <div style={{ marginBottom: "14px", padding: "10px 12px", background: colors.bg, borderRadius: "8px", border: `1px solid ${colors.border}` }}>
                <table style={{ width: "100%", fontSize: "0.75rem", borderCollapse: "collapse", color: colors.textSecondary }}>
                    <thead>
                        <tr style={{ borderBottom: `1px solid ${colors.border}` }}>
                            <th style={{ textAlign: "left", padding: "4px 6px", fontWeight: 600 }}>{t("风险等级", "Risk Level")}</th>
                            <th style={{ textAlign: "center", padding: "4px 6px", fontWeight: 600 }}>{t("宽松", "Relaxed")}</th>
                            <th style={{ textAlign: "center", padding: "4px 6px", fontWeight: 600 }}>{t("标准", "Standard")}</th>
                            <th style={{ textAlign: "center", padding: "4px 6px", fontWeight: 600 }}>{t("严格", "Strict")}</th>
                        </tr>
                    </thead>
                    <tbody>
                        {[
                            { level: "low", zh: "低", relaxed: "✅", standard: "✅", strict: "✅" },
                            { level: "medium", zh: "中", relaxed: "✅", standard: "📝", strict: "⚠️" },
                            { level: "high", zh: "高", relaxed: "✅", standard: "⚠️", strict: "⚠️" },
                            { level: "critical", zh: "危险", relaxed: "⚠️", standard: "⚠️", strict: "⛔" },
                        ].map((row) => (
                            <tr key={row.level} style={{ borderBottom: `1px solid ${colors.borderLight}` }}>
                                <td style={{ padding: "3px 6px" }}>{t(row.level, row.zh)}</td>
                                <td style={{ textAlign: "center", padding: "3px 6px" }}>{row.relaxed}</td>
                                <td style={{ textAlign: "center", padding: "3px 6px" }}>{row.standard}</td>
                                <td style={{ textAlign: "center", padding: "3px 6px" }}>{row.strict}</td>
                            </tr>
                        ))}
                    </tbody>
                </table>
                <div style={{ fontSize: "0.7rem", color: colors.textMuted, marginTop: "4px" }}>
                    ✅ {t("放行", "Allow")}　📝 {t("记录", "Audit")}　⚠️ {t("需确认", "Confirm")}　⛔ {t("拒绝", "Deny")}
                </div>
            </div>

            {/* 2. Sandbox mode */}
            <PolicySelect
                label={t("沙箱模式", "Sandbox Mode")}
                desc={t("工具执行的隔离模式", "Isolation mode for tool execution")}
                value={sandboxMode}
                options={SANDBOX_OPTIONS as unknown as string[]}
                labels={[t("无", "None"), t("系统沙箱", "OS Sandbox"), "Docker"]}
                disabled={readOnly}
                onChange={(v) => saveRemoteConfigField({ sandbox_mode: v } as any)}
            />

            {/* 3. Network level */}
            <PolicySelect
                label={t("网络访问", "Network Access")}
                desc={t("Agent 工具可访问的网络范围", "Network scope for agent tool access")}
                value={networkLevel}
                options={NETWORK_OPTIONS as unknown as string[]}
                labels={[t("禁止", "None"), t("内网", "Intranet"), t("全部", "Full")]}
                disabled={readOnly}
                onChange={(v) => saveRemoteConfigField({ network_level: v } as any)}
            />

            {/* 4-7. Boolean toggles */}
            <PolicyToggle label={t("YOLO 模式", "YOLO Mode")} desc={t("允许自动执行模式（跳过确认）", "Allow auto-execute mode (skip confirmations)")} value={yoloAllowed} disabled={readOnly} onChange={(v) => saveRemoteConfigField({ yolo_mode_allowed: v } as any)} />
            <PolicyToggle label={t("Gossip 模块", "Gossip")} desc={t("启用 Gossip 社区功能", "Enable Gossip community features")} value={gossipOn} disabled={readOnly} onChange={(v) => saveRemoteConfigField({ gossip_enabled: v } as any)} />
            <PolicyToggle label={t("文件外发", "File Outbound")} desc={t("允许通过 IM 通道发送文件", "Allow sending files via IM channels")} value={fileOut} disabled={readOnly} onChange={(v) => saveRemoteConfigField({ file_outbound_enabled: v } as any)} />
            <PolicyToggle label={t("图片外发", "Image Outbound")} desc={t("允许通过 IM 通道发送图片", "Allow sending images via IM channels")} value={imageOut} disabled={readOnly} onChange={(v) => saveRemoteConfigField({ image_outbound_enabled: v } as any)} />

            {/* Audit log info */}
            <div style={{ marginTop: "14px", fontSize: "0.78rem", color: colors.textSecondary, lineHeight: 1.7 }}>
                <div style={{ fontWeight: 600, marginBottom: "4px" }}>
                    📋 {t("审计日志", "Audit Log")}
                </div>
                <div>• {t("存储位置: ~/.maclaw/audit/", "Location: ~/.maclaw/audit/")}</div>
                <div>• {t("可通过 IM 发送消息调用 query_audit_log 工具查询", "Query via IM using query_audit_log tool")}</div>
                <div>• {t("日志按日期自动轮转，保留 30 天", "Auto-rotated daily, retained for 30 days")}</div>
            </div>
        </div>
    );
}

// --- Sub-components ---

function PolicySelect({ label, desc, value, options, labels, disabled, onChange }: {
    label: string; desc: string; value: string; options: string[]; labels: string[];
    disabled: boolean; onChange: (v: string) => void;
}) {
    return (
        <div className="form-group" style={{ marginBottom: "12px", ...(disabled ? { opacity: 0.6, pointerEvents: "none" as const } : {}) }}>
            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
                <div>
                    <label className="form-label" style={{ fontSize: "0.82rem", marginBottom: 0 }}>{label}</label>
                    <div style={{ fontSize: "0.72rem", color: colors.textMuted }}>{desc}</div>
                </div>
                <select
                    value={value}
                    disabled={disabled}
                    onChange={(e) => onChange(e.target.value)}
                    style={{ width: "140px", height: "32px", fontSize: "0.8rem", borderRadius: "6px", border: `1px solid ${colors.border}`, padding: "0 8px", background: colors.surface, color: colors.text }}
                >
                    {options.map((opt, i) => (
                        <option key={opt} value={opt}>{labels[i]}</option>
                    ))}
                </select>
            </div>
        </div>
    );
}

function PolicyToggle({ label, desc, value, disabled, onChange }: {
    label: string; desc: string; value: boolean; disabled: boolean; onChange: (v: boolean) => void;
}) {
    return (
        <div className="form-group" style={{ marginBottom: "10px", ...(disabled ? { opacity: 0.6, pointerEvents: "none" as const } : {}) }}>
            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
                <div>
                    <label className="form-label" style={{ fontSize: "0.82rem", marginBottom: 0 }}>{label}</label>
                    <div style={{ fontSize: "0.72rem", color: colors.textMuted }}>{desc}</div>
                </div>
                <button
                    className={value ? "btn-primary" : "btn-secondary"}
                    style={{ minWidth: "60px", height: "28px", fontSize: "0.75rem", padding: "0 10px" }}
                    disabled={disabled}
                    onClick={() => onChange(!value)}
                >
                    {value ? "ON" : "OFF"}
                </button>
            </div>
        </div>
    );
}

import { useState, useEffect } from "react";
import { main } from "../../../wailsjs/go/models";

type SecurityPolicyMode = "relaxed" | "standard" | "strict";

const SECURITY_MODES: { value: SecurityPolicyMode; labelZh: string; labelEn: string; descZh: string; descEn: string }[] = [
    { value: "relaxed", labelZh: "宽松", labelEn: "Relaxed", descZh: "low/medium/high 放行，critical 需确认", descEn: "low/medium/high allowed, critical requires confirmation" },
    { value: "standard", labelZh: "标准", labelEn: "Standard", descZh: "low 放行，medium 记录，high/critical 需确认", descEn: "low allowed, medium audited, high/critical requires confirmation" },
    { value: "strict", labelZh: "严格", labelEn: "Strict", descZh: "low 放行，medium 及以上均需确认，critical 拒绝", descEn: "low allowed, medium+ requires confirmation, critical denied" },
];

function getModeFromConfig(config: main.AppConfig | null): SecurityPolicyMode {
    const raw = (config as any)?.security_policy_mode;
    if (raw === "relaxed" || raw === "standard" || raw === "strict") return raw;
    return "standard";
}

type Props = {
    config: main.AppConfig | null;
    saveRemoteConfigField: (patch: Partial<main.AppConfig>) => void;
    lang: string;
};

export function SecurityPolicyPanel({ config, saveRemoteConfigField, lang }: Props) {
    const [securityMode, setSecurityMode] = useState<SecurityPolicyMode>(() => getModeFromConfig(config));

    // Sync local state when config changes externally.
    useEffect(() => {
        setSecurityMode(getModeFromConfig(config));
    }, [(config as any)?.security_policy_mode]);

    const isEn = lang === "en";
    const t = (zh: string, en: string) => isEn ? en : zh;

    const currentMode = SECURITY_MODES.find((m) => m.value === securityMode);

    return (
        <div style={{ padding: "2px 0" }}>
            <div style={{ fontSize: "0.9rem", fontWeight: 600, marginBottom: "12px" }}>
                🛡️ {t("安全策略", "Security Policy")}
            </div>
            <div className="form-group" style={{ marginBottom: "10px" }}>
                <label className="form-label" style={{ fontSize: "0.82rem" }}>
                    {t("策略模式", "Policy Mode")}
                </label>
                <div style={{ display: "flex", gap: "6px" }}>
                    {SECURITY_MODES.map((mode) => (
                        <button
                            key={mode.value}
                            className={securityMode === mode.value ? "btn-primary" : "btn-secondary"}
                            style={{ flex: 1, fontSize: "0.8rem", padding: "6px 10px", height: "32px" }}
                            onClick={() => {
                                setSecurityMode(mode.value);
                                saveRemoteConfigField({ security_policy_mode: mode.value } as any);
                            }}
                        >
                            {isEn ? mode.labelEn : mode.labelZh}
                        </button>
                    ))}
                </div>
                <div style={{ fontSize: "0.75rem", color: "#888", marginTop: "6px" }}>
                    {currentMode ? (isEn ? currentMode.descEn : currentMode.descZh) : ""}
                </div>
            </div>

            {/* Risk level reference table */}
            <div style={{ marginTop: "12px", padding: "10px 12px", background: "#f8f9fa", borderRadius: "8px", border: "1px solid #e9ecef" }}>
                <table style={{ width: "100%", fontSize: "0.75rem", borderCollapse: "collapse", color: "#555" }}>
                    <thead>
                        <tr style={{ borderBottom: "1px solid #dee2e6" }}>
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
                            <tr key={row.level} style={{ borderBottom: "1px solid #f0f0f0" }}>
                                <td style={{ padding: "3px 6px" }}>{isEn ? row.level : row.zh}</td>
                                <td style={{ textAlign: "center", padding: "3px 6px" }}>{row.relaxed}</td>
                                <td style={{ textAlign: "center", padding: "3px 6px" }}>{row.standard}</td>
                                <td style={{ textAlign: "center", padding: "3px 6px" }}>{row.strict}</td>
                            </tr>
                        ))}
                    </tbody>
                </table>
                <div style={{ fontSize: "0.7rem", color: "#999", marginTop: "4px" }}>
                    ✅ {t("放行", "Allow")}　📝 {t("记录", "Audit")}　⚠️ {t("需确认", "Confirm")}　⛔ {t("拒绝", "Deny")}
                    <br />
                    {t("注: 含危险关键词（rm -rf / DROP TABLE / sudo）的操作在所有模式下均直接拒绝", "Note: operations with dangerous keywords (rm -rf / DROP TABLE / sudo) are always denied")}
                </div>
            </div>

            {/* Audit log info */}
            <div style={{ marginTop: "14px", fontSize: "0.78rem", color: "#666", lineHeight: 1.7 }}>
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

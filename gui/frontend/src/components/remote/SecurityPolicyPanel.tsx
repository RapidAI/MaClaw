import { useEffect, useState } from "react";
import { main } from "../../../wailsjs/go/models";
import { GetHubSecurityPolicy, IsHubSecurityReadOnly } from "../../../wailsjs/go/main/App";
import { EventsOn } from "../../../wailsjs/runtime/runtime";
import { colors } from "./styles";

type SecurityPolicyMode = "relaxed" | "standard" | "strict" | "developer";

type Props = {
    config: main.AppConfig | null;
    saveRemoteConfigField: (patch: Partial<main.AppConfig>) => void;
    lang: string;
};

const SECURITY_MODES: { value: SecurityPolicyMode; labelZh: string; labelZhHant: string; labelEn: string; descZh: string; descZhHant: string; descEn: string }[] = [
    {
        value: "relaxed",
        labelZh: "宽松",
        labelZhHant: "寬鬆",
        labelEn: "Relaxed",
        descZh: "所有风险等级放行；风险扫描仅记录，不阻断 skill 安装/执行",
        descZhHant: "所有風險等級放行；風險掃描僅記錄，不阻斷 skill 安裝/執行",
        descEn: "all risk levels allowed; scan findings are recorded and do not block skill install/run",
    },
    {
        value: "standard",
        labelZh: "标准",
        labelZhHant: "標準",
        labelEn: "Standard",
        descZh: "low 放行，medium 记录；high/critical 有确认通道则确认，否则记录后放行",
        descZhHant: "low 放行，medium 記錄；high/critical 有確認通道則確認，否則記錄後放行",
        descEn: "low allowed, medium audited; high/critical asks when a confirmation channel exists, otherwise records and allows",
    },
    {
        value: "strict",
        labelZh: "严格",
        labelZhHant: "嚴格",
        labelEn: "Strict",
        descZh: "low 放行，medium/high 需要确认，critical 与危险命令直接阻止",
        descZhHant: "low 放行，medium/high 需要確認，critical 與危險命令直接阻止",
        descEn: "low allowed, medium/high require confirmation, critical and dangerous commands are blocked",
    },
    {
        value: "developer",
        labelZh: "开发者",
        labelZhHant: "開發者",
        labelEn: "Developer",
        descZh: "⚠️ 所有操作放行；仅记录审计，不弹确认、不阻止。仅供开发和安全研究使用",
        descZhHant: "⚠️ 所有操作放行；僅記錄審計，不彈確認、不阻止。僅供開發和安全研究使用",
        descEn: "⚠️ All operations allowed; audit only, no confirmation or blocking. For development and security research only",
    },
];

const SANDBOX_OPTIONS = ["none", "os", "docker"] as const;
const NETWORK_OPTIONS = ["none", "intranet", "full"] as const;

export function SecurityPolicyPanel({ config, saveRemoteConfigField, lang }: Props) {
    const [readOnly, setReadOnly] = useState(false);
    const [hubPolicy, setHubPolicy] = useState<any>(null);

    const t = (en: string, zhHans: string, zhHant: string = zhHans) =>
        lang === "zh-Hans" ? zhHans : lang === "zh-Hant" ? zhHant : en;

    useEffect(() => {
        let mounted = true;
        const refresh = () => {
            IsHubSecurityReadOnly().then((value) => {
                if (mounted) setReadOnly(!!value);
            }).catch(() => {});
            GetHubSecurityPolicy().then((policy: any) => {
                if (!mounted) return;
                if (policy?.centralized_security && policy?.policy) {
                    setHubPolicy(policy.policy);
                } else {
                    setHubPolicy(null);
                }
            }).catch(() => {
                if (mounted) setHubPolicy(null);
            });
        };
        refresh();
        EventsOn("hub-security-policy-changed", refresh);
        return () => {
            mounted = false;
        };
    }, []);

    const getStr = (key: string, hubKey: string, fallback: string): string => {
        if (readOnly && hubPolicy && hubPolicy[hubKey] !== undefined) return String(hubPolicy[hubKey]);
        const value = (config as any)?.[key];
        return typeof value === "string" && value.trim() !== "" ? value : fallback;
    };

    const getBool = (key: string, fallback: boolean): boolean => {
        if (readOnly && hubPolicy && hubPolicy[key] !== undefined) return !!hubPolicy[key];
        const value = (config as any)?.[key];
        return value === undefined ? fallback : !!value;
    };

    const securityMode = getStr("security_policy_mode", "guardrail_mode", "standard") as SecurityPolicyMode;
    const sandboxMode = getStr("sandbox_mode", "sandbox_mode", "none");
    const networkLevel = getStr("network_level", "network_level", "full");
    const yoloAllowed = getBool("yolo_mode_allowed", true);
    const gossipEnabled = getBool("gossip_enabled", true);
    const fileOutboundEnabled = getBool("file_outbound_enabled", true);
    const imageOutboundEnabled = getBool("image_outbound_enabled", true);

    const currentMode = SECURITY_MODES.find((item) => item.value === securityMode) || SECURITY_MODES[1];
    const disabledStyle: React.CSSProperties = readOnly ? { opacity: 0.65, pointerEvents: "none" } : {};

    return (
        <div style={{ padding: "2px 0" }}>
            {readOnly && (
                <div style={{
                    padding: "8px 12px",
                    marginBottom: "12px",
                    background: colors.warningBg,
                    borderRadius: "8px",
                    border: `1px solid ${colors.warning}`,
                    fontSize: "0.78rem",
                    color: colors.warning,
                }}>
                    {t("Managed by Hub centralized security. The settings below are read-only.", "当前由 Hub 集中管控，以下设置为只读", "當前由 Hub 集中管控，以下設置為只讀")}
                </div>
            )}

            <div className="form-group" style={{ marginBottom: "14px", ...disabledStyle }}>
                <label className="form-label" style={{ fontSize: "0.82rem" }}>
                    {t("Guardrail Mode", "安全护栏", "安全護欄")}
                </label>
                <div style={{ display: "flex", gap: "6px" }}>
                    {SECURITY_MODES.map((mode) => (
                        <button
                            key={mode.value}
                            className={securityMode === mode.value ? "btn-primary" : "btn-secondary"}
                            style={{
                                flex: 1,
                                fontSize: "0.8rem",
                                padding: "6px 10px",
                                height: "32px",
                                ...(mode.value === "developer" ? {
                                    borderColor: securityMode === "developer" ? "#f59e0b" : colors.border,
                                    background: securityMode === "developer" ? "#78350f" : undefined,
                                    color: securityMode === "developer" ? "#fbbf24" : colors.textMuted,
                                } : {}),
                            }}
                            disabled={readOnly}
                            onClick={() => saveRemoteConfigField({ security_policy_mode: mode.value } as any)}
                        >
                            {t(mode.labelEn, mode.labelZh, mode.labelZhHant)}
                        </button>
                    ))}
                </div>
                <div style={{ fontSize: "0.75rem", color: colors.textMuted, marginTop: "4px" }}>
                    {t(currentMode.descEn, currentMode.descZh, currentMode.descZhHant)}
                </div>
            </div>

            <div style={{ marginBottom: "14px", padding: "10px 12px", background: colors.bg, borderRadius: "8px", border: `1px solid ${colors.border}` }}>
                <table style={{ width: "100%", fontSize: "0.75rem", borderCollapse: "collapse", color: colors.textSecondary }}>
                    <thead>
                        <tr style={{ borderBottom: `1px solid ${colors.border}` }}>
                            <th style={{ textAlign: "left", padding: "4px 6px", fontWeight: 600 }}>{t("Risk Level", "风险等级", "風險等級")}</th>
                            <th style={{ textAlign: "center", padding: "4px 6px", fontWeight: 600 }}>{t("Relaxed", "宽松", "寬鬆")}</th>
                            <th style={{ textAlign: "center", padding: "4px 6px", fontWeight: 600 }}>{t("Standard", "标准", "標準")}</th>
                            <th style={{ textAlign: "center", padding: "4px 6px", fontWeight: 600 }}>{t("Strict", "严格", "嚴格")}</th>
                            <th style={{ textAlign: "center", padding: "4px 6px", fontWeight: 600, color: "#f59e0b" }}>{t("Dev", "开发者", "開發者")}</th>
                        </tr>
                    </thead>
                    <tbody>
                        {[
                            { levelEn: "Low", levelZh: "低", levelZhHant: "低", relaxed: "Allow", standard: "Allow", strict: "Allow" },
                            { levelEn: "Medium", levelZh: "中", levelZhHant: "中", relaxed: "Allow", standard: "Audit", strict: "Confirm" },
                            { levelEn: "High", levelZh: "高", levelZhHant: "高", relaxed: "Allow", standard: "Confirm*", strict: "Confirm" },
                            { levelEn: "Critical", levelZh: "危险", levelZhHant: "危險", relaxed: "Allow", standard: "Confirm*", strict: "Deny" },
                        ].map((row) => (
                            <tr key={row.levelEn} style={{ borderBottom: `1px solid ${colors.borderLight}` }}>
                                <td style={{ padding: "3px 6px" }}>{t(row.levelEn, row.levelZh, row.levelZhHant)}</td>
                                <td style={{ textAlign: "center", padding: "3px 6px" }}>{t(row.relaxed, row.relaxed === "Allow" ? "放行" : row.relaxed, row.relaxed === "Allow" ? "放行" : row.relaxed)}</td>
                                <td style={{ textAlign: "center", padding: "3px 6px" }}>{t(row.standard, row.standard === "Allow" ? "放行" : row.standard === "Audit" ? "记录" : row.standard.startsWith("Confirm") ? "确认*" : "拒绝", row.standard === "Allow" ? "放行" : row.standard === "Audit" ? "記錄" : row.standard.startsWith("Confirm") ? "確認*" : "拒絕")}</td>
                                <td style={{ textAlign: "center", padding: "3px 6px" }}>{t(row.strict, row.strict === "Allow" ? "放行" : row.strict === "Audit" ? "记录" : row.strict.startsWith("Confirm") ? "确认" : "拒绝", row.strict === "Allow" ? "放行" : row.strict === "Audit" ? "記錄" : row.strict.startsWith("Confirm") ? "確認" : "拒絕")}</td>
                                <td style={{ textAlign: "center", padding: "3px 6px", color: "#f59e0b" }}>{t("Audit+Allow", "记录放行", "記錄放行")}</td>
                            </tr>
                        ))}
                    </tbody>
                </table>
                <div style={{ fontSize: "0.7rem", color: colors.textMuted, marginTop: "6px" }}>
                    {t("Allow = directly allowed, Audit = recorded, Confirm* = ask when UI is available; otherwise record and allow, Deny = blocked", "放行 = 直接允许，记录 = 仅审计，确认* = 有确认界面时询问，否则记录后放行，拒绝 = 直接阻止", "放行 = 直接允許，記錄 = 僅審計，確認* = 有確認介面時詢問，否則記錄後放行，拒絕 = 直接阻止")}
                </div>
            </div>

            <PolicySelect
                label={t("Sandbox Mode", "沙箱模式", "沙箱模式")}
                desc={t("Isolation mode for tool execution", "工具执行时的隔离模式", "工具執行時的隔離模式")}
                value={sandboxMode}
                options={SANDBOX_OPTIONS as unknown as string[]}
                labels={[
                    t("None", "无", "無"),
                    t("OS Sandbox", "系统沙箱", "系統沙箱"),
                    "Docker",
                ]}
                disabled={readOnly}
                onChange={(value) => saveRemoteConfigField({ sandbox_mode: value } as any)}
            />

            <PolicySelect
                label={t("Network Access", "网络访问", "網路訪問")}
                desc={t("Network scope for agent tool access", "Agent 工具可访问的网络范围", "Agent 工具可訪問的網路範圍")}
                value={networkLevel}
                options={NETWORK_OPTIONS as unknown as string[]}
                labels={[
                    t("None", "禁止", "禁止"),
                    t("Intranet", "内网", "內網"),
                    t("Full", "全部", "全部"),
                ]}
                disabled={readOnly}
                onChange={(value) => saveRemoteConfigField({ network_level: value } as any)}
            />

            <PolicyToggle
                label={t("YOLO Mode", "YOLO 模式", "YOLO 模式")}
                desc={t("Allow auto-execute mode (skip confirmations)", "允许自动执行模式（跳过确认）", "允許自動執行模式（跳過確認）")}
                value={yoloAllowed}
                disabled={readOnly}
                onChange={(value) => saveRemoteConfigField({ yolo_mode_allowed: value } as any)}
            />
            <PolicyToggle
                label={t("Gossip", "Gossip 模块", "Gossip 模組")}
                desc={t("Enable Gossip community features", "启用 Gossip 社区功能", "啟用 Gossip 社群功能")}
                value={gossipEnabled}
                disabled={readOnly}
                onChange={(value) => saveRemoteConfigField({ gossip_enabled: value } as any)}
            />
            <PolicyToggle
                label={t("File Outbound", "文件外发", "文件外發")}
                desc={t("Allow sending files via IM channels", "允许通过 IM 通道发送文件", "允許通過 IM 通道發送文件")}
                value={fileOutboundEnabled}
                disabled={readOnly}
                onChange={(value) => saveRemoteConfigField({ file_outbound_enabled: value } as any)}
            />
            <PolicyToggle
                label={t("Image Outbound", "图片外发", "圖片外發")}
                desc={t("Allow sending images via IM channels", "允许通过 IM 通道发送图片", "允許通過 IM 通道發送圖片")}
                value={imageOutboundEnabled}
                disabled={readOnly}
                onChange={(value) => saveRemoteConfigField({ image_outbound_enabled: value } as any)}
            />

            <div style={{ marginTop: "14px", fontSize: "0.78rem", color: colors.textSecondary, lineHeight: 1.7 }}>
                <div style={{ fontWeight: 600, marginBottom: "4px", color: colors.text }}>
                    {t("Audit Log", "审计日志", "審計日誌")}
                </div>
                <div>{t("Location: ~/.maclaw/audit/", "存储位置: ~/.maclaw/audit/", "存儲位置: ~/.maclaw/audit/")}</div>
                <div>{t("Query via IM using query_audit_log tool", "可通过 IM 发送消息调用 query_audit_log 工具查询", "可通過 IM 發送訊息調用 query_audit_log 工具查詢")}</div>
                <div>{t("Auto-rotated daily, retained for 30 days", "日志按日期自动轮转，保留 30 天", "日誌按日期自動輪轉，保留 30 天")}</div>
            </div>
        </div>
    );
}

function PolicySelect({ label, desc, value, options, labels, disabled, onChange }: {
    label: string;
    desc: string;
    value: string;
    options: string[];
    labels: string[];
    disabled: boolean;
    onChange: (value: string) => void;
}) {
    return (
        <div className="form-group" style={{ marginBottom: "12px", ...(disabled ? { opacity: 0.6, pointerEvents: "none" as const } : {}) }}>
            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 16 }}>
                <div>
                    <label className="form-label" style={{ fontSize: "0.82rem", marginBottom: 0 }}>{label}</label>
                    <div style={{ fontSize: "0.72rem", color: colors.textMuted }}>{desc}</div>
                </div>
                <select
                    value={value}
                    disabled={disabled}
                    onChange={(event) => onChange(event.target.value)}
                    style={{ width: "148px", height: "32px", fontSize: "0.8rem", borderRadius: "6px", border: `1px solid ${colors.border}`, padding: "0 8px", background: colors.surface, color: colors.text }}
                >
                    {options.map((option, index) => (
                        <option key={option} value={option}>{labels[index]}</option>
                    ))}
                </select>
            </div>
        </div>
    );
}

function PolicyToggle({ label, desc, value, disabled, onChange }: {
    label: string;
    desc: string;
    value: boolean;
    disabled: boolean;
    onChange: (value: boolean) => void;
}) {
    return (
        <div className="form-group" style={{ marginBottom: "10px", ...(disabled ? { opacity: 0.6, pointerEvents: "none" as const } : {}) }}>
            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 16 }}>
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

import { useEffect, useState } from "react";
import { main } from "../../../wailsjs/go/models";
import { GetHubSecurityPolicy, IsHubSecurityReadOnly } from "../../../wailsjs/go/main/App";
import { EventsOn } from "../../../wailsjs/runtime/runtime";
import { colors } from "./styles";

type SecurityPolicyMode = "none" | "relaxed" | "standard" | "strict" | "developer";

type Props = {
    config: main.AppConfig | null;
    saveRemoteConfigField: (patch: Partial<main.AppConfig>) => void;
    lang: string;
};

type SecurityTableCell = "Allow" | "Audit" | "Confirm" | "Confirm*" | "Deny" | "Yes" | "No";

type SecurityTableRow = {
    itemEn: string;
    itemZh: string;
    itemZhHant: string;
    off: SecurityTableCell;
    relaxed: SecurityTableCell;
    standard: SecurityTableCell;
    strict: SecurityTableCell;
    developer: SecurityTableCell;
};

const SECURITY_MODES: { value: SecurityPolicyMode; labelZh: string; labelZhHant: string; labelEn: string; descZh: string; descZhHant: string; descEn: string }[] = [
    {
        value: "none",
        labelZh: "关闭护栏",
        labelZhHant: "關閉護欄",
        labelEn: "Off",
        descZh: "不做风险护栏的确认/阻断判断；操作仍可能写入安全日志，网络访问、沙箱、文件外发等独立安全开关仍会生效",
        descZhHant: "不做風險護欄的確認/阻斷判斷；操作仍可能寫入安全日誌，網路訪問、沙箱、檔案外發等獨立安全開關仍會生效",
        descEn: "Disables risk-guardrail confirmation and blocking; actions may still be logged, and network access, sandboxing, and outbound-file controls still apply",
    },
    {
        value: "relaxed",
        labelZh: "宽松",
        labelZhHant: "寬鬆",
        labelEn: "Relaxed",
        descZh: "护栏放行所有风险等级；仍记录风险扫描结果，不阻断 skill 安装/执行",
        descZhHant: "護欄放行所有風險等級；仍記錄風險掃描結果，不阻斷 skill 安裝/執行",
        descEn: "Guardrails allow all risk levels; scan findings are still recorded and do not block skill install/run",
    },
    {
        value: "standard",
        labelZh: "标准",
        labelZhHant: "標準",
        labelEn: "Standard",
        descZh: "低风险放行，中风险记录；高/危险操作有确认界面则询问，否则记录后放行",
        descZhHant: "低風險放行，中風險記錄；高/危險操作有確認介面則詢問，否則記錄後放行",
        descEn: "Low risk is allowed, medium is audited; high/critical asks when a confirmation UI exists, otherwise records and allows",
    },
    {
        value: "strict",
        labelZh: "严格",
        labelZhHant: "嚴格",
        labelEn: "Strict",
        descZh: "低风险放行，中/高风险需要确认；危险操作和危险命令直接阻止",
        descZhHant: "低風險放行，中/高風險需要確認；危險操作和危險命令直接阻止",
        descEn: "Low risk is allowed, medium/high require confirmation; critical operations and dangerous commands are blocked",
    },
    {
        value: "developer",
        labelZh: "开发者",
        labelZhHant: "開發者",
        labelEn: "Developer",
        descZh: "开发者旁路：高风险仍记录开发审计，不弹确认、不阻止。仅供开发和安全研究使用",
        descZhHant: "開發者旁路：高風險仍記錄開發稽核，不彈確認、不阻止。僅供開發和安全研究使用",
        descEn: "Developer bypass: high-risk activity is still audited, with no confirmation or blocking. For development and security research only",
    },
];

const SANDBOX_OPTIONS = ["none", "os", "docker"] as const;
const NETWORK_OPTIONS = ["none", "intranet", "allowlist", "full"] as const;
const SECURITY_TABLE_ROWS: SecurityTableRow[] = [
    { itemEn: "Risk Scan", itemZh: "风险扫描", itemZhHant: "風險掃描", off: "No", relaxed: "Yes", standard: "Yes", strict: "Yes", developer: "Yes" },
    { itemEn: "Risk Log", itemZh: "风险日志记录", itemZhHant: "風險日誌記錄", off: "No", relaxed: "Yes", standard: "Yes", strict: "Yes", developer: "Yes" },
    { itemEn: "High-Risk Dev Audit", itemZh: "高风险开发审计", itemZhHant: "高風險開發稽核", off: "No", relaxed: "No", standard: "No", strict: "No", developer: "Yes" },
    { itemEn: "Low-Risk Operation", itemZh: "低风险操作", itemZhHant: "低風險操作", off: "Allow", relaxed: "Allow", standard: "Allow", strict: "Allow", developer: "Allow" },
    { itemEn: "Medium-Risk Operation", itemZh: "中风险操作", itemZhHant: "中風險操作", off: "Allow", relaxed: "Allow", standard: "Audit", strict: "Confirm", developer: "Allow" },
    { itemEn: "High-Risk Operation", itemZh: "高风险操作", itemZhHant: "高風險操作", off: "Allow", relaxed: "Allow", standard: "Confirm*", strict: "Confirm", developer: "Allow" },
    { itemEn: "Critical Operation", itemZh: "危险操作", itemZhHant: "危險操作", off: "Allow", relaxed: "Allow", standard: "Confirm*", strict: "Deny", developer: "Allow" },
];

const SECURITY_TABLE_CELL_LABELS: Record<SecurityTableCell, { en: string; zh: string; zhHant: string }> = {
    Allow: { en: "Allow", zh: "放行", zhHant: "放行" },
    Audit: { en: "Audit", zh: "记录", zhHant: "記錄" },
    Confirm: { en: "Confirm", zh: "确认", zhHant: "確認" },
    "Confirm*": { en: "Confirm*", zh: "确认*", zhHant: "確認*" },
    Deny: { en: "Deny", zh: "拒绝", zhHant: "拒絕" },
    Yes: { en: "Yes", zh: "是", zhHant: "是" },
    No: { en: "No", zh: "否", zhHant: "否" },
};

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

    const getArray = (key: string): string[] => {
        const value = readOnly && hubPolicy && hubPolicy[key] !== undefined ? hubPolicy[key] : (config as any)?.[key];
        return Array.isArray(value) ? value.map((item) => String(item).trim()).filter(Boolean) : [];
    };

    const securityMode = getStr("security_policy_mode", "guardrail_mode", "standard") as SecurityPolicyMode;
    const sandboxMode = getStr("sandbox_mode", "sandbox_mode", "none");
    const networkLevel = getStr("network_level", "network_level", "full");
    const networkAllowlist = getArray("network_allowlist");
    const yoloAllowed = getBool("yolo_mode_allowed", true);
    const smartRouteEnabled = getBool("smart_route_enabled", true);
    const gossipEnabled = getBool("gossip_enabled", true);
    const fileOutboundEnabled = getBool("file_outbound_enabled", true);
    const imageOutboundEnabled = getBool("image_outbound_enabled", true);
    const skillSourcesAllowed = getArray("skill_sources_allowed");

    const currentMode = SECURITY_MODES.find((item) => item.value === securityMode) || SECURITY_MODES[1];
    const disabledStyle: React.CSSProperties = readOnly ? { opacity: 0.65, pointerEvents: "none" } : {};
    const renderSecurityTableCell = (value: SecurityTableCell) => {
        const labels = SECURITY_TABLE_CELL_LABELS[value];
        return t(labels.en, labels.zh, labels.zhHant);
    };

    return (
        <div style={{ padding: "2px 0" }}>
            {readOnly && (
                <div style={{
                    padding: "8px 12px",
                    marginBottom: "12px",
                    background: colors.infoBg,
                    borderRadius: "8px",
                    border: `1px solid ${colors.primary}`,
                    fontSize: "0.78rem",
                    color: colors.primaryDark,
                }}>
                    {t("Managed by Hub centralized security. Local edits are disabled until Hub centralized policy is turned off.", "当前由 Hub 集中安全策略管控，本地不可修改；关闭 Hub 集中策略后才可编辑", "當前由 Hub 集中安全策略管控，本地不可修改；關閉 Hub 集中策略後才可編輯")}
                </div>
            )}

            <div className="form-group" style={{ marginBottom: "14px", ...disabledStyle }}>
                <label className="form-label" style={{ fontSize: "0.82rem" }}>
                    {t("Risk Guardrails", "风险护栏", "風險護欄")}
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
                                    borderColor: securityMode === "developer" ? colors.primary : colors.border,
                                    background: securityMode === "developer" ? colors.infoBg : undefined,
                                    color: securityMode === "developer" ? colors.primaryDark : colors.textMuted,
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
                            <th style={{ textAlign: "left", padding: "4px 6px", fontWeight: 600 }}>{t("Item", "项目", "項目")}</th>
                            <th style={{ textAlign: "center", padding: "4px 6px", fontWeight: 600 }}>{t("Off", "关闭", "關閉")}</th>
                            <th style={{ textAlign: "center", padding: "4px 6px", fontWeight: 600 }}>{t("Relaxed", "宽松", "寬鬆")}</th>
                            <th style={{ textAlign: "center", padding: "4px 6px", fontWeight: 600 }}>{t("Standard", "标准", "標準")}</th>
                            <th style={{ textAlign: "center", padding: "4px 6px", fontWeight: 600 }}>{t("Strict", "严格", "嚴格")}</th>
                            <th style={{ textAlign: "center", padding: "4px 6px", fontWeight: 600, color: colors.primaryDark }}>{t("Dev", "开发者", "開發者")}</th>
                        </tr>
                    </thead>
                    <tbody>
                        {SECURITY_TABLE_ROWS.map((row) => (
                            <tr key={row.itemEn} style={{ borderBottom: `1px solid ${colors.borderLight}` }}>
                                <td style={{ padding: "3px 6px" }}>{t(row.itemEn, row.itemZh, row.itemZhHant)}</td>
                                <td style={{ textAlign: "center", padding: "3px 6px" }}>{renderSecurityTableCell(row.off)}</td>
                                <td style={{ textAlign: "center", padding: "3px 6px" }}>{renderSecurityTableCell(row.relaxed)}</td>
                                <td style={{ textAlign: "center", padding: "3px 6px" }}>{renderSecurityTableCell(row.standard)}</td>
                                <td style={{ textAlign: "center", padding: "3px 6px" }}>{renderSecurityTableCell(row.strict)}</td>
                                <td style={{ textAlign: "center", padding: "3px 6px", color: colors.primaryDark }}>{renderSecurityTableCell(row.developer)}</td>
                            </tr>
                        ))}
                    </tbody>
                </table>
                <div style={{ fontSize: "0.7rem", color: colors.textMuted, marginTop: "6px" }}>
                    {t("This table only covers risk guardrails. Relaxed records scan findings without blocking execution; Developer additionally keeps high-risk developer audit records. Network access, sandboxing, and outbound-file controls are configured separately and may still block actions.", "此表只说明风险护栏：宽松会记录风险扫描结果但不阻断执行；开发者模式在此基础上额外保留高风险开发审计记录。网络访问、沙箱、文件外发单独配置，仍可能阻止操作", "此表只說明風險護欄：寬鬆會記錄風險掃描結果但不阻斷執行；開發者模式在此基礎上額外保留高風險開發稽核記錄。網路訪問、沙箱、檔案外發單獨配置，仍可能阻止操作")}
                </div>
            </div>

            <PolicySelect
                label={t("Execution Sandbox", "执行沙箱", "執行沙箱")}
                desc={t("Process/filesystem isolation for tool execution; separate from risk guardrails", "工具执行时的进程/文件系统隔离；与风险护栏相互独立", "工具執行時的程序/檔案系統隔離；與風險護欄相互獨立")}
                value={sandboxMode}
                options={SANDBOX_OPTIONS as unknown as string[]}
                labels={[
                    t("No Sandbox", "不使用沙箱", "不使用沙箱"),
                    t("OS Sandbox", "系统沙箱", "系統沙箱"),
                    "Docker",
                ]}
                disabled={readOnly}
                onChange={(value) => saveRemoteConfigField({ sandbox_mode: value } as any)}
            />

            <PolicySelect
                label={t("Tool Network Access", "工具网络访问", "工具網路訪問")}
                desc={t("Network scope for agent tools; separate from risk guardrails", "Agent 工具可访问的网络范围；与风险护栏相互独立", "Agent 工具可訪問的網路範圍；與風險護欄相互獨立")}
                value={networkLevel}
                options={NETWORK_OPTIONS as unknown as string[]}
                labels={[
                    t("Blocked", "禁止联网", "禁止聯網"),
                    t("Intranet", "内网", "內網"),
                    t("Allowlist", "允许列表", "允許列表"),
                    t("Full", "完全开放", "完全開放"),
                ]}
                disabled={readOnly}
                onChange={(value) => saveRemoteConfigField({ network_level: value } as any)}
            />

            {networkLevel === "allowlist" && (
                <PolicyTextList
                    label={t("Network Allowlist", "网络允许列表", "網路允許列表")}
                    desc={t("Allowed hosts for web tools", "Web 工具允许访问的主机", "Web 工具允許訪問的主機")}
                    value={networkAllowlist}
                    disabled={readOnly}
                    onChange={(value) => saveRemoteConfigField({ network_allowlist: value } as any)}
                />
            )}

            <PolicyToggle
                label={t("YOLO Mode", "YOLO 模式", "YOLO 模式")}
                desc={t("Allow auto-execute mode (skip confirmations)", "允许自动执行模式（跳过确认）", "允許自動執行模式（跳過確認）")}
                value={yoloAllowed}
                disabled={readOnly}
                onChange={(value) => saveRemoteConfigField({ yolo_mode_allowed: value } as any)}
            />
            <PolicyToggle
                label={t("Smart Route", "智能路由", "智能路由")}
                desc={t("Allow Hub LLM smart routing for IM messages", "允许 Hub LLM 对 IM 消息做智能路由", "允許 Hub LLM 對 IM 訊息做智能路由")}
                value={smartRouteEnabled}
                disabled={readOnly}
                onChange={(value) => saveRemoteConfigField({ smart_route_enabled: value } as any)}
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

            <PolicyTextList
                label={t("Allowed Skill Sources", "Skill 来源允许列表", "Skill 來源允許列表")}
                desc={t("Leave empty to allow all sources", "留空表示允许所有来源", "留空表示允許所有來源")}
                value={skillSourcesAllowed}
                disabled={readOnly}
                placeholder="skillhub, clawhub, github, enterprise_hub"
                onChange={(value) => saveRemoteConfigField({ skill_sources_allowed: value } as any)}
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
                <div style={{ flex: 1, minWidth: 0, textAlign: "left" }}>
                    <label className="form-label" style={{ fontSize: "0.82rem", marginBottom: 0 }}>{label}</label>
                    <div style={{ fontSize: "0.72rem", color: colors.textMuted }}>{desc}</div>
                </div>
                <select
                    value={value}
                    disabled={disabled}
                    onChange={(event) => onChange(event.target.value)}
                    style={{ flex: "0 0 148px", width: "148px", height: "32px", fontSize: "0.8rem", borderRadius: "6px", border: `1px solid ${colors.border}`, padding: "0 8px", background: colors.surface, color: colors.text }}
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
                <div style={{ flex: 1, minWidth: 0, textAlign: "left" }}>
                    <label className="form-label" style={{ fontSize: "0.82rem", marginBottom: 0 }}>{label}</label>
                    <div style={{ fontSize: "0.72rem", color: colors.textMuted }}>{desc}</div>
                </div>
                <button
                    className={value ? "btn-primary" : "btn-secondary"}
                    style={{ flex: "0 0 60px", minWidth: "60px", height: "28px", fontSize: "0.75rem", padding: "0 10px" }}
                    disabled={disabled}
                    onClick={() => onChange(!value)}
                >
                    {value ? "ON" : "OFF"}
                </button>
            </div>
        </div>
    );
}

function PolicyTextList({ label, desc, value, disabled, placeholder, onChange }: {
    label: string;
    desc: string;
    value: string[];
    disabled: boolean;
    placeholder?: string;
    onChange: (value: string[]) => void;
}) {
    const text = value.join(", ");
    const [draft, setDraft] = useState(text);
    useEffect(() => setDraft(text), [text]);
    const parse = (raw: string) => raw.split(/[\n,]+/).map((item) => item.trim()).filter(Boolean);
    return (
        <div className="form-group" style={{ marginBottom: "12px", ...(disabled ? { opacity: 0.6, pointerEvents: "none" as const } : {}) }}>
            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 16 }}>
                <div style={{ flex: 1, minWidth: 0, textAlign: "left" }}>
                    <label className="form-label" style={{ fontSize: "0.82rem", marginBottom: 0 }}>{label}</label>
                    <div style={{ fontSize: "0.72rem", color: colors.textMuted }}>{desc}</div>
                </div>
                <input
                    value={draft}
                    disabled={disabled}
                    placeholder={placeholder || "api.example.com, *.corp.local"}
                    onChange={(event) => setDraft(event.target.value)}
                    onBlur={(event) => onChange(parse(event.target.value))}
                    style={{ flex: "0 0 220px", width: "220px", height: "32px", fontSize: "0.8rem", borderRadius: "6px", border: `1px solid ${colors.border}`, padding: "0 8px", background: colors.surface, color: colors.text }}
                />
            </div>
        </div>
    );
}

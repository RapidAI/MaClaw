import { useEffect, useState } from 'react';
import { corelib } from '../../../wailsjs/go/models';
import { GetHubSecurityPolicy, IsHubSecurityReadOnly } from "../../../wailsjs/go/main/App";
import { EventsOn } from "../../../wailsjs/runtime/runtime";

type SecurityPolicyMode = "none" | "relaxed" | "standard" | "strict" | "developer";

type Props = {
    config: corelib.AppConfig | null;
    saveRemoteConfigField: (patch: Partial<corelib.AppConfig>) => void;
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

type SecurityModeDefinition = {
    value: SecurityPolicyMode;
    labelZh: string;
    labelZhHant: string;
    labelEn: string;
    hintZh: string;
    hintZhHant: string;
    hintEn: string;
    descZh: string;
    descZhHant: string;
    descEn: string;
};

const SECURITY_MODES: SecurityModeDefinition[] = [
    {
        value: "none",
        labelZh: "关闭护栏",
        labelZhHant: "關閉護欄",
        labelEn: "Off",
        hintZh: "不确认、不阻断",
        hintZhHant: "不確認、不阻斷",
        hintEn: "No confirm, no block",
        descZh: "不做风险护栏的确认/阻断判断；操作仍可能写入安全日志，网络访问、沙箱、文件外发等独立安全开关仍会生效",
        descZhHant: "不做風險護欄的確認/阻斷判斷；操作仍可能寫入安全日誌，網路訪問、沙箱、檔案外發等獨立安全開關仍會生效",
        descEn: "Disables risk-guardrail confirmation and blocking; actions may still be logged, and network access, sandboxing, and outbound-file controls still apply",
    },
    {
        value: "relaxed",
        labelZh: "宽松",
        labelZhHant: "寬鬆",
        labelEn: "Relaxed",
        hintZh: "全部放行，仅记录",
        hintZhHant: "全部放行，僅記錄",
        hintEn: "Allow all, record only",
        descZh: "护栏放行所有风险等级；仍记录风险扫描结果，不阻断 skill 安装/执行",
        descZhHant: "護欄放行所有風險等級；仍記錄風險掃描結果，不阻斷 skill 安裝/執行",
        descEn: "Guardrails allow all risk levels; scan findings are still recorded and do not block skill install/run",
    },
    {
        value: "standard",
        labelZh: "标准",
        labelZhHant: "標準",
        labelEn: "Standard",
        hintZh: "中风险记录，高风险确认",
        hintZhHant: "中風險記錄，高風險確認",
        hintEn: "Medium logged, high confirmed",
        descZh: "低风险放行，中风险记录；高/危险操作有确认界面则询问，否则记录后放行",
        descZhHant: "低風險放行，中風險記錄；高/危險操作有確認介面則詢問，否則記錄後放行",
        descEn: "Low risk is allowed, medium is audited; high/critical asks when a confirmation UI exists, otherwise records and allows",
    },
    {
        value: "strict",
        labelZh: "严格",
        labelZhHant: "嚴格",
        labelEn: "Strict",
        hintZh: "高风险确认，危险阻断",
        hintZhHant: "高風險確認，危險阻斷",
        hintEn: "Confirm high, block critical",
        descZh: "低风险放行，中/高风险需要确认；危险操作和危险命令直接阻止",
        descZhHant: "低風險放行，中/高風險需要確認；危險操作和危險命令直接阻止",
        descEn: "Low risk is allowed, medium/high require confirmation; critical operations and dangerous commands are blocked",
    },
    {
        value: "developer",
        labelZh: "开发者",
        labelZhHant: "開發者",
        labelEn: "Developer",
        hintZh: "旁路放行，仅审计",
        hintZhHant: "旁路放行，僅稽核",
        hintEn: "Bypass, audit only",
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

const SECURITY_TABLE_CELL_TONES: Record<SecurityTableCell, string> = {
    Allow: "allow",
    Audit: "audit",
    Confirm: "confirm",
    "Confirm*": "confirm",
    Deny: "deny",
    Yes: "yes",
    No: "no",
};

const SECURITY_TABLE_COLUMNS = ["off", "relaxed", "standard", "strict", "developer"] as const;

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
        const unsubscribe = EventsOn("hub-security-policy-changed", refresh);
        return () => {
            mounted = false;
            if (typeof unsubscribe === "function") unsubscribe();
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
    const semanticRouting = ((config as any)?.semantic_tool_scope_routing || {}) as Record<string, any>;
    const semanticRoutingEnabled = semanticRouting.enabled === true;
    const semanticRoutingMode = typeof semanticRouting.mode === "string" && semanticRouting.mode.trim() !== "" ? semanticRouting.mode : "off";
    const gossipEnabled = getBool("gossip_enabled", true);
    const fileOutboundEnabled = getBool("file_outbound_enabled", true);
    const imageOutboundEnabled = getBool("image_outbound_enabled", true);
    const skillSourcesAllowed = getArray("skill_sources_allowed");

    const currentMode = SECURITY_MODES.find((item) => item.value === securityMode) || SECURITY_MODES[1];
    const renderSecurityTableCell = (value: SecurityTableCell) => {
        const labels = SECURITY_TABLE_CELL_LABELS[value];
        return t(labels.en, labels.zh, labels.zhHant);
    };

    return (
        <div className="sec-policy">
            {readOnly && (
                <div className="sec-policy__managed" role="note">
                    <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><rect x="3" y="7" width="10" height="7" rx="1.5" /><path d="M5.5 7V5a2.5 2.5 0 0 1 5 0v2" /></svg>
                    <span>{t("Managed by Hub centralized security. Local edits are disabled until Hub centralized policy is turned off.", "当前由 Hub 集中安全策略管控，本地不可修改；关闭 Hub 集中策略后才可编辑", "當前由 Hub 集中安全策略管控，本地不可修改；關閉 Hub 集中策略後才可編輯")}</span>
                </div>
            )}

            <section className={`sec-policy__group${readOnly ? " sec-policy__group--readonly" : ""}`}>
                <header className="sec-policy__group-header">
                    <strong>{t("Risk Guardrails", "风险护栏", "風險護欄")}</strong>
                    <span>{t("How each risk level is confirmed or blocked", "按风险等级决定操作的确认与阻断行为", "按風險等級決定操作的確認與阻斷行為")}</span>
                </header>
                <div className="sec-policy__modes" role="group" aria-label={t("Risk guardrail mode", "护栏模式", "護欄模式")}>
                    {SECURITY_MODES.map((mode) => (
                        <button
                            key={mode.value}
                            type="button"
                            className={`sec-policy__mode${securityMode === mode.value ? " sec-policy__mode--active" : ""}${mode.value === "developer" ? " sec-policy__mode--dev" : ""}`}
                            aria-pressed={securityMode === mode.value}
                            disabled={readOnly}
                            onClick={() => saveRemoteConfigField({ security_policy_mode: mode.value } as any)}
                        >
                            <span className="sec-policy__mode-name">{t(mode.labelEn, mode.labelZh, mode.labelZhHant)}</span>
                            <span className="sec-policy__mode-hint">{t(mode.hintEn, mode.hintZh, mode.hintZhHant)}</span>
                        </button>
                    ))}
                </div>
                <p className="sec-policy__mode-desc">{t(currentMode.descEn, currentMode.descZh, currentMode.descZhHant)}</p>

                <div className="sec-policy__table-wrap">
                    <table className="sec-policy__table">
                        <thead>
                            <tr>
                                <th scope="col">{t("Item", "项目", "項目")}</th>
                                {SECURITY_MODES.map((mode) => (
                                    <th
                                        key={mode.value}
                                        scope="col"
                                        data-current={securityMode === mode.value || undefined}
                                        className={mode.value === "developer" ? "sec-policy__table-dev" : undefined}
                                    >
                                        {t(mode.labelEn, mode.labelZh, mode.labelZhHant)}
                                    </th>
                                ))}
                            </tr>
                        </thead>
                        <tbody>
                            {SECURITY_TABLE_ROWS.map((row) => (
                                <tr key={row.itemEn}>
                                    <th scope="row">{t(row.itemEn, row.itemZh, row.itemZhHant)}</th>
                                    {SECURITY_TABLE_COLUMNS.map((column) => (
                                        <td key={column} data-current={securityMode === column || undefined}>
                                            <span className={`sec-policy__chip sec-policy__chip--${SECURITY_TABLE_CELL_TONES[row[column]]}`}>{renderSecurityTableCell(row[column])}</span>
                                        </td>
                                    ))}
                                </tr>
                            ))}
                        </tbody>
                    </table>
                    <p className="sec-policy__table-note">
                        {t("This table only covers risk guardrails. Relaxed records scan findings without blocking execution; Developer additionally keeps high-risk developer audit records. Network access, sandboxing, and outbound-file controls are configured separately and may still block actions.", "此表只说明风险护栏：宽松会记录风险扫描结果但不阻断执行；开发者模式在此基础上额外保留高风险开发审计记录。网络访问、沙箱、文件外发单独配置，仍可能阻止操作", "此表只說明風險護欄：寬鬆會記錄風險掃描結果但不阻斷執行；開發者模式在此基礎上額外保留高風險開發稽核記錄。網路訪問、沙箱、檔案外發單獨配置，仍可能阻止操作")}
                    </p>
                </div>
            </section>

            <section className="sec-policy__group">
                <header className="sec-policy__group-header">
                    <strong>{t("Isolation & Access", "隔离与访问", "隔離與存取")}</strong>
                    <span>{t("Sandbox and network scope for tool execution; separate from risk guardrails", "工具执行时的沙箱与网络范围；与风险护栏相互独立", "工具執行時的沙箱與網路範圍；與風險護欄相互獨立")}</span>
                </header>
                <div className="sec-policy__rows">
                    <PolicySelect
                        label={t("Execution Sandbox", "执行沙箱", "執行沙箱")}
                        desc={t("Process/filesystem isolation for tool execution", "工具执行时的进程/文件系统隔离", "工具執行時的程序/檔案系統隔離")}
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
                        desc={t("Network scope for agent tools", "Agent 工具可访问的网络范围", "Agent 工具可存取的網路範圍")}
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
                            desc={t("Allowed hosts for web tools", "Web 工具允许访问的主机", "Web 工具允許存取的主機")}
                            value={networkAllowlist}
                            disabled={readOnly}
                            onChange={(value) => saveRemoteConfigField({ network_allowlist: value } as any)}
                        />
                    )}
                    <PolicyTextList
                        label={t("Allowed Skill Sources", "Skill 来源允许列表", "Skill 來源允許列表")}
                        desc={t("Leave empty to allow all sources", "留空表示允许所有来源", "留空表示允許所有來源")}
                        value={skillSourcesAllowed}
                        disabled={readOnly}
                        placeholder="skillhub, clawhub, github, enterprise_hub"
                        onChange={(value) => saveRemoteConfigField({ skill_sources_allowed: value } as any)}
                    />
                </div>
            </section>

            <section className="sec-policy__group">
                <header className="sec-policy__group-header">
                    <strong>{t("Capabilities & Routing", "能力与路由", "能力與路由")}</strong>
                    <span>{t("Feature switches applied to conversations and tools", "作用于会话与工具的功能开关", "作用於會話與工具的功能開關")}</span>
                </header>
                <div className="sec-policy__rows">
                    <PolicyToggle
                        label={t("YOLO Mode", "YOLO 模式", "YOLO 模式")}
                        desc={t("Allow auto-execute mode (skip confirmations)", "允许自动执行模式（跳过确认）", "允許自動執行模式（跳過確認）")}
                        value={yoloAllowed}
                        disabled={readOnly}
                        onChange={(value) => saveRemoteConfigField({ yolo_mode_allowed: value } as any)}
                    />
                    <PolicyToggle
                        label={t("Smart Route", "智能路由", "智能路由")}
                        desc={t("Allow Hub LLM message routing", "允许 Hub LLM 对消息做智能路由", "允許 Hub LLM 對訊息做智能路由")}
                        value={smartRouteEnabled}
                        disabled={readOnly}
                        onChange={(value) => saveRemoteConfigField({ smart_route_enabled: value } as any)}
                    />
                    <PolicyToggle
                        label={t("Task-scoped Tool Routing", "任务范围工具路由", "任務範圍工具路由")}
                        desc={t("Plan tools from the task scope; disabled keeps the governed path fail-closed", "按任务范围规划工具；关闭时受治理路径保持拒绝执行", "按任務範圍規劃工具；關閉時受治理路徑保持拒絕執行")}
                        value={semanticRoutingEnabled}
                        disabled={readOnly}
                        onChange={(value) => saveRemoteConfigField({
                            semantic_tool_scope_routing: { ...semanticRouting, enabled: value },
                        } as any)}
                    />
                    <PolicySelect
                        label={t("Task Routing Mode", "任务路由模式", "任務路由模式")}
                        desc={t("Scope-only publishes executable plans; shadow is observational", "scope-only 发布可执行计划；shadow 仅用于观察", "scope-only 發佈可執行計劃；shadow 僅用於觀察")}
                        value={semanticRoutingMode}
                        options={["off", "shadow", "scope_only"]}
                        labels={["Off", "Shadow", "Scope only"]}
                        disabled={readOnly || !semanticRoutingEnabled}
                        onChange={(value) => saveRemoteConfigField({
                            semantic_tool_scope_routing: { ...semanticRouting, mode: value },
                        } as any)}
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
                        desc={t("Allow sending files via IM channels", "允许通过 IM 通道发送文件", "允許透過 IM 通道發送檔案")}
                        value={fileOutboundEnabled}
                        disabled={readOnly}
                        onChange={(value) => saveRemoteConfigField({ file_outbound_enabled: value } as any)}
                    />
                    <PolicyToggle
                        label={t("Image Outbound", "图片外发", "圖片外發")}
                        desc={t("Allow sending images via IM channels", "允许通过 IM 通道发送图片", "允許透過 IM 通道發送圖片")}
                        value={imageOutboundEnabled}
                        disabled={readOnly}
                        onChange={(value) => saveRemoteConfigField({ image_outbound_enabled: value } as any)}
                    />
                </div>
            </section>

            <section className="sec-policy__group sec-policy__group--audit">
                <header className="sec-policy__group-header">
                    <strong>{t("Audit Log", "审计日志", "審計日誌")}</strong>
                </header>
                <div className="sec-policy__audit">
                    <div className="sec-policy__audit-row">
                        <span>{t("Location", "存储位置", "存儲位置")}</span>
                        <code>~/.maclaw/audit/</code>
                    </div>
                    <div>{t("Query via IM using query_audit_log tool", "可通过 IM 发送消息调用 query_audit_log 工具查询", "可透過 IM 發送訊息呼叫 query_audit_log 工具查詢")}</div>
                    <div>{t("Auto-rotated daily, retained for 30 days", "日志按日期自动轮转，保留 30 天", "日誌按日期自動輪轉，保留 30 天")}</div>
                </div>
            </section>
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
        <div className={`sec-policy__row${disabled ? " sec-policy__row--disabled" : ""}`}>
            <div className="sec-policy__row-text">
                <span className="sec-policy__row-label">{label}</span>
                <span className="sec-policy__row-desc">{desc}</span>
            </div>
            <select
                className="sec-policy__control"
                value={value}
                disabled={disabled}
                aria-label={label}
                onChange={(event) => onChange(event.target.value)}
            >
                {options.map((option, index) => (
                    <option key={option} value={option}>{labels[index]}</option>
                ))}
            </select>
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
        <div className={`sec-policy__row${disabled ? " sec-policy__row--disabled" : ""}`}>
            <div className="sec-policy__row-text">
                <span className="sec-policy__row-label">{label}</span>
                <span className="sec-policy__row-desc">{desc}</span>
            </div>
            <label className="sec-policy__switch">
                <input
                    type="checkbox"
                    aria-label={label}
                    disabled={disabled}
                    checked={value}
                    onChange={(event) => onChange(event.target.checked)}
                />
                <span className="sec-policy__switch-track" aria-hidden="true" />
            </label>
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
        <div className={`sec-policy__row${disabled ? " sec-policy__row--disabled" : ""}`}>
            <div className="sec-policy__row-text">
                <span className="sec-policy__row-label">{label}</span>
                <span className="sec-policy__row-desc">{desc}</span>
            </div>
            <input
                className="sec-policy__control sec-policy__control--wide"
                value={draft}
                disabled={disabled}
                aria-label={label}
                placeholder={placeholder || "api.example.com, *.corp.local"}
                onChange={(event) => setDraft(event.target.value)}
                onBlur={(event) => onChange(parse(event.target.value))}
            />
        </div>
    );
}

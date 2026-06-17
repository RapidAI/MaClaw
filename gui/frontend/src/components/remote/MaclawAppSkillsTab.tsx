import type { CSSProperties } from "react";
import {
    colors,
    remoteErrorStateStyle,
    remoteLoadingStateStyle,
} from "./styles";

type LocalizeText = (en: string, zhHans: string, zhHant: string) => string;

export interface MaclawAppSkillDefinition {
    name: string;
    description: string;
    status: string;
    usage_count?: number;
    success_rate?: number;
    hub_skill_id?: string;
    maclaw_app_count?: number;
    maclaw_app_entry?: string;
}

export function MaclawAppSkillsTab({
    skills,
    loading,
    error,
    busy,
    localizeText,
    onRefresh,
    onOpenAppPanel,
    onOpenMarket,
    onEdit,
    onDelete,
    onUpload,
    uploadingSkill,
}: {
    skills: MaclawAppSkillDefinition[];
    loading: boolean;
    error: string;
    busy: boolean;
    localizeText: LocalizeText;
    onRefresh: () => void;
    onOpenAppPanel?: () => void;
    onOpenMarket: () => void;
    onEdit: (skill: MaclawAppSkillDefinition) => void;
    onDelete: (name: string) => void;
    onUpload: (name: string) => void;
    uploadingSkill: string | null;
}) {
    const statusLabel = (status: string) => {
        switch ((status || "").trim().toLowerCase()) {
            case "active": return localizeText("Active", "启用", "啟用");
            case "disabled": return localizeText("Disabled", "停用", "停用");
            default: return status || "-";
        }
    };

    return (
        <>
            <div style={toolbarStyle}>
                <span style={{ fontSize: "0.78rem", color: colors.textSecondary }}>
                    {skills.length} {localizeText("MaClaw App skill(s)", "个 MaClaw App Skill", "個 MaClaw App Skill")}
                </span>
                <div style={actionBarStyle}>
                    <button className="btn-secondary" style={buttonStyle} onClick={onRefresh} disabled={loading}>
                        {loading ? localizeText("Refreshing...", "刷新中...", "重新整理中...") : localizeText("Refresh", "刷新", "重新整理")}
                    </button>
                    <button className="btn-secondary" style={buttonStyle} onClick={onOpenAppPanel}>
                        {localizeText("Open App Panel", "打开应用面板", "開啟應用面板")}
                    </button>
                    <button className="btn-primary" style={buttonStyle} onClick={onOpenMarket}>
                        {localizeText("Upload / Market", "上传 / 能力市场", "上傳 / 能力市場")}
                    </button>
                </div>
            </div>
            <div style={hintStyle}>
                {localizeText(
                    "MaClaw App skills contain maclaw.app.json or maclaw.apps.json and can be opened from the app panel after registration.",
                    "MaClaw App Skill 包含 maclaw.app.json 或 maclaw.apps.json；注册后可加入应用面板打开。",
                    "MaClaw App Skill 包含 maclaw.app.json 或 maclaw.apps.json；註冊後可加入應用面板開啟。",
                )}
            </div>

            {loading && <div style={remoteLoadingStateStyle}>{localizeText("Loading...", "加载中...", "載入中...")}</div>}
            {error && <div style={remoteErrorStateStyle}>{error}</div>}

            {!loading && skills.length > 0 && (
                <div style={tableWrapStyle}>
                    <table style={tableStyle}>
                        <thead>
                            <tr style={{ background: colors.surfaceMuted }}>
                                <th style={{ ...thStyle, width: "150px" }}>{localizeText("Name", "名称", "名稱")}</th>
                                <th style={thStyle}>{localizeText("Description", "描述", "描述")}</th>
                                <th style={{ ...thStyle, width: "130px" }}>{localizeText("App Definition", "应用定义", "應用定義")}</th>
                                <th style={{ ...thStyle, width: "72px", textAlign: "center" }}>{localizeText("Apps", "应用数", "應用數")}</th>
                                <th style={{ ...thStyle, width: "80px" }}>{localizeText("Usage", "使用统计", "使用統計")}</th>
                                <th style={{ ...thStyle, width: "60px", textAlign: "center" }}>{localizeText("Status", "状态", "狀態")}</th>
                                <th style={{ ...thStyle, width: "150px", textAlign: "center" }}>{localizeText("Actions", "操作", "操作")}</th>
                            </tr>
                        </thead>
                        <tbody>
                            {skills.map((skill) => (
                                <tr key={skill.name} style={{ borderTop: `1px solid ${colors.border}` }}>
                                    <td style={tdStyle}>{skill.name}</td>
                                    <td style={tdStyle}><div style={descStyle} title={skill.description || undefined}>{skill.description || "-"}</div></td>
                                    <td style={tdStyle}><span style={badgeStyle}>{skill.maclaw_app_entry || "maclaw.apps.json"}</span></td>
                                    <td style={{ ...tdStyle, textAlign: "center" }}>{skill.maclaw_app_count || 0}</td>
                                    <td style={tdStyle}>
                                        {(skill.usage_count ?? 0) > 0
                                            ? `${skill.usage_count}${localizeText("x", "次", "次")} / ${Math.round((skill.success_rate ?? 0) * 100)}%`
                                            : <span style={{ color: colors.textMuted }}>{localizeText("Unused", "未使用", "未使用")}</span>}
                                    </td>
                                    <td style={{ ...tdStyle, textAlign: "center" }}><span style={statusStyle}>{statusLabel(skill.status)}</span></td>
                                    <td style={{ ...tdStyle, textAlign: "center" }}>
                                        <div style={rowActionsStyle}>
                                            <button className="btn-secondary" style={iconButtonStyle} onClick={() => onEdit(skill)} disabled={busy} title={localizeText("Edit app skill", "编辑 App Skill", "編輯 App Skill")} aria-label={localizeText("Edit app skill", "编辑 App Skill", "編輯 App Skill")}>{"\u270E"}</button>
                                            <button className="btn-secondary" style={deleteButtonStyle} onClick={() => onDelete(skill.name)} disabled={busy} title={localizeText("Delete", "删除", "刪除")} aria-label={localizeText("Delete", "删除", "刪除")}>×</button>
                                            <button className="btn-secondary" style={uploadButtonStyle} onClick={() => onUpload(skill.name)} disabled={busy || uploadingSkill === skill.name}>
                                                {uploadingSkill === skill.name ? localizeText("Uploading...", "上传中...", "上傳中...") : skill.hub_skill_id ? localizeText("Re-upload", "重新上传", "重新上傳") : localizeText("Upload", "上传", "上傳")}
                                            </button>
                                            {skill.hub_skill_id && <span style={uploadedBadgeStyle} title={localizeText("Uploaded to Capability Market", "已上传到能力市场", "已上傳到能力市場")}>OK</span>}
                                        </div>
                                    </td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            )}

            {!loading && skills.length === 0 && !error && (
                <div style={emptyStyle}>{localizeText("No MaClaw App skills yet", "暂无 MaClaw App Skill", "暫無 MaClaw App Skill")}</div>
            )}
        </>
    );
}

const toolbarStyle: CSSProperties = { display: "flex", justifyContent: "space-between", gap: 10, alignItems: "center" };
const actionBarStyle: CSSProperties = { display: "flex", gap: 8, alignItems: "center" };
const buttonStyle: CSSProperties = { fontSize: "0.78rem", padding: "4px 12px" };
const hintStyle: CSSProperties = { fontSize: "0.74rem", color: colors.textSecondary, lineHeight: 1.45 };
const tableWrapStyle: CSSProperties = { overflow: "auto", border: `1px solid ${colors.border}`, borderRadius: 8 };
const tableStyle: CSSProperties = { width: "100%", borderCollapse: "collapse", fontSize: "0.76rem" };
const thStyle: CSSProperties = { padding: "7px 8px", textAlign: "left", color: colors.textSecondary, fontWeight: 700 };
const tdStyle: CSSProperties = { padding: "7px 8px", color: colors.text, verticalAlign: "middle" };
const descStyle: CSSProperties = { maxWidth: 360, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" };
const badgeStyle: CSSProperties = { display: "inline-flex", padding: "2px 6px", borderRadius: 6, background: colors.surfaceMuted, color: colors.textSecondary, fontSize: "0.7rem" };
const statusStyle: CSSProperties = { display: "inline-flex", padding: "2px 6px", borderRadius: 6, background: colors.successBg, color: colors.success, fontSize: "0.7rem", fontWeight: 700 };
const iconButtonStyle: CSSProperties = { width: 26, height: 24, padding: 0, marginRight: 4 };
const deleteButtonStyle: CSSProperties = { ...iconButtonStyle, color: colors.danger };
const rowActionsStyle: CSSProperties = { display: "inline-flex", alignItems: "center", justifyContent: "center", gap: 4 };
const uploadButtonStyle: CSSProperties = { fontSize: "0.7rem", padding: "3px 8px", minWidth: 58 };
const uploadedBadgeStyle: CSSProperties = { fontSize: "0.68rem", color: colors.success, fontWeight: 800 };
const emptyStyle: CSSProperties = { padding: "28px 8px", textAlign: "center", color: colors.textMuted, fontSize: "0.8rem" };

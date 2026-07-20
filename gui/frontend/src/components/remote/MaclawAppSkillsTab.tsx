import type { CSSProperties } from "react";
import { formatMiniAppSkillCount, localizeMiniAppPack, miniAppLabels } from "../../i18n/maclawMiniAppLabels";
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
            case "needs_setup": return localizeText("Needs Setup", "待配置", "待設定");
            case "error": return localizeText("Error", "异常", "異常");
            default: return status || "-";
        }
    };

    const openPanelLabel = localizeMiniAppPack(localizeText, miniAppLabels.openPanel);
    const skillsHintLabel = localizeMiniAppPack(localizeText, miniAppLabels.skillsHint);
    const definitionColumnLabel = localizeMiniAppPack(localizeText, miniAppLabels.definitionColumn);
    const countColumnLabel = localizeMiniAppPack(localizeText, miniAppLabels.countColumn);
    const editSkillLabel = localizeMiniAppPack(localizeText, miniAppLabels.editSkill);
    const noSkillsYetLabel = localizeMiniAppPack(localizeText, miniAppLabels.noSkillsYet);
    const browseMarketSkillsLabel = localizeMiniAppPack(localizeText, miniAppLabels.browseMarketSkills);

    return (
        <>
            <div style={toolbarStyle}>
                <span style={{ fontSize: "0.78rem", color: colors.textSecondary }}>
                    {formatMiniAppSkillCount(skills.length, localizeText)}
                </span>
                <div style={actionBarStyle}>
                    <button className="btn-secondary" style={buttonStyle} onClick={onRefresh} disabled={loading}>
                        {loading ? localizeText("Refreshing...", "刷新中...", "重新整理中...") : localizeText("Refresh", "刷新", "重新整理")}
                    </button>
                    <button className="btn-primary" style={buttonStyle} onClick={onOpenAppPanel} disabled={!onOpenAppPanel}>
                        {openPanelLabel}
                    </button>
                    <button className="btn-secondary" style={buttonStyle} onClick={onOpenMarket}>
                        {localizeText("Market", "能力市场", "能力市場")}
                    </button>
                </div>
            </div>
            <div style={hintStyle}>
                {skillsHintLabel}
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
                                <th style={{ ...thStyle, width: "130px" }}>{definitionColumnLabel}</th>
                                <th style={{ ...thStyle, width: "72px", textAlign: "center" }}>{countColumnLabel}</th>
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
                                    <td style={{ ...tdStyle, textAlign: "center" }}><span style={statusBadgeStyleForStatus((skill.status || "").trim().toLowerCase())}>{statusLabel(skill.status)}</span></td>
                                    <td style={{ ...tdStyle, textAlign: "center" }}>
                                        <div style={rowActionsStyle}>
                                            <button className="btn-secondary" style={iconButtonStyle} onClick={() => onEdit(skill)} disabled={busy} title={editSkillLabel} aria-label={editSkillLabel}>{localizeText("Edit", "编辑", "編輯")}</button>
                                            <button className="btn-secondary" style={deleteButtonStyle} onClick={() => onDelete(skill.name)} disabled={busy} title={localizeText("Delete", "删除", "刪除")} aria-label={localizeText("Delete", "删除", "刪除")}>×</button>
                                            <button className="btn-secondary" style={uploadButtonStyle} onClick={() => onUpload(skill.name)} disabled={busy || uploadingSkill === skill.name} aria-label={`${uploadingSkill === skill.name ? localizeText("Uploading", "上传中", "上傳中") : localizeText("Upload", "上传", "上傳")} ${skill.name}`}>
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
                <div style={emptyStyle}>
                    <div>{noSkillsYetLabel}</div>
                    <button
                        type="button"
                        onClick={onOpenMarket}
                        style={emptyMarketLinkStyle}
                    >
                        {browseMarketSkillsLabel}
                    </button>
                </div>
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
const statusBaseStyle: CSSProperties = { display: "inline-flex", padding: "2px 6px", borderRadius: 6, fontSize: "0.7rem", fontWeight: 700 };
const statusStyles: Record<string, CSSProperties> = {
    active: { ...statusBaseStyle, background: colors.successBg, color: colors.success },
    needs_setup: { ...statusBaseStyle, background: colors.warningBg, color: colors.warning },
    error: { ...statusBaseStyle, background: colors.dangerBg, color: colors.danger },
    _default: { ...statusBaseStyle, background: colors.surfaceMuted, color: colors.textMuted },
};

function statusBadgeStyleForStatus(status: string): CSSProperties {
    return statusStyles[status] || statusStyles._default;
}
const iconButtonStyle: CSSProperties = { width: 26, height: 24, padding: 0, marginRight: 4 };
const deleteButtonStyle: CSSProperties = { ...iconButtonStyle, color: colors.danger };
const rowActionsStyle: CSSProperties = { display: "inline-flex", alignItems: "center", justifyContent: "center", gap: 4 };
const uploadButtonStyle: CSSProperties = { fontSize: "0.7rem", padding: "3px 8px", minWidth: 58 };
const uploadedBadgeStyle: CSSProperties = { fontSize: "0.68rem", color: colors.success, fontWeight: 800 };
const emptyStyle: CSSProperties = { padding: "28px 8px", textAlign: "center", color: colors.textMuted, fontSize: "0.8rem" };
const emptyMarketLinkStyle: CSSProperties = { marginTop: "10px", background: "transparent", border: "none", color: colors.link, cursor: "pointer", fontSize: "0.78rem", textDecoration: "underline" };

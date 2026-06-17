import type { CSSProperties } from "react";
import { colors } from "./styles";

type LocalizeText = (en: string, zhHans: string, zhHant: string) => string;

export interface MaclawAppMarketPreviewSource {
    is_maclaw_app?: boolean;
    maclaw_app_name?: string;
    maclaw_app_description?: string;
    maclaw_app_category?: string;
    maclaw_app_icon?: string;
    maclaw_app_input_mode?: string;
    maclaw_app_output_modes?: string[];
    maclaw_app_definition_sha256?: string;
    maclaw_app_test_evidence?: {
        run_id?: string;
        verified_at?: string;
        definition_fingerprint?: string;
        artifact_present?: boolean;
        artifact_name?: string;
    };
    artifact_contract_required?: boolean;
    artifact_contract_output_modes?: string[];
    artifact_contract_presentation?: string;
}

export function maclawAppArtifactModesLabel(skill: MaclawAppMarketPreviewSource): string {
    const modes = skill.artifact_contract_output_modes?.length
        ? skill.artifact_contract_output_modes
        : skill.maclaw_app_output_modes || [];
    return modes.filter(Boolean).join(" / ");
}

export function MaclawAppMarketPreview({ skill, localizeText }: { skill: MaclawAppMarketPreviewSource; localizeText: LocalizeText }) {
    if (!skill.is_maclaw_app) return null;
    const outputModes = maclawAppArtifactModesLabel(skill);
    const hasSummary = !!(skill.maclaw_app_name || skill.maclaw_app_category || skill.maclaw_app_icon || outputModes);
    const evidence = skill.maclaw_app_test_evidence;
    const hasDetails = !!(skill.maclaw_app_description || skill.maclaw_app_input_mode || skill.artifact_contract_presentation || skill.maclaw_app_definition_sha256 || evidence?.run_id || evidence?.verified_at);
    if (!hasSummary && !hasDetails) return null;
    return (
        <details style={previewStyle}>
            <summary style={summaryStyle}>
                {skill.maclaw_app_icon && <span>{skill.maclaw_app_icon}</span>}
                {skill.maclaw_app_name && <span>{localizeText("App", "应用", "應用")}: {skill.maclaw_app_name}</span>}
                {skill.maclaw_app_category && <span>{skill.maclaw_app_category}</span>}
                {outputModes && <span>{outputModes}</span>}
            </summary>
            <div style={detailGridStyle}>
                {skill.maclaw_app_description && <div style={wideItemStyle}>{skill.maclaw_app_description}</div>}
                <div><strong>{localizeText("Input", "输入", "輸入")}</strong><span>{skill.maclaw_app_input_mode || "-"}</span></div>
                <div><strong>{localizeText("Output", "输出", "輸出")}</strong><span>{outputModes ? localizeText(`${outputModes} contract`, `${outputModes} 契约`, `${outputModes} 契約`) : "-"}</span></div>
                <div><strong>{localizeText("Presentation", "呈现", "呈現")}</strong><span>{skill.artifact_contract_presentation || "-"}</span></div>
                <div><strong>{localizeText("Artifact", "产物", "產物")}</strong><span>{skill.artifact_contract_required ? localizeText("required", "必需", "必需") : "-"}</span></div>
                {evidence && <div style={wideItemStyle}><strong>{localizeText("Test", "测试", "測試")}</strong><span>{[evidence.run_id, evidence.verified_at, evidence.artifact_name].filter(Boolean).join(" · ") || "-"}</span></div>}
                {skill.maclaw_app_definition_sha256 && <div style={hashItemStyle}><strong>SHA256</strong><span>{skill.maclaw_app_definition_sha256}</span></div>}
            </div>
        </details>
    );
}

const previewStyle: CSSProperties = {
    marginTop: "6px",
    fontSize: "0.68rem",
    color: colors.textMuted,
};

const summaryStyle: CSSProperties = {
    display: "flex",
    gap: "6px",
    flexWrap: "wrap",
    cursor: "pointer",
    userSelect: "none",
};

const detailGridStyle: CSSProperties = {
    display: "grid",
    gridTemplateColumns: "repeat(auto-fit, minmax(120px, 1fr))",
    gap: "6px 10px",
    marginTop: "6px",
    padding: "8px",
    border: `1px solid ${colors.border}`,
    borderRadius: "6px",
    background: colors.surfaceMuted,
};

const wideItemStyle: CSSProperties = {
    gridColumn: "1 / -1",
    color: colors.textSecondary,
};

const hashItemStyle: CSSProperties = {
    gridColumn: "1 / -1",
    minWidth: 0,
    wordBreak: "break-all",
};

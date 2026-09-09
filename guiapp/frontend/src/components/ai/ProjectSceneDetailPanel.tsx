import type { CSSProperties } from "react";
import { OpenFileOrShowInFolder } from "../../../wailsjs/go/main/App";
import type { Theme } from "./aiAssistantPanelTheme";
import { localizeText } from "./aiAssistantI18n";

export interface ProjectSearchArtifact {
    title?: string;
    source_type?: string;
    source_url?: string;
    source_hint?: string;
    preview?: string;
    updated_at?: string;
}

export interface ProjectSceneDetail {
    project_path: string;
    name?: string;
    active_workflow?: {
        id?: string;
        type?: string;
        phase?: string;
        status?: string;
        project_path?: string;
        pending_review?: boolean;
    };
    workflow_types?: string[];
    tags?: string[];
    source_urls?: string[];
    recent_artifacts?: ProjectSearchArtifact[];
    entry_count?: number;
    last_activity?: string;
    preview?: string;
}

function iconButtonStyle(t: Theme, opacity = 0.72): CSSProperties {
    return {
        border: "none",
        background: "transparent",
        color: t.headingColor,
        opacity,
        cursor: "pointer",
        width: "22px",
        height: "22px",
        padding: 0,
        borderRadius: "4px",
        flexShrink: 0,
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        lineHeight: 1,
    };
}

function OpenSourceIcon() {
    return <svg width="13" height="13" viewBox="0 0 24 24" aria-hidden="true" focusable="false" style={{ display: "block" }}>
        <path d="M14 3h7v7" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
        <path d="M10 14 21 3" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
        <path d="M21 14v5a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
    </svg>;
}

function CloseIcon() {
    return <svg width="12" height="12" viewBox="0 0 24 24" aria-hidden="true" focusable="false" style={{ display: "block" }}>
        <path d="M18 6 6 18M6 6l12 12" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
    </svg>;
}

export function ProjectSceneDetailPanel({ detail, loading, lang, theme: t, formatTime, onClose }: {
    detail: ProjectSceneDetail | null;
    loading: boolean;
    lang: string;
    theme: Theme;
    formatTime: (iso?: string) => string;
    onClose: () => void;
}) {
    const artifacts = detail?.recent_artifacts || [];
    return <div style={{ borderTop: `1px solid ${t.titleBarBorder}`, padding: "8px 12px 10px", background: t.titleBarBg }}>
        <div style={{ display: "flex", alignItems: "center", gap: "8px", marginBottom: "6px" }}>
            <span style={{ fontSize: "12px", fontWeight: 700, color: t.text, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", flex: 1 }}>{loading ? localizeText(lang, "Loading scene...", "正在加载证据...") : (detail?.name || detail?.project_path || localizeText(lang, "Scene details", "任务证据详情"))}</span>
            {detail?.entry_count !== undefined && <span style={{ fontSize: "10px", color: t.text, opacity: 0.45, flexShrink: 0 }}>{detail.entry_count}</span>}
            <button type="button" onClick={onClose} style={{ ...iconButtonStyle(t, 0.52), color: t.text }} title={localizeText(lang, "Close", "关闭")} aria-label={localizeText(lang, "Close", "关闭")}><CloseIcon /></button>
        </div>
        {detail?.last_activity && <div style={{ fontSize: "10px", color: t.text, opacity: 0.35, marginBottom: "5px" }}>{formatTime(detail.last_activity)}</div>}
        {!loading && artifacts.length === 0 && <div style={{ fontSize: "11px", color: t.text, opacity: 0.42 }}>{localizeText(lang, "No recent artifacts", "暂无最近产物")}</div>}
        {artifacts.slice(0, 5).map((artifact, index) => {
            const label = artifact.title || artifact.preview || artifact.source_url || localizeText(lang, "Artifact", "产物");
            const source = artifact.source_url ? artifact.source_url + (artifact.source_hint ? "; " + artifact.source_hint : "") : "";
            return <div key={artifact.source_url || label + index} style={{ display: "flex", alignItems: "center", gap: "6px", minWidth: 0, marginTop: "4px" }}>
                <span title={source || label} style={{ fontSize: "11px", color: t.text, opacity: 0.72, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", minWidth: 0, flex: 1 }}>{label}</span>
                {artifact.source_url && <button type="button" onClick={() => void OpenFileOrShowInFolder(artifact.source_url || "")} style={iconButtonStyle(t)} title={source} aria-label={localizeText(lang, "Open artifact source", "打开产物来源")}><OpenSourceIcon /></button>}
            </div>;
        })}
    </div>;
}

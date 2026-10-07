import { OpenFileOrShowInFolder, RenameTask } from "../../../wailsjs/go/main/App";
import type { Theme } from "./aiAssistantPanelTheme";
import { localizeText } from "./aiAssistantI18n";
import { agentModeFromTaskTags, isCodingWorkflowSourceTags, isPureCodingTaskTags, isRemoteMaintenanceTaskTags, remoteHostFromTaskTags } from "./codingTaskMode";
import { formatArtifactSummary, formatWorkflowType } from "./projectSearchFormat";
import { ProjectSearchIcon } from "./ProjectSearchIcon";
import type { ProjectSearchItem } from "./projectSearchTypes";

export function ProjectSearchRow({ item, lang, theme: t, formatTime, renamingPath, renameVal, setRenameVal, setRenamingPath, onSelect, onShowSceneDetail, sceneLoading, refreshResults, setCtxMenu }: {
    item: ProjectSearchItem; lang: string; theme: Theme; formatTime: (iso?: string) => string; renamingPath: string | null; renameVal: string; setRenameVal: (value: string) => void; setRenamingPath: (path: string | null) => void; onSelect: (item: ProjectSearchItem) => void | Promise<void>; onShowSceneDetail: (item: ProjectSearchItem) => void | Promise<void>; sceneLoading: boolean; refreshResults: () => void; setCtxMenu: (menu: { x: number; y: number; item: ProjectSearchItem } | null) => void;
}) {
    const artifact = item.recent_artifacts?.find(a => a.title || a.preview || a.source_url);
    const artifactSummary = formatArtifactSummary(item, lang);
    const artifactTooltip = formatArtifactSummary(item, lang, true);
    const pureCoding = isPureCodingTaskTags(item.tags);
    const remoteCoding = agentModeFromTaskTags(item.tags) === "remote_coding_dev";
    const remoteMaintenance = remoteCoding && isRemoteMaintenanceTaskTags(item.tags);
    const remoteHost = remoteHostFromTaskTags(item.tags);
    const fromCodingWorkflow = isCodingWorkflowSourceTags(item.tags);
    const kindLabel = item.archived
        ? "ARC"
        : remoteMaintenance
            ? "OPS"
            : remoteCoding
            ? "SSH"
            : pureCoding
                ? "CODE"
                : item.pinned
                    ? "PIN"
                    : "TASK";
    return <div data-pure-coding={pureCoding ? "true" : "false"} onClick={() => void onSelect(item)} onKeyDown={event => { if (event.target === event.currentTarget && (event.key === "Enter" || event.key === " ")) { event.preventDefault(); void onSelect(item); } }} onContextMenu={event => { event.preventDefault(); setCtxMenu({ x: event.clientX, y: event.clientY, item }); }} role="button" tabIndex={0} aria-label={item.name || item.project_path} className="psp-row" onMouseEnter={event => (event.currentTarget.style.background = t.codeBlockBg)} onMouseLeave={event => (event.currentTarget.style.background = "transparent")}>
        <div className="psp-row-head">
            <span style={{ minWidth: "26px", textAlign: "center", fontSize: "10px", fontWeight: 700, color: pureCoding ? (remoteCoding ? "var(--theme-primary-strong)" : "var(--mc-done, color-mix(in srgb, var(--theme-primary) 20%, var(--theme-text-secondary)))") : t.textMuted, border: pureCoding ? `1px solid ${remoteCoding ? "color-mix(in srgb, var(--theme-primary) 48%, transparent)" : "color-mix(in srgb, var(--theme-primary) 26%, transparent)"}` : `1px solid ${t.titleBarBorder}`, borderRadius: "4px", padding: "1px 4px", flexShrink: 0 }} title={pureCoding ? (remoteMaintenance ? localizeText(lang, "Remote maintenance", "远程维护") : remoteCoding ? localizeText(lang, "Remote pure coding", "远程纯编程") : localizeText(lang, "Local pure coding", "本地纯编程")) : undefined}>{kindLabel}</span>
            {renamingPath === item.project_path ? <input autoFocus value={renameVal} onChange={event => setRenameVal(event.target.value)} onBlur={async () => { const trimmed = renameVal.trim(); if (trimmed && trimmed !== item.name) { await RenameTask(item.project_path, trimmed); refreshResults(); } setRenamingPath(null); }} onKeyDown={event => { if (event.key === "Enter") (event.target as HTMLInputElement).blur(); if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); setRenamingPath(null); } }} onClick={event => event.stopPropagation()} style={{ flex: 1, fontSize: "13px", fontWeight: 600, color: t.text, background: t.codeBlockBg, border: `1px solid ${t.headingColor}`, borderRadius: "3px", padding: "2px 6px", outline: "none", minWidth: 0, fontFamily: "inherit" }} /> : <span style={{ fontSize: "13px", fontWeight: 600, color: t.text, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", flex: 1 }}>{item.name || item.project_path}</span>}
            {pureCoding && <span data-testid={remoteCoding ? "search-remote-coding-badge" : "search-coding-badge"} style={{ fontSize: "10px", padding: "1px 6px", borderRadius: "999px", background: remoteCoding ? "color-mix(in srgb, var(--theme-primary) 12%, transparent)" : "color-mix(in srgb, var(--theme-primary) 7%, transparent)", color: remoteCoding ? "var(--theme-primary-strong)" : "var(--mc-done, color-mix(in srgb, var(--theme-primary) 20%, var(--theme-text-secondary)))", border: remoteCoding ? "1px solid color-mix(in srgb, var(--theme-primary) 48%, transparent)" : "1px solid color-mix(in srgb, var(--theme-primary) 26%, transparent)", flexShrink: 0, maxWidth: 140, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{remoteCoding ? (remoteHost ? `${remoteMaintenance ? localizeText(lang, "Remote maintenance", "远程维护") : localizeText(lang, "Remote coding", "远程编程")} · ${remoteHost}` : (remoteMaintenance ? localizeText(lang, "Remote maintenance", "远程维护") : localizeText(lang, "Remote coding", "远程编程"))) : localizeText(lang, "Pure coding", "纯编程")}</span>}
            {fromCodingWorkflow && <span data-testid="search-coding-workflow-source-badge" className="psp-wf-badge" title={localizeText(lang, "Created from coding workflow", "由编程工作流创建")}>{localizeText(lang, "Workflow", "工作流")}</span>}
            {item.workflow_type && <span style={{ fontSize: "10px", padding: "1px 6px", borderRadius: "999px", background: "rgba(47,111,188,0.10)", color: t.headingColor, border: `1px solid ${t.titleBarBorder}`, flexShrink: 0 }}>{formatWorkflowType(item.workflow_type, lang)}</span>}
            {item.archived && <span style={{ fontSize: "10px", padding: "1px 6px", borderRadius: "999px", background: "rgba(100,116,139,0.10)", color: t.textMuted, border: `1px solid ${t.titleBarBorder}`, flexShrink: 0 }}>{localizeText(lang, "Archived", "\u5df2\u5f52\u6863")}</span>}
            <button type="button" onClick={event => { event.stopPropagation(); void onShowSceneDetail(item); }} style={{ border: "none", background: "transparent", color: t.headingColor, opacity: sceneLoading ? 0.35 : 0.7, width: "20px", height: "20px", cursor: sceneLoading ? "default" : "pointer", flexShrink: 0, fontSize: "12px" }} disabled={sceneLoading} title={localizeText(lang, "Scene details", "任务证据详情")}>{sceneLoading ? "..." : <ProjectSearchIcon name="info" />}</button>
            <button type="button" onClick={event => { event.stopPropagation(); void onSelect(item); }} style={{ border: "none", background: item.archived ? "rgba(100,116,139,0.10)" : "rgba(47,111,188,0.10)", color: item.archived ? t.textMuted : t.headingColor, borderRadius: "999px", width: "22px", height: "22px", cursor: "pointer", flexShrink: 0 }} title={item.archived ? localizeText(lang, "View experience", "\u67e5\u770b\u7ecf\u9a8c") : localizeText(lang, "Resume task", "\u7ee7\u7eed\u4efb\u52a1")}><ProjectSearchIcon name="arrowRight" /></button>
        </div>
        <div style={{ fontSize: "11px", color: t.text, opacity: 0.45, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", paddingLeft: "21px" }}>{item.project_path}</div>
        {item.preview && <div style={{ fontSize: "11px", color: t.text, opacity: 0.35, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", paddingLeft: "21px", marginTop: "1px" }}>{item.preview}</div>}
        {artifactSummary && <div title={artifactTooltip} className="psp-artifact"><span style={{ fontSize: "10px", color: t.headingColor, opacity: 0.58, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", minWidth: 0 }}>{artifactSummary}</span>{artifact?.source_url && <button type="button" onClick={event => { event.stopPropagation(); void OpenFileOrShowInFolder(artifact.source_url || ""); }} style={{ border: "none", background: "transparent", color: t.headingColor, opacity: 0.7, cursor: "pointer", fontSize: "11px", lineHeight: 1, padding: "1px 2px", flexShrink: 0 }} title={localizeText(lang, "Open artifact source", "打开产物来源")}><ProjectSearchIcon name="externalLink" /></button>}</div>}
        {item.last_activity && <div style={{ fontSize: "10px", color: t.text, opacity: 0.32, paddingLeft: "21px", marginTop: "1px" }}>{formatTime(item.last_activity)}</div>}
    </div>;
}

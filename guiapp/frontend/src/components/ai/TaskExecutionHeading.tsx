import type { AITab } from "./AITabTypes";
import { isCloudWorkspacePath } from "./codingTaskMode";
import { workingDirDisplayLabel } from "./SessionWorkingDirChip";
import { localizeText } from "./aiAssistantI18n";

/**
 * Title row plus meta line of the task-execution header.
 *
 * Extracted from AIAssistantPanel so the panel stays inside the 6800-line
 * main-UI guard cap (see scripts/check-main-ui-guards.mjs). Keeps the same
 * class names and data-testid values the panel tests rely on.
 */
type Props = {
    activeTab?: AITab | null;
    lang?: string;
    status: { tone: string; label: string };
    taskCreatedLabel?: string;
    workingDirPath?: string;
    remoteWorkspace?: { host?: string; workDir?: string } | null;
    remoteWorkspaceLabel?: string;
    /** Already resolved against the sidebar task list by the panel. */
    title?: string;
};

export function TaskExecutionHeading({ activeTab, lang, status, taskCreatedLabel, workingDirPath, remoteWorkspace, remoteWorkspaceLabel, title }: Props) {
    const workingDir = workingDirPath || "";
    const executionTitle = String(title || "").trim() || localizeText(lang, "Current task", "当前任务", "目前任務");
    const metaText = [
        activeTab?.type === "local" ? "" : localizeText(lang, "Created by you", "由你创建", "由你建立"),
        taskCreatedLabel,
        (remoteWorkspaceLabel || workingDir) ? workingDirDisplayLabel(workingDir, lang, remoteWorkspaceLabel ? remoteWorkspace : null) : "",
    ].filter(Boolean).join(" · ");
    const pathTitle = remoteWorkspaceLabel || (workingDir && !isCloudWorkspacePath(workingDir) ? workingDir : "");
    // Hover shows the whole second line. When the visible path was shortened,
    // also include the full directory so an ellipsis does not hide it.
    const metaTitle = metaText
        ? (pathTitle && !metaText.includes(pathTitle) ? `${metaText} · ${pathTitle}` : metaText)
        : "";
    return (
        <div className="mc-task-execution-heading">
            <strong className="mc-task-execution-title" data-task-title={executionTitle} role="heading" aria-level={2} aria-label={executionTitle} title={executionTitle}>{executionTitle}</strong>
            <div className="mc-task-execution-subline">
                <span className={`mc-task-execution-status mc-task-execution-status--${status.tone}`} data-status={status.tone} role="status"><i aria-hidden="true" />{status.label}</span>
                {metaText ? <div className="mc-task-execution-meta" data-testid="task-execution-meta" title={metaTitle}>{metaText}</div> : null}
            </div>
        </div>
    );
}

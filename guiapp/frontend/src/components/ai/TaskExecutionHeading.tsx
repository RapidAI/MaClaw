import type { AITab } from "./AITabTypes";
import { getAITabDisplayTitle } from "./AITabItem";
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
};

export function TaskExecutionHeading({ activeTab, lang, status, taskCreatedLabel, workingDirPath, remoteWorkspace, remoteWorkspaceLabel }: Props) {
    const workingDir = workingDirPath || "";
    const executionTitle = (activeTab ? getAITabDisplayTitle(activeTab, lang) : "") || localizeText(lang, "Current task", "当前任务", "目前任務");
    const metaText = [
        activeTab?.type === "local" ? "" : localizeText(lang, "Created by you", "由你创建", "由你建立"),
        taskCreatedLabel,
        (remoteWorkspaceLabel || workingDir) ? workingDirDisplayLabel(workingDir, lang, remoteWorkspaceLabel ? remoteWorkspace : null) : "",
    ].filter(Boolean).join(" · ");
    return (
        <div className="mc-task-execution-heading">
            <div className="mc-task-execution-title-row">
                <strong className="mc-task-execution-title" data-task-title={executionTitle} role="heading" aria-level={2} aria-label={executionTitle}>{executionTitle}</strong>
                <span className={`mc-task-execution-status mc-task-execution-status--${status.tone}`} data-status={status.tone} role="status"><i aria-hidden="true" />{status.label}</span>
            </div>
            <div className="mc-task-execution-meta" data-testid="task-execution-meta" title={remoteWorkspaceLabel || (workingDir && !isCloudWorkspacePath(workingDir) ? workingDir : undefined)}>
                {metaText}
            </div>
        </div>
    );
}

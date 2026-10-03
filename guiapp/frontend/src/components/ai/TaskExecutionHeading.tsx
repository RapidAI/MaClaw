import { useEffect } from "react";
import { CloudWorkspaceEntitlement } from "../../../wailsjs/go/main/App";
import type { AITab } from "./AITabTypes";
import { cloudWorkspaceIdFromPath, isCloudWorkspacePath, rememberCloudWorkspaceDisplayNames } from "./codingTaskMode";
import { useCloudWorkspaceDisplayName, workingDirDisplayLabel } from "./SessionWorkingDirChip";
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
    const cloudId = isCloudWorkspacePath(workingDir) ? cloudWorkspaceIdFromPath(workingDir) : "";
    const cloudName = useCloudWorkspaceDisplayName(cloudId);
    useEffect(() => {
        if (!cloudId) return;
        let cancelled = false;
        // The binding throws synchronously when the desktop bridge is absent.
        // A rename writes the same cache and the subscription re-renders this header.
        void Promise.resolve()
            .then(() => CloudWorkspaceEntitlement())
            .then((ent) => {
                if (cancelled) return;
                rememberCloudWorkspaceDisplayNames(ent);
            })
            .catch(() => {});
        return () => { cancelled = true; };
    }, [cloudId]);
    const executionTitle = String(title || "").trim() || localizeText(lang, "Current task", "当前任务", "目前任務");
    const createdBy = activeTab?.type === "local" ? "" : localizeText(lang, "Created by you", "由你创建", "由你建立");
    const showLocation = !!(remoteWorkspaceLabel || workingDir);
    const locationLabel = showLocation
        ? workingDirDisplayLabel(workingDir, lang, remoteWorkspaceLabel ? remoteWorkspace : null, cloudName)
        : "";
    // Hover uses the full location. The visible line may be only the server or a shortened path.
    const fullLocation = !showLocation
        ? ""
        : remoteWorkspaceLabel
            || (isCloudWorkspacePath(workingDir) ? locationLabel : workingDir);
    const metaText = [createdBy, taskCreatedLabel, locationLabel].filter(Boolean).join(" · ");
    const metaTitle = [createdBy, taskCreatedLabel, fullLocation].filter(Boolean).join(" · ");
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

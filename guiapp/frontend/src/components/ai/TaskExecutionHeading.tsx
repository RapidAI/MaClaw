import { useEffect } from "react";
import { BrowserOpenURL } from "../../../wailsjs/runtime";
import { CloudWorkspaceEntitlement } from "../../../wailsjs/go/main/App";
import { cloudAgentKindLabel } from "../layout/taskPlace";
import type { AITab } from "./AITabTypes";
import { cloudWorkspaceIdFromPath, isCloudWorkspacePath, rememberCloudWorkspaceDisplayNames, remoteWorkspaceLocationLabel } from "./codingTaskMode";
import { useCloudWorkspaceDisplayName, workingDirDisplayLabel } from "./SessionWorkingDirChip";
import { localizeText } from "./aiAssistantI18n";
import { TaskConfigIcon, type TaskConfigIconName } from "./task-config/taskConfigIcons";

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
    remoteWorkspace?: { host?: string; workDir?: string; port?: number } | null;
    remoteWorkspaceLabel?: string;
    /** Connected cloud git repository (owner/repo). Empty = nothing shown. */
    cloudGitRepo?: string;
    /** Provider of the bound repository ("github" | "gitlab"). With cloudGitOrigin
     *  it turns the repo chip into a link to the provider's web page. */
    cloudGitService?: string;
    /** Site root of the provider (e.g. https://gitlab.com). Self-hosted bases. */
    cloudGitOrigin?: string;
    /** Already resolved against the sidebar task list by the panel. */
    title?: string;
    /** Cloud agent instance kind ("coding" | "session"). Empty for local and remote desktops. */
    cloudTaskKind?: string;
};

type ExecutionWorkspaceKind = "local" | "cloud" | "remote";

const KIND_ICON: Record<ExecutionWorkspaceKind, TaskConfigIconName> = {
    local: "folder",
    cloud: "cloud",
    remote: "server",
};

/** Same priority as the location line: a remote host wins, then a cloud workspace path. */
function executionWorkspaceKind(workingDir: string, remoteLabel: string): ExecutionWorkspaceKind {
    if (remoteLabel) return "remote";
    if (isCloudWorkspacePath(workingDir)) return "cloud";
    return "local";
}

function executionWorkspaceKindLabel(kind: ExecutionWorkspaceKind, lang?: string): string {
    if (kind === "cloud") return localizeText(lang, "Cloud", "云端", "雲端");
    if (kind === "remote") return localizeText(lang, "Remote", "远程", "遠端");
    return localizeText(lang, "Local", "本地", "本地");
}

/** Only the two cloud-agent kinds. Anything else stays unmarked so a remote desktop does not read as 会话. */
function cloudTaskKindOf(kind: string | undefined): "coding" | "session" | "" {
    const value = String(kind || "").trim().toLowerCase();
    if (value === "coding" || value === "session") return value;
    return "";
}

/**
 * Web page of the bound repository. GitHub always lives on github.com; a
 * service with an HTTPS origin root rides it (gitlab.com or a self-hosted
 * base — the backend rejects non-HTTPS origins at connect); hosted gitlab
 * without a captured origin falls back to gitlab.com. Empty = the provider
 * cannot be derived, so the chip stays a plain label.
 */
export function cloudGitWebURL(service: string | undefined, origin: string | undefined, repo: string): string {
    const path = String(repo || "").trim().replace(/^\/+/, "");
    if (!path) return "";
    const provider = String(service || "").trim().toLowerCase();
    const base = String(origin || "").trim().replace(/\/+$/, "");
    if (provider === "github") return `https://github.com/${path}`;
    if (/^https:\/\//i.test(base)) return `${base}/${path}`;
    if (provider === "gitlab") return `https://gitlab.com/${path}`;
    return "";
}

function openExternalURL(url: string): void {
    try {
        BrowserOpenURL(url);
    } catch {
        window.open(url, "_blank", "noopener,noreferrer");
    }
}

/** The panel's running tone is the cloud task that still accepts input. In
 * that state the label is only the place word (云端 / 雲端 / Үүл / Cloud), and
 * it is frozen in the language from when the task was opened. Matching those
 * translations drops any language the list forgot, so the tone is the signal:
 * a running pill is the kind, and a pending pill keeps the restriction the
 * panel just localized (只读 / 离线). */
function cloudTaskStatusLabel(statusLabel: string, taskKind: "coding" | "session" | "", lang: string | undefined, tone: string): string {
    if (!taskKind) return statusLabel;
    const kindLabel = cloudAgentKindLabel(taskKind, lang || "");
    const restriction = statusLabel.trim();
    if (!restriction || tone === "running") return kindLabel;
    return `${kindLabel} · ${restriction}`;
}

export function TaskExecutionHeading({ activeTab, lang, status, taskCreatedLabel, workingDirPath, remoteWorkspace, remoteWorkspaceLabel, cloudGitRepo, cloudGitService, cloudGitOrigin, title, cloudTaskKind }: Props) {
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
    // The panel already resolved host/port/directory. Format the workspace only
    // when that string is missing, so a cloud cache path cannot replace it.
    const suppliedRemote = String(remoteWorkspaceLabel || "").trim();
    const remoteLabel = suppliedRemote
        || ((remoteWorkspace?.host || remoteWorkspace?.workDir)
            ? remoteWorkspaceLocationLabel(remoteWorkspace?.host, remoteWorkspace?.workDir, remoteWorkspace?.port)
            : "");
    const showLocation = !!(remoteLabel || workingDir);
    const locationLabel = !showLocation
        ? ""
        : remoteLabel || workingDirDisplayLabel(workingDir, lang, null, cloudName);
    const fullLocation = !showLocation
        ? ""
        : remoteLabel || (isCloudWorkspacePath(workingDir) ? locationLabel : workingDir);
    const metaText = [createdBy, taskCreatedLabel, locationLabel].filter(Boolean).join(" · ");
    const metaTitle = [createdBy, taskCreatedLabel, fullLocation].filter(Boolean).join(" · ");
    const taskKind = cloudTaskKindOf(cloudTaskKind);
    const repoURL = cloudGitWebURL(cloudGitService, cloudGitOrigin, cloudGitRepo || "");
    // A cloud agent passes its service name as the location label. That string
    // must not flip the place icon to 远程; the place is still 云端.
    const workspaceKind = taskKind ? "cloud" : executionWorkspaceKind(workingDir, remoteLabel);
    const workspaceKindLabel = executionWorkspaceKindLabel(workspaceKind, lang);
    const shownStatus = cloudTaskStatusLabel(status.label, taskKind, lang, status.tone);
    return (
        <div className="mc-task-execution-heading">
            <div className="mc-task-execution-title-row">
                <span className="mc-task-execution-kind" data-testid="task-execution-kind" data-kind={workspaceKind} title={workspaceKindLabel} aria-hidden="true"><TaskConfigIcon name={KIND_ICON[workspaceKind]} size={15} /></span>
                <strong className="mc-task-execution-title" data-task-title={executionTitle} role="heading" aria-level={2} aria-label={executionTitle} title={executionTitle}>{executionTitle}</strong>
            </div>
            <div className="mc-task-execution-subline">
                {shownStatus ? <span className={`mc-task-execution-status mc-task-execution-status--${status.tone}`} data-status={status.tone} data-task-kind={taskKind || undefined} data-testid={taskKind ? "task-execution-task-kind" : undefined} role="status"><i aria-hidden="true" />{shownStatus}</span> : null}
                {metaText ? <div className="mc-task-execution-meta" data-testid="task-execution-meta" title={metaTitle}>{metaText}</div> : null}
                {cloudGitRepo ? (repoURL ? (
                    <button
                        type="button"
                        className="mc-task-execution-cloud-git"
                        data-testid="task-execution-cloud-git"
                        title={repoURL}
                        onClick={() => openExternalURL(repoURL)}
                    >
                        <TaskConfigIcon name="git" size={13} />
                        <span>{cloudGitRepo}</span>
                    </button>
                ) : (
                    <div className="mc-task-execution-cloud-git" data-testid="task-execution-cloud-git" title={cloudGitRepo}>
                        <TaskConfigIcon name="git" size={13} />
                        <span>{cloudGitRepo}</span>
                    </div>
                )) : null}
            </div>
        </div>
    );
}

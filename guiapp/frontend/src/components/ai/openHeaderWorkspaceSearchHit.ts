import { ResumeTask } from "../../../wailsjs/go/main/App";
import { agentModeForCloudWorkspace, agentModeFromTaskTags, remoteHostFromTaskTags } from "./codingTaskMode";
import { parkCloudWorkspaceFileOpen } from "./cloudWorkspaceFileOpen";
import type { HeaderCloudWorkspaceHit } from "./cloudWorkspaceContentSearch";
import type { HeaderDataDirectoryHit } from "./dataDirectoryWorkspaceSearch";

type CreateProjectTab = (projectPath: string, taskTitle: string, options?: {
    autoSend?: boolean;
    agentMode?: "coding_dev" | "remote_coding_dev";
    remoteHost?: string;
    remoteSafety?: "diagnosis";
    tags?: string[];
}) => void;

type OpenHitDeps = {
    close: () => void;
    onCreateProjectTab?: CreateProjectTab;
    onProjectSwitch: (msg: string) => Promise<unknown> | unknown;
};

function resumeTaskInPlace(projectPath: string, onProjectSwitch: OpenHitDeps["onProjectSwitch"]): void {
    void (async () => {
        const msg = await ResumeTask(projectPath);
        if (msg) await onProjectSwitch(msg);
    })();
}

/** Open a cloud-workspace file hit in its task, and park the file for the preview. */
export function openHeaderCloudSearchHit(item: HeaderCloudWorkspaceHit, deps: OpenHitDeps): void {
    deps.close();
    parkCloudWorkspaceFileOpen({
        projectPath: item.projectPath,
        relativePath: item.relativePath,
        workspaceId: item.workspaceId,
        fileName: item.title,
    });
    const title = item.workspaceName || item.title;
    try {
        console.info("[ProjectSearch] opened cloud file", { taskPath: item.projectPath, file: item.relativePath, workspaceId: item.workspaceId });
        if (deps.onCreateProjectTab) {
            deps.onCreateProjectTab(item.projectPath, title, {
                autoSend: false,
                agentMode: agentModeForCloudWorkspace(agentModeFromTaskTags(item.tags), item.workspaceId),
                remoteHost: remoteHostFromTaskTags(item.tags),
                tags: item.tags,
            });
            return;
        }
        resumeTaskInPlace(item.projectPath, deps.onProjectSwitch);
    } catch (error) {
        console.error("[ProjectSearch] open cloud file failed:", error);
    }
}

/** Open a data-directory file hit in its task, and park the file for the preview. */
export function openHeaderDataDirectorySearchHit(item: HeaderDataDirectoryHit, deps: OpenHitDeps): void {
    deps.close();
    parkCloudWorkspaceFileOpen({
        projectPath: item.projectPath,
        relativePath: item.relativePath,
        workspaceId: "",
        fileName: item.title,
        source: "data",
    });
    const title = item.taskName || item.title;
    try {
        console.info("[ProjectSearch] opened data directory file", { taskPath: item.projectPath, file: item.relativePath });
        if (deps.onCreateProjectTab) {
            deps.onCreateProjectTab(item.projectPath, title, {
                autoSend: false,
                agentMode: agentModeFromTaskTags(item.tags),
                remoteHost: remoteHostFromTaskTags(item.tags),
                tags: item.tags,
            });
            return;
        }
        resumeTaskInPlace(item.projectPath, deps.onProjectSwitch);
    } catch (error) {
        console.error("[ProjectSearch] open data directory file failed:", error);
    }
}

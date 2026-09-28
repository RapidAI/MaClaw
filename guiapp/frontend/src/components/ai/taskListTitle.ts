import type { AITab } from "./AITabTypes";
import { getAITabDisplayTitle } from "./AITabItem";
import { normalizeProjectSessionPath } from "./aiAssistantPanelSessionUtils";
import { cloudWorkspaceIdFromPath, cloudWorkspaceIdFromTaskFields, lookupCloudWorkspaceDisplayName, visibleTaskRows } from "./codingTaskMode";
import { listedTaskTitle } from "./describeTaskTitle";

export type TaskTitleSource = {
    name?: string;
    project_path?: string;
    tags?: string[];
    working_dir?: string;
    has_output?: boolean | null;
};

function pathKey(path: string): string {
    return normalizeProjectSessionPath(path).toLowerCase();
}

/** Windows task paths differ by slash and drive case and still name the same task. */
export function projectPathsMatch(left: string, right: string): boolean {
    const key = pathKey(left);
    return !!key && key === pathKey(right);
}

function pickTaskRow(rows: TaskTitleSource[], tabPath: string, tabWs: string): TaskTitleSource | undefined {
    const tabKey = pathKey(tabPath);
    let project: TaskTitleSource | undefined;
    let work: TaskTitleSource | undefined;
    let workspace: TaskTitleSource | undefined;
    for (const task of rows) {
        const titled = !!listedTaskTitle(task);
        if (!project && tabKey && pathKey(String(task.project_path || "")) === tabKey) {
            project = task;
            if (titled) return task;
        }
        // The sidebar also highlights a row when the open path is its working directory.
        if (!work && tabKey && pathKey(String(task.working_dir || "")) === tabKey) work = task;
        if (!workspace && tabWs && cloudWorkspaceIdFromTaskFields(task) === tabWs && titled) workspace = task;
    }
    if (work && listedTaskTitle(work)) return work;
    return workspace || project || work;
}

/** Task-list row the sidebar is showing for this project tab. */
export function taskForAssistantTab(
    tab: Pick<AITab, "type" | "projectPath" | "cloudWorkspaceId"> | null | undefined,
    tasks: TaskTitleSource[] | undefined,
): TaskTitleSource | undefined {
    if (!tab || tab.type !== "project" || !Array.isArray(tasks) || tasks.length === 0) return undefined;
    const tabPath = String(tab.projectPath || "");
    const tabWs = String(tab.cloudWorkspaceId || "").trim() || cloudWorkspaceIdFromPath(tab.projectPath);
    // The sidebar collapses duplicate cloud rows and hides tasks with no output.
    // A named visible row wins. An untitled visible hit must not hide a named raw row.
    const visible = pickTaskRow(visibleTaskRows(tasks), tabPath, tabWs);
    if (visible && listedTaskTitle(visible)) return visible;
    const raw = pickTaskRow(tasks, tabPath, tabWs);
    if (raw && listedTaskTitle(raw)) return raw;
    return visible || raw;
}

/**
 * Title shown for the open assistant task. The sidebar row wins over a tab
 * title that was peeled out of a path or URL (for example "v1").
 */
export function assistantTaskTitle(tab: AITab | null | undefined, lang: string | undefined, tasks?: TaskTitleSource[]): string {
    const row = taskForAssistantTab(tab, tasks);
    const fromList = listedTaskTitle(row);
    if (fromList) return fromList;
    // Unnamed cloud rows show the workspace name in the sidebar, from the same cache.
    const workspaceName = lookupCloudWorkspaceDisplayName(row ? cloudWorkspaceIdFromTaskFields(row) : "");
    if (workspaceName) return workspaceName;
    return tab ? getAITabDisplayTitle(tab, lang) : "";
}

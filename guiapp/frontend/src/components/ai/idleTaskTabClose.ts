import type { AITab, AITabState } from "./AITabTypes";
import { expertIDFromTaskTags, expertSessionKey, isACPAssistantSessionKey, normalizeProjectSessionPath, projectSessionKey } from "./aiAssistantPanelSessionUtils";
import { cloudWorkspaceIdFromPath, cloudWorkspaceIdFromTaskFields } from "./codingTaskMode";
import { taskStatusBucketFor, workflowStatusForTaskRow, type TaskManagementItem } from "../layout/SidebarTaskManagement";

/**
 * When the user leaves task A for task B, A’s foreground tab can close if it
 * is not running, paused, or waiting for review, and the composer has no
 * unsent draft. The task row stays in the list and the transcript remains
 * resumable. Other idle tabs stay open unless a new tab would pass the cap.
 */
export type IdleSwitchTarget = {
    nextTabId?: string;
    projectPath?: string;
    cloudWorkspaceId?: string;
    expertId?: string;
};

export type IdleTaskSwitchContext = {
    tabs: AITab[];
    activeTabId: string;
    tasks: TaskManagementItem[];
    maxTabs: number;
    draftText: string;
    unsentAttachmentCount: number;
    preparingTabIds: Set<string>;
    recordingTabId: string | null;
    busySessionKeys: string[];
    streamingSessionKeys: string[];
    sendingSessionKey: string;
    inFlightSessionKeys: Set<string>;
    /** Active tab is executing even when its session key is absent from the busy lists. */
    activeExecutionBusy: boolean;
    /** Active tab is on a workflow form, review, or approval. */
    activeAwaitingUser: boolean;
    scopeApprovalProjectPath: string;
    getTabState: (tabId: string) => AITabState | undefined;
    closeTab: (tabId: string) => void;
};

type CloseableSnapshot = {
    id: string;
    type: string;
    lastActiveAt: number;
    canClose: boolean;
};

function taskWorkflowBlocksAutoClose(task: Pick<TaskManagementItem, "active_workflow" | "has_output"> | null | undefined): boolean {
    if (!task) return false;
    const bucket = taskStatusBucketFor(task);
    if (bucket === "running" || bucket === "paused" || bucket === "pending") return true;
    const shown = workflowStatusForTaskRow(task, "en");
    return shown?.tone === "info" || shown?.tone === "warning";
}

function taskForAssistantTab(tab: AITab, tasks: TaskManagementItem[]): TaskManagementItem | undefined {
    if (tab.type === "expert") {
        const expertId = String(tab.expertId || "").trim();
        if (!expertId) return undefined;
        return tasks.find(task => expertIDFromTaskTags(task.tags) === expertId);
    }
    if (tab.type !== "project") return undefined;
    const path = normalizeProjectSessionPath(tab.projectPath);
    const workspaceId = String(tab.cloudWorkspaceId || "").trim() || cloudWorkspaceIdFromPath(tab.projectPath);
    return tasks.find(task => {
        const taskWorkspaceId = cloudWorkspaceIdFromTaskFields(task);
        if (workspaceId && taskWorkspaceId && workspaceId === taskWorkspaceId) return true;
        const taskPath = normalizeProjectSessionPath(task.project_path);
        return !!path && !!taskPath && taskPath === path;
    });
}

function tabSessionKeys(tab: AITab): string[] {
    const keys: string[] = [];
    const explicit = String(tab.sessionKey || "").trim();
    if (explicit) keys.push(explicit);
    if (tab.type === "project") {
        const key = projectSessionKey(tab.projectPath);
        if (key && !keys.includes(key)) keys.push(key);
    }
    if (tab.type === "expert") {
        const key = expertSessionKey(tab.expertId);
        if (key && !keys.includes(key)) keys.push(key);
    }
    return keys;
}

function tabSessionIsBusy(tab: AITab, ctx: IdleTaskSwitchContext): boolean {
    const busy = new Set([...ctx.busySessionKeys, ...ctx.streamingSessionKeys, ...ctx.inFlightSessionKeys]);
    const sending = String(ctx.sendingSessionKey || "").trim();
    if (sending) busy.add(sending);
    if (tabSessionKeys(tab).some(key => busy.has(key))) return true;
    return tab.id === ctx.activeTabId && ctx.activeExecutionBusy;
}

function scopeApprovalMatches(tab: AITab, projectPath: string): boolean {
    const wanted = normalizeProjectSessionPath(projectPath);
    if (!wanted || tab.type !== "project") return false;
    return normalizeProjectSessionPath(tab.projectPath) === wanted;
}

function snapshotTab(tab: AITab, ctx: IdleTaskSwitchContext): CloseableSnapshot {
    const saved = ctx.getTabState(tab.id);
    const isActive = tab.id === ctx.activeTabId;
    const draft = isActive ? ctx.draftText : String(saved?.inputText || "");
    const workflowHeld = taskWorkflowBlocksAutoClose(taskForAssistantTab(tab, ctx.tasks));
    const awaitingUser = (isActive && ctx.activeAwaitingUser) || scopeApprovalMatches(tab, ctx.scopeApprovalProjectPath);
    const canClose = (tab.type === "project" || tab.type === "expert")
        && tab.closable === true
        && !isACPAssistantSessionKey(tab.sessionKey)
        && !ctx.preparingTabIds.has(tab.id)
        && ctx.recordingTabId !== tab.id
        && !tabSessionIsBusy(tab, ctx)
        && !awaitingUser
        && !workflowHeld
        && !draft.trim()
        && !(isActive && ctx.unsentAttachmentCount > 0);
    return {
        id: tab.id,
        type: tab.type,
        lastActiveAt: saved?.lastActiveAt || 0,
        canClose,
    };
}

function resolveNextTab(ctx: IdleTaskSwitchContext, target: IdleSwitchTarget): { nextTabId?: string; creatingProject: boolean; creatingExpert: boolean } {
    const explicitId = String(target.nextTabId || "").trim();
    if (explicitId) return { nextTabId: explicitId, creatingProject: false, creatingExpert: false };
    const expertId = String(target.expertId || "").trim();
    if (expertId) {
        const existing = ctx.tabs.find(tab => tab.type === "expert" && tab.expertId === expertId);
        if (existing) return { nextTabId: existing.id, creatingProject: false, creatingExpert: false };
        return { creatingProject: false, creatingExpert: true };
    }
    const path = normalizeProjectSessionPath(target.projectPath);
    // A cloud resume can hand back a new cache path for a tab that is already open.
    const workspaceId = String(target.cloudWorkspaceId || "").trim() || cloudWorkspaceIdFromPath(path);
    if (!workspaceId && !path) return { creatingProject: false, creatingExpert: false };
    const existing = ctx.tabs.find(tab => tab.type === "project" && !isACPAssistantSessionKey(tab.sessionKey) && (
        (!!workspaceId && (String(tab.cloudWorkspaceId || "").trim() || cloudWorkspaceIdFromPath(tab.projectPath)) === workspaceId)
        || (!!path && normalizeProjectSessionPath(tab.projectPath) === path)
    ));
    if (existing) return { nextTabId: existing.id, creatingProject: false, creatingExpert: false };
    return { creatingProject: true, creatingExpert: false };
}

function oldestCloseable(snapshots: CloseableSnapshot[], type: string, reserved: Set<string>): string | undefined {
    const candidates = snapshots
        .filter(tab => tab.type === type && tab.canClose && !reserved.has(tab.id))
        .sort((left, right) => left.lastActiveAt - right.lastActiveAt || (left.id < right.id ? -1 : left.id > right.id ? 1 : 0));
    return candidates[0]?.id;
}

/** Tab ids to close before the switch, active idle tab first, then one oldest idle tab if the cap still blocks a create. */
export function planIdleTaskTabCloses(ctx: IdleTaskSwitchContext, target: IdleSwitchTarget): string[] {
    const resolved = resolveNextTab(ctx, target);
    if (!resolved.nextTabId && !resolved.creatingProject && !resolved.creatingExpert) return [];
    if (resolved.nextTabId && resolved.nextTabId === ctx.activeTabId) return [];
    const snapshots = ctx.tabs.map(tab => snapshotTab(tab, ctx));
    const byId = new Map(snapshots.map(tab => [tab.id, tab]));
    const closing: string[] = [];
    const active = byId.get(ctx.activeTabId);
    if (active?.canClose && active.id !== resolved.nextTabId) closing.push(active.id);
    const reserved = new Set(closing);
    if (resolved.nextTabId) reserved.add(resolved.nextTabId);
    const stillOpen = (type: string) => ctx.tabs.filter(tab => tab.type === type && !reserved.has(tab.id)).length;
    if (resolved.creatingProject && stillOpen("project") >= ctx.maxTabs) {
        const victim = oldestCloseable(snapshots, "project", reserved);
        if (victim) closing.push(victim);
    }
    if (resolved.creatingExpert && stillOpen("expert") >= ctx.maxTabs) {
        const victim = oldestCloseable(snapshots, "expert", reserved);
        if (victim) closing.push(victim);
    }
    return closing;
}

/** Close idle foreground tabs before a user task switch. No-op when the context is not ready. */
export function releaseIdleTaskTabs(ctx: IdleTaskSwitchContext | null | undefined, target: IdleSwitchTarget): void {
    if (!ctx?.closeTab || !Array.isArray(ctx.tabs)) return;
    const closing = planIdleTaskTabCloses(ctx, target);
    for (const tabId of closing) {
        console.info("[task-switch] auto-close idle tab", { tabId, nextTabId: target.nextTabId || "", projectPath: target.projectPath || "", expertId: target.expertId || "" });
        ctx.closeTab(tabId);
    }
}

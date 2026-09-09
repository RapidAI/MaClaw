/** Helpers for pure-coding task tags shared by sidebar / search / resume. */

export type PureCodingAgentMode = "coding_dev" | "remote_coding_dev";

/** Max length for create-task command textarea / welcome coding templates (UTF-16 units). */
export const CODING_TASK_COMMAND_MAX_LEN = 2000;

export type RemoteCodingMetaFromTags = {
    host: string;
    user: string;
    port: number;
    workDir: string;
};

export function cloudWorkspaceIdFromTags(tags?: string[] | null): string {
    if (!tags?.length) return "";
    const prefix = "cloud_workspace:";
    for (const raw of tags) {
        const t = String(raw || "").trim();
        if (t.startsWith(prefix)) return t.slice(prefix.length).trim();
    }
    return "";
}

/** Same identity order as the backend: working_dir, then project path, then tag. */
export function cloudWorkspaceIdFromTaskFields(task?: {
    tags?: string[] | null;
    project_path?: string | null;
    projectPath?: string | null;
    working_dir?: string | null;
    workingDir?: string | null;
} | null): string {
    if (!task) return "";
    const pathId = (
        cloudWorkspaceIdFromPath(task.working_dir || task.workingDir)
        || cloudWorkspaceIdFromPath(task.project_path || task.projectPath)
        || ""
    ).trim();
    if (pathId) return pathId;
    // A remote SSH task is a different environment. A stray cloud_workspace
    // tag must not collapse it onto a Hub workspace row.
    if (agentModeFromTaskTags(task.tags) === "remote_coding_dev") return "";
    return cloudWorkspaceIdFromTags(task.tags);
}

const GENERIC_CLOUD_WORKSPACE_TASK_NAMES = new Set([
    "new cloud workspace task",
    "新建云端工作区任务",
    "新建雲端工作區任務",
]);

function cloudWorkspaceTaskNameScore(name?: string | null): number {
    const trimmed = String(name || "").trim();
    if (!trimmed || GENERIC_CLOUD_WORKSPACE_TASK_NAMES.has(trimmed) || GENERIC_CLOUD_WORKSPACE_TASK_NAMES.has(trimmed.toLowerCase())) {
        return 0;
    }
    return 1;
}

/** Keep at most one sidebar row per cloud workspace. Prefer a named title. */
export function collapseCloudWorkspaceTasks<T extends {
    name?: string | null;
    tags?: string[] | null;
    project_path?: string | null;
    projectPath?: string | null;
    working_dir?: string | null;
    workingDir?: string | null;
}>(tasks: T[]): T[] {
    if (tasks.length <= 1) return tasks;
    const byId = new Map<string, number>();
    const out: T[] = [];
    for (const task of tasks) {
        const id = cloudWorkspaceIdFromTaskFields(task);
        if (!id) {
            out.push(task);
            continue;
        }
        const idx = byId.get(id);
        if (idx === undefined) {
            byId.set(id, out.length);
            out.push(task);
            continue;
        }
        if (cloudWorkspaceTaskNameScore(task.name) > cloudWorkspaceTaskNameScore(out[idx].name)) {
            out[idx] = task;
        }
    }
    return out;
}

export function isCloudWorkspacePath(path?: string | null): boolean {
    const normalized = String(path || "").replace(/\\/g, "/").toLowerCase();
    return normalized.includes("/cloud-workspaces/");
}

/** Task list / resume: tagged or mounted on a cloud-workspace cache. */
export function isCloudWorkspaceTask(task?: {
    tags?: string[] | null;
    project_path?: string | null;
    projectPath?: string | null;
    working_dir?: string | null;
    workingDir?: string | null;
} | null): boolean {
    if (!task) return false;
    if (cloudWorkspaceIdFromTaskFields(task)) return true;
    if (agentModeFromTaskTags(task.tags) === "remote_coding_dev") return false;
    return isCloudWorkspacePath(task.project_path || task.projectPath)
        || isCloudWorkspacePath(task.working_dir || task.workingDir);
}

type CloudWorkspaceLeaseResume = (
    workspaceId: string,
    projectPath: string,
) => Promise<{ project_path?: string; projectPath?: string } | null | undefined>;

export type CloudWorkspaceLeaseEnsureResult =
    | { ok: true; skipped: boolean; projectPath: string }
    | { ok: false; cancelled: boolean; error: string };

const cloudWorkspaceLeaseEnsuredIds = new Set<string>();
const cloudWorkspaceLeaseInFlight = new Map<string, Promise<string>>();

/** Prepare returns this when the user declines a dirty-cache dialog. */
const CLOUD_WORKSPACE_LEASE_CANCELLED = "已取消打开云端工作区";

function cloudWorkspaceLeaseErrorMessage(error: unknown): string {
    if (error instanceof Error) return error.message.trim();
    if (typeof error === "string") return error.trim();
    if (error && typeof error === "object" && "message" in error) {
        return String((error as { message?: unknown }).message || "").trim();
    }
    return String(error || "").trim();
}

function isCloudWorkspaceLeaseCancelled(message: string): boolean {
    return message.includes("已取消打开");
}

export function cloudWorkspaceIdFromTab(tab?: {
    cloudWorkspaceId?: string | null;
    projectPath?: string | null;
    workingDir?: string | null;
} | null): string {
    return (
        String(tab?.cloudWorkspaceId || "").trim()
        || cloudWorkspaceIdFromPath(tab?.workingDir)
        || cloudWorkspaceIdFromPath(tab?.projectPath)
        || ""
    );
}

export function cloudWorkspaceLeaseEnsured(workspaceId?: string | null): boolean {
    const id = String(workspaceId || "").trim();
    return !!id && cloudWorkspaceLeaseEnsuredIds.has(id);
}

export function markCloudWorkspaceLeaseEnsured(workspaceId?: string | null): void {
    const id = String(workspaceId || "").trim();
    if (id) cloudWorkspaceLeaseEnsuredIds.add(id);
}

export function forgetCloudWorkspaceLeaseEnsured(workspaceId?: string | null): void {
    const id = String(workspaceId || "").trim();
    if (!id) {
        cloudWorkspaceLeaseEnsuredIds.clear();
        cloudWorkspaceLeaseInFlight.clear();
        return;
    }
    cloudWorkspaceLeaseEnsuredIds.delete(id);
    cloudWorkspaceLeaseInFlight.delete(id);
}

export function __resetCloudWorkspaceLeaseEnsureForTests(): void {
    forgetCloudWorkspaceLeaseEnsured();
}

/**
 * Re-acquire the Hub writer lease before the first command on an already-open
 * cloud-workspace tab. Restarted tabs restore conversation/files from cache
 * without Prepare; sending without this step writes a sealed or stolen tree.
 * Later sends in the same process skip Hub round-trips once the lease is held.
 * Pass `force` when the sidebar row is being opened and no live tab exists:
 * HideTask/DeleteTask release the Hub lease, so a leftover in-process mark is
 * stale. Closing only the assistant tab does not release the mount; force
 * Resume is then a cheap already-held path.
 */
export async function ensureCloudWorkspaceLeaseBeforeSend(input: {
    workspaceId?: string | null;
    projectPath?: string | null;
    resume: CloudWorkspaceLeaseResume;
    force?: boolean;
}): Promise<CloudWorkspaceLeaseEnsureResult> {
    const projectPath = String(input.projectPath || "").trim();
    const workspaceId = String(input.workspaceId || "").trim() || cloudWorkspaceIdFromPath(projectPath);
    if (!workspaceId) return { ok: true, skipped: true, projectPath };

    if (!input.force && cloudWorkspaceLeaseEnsuredIds.has(workspaceId)) {
        return { ok: true, skipped: true, projectPath };
    }

    const existing = cloudWorkspaceLeaseInFlight.get(workspaceId);
    if (existing) {
        try {
            const boundPath = await existing;
            return { ok: true, skipped: false, projectPath: boundPath || projectPath };
        } catch (error) {
            const message = cloudWorkspaceLeaseErrorMessage(error);
            return { ok: false, cancelled: isCloudWorkspaceLeaseCancelled(message), error: message };
        }
    }

    let pending!: Promise<string>;
    pending = (async () => {
        const bound = await input.resume(workspaceId, projectPath);
        const boundPath = String(bound?.project_path || bound?.projectPath || "").trim();
        if (!boundPath) {
            throw new Error("Unable to open the cloud workspace task");
        }
        // Close/delete can drop the in-flight entry while Hub is still working.
        // Do not advertise a held lease after that tab is gone.
        if (cloudWorkspaceLeaseInFlight.get(workspaceId) !== pending) {
            throw new Error(CLOUD_WORKSPACE_LEASE_CANCELLED);
        }
        markCloudWorkspaceLeaseEnsured(workspaceId);
        return boundPath;
    })();
    cloudWorkspaceLeaseInFlight.set(workspaceId, pending);
    try {
        const boundPath = await pending;
        return { ok: true, skipped: false, projectPath: boundPath };
    } catch (error) {
        const message = cloudWorkspaceLeaseErrorMessage(error);
        return { ok: false, cancelled: isCloudWorkspaceLeaseCancelled(message), error: message };
    } finally {
        if (cloudWorkspaceLeaseInFlight.get(workspaceId) === pending) {
            cloudWorkspaceLeaseInFlight.delete(workspaceId);
        }
    }
}

function foldCloudComparePath(path?: string | null): string {
    return String(path || "").trim().replace(/\\/g, "/").replace(/\/+$/, "").toLowerCase();
}

/** True when a reveal path belongs to the active assistant tab. */
export function cloudWorkspaceRevealMatchesTab(
    reveal?: { projectPath?: string | null; workingDir?: string | null } | null,
    tab?: { projectPath?: string | null; workingDir?: string | null } | null,
): boolean {
    if (!reveal || !tab) return false;
    const wantPath = foldCloudComparePath(reveal.projectPath);
    const wantDir = foldCloudComparePath(reveal.workingDir);
    const tabPath = foldCloudComparePath(tab.projectPath);
    const tabDir = foldCloudComparePath(tab.workingDir);
    if (wantPath && tabPath && wantPath === tabPath) return true;
    if (wantPath && tabDir && wantPath === tabDir) return true;
    if (wantDir && tabPath && wantDir === tabPath) return true;
    if (wantDir && tabDir && wantDir === tabDir) return true;
    const wantId = cloudWorkspaceIdFromPath(reveal.workingDir) || cloudWorkspaceIdFromPath(reveal.projectPath);
    const tabId = cloudWorkspaceIdFromPath(tab.workingDir) || cloudWorkspaceIdFromPath(tab.projectPath);
    if (wantId && tabId && wantId === tabId) return true;
    const wantRoot = foldCloudComparePath(cloudWorkspaceRootFromPath(reveal.workingDir || reveal.projectPath));
    const tabRoot = foldCloudComparePath(cloudWorkspaceRootFromPath(tab.workingDir || tab.projectPath));
    return !!wantRoot && !!tabRoot && wantRoot === tabRoot;
}

export type TabWorkingDir = { tabId: string; path: string };

export type CloudWorkspaceReveal = { projectPath?: string | null; workingDir?: string | null };

/** Keep a known cloud cache dir when a later GetTabWorkingDir returns a local default. */
export function nextTabWorkingDir(
    prev: TabWorkingDir | null | undefined,
    tabId: string,
    path: string,
): TabWorkingDir | null {
    const id = String(tabId || "").trim();
    if (!id) return prev ?? null;
    const next = String(path || "").trim();
    if (prev?.tabId === id && isCloudWorkspacePath(prev.path) && !isCloudWorkspacePath(next)) {
        return prev;
    }
    if (prev?.tabId === id && prev.path === next) return prev;
    return { tabId: id, path: next };
}

/** Working dir to show for this tab: stored cache, else a matching pending reveal. */
export function cloudWorkingDirForActiveTab(input: {
    tabId: string;
    projectPath?: string | null;
    stored?: TabWorkingDir | null;
    pending?: CloudWorkspaceReveal | null;
}): string {
    const stored = input.stored?.tabId === input.tabId ? String(input.stored.path || "").trim() : "";
    if (stored) return stored;
    const pendingDir = String(input.pending?.workingDir || "").trim();
    if (!isCloudWorkspacePath(pendingDir)) return "";
    if (!cloudWorkspaceRevealMatchesTab(input.pending, {
        projectPath: input.projectPath,
        workingDir: stored,
    })) return "";
    return pendingDir;
}

/** Whether the active project tab should show the cloud file preview. */
export function isActiveCloudWorkspacePreview(input: {
    isProjectTab: boolean;
    projectPath?: string | null;
    workingDir?: string | null;
    revealPath?: string | null;
    pendingReveal?: CloudWorkspaceReveal | null;
}): boolean {
    if (!input.isProjectTab) return false;
    if (isCloudWorkspacePath(input.projectPath) || isCloudWorkspacePath(input.workingDir)) return true;
    const tab = { projectPath: input.projectPath, workingDir: input.workingDir };
    if (input.pendingReveal && cloudWorkspaceRevealMatchesTab(input.pendingReveal, {
        projectPath: input.projectPath,
        workingDir: input.workingDir,
    })) return true;
    return cloudWorkspaceRevealMatchesTab({ projectPath: input.revealPath }, tab);
}

/** Relative path inside a cloud workspace cache, or "" for the workspace root. */
export function cloudWorkspaceRelativePath(path?: string | null): string {
    const normalized = String(path || "").replace(/\\/g, "/");
    const idx = normalized.toLowerCase().indexOf("/cloud-workspaces/");
    if (idx < 0) return "";
    const parts = normalized.slice(idx + "/cloud-workspaces/".length).split("/").filter(Boolean);
    return parts.length > 2 ? parts.slice(2).join("/") : "";
}

/** A file inside the cache (relative path with an extension), not a workspace/dir. */
export function isCloudWorkspaceFilePath(path?: string | null): boolean {
    if (!isCloudWorkspacePath(path)) return false;
    const raw = String(path || "").replace(/\\/g, "/");
    if (raw.endsWith("/")) return false;
    const rel = cloudWorkspaceRelativePath(path);
    if (!rel) return false;
    const base = rel.split("/").pop() || "";
    const dot = base.lastIndexOf(".");
    return dot > 0 && dot < base.length - 1;
}

/** Hub workspace id from a cache mount path (…/cloud-workspaces/{tenant}/{id}). */
export function cloudWorkspaceIdFromPath(path?: string | null): string {
    const root = cloudWorkspaceRootFromPath(path);
    if (!root) return "";
    const parts = root.split("/").filter(Boolean);
    return parts[parts.length - 1] || "";
}

export function cloudWorkspaceNameFromEntitlement(ent: unknown, workspaceId: string): string {
    const id = String(workspaceId || "").trim();
    if (!id || !ent || typeof ent !== "object") return "";
    const rec = ent as Record<string, unknown>;
    const lists = [rec.workspaces, rec.Workspaces, rec.deleted, rec.Deleted];
    for (const list of lists) {
        if (!Array.isArray(list)) continue;
        for (const row of list) {
            if (!row || typeof row !== "object") continue;
            const r = row as Record<string, unknown>;
            if (String(r.id || r.ID || "").trim() !== id) continue;
            return String(r.name || r.Name || "").trim();
        }
    }
    return "";
}

const cloudWorkspaceDisplayNames = new Map<string, string>();

export function rememberCloudWorkspaceDisplayName(id: string, name: string): void {
    const key = String(id || "").trim();
    const value = String(name || "").trim();
    if (!key || !value) return;
    cloudWorkspaceDisplayNames.set(key, value);
}

export function rememberCloudWorkspaceDisplayNames(ent: unknown): void {
    if (!ent || typeof ent !== "object") return;
    const rec = ent as Record<string, unknown>;
    for (const list of [rec.workspaces, rec.Workspaces, rec.deleted, rec.Deleted]) {
        if (!Array.isArray(list)) continue;
        for (const row of list) {
            if (!row || typeof row !== "object") continue;
            const r = row as Record<string, unknown>;
            rememberCloudWorkspaceDisplayName(String(r.id || r.ID || ""), String(r.name || r.Name || ""));
        }
    }
}

export function lookupCloudWorkspaceDisplayName(workspaceId: string, fallback = ""): string {
    const id = String(workspaceId || "").trim();
    if (id) {
        const cached = cloudWorkspaceDisplayNames.get(id);
        if (cached) return cached;
    }
    return String(fallback || "").trim();
}

export function __resetCloudWorkspaceDisplayNamesForTests(): void {
    cloudWorkspaceDisplayNames.clear();
}

/** Cache-root path (…/cloud-workspaces/{tenant}/{id}) for reveal/resume. */
export function cloudWorkspaceRootFromPath(path?: string | null): string {
    const normalized = String(path || "").replace(/\\/g, "/");
    const marker = "/cloud-workspaces/";
    const idx = normalized.toLowerCase().indexOf(marker);
    if (idx < 0) return "";
    const parts = normalized.slice(idx + marker.length).split("/").filter(Boolean);
    if (parts.length < 2) return "";
    return `${normalized.slice(0, idx + marker.length)}${parts[0]}/${parts[1]}`;
}

/** User-visible path that never includes the local cloud cache directory. */
export function cloudSafePathLabel(path?: string | null, fallback = "cloud"): string {
    const raw = String(path || "").trim();
    if (!raw) return "";
    if (!isCloudWorkspacePath(raw)) return raw;
    return cloudWorkspaceRelativePath(raw) || fallback;
}

/** Sidebar 浏览: open the local cache folder. */
export const REVEAL_CLOUD_WORKSPACE_FILES_EVENT = "ai-reveal-cloud-workspace-files";

/** Dir-bar: switch the already-open panel to the cloud file tree without reloading. */
export const FOCUS_CLOUD_WORKSPACE_TREE_EVENT = "ai-focus-cloud-workspace-tree";

/** Backend watcher: local cloud-cache files changed and should refresh the preview tree. */
export const CLOUD_WORKSPACE_FILES_CHANGED_EVENT = "cloud-workspace-files-changed";

/** Backend sync progress/terminal state for the cloud workspace overview. */
export const CLOUD_WORKSPACE_SYNC_PROGRESS_EVENT = "cloud-workspace-sync-progress";

/** Drop local cache paths from cloud-workspace error text shown in the UI. */
export function parseWailsEventObject(payload: unknown): Record<string, unknown> {
    if (typeof payload === "string") {
        try {
            const parsed = JSON.parse(payload) as unknown;
            if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
                return parsed as Record<string, unknown>;
            }
        } catch {
            return {};
        }
        return {};
    }
    if (payload && typeof payload === "object" && !Array.isArray(payload)) {
        return payload as Record<string, unknown>;
    }
    return {};
}

export function scrubCloudWorkspaceError(message: string, fallback: string): string {
    const text = String(message || "");
    return isCloudWorkspacePath(text) ? fallback : text;
}

export function agentModeFromTaskTags(tags?: string[] | null): PureCodingAgentMode | undefined {
    if (!tags?.length) return undefined;
    if (tags.includes("remote_coding_dev")) return "remote_coding_dev";
    if (tags.includes("coding_dev")) return "coding_dev";
    return undefined;
}

/** Cloud cache is a local working directory; SSH remote_coding must not reopen it. */
export function agentModeForCloudWorkspace(
    mode: PureCodingAgentMode | undefined,
    cloudWorkspaceId?: string | null,
): PureCodingAgentMode | undefined {
    if (String(cloudWorkspaceId || "").trim() && mode === "remote_coding_dev") {
        return "coding_dev";
    }
    return mode;
}

export function remoteHostFromTaskTags(tags?: string[] | null): string | undefined {
    if (!tags?.length) return undefined;
    const raw = tags.find((t) => t.startsWith("remote_host:"));
    if (!raw) return undefined;
    const host = raw.slice("remote_host:".length).trim();
    return host || undefined;
}

/** Whether a remote coding engine task was created for operations diagnosis. */
export function isRemoteMaintenanceTaskTags(tags?: string[] | null): boolean {
    return !!tags?.some((tag) => String(tag || "").trim() === "source:remote_ops_diagnosis");
}

/** Parse non-sensitive remote SSH metadata from task tags (password is never stored). */
export function remoteCodingMetaFromTaskTags(tags?: string[] | null): RemoteCodingMetaFromTags {
    const meta: RemoteCodingMetaFromTags = { host: "", user: "", port: 22, workDir: "" };
    if (!tags?.length) return meta;
    for (const raw of tags) {
        const t = String(raw || "").trim();
        if (t.startsWith("remote_host:")) {
            meta.host = t.slice("remote_host:".length).trim();
        } else if (t.startsWith("remote_user:")) {
            meta.user = t.slice("remote_user:".length).trim();
        } else if (t.startsWith("remote_port:")) {
            const p = Number.parseInt(t.slice("remote_port:".length).trim(), 10);
            if (Number.isFinite(p) && p > 0 && p < 65536) meta.port = p;
        } else if (t.startsWith("remote_workdir:")) {
            meta.workDir = t.slice("remote_workdir:".length).trim();
        }
    }
    return meta;
}

export function isPureCodingTaskTags(tags?: string[] | null): boolean {
    return agentModeFromTaskTags(tags) != null;
}

/** True when the task was created from the multi-phase coding workflow form. */
export function isCodingWorkflowSourceTags(tags?: string[] | null): boolean {
    if (!tags?.length) return false;
    return tags.some((t) => {
        const s = String(t || "").trim();
        return s === "source:coding_workflow" || s.startsWith("source:coding_workflow");
    });
}

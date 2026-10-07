import { cloudWorkspaceIdFromPath } from "./codingTaskMode";

export const OPEN_CLOUD_WORKSPACE_FILE_EVENT = "maclaw:open-cloud-workspace-file";

export type CloudWorkspaceFileOpenRequest = {
    projectPath: string;
    relativePath: string;
    workspaceId: string;
    fileName: string;
    /** data reads {task}/workspace. Omitted requests stay on the cloud preview path. */
    source?: "data";
};

let pendingCloudWorkspaceFileOpen: CloudWorkspaceFileOpenRequest | null = null;

export function parkCloudWorkspaceFileOpen(request: CloudWorkspaceFileOpenRequest): void {
    const relativePath = request.relativePath.trim();
    const projectPath = request.projectPath.trim();
    if (!relativePath || !projectPath) return;
    pendingCloudWorkspaceFileOpen = {
        projectPath,
        relativePath,
        workspaceId: request.workspaceId.trim(),
        fileName: request.fileName.trim() || relativePath.split(/[\\/]/).pop() || relativePath,
        ...(request.source === "data" ? { source: "data" as const } : {}),
    };
    if (typeof window === "undefined") return;
    window.dispatchEvent(new CustomEvent(OPEN_CLOUD_WORKSPACE_FILE_EVENT, { detail: { ...pendingCloudWorkspaceFileOpen } }));
}

export function clearParkedCloudWorkspaceFileOpen(): void {
    pendingCloudWorkspaceFileOpen = null;
}

export function peekParkedCloudWorkspaceFileOpen(): CloudWorkspaceFileOpenRequest | null {
    return pendingCloudWorkspaceFileOpen;
}

export function takeParkedCloudWorkspaceFileOpen(): CloudWorkspaceFileOpenRequest | null {
    const value = pendingCloudWorkspaceFileOpen;
    pendingCloudWorkspaceFileOpen = null;
    return value;
}

function normalizeOpenPath(value?: string | null): string {
    return String(value || "").trim().replace(/[\\/]+$/, "").replace(/\\/g, "/").toLowerCase();
}

/** The parked file belongs to the project tab that is now on screen. */
export function cloudWorkspaceFileOpenMatches(
    request: CloudWorkspaceFileOpenRequest | null | undefined,
    active: { projectPath?: string | null; workspaceId?: string | null },
): boolean {
    if (!request?.relativePath || !request.projectPath) return false;
    const activePath = normalizeOpenPath(active.projectPath);
    if (activePath && activePath === normalizeOpenPath(request.projectPath)) return true;
    const requestId = (request.workspaceId || cloudWorkspaceIdFromPath(request.projectPath)).trim();
    const activeId = (active.workspaceId || cloudWorkspaceIdFromPath(active.projectPath) || "").trim();
    return !!requestId && requestId === activeId;
}

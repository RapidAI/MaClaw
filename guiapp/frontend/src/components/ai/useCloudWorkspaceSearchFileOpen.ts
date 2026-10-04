import { useEffect, useRef } from "react";
import { GetCodingWorkbenchFilePreview, OpenCodingWorkbenchFileLocally } from "../../../wailsjs/go/main/App";
import { isLatexSourceName, isLikelyBinaryName } from "./CodePreviewWorkspace";
import {
    OPEN_CLOUD_WORKSPACE_FILE_EVENT,
    cloudWorkspaceFileOpenMatches,
    peekParkedCloudWorkspaceFileOpen,
    takeParkedCloudWorkspaceFileOpen,
    type CloudWorkspaceFileOpenRequest,
} from "./cloudWorkspaceFileOpen";
import type { CodeFile } from "./useCodePreviewState";

export type CloudWorkspaceSearchFileOpenOptions = {
    /** Cloud preview is mounted for the active tab, so a cloud file open will not be cleared. */
    ready: boolean;
    projectPath?: string | null;
    workspaceId?: string | null;
    openWorkspaceFile: (file: CodeFile) => boolean | void;
    reopenPreview: () => void;
    /** Keep the preview pane up for a data-directory file on an ordinary task tab. */
    holdPreview?: () => void;
};

type DataDirectoryFileBinding = (projectPath: string, relativePath: string) => Promise<{ path?: string; content?: string; language?: string; truncated?: boolean }>;
type DataDirectoryOpenBinding = (projectPath: string, relativePath: string) => Promise<unknown>;

function dataDirectoryFileBindings(): { preview: DataDirectoryFileBinding | null; openLocal: DataDirectoryOpenBinding | null } {
    const app = (window as unknown as {
        go?: { main?: { App?: { GetDataDirectoryWorkspaceFilePreview?: DataDirectoryFileBinding; OpenDataDirectoryWorkspaceFileLocally?: DataDirectoryOpenBinding } } };
    }).go?.main?.App;
    return {
        preview: typeof app?.GetDataDirectoryWorkspaceFilePreview === "function" ? app.GetDataDirectoryWorkspaceFilePreview : null,
        openLocal: typeof app?.OpenDataDirectoryWorkspaceFileLocally === "function" ? app.OpenDataDirectoryWorkspaceFileLocally : null,
    };
}

function binaryPreviewFailure(error: unknown): boolean {
    const text = (error instanceof Error ? error.message : String(error || "")).toLowerCase();
    return text.includes("binary files cannot be previewed");
}

/**
 * Open one searched data-directory workspace file.
 * Text is read from {task}/workspace. Documents the file tree treats as binary
 * open with the local app.
 */
export async function openDataDirectorySearchFile(
    request: CloudWorkspaceFileOpenRequest,
    openWorkspaceFile: (file: CodeFile) => boolean | void,
    reopenPreview: () => void,
    holdPreview?: () => void,
): Promise<void> {
    const fileName = request.fileName || request.relativePath.split(/[\\/]/).pop() || request.relativePath;
    const { preview, openLocal } = dataDirectoryFileBindings();
    if (isLikelyBinaryName(fileName)) {
        if (!openLocal) {
            console.error("[ProjectSearch] data directory local open is unavailable");
            return;
        }
        try {
            await openLocal(request.projectPath, request.relativePath);
        } catch (error) {
            console.error("[ProjectSearch] open data directory file locally failed:", error);
        }
        return;
    }
    if (!preview) {
        console.error("[ProjectSearch] data directory preview is unavailable");
        return;
    }
    holdPreview?.();
    reopenPreview();
    try {
        const data = await preview(request.projectPath, request.relativePath);
        const openedPath = String(data?.path || request.relativePath);
        const latex = isLatexSourceName(fileName);
        openWorkspaceFile({
            filePath: openedPath,
            fileName: openedPath.split(/[\\/]/).pop() || fileName,
            content: String(data?.content || ""),
            language: latex ? "latex" : String(data?.language || "plaintext"),
            opType: "read",
            updatedAt: Date.now(),
            previewTruncated: data?.truncated === true,
            latexWorkbench: latex || undefined,
            projectPath: latex ? request.projectPath : undefined,
        });
        reopenPreview();
    } catch (error) {
        if (binaryPreviewFailure(error) && openLocal) {
            try {
                await openLocal(request.projectPath, request.relativePath);
            } catch (localError) {
                console.error("[ProjectSearch] open data directory file locally failed:", localError);
            }
            return;
        }
        console.error("[ProjectSearch] open data directory file failed:", error);
    }
}

/**
 * Open one searched cloud-workspace file in the active task.
 * Text opens in the preview pane. Documents the file tree treats as binary
 * open with the local app, the same way a click in the cloud tree does.
 */
export async function openCloudWorkspaceSearchFile(
    request: CloudWorkspaceFileOpenRequest,
    openWorkspaceFile: (file: CodeFile) => boolean | void,
    reopenPreview: () => void,
): Promise<void> {
    const fileName = request.fileName || request.relativePath.split(/[\\/]/).pop() || request.relativePath;
    if (isLikelyBinaryName(fileName)) {
        try {
            await OpenCodingWorkbenchFileLocally(request.projectPath, request.relativePath);
        } catch (error) {
            console.error("[ProjectSearch] open cloud file locally failed:", error);
        }
        return;
    }
    reopenPreview();
    try {
        const data = await GetCodingWorkbenchFilePreview(request.projectPath, request.relativePath);
        const openedPath = String(data?.path || request.relativePath);
        const latex = isLatexSourceName(fileName);
        openWorkspaceFile({
            filePath: openedPath,
            fileName: openedPath.split(/[\\/]/).pop() || fileName,
            content: String(data?.content || ""),
            language: latex ? "latex" : String(data?.language || "plaintext"),
            opType: "read",
            updatedAt: Date.now(),
            previewTruncated: data?.truncated === true,
            latexWorkbench: latex || undefined,
            projectPath: latex ? request.projectPath : undefined,
        });
        reopenPreview();
    } catch (error) {
        if (binaryPreviewFailure(error)) {
            try {
                await OpenCodingWorkbenchFileLocally(request.projectPath, request.relativePath);
            } catch (localError) {
                console.error("[ProjectSearch] open cloud file locally failed:", localError);
            }
            return;
        }
        console.error("[ProjectSearch] open cloud file failed:", error);
    }
}

export function useCloudWorkspaceSearchFileOpen({
    ready,
    projectPath,
    workspaceId,
    openWorkspaceFile,
    reopenPreview,
    holdPreview,
}: CloudWorkspaceSearchFileOpenOptions): void {
    const readyRef = useRef(ready);
    readyRef.current = ready;
    const projectPathRef = useRef(projectPath);
    projectPathRef.current = projectPath;
    const workspaceIdRef = useRef(workspaceId);
    workspaceIdRef.current = workspaceId;
    const openRef = useRef(openWorkspaceFile);
    openRef.current = openWorkspaceFile;
    const reopenRef = useRef(reopenPreview);
    reopenRef.current = reopenPreview;
    const holdRef = useRef(holdPreview);
    holdRef.current = holdPreview;
    const generationRef = useRef(0);

    useEffect(() => {
        const attempt = () => {
            const pending = peekParkedCloudWorkspaceFileOpen();
            if (!pending) return;
            const dataFile = pending.source === "data";
            if (!dataFile && !readyRef.current) return;
            if (!cloudWorkspaceFileOpenMatches(pending, {
                projectPath: projectPathRef.current,
                workspaceId: workspaceIdRef.current,
            })) return;
            const request = takeParkedCloudWorkspaceFileOpen();
            if (!request) return;
            const generation = ++generationRef.current;
            const openFile = (file: CodeFile) => {
                if (generation !== generationRef.current) return false;
                return openRef.current(file);
            };
            const reopen = () => {
                if (generation !== generationRef.current) return;
                reopenRef.current();
            };
            if (dataFile) {
                void openDataDirectorySearchFile(request, openFile, reopen, () => holdRef.current?.());
                return;
            }
            void openCloudWorkspaceSearchFile(request, openFile, reopen);
        };
        attempt();
        window.addEventListener(OPEN_CLOUD_WORKSPACE_FILE_EVENT, attempt);
        return () => window.removeEventListener(OPEN_CLOUD_WORKSPACE_FILE_EVENT, attempt);
    }, [ready, projectPath, workspaceId]);
}

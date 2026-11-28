import { useCallback, useEffect, useMemo, useRef, useState, type RefObject } from "react";
import { GetCodingWorkbenchFilePreview } from "../../../wailsjs/go/main/App";
import { localizeText } from "./aiAssistantI18n";
import {
    cloudAgentPreviewCodeFile,
    cloudAgentPreviewDedupeKey,
    cloudAgentPreviewPartsFromSession,
    cloudAgentPreviewPathKey,
    cloudAgentPreviewReadRetries,
    cloudAgentPreviewSnapshot,
    cloudAgentWritesOpenPreview,
} from "./cloudAgentPreview";
import { dismissCloudAgentPreviewOpens } from "./cloudAgentVisualPreview";
import type { RemoteAssistantSession } from "../layout/remoteAssistantSession";
import { useCodePreviewState, type CodePreviewUIState } from "./useCodePreviewState";

type PreviewPayload = {
    path?: string;
    content?: string;
    language?: string;
    truncated?: boolean;
    original?: string;
};

export type CloudAgentPreviewController = {
    active: boolean;
    state: CodePreviewUIState;
    projectPath: string;
    refreshToken: number;
    openFiles: () => void;
    closePanel: () => void;
    closeFile: (filePath: string) => void;
    closeOtherFiles: (filePath: string) => void;
    closeFilesToTheRight: (filePath: string) => void;
    closeAllFiles: () => void;
    moveFile: (fromPath: string, toIndex: number) => void;
    toggleFilePinned: (filePath: string) => void;
    selectFile: (filePath: string) => void;
    /** Focus one changed file. An adopted path is selected; a new path is fetched first. */
    focusPath: (path: string) => void;
    openWorkspaceFile: (file: Parameters<ReturnType<typeof useCodePreviewState>["openWorkspaceFile"]>[0]) => boolean;
    replaceWorkspaceFileContent: (filePath: string, content: string) => void;
};

/**
 * Preview state for the cloud-agent cover. File events stay off so a cloud
 * write cannot land on the covered local tab. The last instance path is kept
 * after the cover closes, otherwise the project filter would drop the tabs.
 */
export function useCloudAgentPreview(
    session: RemoteAssistantSession | null,
    focusFileRef: RefObject<(() => void) | null>,
): CloudAgentPreviewController {
    const coverPath = session?.cloud && session.projectPath ? session.projectPath : "";
    const pathRef = useRef("");
    if (coverPath) pathRef.current = coverPath;
    const hookedPath = pathRef.current || undefined;
    const preview = useCodePreviewState(hookedPath, false, { previewWorkspacePath: hookedPath });
    const [refreshToken, setRefreshToken] = useState(0);
    const seenRef = useRef(new Set<string>());
    const seenForRef = useRef("");
    const kind = session?.cloudKind || "";
    const previewParts = useMemo(
        () => (coverPath && cloudAgentWritesOpenPreview(kind) ? cloudAgentPreviewPartsFromSession(session) : []),
        [coverPath, kind, session],
    );
    const snapshot = useMemo(
        () => previewParts.length ? cloudAgentPreviewSnapshot(previewParts) : null,
        [previewParts],
    );
    const contentKey = snapshot
        ? snapshot.changes.map((change) => cloudAgentPreviewDedupeKey(coverPath, change)).join("\n")
        : "";
    const livePaths = snapshot ? snapshot.changes.map((change) => change.path).join("\n") : "";
    const droppedPaths = snapshot ? snapshot.dropped.join("\n") : "";
    // No path to list: an empty coding round must not reload the jail.
    const treeKey = livePaths || droppedPaths ? `${coverPath}\n${livePaths}\n-\n${droppedPaths}` : "";
    const signature = contentKey || treeKey ? `${contentKey}\n#\n${treeKey}` : "";
    const partsRef = useRef(previewParts);
    partsRef.current = previewParts;
    const treeKeyRef = useRef("");
    const focusGenRef = useRef(0);

    useEffect(() => {
        if (!coverPath) {
            if (seenForRef.current) dismissCloudAgentPreviewOpens(seenForRef.current);
            seenForRef.current = "";
            seenRef.current = new Set();
            treeKeyRef.current = "";
            return;
        }
        if (seenForRef.current === coverPath) return;
        seenForRef.current = coverPath;
        seenRef.current = new Set();
    }, [coverPath]);

    useEffect(() => {
        if (!coverPath || !signature) return;
        const treeAtStart = treeKey;
        let cancelled = false;
        void (async () => {
            // Yield once so a strict-mode remount or a newer signature can
            // cancel before the directory token moves. A synchronous bump
            // would count twice.
            await Promise.resolve();
            if (cancelled) return;
            const folded = cloudAgentPreviewSnapshot(partsRef.current);
            // A delete forgets that path's seen keys. The same counts on a
            // later rewrite then fetch again. A rewrite already in this
            // snapshot is live, so its key stays seen.
            for (const path of folded.dropped) {
                const prefix = `${coverPath}\0${path}\0`;
                for (const key of [...seenRef.current]) {
                    if (key.startsWith(prefix)) seenRef.current.delete(key);
                }
            }
            const pending = folded.changes
                .filter((change) => !seenRef.current.has(cloudAgentPreviewDedupeKey(coverPath, change)));
            // Reload the jail when a path appears or disappears, before any
            // file body. Line-count updates reuse the open listing. Record
            // the key only after this run wins the yield, or the discarded
            // strict-mode pass would swallow the real reload.
            if (treeKeyRef.current !== treeAtStart) {
                treeKeyRef.current = treeAtStart;
                setRefreshToken((token) => token + 1);
            }
            let adopted = false;
            for (const change of pending) {
                if (cancelled) break;
                const key = cloudAgentPreviewDedupeKey(coverPath, change);
                if (seenRef.current.has(key)) continue;
                try {
                    let data: PreviewPayload;
                    try {
                        data = (await GetCodingWorkbenchFilePreview(coverPath, change.path)) as PreviewPayload;
                    } catch (error) {
                        if (cancelled || !cloudAgentPreviewReadRetries(error)) throw error;
                        await Promise.resolve();
                        if (cancelled) throw error;
                        data = (await GetCodingWorkbenchFilePreview(coverPath, change.path)) as PreviewPayload;
                    }
                    if (cancelled) break;
                    if (typeof data?.content !== "string") {
                        seenRef.current.add(key);
                        continue;
                    }
                    preview.adoptPreviewFile(cloudAgentPreviewCodeFile({
                        projectPath: coverPath,
                        path: String(data.path || change.path),
                        content: data.content,
                        language: data.language,
                        truncated: data.truncated === true,
                        original: typeof data.original === "string" ? data.original : undefined,
                        created: change.created,
                        added: change.added,
                        removed: change.removed,
                    }));
                    seenRef.current.add(key);
                    adopted = true;
                } catch {
                    if (cancelled) break;
                    // Binary or missing: do not open the pane on a read that
                    // never became a previewable change. The tree already refreshed.
                    seenRef.current.add(key);
                }
            }
            if (adopted) focusFileRef.current?.();
        })();
        return () => { cancelled = true; };
    }, [coverPath, focusFileRef, preview.adoptPreviewFile, signature, treeKey]);

    const openFiles = useCallback(() => {
        preview.reopenPanel();
    }, [preview.reopenPanel]);

    const closePanel = useCallback(() => {
        focusGenRef.current += 1;
        dismissCloudAgentPreviewOpens(pathRef.current);
        preview.closePanel();
    }, [preview.closePanel]);

    const focusPath = useCallback((rawPath: string) => {
        const path = cloudAgentPreviewPathKey(rawPath);
        const project = pathRef.current;
        if (!path || !project) return;
        const generation = ++focusGenRef.current;
        const existing = [...preview.state.files.values()].find(
            (file) => cloudAgentPreviewPathKey(file.filePath) === path,
        );
        if (existing) {
            preview.focusFile(existing.filePath);
            focusFileRef.current?.();
            return;
        }
        void (async () => {
            try {
                const data = (await GetCodingWorkbenchFilePreview(project, path)) as PreviewPayload;
                if (generation !== focusGenRef.current) return;
                if (pathRef.current !== project || typeof data?.content !== "string") return;
                const file = cloudAgentPreviewCodeFile({
                    projectPath: project,
                    path: String(data.path || path),
                    content: data.content,
                    language: data.language,
                    truncated: data.truncated === true,
                    original: typeof data.original === "string" ? data.original : undefined,
                    created: false,
                });
                preview.adoptPreviewFile(file);
                preview.focusFile(file.filePath);
                focusFileRef.current?.();
            } catch {
                // A missing or binary file stays out of the pane.
            }
        })();
    }, [focusFileRef, preview.adoptPreviewFile, preview.focusFile, preview.state.files]);

    return {
        active: preview.state.active,
        state: preview.state,
        projectPath: coverPath,
        refreshToken,
        openFiles,
        closePanel,
        closeFile: preview.closeFile,
        closeOtherFiles: preview.closeOtherFiles,
        closeFilesToTheRight: preview.closeFilesToTheRight,
        closeAllFiles: preview.closeAllFiles,
        moveFile: preview.moveFile,
        toggleFilePinned: preview.toggleFilePinned,
        selectFile: preview.selectFile,
        focusPath,
        openWorkspaceFile: preview.openWorkspaceFile,
        replaceWorkspaceFileContent: preview.replaceWorkspaceFileContent,
    };
}

/** Header control just left of 暂停任务. Both session and coding tasks use it. */
export function CloudAgentFilesButton({ lang, onOpen }: { lang: string; onOpen: () => void }) {
    const label = localizeText(lang, "Files", "文件", "檔案");
    return (
        <button
            type="button"
            className="task-pause-btn"
            data-testid="cloud-agent-files-btn"
            onClick={onOpen}
            aria-label={label}
            title={localizeText(lang, "Browse workspace files", "浏览工作区文件", "瀏覽工作區檔案")}
        >
            {label}
        </button>
    );
}

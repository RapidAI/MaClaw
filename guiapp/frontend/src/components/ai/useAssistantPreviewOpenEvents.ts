import { useEffect, useRef } from 'react';
import { getWailsAppModule } from '../../utils/wailsAppModule';
import {
    PREVIEW_TASK_RESULT_EVENT,
    codeFileForImmediateTaskResultPreview,
    codeFileFromTaskResultPreview,
    localizeTaskResultPreviewError,
    previewTaskResultPathFromEvent,
    taskResultPreviewKindFromPath,
    type TaskResultPreviewPayload,
} from './taskResultPreview';
import {
    OPEN_LATEX_DOCUMENT_EVENT,
    openLatexDocumentFromEvent,
    takePendingLatexDocument,
    type OpenLatexDocumentDetail,
} from './latexDocumentOpen';
import type { CodeFile } from './useCodePreviewState';

/**
 * Opens documents in the assistant's code-preview pane.
 *
 * Two requests land here:
 *  - a task result the user clicked "preview" on, and
 *  - a LaTeX document, either picked in the template library or created for a
 *    new LaTeX task.
 *
 * Both go through the same pane. The LaTeX request additionally carries the task
 * workspace path and the relative source path — exactly the pair the backend
 * save/compile bindings need — and marks the file so FilePreviewView renders the
 * editable workbench instead of a read-only compile view. That is also why an
 * expert tab can host a LaTeX editor: `allowed` admits expert tabs, and the pane
 * is opened in the same commit as the file because the panel clears preview state
 * whenever the "open" flag is false on such a tab.
 */
export type AssistantPreviewOpenEventsOptions = {
    /** False for tab types that must never show the preview pane. */
    allowed: boolean;
    /**
     * Identity of the session that owns the pane. The effect re-runs when it
     * changes, so a LaTeX request parked before the target tab was mounted is
     * picked up and a read started for a previous tab cannot land on this one.
     */
    sessionKey?: string;
    lang: string;
    openWorkspaceFile: (file: CodeFile) => void;
    /** Bumped by the panel when the pane opens so the tree/file focus follows. */
    focusOpenedFile: () => void;
    /**
     * Monotonic request id. Cancelling a pending fetch increments it, so a late
     * response cannot reopen a pane the user already dismissed.
     */
    generationRef: { current: number };
    openPreviewPane: (open: boolean) => void;
};

/**
 * Shown instead of an empty editable buffer when the template source cannot be
 * read. The editor autosaves on close, so a blank buffer that looked editable
 * would overwrite the real paper. The text is fixed rather than taken from the
 * backend error so a workspace path can never reach the UI.
 */
const LATEX_SOURCE_READ_ERROR = '无法读取论文源文件';

export function useAssistantPreviewOpenEvents({
    allowed,
    sessionKey,
    lang,
    openWorkspaceFile,
    focusOpenedFile,
    generationRef,
    openPreviewPane,
}: AssistantPreviewOpenEventsOptions): void {
    // Held in a ref so a caller that passes an inline callback does not
    // re-subscribe both window listeners on every render.
    const focusRef = useRef(focusOpenedFile);
    focusRef.current = focusOpenedFile;
    // The LaTeX document currently owned by the pane, plus the session it was
    // opened for. A LaTeX expert tab is a chat with an editor attached, so
    // returning to the tab has to put the editor back.
    const lastLatexRef = useRef<OpenLatexDocumentDetail | null>(null);
    const lastSessionRef = useRef<string | undefined>(undefined);
    useEffect(() => {
        if (!allowed) return;
        const openFile = (file: CodeFile) => {
            openWorkspaceFile(file);
            focusRef.current();
        };
        /**
         * Open a LaTeX source. Preloaded content is trusted. Otherwise the file
         * is read from the workspace *before* the file is opened, so the editor is
         * never briefly holding an empty buffer it could autosave over the paper.
         */
        const openLatexDocument = (detail: OpenLatexDocumentDetail) => {
            lastLatexRef.current = detail;
            const generation = ++generationRef.current;
            openPreviewPane(true);
            const base: CodeFile = {
                filePath: detail.relativePath,
                fileName: detail.fileName,
                projectPath: detail.projectPath,
                content: detail.content,
                language: "latex",
                opType: "read",
                updatedAt: Date.now(),
                latexWorkbench: true,
            };
            if (detail.content) {
                openFile(base);
                return;
            }
            void (async () => {
                try {
                    const { GetCodingWorkbenchFilePreview } = await getWailsAppModule();
                    if (typeof GetCodingWorkbenchFilePreview !== "function") {
                        throw new Error("GetCodingWorkbenchFilePreview unavailable");
                    }
                    const data = await GetCodingWorkbenchFilePreview(detail.projectPath, detail.relativePath) as { content?: string; truncated?: boolean };
                    if (generation !== generationRef.current) return;
                    const next = String(data?.content || "");
                    // A truncated or empty read is not a usable buffer either, so
                    // it locks the editor for the same reason a failed read does.
                    if (!next || data?.truncated) {
                        openFile({ ...base, content: "", latexReadError: LATEX_SOURCE_READ_ERROR });
                        return;
                    }
                    openFile({ ...base, content: next });
                } catch {
                    if (generation !== generationRef.current) return;
                    openFile({ ...base, content: "", latexReadError: LATEX_SOURCE_READ_ERROR });
                }
            })();
        };
        const onPreview = (event: Event) => {
            const path = previewTaskResultPathFromEvent(event);
            if (!path) return;
            const generation = ++generationRef.current;
            openPreviewPane(true);
            const immediateKind = taskResultPreviewKindFromPath(path);
            if (immediateKind) {
                openFile(codeFileForImmediateTaskResultPreview(path, immediateKind));
                return;
            }
            void (async () => {
                try {
                    const { PreviewTaskResultFile } = await getWailsAppModule();
                    if (typeof PreviewTaskResultFile !== "function") {
                        throw new Error("PreviewTaskResultFile unavailable");
                    }
                    const preview = await PreviewTaskResultFile(path) as TaskResultPreviewPayload;
                    if (generation !== generationRef.current) return;
                    openFile(codeFileFromTaskResultPreview(preview || { path }, path));
                } catch (err) {
                    if (generation !== generationRef.current) return;
                    const message = err instanceof Error ? err.message : String(err || "");
                    openFile({
                        filePath: path,
                        fileName: path.split(/[/\\]/).pop() || path,
                        absPath: path,
                        content: localizeTaskResultPreviewError(message, lang),
                        language: "plaintext",
                        opType: "read",
                        updatedAt: Date.now(),
                    });
                }
            })();
        };
        const onOpenLatexDocument = (event: Event) => {
            const detail = openLatexDocumentFromEvent(event);
            // The dispatch parked the very same request for a panel that was not
            // mounted yet. Since this listener just handled it live, the parked
            // copy has to be dropped — otherwise the next tab switch would apply
            // it a second time and reset the editor buffer over the user's edits.
            takePendingLatexDocument();
            if (detail) openLatexDocument(detail);
        };
        // Drain a request parked before this tab existed: the event above only
        // reaches a listener that was already attached, so a request fired while
        // the expert tab was still mounting would otherwise be dropped.
        const parked = takePendingLatexDocument();
        if (parked) {
            openLatexDocument(parked);
        } else if (lastSessionRef.current !== sessionKey && lastLatexRef.current) {
            // Returning to a tab that owned a LaTeX document restores its editor.
            openLatexDocument(lastLatexRef.current);
        }
        lastSessionRef.current = sessionKey;
        window.addEventListener(PREVIEW_TASK_RESULT_EVENT, onPreview);
        window.addEventListener(OPEN_LATEX_DOCUMENT_EVENT, onOpenLatexDocument);
        return () => {
            window.removeEventListener(PREVIEW_TASK_RESULT_EVENT, onPreview);
            window.removeEventListener(OPEN_LATEX_DOCUMENT_EVENT, onOpenLatexDocument);
        };
    }, [allowed, sessionKey, lang, openWorkspaceFile, generationRef, openPreviewPane]);
}

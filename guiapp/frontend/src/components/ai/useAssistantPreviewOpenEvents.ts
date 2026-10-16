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
 *
 * The document stays with the session that opened it. Switching to another task
 * must not paint that file into the new preview, and a read still in flight is
 * dropped so it cannot land late. Coming back to the owning session restores it.
 */
export type AssistantPreviewOpenEventsOptions = {
    /** False for tab types that must never show the preview pane. */
    allowed: boolean;
    /**
     * Identity of the session that owns the pane. The effect re-runs when it
     * changes, so a LaTeX request parked before the target tab was mounted is
     * picked up. A document opened for one session is restored only when that
     * same session is active again.
     */
    sessionKey?: string;
    lang: string;
    openWorkspaceFile: (file: CodeFile) => boolean | void;
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

/** What happens to the open LaTeX paper when the assistant session changes. */
export type LatexPreviewSessionAction = 'open-parked' | 'restore' | 'cancel-read' | 'keep';

/**
 * A paper belongs to the session that opened it.
 * Switching away cancels a read still in flight. Switching back restores the
 * editor. Any other session keeps its own preview.
 */
export function latexPreviewSessionAction(input: {
    parked: boolean;
    hasDocument: boolean;
    previousSession?: string;
    sessionKey?: string;
    ownerSession?: string;
    openedForSession?: string;
}): LatexPreviewSessionAction {
    if (input.parked) return 'open-parked';
    const switched = input.previousSession !== undefined && input.previousSession !== input.sessionKey;
    if (!switched) return 'keep';
    if (
        input.hasDocument
        && input.sessionKey
        && input.ownerSession === input.sessionKey
        && input.openedForSession !== input.sessionKey
    ) {
        return 'restore';
    }
    // Only the owning session has a read that must not land on the next task.
    if (input.ownerSession && input.ownerSession === input.previousSession) return 'cancel-read';
    return 'keep';
}

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
    // Updated during render so a listener installed for the previous session
    // still attributes an open to the tab that is current when the event fires.
    const sessionKeyRef = useRef(sessionKey);
    sessionKeyRef.current = sessionKey;
    // The LaTeX document and the session it belongs to. Returning to that
    // session puts the editor back; any other session must not inherit it.
    const lastLatexRef = useRef<OpenLatexDocumentDetail | null>(null);
    const latexOwnerSessionRef = useRef<string | undefined>(undefined);
    const openedForSessionRef = useRef<string | undefined>(undefined);
    const lastSessionRef = useRef<string | undefined>(undefined);
    useEffect(() => {
        const previousSession = lastSessionRef.current;
        // A disallowed tab must leave a parked paper for the task that can show it.
        const parkedDetail = allowed ? takePendingLatexDocument() : null;
        const action = latexPreviewSessionAction({
            parked: parkedDetail != null,
            hasDocument: lastLatexRef.current != null,
            previousSession,
            sessionKey,
            ownerSession: latexOwnerSessionRef.current,
            openedForSession: openedForSessionRef.current,
        });
        // A tab that cannot show the pane must not consume a parked open, but it
        // still has to drop a read started on the task we just left. Otherwise
        // coming back never looks like a return and the editor stays closed.
        const dropInflightRead = () => {
            generationRef.current += 1;
            openedForSessionRef.current = undefined;
        };
        if (!allowed) {
            if (action === 'cancel-read') dropInflightRead();
            lastSessionRef.current = sessionKey;
            return;
        }
        const openFile = (file: CodeFile): boolean => {
            // A mock returns undefined. Only an explicit refusal drops the file.
            if (openWorkspaceFile(file) === false) return false;
            focusRef.current();
            return true;
        };
        /**
         * Open a LaTeX source. Preloaded content is trusted. Otherwise the file
         * is read from the workspace *before* the file is opened, so the editor is
         * never briefly holding an empty buffer it could autosave over the paper.
         */
        const openLatexDocument = (detail: OpenLatexDocumentDetail) => {
            const owner = sessionKeyRef.current;
            const previousDoc = lastLatexRef.current;
            const previousOwner = latexOwnerSessionRef.current;
            const previousOpened = openedForSessionRef.current;
            lastLatexRef.current = detail;
            latexOwnerSessionRef.current = owner;
            openedForSessionRef.current = owner;
            const generation = ++generationRef.current;
            const rollback = () => {
                if (latexOwnerSessionRef.current !== owner) return;
                lastLatexRef.current = previousDoc;
                latexOwnerSessionRef.current = previousOwner;
                openedForSessionRef.current = previousOpened;
            };
            // The pane flag has to be set before the read returns. The panel
            // drops preview state whenever the flag is still false, which would
            // throw away a snapshot just restored for this task.
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
            const publish = (file: CodeFile) => {
                if (generation !== generationRef.current) return;
                if (sessionKeyRef.current !== owner) return;
                if (!openFile(file)) {
                    // Refusal must not replace the paper already owned by
                    // another task, and must not leave the pane open here.
                    rollback();
                    openPreviewPane(false);
                    return;
                }
            };
            if (detail.content) {
                publish(base);
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
                        publish({ ...base, content: "", latexReadError: LATEX_SOURCE_READ_ERROR });
                        return;
                    }
                    publish({ ...base, content: next });
                } catch {
                    if (generation !== generationRef.current) return;
                    publish({ ...base, content: "", latexReadError: LATEX_SOURCE_READ_ERROR });
                }
            })();
        };
        const onPreview = (event: Event) => {
            const path = previewTaskResultPathFromEvent(event);
            if (!path) return;
            const generation = ++generationRef.current;
            const show = (file: CodeFile) => {
                if (generation !== generationRef.current) return;
                if (!openFile(file)) {
                    openPreviewPane(false);
                    return;
                }
                openPreviewPane(true);
            };
            const immediateKind = taskResultPreviewKindFromPath(path);
            if (immediateKind) {
                show(codeFileForImmediateTaskResultPreview(path, immediateKind));
                return;
            }
            void (async () => {
                try {
                    const { PreviewTaskResultFile } = await getWailsAppModule();
                    if (typeof PreviewTaskResultFile !== "function") {
                        throw new Error("PreviewTaskResultFile unavailable");
                    }
                    const preview = await PreviewTaskResultFile(path) as TaskResultPreviewPayload;
                    show(codeFileFromTaskResultPreview(preview || { path }, path));
                } catch (err) {
                    const message = err instanceof Error ? err.message : String(err || "");
                    show({
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
        if (action === 'open-parked' && parkedDetail) {
            openLatexDocument(parkedDetail);
        } else if (action === 'restore' && lastLatexRef.current) {
            openLatexDocument(lastLatexRef.current);
        } else if (action === 'cancel-read') {
            dropInflightRead();
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

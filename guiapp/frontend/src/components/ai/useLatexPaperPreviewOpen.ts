import { useEffect, useRef } from "react";
import { dispatchOpenLatexDocument } from "./latexDocumentOpen";
import { latexWorkbenchFromAgentFile, type CodeFile } from "./useCodePreviewState";
import type { LatexEntryWatch } from "./usePendingAssistantTabOpen";

type LatexPaperPreviewOpenInput = {
    tabId: string;
    projectPath?: string;
    latexPaperTab: boolean;
    latexSourcePath: string;
    entryWatch: LatexEntryWatch;
    files: Map<string, CodeFile>;
    adoptPreviewFile: (file: CodeFile) => void;
    setTaskResultPreviewOpen: (open: boolean) => void;
};

/**
 * Keep a LaTeX paper's preview in step with the workspace.
 *
 * A template rewrite arrives as an ordinary file update (op modify, no
 * workbench flag). Adopt that file as the paper and open the pane; otherwise
 * the chat ends with the source changed and nowhere to look.
 *
 * The remembered source opens once per tab. While the workspace entry is
 * still being read, a restored cache can name main.tex, and opening it would
 * show the skeleton that the read is about to remove.
 */
export function useLatexPaperPreviewOpen({
    tabId,
    projectPath,
    latexPaperTab,
    latexSourcePath,
    entryWatch,
    files,
    adoptPreviewFile,
    setTaskResultPreviewOpen,
}: LatexPaperPreviewOpenInput): void {
    useEffect(() => {
        if (!latexPaperTab) return;
        const path = String(projectPath || "").trim();
        let opened = false;
        for (const file of files.values()) {
            const next = latexWorkbenchFromAgentFile(file, path);
            if (!next) continue;
            adoptPreviewFile(next);
            opened = true;
        }
        if (opened) setTaskResultPreviewOpen(true);
    }, [projectPath, adoptPreviewFile, files, latexPaperTab, setTaskResultPreviewOpen]);

    const openedKeyRef = useRef("");
    const pendingRef = useRef(entryWatch.pending);
    pendingRef.current = entryWatch.pending;
    useEffect(() => {
        if (!latexPaperTab) return;
        const path = String(projectPath || "").trim();
        if (!path || !latexSourcePath) return;
        if (pendingRef.current(path)) return;
        const key = `${tabId}\0${path}\0${latexSourcePath}`;
        if (openedKeyRef.current === key) return;
        openedKeyRef.current = key;
        dispatchOpenLatexDocument({ projectPath: path, relativePath: latexSourcePath });
    }, [tabId, projectPath, entryWatch.epoch, latexPaperTab, latexSourcePath]);
}

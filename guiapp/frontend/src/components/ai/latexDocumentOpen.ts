/**
 * Hand-off used to put the assistant into LaTeX editing mode.
 *
 * The LaTeX workbench lives in the shared assistant preview pane, so a LaTeX
 * document is opened the same way a task result is previewed: a window event
 * that the panel answers by opening one `CodeFile` in the pane. The difference
 * is the payload — it carries the task workspace path plus the relative source
 * path, which is exactly the pair the backend `SaveCodingWorkbenchTextFile` /
 * `CompileLatexWorkbenchFile` bindings need, and it marks the file so the panel
 * renders the editable LaTeX editor instead of a read-only compile view.
 *
 * A request is also parked in a module-level slot. The launcher cannot know when
 * the expert tab has finished mounting, and a plain event fired too early would
 * be lost with no listener attached. The panel drains the slot on every mount and
 * on every tab switch, which makes the hand-off independent of ordering instead
 * of relying on a timing guess.
 */
export const OPEN_LATEX_DOCUMENT_EVENT = "maclaw:open-latex-document";

export type OpenLatexDocumentRequest = {
    /** Task directory. The backend resolves it to the task execution workspace. */
    projectPath: string;
    /** Workspace-relative LaTeX source path, e.g. "main.tex". */
    relativePath: string;
    /** Preloaded source. The panel keeps it editable and re-reads on save. */
    content?: string;
    fileName?: string;
};

/** Normalised detail carried by the event. Both fields the code-preview store
 * needs are always present, so the listener never has to guess a default. */
export type OpenLatexDocumentDetail = Required<OpenLatexDocumentRequest>;

function normalize(request: OpenLatexDocumentRequest): OpenLatexDocumentDetail | null {
    const projectPath = String(request?.projectPath || "").trim();
    const relativePath = String(request?.relativePath || "").trim();
    if (!projectPath || !relativePath) return null;
    return {
        projectPath,
        relativePath,
        content: String(request?.content || ""),
        fileName: String(request?.fileName || "") || relativePath.split(/[/\\]/).pop() || relativePath,
    };
}

function isLatexSourcePath(path: string): boolean {
    const ext = (path.toLowerCase().split(".").pop() || "");
    return ext === "tex" || ext === "latex" || ext === "ltx";
}

/** The most recent request no panel has picked up yet. */
let pendingLatexDocument: OpenLatexDocumentDetail | null = null;
const latexRelativeByProject = new Map<string, string>();

/** Last source opened for a task in this session. Empty when nothing has been
 * opened: the paper entry comes from the task, and a missing memory must not
 * pretend the workspace root holds main.tex. */
export function latexRelativePathForProject(projectPath: string): string {
    return latexRelativeByProject.get(String(projectPath || "").trim()) || "";
}

export function dispatchOpenLatexDocument(request: OpenLatexDocumentRequest): void {
    const detail = normalize(request);
    if (!detail) return;
    // A non-LaTeX path here would silently fall back to the plain code viewer
    // with no editor and no compile button, so refuse it at the boundary.
    if (!isLatexSourcePath(detail.relativePath)) return;
    if (typeof window === "undefined") return;
    latexRelativeByProject.set(detail.projectPath, detail.relativePath);
    pendingLatexDocument = detail;
    window.dispatchEvent(new CustomEvent(OPEN_LATEX_DOCUMENT_EVENT, { detail }));
}

/**
 * Take the parked request, if any. The panel calls this on mount and on every
 * tab switch so a request made before the target tab existed is still honoured.
 */
export function takePendingLatexDocument(): OpenLatexDocumentDetail | null {
    const detail = pendingLatexDocument;
    pendingLatexDocument = null;
    return detail;
}

/**
 * Drop a parked request. Only for tests: the slot is module state, so one test's
 * request would otherwise be consumed by the next test's panel.
 */
export function resetLatexDocumentRequestForTests(): void {
    pendingLatexDocument = null;
    latexRelativeByProject.clear();
}

export function openLatexDocumentFromEvent(event: Event): OpenLatexDocumentDetail | null {
    return normalize((event as CustomEvent<OpenLatexDocumentRequest>).detail);
}

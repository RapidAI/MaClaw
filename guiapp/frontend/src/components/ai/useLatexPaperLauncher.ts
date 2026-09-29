import { useCallback, useRef } from 'react';
import { getWailsAppModule } from '../../utils/wailsAppModule';
import { parseExpertListJSON, type ExpertDefinition } from './expertTypes';
import { dispatchOpenLatexDocument } from './latexDocumentOpen';
import type { PendingExpertOpen } from './usePendingAssistantTabOpen';
import {
    LATEX_EXPERT_ID,
    createLatexDocumentForTask,
    latexExpertStub,
    latexPaperOpeningMessage,
    type LatexTemplate,
} from '../../utils/latexTemplates';

/** Task record shape returned by the `CreateExpertTask` binding. */
type ExpertTaskRecord = { project_path?: string } | null | undefined;

export type LatexPaperLauncherParams = {
    lang: string;
    showAlert: (message: string) => void;
    switchTool: (tool: string) => void;
    /** Add the freshly registered task to the sidebar list. */
    registerTaskItem: (created: ExpertTaskRecord) => void;
    openExpert: (pending: PendingExpertOpen) => void;
    createExpertTask: (expertId: string, expertName: string) => Promise<ExpertTaskRecord>;
};

function launchFailureMessage(lang: string, zhHans: string, zhHant: string, en: string): string {
    if (lang === 'zh-Hans') return zhHans;
    if (lang === 'zh-Hant') return zhHant;
    return en;
}

/**
 * Resolve the LaTeX paper expert definition. The new-task dialog already holds
 * the real one; the library entry point looks it up and falls back to a stub,
 * because the expert is compiled into the binary and a catalogue read failure
 * must never block starting a paper.
 */
async function resolveLatexExpert(lang: string, override?: ExpertDefinition): Promise<ExpertDefinition> {
    if (override) return override;
    const stub = latexExpertStub(lang);
    try {
        const mod = await getWailsAppModule();
        if (typeof mod.ListExperts !== 'function') return stub;
        const found = parseExpertListJSON(await mod.ListExperts()).find((item) => item.id === LATEX_EXPERT_ID);
        return found || stub;
    } catch (error) {
        console.error('[latex_template] expert lookup failed:', error);
        return stub;
    }
}

/**
 * Starts a LaTeX paper from a template.
 *
 * Registering the expert task is what gives the document a workspace, so the
 * order is fixed and not interchangeable: create the task, materialise the
 * document inside it, open the expert, and only then request the LaTeX editor
 * for the source file. Asking for the editor first would resolve the workspace
 * to nothing, so the preview pane would have no file to read.
 */
export function useLatexPaperLauncher(params: LatexPaperLauncherParams) {
    const latest = useRef(params);
    latest.current = params;
    // A template card can be clicked twice before the first launch settles.
    // Two overlapping launches would each create a document and each request the
    // editor, so the second one is dropped rather than interleaved.
    const inFlight = useRef(false);

    return useCallback(async (template: LatexTemplate, expertOverride?: ExpertDefinition) => {
        if (inFlight.current) return;
        inFlight.current = true;
        const { lang, showAlert, switchTool, registerTaskItem, openExpert, createExpertTask } = latest.current;
        try {
            const expert = await resolveLatexExpert(lang, expertOverride);
            let projectPath = '';
            try {
                const created = await createExpertTask(expert.id, expert.name || latexExpertStub(lang).name);
                projectPath = String(created?.project_path || '');
                if (projectPath) registerTaskItem(created);
            } catch (error) {
                console.error('[latex_template] create expert task failed:', error);
                showAlert(launchFailureMessage(
                    lang,
                    '无法创建 LaTeX 论文任务，请稍后重试。',
                    '無法建立 LaTeX 論文任務，請稍後重試。',
                    'The LaTeX paper task could not be created. Please try again.',
                ));
                return;
            }
            if (!projectPath) return;
            const document = await createLatexDocumentForTask(projectPath, template.id, '', lang, (message) => {
                showAlert(launchFailureMessage(
                    lang,
                    `无法创建 LaTeX 文档：${message}`,
                    `無法建立 LaTeX 文件：${message}`,
                    `The LaTeX document could not be created: ${message}`,
                ));
            });
            if (!document) return;
            openExpert({
                expert,
                initialMessage: latexPaperOpeningMessage(lang, template, document),
                latexDocument: { relativePath: document.relative_path },
            });
            switchTool('ai');
            // The request is parked as well as dispatched, so it is honoured even
            // if the expert tab has not finished mounting its listener yet.
            dispatchOpenLatexDocument({
                projectPath: document.project_path,
                relativePath: document.relative_path,
            });
        } finally {
            inFlight.current = false;
        }
    }, []);
}

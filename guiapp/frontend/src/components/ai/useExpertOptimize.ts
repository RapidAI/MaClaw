/**
 * Expert optimization ("专家优化") controller for the assistant panel.
 *
 * Distills the active expert tab's session into an optimized-expert draft via
 * the OptimizeExpertFromSession Wails binding; the panel then opens the expert
 * editor (ExpertOptimizeEditorDialog) prefilled with that draft. Extracted
 * from AIAssistantPanel to keep it under the size guard.
 */
import { useCallback, useState } from "react";
import { localizeText } from "./aiAssistantI18n";
import type { ExpertOptimizeDraft } from "./expertTypes";
import { getWailsAppModule } from "../../utils/wailsAppModule";

export interface ExpertOptimizeController {
    /** True while the backend is distilling the session (disables the button). */
    expertOptimizeBusy: boolean;
    /** Non-null while the prefilled editor dialog should be shown. */
    expertOptimizeDraft: ExpertOptimizeDraft | null;
    /** Title bar button handler; no-op unless an expert tab is active. */
    handleOptimizeExpert: () => void;
    /** Closes the draft dialog without saving. */
    clearExpertOptimizeDraft: () => void;
}

export function useExpertOptimize(opts: {
    /** expertId of the active tab when it is an expert tab, else null/undefined. */
    activeExpertId?: string | null;
    lang?: string;
    showAlert: (message: string, title?: string) => Promise<void>;
}): ExpertOptimizeController {
    const { activeExpertId, lang, showAlert } = opts;
    const [expertOptimizeBusy, setExpertOptimizeBusy] = useState(false);
    const [expertOptimizeDraft, setExpertOptimizeDraft] = useState<ExpertOptimizeDraft | null>(null);

    const clearExpertOptimizeDraft = useCallback(() => setExpertOptimizeDraft(null), []);

    const handleOptimizeExpert = useCallback(async () => {
        if (!activeExpertId || expertOptimizeBusy) return;
        setExpertOptimizeBusy(true);
        try {
            const { OptimizeExpertFromSession } = await getWailsAppModule();
            if (typeof OptimizeExpertFromSession !== "function") {
                throw new Error("OptimizeExpertFromSession unavailable");
            }
            const raw = await OptimizeExpertFromSession(activeExpertId);
            let draft: ExpertOptimizeDraft = {};
            try {
                const parsed = JSON.parse(raw || "{}");
                if (parsed && typeof parsed === "object") draft = parsed as ExpertOptimizeDraft;
            } catch {
                draft = {};
            }
            setExpertOptimizeDraft(draft);
        } catch (err) {
            console.error("[OptimizeExpertFromSession] failed:", err);
            const message = err instanceof Error ? err.message : String(err || "");
            void showAlert(
                message || localizeText(lang, "Failed to optimize expert", "专家优化失败", "專家優化失敗"),
                localizeText(lang, "Optimize Expert", "专家优化", "專家優化"),
            );
        } finally {
            setExpertOptimizeBusy(false);
        }
    }, [activeExpertId, expertOptimizeBusy, lang, showAlert]);

    return { expertOptimizeBusy, expertOptimizeDraft, handleOptimizeExpert, clearExpertOptimizeDraft };
}

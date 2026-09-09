/**
 * Mounts the expert editor prefilled with a "专家优化" draft produced by
 * useExpertOptimize. Saving notifies open expert tabs via the
 * maclaw:expert-updated event so they refresh title/icon in place.
 */
import { ExpertEditorDialog } from "../pages/ExpertEditorDialog";
import type { ExpertOptimizeDraft } from "./expertTypes";

export function ExpertOptimizeEditorDialog({ lang, draft, onClose }: {
    lang?: string;
    draft: ExpertOptimizeDraft | null;
    onClose: () => void;
}) {
    if (!draft) return null;
    return (
        <ExpertEditorDialog
            lang={lang}
            expert={null}
            optimizeDraft={draft}
            onClose={onClose}
            onSaved={(saved) => {
                onClose();
                window.dispatchEvent(new CustomEvent('maclaw:expert-updated', { detail: { expert: saved } }));
            }}
        />
    );
}

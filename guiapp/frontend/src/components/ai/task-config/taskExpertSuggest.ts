import type { TaskDraft } from "./taskDraft";

/** Mirrors guiapp.NewTaskExpertSuggestion. An empty or unconfident reading keeps 通用. */
export interface NewTaskExpertSuggestion {
    expertId?: string;
    expertName?: string;
    confident?: boolean;
    reason?: string;
    source?: string;
}

/**
 * The expert to assign before create.
 * The draft must still be 通用, with no workflow, and a local workspace.
 * An unsure reading, general, or a cloud/remote workspace returns null.
 */
export function suggestedExpertForDraft(
    draft: TaskDraft,
    suggestion: NewTaskExpertSuggestion | null | undefined,
): { id: string; name: string } | null {
    if ((draft.expertId || "").trim()) return null;
    if (draft.workflowTemplateId !== null) return null;
    if (draft.workspace.kind !== "local") return null;
    if (!suggestion?.confident) return null;
    const id = (suggestion.expertId || "").trim();
    if (!id || id === "general") return null;
    return { id, name: (suggestion.expertName || "").trim() || id };
}

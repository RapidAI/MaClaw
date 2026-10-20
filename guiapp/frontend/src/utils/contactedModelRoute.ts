export type ContactedProfileSummary = {
    provider_id?: string;
    providerID?: string;
    provider_name?: string;
    providerName?: string;
    model?: string;
};

/**
 * Profile-less tasks created from 新建任务 have no execution profile of their
 * own. Their turns still use the assistant assignment, so the composer writes
 * that profile. Coding tasks keep the coding assignment.
 */
export function quickModelWriteProfile(activeProfile: string | undefined): "assistant" | "coding" {
    return activeProfile === "coding" ? "coding" : "assistant";
}

/** Profile-less local tasks still run on the assistant route. */
export function contactedProfileForExecution(
    activeProfile: string | undefined,
    summaries: {
        assistant?: ContactedProfileSummary | null;
        coding?: ContactedProfileSummary | null;
        /** Resolved foreground loop role. Hub auto overrides the assistant profile. */
        foreground?: ContactedProfileSummary | null;
    } | null | undefined,
): ContactedProfileSummary | null {
    if (!summaries) return null;
    if (activeProfile === "coding") return summaries.coding ?? null;
    // Desktop chat calls ResolveMaclawLLMRole("foreground"). A Hub role
    // snapshot such as auto replaces the assistant profile for that call.
    if (contactedProfileProviderName(summaries.foreground) || contactedProfileModel(summaries.foreground)) {
        return summaries.foreground ?? null;
    }
    return summaries.assistant ?? null;
}

export function contactedProfileProviderID(summary: ContactedProfileSummary | null | undefined): string {
    return String(summary?.provider_id ?? summary?.providerID ?? "").trim();
}

export function contactedProfileProviderName(summary: ContactedProfileSummary | null | undefined): string {
    return String(summary?.provider_name ?? summary?.providerName ?? "").trim();
}

export function contactedProfileModel(summary: ContactedProfileSummary | null | undefined): string {
    return String(summary?.model ?? "").trim();
}

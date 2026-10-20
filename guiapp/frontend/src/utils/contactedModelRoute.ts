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
        /** Resolved foreground loop role. Official auto can still name a Hub band; a third-party assistant assignment is this role. */
        foreground?: ContactedProfileSummary | null;
    } | null | undefined,
): ContactedProfileSummary | null {
    if (!summaries) return null;
    if (activeProfile === "coding") return summaries.coding ?? null;
    // Desktop chat calls ResolveMaclawLLMRole("foreground"). That resolution
    // follows a third-party assistant assignment. It still replaces the
    // assistant profile when the official Hub band or a local pin differs.
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

/**
 * Desktop chat calls the foreground role. The status card names that role
 * when its provider or model differs from the assistant assignment. Coding
 * tasks stay on the coding assignment. A matching foreground role leaves the
 * quick picker as the live model.
 */
export function foregroundRoleSeparatesTurn(
    activeProfile: string | undefined,
    summaries: {
        assistant?: ContactedProfileSummary | null;
        coding?: ContactedProfileSummary | null;
        foreground?: ContactedProfileSummary | null;
    } | null | undefined,
): boolean {
    if (activeProfile === "coding" || !summaries) return false;
    const foregroundProvider = contactedProfileProviderName(summaries.foreground);
    const foregroundModel = contactedProfileModel(summaries.foreground);
    if (!foregroundProvider && !foregroundModel) return false;
    return foregroundProvider !== contactedProfileProviderName(summaries.assistant)
        || foregroundModel !== contactedProfileModel(summaries.assistant);
}

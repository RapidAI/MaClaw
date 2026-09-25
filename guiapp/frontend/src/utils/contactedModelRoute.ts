export type ContactedProfileSummary = {
    provider_id?: string;
    providerID?: string;
    provider_name?: string;
    providerName?: string;
    model?: string;
};

/** Profile-less local tasks still run on the assistant route. */
export function contactedProfileForExecution(
    activeProfile: string | undefined,
    summaries: { assistant?: ContactedProfileSummary | null; coding?: ContactedProfileSummary | null } | null | undefined,
): ContactedProfileSummary | null {
    if (!summaries) return null;
    if (activeProfile === "coding") return summaries.coding ?? null;
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

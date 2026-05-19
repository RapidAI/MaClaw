import type { AITab } from "./AITabTypes";

export const LEGACY_LOCAL_AI_PARTICIPANT_ID = "local-maclaw";
export const LOCAL_AI_DISPLAY_NAME_EN = "Local AI";
export const LOCAL_AI_DISPLAY_NAME_ZH_HANS = "本机AI";
export const LOCAL_AI_DISPLAY_NAME_ZH_HANT = "本機AI";

export type LocalGroupExecutorRegistration = {
    participant_id?: string;
    ParticipantID?: string;
    display_name?: string;
    DisplayName?: string;
};

export function normalizeParticipantId(value: string | null | undefined): string {
    return String(value || "").trim().toLowerCase();
}

function compactLocalAIName(value: string): string {
    return value.replace(/\s+/g, "").toLowerCase();
}

export function looksLikeRawParticipantId(value: string): boolean {
    return /^(m_[A-Za-z0-9]+|machine[-_][A-Za-z0-9-]+|ve[-_][A-Za-z0-9-]+|profile[-_][A-Za-z0-9-]+|disc[-_][A-Za-z0-9-]+|discussion[-_][A-Za-z0-9-]+|consultation[-_][A-Za-z0-9-]+|session[-_][A-Za-z0-9-]+|[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})$/i.test(value);
}

export function isLocalAIName(value: string | null | undefined): boolean {
    const name = String(value || "").trim();
    if (!name) return false;
    const compact = compactLocalAIName(name);
    return compact === "localai" || compact === "local-ai" || compact === "本机ai" || compact === "本機ai";
}

export function isLocalParticipantId(value: string | null | undefined, localParticipantIds?: string[]): boolean {
    const normalized = normalizeParticipantId(value);
    if (!normalized) return false;
    if (normalized === LEGACY_LOCAL_AI_PARTICIPANT_ID) return true;
    return (localParticipantIds || []).some((id) => normalizeParticipantId(id) === normalized);
}

export function isLocalParticipant(tab: Pick<AITab, "localParticipantIds" | "participantNames">, participantId: string): boolean {
    return isLocalParticipantId(participantId, tab.localParticipantIds) || isLocalAIName(tab.participantNames?.[participantId]);
}

export function hasLocalAIParticipant(tab: Pick<AITab, "participants" | "localParticipantIds" | "participantNames">): boolean {
    return (tab.participants || []).some((id) => isLocalParticipant(tab, id));
}

export function localAINameForLang(lang?: string): string {
    if (lang === "zh-Hant") return LOCAL_AI_DISPLAY_NAME_ZH_HANT;
    if (!lang || lang.startsWith("zh")) return LOCAL_AI_DISPLAY_NAME_ZH_HANS;
    return LOCAL_AI_DISPLAY_NAME_EN;
}

export function localExecutorParticipantID(value: LocalGroupExecutorRegistration | null | undefined): string {
    return String(value?.participant_id || value?.ParticipantID || "").trim();
}

export function localExecutorDisplayName(value: LocalGroupExecutorRegistration | null | undefined): string {
    return String(value?.display_name || value?.DisplayName || LOCAL_AI_DISPLAY_NAME_EN).trim() || LOCAL_AI_DISPLAY_NAME_EN;
}

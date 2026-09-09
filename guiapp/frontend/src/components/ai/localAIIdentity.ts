import type { AITab } from "./AITabTypes";

export const LEGACY_LOCAL_AI_PARTICIPANT_ID = "local-maclaw";
// Display names avoid consumer "AI persona" branding; this is the local worker node.
export const LOCAL_AI_DISPLAY_NAME_EN = "Local";
export const LOCAL_AI_DISPLAY_NAME_ZH_HANS = "本机";
export const LOCAL_AI_DISPLAY_NAME_ZH_HANT = "本機";

export type LocalGroupExecutorRegistration = {
    participant_id?: string;
    ParticipantID?: string;
    display_name?: string;
    DisplayName?: string;
};

export function normalizeParticipantId(value: string | null | undefined): string {
    return String(value || "").trim().replace(/[\\/\s-]+/g, "_").toLowerCase();
}

function participantIdentityKeys(value: string | null | undefined): string[] {
    const normalized = normalizeParticipantId(value);
    if (!normalized) return [];
    const keys = new Set<string>([normalized]);
    const withoutVEPrefix = /^ve[_-](.+)$/.exec(normalized)?.[1] || normalized;
    keys.add(withoutVEPrefix);
    keys.add(`ve_${withoutVEPrefix}`);
    keys.add(`ve-${withoutVEPrefix}`);
    return [...keys];
}

function participantIdentityMatches(left: string | null | undefined, right: string | null | undefined): boolean {
    const rightKeys = new Set(participantIdentityKeys(right));
    if (rightKeys.size === 0) return false;
    return participantIdentityKeys(left).some((key) => rightKeys.has(key));
}

const LOCAL_HUMAN_PARTICIPANT_IDS = new Set(["me", "user", "local", "local-user", "local_user", "operator", "desktop-user", "desktop_user", "initiator"]);
const LOCAL_AI_NAME_ALIASES = new Set([
    "local", "localai", "local-ai", "local maclaw", "localmaclaw",
    "本机", "本机ai", "本機", "本機ai", "本地", "本地ai",
]);

export function isLocalHumanParticipantId(value: string | null | undefined): boolean {
    return participantIdentityKeys(value).some((key) => LOCAL_HUMAN_PARTICIPANT_IDS.has(key));
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
    return LOCAL_AI_NAME_ALIASES.has(compactLocalAIName(name));
}

export function isLocalParticipantId(value: string | null | undefined, localParticipantIds?: string[]): boolean {
    if (participantIdentityMatches(value, LEGACY_LOCAL_AI_PARTICIPANT_ID)) return true;
    return (localParticipantIds || []).some((id) => participantIdentityMatches(id, value));
}

export function isLocalParticipant(tab: Pick<AITab, "localParticipantIds" | "participantNames">, participantId: string): boolean {
    return isLocalParticipantId(participantId, tab.localParticipantIds) || isLocalAIName(participantNameForId(tab.participantNames, participantId));
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

export function participantNameForId(names: Record<string, string> | undefined | null, id: string): string | undefined {
    if (!names) return undefined;
    for (const [key, value] of Object.entries(names)) {
        if (participantIdentityMatches(key, id)) return value;
    }
    return undefined;
}

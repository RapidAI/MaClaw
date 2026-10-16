// Typed questions for the companion composer. This store is not the
// assistant prompt history (`ai-assistant-prompt-history`) and not a
// companion transcript. The 论文解读 button does not write here.
export const FILE_COMPANION_INPUT_HISTORY_KEY = "maclaw.fileCompanionInputHistory.v1";
export const FILE_COMPANION_INPUT_HISTORY_MAX = 100;
export const FILE_COMPANION_INPUT_ENTRY_MAX = 12_000;

function storage(): Storage | null {
    try {
        return window.localStorage;
    } catch {
        return null;
    }
}

function normalize(value: unknown): string[] {
    if (!Array.isArray(value)) return [];
    const out: string[] = [];
    for (const item of value) {
        if (typeof item !== "string") continue;
        const text = item.trim();
        if (!text || text.length > FILE_COMPANION_INPUT_ENTRY_MAX) continue;
        out.push(text);
    }
    return out.length > FILE_COMPANION_INPUT_HISTORY_MAX ? out.slice(-FILE_COMPANION_INPUT_HISTORY_MAX) : out;
}

export function fileCompanionInputHistory(): string[] {
    const store = storage();
    if (!store) return [];
    try {
        return normalize(JSON.parse(store.getItem(FILE_COMPANION_INPUT_HISTORY_KEY) || "[]"));
    } catch {
        return [];
    }
}

// Newest entry is last. A repeat of the latest line is kept once, so ↑ walks
// the sequence the person actually sent.
export function rememberFileCompanionInput(text: string): string[] {
    const trimmed = text.trim();
    const current = fileCompanionInputHistory();
    if (!trimmed || trimmed.length > FILE_COMPANION_INPUT_ENTRY_MAX || current[current.length - 1] === trimmed) return current;
    const next = [...current, trimmed].slice(-FILE_COMPANION_INPUT_HISTORY_MAX);
    try {
        storage()?.setItem(FILE_COMPANION_INPUT_HISTORY_KEY, JSON.stringify(next));
    } catch {
        // localStorage full or unavailable. The composer still sends.
    }
    return next;
}

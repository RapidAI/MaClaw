// Typed questions for the companion composer. This store is not the
// assistant prompt history (`ai-assistant-prompt-history`) and not a
// companion transcript. The 论文解读 button does not write here.
//
// The list lives in the desktop SQLite database (companion_reader_state.db).
// This module keeps a synchronous mirror so the composer can render without
// awaiting a binding; hydrateFileCompanionInputHistory fills it once at startup.

// Older builds kept the list under this WebView localStorage key. It is read
// once during startup migration and removed after the import succeeds.
export const FILE_COMPANION_INPUT_HISTORY_KEY = "maclaw.fileCompanionInputHistory.v1";
export const FILE_COMPANION_INPUT_HISTORY_MAX = 100;
export const FILE_COMPANION_INPUT_ENTRY_MAX = 12_000;

import { enqueueCompanionReaderWrite } from "./companionReaderWriteQueue";

let history: string[] = [];
let saveRevision = 0;

function entryLength(text: string): number {
    // Code points, matching the host's rune count. String.length counts
    // surrogate pairs twice and would reject valid 12k-character questions.
    return Array.from(text).length;
}

function normalize(value: unknown): string[] {
    if (!Array.isArray(value)) return [];
    const out: string[] = [];
    for (const item of value) {
        if (typeof item !== "string") continue;
        const text = item.trim();
        if (!text || entryLength(text) > FILE_COMPANION_INPUT_ENTRY_MAX) continue;
        out.push(text);
    }
    return out.length > FILE_COMPANION_INPUT_HISTORY_MAX ? out.slice(-FILE_COMPANION_INPUT_HISTORY_MAX) : out;
}

// hydrateFileCompanionInputHistory replaces the mirror with the list read from
// SQLite at startup. A non-list clears it, which is also how tests start clean.
export function hydrateFileCompanionInputHistory(value: unknown): void {
    history = normalize(value);
}

export function fileCompanionInputHistorySaveRevision(): number {
    return saveRevision;
}

function saveFileCompanionInputHistoryToHost(next: string[]): void {
    const app = (window as unknown as { go?: { main?: { App?: Record<string, unknown> } } }).go?.main?.App;
    const save = app?.SaveCompanionInputHistory;
    if (typeof save !== "function") return;
    const body = JSON.stringify(next);
    enqueueCompanionReaderWrite(async () => {
        try {
            await (save as (body: string) => Promise<unknown>)(body);
        } catch {
            // The session mirror stays authoritative; the next send retries.
        }
    });
}

export function fileCompanionInputHistory(): string[] {
    return history;
}

// Newest entry is last. A repeat of the latest line is kept once, so ↑ walks
// the sequence the person actually sent.
export function rememberFileCompanionInput(text: string): string[] {
    const trimmed = text.trim();
    if (!trimmed || entryLength(trimmed) > FILE_COMPANION_INPUT_ENTRY_MAX || history[history.length - 1] === trimmed) return history;
    history = [...history, trimmed].slice(-FILE_COMPANION_INPUT_HISTORY_MAX);
    saveRevision += 1;
    saveFileCompanionInputHistoryToHost(history);
    return history;
}

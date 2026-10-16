/** How many submitted inputs the assistant keeps for history recall. */
export const PROMPT_HISTORY_LIMIT = 100;

const IMAGE_FILE_EXTENSIONS = new Set([".png", ".jpg", ".jpeg", ".gif", ".webp"]);

export interface PromptHistoryAttachment {
    filePath: string;
    fileName: string;
    extension: string;
    isImage: boolean;
}

export interface PromptHistoryEntry {
    text: string;
    attachments: PromptHistoryAttachment[];
}

function extensionOf(filePath: string): string {
    const normalized = filePath.trim().toLowerCase();
    if (!normalized) return "";
    const queryIndex = normalized.search(/[?#]/);
    const pathname = queryIndex >= 0 ? normalized.slice(0, queryIndex) : normalized;
    const slash = Math.max(pathname.lastIndexOf("/"), pathname.lastIndexOf("\\"));
    const base = slash >= 0 ? pathname.slice(slash + 1) : pathname;
    const dot = base.lastIndexOf(".");
    if (dot <= 0) return "";
    return base.slice(dot);
}

function isHistoryImagePath(filePath: string): boolean {
    return IMAGE_FILE_EXTENSIONS.has(extensionOf(filePath));
}

function fileNameOf(filePath: string): string {
    return filePath.split(/[/\\]/).pop() || filePath;
}

function sameAttachments(left: readonly PromptHistoryAttachment[], right: readonly PromptHistoryAttachment[]): boolean {
    if (left.length !== right.length) return false;
    return left.every((item, index) => item.filePath === right[index]?.filePath);
}

/**
 * Keep file identity for history restore. Thumbnails are omitted so prompt
 * history stays small; the attachment strip reloads a preview from the path.
 */
export function historyAttachmentsFromSources(
    pending: ReadonlyArray<{ filePath?: string; fileName?: string; extension?: string; isImage?: boolean } | null | undefined>,
    selectedFilePaths: readonly string[] = [],
): PromptHistoryAttachment[] {
    const out: PromptHistoryAttachment[] = [];
    const seen = new Set<string>();
    const push = (filePath: string, fileName?: string, extension?: string) => {
        const path = filePath.trim();
        if (!path || seen.has(path)) return;
        seen.add(path);
        const name = (fileName || "").trim() || fileNameOf(path);
        const ext = (extension || "").trim() || extensionOf(path);
        out.push({ filePath: path, fileName: name, extension: ext, isImage: isHistoryImagePath(path) });
    };
    for (const item of pending) {
        if (!item) continue;
        push(item.filePath || "", item.fileName, item.extension);
    }
    for (const filePath of selectedFilePaths) push(filePath);
    return out;
}

export function normalizePromptHistory(entries: readonly PromptHistoryEntry[]): PromptHistoryEntry[] {
    const out: PromptHistoryEntry[] = [];
    for (const entry of entries) {
        const text = typeof entry?.text === "string" ? entry.text.trim() : "";
        const attachments = historyAttachmentsFromSources(entry?.attachments || [], []);
        if (!text && attachments.length === 0) continue;
        out.push({ text, attachments });
    }
    return out.slice(-PROMPT_HISTORY_LIMIT);
}

export function appendPromptHistory(
    entries: readonly PromptHistoryEntry[],
    text: string,
    attachments?: ReadonlyArray<{ filePath?: string; fileName?: string; extension?: string; isImage?: boolean } | null | undefined>,
): PromptHistoryEntry[] {
    const normalizedText = typeof text === "string" ? text.trim() : "";
    const normalizedAttachments = historyAttachmentsFromSources(attachments || [], []);
    if (!normalizedText && normalizedAttachments.length === 0) return entries as PromptHistoryEntry[];
    const last = entries[entries.length - 1];
    if (last && last.text === normalizedText && sameAttachments(last.attachments, normalizedAttachments)) {
        return entries as PromptHistoryEntry[];
    }
    return [...entries, { text: normalizedText, attachments: normalizedAttachments }].slice(-PROMPT_HISTORY_LIMIT);
}

/** Rebuild history saved as parallel prompt text and attachment rows. */
export function promptHistoryFromPersisted(prompts: unknown, attachments: unknown): PromptHistoryEntry[] {
    if (!Array.isArray(prompts)) return [];
    const rows = Array.isArray(attachments) ? attachments : [];
    return normalizePromptHistory(prompts.map((value, index) => ({
        text: typeof value === "string" ? value : "",
        attachments: historyAttachmentsFromSources(Array.isArray(rows[index]) ? rows[index] : [], []),
    })));
}

/** Newest history row with this exact text, or null when the text was never submitted. */
export function findNewestHistoryAttachments(
    prompts: readonly string[],
    attachments: readonly PromptHistoryAttachment[][] | undefined,
    text: string,
): PromptHistoryAttachment[] | null {
    const rows = attachments || [];
    for (let index = prompts.length - 1; index >= 0; index -= 1) {
        if (prompts[index] !== text) continue;
        return (rows[index] || []).map(item => ({ ...item }));
    }
    return null;
}

export function imageCountsForHistoryMatches(
    prompts: readonly string[],
    attachments: readonly PromptHistoryAttachment[][] | undefined,
    matches: readonly string[],
): number[] {
    return matches.map(text => {
        const row = findNewestHistoryAttachments(prompts, attachments, text);
        return row ? row.filter(item => item.isImage).length : 0;
    });
}

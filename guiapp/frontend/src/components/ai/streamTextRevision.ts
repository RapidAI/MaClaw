const revisionsByMessageId = new Map<string, number>();
const STREAM_TEXT_REVISION_CACHE_MAX = 1024;

export function streamTextRevision(messageId: string): number {
    return revisionsByMessageId.get(messageId) ?? 0;
}

export function markStreamTextReplacement(messageId: string | undefined): void {
    if (!messageId) return;
    const revision = streamTextRevision(messageId) + 1;
    revisionsByMessageId.delete(messageId);
    revisionsByMessageId.set(messageId, revision);
    if (revisionsByMessageId.size <= STREAM_TEXT_REVISION_CACHE_MAX) return;
    const oldest = revisionsByMessageId.keys().next().value;
    if (oldest !== undefined) revisionsByMessageId.delete(oldest);
}

type StreamTextFields = {
    content?: string;
    reasoning?: string;
    resultText?: string;
};

export function noteNonAppendStreamText(
    previous: StreamTextFields & { id?: string },
    next: StreamTextFields,
): void {
    for (const field of ["content", "reasoning", "resultText"] as const) {
        if (!(field in next)) continue;
        const before = previous[field] ?? "";
        const after = next[field] ?? "";
        if (after === before || after.startsWith(before)) continue;
        markStreamTextReplacement(previous.id);
        return;
    }
}

const revisionsByMessageId = new Map<string, number>();

export function streamTextRevision(messageId: string): number {
    return revisionsByMessageId.get(messageId) ?? 0;
}

export function markStreamTextReplacement(messageId: string | undefined): void {
    if (!messageId) return;
    revisionsByMessageId.set(messageId, streamTextRevision(messageId) + 1);
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

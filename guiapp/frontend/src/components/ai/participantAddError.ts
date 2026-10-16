export function participantAddErrorText(err: unknown, lang?: string): string {
    const isZh = !lang || lang.startsWith("zh");
    const generic = isZh ? "\u6dfb\u52a0\u5931\u8d25" : "Failed to add";
    const raw = extractErrorMessage(err);
    if (!raw || raw === "participant_add_failed") return generic;
    return isZh ? `${generic}\uff1a${raw}` : `${generic}: ${raw}`;
}

export function extractErrorMessage(err: unknown): string {
    if (typeof err === "string") return err.trim();
    if (err instanceof Error) return String(err.message || "").trim();
    if (err && typeof err === "object") {
        const anyErr = err as { message?: unknown; error?: unknown };
        if (typeof anyErr.message === "string") return anyErr.message.trim();
        if (typeof anyErr.error === "string") return anyErr.error.trim();
    }
    const text = String(err || "").trim();
    return text === "[object Object]" ? "" : text;
}

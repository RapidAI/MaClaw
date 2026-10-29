import { tailText } from "../../i18n/mnTail"
export function participantAddErrorText(err: unknown, lang?: string): string {
    const raw = extractErrorMessage(err);
    if (!raw || raw === "participant_add_failed") {
        return tailText(lang ?? "", "Failed to add", "\u6dfb\u52a0\u5931\u8d25");
    }
    return tailText(lang ?? "", `Failed to add: ${raw}`, `\u6dfb\u52a0\u5931\u8d25\uff1a${raw}`);
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

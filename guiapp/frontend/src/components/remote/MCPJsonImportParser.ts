import JSON5 from "json5";

function preCleanJsonText(raw: string): string {
    return raw
        .replace(/[\uFEFF\u200B\u200C\u200D\u2060]/g, "")
        .replace(/\uff0c/g, ",")
        .replace(/\uff1a/g, ":")
        .replace(/\uff1b/g, ";")
        .replace(/\u201c/g, '"')
        .replace(/\u201d/g, '"')
        .replace(/\uff02/g, '"')
        .replace(/\uff07/g, "'")
        .replace(/\uff5b/g, "{")
        .replace(/\uff5d/g, "}")
        .replace(/\uff3b/g, "[")
        .replace(/\uff3d/g, "]");
}

function looksLikeObjectPropertyFragment(text: string): boolean {
    const trimmed = text.trim();
    if (!trimmed || trimmed.startsWith("{") || trimmed.startsWith("[")) return false;
    return /^(?:"[^"]+"|'[^']+'|[A-Za-z_$][\w$-]*)\s*:/.test(trimmed);
}

export function parseRelaxedJson(raw: string): any {
    const cleaned = preCleanJsonText(raw);
    try {
        return JSON5.parse(cleaned);
    } catch (err) {
        if (!looksLikeObjectPropertyFragment(cleaned)) throw err;
        try {
            return JSON5.parse(`{${cleaned}}`);
        } catch {
            throw err;
        }
    }
}

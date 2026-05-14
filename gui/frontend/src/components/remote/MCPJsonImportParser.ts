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

export function parseRelaxedJson(raw: string): any {
    return JSON5.parse(preCleanJsonText(raw));
}
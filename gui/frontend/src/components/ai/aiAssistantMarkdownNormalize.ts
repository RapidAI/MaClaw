const escapedNewlinePattern = /\\r\\n|\\n|\\r/g;
const digitalEmployeeCapabilityIconPattern = "[\\u{1f4c1}\\u{1f4c2}\\u{1f4c4}\\u{1f4cb}\\u{1f4dd}\\u{1f4ca}\\u{1f310}\\u{1f50d}\\u{1f4ac}\\u{1f4a1}\\u{1f527}\\u{1f4e6}]";
const digitalEmployeeCapabilityIconScanPattern = new RegExp(digitalEmployeeCapabilityIconPattern, "gu");
const capabilityIconAfterPunctuationPattern = new RegExp(`([\\uff1a:;\\uff1b])\\s*(${digitalEmployeeCapabilityIconPattern}\\s*)`, "gu");
const capabilityIconMidSentencePattern = new RegExp(`([^\\n\\s])\\s+(${digitalEmployeeCapabilityIconPattern}\\s*)`, "gu");
const windowsPathEscapeProtectPattern = /[A-Za-z]:\\[^\n\r\s*?"<>|]+/g;
const compactPipeTableSeparatorPattern = /(\|?\s*:?-{3,}:?\s*(?:\|\s*:?-{3,}:?\s*)+\|?)/g;

function hasMultipleCapabilityIcons(text: string): boolean {
    const matches = text.match(digitalEmployeeCapabilityIconScanPattern);
    return (matches?.length || 0) >= 2;
}

function withWindowsPathsProtected(text: string, transform: (value: string) => string): string {
    const paths: string[] = [];
    const protectedText = text.replace(windowsPathEscapeProtectPattern, (path) => {
        const token = `__MACLAW_WIN_PATH_${paths.length}__`;
        paths.push(path);
        return token;
    });
    const transformed = transform(protectedText);
    return transformed.replace(/__MACLAW_WIN_PATH_(\d+)__/g, (_token, indexText) => {
        const index = Number(indexText);
        return paths[index] ?? _token;
    });
}

function normalizeCompactPipeTables(text: string): string {
    if (!text.includes("|") || !text.includes("---")) return text;
    return text
        .replace(/\|\|(?=\s*[^|\s])/g, "\n|")
        .replace(/([^\n])\s*(\|[^\n|]+\|[^\n]*?)(\|\s*:?-{3,}:?\s*(?:\|\s*:?-{3,}:?\s*)+\|?)/g, (_match, prefix, header, separator) => `${prefix}\n${header.trim()}\n${separator.trim()}`)
        .replace(compactPipeTableSeparatorPattern, (separator, _inner, offset, fullText) => {
            const before = offset > 0 && fullText[offset - 1] !== "\n" ? "\n" : "";
            const afterIndex = offset + separator.length;
            const after = afterIndex < fullText.length && fullText[afterIndex] !== "\n" ? "\n" : "";
            return `${before}${separator.trim()}${after}`;
        })
        .replace(/([^\n])\s+(\|[^\n|]+\|[^\n]+)/g, "$1\n$2");
}

/**
 * Insert newline before list markers that appear mid-line, but only outside
 * fenced code blocks. Prevents corrupting code content (e.g., YAML lists).
 */
export function normalizeInlineListMarkers(content: string): string {
    const parts = content.split(/(```[\s\S]*?```|```[\s\S]*$)/);
    for (let i = 0; i < parts.length; i++) {
        if (i % 2 === 1) {
            parts[i] = parts[i]
                .replace(/^(```[^\n\r\\]*)\\r\\n/, "$1\n")
                .replace(/^(```[^\n\r\\]*)\\n/, "$1\n")
                .replace(/^(```[^\n\r\\]*)\\r/, "$1\n")
                .replace(/\\r\\n(```\s*)$/, "\n$1")
                .replace(/\\n(```\s*)$/, "\n$1")
                .replace(/\\r(```\s*)$/, "\n$1");
            continue;
        }
        let normalized = withWindowsPathsProtected(parts[i], (segment) => segment
            .replace(escapedNewlinePattern, "\n")
            .replace(/\|\|(?=\s*[^|\s])/g, "\n|")
            .replace(/([\uff1a:;\uff1b.!?\uff01\uff1f\u3002,%\uff05)\uff09\]])\s*(#{1,4}\s+)/g, "$1\n$2")
            .replace(/([\uff1a:;\uff1b.!?\uff01\uff1f\u3002,%\uff05)\uff09\]])\s*(#{2,4})(?=[^#\s])/g, "$1\n$2 ")
            .replace(/([^#\n\s])\s*(#{2,4})(?=[\p{Emoji_Presentation}\p{So}])/gu, "$1\n$2 ")
            .replace(/(^|\n)(#{3,4})(?=[^#\s])/g, "$1$2 ")
            .replace(/([\uff1a:])\s*(-\s+)/g, "$1\n$2")
            .replace(/([^\n\s])(- (?:[\p{Emoji_Presentation}\p{So}]|[*]{2}|\p{L}))/gu, "$1\n$2")
            .replace(/([^\n\s])(\d+[.)]\s+)/g, "$1\n$2")
            .replace(capabilityIconAfterPunctuationPattern, "$1\n$2"));
        normalized = normalizeCompactPipeTables(normalized);
        if (hasMultipleCapabilityIcons(normalized)) {
            normalized = normalized.replace(capabilityIconMidSentencePattern, "$1\n$2");
        }
        parts[i] = normalized;
    }
    return parts.join("");
}

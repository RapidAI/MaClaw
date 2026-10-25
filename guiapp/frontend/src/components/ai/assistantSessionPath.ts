/**
 * Project-path identity shared by the transcript and the execution header.
 *
 * A second, weaker fold (slash and drive only) let the chat card and the
 * header disagree: the card uses this cleaner, so a dotted or padded path
 * still showed the running worker while the header said completed.
 */

export function normalizeProjectSessionPath(projectPath?: string | null): string {
    let path = typeof projectPath === "string" ? projectPath.trim() : "";
    if (!path) return "";
    path = path.replace(/\\+/g, "/").replace(/\/+/g, "/");
    const drive = path.match(/^([a-zA-Z]):(\/.*)?$/);
    if (drive) path = `${drive[1].toUpperCase()}:${drive[2] || "/"}`;
    const parts: string[] = [];
    const absolute = path.startsWith("/") || /^[A-Z]:\//.test(path);
    const prefix = /^[A-Z]:\//.test(path) ? path.slice(0, 3) : (path.startsWith("/") ? "/" : "");
    const body = prefix ? path.slice(prefix.length) : path;
    // Trim each segment, not just the whole string. A segment may carry
    // incidental padding around a separator (for example "dir /sub"), and
    // leaving it in place made this function non-idempotent: re-normalizing
    // the stored result then stripped it. Callers compare paths by
    // re-normalizing both sides, so that asymmetry silently broke tab
    // deduplication and produced duplicate tabs for one project.
    for (const rawPart of body.split("/")) {
        const part = rawPart.trim();
        if (!part || part === ".") continue;
        if (part === ".." && parts.length > 0 && parts[parts.length - 1] !== "..") {
            parts.pop();
            continue;
        }
        if (part === ".." && absolute) continue;
        parts.push(part);
    }
    const joined = parts.join("/");
    if (prefix) return joined ? `${prefix}${joined}` : prefix.replace(/\/$/, "") || "/";
    return joined || ".";
}

import {
    codingAgentTurnFileChanges,
    parseCodingAgentEventProgress,
    type CodingAgentFileChange,
} from "./CodingAgentProgressStatus";
import type { CodeFile } from "./useCodePreviewState";

/** One workspace path a cloud coding turn created or modified. */
export type CloudAgentPreviewChange = {
    path: string;
    created: boolean;
    deleted?: boolean;
    added?: number;
    removed?: number;
};

export function cloudAgentPreviewPathKey(path: string): string {
    let next = (path || "").trim().replace(/\\/g, "/").replace(/\/+$/, "");
    while (next.startsWith("./")) next = next.slice(2);
    return next;
}

/** Coding instances open the preview on a write. Session instances stay manual. */
export function cloudAgentWritesOpenPreview(kind?: string): boolean {
    return (kind || "").trim() === "coding";
}

function changeFromRow(row: CodingAgentFileChange): CloudAgentPreviewChange | null {
    const path = cloudAgentPreviewPathKey(row.path);
    if (!path) return null;
    return {
        path,
        created: row.created === true,
        deleted: row.deleted === true,
        added: row.added,
        removed: row.removed,
    };
}

/**
 * Paths a single progress line reports. Reads and failed writes are
 * empty. A deleted row stays so the snapshot fold can drop that path.
 */
export function cloudAgentPreviewChangesFromText(text: string): CloudAgentPreviewChange[] {
    const parsed = parseCodingAgentEventProgress(text || "");
    if (!parsed) return [];
    const event = (parsed.event || "").trim();
    if (event === "tool_finished") {
        if ((parsed.detail || "").trim() !== "write_file") return [];
        if ((parsed.outcome || "").trim().toLowerCase() === "failed") return [];
        const path = cloudAgentPreviewPathKey((parsed.files || [])[0] || "");
        if (!path) return [];
        const row = parsed.fileChanges?.find((item) => cloudAgentPreviewPathKey(item.path) === path);
        return [{
            path,
            created: parsed.created === true || row?.created === true,
            added: parsed.added ?? row?.added,
            removed: parsed.removed ?? row?.removed,
        }];
    }
    if (event !== "diff_summary") return [];
    const rows = parsed.fileChanges?.length
        ? parsed.fileChanges
        : (parsed.files || []).map((path) => ({ path, added: 0, removed: 0 }));
    const changes: CloudAgentPreviewChange[] = [];
    for (const row of rows) {
        const change = changeFromRow(row);
        if (change) changes.push(change);
    }
    return changes;
}

type CloudAgentPreviewPart = { kind?: string; text?: string };

/**
 * Latest live change per path, plus paths whose latest event deleted them.
 * A later write puts the path back in the live list. First-seen order is kept.
 * created stays true across writes until a delete drops the path.
 */
function foldCloudAgentPreviewParts(parts: CloudAgentPreviewPart[] | undefined): {
    changes: CloudAgentPreviewChange[];
    dropped: string[];
} {
    const byPath = new Map<string, CloudAgentPreviewChange>();
    const order: string[] = [];
    const dropped: string[] = [];
    for (const part of parts || []) {
        if (part?.kind !== "progress") continue;
        for (const change of cloudAgentPreviewChangesFromText(part.text || "")) {
            const key = cloudAgentPreviewPathKey(change.path);
            if (!key) continue;
            if (change.deleted) {
                byPath.delete(key);
                if (!dropped.includes(key)) dropped.push(key);
                continue;
            }
            const dropAt = dropped.indexOf(key);
            if (dropAt >= 0) dropped.splice(dropAt, 1);
            const prev = byPath.get(key);
            if (!order.includes(key)) order.push(key);
            byPath.set(key, {
                path: key,
                created: !!(prev?.created || change.created),
                added: change.added ?? prev?.added,
                removed: change.removed ?? prev?.removed,
            });
        }
    }
    return {
        changes: order.flatMap((key) => {
            const change = byPath.get(key);
            return change ? [change] : [];
        }),
        dropped,
    };
}

type CloudAgentTimelineProgress = { kind?: string; content?: string };

/**
 * Progress lines that can open a diff. Transcript rows come first. The live
 * round is appended because a later write of the same path replaces the fold.
 */
export function cloudAgentPreviewPartsFromSession(session: {
    messages?: Array<{ content?: string; codingTimeline?: CloudAgentTimelineProgress[] }>;
    live?: { parts?: CloudAgentPreviewPart[] } | null;
} | null | undefined): CloudAgentPreviewPart[] {
    const parts: CloudAgentPreviewPart[] = [];
    for (const message of session?.messages || []) {
        for (const item of message.codingTimeline || []) {
            if (item?.kind === "progress" && item.content) parts.push({ kind: "progress", text: item.content });
        }
    }
    for (const part of session?.live?.parts || []) {
        if (part?.kind === "progress") parts.push(part);
    }
    return parts;
}

/**
 * Composer file chip for one cloud turn. A transcript timeline wins, so the
 * same live progress is not added a second time. With no timeline yet, the
 * live round is the chip.
 */
export function cloudAgentTurnFileChanges(session: {
    messages?: Array<{ content?: string; codingTimeline?: CloudAgentTimelineProgress[] }>;
    live?: { parts?: CloudAgentPreviewPart[] } | null;
} | null | undefined): CodingAgentFileChange[] {
    const fromTimeline: { content: string }[] = [];
    for (const message of session?.messages || []) {
        for (const item of message.codingTimeline || []) {
            if (item?.kind === "progress" && item.content) fromTimeline.push({ content: item.content });
        }
    }
    const source = fromTimeline.length > 0
        ? fromTimeline
        : (session?.live?.parts || [])
            .filter((part) => part?.kind === "progress" && part.text)
            .map((part) => ({ content: part.text || "" }));
    return codingAgentTurnFileChanges(source).files;
}

/** Latest live changes and the paths a later event deleted. */
export function cloudAgentPreviewSnapshot(parts: CloudAgentPreviewPart[] | undefined): {
    changes: CloudAgentPreviewChange[];
    dropped: string[];
} {
    return foldCloudAgentPreviewParts(parts);
}

/** Latest change per path, in first-seen order. Deleted paths drop out. */
export function cloudAgentPreviewChangesFromParts(parts: CloudAgentPreviewPart[] | undefined): CloudAgentPreviewChange[] {
    return cloudAgentPreviewSnapshot(parts).changes;
}

/**
 * Paths whose latest event deleted them, in the order they were dropped.
 * A later write of the same path removes it. The directory watch uses this
 * so a deletion reloads the jail even though the path is no longer live.
 */
export function cloudAgentPreviewDroppedPaths(parts: CloudAgentPreviewPart[] | undefined): string[] {
    return cloudAgentPreviewSnapshot(parts).dropped;
}

/**
 * A binary, a directory, and a confirmed miss will not succeed on a second
 * read. A transport or decode failure can.
 */
export function cloudAgentPreviewReadRetries(error: unknown): boolean {
    const text = (typeof error === "string"
        ? error
        : error && typeof error === "object" && "message" in error
            ? String((error as { message?: unknown }).message ?? "")
            : "").toLowerCase();
    if (!text) return true;
    if (text.includes("binary")) return false;
    if (text.includes("directory") || text.includes("这是一个目录")) return false;
    if (text.includes("must be relative") || text.includes("outside the working directory")) return false;
    if (text.includes("已经不在了")) return false;
    return true;
}

export function cloudAgentPreviewDedupeKey(projectPath: string, change: CloudAgentPreviewChange): string {
    return [
        projectPath,
        cloudAgentPreviewPathKey(change.path),
        change.created ? "1" : "0",
        change.added === undefined ? "" : String(change.added),
        change.removed === undefined ? "" : String(change.removed),
    ].join("\0");
}

/**
 * A preview tab for one fetched cloud file.
 * A known baseline is a modify diff. A known create, or a pure insertion
 * with no baseline, is a new file. Anything else shows the file without
 * inventing a full-file diff.
 */
export function cloudAgentPreviewCodeFile(input: {
    projectPath: string;
    path: string;
    content: string;
    language?: string;
    truncated?: boolean;
    original?: string;
    created: boolean;
    added?: number;
    removed?: number;
    updatedAt?: number;
}): CodeFile {
    const path = cloudAgentPreviewPathKey(input.path);
    const fileName = path.split("/").pop() || path;
    const hasOriginal = typeof input.original === "string" && input.original.length > 0;
    const removed = input.removed ?? 0;
    const added = input.added ?? 0;
    let opType: CodeFile["opType"] = "modify";
    if (input.created || (!hasOriginal && removed === 0 && added > 0)) opType = "create";
    return {
        filePath: path,
        fileName,
        content: input.content,
        original: opType === "modify" && hasOriginal ? input.original : undefined,
        opType,
        language: input.language || "plaintext",
        updatedAt: input.updatedAt ?? Date.now(),
        forceOpen: true,
        previewTruncated: input.truncated === true,
        projectPath: input.projectPath,
    };
}

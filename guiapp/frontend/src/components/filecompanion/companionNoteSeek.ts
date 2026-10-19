/** Seconds for a note stamp. 12:05 is 725. 1:02:03 is 3723. 12:99 and 99:99 are not stamps. */
export function noteTimestampSeconds(label: string): number | null {
    const match = /^(?:(\d+):)?(\d+):(\d+)$/.exec(label.trim());
    if (!match) return null;
    const hours = match[1] === undefined ? null : Number(match[1]);
    const minutes = Number(match[2]);
    const seconds = Number(match[3]);
    if (!Number.isFinite(minutes) || !Number.isFinite(seconds) || seconds >= 60) return null;
    if (hours !== null && (!Number.isFinite(hours) || minutes >= 60)) return null;
    if (hours === null) return minutes * 60 + seconds;
    return hours * 3600 + minutes * 60 + seconds;
}

export type NoteStampLine = { label: string; seconds: number; rest: string };

const RANGE_SEPARATORS = ["–", "—", "~", "～", "-"];

/** A 0:00–2:00 chapter seeks to its start. Both ends must be real stamps. */
function noteRangeStart(token: string): { label: string; seconds: number } | null {
    for (const sep of RANGE_SEPARATORS) {
        const at = token.indexOf(sep);
        if (at <= 0) continue;
        const head = token.slice(0, at);
        const tail = token.slice(at + sep.length);
        if (!tail) continue;
        const seconds = noteTimestampSeconds(head);
        if (seconds === null || noteTimestampSeconds(tail) === null) continue;
        return { label: token, seconds };
    }
    return null;
}

/** A stamp is a button only at the start of a line, or right after a list marker. */
export function noteStampAtLineStart(line: string): NoteStampLine | null {
    const trimmed = line.trim();
    const body = trimmed.replace(/^[-*•]+(?:\s+|$)/, "").trim();
    if (!body) return null;
    const token = body.split(/\s+/, 1)[0] ?? "";
    if (!token) return null;
    const plain = noteTimestampSeconds(token);
    const parsed = plain === null ? noteRangeStart(token) : { label: token, seconds: plain };
    if (!parsed) return null;
    if (body !== token && !body.startsWith(`${token} `) && !body.startsWith(`${token}\t`)) return null;
    return { label: parsed.label, seconds: parsed.seconds, rest: body.slice(token.length).trim() };
}

const NOTE_EXACT = new Set(["视频笔记", "音频笔记", "整理成笔记", "帮我记笔记"]);
const NOTE_PHRASE = /^(请|帮我)?(给(这个|该)?(视频|音频))?(生成|做|整理)(一下|一份)?(视频|音频)?笔记$/;

/** Same closed phrases the host accepts for purpose=video-note. */
export function companionVideoNotePhrase(text: string): boolean {
    let raw = text.trim().replace(/[。.!！?？]+$/u, "");
    if (!raw) return false;
    if (raw.includes("不要") || raw.includes("别") || raw.includes("不用")) return false;
    if (raw.includes("，") || raw.includes(",")) return false;
    if (NOTE_EXACT.has(raw)) return true;
    return NOTE_PHRASE.test(raw);
}

const ARXIV_HOSTS = new Set(["arxiv.org", "www.arxiv.org", "export.arxiv.org"]);

/** Bare new-style ids, an arxiv: prefix, or an arxiv.org link. youtu.be is not one. */
export function looksLikeArxivReference(value: string): boolean {
    const raw = value.trim();
    if (!raw) return false;
    if (/^https?:\/\//i.test(raw)) {
        try {
            return ARXIV_HOSTS.has(new URL(raw).hostname.toLowerCase());
        } catch {
            return false;
        }
    }
    const lower = raw.toLowerCase();
    if (lower.startsWith("arxiv:") || lower.startsWith("arxiv.org/") || lower.startsWith("www.arxiv.org/") || lower.startsWith("export.arxiv.org/")) {
        return true;
    }
    return /^\d{4}\.\d{4,5}(v\d+)?$/i.test(raw);
}

export const FILE_COMPANION_OPEN_HINT = "如 2510.16079、arxiv.org/abs/…，或 youtube 等视频页面地址";

// Note progress is a sentence from the host. The meter reads that sentence
// and places it on the pipeline the host actually runs. It does not invent
// how long the recording will last.

export type NoteMedia = "web" | "video" | "audio";

// The preview list omits these. The note host still runs them as audio
// (fileCompanionMediaKind), so the meter must not grow a picture step.
const NOTE_AUDIO_EXT = new Set([".opus", ".silk"]);

function noteFileExt(name: string | undefined): string {
    const base = (name || "").split(/[/\\]/).pop()?.trim().toLowerCase() || "";
    const query = base.search(/[?#]/);
    const cleaned = query >= 0 ? base.slice(0, query) : base;
    const dot = cleaned.lastIndexOf(".");
    return dot > 0 ? cleaned.slice(dot) : "";
}

export function noteMediaFromKind(kind: string | undefined, name?: string): NoteMedia {
    if (kind === "web" || kind === "audio" || kind === "video") return kind;
    if (NOTE_AUDIO_EXT.has(noteFileExt(name))) return "audio";
    return "video";
}

export interface NoteStep {
    id: string;
    label: string;
}

export interface NoteStatusModel {
    title: string;
    steps: NoteStep[];
    index: number;
    // 0..1 across the current step. A speech segment uses index/total.
    within: number;
    // Set when the host reported a segment count. That fill does not creep.
    precise: boolean;
}

const LABELS: Record<string, string> = {
    watch: "画面",
    listen: "声音",
    check: "检查",
    audio: "音轨",
    frames: "画面",
    speech: "语音",
    write: "整理",
};

// Local video recognizes speech while extracting the track, then grabs frames.
// Web already has the pictures, so speech stays after the recording.
const PIPELINES: Record<NoteMedia, string[]> = {
    web: ["watch", "listen", "speech", "write"],
    video: ["check", "audio", "speech", "frames", "write"],
    audio: ["check", "audio", "speech", "write"],
};

const SEGMENT = /^正在识别语音（第 (\d+)\/(\d+) 段）$/;

interface Hit {
    id: string;
    within: number;
    precise: boolean;
}

// Phrases that share a step are listed in the order the host emits them.
// A live phrase creeps only until the next phrase of that step.
const PHRASES: ReadonlyArray<{ text: string; id: string; within: number }> = [
    { text: "正在记录画面", id: "watch", within: 0.4 },
    { text: "正在记录声音", id: "listen", within: 0.4 },
    { text: "正在检查文件", id: "check", within: 0.35 },
    { text: "正在读取字幕", id: "check", within: 0.85 },
    { text: "正在安装视频解码器", id: "audio", within: 0.2 },
    { text: "正在提取音轨", id: "audio", within: 0.55 },
    { text: "正在抽取画面", id: "frames", within: 0.4 },
    { text: "正在识别语音", id: "speech", within: 0.25 },
    { text: "正在整理笔记", id: "write", within: 0.35 },
];

function hitFor(text: string, kind: NoteMedia): Hit | null {
    const segment = SEGMENT.exec(text);
    if (segment) {
        const index = Number(segment[1]);
        const total = Number(segment[2]);
        if (total > 0 && index > 0) {
            return { id: "speech", within: Math.min(1, index / total), precise: true };
        }
    }
    // Picture text is the last work on the frames that were just grabbed.
    // On the web those frames were grabbed before speech, so it closes speech.
    if (text === "正在识别画面文字") {
        return { id: kind === "video" ? "frames" : "speech", within: 1, precise: false };
    }
    const phrase = PHRASES.find((item) => item.text === text);
    if (!phrase) return null;
    return { id: phrase.id, within: phrase.within, precise: false };
}

// 0.92 keeps the last phrase of a step from looking finished.
function creepCeiling(id: string, within: number): number {
    let ceiling = 0.92;
    for (const phrase of PHRASES) {
        if (phrase.id === id && phrase.within > within && phrase.within < ceiling) {
            ceiling = phrase.within;
        }
    }
    return ceiling;
}

function pipelineFor(kind: NoteMedia, id: string): string[] {
    const preferred = PIPELINES[kind];
    if (preferred.includes(id)) return preferred;
    if (PIPELINES.web.includes(id)) return PIPELINES.web;
    if (PIPELINES.video.includes(id)) return PIPELINES.video;
    return PIPELINES.audio;
}

export function companionNoteStatus(text: string, kind: NoteMedia): NoteStatusModel | null {
    const hit = hitFor(text.trim(), kind);
    if (!hit) return null;
    const ids = pipelineFor(kind, hit.id);
    const steps = ids.map((id) => ({ id, label: LABELS[id] || id }));
    const index = ids.indexOf(hit.id);
    return {
        title: text.trim(),
        steps,
        index: index < 0 ? 0 : index,
        within: Math.min(1, Math.max(0, hit.within)),
        precise: hit.precise,
    };
}

// Percent of the whole pipeline. elapsedMs is how long this phrase has been
// showing, not the whole note. Creep stays inside this phrase's own span:
// it stops at the next phrase of the same step, and a phrase that already
// fills its span does not move.
export function noteStatusPercent(model: NoteStatusModel, elapsedMs = 0): number {
    let within = model.within;
    const ceiling = creepCeiling(model.steps[model.index]?.id ?? "", within);
    if (!model.precise && elapsedMs > 0 && within < ceiling) {
        const creep = 1 - Math.exp(-elapsedMs / 90000);
        within = within + (ceiling - within) * creep;
    }
    const raw = ((model.index + within) / model.steps.length) * 100;
    return Math.max(4, Math.min(96, Math.round(raw)));
}

// The clock belongs to the document, not the visible card. A later phrase of
// the same note keeps the note start and restarts only the phrase start.
export function noteClockFor(
    previousStatus: string,
    kind: NoteMedia,
    noteStartedAt: number | undefined,
    phaseStartedAt: number | undefined,
    text: string,
    now: number,
): { noteStartedAt?: number; phaseStartedAt?: number } {
    if (!companionNoteStatus(text, kind)) return { noteStartedAt: undefined, phaseStartedAt: undefined };
    const continuing = companionNoteStatus(previousStatus, kind) != null && (noteStartedAt ?? 0) > 0;
    const samePhrase = continuing && previousStatus === text && (phaseStartedAt ?? 0) > 0;
    return {
        noteStartedAt: continuing ? noteStartedAt : now,
        phaseStartedAt: samePhrase ? phaseStartedAt : now,
    };
}

export function formatNoteElapsed(ms: number): string {
    const total = Math.max(0, Math.floor(ms / 1000));
    const h = Math.floor(total / 3600);
    const m = Math.floor((total % 3600) / 60);
    const s = total % 60;
    const mm = String(m).padStart(2, "0");
    const ss = String(s).padStart(2, "0");
    if (h > 0) return `${h}:${mm}:${ss}`;
    return `${mm}:${ss}`;
}

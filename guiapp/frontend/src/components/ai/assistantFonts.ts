import type { CSSProperties } from "react";

/** Prose stack the assistant uses when no custom face is selected. */
export const ASSISTANT_SYSTEM_FONT =
    "-apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', 'Helvetica Neue', Arial, sans-serif";

const CJK_GOTHIC = "'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei'";

/**
 * Installed family names come first so a full local cut (Maple Mono CN, a
 * complete JetBrains Mono, Cascadia Code) supplies its own glyphs. The bundled
 * face is the next family, not a local() source inside the same @font-face:
 * a single local() match would hide the bundled file, and Cascadia Mono would
 * replace the bundled Cascadia Code. Chinese then falls through to the gothic.
 */
export const MAPLE_MONO_STACK = `'Maple Mono CN', 'Maple Mono NF CN', 'Maple Mono', 'MaClaw Maple Mono', ${CJK_GOTHIC}, ui-monospace, monospace`;
export const JETBRAINS_MONO_STACK = `'JetBrains Mono', 'MaClaw JetBrains Mono', ${CJK_GOTHIC}, ui-monospace, monospace`;
export const CASCADIA_CODE_STACK = `'Cascadia Code', 'MaClaw Cascadia Code', ${CJK_GOTHIC}, ui-monospace, monospace`;
export const FIRA_CODE_STACK = `'Fira Code', 'MaClaw Fira Code', ${CJK_GOTHIC}, ui-monospace, monospace`;
export const SARASA_MONO_STACK = `'Sarasa Mono SC', 'Sarasa Term SC', 'Sarasa Mono', 'Cascadia Code', 'MaClaw Cascadia Code', ${CJK_GOTHIC}, ui-monospace, monospace`;
export const DENGXIAN_STACK = "'DengXian', '等线', 'PingFang SC', 'Hiragino Sans GB', 'Noto Sans CJK SC', 'Microsoft YaHei', sans-serif";
export const SARASA_GOTHIC_STACK = "'Sarasa Gothic SC', 'Sarasa UI SC', 'Sarasa Gothic', 'DengXian', '等线', 'PingFang SC', 'Microsoft YaHei', sans-serif";
export const WENKAI_STACK = "'LXGW WenKai', 'LXGW WenKai GB', '霞鹜文楷', 'KaiTi', 'Kaiti SC', 'STKaiti', '楷体', serif";

export const ASSISTANT_FONT_SYSTEM = "system";

/** Shared chat size is one integer in 12–24. Callers still ignore non-numbers and values below 12. */
export function clampChatFontSize(raw: number): number {
    if (!Number.isFinite(raw)) return 14;
    return Math.min(24, Math.max(12, Math.round(raw)));
}

export type ConfigChatFontSizeDecision =
    | { action: "apply"; size: number }
    | { action: "keep" }
    | { action: "invalid" };

/**
 * null keeps the unsaved draft. A matching draft, or no draft, accepts the snapshot.
 * Size and both font families share this so a full config echo cannot roll one field back.
 */
export function acceptDraftedValue<T>(incoming: T, draft: T | null): T | null {
    if (draft != null && draft !== incoming) return null;
    return incoming;
}

/**
 * Call after acceptDraftedValue has accepted the snapshot.
 * A matching snapshot confirms the draft only once that attempt's save has
 * started. Until then the same value may still be waiting in the queue, and
 * clearing the draft lets an older snapshot cancel it.
 */
export function snapshotConfirmsDraft<T>(incoming: T, draft: T | null, newestHasStarted: boolean): boolean {
    if (draft == null || draft !== incoming) return true;
    return newestHasStarted;
}

/**
 * One config field, one in-flight patch. PatchConfigFields commits in lock order,
 * so two overlapping saves of the same field can land with the older value last.
 * Later jobs wait. The first job starts immediately.
 */
export type SerialQueue = {
    tail: Promise<void>;
    idle: boolean;
    ticket: number;
    generation: number;
    /** Bumped when a new value is shown and still needs a save. */
    attempt: number;
    /** The attempt whose save has been handed to the backend. */
    started: number;
};

export function createSerialQueue(): SerialQueue {
    return { tail: Promise.resolve(), idle: true, ticket: 0, generation: 0, attempt: 0, started: 0 };
}

export function resetSerialQueue(queue: SerialQueue) {
    queue.generation += 1;
    queue.idle = true;
    queue.tail = Promise.resolve();
    queue.attempt = 0;
    queue.started = 0;
}

/** One shown value. A later call makes this attempt stale. */
export function claimSerialAttempt(queue: SerialQueue): number {
    queue.attempt += 1;
    return queue.attempt;
}

export function markSerialAttemptStarted(queue: SerialQueue, attempt: number) {
    if (queue.attempt === attempt) queue.started = attempt;
}

/** False while the newest shown value is still waiting to be sent. */
export function serialNewestHasStarted(queue: SerialQueue): boolean {
    return queue.started === queue.attempt;
}

function adoptJob(job: () => Promise<void> | void): Promise<void> {
    try {
        return Promise.resolve(job()).then(() => {}, () => {});
    } catch {
        return Promise.resolve();
    }
}

export function enqueueSerial(queue: SerialQueue, job: () => Promise<void> | void) {
    const generation = queue.generation;
    const mine = ++queue.ticket;
    const finish = () => {
        if (queue.generation !== generation) return;
        if (queue.ticket === mine) queue.idle = true;
    };
    if (!queue.idle) {
        queue.tail = queue.tail.then(() => adoptJob(job), () => adoptJob(job)).then(finish, finish);
        return;
    }
    queue.idle = false;
    let release: (result: Promise<void>) => void = () => {};
    const gate = new Promise<void>((resolve) => {
        release = resolve;
    });
    // Assign the tail before the job so a synchronous follow-up save waits on it.
    queue.tail = gate.then(finish, finish);
    release(adoptJob(job));
}

export const chatFontSizeSaveQueue = createSerialQueue();
export const chatFontFamilySaveQueue = createSerialQueue();
export const codingFontFamilySaveQueue = createSerialQueue();

export function resetAssistantFontSaveQueues() {
    resetSerialQueue(chatFontSizeSaveQueue);
    resetSerialQueue(chatFontFamilySaveQueue);
    resetSerialQueue(codingFontFamilySaveQueue);
}

/**
 * A config snapshot must not replace an unsaved slider draft with an older size.
 * The draft is the value the control has already shown and is still writing.
 * Values below 12 stay invalid so the caller can read GetChatFontSize.
 */
export function decideConfigChatFontSize(incoming: number, draft: number | null): ConfigChatFontSizeDecision {
    if (!Number.isFinite(incoming) || incoming < 12) return { action: "invalid" };
    const size = clampChatFontSize(incoming);
    if (acceptDraftedValue(size, draft) == null) return { action: "keep" };
    return { action: "apply", size };
}

export type ConfigFontFamilyDecision =
    | { action: "apply"; id: string }
    | { action: "keep" }
    | { action: "read" };

/**
 * A full config snapshot drops an empty family: the field is `omitempty`, and
 * the stored default is the empty string. Absence is that default, not a
 * partial update to ignore. A draft still wins. The screen already showing
 * system has nothing to move, so the common snapshot does not start a read.
 * Any other shown face must be re-read: a stale snapshot that simply lacks
 * the key must not force system over a newer id still stored on disk.
 */
export function decideConfigFontFamily(
    audience: AssistantFontAudience,
    incoming: unknown,
    draft: string | null,
    shownId: string,
): ConfigFontFamilyDecision {
    if (typeof incoming !== "string") {
        if (draft != null || shownId === ASSISTANT_FONT_SYSTEM) return { action: "keep" };
        return { action: "read" };
    }
    const id = normalizeAssistantFontId(audience, incoming);
    if (acceptDraftedValue(id, draft) == null) return { action: "keep" };
    return { action: "apply", id };
}

export type AssistantFontAudience = "conversation" | "coding";

export interface AssistantFontCopy {
    en: string;
    zhHans: string;
    zhHant: string;
}

export interface AssistantFontOption {
    id: string;
    label: AssistantFontCopy;
    description: AssistantFontCopy;
    /** Body text. System keeps the current assistant stack. */
    contentFamily: string;
    /** When set, code fences and the coding trail leave the default monospace. */
    codeFamily?: string;
}

const systemOption = (description: AssistantFontCopy): AssistantFontOption => ({
    id: ASSISTANT_FONT_SYSTEM,
    label: { en: "System", zhHans: "系统默认", zhHant: "系統預設" },
    description,
    contentFamily: ASSISTANT_SYSTEM_FONT,
});

const conversationSystem = systemOption({
    en: "The face the assistant uses today: Segoe UI with Microsoft YaHei or PingFang.",
    zhHans: "与现在的助手正文相同：西文 Segoe UI，中文微软雅黑或苹方。",
    zhHant: "與現在的助手正文相同：西文 Segoe UI，中文微軟雅黑或蘋方。",
});

const codingSystem = systemOption({
    en: "The current assistant font. Code stays on the system monospace.",
    zhHans: "现在的助手字体。代码块仍用系统等宽字体。",
    zhHant: "現在的助手字體。程式碼區塊仍用系統等寬字體。",
});

/** Reading faces for the conversation assistant. Default stays the current font. */
export const CONVERSATION_FONT_OPTIONS: readonly AssistantFontOption[] = [
    conversationSystem,
    {
        id: "maple-mono",
        label: { en: "Maple Mono", zhHans: "Maple Mono", zhHant: "Maple Mono" },
        description: {
            en: "Monospace. Chinese stays monospaced when Maple Mono CN is installed; otherwise it falls back to the system gothic.",
            zhHans: "等宽。已安装 Maple Mono CN 时中文也按等宽对齐，否则中文回退系统黑体。",
            zhHant: "等寬。已安裝 Maple Mono CN 時中文也按等寬對齊，否則中文回退系統黑體。",
        },
        contentFamily: MAPLE_MONO_STACK,
        codeFamily: MAPLE_MONO_STACK,
    },
    {
        id: "dengxian",
        label: { en: "DengXian", zhHans: "等线", zhHant: "等線" },
        description: {
            en: "A lighter gothic for long reading. Windows includes it; other systems fall back to PingFang or YaHei.",
            zhHans: "比微软雅黑更轻的黑体，适合长文。Windows 自带，其它系统回退苹方或雅黑。",
            zhHant: "比微軟雅黑更輕的黑體，適合長文。Windows 自帶，其它系統回退蘋方或雅黑。",
        },
        contentFamily: DENGXIAN_STACK,
    },
    {
        id: "sarasa-gothic",
        label: { en: "Sarasa Gothic", zhHans: "更纱黑体", zhHant: "更紗黑體" },
        description: {
            en: "A screen gothic for mixed Chinese and English. Falls back to DengXian when it is not installed.",
            zhHans: "适合中英混排的屏显黑体。未安装时使用等线。",
            zhHant: "適合中英混排的螢幕黑體。未安裝時使用等線。",
        },
        contentFamily: SARASA_GOTHIC_STACK,
    },
    {
        id: "lxgw-wenkai",
        label: { en: "LXGW WenKai", zhHans: "霞鹜文楷", zhHant: "霞鶩文楷" },
        description: {
            en: "A kai face for long reading. Falls back to the system kai font when WenKai is not installed.",
            zhHans: "适合长文的楷体。未安装霞鹜文楷时使用系统楷体。",
            zhHant: "適合長文的楷體。未安裝霞鶩文楷時使用系統楷體。",
        },
        contentFamily: WENKAI_STACK,
    },
];

/** Programming faces for the coding assistant. Default stays the current font. */
export const CODING_FONT_OPTIONS: readonly AssistantFontOption[] = [
    codingSystem,
    {
        id: "maple-mono",
        label: { en: "Maple Mono", zhHans: "Maple Mono", zhHant: "Maple Mono" },
        description: {
            en: "Monospace for code and Chinese comments. Chinese stays monospaced when Maple Mono CN is installed.",
            zhHans: "适合代码和中文注释的等宽字体。已安装 Maple Mono CN 时中文也等宽对齐。",
            zhHant: "適合程式碼和中文註解的等寬字體。已安裝 Maple Mono CN 時中文也等寬對齊。",
        },
        contentFamily: MAPLE_MONO_STACK,
        codeFamily: MAPLE_MONO_STACK,
    },
    {
        id: "jetbrains-mono",
        label: { en: "JetBrains Mono", zhHans: "JetBrains Mono", zhHant: "JetBrains Mono" },
        description: {
            en: "A coding face with a tall x-height. Chinese falls back to the system gothic.",
            zhHans: "字高偏大的编程字体。中文回退系统黑体。",
            zhHant: "字高偏大的編程字體。中文回退系統黑體。",
        },
        contentFamily: JETBRAINS_MONO_STACK,
        codeFamily: JETBRAINS_MONO_STACK,
    },
    {
        id: "cascadia-code",
        label: { en: "Cascadia Code", zhHans: "Cascadia Code", zhHant: "Cascadia Code" },
        description: {
            en: "Microsoft's coding font, with ligatures. Chinese falls back to the system gothic.",
            zhHans: "微软的编程字体，带连字。中文回退系统黑体。",
            zhHant: "微軟的編程字體，帶連字。中文回退系統黑體。",
        },
        contentFamily: CASCADIA_CODE_STACK,
        codeFamily: CASCADIA_CODE_STACK,
    },
    {
        id: "fira-code",
        label: { en: "Fira Code", zhHans: "Fira Code", zhHant: "Fira Code" },
        description: {
            en: "A coding font with ligatures. Chinese falls back to the system gothic.",
            zhHans: "带连字的编程字体。中文回退系统黑体。",
            zhHant: "帶連字的編程字體。中文回退系統黑體。",
        },
        contentFamily: FIRA_CODE_STACK,
        codeFamily: FIRA_CODE_STACK,
    },
    {
        id: "sarasa-mono",
        label: { en: "Sarasa Mono", zhHans: "更纱等宽", zhHant: "更紗等寬" },
        description: {
            en: "Monospace aligned for Chinese and code. Falls back to Cascadia Code when it is not installed.",
            zhHans: "中文与代码对齐的等宽字体。未安装时使用 Cascadia Code。",
            zhHant: "中文與程式碼對齊的等寬字體。未安裝時使用 Cascadia Code。",
        },
        contentFamily: SARASA_MONO_STACK,
        codeFamily: SARASA_MONO_STACK,
    },
];

export function assistantFontOptions(audience: AssistantFontAudience): readonly AssistantFontOption[] {
    return audience === "coding" ? CODING_FONT_OPTIONS : CONVERSATION_FONT_OPTIONS;
}

/** Unknown or cross-audience ids collapse to the current font. */
export function normalizeAssistantFontId(audience: AssistantFontAudience, fontId: string | null | undefined): string {
    const id = String(fontId ?? "").trim();
    if (assistantFontOptions(audience).some((option) => option.id === id)) return id;
    return ASSISTANT_FONT_SYSTEM;
}

export function resolveAssistantFont(audience: AssistantFontAudience, fontId: string | null | undefined): AssistantFontOption {
    const id = normalizeAssistantFontId(audience, fontId);
    return assistantFontOptions(audience).find((option) => option.id === id) ?? assistantFontOptions(audience)[0];
}

/**
 * Font style for the assistant transcript.
 * System leaves code on the default monospace stack.
 */
export function assistantMessageFontStyle(audience: AssistantFontAudience, fontId: string | null | undefined): CSSProperties {
    const option = resolveAssistantFont(audience, fontId);
    if (!option.codeFamily) {
        return { fontFamily: option.contentFamily };
    }
    return {
        fontFamily: option.contentFamily,
        ["--assistant-code-font" as string]: option.codeFamily,
    } as CSSProperties;
}

export const CONVERSATION_FONT_SAMPLE = "你好，MaClaw。Hello.";
/** `!=` is a calt ligature in every bundled programming face, and 你好 shows the CJK fallback. */
export const CODING_FONT_SAMPLE = "x != 0 你好";

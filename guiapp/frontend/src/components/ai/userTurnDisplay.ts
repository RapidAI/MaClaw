import { isBinaryDocumentAttachment } from "./attachmentClassification";

/**
 * Display-time view of one user turn.
 *
 * The desktop composer embeds picked files into the outgoing text as a marker
 * block, and the host appends its own notes (vision routing instruction, OCR
 * results, IM attachment descriptions) before the text reaches a model. All of
 * that is host-authored prompting: the user never typed it, so the transcript
 * must not show it. The file paths it carries are still the only record of what
 * was attached, so they are recovered here and handed back as attachments —
 * which is how a turn whose display path lost its attachment list (for example
 * the initial message delivered to a newly created task tab) still renders its
 * image previews.
 */
export interface UserTurnDisplayAttachment {
    filePath: string;
    fileName: string;
    extension: string;
    isImage: boolean;
}

export interface UserTurnDisplayView {
    /** User-authored text, with every host/prompt block removed. */
    text: string;
    /** Attachments carried by the turn, including ones recovered from the text. */
    attachments: UserTurnDisplayAttachment[];
}

/** Markers the desktop composer uses to embed picked file paths in the text. */
const FILE_PATH_PROMPT_MARKERS = [
    "[用户选择的本地文件路径]",
    "[之前选择的本地文件路径（仅供参考，非本次上传）]",
];

const IMAGE_FILE_EXTENSIONS = new Set([".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg"]);
const DOCUMENT_FILE_EXTENSIONS = new Set([
    ".pdf", ".doc", ".docx", ".xls", ".xlsx", ".csv", ".ppt", ".pptx", ".txt", ".md", ".markdown",
]);

const OCR_NOTE_BEGIN = "--- image_ocr: begin ---";
const OCR_NOTE_END = "--- image_ocr: end ---";
const AUTO_EXTRACT_BEGIN = "--- auto_extract: begin ";
const AUTO_EXTRACT_END = "--- auto_extract: end ";

function extnameOf(filePath: string): string {
    const normalized = filePath.trim().toLowerCase();
    if (!normalized) return "";
    const queryIndex = normalized.search(/[?#]/);
    const pathname = queryIndex >= 0 ? normalized.slice(0, queryIndex) : normalized;
    const slash = Math.max(pathname.lastIndexOf("/"), pathname.lastIndexOf("\\"));
    const base = slash >= 0 ? pathname.slice(slash + 1) : pathname;
    const dot = base.lastIndexOf(".");
    if (dot <= 0) return "";
    return base.slice(dot);
}

function baseNameOf(filePath: string): string {
    const trimmed = filePath.trim();
    return trimmed.split(/[\\/]/).filter(Boolean).pop() || trimmed;
}

/**
 * Path carried by one line of a file-picker block: "" when the line is not a
 * path. List-style entries keep a leading dash, and a file must carry an
 * extension so ordinary prose is never mistaken for a file reference.
 */
function lineAsSelectedPath(line: string): string {
    const trimmed = line.trim();
    if (!trimmed || trimmed.length > 4096) return "";
    const candidate = trimmed.replace(/^-\s*/, "");
    const extension = extnameOf(candidate);
    if (!extension) return "";
    if (DOCUMENT_FILE_EXTENSIONS.has(extension)) return candidate;
    const looksLikePath = /^[a-zA-Z]:[\\/]/.test(candidate)
        || candidate.startsWith("\\\\")
        || candidate.startsWith("/")
        || candidate.startsWith("~/")
        || candidate.startsWith("./")
        || candidate.startsWith("../")
        || /^[\w.\-]+([\\/][\w.\-]+)+$/.test(candidate);
    return looksLikePath ? candidate : "";
}

/** True when the transcript should draw this path as an image preview. */
export function isImageAttachmentPath(filePath: string): boolean {
    const trimmed = filePath.trim();
    if (!trimmed) return false;
    const fileName = baseNameOf(trimmed);
    // A binary document renamed like a picture must not render as one; the
    // shared classifier keeps PDF/Office out of image previews.
    return IMAGE_FILE_EXTENSIONS.has(extnameOf(trimmed)) && !isBinaryDocumentAttachment(fileName, "");
}

function attachmentFromFilePath(filePath: string): UserTurnDisplayAttachment {
    const trimmed = filePath.trim();
    return {
        filePath: trimmed,
        fileName: baseNameOf(trimmed),
        extension: extnameOf(trimmed),
        isImage: isImageAttachmentPath(trimmed),
    };
}

/** Host-owned note tokens. Anything from one of these on is machinery. */
const HOST_NOTE_START_TOKENS = [
    "[Host note:",
    "[用户发送了图片",
    "[之前发送的图片",
    "[之前上传了",
    "[附件:",
    "[之前的附件:",
    "[语音:",
    "[之前的语音:",
    "[系统已自动解析文档正文",
    "[之前已自动解析文档",
    "[图片的本地 OCR 文字内容",
    "For image files, the host sends them directly",
    "Analyze attached images first",
];

/** Lines the host writes for the model (routing notes, OCR, IM fallbacks). */
function isHostNoteLine(line: string): boolean {
    const trimmed = line.trim();
    if (!trimmed) return false;
    if (HOST_NOTE_START_TOKENS.some(token => trimmed.startsWith(token))) return true;
    return /^\[图片 .*的文字内容/.test(trimmed);
}

/**
 * Notes are normally on their own line, but a glued tail (`帮我看[Host note: …]`)
 * must not leak either: cut at the first host token and keep the prose prefix.
 */
function cutHostNoteTail(line: string): string {
    let current = line;
    for (;;) {
        let index = -1;
        for (const token of HOST_NOTE_START_TOKENS) {
            const at = current.indexOf(token);
            if (at > 0 && (index < 0 || at < index)) index = at;
        }
        const ocrAt = current.search(/\[图片 [^\]]*的文字内容/);
        if (ocrAt > 0 && (index < 0 || ocrAt < index)) index = ocrAt;
        if (index < 0) return current;
        current = current.slice(0, index);
    }
}

function stripHostInjectedBodies(text: string): string {
    if (!text.includes(OCR_NOTE_BEGIN) && !text.includes(AUTO_EXTRACT_BEGIN)) return text;
    const kept: string[] = [];
    let skipping = false;
    for (const line of text.split("\n")) {
        const trimmed = line.trim();
        // Prefix matches, not exact ones: a fence glued to other machinery on
        // one line must still open the body skip.
        if (!skipping && trimmed.startsWith(OCR_NOTE_BEGIN)) {
            skipping = true;
            continue;
        }
        if (skipping && trimmed.startsWith(OCR_NOTE_END)) {
            skipping = false;
            continue;
        }
        if (!skipping && trimmed.startsWith(AUTO_EXTRACT_BEGIN)) {
            skipping = true;
            continue;
        }
        if (skipping && trimmed.startsWith(AUTO_EXTRACT_END)) {
            skipping = false;
            continue;
        }
        if (skipping) continue;
        kept.push(line);
    }
    return kept.join("\n");
}

/** Paths listed right after the marker line, stopping at the first foreign line. */
function collectBlockPaths(afterMarker: string): string[] {
    const paths: string[] = [];
    const seen = new Set<string>();
    for (const line of afterMarker.split("\n")) {
        const trimmed = line.trim();
        if (!trimmed) {
            if (paths.length > 0) break;
            continue;
        }
        if (isHostNoteLine(line)) break;
        const path = lineAsSelectedPath(line);
        if (!path) {
            if (paths.length > 0) break;
            continue;
        }
        const key = path.toLowerCase();
        if (seen.has(key)) continue;
        seen.add(key);
        paths.push(path);
    }
    return paths;
}

function locateFilePathPromptBlock(text: string): { index: number; paths: string[] } {
    let index = -1;
    let markerLength = 0;
    for (const marker of FILE_PATH_PROMPT_MARKERS) {
        const at = text.indexOf(marker);
        if (at >= 0 && (index < 0 || at < index)) {
            index = at;
            markerLength = marker.length;
        }
    }
    if (index < 0) return { index: -1, paths: [] };
    const newline = text.indexOf("\n", index + markerLength);
    const body = newline >= 0 ? text.slice(newline + 1) : "";
    return { index, paths: collectBlockPaths(body) };
}

function selectedFilePathsInCleanText(text: string): string[] {
    return locateFilePathPromptBlock(text).paths;
}

/** File paths carried by the file-picker marker block inside `text`. */
export function userSelectedFilePathsInText(text: string): string[] {
    const raw = String(text || "");
    if (!raw.trim()) return [];
    return selectedFilePathsInCleanText(stripHostInjectedBodies(raw));
}

/** Marker-block and host-note removal over text whose bodies are already stripped. */
function stripUserTurnPromptArtifactsClean(text: string): string {
    let rest = text;
    let stripped = false;
    // The composer always appends the file-picker block after the user's prose,
    // so everything from the marker on is machinery: paths plus routing notes.
    for (;;) {
        const block = locateFilePathPromptBlock(rest);
        if (block.index < 0) break;
        rest = rest.slice(0, block.index);
        stripped = true;
    }
    const lines = rest.split("\n");
    const cut = lines.map(cutHostNoteTail);
    const kept = cut.filter(line => !isHostNoteLine(line));
    // A cut shortens a line without removing it, so compare content — counting
    // lines alone would discard the cuts and return the original text.
    if (kept.length !== lines.length || cut.some((line, index) => line !== lines[index])) stripped = true;
    // Untouched prose must round-trip byte-for-byte (blank-line runs included);
    // only removal of injected lines may leave gaps to close up.
    if (!stripped) return rest.trim();
    return kept.join("\n").replace(/\n{3,}/g, "\n\n").trim();
}

/**
 * Remove host/prompt blocks from a user turn so the transcript shows only what
 * the user wrote. Safe on already-clean text: unchanged input comes back out.
 */
export function stripUserTurnPromptArtifacts(text: string): string {
    const raw = String(text || "");
    if (!raw.trim()) return raw.trim();
    return stripUserTurnPromptArtifactsClean(stripHostInjectedBodies(raw));
}

/**
 * Send options that keep a displayed user turn free of host prompt text.
 * Initial messages reach the transcript through task-creation payloads that
 * carry only the outgoing text, so the first bubble of a new task tab would
 * otherwise show the file-picker block and the vision routing instruction.
 */
export function userTurnDisplayOptions(text: string): { displayText: string; displayAttachments?: UserTurnDisplayAttachment[] } {
    const view = userTurnDisplayView(text);
    if (view.attachments.length === 0) return { displayText: view.text };
    return { displayText: view.text, displayAttachments: view.attachments };
}

/**
 * Build the transcript view of a user turn: clean text plus the attachments it
 * carries. Attachments already on the message win; paths recovered from injected
 * text only fill in what is missing.
 */
export function userTurnDisplayView(text: string, existingAttachments?: readonly UserTurnDisplayAttachment[]): UserTurnDisplayView {
    const raw = String(text || "");
    // Strip OCR/auto-extract bodies once; both the path recovery and the text
    // cleanup below work on this single cleaned copy.
    const cleaned = raw.trim() ? stripHostInjectedBodies(raw) : raw;
    const attachments: UserTurnDisplayAttachment[] = [];
    const seen = new Set<string>();
    const push = (attachment: UserTurnDisplayAttachment | undefined) => {
        if (!attachment || !attachment.filePath) return;
        const key = attachment.filePath.toLowerCase();
        if (seen.has(key)) return;
        seen.add(key);
        attachments.push(attachment);
    };
    for (const attachment of existingAttachments || []) push(attachment);
    for (const filePath of selectedFilePathsInCleanText(cleaned)) push(attachmentFromFilePath(filePath));
    return { text: stripUserTurnPromptArtifactsClean(cleaned), attachments };
}

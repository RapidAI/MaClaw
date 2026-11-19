import { isHostGoalChecklistText } from "./aiAssistantPanelSessionUtils";
import { userTurnDisplayView, type UserTurnDisplayAttachment } from "./userTurnDisplay";

/** One user command the chat rail can jump to. */
export interface ChatSessionLocatorTurn {
    id: string;
    /** Single line. The rail clips whatever still overflows. */
    label: string;
}

export interface ChatSessionLocatorMessage {
    id?: string;
    role?: string;
    content?: string;
    hostInitiated?: boolean;
    kind?: string;
    attachments?: ReadonlyArray<{ fileName?: string; filePath?: string; extension?: string; isImage?: boolean }>;
}

const LOCATOR_SCROLL_GAP_PX = 12;

/** Collapse a user command onto one line. The rail ellipsizes whatever still overflows. */
export function chatSessionLocatorLabel(text: string): string {
    return text.replace(/\s+/g, " ").trim();
}

export function locatorCssEscape(value: string): string {
    if (typeof CSS !== "undefined" && typeof CSS.escape === "function") return CSS.escape(value);
    return value.replace(/[^a-zA-Z0-9_-]/g, (char) => `\\${char}`);
}

/** Scroll offset that puts the user row `gap` pixels under the transcript top. */
export function userTurnScrollTop(nodeTop: number, containerTop: number, containerScrollTop: number, gap = LOCATOR_SCROLL_GAP_PX): number {
    return Math.max(0, nodeTop - containerTop + containerScrollTop - gap);
}

/**
 * The open list scrolls itself. A wheel past either end, and any wheel on the
 * closed ticks, belongs to the transcript underneath.
 */
export function locatorWheelTargetsTranscript(open: boolean, scrollHeight: number, clientHeight: number, scrollTop: number, deltaY: number): boolean {
    if (!deltaY) return false;
    const canScroll = open && scrollHeight > clientHeight + 1;
    if (!canScroll) return true;
    if (deltaY < 0 && scrollTop > 0) return false;
    if (deltaY > 0 && scrollHeight - scrollTop - clientHeight > 1) return false;
    return true;
}

function attachmentFileName(attachment: { fileName?: string; filePath?: string }): string {
    const named = String(attachment.fileName || "").trim();
    if (named) return named;
    const path = String(attachment.filePath || "").trim();
    const cut = Math.max(path.lastIndexOf("/"), path.lastIndexOf("\\"));
    return cut >= 0 ? path.slice(cut + 1) : path;
}

function locatorUserId(message: ChatSessionLocatorMessage): string {
    return String(message?.id || "").trim();
}

/** Fields that change the rail line. Assistant body text is not one of them. */
function sameLocatorUser(prev: ChatSessionLocatorMessage, next: ChatSessionLocatorMessage): boolean {
    if (prev === next) return true;
    if (locatorUserId(prev) !== locatorUserId(next)) return false;
    if (!!prev.hostInitiated !== !!next.hostInitiated) return false;
    if ((prev.kind || "") !== (next.kind || "")) return false;
    if ((prev.content || "") !== (next.content || "")) return false;
    const left = prev.attachments || [];
    const right = next.attachments || [];
    if (left.length !== right.length) return false;
    for (let index = 0; index < left.length; index += 1) {
        if ((left[index]?.fileName || "") !== (right[index]?.fileName || "")) return false;
        if ((left[index]?.filePath || "") !== (right[index]?.filePath || "")) return false;
    }
    return true;
}

function takeLocatorUsers<T extends ChatSessionLocatorMessage>(groups: readonly (readonly T[])[]): T[] {
    const seen = new Set<string>();
    const out: T[] = [];
    for (const group of groups) {
        for (const message of group) {
            const id = locatorUserId(message);
            if (!id || message?.role !== "user" || seen.has(id)) continue;
            seen.add(id);
            out.push(message);
        }
    }
    return out;
}

/**
 * User rows for the session locator, oldest first, first id wins.
 * A streaming reply rebuilds the transcript arrays without changing these
 * rows. Returning the previous array keeps the rail from re-labeling that flush.
 */
export function retainLocatorUserMessages<T extends ChatSessionLocatorMessage>(
    cached: readonly T[],
    groups: readonly (readonly T[])[],
): readonly T[] {
    const seen = new Set<string>();
    let index = 0;
    for (const group of groups) {
        for (const message of group) {
            const id = locatorUserId(message);
            if (!id || message?.role !== "user" || seen.has(id)) continue;
            seen.add(id);
            const prev = cached[index];
            if (!prev || !sameLocatorUser(prev, message)) return takeLocatorUsers(groups);
            index += 1;
        }
    }
    if (index !== cached.length) return takeLocatorUsers(groups);
    return cached;
}

function locatorAttachments(message: ChatSessionLocatorMessage): UserTurnDisplayAttachment[] | undefined {
    const out: UserTurnDisplayAttachment[] = [];
    for (const attachment of message.attachments || []) {
        const filePath = String(attachment?.filePath || "").trim();
        if (!filePath) continue;
        out.push({
            filePath,
            fileName: String(attachment?.fileName || "").trim(),
            extension: String(attachment?.extension || ""),
            isImage: !!attachment?.isImage,
        });
    }
    return out.length > 0 ? out : undefined;
}

/**
 * User commands in conversation order.
 * Host continuations and the goal checklist are engine rows, not something the person typed.
 * An attachment-only send uses the file name so the turn still has a line to click.
 */
export function chatSessionLocatorTurns(messages: readonly ChatSessionLocatorMessage[]): ChatSessionLocatorTurn[] {
    const turns: ChatSessionLocatorTurn[] = [];
    const seen = new Set<string>();
    for (const message of messages) {
        const id = String(message?.id || "").trim();
        if (!id || seen.has(id) || message?.role !== "user" || message.hostInitiated) continue;
        if (message.kind === "goalChecklist" || isHostGoalChecklistText(message.content || "")) continue;
        const view = userTurnDisplayView(message.content || "", locatorAttachments(message));
        const typed = chatSessionLocatorLabel(view.text);
        const files = view.attachments.map(attachmentFileName).filter(Boolean);
        const label = typed || files.join("、");
        if (!label) continue;
        seen.add(id);
        turns.push({ id, label });
    }
    return turns;
}

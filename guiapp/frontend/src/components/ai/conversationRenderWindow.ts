// Screen-based render window for the live transcript. The message data stays
// in memory (the archive window already trims it); this module only decides
// which slice still owns DOM nodes. Older rows are represented by one spacer
// element, so scrollbar geometry survives the swap.
//
// Rules:
// - pinned at the bottom: mount only the viewport plus two screens above it;
// - scrolled up: mount three more screens per batch, aligned to a user turn so
//   a read never starts mid-turn;
// - the tail (live reply, pending cards) always stays mounted.

/** Screens of already-read transcript kept mounted above the viewport. */
export const CONVERSATION_RENDER_ABOVE_SCREENS = 2;
/** Screens of older rows re-mounted per scroll-up batch. */
export const CONVERSATION_RENDER_LOAD_SCREENS = 3;
/** Below this viewport height the window logic stands down (hidden pane, jsdom). */
export const CONVERSATION_RENDER_MIN_VIEWPORT_PX = 80;
/** Floor for the measured row height so a degenerate layout cannot divide by ~0. */
const CONVERSATION_RENDER_MIN_ROW_PX = 24;
/** Never unmount the transcript down to a couple of rows. */
export const CONVERSATION_RENDER_MIN_MOUNTED_ROWS = 6;
/**
 * The transcript body sitting this far below the scrollport top means the
 * reader is inside the spacer itself (a scrollbar drag past everything that is
 * mounted). Loading in batches would show blank space, so everything left is
 * mounted at once.
 */
export const CONVERSATION_RENDER_BLANK_PX = 40;

export interface ConversationRenderWindowMessage {
    id: string;
    role?: string;
}

export interface ConversationRenderWindowParams {
    messages: ConversationRenderWindowMessage[];
    windowStart: number;
    viewportPx: number;
    /** Height of the mounted rows (container.scrollHeight minus the spacer). */
    contentPx: number;
}

function conversationRenderRole(message: ConversationRenderWindowMessage | undefined): string {
    return String(message?.role || "").trim().toLowerCase();
}

export function conversationRenderCanWindow(viewportPx: number): boolean {
    return Number.isFinite(viewportPx) && viewportPx >= CONVERSATION_RENDER_MIN_VIEWPORT_PX;
}

/** Average mounted row height, floored so a zero-height layout cannot explode the batch. */
export function conversationRenderRowHeightPx(contentPx: number, rows: number): number {
    if (!Number.isFinite(contentPx) || rows <= 0) return CONVERSATION_RENDER_MIN_ROW_PX;
    const average = contentPx / rows;
    return average > CONVERSATION_RENDER_MIN_ROW_PX ? average : CONVERSATION_RENDER_MIN_ROW_PX;
}

/**
 * Index of the user row that opens the turn containing `index`. Returns 0 when
 * the transcript starts mid-turn (the hot window always cuts on a user row, so
 * this is the "load everything" boundary).
 */
export function conversationRenderTurnStart(messages: ConversationRenderWindowMessage[], index: number): number {
    if (index <= 0 || messages.length === 0) return 0;
    const last = messages.length - 1;
    for (let i = Math.min(index, last); i > 0; i -= 1) {
        if (conversationRenderRole(messages[i]) === "user") return i;
    }
    return 0;
}

/**
 * First row that should stay mounted when the reader is pinned at the bottom,
 * or null when the mounted transcript is already within the DOM budget.
 */
export function conversationRenderTrimStart(params: ConversationRenderWindowParams): number | null {
    const { messages, windowStart, viewportPx, contentPx } = params;
    const total = messages.length;
    const mounted = total - windowStart;
    if (!conversationRenderCanWindow(viewportPx) || mounted <= CONVERSATION_RENDER_MIN_MOUNTED_ROWS) return null;
    const budgetPx = viewportPx * (1 + CONVERSATION_RENDER_ABOVE_SCREENS);
    // One screen of slack keeps a fresh trim from re-firing on the next token.
    if (contentPx <= budgetPx + viewportPx) return null;
    const rowPx = conversationRenderRowHeightPx(contentPx, mounted);
    const keepRows = Math.max(
        1,
        Math.min(mounted - CONVERSATION_RENDER_MIN_MOUNTED_ROWS, Math.floor(budgetPx / rowPx)),
    );
    const candidate = Math.max(windowStart + 1, total - keepRows);
    if (candidate >= total) return null;
    const next = conversationRenderTurnStart(messages, candidate);
    if (next <= windowStart) return null;
    return next;
}

/**
 * First row that should be mounted after a scroll-up batch, or null when the
 * whole live transcript is already mounted.
 */
export function conversationRenderLoadStart(params: ConversationRenderWindowParams): number | null {
    const { messages, windowStart, viewportPx, contentPx } = params;
    const total = messages.length;
    const mounted = total - windowStart;
    if (!conversationRenderCanWindow(viewportPx) || windowStart <= 0 || mounted <= 0) return null;
    const rowPx = conversationRenderRowHeightPx(contentPx, mounted);
    const loadRows = Math.max(1, Math.ceil((viewportPx * CONVERSATION_RENDER_LOAD_SCREENS) / rowPx));
    const candidate = Math.max(0, windowStart - loadRows);
    if (candidate >= windowStart) return null;
    const next = conversationRenderTurnStart(messages, candidate);
    if (next >= windowStart) return null;
    return next;
}

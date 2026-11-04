// resizeCursorForPointer is the frameless edge cursor, or "" when the pointer
// is not on an edge. "" is also returned when the reported window size cannot
// be trusted. A maximised or fullscreen WebView2 window often leaves
// outerWidth/outerHeight at the restored size, or at 0, while the page is
// already the full monitor. The edge test then matches the whole window and
// sets a sizing cursor. Windows does not draw sizing cursors on a maximised
// window, so the pointer disappears until the window is restored.

const RESIZE_CURSORS = {
    se: "se-resize",
    sw: "sw-resize",
    nw: "nw-resize",
    ne: "ne-resize",
    w: "w-resize",
    n: "n-resize",
    s: "s-resize",
    e: "e-resize",
};

// Gaps within this many CSS pixels are rounding. A maximised window that
// still reports the restored outer size is short by a large part of the monitor.
const RESIZE_METRIC_SLACK = 32;

export function isResizeCursor(cursor) {
    return typeof cursor === "string" && /resize$/.test(cursor);
}

export function resizeCursorForPointer(outerWidth, outerHeight, innerWidth, innerHeight, x, y, border) {
    if (!(outerWidth > 0) || !(outerHeight > 0)) return "";
    if (innerWidth - outerWidth > RESIZE_METRIC_SLACK || innerHeight - outerHeight > RESIZE_METRIC_SLACK) return "";
    // The pointer is already outside the reported window. Those metrics belong
    // to the restored size, not the maximised page.
    if (x > outerWidth || y > outerHeight) return "";

    const right = outerWidth - x < border;
    const left = x < border;
    const top = y < border;
    const bottom = outerHeight - y < border;
    if (right && bottom) return RESIZE_CURSORS.se;
    if (left && bottom) return RESIZE_CURSORS.sw;
    if (left && top) return RESIZE_CURSORS.nw;
    if (top && right) return RESIZE_CURSORS.ne;
    if (left) return RESIZE_CURSORS.w;
    if (top) return RESIZE_CURSORS.n;
    if (bottom) return RESIZE_CURSORS.s;
    if (right) return RESIZE_CURSORS.e;
    return "";
}

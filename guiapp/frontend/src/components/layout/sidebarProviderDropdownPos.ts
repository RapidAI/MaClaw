/** Viewport padding so the fixed provider menu never kisses the window edge. */
export const DROPDOWN_EDGE_PAD = 8;
export const DROPDOWN_GAP = 6;
/**
 * Estimated max width before the menu is measured.
 * Keep in sync with `.sidebar-system-status__provider-dropdown { max-width: … }` in App.css.
 */
export const DROPDOWN_EST_MAX_WIDTH = 280;
/** Prefer opening above when at least this much free space exists above the anchor. */
export const DROPDOWN_MIN_ABOVE = 120;

export type ProviderDropdownPos = {
    left: number;
    /** CSS `top` in px, or null when opening upward (use `bottom` instead). */
    top: number | null;
    /** CSS `bottom` in px, or null when opening downward (use `top` instead). */
    bottom: number | null;
    maxHeight: number;
};

export type AnchorRect = Pick<DOMRect, 'left' | 'top' | 'bottom' | 'right' | 'width' | 'height'>;

const roundPx = (n: number) => Math.round(n);

/** Clamp a left offset so a menu of `menuWidth` stays inside the viewport. */
export function clampDropdownLeft(
    left: number,
    menuWidth: number,
    viewportWidth: number,
    edgePad: number = DROPDOWN_EDGE_PAD,
): number {
    const width = Math.max(0, menuWidth);
    let next = left;
    if (next + width > viewportWidth - edgePad) {
        next = Math.max(edgePad, viewportWidth - edgePad - width);
    }
    if (next < edgePad) next = edgePad;
    return roundPx(next);
}

/**
 * Place the provider menu relative to an anchor rect.
 * Prefer opening above (status bar sits at the bottom); fall back below when needed.
 * Left-aligns with the anchor and clamps using `menuWidth` (or an estimate).
 */
export function computeProviderDropdownPos(
    anchor: AnchorRect,
    opts: {
        viewportWidth: number;
        viewportHeight: number;
        /** Measured menu width; when omitted, DROPDOWN_EST_MAX_WIDTH is used for horizontal clamp. */
        menuWidth?: number;
        edgePad?: number;
        gap?: number;
        minAbove?: number;
    },
): ProviderDropdownPos {
    const edgePad = opts.edgePad ?? DROPDOWN_EDGE_PAD;
    const gap = opts.gap ?? DROPDOWN_GAP;
    const minAbove = opts.minAbove ?? DROPDOWN_MIN_ABOVE;
    const vw = opts.viewportWidth;
    const vh = opts.viewportHeight;
    const menuWidth = opts.menuWidth ?? DROPDOWN_EST_MAX_WIDTH;

    const left = clampDropdownLeft(anchor.left, menuWidth, vw, edgePad);

    const spaceAbove = anchor.top - edgePad;
    const spaceBelow = vh - anchor.bottom - edgePad;
    // Never open "above" when there is no real free strip (avoids 0-height / off-screen menus).
    const preferAbove = spaceAbove > 0 && (spaceAbove >= minAbove || spaceAbove >= spaceBelow);

    if (preferAbove) {
        return {
            left,
            top: null,
            bottom: roundPx(vh - anchor.top + gap),
            // Never claim more height than the free viewport strip (avoids top-edge clip).
            maxHeight: Math.max(0, roundPx(spaceAbove - gap)),
        };
    }
    return {
        left,
        top: roundPx(anchor.bottom + gap),
        bottom: null,
        maxHeight: Math.max(0, roundPx(spaceBelow - gap)),
    };
}

export function providerDropdownPosEqual(
    a: ProviderDropdownPos | null | undefined,
    b: ProviderDropdownPos | null | undefined,
): boolean {
    if (a === b) return true;
    if (!a || !b) return false;
    return (
        a.left === b.left
        && a.bottom === b.bottom
        && a.top === b.top
        && a.maxHeight === b.maxHeight
    );
}

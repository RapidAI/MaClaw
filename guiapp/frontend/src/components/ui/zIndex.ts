/**
 * Overlay stacking ladder for TSX inline styles. Numeric mirror of the
 * --z-* tokens in styles/partials/00-tokens.css:
 *
 *   --z-surface-popover: 1000   (Z.surfacePopover)
 *   --z-floating-panel:  4000   (Z.floatingPanel)
 *   --z-overlay:         9999   (Z.overlay)
 *   --z-sticky-toast:   10000   (Z.stickyToast)
 *
 * Pick the lowest rung that works and stay below the two app-wide anchors:
 * CustomDialog (DIALOG_Z_INDEX = 120000, see CustomDialog.tsx) and the
 * expert-market overlay (119000, see 40-pets-expert.css). Values are kept
 * numeric (not var()) so tests can assert exact stacking. A few one-off
 * surfaces still sit between the rungs (40000, 99999) — migrate them onto
 * the ladder when they are next touched.
 */
export const Z = {
    surfacePopover: 1000,
    floatingPanel: 4000,
    overlay: 9999,
    stickyToast: 10000,
} as const;

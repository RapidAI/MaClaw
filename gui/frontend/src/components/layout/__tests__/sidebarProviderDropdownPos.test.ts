import { describe, expect, it } from 'vitest';
import {
    DROPDOWN_EDGE_PAD,
    DROPDOWN_EST_MAX_WIDTH,
    DROPDOWN_GAP,
    clampDropdownLeft,
    computeProviderDropdownPos,
    providerDropdownPosEqual,
} from '../sidebarProviderDropdownPos';

describe('clampDropdownLeft', () => {
    it('keeps left when the menu fits', () => {
        expect(clampDropdownLeft(100, 200, 800)).toBe(100);
    });

    it('shifts left when the menu would overflow the right edge', () => {
        // 700 + 200 = 900 > 800 - 8
        expect(clampDropdownLeft(700, 200, 800)).toBe(800 - DROPDOWN_EDGE_PAD - 200);
    });

    it('never goes past the left edge pad', () => {
        expect(clampDropdownLeft(-40, 200, 800)).toBe(DROPDOWN_EDGE_PAD);
        // Even when the menu is wider than the viewport, stay at the pad.
        expect(clampDropdownLeft(0, 1000, 400)).toBe(DROPDOWN_EDGE_PAD);
    });

    it('returns integer pixels', () => {
        expect(Number.isInteger(clampDropdownLeft(100.6, 200, 800))).toBe(true);
    });
});

describe('computeProviderDropdownPos', () => {
    const tallViewport = { viewportWidth: 1280, viewportHeight: 800 };

    it('left-aligns with the anchor and opens above when space allows', () => {
        // Anchor near bottom-left of a sidebar status bar.
        const anchor = { left: 72, top: 740, bottom: 768, right: 280, width: 208, height: 28 };
        const pos = computeProviderDropdownPos(anchor, tallViewport);

        expect(pos.left).toBe(72);
        expect(pos.bottom).toBe(800 - 740 + DROPDOWN_GAP);
        expect(pos.top).toBeNull();
        // maxHeight must not exceed free space above the anchor.
        expect(pos.maxHeight).toBe(740 - DROPDOWN_EDGE_PAD - DROPDOWN_GAP);
        expect(pos.maxHeight).toBeGreaterThan(100);
    });

    it('does not set maxHeight larger than the free viewport strip', () => {
        // Tiny space above and below — still must not claim more height than available.
        const anchor = { left: 40, top: 30, bottom: 50, right: 120, width: 80, height: 20 };
        const pos = computeProviderDropdownPos(anchor, {
            viewportWidth: 400,
            viewportHeight: 80,
        });
        expect(pos.maxHeight).toBeGreaterThanOrEqual(0);
        expect(pos.maxHeight).toBeLessThanOrEqual(80);
        if (pos.bottom != null) {
            expect(pos.maxHeight).toBeLessThanOrEqual(30 - DROPDOWN_EDGE_PAD - DROPDOWN_GAP + 0.001);
        }
    });

    it('opens below when there is no free space above the anchor', () => {
        const anchor = { left: 40, top: 4, bottom: 28, right: 120, width: 80, height: 24 };
        const pos = computeProviderDropdownPos(anchor, {
            viewportWidth: 800,
            viewportHeight: 600,
        });
        // spaceAbove = 4 - 8 = -4 → must not prefer above
        expect(pos.top).not.toBeNull();
        expect(pos.bottom).toBeNull();
    });

    it('does not use right-alignment that would push a left-side menu off-screen', () => {
        // Regression: old code used `right: vw - anchor.right`, which for a left-side
        // chevron made the menu grow leftward and clip against the window edge.
        const anchor = { left: 20, top: 740, bottom: 768, right: 36, width: 16, height: 28 };
        const pos = computeProviderDropdownPos(anchor, {
            ...tallViewport,
            menuWidth: DROPDOWN_EST_MAX_WIDTH,
        });

        expect(pos.left).toBeGreaterThanOrEqual(DROPDOWN_EDGE_PAD);
        expect(pos.left + DROPDOWN_EST_MAX_WIDTH).toBeLessThanOrEqual(1280 - DROPDOWN_EDGE_PAD + 0.001);
    });

    it('opens below when there is more space under the anchor', () => {
        const anchor = { left: 80, top: 20, bottom: 48, right: 200, width: 120, height: 28 };
        const pos = computeProviderDropdownPos(anchor, {
            viewportWidth: 1280,
            viewportHeight: 800,
        });

        expect(pos.top).toBe(48 + DROPDOWN_GAP);
        expect(pos.bottom).toBeNull();
    });

    it('clamps left using measured menu width', () => {
        const anchor = { left: 1100, top: 700, bottom: 730, right: 1200, width: 100, height: 30 };
        const pos = computeProviderDropdownPos(anchor, {
            viewportWidth: 1280,
            viewportHeight: 800,
            menuWidth: 240,
        });
        expect(pos.left).toBe(1280 - DROPDOWN_EDGE_PAD - 240);
    });

    it('uses the CSS-aligned estimate width when menuWidth is omitted', () => {
        expect(DROPDOWN_EST_MAX_WIDTH).toBe(280);
        const anchor = { left: 1100, top: 700, bottom: 730, right: 1200, width: 100, height: 30 };
        const pos = computeProviderDropdownPos(anchor, tallViewport);
        expect(pos.left).toBe(1280 - DROPDOWN_EDGE_PAD - DROPDOWN_EST_MAX_WIDTH);
    });

    it('rounds fractional geometry to integer CSS pixels', () => {
        const anchor = { left: 72.4, top: 740.6, bottom: 768.2, right: 280.1, width: 207.7, height: 27.6 };
        const pos = computeProviderDropdownPos(anchor, tallViewport);
        expect(Number.isInteger(pos.left)).toBe(true);
        expect(pos.bottom == null || Number.isInteger(pos.bottom)).toBe(true);
        expect(Number.isInteger(pos.maxHeight)).toBe(true);
    });
});

describe('providerDropdownPosEqual', () => {
    it('compares geometric fields including null edges', () => {
        const a = { left: 1, top: null, bottom: 2, maxHeight: 3 };
        expect(providerDropdownPosEqual(a, { ...a })).toBe(true);
        expect(providerDropdownPosEqual(a, { ...a, left: 9 })).toBe(false);
        expect(providerDropdownPosEqual(a, { left: 1, top: 4, bottom: null, maxHeight: 3 })).toBe(false);
        expect(providerDropdownPosEqual(null, a)).toBe(false);
        expect(providerDropdownPosEqual(null, null)).toBe(true);
    });
});

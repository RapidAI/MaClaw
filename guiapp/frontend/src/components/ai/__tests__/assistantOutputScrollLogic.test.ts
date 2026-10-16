import { describe, expect, it } from "vitest";
import {
    pinNestedScrollToBottom,
    shouldIgnoreNestedScrollIntent,
    tryPinNestedScroll,
} from "../assistantOutputScrollLogic";

describe("shouldIgnoreNestedScrollIntent", () => {
    it("ignores downward wheels and text clicks", () => {
        const scroller = {} as EventTarget;
        const child = {} as EventTarget;
        expect(shouldIgnoreNestedScrollIntent({ type: "wheel", deltaY: 40 })).toBe(true);
        expect(shouldIgnoreNestedScrollIntent({ type: "pointerdown", currentTarget: scroller, target: child })).toBe(true);
    });

    it("treats upward wheels and scrollbar pointerdowns as leave-intent", () => {
        const scroller = {} as EventTarget;
        expect(shouldIgnoreNestedScrollIntent({ type: "wheel", deltaY: -40 })).toBe(false);
        expect(shouldIgnoreNestedScrollIntent({ type: "pointerdown", currentTarget: scroller, target: scroller })).toBe(false);
        expect(shouldIgnoreNestedScrollIntent({ type: "touchmove" })).toBe(false);
    });
});

describe("pinNestedScrollToBottom", () => {
    it("pins only while follow is active", () => {
        const el = { scrollTop: 0, scrollHeight: 480 } as HTMLElement;
        pinNestedScrollToBottom(el, false);
        expect(el.scrollTop).toBe(0);
        pinNestedScrollToBottom(el, true);
        expect(el.scrollTop).toBe(480);
        pinNestedScrollToBottom(null, true);
    });
});

describe("tryPinNestedScroll", () => {
    function pane(scrollTop: number): HTMLElement {
        return { scrollTop, scrollHeight: 720, clientHeight: 400 } as HTMLElement;
    }

    it("skips when the pane is missing or already unpinned", () => {
        expect(tryPinNestedScroll(null, true, false)).toBe("skipped");
        const el = pane(0);
        expect(tryPinNestedScroll(el, false, false)).toBe("skipped");
        expect(el.scrollTop).toBe(0);
    });

    it("abandons when the user has already moved away", () => {
        const el = pane(0);
        expect(tryPinNestedScroll(el, true, true)).toBe("abandoned");
        expect(el.scrollTop).toBe(0);
    });

    it("pins when follow is still active", () => {
        const el = pane(80);
        expect(tryPinNestedScroll(el, true, false)).toBe("pinned");
        expect(el.scrollTop).toBe(720);
    });

    it("still pins when intent is set but the pane remains near the tail", () => {
        const el = pane(296);
        expect(tryPinNestedScroll(el, true, true)).toBe("pinned");
        expect(el.scrollTop).toBe(720);
    });
});

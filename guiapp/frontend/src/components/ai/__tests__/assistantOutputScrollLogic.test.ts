import { describe, expect, it } from "vitest";
import {
    pinAssistantOutputToBottom,
    pinNestedScrollToBottom,
    shouldIgnoreNestedScrollIntent,
    shouldIgnoreUserScrollIntent,
    tryPinNestedScroll,
} from "../assistantOutputScrollLogic";

describe("shouldIgnoreNestedScrollIntent", () => {
    it("ignores downward wheels and text clicks", () => {
        const scroller = {} as EventTarget;
        const child = {} as EventTarget;
        expect(shouldIgnoreNestedScrollIntent({ type: "wheel", deltaY: 40 })).toBe(true);
        expect(shouldIgnoreNestedScrollIntent({ type: "pointerdown", currentTarget: scroller, target: child })).toBe(true);
    });

    it("ignores an upward wheel inside an earlier-messages dialog", () => {
        const dialog = document.createElement("div");
        dialog.setAttribute("data-scroll-intent-boundary", "");
        const row = document.createElement("div");
        dialog.appendChild(row);
        document.body.appendChild(dialog);
        expect(shouldIgnoreUserScrollIntent({ type: "wheel", deltaY: -40, target: row })).toBe(true);
        dialog.remove();
    });

    it("treats upward wheels and scrollbar pointerdowns as leave-intent", () => {
        const scroller = {} as EventTarget;
        expect(shouldIgnoreNestedScrollIntent({ type: "wheel", deltaY: -40 })).toBe(false);
        expect(shouldIgnoreNestedScrollIntent({ type: "pointerdown", currentTarget: scroller, target: scroller })).toBe(false);
        expect(shouldIgnoreNestedScrollIntent({ type: "touchmove" })).toBe(false);
    });

    it("counts an upward wheel on a nested pane that is already at its top", () => {
        const pane = document.createElement("div");
        pane.setAttribute("data-nested-scroll", "");
        Object.defineProperty(pane, "scrollTop", { configurable: true, value: 0 });
        const row = document.createElement("div");
        pane.appendChild(row);
        document.body.appendChild(pane);
        // The gesture chains to the transcript: it is a real leave-the-tail
        // intent, so the latch must be able to catch it.
        expect(shouldIgnoreUserScrollIntent({ type: "wheel", deltaY: -40, target: row })).toBe(false);
        pane.remove();
    });

    it("still ignores wheels a nested pane can consume itself", () => {
        const pane = document.createElement("div");
        pane.setAttribute("data-nested-scroll", "");
        Object.defineProperty(pane, "scrollTop", { configurable: true, value: 120 });
        const row = document.createElement("div");
        pane.appendChild(row);
        document.body.appendChild(pane);
        expect(shouldIgnoreUserScrollIntent({ type: "wheel", deltaY: -40, target: row })).toBe(true);
        expect(shouldIgnoreUserScrollIntent({ type: "wheel", deltaY: 40, target: row })).toBe(true);
        pane.remove();
    });

    it("ignores a downward wheel even at the nested pane's top edge", () => {
        const pane = document.createElement("div");
        pane.setAttribute("data-nested-scroll", "");
        Object.defineProperty(pane, "scrollTop", { configurable: true, value: 0 });
        const row = document.createElement("div");
        pane.appendChild(row);
        document.body.appendChild(pane);
        expect(shouldIgnoreUserScrollIntent({ type: "wheel", deltaY: 40, target: row })).toBe(true);
        pane.remove();
    });

    it("counts a content-upward touch drag on a topped nested pane, not a scroll-down one", () => {
        const pane = document.createElement("div");
        pane.setAttribute("data-nested-scroll", "");
        Object.defineProperty(pane, "scrollTop", { configurable: true, value: 0 });
        const row = document.createElement("div");
        pane.appendChild(row);
        document.body.appendChild(pane);
        // A downward finger pull (negative delta, wheel-up sign) on a pane
        // at its top chains to the transcript — a real leave-the-tail intent.
        expect(shouldIgnoreUserScrollIntent({ type: "touchmove", deltaY: -8, target: row })).toBe(false);
        // The opposite drag scrolls content the pane or the tail still owns.
        expect(shouldIgnoreUserScrollIntent({ type: "touchmove", deltaY: 8, target: row })).toBe(true);
        pane.remove();
    });
});

describe("pinAssistantOutputToBottom", () => {
    it("restores scrollHeight after scrollIntoView leaves the padding below the fold", () => {
        const container = { scrollTop: 0, scrollHeight: 1000 } as HTMLElement;
        const end = {
            scrollIntoView() {
                container.scrollTop = container.scrollHeight - 16;
            },
        } as HTMLElement;
        pinAssistantOutputToBottom(container, end, "auto");
        expect(container.scrollTop).toBe(1000);
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

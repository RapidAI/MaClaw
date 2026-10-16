import { describe, expect, it, vi } from "vitest";
import { hideWindowFromControlEvent, stopWindowControlEvent, toggleWindowOnce, windowHideLabel, windowMaximizeLabel } from "../aiAssistantControls";

function fakeMouse(detail: number, type = "click", button = 0) {
    return {
        type,
        detail,
        button,
        preventDefault: vi.fn(),
        stopPropagation: vi.fn(),
    };
}

describe("toggleWindowOnce", () => {
    it("toggles on the first click and swallows the second click of a double-click", () => {
        const toggle = vi.fn();
        const first = fakeMouse(1);
        const second = fakeMouse(2);
        toggleWindowOnce(first, toggle);
        toggleWindowOnce(second, toggle);
        expect(toggle).toHaveBeenCalledTimes(1);
        expect(first.preventDefault).toHaveBeenCalledTimes(1);
        expect(second.stopPropagation).toHaveBeenCalledTimes(1);
    });

    it("still toggles keyboard activation (detail 0)", () => {
        const toggle = vi.fn();
        toggleWindowOnce(fakeMouse(0), toggle);
        expect(toggle).toHaveBeenCalledTimes(1);
    });
});

describe("stopWindowControlEvent", () => {
    it("prevents the title bar from seeing the same gesture", () => {
        const event = fakeMouse(2);
        stopWindowControlEvent(event);
        expect(event.preventDefault).toHaveBeenCalledTimes(1);
        expect(event.stopPropagation).toHaveBeenCalledTimes(1);
    });
});

describe("window control labels", () => {
    it("matches the main header hide copy, including Traditional Chinese", () => {
        expect(windowHideLabel("en")).toBe("Hide window");
        expect(windowHideLabel("zh-Hans")).toBe("隐藏窗口");
        expect(windowHideLabel("zh-Hant")).toBe("隱藏窗口");
    });

    it("switches maximize copy when the window is restored", () => {
        expect(windowMaximizeLabel("en", false)).toBe("Maximize window");
        expect(windowMaximizeLabel("zh-Hans", true)).toBe("还原窗口");
        expect(windowMaximizeLabel("zh-Hant", true)).toBe("還原窗口");
    });
});

describe("hideWindowFromControlEvent", () => {
    it("hides on primary mousedown and ignores the following pointer click", () => {
        const hide = vi.fn();
        hideWindowFromControlEvent(fakeMouse(1, "mousedown"), hide);
        hideWindowFromControlEvent(fakeMouse(1, "click"), hide);
        expect(hide).toHaveBeenCalledTimes(1);
    });

    it("hides from keyboard activation", () => {
        const hide = vi.fn();
        hideWindowFromControlEvent(fakeMouse(0, "click"), hide);
        expect(hide).toHaveBeenCalledTimes(1);
    });

    it("ignores non-primary mouse buttons", () => {
        const hide = vi.fn();
        hideWindowFromControlEvent(fakeMouse(1, "mousedown", 2), hide);
        expect(hide).not.toHaveBeenCalled();
    });
});

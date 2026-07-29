import { describe, expect, it } from "vitest";
import { isPlainEnter } from "../assistantInputShortcuts";

describe("isPlainEnter", () => {
    it("matches only Enter without modifier keys", () => {
        expect(isPlainEnter({ key: "Enter", altKey: false, ctrlKey: false, metaKey: false, shiftKey: false })).toBe(true);
        expect(isPlainEnter({ key: "Enter", altKey: false, ctrlKey: true, metaKey: false, shiftKey: false })).toBe(false);
        expect(isPlainEnter({ key: "Enter", altKey: false, ctrlKey: false, metaKey: true, shiftKey: false })).toBe(false);
        expect(isPlainEnter({ key: "Enter", altKey: false, ctrlKey: false, metaKey: false, shiftKey: true })).toBe(false);
        expect(isPlainEnter({ key: "Enter", altKey: true, ctrlKey: false, metaKey: false, shiftKey: false })).toBe(false);
        expect(isPlainEnter({ key: "Escape", altKey: false, ctrlKey: false, metaKey: false, shiftKey: false })).toBe(false);
    });
});

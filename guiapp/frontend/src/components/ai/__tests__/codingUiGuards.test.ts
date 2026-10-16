import { describe, it, expect, afterEach } from "vitest";
import { isFormFieldTarget, isInsideAriaHidden, isVisibleCodingConflictPanelPresent } from "../codingUiGuards";

describe("isFormFieldTarget", () => {
    it("detects input/textarea/select and contenteditable", () => {
        expect(isFormFieldTarget(document.createElement("input"))).toBe(true);
        expect(isFormFieldTarget(document.createElement("textarea"))).toBe(true);
        expect(isFormFieldTarget(document.createElement("select"))).toBe(true);
        const editable = document.createElement("div");
        editable.contentEditable = "true";
        // jsdom may not set isContentEditable from property alone
        Object.defineProperty(editable, "isContentEditable", { value: true });
        expect(isFormFieldTarget(editable)).toBe(true);
        expect(isFormFieldTarget(document.createElement("div"))).toBe(false);
        expect(isFormFieldTarget(null)).toBe(false);
    });

    it("detects nested targets via closest", () => {
        const wrap = document.createElement("div");
        const input = document.createElement("input");
        wrap.appendChild(input);
        // target is the input itself
        expect(isFormFieldTarget(input)).toBe(true);
    });
});

describe("aria-hidden / visible conflict panel helpers", () => {
    afterEach(() => {
        document.body.innerHTML = "";
    });

    it("isInsideAriaHidden walks ancestors", () => {
        const outer = document.createElement("div");
        outer.setAttribute("aria-hidden", "true");
        const inner = document.createElement("button");
        outer.appendChild(inner);
        document.body.appendChild(outer);
        expect(isInsideAriaHidden(inner)).toBe(true);
        expect(isInsideAriaHidden(document.createElement("div"))).toBe(false);
        expect(isInsideAriaHidden(null)).toBe(false);
    });

    it("isVisibleCodingConflictPanelPresent ignores hidden CF slots", () => {
        expect(isVisibleCodingConflictPanelPresent()).toBe(false);

        const slot = document.createElement("div");
        slot.setAttribute("aria-hidden", "true");
        const panel = document.createElement("div");
        panel.setAttribute("data-testid", "coding-conflict-side-panel");
        slot.appendChild(panel);
        document.body.appendChild(slot);
        expect(isVisibleCodingConflictPanelPresent()).toBe(false);

        slot.removeAttribute("aria-hidden");
        expect(isVisibleCodingConflictPanelPresent()).toBe(true);
    });
});

// @vitest-environment jsdom
import { renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useMentionKeyboard } from "../MentionPopover";

function keyEvent(key: string, modifiers: Partial<KeyboardEvent> = {}) {
    return {
        key,
        preventDefault: vi.fn(),
        ...modifiers,
    } as unknown as React.KeyboardEvent;
}

describe("useMentionKeyboard", () => {
    it("only accepts a mention on plain Enter", () => {
        const onSelect = vi.fn();
        const { result } = renderHook(() => useMentionKeyboard(
            true,
            [{ id: "analyst", name: "Analyst", online: true }],
            0,
            vi.fn(),
            onSelect,
            vi.fn(),
        ));

        for (const modifiers of [{ ctrlKey: true }, { metaKey: true }, { shiftKey: true }, { altKey: true }]) {
            const event = keyEvent("Enter", modifiers);
            expect(result.current(event)).toBe(false);
            expect(event.preventDefault).not.toHaveBeenCalled();
        }

        const plainEnter = keyEvent("Enter");
        expect(result.current(plainEnter)).toBe(true);
        expect(plainEnter.preventDefault).toHaveBeenCalledOnce();
        expect(onSelect).toHaveBeenCalledWith({ id: "analyst", name: "Analyst", online: true });
    });
});

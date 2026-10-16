// @vitest-environment jsdom
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { fireEvent, render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { lightTheme } from "../aiAssistantPanelTheme";
import { BufferQueuePanel, QUEUE_ACTION_BTN_SIZE } from "../BufferQueuePanel";
import type { BufferEntry } from "../useBufferQueue";

const entry: BufferEntry = {
    id: "buf-1",
    text: "use ssh to visit",
    attachments: [],
    createdAt: 1,
    autoDrain: true,
};

function renderQueue() {
    const onEdit = vi.fn();
    const onDelete = vi.fn();
    const onFireEntry = vi.fn();
    const view = render(
        <BufferQueuePanel
            queue={[entry]}
            lang="zh"
            theme={lightTheme}
            themeMode="light"
            editingEntryId={null}
            onEdit={onEdit}
            onCancelEdit={vi.fn()}
            onSaveEdit={vi.fn()}
            onDelete={onDelete}
            onReorder={vi.fn()}
            onFireEntry={onFireEntry}
        />,
    );
    return { ...view, onEdit, onDelete, onFireEntry };
}

describe("BufferQueuePanel action buttons", () => {
    it("renders fire/edit/delete as a matched compact set", () => {
        const { getByTestId } = renderQueue();
        const fire = getByTestId("fire-btn-buf-1");
        const edit = getByTestId("edit-btn-buf-1");
        const del = getByTestId("delete-btn-buf-1");

        for (const btn of [fire, edit, del]) {
            expect(btn.className).toContain("ai-queue-action");
            expect(btn.style.width).toBe(`${QUEUE_ACTION_BTN_SIZE}px`);
            expect(btn.style.height).toBe(`${QUEUE_ACTION_BTN_SIZE}px`);
            expect(btn.style.minWidth).toBe(`${QUEUE_ACTION_BTN_SIZE}px`);
            expect(btn.style.minHeight).toBe(`${QUEUE_ACTION_BTN_SIZE}px`);
            expect(btn.style.borderRadius).toBe("8px");
            expect(btn.querySelector("svg")?.getAttribute("width")).toBe("13");
        }
        expect(fire.className).toContain("ai-queue-action--accent");
        expect(fire.className).not.toContain("ai-queue-action--send");
        expect(del.className).toContain("ai-queue-action--danger");
        expect(edit.style.width).toBe(del.style.width);
        expect(edit.style.height).toBe(fire.style.height);
        expect(edit.style.borderRadius).toBe(fire.style.borderRadius);
        expect(edit.style.background).toBe(del.style.background);
    });

    it("keeps fire/edit/delete working from the compact action cluster", () => {
        const { getByTestId, onEdit, onDelete, onFireEntry } = renderQueue();
        fireEvent.click(getByTestId("fire-btn-buf-1"));
        fireEvent.click(getByTestId("edit-btn-buf-1"));
        fireEvent.click(getByTestId("delete-btn-buf-1"));
        expect(onFireEntry).toHaveBeenCalledWith("buf-1");
        expect(onEdit).toHaveBeenCalledWith("buf-1");
        expect(onDelete).toHaveBeenCalledWith("buf-1");
    });

    it("opts queue actions out of the 34px composer button lock", () => {
        const css = readFileSync(resolve("src/App.css"), "utf8");
        expect(css).toContain("#App .mc-input-stack button.ai-queue-action");
        expect(css).toMatch(/button\.ai-queue-action[\s\S]{0,220}width:\s*24px\s*!important/);
        expect(css).toContain("button.ai-queue-action--accent");
        expect(css).toContain("button.ai-queue-action--danger");
    });

    it("keeps inline edit confirm/cancel compact instead of the 34px toolbar size", () => {
        const { getByTestId } = render(
            <BufferQueuePanel
                queue={[entry]}
                lang="en"
                theme={lightTheme}
                themeMode="light"
                editingEntryId="buf-1"
                onEdit={vi.fn()}
                onCancelEdit={vi.fn()}
                onSaveEdit={vi.fn()}
                onDelete={vi.fn()}
                onReorder={vi.fn()}
                onFireEntry={vi.fn()}
            />,
        );
        const cancel = getByTestId("buffer-entry-cancel-buf-1");
        const confirm = getByTestId("buffer-entry-confirm-buf-1");
        expect(cancel.className).toContain("ai-queue-text-action");
        expect(confirm.className).toContain("ai-queue-text-action");
        expect(confirm.textContent).toBe("OK");
        expect(cancel.textContent).toBe("X");
        expect(cancel.style.height).toBe(`${QUEUE_ACTION_BTN_SIZE}px`);
        expect(confirm.style.height).toBe(`${QUEUE_ACTION_BTN_SIZE}px`);
    });
});

import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { WelcomeTemplatesImportPreviewPanel } from "../WelcomeTemplatesImportPreview";
import { lightTheme } from "../aiAssistantPanelTheme";
import type { WelcomeTemplatesImportPreview } from "../welcomeTaskMemory";

const basePreview = (over: Partial<WelcomeTemplatesImportPreview> = {}): WelcomeTemplatesImportPreview => ({
    mode: "merge",
    raw: "{}",
    toAdd: [{ title: "New A", body: "body-a", snippet: "body-a" }],
    toSkip: [{ title: "Old B", body: "body-b", snippet: "body-b", reason: "duplicate" }],
    extras: { recentCount: 0 },
    hasExtras: false,
    ...over,
});

describe("WelcomeTemplatesImportPreviewPanel", () => {
    it("shows add/skip lists and confirms", () => {
        const onConfirm = vi.fn();
        const onCancel = vi.fn();
        render(
            <WelcomeTemplatesImportPreviewPanel
                lang="zh"
                theme={lightTheme}
                preview={basePreview()}
                onConfirm={onConfirm}
                onCancel={onCancel}
            />,
        );
        const panel = screen.getByTestId("welcome-templates-import-preview");
        expect(panel).toBeTruthy();
        expect(panel.getAttribute("role")).toBe("dialog");
        expect(panel.getAttribute("aria-modal")).toBe("true");
        expect(screen.getByTestId("welcome-import-preview-add").textContent).toContain("New A");
        expect(screen.getByTestId("welcome-import-preview-skip").textContent).toContain("正文已存在");
        fireEvent.click(screen.getByTestId("welcome-import-preview-confirm"));
        expect(onConfirm).toHaveBeenCalledTimes(1);
        fireEvent.click(screen.getByTestId("welcome-import-preview-cancel"));
        expect(onCancel).toHaveBeenCalledTimes(1);
    });

    it("focuses confirm on mount and Esc cancels", () => {
        const onCancel = vi.fn();
        render(
            <WelcomeTemplatesImportPreviewPanel
                lang="en"
                theme={lightTheme}
                preview={basePreview()}
                onConfirm={() => {}}
                onCancel={onCancel}
            />,
        );
        const confirm = screen.getByTestId("welcome-import-preview-confirm");
        expect(document.activeElement).toBe(confirm);
        fireEvent.keyDown(document, { key: "Escape" });
        expect(onCancel).toHaveBeenCalledTimes(1);
    });

    it("disables confirm when nothing to add and no extras", () => {
        render(
            <WelcomeTemplatesImportPreviewPanel
                lang="en"
                theme={lightTheme}
                preview={basePreview({
                    toAdd: [],
                    toSkip: [{ title: "X", body: "y", snippet: "y", reason: "duplicate" }],
                    hasExtras: false,
                })}
                onConfirm={() => {}}
                onCancel={() => {}}
            />,
        );
        expect(
            (screen.getByTestId("welcome-import-preview-confirm") as HTMLButtonElement).disabled,
        ).toBe(true);
    });
});

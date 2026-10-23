import { act, render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { DocxPreviewPanel } from "../DocxPreviewPanel";
import type { CodePreviewTheme } from "../FileTabBar";
import { PREVIEW_BAR_DELAY_MS } from "../PreviewProgressBar";
import { PreviewTaskResultFile } from "../../../../wailsjs/go/main/App";

vi.mock("../../../../wailsjs/go/main/App", () => ({
    PreviewTaskResultFile: vi.fn(),
}));

const preview = vi.mocked(PreviewTaskResultFile);

const theme = {
    bg: "#ffffff",
    text: "#0f172a",
    textMuted: "#6b7280",
    diffDeleteText: "#dc2626",
    lineNumBg: "#f1f5f9",
} as unknown as CodePreviewTheme;

describe("DocxPreviewPanel", () => {
    it("shows a byte progress bar when the document is slow to arrive", async () => {
        preview.mockResolvedValue({
            path: "D:/t/brief.docx",
            kind: "docx",
            preview_url: "/maclaw-preview/v1/file?t=cccccccccccccccccccccccccccccccc",
        } as never);
        let release = () => {};
        const gate = new Promise<void>((resolve) => {
            release = resolve;
        });
        let reads = 0;
        vi.stubGlobal("fetch", vi.fn(async () => ({
            ok: true,
            headers: { get: (name: string) => (name.toLowerCase() === "content-length" ? "100" : null) },
            body: {
                getReader: () => ({
                    read: async () => {
                        reads += 1;
                        if (reads === 1) return { done: false, value: new Uint8Array(25) };
                        await gate;
                        return { done: true, value: undefined };
                    },
                    cancel: async () => {},
                }),
            },
            arrayBuffer: async () => new ArrayBuffer(0),
        })));
        const view = render(<DocxPreviewPanel absPath="D:/t/brief.docx" theme={theme} lang="zh-Hans" />);
        expect(view.queryByTestId("preview-progress-bar")).toBeNull();
        await act(async () => {
            await new Promise((resolve) => setTimeout(resolve, PREVIEW_BAR_DELAY_MS + 80));
        });
        const bar = view.getByTestId("preview-progress-bar");
        expect(bar.getAttribute("data-mode")).toBe("determinate");
        expect(bar.getAttribute("aria-valuenow")).toBe("25");
        expect(view.getByRole("status").textContent).toBe("正在读取 Word…");
        expect(view.getByRole("status").contains(bar)).toBe(false);
        expect(view.getByTestId("docx-preview-loading").textContent).toContain("正在读取 Word");
        expect(view.getByTestId("docx-preview-loading").textContent).toContain("25%");
        view.unmount();
        release();
        await act(async () => {
            await new Promise((resolve) => setTimeout(resolve, 0));
        });
        vi.unstubAllGlobals();
    });
});

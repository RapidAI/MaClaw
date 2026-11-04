import { act, render, waitFor, fireEvent } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { DocxPreviewPanel } from "../DocxPreviewPanel";
import type { CodePreviewTheme } from "../FileTabBar";
import { PREVIEW_BAR_DELAY_MS } from "../PreviewProgressBar";
import { PreviewTaskResultFile } from "../../../../wailsjs/go/main/App";

vi.mock("../../../../wailsjs/go/main/App", () => ({
    PreviewTaskResultFile: vi.fn(),
}));

vi.mock("docx-preview", () => ({
    renderAsync: vi.fn(async (_data: unknown, body: HTMLElement) => {
        body.innerHTML = "<div class=\"maclaw-docx-wrapper\"><section><p>Alpha target</p></section><section><p>Later target</p></section></div>";
    }),
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

    it("highlights Word matches locally and steps through them", async () => {
        preview.mockResolvedValue({
            path: "D:/t/brief.docx",
            kind: "docx",
            preview_url: "/maclaw-preview/v1/file?t=cccccccccccccccccccccccccccccccc",
        } as never);
        vi.stubGlobal("fetch", vi.fn(async () => ({
            ok: true,
            headers: { get: () => null },
            arrayBuffer: async () => new ArrayBuffer(8),
        })));
        const view = render(<DocxPreviewPanel absPath="D:/t/brief.docx" theme={theme} lang="zh-Hans" />);
        await waitFor(() => expect(view.getByTestId("docx-pager")).toBeTruthy());
        expect(view.getByTestId("docx-preview-scroll").textContent).toContain("Alpha target");
        expect(view.getByTestId("companion-doc-find").compareDocumentPosition(view.getByTestId("docx-pager")) & Node.DOCUMENT_POSITION_PRECEDING).toBeTruthy();

        fireEvent.change(view.getByTestId("companion-doc-find-input"), { target: { value: "target" } });
        await waitFor(() => expect(view.getByTestId("companion-doc-find-count").textContent).toBe("1 / 2"));
        expect(view.container.querySelector("mark[data-find-active='true']")?.textContent).toBe("target");
        expect(view.container.querySelectorAll("mark.companion-preview-find")).toHaveLength(2);

        fireEvent.click(view.getByTestId("companion-doc-find-next"));
        expect(view.getByTestId("companion-doc-find-count").textContent).toBe("2 / 2");
        expect(view.container.querySelector("mark[data-find-active='true']")?.parentElement?.textContent).toContain("Later");

        fireEvent.keyDown(window, { key: "f", ctrlKey: true });
        expect(document.activeElement).toBe(view.getByTestId("companion-doc-find-input"));

        fireEvent.change(view.getByTestId("companion-doc-find-input"), { target: { value: "" } });
        await waitFor(() => expect(view.container.querySelector("mark.companion-preview-find")).toBeNull());
        expect(view.getByTestId("docx-preview-scroll").textContent).toContain("Alpha target");

        const scroller = view.getByTestId("docx-preview-scroll");
        const section = scroller.querySelector("section");
        expect(section).toBeTruthy();
        scroller.getBoundingClientRect = () => ({
            x: 0, y: 0, left: 0, top: 0, right: 300, bottom: 200, width: 300, height: 200, toJSON() { return {}; },
        });
        section!.getBoundingClientRect = () => ({
            x: 0, y: 480, left: 0, top: 480, right: 300, bottom: 900, width: 300, height: 420, toJSON() { return {}; },
        });
        const goto = view.getByTestId("docx-goto");
        fireEvent.focus(goto);
        fireEvent.blur(goto);
        expect(scroller.scrollTop).toBe(0);

        view.unmount();
        vi.unstubAllGlobals();
    });
});

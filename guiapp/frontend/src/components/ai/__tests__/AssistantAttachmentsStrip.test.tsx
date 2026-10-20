import { act, fireEvent, render, waitFor } from "@testing-library/react";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../../wailsjs/go/main/App", () => ({
    AIAssistantAttachmentPreviewDataURL: vi.fn(async () => ""),
    AIAssistantAttachmentOpeningPreview: vi.fn(async () => ({
        kind: "pdf",
        preview_url: "/maclaw-preview/v1/file?t=0123456789abcdef0123456789abcdef",
        pages: [{ index: 1, text: "规格书正文".repeat(20), width: 595, height: 842 }],
    })),
    PreviewTaskResultFile: vi.fn(async () => ({ content: "" })),
}));

import { AIAssistantAttachmentOpeningPreview, PreviewTaskResultFile } from "../../../../wailsjs/go/main/App";
import { AssistantAttachmentsStrip } from "../AssistantAttachmentsStrip";
import { AttachmentDocumentPreview, settleAttachmentDocxHost } from "../AttachmentDocumentPreview";
import { AI_PANEL_STATIC_STYLE_ID, lightTheme } from "../aiAssistantPanelTheme";
import type { AttachmentInfo } from "../useBufferQueue";

const pendingAttachment = (filePath: string): AttachmentInfo => ({
    filePath,
    fileName: filePath.split(/[\\/]/).pop() || filePath,
    extension: ".pdf",
    isImage: false,
});

function PendingAttachmentsHarness({ attachments }: { attachments: AttachmentInfo[] }) {
    const [pendingAttachments, setPendingAttachments] = useState(attachments);
    return <AssistantAttachmentsStrip cancelPending={false} lang="en" pendingAttachments={pendingAttachments} selectedFilePaths={[]} setPendingAttachments={setPendingAttachments} theme={lightTheme} />;
}

describe("AssistantAttachmentsStrip", () => {
    afterEach(() => vi.unstubAllGlobals());

    it("loads compact attachment styles wherever the strip is rendered", () => {
        const stylesheet = document.getElementById(AI_PANEL_STATIC_STYLE_ID);
        expect(stylesheet?.textContent).toContain(".ai-attachment-remove:focus-visible");
        expect(stylesheet?.textContent).toContain("width: fit-content");
        expect(stylesheet?.textContent).toContain("grid-template-columns: repeat(auto-fill, minmax(58px, max-content))");
    });

    it("shows a page preview at reading size and removes it", async () => {
        const path = "D:\\cases\\contracts\\supplier-agreement.pdf";
        const { findByTestId, getByLabelText, getByRole } = render(<PendingAttachmentsHarness attachments={[pendingAttachment(path)]} />);

        const row = getByRole("listitem");
        expect(row.getAttribute("title")).toBe(path);
        expect(row.style.display).toBe("grid");
        expect(row.style.width).toBe("max-content");
        expect(row.style.maxWidth).toBe("100%");
        expect(row.style.gridTemplateColumns).toBe("minmax(0, max-content) auto");
        await findByTestId("attachment-document-pdf");
        const preview = await findByTestId("attachment-document-preview");
        expect(preview.getAttribute("data-state")).toBe("ready");
        expect(preview.getAttribute("data-preview-max-width")).toBe("640");
        expect(preview.getAttribute("data-preview-max-height")).toBe("480");
        expect(preview.style.width).toBe("640px");
        expect(preview.style.height).toBe("480px");
        expect(getByLabelText("supplier-agreement.pdf").textContent).toBe("supplier-agreement.pdf");
        fireEvent.click(getByRole("button", { name: "Remove supplier-agreement.pdf" }));
        expect(document.querySelector('[data-testid="ai-pending-attachments"]')).toBeNull();
    });

    it("keeps document previews in one scrolling column", async () => {
        const attachments = Array.from({ length: 8 }, (_, index) => pendingAttachment(`D:\\batch\\document-${index}.pdf`));
        const { findAllByTestId, getByTestId, getAllByRole } = render(<PendingAttachmentsHarness attachments={attachments} />);

        const strip = getByTestId("ai-pending-attachments");
        expect(strip.style.alignSelf).toBe("flex-start");
        expect(strip.style.display).toBe("flex");
        expect(strip.style.maxHeight).toBe("528px");
        expect(strip.style.overflowY).toBe("auto");
        expect(getAllByRole("listitem")).toHaveLength(8);
        expect(await findAllByTestId("attachment-document-pdf")).toHaveLength(8);
    });

    it("removes the matching file-picker attachment by path", async () => {
        const onRemoveSelectedFile = vi.fn();
        const { findAllByText, getByRole } = render(<AssistantAttachmentsStrip cancelPending={false} lang="en-US" pendingAttachments={[]} selectedFilePaths={["D:\\briefs\\first.docx", "D:\\briefs\\second.docx"]} removeSelectedFile={onRemoveSelectedFile} setPendingAttachments={vi.fn()} theme={lightTheme} />);

        expect(await findAllByText("DOCX")).toHaveLength(2);
        fireEvent.click(getByRole("button", { name: "Remove second.docx" }));
        expect(onRemoveSelectedFile).toHaveBeenCalledWith("D:\\briefs\\second.docx");
    });

    it("does not remove pending attachments while cancellation is in progress", async () => {
        const setPendingAttachments = vi.fn();
        const { findByTestId, getByRole } = render(<AssistantAttachmentsStrip cancelPending lang="en" pendingAttachments={[pendingAttachment("D:\\briefs\\locked.pdf")]} selectedFilePaths={[]} setPendingAttachments={setPendingAttachments} theme={lightTheme} />);

        await findByTestId("attachment-document-pdf");
        const remove = getByRole("button", { name: "Remove locked.pdf" }) as HTMLButtonElement;
        expect(remove.disabled).toBe(true);
        fireEvent.click(remove);
        expect(setPendingAttachments).not.toHaveBeenCalled();
    });

    it("watches the chat scroller, drops the PDF plugin outside it, and does not parse again", async () => {
        const path = "D:\\cases\\scroll\\spec.pdf";
        let onEntries: IntersectionObserverCallback | null = null;
        let init: IntersectionObserverInit | undefined;
        class MockObserver {
            constructor(callback: IntersectionObserverCallback, options?: IntersectionObserverInit) {
                onEntries = callback;
                init = options;
            }
            observe() {}
            unobserve() {}
            disconnect() {}
            takeRecords() { return []; }
        }
        vi.stubGlobal("IntersectionObserver", MockObserver);

        const { findByTestId, getByTestId, queryByTestId } = render(
            <div data-testid="chat-scroller" style={{ overflowY: "auto", height: "400px" }}>
                <AttachmentDocumentPreview filePath={path} fileName="spec.pdf" extension=".pdf" theme={lightTheme} lang="zh" onFail={() => {}} />
            </div>,
        );

        await waitFor(() => expect(onEntries).toBeTruthy());
        expect(init?.root).toBe(getByTestId("chat-scroller"));
        expect(init?.rootMargin).toBe("480px");
        expect(queryByTestId("attachment-document-pdf")).toBeNull();

        const emit = (isIntersecting: boolean) => {
            onEntries?.([{ isIntersecting, target: getByTestId("chat-scroller") } as unknown as IntersectionObserverEntry], {} as IntersectionObserver);
        };
        await act(async () => { emit(true); });
        await findByTestId("attachment-document-pdf");

        await act(async () => { emit(false); });
        await waitFor(() => expect(queryByTestId("attachment-document-pdf")).toBeNull());
        expect(getByTestId("attachment-document-preview").getAttribute("data-state")).toBe("ready");

        await act(async () => { emit(true); });
        await findByTestId("attachment-document-pdf");
        const calls = vi.mocked(AIAssistantAttachmentOpeningPreview).mock.calls.filter((call) => call[0] === path);
        expect(calls).toHaveLength(1);
    });

    it("shows source with a monospace page", async () => {
        vi.mocked(PreviewTaskResultFile).mockResolvedValueOnce({ content: "func main() {\n\treturn\n}\n" } as never);
        const { findByTestId } = render(
            <AttachmentDocumentPreview filePath="D:\\src\\main.go" fileName="main.go" extension=".go" theme={lightTheme} lang="zh" onFail={() => {}} />,
        );
        const page = await findByTestId("attachment-document-page");
        expect(page.querySelector("pre")?.style.fontFamily).toContain("monospace");
        expect(page.textContent).toContain("func main()");
    });

    it("fits Word pages to the card instead of the viewer gutter", () => {
        const host = document.createElement("div");
        const dense = "光电流".repeat(40);
        host.innerHTML = `<div></div><div><div class="maclaw-docx-wrapper" style="padding:30px;background:gray"><section class="maclaw-docx">封面</section><section class="maclaw-docx">${dense}</section><section class="maclaw-docx">后面的正文</section></div></div>`;
        expect(settleAttachmentDocxHost(host)).toBe(2);
        expect(host.querySelectorAll("section.maclaw-docx")).toHaveLength(2);
        const wrapper = host.querySelector(".maclaw-docx-wrapper") as HTMLElement;
        expect(wrapper.style.padding).toBe("0px");
        expect(wrapper.style.background).toBe("transparent");
        const pages = [...host.querySelectorAll("section.maclaw-docx")] as HTMLElement[];
        expect(pages[0].style.margin).toBe("0px");
        expect(pages[0].style.boxShadow).toBe("none");
        expect(pages[1].style.borderTopWidth).toBe("1px");
    });
});

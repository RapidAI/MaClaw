import { fireEvent, render } from "@testing-library/react";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AssistantAttachmentsStrip } from "../AssistantAttachmentsStrip";
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
    return (
        <AssistantAttachmentsStrip
            cancelPending={false}
            lang="en"
            pendingAttachments={pendingAttachments}
            selectedFilePaths={[]}
            setPendingAttachments={setPendingAttachments}
            theme={lightTheme}
        />
    );
}

describe("AssistantAttachmentsStrip", () => {
    afterEach(() => {
        vi.unstubAllGlobals();
    });

    it("loads its interaction styles wherever the strip is rendered", () => {
        const stylesheet = document.getElementById(AI_PANEL_STATIC_STYLE_ID);
        expect(stylesheet?.textContent).toContain(".ai-attachment-remove:focus-visible");
        expect(stylesheet?.textContent).not.toContain("content-visibility: auto");
        expect(stylesheet?.textContent).toContain("width: min(100%, 760px)");
        expect(stylesheet?.textContent).toContain("grid-template-columns: repeat(auto-fill, minmax(min(100%, 218px), 1fr))");
    });

    it("shows a filename, exposes its full path, and removes a pending attachment", () => {
        const path = "D:\\cases\\contracts\\supplier-agreement.pdf";
        const { getByRole, getByText, queryByText } = render(<PendingAttachmentsHarness attachments={[pendingAttachment(path)]} />);

        expect(getByText("supplier-agreement.pdf").getAttribute("title")).toBe(path);
        expect(getByRole("listitem").getAttribute("title")).toBe(path);
        fireEvent.click(getByRole("button", { name: "Remove supplier-agreement.pdf" }));
        expect(queryByText("supplier-agreement.pdf")).toBeNull();
    });

    it("keeps a bounded vertical list when many attachments are present", () => {
        const attachments = Array.from({ length: 8 }, (_, index) => pendingAttachment(`D:\\batch\\document-${index}.pdf`));
        const { getByTestId, getAllByRole } = render(<PendingAttachmentsHarness attachments={attachments} />);

        const strip = getByTestId("ai-pending-attachments");
        expect(strip.style.alignSelf).toBe("flex-start");
        expect(strip.style.maxHeight).toBe("184px");
        expect(strip.style.overflowY).toBe("auto");
        expect(getAllByRole("listitem")).toHaveLength(8);
    });

    it("abbreviates a long filename while retaining its distinguishing ending and extension", () => {
        const fileName = "2026-enterprise-procurement-supplier-qualification-and-contract-review-final-version.docx";
        const path = `D:\\briefs\\${fileName}`;
        const { getByLabelText } = render(<PendingAttachmentsHarness attachments={[pendingAttachment(path)]} />);

        const label = getByLabelText(fileName);
        expect(label.textContent).toBe("2026-enterprise-procur...inal-version.docx");
        expect(label.getAttribute("title")).toBe(path);
    });

    it("does not split an emoji or a CJK filename while abbreviating", () => {
        const fileName = "采购合同📄供应商资格审查与最终版本确认材料补充说明与盖章归档及历史修订记录和全部技术规格附录.docx";
        const path = `D:\\briefs\\${fileName}`;
        const { getByLabelText } = render(<PendingAttachmentsHarness attachments={[pendingAttachment(path)]} />);

        const label = getByLabelText(fileName);
        expect(label.textContent).toBe("采购合同📄供应商资格审查与最终版本确认材料补...订记录和全部技术规格附录.docx");
        expect(label.textContent).not.toContain("\uFFFD");
        expect(label.getAttribute("title")).toBe(path);
    });

    it("keeps a combined emoji grapheme intact at an abbreviation boundary", () => {
        const fileName = `A${"x".repeat(25)}👩🏽‍💻${"y".repeat(25)}.pdf`;
        const path = `D:\\briefs\\${fileName}`;
        const { getByLabelText } = render(<PendingAttachmentsHarness attachments={[pendingAttachment(path)]} />);

        const label = getByLabelText(fileName);
        expect(label.textContent).not.toContain("\uFFFD");
        expect(label.textContent).not.toContain("👩🏽‍");
        expect(label.getAttribute("title")).toBe(path);
    });

    it("falls back safely when the runtime does not provide Intl.Segmenter", () => {
        vi.stubGlobal("Intl", { ...Intl, Segmenter: undefined });
        const fileName = `A${"x".repeat(50)}.pdf`;
        const path = `D:\\briefs\\${fileName}`;
        const { getByLabelText } = render(<PendingAttachmentsHarness attachments={[pendingAttachment(path)]} />);

        const label = getByLabelText(fileName);
        expect(label.textContent).toBe("Axxxxxxxxxxxxxxxxxxxxx...xxxxxxxxxxxxx.pdf");
        expect(label.getAttribute("title")).toBe(path);
    });

    it("removes the matching file-picker attachment by path", () => {
        const onRemoveSelectedFile = vi.fn();
        const { getByRole } = render(
            <AssistantAttachmentsStrip
                cancelPending={false}
                lang="en-US"
                pendingAttachments={[]}
                selectedFilePaths={["D:\\briefs\\first.docx", "D:\\briefs\\second.docx"]}
                removeSelectedFile={onRemoveSelectedFile}
                setPendingAttachments={vi.fn()}
                theme={lightTheme}
            />,
        );

        fireEvent.click(getByRole("button", { name: "Remove second.docx" }));
        expect(onRemoveSelectedFile).toHaveBeenCalledWith("D:\\briefs\\second.docx");
    });

    it("keeps the selected-file identity stable across repeated remove clicks", () => {
        const onRemoveSelectedFile = vi.fn();
        const secondPath = "D:\\briefs\\second.docx";
        const { getByRole } = render(
            <AssistantAttachmentsStrip
                cancelPending={false}
                lang="en"
                pendingAttachments={[]}
                selectedFilePaths={["D:\\briefs\\first.docx", secondPath, "D:\\briefs\\third.docx"]}
                removeSelectedFile={onRemoveSelectedFile}
                setPendingAttachments={vi.fn()}
                theme={lightTheme}
            />,
        );

        const remove = getByRole("button", { name: "Remove second.docx" });
        fireEvent.click(remove);
        fireEvent.click(remove);
        expect(onRemoveSelectedFile).toHaveBeenNthCalledWith(1, secondPath);
        expect(onRemoveSelectedFile).toHaveBeenNthCalledWith(2, secondPath);
    });

    it("does not remove pending attachments while cancellation is in progress", () => {
        const setPendingAttachments = vi.fn();
        const { getByRole } = render(
            <AssistantAttachmentsStrip
                cancelPending
                lang="en"
                pendingAttachments={[pendingAttachment("D:\\briefs\\locked.pdf")]}
                selectedFilePaths={[]}
                setPendingAttachments={setPendingAttachments}
                theme={lightTheme}
            />,
        );

        const remove = getByRole("button", { name: "Remove locked.pdf" }) as HTMLButtonElement;
        expect(remove.disabled).toBe(true);
        fireEvent.click(remove);
        expect(setPendingAttachments).not.toHaveBeenCalled();
    });
});

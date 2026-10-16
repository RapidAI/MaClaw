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

    it("shows a compact file icon with the full path as tooltip and removes it", () => {
        const path = "D:\\cases\\contracts\\supplier-agreement.pdf";
        const { getByLabelText, getByRole } = render(<PendingAttachmentsHarness attachments={[pendingAttachment(path)]} />);

        expect(getByRole("listitem").getAttribute("title")).toBe(path);
        expect(getByRole("listitem").textContent).toContain("PDF");
        expect(getByLabelText("supplier-agreement.pdf").textContent).toBe("supplier-agreement.pdf");
        fireEvent.click(getByRole("button", { name: "Remove supplier-agreement.pdf" }));
        expect(document.querySelector('[data-testid="ai-pending-attachments"]')).toBeNull();
    });

    it("keeps a bounded compact grid when many attachments are present", () => {
        const attachments = Array.from({ length: 8 }, (_, index) => pendingAttachment(`D:\\batch\\document-${index}.pdf`));
        const { getByTestId, getAllByRole } = render(<PendingAttachmentsHarness attachments={attachments} />);

        const strip = getByTestId("ai-pending-attachments");
        expect(strip.style.alignSelf).toBe("flex-start");
        expect(strip.style.maxHeight).toBe("120px");
        expect(strip.style.overflowY).toBe("auto");
        expect(getAllByRole("listitem")).toHaveLength(8);
    });

    it("removes the matching file-picker attachment by path", () => {
        const onRemoveSelectedFile = vi.fn();
        const { getByRole } = render(<AssistantAttachmentsStrip cancelPending={false} lang="en-US" pendingAttachments={[]} selectedFilePaths={["D:\\briefs\\first.docx", "D:\\briefs\\second.docx"]} removeSelectedFile={onRemoveSelectedFile} setPendingAttachments={vi.fn()} theme={lightTheme} />);

        fireEvent.click(getByRole("button", { name: "Remove second.docx" }));
        expect(onRemoveSelectedFile).toHaveBeenCalledWith("D:\\briefs\\second.docx");
    });

    it("does not remove pending attachments while cancellation is in progress", () => {
        const setPendingAttachments = vi.fn();
        const { getByRole } = render(<AssistantAttachmentsStrip cancelPending lang="en" pendingAttachments={[pendingAttachment("D:\\briefs\\locked.pdf")]} selectedFilePaths={[]} setPendingAttachments={setPendingAttachments} theme={lightTheme} />);

        const remove = getByRole("button", { name: "Remove locked.pdf" }) as HTMLButtonElement;
        expect(remove.disabled).toBe(true);
        fireEvent.click(remove);
        expect(setPendingAttachments).not.toHaveBeenCalled();
    });
});

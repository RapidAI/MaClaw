import { describe, expect, it } from "vitest";
import {
    ATTACHMENT_PREVIEW_MAX_PAGES,
    ATTACHMENT_PREVIEW_TEXT_PAGE_LINES,
    attachmentPreviewPageIsSparse,
    openingPreviewPageCount,
    openingPreviewTexts,
    splitTextIntoPreviewPages,
} from "./attachmentOpeningPreview";

describe("opening preview pages", () => {
    it("keeps the next page while the current page has little text", () => {
        const dense = "正".repeat(80);
        expect(openingPreviewPageCount(["封面", dense, "不再纳入"])).toBe(2);
        expect(openingPreviewPageCount(["封面", "仍少", dense])).toBe(3);
        expect(openingPreviewPageCount([dense, "封面"])).toBe(1);
        expect(openingPreviewTexts(["封面", dense])).toEqual(["封面", dense]);
    });

    it("stops a run of blank pages at the safety cap", () => {
        const blanks = Array.from({ length: ATTACHMENT_PREVIEW_MAX_PAGES + 4 }, () => "");
        expect(openingPreviewPageCount(blanks)).toBe(ATTACHMENT_PREVIEW_MAX_PAGES);
    });

    it("treats a short virtual page as sparse and includes the next one", () => {
        const sparse = Array.from({ length: ATTACHMENT_PREVIEW_TEXT_PAGE_LINES }, () => "").join("\n");
        const dense = "字".repeat(100);
        const pages = splitTextIntoPreviewPages(`${sparse}\n${dense}`);
        expect(attachmentPreviewPageIsSparse(pages[0])).toBe(true);
        expect(openingPreviewPageCount(pages)).toBe(2);
        expect(openingPreviewTexts(pages)[1]).toContain("字字");
    });

    it("starts a new page at a form feed", () => {
        const pages = splitTextIntoPreviewPages(`封面\f${"正文".repeat(40)}`);
        expect(pages).toHaveLength(2);
        expect(openingPreviewPageCount(pages)).toBe(2);
    });
});

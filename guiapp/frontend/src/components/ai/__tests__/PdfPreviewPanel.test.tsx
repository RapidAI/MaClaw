import { render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { PdfPreviewPanel, isPdfFileName } from "../PdfPreviewPanel";
import type { CodePreviewTheme } from "../FileTabBar";
import { PreviewTaskResultFile } from "../../../../wailsjs/go/main/App";
import { pdfInlineDataURLFromBase64 } from "../taskResultPreview";

const samplePdfDataURL = () => pdfInlineDataURLFromBase64("JVBERi0=");

vi.mock("../../../../wailsjs/go/main/App", () => ({
    PreviewTaskResultFile: vi.fn(async () => ({
        path: "D:/docs/report.pdf",
        file_name: "report.pdf",
        kind: "pdf",
        data_url: "data:" + "application/" + "pdf" + ";base64," + "JVBERi0=",
    })),
}));

const preview = vi.mocked(PreviewTaskResultFile);

const theme = {
    bg: "#ffffff",
    text: "#111827",
    textMuted: "#6b7280",
    diffDeleteText: "#dc2626",
} as CodePreviewTheme;

afterEach(() => {
    preview.mockClear();
});

describe("isPdfFileName", () => {
    it("matches pdf suffixes", () => {
        expect(isPdfFileName("a.pdf")).toBe(true);
        expect(isPdfFileName("a.PDF")).toBe(true);
        expect(isPdfFileName("a.pptx")).toBe(false);
    });
});

describe("PdfPreviewPanel", () => {
    it("renders an iframe for an inline PDF", async () => {
        const { getByTestId } = render(
            <PdfPreviewPanel absPath="D:/docs/report.pdf" theme={theme} lang="zh" />,
        );
        expect(getByTestId("pdf-preview-loading")).toBeTruthy();
        await waitFor(() => expect(getByTestId("pdf-preview-panel")).toBeTruthy());
        const src = getByTestId("pdf-preview-panel").getAttribute("src") || "";
        expect(src.startsWith("blob:") || src === samplePdfDataURL()).toBe(true);
        expect(preview).toHaveBeenCalledWith("D:/docs/report.pdf");
    });

    it("uses a host-provided data URL without a second fetch", async () => {
        const { getByTestId } = render(
            <PdfPreviewPanel
                absPath="D:/docs/report.pdf"
                dataUrl={samplePdfDataURL()}
                theme={theme}
                lang="en"
            />,
        );
        await waitFor(() => expect(getByTestId("pdf-preview-panel")).toBeTruthy());
        expect(preview).not.toHaveBeenCalled();
    });

    it("uses a tokenized preview URL from the backend", async () => {
        preview.mockResolvedValueOnce({
            path: "D:/docs/report.pdf",
            file_name: "report.pdf",
            kind: "pdf",
            preview_url: "/maclaw-preview/v1/file?t=abc",
        } as never);
        const { getByTestId } = render(
            <PdfPreviewPanel absPath="D:/docs/report.pdf" theme={theme} lang="en" />,
        );
        await waitFor(() => expect(getByTestId("pdf-preview-panel")).toBeTruthy());
        expect(getByTestId("pdf-preview-panel").getAttribute("src")).toBe("/maclaw-preview/v1/file?t=abc");
    });

    it("falls back to extracted text when the backend returns content", async () => {
        preview.mockResolvedValueOnce({
            path: "D:/docs/big.pdf",
            file_name: "big.pdf",
            kind: "text",
            content: "extracted pdf body",
        } as never);
        const { getByTestId } = render(
            <PdfPreviewPanel absPath="D:/docs/big.pdf" theme={theme} lang="en" />,
        );
        await waitFor(() => expect(getByTestId("pdf-preview-text").textContent).toBe("extracted pdf body"));
    });
});

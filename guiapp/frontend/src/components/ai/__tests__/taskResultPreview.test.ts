import { describe, expect, it } from "vitest";
import {
    applyContinueEditTaskResultDraft,
    codeFileForImmediateTaskResultPreview,
    codeFileFromTaskResultPreview,
    continueEditTaskResultDraft,
    continueEditTaskResultPathFromEvent,
    isPdfFileName,
    isPdfInlineDataURL,
    isTaskResultPdfPreviewURL,
    localizeTaskResultExportError,
    localizeTaskResultPreviewError,
    objectURLFromPdfDataURL,
    pdfInlineDataURLFromBase64,
    previewTaskResultPathFromEvent,
    taskResultPreviewKindFromPath,
} from "../taskResultPreview";

const samplePdfDataURL = () => pdfInlineDataURLFromBase64("JVBERi0=");

describe("isPdfFileName", () => {
    it("matches .pdf case-insensitively", () => {
        expect(isPdfFileName("report.pdf")).toBe(true);
        expect(isPdfFileName("report.PDF")).toBe(true);
        expect(isPdfFileName("report.pdf.txt")).toBe(false);
        expect(isPdfFileName("report.pptx")).toBe(false);
    });
});

describe("isTaskResultPdfPreviewURL", () => {
    it("accepts only the local tokenized PDF route", () => {
        expect(isTaskResultPdfPreviewURL("/maclaw-preview/v1/file?t=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")).toBe(true);
        expect(isTaskResultPdfPreviewURL("/maclaw-preview/v1/file?t=abc")).toBe(false);
        expect(isTaskResultPdfPreviewURL("https://example.test/maclaw-preview/v1/file?t=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")).toBe(false);
        expect(isTaskResultPdfPreviewURL("//example.test/maclaw-preview/v1/file?t=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")).toBe(false);
    });
});

describe("taskResultPreviewKindFromPath", () => {
    it("classifies visual files from the basename", () => {
        expect(taskResultPreviewKindFromPath("F:\\\\decks\\\\demo.PPTX")).toBe("pptx");
        expect(taskResultPreviewKindFromPath("/tmp/doc.pdf")).toBe("pdf");
        expect(taskResultPreviewKindFromPath("/tmp/photo.png")).toBe("image");
        expect(taskResultPreviewKindFromPath("/tmp/clip.mp4")).toBe("video");
        expect(taskResultPreviewKindFromPath("/tmp/notes.md")).toBe("");
    });
});

describe("codeFileFromTaskResultPreview", () => {
    it("opens pptx as a slide-preview file", () => {
        const file = codeFileFromTaskResultPreview({
            path: "F:\\decks\\demo.pptx",
            file_name: "demo.pptx",
            kind: "pptx",
        }, "F:\\decks\\demo.pptx");
        expect(file.language).toBe("pptx");
        expect(file.absPath).toBe("F:\\decks\\demo.pptx");
        expect(file.content).toBe("");
        expect(file.opType).toBe("read");
    });

    it("opens pdf by path only so bytes are not persisted in preview state", () => {
        const file = codeFileFromTaskResultPreview({
            path: "/tmp/doc.pdf",
            file_name: "doc.pdf",
            kind: "pdf",
            data_url: samplePdfDataURL(),
        }, "/tmp/doc.pdf");
        expect(file.language).toBe("pdf");
        expect(file.content).toBe("");
        expect(file.absPath).toBe("/tmp/doc.pdf");
    });

    it("opens text documents with extracted content", () => {
        const file = codeFileFromTaskResultPreview({
            path: "/tmp/notes.md",
            file_name: "notes.md",
            kind: "text",
            language: "markdown",
            content: "# hi",
            truncated: true,
        }, "/tmp/notes.md");
        expect(file.language).toBe("markdown");
        expect(file.content).toBe("# hi");
        expect(file.previewTruncated).toBe(true);
    });
});

describe("codeFileForImmediateTaskResultPreview", () => {
    it("opens a pptx tab without waiting for a backend round-trip", () => {
        const file = codeFileForImmediateTaskResultPreview("D:/deck.pptx", "pptx");
        expect(file.language).toBe("pptx");
        expect(file.fileName).toBe("deck.pptx");
        expect(file.content).toBe("");
    });
});

describe("previewTaskResultPathFromEvent", () => {
    it("reads the path from the custom event detail", () => {
        const event = new CustomEvent("maclaw:preview-task-result", { detail: { path: " D:\\a.pdf ", messageId: "m1" } });
        expect(previewTaskResultPathFromEvent(event)).toBe("D:\\a.pdf");
    });
});

describe("continueEditTaskResultDraft", () => {
    it("builds a composer draft that names the document and path", () => {
        expect(continueEditTaskResultDraft("C:\\docs\\report.docx", "zh")).toBe("请继续修改文档「report.docx」（C:\\docs\\report.docx）：\n");
        expect(continueEditTaskResultDraft("C:\\docs\\report.docx", "en")).toBe('Please continue editing the document "report.docx" (C:\\docs\\report.docx):\n');
    });

    it("keeps typed text and appends the file prompt when needed", () => {
        expect(applyContinueEditTaskResultDraft("C:\\docs\\report.docx", "zh", "")).toBe("请继续修改文档「report.docx」（C:\\docs\\report.docx）：\n");
        expect(applyContinueEditTaskResultDraft("C:\\docs\\report.docx", "zh", "  ")).toBe("请继续修改文档「report.docx」（C:\\docs\\report.docx）：\n");
        expect(applyContinueEditTaskResultDraft("C:\\docs\\report.docx", "zh", "把第二段改短")).toBe(
            "把第二段改短\n\n请继续修改文档「report.docx」（C:\\docs\\report.docx）：\n",
        );
        expect(applyContinueEditTaskResultDraft("C:\\docs\\report.docx", "zh", "请继续修改文档「report.docx」（C:\\docs\\report.docx）：\n")).toBe(
            "请继续修改文档「report.docx」（C:\\docs\\report.docx）：\n",
        );
        expect(applyContinueEditTaskResultDraft("C:\\docs\\report.docx", "zh", "annual report.docx 的格式")).toBe(
            "annual report.docx 的格式\n\n请继续修改文档「report.docx」（C:\\docs\\report.docx）：\n",
        );
        expect(applyContinueEditTaskResultDraft("C:\\docs\\report.docx", "zh", "请继续改 C:/docs/report.docx")).toBe(
            "请继续改 C:/docs/report.docx",
        );
    });

    it("reads the path from the continue-edit event", () => {
        const event = new CustomEvent("maclaw:continue-edit-task-result", { detail: { path: " D:\\a.docx ", messageId: "m1" } });
        expect(continueEditTaskResultPathFromEvent(event)).toBe("D:\\a.docx");
    });

    it("localizes export errors, including wrapped Wails messages", () => {
        expect(localizeTaskResultExportError("文件不存在", "en")).toBe("The file does not exist");
        expect(localizeTaskResultExportError("Error: 文件过大，无法导出", "zh")).toBe("文件过大，无法导出");
        expect(localizeTaskResultExportError("ExportTaskResultFile: 文件不存在", "en")).toBe("The file does not exist");
    });
});

describe("pdf inline data URL helpers", () => {
    it("localizes preview errors", () => {
        expect(localizeTaskResultPreviewError("文件不存在", "en")).toBe("The file does not exist");
        expect(localizeTaskResultPreviewError("文件不存在", "zh")).toBe("文件不存在");
    });

    it("accepts a PDF data URL and can mint an object URL", () => {
        const url = samplePdfDataURL();
        expect(isPdfInlineDataURL(url)).toBe(true);
        expect(isPdfInlineDataURL("https://example.test/doc.pdf")).toBe(false);
        const objectUrl = objectURLFromPdfDataURL(url);
        expect(objectUrl.startsWith("blob:") || objectUrl === url).toBe(true);
        if (objectUrl.startsWith("blob:")) URL.revokeObjectURL(objectUrl);
    });
});

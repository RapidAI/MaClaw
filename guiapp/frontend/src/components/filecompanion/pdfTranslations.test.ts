// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest";
import {
    clipPdfAnnotationName,
    hydratePdfTranslations,
    loadPdfTranslations,
    PDF_ANNOTATION_NAME_LIMIT,
    pdfAnnotationName,
    savePdfTranslations,
} from "./pdfTranslations";

const part = { page: 1, rects: [{ x: 0.1, y: 0.2, w: 0.3, h: 0.05 }] };

beforeEach(() => {
    hydratePdfTranslations({});
});

describe("pdf translations", () => {
    it("keeps an annotation name on one line inside the name budget", () => {
        const saved = savePdfTranslations("/tmp/a.pdf", [
            { id: "n1", kind: "annotation", name: "  复核   推导  ", text: "Hello", translation: "正文", parts: [part] },
            { id: "n2", kind: "annotation", name: "名".repeat(41), text: "Hello", translation: "正文", parts: [part] },
            { id: "n3", kind: "annotation", text: "Hello", translation: "正文", parts: [part] },
            { id: "n4", kind: "annotation", name: "   ", text: "Hello", translation: "正文", parts: [part] },
        ]);
        expect(PDF_ANNOTATION_NAME_LIMIT).toBe(40);
        expect(saved[0]?.name).toBe("复核 推导");
        expect(saved[1]?.name).toBe("名".repeat(40));
        expect(saved[2]?.name).toBeUndefined();
        expect(saved[3]?.name).toBeUndefined();
        expect(clipPdfAnnotationName(" a \n b ")).toBe("a b");
        expect(clipPdfAnnotationName("x".repeat(50))).toBe("x".repeat(40));
    });

    it("labels a note by its name, or by its source when it has none", () => {
        const [named, old] = savePdfTranslations("/tmp/a.pdf", [
            { id: "n1", kind: "annotation", name: "复核", text: "Hello world", translation: "正文", parts: [part] },
            { id: "n2", kind: "annotation", text: "只用原文", translation: "正文", parts: [part] },
        ]);
        expect(pdfAnnotationName(named)).toBe("复核");
        expect(pdfAnnotationName(old)).toBe("只用原文");
        expect(loadPdfTranslations("C:/tmp/other.pdf")).toEqual([]);
    });

    it("keeps a name when the raw store record is hydrated", () => {
        hydratePdfTranslations({
            "c:/docs/paper.pdf": [
                { id: "n1", kind: "annotation", name: "  复核  推导 ", text: "Hello", translation: "正文", parts: [part] },
                { id: "t1", text: "Hello", translation: "你好", parts: [part] },
            ],
        });
        const [note, translation] = loadPdfTranslations("C:/docs/Paper.PDF");
        expect(note?.name).toBe("复核 推导");
        expect(pdfAnnotationName(note)).toBe("复核 推导");
        expect(translation?.name).toBeUndefined();
    });

    it("keeps a translation's own fields when no name is present", () => {
        const saved = savePdfTranslations("/tmp/b.pdf", [
            { id: "t1", text: "Hello", translation: "你好", parts: [part] },
        ]);
        expect(saved[0]).toEqual({ id: "t1", text: "Hello", translation: "你好", parts: [part] });
        expect(pdfAnnotationName(saved[0])).toBe("Hello");
    });
});

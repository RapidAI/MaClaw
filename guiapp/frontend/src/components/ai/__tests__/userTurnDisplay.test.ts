import { describe, expect, it } from "vitest";
import * as fc from "fast-check";
import { stripUserTurnPromptArtifacts, userSelectedFilePathsInText, userTurnDisplayOptions, userTurnDisplayView } from "../userTurnDisplay";

const MARKER = "[用户选择的本地文件路径]";
const IMAGE_INSTRUCTION = "For image files, the host sends them directly to a vision-capable model when available. Analyze attached images first; do not re-capture them or use read_file on image bytes. Use OCR only for exact text when needed.";

// Arbitrary prose that can never accidentally contain a host marker or note.
const arbPlainText = fc.array(
    fc.constantFrom("帮", "看", "图", "总", "结", "文", "档", "修", "一", "下", "a", "b", "9", " ", "\n", "：", "，", "."),
    { minLength: 0, maxLength: 120 },
).map(chars => chars.join(""));

describe("userTurnDisplay", () => {
    it("drops the file-picker marker block and its routing instruction", () => {
        const text = `看看这张图\n\n${MARKER}\nC:\\Users\\demo\\shot.png\n${IMAGE_INSTRUCTION}`;
        expect(stripUserTurnPromptArtifacts(text)).toBe("看看这张图");
    });

    it("keeps ordinary user prose untouched", () => {
        const text = "重构 utils 目录，注意顺序\n第二行保留";
        expect(stripUserTurnPromptArtifacts(text)).toBe(text);
    });

    it("round-trips untouched prose byte-for-byte, blank-line runs included", () => {
        const text = "第一段\n\n\n\n第二段";
        expect(stripUserTurnPromptArtifacts(text)).toBe(text);
    });

    it("recovers attached files from the injected marker block", () => {
        const view = userTurnDisplayView(`分析一下\n\n${MARKER}\nC:\\Users\\demo\\shot.png\nD:\\docs\\paper.pdf\n${IMAGE_INSTRUCTION}`);
        expect(view.text).toBe("分析一下");
        expect(view.attachments.map(att => att.fileName)).toEqual(["shot.png", "paper.pdf"]);
        expect(view.attachments.map(att => att.isImage)).toEqual([true, false]);
        expect(view.attachments[0].filePath).toBe("C:\\Users\\demo\\shot.png");
        expect(view.attachments[0].extension).toBe(".png");
    });

    it("renders an image-only turn as previews with no text", () => {
        const view = userTurnDisplayView(`${MARKER}\nC:\\Users\\demo\\shot.png\n${IMAGE_INSTRUCTION}`);
        expect(view.text).toBe("");
        expect(view.attachments).toHaveLength(1);
        expect(view.attachments[0].isImage).toBe(true);
    });

    it("treats the historical marker as prompt text too", () => {
        const historical = "[之前选择的本地文件路径（仅供参考，非本次上传）]";
        const text = `继续刚才那个\n\n${historical}\nC:\\Users\\demo\\old.png`;
        expect(userSelectedFilePathsInText(text)).toEqual(["C:\\Users\\demo\\old.png"]);
        expect(stripUserTurnPromptArtifacts(text)).toBe("继续刚才那个");
    });

    it("removes host notes, OCR bodies and IM attachment fallbacks", () => {
        const text = [
            "这是什么图",
            "[Host note: image(s) are attached as vision input. Analyze the image directly before calling tools.]",
            "--- image_ocr: begin ---",
            "OCR 出来的原图文字不应出现在对话里",
            "--- image_ocr: end ---",
            "[附件: report.xlsx → 已保存到 /tmp/report.xlsx]",
            "[之前上传了 2 张图片]",
        ].join("\n");
        expect(stripUserTurnPromptArtifacts(text)).toBe("这是什么图");
    });

    it("keeps a voice transcript while dropping its host save-path note", () => {
        const text = "帮我把这段话整理成周报\n[语音: audio_1.m4a → 已转写并保存到 C:\\tmp\\a.wav]";
        expect(stripUserTurnPromptArtifacts(text)).toBe("帮我把这段话整理成周报");
    });

    it("drops auto-extract notices and their injected document bodies", () => {
        const text = [
            "总结这份合同",
            "[系统已自动解析文档正文 — 优先基于下列内容回答；仅当 truncated=true 或解析失败时，再 office(read_document) 分页]",
            "--- auto_extract: begin path=\"C:\\docs\\contract.pdf\" format=\"pdf\" ---",
            "合同正文不应出现在对话里",
            "--- auto_extract: end path=\"C:\\docs\\contract.pdf\" ---",
        ].join("\n");
        expect(stripUserTurnPromptArtifacts(text)).toBe("总结这份合同");
    });

    it("drops the historical OCR placeholder line", () => {
        expect(stripUserTurnPromptArtifacts("[图片的本地 OCR 文字内容（历史消息，正文已省略；如需请重新识别）]")).toBe("");
    });

    it("never leaks the marker block even when no path in it is recognized", () => {
        const text = `修一下\n\n${MARKER}\n（这里不是路径）`;
        expect(stripUserTurnPromptArtifacts(text)).toBe("修一下");
    });

    it("does not treat user prose that merely mentions a file as an attachment", () => {
        const text = "帮我看下 C 盘还有多少空间";
        const view = userTurnDisplayView(text);
        expect(view.text).toBe(text);
        expect(view.attachments).toHaveLength(0);
    });

    it("property: marker-free prose round-trips through the stripper", () => {
        fc.assert(fc.property(arbPlainText, (text) => {
            expect(stripUserTurnPromptArtifacts(text)).toBe(text.trim());
            expect(userTurnDisplayView(text).attachments).toHaveLength(0);
        }), { numRuns: 200 });
    });

    it("property: stripping is idempotent and the output never carries markers", () => {
        const arbMaybeHostText = fc.array(
            fc.constantFrom(
                "帮我看看", "\n", " ", "正文",
                MARKER, "\n",
                "C:\\Users\\demo\\shot.png", "\n",
                IMAGE_INSTRUCTION, "\n",
                "[Host note: image(s) are attached as vision input.]", "\n",
                "[附件: a.xlsx → 已保存到 /tmp/a.xlsx]", "\n",
                "--- image_ocr: begin ---", "\n", "原图文字", "\n", "--- image_ocr: end ---", "\n",
                "[系统已自动解析文档正文 — 优先基于下列内容回答]", "\n",
            ),
            { minLength: 0, maxLength: 24 },
        ).map(chars => chars.join(""));
        fc.assert(fc.property(arbMaybeHostText, (text) => {
            const once = stripUserTurnPromptArtifacts(text);
            expect(stripUserTurnPromptArtifacts(once)).toBe(once);
            expect(once).not.toContain(MARKER);
            expect(once).not.toContain("Analyze attached images first");
            expect(once).not.toContain("[Host note:");
            expect(userSelectedFilePathsInText(once)).toHaveLength(0);
        }), { numRuns: 200 });
    });

    it("prefers attachments already on the message over recovered paths", () => {
        const existing = [{ filePath: "C:\\Users\\demo\\shot.png", fileName: "shot.png", extension: ".png", isImage: true }];
        const view = userTurnDisplayView(`${MARKER}\nC:\\Users\\demo\\shot.png\n${IMAGE_INSTRUCTION}`, existing);
        expect(view.attachments).toHaveLength(1);
        expect(view.attachments[0].fileName).toBe("shot.png");
    });

    it("builds send options that keep the first bubble of a task clean", () => {
        const options = userTurnDisplayOptions(`帮我改版\n\n${MARKER}\nC:\\Users\\demo\\ui.png\n${IMAGE_INSTRUCTION}`);
        expect(options.displayText).toBe("帮我改版");
        expect(options.displayAttachments).toHaveLength(1);
        expect(options.displayAttachments?.[0].isImage).toBe(true);
        expect(userTurnDisplayOptions("普通一句话").displayAttachments).toBeUndefined();
    });
});

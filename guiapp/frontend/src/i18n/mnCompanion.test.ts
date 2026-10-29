import { afterEach, describe, expect, it } from "vitest";
import {
    COMPANION_NOTE_PROGRESS_ZH,
    companionFill,
    companionNoteAlias,
    companionNoteStepLabel,
    companionNoteTitle,
    companionPaperAlias,
    companionSay,
    setCompanionUiLang,
} from "./mnCompanion";

afterEach(() => {
    setCompanionUiLang("zh-Hans");
});

describe("companion Mongolian copy", () => {
    it("keeps Chinese when the language is zh-Hans", () => {
        expect(companionSay("zh-Hans", "发送")).toBe("发送");
        expect(companionFill("zh-Hans", "关闭 ${name}", { name: "a.pdf" })).toBe("关闭 a.pdf");
    });

    it("shows Khalkha for a catalog sentence and English when the key is missing", () => {
        setCompanionUiLang("mn");
        expect(companionSay("mn", "发送")).toBe("Илгээх");
        expect(companionSay("mn", "发送")).not.toMatch(/[\u3400-\u9fff]/);
        expect(companionSay("mn", "Not a companion sentence")).toBe("Not a companion sentence");
        expect(companionFill("mn", "关闭 ${name}", { name: "a.pdf" })).toBe("a.pdf-г хаах");
    });

    it("translates note progress for display and keeps the Chinese stage token", () => {
        expect(companionNoteTitle("mn", "正在整理笔记")).toBe("Тэмдэглэлийг эмхэлж байна");
        expect(companionNoteTitle("mn", "正在识别语音（第 2/4 段）")).toBe("Яриа таньж байна (2/4 хэсэг)");
        expect(companionNoteStepLabel("mn", "frames")).toBe("Кадр");
        expect(companionNoteStepLabel("mn", "watch")).toBe("Дүрс");
        expect(companionNoteStepLabel("zh-Hans", "frames")).toBe("画面");
        for (const phrase of COMPANION_NOTE_PROGRESS_ZH) {
            expect(companionNoteTitle("mn", phrase)).not.toMatch(/[\u3400-\u9fff]/);
            expect(companionNoteTitle("zh-Hans", phrase)).toBe(phrase);
        }
    });

    it("accepts English and Khalkha commands and leaves a typed Chinese paper label alone", () => {
        expect(companionNoteAlias("Generate Notes")).toBe(true);
        expect(companionNoteAlias("тэмдэглэл үүсгэх")).toBe(true);
        expect(companionNoteAlias("please generate notes")).toBe(false);
        expect(companionPaperAlias("read the paper")).toBe(true);
        expect(companionPaperAlias("өгүүллийг тайлбарлах")).toBe(true);
        expect(companionPaperAlias("论文解读")).toBe(false);
        expect(companionSay("mn", "这个文件不是论文，不能做论文解读")).toBe("Энэ файл өгүүлэл биш тул тайлбар хийх боломжгүй");
    });
});

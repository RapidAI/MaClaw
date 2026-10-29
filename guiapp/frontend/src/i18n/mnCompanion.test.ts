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

    it("translates host error sentences and keeps the Chinese password sentinel matchable", () => {
        expect(companionSay("zh-Hans", "需要分享密码")).toBe("需要分享密码");
        expect(companionSay("mn", "已取消生成笔记")).toBe("Тэмдэглэл үүсгэхийг цуцаллаа");
        expect(companionSay("mn", "暂时无法连接云端")).toBe("Үүлэнтэй түр холбогдож чадсангүй");
        expect(companionSay("mn", "分享密码不正确")).toBe("Хуваалцах нууц үг буруу.");
        expect(companionSay("mn", "云端群组无法读取（HTTP 502）")).toBe("Үүлэн бүлгийг уншиж чадсангүй (HTTP 502)");
        expect(companionSay("mn", "只有视频或音频可以生成笔记")).not.toMatch(/[\u3400-\u9fff]/);
        expect(companionSay("zh-Hans", "分享链接已过期")).toBe("分享链接已过期");
        expect(companionSay("mn", "分享链接已过期")).toBe("Хуваалцах холбоосын хугацаа дууссан");
        expect(companionSay("mn", "没有可用的中文字体，无法生成 PDF")).toBe("Хятад фонт алга тул PDF үүсгэх боломжгүй");
        expect(companionSay("mn", "未找到可嵌入 PDF 的中文字体，无法生成 PDF（最后一个候选不可用原因: missing）")).toBe("Суулгах Хятад фонт олдсонгүй тул PDF үүсгэх боломжгүй (сүүлийн нэр дэвшигч амжилтгүй: missing)");
        expect(companionSay("mn", "这份文档已经以相同的范围分享过了，请先删除原分享，或修改原分享")).not.toMatch(/[\u3400-\u9fff]/);
        expect(companionSay("zh-Hans", "登录后可以查看云端分享")).toBe("登录后可以查看云端分享");
        expect(companionSay("mn", "登录后可以查看云端分享")).toBe("Нэвтэрсний дараа үүлэн хуваалцалтыг харж болно");
        expect(companionSay("mn", "暂时无法连接云端，本机文档仍可查看")).toBe("Үүлэнтэй түр холбогдож чадсангүй. Энэ компьютерийн баримтыг харж болно.");
    });
});

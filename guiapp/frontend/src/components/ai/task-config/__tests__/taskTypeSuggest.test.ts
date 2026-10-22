import { describe, expect, it } from "vitest";
import { mismatchedTaskType, taskTypeMismatchCopy } from "../taskTypeSuggest";

describe("mismatchedTaskType", () => {
    it("returns the other type only when the reading is confident", () => {
        expect(mismatchedTaskType("chat", { suggested: "coding", confident: true })).toBe("coding");
        expect(mismatchedTaskType("coding", { suggested: "chat", confident: true })).toBe("chat");
        expect(mismatchedTaskType("chat", { suggested: "chat", confident: true })).toBeNull();
        expect(mismatchedTaskType("chat", { suggested: "coding", confident: false })).toBeNull();
        expect(mismatchedTaskType("coding", { suggested: "ssh", confident: true })).toBeNull();
        expect(mismatchedTaskType("chat", null)).toBeNull();
    });
});

describe("taskTypeMismatchCopy", () => {
    it("asks to switch and still offers to keep the current type", () => {
        const copy = taskTypeMismatchCopy({
            current: "chat",
            suggested: "coding",
            dropsCloud: false,
            isZh: true,
        });
        expect(copy.title).toBe("改为编程任务？");
        expect(copy.confirmText).toBe("改为编程");
        expect(copy.cancelText).toBe("保持会话");
        expect(copy.message).toContain("「编程」");
        expect(copy.message).not.toContain("云端");
    });

    it("says a confirmed coding switch leaves the cloud workspace", () => {
        const copy = taskTypeMismatchCopy({
            current: "chat",
            suggested: "coding",
            dropsCloud: true,
            isZh: true,
        });
        expect(copy.message).toContain("云端工作区");
        expect(copy.message).toContain("本地目录");
    });

    it("names chat as the suggested type in English", () => {
        const copy = taskTypeMismatchCopy({
            current: "coding",
            suggested: "chat",
            dropsCloud: false,
            isZh: false,
        });
        expect(copy.title).toBe("Switch this task to chat?");
        expect(copy.confirmText).toBe("Switch to chat");
        expect(copy.cancelText).toBe("Keep coding");
    });
});

// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest";
import {
    FILE_COMPANION_INPUT_ENTRY_MAX,
    FILE_COMPANION_INPUT_HISTORY_KEY,
    FILE_COMPANION_INPUT_HISTORY_MAX,
    fileCompanionInputHistory,
    rememberFileCompanionInput,
} from "./fileCompanionInputHistory";

const ASSISTANT_PROMPT_HISTORY_KEY = "ai-assistant-prompt-history";

beforeEach(() => {
    localStorage.clear();
});

describe("fileCompanionInputHistory", () => {
    it("stores companion questions and leaves the assistant history alone", () => {
        localStorage.setItem(ASSISTANT_PROMPT_HISTORY_KEY, JSON.stringify(["助手里的一句"]));
        rememberFileCompanionInput("  文件里有什么  ");
        rememberFileCompanionInput("文件里有什么");
        rememberFileCompanionInput("再总结一下");
        expect(fileCompanionInputHistory()).toEqual(["文件里有什么", "再总结一下"]);
        expect(JSON.parse(localStorage.getItem(ASSISTANT_PROMPT_HISTORY_KEY) || "[]")).toEqual(["助手里的一句"]);
        expect(localStorage.getItem(FILE_COMPANION_INPUT_HISTORY_KEY)).toContain("再总结一下");
    });

    it("drops a blank or oversized entry and keeps the newest hundred", () => {
        rememberFileCompanionInput("   ");
        rememberFileCompanionInput("x".repeat(FILE_COMPANION_INPUT_ENTRY_MAX + 1));
        expect(fileCompanionInputHistory()).toEqual([]);
        for (let i = 0; i < FILE_COMPANION_INPUT_HISTORY_MAX + 5; i += 1) {
            rememberFileCompanionInput(`问 ${i}`);
        }
        const stored = fileCompanionInputHistory();
        expect(stored).toHaveLength(FILE_COMPANION_INPUT_HISTORY_MAX);
        expect(stored[0]).toBe("问 5");
        expect(stored[stored.length - 1]).toBe(`问 ${FILE_COMPANION_INPUT_HISTORY_MAX + 4}`);
    });
});

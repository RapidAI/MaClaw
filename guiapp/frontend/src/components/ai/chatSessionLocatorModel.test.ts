import { describe, expect, it } from "vitest";
import { chatSessionLocatorLabel, chatSessionLocatorTurns, locatorWheelTargetsTranscript, retainLocatorUserMessages, userTurnScrollTop, type ChatSessionLocatorMessage } from "./chatSessionLocatorModel";

describe("chatSessionLocatorTurns", () => {
    it("keeps typed user commands, including short ones, and drops engine rows", () => {
        const turns = chatSessionLocatorTurns([
            { id: "goal", role: "user", content: "目标清单\n正在继续当前步骤" },
            { id: "host", role: "user", content: "〔远控〕继续", hostInitiated: true },
            { id: "ai", role: "assistant", content: "写好了" },
            { id: "ask", role: "user", content: "根据以下宠物规范，开发一个老虎\n再写实一点" },
            { id: "cont", role: "user", content: "继续" },
            { id: "letter", role: "user", content: "A" },
            { id: "blank", role: "user", content: "   " },
            { id: "again", role: "user", content: "重复 id 只留第一条" },
            { id: "again", role: "user", content: "这条丢掉" },
        ]);
        expect(turns.map((turn) => turn.label)).toEqual([
            "根据以下宠物规范，开发一个老虎 再写实一点",
            "继续",
            "A",
            "重复 id 只留第一条",
        ]);
        expect(turns.map((turn) => turn.id)).toEqual(["ask", "cont", "letter", "again"]);
    });

    it("uses the file name when the turn has no typed text", () => {
        const turns = chatSessionLocatorTurns([
            {
                id: "file",
                role: "user",
                content: "",
                attachments: [{ filePath: "D:/pets/tiger.png", fileName: "tiger.png", isImage: true }],
            },
        ]);
        expect(turns).toEqual([{ id: "file", label: "tiger.png" }]);
    });

    it("collapses a command onto one line", () => {
        expect(chatSessionLocatorLabel("  第一行\n\n第二行  ")).toBe("第一行 第二行");
    });
});

describe("retainLocatorUserMessages", () => {
    const ask: ChatSessionLocatorMessage = { id: "ask", role: "user", content: "继续" };

    it("keeps the same array while assistant text changes", () => {
        const cached = retainLocatorUserMessages([], [[ask, { id: "ai", role: "assistant", content: "a" }]]);
        const again = retainLocatorUserMessages(cached, [[
            ask,
            { id: "ai", role: "assistant", content: "ab" },
            { id: "ask", role: "user", content: "重复丢掉" },
        ]]);
        expect(again).toBe(cached);
        expect(again.map((message) => message.id)).toEqual(["ask"]);
    });

    it("replaces the array when a user command is edited, added, or removed", () => {
        const cached = retainLocatorUserMessages([], [[ask]]);
        const edited = retainLocatorUserMessages(cached, [[{ id: "ask", role: "user", content: "继续改" }]]);
        expect(edited).not.toBe(cached);
        expect(edited[0].content).toBe("继续改");
        const added = retainLocatorUserMessages(edited, [[edited[0], { id: "next", role: "user", content: "A" }]]);
        expect(added.map((message) => message.id)).toEqual(["ask", "next"]);
        const cleared = retainLocatorUserMessages(added, [[]]);
        expect(cleared).toEqual([]);
    });

    it("treats a replaced attachment as a new command line", () => {
        const cached = retainLocatorUserMessages([], [[ask]]);
        const withFile = retainLocatorUserMessages(cached, [[{
            ...ask,
            attachments: [{ filePath: "D:/pets/tiger.png", fileName: "tiger.png" }],
        }]]);
        expect(withFile).not.toBe(cached);
        expect(withFile[0].attachments?.[0].fileName).toBe("tiger.png");
    });
});

describe("locatorWheelTargetsTranscript", () => {
    it("gives the closed ticks and a wheel past either end to the transcript", () => {
        expect(locatorWheelTargetsTranscript(false, 800, 200, 0, -40)).toBe(true);
        expect(locatorWheelTargetsTranscript(true, 400, 80, 0, -40)).toBe(true);
        expect(locatorWheelTargetsTranscript(true, 400, 80, 320, 40)).toBe(true);
        expect(locatorWheelTargetsTranscript(true, 400, 80, 100, -40)).toBe(false);
        expect(locatorWheelTargetsTranscript(true, 400, 80, 100, 40)).toBe(false);
        expect(locatorWheelTargetsTranscript(true, 80, 80, 0, 40)).toBe(true);
        expect(locatorWheelTargetsTranscript(true, 400, 80, 0, 0)).toBe(false);
    });
});

describe("userTurnScrollTop", () => {
    it("places the row just under the transcript top", () => {
        expect(userTurnScrollTop(480, 80, 200)).toBe(588);
    });

    it("does not scroll above the transcript", () => {
        expect(userTurnScrollTop(10, 40, 0)).toBe(0);
    });
});

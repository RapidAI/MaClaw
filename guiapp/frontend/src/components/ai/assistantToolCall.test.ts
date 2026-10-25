import { describe, expect, it } from "vitest";
import { appendToolCallMarker, assistantTaskSettledIncomplete, completedAssistantSummary, formatAssistantToolFlip, isTranscriptToolCallText, parseAssistantToolStatus, sessionToolIconKind, splitAssistantToolCallContent, stripAssistantToolCallMarkers, transcriptAlreadyShowsToolCall } from "./assistantToolCall";

describe("assistant tool call transcript", () => {
    it("picks a status icon for session tools that match the coding trail", () => {
        expect(sessionToolIconKind({ name: "web_search", action: "搜索网页" })).toBe("search");
        expect(sessionToolIconKind({ name: "glob", action: "搜索文件" })).toBe("find");
        expect(sessionToolIconKind({ name: "search", action: "搜索文件" })).toBe("find");
        expect(sessionToolIconKind({ name: "查找", action: "查找" })).toBe("find");
        expect(sessionToolIconKind({ name: "bash", action: "执行命令" })).toBe("run");
        expect(sessionToolIconKind({ name: "read_file", action: "读取文件" })).toBe("read");
        expect(sessionToolIconKind({ name: "todo_write", action: "待办" })).toBe("todo");
        expect(sessionToolIconKind({ name: "delegate_task", action: "委派任务" })).toBe("agent");
        expect(sessionToolIconKind({ name: "code_search", action: "" })).toBe("search");
        expect(sessionToolIconKind({ name: "spawn_coding_agent", action: "" })).toBe("agent");
        expect(sessionToolIconKind({ name: "查找文件", action: "查找文件" })).toBe("find");
        expect(sessionToolIconKind({ name: "generate_pdf", action: "生成 PDF" })).toBeNull();
        expect(sessionToolIconKind({ name: "custom_note", action: "save todos" })).toBeNull();
        expect(sessionToolIconKind({ name: "待办", action: "待办" })).toBe("todo");
    });

    it("formats the live strip as the tool name and a short argument", () => {
        expect(formatAssistantToolFlip({ name: "web_search", action: "搜索网页", detail: "北京天气" })).toEqual({
            name: "web_search",
            arg: "北京天气",
            text: "web_search (北京天气)",
        });
        expect(formatAssistantToolFlip({
            name: "web_search",
            action: "搜索网页",
            detail: '{"query":"北京天气","limit":5}',
        }).text).toBe("web_search (北京天气)");
        expect(formatAssistantToolFlip({ name: "bash", action: "执行命令", detail: "" }).text).toBe("bash");
        expect(formatAssistantToolFlip({ name: "web_search", action: "搜索网页", detail: "web_search" }).text).toBe("web_search");
        expect(formatAssistantToolFlip({ name: "bash", action: "执行命令", detail: '{"limit":5}' }).text).toBe("bash");
        expect(formatAssistantToolFlip({ name: "web_search", action: "搜索网页", detail: '["北京天气"]' }).text).toBe("web_search (北京天气)");
        expect(formatAssistantToolFlip({
            name: "bash",
            action: "执行命令",
            detail: "curl -X POST https://api.example/v1/a-very-long-path-that-should-not-fill-the-strip",
        }).arg.endsWith("…")).toBe(true);
    });

    it("reads the tool name and arguments from a status card", () => {
        expect(parseAssistantToolStatus("工具 · 执行命令 (bash)\ncurl -X POST https://api.example")).toEqual({
            name: "bash",
            action: "执行命令",
            detail: "curl -X POST https://api.example",
        });
    });

    it("keeps an older card that only has a localized action", () => {
        expect(parseAssistantToolStatus("工具 · 访问网页\nhttps://weather")).toEqual({
            name: "访问网页",
            action: "访问网页",
            detail: "https://weather",
        });
    });

    it("hides the live tray copy only after the transcript has the same call", () => {
        const progress = "工具 · 执行命令 (bash)\nls";
        expect(transcriptAlreadyShowsToolCall([], progress)).toBe(false);
        expect(transcriptAlreadyShowsToolCall([
            { role: "assistant", toolCalls: [{ id: "1", name: "bash", action: "执行命令", detail: "ls" }] },
        ], progress)).toBe(true);
        expect(transcriptAlreadyShowsToolCall([
            { role: "assistant", toolCalls: [{ id: "1", name: "bash", action: "执行命令", detail: "ls" }] },
            { role: "assistant", toolCalls: [] },
        ], progress)).toBe(false);
    });

    it("leaves command-progress ticks out of the transcript", () => {
        expect(isTranscriptToolCallText("工具 · 命令进度\n仍在运行")).toBe(false);
        expect(isTranscriptToolCallText("工具 · 执行命令 (bash)\nls")).toBe(true);
        expect(isTranscriptToolCallText("〔进度〕仍在执行中")).toBe(false);
    });

    it("places a call after existing prose without stacking blank lines", () => {
        expect(appendToolCallMarker("先试一次\n\n", "call-1")).toBe("先试一次\n\n<!--maclaw-tool:call-1-->\n\n");
        expect(appendToolCallMarker("先试一次", "call-1")).toBe("先试一次\n\n<!--maclaw-tool:call-1-->\n\n");
        expect(stripAssistantToolCallMarkers("<!--maclaw-tool:a-->")).toBe("");
        expect(stripAssistantToolCallMarkers("x<!--maclaw-tool:b-->y")).toBe("xy");
    });

    it("splits prose around a marker and drops the marker from copy text", () => {
        const segments = splitAssistantToolCallContent(
            "先试一次\n\n<!--maclaw-tool:call-1-->\n\n然后换方式",
            [{ id: "call-1", name: "bash", action: "执行命令", detail: "ls" }],
        );
        expect(segments.map((segment) => segment.kind)).toEqual(["text", "tool", "text"]);
        expect(segments[0].text).toBe("先试一次");
        expect(segments[2].text).toBe("然后换方式");
        expect(segments[1].call?.name).toBe("bash");
        expect(stripAssistantToolCallMarkers("先试一次\n\n<!--maclaw-tool:call-1-->\n\n然后换方式")).toBe("先试一次\n\n然后换方式");
    });

    it("drops a marker-only body instead of showing the anchor", () => {
        expect(splitAssistantToolCallContent("\n\n<!--maclaw-tool:msg-1792198671957-29-->\n\n", undefined)).toEqual([]);
        expect(splitAssistantToolCallContent("<!--maclaw-tool:call-1-->\n\n<!--maclaw-tool:call-2-->", undefined)).toEqual([]);
        const placed = splitAssistantToolCallContent("\n\n<!--maclaw-tool:call-1-->\n\n", [
            { id: "call-1", name: "web_search", action: "搜索网络", detail: "北京天气 今天 预报" },
        ]);
        expect(placed.map((segment) => segment.kind)).toEqual(["tool"]);
        expect(placed[0].call?.name).toBe("web_search");
    });

    it("uses the terminal answer, and otherwise the prose after the last call", () => {
        const answer = "接口需要登录，这一步已经改走浏览器，文件已放在任务目录。";
        const transcript = `先用命令试一次。\n\n<!--maclaw-tool:call-1-->\n\n命令被拦住了。\n\n<!--maclaw-tool:call-2-->\n\n再试一次。`;
        expect(completedAssistantSummary(transcript, answer)).toBe(answer);
        expect(completedAssistantSummary(`先用命令试一次。\n\n<!--maclaw-tool:call-1-->\n\n${answer}`)).toBe(answer);
        expect(completedAssistantSummary("只有一句。\n\n<!--maclaw-tool:call-1-->\n\n短")).toBe("短");
        expect(completedAssistantSummary("先试一次。\n\n<!--maclaw-tool:call-1-->\n\n")).toBe("先试一次。");
        expect(completedAssistantSummary("没有调用的普通答复。")).toBe("没有调用的普通答复。");
        expect(assistantTaskSettledIncomplete("incomplete", "部署已经完成。")).toBe(true);
        expect(assistantTaskSettledIncomplete("completed", "正文里引用了任务已经应用户要求取消。")).toBe(false);
        expect(assistantTaskSettledIncomplete(undefined, "任务已经应用户要求取消")).toBe(true);
    });
});

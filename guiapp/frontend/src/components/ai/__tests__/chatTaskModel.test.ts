import { describe, expect, it } from "vitest";
import { appendChatTaskResultMessage, chatTasksToRestore, isChatTaskResultResponse, parseChatTaskEvent, upsertChatTaskCard, type ChatTaskMessage, type ChatTaskView } from "../chatTaskModel";

const task: ChatTaskView = {
    id: "ct-1",
    sessionKey: "desktop-user",
    title: "slides",
    status: "running",
    progress: "page 2",
};

describe("chat task transcript", () => {
    it("keeps a running task out of the foreground result slot and lands the result later", () => {
        const started = upsertChatTaskCard([] as ChatTaskMessage[], task);
        expect(started).toHaveLength(1);
        expect(started[0].id).toBe("chat-task-ct-1");
        expect(started[0].chatTask?.status).toBe("running");

        const updated = upsertChatTaskCard(started, { ...task, status: "done", resultText: "deck is ready" });
        expect(updated).toHaveLength(1);
        expect(updated[0].chatTask?.status).toBe("done");

        const withResult = appendChatTaskResultMessage(updated, {
            response_source: "chat_task_result",
            chat_task_kind: "task_result",
            chat_task_id: "ct-1",
            request_id: "chat-task-result-ct-1",
            session_key: "desktop-user",
            text: "deck is ready",
            local_file_paths: ["/tmp/out.pptx"],
        });
        expect(withResult.map(message => message.id)).toEqual(["chat-task-ct-1", "chat-task-result-ct-1"]);
        expect(withResult[1].content).toBe("deck is ready");
        expect(withResult[1].localFilePaths).toEqual(["/tmp/out.pptx"]);
        expect(appendChatTaskResultMessage(withResult, { request_id: "chat-task-result-ct-1", text: "again" })).toHaveLength(2);
        expect(appendChatTaskResultMessage(updated, { request_id: "chat-task-result-empty", text: "  " })).toEqual(updated);
    });

    it("recognizes a task result that has no foreground round", () => {
        expect(isChatTaskResultResponse({ response_source: "chat_task_result" })).toBe(true);
        expect(isChatTaskResultResponse({ request_id: "chat-task-result-ct-9" })).toBe(true);
        expect(isChatTaskResultResponse({ request_id: "desktop-ai-1", response_source: "agent" })).toBe(false);
    });

    it("restores open, failed, and interrupted cards without replaying finished ones", () => {
        const tasks: ChatTaskView[] = [
            { ...task, id: "live", status: "running" },
            { ...task, id: "old", status: "done" },
            { ...task, id: "stopped", status: "canceled" },
            { ...task, id: "crash", status: "interrupted", recovered: true },
            { ...task, id: "bad", status: "failed" },
            { ...task, id: "ask", status: "needs_input" },
        ];
        expect(chatTasksToRestore(tasks).map(item => item.id)).toEqual(["live", "crash", "bad", "ask"]);
    });

    it("parses the task event stream", () => {
        const parsed = parseChatTaskEvent(JSON.stringify({ id: "ct-2", session_key: "desktop-user", title: "notes", status: "interrupted", recovered: true }));
        expect(parsed).toMatchObject({ id: "ct-2", status: "interrupted", recovered: true });
    });
});

import { describe, expect, it } from "vitest";
import { appendChatTaskResultMessage, chatTaskDisplayTitle, chatTasksToRestore, formatChatTaskFailure, isChatTaskResultResponse, localizeHelperReport, parseChatTaskEvent, upsertChatTaskCard, type ChatTaskMessage, type ChatTaskView } from "../chatTaskModel";

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
        const filesOnly = appendChatTaskResultMessage([] as ChatTaskMessage[], {
            request_id: "chat-task-result-file",
            local_file_paths: ["/tmp/out.pptx"],
        });
        expect(filesOnly).toHaveLength(1);
        expect(filesOnly[0].content).toBe("");
        expect(filesOnly[0].localFilePaths).toEqual(["/tmp/out.pptx"]);
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

    it("localizes a failed helper report and keeps the returned reason", () => {
        const record = "helper_report_empty\nfile: none\ndetail:\n执行策略阻止了 PowerShell 下载";
        const zh = formatChatTaskFailure(record, "zh");
        expect(zh).toContain("助手报告没有结论，也没有文件路径。");
        expect(zh).toContain("结论第 1、2、3 行都是空的。");
        expect(zh).toContain("文件一行没有写出路径。");
        expect(zh).toContain("执行策略阻止了 PowerShell 下载");
        expect(zh).not.toContain("helper_report_empty");
        expect(formatChatTaskFailure(record, "zh-Hant")).toContain("助手報告沒有結論，也沒有檔案路徑。");
        expect(formatChatTaskFailure(record, "en")).toContain("The helper report had no conclusion and no file path.");
        expect(formatChatTaskFailure(record, "en")).toContain("The file line did not name a path.");
        expect(formatChatTaskFailure(record, "mn")).toContain("Туслахын тайланд дүгнэлт ч, файлын зам ч алга.");
        expect(formatChatTaskFailure("helper report has no conclusion and no path", "zh")).toContain("没有给出文件路径。");
        expect(formatChatTaskFailure("helper_report_empty is not a real record", "zh")).toBe("helper_report_empty is not a real record");
        expect(formatChatTaskFailure("chat_task_no_result", "zh-Hant")).toBe("任務結束了，但沒有返回結果。");
        expect(formatChatTaskFailure("chat_task_no_result\nignored", "en")).toBe("The task finished without a result.");
        const failed = upsertChatTaskCard([] as ChatTaskMessage[], {
            ...task,
            status: "failed",
            error: "helper_report_empty\nfile: none\ndetail:\n执行策略阻止了下载",
        });
        expect(failed[0].content).toBe("执行策略阻止了下载");
        expect(failed[0].content).not.toContain("helper_report_empty");
        expect(formatChatTaskFailure("下载失败：磁盘已满", "en")).toBe("下载失败：磁盘已满");
    });

    it("relabels a helper result card in the reader's language", () => {
        const card = "1. 安装包已下载\n2. 无\n3. 无\n文件：C:\\Users\\ma139\\Downloads\\ryzen.exe";
        expect(localizeHelperReport(card, "en")).toBe("What finished: 安装包已下载\nWhat is left: None\nWhat is blocked: None\nFile path: C:\\Users\\ma139\\Downloads\\ryzen.exe");
        expect(localizeHelperReport(card, "zh")).toContain("已完成：安装包已下载");
        expect(localizeHelperReport(card, "zh")).toContain("还剩：无");
        expect(localizeHelperReport(card, "zh-Hant")).toContain("檔案：C:\\Users\\ma139\\Downloads\\ryzen.exe");
        expect(localizeHelperReport(card, "mn")).toContain("Юу дууссан: 安装包已下载");
        expect(localizeHelperReport("已经改好。", "en")).toBe("已经改好。");
        const prose = "1. 先打开页面\n2. 再点下载\n3. 然后安装\n文件已经放在桌面";
        expect(localizeHelperReport(prose, "zh")).toBe(prose);
    });

    it("shows the job, not the dispatcher banner, as the card title", () => {
        expect(chatTaskDisplayTitle({ title: "工作目录：C:\\Users\\ma139\\Desktop\\作业。任务两步： 1) 生成 Word 讲稿文档" })).toBe("1) 生成 Word 讲稿文档");
        expect(chatTaskDisplayTitle({ title: "Working directory: C:\\work. Task steps: export the deck" })).toBe("export the deck");
        expect(chatTaskDisplayTitle({ title: "把word中的配图也放到ppt中适当的位置" })).toBe("把word中的配图也放到ppt中适当的位置");
        expect(chatTaskDisplayTitle({ title: "工作目录：C:\\a\\b。没有后续标题的目录行" })).toBe("没有后续标题的目录行");
        expect(chatTaskDisplayTitle({ title: "" })).toBe("");
    });
});

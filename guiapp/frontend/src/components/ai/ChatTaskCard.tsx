import { useState } from "react";
import type { ChatTaskView } from "./chatTaskModel";
import { chatTaskTerminal } from "./chatTaskModel";
import type { Theme } from "./aiAssistantPanelTheme";
import { tailText } from "../../i18n/mnTail";

function emitChatTaskAction(action: "resume" | "cancel" | "steer" | "answer", taskId: string, text = "") {
    window.dispatchEvent(new CustomEvent("ai-chat-task-action", { detail: { action, taskId, text } }));
}

function statusLabel(status: string, lang: string): string {
    switch (status) {
        case "queued": return tailText(lang, "Queued", "排队");
        case "running": return tailText(lang, "Running", "进行中");
        case "needs_input": return tailText(lang, "Needs your answer", "等待你的回答");
        case "done": return tailText(lang, "Done", "完成");
        case "failed": return tailText(lang, "Failed", "失败");
        case "canceled": return tailText(lang, "Canceled", "已取消");
        case "interrupted": return tailText(lang, "Was running", "上次中断");
        default: return status;
    }
}

export function ChatTaskCard({ task, theme, lang }: { task: ChatTaskView; theme: Theme; lang: string }) {
    const [draft, setDraft] = useState("");
    const interrupted = task.status === "interrupted";
    const open = !chatTaskTerminal(task.status) || interrupted;
    const waiting = task.status === "needs_input";
    return (
        <div
            data-testid={`chat-task-card-${task.id}`}
            data-status={task.status}
            style={{
                margin: "10px 0",
                maxWidth: 560,
                border: `1px solid ${theme.inputBarBorder}`,
                borderRadius: 12,
                background: theme.fieldBg,
                padding: "10px 12px",
                color: theme.text,
            }}
        >
            <div style={{ display: "flex", justifyContent: "space-between", gap: 8, alignItems: "center" }}>
                <strong style={{ fontSize: 13 }}>{task.title || tailText(lang, "This work", "这项工作")}</strong>
                <span style={{ fontSize: 11, color: theme.textMuted }}>{statusLabel(task.status, lang)}</span>
            </div>
            {task.status === "interrupted" && (
                <div style={{ marginTop: 6, fontSize: 12, color: theme.textMuted }}>
                    {task.progress || tailText(lang, "This was still running when the app stopped. It was not resumed.", "上次退出时这个任务还在跑，没有自动重开。")}
                </div>
            )}
            {(waiting || interrupted) && task.question && <div style={{ marginTop: 8, fontSize: 13, lineHeight: 1.45 }}>{task.question}</div>}
            {!waiting && task.progress && open && <div style={{ marginTop: 6, fontSize: 12, color: theme.textMuted }}>{task.progress}</div>}
            {task.error && task.status === "failed" && <div style={{ marginTop: 6, fontSize: 12 }}>{task.error}</div>}
            {!!task.artifactPaths?.length && (
                <div style={{ marginTop: 8, display: "flex", flexDirection: "column", gap: 2 }}>
                    {task.artifactPaths.map(path => <span key={path} style={{ fontSize: 12 }}>{path}</span>)}
                </div>
            )}
            {open && (
                <div style={{ display: "flex", gap: 6, marginTop: 8 }}>
                    <input
                        aria-label={waiting ? tailText(lang, "Answer", "回答") : tailText(lang, "Add a note", "补充")}
                        value={draft}
                        onChange={event => setDraft(event.target.value)}
                        onKeyDown={event => {
                            if (event.key !== "Enter" || event.shiftKey) return;
                            event.preventDefault();
                            const text = draft.trim();
                            if (!text) return;
                            emitChatTaskAction(waiting ? "answer" : interrupted ? "resume" : "steer", task.id, text);
                            setDraft("");
                        }}
                        placeholder={waiting ? tailText(lang, "Answer to continue", "回答后继续") : tailText(lang, "Add to this work", "补充后接着做")}
                        style={{ flex: 1, minWidth: 0, borderRadius: 8, border: `1px solid ${theme.inputBarBorder}`, background: theme.bg, color: theme.text, padding: "6px 8px", fontSize: 12 }}
                    />
                    {interrupted && (
                        <button
                            type="button"
                            onClick={() => {
                                emitChatTaskAction("resume", task.id, draft.trim());
                                setDraft("");
                            }}
                            style={{ borderRadius: 8, border: `1px solid ${theme.inputBarBorder}`, background: theme.bg, color: theme.text, padding: "6px 8px", fontSize: 12, cursor: "pointer" }}
                        >
                            {tailText(lang, "Resume task", "继续任务")}
                        </button>
                    )}
                    <button
                        type="button"
                        onClick={() => emitChatTaskAction("cancel", task.id)}
                        style={{ borderRadius: 8, border: `1px solid ${theme.inputBarBorder}`, background: "transparent", color: theme.text, padding: "6px 8px", fontSize: 12, cursor: "pointer" }}
                    >
                        {tailText(lang, "Cancel", "取消")}
                    </button>
                </div>
            )}
        </div>
    );
}

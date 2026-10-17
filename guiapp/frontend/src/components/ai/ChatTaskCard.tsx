import { useState } from "react";
import type { ChatTaskView } from "./chatTaskModel";
import { chatTaskTerminal } from "./chatTaskModel";
import type { Theme } from "./aiAssistantPanelTheme";

function emitChatTaskAction(action: "cancel" | "steer" | "answer", taskId: string, text = "") {
    window.dispatchEvent(new CustomEvent("ai-chat-task-action", { detail: { action, taskId, text } }));
}

function statusLabel(status: string, lang: string): string {
    const zh = lang.startsWith("zh");
    switch (status) {
        case "queued": return zh ? "排队" : "Queued";
        case "running": return zh ? "进行中" : "Running";
        case "needs_input": return zh ? "等待你的回答" : "Needs your answer";
        case "done": return zh ? "完成" : "Done";
        case "failed": return zh ? "失败" : "Failed";
        case "canceled": return zh ? "已取消" : "Canceled";
        case "interrupted": return zh ? "上次中断" : "Was running";
        default: return status;
    }
}

export function ChatTaskCard({ task, theme, lang }: { task: ChatTaskView; theme: Theme; lang: string }) {
    const [draft, setDraft] = useState("");
    const zh = lang.startsWith("zh");
    const open = !chatTaskTerminal(task.status);
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
                <strong style={{ fontSize: 13 }}>{task.title || (zh ? "这项工作" : "This work")}</strong>
                <span style={{ fontSize: 11, color: theme.textMuted }}>{statusLabel(task.status, lang)}</span>
            </div>
            {task.status === "interrupted" && (
                <div style={{ marginTop: 6, fontSize: 12, color: theme.textMuted }}>
                    {task.progress || (zh ? "上次退出时这个任务还在跑，没有自动重开。" : "This was still running when the app stopped. It was not resumed.")}
                </div>
            )}
            {waiting && task.question && <div style={{ marginTop: 8, fontSize: 13, lineHeight: 1.45 }}>{task.question}</div>}
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
                        aria-label={waiting ? (zh ? "回答" : "Answer") : (zh ? "补充" : "Add a note")}
                        value={draft}
                        onChange={event => setDraft(event.target.value)}
                        onKeyDown={event => {
                            if (event.key !== "Enter" || event.shiftKey) return;
                            event.preventDefault();
                            const text = draft.trim();
                            if (!text) return;
                            emitChatTaskAction(waiting ? "answer" : "steer", task.id, text);
                            setDraft("");
                        }}
                        placeholder={waiting ? (zh ? "回答后继续" : "Answer to continue") : (zh ? "补充后接着做" : "Add to this work")}
                        style={{ flex: 1, minWidth: 0, borderRadius: 8, border: `1px solid ${theme.inputBarBorder}`, background: theme.bg, color: theme.text, padding: "6px 8px", fontSize: 12 }}
                    />
                    <button
                        type="button"
                        onClick={() => emitChatTaskAction("cancel", task.id)}
                        style={{ borderRadius: 8, border: `1px solid ${theme.inputBarBorder}`, background: "transparent", color: theme.text, padding: "6px 8px", fontSize: 12, cursor: "pointer" }}
                    >
                        {zh ? "取消" : "Cancel"}
                    </button>
                </div>
            )}
        </div>
    );
}

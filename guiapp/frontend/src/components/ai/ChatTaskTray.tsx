import type { ChatTaskView } from "./chatTaskModel";
import { chatTaskTerminal } from "./chatTaskModel";
import type { Theme } from "./aiAssistantPanelTheme";
import { tailText } from "../../i18n/mnTail";

export function ChatTaskTray({
    tasks,
    canDispatch,
    lang,
    theme,
    onRunInBackground,
    onCancel,
}: {
    tasks: ChatTaskView[];
    canDispatch: boolean;
    lang: string;
    theme: Theme;
    onRunInBackground: () => void;
    onCancel: (taskID: string) => void;
}) {
    const open = tasks.filter(task => !chatTaskTerminal(task.status));
    if (!canDispatch && open.length === 0) return null;
    return (
        <div data-testid="chat-task-tray" style={{ flexShrink: 0, display: "flex", flexDirection: "column", gap: 6, padding: "6px 10px 0", background: theme.bg }}>
            <div style={{ display: "flex", justifyContent: "space-between", gap: 8, alignItems: "center" }}>
                <span style={{ fontSize: 12, color: theme.textMuted }}>
                    {open.length > 0
                        ? tailText(lang, `${open.length} still going`, `${open.length} 项还在做`)
                        : tailText(lang, "Longer work can keep going. You can keep chatting.", "比较长的事先做着，这里可以继续聊")}
                </span>
                <button
                    type="button"
                    data-testid="run-in-background"
                    disabled={!canDispatch}
                    onClick={onRunInBackground}
                    style={{
                        borderRadius: 999,
                        border: `1px solid ${theme.inputBarBorder}`,
                        background: canDispatch ? theme.fieldBg : "transparent",
                        color: theme.text,
                        padding: "4px 10px",
                        fontSize: 12,
                        cursor: canDispatch ? "pointer" : "default",
                        opacity: canDispatch ? 1 : 0.45,
                    }}
                >
                    {tailText(lang, "Keep this going", "先做着")}
                </button>
            </div>
            {open.length > 0 && (
                <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
                    {open.map(task => (
                        <span key={task.id} data-testid={`chat-task-tray-${task.id}`} style={{ display: "inline-flex", alignItems: "center", gap: 6, borderRadius: 999, border: `1px solid ${theme.inputBarBorder}`, padding: "3px 8px", fontSize: 12 }}>
                            <span>{task.title || tailText(lang, "This work", "这项工作")}</span>
                            <button type="button" onClick={() => onCancel(task.id)} style={{ border: "none", background: "transparent", color: theme.textMuted, cursor: "pointer", padding: 0 }}>
                                {tailText(lang, "Cancel", "取消")}
                            </button>
                        </span>
                    ))}
                </div>
            )}
        </div>
    );
}

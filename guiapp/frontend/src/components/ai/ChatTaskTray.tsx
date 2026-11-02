import type { CSSProperties } from "react";
import type { ChatTaskView } from "./chatTaskModel";
import { chatTaskDisplayTitle, chatTaskStatusTone, chatTaskTerminal, chatTaskToneColor } from "./chatTaskModel";
import type { Theme } from "./aiAssistantPanelTheme";
import { tailText } from "../../i18n/mnTail";

const styles = {
    tray(theme: Theme): CSSProperties {
        return {
            flexShrink: 0,
            display: "flex",
            flexDirection: "column",
            gap: 6,
            padding: "8px 10px 0",
            background: theme.bg,
            borderTop: `1px solid ${theme.divider}`,
        };
    },
    topRow: {
        display: "flex",
        justifyContent: "space-between",
        alignItems: "center",
        gap: 8,
    } satisfies CSSProperties,
    countLabel(theme: Theme): CSSProperties {
        return {
            display: "inline-flex",
            alignItems: "center",
            gap: 7,
            fontSize: 12,
            fontWeight: 600,
            color: theme.textMuted,
        };
    },
    countDot(theme: Theme): CSSProperties {
        return {
            width: 7,
            height: 7,
            borderRadius: "50%",
            background: chatTaskToneColor(theme, "accent"),
            flexShrink: 0,
        };
    },
    chipRow: {
        display: "flex",
        flexWrap: "wrap",
        gap: 6,
    } satisfies CSSProperties,
    chip(theme: Theme): CSSProperties {
        return {
            display: "inline-flex",
            alignItems: "center",
            gap: 6,
            borderRadius: 999,
            border: `1px solid ${theme.inputBarBorder}`,
            background: theme.fieldBg,
            padding: "3px 5px 3px 9px",
            fontSize: 12,
            color: theme.text,
        };
    },
    chipDot(theme: Theme, status: string): CSSProperties {
        return {
            width: 6,
            height: 6,
            borderRadius: "50%",
            background: chatTaskToneColor(theme, chatTaskStatusTone(status)),
            flexShrink: 0,
        };
    },
    chipTitle: {
        maxWidth: 240,
        whiteSpace: "nowrap",
        overflow: "hidden",
        textOverflow: "ellipsis",
    } satisfies CSSProperties,
    chipCancel(): CSSProperties {
        return {
            width: 18,
            height: 18,
            borderRadius: "50%",
            border: "none",
            fontSize: 13,
            lineHeight: 1,
            display: "inline-flex",
            alignItems: "center",
            justifyContent: "center",
            cursor: "pointer",
            flexShrink: 0,
        };
    },
    keepGoing(theme: Theme): CSSProperties {
        return {
            borderRadius: 999,
            color: theme.btnColor || "#2563eb",
            padding: "4px 12px",
            fontSize: 12,
            fontWeight: 600,
        };
    },
};

function chipToneClass(status: string): string | undefined {
    return status === "running" ? "chat-task-dot-live" : undefined;
}

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
        <div data-testid="chat-task-tray" style={styles.tray(theme)}>
            <div style={styles.topRow}>
                <span style={styles.countLabel(theme)}>
                    {open.length > 0 && <span className="chat-task-dot-live" style={styles.countDot(theme)} />}
                    {open.length > 0
                        ? tailText(lang, `${open.length} still going`, `${open.length} 项还在做`)
                        : tailText(lang, "Longer work can keep going. You can keep chatting.", "比较长的事先做着，这里可以继续聊")}
                </span>
                <button
                    type="button"
                    data-testid="run-in-background"
                    className="chat-task-primary"
                    disabled={!canDispatch}
                    onClick={onRunInBackground}
                    style={styles.keepGoing(theme)}
                >
                    {tailText(lang, "Keep this going", "先做着")}
                </button>
            </div>
            {open.length > 0 && (
                <div style={styles.chipRow}>
                    {open.map(task => {
                        const title = chatTaskDisplayTitle(task) || tailText(lang, "This work", "这项工作");
                        return (
                            <span
                                key={task.id}
                                data-testid={`chat-task-tray-${task.id}`}
                                style={styles.chip(theme)}
                                title={task.title || title}
                            >
                                <span className={chipToneClass(task.status)} style={styles.chipDot(theme, task.status)} />
                                <span style={styles.chipTitle}>{title}</span>
                                <button
                                    type="button"
                                    className="chat-task-chip-x"
                                    aria-label={tailText(lang, "Cancel", "取消")}
                                    title={tailText(lang, "Cancel", "取消")}
                                    onClick={() => onCancel(task.id)}
                                    style={styles.chipCancel()}
                                >
                                    ×
                                </button>
                            </span>
                        );
                    })}
                </div>
            )}
        </div>
    );
}

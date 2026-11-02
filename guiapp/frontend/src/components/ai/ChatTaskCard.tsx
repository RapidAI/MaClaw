import { useState } from "react";
import type { CSSProperties } from "react";
import type { ChatTaskView } from "./chatTaskModel";
import { chatTaskDisplayTitle, chatTaskStatusTone, chatTaskTerminal, chatTaskToneColor } from "./chatTaskModel";
import type { ChatTaskTone } from "./chatTaskModel";
import type { Theme } from "./aiAssistantPanelTheme";
import { tailText } from "../../i18n/mnTail";
import { assistantLiveActivityLabel, parseLiveActivityFromProgressText } from "./assistantLiveActivity";
import { stripLeadingEmojiCluster } from "./aiAssistantProgressUtils";

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

const MONO_FONT = "Consolas, 'SFMono-Regular', Menlo, monospace";

const IM_TOOL_LABEL_PREFIX = /^(?:工具\s*[·•]\s*|Tool\s*[·•]\s*)/i;
const TOOL_NAME_SUFFIX = /\s*\(([A-Za-z][A-Za-z0-9_-]*)\)\s*$/;

/** Progress line → friendly action label + tool chip + optional detail lines. */
function parseTaskProgress(progress: string, lang: string): { label: string; tool: string; detail: string } {
    const lines = String(progress || "").split(/\r?\n/);
    const first = stripLeadingEmojiCluster(lines[0] || "").trim();
    const detail = lines.slice(1).map(line => line.trim()).filter(Boolean).join("\n");
    if (!first || first === "__heartbeat__") return { label: "", tool: "", detail };
    const tool = first.match(TOOL_NAME_SUFFIX)?.[1] || "";
    const kind = parseLiveActivityFromProgressText(first);
    if (kind) {
        // The chip carries the tool id; keep the verb short so it never reads twice.
        return { label: assistantLiveActivityLabel(kind, lang), tool, detail };
    }
    const stripped = first.replace(IM_TOOL_LABEL_PREFIX, "").replace(TOOL_NAME_SUFFIX, "").trim();
    return { label: stripped || first, tool, detail };
}

function baseName(path: string): string {
    return path.split(/[\\/]/).pop() || path;
}

const styles = {
    card(theme: Theme, tone: ChatTaskTone): CSSProperties {
        const color = chatTaskToneColor(theme, tone);
        const borderColor = tone === "attention"
            ? `color-mix(in srgb, ${color} 45%, transparent)`
            : tone === "bad"
                ? `color-mix(in srgb, ${color} 35%, transparent)`
                : theme.inputBarBorder;
        const background = tone === "attention"
            ? `color-mix(in srgb, ${color} 5%, ${theme.fieldBg})`
            : theme.fieldBg;
        return {
            margin: "10px 0",
            maxWidth: 560,
            border: `1px solid ${borderColor}`,
            borderRadius: 14,
            background,
            boxShadow: theme.isDark ? "none" : "0 1px 2px rgba(15, 23, 42, 0.05), 0 4px 14px rgba(15, 23, 42, 0.04)",
            padding: "11px 14px 12px",
            color: theme.text,
        };
    },
    headerRow: {
        display: "flex",
        justifyContent: "space-between",
        alignItems: "center",
        gap: 8,
    } satisfies CSSProperties,
    kicker(theme: Theme): CSSProperties {
        return {
            fontSize: 11,
            fontWeight: 600,
            letterSpacing: "0.08em",
            textTransform: "uppercase",
            color: theme.textMuted,
        };
    },
    pill(theme: Theme, tone: ChatTaskTone): CSSProperties {
        const color = chatTaskToneColor(theme, tone);
        return {
            display: "inline-flex",
            alignItems: "center",
            gap: 5,
            borderRadius: 999,
            padding: "2px 9px",
            fontSize: 11,
            fontWeight: 600,
            color,
            background: `color-mix(in srgb, ${color} 12%, transparent)`,
            whiteSpace: "nowrap",
        };
    },
    dot(): CSSProperties {
        return {
            width: 6,
            height: 6,
            borderRadius: "50%",
            background: "currentColor",
            flexShrink: 0,
        };
    },
    title(theme: Theme): CSSProperties {
        return {
            marginTop: 8,
            fontSize: 13.5,
            fontWeight: 600,
            lineHeight: 1.45,
            color: theme.text,
            display: "-webkit-box",
            WebkitLineClamp: 2,
            WebkitBoxOrient: "vertical",
            overflow: "hidden",
            overflowWrap: "anywhere",
        };
    },
    spinner(color: string): CSSProperties {
        return {
            width: 12,
            height: 12,
            borderRadius: "50%",
            flexShrink: 0,
            border: `2px solid color-mix(in srgb, ${color} 25%, transparent)`,
            borderTopColor: color,
        };
    },
    progressRow(theme: Theme): CSSProperties {
        return {
            marginTop: 8,
            display: "flex",
            alignItems: "center",
            gap: 7,
            fontSize: 12,
            color: theme.textMuted,
        };
    },
    toolChip(theme: Theme): CSSProperties {
        return {
            fontFamily: MONO_FONT,
            fontSize: 11,
            lineHeight: "16px",
            padding: "0 6px",
            borderRadius: 6,
            background: theme.codeBg,
            color: theme.codeText,
            whiteSpace: "nowrap",
        };
    },
    progressDetail(theme: Theme): CSSProperties {
        return {
            marginTop: 4,
            marginLeft: 19,
            fontFamily: MONO_FONT,
            fontSize: 11,
            color: theme.textMuted,
            whiteSpace: "nowrap",
            overflow: "hidden",
            textOverflow: "ellipsis",
        };
    },
    question(theme: Theme, color: string): CSSProperties {
        return {
            marginTop: 8,
            padding: "7px 10px",
            borderLeft: `3px solid ${color}`,
            borderRadius: 6,
            background: `color-mix(in srgb, ${color} 10%, transparent)`,
            fontSize: 13,
            lineHeight: 1.5,
            whiteSpace: "pre-line",
        };
    },
    errorBlock(theme: Theme): CSSProperties {
        return {
            marginTop: 8,
            padding: "7px 10px",
            borderRadius: 8,
            background: theme.errorBg,
            color: theme.errorText,
            fontSize: 12.5,
            lineHeight: 1.5,
            whiteSpace: "pre-line",
        };
    },
    mutedNote(theme: Theme): CSSProperties {
        return {
            marginTop: 6,
            fontSize: 12,
            lineHeight: 1.5,
            color: theme.textMuted,
            display: "-webkit-box",
            WebkitLineClamp: 3,
            WebkitBoxOrient: "vertical",
            overflow: "hidden",
            overflowWrap: "anywhere",
        };
    },
    artifactRow: {
        marginTop: 8,
        display: "flex",
        flexWrap: "wrap",
        gap: 6,
    } satisfies CSSProperties,
    artifactChip(theme: Theme): CSSProperties {
        return {
            display: "inline-flex",
            alignItems: "center",
            gap: 5,
            maxWidth: "100%",
            borderRadius: 999,
            border: `1px solid ${theme.inputBarBorder}`,
            background: theme.bg,
            padding: "2px 9px",
            fontFamily: MONO_FONT,
            fontSize: 11,
            color: theme.text,
            whiteSpace: "nowrap",
            overflow: "hidden",
            textOverflow: "ellipsis",
        };
    },
    footer(): CSSProperties {
        return {
            marginTop: 10,
            display: "flex",
            alignItems: "center",
            gap: 6,
        };
    },
    footerInput(theme: Theme, waiting: boolean, color: string): CSSProperties {
        return {
            flex: 1,
            minWidth: 0,
            height: 30,
            borderRadius: 9,
            border: `1px solid ${waiting ? `color-mix(in srgb, ${color} 50%, transparent)` : theme.inputBarBorder}`,
            background: theme.bg,
            color: theme.text,
            padding: "0 10px",
            fontSize: 12.5,
        };
    },
    ghostBtn(theme: Theme): CSSProperties {
        return {
            flexShrink: 0,
            borderRadius: 8,
            border: "none",
            color: theme.textMuted,
            padding: "6px 10px",
            fontSize: 12,
            cursor: "pointer",
        };
    },
    outlineBtn(theme: Theme): CSSProperties {
        return {
            flexShrink: 0,
            borderRadius: 8,
            border: `1px solid ${theme.inputBarBorder}`,
            color: theme.text,
            padding: "5px 10px",
            fontSize: 12,
            cursor: "pointer",
        };
    },
};

export function ChatTaskCard({ task, theme, lang }: { task: ChatTaskView; theme: Theme; lang: string }) {
    const [draft, setDraft] = useState("");
    const interrupted = task.status === "interrupted";
    const open = !chatTaskTerminal(task.status) || interrupted;
    const waiting = task.status === "needs_input";
    const tone = chatTaskStatusTone(task.status);
    const color = chatTaskToneColor(theme, tone);
    const displayTitle = chatTaskDisplayTitle(task) || tailText(lang, "This work", "这项工作");
    const live = task.status === "running" || task.status === "queued";
    const progress = live && task.progress ? parseTaskProgress(task.progress, lang) : null;
    return (
        <div
            data-testid={`chat-task-card-${task.id}`}
            data-status={task.status}
            style={styles.card(theme, tone)}
        >
            <div style={styles.headerRow}>
                <span style={styles.kicker(theme)}>{tailText(lang, "Background task", "后台任务")}</span>
                <span style={styles.pill(theme, tone)}>
                    <span className={live ? "chat-task-dot-live" : undefined} style={styles.dot()} />
                    {statusLabel(task.status, lang)}
                </span>
            </div>
            <div style={styles.title(theme)} title={task.title || displayTitle}>{displayTitle}</div>
            {task.status === "interrupted" && (
                <div style={styles.mutedNote(theme)}>
                    {task.progress || tailText(lang, "This was still running when the app stopped. It was not resumed.", "上次退出时这个任务还在跑，没有自动重开。")}
                </div>
            )}
            {waiting && task.question && <div style={styles.question(theme, color)}>{task.question}</div>}
            {progress && progress.label && (
                <div style={styles.progressRow(theme)}>
                    <span className="chat-task-spin" style={styles.spinner(color)} />
                    <span>{progress.label}</span>
                    {progress.tool && <span style={styles.toolChip(theme)}>{progress.tool}</span>}
                </div>
            )}
            {progress && progress.detail && <div style={styles.progressDetail(theme)} title={progress.detail}>{progress.detail}</div>}
            {task.error && task.status === "failed" && <div style={styles.errorBlock(theme)}>{task.error}</div>}
            {!!task.artifactPaths?.length && (
                <div style={styles.artifactRow}>
                    {task.artifactPaths.map(path => (
                        <span key={path} style={styles.artifactChip(theme)} title={path}>{baseName(path)}</span>
                    ))}
                </div>
            )}
            {open && (
                <div style={styles.footer()}>
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
                        style={styles.footerInput(theme, waiting, color)}
                    />
                    {interrupted && (
                        <button
                            type="button"
                            className="chat-task-ghost"
                            onClick={() => {
                                emitChatTaskAction("resume", task.id, draft.trim());
                                setDraft("");
                            }}
                            style={styles.outlineBtn(theme)}
                        >
                            {tailText(lang, "Resume task", "继续任务")}
                        </button>
                    )}
                    <button
                        type="button"
                        className="chat-task-ghost"
                        onClick={() => emitChatTaskAction("cancel", task.id)}
                        style={styles.ghostBtn(theme)}
                    >
                        {tailText(lang, "Cancel", "取消")}
                    </button>
                </div>
            )}
        </div>
    );
}

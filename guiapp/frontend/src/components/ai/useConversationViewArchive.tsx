import { useEffect, useMemo, useRef, useState } from "react";
import type { ReactNode } from "react";
import { ConversationViewArchiveCount, LoadAIAssistantUIArchive, LoadProjectTabConversationArchive } from "../../../wailsjs/go/main/App";
import type { ChatMessage } from "./useAIAssistant";
import type { Theme } from "./aiAssistantPanelTheme";
import { localizeText } from "./aiAssistantI18n";
import { messageIsLocalSession } from "./aiAssistantPanelSessionUtils";
import { splitConversationView } from "./conversationViewWindow";

interface ConversationViewArchiveOptions {
    messages: ChatMessage[];
    /** "desktop" or "tab:<tabId>". */
    scope: string;
    enabled: boolean;
    lang: string;
    theme: Theme;
}

function asChatMessages(raw: unknown): ChatMessage[] {
    if (!Array.isArray(raw)) return [];
    return raw.filter((message) => {
        if (!message || typeof message !== "object") return false;
        const row = message as ChatMessage;
        return !!row.id && !!row.role;
    }) as ChatMessage[];
}

function archiveText(message: ChatMessage): string {
    const content = typeof message.content === "string" ? message.content.trim() : "";
    if (content) return content;
    const reasoning = typeof message.reasoning === "string" ? message.reasoning.trim() : "";
    if (reasoning) return reasoning;
    return "";
}

export function useConversationViewArchive({ messages, scope, enabled, lang, theme: t }: ConversationViewArchiveOptions): {
    visibleMessages: ChatMessage[];
    earlierCount: number;
    banner: ReactNode;
    dialog: ReactNode;
} {
    const { visible, earlier } = useMemo(() => splitConversationView(messages), [messages]);
    const [serverCount, setServerCount] = useState(0);
    const [open, setOpen] = useState(false);
    const [loading, setLoading] = useState(false);
    const [rows, setRows] = useState<ChatMessage[]>([]);

    useEffect(() => {
        if (!enabled || !scope) {
            setServerCount(0);
            return;
        }
        let cancelled = false;
        const loadCount = typeof ConversationViewArchiveCount === "function"
            ? ConversationViewArchiveCount(scope)
            : Promise.resolve(0);
        void Promise.resolve(loadCount).then((count) => {
            if (!cancelled) setServerCount(Math.max(0, Number(count) || 0));
        }).catch(() => {
            if (!cancelled) setServerCount(0);
        });
        return () => {
            cancelled = true;
        };
    }, [enabled, scope, messages.length]);

    const seenScope = useRef(scope);
    useEffect(() => {
        if (seenScope.current === scope) return;
        seenScope.current = scope;
        setOpen(false);
        setRows([]);
    }, [scope]);

    useEffect(() => {
        if (!open) return;
        const onKey = (event: KeyboardEvent) => {
            if (event.key === "Escape") setOpen(false);
        };
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [open]);

    const earlierCount = Math.max(earlier.length, serverCount);
    const openArchive = () => {
        setOpen(true);
        setLoading(true);
        const load = scope.startsWith("tab:")
            ? (typeof LoadProjectTabConversationArchive === "function"
                ? LoadProjectTabConversationArchive(scope.slice(4))
                : Promise.resolve([]))
            : (typeof LoadAIAssistantUIArchive === "function"
                ? LoadAIAssistantUIArchive()
                : Promise.resolve([]));
        void Promise.resolve(load).then((remote) => {
            const known = new Set(messages.map((message) => message.id));
            const older = asChatMessages(remote).filter((message) => {
                if (scope === "desktop" && !messageIsLocalSession(message)) return false;
                return !message.id || !known.has(message.id);
            });
            setRows([...older, ...earlier]);
        }).catch(() => {
            setRows(earlier);
        }).finally(() => setLoading(false));
    };

    const banner = earlierCount > 0 ? (
        <button
            type="button"
            data-testid="conversation-archive-banner"
            onClick={openArchive}
            style={{
                display: "block",
                margin: "0 auto 12px",
                padding: "4px 12px",
                borderRadius: "999px",
                border: `1px solid ${t.titleBarBorder}`,
                background: t.fieldBg,
                color: t.textMuted,
                fontSize: "12px",
                lineHeight: 1.4,
                cursor: "pointer",
            }}
        >
            {localizeText(lang, `View ${earlierCount} earlier messages`, `查看更早的 ${earlierCount} 条对话`, `查看更早的 ${earlierCount} 條對話`)}
        </button>
    ) : null;

    const dialog = open ? (
        <div
            role="dialog"
            aria-modal="true"
            data-testid="conversation-archive-dialog"
            aria-label={localizeText(lang, "Earlier messages", "更早的对话", "更早的對話")}
            style={{
                position: "fixed",
                inset: "48px 24px",
                zIndex: 40,
                display: "flex",
                flexDirection: "column",
                minHeight: 0,
                background: t.bg,
                color: t.text,
                border: `1px solid ${t.titleBarBorder}`,
                borderRadius: "12px",
                boxShadow: "0 16px 48px rgba(0,0,0,0.18)",
            }}
        >
            <div style={{ display: "flex", alignItems: "center", gap: "8px", padding: "10px 14px", borderBottom: `1px solid ${t.titleBarBorder}` }}>
                <span style={{ flex: 1, fontSize: "13px", fontWeight: 600 }}>
                    {localizeText(lang, "Earlier messages", "更早的对话", "更早的對話")}
                </span>
                <button
                    type="button"
                    data-testid="conversation-archive-close"
                    onClick={() => setOpen(false)}
                    style={{ background: "none", border: "none", color: t.textMuted, cursor: "pointer", fontSize: "12px" }}
                >
                    {localizeText(lang, "Close", "关闭", "關閉")}
                </button>
            </div>
            <div style={{ flex: 1, minHeight: 0, overflowY: "auto", padding: "12px 14px" }}>
                {loading ? (
                    <div style={{ color: t.textMuted, fontSize: "12px" }}>{localizeText(lang, "Loading...", "加载中...", "載入中...")}</div>
                ) : rows.length === 0 ? (
                    <div style={{ color: t.textMuted, fontSize: "12px" }}>{localizeText(lang, "No earlier messages", "没有更早的对话", "沒有更早的對話")}</div>
                ) : rows.map((message) => {
                    const text = archiveText(message);
                    return (
                        <div key={message.id} data-testid="conversation-archive-message" style={{ marginBottom: "12px" }}>
                            <div style={{ fontSize: "11px", color: t.textMuted, marginBottom: "2px" }}>
                                {message.role === "user"
                                    ? localizeText(lang, "You", "你", "你")
                                    : localizeText(lang, "Assistant", "助手", "助手")}
                            </div>
                            <div style={{ fontSize: "13px", lineHeight: 1.55, whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>
                                {text || localizeText(lang, "(no text)", "（无文本）", "（無文本）")}
                            </div>
                        </div>
                    );
                })}
            </div>
        </div>
    ) : null;

    return { visibleMessages: visible, earlierCount, banner, dialog };
}

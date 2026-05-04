import type { ChatMessage } from "./useAIAssistant";
import { renderInlineMarkdown } from "./aiAssistantMarkdown";
import type { Theme } from "./aiAssistantPanelTheme";

interface AssistantPinnedNewsCardsProps {
    messages: ChatMessage[];
    theme: Theme;
}

export function AssistantPinnedNewsCards({ messages, theme: t }: AssistantPinnedNewsCardsProps) {
    if (messages.length === 0) return null;
    return (
        <div style={{
            display: "grid",
            gridTemplateColumns: messages.length >= 2 ? "1fr 1fr" : "1fr",
            gap: "6px",
            marginBottom: "6px",
        }}>
            {messages.map(msg => {
                const news = msg.news;
                if (!news) return null;
                const tooltipText = news.title + (news.body ? "\n" + news.body : "");
                return (
                    <div key={msg.id} className="pinned-news-card" title={tooltipText} style={{
                        padding: "6px 8px",
                        borderRadius: "6px",
                        background: "linear-gradient(135deg, rgba(99,102,241,0.06), rgba(139,92,246,0.06))",
                        borderLeft: `3px solid ${t.promptColor}`,
                        color: t.text,
                        fontSize: "11px",
                        lineHeight: "1.4",
                        overflow: "hidden",
                    }}>
                        <div style={{
                            overflow: "hidden",
                            textOverflow: "ellipsis",
                            whiteSpace: "nowrap",
                            fontWeight: 600,
                        }}>
                            <span>{news.icon} </span>
                            {renderInlineMarkdown(news.title, t)}
                        </div>
                        {news.body && (
                            <div style={{
                                overflow: "hidden",
                                display: "-webkit-box",
                                WebkitLineClamp: 2,
                                WebkitBoxOrient: "vertical" as any,
                                marginTop: "2px",
                                color: t.textMuted,
                            }}>
                                {renderInlineMarkdown(news.body, t)}
                            </div>
                        )}
                    </div>
                );
            })}
        </div>
    );
}
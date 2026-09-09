import type { ChatMessage } from "./useAIAssistant";
import { renderInlineMarkdown } from "./aiAssistantMarkdown";
import type { Theme } from "./aiAssistantPanelTheme";
import { StatusGlyph } from "./WorkbenchIcons";
import { resolveNewsBadge } from "./newsBadge";

interface AssistantPinnedNewsCardsProps {
    messages: ChatMessage[];
    theme: Theme;
}

export function AssistantPinnedNewsCards({ messages, theme: t }: AssistantPinnedNewsCardsProps) {
    if (messages.length === 0) return null;
    return (
        <div style={{ display: "grid", gridTemplateColumns: messages.length >= 2 ? "1fr 1fr" : "1fr", gap: "6px", marginBottom: "6px" }}>
            {messages.map(msg => {
                const news = msg.news;
                if (!news) return null;
                const badge = resolveNewsBadge(news);
                const tooltipText = news.title + (news.body ? "\n" + news.body : "");
                return (
                    <div key={msg.id} className="pinned-news-card" title={tooltipText} style={{
                        padding: "6px 8px", borderRadius: "6px", background: t.fieldBg, border: `1px solid ${t.fieldBorder}`,
                        color: t.text, fontSize: "11px", lineHeight: "1.4", overflow: "hidden",
                    }}>
                        <div style={{ display: "flex", alignItems: "center", gap: "6px", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", fontWeight: 600 }}>
                            <span aria-hidden="true" style={{
                                display: "inline-flex", alignItems: "center", justifyContent: "center", gap: 3, minWidth: "28px",
                                height: "16px", padding: "0 5px", borderRadius: "3px", background: t.codeBg, color: t.pathColor,
                                fontSize: "9px", fontWeight: 700, lineHeight: 1, flexShrink: 0,
                            }}>
                                {badge.glyph ? <StatusGlyph kind={badge.glyph} size={10} color="currentColor" /> : null}
                                {badge.label}
                            </span>
                            <span style={{ minWidth: 0, overflow: "hidden", textOverflow: "ellipsis" }}>{renderInlineMarkdown(news.title, t)}</span>
                        </div>
                        {news.body && (
                            <div style={{
                                overflow: "hidden", display: "-webkit-box", WebkitLineClamp: 2,
                                WebkitBoxOrient: "vertical" as any, marginTop: "2px", color: t.textMuted,
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

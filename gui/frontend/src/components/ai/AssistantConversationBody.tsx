import type { ReactNode } from "react";
import type { ChatMessage } from "./useAIAssistant";
import type { Theme } from "./aiAssistantPanelTheme";
import { AssistantPinnedNewsCards } from "./AssistantPinnedNewsCards";

interface AssistantConversationBodyProps {
    initLabel: string;
    lang: string;
    messages: ChatMessage[];
    onOpenOnboarding?: () => void;
    onboardingIncomplete?: boolean;
    pinnedNews: ChatMessage[];
    processingText: string;
    ready: boolean;
    renderedOtherMessages: ReactNode;
    renderedProgressMessages: ReactNode;
    showProcessingState: boolean;
    showThinkingState: boolean;
    theme: Theme;
    thinkingText: string;
}

export function AssistantConversationBody({
    initLabel,
    lang,
    messages,
    onOpenOnboarding,
    onboardingIncomplete,
    pinnedNews,
    processingText,
    ready,
    renderedOtherMessages,
    renderedProgressMessages,
    showProcessingState,
    showThinkingState,
    theme: t,
    thinkingText,
}: AssistantConversationBodyProps) {
    return (
        <>
            {onboardingIncomplete ? (
                <div style={{ display: "flex", flexDirection: "column", alignItems: "center", justifyContent: "center", height: "100%", gap: "16px" }}>
                    <div style={{ color: t.textMuted, fontSize: "13px" }}>
                        {lang === "en" ? "Setup not completed" : "\u8bbe\u7f6e\u672a\u5b8c\u6210"}
                    </div>
                    <button
                        onClick={onOpenOnboarding}
                        style={{ padding: "10px 28px", fontSize: "15px", fontWeight: 600, background: "linear-gradient(135deg, #6366f1, #8b5cf6)", color: "#fff", border: "none", borderRadius: "8px", cursor: "pointer", transition: "opacity 0.2s" }}
                        onMouseEnter={e => (e.currentTarget.style.opacity = "0.85")}
                        onMouseLeave={e => (e.currentTarget.style.opacity = "1")}
                    >
                        {lang === "en" ? "Complete Setup" : "\u5b8c\u6210\u8bbe\u7f6e"}
                    </button>
                </div>
            ) : !ready ? (
                <div style={{ display: "flex", flexDirection: "column", alignItems: "center", justifyContent: "center", height: "100%", gap: "12px" }}>
                    <div style={{ width: "24px", height: "24px", border: `4px solid ${t.inputBarBorder}`, borderTopColor: t.promptColor, borderRadius: "50%", boxSizing: "border-box", animation: "maclaw-spin 0.8s linear infinite" }} />
                    <div style={{ color: t.textMuted, fontSize: "12px" }}>{initLabel}</div>
                </div>
            ) : messages.length === 0 ? (
                <span style={{ color: t.emptyHint }}>
                    {lang === "en" ? "Ask me anything..." : "\u6709\u4ec0\u4e48\u53ef\u4ee5\u5e2e\u4f60\u7684\uff1f"}
                </span>
            ) : (
                <>
                    <AssistantPinnedNewsCards messages={pinnedNews} theme={t} />
                    {renderedOtherMessages}
                    {renderedProgressMessages}
                </>
            )}
            {showThinkingState && <div style={{ color: t.textMuted, fontSize: "11px", padding: "4px 0", fontStyle: "italic" }}>{thinkingText}</div>}
            {showProcessingState && <div style={{ color: t.textMuted, fontSize: "11px", padding: "4px 0", fontStyle: "italic" }}>{processingText}</div>}
        </>
    );
}

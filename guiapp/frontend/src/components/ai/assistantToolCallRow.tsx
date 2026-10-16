import React from "react";
import type { Theme } from "./aiAssistantPanelTheme";
import { splitAssistantToolCallContent, type AssistantToolCall } from "./assistantToolCall";

function ToolCallIcon({ color }: { color: string }) {
    return (
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <path d="M14.7 6.3a4 4 0 0 0-5.5 5.5L4 17l3 3 5.2-5.2a4 4 0 0 0 5.5-5.5l-2.5 2.5-3-3 2.5-2.5Z" stroke={color} strokeWidth="1.8" strokeLinejoin="round" />
        </svg>
    );
}

export const AssistantToolCallRow = React.memo(function AssistantToolCallRow({ call, theme }: { call: AssistantToolCall; theme: Theme }) {
    const title = call.name || call.action || "tool";
    const action = call.action && call.action !== call.name ? call.action : "";
    return (
        <div
            data-testid="assistant-tool-call"
            data-tool-name={call.name}
            aria-label={[title, action, call.detail].filter(Boolean).join(" ")}
            style={{
                display: "flex",
                alignItems: "flex-start",
                gap: 8,
                margin: "8px 0",
                padding: "6px 8px",
                maxWidth: "100%",
                minWidth: 0,
                boxSizing: "border-box",
                borderRadius: 8,
                border: `1px solid ${theme.fieldBorder}`,
                background: theme.isDark ? "rgba(255,255,255,0.04)" : "rgba(15, 23, 42, 0.03)",
            }}
        >
            <span aria-hidden="true" style={{ display: "inline-flex", flex: "0 0 auto", marginTop: 1, color: theme.headingColor }}>
                <ToolCallIcon color={theme.headingColor || theme.text} />
            </span>
            <div style={{ minWidth: 0, flex: "1 1 auto" }}>
                <div style={{ display: "flex", flexWrap: "wrap", alignItems: "baseline", gap: "2px 8px" }}>
                    <span style={{ fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace", fontSize: 12, fontWeight: 650, color: theme.text }}>
                        {title}
                    </span>
                    {action && (
                        <span style={{ fontSize: 11, color: theme.textMuted }}>{action}</span>
                    )}
                </div>
                {call.detail && (
                    <pre
                        data-testid="assistant-tool-call-detail"
                        style={{
                            margin: "4px 0 0",
                            whiteSpace: "pre-wrap",
                            overflowWrap: "anywhere",
                            wordBreak: "break-word",
                            minWidth: 0,
                            maxWidth: "100%",
                            fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
                            fontSize: 11,
                            lineHeight: 1.45,
                            color: theme.text,
                        }}
                    >
                        {call.detail}
                    </pre>
                )}
            </div>
        </div>
    );
});

export function renderAssistantBodyWithToolCalls(
    content: string,
    calls: AssistantToolCall[] | undefined,
    theme: Theme,
    renderSegment: (segment: string, isTail: boolean) => React.ReactNode,
): React.ReactNode {
    if (!calls?.length && !content.includes("maclaw-tool:")) {
        if (!content) return null;
        return renderSegment(content, true);
    }
    const segments = splitAssistantToolCallContent(content, calls);
    if (segments.length === 1 && segments[0].kind === "text") {
        return renderSegment(segments[0].text || "", true);
    }
    const lastTextIndex = segments.reduce((found, segment, index) => (segment.kind === "text" ? index : found), -1);
    return (
        <>
            {segments.map((segment, index) => {
                if (segment.kind === "tool" && segment.call) {
                    return <AssistantToolCallRow key={segment.call.id || `tool-${index}`} call={segment.call} theme={theme} />;
                }
                const text = segment.text || "";
                if (!text.trim()) return null;
                return (
                    <React.Fragment key={`text-${index}`}>
                        {renderSegment(text, index === lastTextIndex)}
                    </React.Fragment>
                );
            })}
        </>
    );
}

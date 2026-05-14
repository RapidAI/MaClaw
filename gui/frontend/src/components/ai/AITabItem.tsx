import { useCallback } from "react";
import type { AITab } from "./AITabTypes";
import type { Theme } from "./aiAssistantPanelTheme";

export interface AITabItemProps {
    tab: AITab;
    active: boolean;
    theme: Theme;
    onActivate: (tabId: string) => void;
    onClose?: (tabId: string) => void;
}

export function AITabItem({ tab, active, theme, onActivate, onClose }: AITabItemProps) {
    const handleClick = useCallback(() => {
        onActivate(tab.id);
    }, [onActivate, tab.id]);

    const handleClose = useCallback((e: React.MouseEvent) => {
        e.stopPropagation();
        onClose?.(tab.id);
    }, [onClose, tab.id]);

    const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
        if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            onActivate(tab.id);
        }
    }, [onActivate, tab.id]);

    const handleCloseKeyDown = useCallback((e: React.KeyboardEvent) => {
        if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            e.stopPropagation();
            onClose?.(tab.id);
        }
    }, [onClose, tab.id]);

    const isOnline = tab.type === "ve" || tab.type === "group";
    const readOnlyLabel = tab.readOnly ? "\u53ea\u8bfb" : "";

    return (
        <div
            data-testid={`ai-tab-${tab.id}`}
            role="tab"
            aria-selected={active}
            aria-label={tab.title}
            tabIndex={0}
            onClick={handleClick}
            onKeyDown={handleKeyDown}
            style={{
                display: "flex",
                alignItems: "center",
                gap: 4,
                padding: "4px 10px",
                cursor: "pointer",
                fontSize: 12,
                fontWeight: active ? 600 : 400,
                color: active ? theme.text : theme.textMuted,
                background: active ? theme.bg : "transparent",
                borderBottom: active ? `2px solid ${theme.btnColor}` : "2px solid transparent",
                whiteSpace: "nowrap",
                userSelect: "none",
                transition: "background 0.15s, border-color 0.15s",
                maxWidth: 140,
                overflow: "hidden",
            }}
            title={tab.title}
        >
            {isOnline && (
                <span
                    data-testid={`ai-tab-indicator-${tab.id}`}
                    aria-label={`${tab.title} online`}
                    style={{
                        width: 6,
                        height: 6,
                        borderRadius: "50%",
                        flexShrink: 0,
                        background: "#22c55e",
                    }}
                />
            )}
            <span style={{ overflow: "hidden", textOverflow: "ellipsis" }}>
                {tab.title}
            </span>
            {readOnlyLabel && (
                <span style={{ flexShrink: 0, fontSize: 10, lineHeight: 1, padding: "2px 4px", borderRadius: 4, border: `1px solid ${theme.divider}`, color: theme.textMuted }}>
                    {readOnlyLabel}
                </span>
            )}
            {tab.closable && (
                <span
                    data-testid={`ai-tab-close-${tab.id}`}
                    role="button"
                    aria-label={`Close ${tab.title}`}
                    tabIndex={0}
                    onClick={handleClose}
                    onKeyDown={handleCloseKeyDown}
                    style={{
                        marginLeft: 4,
                        fontSize: 14,
                        lineHeight: 1,
                        color: theme.textMuted,
                        cursor: "pointer",
                        flexShrink: 0,
                        borderRadius: 3,
                        padding: "0 2px",
                    }}
                    title="Close"
                >
                    ×
                </span>
            )}
        </div>
    );
}

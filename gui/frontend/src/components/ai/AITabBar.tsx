import type { AITab } from "./AITabTypes";
import type { Theme } from "./aiAssistantPanelTheme";
import { AITabItem } from "./AITabItem";

export interface AITabBarProps {
    tabs: AITab[];
    activeTabId: string;
    theme: Theme;
    onActivate: (tabId: string) => void;
    onClose: (tabId: string) => void;
}

/**
 * Horizontal tab bar for the AI Assistant Panel.
 * The first tab ("AI 助手") is always present and not closable.
 * VE/group tabs appear after it and can be closed.
 */
export function AITabBar({ tabs, activeTabId, theme, onActivate, onClose }: AITabBarProps) {
    if (tabs.length <= 1) {
        // Only the local tab — no need to show the tab bar
        return null;
    }

    return (
        <div
            data-testid="ai-tab-bar"
            role="tablist"
            aria-label="AI 对话标签"
            style={{
                display: "flex",
                alignItems: "flex-end",
                gap: 0,
                borderBottom: `1px solid ${theme.divider}`,
                background: theme.titleBarBg,
                overflowX: "auto",
                overflowY: "hidden",
                flexShrink: 0,
                minHeight: 30,
                paddingLeft: 4,
            }}
        >
            {tabs.map((tab) => (
                <AITabItem
                    key={tab.id}
                    tab={tab}
                    active={tab.id === activeTabId}
                    theme={theme}
                    onActivate={onActivate}
                    onClose={tab.closable ? onClose : undefined}
                />
            ))}
        </div>
    );
}

import { useCallback, useEffect, useRef, useState } from "react";
import type { AITab } from "./AITabTypes";
import type { Theme } from "./aiAssistantPanelTheme";
import { AITabItem } from "./AITabItem";

export interface AITabBarProps {
    tabs: AITab[];
    activeTabId: string;
    theme: Theme;
    onActivate: (tabId: string) => void;
    onClose: (tabId: string) => void;
    lang?: string;
    /** Returns the lastActiveAt timestamp for a tab (used for overflow sorting). */
    getLastActiveAt?: (tabId: string) => number;
}

/** Minimum width per tab in pixels. Used to calculate how many tabs fit. */
const MIN_TAB_WIDTH = 110;
/** Extra space reserved for the overflow button. */
const OVERFLOW_BUTTON_WIDTH = 50;

/**
 * Horizontal tab bar for the AI Assistant Panel.
 * Shows as many tabs as fit in the available width; overflow tabs are
 * accessible via a more-tabs dropdown.
 */
export function AITabBar({ tabs, activeTabId, theme, onActivate, onClose, lang, getLastActiveAt }: AITabBarProps) {
    const containerRef = useRef<HTMLDivElement>(null);
    const [visibleCount, setVisibleCount] = useState(tabs.length);
    const [dropdownOpen, setDropdownOpen] = useState(false);

    // Recalculate visible tab count when container width or tab count changes.
    useEffect(() => {
        const el = containerRef.current;
        if (!el) return;

        const applyWidth = (width: number) => {
            const maxTabs = Math.max(1, Math.floor((width - OVERFLOW_BUTTON_WIDTH) / MIN_TAB_WIDTH));
            setVisibleCount(maxTabs >= tabs.length ? tabs.length : maxTabs);
        };

        if (typeof ResizeObserver === "undefined") {
            const recalculate = () => applyWidth(el.getBoundingClientRect().width);
            recalculate();
            window.addEventListener("resize", recalculate);
            return () => window.removeEventListener("resize", recalculate);
        }

        const observer = new ResizeObserver((entries) => {
            for (const entry of entries) {
                applyWidth(entry.contentRect.width);
            }
        });
        observer.observe(el);
        return () => observer.disconnect();
    }, [tabs.length]);
    // Close dropdown on outside click.
    useEffect(() => {
        if (!dropdownOpen) return;
        const handler = (e: MouseEvent) => {
            if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
                setDropdownOpen(false);
            }
        };
        document.addEventListener("mousedown", handler);
        return () => document.removeEventListener("mousedown", handler);
    }, [dropdownOpen]);

    const handleOverflowActivate = useCallback((tabId: string) => {
        setDropdownOpen(false);
        onActivate(tabId);
    }, [onActivate]);

    const handleOverflowClose = useCallback((tabId: string) => {
        onClose(tabId);
    }, [onClose]);

    // Only the local tab: no need to show the tab bar.
    if (tabs.length <= 1) {
        return null;
    }

    const hasOverflow = visibleCount < tabs.length;
    const visibleTabs = computeVisibleTabs(tabs, activeTabId, visibleCount);
    const overflowTabs = tabs
        .filter(t => !visibleTabs.includes(t))
        .sort((a, b) => {
            // Sort by lastActiveAt descending (most recently active first)
            const aTime = getLastActiveAt?.(a.id) ?? 0;
            const bTime = getLastActiveAt?.(b.id) ?? 0;
            return bTime - aTime;
        });

    return (
        <div
            ref={containerRef}
            data-testid="ai-tab-bar"
            role="tablist"
            aria-label="AI conversation tabs"
            style={{
                display: "flex",
                alignItems: "flex-end",
                gap: 0,
                borderBottom: `1px solid ${theme.divider}`,
                background: theme.titleBarBg,
                overflowX: "visible",
                overflowY: "visible",
                flexShrink: 0,
                minHeight: 30,
                paddingLeft: 4,
                position: "relative",
            }}
        >
            {visibleTabs.map((tab) => (
                <AITabItem
                    key={tab.id}
                    tab={tab}
                    active={tab.id === activeTabId}
                    theme={theme}
                    onActivate={onActivate}
                    onClose={tab.closable ? onClose : undefined}
                    lang={lang}
                />
            ))}
            {hasOverflow && (
                <button
                    data-testid="ai-tab-overflow-btn"
                    type="button"
                    onClick={() => setDropdownOpen(v => !v)}
                    style={{
                        border: "none",
                        background: dropdownOpen ? theme.codeBlockBg : "transparent",
                        color: theme.textMuted,
                        fontSize: 11,
                        padding: "4px 8px",
                        cursor: "pointer",
                        flexShrink: 0,
                        borderBottom: "2px solid transparent",
                        whiteSpace: "nowrap",
                        transition: "background 0.15s",
                    }}
                    title={lang === "en" ? `${overflowTabs.length} more tabs` : `还有 ${overflowTabs.length} 个标签`}
                >
                    {"▼ "}{overflowTabs.length}
                </button>
            )}
            {dropdownOpen && overflowTabs.length > 0 && (
                <div
                    data-testid="ai-tab-overflow-dropdown"
                    style={{
                        position: "absolute",
                        top: "100%",
                        right: 0,
                        zIndex: 9999,
                        background: theme.titleBarBg,
                        border: `1px solid ${theme.divider}`,
                        borderRadius: 6,
                        boxShadow: "0 4px 12px rgba(0,0,0,0.15)",
                        padding: "4px 0",
                        minWidth: 160,
                        maxHeight: 300,
                        overflowY: "auto",
                    }}
                >
                    {overflowTabs.map((tab) => (
                        <div
                            key={tab.id}
                            style={{
                                display: "flex",
                                alignItems: "center",
                                gap: 6,
                                padding: "6px 12px",
                                cursor: "pointer",
                                fontSize: 12,
                                color: tab.id === activeTabId ? theme.text : theme.textMuted,
                                fontWeight: tab.id === activeTabId ? 600 : 400,
                                transition: "background 0.1s",
                            }}
                            onClick={() => handleOverflowActivate(tab.id)}
                            onMouseEnter={e => (e.currentTarget.style.background = theme.codeBlockBg)}
                            onMouseLeave={e => (e.currentTarget.style.background = "transparent")}
                        >
                            <span style={{ fontSize: 13, flexShrink: 0 }}>
                                {tab.type === "project" ? (tab.archived ? "\u{1F4E6}" : "\u{1F4C1}") : tab.type === "ve" ? "\u{1F916}" : "\u{1F4AC}"}
                            </span>
                            <span style={{ flex: 1, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                                {tab.title}
                            </span>
                            {tab.readOnly && (
                                <span style={{ flexShrink: 0, fontSize: 10, lineHeight: 1, padding: "2px 4px", borderRadius: 4, border: `1px solid ${theme.divider}`, color: theme.textMuted }}>
                                    {lang === "en" ? "Read-only" : lang === "zh-Hant" ? "\u552f\u8b80" : "\u53ea\u8bfb"}
                                </span>
                            )}
                            {tab.closable && (
                                <span
                                    role="button"
                                    onClick={(e) => { e.stopPropagation(); handleOverflowClose(tab.id); }}
                                    style={{ fontSize: 14, color: theme.textMuted, cursor: "pointer", flexShrink: 0, padding: "0 2px" }}
                                    title="Close"
                                >{"\u00d7"}</span>
                            )}
                        </div>
                    ))}
                </div>
            )}
        </div>
    );
}

/**
 * Compute which tabs should be visible in the tab bar.
 * Rules:
 * - Local tab (index 0) is always visible.
 * - Active tab is always visible.
 * - Remaining slots filled in order.
 */
function computeVisibleTabs(tabs: AITab[], activeTabId: string, maxVisible: number): AITab[] {
    if (maxVisible >= tabs.length) return tabs;

    const result: AITab[] = [];
    const used = new Set<string>();

    // Always include local tab (first).
    if (tabs.length > 0) {
        result.push(tabs[0]);
        used.add(tabs[0].id);
    }

    // Always include active tab.
    const activeTab = tabs.find(t => t.id === activeTabId);
    if (activeTab && !used.has(activeTab.id)) {
        result.push(activeTab);
        used.add(activeTab.id);
    }

    // Fill remaining slots in order.
    for (const tab of tabs) {
        if (result.length >= maxVisible) break;
        if (!used.has(tab.id)) {
            result.push(tab);
            used.add(tab.id);
        }
    }

    return result;
}

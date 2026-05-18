import { useCallback } from "react";
import type { AITab } from "./AITabTypes";
import type { Theme } from "./aiAssistantPanelTheme";

const textForTabLang = (lang: string | undefined, en: string, zhHans: string, zhHant = zhHans): string => (
    lang === "zh-Hant" ? zhHant : lang?.startsWith("zh") || !lang ? zhHans : en
);

function looksLikeRawParticipantId(value: string): boolean {
    return /^(m_[A-Za-z0-9]+|machine[-_][A-Za-z0-9-]+|ve[-_][A-Za-z0-9-]+|profile[-_][A-Za-z0-9-]+|disc[-_][A-Za-z0-9-]+|discussion[-_][A-Za-z0-9-]+|consultation[-_][A-Za-z0-9-]+|session[-_][A-Za-z0-9-]+|[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})$/i.test(value);
}

function participantTitleName(tab: AITab, participantId: string, index: number, lang?: string): string {
    if (participantId === "local-maclaw") return textForTabLang(lang, "Local AI", "本机AI", "本機AI");
    const mapped = String(tab.participantNames?.[participantId] || "").trim();
    if (mapped && mapped !== participantId && !looksLikeRawParticipantId(mapped)) return mapped.replace(/\s+\([^()]+\)$/, "").trim();
    const tabTitle = String(tab.title || "").trim();
    if (participantId === tab.veId && tabTitle && tabTitle !== participantId && !looksLikeRawParticipantId(tabTitle)) return tabTitle;
    return textForTabLang(lang, `Participant ${index + 1}`, `参与者 ${index + 1}`, `參與者 ${index + 1}`);
}

function directVETitleName(tab: AITab, lang?: string): string {
    const title = String(tab.title || "").trim();
    const id = String(tab.veId || "").trim();
    if (title && title !== id && !looksLikeRawParticipantId(title)) return title;
    return textForTabLang(lang, "Digital employee", "数字员工", "數字員工");
}

export function getAITabDisplayTitle(tab: AITab, lang?: string): string {
    if (tab.type === "ve") return directVETitleName(tab, lang);
    if (tab.type !== "group" || !tab.veId || !tab.participants?.length) return tab.title;
    const names = tab.participants.map((id, index) => participantTitleName(tab, id, index, lang));
    return names.join(", ");
}
export interface AITabItemProps {
    tab: AITab;
    active: boolean;
    theme: Theme;
    onActivate: (tabId: string) => void;
    onClose?: (tabId: string) => void;
    onContextMenu?: (e: React.MouseEvent, tab: AITab) => void;
    lang?: string;
}

export function AITabItem({ tab, active, theme, onActivate, onClose, onContextMenu, lang }: AITabItemProps) {
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

    const isLocal = tab.type === "local";
    const isProject = tab.type === "project";
    const isVE = tab.type === "ve";
    const isGroup = tab.type === "group";
    // VE tabs reflect actual online status (undefined defaults to online for optimistic UX);
    // group tabs are always considered "online".
    const isOnline = isGroup || (isVE && tab.onlineStatus !== "offline");
    const readOnlyLabel = tab.readOnly ? (lang === "en" ? "Read-only" : lang === "zh-Hant" ? "\u552f\u8b80" : "\u53ea\u8bfb") : "";
    const displayTitle = getAITabDisplayTitle(tab, lang);
    const accessibleTitle = readOnlyLabel ? `${displayTitle} - ${readOnlyLabel}` : displayTitle;

    // Tab icon by type — inline SVG for consistent cross-platform rendering
    // Each tab type has a distinct silhouette for instant recognition.
    // Color encodes state: active tab uses btnColor, online VE/group uses green, others use textMuted.
    const iconColor = isOnline
        ? "#22c55e"  // green for online VE/group — replaces the separate green dot
        : (active ? theme.btnColor : theme.textMuted);

    const tabIconElement = isLocal ? (
        // Sparkle/star — AI assistant main session
        <svg width="12" height="12" viewBox="0 0 16 16" fill="none" style={{ flexShrink: 0 }}>
            <path d="M8 1l1.5 4.5L14 7l-4.5 1.5L8 13l-1.5-4.5L2 7l4.5-1.5L8 1z" fill={iconColor} />
        </svg>
    ) : isProject ? (
        // Document with lines — task/project session
        <svg width="12" height="12" viewBox="0 0 16 16" fill="none" style={{ flexShrink: 0 }}>
            <rect x="3" y="2" width="10" height="12" rx="1.5" stroke={iconColor} strokeWidth="1.3" />
            <line x1="5.5" y1="5.5" x2="10.5" y2="5.5" stroke={iconColor} strokeWidth="1.2" strokeLinecap="round" />
            <line x1="5.5" y1="8" x2="10.5" y2="8" stroke={iconColor} strokeWidth="1.2" strokeLinecap="round" />
            <line x1="5.5" y1="10.5" x2="8.5" y2="10.5" stroke={iconColor} strokeWidth="1.2" strokeLinecap="round" />
        </svg>
    ) : isVE ? (
        // Person silhouette — digital employee (VE)
        <svg width="12" height="12" viewBox="0 0 16 16" fill="none" style={{ flexShrink: 0 }}>
            <circle cx="8" cy="5" r="2.5" stroke={iconColor} strokeWidth="1.3" />
            <path d="M3.5 14c0-2.5 2-4.5 4.5-4.5s4.5 2 4.5 4.5" stroke={iconColor} strokeWidth="1.3" strokeLinecap="round" />
        </svg>
    ) : isGroup ? (
        // Two people — group chat
        <svg width="12" height="12" viewBox="0 0 16 16" fill="none" style={{ flexShrink: 0 }}>
            <circle cx="6" cy="5" r="2" stroke={iconColor} strokeWidth="1.2" />
            <path d="M2.5 13c0-2 1.5-3.5 3.5-3.5s3.5 1.5 3.5 3.5" stroke={iconColor} strokeWidth="1.2" strokeLinecap="round" />
            <circle cx="11" cy="4.5" r="1.7" stroke={iconColor} strokeWidth="1.1" />
            <path d="M9 12.5c0-1.5 1-2.8 2-2.8s2 1.3 2 2.8" stroke={iconColor} strokeWidth="1.1" strokeLinecap="round" />
        </svg>
    ) : null;

    return (
        <div
            data-testid={`ai-tab-${tab.id}`}
            role="tab"
            aria-selected={active}
            aria-label={accessibleTitle}
            tabIndex={0}
            onClick={handleClick}
            onKeyDown={handleKeyDown}
            onContextMenu={(e) => { if (onContextMenu) { e.preventDefault(); onContextMenu(e, tab); } }}
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
            title={accessibleTitle}
        >
            {tabIconElement}
            <span style={{ overflow: "hidden", textOverflow: "ellipsis" }}>
                {displayTitle}
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
                    aria-label={`Close ${accessibleTitle}`}
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
                >{"\u00d7"}</span>
            )}
        </div>
    );
}

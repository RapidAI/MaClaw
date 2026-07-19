import { useCallback } from "react";
import type { AITab } from "./AITabTypes";
import type { Theme } from "./aiAssistantPanelTheme";
import { DEFAULT_EXPERT_ICON } from "./expertTypes";
import { isLocalParticipant, localAINameForLang, looksLikeRawParticipantId } from "./localAIIdentity";
import { participantIdentityMatches, participantNameForIdentity } from "./participantIdentity";
import { safeAvatarDataURL } from "./virtualEmployeeAvatar";

const textForTabLang = (lang: string | undefined, en: string, zhHans: string, zhHant = zhHans): string => (
    lang === "zh-Hant" ? zhHant : lang?.startsWith("zh") || !lang ? zhHans : en
);


function participantTitleName(tab: AITab, participantId: string, index: number, lang?: string): string {
    if (isLocalParticipant(tab, participantId)) return localAINameForLang(lang);
    const mapped = String(participantNameForIdentity(tab.participantNames, participantId) || "").trim();
    if (mapped && mapped !== participantId && !looksLikeRawParticipantId(mapped)) return mapped.replace(/\s+\([^()]+\)$/, "").trim();
    const tabTitle = String(tab.title || "").trim();
    if (participantIdentityMatches(participantId, tab.veId) && tabTitle && tabTitle !== participantId && !looksLikeRawParticipantId(tabTitle)) return tabTitle;
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
    if (tab.type === "group" && String(tab.groupTitle || "").trim()) return String(tab.groupTitle || "").trim();
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
    /** Whether this tab is currently recording a skill */
    recording?: boolean;
}

export function AITabItem({ tab, active, theme, onActivate, onClose, onContextMenu, lang, recording }: AITabItemProps) {
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
    // VE/group tabs reflect known status; undefined stays optimistic for live tabs.
    const isOnline = (isVE || isGroup) && tab.onlineStatus !== "offline";
    const readOnlyLabel = tab.readOnly ? (lang === "en" ? "Read-only" : lang === "zh-Hant" ? "\u552f\u8b80" : "\u53ea\u8bfb") : "";
    const displayTitle = getAITabDisplayTitle(tab, lang);
    const codingEnvLabel = tab.type === "project" && tab.agentMode === "remote_coding_dev"
        ? textForTabLang(lang, "Remote coding environment", "远程编程环境", "遠端程式開發環境")
        : (tab.type === "project" && tab.agentMode === "coding_dev"
            ? textForTabLang(lang, "Coding environment", "编程环境", "程式開發環境")
            : "");
    const accessibleTitle = [displayTitle, codingEnvLabel, readOnlyLabel].filter(Boolean).join(" - ");
    const avatarDataURL = safeAvatarDataURL(tab.avatarDataURL);

    // Tab icon by type — inline SVG for consistent cross-platform rendering
    // Each tab type has a distinct silhouette for instant recognition.
    // Color encodes state: active tab uses btnColor, online VE/group uses green, others use textMuted.
    const iconColor = isOnline
        ? "#4f7f6f"  // muted green for online VE/group — replaces the separate green dot
        : (active ? theme.btnColor : theme.textMuted);

    const tabIconElement = avatarDataURL ? (
        <span style={{ position: "relative", width: 14, height: 14, flexShrink: 0, display: "inline-flex" }}>
            <img
                data-testid={`ai-tab-avatar-${tab.id}`}
                src={avatarDataURL}
                alt=""
                style={{ width: 14, height: 14, borderRadius: "50%", objectFit: "cover", display: "block" }}
            />
            {(isVE || isGroup) && (
                <span
                    data-testid={`ai-tab-status-${tab.id}`}
                    aria-hidden="true"
                    style={{
                        position: "absolute",
                        right: -2,
                        bottom: -2,
                        width: 6,
                        height: 6,
                        borderRadius: "50%",
                        background: isOnline ? "#4f7f6f" : "#6b7280",
                        border: `1px solid ${active ? theme.bg : theme.titleBarBg || theme.bg}`,
                        boxSizing: "border-box",
                    }}
                />
            )}
        </span>
    ) : isLocal ? (
        // Sparkle/star — AI assistant main session
        <svg width="12" height="12" viewBox="0 0 16 16" fill="none" style={{ flexShrink: 0 }}>
            <path d="M8 1l1.5 4.5L14 7l-4.5 1.5L8 13l-1.5-4.5L2 7l4.5-1.5L8 1z" fill={iconColor} />
        </svg>
    ) : isProject && tab.agentMode === "remote_coding_dev" ? (
        // Terminal / remote host — pure remote coding environment
        <svg width="12" height="12" viewBox="0 0 16 16" fill="none" style={{ flexShrink: 0 }} data-testid={`ai-tab-remote-coding-icon-${tab.id}`}>
            <rect x="2.5" y="3.5" width="11" height="9" rx="1.5" stroke={iconColor} strokeWidth="1.3" />
            <path d="M5 7h3M5 9.5h5" stroke={iconColor} strokeWidth="1.2" strokeLinecap="round" />
            <path d="m10.5 6.5 1.5 1.2-1.5 1.2" stroke={iconColor} strokeWidth="1.2" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
    ) : isProject && tab.agentMode === "coding_dev" ? (
        // Code brackets — pure coding environment
        <svg width="12" height="12" viewBox="0 0 16 16" fill="none" style={{ flexShrink: 0 }} data-testid={`ai-tab-coding-icon-${tab.id}`}>
            <path d="M5.5 4.5 2.5 8l3 3.5" stroke={iconColor} strokeWidth="1.3" strokeLinecap="round" strokeLinejoin="round" />
            <path d="m10.5 4.5 3 3.5-3 3.5" stroke={iconColor} strokeWidth="1.3" strokeLinecap="round" strokeLinejoin="round" />
            <path d="m9 3.5-2 9" stroke={iconColor} strokeWidth="1.3" strokeLinecap="round" />
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
            {recording && <span data-testid={`ai-tab-recording-${tab.id}`} aria-label="Recording" style={{ display: "inline-block", width: 6, height: 6, borderRadius: "50%", background: "#dc2626", flexShrink: 0, animation: "pulse-recording 1.5s ease-in-out infinite" }} />}
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

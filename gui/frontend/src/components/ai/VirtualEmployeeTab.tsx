import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";
import type { Theme } from "./aiAssistantPanelTheme";
import { looksLikeRawParticipantId } from "./localAIIdentity";
import { participantIdentityMatches } from "./participantIdentity";
export { safeAvatarDataURL } from "./virtualEmployeeAvatar";
import { safeAvatarDataURL } from "./virtualEmployeeAvatar";
import { isVirtualEmployeeOnline } from "./virtualEmployeeStatus";

// --- Types ---

export interface VirtualEmployeeEntry {
    id: string;
    machine_id?: string;
    name: string;
    skill_description: string;
    avatar_data_url?: string;
    access_policy: "public" | "whitelist" | "blacklist" | "per_request";
    status: string;
    online_status: "online" | "offline";
    resident?: boolean;
    registered_at?: string;
}

export interface VETabProps {
    onStartConversation: (ve: VirtualEmployeeEntry) => void;
    theme: Theme;
    lang?: string;
    /** Override for testing - if provided, used instead of the Wails binding */
    listVirtualEmployees?: () => Promise<VirtualEmployeeEntry[]>;
    /** IDs of currently favorited employees */
    favoriteEmployeeIds?: string[];
    /** Called when user clicks "Set as Favorite" in context menu */
    onSetFavorite?: (ve: VirtualEmployeeEntry) => void;
    /** Called when user clicks "Remove from Favorite" in context menu */
    onRemoveFavorite?: (ve: VirtualEmployeeEntry) => void;
}

// --- Helpers ---


function readableVirtualEmployeeName(ve: Pick<VirtualEmployeeEntry, "id" | "machine_id" | "name">, index: number, lang?: string): string {
    const name = String(ve.name || "").trim();
    const id = String(ve.id || "").trim();
    const machineId = String(ve.machine_id || "").trim();
    if (name && name !== id && name !== machineId && !looksLikeRawParticipantId(name)) return name;
    return !lang || lang.startsWith("zh") ? "数字员工 " + (index + 1) : "Digital employee " + (index + 1);
}

/** Truncate a string to maxLen characters, appending ellipsis if exceeded. */
export function truncateText(text: string, maxLen: number): string {
    if (!text) return "";
    if (text.length <= maxLen) return text;
    return text.slice(0, maxLen) + "\u2026";
}

/** Map access_policy to a short icon/label. */
export function policyIcon(policy: string): string {
    switch (policy) {
        case "public": return "\u{1F310}";
        case "whitelist": return "\u2705";
        case "blacklist": return "\u{1F6AB}";
        case "per_request": return "\u{1F512}";
        default: return "\u2753";
    }
}

export function policyLabel(policy: string, lang?: string): string {
    const isZh = !lang || lang.startsWith("zh");
    switch (policy) {
        case "public": return isZh ? "公开访问" : "Public access";
        case "whitelist": return isZh ? "白名单" : "Allowlist";
        case "blacklist": return isZh ? "黑名单" : "Blocklist";
        case "per_request": return isZh ? "首次访问需同意" : "Approval required";
        default: return isZh ? "未知策略" : "Unknown policy";
    }
}

function EmployeeAvatar({ ve, displayName }: { ve: VirtualEmployeeEntry; displayName: string }) {
    const avatarDataURL = safeAvatarDataURL(ve.avatar_data_url);
    if (avatarDataURL) {
        return (
            <img
                src={avatarDataURL}
                alt=""
                style={{ width: 28, height: 28, borderRadius: "50%", objectFit: "cover", flexShrink: 0 }}
            />
        );
    }
    return (
        <span
            aria-hidden="true"
            style={{
                width: 28,
                height: 28,
                borderRadius: "50%",
                display: "inline-flex",
                alignItems: "center",
                justifyContent: "center",
                flexShrink: 0,
                background: "rgba(99, 102, 241, 0.12)",
                color: "#6366f1",
                fontSize: 11,
                fontWeight: 700,
            }}
        >
            {displayName.trim().slice(0, 1).toUpperCase() || "D"}
        </span>
    );
}

function isFavoriteEmployee(ve: Pick<VirtualEmployeeEntry, "id" | "machine_id">, favoriteEmployeeIds: string[] | undefined): boolean {
    if (!favoriteEmployeeIds?.length) return false;
    const id = String(ve.id || "").trim();
    const machineId = String(ve.machine_id || "").trim();
    return favoriteEmployeeIds.some((favoriteId) => participantIdentityMatches(favoriteId, id) || participantIdentityMatches(favoriteId, machineId));
}

// --- Component ---

export function VirtualEmployeeTab({ onStartConversation, theme, lang, listVirtualEmployees, favoriteEmployeeIds, onSetFavorite, onRemoveFavorite }: VETabProps) {
    const [employees, setEmployees] = useState<VirtualEmployeeEntry[]>([]);
    const [loading, setLoading] = useState(true);
    const [refreshing, setRefreshing] = useState(false);
    const [error, setError] = useState<string>("");
    const [query, setQuery] = useState("");
    const [contextMenu, setContextMenu] = useState<{ x: number; y: number; ve: VirtualEmployeeEntry } | null>(null);

    const throttleRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const pendingRefreshRef = useRef(false);
    const mountedRef = useRef(true);
    const requestSeqRef = useRef(0);

    // Resolve the list function - use injected or dynamically import Wails binding
    const listFnRef = useRef<(() => Promise<VirtualEmployeeEntry[]>) | null>(listVirtualEmployees || null);

    useEffect(() => {
        if (!listVirtualEmployees) {
            // Dynamically import to avoid hard dependency in tests
            import("../../../wailsjs/go/main/App").then((mod) => {
                if (mountedRef.current) {
                    const listFn = (mod as any).ListVirtualEmployees;
                    if (typeof listFn === "function") {
                        listFnRef.current = listFn;
                        fetchList();
                    } else {
                        setError("hub_unavailable");
                        setLoading(false);
                    }
                }
            }).catch(() => {
                if (mountedRef.current) {
                    setError("hub_unavailable");
                    setLoading(false);
                }
            });
        }
    // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    const fetchList = useCallback((options?: { showLoading?: boolean }) => {
        const fn = listFnRef.current;
        if (!fn) return;
        const requestSeq = requestSeqRef.current + 1;
        requestSeqRef.current = requestSeq;
        const showLoading = options?.showLoading !== false;
        if (showLoading) setLoading(true);
        else setRefreshing(true);
        fn()
            .then((result) => {
                if (!mountedRef.current || requestSeq !== requestSeqRef.current) return;
                setEmployees(Array.isArray(result) ? result : []);
                setError("");
            })
            .catch(() => {
                if (!mountedRef.current || requestSeq !== requestSeqRef.current) return;
                if (showLoading) {
                    setError("hub_unavailable");
                    setEmployees([]);
                }
            })
            .finally(() => {
                if (mountedRef.current && requestSeq === requestSeqRef.current) {
                    setLoading(false);
                    setRefreshing(false);
                }
            });
    }, []);

    // Initial fetch when listVirtualEmployees is provided directly (test/prop injection)
    useEffect(() => {
        if (listVirtualEmployees) {
            listFnRef.current = listVirtualEmployees;
            fetchList();
        }
    }, [listVirtualEmployees, fetchList]);

    // 500ms throttled refresh
    const throttledRefresh = useCallback(() => {
        if (throttleRef.current) {
            pendingRefreshRef.current = true;
            return;
        }
        fetchList({ showLoading: false });
        throttleRef.current = setTimeout(() => {
            throttleRef.current = null;
            if (pendingRefreshRef.current) {
                pendingRefreshRef.current = false;
                fetchList({ showLoading: false });
            }
        }, 500);
    }, [fetchList]);

    // WebSocket event listeners
    useEffect(() => {
        mountedRef.current = true;
        const unsub1 = EventsOn("ve:list_update", () => throttledRefresh());
        const unsub2 = EventsOn("ve:status_change", () => throttledRefresh());
        const interval = window.setInterval(() => throttledRefresh(), 30000);
        return () => {
            mountedRef.current = false;
            window.clearInterval(interval);
            if (throttleRef.current) {
                clearTimeout(throttleRef.current);
                throttleRef.current = null;
            }
            if (typeof unsub1 === "function") unsub1();
            else EventsOff("ve:list_update");
            if (typeof unsub2 === "function") unsub2();
            else EventsOff("ve:status_change");
        };
    }, [throttledRefresh]);

    // Close context menu on outside click
    useEffect(() => {
        if (!contextMenu) return;
        const handler = () => setContextMenu(null);
        document.addEventListener("click", handler);
        return () => document.removeEventListener("click", handler);
    }, [contextMenu]);

    // Clamp context menu position to viewport bounds
    const menuRef = useRef<HTMLDivElement>(null);
    const [menuPos, setMenuPos] = useState<{ x: number; y: number }>({ x: 0, y: 0 });
    useEffect(() => {
        if (!contextMenu) return;
        // Start at click position, then adjust after menu renders
        setMenuPos({ x: contextMenu.x, y: contextMenu.y });
        // Use rAF to measure after paint
        const raf = requestAnimationFrame(() => {
            const el = menuRef.current;
            if (!el) return;
            const rect = el.getBoundingClientRect();
            const vw = window.innerWidth;
            const vh = window.innerHeight;
            let x = contextMenu.x;
            let y = contextMenu.y;
            if (x + rect.width > vw - 4) x = vw - rect.width - 4;
            if (y + rect.height > vh - 4) y = vh - rect.height - 4;
            if (x < 4) x = 4;
            if (y < 4) y = 4;
            setMenuPos({ x, y });
        });
        return () => cancelAnimationFrame(raf);
    }, [contextMenu]);

    // --- Render ---

    const isZh = !lang || lang.startsWith("zh");
    const onlineEmployees = employees.filter(isVirtualEmployeeOnline);
    const normalizedQuery = query.trim().toLowerCase();
    const visibleEmployees = normalizedQuery
        ? onlineEmployees.filter((employee) => {
            return [
                employee.name,
                employee.skill_description,
                employee.id,
                employee.machine_id || "",
            ].some((value) => String(value || "").toLowerCase().includes(normalizedQuery));
        })
        : onlineEmployees;
    const refreshLabel = isZh ? "\u5237\u65b0\u6570\u5b57\u5458\u5de5\u5217\u8868" : "Refresh digital employees";
    const searchLabel = isZh ? "\u641c\u7d22\u6570\u5b57\u5458\u5de5" : "Search digital employees";
    const searchPlaceholder = isZh ? "\u641c\u7d22\u540d\u79f0\u6216\u6280\u80fd" : "Search name or skill";
    const emptyListText = employees.length > 0
        ? (normalizedQuery && onlineEmployees.length > 0
            ? (isZh ? "\u672a\u627e\u5230\u5339\u914d\u7684\u5728\u7ebf\u6570\u5b57\u5458\u5de5" : "No matching online digital employees")
            : (isZh ? "\u6682\u65e0\u5728\u7ebf\u7684\u6570\u5b57\u5458\u5de5" : "No online digital employees"))
        : (isZh ? "\u6682\u65e0\u53ef\u7528\u7684\u6570\u5b57\u5458\u5de5" : "No digital employees available");

    const renderShell = (children: ReactNode, options?: { testId?: string; center?: boolean }) => (
        <div style={{ position: "relative", overflow: "auto", height: "100%" }} data-testid={options?.testId || "ve-list-container"}>
            <div
                style={{
                    position: "sticky",
                    top: 0,
                    zIndex: 2,
                    display: "flex",
                    alignItems: "center",
                    gap: 6,
                    padding: "6px 8px",
                    background: theme.bg,
                    borderBottom: `1px solid ${theme.divider}`,
                }}
            >
                <input
                    data-testid="ve-search-input"
                    aria-label={searchLabel}
                    value={query}
                    onChange={(event) => setQuery(event.target.value)}
                    placeholder={searchPlaceholder}
                    style={{
                        flex: 1,
                        minWidth: 0,
                        height: 28,
                        borderRadius: 6,
                        border: `1px solid ${theme.divider}`,
                        background: theme.fieldBg,
                        color: theme.text,
                        padding: "0 9px",
                        fontSize: 12,
                        outline: "none",
                    }}
                />
                <button
                    type="button"
                    data-testid="ve-refresh-button"
                    aria-label={refreshLabel}
                    title={refreshLabel}
                    disabled={refreshing}
                    onClick={() => fetchList({ showLoading: false })}
                    style={{
                        width: 28,
                        height: 28,
                        borderRadius: 6,
                        border: `1px solid ${theme.divider}`,
                        background: theme.bg,
                        color: theme.textMuted,
                        display: "inline-flex",
                        alignItems: "center",
                        justifyContent: "center",
                        flexShrink: 0,
                        cursor: refreshing ? "default" : "pointer",
                        opacity: refreshing ? 0.62 : 0.96,
                        transition: "background 0.15s, color 0.15s, opacity 0.15s",
                    }}
                    onMouseEnter={(e) => {
                        if (refreshing) return;
                        (e.currentTarget as HTMLElement).style.background = theme.fieldBg;
                        (e.currentTarget as HTMLElement).style.color = theme.text;
                    }}
                    onMouseLeave={(e) => {
                        (e.currentTarget as HTMLElement).style.background = theme.bg;
                        (e.currentTarget as HTMLElement).style.color = theme.textMuted;
                    }}
                >
                    <span aria-hidden="true" style={{ fontSize: 15, lineHeight: 1, transform: refreshing ? "rotate(20deg)" : undefined }}>
                        {"\u21bb"}
                    </span>
                </button>
            </div>
            <div style={options?.center ? { display: "flex", alignItems: "center", justifyContent: "center", minHeight: "calc(100% - 41px)", padding: "0 12px 12px" } : undefined}>
                {children}
            </div>
        </div>
    );

    if (loading) {
        return (
            <div style={{ display: "flex", alignItems: "center", justifyContent: "center", padding: 12, color: theme.textMuted, fontSize: 12 }}>
                <span data-testid="ve-loading">{isZh ? "\u52a0\u8f7d\u4e2d..." : "Loading..."}</span>
            </div>
        );
    }

    if (error === "hub_unavailable") {
        return renderShell(
            <div style={{ color: theme.textMuted, fontSize: 12 }} data-testid="ve-empty-hub">
                <span>{isZh ? "Hub \u4e0d\u53ef\u7528\uff0c\u65e0\u6cd5\u83b7\u53d6\u6570\u5b57\u5458\u5de5\u5217\u8868" : "Hub unavailable"}</span>
            </div>,
            { testId: "ve-error-container", center: true }
        );
    }

    if (visibleEmployees.length === 0) {
        return renderShell(
            <div style={{ color: theme.textMuted, fontSize: 12 }} data-testid="ve-empty-list">
                <span>{emptyListText}</span>
            </div>,
            { testId: "ve-empty-container", center: true }
        );
    }

    return renderShell(
        <>
            {visibleEmployees.map((ve, index) => {
                const displayName = readableVirtualEmployeeName(ve, index, lang);
                return (
                <div
                    key={ve.id}
                    data-testid={`ve-item-${ve.id}`}
                    role="button"
                    tabIndex={0}
                    onClick={() => onStartConversation(ve)}
                    onKeyDown={(e) => {
                        if (e.key !== "Enter" && e.key !== " ") return;
                        e.preventDefault();
                        onStartConversation(ve);
                    }}
                    onContextMenu={(e) => {
                        e.preventDefault();
                        setContextMenu({ x: e.clientX, y: e.clientY, ve });
                    }}
                    title={ve.skill_description || displayName}
                    style={{
                        display: "flex",
                        alignItems: "center",
                        gap: 8,
                        padding: "8px 12px",
                        cursor: "pointer",
                        borderBottom: `1px solid ${theme.divider}`,
                        transition: "background 0.15s",
                    }}
                    onMouseEnter={(e) => { (e.currentTarget as HTMLElement).style.background = theme.fieldBg; }}
                    onMouseLeave={(e) => { (e.currentTarget as HTMLElement).style.background = ""; }}
                >
                    <span style={{ position: "relative", width: 28, height: 28, flexShrink: 0 }}>
                        <EmployeeAvatar ve={ve} displayName={displayName} />
                        <span
                            data-testid={`ve-status-${ve.id}`}
                            style={{
                                position: "absolute",
                                right: -1,
                                bottom: -1,
                                width: 8,
                                height: 8,
                                borderRadius: "50%",
                                background: isVirtualEmployeeOnline(ve) ? "#22c55e" : "#9ca3af",
                                border: `1.5px solid ${theme.bg}`,
                                boxSizing: "border-box",
                            }}
                        />
                    </span>

                    {/* Name + skill description */}
                    <div style={{ flex: 1, minWidth: 0, textAlign: "left" }}>
                        <div style={{ display: "flex", alignItems: "center", gap: 4, minWidth: 0 }}>
                            <span style={{ color: theme.text, fontSize: 13, fontWeight: 500, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis", minWidth: 0 }}>
                                {truncateText(displayName, 20)}
                            </span>
                            <span style={{ fontSize: 12, flexShrink: 0 }} title={policyLabel(ve.access_policy, lang)}>{policyIcon(ve.access_policy)}</span>
                            {ve.access_policy === "per_request" && (
                                <span
                                    data-testid={`ve-badge-${ve.id}`}
                                    title={policyLabel(ve.access_policy, lang)}
                                    style={{
                                        fontSize: 10,
                                        padding: "1px 4px",
                                        borderRadius: 3,
                                        background: theme.errorBg || "#fef2f2",
                                        color: theme.errorText || "#dc2626",
                                        border: `1px solid ${theme.errorBorder || "#fecaca"}`,
                                        whiteSpace: "nowrap",
                                        flexShrink: 0,
                                    }}
                                >
                                    {isZh ? "需同意" : "Needs approval"}
                                </span>
                            )}
                        </div>
                        <div style={{ color: theme.textMuted, fontSize: 12, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis", textAlign: "left" }}>
                            {truncateText(ve.skill_description, 50)}
                        </div>
                    </div>
                </div>
                );
            })}

            {/* Context menu */}
            {contextMenu && (() => {
                const isFav = isFavoriteEmployee(contextMenu.ve, favoriteEmployeeIds);
                const hasFavAction = !contextMenu.ve.resident && !!(onSetFavorite || onRemoveFavorite);
                return (
                <div
                    ref={menuRef}
                    data-testid="ve-context-menu"
                    role="menu"
                    style={{
                        position: "fixed",
                        left: menuPos.x,
                        top: menuPos.y,
                        background: theme.bg,
                        border: `1px solid ${theme.divider}`,
                        borderRadius: 6,
                        boxShadow: "0 4px 12px rgba(0,0,0,0.15)",
                        zIndex: 9999,
                        minWidth: 160,
                        padding: "4px 0",
                    }}
                >
                    {/* 对话 */}
                    <div
                        data-testid="ve-menu-conversation"
                        role="menuitem"
                        onClick={() => { onStartConversation(contextMenu.ve); setContextMenu(null); }}
                        style={{ display: "flex", alignItems: "center", gap: 8, padding: "7px 12px", cursor: "pointer", fontSize: 13, color: theme.text }}
                        onMouseEnter={(e) => { (e.currentTarget as HTMLElement).style.background = theme.fieldBg; }}
                        onMouseLeave={(e) => { (e.currentTarget as HTMLElement).style.background = ""; }}
                    >
                        <span style={{ width: 16, textAlign: "center", fontSize: 14, flexShrink: 0 }}>💬</span>
                        <span>{isZh ? "对话" : "Chat"}</span>
                    </div>
                    {/* 设为常用 / 取消常用 — only when callbacks are wired */}
                    {hasFavAction && (
                        <div
                            data-testid="ve-menu-set-favorite"
                            role="menuitem"
                            onClick={() => {
                                if (isFav && onRemoveFavorite) {
                                    onRemoveFavorite(contextMenu.ve);
                                } else if (!isFav && onSetFavorite) {
                                    onSetFavorite(contextMenu.ve);
                                }
                                setContextMenu(null);
                            }}
                            style={{ display: "flex", alignItems: "center", gap: 8, padding: "7px 12px", cursor: "pointer", fontSize: 13, color: theme.text }}
                            onMouseEnter={(e) => { (e.currentTarget as HTMLElement).style.background = theme.fieldBg; }}
                            onMouseLeave={(e) => { (e.currentTarget as HTMLElement).style.background = ""; }}
                        >
                            <span style={{ width: 16, textAlign: "center", fontSize: 14, flexShrink: 0 }}>{isFav ? "☆" : "★"}</span>
                            <span>{isFav ? (isZh ? "取消常用" : "Remove from favorites") : (isZh ? "设为常用" : "Set as favorite")}</span>
                        </div>
                    )}
                </div>
                );
            })()}
        </>
    );
}

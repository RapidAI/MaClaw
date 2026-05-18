import { useCallback, useEffect, useRef, useState } from "react";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";
import type { Theme } from "./aiAssistantPanelTheme";

// --- Types ---

export interface VirtualEmployeeEntry {
    id: string;
    machine_id?: string;
    name: string;
    skill_description: string;
    access_policy: "public" | "whitelist" | "blacklist" | "per_request";
    status: string;
    online_status: "online" | "offline";
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

function looksLikeRawParticipantId(value: string): boolean {
    return /^(m_[A-Za-z0-9]+|machine[-_][A-Za-z0-9-]+|ve[-_][A-Za-z0-9-]+|profile[-_][A-Za-z0-9-]+|disc[-_][A-Za-z0-9-]+|discussion[-_][A-Za-z0-9-]+|consultation[-_][A-Za-z0-9-]+|session[-_][A-Za-z0-9-]+|[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})$/i.test(value);
}

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

// --- Component ---

export function VirtualEmployeeTab({ onStartConversation, theme, lang, listVirtualEmployees, favoriteEmployeeIds, onSetFavorite, onRemoveFavorite }: VETabProps) {
    const [employees, setEmployees] = useState<VirtualEmployeeEntry[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string>("");
    const [contextMenu, setContextMenu] = useState<{ x: number; y: number; ve: VirtualEmployeeEntry } | null>(null);

    const throttleRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const pendingRefreshRef = useRef(false);
    const mountedRef = useRef(true);

    // Resolve the list function - use injected or dynamically import Wails binding
    const listFnRef = useRef<(() => Promise<VirtualEmployeeEntry[]>) | null>(listVirtualEmployees || null);

    useEffect(() => {
        if (!listVirtualEmployees) {
            // Dynamically import to avoid hard dependency in tests
            import("../../../wailsjs/go/main/App").then((mod) => {
                if (mountedRef.current) {
                    listFnRef.current = (mod as any).ListVirtualEmployees;
                    fetchList();
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

    const fetchList = useCallback(() => {
        const fn = listFnRef.current;
        if (!fn) return;
        setLoading(true);
        fn()
            .then((result) => {
                if (!mountedRef.current) return;
                setEmployees(result || []);
                setError("");
            })
            .catch(() => {
                if (!mountedRef.current) return;
                setError("hub_unavailable");
                setEmployees([]);
            })
            .finally(() => {
                if (mountedRef.current) setLoading(false);
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
        fetchList();
        throttleRef.current = setTimeout(() => {
            throttleRef.current = null;
            if (pendingRefreshRef.current) {
                pendingRefreshRef.current = false;
                fetchList();
            }
        }, 500);
    }, [fetchList]);

    // WebSocket event listeners
    useEffect(() => {
        mountedRef.current = true;
        const unsub1 = EventsOn("ve:list_update", () => throttledRefresh());
        const unsub2 = EventsOn("ve:status_change", () => throttledRefresh());
        return () => {
            mountedRef.current = false;
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

    if (loading) {
        return (
            <div style={{ display: "flex", alignItems: "center", justifyContent: "center", padding: 12, color: theme.textMuted, fontSize: 12 }}>
                <span data-testid="ve-loading">{isZh ? "\u52a0\u8f7d\u4e2d..." : "Loading..."}</span>
            </div>
        );
    }

    if (error === "hub_unavailable") {
        return (
            <div style={{ display: "flex", alignItems: "center", justifyContent: "center", padding: 12, color: theme.textMuted, fontSize: 12 }} data-testid="ve-empty-hub">
                <span>{isZh ? "Hub \u4e0d\u53ef\u7528\uff0c\u65e0\u6cd5\u83b7\u53d6\u6570\u5b57\u5458\u5de5\u5217\u8868" : "Hub unavailable"}</span>
            </div>
        );
    }

    if (employees.length === 0) {
        return (
            <div style={{ display: "flex", alignItems: "center", justifyContent: "center", padding: 12, color: theme.textMuted, fontSize: 12 }} data-testid="ve-empty-list">
                <span>{isZh ? "\u6682\u65e0\u53ef\u7528\u7684\u6570\u5b57\u5458\u5de5" : "No digital employees available"}</span>
            </div>
        );
    }

    return (
        <div style={{ position: "relative", overflow: "auto", height: "100%" }} data-testid="ve-list-container">
            {employees.map((ve, index) => {
                const displayName = readableVirtualEmployeeName(ve, index, lang);
                return (
                <div
                    key={ve.id}
                    data-testid={`ve-item-${ve.id}`}
                    onDoubleClick={() => onStartConversation(ve)}
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
                    {/* Online status indicator */}
                    <span
                        data-testid={`ve-status-${ve.id}`}
                        style={{
                            width: 8,
                            height: 8,
                            borderRadius: "50%",
                            flexShrink: 0,
                            background: ve.online_status === "online" ? "#22c55e" : "#9ca3af",
                        }}
                    />

                    {/* Name + skill description */}
                    <div style={{ flex: 1, minWidth: 0 }}>
                        <div style={{ display: "flex", alignItems: "center", gap: 4 }}>
                            <span style={{ color: theme.text, fontSize: 13, fontWeight: 500, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>
                                {truncateText(displayName, 20)}
                            </span>
                            <span style={{ fontSize: 12 }} title={ve.access_policy}>{policyIcon(ve.access_policy)}</span>
                            {ve.access_policy === "per_request" && (
                                <span
                                    data-testid={`ve-badge-${ve.id}`}
                                    style={{
                                        fontSize: 10,
                                        padding: "1px 4px",
                                        borderRadius: 3,
                                        background: theme.errorBg || "#fef2f2",
                                        color: theme.errorText || "#dc2626",
                                        border: `1px solid ${theme.errorBorder || "#fecaca"}`,
                                        whiteSpace: "nowrap",
                                    }}
                                >
                                    {isZh ? "\u9700\u6388\u6743" : "Auth"}
                                </span>
                            )}
                        </div>
                        <div style={{ color: theme.textMuted, fontSize: 12, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>
                            {truncateText(ve.skill_description, 50)}
                        </div>
                    </div>
                </div>
                );
            })}

            {/* Context menu */}
            {contextMenu && (() => {
                const isFav = favoriteEmployeeIds?.includes(contextMenu.ve.id);
                const hasFavAction = !!(onSetFavorite || onRemoveFavorite);
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
        </div>
    );
}

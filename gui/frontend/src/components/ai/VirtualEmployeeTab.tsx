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
    onAddToGroup: (ve: VirtualEmployeeEntry) => void;
    theme: Theme;
    lang?: string;
    /** Override for testing - if provided, used instead of the Wails binding */
    listVirtualEmployees?: () => Promise<VirtualEmployeeEntry[]>;
    /** IDs of currently favorited employees */
    favoriteEmployeeIds?: string[];
    /** Called when user clicks "Set as Favorite" in context menu */
    onSetFavorite?: (ve: VirtualEmployeeEntry) => void;
}

// --- Helpers ---

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

export function VirtualEmployeeTab({ onStartConversation, onAddToGroup, theme, lang, listVirtualEmployees, favoriteEmployeeIds, onSetFavorite }: VETabProps) {
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

    // --- Render ---

    const isZh = !lang || lang.startsWith("zh");

    if (loading) {
        return (
            <div style={{ display: "flex", alignItems: "center", justifyContent: "center", padding: 32, color: theme.textMuted }}>
                <span data-testid="ve-loading">{isZh ? "加载中..." : "Loading..."}</span>
            </div>
        );
    }

    if (error === "hub_unavailable") {
        return (
            <div style={{ display: "flex", alignItems: "center", justifyContent: "center", padding: 32, color: theme.textMuted }} data-testid="ve-empty-hub">
                <span>{isZh ? "Hub 不可用，无法获取数字员工列表" : "Hub unavailable"}</span>
            </div>
        );
    }

    if (employees.length === 0) {
        return (
            <div style={{ display: "flex", alignItems: "center", justifyContent: "center", padding: 32, color: theme.textMuted }} data-testid="ve-empty-list">
                <span>{isZh ? "暂无可用的数字员工" : "No digital employees available"}</span>
            </div>
        );
    }

    return (
        <div style={{ position: "relative", overflow: "auto", height: "100%" }} data-testid="ve-list-container">
            {employees.map((ve) => (
                <div
                    key={ve.id}
                    data-testid={`ve-item-${ve.id}`}
                    onDoubleClick={() => onStartConversation(ve)}
                    onContextMenu={(e) => {
                        e.preventDefault();
                        setContextMenu({ x: e.clientX, y: e.clientY, ve });
                    }}
                    title={ve.skill_description || ve.name}
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
                                {truncateText(ve.name, 20)}
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
                                    {isZh ? "需授权" : "Auth"}
                                </span>
                            )}
                        </div>
                        <div style={{ color: theme.textMuted, fontSize: 12, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>
                            {truncateText(ve.skill_description, 50)}
                        </div>
                    </div>
                </div>
            ))}

            {/* Context menu */}
            {contextMenu && (
                <div
                    data-testid="ve-context-menu"
                    role="menu"
                    style={{
                        position: "fixed",
                        left: contextMenu.x,
                        top: contextMenu.y,
                        background: theme.bg,
                        border: `1px solid ${theme.divider}`,
                        borderRadius: 6,
                        boxShadow: "0 4px 12px rgba(0,0,0,0.15)",
                        zIndex: 9999,
                        minWidth: 120,
                        padding: "4px 0",
                    }}
                >
                    <div
                        data-testid="ve-menu-conversation"
                        role="menuitem"
                        onClick={() => { onStartConversation(contextMenu.ve); setContextMenu(null); }}
                        style={{ padding: "6px 14px", cursor: "pointer", fontSize: 13, color: theme.text }}
                        onMouseEnter={(e) => { (e.currentTarget as HTMLElement).style.background = theme.fieldBg; }}
                        onMouseLeave={(e) => { (e.currentTarget as HTMLElement).style.background = ""; }}
                    >
                        [Chat] {isZh ? "对话" : "Chat"}
                    </div>
                    <div
                        data-testid="ve-menu-add-group"
                        role="menuitem"
                        onClick={() => { onAddToGroup(contextMenu.ve); setContextMenu(null); }}
                        style={{ padding: "6px 14px", cursor: "pointer", fontSize: 13, color: theme.text }}
                        onMouseEnter={(e) => { (e.currentTarget as HTMLElement).style.background = theme.fieldBg; }}
                        onMouseLeave={(e) => { (e.currentTarget as HTMLElement).style.background = ""; }}
                    >
                        [Group] {isZh ? "添加到群聊" : "Add to group"}
                    </div>
                    {onSetFavorite && (
                        <div
                            data-testid="ve-menu-set-favorite"
                            role="menuitem"
                            onClick={() => {
                                if (!favoriteEmployeeIds?.includes(contextMenu.ve.id)) {
                                    onSetFavorite(contextMenu.ve);
                                }
                                setContextMenu(null);
                            }}
                            style={{
                                padding: "6px 14px",
                                cursor: favoriteEmployeeIds?.includes(contextMenu.ve.id) ? "default" : "pointer",
                                fontSize: 13,
                                color: favoriteEmployeeIds?.includes(contextMenu.ve.id) ? theme.textMuted : theme.text,
                                opacity: favoriteEmployeeIds?.includes(contextMenu.ve.id) ? 0.6 : 1,
                            }}
                            onMouseEnter={(e) => { if (!favoriteEmployeeIds?.includes(contextMenu.ve.id)) (e.currentTarget as HTMLElement).style.background = theme.fieldBg; }}
                            onMouseLeave={(e) => { (e.currentTarget as HTMLElement).style.background = ""; }}
                        >
                            * {favoriteEmployeeIds?.includes(contextMenu.ve.id) ? (isZh ? "已是常用" : "Already favorite") : (isZh ? "设为常用" : "Set as favorite")}
                        </div>
                    )}
                </div>
            )}
        </div>
    );
}

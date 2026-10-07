import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";
import { primaryFilledButtonStyle, relativeLuminance, type Theme } from "./aiAssistantPanelTheme";
import { looksLikeRawParticipantId } from "./localAIIdentity";
import { participantIdentityMatches, participantNameForIdentity } from "./participantIdentity";
export { safeAvatarDataURL } from "./virtualEmployeeAvatar";
import { safeAvatarDataURL } from "./virtualEmployeeAvatar";
import { isVirtualEmployeeOnline } from "./virtualEmployeeStatus";
import { VEStatusDot } from "./VEStatusDot";
import { TitleBarToolIcon } from "./AssistantTitleBarIcons";
import { getWailsAppModule } from "../../utils/wailsAppModule";

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
    whitelist?: string[];
}

export interface VETabProps {
    onStartConversation: (ve: VirtualEmployeeEntry) => void;
    theme: Theme;
    lang?: string;
    /** Override for testing - if provided, used instead of the Wails binding */
    listVirtualEmployees?: () => Promise<VirtualEmployeeEntry[]>;
    /** IDs of currently favorited employees */
    favoriteEmployeeIds?: string[];
    /** Local display names keyed by any known employee identity */
    favoriteEmployeeNames?: Record<string, string>;
    /** Called when user clicks "Set as Favorite" in context menu */
    onSetFavorite?: (ve: VirtualEmployeeEntry) => void;
    /** Called when user clicks "Remove from Favorite" in context menu */
    onRemoveFavorite?: (ve: VirtualEmployeeEntry) => void;
    /** Called when user renames an employee from the list context menu */
    onRenameEmployee?: (ve: VirtualEmployeeEntry, name: string) => void | Promise<void>;
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

/** Icon identifier for an access policy, rendered by VEPolicyIcon. */
export type PolicyIconName = "pub" | "allow" | "block" | "ask" | "unknown";

/** Map access_policy to a policy icon name. */
export function policyIcon(policy: string): PolicyIconName {
    switch (policy) {
        case "public": return "pub";
        case "whitelist": return "allow";
        case "blacklist": return "block";
        case "per_request": return "ask";
        default: return "unknown";
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

const POLICY_ICON_TONES: Record<PolicyIconName, { light: string; dark: string }> = {
    pub: { light: "#3f7060", dark: "#7dbfa8" },
    allow: { light: "#2f5f98", dark: "#8db4d8" },
    block: { light: "#c43d34", dark: "#e07b72" },
    ask: { light: "#a8641f", dark: "#e0a44c" },
    unknown: { light: "#7a8592", dark: "#a2aeb9" },
};

const FAVORITE_STAR_TONE = { light: "#c08a1e", dark: "#e0b64c" };

/** Shield/globe SVG glyphs for access policies — replaces the old "[pub]" text badges. */
export function VEPolicyIcon({ name, size = 14, isDark }: { name: PolicyIconName; size?: number; isDark?: boolean }) {
    const tone = POLICY_ICON_TONES[name];
    const stroke = { fill: "none", stroke: "currentColor", strokeWidth: 2, strokeLinecap: "round", strokeLinejoin: "round" } as const;
    return (
        <svg
            width={size}
            height={size}
            viewBox="0 0 24 24"
            aria-hidden="true"
            focusable="false"
            style={{ color: isDark ? tone.dark : tone.light, display: "block", flexShrink: 0 }}
        >
            {name === "pub" && (
                <>
                    <circle {...stroke} cx="12" cy="12" r="7.6" />
                    <path {...stroke} d="M4.4 12h15.2" />
                    <path {...stroke} d="M12 4.4a11.5 11.5 0 0 1 0 15.2" />
                    <path {...stroke} d="M12 4.4a11.5 11.5 0 0 0 0 15.2" />
                </>
            )}
            {name !== "pub" && <path {...stroke} d="M12 3.4 18.6 5.9v5.2c0 4.5-2.7 7.7-6.6 9.5-3.9-1.8-6.6-5-6.6-9.5V5.9Z" />}
            {name === "allow" && <path {...stroke} d="m9.1 12 2 2 3.9-4.4" />}
            {name === "block" && <path {...stroke} d="M9.8 9.8l4.4 4.4M14.2 9.8l-4.4 4.4" />}
            {name === "ask" && (
                <>
                    <path {...stroke} d="M10 10a2 2 0 1 1 3.4 1.4c-.7.7-1.4 1-1.4 2" />
                    <path {...stroke} d="M12 16.4h.01" />
                </>
            )}
            {name === "unknown" && <circle cx="12" cy="12" r="1.5" fill="currentColor" />}
        </svg>
    );
}

/** Context-menu glyphs, matching the app's rounded stroke icon style. */
function VEMenuGlyph({ name, filled }: { name: "chat" | "star" | "pencil" | "info"; filled?: boolean }) {
    const stroke = { fill: "none", stroke: "currentColor", strokeWidth: 1.8, strokeLinecap: "round", strokeLinejoin: "round" } as const;
    return (
        <svg width="15" height="15" viewBox="0 0 24 24" aria-hidden="true" focusable="false">
            {name === "chat" && <path {...stroke} d="M20 5.2H4a1 1 0 0 0-1 1v9.6a1 1 0 0 0 1 1h3.2v3.4l4.4-3.4H20a1 1 0 0 0 1-1V6.2a1 1 0 0 0-1-1Z" />}
            {name === "star" && <path {...stroke} fill={filled ? "currentColor" : "none"} d="M12 4.2l2.3 4.7 5.2.7-3.8 3.7.9 5.1-4.6-2.4-4.6 2.4.9-5.1-3.8-3.7 5.2-.7Z" />}
            {name === "pencil" && <path {...stroke} d="M16.6 3.6a2.1 2.1 0 0 1 2.9 2.9L7.2 18.8l-4 1 1-4L16.6 3.6Z" />}
            {name === "info" && (
                <>
                    <circle {...stroke} cx="12" cy="12" r="7.6" />
                    <path {...stroke} d="M12 11.2v4.8" />
                    <path {...stroke} d="M12 8.1h.01" />
                </>
            )}
        </svg>
    );
}

function EmployeeAvatar({ ve, displayName }: { ve: VirtualEmployeeEntry; displayName: string }) {
    const avatarDataURL = safeAvatarDataURL(ve.avatar_data_url);
    if (avatarDataURL) {
        return (
            <img
                className="vet-avatar-img"
                src={avatarDataURL}
                alt=""
            />
        );
    }
    return (
        <span
            className="vet-avatar-fallback"
            aria-hidden="true"
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

function displayNameForVirtualEmployee(ve: VirtualEmployeeEntry, index: number, lang: string | undefined, favoriteEmployeeNames: Record<string, string> | undefined): string {
    const customName = participantNameForIdentity(favoriteEmployeeNames, ve.machine_id) || participantNameForIdentity(favoriteEmployeeNames, ve.id);
    if (customName) return customName;
    return readableVirtualEmployeeName(ve, index, lang);
}

// --- Component ---

/**
 * Module-level cache for the employee list.
 * Survives component unmount/remount (tab switching) so the list is shown
 * instantly on tab re-entry while a background refresh runs.
 */
let _cachedEmployees: VirtualEmployeeEntry[] | null = null;
let _cachedEmployeesFetchedAt = 0;
const VE_LIST_CACHE_TTL_MS = 15_000;
const VE_LIST_EVENT_THROTTLE_MS = 1_500;
const VE_LIST_POLL_BASE_MS = 45_000;
const VE_LIST_POLL_MAX_MS = 180_000;

export function __resetVirtualEmployeeTabCacheForTests() {
    _cachedEmployees = null;
    _cachedEmployeesFetchedAt = 0;
}

export function VirtualEmployeeTab({ onStartConversation, theme, lang, listVirtualEmployees, favoriteEmployeeIds, favoriteEmployeeNames, onSetFavorite, onRemoveFavorite, onRenameEmployee }: VETabProps) {
    const [employees, setEmployees] = useState<VirtualEmployeeEntry[]>(_cachedEmployees || []);
    const [loading, setLoading] = useState(_cachedEmployees === null);
    const [refreshing, setRefreshing] = useState(false);
    const [error, setError] = useState<string>("");
    const [query, setQuery] = useState("");
    const [contextMenu, setContextMenu] = useState<{ x: number; y: number; ve: VirtualEmployeeEntry; displayName: string } | null>(null);
    const [renamingEmployee, setRenamingEmployee] = useState<VirtualEmployeeEntry | null>(null);
    const [viewInfoVE, setViewInfoVE] = useState<{ ve: VirtualEmployeeEntry; displayName: string } | null>(null);
    const [renameValue, setRenameValue] = useState("");
    const [renameSaving, setRenameSaving] = useState(false);
    const [renameError, setRenameError] = useState("");
    const [searchFocused, setSearchFocused] = useState(false);

    const throttleRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const pollTimerRef = useRef<number | null>(null);
    const pendingRefreshRef = useRef(false);
    const mountedRef = useRef(true);
    const requestSeqRef = useRef(0);
    const consecutiveRefreshFailuresRef = useRef(0);
    const isZh = !lang || lang.startsWith("zh");
    // Sidebar dark schemes do not set Theme.isDark — infer from the panel background,
    // honoring an explicit flag when a caller provides one.
    const darkSurface = theme.isDark ?? ((relativeLuminance(theme.bg) ?? 1) < 0.5);

    // Resolve the list function - use injected or dynamically import Wails binding
    const listFnRef = useRef<(() => Promise<VirtualEmployeeEntry[]>) | null>(listVirtualEmployees || null);

    useEffect(() => {
        if (!listVirtualEmployees) {
            getWailsAppModule().then((mod) => {
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

    const scheduleNextPoll = useCallback(() => {
        if (pollTimerRef.current) {
            window.clearTimeout(pollTimerRef.current);
            pollTimerRef.current = null;
        }
        if (!mountedRef.current) return;
        const failureCount = consecutiveRefreshFailuresRef.current;
        const backoff = Math.min(VE_LIST_POLL_MAX_MS, VE_LIST_POLL_BASE_MS * Math.max(1, 2 ** Math.min(failureCount, 3)));
        const jitter = Math.floor(Math.random() * 10_000);
        pollTimerRef.current = window.setTimeout(() => {
            pollTimerRef.current = null;
            throttledRefresh();
            scheduleNextPoll();
        }, backoff + jitter);
    // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    const fetchList = useCallback((options?: { showLoading?: boolean; force?: boolean }) => {
        const fn = listFnRef.current;
        if (!fn) return;
        if (!options?.force && _cachedEmployees !== null && Date.now() - _cachedEmployeesFetchedAt < VE_LIST_CACHE_TTL_MS) {
            if (mountedRef.current) {
                setEmployees(_cachedEmployees);
                setLoading(false);
                setRefreshing(false);
            }
            return;
        }
        const requestSeq = requestSeqRef.current + 1;
        requestSeqRef.current = requestSeq;
        // Stale-while-revalidate: if we have cached data, skip the loading spinner
        // and silently refresh in the background.
        const showLoading = options?.showLoading !== false && _cachedEmployees === null;
        if (showLoading) setLoading(true);
        else setRefreshing(true);
        fn()
            .then((result) => {
                const isCurrent = requestSeq === requestSeqRef.current;
                if (!mountedRef.current) {
                    // Component unmounted mid-flight - still update cache so
                    // the next mount shows fresh data (but only if not superseded).
                    if (isCurrent && Array.isArray(result)) {
                        _cachedEmployees = result;
                        _cachedEmployeesFetchedAt = Date.now();
                        consecutiveRefreshFailuresRef.current = 0;
                    }
                    return;
                }
                if (!isCurrent) return; // superseded by a newer request
                const list = Array.isArray(result) ? result : [];
                _cachedEmployees = list;
                _cachedEmployeesFetchedAt = Date.now();
                consecutiveRefreshFailuresRef.current = 0;
                setEmployees(list);
                setError("");
            })
            .catch(() => {
                if (!mountedRef.current || requestSeq !== requestSeqRef.current) return;
                consecutiveRefreshFailuresRef.current += 1;
                // Only show error state if we have no cached data at all
                if (_cachedEmployees === null) {
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

    // Throttled refresh: coalesce bursty websocket status/list events.
    const throttledRefresh = useCallback((options?: { force?: boolean }) => {
        if (throttleRef.current) {
            pendingRefreshRef.current = true;
            return;
        }
        fetchList({ showLoading: false, force: options?.force });
        throttleRef.current = setTimeout(() => {
            throttleRef.current = null;
            if (pendingRefreshRef.current) {
                pendingRefreshRef.current = false;
                fetchList({ showLoading: false, force: true });
            }
        }, VE_LIST_EVENT_THROTTLE_MS);
    }, [fetchList]);

    // WebSocket event listeners
    useEffect(() => {
        mountedRef.current = true;
        const unsub1 = EventsOn("ve:list_update", () => throttledRefresh({ force: true }));
        const unsub2 = EventsOn("ve:status_change", () => throttledRefresh({ force: true }));
        scheduleNextPoll();
        return () => {
            mountedRef.current = false;
            if (pollTimerRef.current) {
                window.clearTimeout(pollTimerRef.current);
                pollTimerRef.current = null;
            }
            if (throttleRef.current) {
                clearTimeout(throttleRef.current);
                throttleRef.current = null;
            }
            if (typeof unsub1 === "function") unsub1();
            else EventsOff("ve:list_update");
            if (typeof unsub2 === "function") unsub2();
            else EventsOff("ve:status_change");
        };
    }, [scheduleNextPoll, throttledRefresh]);

    // Close context menu on outside click or Escape
    useEffect(() => {
        if (!contextMenu) return;
        const handler = () => setContextMenu(null);
        const keyHandler = (event: KeyboardEvent) => {
            if (event.key === "Escape") setContextMenu(null);
        };
        document.addEventListener("click", handler);
        document.addEventListener("keydown", keyHandler);
        return () => {
            document.removeEventListener("click", handler);
            document.removeEventListener("keydown", keyHandler);
        };
    }, [contextMenu]);

    // Clamp context menu position to viewport bounds
    const menuRef = useRef<HTMLDivElement>(null);
    const renameInputRef = useRef<HTMLInputElement | null>(null);
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

    useEffect(() => {
        if (!renamingEmployee) return;
        const timer = window.setTimeout(() => renameInputRef.current?.focus(), 0);
        return () => window.clearTimeout(timer);
    }, [renamingEmployee]);

    const openRenameDialog = useCallback((ve: VirtualEmployeeEntry) => {
        const index = employees.findIndex((employee) => employee.id === ve.id);
        setContextMenu(null);
        setRenamingEmployee(ve);
        setRenameValue(displayNameForVirtualEmployee(ve, index >= 0 ? index : 0, lang, favoriteEmployeeNames));
        setRenameError("");
        setRenameSaving(false);
    }, [employees, favoriteEmployeeNames, lang]);

    const saveRename = useCallback(async () => {
        if (!renamingEmployee || renameSaving) return;
        const nextName = renameValue.trim();
        if (!nextName) return;
        setRenameSaving(true);
        setRenameError("");
        try {
            await onRenameEmployee?.(renamingEmployee, nextName);
            setEmployees((prev) => prev.map((employee) => (
                participantIdentityMatches(employee.id, renamingEmployee.id) || participantIdentityMatches(employee.machine_id, renamingEmployee.machine_id)
                    ? { ...employee, name: nextName }
                    : employee
            )));
            if (!mountedRef.current) return;
            setRenamingEmployee(null);
        } catch (error) {
            if (!mountedRef.current) return;
            console.error("Failed to rename digital employee:", error);
            setRenameError(isZh ? "\u6539\u540d\u5931\u8d25\uff0c\u8bf7\u91cd\u8bd5\u3002" : "Rename failed. Please try again.");
        } finally {
            if (mountedRef.current) setRenameSaving(false);
        }
    }, [isZh, onRenameEmployee, renameSaving, renameValue, renamingEmployee]);

    // --- Render ---

    const onlineEmployees = employees.filter(isVirtualEmployeeOnline);
    const normalizedQuery = query.trim().toLowerCase();
    const visibleEmployees = normalizedQuery
        ? onlineEmployees.filter((employee, index) => {
            const displayName = displayNameForVirtualEmployee(employee, index, lang, favoriteEmployeeNames);
            return [
                displayName,
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
        <div className="vet-list-shell" data-testid={options?.testId || "ve-list-container"}>
            <div
                style={{
                    position: "sticky",
                    top: 0,
                    zIndex: 2,
                    display: "flex",
                    alignItems: "center",
                    gap: 8,
                    padding: "8px 10px 8px 12px",
                    // Match the hosting pane surface (--theme-page-bg) so the sticky
                    // bar doesn't draw a lighter stripe over dark schemes.
                    background: `var(--theme-page-bg, ${theme.bg})`,
                    borderBottom: `1px solid ${theme.divider}`,
                    boxSizing: "border-box",
                    width: "100%",
                }}
            >
                <div style={{ flex: 1, position: "relative", minWidth: 0, display: "flex", alignItems: "center" }}>
                    <span
                        aria-hidden="true"
                        style={{
                            position: "absolute",
                            left: 8,
                            top: "50%",
                            transform: "translateY(-50%)",
                            display: "inline-flex",
                            color: theme.textMuted,
                            pointerEvents: "none",
                        }}
                    >
                        <TitleBarToolIcon name="search" />
                    </span>
                    <input
                        data-testid="ve-search-input"
                        aria-label={searchLabel}
                        value={query}
                        onChange={(event) => setQuery(event.target.value)}
                        onFocus={() => setSearchFocused(true)}
                        onBlur={() => setSearchFocused(false)}
                        placeholder={searchPlaceholder}
                        style={{
                            flex: 1,
                            minWidth: 0,
                            height: 28,
                            borderRadius: 8,
                            border: `1px solid ${searchFocused ? theme.btnColor : theme.divider}`,
                            background: theme.fieldBg,
                            color: theme.text,
                            padding: "0 9px 0 27px",
                            fontSize: 12,
                            outline: "none",
                            transition: "border-color 0.15s",
                        }}
                    />
                </div>
                <button
                    type="button"
                    data-testid="ve-refresh-button"
                    aria-label={refreshLabel}
                    title={refreshLabel}
                    disabled={refreshing}
                    onClick={() => fetchList({ showLoading: false, force: true })}
                    style={{
                        width: 28,
                        height: 28,
                        borderRadius: 8,
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
                    <span aria-hidden="true" className={refreshing ? "vet-spinning" : undefined} style={{ display: "inline-flex", lineHeight: 1 }}>
                        <TitleBarToolIcon name="refresh" />
                    </span>
                </button>
            </div>
            <div style={options?.center ? { display: "flex", alignItems: "center", justifyContent: "center", minHeight: "calc(100% - 45px)", padding: "0 12px 12px" } : undefined}>
                {children}
            </div>
        </div>
    );

    const emptyStatePanel = (icon: "search" | "book", text: string, textTestId?: string) => (
        <div
            data-testid={textTestId}
            style={{
                display: "flex",
                flexDirection: "column",
                alignItems: "center",
                gap: 10,
                padding: "36px 16px",
                color: theme.textMuted,
                fontSize: 12,
                textAlign: "center",
            }}
        >
            <span
                aria-hidden="true"
                style={{
                    width: 40,
                    height: 40,
                    borderRadius: "50%",
                    display: "inline-flex",
                    alignItems: "center",
                    justifyContent: "center",
                    background: theme.fieldBg,
                    border: `1px solid ${theme.divider}`,
                    color: theme.textMuted,
                    opacity: 0.9,
                }}
            >
                <TitleBarToolIcon name={icon} />
            </span>
            <span>{text}</span>
        </div>
    );

    if (loading) {
        return (
            <div style={{ display: "flex", alignItems: "center", justifyContent: "center", gap: 8, padding: 16, color: theme.textMuted, fontSize: 12 }}>
                <span className="vet-spinning" aria-hidden="true" style={{ display: "inline-flex", color: theme.btnColor }}>
                    <TitleBarToolIcon name="refresh" />
                </span>
                <span data-testid="ve-loading">{isZh ? "\u52a0\u8f7d\u4e2d..." : "Loading..."}</span>
            </div>
        );
    }

    if (error === "hub_unavailable") {
        return renderShell(
            emptyStatePanel("book", isZh ? "Hub \u4e0d\u53ef\u7528\uff0c\u65e0\u6cd5\u83b7\u53d6\u6570\u5b57\u5458\u5de5\u5217\u8868" : "Hub unavailable", "ve-empty-hub"),
            { testId: "ve-error-container", center: true }
        );
    }

    if (visibleEmployees.length === 0) {
        return renderShell(
            emptyStatePanel("search", emptyListText, "ve-empty-list"),
            { testId: "ve-empty-container", center: true }
        );
    }

    return renderShell(
        <>
            {visibleEmployees.map((ve, index) => {
                const displayName = displayNameForVirtualEmployee(ve, index, lang, favoriteEmployeeNames);
                const displayEmployee = { ...ve, name: displayName };
                return (
                <div
                    key={ve.id}
                    data-testid={`ve-item-${ve.id}`}
                    role="button"
                    tabIndex={0}
                    title={ve.skill_description || displayName}
                    onClick={() => onStartConversation(displayEmployee)}
                    onKeyDown={(e) => {
                        if (e.key !== "Enter" && e.key !== " ") return;
                        e.preventDefault();
                        onStartConversation(displayEmployee);
                    }}
                    onContextMenu={(e) => {
                        e.preventDefault();
                        setContextMenu({ x: e.clientX, y: e.clientY, ve, displayName });
                    }}
                    style={{
                        display: "flex",
                        alignItems: "center",
                        gap: 10,
                        padding: "10px 12px",
                        cursor: "pointer",
                        borderBottom: `1px solid ${theme.divider}`,
                        transition: "background 0.15s",
                    }}
                    onMouseEnter={(e) => { (e.currentTarget as HTMLElement).style.background = theme.fieldBg; }}
                    onMouseLeave={(e) => { (e.currentTarget as HTMLElement).style.background = ""; }}
                >
                    <span className="vet-avatar-wrap">
                        <EmployeeAvatar ve={ve} displayName={displayName} />
                        <span style={{ position: "absolute", right: -1, bottom: -1 }}>
                            <VEStatusDot
                                status={isVirtualEmployeeOnline(ve) ? "online" : "offline"}
                                size={8}
                                variant="badge"
                                dataTestId={`ve-status-${ve.id}`}
                                title={isVirtualEmployeeOnline(ve) ? (isZh ? "在线" : "Online") : (isZh ? "离线" : "Offline")}
                            />
                        </span>
                    </span>

                    {/* Name + skill description */}
                    <div className="vet-row-text">
                        <div className="vet-row-name-line">
                            <span style={{ color: theme.text, fontSize: 13, fontWeight: 600, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis", minWidth: 0 }}>
                                {truncateText(displayName, 20)}
                            </span>
                            <span
                                className="vet-policy-icon"
                                role="img"
                                aria-label={policyLabel(ve.access_policy, lang)}
                                title={policyLabel(ve.access_policy, lang)}
                            >
                                <VEPolicyIcon name={policyIcon(ve.access_policy)} isDark={darkSurface} />
                            </span>
                            {ve.access_policy === "per_request" && (
                                <span
                                    data-testid={`ve-badge-${ve.id}`}
                                    title={policyLabel(ve.access_policy, lang)}
                                    style={{
                                        fontSize: 10,
                                        padding: "1px 6px",
                                        borderRadius: 999,
                                        background: theme.errorBg || "#fbf1f0",
                                        color: theme.errorText || "#c43d34",
                                        border: `1px solid ${theme.errorBorder || "rgba(196, 61, 52, 0.24)"}`,
                                        whiteSpace: "nowrap",
                                        flexShrink: 0,
                                        fontWeight: 600,
                                    }}
                                >
                                    {isZh ? "需同意" : "Needs approval"}
                                </span>
                            )}
                        </div>
                        <div style={{ color: theme.textMuted, fontSize: 12, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis", textAlign: "left", marginTop: 1 }}>
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
                        borderRadius: 10,
                        boxShadow: "0 12px 32px rgba(15, 23, 42, 0.18)",
                        zIndex: 9999,
                        minWidth: 172,
                        padding: "4px",
                    }}
                >
                    {/* 对话 */}
                    <div
                        data-testid="ve-menu-conversation"
                        role="menuitem"
                        onClick={() => { onStartConversation({ ...contextMenu.ve, name: contextMenu.displayName }); setContextMenu(null); }}
                        style={{ display: "flex", alignItems: "center", gap: 8, padding: "8px 10px", cursor: "pointer", fontSize: 13, color: theme.text, borderRadius: 8 }}
                        onMouseEnter={(e) => { (e.currentTarget as HTMLElement).style.background = theme.fieldBg; }}
                        onMouseLeave={(e) => { (e.currentTarget as HTMLElement).style.background = ""; }}
                    >
                        <span className="vet-menu-icon" style={{ width: 26, display: "inline-flex", justifyContent: "center", flexShrink: 0, color: theme.textMuted }}>
                            <VEMenuGlyph name="chat" />
                        </span>
                        <span>{isZh ? "对话" : "Chat"}</span>
                    </div>
                    {/* Favorite toggle - only when callbacks are wired */}
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
                            style={{ display: "flex", alignItems: "center", gap: 8, padding: "8px 10px", cursor: "pointer", fontSize: 13, color: theme.text, borderRadius: 8 }}
                            onMouseEnter={(e) => { (e.currentTarget as HTMLElement).style.background = theme.fieldBg; }}
                            onMouseLeave={(e) => { (e.currentTarget as HTMLElement).style.background = ""; }}
                        >
                            <span
                                className="vet-menu-icon"
                                style={{ width: 26, display: "inline-flex", justifyContent: "center", flexShrink: 0, color: isFav ? (darkSurface ? FAVORITE_STAR_TONE.dark : FAVORITE_STAR_TONE.light) : theme.textMuted }}
                            >
                                <VEMenuGlyph name="star" filled={isFav} />
                            </span>
                            <span>{isFav ? (isZh ? "取消常用" : "Remove from favorites") : (isZh ? "设为常用" : "Set as favorite")}</span>
                        </div>
                    )}
                    {onRenameEmployee && (
                        <div
                            data-testid="ve-menu-rename"
                            role="menuitem"
                            onClick={() => openRenameDialog(contextMenu.ve)}
                            style={{ display: "flex", alignItems: "center", gap: 8, padding: "8px 10px", cursor: "pointer", fontSize: 13, color: theme.text, borderRadius: 8 }}
                            onMouseEnter={(e) => { (e.currentTarget as HTMLElement).style.background = theme.fieldBg; }}
                            onMouseLeave={(e) => { (e.currentTarget as HTMLElement).style.background = ""; }}
                        >
                            <span className="vet-menu-icon" style={{ width: 26, display: "inline-flex", justifyContent: "center", flexShrink: 0, color: theme.textMuted }}>
                                <VEMenuGlyph name="pencil" />
                            </span>
                            <span>{isZh ? "\u6539\u540d" : "Rename"}</span>
                        </div>
                    )}
                    {/* 查看信息 */}
                    <div
                        data-testid="ve-menu-view-info"
                        role="menuitem"
                        onClick={() => { setViewInfoVE({ ve: contextMenu.ve, displayName: contextMenu.displayName }); setContextMenu(null); }}
                        style={{ display: "flex", alignItems: "center", gap: 8, padding: "8px 10px", cursor: "pointer", fontSize: 13, color: theme.text, borderRadius: 8 }}
                        onMouseEnter={(e) => { (e.currentTarget as HTMLElement).style.background = theme.fieldBg; }}
                        onMouseLeave={(e) => { (e.currentTarget as HTMLElement).style.background = ""; }}
                    >
                        <span className="vet-menu-icon" style={{ width: 26, display: "inline-flex", justifyContent: "center", flexShrink: 0, color: theme.textMuted }}>
                            <VEMenuGlyph name="info" />
                        </span>
                        <span>{isZh ? "\u67e5\u770b\u4fe1\u606f" : "View Info"}</span>
                    </div>
                </div>
                );
            })()}
            {renamingEmployee && (
                <div
                    className="vet-dialog-overlay"
                    role="dialog"
                    aria-modal="true"
                    aria-labelledby="ve-rename-title"
                    data-testid="ve-rename-dialog"
                    onPointerDown={() => { if (!renameSaving) setRenamingEmployee(null); }}
                >
                    <form
                        onPointerDown={(e) => e.stopPropagation()}
                        onSubmit={(e) => { e.preventDefault(); void saveRename(); }}
                        style={{
                            width: "min(360px, calc(100vw - 32px))",
                            padding: 18,
                            borderRadius: 8,
                            border: `1px solid ${theme.divider}`,
                            background: theme.bg,
                            boxShadow: "0 18px 44px rgba(15, 23, 42, 0.24)",
                        }}
                    >
                        <h2 id="ve-rename-title" style={{ margin: "0 0 14px", fontSize: 16, lineHeight: 1.3, color: theme.text }}>
                            {isZh ? "\u6539\u540d\u6570\u5b57\u5458\u5de5" : "Rename digital employee"}
                        </h2>
                        <label style={{ display: "grid", gap: 8, fontSize: 13, fontWeight: 600, color: theme.text }}>
                            {isZh ? "\u663e\u793a\u540d\u79f0" : "Display name"}
                            <input
                                ref={renameInputRef}
                                value={renameValue}
                                disabled={renameSaving}
                                aria-invalid={renameError ? "true" : undefined}
                                onChange={(e) => setRenameValue(e.target.value)}
                                maxLength={32}
                                data-testid="ve-rename-input"
                                style={{
                                    height: 44,
                                    borderRadius: 8,
                                    border: `1px solid ${theme.divider}`,
                                    padding: "0 10px",
                                    background: theme.fieldBg,
                                    color: theme.text,
                                    font: "inherit",
                                }}
                            />
                            {renameError && <span role="alert" style={{ color: theme.errorText || "#c43d34", fontSize: 12, lineHeight: 1.4 }}>{renameError}</span>}
                        </label>
                        <div className="vet-dialog-actions">
                            <button type="button" onClick={() => setRenamingEmployee(null)} disabled={renameSaving} style={{ minWidth: 72, minHeight: 40, borderRadius: 8, border: `1px solid ${theme.divider}`, background: theme.bg, color: theme.text, font: "inherit", fontWeight: 700, cursor: renameSaving ? "default" : "pointer" }}>
                                {isZh ? "\u53d6\u6d88" : "Cancel"}
                            </button>
                            <button type="submit" disabled={!renameValue.trim() || renameSaving} data-testid="ve-rename-save" style={primaryFilledButtonStyle(theme, { minWidth: 72, minHeight: 40, borderRadius: 8, font: "inherit", fontWeight: 700, opacity: renameValue.trim() && !renameSaving ? 1 : 0.55, cursor: renameValue.trim() && !renameSaving ? "pointer" : "default" })}>
                                {isZh ? "\u4fdd\u5b58" : "Save"}
                            </button>
                        </div>
                    </form>
                </div>
            )}
            {/* Info dialog */}
            {viewInfoVE && (
                <div
                    className="vet-dialog-overlay"
                    role="dialog"
                    aria-modal="true"
                    aria-labelledby="ve-info-title"
                    data-testid="ve-info-dialog"
                    onPointerDown={() => setViewInfoVE(null)}
                >
                    <div
                        onPointerDown={(e) => e.stopPropagation()}
                        onKeyDown={(e) => { if (e.key === "Escape") setViewInfoVE(null); }}
                        tabIndex={-1}
                        style={{
                            width: "min(400px, calc(100vw - 32px))",
                            maxHeight: "calc(100vh - 64px)",
                            overflow: "auto",
                            padding: 24,
                            borderRadius: 12,
                            border: `1px solid ${theme.divider}`,
                            background: theme.bg,
                            boxShadow: "0 18px 44px rgba(15, 23, 42, 0.24)",
                            outline: "none",
                        }}
                    >
                        {/* Large avatar + name */}
                        <div className="vet-info-avatar-block">
                            <div className="vet-info-avatar-wrap">
                                {safeAvatarDataURL(viewInfoVE.ve.avatar_data_url) ? (
                                    <img
                                        src={safeAvatarDataURL(viewInfoVE.ve.avatar_data_url)!}
                                        alt={viewInfoVE.displayName}
                                        style={{ width: 72, height: 72, borderRadius: "50%", objectFit: "cover", display: "block", border: `3px solid ${theme.divider}` }}
                                    />
                                ) : (
                                    <span style={{
                                        width: 72, height: 72, borderRadius: "50%",
                                        display: "inline-flex", alignItems: "center", justifyContent: "center",
                                        background: `color-mix(in srgb, ${theme.btnColor} 10%, ${theme.fieldBg})`, color: theme.btnColor,
                                        fontSize: 28, fontWeight: 700,
                                        border: `3px solid ${theme.divider}`,
                                    }}>
                                        {viewInfoVE.displayName.trim().slice(0, 2).toUpperCase() || "D"}
                                    </span>
                                )}
                                <span style={{ position: "absolute", bottom: 2, right: 2 }}>
                                    <VEStatusDot
                                        status={isVirtualEmployeeOnline(viewInfoVE.ve) ? "online" : "offline"}
                                        size={10}
                                        variant="badge"
                                    />
                                </span>
                            </div>
                            <h2 id="ve-info-title" style={{ margin: 0, fontSize: 18, fontWeight: 700, color: theme.text, textAlign: "center" }}>
                                {viewInfoVE.displayName}
                            </h2>
                            {viewInfoVE.ve.resident && (
                                <span style={{ fontSize: 11, color: theme.btnColor, background: theme.fieldBg, padding: "2px 8px", borderRadius: 4, fontWeight: 600 }}>
                                    {isZh ? "\u5e38\u9a7b" : "Resident"}
                                </span>
                            )}
                        </div>

                        {/* Info rows */}
                        <div className="vet-info-rows">
                            {/* Status */}
                            <div className="vet-info-row">
                                <span style={{ fontSize: 12, fontWeight: 600, color: theme.textMuted, minWidth: 72, flexShrink: 0 }}>{isZh ? "\u72b6\u6001" : "Status"}</span>
                                <span style={{ display: "inline-flex", alignItems: "center", gap: 6, fontSize: 13, color: isVirtualEmployeeOnline(viewInfoVE.ve) ? (darkSurface ? "#7aa89a" : "#4f7f6f") : (darkSurface ? "#a8b8c8" : "#9ca3af"), fontWeight: 600 }}>
                                    <VEStatusDot status={isVirtualEmployeeOnline(viewInfoVE.ve) ? "online" : "offline"} size={7} />
                                    {isVirtualEmployeeOnline(viewInfoVE.ve) ? (isZh ? "\u5728\u7ebf" : "Online") : (isZh ? "\u79bb\u7ebf" : "Offline")}
                                </span>
                            </div>

                            {/* Source */}
                            <div className="vet-info-row">
                                <span style={{ fontSize: 12, fontWeight: 600, color: theme.textMuted, minWidth: 72, flexShrink: 0 }}>{isZh ? "\u6765\u6e90" : "Source"}</span>
                                <span style={{ fontSize: 13, color: theme.text }}>
                                    {viewInfoVE.ve.machine_id
                                        ? (isZh ? "\u865a\u62df\u5458\u5de5\uff08\u8fdc\u7a0b\uff09" : "Virtual Employee (Remote)")
                                        : (isZh ? "\u672c\u673a\u5458\u5de5" : "Local Employee")}
                                </span>
                            </div>

                            {/* Access policy */}
                            <div className="vet-info-row">
                                <span style={{ fontSize: 12, fontWeight: 600, color: theme.textMuted, minWidth: 72, flexShrink: 0 }}>{isZh ? "\u8bbf\u95ee\u7b56\u7565" : "Access Policy"}</span>
                                <span style={{ fontSize: 13, color: theme.text, display: "inline-flex", alignItems: "center", gap: 6 }}>
                                    <VEPolicyIcon name={policyIcon(viewInfoVE.ve.access_policy)} size={13} isDark={darkSurface} />
                                    {policyLabel(viewInfoVE.ve.access_policy, lang)}
                                </span>
                            </div>

                            {/* Accessible departments */}
                            <div className="vet-info-row">
                                <span style={{ fontSize: 12, fontWeight: 600, color: theme.textMuted, minWidth: 72, flexShrink: 0 }}>{isZh ? "\u53ef\u8bbf\u95ee\u90e8\u95e8" : "Departments"}</span>
                                <span style={{ fontSize: 13, color: theme.text }}>
                                    {viewInfoVE.ve.whitelist && viewInfoVE.ve.whitelist.length > 0
                                        ? viewInfoVE.ve.whitelist.join('\uff1b')
                                        : (isZh ? "\u65e0\u9650\u5236" : "Unrestricted")}
                                </span>
                            </div>

                            {/* Registration time */}
                            {viewInfoVE.ve.registered_at && (
                            <div className="vet-info-row">
                                <span style={{ fontSize: 12, fontWeight: 600, color: theme.textMuted, minWidth: 72, flexShrink: 0 }}>{isZh ? "\u6ce8\u518c\u65f6\u95f4" : "Registered"}</span>
                                <span style={{ fontSize: 13, color: theme.text }}>
                                    {(() => { try { const d = new Date(viewInfoVE.ve.registered_at!); return isNaN(d.getTime()) ? viewInfoVE.ve.registered_at : d.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" }); } catch { return viewInfoVE.ve.registered_at; } })()}
                                </span>
                            </div>
                            )}

                            {/* ID */}
                            <div className="vet-info-row">
                                <span style={{ fontSize: 12, fontWeight: 600, color: theme.textMuted, minWidth: 72, flexShrink: 0 }}>ID</span>
                                <span style={{ fontSize: 11, color: theme.text, fontFamily: "monospace", opacity: 0.7, wordBreak: "break-all" }}>
                                    {viewInfoVE.ve.machine_id || viewInfoVE.ve.id}
                                </span>
                            </div>
                        </div>

                        {/* Skill description */}
                        <div className="vet-info-skill">
                            <div style={{ fontSize: 12, fontWeight: 600, color: theme.textMuted, marginBottom: 6 }}>{isZh ? "\u6280\u80fd\u4ecb\u7ecd" : "Skill"}</div>
                            <div style={{
                                fontSize: 13, lineHeight: 1.6, color: theme.text,
                                padding: "10px 12px", borderRadius: 8,
                                background: theme.fieldBg,
                                border: `1px solid ${theme.divider}`,
                                whiteSpace: "pre-wrap", wordBreak: "break-word",
                            }}>
                                {viewInfoVE.ve.skill_description || (isZh ? "\u6682\u65e0\u6280\u80fd\u4ecb\u7ecd" : "No description available")}
                            </div>
                        </div>

                        {/* Close button */}
                        <div className="vet-info-close">
                            <button
                                type="button"
                                onClick={() => setViewInfoVE(null)}
                                style={{ minWidth: 100, minHeight: 40, borderRadius: 8, border: `1px solid ${theme.divider}`, background: theme.bg, color: theme.text, font: "inherit", fontWeight: 700, cursor: "pointer" }}
                            >
                                {isZh ? "\u5173\u95ed" : "Close"}
                            </button>
                        </div>
                    </div>
                </div>
            )}
        </>
    );
}

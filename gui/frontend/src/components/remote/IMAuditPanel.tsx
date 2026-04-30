import { useState, useEffect, useCallback, useRef } from "react";
import { OpenFileOrShowInFolder } from "../../../wailsjs/go/main/App";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface IMAuditMessage {
    id: number;
    timestamp: string;
    user_id: string;
    platform: string;
    role: "user" | "assistant";
    content: string;
}

interface IMAuditQueryResult {
    messages: IMAuditMessage[];
    total: number;
    page: number;
    page_size: number;
}

interface IMAuditPanelProps {
    platform: string; // "qq" | "telegram" | "weixin" | "lansenger"
    onClose: () => void;
    lang: string;
}

// ---------------------------------------------------------------------------
// Wails binding helpers (auto-generated bindings may not exist yet)
// ---------------------------------------------------------------------------

async function queryIMAuditMessages(platform: string, userID: string, keyword: string, page: number): Promise<IMAuditQueryResult> {
    // @ts-ignore
    return window.go?.main?.App?.QueryIMAuditMessages(platform, userID, keyword, page) ?? { messages: [], total: 0, page: 1, page_size: 50 };
}
async function deleteIMAuditMessagesBefore(days: number): Promise<number> {
    // @ts-ignore
    return window.go?.main?.App?.DeleteIMAuditMessagesBefore(days) ?? 0;
}
async function exportIMAuditCSV(platform: string, userID: string, keyword: string): Promise<string> {
    // @ts-ignore
    return window.go?.main?.App?.ExportIMAuditCSV(platform, userID, keyword) ?? "";
}
async function getIMAuditUsers(platform: string): Promise<string[]> {
    // @ts-ignore
    return window.go?.main?.App?.GetIMAuditUsers(platform) ?? [];
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const platformLabels: Record<string, string> = {
    qq: "QQ",
    telegram: "Telegram",
    weixin: "微信",
    lansenger: "蓝信",
};

function formatTimestamp(ts: string): string {
    try {
        const d = new Date(ts);
        const pad = (n: number) => String(n).padStart(2, "0");
        return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
    } catch {
        return ts;
    }
}

/** Simple inline markdown: bold, code, links */
function renderSimpleMarkdown(text: string): React.ReactNode[] {
    const parts: React.ReactNode[] = [];
    const re = /(`[^`]+`)|(\*\*[^*]+\*\*)|(\[[^\]]+\]\([^)]+\))/g;
    let lastIndex = 0;
    let match: RegExpExecArray | null;
    let idx = 0;
    while ((match = re.exec(text)) !== null) {
        if (match.index > lastIndex) parts.push(text.slice(lastIndex, match.index));
        const m = match[0];
        if (match[1]) {
            parts.push(<code key={idx++} style={{ background: "rgba(0,0,0,0.06)", padding: "1px 4px", borderRadius: "3px", fontSize: "0.9em" }}>{m.slice(1, -1)}</code>);
        } else if (match[2]) {
            parts.push(<strong key={idx++}>{m.slice(2, -2)}</strong>);
        } else if (match[3]) {
            const lm = m.match(/^\[([^\]]+)\]\(([^)]+)\)$/);
            if (lm) parts.push(<a key={idx++} href={lm[2]} target="_blank" rel="noopener noreferrer" style={{ color: "var(--theme-primary, #6366f1)" }}>{lm[1]}</a>);
        }
        lastIndex = re.lastIndex;
    }
    if (lastIndex < text.length) parts.push(text.slice(lastIndex));
    return parts.length > 0 ? parts : [text];
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export function IMAuditPanel({ platform, onClose, lang }: IMAuditPanelProps) {
    const [messages, setMessages] = useState<IMAuditMessage[]>([]);
    const [total, setTotal] = useState(0);
    const [page, setPage] = useState(1);
    const [pageSize, setPageSize] = useState(50);
    const [keyword, setKeyword] = useState("");
    const [searchInput, setSearchInput] = useState("");
    const [userID, setUserID] = useState("");
    const [users, setUsers] = useState<string[]>([]);
    const [loading, setLoading] = useState(false);
    const [exporting, setExporting] = useState(false);
    const [cleanupDays, setCleanupDays] = useState(30);
    const [showCleanup, setShowCleanup] = useState(false);
    const [refreshKey, setRefreshKey] = useState(0);
    const listRef = useRef<HTMLDivElement>(null);

    const isZh = lang === "zh-Hans" || lang === "zh-Hant";
    const totalPages = Math.max(1, Math.ceil(total / pageSize));

    // Load users list
    useEffect(() => {
        getIMAuditUsers(platform).then(setUsers).catch(() => {});
    }, [platform]);

    const [initDone, setInitDone] = useState(false);

    // On first open, jump to the last page so user sees newest messages.
    useEffect(() => {
        if (initDone) return;
        (async () => {
            try {
                const result = await queryIMAuditMessages(platform, "", "", 1);
                const lastPage = Math.max(1, Math.ceil(result.total / (result.page_size || 50)));
                setTotal(result.total);
                setPageSize(result.page_size || 50);
                setPage(lastPage);
            } catch {}
            setInitDone(true);
        })();
    }, [platform, initDone]);

    // Load messages for current page.
    const loadMessages = useCallback(async () => {
        if (!initDone) return;
        setLoading(true);
        try {
            const result = await queryIMAuditMessages(platform, userID, keyword, page);
            setMessages(result.messages || []);
            setTotal(result.total);
            setPageSize(result.page_size || 50);
        } catch (err) {
            console.error("[im-audit] query error:", err);
        } finally {
            setLoading(false);
        }
    }, [platform, userID, keyword, page, refreshKey, initDone]);

    useEffect(() => { loadMessages(); }, [loadMessages]);

    // Scroll to bottom after messages load (newest at bottom, like chat).
    useEffect(() => {
        if (messages.length > 0 && listRef.current) {
            requestAnimationFrame(() => {
                listRef.current?.scrollTo({ top: listRef.current.scrollHeight });
            });
        }
    }, [messages]);

    const handleSearch = () => {
        setKeyword(searchInput.trim());
        setPage(1);
    };

    const handleCleanup = async () => {
        if (!confirm(isZh ? `确定删除 ${cleanupDays} 天前的记录？` : `Delete records older than ${cleanupDays} days?`)) return;
        try {
            const n = await deleteIMAuditMessagesBefore(cleanupDays);
            alert(isZh ? `已删除 ${n} 条记录` : `Deleted ${n} records`);
            // Reset filters and bump refreshKey to force reload even if
            // all filter values are already at their defaults.
            setPage(1);
            setKeyword("");
            setSearchInput("");
            setUserID("");
            setRefreshKey(k => k + 1);
            getIMAuditUsers(platform).then(setUsers).catch(() => {});
        } catch (err) {
            alert(String(err));
        }
        setShowCleanup(false);
    };

    const handleExport = async () => {
        setExporting(true);
        try {
            const filePath = await exportIMAuditCSV(platform, userID, keyword);
            if (filePath) {
                OpenFileOrShowInFolder(filePath);
            }
        } catch (err) {
            alert(String(err));
        } finally {
            setExporting(false);
        }
    };

    return (
        <div style={{
            position: "fixed", inset: 0, zIndex: 20000,
            display: "flex", alignItems: "center", justifyContent: "center",
            background: "rgba(0,0,0,0.45)",
        }} onClick={onClose}>
            <div style={{
                width: "min(780px, 92vw)", maxHeight: "88vh",
                display: "flex", flexDirection: "column",
                background: "var(--theme-surface, #fff)",
                borderRadius: "12px",
                boxShadow: "0 8px 32px rgba(0,0,0,0.18)",
                overflow: "hidden",
            }} onClick={e => e.stopPropagation()}>

                {/* Header */}
                <div style={{
                    display: "flex", alignItems: "center", justifyContent: "space-between",
                    padding: "14px 20px", borderBottom: "1px solid var(--theme-border, #e5e7eb)",
                    flexShrink: 0,
                }}>
                    <div style={{ display: "flex", alignItems: "center", gap: "8px" }}>
                        <span style={{ fontSize: "16px" }}>📋</span>
                        <span style={{ fontSize: "15px", fontWeight: 600, color: "var(--theme-text-primary, #1f2937)" }}>
                            {isZh ? `${platformLabels[platform] || platform} 消息审计` : `${platformLabels[platform] || platform} Message Audit`}
                        </span>
                        <span style={{ fontSize: "12px", color: "var(--theme-text-muted, #9ca3af)" }}>
                            {isZh ? `共 ${total} 条` : `${total} total`}
                        </span>
                    </div>
                    <button onClick={onClose} style={{
                        background: "none", border: "none", fontSize: "18px",
                        color: "var(--theme-text-muted, #9ca3af)", cursor: "pointer", padding: "4px",
                    }}>✕</button>
                </div>

                {/* Toolbar */}
                <div style={{
                    display: "flex", alignItems: "center", gap: "8px",
                    padding: "10px 20px", borderBottom: "1px solid var(--theme-border, #e5e7eb)",
                    flexWrap: "wrap", flexShrink: 0,
                }}>
                    {/* Search */}
                    <input
                        type="text"
                        value={searchInput}
                        onChange={e => setSearchInput(e.target.value)}
                        onKeyDown={e => { if (e.key === "Enter") handleSearch(); }}
                        placeholder={isZh ? "搜索消息内容..." : "Search messages..."}
                        style={{
                            flex: 1, minWidth: "140px", padding: "6px 10px",
                            borderRadius: "6px", border: "1px solid var(--theme-border, #d1d5db)",
                            fontSize: "13px", background: "var(--theme-surface-muted, #f9fafb)",
                            color: "var(--theme-text-primary, #1f2937)",
                            outline: "none",
                        }}
                    />
                    <button onClick={handleSearch} style={toolbarBtnStyle}>
                        {isZh ? "搜索" : "Search"}
                    </button>
                    <button onClick={() => {
                        // Jump to last page and refresh to show newest messages.
                        const lastPage = Math.max(1, Math.ceil(total / pageSize));
                        setPage(lastPage);
                        setRefreshKey(k => k + 1);
                    }} disabled={loading} style={toolbarBtnStyle}>
                        {loading ? "..." : "🔄"}
                    </button>

                    {/* User filter */}
                    {users.length > 0 && (
                        <select
                            value={userID}
                            onChange={e => { setUserID(e.target.value); setPage(1); }}
                            style={{
                                padding: "5px 8px", borderRadius: "6px",
                                border: "1px solid var(--theme-border, #d1d5db)",
                                fontSize: "12px", background: "var(--theme-surface-muted, #f9fafb)",
                                color: "var(--theme-text-primary, #1f2937)",
                            }}
                        >
                            <option value="">{isZh ? "全部用户" : "All users"}</option>
                            {users.map(u => <option key={u} value={u}>{u}</option>)}
                        </select>
                    )}

                    {/* Export CSV */}
                    <button onClick={handleExport} disabled={exporting} style={toolbarBtnStyle}>
                        {exporting ? "..." : (isZh ? "📥 导出 CSV" : "📥 Export CSV")}
                    </button>

                    {/* Cleanup */}
                    <div style={{ position: "relative" }}>
                        <button onClick={() => setShowCleanup(!showCleanup)} style={{
                            ...toolbarBtnStyle,
                            color: "var(--theme-danger, #ef4444)",
                            borderColor: "var(--theme-danger, #ef4444)",
                        }}>
                            {isZh ? "🗑 清理" : "🗑 Cleanup"}
                        </button>
                        {showCleanup && (
                            <div style={{
                                position: "absolute", top: "100%", right: 0, marginTop: "4px",
                                background: "var(--theme-surface, #fff)",
                                border: "1px solid var(--theme-border, #d1d5db)",
                                borderRadius: "8px", padding: "10px 14px",
                                boxShadow: "0 4px 12px rgba(0,0,0,0.12)",
                                zIndex: 10, minWidth: "200px",
                            }}>
                                <div style={{ fontSize: "12px", color: "var(--theme-text-secondary, #6b7280)", marginBottom: "8px" }}>
                                    {isZh ? "删除多少天前的记录？" : "Delete records older than:"}
                                </div>
                                <div style={{ display: "flex", gap: "6px", flexWrap: "wrap", marginBottom: "10px" }}>
                                    {[7, 14, 30, 60, 90].map(d => (
                                        <button key={d} onClick={() => setCleanupDays(d)} style={{
                                            padding: "3px 10px", borderRadius: "12px", fontSize: "12px",
                                            border: cleanupDays === d ? "1.5px solid var(--theme-primary, #6366f1)" : "1px solid var(--theme-border, #d1d5db)",
                                            background: cleanupDays === d ? "var(--theme-info-bg, #eef2ff)" : "transparent",
                                            color: cleanupDays === d ? "var(--theme-primary, #6366f1)" : "var(--theme-text-secondary, #6b7280)",
                                            cursor: "pointer",
                                        }}>
                                            {d} {isZh ? "天" : "days"}
                                        </button>
                                    ))}
                                </div>
                                <button onClick={handleCleanup} style={{
                                    width: "100%", padding: "6px", borderRadius: "6px",
                                    border: "1px solid var(--theme-danger, #ef4444)", fontSize: "12px", fontWeight: 600,
                                    background: "var(--theme-danger-bg, rgba(239,68,68,0.12))", color: "var(--theme-danger, #ef4444)",
                                    cursor: "pointer",
                                }}>
                                    {isZh ? `确认删除 ${cleanupDays} 天前的记录` : `Delete records older than ${cleanupDays} days`}
                                </button>
                            </div>
                        )}
                    </div>
                </div>

                {/* Message list */}
                <div ref={listRef} style={{
                    flex: 1, overflowY: "auto", padding: "16px 20px",
                    display: "flex", flexDirection: "column", gap: "8px",
                }}>
                    {loading && (
                        <div style={{ textAlign: "center", padding: "20px", color: "var(--theme-text-muted, #9ca3af)", fontSize: "13px" }}>
                            {isZh ? "加载中..." : "Loading..."}
                        </div>
                    )}
                    {!loading && messages.length === 0 && (
                        <div style={{ textAlign: "center", padding: "40px 0", color: "var(--theme-text-muted, #9ca3af)", fontSize: "13px" }}>
                            {isZh ? "暂无消息记录" : "No messages found"}
                        </div>
                    )}
                    {!loading && messages.map(msg => {
                        const isUser = msg.role === "user";
                        return (
                            <div key={msg.id} style={{
                                display: "flex",
                                flexDirection: "column",
                                alignItems: isUser ? "flex-end" : "flex-start",
                                maxWidth: "100%",
                            }}>
                                {/* Meta line */}
                                <div style={{
                                    fontSize: "11px",
                                    color: "var(--theme-text-muted, #9ca3af)",
                                    marginBottom: "3px",
                                    display: "flex", gap: "6px", alignItems: "center",
                                    flexDirection: isUser ? "row-reverse" : "row",
                                }}>
                                    <span>{isUser ? (msg.user_id || "用户") : "🤖 MaClaw"}</span>
                                    <span>{formatTimestamp(msg.timestamp)}</span>
                                </div>
                                {/* Bubble */}
                                <div style={{
                                    maxWidth: "75%",
                                    padding: "10px 14px",
                                    borderRadius: isUser ? "14px 14px 4px 14px" : "14px 14px 14px 4px",
                                    background: isUser
                                        ? "var(--theme-primary, #6366f1)"
                                        : "var(--theme-surface-muted, #f3f4f6)",
                                    color: isUser
                                        ? "#fff"
                                        : "var(--theme-text-primary, #1f2937)",
                                    fontSize: "13px",
                                    lineHeight: 1.6,
                                    whiteSpace: "pre-wrap",
                                    wordBreak: "break-word",
                                    textAlign: "left",
                                }}>
                                    {renderSimpleMarkdown(msg.content)}
                                </div>
                            </div>
                        );
                    })}
                </div>

                {/* Pagination */}
                {totalPages > 1 && (
                    <div style={{
                        display: "flex", alignItems: "center", justifyContent: "center", gap: "8px",
                        padding: "10px 20px", borderTop: "1px solid var(--theme-border, #e5e7eb)",
                        flexShrink: 0,
                    }}>
                        <button
                            disabled={page <= 1}
                            onClick={() => setPage(p => Math.max(1, p - 1))}
                            style={{ ...pageBtnStyle, opacity: page <= 1 ? 0.4 : 1 }}
                        >
                            ◀
                        </button>
                        <span style={{ fontSize: "12px", color: "var(--theme-text-secondary, #6b7280)" }}>
                            {page} / {totalPages}
                        </span>
                        <button
                            disabled={page >= totalPages}
                            onClick={() => setPage(p => Math.min(totalPages, p + 1))}
                            style={{ ...pageBtnStyle, opacity: page >= totalPages ? 0.4 : 1 }}
                        >
                            ▶
                        </button>
                    </div>
                )}
            </div>
        </div>
    );
}

// ---------------------------------------------------------------------------
// Shared styles
// ---------------------------------------------------------------------------

const toolbarBtnStyle: React.CSSProperties = {
    padding: "5px 12px",
    borderRadius: "6px",
    border: "1px solid var(--theme-border, #d1d5db)",
    background: "transparent",
    color: "var(--theme-text-secondary, #6b7280)",
    fontSize: "12px",
    cursor: "pointer",
    whiteSpace: "nowrap",
    flexShrink: 0,
};

const pageBtnStyle: React.CSSProperties = {
    padding: "4px 10px",
    borderRadius: "6px",
    border: "1px solid var(--theme-border, #d1d5db)",
    background: "transparent",
    color: "var(--theme-text-secondary, #6b7280)",
    fontSize: "13px",
    cursor: "pointer",
};

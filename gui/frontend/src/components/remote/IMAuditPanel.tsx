import { useState, useEffect, useCallback, useRef } from "react";
import {
    DeleteIMAuditMessagesBefore,
    ExportIMAuditCSV,
    GetIMAuditUsers,
    OpenFileOrShowInFolder,
    QueryIMAuditMessages,
} from "../../../wailsjs/go/main/App";

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
    platform: string;
    onClose: () => void;
    lang: string;
}

const platformLabels: Record<string, { zh: string; en: string }> = {
    qq: { zh: "QQ", en: "QQ" },
    telegram: { zh: "Telegram", en: "Telegram" },
    weixin: { zh: "\u5fae\u4fe1", en: "WeChat" },
    lansenger: { zh: "\u84dd\u4fe1", en: "Lansenger" },
    thirdparty: { zh: "\u7b2c\u4e09\u65b9\u63a5\u5165", en: "Third-party" },
};

function isZhLang(lang: string) {
    return lang === "zh-Hans" || lang === "zh-Hant";
}

function labelForPlatform(platform: string, lang: string) {
    const item = platformLabels[platform];
    if (!item) return platform;
    return isZhLang(lang) ? item.zh : item.en;
}

function formatTimestamp(ts: string): string {
    try {
        const d = new Date(ts);
        const pad = (n: number) => String(n).padStart(2, "0");
        return d.getFullYear() + "-" + pad(d.getMonth() + 1) + "-" + pad(d.getDate()) + " " + pad(d.getHours()) + ":" + pad(d.getMinutes()) + ":" + pad(d.getSeconds());
    } catch {
        return ts;
    }
}

function normalizeAuditResult(result: any): IMAuditQueryResult {
    return {
        messages: result?.messages || result?.Messages || [],
        total: result?.total ?? result?.Total ?? 0,
        page: result?.page ?? result?.Page ?? 1,
        page_size: result?.page_size ?? result?.PageSize ?? 50,
    };
}

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
            parts.push(<code key={idx++} style={inlineCodeStyle}>{m.slice(1, -1)}</code>);
        } else if (match[2]) {
            parts.push(<strong key={idx++}>{m.slice(2, -2)}</strong>);
        } else if (match[3]) {
            const lm = m.match(/^\[([^\]]+)\]\(([^)]+)\)$/);
            if (lm) {
                parts.push(<a key={idx++} href={lm[2]} target="_blank" rel="noopener noreferrer" style={{ color: "var(--theme-primary, #6366f1)" }}>{lm[1]}</a>);
            }
        }
        lastIndex = re.lastIndex;
    }
    if (lastIndex < text.length) parts.push(text.slice(lastIndex));
    return parts.length > 0 ? parts : [text];
}

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
    const [message, setMessage] = useState("");
    const [refreshKey, setRefreshKey] = useState(0);
    const [initDone, setInitDone] = useState(false);
    const listRef = useRef<HTMLDivElement>(null);

    const isZh = isZhLang(lang);
    const totalPages = Math.max(1, Math.ceil(total / pageSize));
    const title = labelForPlatform(platform, lang) + (isZh ? " \u6d88\u606f\u76d1\u770b" : " Message Watch");

    useEffect(() => {
        GetIMAuditUsers(platform).then(setUsers).catch(() => setUsers([]));
    }, [platform]);

    useEffect(() => {
        setInitDone(false);
        setPage(1);
        setKeyword("");
        setSearchInput("");
        setUserID("");
        setMessage("");
    }, [platform]);

    useEffect(() => {
        if (initDone) return;
        (async () => {
            try {
                const result = normalizeAuditResult(await QueryIMAuditMessages(platform, "", "", 1));
                const lastPage = Math.max(1, Math.ceil(result.total / (result.page_size || 50)));
                setTotal(result.total);
                setPageSize(result.page_size || 50);
                setPage(lastPage);
            } catch (err) {
                console.error("[im-audit] init error:", err);
            } finally {
                setInitDone(true);
            }
        })();
    }, [platform, initDone]);

    const loadMessages = useCallback(async () => {
        if (!initDone) return;
        setLoading(true);
        try {
            const result = normalizeAuditResult(await QueryIMAuditMessages(platform, userID, keyword, page));
            setMessages(result.messages || []);
            setTotal(result.total);
            setPageSize(result.page_size || 50);
        } catch (err) {
            console.error("[im-audit] query error:", err);
            setMessage(isZh ? "\u52a0\u8f7d\u6d88\u606f\u8bb0\u5f55\u5931\u8d25" : "Failed to load audit messages");
        } finally {
            setLoading(false);
        }
    }, [platform, userID, keyword, page, refreshKey, initDone, isZh]);

    useEffect(() => { loadMessages(); }, [loadMessages]);

    useEffect(() => {
        if (messages.length > 0 && listRef.current) {
            requestAnimationFrame(() => listRef.current?.scrollTo({ top: listRef.current.scrollHeight }));
        }
    }, [messages]);

    const handleSearch = () => {
        setKeyword(searchInput.trim());
        setPage(1);
    };

    const handleRefresh = () => {
        setPage(Math.max(1, Math.ceil(total / pageSize)));
        setRefreshKey(k => k + 1);
    };

    const handleCleanup = async () => {
        if (!confirm(isZh ? "\u786e\u5b9a\u5220\u9664 " + cleanupDays + " \u5929\u524d\u7684\u8bb0\u5f55\uff1f" : "Delete records older than " + cleanupDays + " days?")) return;
        try {
            const n = await DeleteIMAuditMessagesBefore(cleanupDays);
            setMessage(isZh ? "\u5df2\u5220\u9664 " + n + " \u6761\u8bb0\u5f55" : "Deleted " + n + " records");
            setPage(1);
            setKeyword("");
            setSearchInput("");
            setUserID("");
            setRefreshKey(k => k + 1);
            GetIMAuditUsers(platform).then(setUsers).catch(() => {});
        } catch (err) {
            setMessage(String(err));
        }
        setShowCleanup(false);
    };

    const handleExport = async () => {
        setExporting(true);
        try {
            const filePath = await ExportIMAuditCSV(platform, userID, keyword);
            if (filePath) {
                await OpenFileOrShowInFolder(filePath);
                setMessage(isZh ? "\u5df2\u5bfc\u51fa CSV" : "CSV exported");
            }
        } catch (err) {
            setMessage(String(err));
        } finally {
            setExporting(false);
        }
    };

    return (
        <div style={overlayStyle} onClick={onClose}>
            <div style={panelStyle} onClick={e => e.stopPropagation()}>
                <div style={headerStyle}>
                    <div style={{ display: "flex", alignItems: "center", gap: "8px", minWidth: 0 }}>
                        <span style={{ fontSize: "15px", fontWeight: 700, color: "var(--theme-text-primary, #1f2937)" }}>{title}</span>
                        <span style={{ fontSize: "12px", color: "var(--theme-text-muted, #9ca3af)", flexShrink: 0 }}>{isZh ? "\u5171 " + total + " \u6761" : total + " total"}</span>
                    </div>
                    <button onClick={onClose} aria-label={isZh ? "\u5173\u95ed" : "Close"} style={closeBtnStyle}>x</button>
                </div>

                <div style={toolbarStyle}>
                    <input
                        type="text"
                        value={searchInput}
                        onChange={e => setSearchInput(e.target.value)}
                        onKeyDown={e => { if (e.key === "Enter") handleSearch(); }}
                        placeholder={isZh ? "\u641c\u7d22\u6d88\u606f\u5185\u5bb9..." : "Search messages..."}
                        style={inputStyle}
                    />
                    <button onClick={handleSearch} style={toolbarBtnStyle}>{isZh ? "\u641c\u7d22" : "Search"}</button>
                    <button onClick={handleRefresh} disabled={loading} style={toolbarBtnStyle}>{loading ? "..." : (isZh ? "\u5237\u65b0" : "Refresh")}</button>

                    {users.length > 0 && (
                        <select value={userID} onChange={e => { setUserID(e.target.value); setPage(1); }} style={selectStyle}>
                            <option value="">{isZh ? "\u5168\u90e8\u7528\u6237" : "All users"}</option>
                            {users.map(u => <option key={u} value={u}>{u}</option>)}
                        </select>
                    )}

                    <button onClick={handleExport} disabled={exporting} style={toolbarBtnStyle}>{exporting ? "..." : (isZh ? "\u5bfc\u51fa CSV" : "Export CSV")}</button>

                    <div style={{ position: "relative" }}>
                        <button onClick={() => setShowCleanup(!showCleanup)} style={{ ...toolbarBtnStyle, color: "var(--theme-danger, #ef4444)", borderColor: "var(--theme-danger, #ef4444)" }}>
                            {isZh ? "\u6e05\u7406" : "Cleanup"}
                        </button>
                        {showCleanup && (
                            <div style={cleanupPopoverStyle}>
                                <div style={{ fontSize: "12px", color: "var(--theme-text-secondary, #6b7280)", marginBottom: "8px" }}>
                                    {isZh ? "\u5220\u9664\u591a\u5c11\u5929\u524d\u7684\u8bb0\u5f55\uff1f" : "Delete records older than:"}
                                </div>
                                <div style={{ display: "flex", gap: "6px", flexWrap: "wrap", marginBottom: "10px" }}>
                                    {[7, 14, 30, 60, 90].map(d => (
                                        <button key={d} onClick={() => setCleanupDays(d)} style={{
                                            padding: "3px 10px", borderRadius: "12px", fontSize: "12px",
                                            border: cleanupDays === d ? "1.5px solid var(--theme-primary, #6366f1)" : "1px solid var(--theme-border, #d1d5db)",
                                            background: cleanupDays === d ? "var(--theme-info-bg, #eef2ff)" : "transparent",
                                            color: cleanupDays === d ? "var(--theme-primary, #6366f1)" : "var(--theme-text-secondary, #6b7280)",
                                            cursor: "pointer",
                                        }}>{d} {isZh ? "\u5929" : "days"}</button>
                                    ))}
                                </div>
                                <button onClick={handleCleanup} style={cleanupConfirmStyle}>
                                    {isZh ? "\u786e\u8ba4\u5220\u9664 " + cleanupDays + " \u5929\u524d\u7684\u8bb0\u5f55" : "Delete records older than " + cleanupDays + " days"}
                                </button>
                            </div>
                        )}
                    </div>
                </div>

                {message && <div style={messageStyle}>{message}</div>}

                <div ref={listRef} style={listStyle}>
                    {loading && <div style={emptyStyle}>{isZh ? "\u52a0\u8f7d\u4e2d..." : "Loading..."}</div>}
                    {!loading && messages.length === 0 && <div style={emptyStyle}>{isZh ? "\u6682\u65e0\u6d88\u606f\u8bb0\u5f55" : "No messages found"}</div>}
                    {!loading && messages.map(msg => {
                        const isUser = msg.role === "user";
                        return (
                            <div key={msg.id} style={{ display: "flex", flexDirection: "column", alignItems: isUser ? "flex-end" : "flex-start", maxWidth: "100%" }}>
                                <div style={{ ...metaStyle, flexDirection: isUser ? "row-reverse" : "row" }}>
                                    <span>{isUser ? (msg.user_id || (isZh ? "\u7528\u6237" : "User")) : "MaClaw"}</span>
                                    <span>{formatTimestamp(msg.timestamp)}</span>
                                </div>
                                <div style={{ ...bubbleStyle, ...(isUser ? userBubbleStyle : assistantBubbleStyle) }}>
                                    {renderSimpleMarkdown(msg.content)}
                                </div>
                            </div>
                        );
                    })}
                </div>

                {totalPages > 1 && (
                    <div style={paginationStyle}>
                        <button disabled={page <= 1} onClick={() => setPage(p => Math.max(1, p - 1))} style={{ ...pageBtnStyle, opacity: page <= 1 ? 0.4 : 1 }}>prev</button>
                        <span style={{ fontSize: "12px", color: "var(--theme-text-secondary, #6b7280)" }}>{page} / {totalPages}</span>
                        <button disabled={page >= totalPages} onClick={() => setPage(p => Math.min(totalPages, p + 1))} style={{ ...pageBtnStyle, opacity: page >= totalPages ? 0.4 : 1 }}>next</button>
                    </div>
                )}
            </div>
        </div>
    );
}

const overlayStyle: React.CSSProperties = {
    position: "fixed",
    inset: 0,
    zIndex: 20000,
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    background: "rgba(0,0,0,0.45)",
};

const panelStyle: React.CSSProperties = {
    width: "min(780px, 92vw)",
    maxHeight: "88vh",
    display: "flex",
    flexDirection: "column",
    background: "var(--theme-surface, #fff)",
    color: "var(--theme-text-primary, #1f2937)",
    border: "1px solid var(--theme-border, #e5e7eb)",
    borderRadius: "12px",
    boxShadow: "0 8px 32px rgba(0,0,0,0.18)",
    overflow: "hidden",
};

const headerStyle: React.CSSProperties = {
    display: "flex",
    alignItems: "center",
    justifyContent: "space-between",
    padding: "14px 20px",
    borderBottom: "1px solid var(--theme-border, #e5e7eb)",
    flexShrink: 0,
};

const closeBtnStyle: React.CSSProperties = {
    background: "transparent",
    border: "1px solid var(--theme-border, #e5e7eb)",
    borderRadius: "6px",
    color: "var(--theme-text-muted, #9ca3af)",
    cursor: "pointer",
    padding: "2px 8px",
};

const toolbarStyle: React.CSSProperties = {
    display: "flex",
    alignItems: "center",
    gap: "8px",
    padding: "10px 20px",
    borderBottom: "1px solid var(--theme-border, #e5e7eb)",
    flexWrap: "wrap",
    flexShrink: 0,
};

const inputStyle: React.CSSProperties = {
    flex: 1,
    minWidth: "140px",
    padding: "6px 10px",
    borderRadius: "6px",
    border: "1px solid var(--theme-border, #d1d5db)",
    fontSize: "13px",
    background: "var(--theme-surface-muted, #f9fafb)",
    color: "var(--theme-text-primary, #1f2937)",
    outline: "none",
};

const selectStyle: React.CSSProperties = {
    padding: "5px 8px",
    borderRadius: "6px",
    border: "1px solid var(--theme-border, #d1d5db)",
    fontSize: "12px",
    background: "var(--theme-surface-muted, #f9fafb)",
    color: "var(--theme-text-primary, #1f2937)",
};

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

const cleanupPopoverStyle: React.CSSProperties = {
    position: "absolute",
    top: "100%",
    right: 0,
    marginTop: "4px",
    background: "var(--theme-surface, #fff)",
    border: "1px solid var(--theme-border, #d1d5db)",
    borderRadius: "8px",
    padding: "10px 14px",
    boxShadow: "0 4px 12px rgba(0,0,0,0.12)",
    zIndex: 10,
    minWidth: "200px",
};

const cleanupConfirmStyle: React.CSSProperties = {
    width: "100%",
    padding: "6px",
    borderRadius: "6px",
    border: "1px solid var(--theme-danger, #ef4444)",
    fontSize: "12px",
    fontWeight: 600,
    background: "var(--theme-danger-bg, rgba(239,68,68,0.12))",
    color: "var(--theme-danger, #ef4444)",
    cursor: "pointer",
};

const messageStyle: React.CSSProperties = {
    margin: "8px 20px 0",
    padding: "7px 10px",
    borderRadius: "8px",
    background: "var(--theme-info-bg, rgba(99,102,241,0.10))",
    color: "var(--theme-text-secondary, #6b7280)",
    fontSize: "12px",
};

const listStyle: React.CSSProperties = {
    flex: 1,
    overflowY: "auto",
    padding: "16px 20px",
    display: "flex",
    flexDirection: "column",
    gap: "8px",
};

const emptyStyle: React.CSSProperties = {
    textAlign: "center",
    padding: "32px 0",
    color: "var(--theme-text-muted, #9ca3af)",
    fontSize: "13px",
};

const metaStyle: React.CSSProperties = {
    fontSize: "11px",
    color: "var(--theme-text-muted, #9ca3af)",
    marginBottom: "3px",
    display: "flex",
    gap: "6px",
    alignItems: "center",
};

const bubbleStyle: React.CSSProperties = {
    maxWidth: "75%",
    padding: "10px 14px",
    fontSize: "13px",
    lineHeight: 1.6,
    whiteSpace: "pre-wrap",
    wordBreak: "break-word",
    textAlign: "left",
};

const userBubbleStyle: React.CSSProperties = {
    borderRadius: "14px 14px 4px 14px",
    background: "var(--theme-primary, #6366f1)",
    color: "#fff",
};

const assistantBubbleStyle: React.CSSProperties = {
    borderRadius: "14px 14px 14px 4px",
    background: "var(--theme-surface-muted, #f3f4f6)",
    color: "var(--theme-text-primary, #1f2937)",
};

const inlineCodeStyle: React.CSSProperties = {
    background: "rgba(0,0,0,0.06)",
    padding: "1px 4px",
    borderRadius: "3px",
    fontSize: "0.9em",
};

const paginationStyle: React.CSSProperties = {
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    gap: "8px",
    padding: "10px 20px",
    borderTop: "1px solid var(--theme-border, #e5e7eb)",
    flexShrink: 0,
};

const pageBtnStyle: React.CSSProperties = {
    padding: "4px 10px",
    borderRadius: "6px",
    border: "1px solid var(--theme-border, #d1d5db)",
    background: "transparent",
    color: "var(--theme-text-secondary, #6b7280)",
    fontSize: "12px",
    cursor: "pointer",
};

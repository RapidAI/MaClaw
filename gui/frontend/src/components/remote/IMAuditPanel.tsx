import { useState, useEffect, useCallback, useRef } from "react";
import {
    DeleteIMAuditMessagesBefore,
    ExportIMAuditCSV,
    GetIMAuditUsers,
    OpenFileOrShowInFolder,
    QueryIMAuditMessages,
} from "../../../wailsjs/go/main/App";
import { useSafeBackdropDismiss } from "../../hooks/useSafeBackdropDismiss";
import { useDialog } from "../CustomDialog";

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
            parts.push(<code key={idx++} className="im-audit-inline-code">{m.slice(1, -1)}</code>);
        } else if (match[2]) {
            parts.push(<strong key={idx++}>{m.slice(2, -2)}</strong>);
        } else if (match[3]) {
            const lm = m.match(/^\[([^\]]+)\]\(([^)]+)\)$/);
            if (lm) {
                parts.push(<a key={idx++} href={lm[2]} target="_blank" rel="noopener noreferrer" className="im-audit-link">{lm[1]}</a>);
            }
        }
        lastIndex = re.lastIndex;
    }
    if (lastIndex < text.length) parts.push(text.slice(lastIndex));
    return parts.length > 0 ? parts : [text];
}

export function IMAuditPanel({ platform, onClose, lang }: IMAuditPanelProps) {
    const { showConfirm } = useDialog();
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
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(onClose);

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
        if (!await showConfirm(isZh ? "确定删除 " + cleanupDays + " 天前的记录？" : "Delete records older than " + cleanupDays + " days?", isZh ? '清理记录' : 'Clean up records', { confirmText: isZh ? '删除' : 'Delete', cancelText: isZh ? '取消' : 'Cancel', confirmVariant: 'danger' })) return;
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
        <div className="im-audit-overlay" {...backdropProps}>
            <div
                className="im-audit-panel"
                {...dialogProps}
            >
                <div className="im-audit-header">
                    <div className="im-audit-title-wrap">
                        <span className="im-audit-title">{title}</span>
                        <span className="im-audit-total">{isZh ? "\u5171 " + total + " \u6761" : total + " total"}</span>
                    </div>
                    <button onClick={onClose} aria-label={isZh ? "\u5173\u95ed" : "Close"} className="im-audit-close">x</button>
                </div>

                <div className="im-audit-toolbar">
                    <input
                        type="text"
                        value={searchInput}
                        onChange={e => setSearchInput(e.target.value)}
                        onKeyDown={e => { if (e.key === "Enter") handleSearch(); }}
                        placeholder={isZh ? "\u641c\u7d22\u6d88\u606f\u5185\u5bb9..." : "Search messages..."}
                        className="im-audit-input"
                    />
                    <button onClick={handleSearch} className="im-audit-toolbar-button">{isZh ? "\u641c\u7d22" : "Search"}</button>
                    <button onClick={handleRefresh} disabled={loading} className="im-audit-toolbar-button">{loading ? "..." : (isZh ? "\u5237\u65b0" : "Refresh")}</button>

                    {users.length > 0 && (
                        <select value={userID} onChange={e => { setUserID(e.target.value); setPage(1); }} className="im-audit-select">
                            <option value="">{isZh ? "\u5168\u90e8\u7528\u6237" : "All users"}</option>
                            {users.map(u => <option key={u} value={u}>{u}</option>)}
                        </select>
                    )}

                    <button onClick={handleExport} disabled={exporting} className="im-audit-toolbar-button">{exporting ? "..." : (isZh ? "\u5bfc\u51fa CSV" : "Export CSV")}</button>

                    <div className="im-audit-cleanup-wrap">
                        <button onClick={() => setShowCleanup(!showCleanup)} className="im-audit-toolbar-button im-audit-toolbar-button--danger">
                            {isZh ? "\u6e05\u7406" : "Cleanup"}
                        </button>
                        {showCleanup && (
                            <div className="im-audit-cleanup-popover">
                                <div className="im-audit-cleanup-copy">
                                    {isZh ? "\u5220\u9664\u591a\u5c11\u5929\u524d\u7684\u8bb0\u5f55\uff1f" : "Delete records older than:"}
                                </div>
                                <div className="im-audit-cleanup-days">
                                    {[7, 14, 30, 60, 90].map(d => (
                                        <button key={d} onClick={() => setCleanupDays(d)} className="im-audit-day-button" data-active={cleanupDays === d ? "true" : "false"}>{d} {isZh ? "\u5929" : "days"}</button>
                                    ))}
                                </div>
                                <button onClick={handleCleanup} className="im-audit-cleanup-confirm">
                                    {isZh ? "\u786e\u8ba4\u5220\u9664 " + cleanupDays + " \u5929\u524d\u7684\u8bb0\u5f55" : "Delete records older than " + cleanupDays + " days"}
                                </button>
                            </div>
                        )}
                    </div>
                </div>

                {message && <div className="im-audit-message">{message}</div>}

                <div ref={listRef} className="im-audit-list">
                    {loading && <div className="im-audit-empty">{isZh ? "\u52a0\u8f7d\u4e2d..." : "Loading..."}</div>}
                    {!loading && messages.length === 0 && <div className="im-audit-empty">{isZh ? "\u6682\u65e0\u6d88\u606f\u8bb0\u5f55" : "No messages found"}</div>}
                    {!loading && messages.map(msg => {
                        const isUser = msg.role === "user";
                        return (
                            <div key={msg.id} className="im-audit-item" data-role={isUser ? "user" : "assistant"}>
                                <div className="im-audit-meta">
                                    <span>{isUser ? (msg.user_id || (isZh ? "\u7528\u6237" : "User")) : "MaClaw"}</span>
                                    <span>{formatTimestamp(msg.timestamp)}</span>
                                </div>
                                <div className="im-audit-bubble">
                                    {renderSimpleMarkdown(msg.content)}
                                </div>
                            </div>
                        );
                    })}
                </div>

                {totalPages > 1 && (
                    <div className="im-audit-pagination">
                        <button disabled={page <= 1} onClick={() => setPage(p => Math.max(1, p - 1))} className="im-audit-page-button">prev</button>
                        <span className="im-audit-page-label">{page} / {totalPages}</span>
                        <button disabled={page >= totalPages} onClick={() => setPage(p => Math.min(totalPages, p + 1))} className="im-audit-page-button">next</button>
                    </div>
                )}
            </div>
        </div>
    );
}

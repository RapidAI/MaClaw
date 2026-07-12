import { useCallback, useEffect, useState, type CSSProperties, type KeyboardEvent, type ReactNode } from "react";
import { DeleteSession, GetSessionCount, GetSessionFullText, ListSessionHistory, SearchSessionHistory } from "../../../wailsjs/go/main/App";
import { colors, radius } from "./styles";
import { useSafeBackdropDismiss } from "../../hooks/useSafeBackdropDismiss";

type Translate = (en: string, zhHans: string, zhHant?: string) => string;

interface SessionSummaryItem {
    session_id: string;
    timestamp: string;
    platform: string;
    topic: string;
    text_len: number;
}

interface SessionSearchHit {
    session_id: string;
    timestamp: string;
    platform: string;
    topic: string;
    snippet: string;
    rank: number;
}

type WailsNoDragStyle = CSSProperties & {
    WebkitAppRegion?: "no-drag";
    "--wails-draggable"?: "no-drag";
};

type SessionHistoryTabProps = {
    t: Translate;
    lang: string;
    onOpenTrace?: (focus: string) => void;
};

type ViewSession = { id: string; topic: string; platform: string; timestamp: string };

export function SessionHistoryTab({ t, lang, onOpenTrace }: SessionHistoryTabProps) {
    const [sessions, setSessions] = useState<SessionSummaryItem[]>([]);
    const [searchResults, setSearchResults] = useState<SessionSearchHit[] | null>(null);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState("");
    const [query, setQuery] = useState("");
    const [totalCount, setTotalCount] = useState(0);
    const [viewSession, setViewSession] = useState<ViewSession | null>(null);
    const [fullText, setFullText] = useState("");
    const [fullTextLoading, setFullTextLoading] = useState(false);
    const [deleteTarget, setDeleteTarget] = useState<string | null>(null);

    const loadSessions = useCallback(async () => {
        setLoading(true);
        setError("");
        try {
            const [list, count] = await Promise.all([ListSessionHistory(100), GetSessionCount()]);
            setSessions(Array.isArray(list) ? list : []);
            setTotalCount(count ?? 0);
        } catch (e) {
            setError(String(e));
        }
        setLoading(false);
    }, []);

    useEffect(() => { loadSessions(); }, [loadSessions]);

    const handleSearch = useCallback(async () => {
        const q = query.trim();
        if (!q) {
            setSearchResults(null);
            return;
        }
        setLoading(true);
        setError("");
        try {
            const results = await SearchSessionHistory(q, 30);
            const hits = Array.isArray(results) ? results : [];
            setSearchResults(hits.filter((r: any) => r.session_id || r.snippet !== "no results found"));
        } catch (e) {
            setError(String(e));
        }
        setLoading(false);
    }, [query]);

    const handleKeyDown = (e: KeyboardEvent<HTMLInputElement>) => { if (e.key === "Enter") handleSearch(); };

    const openSessionTrace = useCallback((sessionId: string) => {
        const focus = String(sessionId || "").trim();
        if (focus) onOpenTrace?.("session:" + focus);
    }, [onOpenTrace]);

    const handleView = async (sessionId: string, topic: string, platform: string, timestamp: string) => {
        setViewSession({ id: sessionId, topic, platform, timestamp });
        setFullText("");
        setFullTextLoading(true);
        try {
            const text = await GetSessionFullText(sessionId);
            setFullText(text || "");
        } catch (e) {
            setFullText(`Error: ${e}`);
        }
        setFullTextLoading(false);
    };

    const handleDelete = async (sessionId: string) => {
        setError("");
        try {
            await DeleteSession(sessionId);
            setDeleteTarget(null);
            setSessions(prev => prev.filter(s => s.session_id !== sessionId));
            if (searchResults) setSearchResults(prev => prev ? prev.filter(s => s.session_id !== sessionId) : null);
            if (viewSession?.id === sessionId) setViewSession(null);
            setTotalCount(prev => Math.max(0, prev - 1));
        } catch (e) {
            setError(String(e));
        }
    };

    const renderSnippet = (snippet: string) => {
        const parts = snippet.split(/(<b>[^<]*<\/b>)/g);
        return parts.filter(Boolean).map((part, i) => {
            const m = part.match(/^<b>([^<]*)<\/b>$/);
            if (m) return <strong key={i} style={{ color: colors.primary }}>{m[1]}</strong>;
            return <span key={i}>{part}</span>;
        });
    };

    const displayList = searchResults !== null ? searchResults : sessions;

    return (
        <div>
            <div style={{ display: "flex", gap: 8, marginBottom: 10, alignItems: "center" }}>
                <input placeholder={t("Full-text search (Enter)", "\u5168\u6587\u68c0\u7d22\uff08\u56de\u8f66\u641c\u7d22\uff09")} value={query} onChange={e => setQuery(e.target.value)} onKeyDown={handleKeyDown} aria-label={t("Search sessions", "\u641c\u7d22\u4f1a\u8bdd")} style={{ ...inputStyle, flex: 1 }} />
                <button onClick={handleSearch} disabled={loading} style={{ ...primaryBtnStyle, cursor: loading ? "wait" : "pointer", opacity: loading ? 0.6 : 1 }}>{t("Search", "\u641c\u7d22")}</button>
                {searchResults !== null && <button onClick={() => { setQuery(""); setSearchResults(null); }} style={neutralBtnStyle}>{t("Clear", "\u6e05\u9664")}</button>}
            </div>
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 8 }}>
                <span style={{ fontSize: "0.72rem", color: colors.textSecondary }}>
                    {searchResults !== null ? `${searchResults.length} ${t("results", "\u6761\u7ed3\u679c")}` : `${totalCount} ${t("sessions total", "\u6761\u4f1a\u8bdd\u8bb0\u5f55")}`}
                </span>
            </div>
            {error && <div role="alert" style={{ color: colors.danger, fontSize: "0.76rem", marginBottom: 8 }}>{error}</div>}
            {loading && <div style={{ fontSize: "0.76rem", color: colors.textMuted }}>{t("Loading...", "\u52a0\u8f7d\u4e2d...")}</div>}
            <div style={{ display: "flex", flexDirection: "column", gap: 6, maxHeight: "calc(100vh - 340px)", overflowY: "auto", border: `1px solid ${colors.border}`, borderRadius: radius.md, padding: 6 }}>
                {displayList.length === 0 && !loading && (
                    <div style={{ fontSize: "0.78rem", color: colors.textMuted, textAlign: "center", padding: "20px 0" }}>
                        {searchResults !== null ? t("No matching sessions", "\u672a\u627e\u5230\u5339\u914d\u7684\u4f1a\u8bdd") : t("No session history yet", "\u6682\u65e0\u4f1a\u8bdd\u5386\u53f2")}
                    </div>
                )}
                {displayList.map(item => {
                    const sid = (item as any).session_id;
                    const ts = (item as any).timestamp;
                    const platform = (item as any).platform || "";
                    const topic = (item as any).topic || "";
                    const snippet = (item as SessionSearchHit).snippet;
                    const textLen = (item as SessionSummaryItem).text_len;
                    return (
                        <div key={sid} style={{ border: `1px solid ${colors.border}`, borderRadius: radius.md, padding: "8px 10px", background: colors.surface }}>
                            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", gap: 8 }}>
                                <div style={{ flex: 1, minWidth: 0, overflow: "hidden", cursor: "pointer" }} onClick={() => handleView(sid, topic, platform, ts)}>
                                    <div style={{ display: "flex", alignItems: "center", gap: 6, marginBottom: 3 }}>
                                        <span style={{ fontSize: "0.64rem", fontWeight: 700, color: colors.textMuted }}>{(platform || "doc").slice(0, 3).toUpperCase()}</span>
                                        <span style={platformBadgeStyle}>{platform || "unknown"}</span>
                                        {topic && <span style={{ fontSize: "0.76rem", color: colors.text, fontWeight: 500, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{topic}</span>}
                                    </div>
                                    {snippet ? <div style={snippetStyle}>{renderSnippet(snippet)}</div> : (!topic && <div style={{ fontSize: "0.72rem", color: colors.textMuted, fontStyle: "italic" }}>{t("(no topic)", "(\u65e0\u4e3b\u9898)")}</div>)}
                                    <div style={{ fontSize: "0.64rem", color: colors.textMuted, marginTop: 3, textAlign: "left" }}>
                                        {fmtDate(ts, lang)}
                                        {textLen > 0 && <> {" \u00b7 "}{textLen > 1000 ? `${(textLen / 1000).toFixed(1)}K` : textLen} {t("chars", "\u5b57\u7b26")}</>}
                                    </div>
                                </div>
                                <div style={{ display: "flex", gap: 4, flexShrink: 0, alignSelf: "flex-start" }}>
                                    <button onClick={() => handleView(sid, topic, platform, ts)} title={t("View", "\u67e5\u770b")} style={iconBtnStyle}>VIEW</button>
                                    <button onClick={() => setDeleteTarget(sid)} title={t("Delete", "\u5220\u9664")} style={{ ...iconBtnStyle, color: colors.danger }}>DEL</button>
                                </div>
                            </div>
                        </div>
                    );
                })}
            </div>
            {viewSession && <SessionViewerModal t={t} lang={lang} viewSession={viewSession} fullText={fullText} loading={fullTextLoading} onClose={() => setViewSession(null)} onOpenTrace={onOpenTrace ? openSessionTrace : undefined} />}
            {deleteTarget && (
                <ModalOverlay onClose={() => setDeleteTarget(null)}>
                    <p style={{ fontSize: "0.82rem", marginBottom: 16 }}>{t("Delete this session? This cannot be undone.", "\u786e\u5b9a\u5220\u9664\u8fd9\u6761\u4f1a\u8bdd\u8bb0\u5f55\uff1f\u6b64\u64cd\u4f5c\u4e0d\u53ef\u64a4\u9500\u3002")}</p>
                    <div style={{ display: "flex", justifyContent: "flex-end", gap: 8 }}>
                        <button onClick={() => setDeleteTarget(null)} style={neutralBtnStyle}>{t("Cancel", "\u53d6\u6d88")}</button>
                        <button onClick={() => handleDelete(deleteTarget)} style={dangerBtnStyle}>{t("Delete", "\u5220\u9664")}</button>
                    </div>
                </ModalOverlay>
            )}
        </div>
    );
}

function SessionViewerModal({ t, lang, viewSession, fullText, loading, onClose, onOpenTrace }: { t: Translate; lang: string; viewSession: ViewSession; fullText: string; loading: boolean; onClose: () => void; onOpenTrace?: (sessionId: string) => void }) {
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(onClose);

    return (
        <div style={overlayStyle} {...backdropProps}>
            <div
                role="dialog"
                aria-modal="true"
                style={viewerStyle}
                {...dialogProps}
            >
                <div style={{ padding: "14px 18px 10px", borderBottom: `1px solid ${colors.border}`, flexShrink: 0 }}>
                    <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                        <div style={{ display: "flex", alignItems: "center", gap: 8, minWidth: 0, flex: 1 }}>
                            <span style={{ fontSize: "0.64rem", fontWeight: 700, color: colors.textMuted }}>{(viewSession.platform || "doc").slice(0, 3).toUpperCase()}</span>
                            <span style={platformBadgeStyle}>{viewSession.platform || "unknown"}</span>
                            <span style={{ fontSize: "0.82rem", fontWeight: 600, color: colors.text, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{viewSession.topic || t("(no topic)", "(\u65e0\u4e3b\u9898)")}</span>
                        </div>
                        <div style={{ display: "flex", gap: 6, flexShrink: 0 }}>
                            {onOpenTrace && <button onClick={() => onOpenTrace(viewSession.id)} style={{ ...neutralBtnStyle, color: colors.primaryDark }}>{t("Experience", "\u7ecf\u9a8c", "\u7d93\u9a57")}</button>}
                            <button onClick={onClose} style={neutralBtnStyle}>X</button>
                        </div>
                    </div>
                    <div style={{ fontSize: "0.66rem", color: colors.textMuted, marginTop: 4 }}>{fmtDate(viewSession.timestamp, lang)} {"\u00b7"} ID: {viewSession.id}</div>
                </div>
                <div style={{ flex: 1, overflowY: "auto", padding: "14px 18px" }}>
                    {loading ? <div style={{ fontSize: "0.76rem", color: colors.textMuted, padding: "20px 0", textAlign: "center" }}>{t("Loading...", "\u52a0\u8f7d\u4e2d...")}</div> : <pre style={preStyle}>{fullText || t("(empty)", "(\u7a7a)")}</pre>}
                </div>
            </div>
        </div>
    );
}

function ModalOverlay({ onClose, children }: { onClose: () => void; children: ReactNode }) {
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(onClose);

    useEffect(() => {
        const onKey = (e: globalThis.KeyboardEvent) => { if (e.key === "Escape") onClose(); };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [onClose]);
    return (
        <div style={overlayStyle} {...backdropProps}>
            <div
                role="dialog"
                aria-modal="true"
                style={dialogStyle}
                {...dialogProps}
            >
                {children}
            </div>
        </div>
    );
}

function fmtDate(s: string, lang: string): string {
    if (!s) return "-";
    const locale = lang === "zh-Hans" ? "zh-CN" : lang === "zh-Hant" ? "zh-TW" : "en-US";
    try { return new Date(s).toLocaleString(locale); } catch { return s; }
}

const inputStyle: CSSProperties = { width: "100%", padding: "6px 10px", fontSize: "0.78rem", border: `1px solid ${colors.border}`, borderRadius: radius.sm, background: colors.surface, color: colors.text, boxSizing: "border-box" };
const neutralBtnStyle: CSSProperties = { padding: "6px 10px", fontSize: "0.72rem", border: `1px solid ${colors.border}`, borderRadius: radius.md, background: colors.surface, cursor: "pointer", color: colors.textSecondary, whiteSpace: "nowrap" };
const primaryBtnStyle: CSSProperties = { ...neutralBtnStyle, padding: "6px 14px", fontWeight: 600, border: `1px solid ${colors.primary}`, background: colors.primaryLight, color: colors.primaryDark };
const dangerBtnStyle: CSSProperties = { ...neutralBtnStyle, fontWeight: 600, border: `1px solid ${colors.danger}`, background: colors.dangerBg, color: colors.danger };
const iconBtnStyle: CSSProperties = { padding: "3px 8px", fontSize: "0.68rem", cursor: "pointer", background: "none", border: `1px solid ${colors.border}`, borderRadius: radius.sm, color: colors.textSecondary };
const platformBadgeStyle: CSSProperties = { fontSize: "0.62rem", fontWeight: 600, padding: "1px 6px", borderRadius: radius.sm, background: colors.bg, color: colors.textSecondary, border: `1px solid ${colors.border}` };
const snippetStyle: CSSProperties = { fontSize: "0.74rem", color: colors.textSecondary, maxHeight: 48, overflowY: "hidden", textAlign: "left", lineHeight: 1.4 };
const overlayStyle: WailsNoDragStyle = { position: "fixed", inset: 0, background: "rgba(0,0,0,0.4)", display: "flex", alignItems: "center", justifyContent: "center", zIndex: 9999, WebkitAppRegion: "no-drag", "--wails-draggable": "no-drag" };
const viewerStyle: WailsNoDragStyle = { background: colors.surface, borderRadius: radius.lg, boxShadow: "0 12px 40px rgba(0,0,0,0.2)", width: "90vw", maxWidth: 700, maxHeight: "80vh", display: "flex", flexDirection: "column", overflow: "hidden", WebkitAppRegion: "no-drag", "--wails-draggable": "no-drag" };
const dialogStyle: WailsNoDragStyle = { background: colors.surface, borderRadius: radius.lg, padding: "20px 24px", minWidth: 280, boxShadow: "0 8px 30px rgba(0,0,0,0.12)", WebkitAppRegion: "no-drag", "--wails-draggable": "no-drag" };
const preStyle: CSSProperties = { fontSize: "0.74rem", color: colors.text, whiteSpace: "pre-wrap", wordBreak: "break-word", margin: 0, fontFamily: "inherit", lineHeight: 1.6, textAlign: "left" };

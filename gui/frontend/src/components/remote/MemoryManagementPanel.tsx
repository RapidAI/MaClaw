import { useState, useEffect, useCallback, useRef } from "react";
import {
    ListMemories,
    SaveMemory,
    UpdateMemory,
    DeleteMemory,
    CompressMemories,
    ListMemoryBackups,
    RestoreMemoryBackup,
    DeleteMemoryBackup,
    SetAutoCompress,
    GetAutoCompressStatus,
    IsMemoryCompressing,
    GetMemoryMaxBackups,
    SetMemoryMaxBackups,
    ListSessionHistory,
    SearchSessionHistory,
    GetSessionFullText,
    DeleteSession,
    GetSessionCount,
} from "../../../wailsjs/go/main/App";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";
import { colors, radius } from "./styles";

interface MemoryEntry {
    id: string;
    content: string;
    category: string;
    tags: string[];
    created_at: string;
    updated_at: string;
    access_count: number;
}

interface BackupInfo {
    name: string;
    created_at: string;
    size_bytes: number;
    entry_count: number;
}

interface CompressResult {
    dedup_count: number;
    merged_count: number;
    compressed_count: number;
    skipped_count: number;
    error_count: number;
    saved_chars: number;
}

interface AutoCompressStatus {
    running: boolean;
    last_run?: string;
    last_error?: string;
}

const CATEGORIES = [
    { value: "", label: { zh: "全部", en: "All" } },
    { value: "self_identity", label: { zh: "自我认知", en: "Self Identity" } },
    { value: "user_fact", label: { zh: "用户事实", en: "User Fact" } },
    { value: "preference", label: { zh: "偏好设置", en: "Preference" } },
    { value: "project_knowledge", label: { zh: "项目知识", en: "Project" } },
    { value: "instruction", label: { zh: "指令", en: "Instruction" } },
    { value: "conversation_summary", label: { zh: "对话摘要", en: "Summary" } },
    { value: "session_checkpoint", label: { zh: "会话检查点", en: "Checkpoint" } },
] as const;

const CATEGORY_COLORS: Record<string, string> = {
    self_identity: "var(--theme-danger)",
    user_fact: "var(--theme-primary)",
    preference: "var(--theme-info, #0891b2)",
    project_knowledge: "var(--theme-success)",
    instruction: "var(--theme-warning)",
    conversation_summary: "var(--theme-primary-strong, #8b5cf6)",
    session_checkpoint: "var(--theme-text-muted)",
};

type Props = { lang: string };

const inputStyle: React.CSSProperties = {
    width: "100%", padding: "7px 10px", fontSize: "0.8rem",
    border: `1px solid ${colors.border}`, borderRadius: 4,
    background: colors.surface, color: colors.text, boxSizing: "border-box",
};
const labelStyle: React.CSSProperties = {
    fontSize: "0.76rem", color: colors.textSecondary, marginBottom: 4, display: "block",
};
const tabBtnStyle = (active: boolean): React.CSSProperties => ({
    padding: "5px 16px", fontSize: "0.76rem", fontWeight: 600, cursor: "pointer",
    border: "none", borderBottom: active ? `2px solid ${colors.primary}` : "2px solid transparent",
    background: "none", color: active ? colors.primary : colors.textSecondary,
});

/** Shared date formatter. */
function fmtDate(s: string, lang: string): string {
    if (!s) return "-";
    const locale = lang === 'zh-Hans' ? 'zh-CN' : lang === 'zh-Hant' ? 'zh-TW' : 'en-US';
    try { return new Date(s).toLocaleString(locale); }
    catch { return s; }
}

/** Category value → display label. */
function catLabel(cat: string, lang: string): string {
    const found = CATEGORIES.find(c => c.value === cat);
    if (!found) return cat;
    if (lang === 'zh-Hans') return found.label.zh;
    if (lang === 'zh-Hant') return found.label.zh; // TODO: add zhHant to CATEGORIES
    return found.label.en;
}

/** Human-readable file size. */
function fmtSize(b: number): string {
    if (b < 1024) return `${b} B`;
    if (b < 1048576) return `${(b / 1024).toFixed(1)} KB`;
    return `${(b / 1048576).toFixed(1)} MB`;
}

/** Reusable modal overlay — handles backdrop click + Escape key. */
function ModalOverlay({ onClose, children }: { onClose: () => void; children: React.ReactNode }) {
    useEffect(() => {
        const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") onClose(); };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [onClose]);
    return (
        <div style={overlayStyle} onClick={onClose}>
            <div role="dialog" aria-modal="true" onClick={e => e.stopPropagation()} style={dialogBaseStyle}>
                {children}
            </div>
        </div>
    );
}

const overlayStyle: React.CSSProperties = {
    position: "fixed", inset: 0, background: "rgba(0,0,0,0.3)",
    display: "flex", alignItems: "center", justifyContent: "center", zIndex: 9999,
};
const dialogBaseStyle: React.CSSProperties = {
    background: colors.surface, borderRadius: radius.lg,
    padding: "20px 24px", minWidth: 280, boxShadow: "0 8px 30px rgba(0,0,0,0.12)",
};
const cancelBtnStyle: React.CSSProperties = {
    padding: "5px 14px", fontSize: "0.76rem",
    border: `1px solid ${colors.border}`, borderRadius: radius.md,
    background: colors.surface, color: colors.text, cursor: "pointer",
};
const dangerBtnStyle: React.CSSProperties = {
    padding: "5px 14px", fontSize: "0.76rem", border: "none",
    borderRadius: radius.md, background: colors.danger, color: colors.onPrimary, cursor: "pointer",
};

export function MemoryManagementPanel({ lang }: Props) {
    const [tab, setTab] = useState<"edit" | "timemachine" | "history">("edit");
    const t = useCallback((en: string, zhHans: string, zhHant: string = zhHans) =>
        lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en, [lang]);
    // Revision counter — bumped by TimeMachine after restore/compress so
    // MemoryEditTab can re-fetch without unmount/remount.
    const [revision, setRevision] = useState(0);
    const bumpRevision = useCallback(() => setRevision(r => r + 1), []);
    const [entryCount, setEntryCount] = useState(0);
    const createRef = useRef<(() => void) | null>(null);

    return (
        <div style={{ padding: 0 }}>
            <div style={{ display: "flex", alignItems: "center", borderBottom: `1px solid ${colors.border}`, marginBottom: 10 }} role="tablist">
                <button role="tab" aria-selected={tab === "edit"} style={tabBtnStyle(tab === "edit")} onClick={() => setTab("edit")}>
                    📝 {t("Memory Edit", "记忆编辑")}
                </button>
                <button role="tab" aria-selected={tab === "history"} style={tabBtnStyle(tab === "history")} onClick={() => setTab("history")}>
                    💬 {t("Session History", "会话历史")}
                </button>
                <button role="tab" aria-selected={tab === "timemachine"} style={tabBtnStyle(tab === "timemachine")} onClick={() => setTab("timemachine")}>
                    ⏳ {t("Time Machine", "时光机")}
                </button>
                {tab === "edit" && (
                    <div style={{ marginLeft: "auto", display: "flex", alignItems: "center", gap: 8 }}>
                        <span style={{ fontSize: "0.72rem", color: colors.textSecondary }}>{entryCount} {t("entries", "条记忆")}</span>
                        <button onClick={() => createRef.current?.()} style={{ padding: "3px 12px", fontSize: "0.72rem", fontWeight: 600, background: colors.primary, color: colors.onPrimary, border: "none", borderRadius: radius.md, cursor: "pointer" }}>
                            + {t("New", "新建")}
                        </button>
                    </div>
                )}
            </div>
            {tab === "edit"
                ? <MemoryEditTab t={t} lang={lang} revision={revision} onCountChange={setEntryCount} createRef={createRef} />
                : tab === "history"
                ? <SessionHistoryTab t={t} lang={lang} />
                : <TimeMachineTab t={t} lang={lang} onDataChanged={bumpRevision} />}
        </div>
    );
}

// ---------------------------------------------------------------------------
// Tab 1: Memory Edit
// ---------------------------------------------------------------------------
type EditTabProps = {
    t: (en: string, zhHans: string, zhHant?: string) => string;
    lang: string;
    /** Incremented externally (e.g. after backup restore) to trigger re-fetch. */
    revision: number;
    onCountChange: (count: number) => void;
    createRef: React.MutableRefObject<(() => void) | null>;
};

function MemoryEditTab({ t, lang, revision, onCountChange, createRef }: EditTabProps) {
    const [entries, setEntries] = useState<MemoryEntry[]>([]);
    const [loading, setLoading] = useState(false);
    const [filterCat, setFilterCat] = useState("");
    const [keyword, setKeyword] = useState("");
    const [debouncedKeyword, setDebouncedKeyword] = useState("");
    const [error, setError] = useState("");
    const [dlgOpen, setDlgOpen] = useState(false);
    const [editEntry, setEditEntry] = useState<MemoryEntry | null>(null);
    const [formContent, setFormContent] = useState("");
    const [formCategory, setFormCategory] = useState("user_fact");
    const [formTags, setFormTags] = useState("");
    const [saving, setSaving] = useState(false);
    const [deleteTarget, setDeleteTarget] = useState<string | null>(null);

    // Debounce keyword input — 300ms delay to avoid excessive API calls.
    const debounceRef = useRef<ReturnType<typeof setTimeout>>();
    useEffect(() => {
        debounceRef.current = setTimeout(() => setDebouncedKeyword(keyword), 300);
        return () => clearTimeout(debounceRef.current);
    }, [keyword]);

    const loadEntries = useCallback(async () => {
        setLoading(true); setError("");
        try {
            const list = await ListMemories(filterCat, debouncedKeyword);
            setEntries(Array.isArray(list) ? list : []);
        } catch (e) { setError(String(e)); }
        setLoading(false);
    }, [filterCat, debouncedKeyword]);

    // Re-fetch when filters change OR when external revision bumps.
    useEffect(() => { loadEntries(); }, [loadEntries, revision]);

    // Report entry count to parent for tab-bar display.
    useEffect(() => { onCountChange(entries.length); }, [entries.length, onCountChange]);

    const openCreate = useCallback(() => {
        setEditEntry(null); setFormContent(""); setFormCategory("user_fact"); setFormTags(""); setError(""); setDlgOpen(true);
    }, []);

    // Expose openCreate to parent via ref.
    useEffect(() => { createRef.current = openCreate; }, [createRef, openCreate]);
    const openEdit = (entry: MemoryEntry) => {
        setEditEntry(entry); setFormContent(entry.content); setFormCategory(entry.category);
        setFormTags((entry.tags || []).join(", ")); setError(""); setDlgOpen(true);
    };

    const handleSave = async () => {
        if (!formContent.trim()) return;
        setSaving(true); setError("");
        try {
            const tags = formTags.split(",").map(s => s.trim()).filter(Boolean);
            if (editEntry) { await UpdateMemory(editEntry.id, formContent.trim(), formCategory, tags); }
            else { await SaveMemory(formContent.trim(), formCategory, tags); }
            setDlgOpen(false); await loadEntries();
        } catch (e) { setError(String(e)); }
        setSaving(false);
    };

    const handleDelete = async (id: string) => {
        setError("");
        try { await DeleteMemory(id); setDeleteTarget(null); await loadEntries(); }
        catch (e) { setError(String(e)); }
    };

    return (
        <>
            {/* Filters */}
            <div style={{ display: "flex", gap: 8, marginBottom: 10, flexWrap: "wrap" }}>
                <select value={filterCat} onChange={e => setFilterCat(e.target.value)} aria-label={t("Filter by category", "分类筛选")} style={{ ...inputStyle, width: "auto", padding: "4px 8px", fontSize: "0.76rem" }}>
                    {CATEGORIES.map(c => (<option key={c.value} value={c.value}>{catLabel(c.value, lang)}</option>))}
                </select>
                <input placeholder={t("Search keyword…", "搜索关键词…")} value={keyword} onChange={e => setKeyword(e.target.value)} aria-label={t("Search keyword", "搜索关键词")} style={{ ...inputStyle, width: "180px", padding: "4px 8px", fontSize: "0.76rem" }} />
            </div>

            {error && <div role="alert" style={{ color: colors.danger, fontSize: "0.76rem", marginBottom: 8 }}>{error}</div>}
            {loading && <div style={{ fontSize: "0.76rem", color: colors.textMuted }}>{t("Loading…", "加载中…")}</div>}

            {/* Entry list */}
            <div style={{ display: "flex", flexDirection: "column", gap: 6, maxHeight: "calc(100vh - 310px)", overflowY: "auto", border: `1px solid ${colors.border}`, borderRadius: radius.md, padding: 6 }}>
                {entries.length === 0 && !loading && (
                    <div style={{ fontSize: "0.78rem", color: colors.textMuted, textAlign: "center", padding: "20px 0" }}>{t("No memory entries", "暂无记忆条目")}</div>
                )}
                {entries.map(entry => (
                    <div key={entry.id} style={{ border: `1px solid ${colors.border}`, borderRadius: radius.md, padding: "8px 10px", background: colors.surface }}>
                        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", gap: 8 }}>
                            <div style={{ flex: 1, minWidth: 0, overflow: "hidden" }}>
                                <div style={{ display: "flex", alignItems: "center", gap: 6, marginBottom: 4, flexWrap: "wrap", justifyContent: "flex-start" }}>
                                    <span style={{ fontSize: "0.66rem", fontWeight: 600, padding: "1px 6px", borderRadius: radius.sm, color: colors.onPrimary, background: CATEGORY_COLORS[entry.category] || colors.textMuted }}>{catLabel(entry.category, lang)}</span>
                                    {(entry.tags || []).map(tag => (
                                        <span key={tag} style={{ fontSize: "0.64rem", padding: "1px 5px", borderRadius: radius.sm, background: colors.bg, color: colors.textSecondary, border: `1px solid ${colors.border}` }}>{tag}</span>
                                    ))}
                                </div>
                                <div style={{ fontSize: "0.78rem", color: colors.text, whiteSpace: "pre-wrap", wordBreak: "break-word", maxHeight: 120, overflowY: "auto", paddingRight: 4, textAlign: "left" }}>{entry.content}</div>
                                <div style={{ fontSize: "0.66rem", color: colors.textMuted, marginTop: 4, textAlign: "left" }}>{t("Updated", "更新")}: {fmtDate(entry.updated_at, lang)} · {t("Access", "访问")}: {entry.access_count}</div>
                            </div>
                            <div style={{ display: "flex", gap: 4, flexShrink: 0, alignSelf: "flex-start" }}>
                                <button onClick={() => openEdit(entry)} aria-label={t("Edit", "编辑")} title={t("Edit", "编辑")} style={{ padding: "3px 8px", fontSize: "0.7rem", cursor: "pointer", background: "none", border: `1px solid ${colors.border}`, borderRadius: radius.sm, color: colors.textSecondary }}>✏️</button>
                                <button onClick={() => setDeleteTarget(entry.id)} aria-label={t("Delete", "删除")} title={t("Delete", "删除")} style={{ padding: "3px 8px", fontSize: "0.7rem", cursor: "pointer", background: "none", border: `1px solid ${colors.border}`, borderRadius: radius.sm, color: colors.danger }}>🗑️</button>
                            </div>
                        </div>
                    </div>
                ))}
            </div>

            {/* Delete confirmation */}
            {deleteTarget && (
                <ModalOverlay onClose={() => setDeleteTarget(null)}>
                    <p style={{ fontSize: "0.82rem", marginBottom: 16 }}>{t("Delete this memory? This cannot be undone.", "确定删除这条记忆？此操作不可撤销。")}</p>
                    <div style={{ display: "flex", justifyContent: "flex-end", gap: 8 }}>
                        <button onClick={() => setDeleteTarget(null)} style={cancelBtnStyle}>{t("Cancel", "取消")}</button>
                        <button onClick={() => handleDelete(deleteTarget)} style={dangerBtnStyle}>{t("Delete", "删除")}</button>
                    </div>
                </ModalOverlay>
            )}

            {/* Create / Edit dialog */}
            {dlgOpen && (
                <ModalOverlay onClose={() => setDlgOpen(false)}>
                    <div style={{ width: 420, maxWidth: "90vw" }}>
                        <h4 style={{ fontSize: "0.82rem", margin: "0 0 14px", color: colors.text }}>{editEntry ? t("Edit Memory", "编辑记忆") : t("New Memory", "新建记忆")}</h4>
                        <div style={{ marginBottom: 10 }}>
                            <label style={labelStyle}>{t("Category", "分类")}</label>
                            <select value={formCategory} onChange={e => setFormCategory(e.target.value)} style={{ ...inputStyle, width: "auto" }}>
                                {CATEGORIES.filter(c => c.value).map(c => (<option key={c.value} value={c.value}>{catLabel(c.value, lang)}</option>))}
                            </select>
                        </div>
                        <div style={{ marginBottom: 10 }}>
                            <label style={labelStyle}>{t("Content", "内容")}</label>
                            <textarea value={formContent} onChange={e => setFormContent(e.target.value)} rows={4} style={{ ...inputStyle, resize: "vertical", fontFamily: "inherit" }} />
                        </div>
                        <div style={{ marginBottom: 14 }}>
                            <label style={labelStyle}>{t("Tags (comma separated)", "标签（逗号分隔）")}</label>
                            <input value={formTags} onChange={e => setFormTags(e.target.value)} placeholder="tag1, tag2" style={inputStyle} />
                        </div>
                        <div style={{ display: "flex", justifyContent: "flex-end", gap: 8 }}>
                            <button onClick={() => setDlgOpen(false)} style={cancelBtnStyle}>{t("Cancel", "取消")}</button>
                            <button onClick={handleSave} disabled={saving || !formContent.trim()} style={{ padding: "5px 14px", fontSize: "0.76rem", border: "none", borderRadius: radius.md, background: colors.primary, color: colors.onPrimary, cursor: "pointer", opacity: saving || !formContent.trim() ? 0.5 : 1 }}>{saving ? t("Saving…", "保存中…") : t("Save", "保存")}</button>
                        </div>
                    </div>
                </ModalOverlay>
            )}
        </>
    );
}

// ---------------------------------------------------------------------------
// Tab 1.5: Session History (SQLite FTS5 full-text search)
// ---------------------------------------------------------------------------

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

const PLATFORM_ICONS: Record<string, string> = {
    gui: "🖥️", tui: "⌨️", im: "💬", desktop: "🖥️",
};

type SessionHistoryTabProps = {
    t: (en: string, zhHans: string, zhHant?: string) => string;
    lang: string;
};

function SessionHistoryTab({ t, lang }: SessionHistoryTabProps) {
    const [sessions, setSessions] = useState<SessionSummaryItem[]>([]);
    const [searchResults, setSearchResults] = useState<SessionSearchHit[] | null>(null);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState("");
    const [query, setQuery] = useState("");
    const [totalCount, setTotalCount] = useState(0);
    const [viewSession, setViewSession] = useState<{ id: string; topic: string; platform: string; timestamp: string } | null>(null);
    const [fullText, setFullText] = useState<string>("");
    const [fullTextLoading, setFullTextLoading] = useState(false);
    const [deleteTarget, setDeleteTarget] = useState<string | null>(null);

    const loadSessions = useCallback(async () => {
        setLoading(true); setError("");
        try {
            const [list, count] = await Promise.all([ListSessionHistory(100), GetSessionCount()]);
            setSessions(Array.isArray(list) ? list : []);
            setTotalCount(count ?? 0);
        } catch (e) { setError(String(e)); }
        setLoading(false);
    }, []);

    useEffect(() => { loadSessions(); }, [loadSessions]);

    const handleSearch = useCallback(async () => {
        const q = query.trim();
        if (!q) { setSearchResults(null); return; }
        setLoading(true); setError("");
        try {
            const results = await SearchSessionHistory(q, 30);
            const hits = Array.isArray(results) ? results : [];
            setSearchResults(hits.filter((r: any) => r.session_id || r.snippet !== "no results found"));
        } catch (e) { setError(String(e)); }
        setLoading(false);
    }, [query]);

    const handleKeyDown = (e: React.KeyboardEvent) => { if (e.key === "Enter") handleSearch(); };

    const handleView = async (sessionId: string, topic: string, platform: string, timestamp: string) => {
        setViewSession({ id: sessionId, topic, platform, timestamp });
        setFullText(""); setFullTextLoading(true);
        try { const text = await GetSessionFullText(sessionId); setFullText(text || ""); }
        catch (e) { setFullText(`Error: ${e}`); }
        setFullTextLoading(false);
    };

    const handleDelete = async (sessionId: string) => {
        setError("");
        try {
            await DeleteSession(sessionId); setDeleteTarget(null);
            setSessions(prev => prev.filter(s => s.session_id !== sessionId));
            if (searchResults) setSearchResults(prev => prev ? prev.filter(s => s.session_id !== sessionId) : null);
            if (viewSession?.id === sessionId) setViewSession(null);
            setTotalCount(prev => Math.max(0, prev - 1));
        } catch (e) { setError(String(e)); }
    };

    /** Render FTS5 snippet with <b> tags as bold spans. */
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
            {/* Search bar */}
            <div style={{ display: "flex", gap: 8, marginBottom: 10, alignItems: "center" }}>
                <input placeholder={t("Full-text search (Enter)…", "全文检索（回车搜索）…")} value={query} onChange={e => setQuery(e.target.value)} onKeyDown={handleKeyDown} aria-label={t("Search sessions", "搜索会话")} style={{ ...inputStyle, flex: 1, padding: "6px 10px", fontSize: "0.78rem" }} />
                <button onClick={handleSearch} disabled={loading} style={{ padding: "6px 14px", fontSize: "0.74rem", fontWeight: 600, border: "none", borderRadius: radius.md, background: colors.primary, color: colors.onPrimary, cursor: loading ? "wait" : "pointer", opacity: loading ? 0.6 : 1, whiteSpace: "nowrap" }}>🔍 {t("Search", "搜索")}</button>
                {searchResults !== null && (
                    <button onClick={() => { setQuery(""); setSearchResults(null); }} style={{ padding: "6px 10px", fontSize: "0.72rem", border: `1px solid ${colors.border}`, borderRadius: radius.md, background: colors.surface, cursor: "pointer", color: colors.textSecondary, whiteSpace: "nowrap" }}>✕ {t("Clear", "清除")}</button>
                )}
            </div>
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 8 }}>
                <span style={{ fontSize: "0.72rem", color: colors.textSecondary }}>
                    {searchResults !== null ? `${searchResults.length} ${t("results", "条结果")}` : `${totalCount} ${t("sessions total", "条会话记录")}`}
                </span>
            </div>
            {error && <div role="alert" style={{ color: colors.danger, fontSize: "0.76rem", marginBottom: 8 }}>{error}</div>}

            {loading && <div style={{ fontSize: "0.76rem", color: colors.textMuted }}>{t("Loading…", "加载中…")}</div>}
            {/* Session list */}
            <div style={{ display: "flex", flexDirection: "column", gap: 6, maxHeight: "calc(100vh - 340px)", overflowY: "auto", border: `1px solid ${colors.border}`, borderRadius: radius.md, padding: 6 }}>
                {displayList.length === 0 && !loading && (
                    <div style={{ fontSize: "0.78rem", color: colors.textMuted, textAlign: "center", padding: "20px 0" }}>
                        {searchResults !== null ? t("No matching sessions", "未找到匹配的会话") : t("No session history yet", "暂无会话历史")}
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
                                        <span style={{ fontSize: "0.72rem" }}>{PLATFORM_ICONS[platform] || "📄"}</span>
                                        <span style={{ fontSize: "0.62rem", fontWeight: 600, padding: "1px 6px", borderRadius: radius.sm, background: colors.bg, color: colors.textSecondary, border: `1px solid ${colors.border}` }}>{platform || "unknown"}</span>
                                        {topic && <span style={{ fontSize: "0.76rem", color: colors.text, fontWeight: 500, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{topic}</span>}
                                    </div>
                                    {snippet ? (
                                        <div style={{ fontSize: "0.74rem", color: colors.textSecondary, maxHeight: 48, overflowY: "hidden", textAlign: "left", lineHeight: 1.4 }}>{renderSnippet(snippet)}</div>
                                    ) : (
                                        !topic && <div style={{ fontSize: "0.72rem", color: colors.textMuted, fontStyle: "italic" }}>{t("(no topic)", "(无主题)")}</div>
                                    )}
                                    <div style={{ fontSize: "0.64rem", color: colors.textMuted, marginTop: 3, textAlign: "left" }}>
                                        {fmtDate(ts, lang)}
                                        {textLen > 0 && <> · {textLen > 1000 ? `${(textLen / 1000).toFixed(1)}K` : textLen} {t("chars", "字符")}</>}
                                    </div>
                                </div>
                                <div style={{ display: "flex", gap: 4, flexShrink: 0, alignSelf: "flex-start" }}>
                                    <button onClick={() => handleView(sid, topic, platform, ts)} title={t("View", "查看")} style={{ padding: "3px 8px", fontSize: "0.68rem", cursor: "pointer", background: "none", border: `1px solid ${colors.border}`, borderRadius: radius.sm, color: colors.textSecondary }}>👁️</button>
                                    <button onClick={() => setDeleteTarget(sid)} title={t("Delete", "删除")} style={{ padding: "3px 8px", fontSize: "0.68rem", cursor: "pointer", background: "none", border: `1px solid ${colors.border}`, borderRadius: radius.sm, color: colors.danger }}>🗑️</button>
                                </div>
                            </div>
                        </div>
                    );
                })}
            </div>
            {/* Session viewer modal */}
            {viewSession && (
                <div style={{ position: "fixed", inset: 0, background: "rgba(0,0,0,0.4)", display: "flex", alignItems: "center", justifyContent: "center", zIndex: 9999 }} onClick={() => setViewSession(null)}>
                    <div role="dialog" aria-modal="true" onClick={e => e.stopPropagation()} style={{
                        background: colors.surface, borderRadius: radius.lg, boxShadow: "0 12px 40px rgba(0,0,0,0.2)",
                        width: "90vw", maxWidth: 700, maxHeight: "80vh", display: "flex", flexDirection: "column", overflow: "hidden",
                    }}>
                        {/* Header */}
                        <div style={{ padding: "14px 18px 10px", borderBottom: `1px solid ${colors.border}`, flexShrink: 0 }}>
                            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                                <div style={{ display: "flex", alignItems: "center", gap: 8, minWidth: 0, flex: 1 }}>
                                    <span style={{ fontSize: "0.82rem" }}>{PLATFORM_ICONS[viewSession.platform] || "📄"}</span>
                                    <span style={{ fontSize: "0.62rem", fontWeight: 600, padding: "1px 6px", borderRadius: radius.sm, background: colors.bg, color: colors.textSecondary, border: `1px solid ${colors.border}` }}>{viewSession.platform || "unknown"}</span>
                                    <span style={{ fontSize: "0.82rem", fontWeight: 600, color: colors.text, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{viewSession.topic || t("(no topic)", "(无主题)")}</span>
                                </div>
                                <button onClick={() => setViewSession(null)} style={{ padding: "4px 10px", fontSize: "0.76rem", border: `1px solid ${colors.border}`, borderRadius: radius.md, background: colors.surface, cursor: "pointer", color: colors.textSecondary, flexShrink: 0 }}>✕</button>
                            </div>
                            <div style={{ fontSize: "0.66rem", color: colors.textMuted, marginTop: 4 }}>
                                {fmtDate(viewSession.timestamp, lang)} · ID: {viewSession.id}
                            </div>
                        </div>
                        {/* Body */}
                        <div style={{ flex: 1, overflowY: "auto", padding: "14px 18px" }}>
                            {fullTextLoading
                                ? <div style={{ fontSize: "0.76rem", color: colors.textMuted, padding: "20px 0", textAlign: "center" }}>{t("Loading…", "加载中…")}</div>
                                : <pre style={{ fontSize: "0.74rem", color: colors.text, whiteSpace: "pre-wrap", wordBreak: "break-word", margin: 0, fontFamily: "inherit", lineHeight: 1.6, textAlign: "left" }}>{fullText || t("(empty)", "(空)")}</pre>
                            }
                        </div>
                    </div>
                </div>
            )}
            {deleteTarget && (
                <ModalOverlay onClose={() => setDeleteTarget(null)}>
                    <p style={{ fontSize: "0.82rem", marginBottom: 16 }}>{t("Delete this session? This cannot be undone.", "确定删除这条会话记录？此操作不可撤销。")}</p>
                    <div style={{ display: "flex", justifyContent: "flex-end", gap: 8 }}>
                        <button onClick={() => setDeleteTarget(null)} style={cancelBtnStyle}>{t("Cancel", "取消")}</button>
                        <button onClick={() => handleDelete(deleteTarget)} style={dangerBtnStyle}>{t("Delete", "删除")}</button>
                    </div>
                </ModalOverlay>
            )}
        </div>
    );
}

// ---------------------------------------------------------------------------
// Tab 2: Time Machine (compression + backup restore)
// ---------------------------------------------------------------------------
type TimeMachineProps = {
    t: (en: string, zhHans: string, zhHant?: string) => string;
    lang: string;
    /** Called after restore or compress so the edit tab can refresh. */
    onDataChanged: () => void;
};

function TimeMachineTab({ t, lang, onDataChanged }: TimeMachineProps) {
    const [compressing, setCompressing] = useState(false);
    const [compressResult, setCompressResult] = useState<CompressResult | null>(null);
    const [backups, setBackups] = useState<BackupInfo[]>([]);
    const [backupsLoading, setBackupsLoading] = useState(false);
    const [error, setError] = useState("");
    const [restoreTarget, setRestoreTarget] = useState<string | null>(null);
    const [deleteTarget, setDeleteTarget] = useState<string | null>(null);
    const [autoEnabled, setAutoEnabled] = useState(false);
    const [serviceStatus, setServiceStatus] = useState<AutoCompressStatus | null>(null);
    const [toggling, setToggling] = useState(false);
    const [maxBackups, setMaxBackupsLocal] = useState(20);
    const [savingMax, setSavingMax] = useState(false);

    const loadBackups = useCallback(async () => {
        setBackupsLoading(true);
        try {
            const list = await ListMemoryBackups();
            setBackups(Array.isArray(list) ? list : []);
        } catch (e) { setError(String(e)); }
        setBackupsLoading(false);
    }, []);

    const loadStatus = useCallback(async () => {
        try {
            const s = await GetAutoCompressStatus();
            setServiceStatus(s as AutoCompressStatus);
            setAutoEnabled(!!(s as AutoCompressStatus)?.running);
        } catch { /* ignore */ }
    }, []);

    const loadMaxBackups = useCallback(async () => {
        try {
            const n = await GetMemoryMaxBackups();
            if (n > 0) setMaxBackupsLocal(n);
        } catch { /* ignore */ }
    }, []);

    useEffect(() => { loadBackups(); loadStatus(); loadMaxBackups(); }, [loadBackups, loadStatus, loadMaxBackups]);

    // On mount, check if a compression is already in progress (e.g. after tab switch).
    useEffect(() => {
        IsMemoryCompressing().then(running => {
            if (running) setCompressing(true);
        }).catch(() => { /* ignore */ });
    }, []);

    // Listen for the backend "memory:compressed" event — the single source of
    // truth for "compression finished". Both manual (CompressMemories) and auto
    // (runOnce) paths emit this event. All post-completion UI updates happen here.
    useEffect(() => {
        const handler = async (result: any) => {
            // A compression round finished, but another may still be in flight
            // (manual + auto overlap). Query the backend for the true state.
            try {
                const stillRunning = await IsMemoryCompressing();
                setCompressing(stillRunning);
            } catch {
                setCompressing(false);
            }
            if (result && typeof result === "object") {
                setCompressResult(result as CompressResult);
            }
            loadBackups();
            loadStatus();
            onDataChanged();
        };
        EventsOn("memory:compressed", handler);
        return () => { EventsOff("memory:compressed"); };
    }, [loadBackups, loadStatus, onDataChanged]);

    // Clean up delayed refresh timer on unmount.
    const autoRefreshTimer = useRef<ReturnType<typeof setTimeout>>();
    useEffect(() => () => clearTimeout(autoRefreshTimer.current), []);

    const handleToggleAuto = async () => {
        setToggling(true); setError("");
        try {
            const next = !autoEnabled;
            await SetAutoCompress(next);
            setAutoEnabled(next);
            if (next) {
                clearTimeout(autoRefreshTimer.current);
                autoRefreshTimer.current = setTimeout(async () => { await loadBackups(); await loadStatus(); onDataChanged(); }, 2000);
            }
        } catch (e) {
            setError(String(e));
        } finally {
            setToggling(false);
        }
    };

    const handleCompress = async () => {
        setCompressing(true); setError(""); setCompressResult(null);
        try {
            // Post-completion UI update (setCompressing(false), setCompressResult,
            // loadBackups, etc.) is handled by the "memory:compressed" event handler.
            // Go-side has a 5-minute context timeout; no client-side timeout needed.
            await CompressMemories();
        } catch (e) {
            setError(String(e));
            setCompressing(false);
        }
    };

    const handleRestore = async (name: string) => {
        setError("");
        try {
            await RestoreMemoryBackup(name);
            setRestoreTarget(null);
            loadBackups();
            onDataChanged();
        } catch (e) { setError(String(e)); }
    };

    const handleDeleteBackup = async (name: string) => {
        setError("");
        try { await DeleteMemoryBackup(name); setDeleteTarget(null); loadBackups(); }
        catch (e) { setError(String(e)); }
    };

    return (
        <>
            {/* Auto-compress + One-shot compress in one row */}
            <div style={{ border: `1px solid ${colors.border}`, borderRadius: radius.md, padding: "10px 14px", background: colors.surface, marginBottom: 12 }}>
                <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 8 }}>
                    <div style={{ display: "flex", alignItems: "center", gap: 6, fontSize: "0.76rem", color: colors.text, fontWeight: 600, minWidth: 0 }}>
                        🔄 {t("Auto-Compress", "自动压缩")}
                        <span style={{ fontSize: "0.68rem", color: colors.textMuted, fontWeight: 400 }}>
                            {t("Every 6h dedup+LLM", "每6h去重+LLM压缩")}
                        </span>
                        {serviceStatus?.last_run && (
                            <span style={{ fontSize: "0.66rem", color: colors.textMuted, fontWeight: 400 }}>
                                · {fmtDate(serviceStatus.last_run, lang)}
                                {serviceStatus.last_error && <span style={{ color: colors.danger }}> · {serviceStatus.last_error}</span>}
                            </span>
                        )}
                    </div>
                    <div style={{ display: "flex", alignItems: "center", gap: 6, flexShrink: 0 }}>
                        <button onClick={handleToggleAuto} disabled={toggling} style={{
                            padding: "4px 14px", fontSize: "0.74rem", fontWeight: 600, border: "none", borderRadius: radius.md, cursor: toggling ? "wait" : "pointer",
                            background: autoEnabled ? colors.success : colors.textMuted, color: colors.onPrimary, whiteSpace: "nowrap",
                        }}>
                            {autoEnabled ? t("ON", "已开启") : t("OFF", "已关闭")}
                        </button>
                        <button onClick={handleCompress} disabled={compressing} aria-label={t("Compress Now", "立即压缩")} style={{
                            padding: "4px 14px", fontSize: "0.74rem", fontWeight: 600, border: "none", borderRadius: radius.md, cursor: compressing ? "wait" : "pointer",
                            background: compressing ? colors.textMuted : colors.primary, color: colors.onPrimary, opacity: compressing ? 0.6 : 1, whiteSpace: "nowrap",
                        }}>
                            {compressing ? t("…", "压缩中…") : t("Compress", "立即压缩")}
                        </button>
                    </div>
                </div>
                {compressResult && (
                    <div role="status" style={{ fontSize: "0.72rem", color: colors.success, background: colors.successBg, borderRadius: radius.sm, padding: "5px 10px", marginTop: 6 }}>
                        {compressResult.dedup_count > 0 && <>{t("Dedup", "去重")}: {compressResult.dedup_count} {t("removed", "条移除")} · </>}
                        {compressResult.merged_count > 0 && <>{t("Merged", "合并")}: {compressResult.merged_count} {t("merged", "条合并")} · </>}
                        {t("Compress", "压缩")}: {compressResult.compressed_count} {t("compressed", "条已压缩")}, {compressResult.skipped_count} {t("skipped", "条跳过")}, {compressResult.error_count} {t("errors", "条失败")}, {t("saved", "节省")} {compressResult.saved_chars} {t("chars", "字符")}
                    </div>
                )}
            </div>

            {error && <div role="alert" style={{ color: colors.danger, fontSize: "0.76rem", marginBottom: 8 }}>{error}</div>}

            {/* Backup list */}
            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: 8 }}>
                <div style={{ fontSize: "0.8rem", color: colors.text, fontWeight: 600 }}>📦 {t("Backup History", "历史备份")}</div>
                <div style={{ display: "flex", alignItems: "center", gap: 4, fontSize: "0.7rem", color: colors.textMuted }}>
                    {t("Keep", "保留")}
                    <input
                        type="number" min={8} max={100} value={maxBackups}
                        onChange={e => { const v = parseInt(e.target.value, 10); if (!isNaN(v)) setMaxBackupsLocal(v); }}
                        onBlur={async () => { const clamped = Math.max(8, maxBackups); setMaxBackupsLocal(clamped); setSavingMax(true); try { await SetMemoryMaxBackups(clamped); loadBackups(); } catch (e) { setError(String(e)); } setSavingMax(false); }}
                        onKeyDown={e => { if (e.key === "Enter") (e.target as HTMLInputElement).blur(); }}
                        disabled={savingMax}
                        style={{ width: 48, padding: "2px 4px", fontSize: "0.7rem", border: `1px solid ${colors.border}`, borderRadius: radius.sm, textAlign: "center", background: colors.surface, color: colors.text }}
                        aria-label={t("Max backups", "最大备份数")}
                    />
                    {t("backups", "份")}
                </div>
            </div>
            {backupsLoading && <div style={{ fontSize: "0.76rem", color: colors.textMuted }}>{t("Loading…", "加载中…")}</div>}
            <div style={{ display: "flex", flexDirection: "column", gap: 6, maxHeight: "calc(100vh - 390px)", overflowY: "auto" }}>
                {backups.length === 0 && !backupsLoading && (
                    <div style={{ fontSize: "0.78rem", color: colors.textMuted, textAlign: "center", padding: "20px 0" }}>{t("No backups yet", "暂无备份")}</div>
                )}
                {backups.map(bk => (
                    <div key={bk.name} style={{ border: `1px solid ${colors.border}`, borderRadius: radius.md, padding: "8px 10px", background: colors.surface }}>
                        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                            <div style={{ minWidth: 0 }}>
                                <div style={{ fontSize: "0.76rem", color: colors.text, fontWeight: 500 }}>{bk.name}</div>
                                <div style={{ fontSize: "0.68rem", color: colors.textMuted, marginTop: 2 }}>
                                    {fmtDate(bk.created_at, lang)} · {bk.entry_count >= 0 ? `${bk.entry_count} ${t("entries", "条")}` : "?"} · {fmtSize(bk.size_bytes)}
                                </div>
                            </div>
                            <div style={{ display: "flex", gap: 4, flexShrink: 0 }}>
                                <button onClick={() => setRestoreTarget(bk.name)} aria-label={`${t("Restore", "恢复")} ${bk.name}`} title={t("Restore", "恢复")} style={{
                                    padding: "3px 10px", fontSize: "0.7rem", cursor: "pointer", fontWeight: 600,
                                    background: colors.success, color: colors.onPrimary, border: "none", borderRadius: radius.sm,
                                }}>⏪ {t("Restore", "恢复")}</button>
                                <button onClick={() => setDeleteTarget(bk.name)} aria-label={`${t("Delete", "删除")} ${bk.name}`} title={t("Delete", "删除")} style={{
                                    padding: "3px 8px", fontSize: "0.7rem", cursor: "pointer",
                                    background: "none", border: `1px solid ${colors.border}`, borderRadius: radius.sm, color: colors.danger,
                                }}>🗑️</button>
                            </div>
                        </div>
                    </div>
                ))}
            </div>

            {/* Restore confirmation */}
            {restoreTarget && (
                <ModalOverlay onClose={() => setRestoreTarget(null)}>
                    <p style={{ fontSize: "0.82rem", marginBottom: 6 }}>{t("Restore this backup?", "确定恢复此备份？")}</p>
                    <p style={{ fontSize: "0.72rem", color: colors.textMuted, marginBottom: 16 }}>
                        {t("Current memory will be replaced by this backup immediately. A safety backup of the current state will be created first.", "当前记忆将被此备份覆盖并立即生效。覆盖前会自动保存一份当前记忆的备份。")}
                    </p>
                    <div style={{ display: "flex", justifyContent: "flex-end", gap: 8 }}>
                        <button onClick={() => setRestoreTarget(null)} style={cancelBtnStyle}>{t("Cancel", "取消")}</button>
                        <button onClick={() => handleRestore(restoreTarget)} style={{ ...cancelBtnStyle, background: colors.success, color: colors.onPrimary, border: "none" }}>{t("Confirm Restore", "确认恢复")}</button>
                    </div>
                </ModalOverlay>
            )}

            {/* Delete backup confirmation */}
            {deleteTarget && (
                <ModalOverlay onClose={() => setDeleteTarget(null)}>
                    <p style={{ fontSize: "0.82rem", marginBottom: 16 }}>{t("Delete this backup?", "确定删除此备份？")}</p>
                    <div style={{ display: "flex", justifyContent: "flex-end", gap: 8 }}>
                        <button onClick={() => setDeleteTarget(null)} style={cancelBtnStyle}>{t("Cancel", "取消")}</button>
                        <button onClick={() => handleDeleteBackup(deleteTarget)} style={dangerBtnStyle}>{t("Delete", "删除")}</button>
                    </div>
                </ModalOverlay>
            )}
        </>
    );
}

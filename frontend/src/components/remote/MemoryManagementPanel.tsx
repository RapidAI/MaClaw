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
} from "../../../wailsjs/go/main/App";
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
    { value: "user_fact", label: { zh: "用户事实", en: "User Fact" } },
    { value: "preference", label: { zh: "偏好设置", en: "Preference" } },
    { value: "project_knowledge", label: { zh: "项目知识", en: "Project" } },
    { value: "instruction", label: { zh: "指令", en: "Instruction" } },
    { value: "conversation_summary", label: { zh: "对话摘要", en: "Summary" } },
] as const;

const CATEGORY_COLORS: Record<string, string> = {
    user_fact: "#6366f1",
    preference: "#0891b2",
    project_knowledge: "#059669",
    instruction: "#d97706",
    conversation_summary: "#8b5cf6",
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
    border: "none", borderBottom: active ? "2px solid #6366f1" : "2px solid transparent",
    background: "none", color: active ? "#6366f1" : colors.textSecondary,
});

/** Shared date formatter. */
function fmtDate(s: string, isZh: boolean): string {
    if (!s) return "-";
    try { return new Date(s).toLocaleString(isZh ? "zh-CN" : "en-US"); }
    catch { return s; }
}

/** Category value → display label. */
function catLabel(cat: string, isZh: boolean): string {
    const found = CATEGORIES.find(c => c.value === cat);
    return found ? (isZh ? found.label.zh : found.label.en) : cat;
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
    background: colors.surface, cursor: "pointer",
};
const dangerBtnStyle: React.CSSProperties = {
    padding: "5px 14px", fontSize: "0.76rem", border: "none",
    borderRadius: radius.md, background: colors.danger, color: "#fff", cursor: "pointer",
};

export function MemoryManagementPanel({ lang }: Props) {
    const [tab, setTab] = useState<"edit" | "timemachine">("edit");
    const isZh = lang?.startsWith("zh");
    const t = useCallback((zh: string, en: string) => isZh ? zh : en, [isZh]);
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
                    📝 {t("记忆编辑", "Memory Edit")}
                </button>
                <button role="tab" aria-selected={tab === "timemachine"} style={tabBtnStyle(tab === "timemachine")} onClick={() => setTab("timemachine")}>
                    ⏳ {t("时光机", "Time Machine")}
                </button>
                {tab === "edit" && (
                    <div style={{ marginLeft: "auto", display: "flex", alignItems: "center", gap: 8 }}>
                        <span style={{ fontSize: "0.72rem", color: colors.textSecondary }}>{entryCount} {t("条记忆", "entries")}</span>
                        <button onClick={() => createRef.current?.()} style={{ padding: "3px 12px", fontSize: "0.72rem", fontWeight: 600, background: "#6366f1", color: "#fff", border: "none", borderRadius: radius.md, cursor: "pointer" }}>
                            + {t("新建", "New")}
                        </button>
                    </div>
                )}
            </div>
            {tab === "edit"
                ? <MemoryEditTab t={t} isZh={isZh} revision={revision} onCountChange={setEntryCount} createRef={createRef} />
                : <TimeMachineTab t={t} isZh={isZh} onDataChanged={bumpRevision} />}
        </div>
    );
}

// ---------------------------------------------------------------------------
// Tab 1: Memory Edit
// ---------------------------------------------------------------------------
type EditTabProps = {
    t: (zh: string, en: string) => string;
    isZh: boolean;
    /** Incremented externally (e.g. after backup restore) to trigger re-fetch. */
    revision: number;
    onCountChange: (count: number) => void;
    createRef: React.MutableRefObject<(() => void) | null>;
};

function MemoryEditTab({ t, isZh, revision, onCountChange, createRef }: EditTabProps) {
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
                <select value={filterCat} onChange={e => setFilterCat(e.target.value)} aria-label={t("分类筛选", "Filter by category")} style={{ ...inputStyle, width: "auto", padding: "4px 8px", fontSize: "0.76rem" }}>
                    {CATEGORIES.map(c => (<option key={c.value} value={c.value}>{isZh ? c.label.zh : c.label.en}</option>))}
                </select>
                <input placeholder={t("搜索关键词…", "Search keyword…")} value={keyword} onChange={e => setKeyword(e.target.value)} aria-label={t("搜索关键词", "Search keyword")} style={{ ...inputStyle, width: "180px", padding: "4px 8px", fontSize: "0.76rem" }} />
            </div>

            {error && <div role="alert" style={{ color: colors.danger, fontSize: "0.76rem", marginBottom: 8 }}>{error}</div>}
            {loading && <div style={{ fontSize: "0.76rem", color: colors.textMuted }}>{t("加载中…", "Loading…")}</div>}

            {/* Entry list */}
            <div style={{ display: "flex", flexDirection: "column", gap: 6, maxHeight: "calc(100vh - 310px)", overflowY: "auto", border: `1px solid ${colors.border}`, borderRadius: radius.md, padding: 6 }}>
                {entries.length === 0 && !loading && (
                    <div style={{ fontSize: "0.78rem", color: colors.textMuted, textAlign: "center", padding: "20px 0" }}>{t("暂无记忆条目", "No memory entries")}</div>
                )}
                {entries.map(entry => (
                    <div key={entry.id} style={{ border: `1px solid ${colors.border}`, borderRadius: radius.md, padding: "8px 10px", background: colors.surface }}>
                        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", gap: 8 }}>
                            <div style={{ flex: 1, minWidth: 0 }}>
                                <div style={{ display: "flex", alignItems: "center", gap: 6, marginBottom: 4, flexWrap: "wrap" }}>
                                    <span style={{ fontSize: "0.66rem", fontWeight: 600, padding: "1px 6px", borderRadius: radius.sm, color: "#fff", background: CATEGORY_COLORS[entry.category] || colors.textMuted }}>{catLabel(entry.category, isZh)}</span>
                                    {(entry.tags || []).map(tag => (
                                        <span key={tag} style={{ fontSize: "0.64rem", padding: "1px 5px", borderRadius: radius.sm, background: colors.bg, color: colors.textSecondary, border: `1px solid ${colors.border}` }}>{tag}</span>
                                    ))}
                                </div>
                                <div style={{ fontSize: "0.78rem", color: colors.text, whiteSpace: "pre-wrap", wordBreak: "break-word" }}>{entry.content}</div>
                                <div style={{ fontSize: "0.66rem", color: colors.textMuted, marginTop: 4 }}>{t("更新", "Updated")}: {fmtDate(entry.updated_at, isZh)} · {t("访问", "Access")}: {entry.access_count}</div>
                            </div>
                            <div style={{ display: "flex", gap: 4, flexShrink: 0 }}>
                                <button onClick={() => openEdit(entry)} aria-label={t("编辑", "Edit")} title={t("编辑", "Edit")} style={{ padding: "3px 8px", fontSize: "0.7rem", cursor: "pointer", background: "none", border: `1px solid ${colors.border}`, borderRadius: radius.sm, color: colors.textSecondary }}>✏️</button>
                                <button onClick={() => setDeleteTarget(entry.id)} aria-label={t("删除", "Delete")} title={t("删除", "Delete")} style={{ padding: "3px 8px", fontSize: "0.7rem", cursor: "pointer", background: "none", border: `1px solid ${colors.border}`, borderRadius: radius.sm, color: colors.danger }}>🗑️</button>
                            </div>
                        </div>
                    </div>
                ))}
            </div>

            {/* Delete confirmation */}
            {deleteTarget && (
                <ModalOverlay onClose={() => setDeleteTarget(null)}>
                    <p style={{ fontSize: "0.82rem", marginBottom: 16 }}>{t("确定删除这条记忆？此操作不可撤销。", "Delete this memory? This cannot be undone.")}</p>
                    <div style={{ display: "flex", justifyContent: "flex-end", gap: 8 }}>
                        <button onClick={() => setDeleteTarget(null)} style={cancelBtnStyle}>{t("取消", "Cancel")}</button>
                        <button onClick={() => handleDelete(deleteTarget)} style={dangerBtnStyle}>{t("删除", "Delete")}</button>
                    </div>
                </ModalOverlay>
            )}

            {/* Create / Edit dialog */}
            {dlgOpen && (
                <ModalOverlay onClose={() => setDlgOpen(false)}>
                    <div style={{ width: 420, maxWidth: "90vw" }}>
                        <h4 style={{ fontSize: "0.82rem", margin: "0 0 14px", color: colors.text }}>{editEntry ? t("编辑记忆", "Edit Memory") : t("新建记忆", "New Memory")}</h4>
                        <div style={{ marginBottom: 10 }}>
                            <label style={labelStyle}>{t("分类", "Category")}</label>
                            <select value={formCategory} onChange={e => setFormCategory(e.target.value)} style={{ ...inputStyle, width: "auto" }}>
                                {CATEGORIES.filter(c => c.value).map(c => (<option key={c.value} value={c.value}>{isZh ? c.label.zh : c.label.en}</option>))}
                            </select>
                        </div>
                        <div style={{ marginBottom: 10 }}>
                            <label style={labelStyle}>{t("内容", "Content")}</label>
                            <textarea value={formContent} onChange={e => setFormContent(e.target.value)} rows={4} style={{ ...inputStyle, resize: "vertical", fontFamily: "inherit" }} />
                        </div>
                        <div style={{ marginBottom: 14 }}>
                            <label style={labelStyle}>{t("标签（逗号分隔）", "Tags (comma separated)")}</label>
                            <input value={formTags} onChange={e => setFormTags(e.target.value)} placeholder="tag1, tag2" style={inputStyle} />
                        </div>
                        <div style={{ display: "flex", justifyContent: "flex-end", gap: 8 }}>
                            <button onClick={() => setDlgOpen(false)} style={cancelBtnStyle}>{t("取消", "Cancel")}</button>
                            <button onClick={handleSave} disabled={saving || !formContent.trim()} style={{ padding: "5px 14px", fontSize: "0.76rem", border: "none", borderRadius: radius.md, background: "#6366f1", color: "#fff", cursor: "pointer", opacity: saving || !formContent.trim() ? 0.5 : 1 }}>{saving ? t("保存中…", "Saving…") : t("保存", "Save")}</button>
                        </div>
                    </div>
                </ModalOverlay>
            )}
        </>
    );
}

// ---------------------------------------------------------------------------
// Tab 2: Time Machine (compression + backup restore)
// ---------------------------------------------------------------------------
type TimeMachineProps = {
    t: (zh: string, en: string) => string;
    isZh: boolean;
    /** Called after restore or compress so the edit tab can refresh. */
    onDataChanged: () => void;
};

function TimeMachineTab({ t, isZh, onDataChanged }: TimeMachineProps) {
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

    useEffect(() => { loadBackups(); loadStatus(); }, [loadBackups, loadStatus]);

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
        } catch (e) { setError(String(e)); }
        setToggling(false);
    };

    const handleCompress = async () => {
        setCompressing(true); setError(""); setCompressResult(null);
        try {
            const result = await CompressMemories();
            setCompressResult(result as CompressResult);
            await loadBackups();
            onDataChanged();
        } catch (e) { setError(String(e)); }
        setCompressing(false);
    };

    const handleRestore = async (name: string) => {
        setError("");
        try {
            await RestoreMemoryBackup(name);
            setRestoreTarget(null);
            await loadBackups();
            onDataChanged();
        } catch (e) { setError(String(e)); }
    };

    const handleDeleteBackup = async (name: string) => {
        setError("");
        try { await DeleteMemoryBackup(name); setDeleteTarget(null); await loadBackups(); }
        catch (e) { setError(String(e)); }
    };

    return (
        <>
            {/* Auto-compress + One-shot compress in one row */}
            <div style={{ border: `1px solid ${colors.border}`, borderRadius: radius.md, padding: "10px 14px", background: colors.surface, marginBottom: 12 }}>
                <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 8 }}>
                    <div style={{ display: "flex", alignItems: "center", gap: 6, fontSize: "0.76rem", color: colors.text, fontWeight: 600, minWidth: 0 }}>
                        🔄 {t("自动压缩", "Auto-Compress")}
                        <span style={{ fontSize: "0.68rem", color: colors.textMuted, fontWeight: 400 }}>
                            {t("每6h去重+LLM压缩", "Every 6h dedup+LLM")}
                        </span>
                        {serviceStatus?.last_run && (
                            <span style={{ fontSize: "0.66rem", color: colors.textMuted, fontWeight: 400 }}>
                                · {fmtDate(serviceStatus.last_run, isZh)}
                                {serviceStatus.last_error && <span style={{ color: colors.danger }}> · {serviceStatus.last_error}</span>}
                            </span>
                        )}
                    </div>
                    <div style={{ display: "flex", alignItems: "center", gap: 6, flexShrink: 0 }}>
                        <button onClick={handleToggleAuto} disabled={toggling} style={{
                            padding: "4px 14px", fontSize: "0.74rem", fontWeight: 600, border: "none", borderRadius: radius.md, cursor: toggling ? "wait" : "pointer",
                            background: autoEnabled ? "#059669" : colors.textMuted, color: "#fff", whiteSpace: "nowrap",
                        }}>
                            {autoEnabled ? t("已开启", "ON") : t("已关闭", "OFF")}
                        </button>
                        <button onClick={handleCompress} disabled={compressing} aria-label={t("立即压缩", "Compress Now")} style={{
                            padding: "4px 14px", fontSize: "0.74rem", fontWeight: 600, border: "none", borderRadius: radius.md, cursor: compressing ? "wait" : "pointer",
                            background: compressing ? colors.textMuted : "#6366f1", color: "#fff", opacity: compressing ? 0.6 : 1, whiteSpace: "nowrap",
                        }}>
                            {compressing ? t("压缩中…", "…") : t("立即压缩", "Compress")}
                        </button>
                    </div>
                </div>
                {compressResult && (
                    <div role="status" style={{ fontSize: "0.72rem", color: "#059669", background: "#ecfdf5", borderRadius: radius.sm, padding: "5px 10px", marginTop: 6 }}>
                        {compressResult.dedup_count > 0 && <>{t("去重", "Dedup")}: {compressResult.dedup_count} {t("条移除", "removed")} · </>}
                        {compressResult.merged_count > 0 && <>{t("合并", "Merged")}: {compressResult.merged_count} {t("条合并", "merged")} · </>}
                        {t("压缩", "Compress")}: {compressResult.compressed_count} {t("条已压缩", "compressed")}, {compressResult.skipped_count} {t("条跳过", "skipped")}, {compressResult.error_count} {t("条失败", "errors")}, {t("节省", "saved")} {compressResult.saved_chars} {t("字符", "chars")}
                    </div>
                )}
            </div>

            {error && <div role="alert" style={{ color: colors.danger, fontSize: "0.76rem", marginBottom: 8 }}>{error}</div>}

            {/* Backup list */}
            <div style={{ fontSize: "0.8rem", color: colors.text, fontWeight: 600, marginBottom: 8 }}>📦 {t("历史备份", "Backup History")}</div>
            {backupsLoading && <div style={{ fontSize: "0.76rem", color: colors.textMuted }}>{t("加载中…", "Loading…")}</div>}
            <div style={{ display: "flex", flexDirection: "column", gap: 6, maxHeight: "calc(100vh - 390px)", overflowY: "auto" }}>
                {backups.length === 0 && !backupsLoading && (
                    <div style={{ fontSize: "0.78rem", color: colors.textMuted, textAlign: "center", padding: "20px 0" }}>{t("暂无备份", "No backups yet")}</div>
                )}
                {backups.map(bk => (
                    <div key={bk.name} style={{ border: `1px solid ${colors.border}`, borderRadius: radius.md, padding: "8px 10px", background: colors.surface }}>
                        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                            <div style={{ minWidth: 0 }}>
                                <div style={{ fontSize: "0.76rem", color: colors.text, fontWeight: 500 }}>{bk.name}</div>
                                <div style={{ fontSize: "0.68rem", color: colors.textMuted, marginTop: 2 }}>
                                    {fmtDate(bk.created_at, isZh)} · {bk.entry_count >= 0 ? `${bk.entry_count} ${t("条", "entries")}` : "?"} · {fmtSize(bk.size_bytes)}
                                </div>
                            </div>
                            <div style={{ display: "flex", gap: 4, flexShrink: 0 }}>
                                <button onClick={() => setRestoreTarget(bk.name)} aria-label={`${t("恢复", "Restore")} ${bk.name}`} title={t("恢复", "Restore")} style={{
                                    padding: "3px 10px", fontSize: "0.7rem", cursor: "pointer", fontWeight: 600,
                                    background: "#059669", color: "#fff", border: "none", borderRadius: radius.sm,
                                }}>⏪ {t("恢复", "Restore")}</button>
                                <button onClick={() => setDeleteTarget(bk.name)} aria-label={`${t("删除", "Delete")} ${bk.name}`} title={t("删除", "Delete")} style={{
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
                    <p style={{ fontSize: "0.82rem", marginBottom: 6 }}>{t("确定恢复此备份？", "Restore this backup?")}</p>
                    <p style={{ fontSize: "0.72rem", color: colors.textMuted, marginBottom: 16 }}>
                        {t("当前记忆将被此备份覆盖并立即生效。覆盖前会自动保存一份当前记忆的备份。", "Current memory will be replaced by this backup immediately. A safety backup of the current state will be created first.")}
                    </p>
                    <div style={{ display: "flex", justifyContent: "flex-end", gap: 8 }}>
                        <button onClick={() => setRestoreTarget(null)} style={cancelBtnStyle}>{t("取消", "Cancel")}</button>
                        <button onClick={() => handleRestore(restoreTarget)} style={{ ...cancelBtnStyle, background: "#059669", color: "#fff", border: "none" }}>{t("确认恢复", "Confirm Restore")}</button>
                    </div>
                </ModalOverlay>
            )}

            {/* Delete backup confirmation */}
            {deleteTarget && (
                <ModalOverlay onClose={() => setDeleteTarget(null)}>
                    <p style={{ fontSize: "0.82rem", marginBottom: 16 }}>{t("确定删除此备份？", "Delete this backup?")}</p>
                    <div style={{ display: "flex", justifyContent: "flex-end", gap: 8 }}>
                        <button onClick={() => setDeleteTarget(null)} style={cancelBtnStyle}>{t("取消", "Cancel")}</button>
                        <button onClick={() => handleDeleteBackup(deleteTarget)} style={dangerBtnStyle}>{t("删除", "Delete")}</button>
                    </div>
                </ModalOverlay>
            )}
        </>
    );
}

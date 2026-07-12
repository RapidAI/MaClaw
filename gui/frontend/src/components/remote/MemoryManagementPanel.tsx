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
    GetMemoryStatus, GetExperienceLearningSnapshot, GetExperienceGovernanceSummary,
} from "../../../wailsjs/go/main/App";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";
import { colors, radius } from "./styles";
import { ExperienceLearningPanel } from "./MemoryExperienceLearningPanel";
import { SessionHistoryTab } from "./MemorySessionHistoryTab";
import { useSafeBackdropDismiss } from "../../hooks/useSafeBackdropDismiss";

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

const buttonSpinnerStyle: React.CSSProperties = {
    width: 12,
    height: 12,
    borderRadius: "50%",
    border: "2px solid currentColor",
    borderRightColor: "transparent",
    display: "inline-block",
    animation: "spin 0.8s linear infinite",
    flexShrink: 0,
};

const CATEGORIES = [
    { value: "", label: { zh: "全部", en: "All" } },
    { value: "self_identity", label: { zh: "自我认知", en: "Self Identity" } },
    { value: "user_fact", label: { zh: "用户事实", en: "User Fact" } },
    { value: "preference", label: { zh: "偏好设置", en: "Preference" } },
    { value: "project_knowledge", label: { zh: "项目知识", en: "Project" } },
    { value: "instruction", label: { zh: "指令", en: "Instruction" } },
    { value: "conversation_summary", label: { zh: "对话摘要", en: "Summary" } },
    { value: "session_checkpoint", label: { zh: "会话检查点", en: "Checkpoint" } },
    { value: "task_artifact", label: { zh: "任务产出物", en: "Task Artifact" } },
    { value: "profile", label: { zh: "用户画像", en: "Profile" } },
] as const;

const CATEGORY_COLORS: Record<string, string> = {
    self_identity: "var(--theme-primary-strong, #183b63)",
    user_fact: "var(--theme-primary)",
    preference: "var(--theme-info, var(--theme-primary))",
    project_knowledge: "var(--theme-success)",
    instruction: "var(--theme-primary)",
    conversation_summary: "var(--theme-primary-strong, #183b63)",
    session_checkpoint: "var(--theme-text-muted)",
    task_artifact: "var(--theme-text-muted)",
    profile: "var(--theme-success)",
};

type TraceFocus = { value?: string; seq?: number };
type Props = { lang: string; traceFocus?: TraceFocus };
type WailsNoDragStyle = React.CSSProperties & {
    WebkitAppRegion?: "no-drag";
    "--wails-draggable"?: "no-drag";
};

const inputStyle: React.CSSProperties = {
    width: "100%", padding: "7px 10px", fontSize: "0.8rem",
    border: `1px solid ${colors.border}`, borderRadius: 4,
    background: colors.surface, color: colors.text, boxSizing: "border-box",
};
const labelStyle: React.CSSProperties = {
    fontSize: "0.76rem", color: colors.textSecondary, marginBottom: 4, display: "block",
};
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
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(onClose);

    useEffect(() => {
        const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") onClose(); };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [onClose]);
    return (
        <div style={overlayStyle} {...backdropProps}>
            <div
                role="dialog"
                aria-modal="true"
                style={dialogBaseStyle}
                {...dialogProps}
            >
                {children}
            </div>
        </div>
    );
}

const overlayStyle: WailsNoDragStyle = {
    position: "fixed", inset: 0, background: "rgba(0,0,0,0.3)",
    display: "flex", alignItems: "center", justifyContent: "center", zIndex: 9999,
    WebkitAppRegion: "no-drag", "--wails-draggable": "no-drag",
};
const dialogBaseStyle: WailsNoDragStyle = {
    background: colors.surface, borderRadius: radius.lg,
    padding: "20px 24px", minWidth: 280, boxShadow: "0 8px 30px rgba(0,0,0,0.12)",
    WebkitAppRegion: "no-drag", "--wails-draggable": "no-drag",
};
const cancelBtnStyle: React.CSSProperties = {
    padding: "5px 14px", fontSize: "0.76rem",
    border: `1px solid ${colors.border}`, borderRadius: radius.md,
    background: colors.surface, color: colors.text, cursor: "pointer",
};
const primaryBtnStyle: React.CSSProperties = {
    padding: "5px 14px", fontSize: "0.76rem", fontWeight: 600,
    border: `1px solid ${colors.primary}`, borderRadius: radius.md,
    background: colors.primaryLight, color: colors.primaryDark, cursor: "pointer",
};
const dangerBtnStyle: React.CSSProperties = {
    padding: "5px 14px", fontSize: "0.76rem", fontWeight: 600,
    border: `1px solid ${colors.danger}`, borderRadius: radius.md,
    background: colors.dangerBg, color: colors.danger, cursor: "pointer",
};

export function MemoryManagementPanel({ lang, traceFocus }: Props) {
    const [tab, setTab] = useState<"edit" | "timemachine" | "history" | "status">(traceFocus?.seq ? "status" : "edit");
    const t = useCallback((en: string, zhHans: string, zhHant: string = zhHans) =>
        lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en, [lang]);
    // Revision counter — bumped by TimeMachine after restore/compress so
    // MemoryEditTab can re-fetch without unmount/remount.
    const [revision, setRevision] = useState(0);
    const bumpRevision = useCallback(() => setRevision(r => r + 1), []);
    const [entryCount, setEntryCount] = useState(0);
    const [localTraceFocus, setLocalTraceFocus] = useState<TraceFocus>({ value: "", seq: 0 });
    const activeTraceFocus = (traceFocus?.seq || 0) >= (localTraceFocus.seq || 0) ? traceFocus : localTraceFocus;
    useEffect(() => { if (traceFocus?.seq) setTab("status"); }, [traceFocus?.seq]);
    const openLocalTraceFocus = useCallback((value: string) => {
        setLocalTraceFocus((prev) => ({ value, seq: Math.max(prev.seq || 0, traceFocus?.seq || 0) + 1 }));
        setTab("status");
    }, [traceFocus?.seq]);
    const createRef = useRef<(() => void) | null>(null);
    const memoryTabs = [
        { id: "edit" as const, label: t("Memory Edit", "记忆编辑"), desc: t("Create, filter, and edit entries", "新建、筛选与编辑条目") },
        { id: "status" as const, label: t("Memory Status", "记忆状态"), desc: t("Capacity, categories, and learning", "容量、分类与学习状态") },
        { id: "history" as const, label: t("Session History", "会话历史"), desc: t("Browse remembered sessions", "查看已沉淀会话") },
        { id: "timemachine" as const, label: t("Time Machine", "时光机"), desc: t("Backups and compression", "备份与压缩") },
    ];

    return (
        <div className="memory-management-panel">
            <div className="settings-subtab-row">
                <div className="settings-subtab-bar settings-subtab-bar--memory" role="tablist" aria-label={t("Memory sections", "记忆管理分区")}>
                    {memoryTabs.map(item => (
                        <button
                            key={item.id}
                            id={`memory-tab-${item.id}`}
                            className="settings-subtab-button"
                            data-active={tab === item.id ? "true" : undefined}
                            type="button"
                            role="tab"
                            aria-selected={tab === item.id}
                            aria-controls={`memory-panel-${item.id}`}
                            aria-label={item.label}
                            onClick={() => setTab(item.id)}
                        >
                            <span className="settings-subtab-button__label">{item.label}</span>
                            <span className="settings-subtab-button__desc">{item.desc}</span>
                        </button>
                    ))}
                </div>
                {tab === "edit" && (
                    <div className="settings-subtab-actions">
                        <span className="settings-subtab-count">{entryCount} {t("entries", "条记忆")}</span>
                        <button className="settings-subtab-primary" onClick={() => createRef.current?.()}>
                            + {t("New", "新建")}
                        </button>
                    </div>
                )}
            </div>
            {tab === "edit"
                ? <div role="tabpanel" id="memory-panel-edit" aria-labelledby="memory-tab-edit"><MemoryEditTab t={t} lang={lang} revision={revision} onCountChange={setEntryCount} createRef={createRef} /></div>
                : tab === "status"
                ? <div role="tabpanel" id="memory-panel-status" aria-labelledby="memory-tab-status"><MemoryStatusTab t={t} lang={lang} traceFocus={activeTraceFocus} /></div>
                : tab === "history"
                ? <div role="tabpanel" id="memory-panel-history" aria-labelledby="memory-tab-history"><SessionHistoryTab t={t} lang={lang} onOpenTrace={openLocalTraceFocus} /></div>
                : <div role="tabpanel" id="memory-panel-timemachine" aria-labelledby="memory-tab-timemachine"><TimeMachineTab t={t} lang={lang} onDataChanged={bumpRevision} /></div>}
        </div>
    );
}

// ---------------------------------------------------------------------------
// Tab: Memory Status (pie chart + capacity gauge)
// ---------------------------------------------------------------------------

interface MemoryStatusData {
    total_entries: number;
    max_capacity: number;
    capacity_percent: number;
    archived_entries: number;
    stale_entries: number;
    pinned_entries: number;
    embedder_active: boolean;
    oldest_entry?: string;
    newest_entry?: string;
    categories: Array<{
        category: string;
        label: string;
        count: number;
        percent: number;
    }>;
}

// Pie chart color palette — visually distinct, works on both light and dark themes.
const PIE_COLORS = [
    "#2f5f98",
    "#4f7f6f",
    "#64748b",
    "#183b63",
    "#5b7898",
    "#8aa4bf",
    "#526579",
    "#7f8da1",
    "#3f5872",
    "#6b8baa",
    "#789185",
    "#94a3b8",
    "#b7c5d4",
];

function MemoryStatusTab({ t, lang, traceFocus }: { t: (en: string, zhHans: string, zhHant?: string) => string; lang: string; traceFocus?: TraceFocus }) {
    const [data, setData] = useState<MemoryStatusData | null>(null);
    const [loading, setLoading] = useState(true);
    const [learning, setLearning] = useState<any>(null);
    const [learningError, setLearningError] = useState("");

    const fetchData = useCallback(async () => {
        setLoading(true);
        try {
            const result = await GetMemoryStatus();
            setData(result);
            try {
                const snapshot = await GetExperienceLearningSnapshot();
                try {
                    const governanceSummary = await GetExperienceGovernanceSummary({});
                    setLearning({ ...(snapshot || {}), governance_summary: governanceSummary });
                } catch {
                    setLearning(snapshot);
                }
                setLearningError("");
            } catch (learningErr) {
                setLearning(null);
                setLearningError(String(learningErr));
            }
        } catch (e) {
            console.error("GetMemoryStatus failed:", e);
        } finally {
            setLoading(false);
        }
    }, []);

    useEffect(() => { fetchData(); }, [fetchData]);

    if (loading || !data) {
        return <div className="memory-status-loading">{t("Loading...", "加载中...")}</div>;
    }

    const cats = (data.categories || []).map((item) => ({
        ...item,
        count: Number.isFinite(item.count) ? Math.max(0, item.count) : 0,
        percent: Number.isFinite(item.percent) ? Math.max(0, item.percent) : 0,
        label: String(item.label || "").trim() || catLabel(item.category, lang) || item.category || t("Uncategorized", "未分类"),
    }));
    const totalEntries = Math.max(0, data.total_entries || 0);
    const maxCap = data.max_capacity || 2000;
    const capPct = Math.max(0, data.capacity_percent || 0);
    const boundedEntries = Math.max(0, Math.min(totalEntries, maxCap));
    const boundedCapPct = Math.min(capPct, 100);

    return (
        <div className="memory-status-tab">
            <section className="memory-status-card memory-status-card--capacity" aria-labelledby="memory-capacity-title">
                <div className="memory-status-card__header">
                    <h3 id="memory-capacity-title" className="memory-status-card__title">{t("Capacity", "容量使用")}</h3>
                    <span className="memory-capacity-count">{totalEntries} / {maxCap}</span>
                </div>
                <div
                    className="memory-capacity-track"
                    role="meter"
                    aria-valuemin={0}
                    aria-valuemax={maxCap}
                    aria-valuenow={boundedEntries}
                    aria-valuetext={`${totalEntries} / ${maxCap}`}
                >
                    <div
                        className="memory-capacity-fill"
                        style={{
                            width: `${boundedCapPct}%`,
                            background: capPct >= 90 ? "var(--theme-danger)" : capPct >= 70 ? "var(--theme-primary)" : "var(--theme-success)",
                        }}
                    />
                </div>
                <p className="memory-status-hint">
                    {capPct >= 90
                        ? t("Capacity is nearly full. Older memories may be evicted.", "容量接近上限，旧记忆可能会被淘汰。")
                        : capPct >= 70
                        ? t("Capacity usage is moderate.", "容量使用适中。")
                        : t("Capacity usage is healthy.", "容量充足。")}
                </p>
            </section>

            <section className="memory-status-card" aria-labelledby="memory-category-title">
                <div className="memory-status-card__header">
                    <h3 id="memory-category-title" className="memory-status-card__title">{t("Category Distribution", "分类占比")}</h3>
                    <span className="memory-status-card__meta">{cats.length} {t("categories", "类")}</span>
                </div>
                {cats.length === 0 ? (
                    <div className="memory-status-empty">{t("No memory entries yet.", "暂无记忆数据。")}</div>
                ) : (
                    <div className="memory-category-layout">
                        <div className="memory-category-chart-wrap">
                            <PieChart
                                data={cats}
                                size={172}
                                centerLabel={t("entries", "条记忆")}
                                ariaLabel={t("Memory category distribution", "记忆分类占比")}
                            />
                        </div>
                        <div className="memory-category-legend" aria-label={t("Category legend", "分类图例")}>
                            {cats.map((c, i) => (
                                <div key={c.category || i} className="memory-category-legend__item">
                                    <span className="memory-category-legend__swatch" style={{ background: PIE_COLORS[i % PIE_COLORS.length] }} />
                                    <span className="memory-category-legend__label" title={c.label}>{c.label}</span>
                                    <span className="memory-category-legend__value">{c.count}{t(" entries", "条")} ({c.percent.toFixed(1)}%)</span>
                                </div>
                            ))}
                        </div>
                    </div>
                )}
            </section>

            <div className="memory-status-stat-grid">
                <MemoryStatusStatCard label={t("Archived", "已归档")} value={data.archived_entries} />
                <MemoryStatusStatCard label={t("Stale", "过期")} value={data.stale_entries} />
                <MemoryStatusStatCard label={t("Pinned", "固定")} value={data.pinned_entries} />
                <MemoryStatusStatCard label={t("Embedder", "向量化")} value={data.embedder_active ? t("Active", "已启用") : t("Off", "未启用")} />
            </div>

            <ExperienceLearningPanel t={t} learning={learning} error={learningError} focusTrace={traceFocus} onReviewed={fetchData} />

            {(data.oldest_entry || data.newest_entry) && (
                <section className="memory-status-card memory-status-range" aria-label={t("Memory time range", "记忆时间范围")}>
                    {data.oldest_entry && <span>{t("Oldest", "最早")}: {fmtDate(data.oldest_entry, lang)}</span>}
                    {data.newest_entry && <span>{t("Newest", "最新")}: {fmtDate(data.newest_entry, lang)}</span>}
                </section>
            )}

            <div className="memory-status-refresh-row">
                <button type="button" onClick={fetchData} className="memory-status-refresh-btn">
                    {t("Refresh", "刷新")}
                </button>
            </div>
        </div>
    );
}

function MemoryStatusStatCard({ label, value }: { label: string; value: number | string }) {
    return (
        <div className="memory-status-stat-card">
            <div className="memory-status-stat-card__value">{value}</div>
            <div className="memory-status-stat-card__label">{label}</div>
        </div>
    );
}


/** Pure SVG donut/pie chart — no external dependencies. */
function PieChart({ data, size = 160, centerLabel, ariaLabel }: { data: Array<{ category: string; label: string; count: number; percent: number }>; size?: number; centerLabel: string; ariaLabel: string }) {
    const cx = size / 2;
    const cy = size / 2;
    const outerR = size / 2 - 4;
    const innerR = outerR * 0.55; // donut hole

    // Build arc paths.
    let startAngle = -90; // start from top
    const arcs: Array<{ path: string; color: string; label: string; pct: number }> = [];

    for (let i = 0; i < data.length; i++) {
        const pct = data[i].percent;
        const sweep = (pct / 100) * 360;
        if (sweep < 0.5) continue; // skip tiny slices

        const endAngle = startAngle + sweep;
        const largeArc = sweep > 180 ? 1 : 0;

        const toRad = (deg: number) => (deg * Math.PI) / 180;
        const x1o = cx + outerR * Math.cos(toRad(startAngle));
        const y1o = cy + outerR * Math.sin(toRad(startAngle));
        const x2o = cx + outerR * Math.cos(toRad(endAngle));
        const y2o = cy + outerR * Math.sin(toRad(endAngle));
        const x1i = cx + innerR * Math.cos(toRad(endAngle));
        const y1i = cy + innerR * Math.sin(toRad(endAngle));
        const x2i = cx + innerR * Math.cos(toRad(startAngle));
        const y2i = cy + innerR * Math.sin(toRad(startAngle));

        const path = [
            `M ${x1o} ${y1o}`,
            `A ${outerR} ${outerR} 0 ${largeArc} 1 ${x2o} ${y2o}`,
            `L ${x1i} ${y1i}`,
            `A ${innerR} ${innerR} 0 ${largeArc} 0 ${x2i} ${y2i}`,
            "Z",
        ].join(" ");

        arcs.push({ path, color: PIE_COLORS[i % PIE_COLORS.length], label: data[i].label, pct });
        startAngle = endAngle;
    }

    // Center text: total count.
    const total = data.reduce((s, d) => s + d.count, 0);

    return (
        <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} role="img" aria-label={ariaLabel}>
            {arcs.map((a, i) => (
                <path key={i} d={a.path} fill={a.color} stroke={colors.surface} strokeWidth={1.5}>
                    <title>{a.label}: {a.pct.toFixed(1)}%</title>
                </path>
            ))}
            {/* Center label */}
            <text x={cx} y={cy - 6} textAnchor="middle" fill="currentColor" fontSize={size * 0.13} fontWeight={700}>
                {total}
            </text>
            <text x={cx} y={cy + 10} textAnchor="middle" fill="currentColor" fontSize={size * 0.075} opacity={0.6}>
                {centerLabel}
            </text>
        </svg>
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
                                <button onClick={() => openEdit(entry)} aria-label={t("Edit", "编辑")} title={t("Edit", "编辑")} style={{ padding: "3px 8px", fontSize: "0.7rem", cursor: "pointer", background: "none", border: `1px solid ${colors.border}`, borderRadius: radius.sm, color: colors.textSecondary }}>EDIT</button>
                                <button onClick={() => setDeleteTarget(entry.id)} aria-label={t("Delete", "删除")} title={t("Delete", "删除")} style={{ padding: "3px 8px", fontSize: "0.7rem", cursor: "pointer", background: "none", border: `1px solid ${colors.border}`, borderRadius: radius.sm, color: colors.danger }}>DEL</button>
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
                            <button onClick={handleSave} disabled={saving || !formContent.trim()} style={{ ...primaryBtnStyle, opacity: saving || !formContent.trim() ? 0.5 : 1, cursor: saving || !formContent.trim() ? "default" : "pointer" }}>{saving ? t("Saving…", "保存中…") : t("Save", "保存")}</button>
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
                        {t("Auto-Compress", "自动压缩")}
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
                            padding: "4px 14px", fontSize: "0.74rem", fontWeight: 600, border: `1px solid ${autoEnabled ? colors.success : colors.border}`, borderRadius: radius.md, cursor: toggling ? "wait" : "pointer",
                            background: autoEnabled ? colors.successBg : colors.surfaceMuted, color: autoEnabled ? colors.success : colors.textSecondary, whiteSpace: "nowrap",
                        }}>
                            {autoEnabled ? t("ON", "已开启") : t("OFF", "已关闭")}
                        </button>
                        <button onClick={handleCompress} disabled={compressing} aria-label={t("Compress Now", "立即压缩")} style={{
                            padding: "4px 14px", fontSize: "0.74rem", fontWeight: 600, border: `1px solid ${compressing ? colors.border : colors.primary}`, borderRadius: radius.md, cursor: compressing ? "wait" : "pointer",
                            background: compressing ? colors.surfaceMuted : colors.primaryLight, color: compressing ? colors.textMuted : colors.primaryDark, opacity: compressing ? 0.75 : 1, whiteSpace: "nowrap",
                            display: "inline-flex", alignItems: "center", justifyContent: "center", gap: 6,
                        }}>
                            {compressing && <span aria-hidden="true" style={buttonSpinnerStyle} />}
                            {compressing ? t("Compressing…", "压缩中…") : t("Compress", "立即压缩")}
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
                <div style={{ fontSize: "0.8rem", color: colors.text, fontWeight: 600 }}>PACK {t("Backup History", "历史备份")}</div>
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
                                    background: colors.successBg, color: colors.success, border: `1px solid ${colors.success}`, borderRadius: radius.sm,
                                }}>RESTORE</button>
                                <button onClick={() => setDeleteTarget(bk.name)} aria-label={`${t("Delete", "删除")} ${bk.name}`} title={t("Delete", "删除")} style={{
                                    padding: "3px 8px", fontSize: "0.7rem", cursor: "pointer",
                                    background: "none", border: `1px solid ${colors.border}`, borderRadius: radius.sm, color: colors.danger,
                                }}>DEL</button>
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
                        <button onClick={() => handleRestore(restoreTarget)} style={{ ...cancelBtnStyle, background: colors.successBg, color: colors.success, border: `1px solid ${colors.success}`, fontWeight: 600 }}>{t("Confirm Restore", "确认恢复")}</button>
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

import { useCallback, useEffect, useState } from "react";
import type { CSSProperties, ReactNode } from "react";
import { BrowserOpenURL } from "../../../wailsjs/runtime";
import { getWailsAppModule } from "../../utils/wailsAppModule";

// --- Types ---

export interface DirectoryItem {
    instance_id: string;
    workflow_name: string;
    status: string;
    current_node?: string;
    initiator_name?: string;
    initiated_at: string;
    completed_at?: string;
    result?: string;
    user_role?: string;
    urgency?: string;
    time_remaining_hours?: number;
    confirm_type?: string;
}

export interface DirectoryResponse {
    items: DirectoryItem[];
    total: number;
    page: number;
    page_size: number;
}

export interface DirectoryFilter {
    status?: string;
    date_from?: string; // ISO 8601
    date_to?: string;
    workflow_type?: string;
    page: number;
    page_size: number;
}

type DirectoryView = "initiated" | "pending_action" | "pending_confirmation" | "completed";

export interface WorkflowDirectoryPanelProps {
    /** Hub base URL for constructing instance detail links */
    hubBaseUrl?: string;
    /** Wails binding override (for testing or when binding is available) */
    getWorkflowDirectory?: (view: string, filter: string) => Promise<DirectoryResponse>;
}

// --- Constants ---

const PAGE_SIZE = 20;

const TAB_DEFS: { key: DirectoryView; label: string }[] = [
    { key: "initiated", label: "我发起的" },
    { key: "pending_action", label: "待我处理的" },
    { key: "pending_confirmation", label: "待我确认的" },
    { key: "completed", label: "已完成的" },
];

const STATUS_OPTIONS: { value: string; label: string }[] = [
    { value: "", label: "全部状态" },
    { value: "running", label: "进行中" },
    { value: "completed", label: "已完成" },
    { value: "withdrawn", label: "已撤回" },
    { value: "cancelled", label: "已取消" },
];

// --- Filter Bar Component ---

interface FilterBarProps {
    status: string;
    dateFrom: string;
    dateTo: string;
    workflowType: string;
    onStatusChange: (v: string) => void;
    onDateFromChange: (v: string) => void;
    onDateToChange: (v: string) => void;
    onWorkflowTypeChange: (v: string) => void;
}

function DirectoryFilterBar({
    status,
    dateFrom,
    dateTo,
    workflowType,
    onStatusChange,
    onDateFromChange,
    onDateToChange,
    onWorkflowTypeChange,
}: FilterBarProps) {
    return (
        <div style={filterBarStyle} role="search" aria-label="过滤条件">
            {/* Status filter */}
            <label style={filterLabelStyle}>
                <span style={filterLabelTextStyle}>状态</span>
                <select
                    value={status}
                    onChange={(e) => onStatusChange(e.target.value)}
                    style={filterSelectStyle}
                    aria-label="状态过滤"
                >
                    {STATUS_OPTIONS.map((opt) => (
                        <option key={opt.value} value={opt.value}>{opt.label}</option>
                    ))}
                </select>
            </label>

            {/* Date range */}
            <label style={filterLabelStyle}>
                <span style={filterLabelTextStyle}>起始日期</span>
                <input
                    type="date"
                    value={dateFrom}
                    onChange={(e) => onDateFromChange(e.target.value)}
                    style={filterInputStyle}
                    aria-label="起始日期"
                />
            </label>
            <label style={filterLabelStyle}>
                <span style={filterLabelTextStyle}>截止日期</span>
                <input
                    type="date"
                    value={dateTo}
                    onChange={(e) => onDateToChange(e.target.value)}
                    style={filterInputStyle}
                    aria-label="截止日期"
                />
            </label>

            {/* Workflow type */}
            <label style={filterLabelStyle}>
                <span style={filterLabelTextStyle}>工作流类型</span>
                <input
                    type="text"
                    value={workflowType}
                    onChange={(e) => onWorkflowTypeChange(e.target.value)}
                    placeholder="输入类型名称"
                    style={filterInputStyle}
                    aria-label="工作流类型过滤"
                />
            </label>
        </div>
    );
}

// --- Component ---

export function WorkflowDirectoryPanel({ hubBaseUrl, getWorkflowDirectory }: WorkflowDirectoryPanelProps) {
    const [activeView, setActiveView] = useState<DirectoryView>("initiated");
    const [items, setItems] = useState<DirectoryItem[]>([]);
    const [total, setTotal] = useState(0);
    const [page, setPage] = useState(1);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);

    // Filter state
    const [filterStatus, setFilterStatus] = useState("");
    const [filterDateFrom, setFilterDateFrom] = useState("");
    const [filterDateTo, setFilterDateTo] = useState("");
    const [filterWorkflowType, setFilterWorkflowType] = useState("");

    // Debounce workflow type text input (300ms) to avoid excessive API calls while typing
    const [debouncedWorkflowType, setDebouncedWorkflowType] = useState("");
    useEffect(() => {
        const timer = setTimeout(() => setDebouncedWorkflowType(filterWorkflowType), 300);
        return () => clearTimeout(timer);
    }, [filterWorkflowType]);

    const buildFilterJSON = useCallback((currentPage: number): string => {
        const f: DirectoryFilter = {
            page: currentPage,
            page_size: PAGE_SIZE,
        };
        if (filterStatus) f.status = filterStatus;
        if (filterDateFrom) f.date_from = filterDateFrom;
        if (filterDateTo) f.date_to = filterDateTo;
        if (debouncedWorkflowType.trim()) f.workflow_type = debouncedWorkflowType.trim();
        return JSON.stringify(f);
    }, [filterStatus, filterDateFrom, filterDateTo, debouncedWorkflowType]);

    const fetchData = useCallback(async (view: DirectoryView, currentPage: number) => {
        setLoading(true);
        setError(null);
        try {
            const filter = buildFilterJSON(currentPage);
            let resp: DirectoryResponse;
            if (getWorkflowDirectory) {
                resp = await getWorkflowDirectory(view, filter);
            } else {
                const mod = await getWailsAppModule();
                const fn = (mod as Record<string, unknown>)["GetWorkflowDirectory"] as
                    | ((view: string, filter: string) => Promise<DirectoryResponse>)
                    | undefined;
                if (!fn) {
                    throw new Error("GetWorkflowDirectory binding not available");
                }
                resp = await fn(view, filter);
            }
            setItems(resp?.items || []);
            setTotal(resp?.total || 0);
        } catch (err: unknown) {
            const msg = err instanceof Error ? err.message : String(err);
            setError(msg);
            setItems([]);
            setTotal(0);
        } finally {
            setLoading(false);
        }
    }, [getWorkflowDirectory, buildFilterJSON]);

    useEffect(() => {
        fetchData(activeView, page);
    }, [activeView, page, fetchData]);

    const handleTabChange = (view: DirectoryView) => {
        setActiveView(view);
        setPage(1);
    };

    // Reset page to 1 when any filter changes
    const handleFilterStatusChange = (v: string) => { setFilterStatus(v); setPage(1); };
    const handleFilterDateFromChange = (v: string) => { setFilterDateFrom(v); setPage(1); };
    const handleFilterDateToChange = (v: string) => { setFilterDateTo(v); setPage(1); };
    const handleFilterWorkflowTypeChange = (v: string) => { setFilterWorkflowType(v); setPage(1); };

    const handleItemClick = (item: DirectoryItem) => {
        const base = (hubBaseUrl || "").replace(/\/+$/, "");
        if (base) {
            BrowserOpenURL(`${base}/workflow/instances/${item.instance_id}`);
        }
    };

    const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

    return (
        <div style={containerStyle}>
            {/* Tab Bar */}
            <div style={tabBarStyle} role="tablist" aria-label="工作流目录视图">
                {TAB_DEFS.map((tab) => (
                    <button
                        key={tab.key}
                        role="tab"
                        aria-selected={activeView === tab.key}
                        aria-controls={`wf-tabpanel-${tab.key}`}
                        style={activeView === tab.key ? activeTabStyle : tabStyle}
                        onClick={() => handleTabChange(tab.key)}
                    >
                        {tab.label}
                    </button>
                ))}
            </div>

            {/* Filter Bar */}
            <DirectoryFilterBar
                status={filterStatus}
                dateFrom={filterDateFrom}
                dateTo={filterDateTo}
                workflowType={filterWorkflowType}
                onStatusChange={handleFilterStatusChange}
                onDateFromChange={handleFilterDateFromChange}
                onDateToChange={handleFilterDateToChange}
                onWorkflowTypeChange={handleFilterWorkflowTypeChange}
            />

            {/* Tab Panel Content */}
            <div
                id={`wf-tabpanel-${activeView}`}
                role="tabpanel"
                style={panelStyle}
            >
                {loading && <CenteredMessage>加载中...</CenteredMessage>}
                {!loading && error && <CenteredMessage color="var(--theme-danger, #c43d34)">加载失败: {error}</CenteredMessage>}
                {!loading && !error && items.length === 0 && (
                    <CenteredMessage>
                        <span style={{ display: "inline-flex", marginBottom: 8, opacity: 0.55 }}>
                            {/* inline SVG to avoid extra import surface in workflow package */}
                            <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.65" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                                <rect x="6" y="5" width="12" height="16" rx="1.5" />
                                <path d="M9 5V4h6v1" />
                                <path d="M9 10h6M9 13.5h6M9 17h4" />
                            </svg>
                        </span>
                        暂无数据
                    </CenteredMessage>
                )}
                {!loading && !error && items.length > 0 && (
                    <ul style={listStyle} aria-label="工作流列表">
                        {items.map((item) => (
                            <DirectoryListItem
                                key={item.instance_id}
                                item={item}
                                view={activeView}
                                onClick={() => handleItemClick(item)}
                            />
                        ))}
                    </ul>
                )}
            </div>

            {/* Pagination */}
            <div style={paginationStyle}>
                <button
                    onClick={() => setPage((p) => Math.max(1, p - 1))}
                    disabled={page <= 1}
                    style={page <= 1 ? paginationBtnDisabledStyle : paginationBtnStyle}
                    aria-label="上一页"
                >
                    上一页
                </button>
                <span style={pageInfoStyle}>
                    第 {page} 页，共 {total} 条
                </span>
                <button
                    onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
                    disabled={page >= totalPages}
                    style={page >= totalPages ? paginationBtnDisabledStyle : paginationBtnStyle}
                    aria-label="下一页"
                >
                    下一页
                </button>
            </div>
        </div>
    );
}

// --- Sub-components ---

function CenteredMessage({ children, color }: { children: ReactNode; color?: string }) {
    return (
        <div style={{ ...centeredStyle, color: color || "var(--theme-text-muted, #657384)" }}>
            {children}
        </div>
    );
}

function DirectoryListItem({
    item,
    view,
    onClick,
}: {
    item: DirectoryItem;
    view: DirectoryView;
    onClick: () => void;
}) {
    return (
        <li
            style={listItemStyle}
            onClick={onClick}
            onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") onClick(); }}
            tabIndex={0}
            role="button"
            aria-label={`${item.workflow_name} - ${getStatusLabel(item.status)}`}
        >
            {/* Row 1: Name + Status Badge */}
            <div style={itemRow1Style}>
                <span style={itemNameStyle}>{item.workflow_name}</span>
                <StatusBadge status={item.status} />
                {item.result && <ResultBadge result={item.result} />}
                <UrgencyIndicator urgency={item.urgency} timeRemaining={item.time_remaining_hours} />
            </div>

            {/* Row 2: Contextual metadata */}
            <div style={itemRow2Style}>
                {item.initiator_name && <span>发起人: {item.initiator_name}</span>}
                {item.current_node && <span>当前节点: {item.current_node}</span>}
                {view === "pending_confirmation" && item.confirm_type && (
                    <span>确认类型: {item.confirm_type === "executor" ? "执行确认" : "知会确认"}</span>
                )}
                {item.user_role && view === "completed" && (
                    <span>角色: {getRoleLabel(item.user_role)}</span>
                )}
            </div>

            {/* Row 3: Date */}
            <div style={itemRow3Style}>
                <span>{formatDate(item.initiated_at)}</span>
                {item.completed_at && <span style={{ marginLeft: 12 }}>完成: {formatDate(item.completed_at)}</span>}
            </div>
        </li>
    );
}

function StatusBadge({ status }: { status: string }) {
    return (
        <span style={{ ...badgeBaseStyle, ...getStatusBadgeColors(status) }}>
            {getStatusLabel(status)}
        </span>
    );
}

function ResultBadge({ result }: { result: string }) {
    const color = result === "approved" ? "var(--theme-success, #4f7f6f)" : result === "rejected" ? "var(--theme-danger, #c43d34)" : "var(--theme-text-muted, #64748b)";
    const label = result === "approved" ? "通过" : result === "rejected" ? "驳回" : result;
    return <span style={{ fontSize: "0.72rem", fontWeight: 500, color, marginLeft: 4 }}>{label}</span>;
}

function UrgencyIndicator({ urgency, timeRemaining }: { urgency?: string; timeRemaining?: number }) {
    if (!urgency || urgency === "normal") return null;
    if (urgency === "overdue") {
        return <span style={{ color: "var(--theme-danger, #c43d34)", fontWeight: 600, fontSize: "0.72rem", marginLeft: 6 }}>已超时</span>;
    }
    if (urgency === "approaching_timeout") {
        const remaining = timeRemaining != null ? ` (${timeRemaining}h)` : "";
        return <span style={{ color: "var(--theme-warning, #64748b)", fontWeight: 500, fontSize: "0.72rem", marginLeft: 6 }}>即将超时{remaining}</span>;
    }
    return null;
}

// --- Helpers ---

function formatDate(isoStr: string): string {
    if (!isoStr) return "";
    try {
        const d = new Date(isoStr);
        const pad = (n: number) => String(n).padStart(2, "0");
        return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
    } catch {
        return isoStr;
    }
}

function getStatusLabel(status: string): string {
    switch (status) {
        case "running": return "进行中";
        case "completed": return "已完成";
        case "withdrawn": return "已撤回";
        case "cancelled": return "已取消";
        case "failed": return "失败";
        case "blocked": return "阻塞";
        default: return status;
    }
}

function getStatusBadgeColors(status: string): CSSProperties {
    switch (status) {
        case "running": return { background: "var(--theme-info-bg, #eef5fb)", color: "var(--theme-primary, #2f6fbc)" };
        case "completed": return { background: "var(--theme-success-bg, rgba(79, 127, 111, 0.12))", color: "var(--theme-success, #4f7f6f)" };
        case "withdrawn": return { background: "var(--theme-info-bg, #eef5fb)", color: "var(--theme-primary, #2f6fbc)" };
        case "failed": return { background: "var(--theme-danger-bg, #fbf1f0)", color: "var(--theme-danger, #c43d34)" };
        case "blocked": return { background: "var(--theme-warning-bg, #f8fafc)", color: "var(--theme-warning, #64748b)" };
        case "cancelled": return { background: "var(--theme-surface-muted, #f8fafc)", color: "var(--theme-text-muted, #64748b)" };
        default: return { background: "var(--theme-surface-muted, #f8fafc)", color: "var(--theme-text-muted, #64748b)" };
    }
}

function getRoleLabel(role: string): string {
    switch (role) {
        case "initiator": return "发起人";
        case "approver": return "审批人";
        case "executor": return "执行人";
        case "notifier": return "知会人";
        default: return role;
    }
}

// --- Styles ---

const containerStyle: CSSProperties = {
    display: "flex",
    flexDirection: "column",
    height: "100%",
    overflow: "hidden",
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif",
};

const tabBarStyle: CSSProperties = {
    display: "flex",
    gap: 0,
    padding: "0 12px",
    borderBottom: "1px solid var(--theme-border, #d9e1ec)",
    flexShrink: 0,
    background: "var(--theme-surface-muted, #f8fafc)",
};

const tabStyle: CSSProperties = {
    padding: "10px 16px",
    border: "none",
    background: "transparent",
    color: "var(--theme-text-secondary, #44546a)",
    cursor: "pointer",
    fontSize: "0.82rem",
    fontWeight: 500,
    borderBottom: "2px solid transparent",
    transition: "color 0.15s, border-color 0.15s",
    whiteSpace: "nowrap",
};

const activeTabStyle: CSSProperties = {
    ...tabStyle,
    color: "var(--theme-primary, #2f5f98)",
    borderBottomColor: "var(--theme-primary, #2f5f98)",
    fontWeight: 600,
};

const filterBarStyle: CSSProperties = {
    display: "flex",
    flexWrap: "wrap",
    gap: "8px",
    padding: "10px 12px",
    borderBottom: "1px solid var(--theme-border-subtle, #edf2f7)",
    background: "var(--theme-surface-muted, #f8fafc)",
    flexShrink: 0,
    alignItems: "flex-end",
};

const filterLabelStyle: CSSProperties = {
    display: "flex",
    flexDirection: "column",
    gap: "2px",
    minWidth: 0,
};

const filterLabelTextStyle: CSSProperties = {
    fontSize: "0.7rem",
    color: "var(--theme-text-muted, #657384)",
    fontWeight: 500,
};

const filterSelectStyle: CSSProperties = {
    padding: "4px 8px",
    fontSize: "0.78rem",
    border: "1px solid var(--theme-border, #d9e1ec)",
    borderRadius: "4px",
    background: "var(--theme-surface, #ffffff)",
    color: "var(--theme-text-primary, #1c2733)",
    minWidth: "90px",
    outline: "none",
};

const filterInputStyle: CSSProperties = {
    padding: "4px 8px",
    fontSize: "0.78rem",
    border: "1px solid var(--theme-border, #d9e1ec)",
    borderRadius: "4px",
    background: "var(--theme-surface, #ffffff)",
    color: "var(--theme-text-primary, #1c2733)",
    minWidth: "100px",
    outline: "none",
};

const panelStyle: CSSProperties = {
    flex: 1,
    overflow: "auto",
    padding: "8px 0",
};

const centeredStyle: CSSProperties = {
    display: "flex",
    flexDirection: "column",
    alignItems: "center",
    justifyContent: "center",
    padding: "48px 16px",
    fontSize: "0.82rem",
};

const listStyle: CSSProperties = {
    listStyle: "none",
    margin: 0,
    padding: 0,
};

const listItemStyle: CSSProperties = {
    padding: "12px 16px",
    borderBottom: "1px solid var(--theme-border-subtle, #edf2f7)",
    cursor: "pointer",
    transition: "background 0.12s",
};

const itemRow1Style: CSSProperties = {
    display: "flex",
    alignItems: "center",
    gap: "8px",
    marginBottom: "4px",
    flexWrap: "wrap",
};

const itemNameStyle: CSSProperties = {
    fontSize: "0.85rem",
    fontWeight: 600,
    color: "var(--theme-text-primary, #1c2733)",
};

const badgeBaseStyle: CSSProperties = {
    display: "inline-block",
    padding: "2px 8px",
    borderRadius: "10px",
    fontSize: "0.72rem",
    fontWeight: 500,
    lineHeight: "1.4",
};

const itemRow2Style: CSSProperties = {
    display: "flex",
    flexWrap: "wrap",
    gap: "12px",
    fontSize: "0.75rem",
    color: "var(--theme-text-secondary, #44546a)",
    marginBottom: "2px",
};

const itemRow3Style: CSSProperties = {
    fontSize: "0.72rem",
    color: "var(--theme-text-muted, #657384)",
};

const paginationStyle: CSSProperties = {
    display: "flex",
    justifyContent: "center",
    alignItems: "center",
    gap: "12px",
    padding: "10px 12px",
    borderTop: "1px solid var(--theme-border, #d9e1ec)",
    flexShrink: 0,
    background: "var(--theme-surface-muted, #f8fafc)",
};

const paginationBtnStyle: CSSProperties = {
    padding: "5px 12px",
    fontSize: "0.78rem",
    border: "1px solid var(--theme-border, #d9e1ec)",
    borderRadius: "4px",
    background: "var(--theme-surface, #ffffff)",
    color: "var(--theme-text-primary, #1c2733)",
    cursor: "pointer",
};

const paginationBtnDisabledStyle: CSSProperties = {
    ...paginationBtnStyle,
    opacity: 0.4,
    cursor: "not-allowed",
};

const pageInfoStyle: CSSProperties = {
    fontSize: "0.78rem",
    color: "var(--theme-text-secondary, #44546a)",
};

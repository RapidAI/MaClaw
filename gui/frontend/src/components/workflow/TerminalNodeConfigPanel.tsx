import { useCallback, useEffect, useState } from "react";
import type { CSSProperties } from "react";

// --- Types ---

export interface ExecutorConfig {
    user_id: string;
    user_name?: string;
    timeout_hours: number;    // 1-720, default 48
    max_reminders: number;    // 1-10, default 3
    reminder_interval_hours: number; // default 24
}

export interface NotifierConfig {
    user_id: string;
    user_name?: string;
    timeout_hours: number;    // 1-720, default 72
    max_reminders: number;    // 1-10, default 2
    reminder_interval_hours: number; // default 24
}

export interface TerminalNodeConfig {
    result_executors: ExecutorConfig[];
    notifiers: NotifierConfig[];
}

export interface UserSearchResult {
    user_id: string;
    user_name: string;
    email?: string;
    avatar_url?: string;
}

export interface TerminalNodeConfigPanelProps {
    /** Initial config to populate the panel */
    initialConfig?: TerminalNodeConfig;
    /** Called when config changes (debounced) */
    onChange?: (config: TerminalNodeConfig) => void;
    /** Called when user clicks save */
    onSave?: (config: TerminalNodeConfig) => void;
    /** User search function (Hub user directory) */
    searchUsers?: (query: string) => Promise<UserSearchResult[]>;
}

// --- Constants ---

const DEFAULT_EXECUTOR_TIMEOUT = 48;
const DEFAULT_EXECUTOR_REMINDERS = 3;
const DEFAULT_NOTIFIER_TIMEOUT = 72;
const DEFAULT_NOTIFIER_REMINDERS = 2;
const DEFAULT_REMINDER_INTERVAL = 24;

const MIN_TIMEOUT = 1;
const MAX_TIMEOUT = 720;
const MIN_REMINDERS = 1;
const MAX_REMINDERS = 10;

// --- Component ---

export function TerminalNodeConfigPanel({
    initialConfig,
    onChange,
    onSave,
    searchUsers,
}: TerminalNodeConfigPanelProps) {
    const [executors, setExecutors] = useState<ExecutorConfig[]>(
        initialConfig?.result_executors || []
    );
    const [notifiers, setNotifiers] = useState<NotifierConfig[]>(
        initialConfig?.notifiers || []
    );

    // User search state
    const [executorSearchQuery, setExecutorSearchQuery] = useState("");
    const [notifierSearchQuery, setNotifierSearchQuery] = useState("");
    const [executorSearchResults, setExecutorSearchResults] = useState<UserSearchResult[]>([]);
    const [notifierSearchResults, setNotifierSearchResults] = useState<UserSearchResult[]>([]);
    const [executorSearching, setExecutorSearching] = useState(false);
    const [notifierSearching, setNotifierSearching] = useState(false);

    // Notify parent of changes
    useEffect(() => {
        if (onChange) {
            onChange({ result_executors: executors, notifiers });
        }
    }, [executors, notifiers, onChange]);

    // --- Executor search ---
    const handleExecutorSearch = useCallback(async (query: string) => {
        setExecutorSearchQuery(query);
        if (!query.trim() || !searchUsers) {
            setExecutorSearchResults([]);
            return;
        }
        setExecutorSearching(true);
        try {
            const results = await searchUsers(query.trim());
            // Filter out already-added executors
            const existingIds = new Set(executors.map((e) => e.user_id));
            setExecutorSearchResults(results.filter((r) => !existingIds.has(r.user_id)));
        } catch {
            setExecutorSearchResults([]);
        } finally {
            setExecutorSearching(false);
        }
    }, [searchUsers, executors]);

    // Debounce executor search
    useEffect(() => {
        if (!executorSearchQuery.trim()) {
            setExecutorSearchResults([]);
            return;
        }
        const timer = setTimeout(() => handleExecutorSearch(executorSearchQuery), 300);
        return () => clearTimeout(timer);
    }, [executorSearchQuery, handleExecutorSearch]);

    // --- Notifier search ---
    const handleNotifierSearch = useCallback(async (query: string) => {
        setNotifierSearchQuery(query);
        if (!query.trim() || !searchUsers) {
            setNotifierSearchResults([]);
            return;
        }
        setNotifierSearching(true);
        try {
            const results = await searchUsers(query.trim());
            const existingIds = new Set(notifiers.map((n) => n.user_id));
            setNotifierSearchResults(results.filter((r) => !existingIds.has(r.user_id)));
        } catch {
            setNotifierSearchResults([]);
        } finally {
            setNotifierSearching(false);
        }
    }, [searchUsers, notifiers]);

    useEffect(() => {
        if (!notifierSearchQuery.trim()) {
            setNotifierSearchResults([]);
            return;
        }
        const timer = setTimeout(() => handleNotifierSearch(notifierSearchQuery), 300);
        return () => clearTimeout(timer);
    }, [notifierSearchQuery, handleNotifierSearch]);

    // --- Add/Remove ---
    const addExecutor = (user: UserSearchResult) => {
        setExecutors((prev) => [
            ...prev,
            {
                user_id: user.user_id,
                user_name: user.user_name,
                timeout_hours: DEFAULT_EXECUTOR_TIMEOUT,
                max_reminders: DEFAULT_EXECUTOR_REMINDERS,
                reminder_interval_hours: DEFAULT_REMINDER_INTERVAL,
            },
        ]);
        setExecutorSearchQuery("");
        setExecutorSearchResults([]);
    };

    const removeExecutor = (userId: string) => {
        setExecutors((prev) => prev.filter((e) => e.user_id !== userId));
    };

    const addNotifier = (user: UserSearchResult) => {
        setNotifiers((prev) => [
            ...prev,
            {
                user_id: user.user_id,
                user_name: user.user_name,
                timeout_hours: DEFAULT_NOTIFIER_TIMEOUT,
                max_reminders: DEFAULT_NOTIFIER_REMINDERS,
                reminder_interval_hours: DEFAULT_REMINDER_INTERVAL,
            },
        ]);
        setNotifierSearchQuery("");
        setNotifierSearchResults([]);
    };

    const removeNotifier = (userId: string) => {
        setNotifiers((prev) => prev.filter((n) => n.user_id !== userId));
    };

    // --- Update executor/notifier config ---
    const updateExecutor = (userId: string, field: keyof ExecutorConfig, value: number) => {
        setExecutors((prev) =>
            prev.map((e) => (e.user_id === userId ? { ...e, [field]: value } : e))
        );
    };

    const updateNotifier = (userId: string, field: keyof NotifierConfig, value: number) => {
        setNotifiers((prev) =>
            prev.map((n) => (n.user_id === userId ? { ...n, [field]: value } : n))
        );
    };

    // --- Validation ---
    const clampTimeout = (v: number) => Math.max(MIN_TIMEOUT, Math.min(MAX_TIMEOUT, v));
    const clampReminders = (v: number) => Math.max(MIN_REMINDERS, Math.min(MAX_REMINDERS, v));

    const handleSave = () => {
        if (onSave) {
            onSave({ result_executors: executors, notifiers });
        }
    };

    const hasNoExecutor = executors.length === 0;

    return (
        <div style={containerStyle} role="form" aria-label="Terminal Node 配置">
            <h3 style={headingStyle}>Terminal Node 配置</h3>

            {/* Warning: no executor */}
            {hasNoExecutor && (
                <div style={warningStyle} role="alert" aria-live="polite">
                    <span style={warningIconStyle}>⚠️</span>
                    <span>未配置结果执行人。工作流完成后将无人负责执行操作。</span>
                </div>
            )}

            {/* Result Executors Section */}
            <section style={sectionStyle} aria-label="结果执行人配置">
                <h4 style={sectionHeadingStyle}>结果执行人 (Result Executors)</h4>
                <p style={sectionDescStyle}>工作流完成后负责执行操作的人员</p>

                {/* Search input */}
                <div style={searchContainerStyle}>
                    <input
                        type="text"
                        value={executorSearchQuery}
                        onChange={(e) => setExecutorSearchQuery(e.target.value)}
                        placeholder="搜索用户..."
                        style={searchInputStyle}
                        aria-label="搜索执行人"
                        aria-expanded={executorSearchResults.length > 0}
                        aria-controls="executor-search-results"
                        role="combobox"
                        aria-autocomplete="list"
                    />
                    {executorSearching && <span style={searchSpinnerStyle}>⏳</span>}
                    {executorSearchResults.length > 0 && (
                        <ul id="executor-search-results" style={searchResultsStyle} role="listbox">
                            {executorSearchResults.map((user) => (
                                <li
                                    key={user.user_id}
                                    style={searchResultItemStyle}
                                    onClick={() => addExecutor(user)}
                                    onKeyDown={(e) => { if (e.key === "Enter") addExecutor(user); }}
                                    tabIndex={0}
                                    role="option"
                                    aria-label={`添加 ${user.user_name}`}
                                >
                                    <span style={userNameStyle}>{user.user_name}</span>
                                    {user.email && <span style={userEmailStyle}>{user.email}</span>}
                                </li>
                            ))}
                        </ul>
                    )}
                </div>

                {/* Executor list */}
                {executors.length > 0 && (
                    <ul style={userListStyle} aria-label="已添加的执行人">
                        {executors.map((exec) => (
                            <li key={exec.user_id} style={userItemStyle}>
                                <div style={userItemHeaderStyle}>
                                    <span style={userItemNameStyle}>
                                        {exec.user_name || exec.user_id}
                                    </span>
                                    <button
                                        onClick={() => removeExecutor(exec.user_id)}
                                        style={removeButtonStyle}
                                        aria-label={`移除 ${exec.user_name || exec.user_id}`}
                                    >
                                        ✕
                                    </button>
                                </div>
                                <div style={configRowStyle}>
                                    <label style={configLabelStyle}>
                                        <span>超时时间 (小时)</span>
                                        <input
                                            type="number"
                                            min={MIN_TIMEOUT}
                                            max={MAX_TIMEOUT}
                                            value={exec.timeout_hours}
                                            onChange={(e) =>
                                                updateExecutor(exec.user_id, "timeout_hours", clampTimeout(Number(e.target.value) || DEFAULT_EXECUTOR_TIMEOUT))
                                            }
                                            style={configInputStyle}
                                            aria-label="超时时间（小时）"
                                        />
                                    </label>
                                    <label style={configLabelStyle}>
                                        <span>最大提醒次数</span>
                                        <input
                                            type="number"
                                            min={MIN_REMINDERS}
                                            max={MAX_REMINDERS}
                                            value={exec.max_reminders}
                                            onChange={(e) =>
                                                updateExecutor(exec.user_id, "max_reminders", clampReminders(Number(e.target.value) || DEFAULT_EXECUTOR_REMINDERS))
                                            }
                                            style={configInputStyle}
                                            aria-label="最大提醒次数"
                                        />
                                    </label>
                                </div>
                            </li>
                        ))}
                    </ul>
                )}
            </section>

            {/* Notifiers Section */}
            <section style={sectionStyle} aria-label="知会人配置">
                <h4 style={sectionHeadingStyle}>知会人 (Notifiers)</h4>
                <p style={sectionDescStyle}>工作流完成后接收通知的人员</p>

                {/* Search input */}
                <div style={searchContainerStyle}>
                    <input
                        type="text"
                        value={notifierSearchQuery}
                        onChange={(e) => setNotifierSearchQuery(e.target.value)}
                        placeholder="搜索用户..."
                        style={searchInputStyle}
                        aria-label="搜索知会人"
                        aria-expanded={notifierSearchResults.length > 0}
                        aria-controls="notifier-search-results"
                        role="combobox"
                        aria-autocomplete="list"
                    />
                    {notifierSearching && <span style={searchSpinnerStyle}>⏳</span>}
                    {notifierSearchResults.length > 0 && (
                        <ul id="notifier-search-results" style={searchResultsStyle} role="listbox">
                            {notifierSearchResults.map((user) => (
                                <li
                                    key={user.user_id}
                                    style={searchResultItemStyle}
                                    onClick={() => addNotifier(user)}
                                    onKeyDown={(e) => { if (e.key === "Enter") addNotifier(user); }}
                                    tabIndex={0}
                                    role="option"
                                    aria-label={`添加 ${user.user_name}`}
                                >
                                    <span style={userNameStyle}>{user.user_name}</span>
                                    {user.email && <span style={userEmailStyle}>{user.email}</span>}
                                </li>
                            ))}
                        </ul>
                    )}
                </div>

                {/* Notifier list */}
                {notifiers.length > 0 && (
                    <ul style={userListStyle} aria-label="已添加的知会人">
                        {notifiers.map((notif) => (
                            <li key={notif.user_id} style={userItemStyle}>
                                <div style={userItemHeaderStyle}>
                                    <span style={userItemNameStyle}>
                                        {notif.user_name || notif.user_id}
                                    </span>
                                    <button
                                        onClick={() => removeNotifier(notif.user_id)}
                                        style={removeButtonStyle}
                                        aria-label={`移除 ${notif.user_name || notif.user_id}`}
                                    >
                                        ✕
                                    </button>
                                </div>
                                <div style={configRowStyle}>
                                    <label style={configLabelStyle}>
                                        <span>超时时间 (小时)</span>
                                        <input
                                            type="number"
                                            min={MIN_TIMEOUT}
                                            max={MAX_TIMEOUT}
                                            value={notif.timeout_hours}
                                            onChange={(e) =>
                                                updateNotifier(notif.user_id, "timeout_hours", clampTimeout(Number(e.target.value) || DEFAULT_NOTIFIER_TIMEOUT))
                                            }
                                            style={configInputStyle}
                                            aria-label="超时时间（小时）"
                                        />
                                    </label>
                                    <label style={configLabelStyle}>
                                        <span>最大提醒次数</span>
                                        <input
                                            type="number"
                                            min={MIN_REMINDERS}
                                            max={MAX_REMINDERS}
                                            value={notif.max_reminders}
                                            onChange={(e) =>
                                                updateNotifier(notif.user_id, "max_reminders", clampReminders(Number(e.target.value) || DEFAULT_NOTIFIER_REMINDERS))
                                            }
                                            style={configInputStyle}
                                            aria-label="最大提醒次数"
                                        />
                                    </label>
                                </div>
                            </li>
                        ))}
                    </ul>
                )}
            </section>

            {/* Save Button */}
            <div style={footerStyle}>
                <button onClick={handleSave} style={saveButtonStyle} aria-label="保存配置">
                    保存配置
                </button>
            </div>
        </div>
    );
}

// --- Styles ---

const containerStyle: CSSProperties = {
    display: "flex",
    flexDirection: "column",
    gap: "16px",
    padding: "16px",
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif",
    fontSize: "0.85rem",
    maxWidth: "600px",
};

const headingStyle: CSSProperties = {
    margin: 0,
    fontSize: "1rem",
    fontWeight: 600,
    color: "#212121",
};

const warningStyle: CSSProperties = {
    display: "flex",
    alignItems: "center",
    gap: "8px",
    padding: "10px 14px",
    background: "var(--theme-warning-bg, #f8fafc)",
    border: "1px solid color-mix(in srgb, var(--theme-warning, #64748b) 34%, transparent)",
    borderRadius: "6px",
    fontSize: "0.8rem",
    color: "var(--theme-warning, #64748b)",
};

const warningIconStyle: CSSProperties = {
    fontSize: "1rem",
    flexShrink: 0,
};

const sectionStyle: CSSProperties = {
    display: "flex",
    flexDirection: "column",
    gap: "10px",
};

const sectionHeadingStyle: CSSProperties = {
    margin: 0,
    fontSize: "0.88rem",
    fontWeight: 600,
    color: "#333",
};

const sectionDescStyle: CSSProperties = {
    margin: 0,
    fontSize: "0.75rem",
    color: "#888",
};

const searchContainerStyle: CSSProperties = {
    position: "relative",
};

const searchInputStyle: CSSProperties = {
    width: "100%",
    padding: "8px 12px",
    fontSize: "0.82rem",
    border: "1px solid #ddd",
    borderRadius: "6px",
    outline: "none",
    boxSizing: "border-box",
};

const searchSpinnerStyle: CSSProperties = {
    position: "absolute",
    right: "10px",
    top: "8px",
    fontSize: "0.8rem",
};

const searchResultsStyle: CSSProperties = {
    position: "absolute",
    top: "100%",
    left: 0,
    right: 0,
    listStyle: "none",
    margin: 0,
    padding: 0,
    background: "#fff",
    border: "1px solid #ddd",
    borderTop: "none",
    borderRadius: "0 0 6px 6px",
    maxHeight: "180px",
    overflow: "auto",
    zIndex: 100,
    boxShadow: "0 4px 12px rgba(0,0,0,0.1)",
};

const searchResultItemStyle: CSSProperties = {
    padding: "8px 12px",
    cursor: "pointer",
    display: "flex",
    alignItems: "center",
    gap: "8px",
    borderBottom: "1px solid #f5f5f5",
};

const userNameStyle: CSSProperties = {
    fontWeight: 500,
    color: "#333",
};

const userEmailStyle: CSSProperties = {
    fontSize: "0.72rem",
    color: "#999",
};

const userListStyle: CSSProperties = {
    listStyle: "none",
    margin: 0,
    padding: 0,
    display: "flex",
    flexDirection: "column",
    gap: "8px",
};

const userItemStyle: CSSProperties = {
    padding: "10px 12px",
    background: "#f9f9f9",
    borderRadius: "6px",
    border: "1px solid #eee",
};

const userItemHeaderStyle: CSSProperties = {
    display: "flex",
    justifyContent: "space-between",
    alignItems: "center",
    marginBottom: "8px",
};

const userItemNameStyle: CSSProperties = {
    fontWeight: 600,
    color: "#333",
    fontSize: "0.82rem",
};

const removeButtonStyle: CSSProperties = {
    border: "none",
    background: "transparent",
    color: "var(--theme-danger, #b42318)",
    cursor: "pointer",
    fontSize: "0.9rem",
    padding: "2px 6px",
    borderRadius: "4px",
};

const configRowStyle: CSSProperties = {
    display: "flex",
    gap: "16px",
    flexWrap: "wrap",
};

const configLabelStyle: CSSProperties = {
    display: "flex",
    flexDirection: "column",
    gap: "3px",
    fontSize: "0.72rem",
    color: "#666",
};

const configInputStyle: CSSProperties = {
    width: "80px",
    padding: "4px 8px",
    fontSize: "0.8rem",
    border: "1px solid #ddd",
    borderRadius: "4px",
    outline: "none",
};

const footerStyle: CSSProperties = {
    display: "flex",
    justifyContent: "flex-end",
    paddingTop: "8px",
    borderTop: "1px solid #eee",
};

const saveButtonStyle: CSSProperties = {
    padding: "8px 20px",
    fontSize: "0.82rem",
    fontWeight: 500,
    background: "#1565c0",
    color: "#fff",
    border: "none",
    borderRadius: "6px",
    cursor: "pointer",
};

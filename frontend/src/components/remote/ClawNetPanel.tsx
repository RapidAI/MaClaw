import { useState, useEffect, useCallback, useRef } from "react";
import { main } from "../../../wailsjs/go/models";
import {
    ClawNetStopDaemon,
    ClawNetIsRunning,
    ClawNetGetStatus,
    ClawNetGetPeers,
    ClawNetGetCredits,
    ClawNetGetBinaryPath,
    ClawNetEnsureDaemonWithDownload,
    ClawNetHasIdentity,
    ClawNetExportIdentity,
    ClawNetImportIdentity,
    ClawNetOnlineBackupKey,
    ClawNetOnlineRestoreKey,
    ClawNetGetTransactions,
    ClawNetGetCreditsAudit,
    ClawNetGetLeaderboard,
    ClawNetAutoPickerGetStatus,
    ClawNetAutoPickerConfigure,
    ClawNetAutoPickerTriggerNow,
} from "../../../wailsjs/go/main/App";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime/runtime";

type Props = {
    config: main.AppConfig | null;
    saveRemoteConfigField: (patch: Partial<main.AppConfig>) => void;
    lang: string;
    onRunningChange: (running: boolean) => void;
};

export function ClawNetPanel({ config, saveRemoteConfigField, lang, onRunningChange }: Props) {
    const [running, setRunning] = useState(false);
    const [busy, setBusy] = useState(false);
    const [status, setStatus] = useState<any>(null);
    const [peers, setPeers] = useState<any[]>([]);
    const [credits, setCredits] = useState<any>(null);
    const [binPath, setBinPath] = useState("");
    const [error, setError] = useState("");
    const [downloadProgress, setDownloadProgress] = useState<{ stage: string; percent: number; message: string } | null>(null);
    const [identityExists, setIdentityExists] = useState(false);
    const identityFoundRef = useRef(false);
    const [identityPath, setIdentityPath] = useState("");
    const [keyBusy, setKeyBusy] = useState(false);
    const [keyMsg, setKeyMsg] = useState("");
    // Finance
    const [financeTab, setFinanceTab] = useState<"transactions" | "audit" | "leaderboard">("transactions");
    const [transactions, setTransactions] = useState<any[]>([]);
    const [auditLog, setAuditLog] = useState<any[]>([]);
    const [leaderboard, setLeaderboard] = useState<any[]>([]);
    const [financeLoading, setFinanceLoading] = useState(false);
    const [financeOpen, setFinanceOpen] = useState(false);
    // Online backup/restore
    const [onlinePwd, setOnlinePwd] = useState("");
    const [onlinePwd2, setOnlinePwd2] = useState("");
    const [onlineRestorePwd, setOnlineRestorePwd] = useState("");
    const [onlineBusy, setOnlineBusy] = useState(false);
    const [onlineMsg, setOnlineMsg] = useState("");
    // Auto task picker
    const [pickerStatus, setPickerStatus] = useState<any>(null);
    const [pickerBusy, setPickerBusy] = useState(false);

    const zh = lang?.startsWith("zh");
    const enabled = !!config?.clawnet_enabled;

    // Poll running state; also checks identity when not yet detected.
    const refreshStatus = useCallback(async () => {
        try {
            const isUp = await ClawNetIsRunning();
            setRunning(isUp);
            onRunningChange(isUp);
            if (isUp) {
                const [s, p, c] = await Promise.all([
                    ClawNetGetStatus(),
                    ClawNetGetPeers(),
                    ClawNetGetCredits(),
                ]);
                if (s.ok) setStatus(s);
                if (p.ok) setPeers(p.peers || []);
                if (c.ok) setCredits(c);
            } else {
                setStatus(null);
                setPeers([]);
                setCredits(null);
            }
        } catch {
            setRunning(false);
            onRunningChange(false);
        }
        // Only poll identity while it hasn't been found yet.
        if (!identityFoundRef.current) {
            try {
                const res = await ClawNetHasIdentity();
                if (res.ok) {
                    if (res.exists) identityFoundRef.current = true;
                    setIdentityExists(!!res.exists);
                    setIdentityPath(res.path || "");
                }
            } catch {}
        }
    }, [onRunningChange]);

    const refreshIdentity = useCallback(async () => {
        try {
            const res = await ClawNetHasIdentity();
            if (res.ok) {
                if (res.exists) identityFoundRef.current = true;
                setIdentityExists(!!res.exists);
                setIdentityPath(res.path || "");
            }
        } catch {}
    }, []);

    const refreshFinance = useCallback(async (tab: "transactions" | "audit" | "leaderboard") => {
        setFinanceLoading(true);
        try {
            if (tab === "transactions") {
                const res = await ClawNetGetTransactions();
                if (res.ok) setTransactions((res.transactions || []).slice(0, 20));
            } else if (tab === "audit") {
                const res = await ClawNetGetCreditsAudit();
                if (res.ok) setAuditLog((res.audit || []).slice(0, 20));
            } else if (tab === "leaderboard") {
                const res = await ClawNetGetLeaderboard();
                if (res.ok) setLeaderboard((res.leaderboard || []).slice(0, 20));
            }
        } catch {}
        setFinanceLoading(false);
    }, []);

    useEffect(() => {
        ClawNetGetBinaryPath().then(setBinPath).catch(() => {});
        refreshStatus();
        const timer = setInterval(refreshStatus, 15000);
        return () => clearInterval(timer);
    }, [refreshStatus]);

    // Auto-picker status polling & event listener
    const refreshPickerStatus = useCallback(async () => {
        try {
            const res = await ClawNetAutoPickerGetStatus();
            if (res.ok) setPickerStatus(res);
        } catch {}
    }, []);

    useEffect(() => {
        if (running) refreshPickerStatus();
        EventsOn("clawnet:auto-picker-changed", refreshPickerStatus);
        return () => { EventsOff("clawnet:auto-picker-changed"); };
    }, [running, refreshPickerStatus]);

    // Listen for download progress events from backend
    useEffect(() => {
        EventsOn("clawnet-install-progress", (data: any) => {
            if (data && typeof data === "object") {
                setDownloadProgress({ stage: data.stage, percent: data.percent, message: data.message });
                if (data.stage === "done") {
                    setTimeout(() => setDownloadProgress(null), 3000);
                    ClawNetGetBinaryPath().then(setBinPath).catch(() => {});
                }
            }
        });
        return () => { EventsOff("clawnet-install-progress"); };
    }, []);

    // Auto-start daemon when enabled
    useEffect(() => {
        if (enabled && !running && !busy) {
            handleStart();
        }
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [enabled]);

    const handleStart = async () => {
        setBusy(true);
        setError("");
        try {
            const res = await ClawNetEnsureDaemonWithDownload();
            if (!res.ok) {
                setError(res.error || "Failed to start");
            }
            await refreshStatus();
            // Force an identity re-check after daemon start regardless of
            // current state, since the daemon may have just generated the key.
            await refreshIdentity();
        } catch (e) {
            setError(String(e));
        } finally {
            setBusy(false);
        }
    };

    const handleStop = async () => {
        setBusy(true);
        setError("");
        try {
            await ClawNetStopDaemon();
            setRunning(false);
            onRunningChange(false);
            setStatus(null);
            setPeers([]);
            setCredits(null);
            setFinanceOpen(false);
            setTransactions([]);
            setAuditLog([]);
            setLeaderboard([]);
        } catch (e) {
            setError(String(e));
        } finally {
            setBusy(false);
        }
    };

    const handleToggle = (checked: boolean) => {
        saveRemoteConfigField({ clawnet_enabled: checked });
        if (!checked && running) {
            handleStop();
        } else if (checked && !running) {
            handleStart();
        }
    };

    const handleExportKey = async () => {
        setKeyBusy(true);
        setKeyMsg("");
        try {
            const res = await ClawNetExportIdentity();
            if (res.ok) {
                setKeyMsg(zh ? `✅ 已导出到 ${res.path}` : `✅ Exported to ${res.path}`);
            } else if (res.error !== "cancelled") {
                setKeyMsg(`❌ ${res.error}`);
            }
        } catch (e) {
            setKeyMsg(`❌ ${e}`);
        } finally {
            setKeyBusy(false);
            setTimeout(() => setKeyMsg(""), 5000);
        }
    };

    const handleImportKey = async () => {
        const confirmMsg = zh
            ? "⚠️ 恢复身份密钥将替换当前密钥（已有密钥会自动备份为 .bak）。确定继续？"
            : "⚠️ Restoring an identity key will replace the current one (existing key is auto-backed up as .bak). Continue?";
        if (!window.confirm(confirmMsg)) return;
        setKeyBusy(true);
        setKeyMsg("");
        try {
            const res = await ClawNetImportIdentity();
            if (res.ok) {
                await refreshIdentity();
                if (res.restarted) {
                    setKeyMsg(zh ? "✅ 身份密钥已恢复，虾网已重新上线" : "✅ Identity restored, ClawNet is back online");
                    await refreshStatus();
                } else {
                    setKeyMsg(zh ? "✅ 身份密钥已恢复" : "✅ Identity restored");
                }
            } else if (res.error !== "cancelled") {
                setKeyMsg(`❌ ${res.error}`);
            }
        } catch (e) {
            setKeyMsg(`❌ ${e}`);
        } finally {
            setKeyBusy(false);
            setTimeout(() => setKeyMsg(""), 5000);
        }
    };

    const handleOnlineBackup = async () => {
        if (onlinePwd.length < 6) {
            setOnlineMsg(zh ? "❌ 口令至少6位" : "❌ Password must be at least 6 characters");
            return;
        }
        if (onlinePwd !== onlinePwd2) {
            setOnlineMsg(zh ? "❌ 两次口令不一致" : "❌ Passwords do not match");
            return;
        }
        setOnlineBusy(true);
        setOnlineMsg("");
        try {
            const res = await ClawNetOnlineBackupKey(onlinePwd);
            if (res.ok) {
                setOnlineMsg(zh ? "✅ 已加密备份到 Hub" : "✅ Encrypted backup saved to Hub");
                setOnlinePwd("");
                setOnlinePwd2("");
            } else {
                setOnlineMsg(`❌ ${res.error}`);
            }
        } catch (e) {
            setOnlineMsg(`❌ ${e}`);
        } finally {
            setOnlineBusy(false);
            setTimeout(() => setOnlineMsg(""), 6000);
        }
    };

    const handleOnlineRestore = async () => {
        if (!onlineRestorePwd) {
            setOnlineMsg(zh ? "❌ 请输入口令" : "❌ Please enter password");
            return;
        }
        const confirmMsg = zh
            ? "⚠️ 从 Hub 恢复身份密钥将替换当前密钥（已有密钥会自动备份为 .bak）。确定继续？"
            : "⚠️ Restoring identity key from Hub will replace the current one (existing key is auto-backed up as .bak). Continue?";
        if (!window.confirm(confirmMsg)) return;
        setOnlineBusy(true);
        setOnlineMsg("");
        try {
            const res = await ClawNetOnlineRestoreKey(onlineRestorePwd);
            if (res.ok) {
                setOnlineRestorePwd("");
                await refreshIdentity();
                if (res.restarted) {
                    setOnlineMsg(zh ? "✅ 身份密钥已从 Hub 恢复，虾网已重新上线" : "✅ Identity restored from Hub, ClawNet is back online");
                    await refreshStatus();
                } else {
                    setOnlineMsg(zh ? "✅ 身份密钥已从 Hub 恢复" : "✅ Identity restored from Hub");
                }
            } else {
                setOnlineMsg(`❌ ${res.error}`);
            }
        } catch (e) {
            setOnlineMsg(`❌ ${e}`);
        } finally {
            setOnlineBusy(false);
            setTimeout(() => setOnlineMsg(""), 6000);
        }
    };

    return (
        <div style={{ fontSize: "0.85rem" }}>
            <h4 style={{ fontSize: "0.8rem", color: "#6366f1", marginBottom: "10px", marginTop: 0, textTransform: "uppercase", letterSpacing: "0.025em" }}>
                🦞 ClawNet {zh ? "虾网" : "P2P Network"}
            </h4>

            {/* Enable/Disable toggle */}
            <div style={{ display: "flex", alignItems: "center", gap: "10px", marginBottom: "12px" }}>
                <label style={{ display: "flex", alignItems: "center", gap: "8px", cursor: "pointer" }}>
                    <input
                        type="checkbox"
                        checked={enabled}
                        onChange={(e) => handleToggle(e.target.checked)}
                        disabled={busy}
                    />
                    <span>{zh ? "启用虾网" : "Enable ClawNet"}</span>
                </label>
                <span style={{
                    display: "inline-block",
                    width: "8px",
                    height: "8px",
                    borderRadius: "50%",
                    backgroundColor: running ? "#22c55e" : "#94a3b8",
                    flexShrink: 0,
                }} />
                <span style={{ fontSize: "0.78rem", color: running ? "#22c55e" : "#94a3b8" }}>
                    {running ? (zh ? "已连接" : "Connected") : (zh ? "未连接" : "Disconnected")}
                </span>
            </div>

            {error && (
                <div style={{ fontSize: "0.78rem", color: "#ef4444", marginBottom: "8px", padding: "6px 10px", background: "#fef2f2", borderRadius: "6px" }}>
                    {error}
                </div>
            )}

            {/* Download progress */}
            {downloadProgress && downloadProgress.stage !== "done" && (
                <div style={{ marginBottom: "10px", padding: "8px 12px", background: "#eef2ff", borderRadius: "8px", fontSize: "0.78rem" }}>
                    <div style={{ display: "flex", justifyContent: "space-between", marginBottom: "4px" }}>
                        <span>{zh ? "正在下载 ClawNet..." : "Downloading ClawNet..."}</span>
                        <span style={{ fontFamily: "monospace" }}>{downloadProgress.percent}%</span>
                    </div>
                    <div style={{ height: "4px", background: "#c7d2fe", borderRadius: "2px", overflow: "hidden" }}>
                        <div style={{ height: "100%", width: `${downloadProgress.percent}%`, background: "#6366f1", borderRadius: "2px", transition: "width 0.3s ease" }} />
                    </div>
                    <div style={{ fontSize: "0.72rem", color: "#888", marginTop: "3px" }}>{downloadProgress.message}</div>
                </div>
            )}

            {/* Status info */}
            {running && status && (
                <div style={{ background: "#f0fdf4", borderRadius: "8px", padding: "10px 14px", marginBottom: "10px" }}>
                    <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: "6px 16px", fontSize: "0.78rem" }}>
                        <div><span style={{ color: "#888" }}>Peer ID:</span> <span style={{ fontFamily: "monospace", fontSize: "0.72rem" }}>{(status.peer_id || "").slice(0, 16)}…</span></div>
                        <div><span style={{ color: "#888" }}>{zh ? "节点数" : "Peers"}:</span> {status.peers}</div>
                        <div><span style={{ color: "#888" }}>{zh ? "未读私信" : "Unread DM"}:</span> {status.unread_dm || 0}</div>
                        <div><span style={{ color: "#888" }}>{zh ? "版本" : "Version"}:</span> {status.version}</div>
                        {status.uptime && <div><span style={{ color: "#888" }}>{zh ? "运行时间" : "Uptime"}:</span> {status.uptime}</div>}
                    </div>
                </div>
            )}

            {/* Credits */}
            {running && credits && (
                <div style={{ background: "#fffbeb", borderRadius: "8px", padding: "8px 14px", marginBottom: "10px", fontSize: "0.78rem" }}>
                    <span style={{ color: "#888" }}>🐚 Shell:</span>{" "}
                    <span style={{ fontWeight: 600 }}>{credits.balance ?? 0}</span>
                    {credits.local_value && <span style={{ marginLeft: "6px", color: "#a16207", fontSize: "0.72rem" }}>({credits.local_value})</span>}
                    {credits.tier && <span style={{ marginLeft: "10px", color: "#888" }}>{zh ? "等级" : "Tier"}: {credits.tier}</span>}
                </div>
            )}

            {/* Auto Task Picker — maClaw auto-earns credits */}
            {running && (
                <div style={{ marginBottom: "10px", padding: "10px 14px", background: "#fdf4ff", borderRadius: "8px", border: "1px solid #e9d5ff" }}>
                    <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: "8px" }}>
                        <div style={{ fontSize: "0.78rem", fontWeight: 600, color: "#7c3aed" }}>
                            🤖 {zh ? "自动接单" : "Auto Task Pickup"}
                        </div>
                        <label style={{ display: "flex", alignItems: "center", gap: "6px", cursor: "pointer", fontSize: "0.72rem" }}>
                            <input
                                type="checkbox"
                                checked={!!pickerStatus?.enabled}
                                disabled={pickerBusy}
                                onChange={async (e) => {
                                    setPickerBusy(true);
                                    try {
                                        await ClawNetAutoPickerConfigure(e.target.checked, 5, 0, []);
                                        await refreshPickerStatus();
                                    } catch {}
                                    setPickerBusy(false);
                                }}
                            />
                            <span style={{ color: pickerStatus?.enabled ? "#7c3aed" : "#94a3b8" }}>
                                {pickerStatus?.enabled ? (zh ? "已开启" : "Enabled") : (zh ? "已关闭" : "Disabled")}
                            </span>
                        </label>
                    </div>
                    <div style={{ fontSize: "0.72rem", color: "#a78bfa", marginBottom: "6px" }}>
                        {zh
                            ? "开启后，maClaw 会自动从虾网寻找任务、完成并提交，赚取 🐚 Shell"
                            : "When enabled, maClaw auto-discovers tasks from ClawNet, completes them, and earns 🐚 Shell"}
                    </div>
                    {pickerStatus?.enabled && (
                        <div style={{ fontSize: "0.72rem", color: "#6d28d9" }}>
                            <div style={{ display: "flex", gap: "12px", flexWrap: "wrap", marginBottom: "4px" }}>
                                <span>{zh ? "已完成" : "Completed"}: {pickerStatus.completed_count ?? 0}</span>
                                <span>{zh ? "失败" : "Failed"}: {pickerStatus.failed_count ?? 0}</span>
                                <span>{zh ? "累计赚取" : "Earned"}: {pickerStatus.total_earned ?? 0} 🐚</span>
                            </div>
                            {pickerStatus.active_tasks?.length > 0 && (
                                <div style={{ marginTop: "4px", padding: "4px 8px", background: "#f5f3ff", borderRadius: "4px" }}>
                                    {zh ? "正在执行" : "Running"}: {pickerStatus.active_tasks.map((t: any) => t.title).join(", ")}
                                </div>
                            )}
                            {pickerStatus.last_error && (
                                <div style={{ marginTop: "4px", color: "#dc2626", fontSize: "0.68rem" }}>
                                    {pickerStatus.last_error}
                                </div>
                            )}
                            <button
                                onClick={async () => {
                                    setPickerBusy(true);
                                    try { await ClawNetAutoPickerTriggerNow(); } catch {}
                                    setPickerBusy(false);
                                    setTimeout(refreshPickerStatus, 2000);
                                }}
                                disabled={pickerBusy}
                                style={{
                                    marginTop: "6px", background: "none", border: "1px solid #e9d5ff",
                                    borderRadius: "6px", padding: "3px 10px", fontSize: "0.72rem",
                                    cursor: pickerBusy ? "not-allowed" : "pointer", color: "#7c3aed",
                                }}
                            >
                                {zh ? "立即寻找任务" : "Find Task Now"}
                            </button>
                        </div>
                    )}
                </div>
            )}

            {/* Finance Details */}
            {running && credits && (
                <div style={{ marginBottom: "10px", border: "1px solid #e2e8f0", borderRadius: "8px", overflow: "hidden" }}>
                    <div
                        onClick={() => { if (!financeOpen) { setFinanceOpen(true); refreshFinance(financeTab); } else { setFinanceOpen(false); } }}
                        style={{ display: "flex", alignItems: "center", justifyContent: "space-between", padding: "8px 14px", background: "#fefce8", cursor: "pointer", fontSize: "0.78rem", fontWeight: 600, color: "#92400e" }}
                    >
                        <span>💰 {zh ? "财务详情" : "Finance Details"}</span>
                        <span style={{ fontSize: "0.72rem", color: "#a16207" }}>{financeOpen ? "▲" : "▼"}</span>
                    </div>
                    {financeOpen && (
                        <div style={{ padding: "10px 14px", background: "#fffef5" }}>
                            {/* Tabs */}
                            <div style={{ display: "flex", gap: "4px", marginBottom: "10px" }}>
                                {([
                                    { key: "transactions" as const, label: zh ? "交易记录" : "Transactions" },
                                    { key: "audit" as const, label: zh ? "审计日志" : "Audit" },
                                    { key: "leaderboard" as const, label: zh ? "排行榜" : "Leaderboard" },
                                ]).map(t => (
                                    <button
                                        key={t.key}
                                        onClick={() => { setFinanceTab(t.key); refreshFinance(t.key); }}
                                        style={{
                                            border: "none", borderRadius: "6px", padding: "4px 12px", fontSize: "0.72rem", cursor: "pointer",
                                            background: financeTab === t.key ? "#f59e0b" : "#fef3c7",
                                            color: financeTab === t.key ? "#fff" : "#92400e",
                                            fontWeight: financeTab === t.key ? 600 : 400,
                                        }}
                                    >{t.label}</button>
                                ))}
                            </div>

                            {financeLoading && <div style={{ fontSize: "0.72rem", color: "#a16207", padding: "8px 0" }}>{zh ? "加载中..." : "Loading..."}</div>}

                            {/* Transactions */}
                            {!financeLoading && financeTab === "transactions" && (
                                <div style={{ maxHeight: "180px", overflowY: "auto", fontSize: "0.72rem" }}>
                                    {transactions.length === 0 && <div style={{ color: "#a16207", padding: "6px 0" }}>{zh ? "暂无交易记录" : "No transactions yet"}</div>}
                                    {transactions.map((tx: any, i: number) => (
                                        <div key={i} style={{ display: "flex", justifyContent: "space-between", alignItems: "center", padding: "4px 0", borderBottom: "1px solid #fde68a" }}>
                                            <div>
                                                <span style={{ color: "#78716c" }}>{tx.type || tx.description || "—"}</span>
                                                {tx.created_at && <span style={{ marginLeft: "6px", color: "#d4d4d8", fontSize: "0.65rem" }}>{tx.created_at}</span>}
                                            </div>
                                            <span style={{ fontWeight: 600, color: (tx.amount ?? 0) >= 0 ? "#16a34a" : "#dc2626", fontFamily: "monospace" }}>
                                                {(tx.amount ?? 0) >= 0 ? "+" : ""}{tx.amount ?? 0}
                                            </span>
                                        </div>
                                    ))}
                                </div>
                            )}

                            {/* Audit */}
                            {!financeLoading && financeTab === "audit" && (
                                <div style={{ maxHeight: "180px", overflowY: "auto", fontSize: "0.72rem" }}>
                                    {auditLog.length === 0 && <div style={{ color: "#a16207", padding: "6px 0" }}>{zh ? "暂无审计记录" : "No audit entries"}</div>}
                                    {auditLog.map((entry: any, i: number) => (
                                        <div key={i} style={{ padding: "4px 0", borderBottom: "1px solid #fde68a" }}>
                                            <div style={{ display: "flex", justifyContent: "space-between" }}>
                                                <span style={{ color: "#78716c" }}>{entry.action || entry.event || "—"}</span>
                                                <span style={{ fontFamily: "monospace", color: "#92400e" }}>{entry.amount ?? ""}</span>
                                            </div>
                                            {(entry.created_at || entry.timestamp) && (
                                                <div style={{ fontSize: "0.65rem", color: "#d4d4d8" }}>{entry.created_at || entry.timestamp}</div>
                                            )}
                                        </div>
                                    ))}
                                </div>
                            )}

                            {/* Leaderboard */}
                            {!financeLoading && financeTab === "leaderboard" && (
                                <div style={{ maxHeight: "180px", overflowY: "auto", fontSize: "0.72rem" }}>
                                    {leaderboard.length === 0 && <div style={{ color: "#a16207", padding: "6px 0" }}>{zh ? "暂无排行数据" : "No leaderboard data"}</div>}
                                    {leaderboard.map((entry: any, i: number) => (
                                        <div key={i} style={{ display: "flex", alignItems: "center", gap: "8px", padding: "4px 0", borderBottom: "1px solid #fde68a" }}>
                                            <span style={{ width: "20px", textAlign: "center", fontWeight: 600, color: i < 3 ? "#f59e0b" : "#a1a1aa" }}>
                                                {i === 0 ? "🥇" : i === 1 ? "🥈" : i === 2 ? "🥉" : `${i + 1}`}
                                            </span>
                                            <span style={{ flex: 1, fontFamily: "monospace", color: "#78716c" }}>
                                                {(entry.peer_id || entry.name || "").slice(0, 16)}{(entry.peer_id || "").length > 16 ? "…" : ""}
                                            </span>
                                            <span style={{ fontWeight: 600, color: "#92400e", fontFamily: "monospace" }}>{entry.balance ?? entry.score ?? 0}</span>
                                            {entry.tier && <span style={{ fontSize: "0.65rem", color: "#a16207" }}>{entry.tier}</span>}
                                        </div>
                                    ))}
                                </div>
                            )}
                        </div>
                    )}
                </div>
            )}

            {/* Peers list */}
            {running && peers.length > 0 && (
                <div style={{ marginBottom: "10px" }}>
                    <div style={{ fontSize: "0.78rem", color: "#888", marginBottom: "4px" }}>{zh ? "已连接节点" : "Connected Peers"} ({peers.length})</div>
                    <div style={{ maxHeight: "120px", overflowY: "auto", fontSize: "0.72rem", fontFamily: "monospace", background: "#f8fafc", borderRadius: "6px", padding: "6px 10px" }}>
                        {peers.slice(0, 20).map((p: any, i: number) => (
                            <div key={i} style={{ display: "flex", gap: "8px", padding: "2px 0" }}>
                                <span>{(p.peer_id || "").slice(0, 12)}…</span>
                                {p.country && <span style={{ color: "#888" }}>{p.country}</span>}
                                {p.latency && <span style={{ color: "#888" }}>{p.latency}</span>}
                            </div>
                        ))}
                    </div>
                </div>
            )}

            {/* Identity Key Backup / Restore */}
            <div style={{ marginTop: "14px", padding: "10px 14px", background: "#f8fafc", borderRadius: "8px", border: "1px solid #e2e8f0" }}>
                <div style={{ fontSize: "0.78rem", fontWeight: 600, color: "#475569", marginBottom: "8px" }}>
                    🔑 {zh ? "身份密钥" : "Identity Key"}
                </div>
                <div style={{ fontSize: "0.72rem", color: "#94a3b8", marginBottom: "8px" }}>
                    {zh
                        ? "身份密钥是你在虾网上的唯一身份凭证（Ed25519），丢失后无法恢复。请妥善备份。"
                        : "Your identity key (Ed25519) is your unique credential on ClawNet. Back it up — it cannot be recovered if lost."}
                </div>
                <div style={{ display: "flex", alignItems: "center", gap: "6px", flexWrap: "wrap" }}>
                    <span style={{
                        fontSize: "0.68rem",
                        padding: "2px 8px",
                        borderRadius: "10px",
                        background: identityExists ? "#ecfdf5" : "#fef2f2",
                        color: identityExists ? "#059669" : "#dc2626",
                    }}>
                        {identityExists ? (zh ? "已生成" : "Exists") : (zh ? "未生成" : "Not found")}
                    </span>
                    <button
                        onClick={handleExportKey}
                        disabled={keyBusy || !identityExists}
                        style={{
                            background: "none", border: "1px solid #e2e8f0", borderRadius: "6px",
                            padding: "3px 10px", fontSize: "0.72rem", cursor: keyBusy || !identityExists ? "not-allowed" : "pointer",
                            color: keyBusy || !identityExists ? "#cbd5e1" : "#6366f1",
                        }}
                    >
                        {zh ? "备份" : "Export"}
                    </button>
                    <button
                        onClick={handleImportKey}
                        disabled={keyBusy}
                        style={{
                            background: "none", border: "1px solid #e2e8f0", borderRadius: "6px",
                            padding: "3px 10px", fontSize: "0.72rem", cursor: keyBusy ? "not-allowed" : "pointer",
                            color: keyBusy ? "#cbd5e1" : "#b45309",
                        }}
                    >
                        {zh ? "恢复" : "Import"}
                    </button>
                </div>
                {keyMsg && (
                    <div style={{ fontSize: "0.72rem", marginTop: "6px", color: keyMsg.startsWith("✅") ? "#16a34a" : "#ef4444" }}>
                        {keyMsg}
                    </div>
                )}
                {identityPath && (
                    <div style={{ fontSize: "0.65rem", color: "#cbd5e1", marginTop: "4px", fontFamily: "monospace", wordBreak: "break-all" }}>
                        {identityPath}
                    </div>
                )}
            </div>

            {/* Online Key Backup / Restore via Hub */}
            <div style={{ marginTop: "10px", padding: "10px 14px", background: "#fefce8", borderRadius: "8px", border: "1px solid #fde68a" }}>
                <div style={{ fontSize: "0.78rem", fontWeight: 600, color: "#92400e", marginBottom: "8px" }}>
                    ☁️ {zh ? "在线备份 / 恢复" : "Online Backup / Restore"}
                </div>
                <div style={{ fontSize: "0.72rem", color: "#a16207", marginBottom: "8px" }}>
                    {zh
                        ? "将密钥加密后保存到 Hub，与你的邮箱绑定。换设备时可用口令恢复。"
                        : "Encrypt and save your key to Hub, bound to your email. Restore on any device with your password."}
                </div>

                {/* Backup section */}
                <div style={{ marginBottom: "8px" }}>
                    <div style={{ fontSize: "0.72rem", color: "#78716c", marginBottom: "4px", fontWeight: 500 }}>
                        {zh ? "备份（设置口令）" : "Backup (set password)"}
                    </div>
                    <div style={{ display: "flex", gap: "4px", alignItems: "center", flexWrap: "wrap" }}>
                        <input
                            type="password"
                            value={onlinePwd}
                            onChange={(e) => setOnlinePwd(e.target.value)}
                            placeholder={zh ? "口令（≥6位）" : "Password (≥6)"}
                            style={{ width: "100px", border: "1px solid #e2e8f0", borderRadius: "6px", padding: "3px 8px", fontSize: "0.72rem" }}
                        />
                        <input
                            type="password"
                            value={onlinePwd2}
                            onChange={(e) => setOnlinePwd2(e.target.value)}
                            placeholder={zh ? "确认口令" : "Confirm"}
                            style={{ width: "100px", border: "1px solid #e2e8f0", borderRadius: "6px", padding: "3px 8px", fontSize: "0.72rem" }}
                        />
                        <button
                            onClick={handleOnlineBackup}
                            disabled={onlineBusy || !identityExists || !onlinePwd || !onlinePwd2}
                            style={{
                                background: "none", border: "1px solid #fde68a", borderRadius: "6px",
                                padding: "3px 10px", fontSize: "0.72rem",
                                cursor: onlineBusy || !identityExists ? "not-allowed" : "pointer",
                                color: onlineBusy || !identityExists ? "#cbd5e1" : "#92400e",
                            }}
                        >
                            {zh ? "加密备份" : "Backup"}
                        </button>
                    </div>
                </div>

                {/* Restore section */}
                <div>
                    <div style={{ fontSize: "0.72rem", color: "#78716c", marginBottom: "4px", fontWeight: 500 }}>
                        {zh ? "恢复（输入口令）" : "Restore (enter password)"}
                    </div>
                    <div style={{ display: "flex", gap: "4px", alignItems: "center", flexWrap: "wrap" }}>
                        <input
                            type="password"
                            value={onlineRestorePwd}
                            onChange={(e) => setOnlineRestorePwd(e.target.value)}
                            placeholder={zh ? "口令" : "Password"}
                            style={{ width: "100px", border: "1px solid #e2e8f0", borderRadius: "6px", padding: "3px 8px", fontSize: "0.72rem" }}
                        />
                        <button
                            onClick={handleOnlineRestore}
                            disabled={onlineBusy || !onlineRestorePwd}
                            style={{
                                background: "none", border: "1px solid #fde68a", borderRadius: "6px",
                                padding: "3px 10px", fontSize: "0.72rem",
                                cursor: onlineBusy || !onlineRestorePwd ? "not-allowed" : "pointer",
                                color: onlineBusy || !onlineRestorePwd ? "#cbd5e1" : "#b45309",
                            }}
                        >
                            {zh ? "在线恢复" : "Restore"}
                        </button>
                    </div>
                </div>

                {onlineMsg && (
                    <div style={{ fontSize: "0.72rem", marginTop: "6px", color: onlineMsg.startsWith("✅") ? "#16a34a" : "#ef4444" }}>
                        {onlineMsg}
                    </div>
                )}
            </div>

            {/* Binary path info */}
            <div style={{ fontSize: "0.72rem", color: "#aaa", marginTop: "8px" }}>
                {zh ? "二进制路径" : "Binary"}: {binPath || (zh ? "未找到" : "not found")}
            </div>
        </div>
    );
}

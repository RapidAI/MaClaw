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
    ClawNetGetDaemonInfo,
} from "../../../wailsjs/go/main/App";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime/runtime";
import { colors, radius } from "./styles";

type Props = {
    config: main.AppConfig | null;
    saveRemoteConfigField: (patch: Partial<main.AppConfig>) => void;
    lang: string;
    onRunningChange: (running: boolean) => void;
};

/* ── Shared inline styles using design tokens from styles.ts ── */
const card = {
    border: `1px solid ${colors.border}`,
    borderRadius: radius.lg,
    padding: "10px 14px",
    marginBottom: "10px",
    background: colors.surface,
} as const;

const cardMuted = {
    ...card,
    background: colors.bg,
} as const;

const heading = {
    fontSize: "0.78rem",
    fontWeight: 600 as const,
    color: colors.text,
    marginBottom: "8px",
} as const;

const label = {
    fontSize: "0.72rem",
    color: colors.textMuted,
} as const;

const mono = {
    fontSize: "0.72rem",
    color: colors.textSecondary,
    fontFamily: "monospace",
} as const;

const tabStyle = (active: boolean) => ({
    background: active ? colors.primary : colors.bg,
    color: active ? "#fff" : colors.textSecondary,
    border: "none",
    borderRadius: radius.md,
    padding: "4px 12px",
    fontSize: "0.72rem",
    fontWeight: (active ? 600 : 400) as any,
    cursor: "pointer" as const,
});

const actionBtn = (disabled?: boolean) => ({
    background: "transparent",
    color: disabled ? colors.textMuted : colors.primary,
    border: `1px solid ${disabled ? colors.border : colors.primary}`,
    borderRadius: radius.md,
    padding: "3px 10px",
    fontSize: "0.72rem",
    cursor: (disabled ? "not-allowed" : "pointer") as any,
    opacity: disabled ? 0.5 : 1,
});

/* ── Bitcoin-style SVG icon for finance entry ── */
const FinanceIcon = () => (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke={colors.primary} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <circle cx="12" cy="12" r="10" />
        <path d="M9 8h4a2 2 0 0 1 0 4H9V8z" />
        <path d="M9 12h5a2 2 0 0 1 0 4H9v-4z" />
        <line x1="10" y1="6" x2="10" y2="8" />
        <line x1="13" y1="6" x2="13" y2="8" />
        <line x1="10" y1="16" x2="10" y2="18" />
        <line x1="13" y1="16" x2="13" y2="18" />
    </svg>
);

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
    const [financeError, setFinanceError] = useState("");
    // Online backup/restore
    const [onlinePwd, setOnlinePwd] = useState("");
    const [onlinePwd2, setOnlinePwd2] = useState("");
    const [onlineRestorePwd, setOnlineRestorePwd] = useState("");
    const [onlineBusy, setOnlineBusy] = useState(false);
    const [onlineMsg, setOnlineMsg] = useState("");
    // Auto task picker
    const [pickerStatus, setPickerStatus] = useState<any>(null);
    const [pickerBusy, setPickerBusy] = useState(false);
    const [triggerMsg, setTriggerMsg] = useState("");
    // Daemon info
    const [daemonInfo, setDaemonInfo] = useState<any>(null);
    const mountedRef = useRef(true);
    useEffect(() => { return () => { mountedRef.current = false; }; }, []);

    const zh = lang?.startsWith("zh");
    const enabled = !!config?.clawnet_enabled;

    // Poll running state; also checks identity when not yet detected.
    const refreshStatus = useCallback(async () => {
        try {
            const isUp = await ClawNetIsRunning();
            setRunning(isUp);
            onRunningChange(isUp);
            if (isUp) {
                const [s, p, c, d] = await Promise.all([
                    ClawNetGetStatus(),
                    ClawNetGetPeers(),
                    ClawNetGetCredits(),
                    ClawNetGetDaemonInfo(),
                ]);
                if (s.ok) setStatus(s);
                if (p.ok) setPeers(p.peers || []);
                if (c.ok) setCredits(c);
                if (d.ok) setDaemonInfo(d);
            } else {
                setStatus(null);
                setPeers([]);
                setCredits(null);
                // Still fetch daemon info when disconnected — shows last known PID / "process lost".
                try {
                    const d = await ClawNetGetDaemonInfo();
                    if (d.ok) setDaemonInfo(d);
                } catch {}
            }
        } catch {
            setRunning(false);
            onRunningChange(false);
        }
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
        setFinanceError("");
        try {
            if (tab === "transactions") {
                const res = await ClawNetGetTransactions();
                if (res.ok) setTransactions((res.transactions || []).slice(0, 20));
                else setFinanceError(res.error || "Failed to load transactions");
            } else if (tab === "audit") {
                const res = await ClawNetGetCreditsAudit();
                if (res.ok) setAuditLog((res.audit || []).slice(0, 20));
                else setFinanceError(res.error || "Failed to load audit");
            } else if (tab === "leaderboard") {
                const res = await ClawNetGetLeaderboard();
                if (res.ok) setLeaderboard((res.leaderboard || []).slice(0, 20));
                else setFinanceError(res.error || "Failed to load leaderboard");
            }
        } catch (e) {
            setFinanceError(String(e));
        }
        setFinanceLoading(false);
    }, []);

    useEffect(() => {
        ClawNetGetBinaryPath().then(setBinPath).catch(() => {});
        refreshStatus();
        const timer = setInterval(refreshStatus, 8000);
        return () => clearInterval(timer);
    }, [refreshStatus]);

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

    useEffect(() => {
        EventsOn("clawnet-install-progress", (data: any) => {
            if (data && typeof data === "object") {
                setDownloadProgress({ stage: data.stage, percent: data.percent, message: data.message });
                if (data.stage === "done") {
                    setTimeout(() => { if (mountedRef.current) setDownloadProgress(null); }, 3000);
                    ClawNetGetBinaryPath().then(setBinPath).catch(() => {});
                }
            }
        });
        return () => { EventsOff("clawnet-install-progress"); };
    }, []);

    // Auto-start daemon when enabled — but check running state first to avoid
    // racing with the App-level auto-start that may have already launched it.
    useEffect(() => {
        if (!enabled || running || busy) return;
        let cancelled = false;
        ClawNetIsRunning().then(up => {
            if (!cancelled && !up) handleStart();
        }).catch(() => {});
        return () => { cancelled = true; };
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
            setFinanceError("");
            setTransactions([]);
            setAuditLog([]);
            setLeaderboard([]);
            setPickerStatus(null);
            setTriggerMsg("");
            setDaemonInfo(null);
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
            setTimeout(() => { if (mountedRef.current) setKeyMsg(""); }, 5000);
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
            setTimeout(() => { if (mountedRef.current) setKeyMsg(""); }, 5000);
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
            setTimeout(() => { if (mountedRef.current) setOnlineMsg(""); }, 6000);
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
            setTimeout(() => { if (mountedRef.current) setOnlineMsg(""); }, 6000);
        }
    };

    const handleTriggerNow = async () => {
        setPickerBusy(true);
        setTriggerMsg(zh ? "⏳ 正在寻找任务..." : "⏳ Searching for tasks...");
        try {
            const res = await ClawNetAutoPickerTriggerNow();
            if (!mountedRef.current) return;
            if (res.ok) {
                setTriggerMsg(zh ? "✅ 已触发任务搜索，请稍候查看结果" : "✅ Task search triggered, check results shortly");
            } else {
                setTriggerMsg(`❌ ${res.error || "Failed"}`);
            }
        } catch (e) {
            if (!mountedRef.current) return;
            setTriggerMsg(`❌ ${e}`);
        } finally {
            if (!mountedRef.current) return;
            setPickerBusy(false);
            // Poll status a few times to catch the async result
            const poll = (ms: number) => setTimeout(() => { if (mountedRef.current) refreshPickerStatus(); }, ms);
            poll(2000);
            poll(5000);
            poll(10000);
            setTimeout(() => { if (mountedRef.current) setTriggerMsg(""); }, 8000);
        }
    };

    return (
        <div style={{ fontSize: "0.85rem" }}>
            {/* Header */}
            <div style={{ display: "flex", alignItems: "center", gap: "8px", marginBottom: "12px" }}>
                <span style={{ fontSize: "0.8rem", fontWeight: 600, color: colors.text, letterSpacing: "0.01em" }}>
                    🦞 ClawNet {zh ? "虾网" : "P2P Network"}
                </span>
            </div>

            {/* Enable/Disable toggle */}
            <div style={{ ...card, display: "flex", alignItems: "center", justifyContent: "space-between" }}>
                <label style={{ display: "flex", alignItems: "center", gap: "8px", cursor: "pointer", fontSize: "0.78rem", color: colors.textSecondary }}>
                    <input type="checkbox" checked={enabled} onChange={(e) => handleToggle(e.target.checked)} disabled={busy} />
                    <span>{zh ? "启用虾网" : "Enable ClawNet"}</span>
                </label>
                <div style={{ display: "flex", alignItems: "center", gap: "6px" }}>
                    <span style={{
                        display: "inline-block", width: "8px", height: "8px", borderRadius: "50%",
                        backgroundColor: running ? colors.success : colors.primaryLight,
                    }} />
                    <span style={{ fontSize: "0.72rem", color: running ? colors.success : colors.textMuted }}>
                        {running ? (zh ? "已连接" : "Connected") : (zh ? "未连接" : "Disconnected")}
                    </span>
                </div>
            </div>

            {error && (
                <div style={{
                    fontSize: "0.75rem",
                    color: error.includes("[clawnet-not-available]") ? colors.warning : colors.danger,
                    marginBottom: "8px",
                    padding: "8px 12px",
                    background: error.includes("[clawnet-not-available]") ? colors.warningBg : colors.dangerBg,
                    borderRadius: radius.md,
                    border: `1px solid ${colors.border}`,
                    whiteSpace: "pre-wrap",
                    wordBreak: "break-word",
                    lineHeight: 1.6,
                }}>
                    {error.replace("[clawnet-not-available] ", "")}
                </div>
            )}

            {/* Download progress */}
            {downloadProgress && downloadProgress.stage !== "done" && (
                <div style={{ ...card, background: colors.accentBg }}>
                    <div style={{ display: "flex", justifyContent: "space-between", marginBottom: "4px", fontSize: "0.78rem", color: colors.textSecondary }}>
                        <span>{zh ? "正在下载 ClawNet..." : "Downloading ClawNet..."}</span>
                        <span style={{ fontFamily: "monospace" }}>{downloadProgress.percent}%</span>
                    </div>
                    <div style={{ height: "4px", background: colors.border, borderRadius: "2px", overflow: "hidden" }}>
                        <div style={{ height: "100%", width: `${downloadProgress.percent}%`, background: colors.primary, borderRadius: "2px", transition: "width 0.3s ease" }} />
                    </div>
                    <div style={{ fontSize: "0.68rem", color: colors.textMuted, marginTop: "3px" }}>{downloadProgress.message}</div>
                </div>
            )}

            {/* Status info */}
            {running && status && (
                <div style={{ ...card, background: colors.successBg }}>
                    <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: "6px 16px", fontSize: "0.78rem" }}>
                        <div><span style={label}>Peer ID:</span> <span style={{ ...mono, fontSize: "0.68rem" }}>{String(status.peer_id || "").slice(0, 16)}…</span></div>
                        <div><span style={label}>{zh ? "节点数" : "Peers"}:</span> <span style={mono}>{status.peers}</span></div>
                        <div><span style={label}>{zh ? "未读私信" : "Unread DM"}:</span> <span style={mono}>{status.unread_dm || 0}</span></div>
                        <div><span style={label}>{zh ? "版本" : "Version"}:</span> <span style={mono}>{status.version}</span></div>
                        {status.uptime && <div><span style={label}>{zh ? "运行时间" : "Uptime"}:</span> <span style={mono}>{status.uptime}</span></div>}
                    </div>
                </div>
            )}

            {/* Daemon process info — visible even when disconnected so user can see dead state */}
            {enabled && daemonInfo && (
                <div style={{ ...card, background: colors.bg }}>
                    <div style={{ display: "flex", alignItems: "center", gap: "8px", fontSize: "0.72rem", flexWrap: "wrap" }}>
                        <span style={label}>{zh ? "进程" : "Process"}:</span>
                        <span style={mono}>clawnet{daemonInfo.bin_path?.endsWith(".exe") ? ".exe" : ""}</span>
                        {daemonInfo.pid > 0 && (
                            <>
                                <span style={{ color: colors.border }}>|</span>
                                <span style={label}>PID:</span>
                                <span style={mono}>{daemonInfo.pid}</span>
                            </>
                        )}
                        {daemonInfo.version && (
                            <>
                                <span style={{ color: colors.border }}>|</span>
                                <span style={mono}>{daemonInfo.version}</span>
                            </>
                        )}
                        {!running && daemonInfo.pid > 0 && (
                            <span style={{ fontSize: "0.68rem", color: colors.danger }}>
                                ({zh ? "进程已断开" : "process lost"})
                            </span>
                        )}
                    </div>
                </div>
            )}

            {/* Credits */}
            {running && credits && (
                <div style={{ ...card, background: colors.warningBg }}>
                    <span style={{ ...label, marginRight: "4px" }}>🐚 Shell:</span>
                    <span style={{ fontWeight: 600, fontSize: "0.78rem", color: colors.text }}>{credits.balance ?? 0}</span>
                    {credits.local_value && <span style={{ marginLeft: "6px", color: colors.warning, fontSize: "0.68rem" }}>({credits.local_value})</span>}
                    {credits.tier && <span style={{ marginLeft: "10px", ...label }}>{zh ? "等级" : "Tier"}: {credits.tier}</span>}
                </div>
            )}

            {/* Finance entry — always visible, right after credits for discoverability */}
            <div style={{ ...card, padding: 0, overflow: "hidden" }}>
                <div
                    onClick={() => {
                        if (!financeOpen) {
                            setFinanceOpen(true);
                            if (running) refreshFinance(financeTab);
                        } else {
                            setFinanceOpen(false);
                        }
                    }}
                    style={{
                        display: "flex", alignItems: "center", gap: "8px", padding: "9px 14px",
                        cursor: "pointer", userSelect: "none",
                    }}
                >
                    <FinanceIcon />
                    <span style={{ fontSize: "0.78rem", fontWeight: 500, color: colors.text, flex: 1 }}>
                        {zh ? "财务信息" : "Finance Details"}
                    </span>
                    {credits && (
                        <span style={{ fontSize: "0.68rem", color: colors.textSecondary, marginRight: "6px" }}>
                            🐚 {credits.balance ?? 0}
                        </span>
                    )}
                    <span style={{ fontSize: "0.68rem", color: colors.textMuted }}>{financeOpen ? "▲" : "▼"}</span>
                </div>
                {financeOpen && !running && (
                    <div style={{ padding: "8px 14px", borderTop: `1px solid ${colors.border}`, fontSize: "0.72rem", color: colors.textMuted }}>
                        {zh ? "虾网未连接，连接后可查看财务数据" : "ClawNet not connected. Connect to view finance data."}
                    </div>
                )}
                {financeOpen && running && (
                    <div style={{ padding: "0 14px 10px 14px", borderTop: `1px solid ${colors.border}` }}>
                        <div style={{ display: "flex", gap: "4px", margin: "10px 0" }}>
                            {([
                                { key: "transactions" as const, lbl: zh ? "交易记录" : "Transactions" },
                                { key: "audit" as const, lbl: zh ? "审计日志" : "Audit" },
                                { key: "leaderboard" as const, lbl: zh ? "排行榜" : "Leaderboard" },
                            ]).map(t => (
                                <button key={t.key} onClick={() => { setFinanceTab(t.key); refreshFinance(t.key); }} style={tabStyle(financeTab === t.key)}>
                                    {t.lbl}
                                </button>
                            ))}
                        </div>
                        {financeLoading && <div style={{ ...label, padding: "8px 0" }}>{zh ? "加载中..." : "Loading..."}</div>}
                        {!financeLoading && financeError && (
                            <div style={{ padding: "6px 0", fontSize: "0.72rem", color: colors.danger }}>
                                {financeError}
                            </div>
                        )}
                        {!financeLoading && !financeError && financeTab === "transactions" && (
                            <div style={{ maxHeight: "180px", overflowY: "auto", fontSize: "0.72rem" }}>
                                {transactions.length === 0 && <div style={{ ...label, padding: "6px 0" }}>{zh ? "暂无交易记录" : "No transactions yet"}</div>}
                                {transactions.map((tx: any, i: number) => (
                                    <div key={i} style={{ display: "flex", justifyContent: "space-between", alignItems: "center", padding: "4px 0", borderBottom: `1px solid ${colors.border}` }}>
                                        <div>
                                            <span style={{ color: colors.textSecondary }}>{tx.type || tx.description || "—"}</span>
                                            {tx.created_at && <span style={{ marginLeft: "6px", color: colors.textMuted, fontSize: "0.65rem" }}>{tx.created_at}</span>}
                                        </div>
                                        <span style={{ fontWeight: 600, color: (tx.amount ?? 0) >= 0 ? colors.success : colors.danger, fontFamily: "monospace" }}>
                                            {(tx.amount ?? 0) >= 0 ? "+" : ""}{tx.amount ?? 0}
                                        </span>
                                    </div>
                                ))}
                            </div>
                        )}
                        {!financeLoading && !financeError && financeTab === "audit" && (
                            <div style={{ maxHeight: "180px", overflowY: "auto", fontSize: "0.72rem" }}>
                                {auditLog.length === 0 && <div style={{ ...label, padding: "6px 0" }}>{zh ? "暂无审计记录" : "No audit entries"}</div>}
                                {auditLog.map((entry: any, i: number) => (
                                    <div key={i} style={{ padding: "4px 0", borderBottom: `1px solid ${colors.border}` }}>
                                        <div style={{ display: "flex", justifyContent: "space-between" }}>
                                            <span style={{ color: colors.textSecondary }}>{entry.action || entry.event || "—"}</span>
                                            <span style={{ ...mono, color: colors.warning }}>{entry.amount ?? ""}</span>
                                        </div>
                                        {(entry.created_at || entry.timestamp) && (
                                            <div style={{ fontSize: "0.65rem", color: colors.textMuted }}>{entry.created_at || entry.timestamp}</div>
                                        )}
                                    </div>
                                ))}
                            </div>
                        )}
                        {!financeLoading && !financeError && financeTab === "leaderboard" && (
                            <div style={{ maxHeight: "180px", overflowY: "auto", fontSize: "0.72rem" }}>
                                {leaderboard.length === 0 && <div style={{ ...label, padding: "6px 0" }}>{zh ? "暂无排行数据" : "No leaderboard data"}</div>}
                                {leaderboard.map((entry: any, i: number) => {
                                    if (!entry || typeof entry !== "object") return null;
                                    const peerId = String(entry.peer_id || entry.name || "");
                                    const displayName = peerId.slice(0, 16) + (peerId.length > 16 ? "…" : "");
                                    const score = entry.balance ?? entry.score ?? 0;
                                    const tier = entry.tier ? String(entry.tier) : "";
                                    return (
                                        <div key={i} style={{ display: "flex", alignItems: "center", gap: "8px", padding: "4px 0", borderBottom: `1px solid ${colors.border}` }}>
                                            <span style={{ width: "20px", textAlign: "center", fontWeight: 600, color: i < 3 ? colors.warning : colors.textMuted }}>
                                                {i === 0 ? "🥇" : i === 1 ? "🥈" : i === 2 ? "🥉" : `${i + 1}`}
                                            </span>
                                            <span style={{ flex: 1, ...mono }}>
                                                {displayName}
                                            </span>
                                            <span style={{ fontWeight: 600, color: colors.warning, fontFamily: "monospace" }}>{score}</span>
                                            {tier && <span style={{ fontSize: "0.65rem", color: colors.textMuted }}>{tier}</span>}
                                        </div>
                                    );
                                })}
                            </div>
                        )}
                    </div>
                )}
            </div>

            {/* Auto Task Picker */}
            {running && (
                <div style={card}>
                    <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: "8px" }}>
                        <div style={{ ...heading, marginBottom: 0, color: colors.primary }}>
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
                                        if (mountedRef.current) await refreshPickerStatus();
                                    } catch {}
                                    if (mountedRef.current) setPickerBusy(false);
                                }}
                            />
                            <span style={{ color: pickerStatus?.enabled ? colors.primary : colors.textMuted }}>
                                {pickerStatus?.enabled ? (zh ? "已开启" : "Enabled") : (zh ? "已关闭" : "Disabled")}
                            </span>
                        </label>
                    </div>
                    <div style={{ fontSize: "0.72rem", color: colors.textMuted, marginBottom: "6px" }}>
                        {zh
                            ? "开启后，maClaw 会自动从虾网寻找任务、完成并提交，赚取 🐚 Shell"
                            : "When enabled, maClaw auto-discovers tasks from ClawNet, completes them, and earns 🐚 Shell"}
                    </div>
                    {pickerStatus?.enabled && (
                        <div style={{ fontSize: "0.72rem", color: colors.textSecondary }}>
                            <div style={{ display: "flex", gap: "12px", flexWrap: "wrap", marginBottom: "6px" }}>
                                <span>{zh ? "已完成" : "Completed"}: {pickerStatus.completed_count ?? 0}</span>
                                <span>{zh ? "失败" : "Failed"}: {pickerStatus.failed_count ?? 0}</span>
                                <span>{zh ? "累计赚取" : "Earned"}: {pickerStatus.total_earned ?? 0} 🐚</span>
                            </div>
                            {pickerStatus.active_tasks?.length > 0 && (
                                <div style={{ marginTop: "4px", padding: "4px 8px", background: colors.accentBg, borderRadius: radius.sm, fontSize: "0.68rem" }}>
                                    {zh ? "正在执行" : "Running"}: {pickerStatus.active_tasks.map((t: any) => t.title).join(", ")}
                                </div>
                            )}
                            {pickerStatus.last_error && (
                                <div style={{ marginTop: "4px", color: colors.danger, fontSize: "0.68rem" }}>
                                    {pickerStatus.last_error}
                                </div>
                            )}
                            <button
                                onClick={handleTriggerNow}
                                disabled={pickerBusy}
                                style={{ ...actionBtn(pickerBusy), marginTop: "6px" }}
                            >
                                {pickerBusy
                                    ? (zh ? "搜索中..." : "Searching...")
                                    : (zh ? "立即寻找任务" : "Find Task Now")}
                            </button>
                            {triggerMsg && (
                                <div style={{ fontSize: "0.72rem", marginTop: "4px", color: triggerMsg.startsWith("✅") ? colors.success : triggerMsg.startsWith("⏳") ? colors.primary : colors.danger }}>
                                    {triggerMsg}
                                </div>
                            )}
                        </div>
                    )}
                </div>
            )}

            {/* Peers list */}
            {running && peers.length > 0 && (
                <div style={{ marginBottom: "10px" }}>
                    <div style={{ ...label, marginBottom: "4px" }}>{zh ? "已连接节点" : "Connected Peers"} ({peers.length})</div>
                    <div style={{ maxHeight: "120px", overflowY: "auto", fontSize: "0.72rem", fontFamily: "monospace", background: colors.bg, borderRadius: radius.md, padding: "6px 10px", border: `1px solid ${colors.border}` }}>
                        {peers.slice(0, 20).map((p: any, i: number) => (
                            <div key={i} style={{ display: "flex", gap: "8px", padding: "2px 0" }}>
                                <span style={{ color: colors.textSecondary }}>{String(p.peer_id || "").slice(0, 12)}…</span>
                                {p.country && <span style={{ color: colors.textMuted }}>{p.country}</span>}
                                {p.latency && <span style={{ color: colors.textMuted }}>{p.latency}</span>}
                            </div>
                        ))}
                    </div>
                </div>
            )}

            {/* Identity Key Backup / Restore */}
            <div style={cardMuted}>
                <div style={heading}>🔑 {zh ? "身份密钥" : "Identity Key"}</div>
                <div style={{ fontSize: "0.72rem", color: colors.textMuted, marginBottom: "8px" }}>
                    {zh
                        ? "身份密钥是你在虾网上的唯一身份凭证（Ed25519），丢失后无法恢复。请妥善备份。"
                        : "Your identity key (Ed25519) is your unique credential on ClawNet. Back it up — it cannot be recovered if lost."}
                </div>
                <div style={{ display: "flex", alignItems: "center", gap: "6px", flexWrap: "wrap" }}>
                    <span style={{
                        fontSize: "0.68rem", padding: "2px 8px", borderRadius: radius.pill,
                        background: identityExists ? colors.successBg : colors.dangerBg,
                        color: identityExists ? colors.success : colors.danger,
                        border: `1px solid ${identityExists ? colors.success : colors.danger}20`,
                    }}>
                        {identityExists ? (zh ? "已生成" : "Exists") : (zh ? "未生成" : "Not found")}
                    </span>
                    <button onClick={handleExportKey} disabled={keyBusy || !identityExists} style={actionBtn(keyBusy || !identityExists)}>
                        {zh ? "备份" : "Export"}
                    </button>
                    <button onClick={handleImportKey} disabled={keyBusy} style={actionBtn(keyBusy)}>
                        {zh ? "恢复" : "Import"}
                    </button>
                </div>
                {keyMsg && (
                    <div style={{ fontSize: "0.72rem", marginTop: "6px", color: keyMsg.startsWith("✅") ? colors.success : colors.danger }}>
                        {keyMsg}
                    </div>
                )}
                {identityPath && (
                    <div style={{ fontSize: "0.65rem", color: colors.textMuted, marginTop: "4px", fontFamily: "monospace", wordBreak: "break-all" }}>
                        {identityPath}
                    </div>
                )}
            </div>

            {/* Online Key Backup / Restore via Hub */}
            <div style={cardMuted}>
                <div style={heading}>☁️ {zh ? "在线备份 / 恢复" : "Online Backup / Restore"}</div>
                <div style={{ fontSize: "0.72rem", color: colors.textMuted, marginBottom: "8px" }}>
                    {zh
                        ? "将密钥加密后保存到 Hub，与你的邮箱绑定。换设备时可用口令恢复。"
                        : "Encrypt and save your key to Hub, bound to your email. Restore on any device with your password."}
                </div>

                {/* Backup section */}
                <div style={{ marginBottom: "8px" }}>
                    <div style={{ fontSize: "0.72rem", color: colors.textSecondary, marginBottom: "4px", fontWeight: 500 }}>
                        {zh ? "备份（设置口令）" : "Backup (set password)"}
                    </div>
                    <div style={{ display: "flex", gap: "4px", alignItems: "center", flexWrap: "wrap" }}>
                        <input type="password" value={onlinePwd} onChange={(e) => setOnlinePwd(e.target.value)}
                            placeholder={zh ? "口令（≥6位）" : "Password (≥6)"}
                            style={{ width: "100px", border: `1px solid ${colors.border}`, borderRadius: radius.md, padding: "3px 8px", fontSize: "0.72rem" }} />
                        <input type="password" value={onlinePwd2} onChange={(e) => setOnlinePwd2(e.target.value)}
                            placeholder={zh ? "确认口令" : "Confirm"}
                            style={{ width: "100px", border: `1px solid ${colors.border}`, borderRadius: radius.md, padding: "3px 8px", fontSize: "0.72rem" }} />
                        <button onClick={handleOnlineBackup} disabled={onlineBusy || !identityExists || !onlinePwd || !onlinePwd2}
                            style={actionBtn(onlineBusy || !identityExists || !onlinePwd || !onlinePwd2)}>
                            {zh ? "加密备份" : "Backup"}
                        </button>
                    </div>
                </div>

                {/* Restore section */}
                <div>
                    <div style={{ fontSize: "0.72rem", color: colors.textSecondary, marginBottom: "4px", fontWeight: 500 }}>
                        {zh ? "恢复（输入口令）" : "Restore (enter password)"}
                    </div>
                    <div style={{ display: "flex", gap: "4px", alignItems: "center", flexWrap: "wrap" }}>
                        <input type="password" value={onlineRestorePwd} onChange={(e) => setOnlineRestorePwd(e.target.value)}
                            placeholder={zh ? "口令" : "Password"}
                            style={{ width: "100px", border: `1px solid ${colors.border}`, borderRadius: radius.md, padding: "3px 8px", fontSize: "0.72rem" }} />
                        <button onClick={handleOnlineRestore} disabled={onlineBusy || !onlineRestorePwd}
                            style={actionBtn(onlineBusy || !onlineRestorePwd)}>
                            {zh ? "在线恢复" : "Restore"}
                        </button>
                    </div>
                </div>

                {onlineMsg && (
                    <div style={{ fontSize: "0.72rem", marginTop: "6px", color: onlineMsg.startsWith("✅") ? colors.success : colors.danger }}>
                        {onlineMsg}
                    </div>
                )}
            </div>

            {/* Binary path info */}
            <div style={{ fontSize: "0.68rem", color: colors.textMuted, marginTop: "4px" }}>
                {zh ? "二进制路径" : "Binary"}: {binPath || (zh ? "未找到" : "not found")}
            </div>
        </div>
    );
}

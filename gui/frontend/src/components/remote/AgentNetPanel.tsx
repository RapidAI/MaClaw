import { useState, useEffect, useCallback, useRef } from "react";
import { main } from "../../../wailsjs/go/models";
import {
    AgentNetStopDaemon,
    AgentNetIsRunning,
    AgentNetGetStatus,
    AgentNetGetPeers,
    AgentNetGetCredits,
    AgentNetGetBinaryPath,
    AgentNetEnsureDaemonWithDownload,
    AgentNetHasIdentity,
    AgentNetExportIdentity,
    AgentNetImportIdentity,
    AgentNetOnlineBackupKey,
    AgentNetOnlineRestoreKey,
    AgentNetGetLeaderboard,
    AgentNetAutoPickerGetStatus,
    AgentNetAutoPickerConfigure,
    AgentNetAutoPickerTriggerNow,
    AgentNetGetDaemonInfo,
    AgentNetManualUpdate,
} from "../../../wailsjs/go/main/App";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime/runtime";
import { colors, radius } from "./styles";
import { useDialog } from "../CustomDialog";

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
    background: active ? colors.primaryLight : colors.bg,
    color: active ? colors.primaryDark : colors.textSecondary,
    border: `1px solid ${active ? colors.primary : colors.border}`,
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

export function AgentNetPanel({ config, saveRemoteConfigField, lang, onRunningChange }: Props) {
    const { showConfirm } = useDialog();
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
    // Manual update
    const [updating, setUpdating] = useState(false);
    const [updateMsg, setUpdateMsg] = useState("");
    const mountedRef = useRef(true);
    useEffect(() => { return () => { mountedRef.current = false; }; }, []);

    const isZh = lang === 'zh-Hans' || lang === 'zh-Hant';
    const t = useCallback((en: string, zhHans: string, zhHant: string = zhHans) =>
        lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en, [lang]);
    const enabled = !!config?.agentnet_enabled;

    // Poll running state; also checks identity when not yet detected.
    const refreshStatus = useCallback(async () => {
        try {
            const isUp = await AgentNetIsRunning();
            setRunning(isUp);
            onRunningChange(isUp);
            if (isUp) {
                const [s, p, c, d] = await Promise.all([
                    AgentNetGetStatus(),
                    AgentNetGetPeers(),
                    AgentNetGetCredits(),
                    AgentNetGetDaemonInfo(),
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
                    const d = await AgentNetGetDaemonInfo();
                    if (d.ok) setDaemonInfo(d);
                } catch {}
            }
        } catch {
            setRunning(false);
            onRunningChange(false);
        }
        if (!identityFoundRef.current) {
            try {
                const res = await AgentNetHasIdentity();
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
            const res = await AgentNetHasIdentity();
            if (res.ok) {
                if (res.exists) identityFoundRef.current = true;
                setIdentityExists(!!res.exists);
                setIdentityPath(res.path || "");
            }
        } catch {}
    }, []);

    const refreshFinance = useCallback(async () => {
        setFinanceLoading(true);
        setFinanceError("");
        try {
            const res = await AgentNetGetLeaderboard();
            if (res.ok) setLeaderboard((res.leaderboard || []).slice(0, 20));
            else setFinanceError(res.error || "Failed to load leaderboard");
        } catch (e) {
            setFinanceError(String(e));
        }
        setFinanceLoading(false);
    }, []);

    useEffect(() => {
        AgentNetGetBinaryPath().then(setBinPath).catch(() => {});
        refreshStatus();
        const timer = setInterval(refreshStatus, 8000);
        return () => clearInterval(timer);
    }, [refreshStatus]);

    const refreshPickerStatus = useCallback(async () => {
        try {
            const res = await AgentNetAutoPickerGetStatus();
            if (res.ok) setPickerStatus(res);
        } catch {}
    }, []);

    useEffect(() => {
        if (running) refreshPickerStatus();
        EventsOn("agentnet:auto-picker-changed", refreshPickerStatus);
        return () => { EventsOff("agentnet:auto-picker-changed"); };
    }, [running, refreshPickerStatus]);

    useEffect(() => {
        EventsOn("agentnet-install-progress", (data: any) => {
            if (data && typeof data === "object") {
                setDownloadProgress({ stage: data.stage, percent: data.percent, message: data.message });
                if (data.stage === "done") {
                    setTimeout(() => { if (mountedRef.current) setDownloadProgress(null); }, 3000);
                    AgentNetGetBinaryPath().then(setBinPath).catch(() => {});
                }
            }
        });
        return () => { EventsOff("agentnet-install-progress"); };
    }, []);

    // Auto-start daemon when enabled — but check running state first to avoid
    // racing with the App-level auto-start that may have already launched it.
    useEffect(() => {
        if (!enabled || running || busy) return;
        let cancelled = false;
        AgentNetIsRunning().then(up => {
            if (!cancelled && !up) handleStart();
        }).catch(() => {});
        return () => { cancelled = true; };
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [enabled]);

    const handleStart = async () => {
        setBusy(true);
        setError("");
        try {
            const res = await AgentNetEnsureDaemonWithDownload();
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
            await AgentNetStopDaemon();
            setRunning(false);
            onRunningChange(false);
            setStatus(null);
            setPeers([]);
            setCredits(null);
            setFinanceOpen(false);
            setFinanceError("");
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

    const handleToggle = async (checked: boolean) => {
        setBusy(true);
        setError("");
        try {
            await saveRemoteConfigField({ agentnet_enabled: checked });
        } catch (e) {
            // Rollback on failure — keep checkbox in previous state.
            setError(checked
                ? t("Failed to enable AgentNet. Please try again.", "启用智网失败，请重试。")
                : t("Failed to disable AgentNet. Please try again.", "关闭智网失败，请重试。"));
            // saveRemoteConfigField already handles rollback of the local config ref.
            setBusy(false);
            return;
        }
        if (!checked && running) {
            handleStop();
        } else if (checked && !running) {
            handleStart();
        }
        setBusy(false);
    };

    const handleManualUpdate = async () => {
        setUpdating(true);
        setUpdateMsg(t("⏳ Checking for updates...", "⏳ 正在检查更新..."));
        try {
            const res = await AgentNetManualUpdate();
            if (!mountedRef.current) return;
            if (res.ok) {
                setUpdateMsg(t("✅ Updated and restarted", "✅ 更新完成，服务已重启"));
                await refreshStatus();
            } else if (res.updated) {
                // Updated but restart failed
                setUpdateMsg(t(`⚠️ Updated, but restart failed: ${res.error}`, `⚠️ 已更新，但重启失败: ${res.error}`));
            } else {
                setUpdateMsg(`❌ ${res.error || (t("Update failed", "更新失败"))}`);
            }
        } catch (e) {
            if (!mountedRef.current) return;
            setUpdateMsg(`❌ ${e}`);
        } finally {
            if (mountedRef.current) setUpdating(false);
            setTimeout(() => { if (mountedRef.current) setUpdateMsg(""); }, 6000);
        }
    };

    const handleExportKey = async () => {
        setKeyBusy(true);
        setKeyMsg("");
        try {
            const res = await AgentNetExportIdentity();
            if (res.ok) {
                setKeyMsg(t(`✅ Exported to ${res.path}`, `✅ 已导出到 ${res.path}`));
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
        const confirmMsg = t(
            "⚠️ Restoring an identity key will replace the current one (existing key is auto-backed up as .bak). Continue?",
            "⚠️ 恢复身份密钥将替换当前密钥（已有密钥会自动备份为 .bak）。确定继续？"
        );
        if (!await showConfirm(confirmMsg)) return;
        setKeyBusy(true);
        setKeyMsg("");
        try {
            const res = await AgentNetImportIdentity();
            if (res.ok) {
                await refreshIdentity();
                if (res.restarted) {
                    setKeyMsg(t("✅ Identity restored, AgentNet is back online", "✅ 身份密钥已恢复，智网已重新上线"));
                    await refreshStatus();
                } else {
                    setKeyMsg(t("✅ Identity restored", "✅ 身份密钥已恢复"));
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
            setOnlineMsg(t("❌ Password must be at least 6 characters", "❌ 口令至少6位"));
            return;
        }
        if (onlinePwd !== onlinePwd2) {
            setOnlineMsg(t("❌ Passwords do not match", "❌ 两次口令不一致"));
            return;
        }
        setOnlineBusy(true);
        setOnlineMsg("");
        try {
            const res = await AgentNetOnlineBackupKey(onlinePwd);
            if (res.ok) {
                setOnlineMsg(t("✅ Encrypted backup saved to Hub", "✅ 已加密备份到 Hub"));
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
            setOnlineMsg(t("❌ Please enter password", "❌ 请输入口令"));
            return;
        }
        const confirmMsg = t(
            "⚠️ Restoring identity key from Hub will replace the current one (existing key is auto-backed up as .bak). Continue?",
            "⚠️ 从 Hub 恢复身份密钥将替换当前密钥（已有密钥会自动备份为 .bak）。确定继续？"
        );
        if (!await showConfirm(confirmMsg)) return;
        setOnlineBusy(true);
        setOnlineMsg("");
        try {
            const res = await AgentNetOnlineRestoreKey(onlineRestorePwd);
            if (res.ok) {
                setOnlineRestorePwd("");
                await refreshIdentity();
                if (res.restarted) {
                    setOnlineMsg(t("✅ Identity restored from Hub, AgentNet is back online", "✅ 身份密钥已从 Hub 恢复，智网已重新上线"));
                    await refreshStatus();
                } else {
                    setOnlineMsg(t("✅ Identity restored from Hub", "✅ 身份密钥已从 Hub 恢复"));
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
        setTriggerMsg(t("⏳ Searching for tasks...", "⏳ 正在寻找任务..."));
        try {
            const res = await AgentNetAutoPickerTriggerNow();
            if (!mountedRef.current) return;
            if (res.ok) {
                setTriggerMsg(t("✅ Task search triggered, check results shortly", "✅ 已触发任务搜索，请稍候查看结果"));
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
                    🦞 AgentNet {t("P2P Network", "智网")}
                </span>
            </div>

            {/* Enable/Disable toggle */}
            <div style={{ ...card, display: "flex", alignItems: "center", justifyContent: "space-between" }}>
                <div style={{ display: "flex", alignItems: "center", gap: "10px" }}>
                    <label style={{ display: "flex", alignItems: "center", gap: "8px", cursor: "pointer", fontSize: "0.78rem", color: colors.textSecondary }}>
                        <input type="checkbox" checked={enabled} onChange={(e) => handleToggle(e.target.checked)} disabled={busy} />
                        <span>{t("Enable AgentNet", "启用智网")}</span>
                    </label>
                    <button
                        onClick={handleManualUpdate}
                        disabled={updating || busy}
                        style={actionBtn(updating || busy)}
                    >
                        {updating ? (t("Updating...", "更新中...")) : (t("Update", "手动更新"))}
                    </button>
                </div>
                <div style={{ display: "flex", alignItems: "center", gap: "6px" }}>
                    <span style={{
                        display: "inline-block", width: "8px", height: "8px", borderRadius: "50%",
                        backgroundColor: running ? colors.success : colors.primaryLight,
                    }} />
                    <span style={{ fontSize: "0.72rem", color: running ? colors.success : colors.textMuted }}>
                        {running ? (t("Connected", "已连接")) : (t("Disconnected", "未连接"))}
                    </span>
                </div>
            </div>

            {updateMsg && (
                <div style={{
                    fontSize: "0.72rem", marginBottom: "8px", padding: "4px 10px",
                    color: updateMsg.startsWith("✅") ? colors.success
                        : updateMsg.startsWith("⏳") ? colors.primary
                        : updateMsg.startsWith("⚠️") ? colors.warning
                        : colors.danger,
                }}>
                    {updateMsg}
                </div>
            )}

            {error && (
                <div style={{
                    fontSize: "0.75rem",
                    color: error.includes("[agentnet-not-available]") ? colors.warning : colors.danger,
                    marginBottom: "8px",
                    padding: "8px 12px",
                    background: error.includes("[agentnet-not-available]") ? colors.warningBg : colors.dangerBg,
                    borderRadius: radius.md,
                    border: `1px solid ${colors.border}`,
                    whiteSpace: "pre-wrap",
                    wordBreak: "break-word",
                    lineHeight: 1.6,
                }}>
                    {error.replace("[agentnet-not-available] ", "")}
                </div>
            )}

            {/* Download progress */}
            {downloadProgress && downloadProgress.stage !== "done" && (
                <div style={{ ...card, background: colors.accentBg }}>
                    <div style={{ display: "flex", justifyContent: "space-between", marginBottom: "4px", fontSize: "0.78rem", color: colors.textSecondary }}>
                        <span>{t("Downloading AgentNet...", "正在下载 AgentNet...")}</span>
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
                        <div><span style={label}>{t("Peers", "节点数")}:</span> <span style={mono}>{status.peers}</span></div>
                        <div><span style={label}>{t("Unread DM", "未读私信")}:</span> <span style={mono}>{status.unread_dm || 0}</span></div>
                        <div><span style={label}>{t("Version", "版本")}:</span> <span style={mono}>{status.version}</span></div>
                        {status.uptime && <div><span style={label}>{t("Uptime", "运行时间")}:</span> <span style={mono}>{status.uptime}</span></div>}
                    </div>
                </div>
            )}

            {/* Daemon process info — visible even when disconnected so user can see dead state */}
            {enabled && daemonInfo && (
                <div style={{ ...card, background: colors.bg }}>
                    <div style={{ display: "flex", alignItems: "center", gap: "8px", fontSize: "0.72rem", flexWrap: "wrap" }}>
                        <span style={label}>{t("Process", "进程")}:</span>
                        <span style={mono}>agentnet{daemonInfo.bin_path?.endsWith(".exe") ? ".exe" : ""}</span>
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
                                ({t("process lost", "进程已断开")})
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
                    {credits.tier && <span style={{ marginLeft: "10px", ...label }}>{t("Tier", "等级")}: {credits.tier}</span>}
                </div>
            )}

            {/* Finance entry — always visible, right after credits for discoverability */}
            <div style={{ ...card, padding: 0, overflow: "hidden" }}>
                <div
                    onClick={() => {
                        if (!financeOpen) {
                            setFinanceOpen(true);
                            if (running) refreshFinance();
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
                        {t("Finance Details", "财务信息")}
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
                        {t("AgentNet not connected. Connect to view finance data.", "智网未连接，连接后可查看财务数据")}
                    </div>
                )}
                {financeOpen && running && (
                    <div style={{ padding: "0 14px 10px 14px", borderTop: `1px solid ${colors.border}` }}>
                        <div style={{ ...label, margin: "10px 0 6px 0", fontWeight: 500 }}>{t("Leaderboard", "排行榜")}</div>
                        {financeLoading && <div style={{ ...label, padding: "8px 0" }}>{t("Loading...", "加载中...")}</div>}
                        {!financeLoading && financeError && (
                            <div style={{ padding: "6px 0", fontSize: "0.72rem", color: colors.danger }}>
                                {financeError}
                            </div>
                        )}
                        {!financeLoading && !financeError && (
                            <div style={{ maxHeight: "180px", overflowY: "auto", fontSize: "0.72rem" }}>
                                {leaderboard.length === 0 && <div style={{ ...label, padding: "6px 0" }}>{t("No leaderboard data", "暂无排行数据")}</div>}
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
                            🤖 {t("Auto Task Pickup", "自动接单")}
                        </div>
                        <label style={{ display: "flex", alignItems: "center", gap: "6px", cursor: "pointer", fontSize: "0.72rem" }}>
                            <input
                                type="checkbox"
                                checked={!!pickerStatus?.enabled}
                                disabled={pickerBusy}
                                onChange={async (e) => {
                                    setPickerBusy(true);
                                    try {
                                        await AgentNetAutoPickerConfigure(e.target.checked, 5, 0, []);
                                        if (mountedRef.current) await refreshPickerStatus();
                                    } catch {}
                                    if (mountedRef.current) setPickerBusy(false);
                                }}
                            />
                            <span style={{ color: pickerStatus?.enabled ? colors.primary : colors.textMuted }}>
                                {pickerStatus?.enabled ? (t("Enabled", "已开启")) : (t("Disabled", "已关闭"))}
                            </span>
                        </label>
                    </div>
                    <div style={{ fontSize: "0.72rem", color: colors.textMuted, marginBottom: "6px" }}>
                        {t("When enabled, maClaw auto-discovers tasks from AgentNet, completes them, and earns 🐚 Shell",
                           "开启后，maClaw 会自动从智网寻找任务、完成并提交，赚取 🐚 Shell")}
                    </div>
                    {pickerStatus?.enabled && (
                        <div style={{ fontSize: "0.72rem", color: colors.textSecondary }}>
                            <div style={{ display: "flex", gap: "12px", flexWrap: "wrap", marginBottom: "6px" }}>
                                <span>{t("Completed", "已完成")}: {pickerStatus.completed_count ?? 0}</span>
                                <span>{t("Failed", "失败")}: {pickerStatus.failed_count ?? 0}</span>
                                <span>{t("Earned", "累计赚取")}: {pickerStatus.total_earned ?? 0} 🐚</span>
                            </div>
                            {pickerStatus.active_tasks?.length > 0 && (
                                <div style={{ marginTop: "4px", padding: "4px 8px", background: colors.accentBg, borderRadius: radius.sm, fontSize: "0.68rem" }}>
                                    {t("Running", "正在执行")}: {pickerStatus.active_tasks.map((t: any) => t.title).join(", ")}
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
                                    ? (t("Searching...", "搜索中..."))
                                    : (t("Find Task Now", "立即寻找任务"))}
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
                    <div style={{ ...label, marginBottom: "4px" }}>{t("Connected Peers", "已连接节点")} ({peers.length})</div>
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
                <div style={heading}>🔑 {t("Identity Key", "身份密钥")}</div>
                <div style={{ fontSize: "0.72rem", color: colors.textMuted, marginBottom: "8px" }}>
                    {t("Your identity key (Ed25519) is your unique credential on AgentNet. Back it up — it cannot be recovered if lost.",
                       "身份密钥是你在智网上的唯一身份凭证（Ed25519），丢失后无法恢复。请妥善备份。")}
                </div>
                <div style={{ display: "flex", alignItems: "center", gap: "6px", flexWrap: "wrap" }}>
                    <span style={{
                        fontSize: "0.68rem", padding: "2px 8px", borderRadius: radius.pill,
                        background: identityExists ? colors.successBg : colors.dangerBg,
                        color: identityExists ? colors.success : colors.danger,
                        border: `1px solid ${identityExists ? colors.success : colors.danger}20`,
                    }}>
                        {identityExists ? (t("Exists", "已生成")) : (t("Not found", "未生成"))}
                    </span>
                    <button onClick={handleExportKey} disabled={keyBusy || !identityExists} style={actionBtn(keyBusy || !identityExists)}>
                        {t("Export", "备份")}
                    </button>
                    <button onClick={handleImportKey} disabled={keyBusy} style={actionBtn(keyBusy)}>
                        {t("Import", "恢复")}
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
                <div style={heading}>☁️ {t("Online Backup / Restore", "在线备份 / 恢复")}</div>
                <div style={{ fontSize: "0.72rem", color: colors.textMuted, marginBottom: "8px" }}>
                    {t("Encrypt and save your key to Hub, bound to your email. Restore on any device with your password.",
                       "将密钥加密后保存到 Hub，与你的邮箱绑定。换设备时可用口令恢复。")}
                </div>

                {/* Backup section */}
                <div style={{ marginBottom: "8px" }}>
                    <div style={{ fontSize: "0.72rem", color: colors.textSecondary, marginBottom: "4px", fontWeight: 500 }}>
                        {t("Backup (set password)", "备份（设置口令）")}
                    </div>
                    <div style={{ display: "flex", gap: "4px", alignItems: "center", flexWrap: "wrap" }}>
                        <input type="password" value={onlinePwd} onChange={(e) => setOnlinePwd(e.target.value)}
                            placeholder={t("Password (≥6)", "口令（≥6位）")}
                            style={{ width: "100px", border: `1px solid ${colors.border}`, borderRadius: radius.md, padding: "3px 8px", fontSize: "0.72rem" }} />
                        <input type="password" value={onlinePwd2} onChange={(e) => setOnlinePwd2(e.target.value)}
                            placeholder={t("Confirm", "确认口令")}
                            style={{ width: "100px", border: `1px solid ${colors.border}`, borderRadius: radius.md, padding: "3px 8px", fontSize: "0.72rem" }} />
                        <button onClick={handleOnlineBackup} disabled={onlineBusy || !identityExists || !onlinePwd || !onlinePwd2}
                            style={actionBtn(onlineBusy || !identityExists || !onlinePwd || !onlinePwd2)}>
                            {t("Backup", "加密备份")}
                        </button>
                    </div>
                </div>

                {/* Restore section */}
                <div>
                    <div style={{ fontSize: "0.72rem", color: colors.textSecondary, marginBottom: "4px", fontWeight: 500 }}>
                        {t("Restore (enter password)", "恢复（输入口令）")}
                    </div>
                    <div style={{ display: "flex", gap: "4px", alignItems: "center", flexWrap: "wrap" }}>
                        <input type="password" value={onlineRestorePwd} onChange={(e) => setOnlineRestorePwd(e.target.value)}
                            placeholder={t("Password", "口令")}
                            style={{ width: "100px", border: `1px solid ${colors.border}`, borderRadius: radius.md, padding: "3px 8px", fontSize: "0.72rem" }} />
                        <button onClick={handleOnlineRestore} disabled={onlineBusy || !onlineRestorePwd}
                            style={actionBtn(onlineBusy || !onlineRestorePwd)}>
                            {t("Restore", "在线恢复")}
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
                {t("Binary", "二进制路径")}: {binPath || (t("not found", "未找到"))}
            </div>
        </div>
    );
}

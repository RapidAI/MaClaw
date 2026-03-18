import { useState, useEffect, useCallback } from "react";
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
    const [identityPath, setIdentityPath] = useState("");
    const [keyBusy, setKeyBusy] = useState(false);
    const [keyMsg, setKeyMsg] = useState("");
    // Online backup/restore
    const [onlinePwd, setOnlinePwd] = useState("");
    const [onlinePwd2, setOnlinePwd2] = useState("");
    const [onlineRestorePwd, setOnlineRestorePwd] = useState("");
    const [onlineBusy, setOnlineBusy] = useState(false);
    const [onlineMsg, setOnlineMsg] = useState("");

    const zh = lang?.startsWith("zh");
    const enabled = (config as any)?.clawnet_enabled !== false;

    // Poll running state
    const refreshStatus = useCallback(async () => {
        try {
            const isUp = await ClawNetIsRunning();
            setRunning(isUp);
            onRunningChange(isUp);
            if (isUp) {
                const s = await ClawNetGetStatus();
                if (s.ok) setStatus(s);
                const p = await ClawNetGetPeers();
                if (p.ok) setPeers(p.peers || []);
                const c = await ClawNetGetCredits();
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
    }, [onRunningChange]);

    const refreshIdentity = useCallback(async () => {
        try {
            const res = await ClawNetHasIdentity();
            if (res.ok) {
                setIdentityExists(!!res.exists);
                setIdentityPath(res.path || "");
            }
        } catch {}
    }, []);

    useEffect(() => {
        ClawNetGetBinaryPath().then(setBinPath).catch(() => {});
        refreshStatus();
        refreshIdentity();
        const timer = setInterval(refreshStatus, 15000);
        return () => clearInterval(timer);
    }, [refreshStatus, refreshIdentity]);

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
        } catch (e) {
            setError(String(e));
        } finally {
            setBusy(false);
        }
    };

    const handleToggle = (checked: boolean) => {
        saveRemoteConfigField({ clawnet_enabled: checked } as any);
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

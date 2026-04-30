import { useEffect, useMemo, useState } from "react";
import { main } from "../../../wailsjs/go/models";
import {
    IWorkerCenterConfigStatus,
    IWorkerGoalWatchStatus,
    RunIWorkerGoalWatchOnce,
    SaveIWorkerCenterConfig,
    StartIWorkerGoalWatch,
    StopIWorkerGoalWatch,
} from "../../../wailsjs/go/main/App";
import { colors, radius, remoteBodyTextStyle, remoteCardStyle, remoteSectionTitleStyle } from "./styles";

type Props = {
    config: main.AppConfig | null;
    lang: string;
    onConfigSaved?: (patch: Partial<main.AppConfig>) => void;
};

type GoalWatchStatus = {
    running?: boolean;
    skipped_reason?: string;
    last_run_age_seconds?: number;
    summary?: {
        checked?: number;
        recovered?: number;
        skipped?: number;
        errors?: string[];
    };
};

const text = (lang: string, en: string, zhHans: string, zhHant: string) => {
    if (lang === "zh-Hans") return zhHans;
    if (lang === "zh-Hant") return zhHant;
    return en;
};

export function IWorkerCenterPanel({ config, lang, onConfigSaved }: Props) {
    const [url, setURL] = useState(config?.iworkercenter_url || "");
    const [tenantID, setTenantID] = useState(config?.iworkercenter_tenant_id || "");
    const [colleagueID, setColleagueID] = useState(config?.iworkercenter_colleague_id || "");
    const [intervalSec, setIntervalSec] = useState(String(config?.iworkercenter_goalwatch_interval_sec || 60));
    const [status, setStatus] = useState<GoalWatchStatus | null>(null);
    const [configStatus, setConfigStatus] = useState<any>(null);
    const [busy, setBusy] = useState("");
    const [message, setMessage] = useState("");
    const [error, setError] = useState("");

    useEffect(() => {
        setURL(config?.iworkercenter_url || "");
        setTenantID(config?.iworkercenter_tenant_id || "");
        setColleagueID(config?.iworkercenter_colleague_id || "");
        setIntervalSec(String(config?.iworkercenter_goalwatch_interval_sec || 60));
    }, [config?.iworkercenter_url, config?.iworkercenter_tenant_id, config?.iworkercenter_colleague_id, config?.iworkercenter_goalwatch_interval_sec]);

    const configured = useMemo(() => url.trim() !== "" && tenantID.trim() !== "" && colleagueID.trim() !== "", [url, tenantID, colleagueID]);

    const refresh = async () => {
        try {
            const [cfgStatus, watchStatus] = await Promise.all([IWorkerCenterConfigStatus(), IWorkerGoalWatchStatus()]);
            setConfigStatus(cfgStatus);
            setStatus(watchStatus as GoalWatchStatus);
        } catch (err) {
            setError(String(err));
        }
    };

    useEffect(() => {
        refresh();
        const timer = window.setInterval(refresh, 5000);
        return () => window.clearInterval(timer);
    }, []);

    const save = async (autoStart: boolean) => {
        setBusy(autoStart ? "save-start" : "save");
        setError("");
        setMessage("");
        try {
            const seconds = Number.parseInt(intervalSec || "60", 10);
            const payload = new main.IWorkerCenterConfigRequest({
                url,
                tenant_id: tenantID,
                colleague_id: colleagueID,
                goalwatch_interval_sec: Number.isFinite(seconds) && seconds > 0 ? seconds : 60,
                auto_start: autoStart,
            });
            const result = await SaveIWorkerCenterConfig(payload) as any;
            if (!result?.ok) throw new Error(result?.error || "save failed");
            onConfigSaved?.({
                iworkercenter_url: url.trim().replace(/\/+$/, ""),
                iworkercenter_tenant_id: tenantID.trim(),
                iworkercenter_colleague_id: colleagueID.trim(),
                iworkercenter_goalwatch_interval_sec: Number.isFinite(seconds) && seconds > 0 ? seconds : 60,
            } as Partial<main.AppConfig>);
            setMessage(autoStart ? text(lang, "Saved and watcher started.", "已保存并启动 watcher。", "已保存並啟動 watcher。") : text(lang, "Saved.", "已保存。", "已保存。"));
            await refresh();
        } catch (err) {
            setError(String(err));
        } finally {
            setBusy("");
        }
    };

    const runAction = async (action: "start" | "stop" | "run") => {
        setBusy(action);
        setError("");
        setMessage("");
        try {
            if (action === "start") await StartIWorkerGoalWatch();
            if (action === "stop") await StopIWorkerGoalWatch();
            if (action === "run") await RunIWorkerGoalWatchOnce();
            setMessage(text(lang, "Command sent.", "指令已发送。", "指令已發送。"));
            await refresh();
        } catch (err) {
            setError(String(err));
        } finally {
            setBusy("");
        }
    };

    const summary = status?.summary || {};
    const running = !!status?.running;
    const serviceProbe = configStatus?.center_service as any;
    const serviceOK = serviceProbe?.ok === true;
    const serviceError = serviceProbe?.error ? String(serviceProbe.error) : "";
    const serviceIdentity = serviceProbe?.status || {};
    const cloudHeartbeat = serviceIdentity?.cloud_heartbeat || {};
    const cloudHeartbeatStatus = cloudHeartbeat?.status ? String(cloudHeartbeat.status) : "";
    const cloudHeartbeatOK = cloudHeartbeatStatus === "online";
    const cloudHeartbeatWarn = cloudHeartbeatStatus === "degraded" || cloudHeartbeatStatus === "waiting_for_credentials";
    const formatHeartbeatTime = (value?: string) => {
        if (!value || value.startsWith("0001-")) return "-";
        const date = new Date(value);
        return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
    };

    return (
        <div style={{ display: "grid", gap: "12px" }}>
            <div style={{ ...remoteCardStyle, background: `linear-gradient(135deg, ${colors.surface}, ${colors.surfaceMuted})` }}>
                <div style={{ display: "flex", justifyContent: "space-between", gap: "12px", alignItems: "flex-start", marginBottom: "10px" }}>
                    <div>
                        <div style={{ ...remoteSectionTitleStyle, fontSize: "0.9rem" }}>{text(lang, "Center Service Connection", "组织服务连接", "組織服務連接")}</div>
                        <div style={{ ...remoteBodyTextStyle, lineHeight: 1.55 }}>
                            {text(lang, "Connect this iWorker body to the company-owned iWorkerCenter service. This is not a desktop Center GUI; memory, workflow and GoalWatch authority stay on the server.", "把这个 iWorker 身体连接到企业自己的 iWorkerCenter 服务。这里不是 Center 桌面 GUI；记忆、Workflow 和 GoalWatch 权威仍保存在服务端。", "把這個 iWorker 身體連接到企業自己的 iWorkerCenter 服務。這裡不是 Center 桌面 GUI；記憶、Workflow 和 GoalWatch 權威仍保存在服務端。")}
                        </div>
                    </div>
                    <span style={{ borderRadius: radius.pill, padding: "3px 10px", fontSize: "0.72rem", fontWeight: 700, color: configured ? colors.success : colors.textMuted, background: configured ? colors.successBg : colors.surfaceMuted, border: `1px solid ${configured ? colors.success : colors.border}` }}>
                        {configured ? text(lang, "Configured", "已配置", "已配置") : text(lang, "Not configured", "未配置", "未配置")}
                    </span>
                </div>
                <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(180px, 1fr))", gap: "10px" }}>
                    <label className="form-group" style={{ marginBottom: 0 }}>
                        <span className="form-label">Center URL</span>
                        <input className="form-input" value={url} onChange={(e) => setURL(e.target.value)} placeholder="https://center.example.com" spellCheck={false} />
                    </label>
                    <label className="form-group" style={{ marginBottom: 0 }}>
                        <span className="form-label">Tenant ID</span>
                        <input className="form-input" value={tenantID} onChange={(e) => setTenantID(e.target.value)} placeholder="tenant-a" spellCheck={false} />
                    </label>
                    <label className="form-group" style={{ marginBottom: 0 }}>
                        <span className="form-label">Colleague ID</span>
                        <input className="form-input" value={colleagueID} onChange={(e) => setColleagueID(e.target.value)} placeholder="worker-a" spellCheck={false} />
                    </label>
                    <label className="form-group" style={{ marginBottom: 0 }}>
                        <span className="form-label">GoalWatch interval</span>
                        <input className="form-input" type="number" min={5} value={intervalSec} onChange={(e) => setIntervalSec(e.target.value)} />
                    </label>
                </div>
                <div style={{ display: "flex", gap: "8px", flexWrap: "wrap", marginTop: "12px" }}>
                    <button className="btn-primary" disabled={!!busy} onClick={() => save(true)}>{busy === "save-start" ? "..." : text(lang, "Save & Start", "保存并启动", "保存並啟動")}</button>
                    <button className="btn-secondary" disabled={!!busy} onClick={() => save(false)}>{text(lang, "Save Only", "仅保存", "僅保存")}</button>
                    <button className="btn-secondary" disabled={!!busy || !configured} onClick={() => runAction("start")}>{text(lang, "Start Watcher", "启动 watcher", "啟動 watcher")}</button>
                    <button className="btn-secondary" disabled={!!busy} onClick={() => runAction("stop")}>{text(lang, "Stop", "停止", "停止")}</button>
                    <button className="btn-secondary" disabled={!!busy || !configured} onClick={() => runAction("run")}>{text(lang, "Run Once", "立即执行一次", "立即執行一次")}</button>
                    <button className="btn-secondary" disabled={!!busy} onClick={refresh}>{text(lang, "Refresh", "刷新", "刷新")}</button>
                </div>
                {message && <div style={{ color: colors.success, fontSize: "0.76rem", marginTop: "8px" }}>{message}</div>}
                {error && <div style={{ color: colors.danger, fontSize: "0.76rem", marginTop: "8px" }}>{error}</div>}
                {configured && serviceProbe && (
                    <div style={{ marginTop: "10px", padding: "8px 10px", borderRadius: radius.md, border: `1px solid ${serviceOK ? colors.success : colors.danger}`, background: serviceOK ? colors.successBg : colors.dangerBg, color: serviceOK ? colors.success : colors.danger, fontSize: "0.74rem", lineHeight: 1.45 }}>
                        {serviceOK
                            ? text(lang, "Connected to iWorkerCenter service runtime.", "已连接到 iWorkerCenter 服务运行时。", "已連接到 iWorkerCenter 服務執行時。")
                            : text(lang, "Center service probe failed: ", "Center 服务探测失败：", "Center 服務探測失敗：") + serviceError}
                        {serviceOK && <span style={{ color: colors.textMuted }}> {serviceIdentity.runtime_type || "service"} / {serviceIdentity.product_kind || "iworkercenter"} / {serviceIdentity.admin_console || "web_console"}</span>}
                    </div>
                )}
                {serviceOK && cloudHeartbeatStatus && (
                    <div style={{ marginTop: "8px", padding: "8px 10px", borderRadius: radius.md, border: `1px solid ${cloudHeartbeatOK ? colors.success : cloudHeartbeatWarn ? colors.warning : colors.danger}`, background: cloudHeartbeatOK ? colors.successBg : cloudHeartbeatWarn ? colors.warningBg : colors.dangerBg, color: cloudHeartbeatOK ? colors.success : cloudHeartbeatWarn ? colors.warning : colors.danger, fontSize: "0.74rem", lineHeight: 1.45 }}>
                        <strong>{text(lang, "Center -> Cloud heartbeat", "Center -> Cloud 蹇冭烦", "Center -> Cloud 蹇冭烦")}: {cloudHeartbeatStatus}</strong>
                        <span style={{ color: colors.textMuted }}> · center_id: {cloudHeartbeat.center_id || "-"} · last success: {formatHeartbeatTime(cloudHeartbeat.last_success_at)} · failures: {cloudHeartbeat.consecutive_failures ?? 0}</span>
                        {cloudHeartbeat.last_error && <div style={{ marginTop: "4px" }}>{String(cloudHeartbeat.last_error)}</div>}
                    </div>
                )}
            </div>

            <div style={{ ...remoteCardStyle }}>
                <div style={remoteSectionTitleStyle}>{text(lang, "GoalWatch Runtime", "GoalWatch 运行状态", "GoalWatch 執行狀態")}</div>
                <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(130px, 1fr))", gap: "8px" }}>
                    <Metric label={text(lang, "Watcher", "Watcher", "Watcher")} value={running ? text(lang, "Running", "运行中", "執行中") : text(lang, "Idle", "空闲", "閒置")} tone={running ? "good" : "muted"} />
                    <Metric label={text(lang, "Checked", "检查", "檢查")} value={summary.checked ?? 0} />
                    <Metric label={text(lang, "Recovered", "恢复", "恢復")} value={summary.recovered ?? 0} tone="good" />
                    <Metric label={text(lang, "Skipped", "跳过", "略過")} value={summary.skipped ?? 0} />
                    <Metric label={text(lang, "Last age", "距上次", "距上次")} value={status?.last_run_age_seconds != null ? `${status.last_run_age_seconds}s` : "-"} />
                </div>
                {status?.skipped_reason && <div style={{ ...remoteBodyTextStyle, marginTop: "8px" }}>{status.skipped_reason}</div>}
                {!!summary.errors?.length && <div style={{ color: colors.danger, fontSize: "0.76rem", marginTop: "8px" }}>{summary.errors.join("; ")}</div>}
                {configStatus?.configured === false && <div style={{ ...remoteBodyTextStyle, marginTop: "8px" }}>{text(lang, "Complete URL, tenant and colleague fields to enable local watcher.", "填写 URL、租户和数字员工 ID 后可启用本地 watcher。", "填寫 URL、租戶和數字員工 ID 後可啟用本地 watcher。")}</div>}
            </div>
        </div>
    );
}

function Metric({ label, value, tone = "normal" }: { label: string; value: string | number; tone?: "normal" | "good" | "muted" }) {
    const color = tone === "good" ? colors.success : tone === "muted" ? colors.textMuted : colors.text;
    return (
        <div style={{ border: `1px solid ${colors.border}`, borderRadius: radius.md, padding: "8px 10px", background: colors.bg }}>
            <div style={{ fontSize: "0.68rem", color: colors.textMuted, marginBottom: "4px" }}>{label}</div>
            <div style={{ fontSize: "0.9rem", fontWeight: 700, color }}>{value}</div>
        </div>
    );
}
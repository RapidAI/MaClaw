import { useEffect, useState } from "react";
import { AssistantReasoningPanel } from "./AssistantReasoningPanel";
import { localizeText } from "../../i18n";
import { workflowStatusForTask } from "../layout/SidebarTaskManagement";
import type { RemoteAssistantSession } from "../layout/remoteAssistantSession";
import type { Theme } from "./aiAssistantPanelTheme";
import { formatProjectSearchTime } from "./projectSearchTime";

// Heartbeat timestamps arrive on the poll cadence; a local 1s tick keeps the
// "N 秒前" and elapsed readouts moving between polls while a run is live.
const TICK_BUSY_MS = 1000;

function remoteTimeAgo(lang: string, iso: string | undefined, now: number): string {
    const stamp = Date.parse(iso || "");
    if (!iso || !Number.isFinite(stamp)) return "";
    const diff = Math.max(0, now - stamp);
    if (diff < 60_000) {
        return `${Math.max(1, Math.floor(diff / 1000))}${localizeText(lang, "s ago", "秒前", "秒前")}`;
    }
    if (diff < 3_600_000) {
        return `${Math.floor(diff / 60_000)}${localizeText(lang, "min ago", "分钟前", "分鐘前")}`;
    }
    return formatProjectSearchTime(lang, iso);
}

function remoteElapsed(lang: string, busySince: number | undefined, now: number): string {
    if (!busySince) return "";
    const total = Math.max(0, Math.floor((now - busySince) / 1000));
    const minutes = Math.floor(total / 60);
    const seconds = total % 60;
    const value = minutes > 0 ? `${minutes}:${String(seconds).padStart(2, "0")}` : `${seconds}s`;
    return localizeText(lang, `Elapsed ${value}`, `已用时 ${value}`, `已用時 ${value}`);
}

function remoteTerminalStatus(status?: string): boolean {
    return /(completed|cancelled|canceled|failed|interrupted|done)/.test(String(status || "").trim().toLowerCase());
}

/**
 * Live status surface for a remote-control task. The get_task poll carries a
 * workflow snapshot and a heartbeat; the transcript itself renders each
 * turn's thinking as 思考过程 blocks, so this panel only reports the live
 * state instead of duplicating the reasoning or the produced-file list.
 */
export function RemoteAssistantActivity({ session, lang, theme: t }: { session: RemoteAssistantSession; lang: string; theme: Theme }) {
    const activity = session.activity;
    const busy = !!session.busy && !session.loading;
    const workflow = activity?.workflow || undefined;
    const workflowStatus = workflowStatusForTask(workflow, lang);
    const paused = /(paused|pause|已暂停|暂停)/.test(`${workflow?.status || ""} ${workflow?.phase || ""}`.toLowerCase());
    const review = !!workflow?.pending_review && !paused && !remoteTerminalStatus(workflow?.status);
    // "busy" is awaitingReply-or-running, but paused / review-needed are the
    // attention states worth keeping on screen after the run itself idles.
    // The loading splash owns the surface until the transcript lands.
    const showPanel = (busy || paused || review) && !session.loading;
    const [now, setNow] = useState(() => Date.now());
    useEffect(() => {
        // The elapsed/heartbeat meta only renders while busy — ticking in a
        // paused or waiting state would re-render for nobody.
        if (!busy) return;
        const timer = window.setInterval(() => setNow(Date.now()), TICK_BUSY_MS);
        return () => window.clearInterval(timer);
    }, [busy]);

    if (!showPanel) return null;

    let liveLabel = localizeText(lang, "Working on the other computer…", "对方正在执行…", "對方正在執行…");
    if (activity?.stopping) {
        liveLabel = localizeText(lang, "Stopping the task on the other computer…", "正在停止对方任务…", "正在停止對方任務…");
    } else if (paused) {
        liveLabel = localizeText(lang, "Paused on the other computer", "对方任务已暂停", "對方任務已暫停");
    } else if (review) {
        liveLabel = localizeText(lang, "Waiting for review on the other computer…", "等待对方处理审核…", "等待對方處理審核…");
    } else if (activity?.awaitingReply && (!workflowStatus || workflowStatus.tone !== "info")) {
        liveLabel = localizeText(lang, "Sent. Waiting for the other computer to start…", "已发送，等待对方开始执行…", "已發送，等待對方開始執行…");
    }
    // A terminal snapshot is the previous run's residue — "正在执行 · 已完成"
    // would read as a contradiction, so only live states make the object slot.
    const liveObject = remoteTerminalStatus(workflow?.status)
        ? undefined
        : [workflowStatus?.detail, workflowStatus?.label].filter(Boolean).join(" · ")
            || (activity?.workflowType === "coding" ? localizeText(lang, "Coding task", "编码任务", "編碼任務") : undefined);

    const lastActivityAgo = remoteTimeAgo(lang, activity?.lastActivityAt, now);
    const meta = [
        remoteElapsed(lang, activity?.busySince, now),
        lastActivityAgo
            ? localizeText(lang, `Last activity ${lastActivityAgo}`, `对方最近活动：${lastActivityAgo}`, `對方最近活動：${lastActivityAgo}`)
            : "",
    ].filter(Boolean).join(" · ");

    return (
        <div className="aap-remote-activity" data-testid="remote-activity">
            <AssistantReasoningPanel
                live={busy}
                // A paused/review row cannot expand (no body); preview keeps
                // the object name visible under the label instead.
                preview={busy ? undefined : liveObject}
                defaultOpen={false}
                label={liveLabel}
                objectLabel={liveObject}
                lang={lang}
                theme={t}
                contentKey={`remote-live|${session.grantId}|${session.projectPath}`}
            >
                {null}
            </AssistantReasoningPanel>
            {busy && meta ? (
                <div className="aap-remote-activity-meta" data-testid="remote-activity-meta">{meta}</div>
            ) : null}
        </div>
    );
}

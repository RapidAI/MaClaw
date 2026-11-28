import { useEffect, useState } from "react";
import { AssistantReasoningPanel } from "./AssistantReasoningPanel";
import { localizeText } from "../../i18n";
import { workflowStatusForTask } from "../layout/SidebarTaskManagement";
import { remoteAssistantSend, remoteLiveRoundPaintsRows, type RemoteAssistantSession } from "../layout/remoteAssistantSession";
import type { Theme } from "./aiAssistantPanelTheme";
import { formatProjectSearchTime } from "./projectSearchTime";
import { renderCodingAgentWorkingTrail } from "./CodingAgentProgressStatus";
import { CodingAgentPlanChecklist } from "./CodingAgentPlanChecklist";
import { cloudAgentTurnFileChanges } from "./cloudAgentPreview";
import {
    buildCodingBannerChrome,
    codingChecklistAllDone,
    codingPlanAwaitingStart,
    codingSharedExecutionStatus,
    codingStepIsActive,
    codingStepIsFailed,
    codingWorkbenchStepsFromPayload,
    presentCodingAgentChecklist,
    type CodingStepStatus,
} from "./CodingWorkbenchControlPanel";

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

/** The task list's finished-success and cancelled words. Failed and
 * interrupted stay open: a failed row is still the outcome, and an
 * interrupt is the cursor a later continue resumes from. */
function remoteWorkflowClosed(status?: string): boolean {
    const value = String(status || "").trim().toLowerCase();
    if (/^(complete|completed|finish|finished|success|succeeded|done|passed)$/.test(value)) return true;
    return value === "cancelled" || value === "canceled";
}

function remoteWorkbenchSteps(session: RemoteAssistantSession): CodingStepStatus[] {
    const workbench = session.workbench;
    return presentCodingAgentChecklist(
        workbench?.execution_plan,
        codingWorkbenchStepsFromPayload(workbench?.step_statuses),
    );
}

/**
 * The controlled desktop's plan, drawn with the same bar the local composer
 * uses. It sits above the input, centered; the message stream does not host
 * the rows. Approval stays on the other computer, so this bar has no gate.
 */
export function RemoteCodingPlanChecklist({ session, lang, theme: t, onOpenFile }: { session: RemoteAssistantSession; lang: string; theme: Theme; onOpenFile?: (path: string) => void }) {
    const steps = remoteWorkbenchSteps(session);
    const understanding = (session.workbench?.requirement_restatement || "").trim();
    const fileChanges = cloudAgentTurnFileChanges(session);
    const failed = steps.some(step => codingStepIsFailed(step.status));
    // Same retirement as the local composer bar. All-passed folds on its own.
    // A completed or cancelled task also folds a leftover running cursor, a
    // pending tail, or a restatement that no longer has a turn. An open
    // approval and a failed row stay. A turn that is still streaming wins
    // over a status word that already says completed.
    // A stop in flight is not a live step. Busy stays true until the other
    // computer leaves the running bucket, and the mirrored round can still
    // be open; neither one should keep the cursor spinning.
    const stopping = !!session.activity?.stopping;
    // Loading only means the transcript has not arrived. The task list can
    // already say the other computer is running; the bar must not call that
    // cursor stopped while the splash is up.
    const working = !stopping && (!!session.busy || (!!session.live && !session.live.responded));
    const gateOpen = !!session.workbench?.pending_approval && codingPlanAwaitingStart(steps);
    // The stop flag wins over a status word that already says completed or
    // cancelled. The poll can flip that word while the stop is still in
    // flight; folding here would drop the list out from under "正在停止".
    const closed = !stopping && !working && remoteWorkflowClosed(session.activity?.workflow?.status);
    const projection = codingSharedExecutionStatus(session.workbench);
    const projectionOpen = projection === "failed" || projection === "waiting_review";
    const retired = (
        (!gateOpen && !failed && !projectionOpen && (codingChecklistAllDone(steps) || (closed && (steps.length > 0 || !!understanding))))
        || (steps.length === 0 && !understanding)
    );
    if (retired && fileChanges.length === 0) return null;
    const workflowStatus = workflowStatusForTask(session.activity?.workflow, lang);
    // A stored running mark is only "in progress" while this turn is live.
    // Paused, interrupted, and review keep their own outcome on that cursor.
    // The generic in-progress word is not an outcome; a blank label falls
    // back to stopped inside the checklist.
    const heldLabel = stopping
        ? localizeText(lang, "Stopping", "正在停止", "正在停止")
        : (!working && workflowStatus && workflowStatus.tone !== "info" ? workflowStatus.label : "");
    // A controlled desktop decides on the other computer — no gate buttons.
    // A cloud task's gate is answered by this panel's own send.
    const cloudGate = session.cloud && gateOpen
        ? {
            onApprove: () => { void remoteAssistantSend("按计划开始"); },
            onSkip: () => { void remoteAssistantSend("不用计划，直接做"); },
            onReject: () => { void remoteAssistantSend("取消这个计划"); },
        }
        : {};
    return (
        <CodingAgentPlanChecklist
            lang={lang}
            theme={t}
            chrome={buildCodingBannerChrome({ isDark: !!t.isDark, remote: true, theme: t })}
            steps={retired ? [] : steps}
            understanding={retired ? "" : understanding}
            pendingApproval={retired ? false : !!session.cloud && gateOpen}
            executionLive={working}
            executionStatusLabel={heldLabel}
            fileChanges={fileChanges}
            onOpenFile={onOpenFile}
            {...(retired ? {} : cloudGate)}
        />
    );
}

/**
 * Live status for a remote or cloud task. The Working trail fills a busy
 * round only while the bubble is not already painting that round.
 */
export function RemoteAssistantActivity({ session, lang, theme: t }: { session: RemoteAssistantSession; lang: string; theme: Theme }) {
    const activity = session.activity;
    const busy = !!session.busy && !session.loading;
    const workflow = activity?.workflow || undefined;
    const paused = /(paused|pause|已暂停|暂停)/.test(`${workflow?.status || ""} ${workflow?.phase || ""}`.toLowerCase());
    const review = !!workflow?.pending_review && !paused && !remoteTerminalStatus(workflow?.status);
    // "busy" is awaitingReply-or-running, but paused / review-needed are the
    // attention states worth keeping on screen after the run itself idles.
    // The loading splash owns the surface until the transcript lands.
    // A mirrored round that is still streaming keeps the surface up too — the
    // live trail arrives over remote_control.stream before the task poll can
    // report the run.
    const hasLive = !!session.live && !session.live.responded && session.live.parts.length > 0;

    // Step rows live on the composer bar (RemoteCodingPlanChecklist). This
    // panel only names the current step; it does not draw the list.
    const workbench = session.workbench || null;

    const showPanel = (busy || paused || review || hasLive || !!workbench?.pending_approval) && !session.loading;
    const [now, setNow] = useState(() => Date.now());
    // The heartbeat meta is the only consumer of the ticking clock; without
    // a heartbeat to show, a per-second re-render runs for nobody.
    const metaVisible = busy && !!(activity?.busySince || activity?.lastActivityAt);
    useEffect(() => {
        if (!metaVisible) return;
        const timer = window.setInterval(() => setNow(Date.now()), TICK_BUSY_MS);
        return () => window.clearInterval(timer);
    }, [metaVisible]);

    if (!showPanel) return null;

    // Below the visibility gate: these feed only the collapsible panel's
    // labels, so an idle session's poll re-renders skip the step-payload
    // and workflow-status work entirely.
    const workflowStatus = workflowStatusForTask(workflow, lang);
    const activeStep = remoteWorkbenchSteps(session).find(step => codingStepIsActive(step.status));
    // The trail yields only to rows the bubble actually paints. A raw part
    // count is not that: plain progress lines render in the tray (a local
    // turn keeps the trail under them too), and hidden audit events paint
    // nowhere — silencing the trail for those left a busy round with no
    // live marker at all.
    const bubbleOwnsLiveRows = remoteLiveRoundPaintsRows(session.live);

    // A cloud agent session runs on the cloud server, not a controlled
    // computer — the labels name 云端 instead of 对方.
    const target = session.cloud
        ? localizeText(lang, "the cloud", "云端", "雲端")
        : localizeText(lang, "the other computer", "对方", "對方");
    let liveLabel = localizeText(lang, `Working on ${target}…`, `${target}正在执行…`, `${target}正在執行…`);
    if (activity?.stopping) {
        liveLabel = localizeText(lang, `Stopping the task on ${target}…`, `正在停止${target}任务…`, `正在停止${target}任務…`);
    } else if (paused) {
        liveLabel = localizeText(lang, `Paused on ${target}`, `${target}任务已暂停`, `${target}任務已暫停`);
    } else if (review) {
        liveLabel = localizeText(lang, `Waiting for review on ${target}…`, `等待${target}处理审核…`, `等待${target}處理審核…`);
    } else if (session.syncing) {
        liveLabel = localizeText(lang, `Catching up ${target}'s output…`, "正在补齐中断的输出…", "正在補齊中斷的輸出…");
    } else if (workbench?.pending_approval && !hasLive) {
        liveLabel = localizeText(lang, `Plan waiting for approval on ${target}…`, `${target}任务等待计划确认…`, `${target}任務等待計畫確認…`);
    } else if (activity?.awaitingReply && (!workflowStatus || workflowStatus.tone !== "info")) {
        liveLabel = localizeText(lang, `Sent. Waiting for ${target} to start…`, `已发送，等待${target}开始执行…`, `已發送，等待${target}開始執行…`);
    } else if (!busy && !hasLive) {
        // Idle attention (a plan waiting for approval there) must not
        // read as a live run.
        liveLabel = localizeText(lang, `Work plan on ${target}`, `${target}任务的工作清单`, `${target}任務的工作清單`);
    }

    const lastActivityAgo = remoteTimeAgo(lang, activity?.lastActivityAt, now);
    const meta = [
        remoteElapsed(lang, activity?.busySince, now),
        lastActivityAgo
            ? localizeText(lang, `Last activity ${lastActivityAgo}`, `${target}最近活动：${lastActivityAgo}`, `${target}最近活動：${lastActivityAgo}`)
            : "",
    ].filter(Boolean).join(" · ");

    const attention = !activity?.stopping && !paused && !review && !session.syncing
        && !(workbench?.pending_approval && !hasLive)
        && !(activity?.awaitingReply && (!workflowStatus || workflowStatus.tone !== "info"))
        && (busy || hasLive);
    if (attention) {
        const trail = bubbleOwnsLiveRows
            ? null
            : renderCodingAgentWorkingTrail({ text: t.text, fieldLabel: t.fieldLabel }, lang);
        const metaRow = busy && meta ? (
            <div className="aap-remote-activity-meta" data-testid="remote-activity-meta">{meta}</div>
        ) : null;
        if (!trail && !metaRow) return null;
        return (
            <div className="aap-remote-activity" data-testid="remote-activity">
                {trail}
                {metaRow}
            </div>
        );
    }

    // A terminal snapshot is the previous run's residue — "正在执行 · 已完成"
    // would read as a contradiction, so only live states make the object slot.
    // The running plan step is more specific than the workflow phase label,
    // so it names the object first, like the local checklist chip does.
    const liveObject = remoteTerminalStatus(workflow?.status)
        ? undefined
        : (activeStep ? `T${activeStep.index} ${activeStep.title}`.trim() : undefined)
            || [workflowStatus?.detail, workflowStatus?.label].filter(Boolean).join(" · ")
            || (activity?.workflowType === "coding" ? localizeText(lang, "Coding task", "编码任务", "編碼任務") : undefined);

    return (
        <div className="aap-remote-activity" data-testid="remote-activity">
            <AssistantReasoningPanel
                live={busy || hasLive}
                defaultOpen={false}
                // A paused/review row cannot expand (no body); preview keeps
                // the object name visible under the label instead.
                preview={liveObject}
                label={liveLabel}
                objectLabel={liveObject}
                lang={lang}
                theme={t}
                contentKey={`remote-live|${session.grantId}|${session.projectPath}`}
            >{null}</AssistantReasoningPanel>
            {busy && meta ? (
                <div className="aap-remote-activity-meta" data-testid="remote-activity-meta">{meta}</div>
            ) : null}
        </div>
    );
}

import { useEffect, useState } from "react";
import { AssistantReasoningPanel } from "./AssistantReasoningPanel";
import { AssistantToolCallRow } from "./assistantToolCallRow";
import { AssistantToolFlipTicker } from "./AssistantToolFlipTicker";
import { isProgressOnlyToolAction, parseAssistantToolStatus, type AssistantToolCall } from "./assistantToolCall";
import { localizeText } from "../../i18n";
import { workflowStatusForTask } from "../layout/SidebarTaskManagement";
import type { RemoteAssistantSession, RemoteLivePart } from "../layout/remoteAssistantSession";
import type { Theme } from "./aiAssistantPanelTheme";
import { formatProjectSearchTime } from "./projectSearchTime";
import { isCodingAgentProgressContent, renderCodingAgentActivityFeed } from "./CodingAgentProgressStatus";
import { CodingAgentPlanChecklist } from "./CodingAgentPlanChecklist";
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
import type { ChatMessage } from "./useAIAssistant";

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

type RemoteTrailItem = { kind: "tool"; call: AssistantToolCall } | { kind: "line"; text: string };

// The trail is a tail view — bound it like the plain-line cap it replaces.
const REMOTE_TRAIL_KEPT = 8;

/**
 * One fold of the mirrored progress parts. Coding-agent lines go to the
 * feed; styled tool-status lines (工具 · action (name)) fold into the same
 * tool cards a local turn paints, collapsing adjacent identical calls like
 * remoteLiveToolCalls; anything else stays a plain mono line. Position-based
 * ids keep rows mounted while the stream appends: part.seq is position-
 * derived on the reduced path and shifts when thinking/text parts appear,
 * which would re-flip an unchanged call. Only the rare left-trim moves one.
 */
function remoteProgressTrail(parts: RemoteLivePart[]): {
    codingFeedMessages: ChatMessage[];
    shownTrail: RemoteTrailItem[];
    lastToolIndex: number;
} {
    const codingFeedMessages: ChatMessage[] = [];
    const trail: RemoteTrailItem[] = [];
    parts.forEach((part, index) => {
        if (isCodingAgentProgressContent(part.text)) {
            codingFeedMessages.push({ id: `remote-live-${index}`, role: "progress", content: part.text, timestamp: index + 1 });
            return;
        }
        const parsed = parseAssistantToolStatus(part.text);
        if (parsed && !isProgressOnlyToolAction(parsed.action)) {
            const tail = trail[trail.length - 1];
            const repeated = tail?.kind === "tool"
                && tail.call.name === parsed.name
                && tail.call.action === parsed.action
                && tail.call.detail === parsed.detail;
            if (!repeated) trail.push({ kind: "tool", call: { id: `remote-trail-tool-${trail.length}`, ...parsed } });
            return;
        }
        trail.push({ kind: "line", text: part.text });
    });
    const shownTrail = trail.slice(-REMOTE_TRAIL_KEPT);
    const lastToolIndex = shownTrail.reduce((found, item, index) => (item.kind === "tool" ? index : found), -1);
    return { codingFeedMessages, shownTrail, lastToolIndex };
}

/**
 * The controlled desktop's plan, drawn with the same bar the local composer
 * uses. It sits above the input, centered; the message stream does not host
 * the rows. Approval stays on the other computer, so this bar has no gate.
 */
export function RemoteCodingPlanChecklist({ session, lang, theme: t }: { session: RemoteAssistantSession; lang: string; theme: Theme }) {
    const steps = remoteWorkbenchSteps(session);
    const understanding = (session.workbench?.requirement_restatement || "").trim();
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
    if (
        (!gateOpen && !failed && !projectionOpen && (codingChecklistAllDone(steps) || (closed && (steps.length > 0 || !!understanding))))
        || (steps.length === 0 && !understanding)
    ) {
        return null;
    }
    const workflowStatus = workflowStatusForTask(session.activity?.workflow, lang);
    // A stored running mark is only "in progress" while this turn is live.
    // Paused, interrupted, and review keep their own outcome on that cursor.
    // The generic in-progress word is not an outcome; a blank label falls
    // back to stopped inside the checklist.
    const heldLabel = stopping
        ? localizeText(lang, "Stopping", "正在停止", "正在停止")
        : (!working && workflowStatus && workflowStatus.tone !== "info" ? workflowStatus.label : "");
    return (
        <CodingAgentPlanChecklist
            lang={lang}
            theme={t}
            chrome={buildCodingBannerChrome({ isDark: !!t.isDark, remote: true, theme: t })}
            steps={steps}
            understanding={understanding}
            executionLive={working}
            executionStatusLabel={heldLabel}
        />
    );
}

/**
 * Live status surface for a remote-control task. The get_task poll carries a
 * workflow snapshot and a heartbeat; the mirrored live round carries the same
 * coding feed / plan checklist the controlled desktop shows, so this panel
 * renders the working trail instead of a bare "working" banner.
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
    // A mirrored round that is still streaming keeps the panel up too — the
    // live working trail arrives over remote_control.stream before the task
    // poll can report the run.
    const liveProgress = (session.live?.parts || []).filter(part => part.kind === "progress" && part.text);
    // Coding-agent progress lines render through the same feed the local
    // panel uses; styled tool-status lines fold into the same tool cards a
    // local turn paints (settled calls as rows, the newest on the split-flap
    // strip while the round streams so it flips like the local bubble);
    // anything else stays a plain mono line.
    const { codingFeedMessages, shownTrail, lastToolIndex } = remoteProgressTrail(liveProgress);
    const codingFeed = codingFeedMessages.length
        ? renderCodingAgentActivityFeed(codingFeedMessages, { text: t.text, fieldLabel: t.fieldLabel, isDark: t.isDark }, lang)
        : null;

    // Step rows live on the composer bar (RemoteCodingPlanChecklist). This
    // panel only names the current step; it does not draw the list.
    const workbench = session.workbench || null;
    const activeStep = remoteWorkbenchSteps(session).find(step => codingStepIsActive(step.status));

    const hasLive = !!session.live && !session.live.responded && session.live.parts.length > 0;
    const showPanel = (busy || paused || review || hasLive || !!workbench?.pending_approval) && !session.loading;
    const [now, setNow] = useState(() => Date.now());
    useEffect(() => {
        // The elapsed/heartbeat meta only renders while busy — ticking in a
        // paused or waiting state would re-render for nobody.
        if (!busy) return;
        const timer = window.setInterval(() => setNow(Date.now()), TICK_BUSY_MS);
        return () => window.clearInterval(timer);
    }, [busy]);

    if (!showPanel) return null;

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
    // A terminal snapshot is the previous run's residue — "正在执行 · 已完成"
    // would read as a contradiction, so only live states make the object slot.
    // The running plan step is more specific than the workflow phase label,
    // so it names the object first, like the local checklist chip does.
    const liveObject = remoteTerminalStatus(workflow?.status)
        ? undefined
        : (activeStep ? `T${activeStep.index} ${activeStep.title}`.trim() : undefined)
            || [workflowStatus?.detail, workflowStatus?.label].filter(Boolean).join(" · ")
            || (activity?.workflowType === "coding" ? localizeText(lang, "Coding task", "编码任务", "編碼任務") : undefined);

    const lastActivityAgo = remoteTimeAgo(lang, activity?.lastActivityAt, now);
    const meta = [
        remoteElapsed(lang, activity?.busySince, now),
        lastActivityAgo
            ? localizeText(lang, `Last activity ${lastActivityAgo}`, `${target}最近活动：${lastActivityAgo}`, `${target}最近活動：${lastActivityAgo}`)
            : "",
    ].filter(Boolean).join(" · ");

    return (
        <div className="aap-remote-activity" data-testid="remote-activity">
            <AssistantReasoningPanel
                live={busy || hasLive}
                // A paused/review row cannot expand (no body); preview keeps
                // the object name visible under the label instead.
                preview={liveObject}
                // Live detail opens by itself — the point of the mirror is
                // seeing the trail, not clicking for it. No liveness gate: a
                // fold at turn end would hide the trail just as it finishes.
                defaultOpen={liveProgress.length > 0}
                label={liveLabel}
                objectLabel={liveObject}
                lang={lang}
                theme={t}
                contentKey={`remote-live|${session.grantId}|${session.projectPath}`}
            >
                {codingFeed}
                {shownTrail.length ? (
                    <div className="aap-remote-progress" data-testid="remote-progress">
                        {shownTrail.map((item, index) => item.kind === "tool"
                            ? (hasLive && index === lastToolIndex
                                // A stable key keeps one mounted strip across
                                // calls: a new call flips the same tile like
                                // the local bubble instead of remounting.
                                ? <AssistantToolFlipTicker key="remote-trail-live-flip" call={item.call} theme={t} />
                                : <AssistantToolCallRow key={item.call.id} call={item.call} theme={t} />)
                            : <div key={`line-${index}`} className="aap-remote-progress-line">{item.text}</div>)}
                    </div>
                ) : null}
            </AssistantReasoningPanel>
            {busy && meta ? (
                <div className="aap-remote-activity-meta" data-testid="remote-activity-meta">{meta}</div>
            ) : null}
        </div>
    );
}

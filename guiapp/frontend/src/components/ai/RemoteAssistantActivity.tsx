import { useEffect, useState } from "react";
import { AssistantReasoningPanel } from "./AssistantReasoningPanel";
import { localizeText } from "../../i18n";
import { workflowStatusForTask } from "../layout/SidebarTaskManagement";
import type { RemoteAssistantSession } from "../layout/remoteAssistantSession";
import type { Theme } from "./aiAssistantPanelTheme";
import { formatProjectSearchTime } from "./projectSearchTime";
import { isCodingAgentProgressContent, renderCodingAgentActivityFeed } from "./CodingAgentProgressStatus";
import {
    buildCodingBannerChrome,
    codingStepGlyph,
    codingStepIsActive,
    codingStepPresentedLabel,
    codingStepStatusColor,
    codingWorkbenchStepsFromPayload,
    presentCodingAgentChecklist,
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
    // panel uses; anything else stays a plain mono line. Seq-based ids keep
    // feed rows mounted while the stream appends.
    const codingFeedMessages: ChatMessage[] = liveProgress
        .filter(part => isCodingAgentProgressContent(part.text))
        .map((part, index) => ({ id: `remote-live-${part.seq || index}`, role: "progress", content: part.text, timestamp: index + 1 }));
    const plainProgress = liveProgress
        .filter(part => !isCodingAgentProgressContent(part.text))
        .map(part => part.text)
        .slice(-8);
    const codingFeed = codingFeedMessages.length
        ? renderCodingAgentActivityFeed(codingFeedMessages, { text: t.text, fieldLabel: t.fieldLabel, isDark: t.isDark }, lang)
        : null;

    // The mirrored workbench checklist: same payload the local
    // coding-workbench-steps event carries, so present the same rows.
    const workbench = session.workbench || null;
    const workbenchSteps = presentCodingAgentChecklist(
        workbench?.execution_plan,
        codingWorkbenchStepsFromPayload(workbench?.step_statuses),
    );
    const hasWorkbench = workbenchSteps.length > 0 || !!workbench?.pending_approval;
    const chrome = buildCodingBannerChrome({ isDark: !!t.isDark, remote: true, theme: t });
    const liveExecution = busy || (!!session.live && !session.live.responded);
    const activeStep = workbenchSteps.find(step => codingStepIsActive(step.status));

    const hasLive = !!session.live && !session.live.responded && session.live.parts.length > 0;
    const showPanel = (busy || paused || review || hasLive || hasWorkbench) && !session.loading;
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
    } else if (workbench?.pending_approval && !hasLive) {
        liveLabel = localizeText(lang, "Plan waiting for approval on the other computer…", "对方任务等待计划确认…", "對方任務等待計畫確認…");
    } else if (activity?.awaitingReply && (!workflowStatus || workflowStatus.tone !== "info")) {
        liveLabel = localizeText(lang, "Sent. Waiting for the other computer to start…", "已发送，等待对方开始执行…", "已發送，等待對方開始執行…");
    } else if (!busy && !hasLive) {
        // A settled checklist is the only reason this panel is up — calling
        // it "正在执行" would lie about an idle task.
        liveLabel = localizeText(lang, "Work plan on the other computer", "对方任务的工作清单", "對方任務的工作清單");
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
            ? localizeText(lang, `Last activity ${lastActivityAgo}`, `对方最近活动：${lastActivityAgo}`, `對方最近活動：${lastActivityAgo}`)
            : "",
    ].filter(Boolean).join(" · ");

    const detailCount = workbenchSteps.length + liveProgress.length + (workbench?.pending_approval ? 1 : 0);
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
                defaultOpen={detailCount > 0}
                label={liveLabel}
                objectLabel={liveObject}
                lang={lang}
                theme={t}
                contentKey={`remote-live|${session.grantId}|${session.projectPath}`}
            >
                {hasWorkbench ? (
                    <div className="aap-remote-workbench" data-testid="remote-workbench-steps">
                        {workbenchSteps.map(step => (
                            <div
                                key={step.index}
                                className="aap-remote-workbench-step"
                                data-status={step.status}
                                style={{ display: "flex", gap: 6, alignItems: "baseline", fontSize: 11, lineHeight: 1.5 }}
                            >
                                <span
                                    aria-hidden="true"
                                    style={{ color: codingStepStatusColor(step.status, chrome), flexShrink: 0, width: 12, textAlign: "center" }}
                                >
                                    {codingStepGlyph(step.status, liveExecution)}
                                </span>
                                <span style={{ color: codingStepStatusColor(step.status, chrome), flex: 1, minWidth: 0 }}>
                                    {`T${step.index} ${step.title}`.trim()}
                                    {step.summary ? ` — ${step.summary}` : ""}
                                    <span style={{ opacity: 0.75 }}>
                                        {` · ${codingStepPresentedLabel(lang, step.status, liveExecution)}`}
                                    </span>
                                </span>
                            </div>
                        ))}
                        {workbench?.pending_approval ? (
                            <div
                                className="aap-remote-workbench-approval"
                                data-testid="remote-workbench-approval"
                                style={{ fontSize: 11, color: chrome.accentStrong, marginTop: 2 }}
                            >
                                {localizeText(lang, "Waiting for plan approval on that computer", "对方电脑上的计划等待确认", "對方電腦上的計畫等待確認")}
                            </div>
                        ) : null}
                    </div>
                ) : null}
                {codingFeed}
                {plainProgress.length ? (
                    <div className="aap-remote-progress" data-testid="remote-progress">
                        {plainProgress.map((line, index) => (
                            <div key={index} className="aap-remote-progress-line">{line}</div>
                        ))}
                    </div>
                ) : null}
            </AssistantReasoningPanel>
            {busy && meta ? (
                <div className="aap-remote-activity-meta" data-testid="remote-activity-meta">{meta}</div>
            ) : null}
        </div>
    );
}

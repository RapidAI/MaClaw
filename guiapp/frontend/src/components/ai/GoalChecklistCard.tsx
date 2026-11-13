/**
 * The 目标清单 card in the assistant transcript (kind: "goalChecklist").
 * Host continuations publish the checklist as plain text lines
 * (目标清单 / ✓ 1. … / → 2. … / 进度：第 x/y 轮); goalChecklistBodyFromText
 * re-parses those lines into structured state so the card reads like the
 * coding plan checklist (CodingAgentPlanChecklist) instead of a monospace
 * glyph dump. A body that parses to nothing recognizable returns null and the
 * caller keeps the legacy pre-wrap card, so unusual stored text never breaks.
 */
import type { PointerEvent as ReactPointerEvent, ReactNode } from "react";
import { localizeText } from "./aiAssistantI18n";

export type GoalStepMark = "done" | "progress" | "failed" | "pending";
export type GoalChecklistStatus = "active" | "complete" | "failed" | "paused" | "stopped";

export type GoalChecklistStep = {
    index: number;
    title: string;
    mark: GoalStepMark;
};

export type GoalChecklistBody = {
    /** Header line from the record ("目标清单" / "Goal checklist"); the card chrome shows its own title. */
    title: string;
    steps: GoalChecklistStep[];
    /** Host continuation narration: 正在分解：… / 正在核对验收 / Continuing: … */
    activity: string;
    status: GoalChecklistStatus;
    /** Reason attached to a terminal status (目标失败：…), empty while active or complete. */
    statusReason: string;
    turns: { current: number; max: number } | null;
    tokens: { used: number; budget: number } | null;
};

const GOAL_TITLE_LINE = /^(?:目标清单|目標清單|Goal checklist)$/i;
const GOAL_STEP_LINE = /^([✓→○×✕✗])?\s*(\d+)\s*[.、]\s*(.+)$/;
const GOAL_ACTIVITY_LINE = /^(?:正在|Continuing(?:$|[:：]?\s))/;
const GOAL_REASON_LINE = /^(目標失敗|目标失败|目标已暂停|目標已暫停|目标已停止|目標已停止)[：:]?\s*(.*)$/;
const GOAL_PROGRESS_LINE = /^进度[：:]\s*第\s*(\d+)\s*[/／]\s*(\d+)\s*轮/;
const GOAL_TOKEN_LINE = /Token[：:]\s*(\d+)\s*[/／]\s*(\d+)/i;
// formatGoalChecklistFromRecord appends "| Token: x/y" to the LAST line when a
// record carries a token budget but no turn cap, so the tail can ride on the
// status, step, activity or turn line. Split it off before classifying.
const GOAL_TOKEN_TAIL = /\s*[|｜]\s*Token[：:]\s*(\d+)\s*[/／]\s*(\d+)\s*$/i;

function goalStepMarkFromGlyph(glyph: string | undefined): GoalStepMark {
    if (glyph === "✓") return "done";
    if (glyph === "→") return "progress";
    if (glyph === "×" || glyph === "✕" || glyph === "✗") return "failed";
    // A bare "N. title" comes from collapsed history snapshots: only the step
    // that announced the turn was marked "→". Everything else is untouched.
    return "pending";
}

/** Parse the published checklist body; null keeps the caller's legacy rendering. */
export function goalChecklistBodyFromText(text: string): GoalChecklistBody | null {
    const lines = String(text || "").replace(/\r\n/g, "\n").split("\n").map((line) => line.trim()).filter(Boolean);
    if (lines.length === 0) return null;
    const body: GoalChecklistBody = {
        title: "",
        steps: [],
        activity: "",
        status: "active",
        statusReason: "",
        turns: null,
        tokens: null,
    };
    let cursor = 0;
    if (GOAL_TITLE_LINE.test(lines[0])) {
        body.title = lines[0];
        cursor = 1;
    }
    for (; cursor < lines.length; cursor++) {
        let line = lines[cursor];
        let inlineTokens: GoalChecklistBody["tokens"] = null;
        const tokenTail = line.match(GOAL_TOKEN_TAIL);
        if (tokenTail) {
            inlineTokens = { used: Number(tokenTail[1]), budget: Number(tokenTail[2]) };
            line = line.slice(0, tokenTail.index ?? line.length).trimEnd();
        }
        const step = line.match(GOAL_STEP_LINE);
        if (step && !GOAL_ACTIVITY_LINE.test(line) && !line.startsWith("进度")) {
            const index = Number(step[2]);
            if (Number.isFinite(index) && index > 0 && step[3].trim()) {
                body.steps.push({ index, title: step[3].trim(), mark: goalStepMarkFromGlyph(step[1]) });
                if (!body.tokens) body.tokens = inlineTokens;
                continue;
            }
        }
        if (GOAL_ACTIVITY_LINE.test(line)) {
            body.activity = line;
            if (!body.tokens) body.tokens = inlineTokens;
            continue;
        }
        if (line === "已完成") {
            body.status = "complete";
            if (!body.tokens) body.tokens = inlineTokens;
            continue;
        }
        const reason = line.match(GOAL_REASON_LINE);
        if (reason) {
            body.status = reason[1].indexOf("失败") >= 0 || reason[1].indexOf("失敗") >= 0
                ? "failed"
                : reason[1].indexOf("暂停") >= 0 || reason[1].indexOf("暫停") >= 0
                    ? "paused"
                    : "stopped";
            body.statusReason = reason[2].trim();
            if (!body.tokens) body.tokens = inlineTokens;
            continue;
        }
        const progress = line.match(GOAL_PROGRESS_LINE);
        if (progress) {
            body.turns = { current: Number(progress[1]), max: Number(progress[2]) };
        }
        const token = line.match(GOAL_TOKEN_LINE);
        if (token) {
            body.tokens = { used: Number(token[1]), budget: Number(token[2]) };
        } else if (!body.tokens) {
            body.tokens = inlineTokens;
        }
        // Unrecognized lines are dropped: the publishers above always write
        // recognized lines, so anything else is legacy residue.
    }
    // Publishes are in order; sorting only guards a reordered legacy snapshot.
    body.steps.sort((left, right) => left.index - right.index);
    const meaningful = body.steps.length > 0
        || body.activity
        || body.status !== "active"
        || !!body.turns
        || !!body.tokens
        || !!body.title;
    if (!meaningful) return null;
    return body;
}

export function goalChecklistDoneCount(steps: GoalChecklistStep[]): number {
    return steps.reduce((done, step) => (step.mark === "done" ? done + 1 : done), 0);
}

/** Codex-style compact stroke glyphs; the row color carries the state. */
function GoalStepIcon({ mark }: { mark: GoalStepMark }) {
    if (mark === "done") {
        return (
            <svg viewBox="0 0 16 16" width="15" height="15" aria-hidden="true">
                <circle cx="8" cy="8" r="6.15" fill="none" stroke="currentColor" strokeWidth="1.3" opacity="0.45" />
                <path d="M5 8.35l2 2 4-4.2" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
        );
    }
    if (mark === "failed") {
        return (
            <svg viewBox="0 0 16 16" width="15" height="15" aria-hidden="true">
                <circle cx="8" cy="8" r="6.15" fill="none" stroke="currentColor" strokeWidth="1.3" opacity="0.45" />
                <path d="M5.7 5.7l4.6 4.6M10.3 5.7l-4.6 4.6" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
            </svg>
        );
    }
    if (mark === "progress") {
        return (
            <svg viewBox="0 0 16 16" width="15" height="15" aria-hidden="true">
                <g className="gkc-spin">
                    <path d="M8 1.85A6.15 6.15 0 1 1 3.65 3.65" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
                    <circle cx="8" cy="8" r="2" fill="currentColor" opacity="0.55" />
                </g>
            </svg>
        );
    }
    return (
        <svg viewBox="0 0 16 16" width="15" height="15" aria-hidden="true">
            <circle cx="8" cy="8" r="6.15" fill="none" stroke="currentColor" strokeWidth="1.3" opacity="0.45" />
            <circle cx="8" cy="8" r="1.8" fill="currentColor" opacity="0.3" />
        </svg>
    );
}

export function GoalTitleGlyph() {
    return (
        <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
            <circle cx="8" cy="8" r="5.6" fill="none" stroke="currentColor" strokeWidth="1.4" />
            <circle cx="8" cy="8" r="2.1" fill="currentColor" opacity="0.6" />
            <path d="M8 1v2.2M8 12.8V15M1 8h2.2M12.8 8H15" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" />
        </svg>
    );
}

export function GoalChecklistCard({ lang, body, headEnd, onHeadPointerDown }: {
    lang?: string;
    body: GoalChecklistBody;
    /** Overlay chrome: extra control pinned at the header's right end (e.g. the float card's collapse button). */
    headEnd?: ReactNode;
    /** Overlay chrome: the header doubles as the floating card's drag handle. */
    onHeadPointerDown?: (event: ReactPointerEvent<HTMLDivElement>) => void;
}) {
    const doneCount = goalChecklistDoneCount(body.steps);
    const percent = body.steps.length > 0
        ? Math.round(Math.min(100, Math.max(0, (doneCount / body.steps.length) * 100)))
        : 0;
    const headerCount = body.steps.length > 1
        ? localizeText(lang, "complete", "完成", "完成")
        : null;
    // Terminal statuses own the chip. A live goal only needs the pulsing
    // "running" badge when the list cannot carry that state itself: either
    // there are no step rows (分解中/核对验收) or the body is the compressed
    // single-step snapshot whose one row is the running step. A full list is
    // navigated by the highlighted row and the progress bar instead.
    const runningChip = body.steps.length === 0 || (body.steps.length === 1 && body.steps[0].mark === "progress");
    const statusChip = body.status === "complete"
        ? { label: localizeText(lang, "Completed", "已完成", "已完成"), tone: "done" as const }
        : body.status === "failed"
            ? { label: localizeText(lang, "Failed", "已失败", "已失敗"), tone: "fail" as const }
            : body.status === "paused"
                ? { label: localizeText(lang, "Paused", "已暂停", "已暫停"), tone: "hold" as const }
                : body.status === "stopped"
                    ? { label: localizeText(lang, "Stopped", "已停止", "已停止"), tone: "hold" as const }
                    : runningChip
                        ? { label: localizeText(lang, "Running", "进行中", "進行中"), tone: "run" as const }
                        : null;
    const showBar = body.steps.length > 0;
    // Only a real failure is red; budget stops and pauses stay neutral amber.
    const reasonTone = body.status === "failed" ? "gkc-reason--fail" : "gkc-reason--hold";
    // A solo activity row (分解中/继续当前步骤) is the whole card; keep it airy.
    const soloActivity = body.activity && body.steps.length === 0 && !body.statusReason;
    return (
        <section className="gkc-card" aria-label={localizeText(lang, "Goal checklist", "目标清单", "目標清單")}>
            <div
                className="gkc-head"
                onPointerDown={onHeadPointerDown}
                style={onHeadPointerDown ? { cursor: "grab", userSelect: "none", touchAction: "none" } : undefined}
            >
                <span className="gkc-title">
                    <span className="gkc-title-icon" aria-hidden="true"><GoalTitleGlyph /></span>
                    {localizeText(lang, "Goal checklist", "目标清单", "目標清單")}
                </span>
                <span className="gkc-head-end">
                    {headerCount ? (
                        <span data-testid="goal-checklist-progress" className="gkc-count">
                            {doneCount}/{body.steps.length} {headerCount}
                            <span className="gkc-count-track" aria-hidden="true">
                                <span className="gkc-count-fill" style={{ width: `${percent}%` }} />
                            </span>
                        </span>
                    ) : null}
                    {statusChip ? (
                        <span className={`gkc-chip gkc-chip--${statusChip.tone}`}>
                            {statusChip.tone === "run" ? <span className="gkc-pulse-dot" aria-hidden="true" /> : null}
                            {statusChip.label}
                        </span>
                    ) : null}
                    {headEnd}
                </span>
            </div>
            {showBar ? (
                <div className="gkc-track" aria-hidden="true">
                    <div className="gkc-fill" style={{ width: `${percent}%` }} />
                </div>
            ) : null}
            {body.activity ? (
                <div className={"gkc-activity" + (soloActivity ? " gkc-activity--solo" : "")}>
                    <svg viewBox="0 0 16 16" width="13" height="13" aria-hidden="true" className="gkc-spin">
                        <path d="M8 1.85A6.15 6.15 0 1 1 3.65 3.65" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" />
                    </svg>
                    <span className="gkc-activity-text">{body.activity}</span>
                </div>
            ) : null}
            {body.steps.length > 0 ? (
                <ol className="gkc-steps">
                    {body.steps.map((step) => (
                        <li
                            key={`${step.index}-${step.title}`}
                            className="gkc-step"
                            data-mark={step.mark}
                            aria-current={step.mark === "progress" ? "step" : undefined}
                        >
                            <span className="gkc-glyph" aria-hidden="true"><GoalStepIcon mark={step.mark} /></span>
                            <span className="gkc-step-idx">{step.index}.</span>
                            <span className="gkc-step-title">{step.title}</span>
                        </li>
                    ))}
                </ol>
            ) : null}
            {body.statusReason && body.status !== "complete" ? (
                <div className={`gkc-reason ${reasonTone}`}>{body.statusReason}</div>
            ) : null}
            {body.turns || body.tokens ? (
                <div className="gkc-meta">
                    {body.turns ? (
                        <span className="gkc-meta-chip">
                            {localizeText(
                                lang,
                                `Round ${body.turns.current}/${body.turns.max}`,
                                `第 ${body.turns.current}/${body.turns.max} 轮`,
                                `第 ${body.turns.current}/${body.turns.max} 輪`,
                            )}
                        </span>
                    ) : null}
                    {body.tokens ? (
                        <span className="gkc-meta-chip">Token {body.tokens.used}/{body.tokens.budget}</span>
                    ) : null}
                </div>
            ) : null}
        </section>
    );
}

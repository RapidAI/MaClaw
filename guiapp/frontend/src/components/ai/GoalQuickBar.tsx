/**
 * Goal controls on the composer, for both chat sessions and the coding agent.
 * The 目标清单 card in the transcript is unchanged. This bar stays until the
 * goal succeeds or the user confirms 中止. Continue re-runs the same goal
 * after any other stop.
 */
import { useCallback, useEffect, useRef, useState, type CSSProperties } from "react";
import { EventsOn } from "../../../wailsjs/runtime";
import { localizeText } from "../../i18n";
import type { Theme } from "./aiAssistantPanelTheme";
import { formatGoalElapsed, goalBarView, type GoalBarSnapshot } from "./goalQuickBarModel";
import { normalizeAssistantSessionKey } from "./aiAssistantPanelSessionUtils";

type Props = {
    lang: string;
    theme: Theme;
    sessionKey: string;
};

type GoalCall = (...args: string[]) => Promise<GoalBarSnapshot>;

type GoalApp = {
    GetSessionGoalBar?: GoalCall;
    PauseSessionGoal?: GoalCall;
    ContinueSessionGoal?: GoalCall;
    CancelSessionGoal?: GoalCall;
    UpdateSessionGoal?: GoalCall;
};

const GOAL_BAR_EVENT = "goal-state-changed";

function goalApp(): GoalApp | null {
    const app = (window as unknown as { go?: { main?: { App?: GoalApp } } }).go?.main?.App;
    return app ?? null;
}

async function callGoal(name: keyof GoalApp, sessionKey: string, extra?: string): Promise<GoalBarSnapshot | null> {
    const fn = goalApp()?.[name];
    if (typeof fn !== "function") return null;
    try {
        const snap = extra === undefined ? await fn(sessionKey) : await fn(sessionKey, extra);
        return snap && typeof snap === "object" ? snap : null;
    } catch {
        return null;
    }
}

function sameGoalSession(eventUser: unknown, sessionKey: string): boolean {
    const left = normalizeAssistantSessionKey(String(eventUser ?? ""));
    const right = normalizeAssistantSessionKey(sessionKey);
    return !!left && !!right && left === right;
}

export function GoalQuickBar({ lang, theme: t, sessionKey }: Props) {
    const tr = useCallback(
        (en: string, zh: string, zhHant: string = zh) => localizeText(lang, en, zh, zhHant),
        [lang],
    );
    const [snap, setSnap] = useState<GoalBarSnapshot | null>(null);
    const [busy, setBusy] = useState(false);
    const [confirming, setConfirming] = useState(false);
    const [editing, setEditing] = useState(false);
    const [draft, setDraft] = useState("");
    const pollTicket = useRef(0);
    const actionTicket = useRef(0);
    const actionInflight = useRef(false);
    const hideEpoch = useRef(0);
    const sessionKeyRef = useRef(sessionKey);
    sessionKeyRef.current = sessionKey;

    const refresh = useCallback(async () => {
        const key = sessionKeyRef.current;
        const poll = ++pollTicket.current;
        const actionAtStart = actionTicket.current;
        const inflightAtStart = actionInflight.current;
        const hideAtStart = hideEpoch.current;
        const next = await callGoal("GetSessionGoalBar", key);
        if (poll !== pollTicket.current || key !== sessionKeyRef.current) return;
        // A click owns the bar until it settles. A read that started after the
        // click can still observe the pre-click goal, so that read is dropped.
        if (actionAtStart !== actionTicket.current || inflightAtStart || actionInflight.current) return;
        if (hideAtStart !== hideEpoch.current) return;
        if (next) setSnap(next);
    }, []);

    useEffect(() => {
        setSnap(null);
        setConfirming(false);
        setEditing(false);
        void refresh();
        const id = window.setInterval(() => void refresh(), 4000);
        const onGoal = (payload: GoalBarSnapshot) => {
            if (!sameGoalSession(payload?.user_id, sessionKeyRef.current)) return;
            const status = String(payload?.status || "").toLowerCase();
            if (payload?.visible === false || status === "cleared" || status === "complete") {
                hideEpoch.current += 1;
                setSnap({ ...payload, visible: false });
                setConfirming(false);
                setEditing(false);
                void refresh();
                return;
            }
            // A payload can be older than a click. Re-read, and drop that read
            // when it overlaps the click or a goal that has already finished.
            void refresh();
        };
        const off = EventsOn(GOAL_BAR_EVENT, onGoal);
        return () => {
            window.clearInterval(id);
            if (typeof off === "function") off();
        };
    }, [refresh, sessionKey]);

    useEffect(() => {
        if (!confirming) return;
        const id = window.setTimeout(() => setConfirming(false), 4000);
        return () => window.clearTimeout(id);
    }, [confirming]);

    const view = goalBarView(snap);
    const [now, setNow] = useState(() => Date.now());
    useEffect(() => {
        if (!view.visible) return;
        const id = window.setInterval(() => setNow(Date.now()), 1000);
        return () => window.clearInterval(id);
    }, [view.visible, snap?.created_at]);
    if (!view.visible) return null;

    const run = async (name: keyof GoalApp, extra?: string) => {
        const action = ++actionTicket.current;
        const hideAtStart = hideEpoch.current;
        actionInflight.current = true;
        setBusy(true);
        setConfirming(false);
        try {
            const next = await callGoal(name, sessionKeyRef.current, extra);
            if (action !== actionTicket.current || hideAtStart !== hideEpoch.current) return;
            if (next) {
                setSnap(next);
                return;
            }
            const fetched = await callGoal("GetSessionGoalBar", sessionKeyRef.current);
            if (action !== actionTicket.current || hideAtStart !== hideEpoch.current) return;
            if (fetched) setSnap(fetched);
        } finally {
            if (action === actionTicket.current) {
                actionInflight.current = false;
                setBusy(false);
                if (hideAtStart !== hideEpoch.current) void refresh();
            }
        }
    };

    const saveDraft = async () => {
        if (!editing) return;
        const next = draft.trim();
        setEditing(false);
        if (!next || next === view.objective) return;
        await run("UpdateSessionGoal", next);
    };

    const labels: Record<typeof view.label, string> = {
        planning: tr("Planning", "分解中", "分解中"),
        executing: tr("Running", "进行中", "進行中"),
        waiting: tr("Waiting", "等待回复", "等待回覆"),
        paused: tr("Goal stalled", "已暂停", "已暫停"),
        "paused-notool": tr("Goal stalled", "已暂停", "已暫停"),
        "paused-remote": tr("Remote offline", "远程未连接", "遠端未連線"),
        budget: tr("Goal stopped", "已停止", "已停止"),
        failed: tr("Goal failed", "已失败", "已失敗"),
    };
    const label = labels[view.label];
    const elapsed = formatGoalElapsed(Number(snap?.created_at) || 0, now);
    const iconBtn = (tone: "idle" | "danger" | "active" = "idle"): CSSProperties => ({
        width: 28,
        height: 28,
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        border: "none",
        borderRadius: 8,
        background: tone === "danger" ? "rgba(212,83,75,0.16)" : tone === "active" ? "rgba(47,95,152,0.12)" : "transparent",
        color: tone === "danger" ? "#d4534b" : (t.textMuted || t.text),
        cursor: busy ? "wait" : "pointer",
        padding: 0,
        flex: "0 0 auto",
    });

    return (
        <div
            data-testid="goal-quick-bar"
            style={{
                display: "flex",
                alignItems: "center",
                gap: 8,
                margin: "0 10px 6px",
                padding: "4px 6px 4px 12px",
                minHeight: 36,
                minWidth: 0,
                boxSizing: "border-box",
                borderRadius: 14,
                border: `1.5px solid ${t.inputBarBorder || t.divider}`,
                background: t.inputBarBg || t.bg,
                color: t.textMuted || t.text,
                fontSize: 12,
                lineHeight: 1.3,
                flexShrink: 0,
            }}
        >
            <span
                data-testid="goal-bar-label"
                title={view.reason || label}
                style={{ fontWeight: 600, color: t.headingColor || t.text, flex: "0 0 auto", whiteSpace: "nowrap" }}
            >
                {label}
            </span>
            {editing ? (
                <input
                    data-testid="goal-bar-edit-input"
                    value={draft}
                    aria-label={tr("Goal command", "目标命令", "目標命令")}
                    onChange={event => setDraft(event.target.value)}
                    onKeyDown={event => {
                        if (event.key === "Enter") {
                            event.preventDefault();
                            void saveDraft();
                        } else if (event.key === "Escape") {
                            event.preventDefault();
                            setEditing(false);
                        }
                    }}
                    style={{
                        flex: "1 1 80px",
                        minWidth: 0,
                        height: 26,
                        border: "none",
                        outline: "none",
                        background: "transparent",
                        color: t.text,
                        font: "inherit",
                        padding: 0,
                    }}
                    autoFocus
                />
            ) : (
                <span
                    data-testid="goal-bar-objective"
                    title={view.objective}
                    style={{
                        flex: "1 1 80px",
                        minWidth: 0,
                        overflow: "hidden",
                        textOverflow: "ellipsis",
                        whiteSpace: "nowrap",
                    }}
                >
                    {view.objective}
                </span>
            )}
            {elapsed ? (
                <span data-testid="goal-bar-elapsed" style={{ flex: "0 0 auto", whiteSpace: "nowrap" }}>· {elapsed}</span>
            ) : null}
            <span style={{ display: "inline-flex", alignItems: "center", gap: 2, flex: "0 0 auto" }}>
                <button
                    type="button"
                    data-testid="goal-bar-cancel"
                    disabled={busy}
                    aria-label={confirming
                        ? tr("Confirm delete", "确认删除", "確認刪除")
                        : tr("Delete goal", "删除目标", "刪除目標")}
                    title={confirming
                        ? tr("Click again to delete this goal", "再点一次删除这个目标", "再點一次刪除這個目標")
                        : tr("Delete goal", "删除目标", "刪除目標")}
                    onClick={() => {
                        setEditing(false);
                        if (!confirming) {
                            setConfirming(true);
                            return;
                        }
                        void run("CancelSessionGoal");
                    }}
                    style={iconBtn(confirming ? "danger" : "idle")}
                >
                    <TrashIcon />
                </button>
                {view.running ? (
                    <button
                        type="button"
                        data-testid="goal-bar-pause"
                        disabled={busy}
                        aria-label={tr("Pause", "暂停", "暫停")}
                        title={tr("Pause this goal", "暂停这个目标", "暫停這個目標")}
                        onClick={() => { void saveDraft().then(() => run("PauseSessionGoal")); }}
                        style={iconBtn()}
                    >
                        <PauseIcon />
                    </button>
                ) : (
                    <button
                        type="button"
                        data-testid="goal-bar-continue"
                        disabled={busy}
                        aria-label={tr("Continue", "继续", "繼續")}
                        title={tr("Continue this goal", "继续执行这个目标", "繼續執行這個目標")}
                        onClick={() => { void saveDraft().then(() => run("ContinueSessionGoal")); }}
                        style={iconBtn()}
                    >
                        <PlayIcon />
                    </button>
                )}
                <button
                    type="button"
                    data-testid="goal-bar-edit"
                    disabled={busy}
                    aria-label={editing ? tr("Save goal", "保存目标", "儲存目標") : tr("Edit goal", "编辑目标", "編輯目標")}
                    title={editing ? tr("Save goal command", "保存目标命令", "儲存目標命令") : tr("Edit goal command", "编辑目标命令", "編輯目標命令")}
                    aria-pressed={editing}
                    onClick={() => {
                        setConfirming(false);
                        if (editing) {
                            void saveDraft();
                            return;
                        }
                        setDraft(view.objective);
                        setEditing(true);
                    }}
                    style={iconBtn(editing ? "active" : "idle")}
                >
                    <EditIcon />
                </button>
            </span>
        </div>
    );
}

function TrashIcon() {
    return (
        <svg width="15" height="15" viewBox="0 0 16 16" aria-hidden="true">
            <path d="M3.2 4.6h9.6M6.4 4.6V3.2h3.2v1.4M4.4 4.6l.55 8.1h6.1l.55-8.1" fill="none" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
    );
}

function PauseIcon() {
    return (
        <svg width="15" height="15" viewBox="0 0 16 16" aria-hidden="true">
            <path d="M5.2 3.4h1.8v9.2H5.2zM9 3.4h1.8v9.2H9z" fill="currentColor" />
        </svg>
    );
}

function PlayIcon() {
    return (
        <svg width="15" height="15" viewBox="0 0 16 16" aria-hidden="true">
            <path d="M5.2 3.3v9.4l7-4.7-7-4.7z" fill="currentColor" />
        </svg>
    );
}

function EditIcon() {
    return (
        <svg width="15" height="15" viewBox="0 0 16 16" aria-hidden="true">
            <path d="M9.2 2.8l4 4L6.2 13.8H2.2v-4L9.2 2.8z" fill="none" stroke="currentColor" strokeWidth="1.3" strokeLinejoin="round" />
        </svg>
    );
}

export default GoalQuickBar;

import { Fragment, type ReactNode } from "react";

/** The stage assignment that stays above a goal stage's process list and report. */
export function GoalStageCommand({ text }: { text?: string }): ReactNode {
    const command = (text || "").trim();
    if (!command) return null;
    return <div className="goal-stage-command" data-testid="goal-stage-command">{command}</div>;
}

/** Puts the stage command above a reply that has no process list yet. */
export function withGoalStageCommand(id: string, command: string | undefined, node: ReactNode): ReactNode {
    const text = (command || "").trim();
    if (!text) return node;
    return <Fragment key={id}><GoalStageCommand text={text} />{node}</Fragment>;
}

import { useEffect, useState } from "react";
import { companionNoteStatus, formatNoteElapsed, noteStatusPercent, type NoteMedia } from "./noteStatusModel";

export function CompanionStatus({ text, kind, noteStartedAt, phaseStartedAt }: {
    text: string;
    kind: NoteMedia;
    noteStartedAt?: number;
    phaseStartedAt?: number;
}) {
    const model = companionNoteStatus(text, kind);
    const active = model != null;
    const [now, setNow] = useState(() => Date.now());
    useEffect(() => {
        if (!active) return;
        setNow(Date.now());
        const id = window.setInterval(() => setNow(Date.now()), 1000);
        return () => window.clearInterval(id);
    }, [active]);

    if (!model) {
        return <div data-testid="file-companion-progress" className="file-companion-progress">{text}</div>;
    }
    // Times belong to the document. This card only paints them, so a new phrase
    // is at its own start on the same frame the sentence changes.
    const noteStart = noteStartedAt && noteStartedAt > 0 ? noteStartedAt : now;
    const phaseStart = phaseStartedAt && phaseStartedAt > 0 ? phaseStartedAt : noteStart;
    const elapsed = Math.max(0, now - noteStart);
    const phaseElapsed = Math.max(0, now - phaseStart);
    const percent = noteStatusPercent(model, phaseElapsed);
    return (
        <div className="file-companion-note-status" data-testid="file-companion-progress">
            <div className="file-companion-note-status-head">
                <span className="file-companion-note-status-kicker">笔记</span>
                <span className="file-companion-note-status-time" data-testid="file-companion-note-elapsed">{formatNoteElapsed(elapsed)}</span>
            </div>
            <p className="file-companion-note-status-title">{model.title}</p>
            <div
                className="file-companion-note-status-track"
                role="progressbar"
                aria-label="笔记进度"
                aria-valuemin={0}
                aria-valuemax={100}
                aria-valuenow={percent}
                aria-valuetext={model.title}
            >
                <span
                    className={model.precise ? "file-companion-note-status-fill is-precise" : "file-companion-note-status-fill is-live"}
                    style={{ width: `${percent}%` }}
                />
            </div>
            <ol className="file-companion-note-status-steps">
                {model.steps.map((step, index) => {
                    const state = index < model.index ? "is-done" : index === model.index ? "is-current" : "";
                    return (
                        <li key={step.id} className={state} aria-current={index === model.index ? "step" : undefined}>{step.label}</li>
                    );
                })}
            </ol>
        </div>
    );
}

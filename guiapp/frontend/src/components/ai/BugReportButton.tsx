import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { CaptureGUIBugReportScreenshot, DiscardGUIBugReportScreenshot, SubmitGUIBugReport } from "../../../wailsjs/go/main/App";
import { useModalFocusTrap } from "../../hooks/useModalFocusTrap";
import { localizeText } from "./aiAssistantI18n";
import { useDialog } from "../CustomDialog";

/** HubCenter and SubmitGUIBugReport both cap the description at 32 KB of UTF-8. */
export const GUI_BUG_REPORT_DESCRIPTION_BYTES = 32 * 1024;

// The textarea maxLength counts UTF-16 code units. A Chinese description of
// that many characters is about 96 KB and is rejected after the user clicks
// Send. Clamp here so the text they can submit is the text the server accepts.
// While an IME composition is open the draft must stay intact. Clamping on
// each input event cuts the character being composed once the text crosses
// 32 KB, which is exactly when a Chinese description hits the cap.
export function bugReportDescriptionDraft(value: string, composing: boolean): string {
    if (composing) return value;
    return clampBugReportDescription(value);
}

// A composition can leave the draft over 32 KB, because the textarea is not
// clamped until composition ends. Sending that draft would upload a shorter
// string than the one on screen. Null asks the caller to show the cap and
// wait for another click once the draft itself fits.
export function bugReportSubmitDescription(value: string): string | null {
    const limited = bugReportDescriptionDraft(value, false);
    if (limited !== value) return null;
    const description = limited.trim();
    if (!description) return null;
    return description;
}

export function clampBugReportDescription(value: string, maxBytes = GUI_BUG_REPORT_DESCRIPTION_BYTES): string {
    const encoder = new TextEncoder();
    if (encoder.encode(value).byteLength <= maxBytes) return value;
    const chars = Array.from(value);
    let low = 0;
    let high = chars.length;
    while (low < high) {
        const mid = Math.ceil((low + high) / 2);
        if (encoder.encode(chars.slice(0, mid).join("")).byteLength <= maxBytes) low = mid;
        else high = mid - 1;
    }
    return chars.slice(0, low).join("");
}

type BugReportPhase = "idle" | "capture" | "edit" | "send";

function BugIcon() {
    return (
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <path d="m8 2 1.88 1.88" />
            <path d="M14.12 3.88 16 2" />
            <path d="M9 7.13v-1a3.003 3.003 0 1 1 6 0v1" />
            <path d="M12 20c-3.3 0-6-2.7-6-6v-3a4 4 0 0 1 4-4h4a4 4 0 0 1 4 4v3c0 3.3-2.7 6-6 6" />
            <path d="M12 20v-9" />
            <path d="M6.53 9C4.6 8.8 3 7.1 3 5" />
            <path d="M6 13H2" />
            <path d="M3 21c0-2.1 1.7-3.9 3.8-4" />
            <path d="M20.97 5c0 2.1-1.6 3.8-3.5 4" />
            <path d="M22 13h-4" />
            <path d="M17.2 17c2.1.1 3.8 1.9 3.8 4" />
        </svg>
    );
}

export function BugReportButton({ lang }: { lang: string }) {
    const { showAlert } = useDialog();
    const phase = useRef<BugReportPhase>("idle");
    // Pointer-down on Send blurs the textarea, and the browser ends IME
    // composition before click. That commits a clamp into state, so the click
    // would upload a shorter description than the one just composed. The
    // rejection is tied to that gesture id. An abandoned press must not reject
    // a later press: each pointer-down advances the id.
    const sendGesture = useRef(0);
    const armedGesture = useRef(0);
    const rejectedGesture = useRef(0);
    // Token of the capture this dialog owns. Close passes that token to
    // Discard. A newer capture has a different token, so a discard that
    // arrives after the next shot is stored cannot delete it.
    const shotToken = useRef("");
    const alive = useRef(true);
    const [open, setOpen] = useState(false);
    const [text, setText] = useState("");
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState("");
    const title = localizeText(lang, "Bug report", "故障报告", "故障報告");
    const sendLabel = localizeText(lang, "Send report", "发送报告", "發送報告");
    const tooLong = localizeText(
        lang,
        "The description is limited to 32 KB.",
        "故障描述不能超过 32 KB。",
        "故障描述不能超過 32 KB。",
    );

    useEffect(() => {
        alive.current = true;
        return () => {
            alive.current = false;
            // Send owns the file until Submit returns. Every other phase
            // still has the capture only because this button is on screen.
            if (phase.current === "send") return;
            const token = shotToken.current;
            shotToken.current = "";
            phase.current = "idle";
            if (token) void DiscardGUIBugReportScreenshot(token);
        };
    }, []);

    const openReport = async () => {
        // busy is React state, so a second click in the same turn still sees
        // the old value. The phase ref is the session lock: one capture, and
        // no second shot after the dialog is open (that shot would include
        // the dialog and wipe the description).
        if (phase.current !== "idle") return;
        phase.current = "capture";
        setBusy(true);
        setError("");
        try {
            const token = await CaptureGUIBugReportScreenshot();
            if (!alive.current || phase.current !== "capture") {
                if (token) void DiscardGUIBugReportScreenshot(token);
                return;
            }
            shotToken.current = token || "";
            phase.current = "edit";
            setText("");
            setOpen(true);
        } catch (err: any) {
            phase.current = "idle";
            if (!alive.current) return;
            await showAlert(String(err?.message || err), title);
        } finally {
            if (alive.current) setBusy(false);
        }
    };

    const closeReport = () => {
        if (phase.current === "capture" || phase.current === "send") return;
        phase.current = "idle";
        const token = shotToken.current;
        shotToken.current = "";
        setOpen(false);
        setError("");
        if (token) void DiscardGUIBugReportScreenshot(token);
    };

    const commitDescription = (value: string, composing: boolean) => {
        const next = bugReportDescriptionDraft(value, composing);
        setText(next);
        if (composing) return;
        if (next !== value) {
            if (armedGesture.current !== 0) rejectedGesture.current = armedGesture.current;
            setError(tooLong);
        } else setError((current) => (current === tooLong ? "" : current));
    };

    const sendReport = async () => {
        const gesture = sendGesture.current;
        const truncatedByThisGesture = rejectedGesture.current === gesture && gesture !== 0;
        rejectedGesture.current = 0;
        armedGesture.current = 0;
        if (truncatedByThisGesture) {
            // This click is the one that ended the composition. The box now
            // shows the shortened text; wait for another click to send it.
            setError(tooLong);
            return;
        }
        const description = bugReportSubmitDescription(text);
        if (description === null) {
            const limited = bugReportDescriptionDraft(text, false);
            if (limited !== text) {
                setText(limited);
                setError(tooLong);
            }
            return;
        }
        if (phase.current !== "edit") return;
        phase.current = "send";
        setBusy(true);
        setError("");
        try {
            const report = await SubmitGUIBugReport(description);
            phase.current = "idle";
            // Submit removed the file. This dialog no longer owns a capture,
            // so it must not discard that token after the next shot is stored.
            shotToken.current = "";
            if (!alive.current) return;
            setOpen(false);
            const id = String((report as { id?: string })?.id || "");
            await showAlert(
                id
                    ? localizeText(lang, `Report ${id} submitted.`, `报告 ${id} 已发送。`, `報告 ${id} 已發送。`)
                    : localizeText(lang, "Report submitted.", "报告已发送。", "報告已發送。"),
                title,
            );
        } catch (err: any) {
            if (!alive.current) {
                phase.current = "idle";
                const token = shotToken.current;
                shotToken.current = "";
                if (token) void DiscardGUIBugReportScreenshot(token);
                return;
            }
            phase.current = "edit";
            setError(String(err?.message || err));
        } finally {
            if (alive.current) setBusy(false);
        }
    };

    return (
        <>
            <button
                className="task-bug-report-btn"
                data-testid="task-bug-report-btn"
                type="button"
                onClick={() => { void openReport(); }}
                disabled={busy || open}
                aria-busy={busy}
                aria-label={title}
                title={title}
            >
                <BugIcon />
            </button>
            {open ? (
                <BugReportDialog
                    lang={lang}
                    title={title}
                    sendLabel={sendLabel}
                    text={text}
                    busy={busy}
                    error={error}
                    onClose={closeReport}
                    onCommit={commitDescription}
                    onArmSend={() => {
                        sendGesture.current += 1;
                        armedGesture.current = sendGesture.current;
                    }}
                    onDisarmSend={() => {
                        // Pointer-up is after compositionend for a real click.
                        // Clearing the arm there keeps a later composition
                        // from being treated as a click that never happened.
                        // Pointer-leave is too early: it can run before that
                        // compositionend, and the click would then upload the
                        // shortened text.
                        armedGesture.current = 0;
                    }}
                    onSend={() => { void sendReport(); }}
                />
            ) : null}
        </>
    );
}

// Mounted only while the dialog is open. The focus trap's first effect is
// mount-only, so it has to live here rather than on the always-mounted button.
function BugReportDialog({
    lang,
    title,
    sendLabel,
    text,
    busy,
    error,
    onClose,
    onCommit,
    onArmSend,
    onDisarmSend,
    onSend,
}: {
    lang: string;
    title: string;
    sendLabel: string;
    text: string;
    busy: boolean;
    error: string;
    onClose: () => void;
    onCommit: (value: string, composing: boolean) => void;
    onArmSend: () => void;
    onDisarmSend: () => void;
    onSend: () => void;
}) {
    const dialogRef = useRef<HTMLDivElement>(null);
    const descriptionRef = useRef<HTMLTextAreaElement>(null);
    const { onKeyDown } = useModalFocusTrap(dialogRef, { onEscape: onClose, initialFocusRef: descriptionRef });
    const host = document.getElementById("App") ?? document.body;
    return createPortal(
        <div className="modal-overlay" role="presentation" onMouseDown={(event) => {
            if (event.target === event.currentTarget) onClose();
        }}>
            <div
                ref={dialogRef}
                className="modal-content problem-report-dialog gui-bug-report-dialog"
                role="dialog"
                aria-modal="true"
                aria-labelledby="gui-bug-report-title"
                tabIndex={-1}
                onKeyDown={onKeyDown}
            >
                <div className="problem-report-dialog__header">
                    <h3 id="gui-bug-report-title">{title}</h3>
                    <button className="modal-close" type="button" disabled={busy} onClick={onClose} aria-label={localizeText(lang, "Close", "关闭", "關閉")}>&times;</button>
                </div>
                <label htmlFor="gui-bug-report-description">{localizeText(lang, "Problem description", "故障描述", "故障描述")}</label>
                <textarea
                    ref={descriptionRef}
                    id="gui-bug-report-description"
                    value={text}
                    disabled={busy}
                    maxLength={GUI_BUG_REPORT_DESCRIPTION_BYTES}
                    placeholder={localizeText(lang, "What happened, and what did you expect?", "请描述发生了什么，以及你期望的结果。", "請描述發生了什麼，以及你期望的結果。")}
                    onChange={(event) => {
                        const native = event.nativeEvent as InputEvent;
                        onCommit(event.target.value, native.isComposing);
                    }}
                    onCompositionEnd={(event) => {
                        onCommit(event.currentTarget.value, false);
                    }}
                />
                {error ? <div className="problem-report-dialog__message" role="alert">{error}</div> : null}
                <div className="modal-actions problem-report-dialog__actions">
                    <button className="btn-hide" type="button" disabled={busy} onClick={onClose}>{localizeText(lang, "Cancel", "取消", "取消")}</button>
                    <button
                        className="btn-primary"
                        type="button"
                        disabled={busy || !text.trim()}
                        onPointerDown={(event) => {
                            onArmSend();
                            // Capture so pointer-up still arrives after the
                            // pointer slides off the button. Leave must not
                            // end the gesture: compositionend follows the
                            // press and has to see the arm.
                            try {
                                event.currentTarget.setPointerCapture(event.pointerId);
                            } catch {
                                // The press still ends on pointer-up when it stays on the button.
                            }
                        }}
                        onPointerUp={(event) => {
                            try {
                                if (typeof event.currentTarget.hasPointerCapture === "function" && event.currentTarget.hasPointerCapture(event.pointerId)) {
                                    event.currentTarget.releasePointerCapture(event.pointerId);
                                }
                            } catch {
                                // Releasing an uncaptured pointer is not part of the gesture.
                            }
                            onDisarmSend();
                        }}
                        onPointerCancel={onDisarmSend}
                        onClick={onSend}
                    >
                        {busy ? localizeText(lang, "Sending…", "正在发送…", "正在發送…") : sendLabel}
                    </button>
                </div>
            </div>
        </div>,
        host,
    );
}

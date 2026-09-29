import { localizeText } from "./aiAssistantI18n";
import { formFieldInputStyle, formFieldLabelColor, primaryFilledButtonStyle, type Theme } from "./aiAssistantPanelTheme";

interface SaveTaskDialogProps {
    lang?: string;
    theme: Theme;
    taskName: string;
    onTaskNameChange: (value: string) => void;
    saving: boolean;
    onClose: () => void;
    onSubmit: () => void;
}

/** "Save as Task" modal: persists the current main conversation as a resumable task. */
export function SaveTaskDialog({ lang, theme: t, taskName, onTaskNameChange, saving, onClose, onSubmit }: SaveTaskDialogProps) {
    return (
        <div className="aap-save-backdrop" data-testid="save-task-dialog-backdrop" onMouseDown={event => { if (event.target === event.currentTarget && !saving) onClose(); }}>
            <form role="dialog" aria-modal="true" aria-labelledby="save-task-dialog-title" onSubmit={event => { event.preventDefault(); void onSubmit(); }} style={{ width: 390, maxWidth: "calc(100vw - 32px)", background: t.titleBarBg, border: `1px solid ${t.titleBarBorder}`, borderRadius: 14, boxShadow: (t.bg.startsWith("#0") || t.bg.startsWith("#1") || t.bg.startsWith("#2")) ? "0 1px 2px rgba(0, 0, 0, 0.30), 0 4px 12px -2px rgba(0, 0, 0, 0.40)" : "0 1px 2px rgba(30, 58, 95, 0.05), 0 4px 12px -2px rgba(30, 58, 95, 0.10)", color: t.text, overflow: "hidden" }} onMouseDown={event => event.stopPropagation()}>
                <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12, padding: "12px 14px", borderBottom: `1px solid ${t.titleBarBorder}` }}>
                    <h3 id="save-task-dialog-title" style={{ margin: 0, fontSize: 14, fontWeight: 700, color: t.text }}>{localizeText(lang, "Save as Task", "\u4fdd\u5b58\u4e3a\u4efb\u52a1", "\u4fdd\u5b58\u70ba\u4efb\u52d9")}</h3>
                    <button type="button" disabled={saving} onClick={onClose} style={{ border: "none", background: "transparent", color: t.text, opacity: 0.62, cursor: saving ? "default" : "pointer", fontSize: 14, lineHeight: 1 }}>x</button>
                </div>
                <div className="aap-save-body">
                    <label htmlFor="save-task-name" style={{ fontSize: 12, fontWeight: 700, color: formFieldLabelColor(t) }}>{localizeText(lang, "Task name", "\u4efb\u52a1\u540d\u79f0", "\u4efb\u52d9\u540d\u7a31")}</label>
                    <input id="save-task-name" autoFocus value={taskName} disabled={saving} onChange={event => onTaskNameChange(event.target.value)} onKeyDown={event => { if (event.key === "Escape" && !saving) onClose(); }} style={{ width: "100%", boxSizing: "border-box", borderRadius: 6, fontSize: 13, padding: "7px 9px", fontFamily: "inherit", ...formFieldInputStyle(t) }} />
                    <p style={{ margin: "4px 0 0", fontSize: 12, lineHeight: 1.45, color: formFieldLabelColor(t) }}>{localizeText(lang, "The current main conversation history and task context will be saved. Double-click it in Task Management to continue in a separate tab.", "\u5c06\u4fdd\u5b58\u5f53\u524d\u4e3b\u5bf9\u8bdd\u5386\u53f2\u548c\u4efb\u52a1\u4e0a\u4e0b\u6587\u3002\u4e4b\u540e\u53ef\u5728\u4efb\u52a1\u7ba1\u7406\u4e2d\u53cc\u51fb\uff0c\u4ee5\u72ec\u7acb Tab \u7ee7\u7eed\u3002", "\u5c07\u4fdd\u5b58\u76ee\u524d\u4e3b\u5c0d\u8a71\u6b77\u53f2\u548c\u4efb\u52d9\u4e0a\u4e0b\u6587\u3002\u4e4b\u5f8c\u53ef\u5728\u4efb\u52d9\u7ba1\u7406\u4e2d\u96d9\u64ca\uff0c\u4ee5\u7368\u7acb Tab \u7e7c\u7e8c\u3002")}</p>
                </div>
                <div style={{ display: "flex", justifyContent: "flex-end", gap: 8, padding: "10px 14px 12px", borderTop: `1px solid ${t.titleBarBorder}` }}>
                    <button type="button" disabled={saving} onClick={onClose} style={{ border: `1px solid ${t.titleBarBorder}`, borderRadius: 6, background: t.fieldBg, color: t.text, cursor: saving ? "default" : "pointer", fontSize: 12, padding: "5px 12px" }}>{localizeText(lang, "Cancel", "\u53d6\u6d88", "\u53d6\u6d88")}</button>
                    <button type="submit" disabled={saving || !taskName.trim()} style={primaryFilledButtonStyle(t, { borderRadius: 6, cursor: saving || !taskName.trim() ? "default" : "pointer", opacity: saving || !taskName.trim() ? 0.62 : 1, fontSize: 12, padding: "5px 12px" })}>{saving ? localizeText(lang, "Saving...", "\u4fdd\u5b58\u4e2d...", "\u4fdd\u5b58\u4e2d...") : localizeText(lang, "Save", "\u4fdd\u5b58", "\u4fdd\u5b58")}</button>
                </div>
            </form>
        </div>
    );
}

import type { Theme } from "./aiAssistantPanelTheme";
import { primaryFilledButtonStyle } from "./aiAssistantPanelTheme";
import { localizeText } from "./aiAssistantI18n";
import { ClearCodingWorkbenchConflictLog } from "../../../wailsjs/go/main/App";

/**
 * Mini conflict-log strip shown in the coding control area only while the
 * full conflict side panel is closed (the side panel carries the full log).
 */
export function CodingConflictLogStrip({ conflictLog, activeProjectPath, lang, t, onExport, onCleared }: {
    conflictLog: string[];
    activeProjectPath: string;
    lang?: string;
    t: Theme;
    onExport: () => void;
    onCleared: () => void;
}) {
    if (conflictLog.length <= 0) return null;
    return (
        <div data-testid="coding-conflict-log" style={{ marginTop: 4, fontSize: 10, color: t.textMuted || t.promptColor, maxHeight: 56, overflow: "auto", opacity: 0.9 }}>
            <div className="aap-log-head">
                <span className="aap-strong">{localizeText(lang, "Conflict log", "冲突日志", "衝突日誌")}</span>
                <span className="aap-log-actions">
                    <button
                        type="button"
                        data-testid="coding-conflict-log-export"
                        onClick={onExport}
                        title={localizeText(lang, "Export log into worktree notes", "导出日志到 worktree notes", "匯出日誌到 worktree notes")}
                        style={{ border: "none", background: "transparent", color: t.headingColor || t.btnColor || t.textMuted, fontSize: 10, cursor: "pointer", padding: 0 }}
                    >
                        {localizeText(lang, "Export", "导出", "匯出")}
                    </button>
                    <button
                        type="button"
                        data-testid="coding-conflict-log-clear"
                        onClick={() => {
                            if (!activeProjectPath) return;
                            void ClearCodingWorkbenchConflictLog(activeProjectPath).then(onCleared).catch(() => { /* ignore */ });
                        }}
                        style={{ border: "none", background: "transparent", color: t.textMuted, fontSize: 10, cursor: "pointer", padding: 0 }}
                    >
                        {localizeText(lang, "Clear", "清空", "清空")}
                    </button>
                </span>
            </div>
            {conflictLog.slice().reverse().slice(0, 4).map((line, i) => (
                <div className="aap-log-line" key={`${i}-${line.slice(0, 24)}`}>{line}</div>
            ))}
        </div>
    );
}

/** Execution plan card: editable draft while pending approval, read-only after. */
export function CodingExecutionPlanCard({ executionPlan, pendingApproval, pendingPlanEditing, pendingPlanSaving, pendingPlanDraft, setPendingPlanDraft, onSavePlanEdit, lang, t }: {
    executionPlan: string;
    pendingApproval: boolean;
    pendingPlanEditing: boolean;
    pendingPlanSaving: boolean;
    pendingPlanDraft: string;
    setPendingPlanDraft: (v: string) => void;
    onSavePlanEdit: () => void;
    lang?: string;
    t: Theme;
}) {
    return (
        <div data-testid="coding-execution-plan" style={{ marginTop: 4, padding: "6px 8px", borderRadius: 6, border: `1px solid ${pendingApproval ? "#dc262655" : (t.fieldBorder || "rgba(127,127,127,0.25)")}`, background: t.fieldBg || "transparent", fontSize: 11, color: t.textMuted || t.promptColor, lineHeight: 1.35 }}>
            <div className="aap-plan-title-row">
                <div style={{ fontWeight: 600, color: t.headingColor || t.btnColor || t.text }}>
                    {localizeText(lang, "Execution plan", "执行计划", "執行計畫")}
                    {pendingApproval ? ` · ${localizeText(lang, "pending", "待批准", "待批准")}` : ""}
                </div>
                {pendingApproval && pendingPlanEditing ? (
                    <button
                        type="button"
                        data-testid="coding-plan-edit-save"
                        disabled={pendingPlanSaving || !pendingPlanDraft.trim()}
                        onClick={onSavePlanEdit}
                        style={primaryFilledButtonStyle(t, { height: 20, padding: "0 8px", borderRadius: 4, fontSize: 10, cursor: (pendingPlanSaving || !pendingPlanDraft.trim()) ? "not-allowed" : "pointer", opacity: pendingPlanDraft.trim() ? 1 : 0.5 })}
                    >
                        {pendingPlanSaving
                            ? localizeText(lang, "Saving…", "保存中…", "儲存中…")
                            : localizeText(lang, "Save plan", "保存计划", "儲存計畫")}
                    </button>
                ) : null}
            </div>
            {pendingApproval && pendingPlanEditing ? (
                <textarea
                    data-testid="coding-pending-plan-draft"
                    value={pendingPlanDraft}
                    onChange={(e) => setPendingPlanDraft(e.target.value)}
                    rows={8}
                    spellCheck={false}
                    placeholder={localizeText(lang, "T1: …\nT2: …", "T1: …\nT2: …", "T1: …\nT2: …")}
                    style={{
                        width: "100%",
                        boxSizing: "border-box",
                        margin: 0,
                        fontSize: 11,
                        fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
                        lineHeight: 1.35,
                        resize: "vertical",
                        minHeight: 100,
                        maxHeight: 220,
                        border: `1px solid ${t.fieldBorder || "rgba(127,127,127,0.3)"}`,
                        borderRadius: 4,
                        padding: 6,
                        background: "transparent",
                        color: t.text || t.promptColor,
                    }}
                />
            ) : (
                <div className="aap-plan-text">{executionPlan}</div>
            )}
        </div>
    );
}

import type { Theme } from "./aiAssistantPanelTheme";
import { chromeText } from "../../i18n/chromeText";


export function WorkflowReviewInlinePrompt({
    lang,
    onAbort,
    onConfirm,
    onRequestRevision,
    onViewDocument,
    phaseName,
    theme: t,
}: {
    lang?: string;
    onAbort: () => void;
    onConfirm: () => void;
    onRequestRevision: () => void;
    onViewDocument: () => void;
    phaseName: string;
    theme: Theme;
}) {
    const resolvedPhaseName = phaseName.trim();
    const title = resolvedPhaseName
        ? chromeText(lang, "{phase} is waiting for review", "当前「{phase}」等待确认").replace("{phase}", resolvedPhaseName)
        : chromeText(lang, "Current phase is waiting for review", "当前阶段等待确认");
    const description = chromeText(lang, "This is not stopped. Review the workflow document and choose how to continue.", "这不是停止状态。请查看工作流文档，并选择继续推进或补充修改。");
    const viewLabel = (chromeText(lang, "View document", "查看文档"));
    const confirmLabel = (chromeText(lang, "Confirm & proceed", "确认并推进"));
    const reviseLabel = (chromeText(lang, "Provide feedback", "输入补充/修改意见"));
    const abortLabel = (chromeText(lang, "Abort", "中止"));
    const baseButtonStyle = {
        borderRadius: 999,
        border: `1px solid ${t.titleBarBorder}`,
        cursor: "pointer",
        fontSize: 12,
        fontWeight: 700,
        padding: "6px 10px",
    } as const;

    return (
        <div
            aria-live="polite"
            data-testid="workflow-review-inline-prompt"
            style={{
                flexShrink: 0,
                padding: "9px 10px 10px",
                borderTop: `1px solid ${t.inputBarBorder}`,
                background: `linear-gradient(135deg, color-mix(in srgb, ${t.headingColor} 12%, ${t.inputBarBg}) 0%, ${t.inputBarBg} 72%)`,
                color: t.text,
            }}
        >
            <div className="wip-head">
                <div className="wip-text">
                    <div className="wip-title">{title}</div>
                    <div style={{ color: t.textMuted, fontSize: 12 }}>{description}</div>
                </div>
                <div className="wip-actions">
                    <button
                        type="button"
                        onClick={onViewDocument}
                        style={{
                            ...baseButtonStyle,
                            background: t.fieldBg,
                            color: t.text,
                        }}
                    >
                        {viewLabel}
                    </button>
                    <button
                        type="button"
                        onClick={onRequestRevision}
                        style={{ ...baseButtonStyle, background: t.fieldBg, color: t.text }}
                    >
                        {reviseLabel}
                    </button>
                    <button
                        type="button"
                        onClick={onAbort}
                        style={{ ...baseButtonStyle, background: "transparent", color: "var(--theme-danger)", borderColor: "color-mix(in srgb, var(--theme-danger) 45%, transparent)" }}
                    >
                        {abortLabel}
                    </button>
                    <button
                        type="button"
                        onClick={onConfirm}
                        style={{ ...baseButtonStyle, background: t.sendBtnBg, borderColor: t.sendBtnBg, color: t.sendBtnColor }}
                    >
                        {confirmLabel}
                    </button>
                </div>
            </div>
        </div>
    );
}

export function WorkflowFormInlinePrompt({
    formActive,
    generatingDocument,
    lang,
    phaseName,
    theme: t,
}: {
    formActive: boolean;
    generatingDocument?: boolean;
    lang?: string;
    phaseName: string;
    theme: Theme;
}) {
    const resolvedPhaseName = phaseName.trim();
    const title = generatingDocument
        ? ((chromeText(lang, "Generating workflow document", "正在生成工作流文档")))
        : formActive
        ? ((chromeText(lang, "Workflow form is ready", "工作流表单已打开")))
        : ((chromeText(lang, "Opening workflow form", "正在打开工作流表单")));
    const description = generatingDocument
        ? (resolvedPhaseName
            ? chromeText(lang, "Generating the phase document for {phase}. The review controls will appear here when it is ready.", "正在生成「{phase}」文档，完成后会在这里显示确认推进入口。").replace("{phase}", resolvedPhaseName)
            : chromeText(lang, "Generating the phase document. The review controls will appear here when it is ready.", "正在生成当前阶段文档，完成后会在这里显示确认推进入口。"))
        : formActive
        ? (resolvedPhaseName
            ? chromeText(lang, "Fill in the form on the right to continue: {phase}", "请在右侧表单填写并提交「{phase}」，提交后会继续推进。").replace("{phase}", resolvedPhaseName)
            : chromeText(lang, "Fill in the form on the right to continue.", "请在右侧表单填写并提交，提交后会继续推进。"))
        : (resolvedPhaseName
            ? chromeText(lang, "Preparing the right-side form for {phase}.", "正在准备右侧表单「{phase}」，请稍候。").replace("{phase}", resolvedPhaseName)
            : chromeText(lang, "Preparing the right-side form.", "正在准备右侧表单，请稍候。"));
    const statusText = generatingDocument
        ? ((chromeText(lang, "Generating document", "生成文档中")))
        : ((chromeText(lang, "Waiting for form input", "等待表单输入")));

    return (
        <div
            aria-live="polite"
            data-testid="workflow-form-inline-prompt"
            style={{
                flexShrink: 0,
                padding: "8px 10px 9px",
                borderTop: `1px solid ${t.inputBarBorder}`,
                background: t.inputBarBg,
                color: t.text,
                fontSize: 12,
            }}
        >
            <div className="wip-row">
                <span className="wip-strong">{title}</span>
                <span style={{ color: t.textMuted }}>{statusText}</span>
            </div>
            <div style={{ color: t.textMuted, marginBottom: 7 }}>{description}</div>
            <div style={{ height: 3, overflow: "hidden", borderRadius: 999, background: `color-mix(in srgb, ${t.headingColor} 16%, transparent)` }}>
                <div style={{ width: generatingDocument ? "78%" : formActive ? "62%" : "38%", height: "100%", borderRadius: "inherit", background: t.headingColor, animation: "sidebar-task-restore-progress 0.9s ease-in-out infinite alternate" }} />
            </div>
        </div>
    );
}

import { useEffect, useId, useRef } from "react";
import type { CSSProperties } from "react";
import { primaryFilledButtonStyle, type Theme } from "./aiAssistantPanelTheme";
import type { WelcomeTemplatesImportPreview } from "./welcomeTaskMemory";
import { tailText } from "../../i18n/mnTail"

export type WelcomeTemplatesImportPreviewPanelProps = {
    lang: string;
    theme: Theme;
    preview: WelcomeTemplatesImportPreview;
    onConfirm: () => void;
    onCancel: () => void;
};

/**
 * Pre-apply import summary: what will be added / skipped, plus optional extras.
 */
export function WelcomeTemplatesImportPreviewPanel({
    lang,
    theme: t,
    preview,
    onConfirm,
    onCancel,
}: WelcomeTemplatesImportPreviewPanelProps) {
    const isZh = !!lang?.startsWith("zh");
    const titleId = useId();
    const descId = useId();
    const panelRef = useRef<HTMLDivElement | null>(null);
    const confirmRef = useRef<HTMLButtonElement | null>(null);
    const modeLabel = preview.mode === "replace"
        ? (tailText(lang ?? "", "Replace", "替换"))
        : (tailText(lang ?? "", "Merge", "合并"));

    // Focus the panel on mount; Esc cancels.
    useEffect(() => {
        confirmRef.current?.focus();
        panelRef.current?.scrollIntoView({ block: "nearest", behavior: "smooth" });
        const onKey = (event: KeyboardEvent) => {
            if (event.key === "Escape") {
                event.preventDefault();
                onCancel();
            }
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [onCancel, preview.raw, preview.mode]);

    const listStyle: CSSProperties = {
        margin: 0,
        padding: "0 0 0 16px",
        fontSize: 11,
        lineHeight: 1.45,
        color: t.textMuted,
        maxHeight: 120,
        overflowY: "auto",
    };

    return (
        <div
            ref={panelRef}
            data-testid="welcome-templates-import-preview"
            role="dialog"
            aria-modal="true"
            aria-labelledby={titleId}
            aria-describedby={descId}
            tabIndex={-1}
            style={{
                width: "100%",
                maxWidth: "100%",
                boxSizing: "border-box",
                padding: "10px 12px",
                borderRadius: 8,
                border: `1px solid ${t.fieldBorder}`,
                background: t.inputBarBg || t.fieldBg,
                fontFamily: "system-ui, -apple-system, sans-serif",
            }}
        >
            <div id={titleId} style={{ fontSize: 12, fontWeight: 600, color: t.text, marginBottom: 6 }}>
                {tailText(lang ?? "", `Import preview (${modeLabel})`, `导入预览（${modeLabel}）`)}
            </div>
            <div id={descId} style={{ fontSize: 11, color: t.textMuted, marginBottom: 8 }}>
                {tailText(lang ?? "", `Will add ${preview.toAdd.length}, skip ${preview.toSkip.length}`, `将新增 ${preview.toAdd.length}，跳过 ${preview.toSkip.length}`)}
                {preview.hasExtras && (
                    <span>
                        {tailText(lang ?? "", " · Will restore ", " · 将恢复 ")}
                        {[
                            preview.extras.userRole && (tailText(lang ?? "", `role(${preview.extras.userRole})`, `角色(${preview.extras.userRole})`)),
                            preview.extras.recentCount > 0 && (tailText(lang ?? "", `${preview.extras.recentCount} recent`, `最近${preview.extras.recentCount}项`)),
                            preview.extras.lastScenarioTab && (tailText(lang ?? "", `tab(${preview.extras.lastScenarioTab})`, `分类(${preview.extras.lastScenarioTab})`)),
                        ].filter(Boolean).join(isZh ? "、" : ", ")}
                    </span>
                )}
                <span className="wtip-hint">
                    {tailText(lang ?? "", "Esc to cancel · Enter confirms when focused", "Esc 取消 · Enter 确认（焦点在确认按钮时）")}
                </span>
            </div>

            {preview.toAdd.length > 0 && (
                <div className="wtip-section" data-testid="welcome-import-preview-add">
                    <div style={{ fontSize: 11, fontWeight: 600, color: t.text, marginBottom: 4 }}>
                        {tailText(lang ?? "", "To add", "将新增")}
                    </div>
                    <ul style={listStyle}>
                        {preview.toAdd.slice(0, 12).map((item, i) => (
                            <li key={`add-${i}`} title={item.body}>
                                <strong style={{ color: t.text }}>{item.title}</strong>
                                {" — "}
                                {item.snippet}
                            </li>
                        ))}
                        {preview.toAdd.length > 12 && (
                            <li>{tailText(lang ?? "", `…and ${preview.toAdd.length - 12} more`, `…另有 ${preview.toAdd.length - 12} 项`)}</li>
                        )}
                    </ul>
                </div>
            )}

            {preview.toSkip.length > 0 && (
                <div className="wtip-section" data-testid="welcome-import-preview-skip">
                    <div style={{ fontSize: 11, fontWeight: 600, color: t.text, marginBottom: 4 }}>
                        {tailText(lang ?? "", "To skip", "将跳过")}
                    </div>
                    <ul style={listStyle}>
                        {preview.toSkip.slice(0, 8).map((item, i) => (
                            <li key={`skip-${i}`} title={item.body}>
                                <strong style={{ color: t.text }}>{item.title}</strong>
                                {" — "}
                                {item.reason === "duplicate"
                                    ? (tailText(lang ?? "", "duplicate body", "正文已存在"))
                                    : (tailText(lang ?? "", "over limit", "超出上限"))}
                            </li>
                        ))}
                        {preview.toSkip.length > 8 && (
                            <li>{tailText(lang ?? "", `…and ${preview.toSkip.length - 8} more`, `…另有 ${preview.toSkip.length - 8} 项`)}</li>
                        )}
                    </ul>
                </div>
            )}

            {preview.toAdd.length === 0 && preview.toSkip.length > 0 && (
                <div style={{ fontSize: 11, color: t.textMuted, marginBottom: 8 }}>
                    {tailText(lang ?? "", "Nothing new to add (all duplicates or over limit).", "没有可新增的模板（可能全部重复或超限）。")}
                </div>
            )}

            <div className="wtip-footer">
                <button
                    type="button"
                    data-testid="welcome-import-preview-cancel"
                    onClick={onCancel}
                    style={{
                        padding: "5px 10px",
                        borderRadius: 6,
                        border: `1px solid ${t.fieldBorder}`,
                        background: "transparent",
                        color: t.textMuted,
                        fontSize: 12,
                        cursor: "pointer",
                    }}
                >
                    {tailText(lang ?? "", "Cancel", "取消")}
                </button>
                <button
                    ref={confirmRef}
                    type="button"
                    data-testid="welcome-import-preview-confirm"
                    onClick={onConfirm}
                    disabled={preview.toAdd.length === 0 && !preview.hasExtras}
                    style={primaryFilledButtonStyle(t, {
                        padding: "5px 12px",
                        borderRadius: 6,
                        fontSize: 12,
                        fontWeight: 600,
                        cursor: preview.toAdd.length === 0 && !preview.hasExtras ? "default" : "pointer",
                        opacity: preview.toAdd.length === 0 && !preview.hasExtras ? 0.45 : 1,
                    })}
                >
                    {tailText(lang ?? "", "Confirm import", "确认导入")}
                </button>
            </div>
        </div>
    );
}

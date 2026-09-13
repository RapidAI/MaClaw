import type { MouseEvent } from "react";

type TaskMoreActionsProps = {
    lang: string;
    onSave: () => void | Promise<void>;
    onClear: () => void | Promise<void>;
    onCopyTitle: () => void | Promise<void>;
    /** Opens the right-side preview area; omitted when preview is unavailable. */
    onPreview?: () => void;
};

const label = (lang: string, en: string, zh: string, hant = zh) => lang.startsWith("en") ? en : lang.startsWith("zh-Hant") ? hant : zh;

export function TaskMoreActions({ lang, onSave, onClear, onCopyTitle, onPreview }: TaskMoreActionsProps) {
    const close = (event: MouseEvent<HTMLButtonElement>) => {
        event.currentTarget.closest("details")?.removeAttribute("open");
    };
    return (
        <details className="mc-task-more-menu">
            <summary className="task-more-btn" data-testid="task-more-btn" aria-label={label(lang, "More task actions", "更多任务操作", "更多任務操作")} title={label(lang, "More task actions", "更多任务操作", "更多任務操作")}>•••</summary>
            <div className="mc-task-more-menu__popover" role="menu">
                {onPreview ? (
                    <button type="button" role="menuitem" data-testid="task-more-preview-btn" onClick={(event) => { close(event); onPreview(); }}>{label(lang, "Preview", "预览", "預覽")}</button>
                ) : null}
                <button type="button" role="menuitem" onClick={(event) => { close(event); void onSave(); }}>{label(lang, "Save as task", "保存为任务", "保存為任務")}</button>
                <button type="button" role="menuitem" onClick={(event) => { close(event); void onCopyTitle(); }}>{label(lang, "Copy task title", "复制任务标题", "複製任務標題")}</button>
                <button type="button" role="menuitem" onClick={(event) => { close(event); void onClear(); }}>{label(lang, "Clear conversation", "清空对话", "清空對話")}</button>
            </div>
        </details>
    );
}

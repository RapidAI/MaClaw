import { useRef, type MouseEvent } from "react";
import { localizeText } from "./aiAssistantI18n";
import { getTitleBarToolButtonStyle, type Theme } from "./aiAssistantPanelTheme";

/**
 * Header control shown while the new-task page covers a live conversation.
 * It sits with the other title-bar tools so the home canvas stays the startup page.
 * The keep-conversation note stays in the status text for assistive tech.
 */
type Props = {
    lang?: string;
    theme: Theme;
    inline?: boolean;
    onBack: () => void;
};

export function NewTaskWizardRunningBanner({ lang, theme: t, inline, onBack }: Props) {
    const fromPointer = useRef(false);
    const note = localizeText(lang, "This conversation stays. The new task opens in its own tab.", "当前对话会保留，新任务在单独的页签里开始。", "目前對話會保留，新任務在單獨的頁籤裡開始。");
    const back = localizeText(lang, "Back to this conversation", "返回当前对话", "返回目前對話");
    const onMouseDown = (event: MouseEvent<HTMLButtonElement>) => {
        if (!inline) return;
        event.preventDefault();
        event.stopPropagation();
        fromPointer.current = true;
        onBack();
    };
    const onClick = () => {
        if (fromPointer.current) {
            fromPointer.current = false;
            return;
        }
        onBack();
    };
    return (
        <span data-testid="new-task-wizard-running-banner" role="status" style={{ display: "inline-flex", alignItems: "center" }}>
            <span style={{ position: "absolute", width: 1, height: 1, padding: 0, margin: -1, overflow: "hidden", clip: "rect(0, 0, 0, 0)", whiteSpace: "nowrap", border: 0 }}>{note}</span>
            <button
                type="button"
                className="ai-titlebar-tool"
                data-testid="new-task-wizard-back"
                title={note}
                aria-label={back}
                onMouseDown={onMouseDown}
                onClick={onClick}
                style={{ ...getTitleBarToolButtonStyle(t), width: "auto", padding: "0 10px", gap: 6 }}
            >
                {back}
            </button>
        </span>
    );
}

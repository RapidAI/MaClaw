import { localizeText } from "./aiAssistantI18n";
import { getWindowControlButtonStyle } from "./aiAssistantControls";
import type { Theme } from "./aiAssistantPanelTheme";
import { WindowCloseIcon } from "../layout/WindowControlIcons";

export function TaskHideWindowButton({
    lang,
    theme,
    onHideWindow,
}: {
    lang: string;
    theme: Theme;
    onHideWindow: () => void;
}) {
    return (
        <button
            type="button"
            className="task-hide-btn"
            data-testid="task-hide-window"
            onMouseDown={(event) => {
                event.preventDefault();
                event.stopPropagation();
                onHideWindow();
            }}
            aria-label={localizeText(lang, "Hide window", "隐藏窗口", "隱藏窗口")}
            title={localizeText(lang, "Hide window", "隐藏窗口", "隱藏窗口")}
            style={getWindowControlButtonStyle(theme, "hide")}
        >
            <WindowCloseIcon />
        </button>
    );
}

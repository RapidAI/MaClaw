import { stopWindowControlEvent, toggleWindowOnce, windowMaximizeLabel } from "./aiAssistantControls";
import { WindowMaximizeIcon, WindowRestoreIcon } from "../layout/WindowControlIcons";

export function TaskMaximizeWindowButton({
    lang,
    maximized,
    onToggleMaximize,
}: {
    lang: string;
    maximized: boolean;
    onToggleMaximize: () => void;
}) {
    const label = windowMaximizeLabel(lang, maximized);
    return (
        <button
            type="button"
            className="task-window-btn"
            data-testid="task-maximize-toggle"
            aria-pressed={maximized}
            onMouseDown={(event) => event.stopPropagation()}
            onClick={(event) => toggleWindowOnce(event, onToggleMaximize)}
            onDoubleClick={stopWindowControlEvent}
            aria-label={label}
            title={label}
        >
            {maximized ? <WindowRestoreIcon /> : <WindowMaximizeIcon />}
        </button>
    );
}

import { hideWindowFromControlEvent, windowHideLabel } from "./aiAssistantControls";
import { WindowCloseIcon } from "../layout/WindowControlIcons";

export function TaskHideWindowButton({
    lang,
    onHideWindow,
}: {
    lang: string;
    onHideWindow: () => void;
}) {
    const label = windowHideLabel(lang);
    return (
        <button
            type="button"
            className="task-window-btn"
            data-testid="task-hide-window"
            onMouseDown={(event) => hideWindowFromControlEvent(event, onHideWindow)}
            onClick={(event) => hideWindowFromControlEvent(event, onHideWindow)}
            aria-label={label}
            title={label}
        >
            <WindowCloseIcon />
        </button>
    );
}

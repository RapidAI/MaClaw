import { useState, type MouseEvent } from "react";
import { MobileDocumentsPanel } from "../layout/MobileDocumentsPanel";
import { localizeText } from "./aiAssistantI18n";
import { getTitleBarToolButtonStyle, type Theme } from "./aiAssistantPanelTheme";
import { TitleBarToolIcon } from "./AssistantTitleBarIcons";

type Props = {
    lang: string;
    theme: Theme;
    inline?: boolean;
};

const stopMouse = (handler: () => void) => (e: MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    handler();
};

/** AI title-bar entry for shared Hub Mobile document library. */
export function AssistantMobileDocsControl({ lang, theme: t, inline }: Props) {
    const [open, setOpen] = useState(false);
    const title = localizeText(
        lang,
        "Mobile documents (shared Hub library)",
        "Mobile 文稿（与手机共享的 Hub 文库）",
        "Mobile 文稿（與手機共享的 Hub 文庫）",
    );
    return (
        <>
            <button
                className="ai-titlebar-tool"
                data-testid="mobile-docs-titlebar-btn"
                aria-label={title}
                {...(inline
                    ? { onMouseDown: stopMouse(() => setOpen(true)) }
                    : { onClick: () => setOpen(true) })}
                style={getTitleBarToolButtonStyle(t, open ? "active" : "default")}
                title={title}
            >
                <TitleBarToolIcon name="mobileDocs" />
            </button>
            <MobileDocumentsPanel lang={lang} open={open} onClose={() => setOpen(false)} />
        </>
    );
}

import type { ChatMessage } from "./useAIAssistant";
import type { Theme } from "./aiAssistantPanelTheme";
import { AssistantWelcomeView, type WelcomeComposerProps, type WelcomePromptSubmitMeta, type WelcomeTaskConfig } from "./AssistantWelcomeView";
import { NewTaskWizardRunningBanner } from "./NewTaskWizardRunningBanner";

type Props = {
    lang: string;
    theme: Theme;
    themeMode: "light" | "dark";
    active: boolean;
    hidden?: boolean;
    /** Draw over the live transcript instead of replacing it. The first click paints this layer; the in-panel page mounts after that frame. */
    cover?: boolean;
    showRunningBanner?: boolean;
    onBack: () => void;
    onPromptSelect: (text: string, meta?: WelcomePromptSubmitMeta) => void;
    onPromptSend?: (text: string, meta?: WelcomePromptSubmitMeta) => void;
    pinnedNews?: ChatMessage[];
    taskConfig?: WelcomeTaskConfig;
    composer: WelcomeComposerProps;
};

/**
 * New-task guide body. Shared by the in-panel welcome and the first-paint
 * cover so the click does not unmount a tall conversation before the page shows.
 */
export function NewTaskWizardWelcomePane({
    lang, theme: t, themeMode, active, hidden, cover, showRunningBanner, onBack,
    onPromptSelect, onPromptSend, pinnedNews, taskConfig, composer,
}: Props) {
    return (
        <div
            data-testid="ai-welcome-container"
            hidden={hidden}
            style={{
                flex: cover ? undefined : 1,
                minHeight: 0,
                overflow: "auto",
                background: t.bg,
                boxSizing: "border-box",
                ...(cover ? { position: "absolute" as const, inset: 0, zIndex: 40 } : {}),
            }}
        >
            {showRunningBanner && <NewTaskWizardRunningBanner lang={lang} theme={t} onBack={onBack} />}
            <AssistantWelcomeView
                lang={lang}
                theme={t}
                themeMode={themeMode}
                active={active}
                onPromptSelect={onPromptSelect}
                onPromptSend={onPromptSend}
                pinnedNews={pinnedNews}
                taskConfig={taskConfig}
                composer={composer}
            />
        </div>
    );
}

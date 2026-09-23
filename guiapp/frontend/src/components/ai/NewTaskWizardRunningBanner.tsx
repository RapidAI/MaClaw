import { localizeText } from "./aiAssistantI18n";

/**
 * Banner shown on the local tab while the new-task wizard covers the
 * conversation underneath it.
 *
 * Extracted from AIAssistantPanel so the panel stays inside the 6800-line
 * main-UI guard cap (see scripts/check-main-ui-guards.mjs). The theme argument
 * is structurally typed on purpose: the panel passes its composed assistant
 * theme, and only these four slots are read here.
 */
type WizardBannerTheme = {
    bg: string;
    text: string;
    fieldBg: string;
    fieldBorder: string;
};

type Props = {
    lang?: string;
    theme: WizardBannerTheme;
    onBack: () => void;
};

export function NewTaskWizardRunningBanner({ lang, theme: t, onBack }: Props) {
    return (
        <div
            data-testid="new-task-wizard-running-banner"
            role="status"
            style={{
                position: "sticky",
                top: 0,
                zIndex: 2,
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                gap: 12,
                width: "min(100% - 32px, 760px)",
                margin: "12px auto 0",
                padding: "8px 10px 8px 12px",
                boxSizing: "border-box",
                borderRadius: 10,
                border: `1px solid ${t.fieldBorder}`,
                background: t.fieldBg,
                color: t.text,
                fontSize: 12,
                lineHeight: 1.45,
                fontFamily: "system-ui, -apple-system, sans-serif",
            }}
        >
            <span>{localizeText(lang, "This conversation stays. The new task opens in its own tab.", "当前对话会保留，新任务在单独的页签里开始。", "目前對話會保留，新任務在單獨的頁籤裡開始。")}</span>
            <button
                type="button"
                data-testid="new-task-wizard-back"
                onClick={onBack}
                style={{
                    flexShrink: 0,
                    border: `1px solid ${t.fieldBorder}`,
                    background: t.bg,
                    color: t.text,
                    borderRadius: 8,
                    padding: "4px 10px",
                    fontSize: 12,
                    cursor: "pointer",
                    fontFamily: "system-ui, -apple-system, sans-serif",
                }}
            >
                {localizeText(lang, "Back to this conversation", "返回当前对话", "返回目前對話")}
            </button>
        </div>
    );
}

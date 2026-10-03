import { DEFAULT_EXPERT_ICON, expertWelcomeMessageText } from "./expertTypes";
import { localizeText } from "./aiAssistantI18n";

type BannerTheme = {
    bg: string;
    text: string;
    textMuted: string;
    inputBarBorder: string;
    inputBarBg: string;
    headingColor: string;
};

// Expert tab empty state (e.g. after a conversation clear):
// expert name + intro instead of the generic welcome view.
export function ExpertTabEmptyBanner({ activeTab, lang, theme }: { activeTab: any; lang: string; theme: BannerTheme }) {
    return (
        <div data-testid="ai-expert-empty" style={{ display: "flex", flexDirection: "column", alignItems: "center", justifyContent: "center", gap: 10, padding: "40px 20px", textAlign: "center", color: theme.textMuted }}>
            <div className="aap-expert-icon" aria-hidden="true">{activeTab.expertIcon || DEFAULT_EXPERT_ICON}</div>
            <div style={{ fontSize: "0.95rem", fontWeight: 600, color: theme.text }}>{activeTab.title}</div>
            {activeTab.expertDescription ? (
                <div className="aap-expert-desc">{activeTab.expertDescription}</div>
            ) : null}
            <div className="aap-expert-msg">
                {expertWelcomeMessageText({ name: activeTab.title, description: activeTab.expertDescription || "" }, lang)}
            </div>
        </div>
    );
}

// Progress banner shown while a project session is being prepared/restored.
export function ProjectPreparingBanner({ prepareMode, isRemoteCodingDev, isRemoteMaintenance, isCodingDev, lang, theme }: {
    prepareMode: string;
    isRemoteCodingDev: boolean;
    isRemoteMaintenance: boolean;
    isCodingDev: boolean;
    lang: string;
    theme: BannerTheme;
}) {
    return (
        <div data-testid="project-tab-restore-progress" style={{ flexShrink: 0, padding: "7px 10px 8px", borderTop: `1px solid ${theme.inputBarBorder}`, background: theme.inputBarBg, color: theme.textMuted, fontSize: 12 }}>
            <div className="aap-restore-head">
                <span>{prepareMode === "new-agent"
                    ? (isRemoteCodingDev
                        ? (isRemoteMaintenance
                            ? localizeText(lang, "Preparing remote maintenance", "正在准备远程维护", "正在準備遠端維護")
                            : localizeText(lang, "Creating remote coding environment", "正在创建远程编程环境", "正在建立遠端程式開發環境"))
                        : isCodingDev
                        ? localizeText(lang, "Creating coding environment", "正在创建编程环境", "正在建立程式開發環境")
                        : (lang === "en" ? "Creating project session" : "正在创建项目会话"))
                    : (lang === "en" ? "Restoring task context" : "正在恢复任务上下文")}</span>
                <span className="aap-restore-note">{lang === "en" ? "Input will wait" : "输入会先等待"}</span>
            </div>
            <div style={{ height: 3, overflow: "hidden", borderRadius: 999, background: `color-mix(in srgb, ${theme.headingColor} 16%, transparent)` }}>
                <div style={{ width: "38%", height: "100%", borderRadius: "inherit", background: theme.headingColor, animation: "sidebar-task-restore-progress 0.9s ease-in-out infinite alternate" }} />
            </div>
        </div>
    );
}

import { colors } from "./styles";
import { wizardOptionCardStyle } from "./OnboardingWizardShared";

type Translator = (zh: string, en: string, zhHant?: string) => string;

interface Props {
    offlineMode: boolean;
    freeTrial: boolean;
    onToggle: (enabled: boolean) => void;
    onFreeTrialChange: (enabled: boolean) => void;
    t: Translator;
}

export function OnboardingOfflineModeOption({ offlineMode, freeTrial, onToggle, onFreeTrialChange, t }: Props) {
    return (
        <div style={{ display: "grid", gap: 6, marginBottom: 10 }}>
            <div style={{ display: "flex", alignItems: "stretch", gap: 8, flexWrap: "wrap" }}>
                <div role="group" aria-label={t("运行模式", "Run mode", "運行模式")} style={{ display: "inline-flex", border: `1px solid ${colors.border}`, borderRadius: 8, overflow: "hidden", background: colors.surfaceMuted }}>
                    <button type="button" aria-label={t("联网模式", "Online mode", "聯網模式")} aria-pressed={!offlineMode} onClick={() => onToggle(false)} style={{
                        minHeight: 44,
                        padding: "0 14px",
                        border: 0,
                        borderRight: `1px solid ${colors.border}`,
                        background: !offlineMode ? "var(--theme-info-bg)" : "transparent",
                        color: !offlineMode ? colors.primaryDark : colors.textSecondary,
                        fontSize: "0.76rem",
                        fontWeight: 700,
                        cursor: "pointer",
                    }}>
                        {t("联网模式", "Online mode", "聯網模式")}
                        <span style={{ marginLeft: 6, color: colors.success, fontSize: "0.68rem", fontWeight: 600 }}>
                            {t("推荐", "Recommended", "推薦")}
                        </span>
                    </button>
                    <button type="button" aria-label={t("离网模式", "Offline mode", "離網模式")} aria-pressed={offlineMode} onClick={() => onToggle(true)} style={{
                        minHeight: 44,
                        padding: "0 14px",
                        border: 0,
                        background: offlineMode ? "rgba(245,158,11,0.12)" : "transparent",
                        color: offlineMode ? colors.warning : colors.textSecondary,
                        fontSize: "0.76rem",
                        fontWeight: 700,
                        cursor: "pointer",
                    }}>
                        {t("离网模式", "Offline mode", "離網模式")}
                    </button>
                </div>

                <label style={{
                    ...wizardOptionCardStyle,
                    minHeight: 44,
                    alignItems: "center",
                    padding: "0 12px",
                    marginLeft: "auto",
                    border: `1px solid ${!offlineMode && freeTrial ? colors.primary : colors.border}`,
                    background: !offlineMode && freeTrial ? "var(--theme-info-bg)" : colors.surfaceMuted,
                    color: offlineMode ? colors.textMuted : colors.text,
                    cursor: offlineMode ? "not-allowed" : "pointer",
                    opacity: offlineMode ? 0.58 : 1,
                }}>
                    <input
                        type="checkbox"
                        checked={!offlineMode && freeTrial}
                        disabled={offlineMode}
                        aria-describedby="onboarding-run-mode-note"
                        onChange={e => onFreeTrialChange(e.target.checked)}
                        style={{ margin: 0 }}
                    />
                    <strong>{t("免费试用", "Free trial", "免費試用")}</strong>
                </label>
            </div>

            <div id="onboarding-run-mode-note" style={{ color: colors.textMuted, fontSize: "0.72rem", lineHeight: 1.45 }}>
                {!offlineMode
                    ? t("注册 Hub，可使用远程控制、微信绑定、联网搜索和 Hub 服务。", "Register Hub to use remote control, WeChat binding, web search, and Hub services.", "註冊 Hub，可使用遠端控制、微信綁定、聯網搜尋和 Hub 服務。")
                    : t("跳过 Hub 注册和微信绑定；配置并测试通过 LLM 后即可完成初始化。", "Skip Hub registration and WeChat binding. Onboarding finishes after LLM setup is tested and saved.", "跳過 Hub 註冊和微信綁定；配置並測試通過 LLM 後即可完成初始化。")}
            </div>
        </div>
    );
}

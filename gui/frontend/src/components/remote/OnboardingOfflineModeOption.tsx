import { colors } from "./styles";
import { wizardOptionCardStyle } from "./OnboardingWizardShared";

type Translator = (zh: string, en: string, zhHant?: string) => string;

interface Props {
    offlineMode: boolean;
    onToggle: (enabled: boolean) => void;
    t: Translator;
}

export function OnboardingOfflineModeOption({ offlineMode, onToggle, t }: Props) {
    return (
        <div style={{ display: "grid", gap: 8, marginBottom: 10 }}>
            <label style={{
                ...wizardOptionCardStyle,
                border: `1px solid ${!offlineMode ? colors.primary : colors.border}`,
                background: !offlineMode ? "var(--theme-info-bg)" : colors.surfaceMuted,
            }}>
                <input type="radio" name="onboarding-run-mode" checked={!offlineMode} onChange={() => onToggle(false)} style={{ marginTop: 2 }} />
                <span style={{ lineHeight: 1.45 }}>
                    <strong>{t("正常联网模式", "Online mode", "正常聯網模式")}</strong>
                    <span style={{ marginLeft: 6, color: colors.success, fontSize: "0.68rem" }}>{t("推荐", "Recommended", "推薦")}</span>
                    <br />
                    <span style={{ color: colors.textMuted }}>
                        {t(
                            "注册 Hub，可使用远程控制、微信绑定、联网搜索和 Hub 服务。",
                            "Register Hub to use remote control, WeChat binding, web search, and Hub services.",
                            "註冊 Hub，可使用遠端控制、微信綁定、聯網搜尋和 Hub 服務。",
                        )}
                    </span>
                </span>
            </label>
            <label style={{
                ...wizardOptionCardStyle,
                border: `1px solid ${offlineMode ? colors.primary : colors.border}`,
                background: offlineMode ? "rgba(245,158,11,0.10)" : colors.surfaceMuted,
            }}>
                <input type="radio" name="onboarding-run-mode" checked={offlineMode} onChange={() => onToggle(true)} style={{ marginTop: 2 }} />
                <span style={{ lineHeight: 1.45 }}>
                    <strong>{t("离网模式", "Offline mode", "離網模式")}</strong>
                    <br />
                    <span style={{ color: colors.textMuted }}>
                        {t(
                            "跳过 Hub 注册和微信绑定；配置并测试通过 LLM 后即完成初始化。",
                            "Skip Hub registration and WeChat binding. Onboarding finishes after LLM setup is tested and saved.",
                            "跳過 Hub 註冊和微信綁定；配置並測試通過 LLM 後即完成初始化。",
                        )}
                    </span>
                    {offlineMode && (
                        <div style={{
                            marginTop: 6, padding: "6px 8px", borderRadius: 6,
                            background: "rgba(245,158,11,0.12)", border: "1px solid rgba(245,158,11,0.28)",
                            color: colors.warning, fontSize: "0.7rem", lineHeight: 1.45,
                        }}>
                            {t(
                                "注意：离网模式下将无法访问外网，无法进行网页搜索，也无法使用依赖 Hub 的服务。",
                                "Note: offline mode cannot access the public internet, perform web search, or use Hub-dependent services.",
                                "注意：離網模式下將無法訪問外網，無法進行網頁搜尋，也無法使用依賴 Hub 的服務。",
                            )}
                        </div>
                    )}
                </span>
            </label>
        </div>
    );
}

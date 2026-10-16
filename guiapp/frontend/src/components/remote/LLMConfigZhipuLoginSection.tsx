import { colors } from "./styles";
import { labelStyle } from "./LLMConfigPanelShared";
import { ZhipuLogo } from "./providerLogos";

type Translate = (en: string, zhHans: string, zhHant?: string) => string;

/**
 * ZCode online login (智谱在线登录) for the 智谱编程 provider. The browser
 * authorization resolves a coding-plan API key on the backend; while the
 * approval is pending the entry doubles as the cancel surface, because the
 * dialog itself refuses to close during an active OAuth flow.
 */
export function LLMConfigZhipuLoginSection({
    t,
    busy,
    onLogin,
    onCancel,
}: {
    t: Translate;
    busy: boolean;
    onLogin: () => void;
    onCancel: () => void;
}) {
    return (
        <div style={{ marginBottom: 12 }}>
            <label style={labelStyle}>{t("ZCode Login", "ZCode \u767b\u5f55")}</label>
            <div style={{ display: "flex", gap: 6 }}>
                <button
                    data-testid="zhipu-zcode-login"
                    onClick={onLogin}
                    disabled={busy}
                    style={{
                        flex: 1, padding: "10px 0", fontSize: "0.8rem",
                        display: "inline-flex", alignItems: "center", justifyContent: "center", gap: 6,
                        cursor: busy ? "default" : "pointer",
                        background: colors.primaryLight, color: colors.primaryDark,
                        border: `1px solid ${colors.primary}`, borderRadius: 4,
                        opacity: busy ? 0.6 : 1,
                    }}
                >
                    <ZhipuLogo />
                    {busy
                        ? t("Waiting for Zhipu authorization...", "\u7b49\u5f85\u667a\u8c31\u6388\u6743...")
                        : t("Zhipu online login (ZCode)", "\u667a\u8c31\u5728\u7ebf\u767b\u5f55 (ZCode)")}
                </button>
                {busy && (
                    <button
                        data-testid="zhipu-zcode-cancel"
                        onClick={onCancel}
                        style={{
                            padding: "10px 14px", fontSize: "0.76rem", flexShrink: 0,
                            cursor: "pointer",
                            background: colors.surface, color: colors.textSecondary,
                            border: `1px solid ${colors.border}`, borderRadius: 4,
                        }}
                    >
                        {t("Cancel", "\u53d6\u6d88")}
                    </button>
                )}
            </div>
            <p style={{ fontSize: "0.68rem", color: colors.textMuted, margin: "4px 0 0 0", lineHeight: 1.4 }}>
                {t(
                    "Authorize with your Zhipu (bigmodel.cn) account in the browser; MaClaw fetches and fills the coding-plan API key automatically, no manual paste needed.",
                    "\u5728\u6d4f\u89c8\u5668\u4e2d\u5b8c\u6210\u667a\u8c31\uff08bigmodel.cn\uff09\u8d26\u53f7\u6388\u6743\u540e\uff0c\u4f1a\u81ea\u52a8\u83b7\u53d6\u5e76\u586b\u5165\u7f16\u7a0b\u8ba1\u5212 API Key\uff0c\u65e0\u9700\u624b\u52a8\u7c98\u8d34\u3002",
                )}
            </p>
        </div>
    );
}

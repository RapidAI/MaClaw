import { colors } from "./styles";
import { labelStyle, type LLMProvider } from "./LLMConfigPanelShared";

type Translate = (en: string, zhHans: string, zhHant?: string) => string;

export function LLMConfigCodeGenSSOFields({
    provider,
    busy,
    t,
    onLogin,
    onCancel,
}: {
    provider: LLMProvider;
    busy: boolean;
    t: Translate;
    onLogin: () => void;
    onCancel?: () => void;
}) {
    return (
        <div>
            <label style={labelStyle}>{t("Authentication", "认证方式")}</label>
            <button
                data-testid="codegen-sso-login"
                onClick={onLogin}
                disabled={busy}
                style={{
                    width: "100%", padding: "10px 0", fontSize: "0.8rem",
                    cursor: busy ? "default" : "pointer",
                    background: colors.primaryLight, color: colors.primaryDark,
                    border: `1px solid ${colors.primary}`, borderRadius: 4,
                    opacity: busy ? 0.6 : 1,
                }}
            >
                {busy
                    ? t("Waiting for browser auth...", "等待浏览器授权...")
                    : provider.key
                        ? t("Re-run Enterprise SSO Login", "重新进行企业 SSO 登录")
                        : t("Enterprise SSO Login", "企业 SSO 登录")}
            </button>
            {busy && onCancel && (
                <button
                    data-testid="codegen-sso-cancel"
                    onClick={onCancel}
                    style={{
                        width: "100%", marginTop: 6, padding: "8px 0", fontSize: "0.78rem",
                        cursor: "pointer",
                        background: "transparent", color: colors.textSecondary,
                        border: `1px solid ${colors.border}`, borderRadius: 4,
                    }}
                >
                    {t("Cancel login", "取消登录")}
                </button>
            )}
            <p style={{ fontSize: "0.68rem", color: colors.textMuted, margin: "4px 0 0 0", lineHeight: 1.4 }}>
                {t(
                    "Sign in with your enterprise account in the browser. The token and model list are fetched automatically; no API Key needed.",
                    "使用企业账号在浏览器中一键登录，自动获取令牌与模型列表，无需填写 API Key。",
                )}
            </p>
        </div>
    );
}

import { useEffect, useRef } from "react";
import { colors } from "./styles";
import { inputStyle, isOpenCodeProvider, labelStyle, type LLMProvider } from "./LLMConfigPanelShared";

type Translate = (en: string, zhHans: string, zhHant?: string) => string;

export function LLMConfigApiKeyFields({
    provider,
    loginBusy,
    t,
    onUpdateKey,
    onOpenCodeLogin,
    focusNonce,
}: {
    provider: LLMProvider;
    loginBusy: boolean;
    t: Translate;
    onUpdateKey: (key: string) => void;
    onOpenCodeLogin: () => void;
    focusNonce?: number;
}) {
    const isOpenCode = isOpenCodeProvider(provider);
    const isAnthropicStyle = provider.name === "\u667a\u8c31\u7f16\u7a0b" || (provider.protocol || "openai") === "anthropic";
    const keyInputRef = useRef<HTMLInputElement>(null);
    useEffect(() => {
        if (focusNonce && isOpenCode) keyInputRef.current?.focus();
    }, [focusNonce, isOpenCode]);
    return (
        <div>
            {isOpenCode && (
                <div style={{ marginBottom: 12 }}>
                    <label style={labelStyle}>{t("Authentication", "\u8ba4\u8bc1\u65b9\u5f0f")}</label>
                    <button
                        data-testid="opencode-zen-login"
                        onClick={onOpenCodeLogin}
                        disabled={loginBusy}
                        style={{
                            width: "100%", padding: "10px 0", fontSize: "0.8rem",
                            cursor: loginBusy ? "default" : "pointer",
                            background: colors.primaryLight, color: colors.primaryDark,
                            border: `1px solid ${colors.primary}`, borderRadius: 4,
                            opacity: loginBusy ? 0.6 : 1,
                        }}
                    >
                        {loginBusy
                            ? t("Opening OpenCode...", "\u6b63\u5728\u6253\u5f00 OpenCode...")
                            : provider.key
                                ? t("Re-login to OpenCode Zen", "\u91cd\u65b0\u767b\u5f55 OpenCode Zen")
                                : t("Sign in to OpenCode Zen", "\u767b\u5f55 OpenCode Zen")}
                    </button>
                </div>
            )}
            <label style={labelStyle}>{t("API Key", "API Key")} <span style={{ color: colors.danger }}>*</span></label>
            <input ref={keyInputRef} style={inputStyle} type="password" value={provider.key}
                onChange={e => onUpdateKey(e.target.value)}
                placeholder={isAnthropicStyle ? "xxxxxxxx.yyyyyyyy" : "sk-..."}
                autoCapitalize="off" autoCorrect="off" spellCheck={false} autoComplete="off" />
            {isOpenCode && (
                <p style={{ fontSize: "0.68rem", color: colors.textMuted, margin: "4px 0 0 0", lineHeight: 1.4 }}>
                    {t(
                        "Sign in, open API Keys, copy the key, paste it below, then Test & Save. The model list is whatever this key can use, including paid models.",
                        "\u767b\u5f55\u540e\u6253\u5f00 API Keys\uff0c\u590d\u5236\u5bc6\u94a5\u7c98\u8d34\u5230\u4e0b\u65b9\uff0c\u518d\u68c0\u6d4b\u5e76\u4fdd\u5b58\u3002\u6a21\u578b\u5217\u8868\u4e3a\u8be5\u8d26\u53f7\u53ef\u7528\u7684\u5168\u90e8\u6a21\u578b\uff0c\u542b\u4ed8\u8d39\u6863\u3002",
                    )}
                </p>
            )}
        </div>
    );
}

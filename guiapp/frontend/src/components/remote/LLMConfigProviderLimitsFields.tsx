import { colors } from "./styles";
import { inputStyle, labelStyle, type LLMProvider } from "./LLMConfigPanelShared";

type Translate = (en: string, zhHans: string, zhHant?: string) => string;

export function LLMConfigProviderLimitsFields({
    provider,
    t,
    onUpdateField,
    onVisionChange,
}: {
    provider: LLMProvider;
    t: Translate;
    onUpdateField: (field: keyof LLMProvider, value: string) => void;
    onVisionChange: (supportsVision: boolean) => void;
}) {
    if (provider.import_source) return null;
    return (
        <>
            <div style={{ marginTop: 12 }}>
                <label style={labelStyle}>{t("Context Length (tokens)", "\u4e0a\u4e0b\u6587\u957f\u5ea6 (tokens)")}</label>
                <input style={inputStyle} type="number" min={0} step={1000}
                    autoCapitalize="off" autoCorrect="off" spellCheck={false} autoComplete="off"
                    value={provider.context_length || ""}
                    onChange={e => onUpdateField("context_length", e.target.value)}
                    placeholder="128000" />
                <p style={{ fontSize: "0.68rem", color: colors.textMuted, margin: "4px 0 0 0", lineHeight: 1.4 }}>
                    {t(
                        "Max context window of the model. GLM supports 180000. Defaults to 128000 if empty.",
                        "\u6a21\u578b\u652f\u6301\u7684\u6700\u5927\u4e0a\u4e0b\u6587\u957f\u5ea6\u3002GLM \u53ef\u652f\u6301 180000\uff0c\u7559\u7a7a\u9ed8\u8ba4 128000\u3002",
                    )}
                </p>
            </div>
            <div style={{ marginTop: 12 }}>
                <label style={labelStyle}>{t("Max Output Tokens", "\u6700\u5927\u8f93\u51fa\u957f\u5ea6 (tokens)")}</label>
                <input style={inputStyle} type="number" min={1024} step={1024}
                    autoCapitalize="off" autoCorrect="off" spellCheck={false} autoComplete="off"
                    value={provider.max_output_tokens || ""}
                    onChange={e => onUpdateField("max_output_tokens", e.target.value)}
                    placeholder="65536" />
                <p style={{ fontSize: "0.68rem", color: colors.textMuted, margin: "4px 0 0 0", lineHeight: 1.4 }}>
                    {t(
                        "Max tokens per LLM response. Defaults to 65536. For models with lower limits, the system auto-detects and adapts on first use.",
                        "\u5355\u6b21\u56de\u590d\u6700\u5927 token \u6570\u3002\u9ed8\u8ba4 65536\u3002\u5bf9\u4e8e\u9650\u5236\u8f83\u4f4e\u7684\u6a21\u578b\uff0c\u7cfb\u7edf\u5728\u9996\u6b21\u4f7f\u7528\u65f6\u81ea\u52a8\u68c0\u6d4b\u5e76\u9002\u914d\u3002",
                    )}
                </p>
            </div>
            <div style={{
                marginTop: 12, display: "flex", alignItems: "center",
                justifyContent: "space-between", gap: 10,
            }}>
                <div style={{ flex: 1 }}>
                    <label style={{ ...labelStyle, marginBottom: 2 }}>
                        {t("Vision Support", "\u89c6\u89c9\u652f\u6301")}
                    </label>
                    <p style={{ fontSize: "0.68rem", color: colors.textMuted, margin: 0, lineHeight: 1.4 }}>
                        {provider.supports_vision
                            ? t("Supports image input (WeChat images understood by model)", "\u652f\u6301\u56fe\u7247\u8f93\u5165\uff08\u5fae\u4fe1\u53d1\u56fe\u53ef\u88ab\u6a21\u578b\u7406\u89e3\uff09")
                            : t("No vision (images saved as files, not sent to model)", "\u4e0d\u652f\u6301\u89c6\u89c9\uff08\u56fe\u7247\u4f1a\u4fdd\u5b58\u4e3a\u6587\u4ef6\uff0c\u4e0d\u53d1\u9001\u7ed9\u6a21\u578b\uff09")}
                    </p>
                    <p style={{ fontSize: "0.64rem", color: colors.textMuted, margin: "2px 0 0 0", lineHeight: 1.4 }}>
                        {t(
                            "Vision support is auto-detected during the initial test-and-save. If inaccurate, you can adjust it manually and save again.",
                            "\u9996\u6b21\u6d4b\u8bd5\u5e76\u4fdd\u5b58\u65f6\u4f1a\u81ea\u52a8\u68c0\u6d4b\u89c6\u89c9\u80fd\u529b\uff1b\u5982\u679c\u4e0d\u51c6\u786e\uff0c\u53ef\u624b\u52a8\u8c03\u6574\u540e\u518d\u4fdd\u5b58\u3002",
                        )}
                    </p>
                </div>
                <input type="checkbox" checked={!!provider.supports_vision}
                    onChange={e => onVisionChange(e.target.checked)}
                    style={{ width: 18, height: 18, accentColor: "var(--theme-primary)", cursor: "pointer", flexShrink: 0 }} />
            </div>
        </>
    );
}

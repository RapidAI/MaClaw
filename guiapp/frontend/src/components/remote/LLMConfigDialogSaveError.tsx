import { colors } from "./styles";

type Translate = (en: string, zhHans: string, zhHant?: string) => string;

export function LLMConfigDialogSaveError({
    reason,
    title,
    t,
}: {
    reason: string;
    title?: string;
    t: Translate;
}) {
    if (!reason) return null;
    return (
        <div role="alert" data-testid="llm-config-save-error" style={{
            marginBottom: 10, padding: "8px 10px", borderRadius: 4, fontSize: "0.72rem", lineHeight: 1.45,
            background: colors.dangerBg, border: `1px solid color-mix(in srgb, ${colors.danger} 30%, transparent)`,
            color: colors.danger, whiteSpace: "pre-wrap", wordBreak: "break-word",
        }}>
            <div style={{ fontWeight: 700 }}>{title || t("Connection failed, not saved", "\u8fde\u63a5\u5931\u8d25\uff0c\u672a\u4fdd\u5b58")}</div>
            <div style={{ marginTop: 4 }}>{reason}</div>
        </div>
    );
}

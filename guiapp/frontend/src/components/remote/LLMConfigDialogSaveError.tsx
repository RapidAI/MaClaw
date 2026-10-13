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
        <div role="alert" data-testid="llm-config-save-error" className="llm-config-save-error">
            <div className="llm-config-save-error__title">{title || t("Connection failed, not saved", "\u8fde\u63a5\u5931\u8d25\uff0c\u672a\u4fdd\u5b58")}</div>
            <div className="llm-config-save-error__reason">{reason}</div>
        </div>
    );
}

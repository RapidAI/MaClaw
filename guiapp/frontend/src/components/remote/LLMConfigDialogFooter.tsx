import { LLMConfigDialogSaveError } from "./LLMConfigDialogSaveError";

type Translate = (en: string, zhHans: string, zhHant?: string) => string;

export function LLMConfigDialogFooter({
    dirty,
    error,
    errorTitle,
    hubSelected,
    hubAlreadySynced,
    hubModelDirty = false,
    needsOAuthLogin,
    oauthBusy,
    saving,
    t,
    tested,
    onCancel,
    onSave,
    onSaveHub,
}: {
    dirty: boolean;
    error?: string;
    errorTitle?: string;
    hubSelected: boolean;
    hubAlreadySynced: boolean;
    hubModelDirty?: boolean;
    needsOAuthLogin: boolean;
    oauthBusy: boolean;
    saving: boolean;
    t: Translate;
    tested: boolean;
    onCancel: () => void;
    onSave: () => void;
    onSaveHub: () => void;
}) {
    const canSave = dirty || tested || needsOAuthLogin;
    const hubSaveLocked = hubAlreadySynced && !hubModelDirty;
    const reason = error?.trim() || "";
    return (
        <div className="llm-config-footer">
            <LLMConfigDialogSaveError reason={reason} title={errorTitle} t={t} />
            <div className="llm-config-footer__actions">
                {dirty && !hubSelected && !reason && (
                    <span className="llm-config-footer__unsaved">
                        {t("unsaved", "\u672a\u4fdd\u5b58")}
                    </span>
                )}
                <button className="llm-config-btn" onClick={onCancel}>
                    {t("Cancel", "\u53d6\u6d88")}
                </button>
                {hubSelected ? (
                    <button className="llm-config-btn llm-config-btn--primary" onClick={onSaveHub} disabled={saving || hubSaveLocked}>
                        {saving ? t("Saving...", "\u4fdd\u5b58\u4e2d...")
                            : hubModelDirty ? t("Save model", "\u4fdd\u5b58\u6a21\u578b")
                            : hubAlreadySynced ? t("Currently Active", "\u5f53\u524d\u5df2\u542f\u7528")
                            : t("Use This Service", "\u4f7f\u7528\u6b64\u670d\u52a1")}
                    </button>
                ) : (
                    <button className="llm-config-btn llm-config-btn--primary" onClick={onSave} disabled={saving || oauthBusy || !canSave}>
                        {saving ? t("Testing & Saving...", "\u68c0\u6d4b\u5e76\u4fdd\u5b58\u4e2d...") : tested ? t("Save Changes", "\u4fdd\u5b58\u4fee\u6539") : t("Test & Save", "\u68c0\u6d4b\u5e76\u4fdd\u5b58")}
                    </button>
                )}
            </div>
        </div>
    );
}

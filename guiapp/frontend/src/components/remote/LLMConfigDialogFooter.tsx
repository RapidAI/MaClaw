import { colors } from "./styles";
import { LLMConfigDialogSaveError } from "./LLMConfigDialogSaveError";

type Translate = (en: string, zhHans: string, zhHant?: string) => string;

const footerBtnBase = { fontSize: "0.76rem", padding: "6px 18px", borderRadius: 4 } as const;

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
        <div style={{ marginTop: 20 }}>
            <LLMConfigDialogSaveError reason={reason} title={errorTitle} t={t} />
            <div style={{ display: "flex", gap: 10, alignItems: "center", justifyContent: "flex-end" }}>
                {dirty && !hubSelected && !reason && (
                    <span style={{ fontSize: "0.68rem", color: colors.primaryDark, marginRight: "auto" }}>
                        {t("unsaved", "\u672a\u4fdd\u5b58")}
                    </span>
                )}
                <button onClick={onCancel} style={{ ...footerBtnBase, cursor: "pointer", background: colors.bg, color: colors.text, border: `1px solid ${colors.border}` }}>
                    {t("Cancel", "\u53d6\u6d88")}
                </button>
                {hubSelected ? (
                    <button onClick={onSaveHub} disabled={saving || hubSaveLocked} style={{ ...footerBtnBase, cursor: (saving || hubSaveLocked) ? "default" : "pointer", background: hubSaveLocked ? colors.bg : colors.primaryLight, color: hubSaveLocked ? colors.textMuted : colors.primaryDark, border: `1px solid ${hubSaveLocked ? colors.border : colors.primary}`, opacity: saving ? 0.6 : 1 }}>
                        {saving ? t("Saving...", "\u4fdd\u5b58\u4e2d...")
                            : hubModelDirty ? t("Save model", "\u4fdd\u5b58\u6a21\u578b")
                            : hubAlreadySynced ? t("Currently Active", "\u5f53\u524d\u5df2\u542f\u7528")
                            : t("Use This Service", "\u4f7f\u7528\u6b64\u670d\u52a1")}
                    </button>
                ) : (
                    <button onClick={onSave} disabled={saving || oauthBusy || !canSave} style={{ ...footerBtnBase, cursor: canSave ? "pointer" : "default", background: canSave ? colors.primaryLight : colors.bg, color: canSave ? colors.primaryDark : colors.textMuted, border: `1px solid ${canSave ? colors.primary : colors.border}`, opacity: saving ? 0.6 : 1 }}>
                        {saving ? t("Testing & Saving...", "\u68c0\u6d4b\u5e76\u4fdd\u5b58\u4e2d...") : tested ? t("Save Changes", "\u4fdd\u5b58\u4fee\u6539") : t("Test & Save", "\u68c0\u6d4b\u5e76\u4fdd\u5b58")}
                    </button>
                )}
            </div>
        </div>
    );
}

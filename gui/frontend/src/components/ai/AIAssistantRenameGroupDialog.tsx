import { localizeText } from "./aiAssistantI18n";
import type { Theme } from "./aiAssistantMarkdown";

interface AIAssistantRenameGroupDialogProps {
    error: string;
    lang: string;
    onClose: () => void;
    onSubmit: () => void;
    onValueChange: (value: string) => void;
    saving: boolean;
    theme: Theme & { bg: string };
    value: string;
}

export function AIAssistantRenameGroupDialog({
    error,
    lang,
    onClose,
    onSubmit,
    onValueChange,
    saving,
    theme: t,
    value,
}: AIAssistantRenameGroupDialogProps) {
    const canSave = !!value.trim() && !saving;
    return (
        <div
            data-testid="rename-group-dialog"
            role="dialog"
            aria-modal="true"
            aria-labelledby="rename-group-title"
            onMouseDown={event => { if (!saving && event.target === event.currentTarget) onClose(); }}
            style={{ position: "absolute", inset: 0, zIndex: 30050, display: "flex", alignItems: "center", justifyContent: "center", padding: 20, background: "rgba(15, 23, 42, 0.34)" }}
        >
            <div
                onMouseDown={event => event.stopPropagation()}
                style={{ width: "min(360px, 100%)", border: `1px solid ${t.divider}`, borderRadius: 8, background: t.bg, color: t.text, boxShadow: "0 18px 48px rgba(0,0,0,0.24)", padding: 16 }}
            >
                <h2 id="rename-group-title" style={{ margin: "0 0 12px", fontSize: 16, lineHeight: 1.3, color: t.text }}>
                    {localizeText(lang, "Rename group", "修改群名", "修改群名")}
                </h2>
                <label htmlFor="rename-group-input" style={{ display: "block", marginBottom: 6, fontSize: 12, color: t.fieldLabel }}>
                    {localizeText(lang, "Group name", "群名", "群名")}
                </label>
                <input
                    id="rename-group-input"
                    data-testid="rename-group-input"
                    autoFocus
                    value={value}
                    maxLength={60}
                    disabled={saving}
                    aria-invalid={error ? "true" : undefined}
                    aria-describedby={error ? "rename-group-error" : undefined}
                    onChange={event => onValueChange(event.target.value)}
                    onKeyDown={event => {
                        if (event.key === "Escape" && !saving) onClose();
                        if (event.key === "Enter") onSubmit();
                    }}
                    style={{ width: "100%", boxSizing: "border-box", border: `1px solid ${error ? t.errorBorder : t.fieldBorder}`, borderRadius: 6, background: t.fieldBg, color: t.text, padding: "8px 10px", fontSize: 14, outline: "none" }}
                />
                {error && (
                    <div id="rename-group-error" role="alert" style={{ marginTop: 6, color: t.errorText, fontSize: 12, lineHeight: 1.4 }}>
                        {error}
                    </div>
                )}
                <div style={{ display: "flex", justifyContent: "flex-end", gap: 8, marginTop: 16 }}>
                    <button type="button" onClick={onClose} disabled={saving} style={{ border: `1px solid ${t.divider}`, borderRadius: 6, background: t.fieldBg, color: t.text, padding: "7px 12px", cursor: saving ? "not-allowed" : "pointer", opacity: saving ? 0.55 : 1 }}>
                        {localizeText(lang, "Cancel", "取消", "取消")}
                    </button>
                    <button
                        type="button"
                        data-testid="rename-group-save"
                        onClick={onSubmit}
                        disabled={!canSave}
                        style={{ border: `1px solid ${t.btnBorder}`, borderRadius: 6, background: t.btnColor, color: "#fff", padding: "7px 12px", cursor: canSave ? "pointer" : "not-allowed", opacity: canSave ? 1 : 0.55 }}
                    >
                        {localizeText(lang, "Save", "保存", "儲存")}
                    </button>
                </div>
            </div>
        </div>
    );
}

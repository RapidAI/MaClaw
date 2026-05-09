import { useState, useEffect, useCallback } from "react";
import { main } from "../../../wailsjs/go/models";
import { colors } from "./styles";

type Props = {
    config: main.AppConfig | null;
    saveRemoteConfigField: (patch: Partial<main.AppConfig>) => void;
    lang: string;
};

const DEFAULT_NAME = "MaClaw";
const DEFAULT_DESC = "一个尽心尽责无所不能的软件开发管家";

export function MaclawRolePanel({ config, saveRemoteConfigField, lang }: Props) {
    const [name, setName] = useState("");
    const [desc, setDesc] = useState("");
    const [saved, setSaved] = useState(false);

    const t = useCallback(
        (en: string, zhHans: string, zhHant: string = zhHans) =>
            lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en,
        [lang]
    );

    // Sync local state from config on load / external config change
    useEffect(() => {
        if (!config) return;
        setName(config.maclaw_role_name || "");
        setDesc(config.maclaw_role_description || "");
    }, [config?.maclaw_role_name, config?.maclaw_role_description]);

    const showSaved = () => {
        setSaved(true);
        setTimeout(() => setSaved(false), 2000);
    };

    const handleSave = () => {
        const roleName = name.trim() || DEFAULT_NAME;
        saveRemoteConfigField({
            maclaw_role_name: roleName,
            maclaw_role_description: desc.trim() || DEFAULT_DESC,
            group_discussion: {
                ...((config as any)?.group_discussion || {}),
                display_name: roleName,
            },
        } as any);
        showSaved();
    };

    const handleReset = () => {
        setName(DEFAULT_NAME);
        setDesc(DEFAULT_DESC);
        saveRemoteConfigField({
            maclaw_role_name: DEFAULT_NAME,
            maclaw_role_description: DEFAULT_DESC,
            group_discussion: {
                ...((config as any)?.group_discussion || {}),
                display_name: DEFAULT_NAME,
            },
        } as any);
        showSaved();
    };

    return (
        <div>
            <p style={{ fontSize: "0.78rem", color: colors.textSecondary, marginBottom: "14px", lineHeight: 1.5 }}>
                {t(
                    "Customize MaClaw Agent's name and role description. Takes effect immediately after saving. You can also redefine the role during chat.",
                    "自定义 MaClaw Agent 的名字和角色描述。保存后立即生效。用户也可以在聊天中临时重新定义角色。",
                    "自訂 MaClaw Agent 的名稱和角色描述。儲存後立即生效。使用者也可以在聊天中臨時重新定義角色。"
                )}
            </p>

            <div className="form-group" style={{ marginBottom: "12px", display: "flex", alignItems: "center", gap: "10px" }}>
                <label className="form-label" style={{ marginBottom: 0, whiteSpace: "nowrap", minWidth: "60px" }}>{t("Role Name", "角色名称", "角色名稱")}</label>
                <input
                    className="form-input"
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    placeholder={DEFAULT_NAME}
                    spellCheck={false}
                    style={{ maxWidth: "320px", flex: 1 }}
                />
            </div>

            <div className="form-group" style={{ marginBottom: "14px", display: "flex", alignItems: "flex-start", gap: "10px" }}>
                <label className="form-label" style={{ marginBottom: 0, whiteSpace: "nowrap", minWidth: "60px", paddingTop: "6px" }}>{t("Role Description", "角色描述")}</label>
                <textarea
                    className="form-input"
                    value={desc}
                    onChange={(e) => setDesc(e.target.value)}
                    placeholder={DEFAULT_DESC}
                    spellCheck={false}
                    rows={3}
                    style={{ resize: "vertical", minHeight: "60px", fontFamily: "inherit", flex: 1 }}
                />
            </div>

            <div style={{ display: "flex", gap: "10px", alignItems: "center" }}>
                <button className="btn-primary" onClick={handleSave} style={{ minWidth: "90px" }}>
                    {saved ? t("Saved ✓", "已保存 ✓", "已儲存 ✓") : t("Save", "保存", "儲存")}
                </button>
                <button className="btn-secondary" onClick={handleReset} style={{ minWidth: "90px" }}>
                    {t("Reset Default", "恢复默认", "恢復預設")}
                </button>
            </div>

        </div>
    );
}

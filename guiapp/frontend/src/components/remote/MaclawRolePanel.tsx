import { useCallback, useEffect, useState } from 'react';
import { corelib, main } from '../../../wailsjs/go/models';

type Props = {
    config: corelib.AppConfig | null;
    saveRemoteConfigField: (patch: Partial<corelib.AppConfig>) => void;
    lang: string;
};

const DEFAULT_NAME = "MaClaw";
const DEFAULT_DESC = "你的全能数智伴侣MaClaw";

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
        <div className="maclaw-role-panel">
            <p className="maclaw-role-panel__intro">
                {t(
                    "Customize MaClaw Agent's name and role description. Takes effect immediately after saving. You can also redefine the role during chat.",
                    "自定义 MaClaw Agent 的名字和角色描述。保存后立即生效。也可以在聊天中临时重新定义角色。",
                    "自訂 MaClaw Agent 的名稱和角色描述。儲存後立即生效。也可以在聊天中臨時重新定義角色。"
                )}
            </p>

            <div className="form-group maclaw-role-panel__row">
                <label className="form-label maclaw-role-panel__label">{t("Role Name", "角色名称", "角色名稱")}</label>
                <input
                    className="form-input maclaw-role-panel__input"
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    placeholder={DEFAULT_NAME}
                    spellCheck={false}
                />
            </div>

            <div className="form-group maclaw-role-panel__row maclaw-role-panel__row--top">
                <label className="form-label maclaw-role-panel__label maclaw-role-panel__label--textarea">{t("Role Description", "角色描述", "角色描述")}</label>
                <textarea
                    className="form-input maclaw-role-panel__textarea"
                    value={desc}
                    onChange={(e) => setDesc(e.target.value)}
                    placeholder={DEFAULT_DESC}
                    spellCheck={false}
                    rows={3}
                />
            </div>

            <div className="maclaw-role-panel__actions">
                <button className="btn-primary maclaw-role-panel__button" onClick={handleSave}>
                    {saved ? t("Saved", "已保存", "已儲存") : t("Save", "保存", "儲存")}
                </button>
                <button className="btn-secondary maclaw-role-panel__button" onClick={handleReset}>
                    {t("Reset Default", "恢复默认", "恢復預設")}
                </button>
            </div>

        </div>
    );
}

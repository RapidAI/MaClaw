import { useState, useEffect, useCallback } from "react";
import { main } from "../../../wailsjs/go/models";

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
    const [gossipEnabled, setGossipEnabled] = useState(true);

    const t = useCallback(
        (zh: string, en: string) => (lang?.startsWith("zh") ? zh : en),
        [lang]
    );

    // Sync local state from config on load / external config change
    useEffect(() => {
        if (!config) return;
        setName(config.maclaw_role_name || "");
        setDesc(config.maclaw_role_description || "");
        // gossip_auto_publish defaults to true when not explicitly set
        setGossipEnabled(config.gossip_auto_publish !== false);
    }, [config?.maclaw_role_name, config?.maclaw_role_description, config?.gossip_auto_publish]);

    const showSaved = () => {
        setSaved(true);
        setTimeout(() => setSaved(false), 2000);
    };

    const handleSave = () => {
        saveRemoteConfigField({
            maclaw_role_name: name.trim() || DEFAULT_NAME,
            maclaw_role_description: desc.trim() || DEFAULT_DESC,
        });
        showSaved();
    };

    const handleReset = () => {
        setName(DEFAULT_NAME);
        setDesc(DEFAULT_DESC);
        saveRemoteConfigField({
            maclaw_role_name: DEFAULT_NAME,
            maclaw_role_description: DEFAULT_DESC,
        });
        showSaved();
    };

    return (
        <div>
            <p style={{ fontSize: "0.78rem", color: "#888", marginBottom: "14px", lineHeight: 1.5 }}>
                {t(
                    "自定义 MaClaw Agent 的名字和角色描述。保存后立即生效。用户也可以在聊天中临时重新定义角色。",
                    "Customize MaClaw Agent's name and role description. Takes effect immediately after saving. You can also redefine the role during chat."
                )}
            </p>

            <div className="form-group" style={{ marginBottom: "12px", display: "flex", alignItems: "center", gap: "10px" }}>
                <label className="form-label" style={{ marginBottom: 0, whiteSpace: "nowrap", minWidth: "60px" }}>{t("角色名称", "Role Name")}</label>
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
                <label className="form-label" style={{ marginBottom: 0, whiteSpace: "nowrap", minWidth: "60px", paddingTop: "6px" }}>{t("角色描述", "Role Description")}</label>
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
                    {saved ? t("已保存 ✓", "Saved ✓") : t("保存", "Save")}
                </button>
                <button className="btn-secondary" onClick={handleReset} style={{ minWidth: "90px" }}>
                    {t("恢复默认", "Reset Default")}
                </button>
            </div>

            {/* Gossip 聊天八卦自动发帖开关 */}
            <div style={{ marginTop: "20px", borderTop: "1px solid #e0e0e0", paddingTop: "16px" }}>
                <div style={{ display: "flex", alignItems: "center", gap: "10px" }}>
                    <label style={{ display: "flex", alignItems: "center", gap: "8px", cursor: "pointer", fontSize: "0.85rem" }}>
                        <input
                            type="checkbox"
                            checked={gossipEnabled}
                            onChange={(e) => {
                                const val = e.target.checked;
                                setGossipEnabled(val);
                                saveRemoteConfigField({ gossip_auto_publish: val });
                            }}
                            style={{ width: "16px", height: "16px" }}
                        />
                        {t("聊天八卦自动发帖", "Auto-post Chat Gossip")}
                    </label>
                </div>
                <p style={{ fontSize: "0.75rem", color: "#999", marginTop: "6px", lineHeight: 1.4 }}>
                    {t(
                        "开启后，MaClaw 在与你聊天时如果发现有趣的内容，会自动提取精华发布到 Gossip 社区。",
                        "When enabled, MaClaw will automatically extract interesting highlights from your conversations and post them to the Gossip community."
                    )}
                </p>
            </div>


        </div>
    );
}

import type { CSSProperties } from 'react';
import { getModelDisplayName } from '../../config/providerCatalog';

const badgeBaseStyle: CSSProperties = {
    position: 'absolute',
    top: '-8px',
    right: '0px',
    color: 'white',
    fontSize: '10px',
    padding: '1px 6px',
    borderRadius: '999px',
    fontWeight: 'bold',
    zIndex: 10,
    transform: 'scale(0.85)',
    boxShadow: '0 1px 4px rgba(0,0,0,0.15)',
    letterSpacing: '0.02em'
};

export interface ToolConfigurationProps {
    toolName: string;
    toolCfg: any;
    showModelSettings: boolean;
    setShowModelSettings: (show: boolean) => void;
    handleModelSwitch: (name: string) => void;
    t: (key: string) => string;
    lang: string;
}

export const ToolConfiguration = ({
    toolName, toolCfg, showModelSettings, setShowModelSettings,
    handleModelSwitch, t, lang
}: ToolConfigurationProps) => {
    if (!toolCfg || !toolCfg.models) {
        return <div style={{ padding: '15px', color: '#6b7280' }}>{t("loadingConfig")}</div>;
    }

    const getBadge = (model: any): { bg: string; label: string } | null => {
        const name = model.model_name.toLowerCase();
        if (model.model_name === "Original") return { bg: '#6366f1', label: t("originalFlag") };
        if (model.has_subscription) return { bg: '#ec4899', label: t("subscription") };
        if (name.includes("glm") || name.includes("kimi") || name.includes("doubao") || name.includes("minimax"))
            return { bg: '#ec4899', label: t("monthly") };
        if (name.includes("deepseek")) return { bg: '#f59e0b', label: t("premium") };
        if (name.includes("xiaomi")) return { bg: '#f59e0b', label: t("bigSpender") };
        if (model.is_custom) return { bg: '#9ca3af', label: t("customized") };
        if (["aicodemirror", "aigocode", "noin.ai", "gaccode", "coderelay"].some(p => name.includes(p)))
            return { bg: '#14b8a6', label: t("forward") };
        return null;
    };

    return (
        <div style={{
            backgroundColor: 'var(--theme-surface)',
            padding: '12px 14px',
            borderRadius: '14px',
            border: '1px solid var(--theme-border)',
            marginBottom: '10px',
            color: 'var(--theme-text-primary)',
            boxShadow: '0 10px 26px rgba(15, 23, 42, 0.06)'
        }}>
            <div className="model-switcher">
                {toolCfg.models.map((model: any) => {
                    const badge = getBadge(model);
                    return (
                        <button
                            type="button"
                            key={model.model_name}
                            className={`model-btn ${toolCfg.current_model === model.model_name ? 'selected' : ''}`}
                            onClick={() => handleModelSwitch(model.model_name)}
                            style={{
                                borderBottom: (model.api_key && model.api_key.trim() !== "") ? '2px solid var(--primary-color)' : '1px solid var(--theme-border)'
                            }}
                        >
                            {model.model_name === "Original" ? t("original") : getModelDisplayName(model.model_name, lang)}
                            {badge && (
                                <span style={{ ...badgeBaseStyle, backgroundColor: badge.bg }}>
                                    {badge.label}
                                </span>
                            )}
                        </button>
                    );
                })}
            </div>
        </div>
    );
};

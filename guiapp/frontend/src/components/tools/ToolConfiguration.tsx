import type { CSSProperties } from 'react';
import { getModelDisplayName } from '../../config/providerCatalog';

const badgeBaseStyle: CSSProperties = {
    position: 'absolute',
    top: '-8px',
    right: '0px',
    fontSize: '10px',
    padding: '1px 6px',
    borderRadius: 'var(--radius-pill, 999px)',
    fontWeight: 'bold',
    zIndex: 10,
    transform: 'scale(0.85)',
    boxShadow: 'var(--shadow-sm, 0 1px 2px rgba(30,58,95,0.06))',
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
        return <div className="secondary-page-shell tool-configuration-page tool-configuration-page--loading" style={{ padding: '15px', color: 'var(--theme-text-muted, #6b7280)' }}>{t("loadingConfig")}</div>;
    }

    const getBadge = (model: any): { bg: string; fg: string; label: string } | null => {
        const name = model.model_name.toLowerCase();
        const themed = 'var(--theme-on-primary, #ffffff)';
        if (model.model_name === "Original") return { bg: 'var(--theme-primary, #2f5f98)', fg: themed, label: t("originalFlag") };
        if (model.has_subscription) return { bg: 'var(--theme-primary-strong, #183b63)', fg: themed, label: t("subscription") };
        if (name.includes("glm") || name.includes("kimi") || name.includes("doubao") || name.includes("minimax"))
            return { bg: 'var(--theme-primary-strong, #183b63)', fg: themed, label: t("monthly") };
        if (name.includes("deepseek")) return { bg: 'var(--theme-text-muted, #64748b)', fg: themed, label: t("premium") };
        if (name.includes("xiaomi")) return { bg: 'var(--theme-text-muted, #64748b)', fg: themed, label: t("bigSpender") };
        if (model.is_custom) return { bg: 'var(--theme-text-muted, #64748b)', fg: themed, label: t("customized") };
        if (["aicodemirror", "aigocode", "noin.ai", "gaccode", "coderelay"].some(p => name.includes(p)))
            return { bg: 'var(--theme-success, #4f7f6f)', fg: themed, label: t("forward") };
        return null;
    };

    return (
        <div className="secondary-page-shell tool-configuration-page" style={{
            backgroundColor: 'var(--theme-surface)',
            padding: '12px 14px',
            borderRadius: 'var(--radius-lg, 14px)',
            border: '1px solid var(--theme-border)',
            marginBottom: '10px',
            color: 'var(--theme-text-primary)',
            boxShadow: 'var(--shadow-md, 0 1px 2px rgba(30,58,95,0.05), 0 4px 12px -2px rgba(30,58,95,0.10))'
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
                                borderBottom: (model.api_key && model.api_key.trim() !== "") ? '2px solid var(--theme-primary)' : '1px solid var(--theme-border)'
                            }}
                        >
                            {model.model_name === "Original" ? t("original") : getModelDisplayName(model.model_name, lang)}
                            {badge && (
                                <span style={{ ...badgeBaseStyle, backgroundColor: badge.bg, color: badge.fg }}>
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

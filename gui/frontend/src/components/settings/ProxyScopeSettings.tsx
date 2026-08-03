import { corelib } from '../../../wailsjs/go/models';

type ProxyScopeSettingsProps = {
    config: corelib.AppConfig | null;
    isWindows: boolean;
    t: (key: string) => string;
    updateConfig: (patch: Record<string, any>) => void;
};

export const ProxyScopeSettings = ({ config, isWindows, t, updateConfig }: ProxyScopeSettingsProps) => (
    <section className="proxy-settings-scope">
        <div className="proxy-settings-scope__title">{t('proxyScopeTitle')}</div>
        <div className="proxy-settings-scope__grid">
            <label className="proxy-settings-scope__option">
                <input
                    type="checkbox"
                    aria-label={t('proxyScopeMaclaw')}
                    checked={config?.default_proxy_scope_maclaw || false}
                    onChange={(e) => updateConfig({ default_proxy_scope_maclaw: e.target.checked })}
                />
                <span>{t('proxyScopeMaclaw')}</span>
            </label>
            <label className="proxy-settings-scope__option" data-disabled={isWindows ? 'true' : 'false'}>
                <input
                    type="checkbox"
                    aria-label={t('proxyScopeCodingTools')}
                    checked={isWindows ? false : (config?.default_proxy_scope_coding_tools || false)}
                    disabled={isWindows}
                    onChange={(e) => { if (!isWindows) updateConfig({ default_proxy_scope_coding_tools: e.target.checked }); }}
                />
                <span>{t('proxyScopeCodingTools')}</span>
            </label>
            <label className="proxy-settings-scope__option">
                <input
                    type="checkbox"
                    aria-label={t('proxyScopeAgent')}
                    checked={config?.default_proxy_scope_agent || false}
                    onChange={(e) => updateConfig({ default_proxy_scope_agent: e.target.checked })}
                />
                <span>{t('proxyScopeAgent')}</span>
            </label>
        </div>
    </section>
);

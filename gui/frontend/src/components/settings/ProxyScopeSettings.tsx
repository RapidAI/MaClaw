import { main } from '../../../wailsjs/go/models';

type ProxyScopeSettingsProps = {
    config: main.AppConfig | null;
    isWindows: boolean;
    t: (key: string) => string;
    updateConfig: (patch: Record<string, any>) => void;
};

export const ProxyScopeSettings = ({ config, isWindows, t, updateConfig }: ProxyScopeSettingsProps) => (
    <div style={{ marginBottom: '12px', padding: '10px', backgroundColor: 'var(--theme-surface-muted)', borderRadius: '6px', border: '1px solid var(--theme-border)' }}>
        <label className="form-label" style={{ fontSize: '0.78rem', marginBottom: '8px', display: 'block' }}>{t('proxyScopeTitle')}</label>
        <div style={{ display: 'flex', flexDirection: 'column', gap: '6px' }}>
            <label style={{ display: 'flex', alignItems: 'center', cursor: 'pointer', gap: '8px', fontSize: '0.82rem', color: 'var(--theme-text-primary)' }}>
                <input
                    type="checkbox"
                    checked={config?.default_proxy_scope_maclaw || false}
                    onChange={(e) => updateConfig({ default_proxy_scope_maclaw: e.target.checked })}
                />
                {t('proxyScopeMaclaw')}
            </label>
            <label style={{ display: 'flex', alignItems: 'center', cursor: isWindows ? 'not-allowed' : 'pointer', gap: '8px', fontSize: '0.82rem', opacity: isWindows ? 0.45 : 0.75, color: 'var(--theme-text-primary)' }}>
                <input
                    type="checkbox"
                    checked={isWindows ? false : (config?.default_proxy_scope_coding_tools || false)}
                    disabled={isWindows}
                    onChange={(e) => { if (!isWindows) updateConfig({ default_proxy_scope_coding_tools: e.target.checked }); }}
                />
                {t('proxyScopeCodingTools')}
            </label>
            <label style={{ display: 'flex', alignItems: 'center', cursor: 'pointer', gap: '8px', fontSize: '0.82rem', color: 'var(--theme-text-primary)' }}>
                <input
                    type="checkbox"
                    checked={config?.default_proxy_scope_agent || false}
                    onChange={(e) => updateConfig({ default_proxy_scope_agent: e.target.checked })}
                />
                {t('proxyScopeAgent')}
            </label>
        </div>
    </div>
);

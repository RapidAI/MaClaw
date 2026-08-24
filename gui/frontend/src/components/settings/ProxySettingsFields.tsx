import { corelib } from '../../../wailsjs/go/models';

type ProxySettingsFieldsProps = {
    config: corelib.AppConfig | null;
    t: (key: string) => string;
    updateConfig: (patch: Record<string, any>) => void;
};

export const ProxySettingsFields = ({ config, t, updateConfig }: ProxySettingsFieldsProps) => (
    <>
        <div className="proxy-settings-grid proxy-settings-grid--server">
            <div className="proxy-settings-field proxy-settings-field--protocol">
                <label className="form-label">{t("proxyProtocol")}</label>
                <select
                    className="form-input"
                    value={config?.default_proxy_protocol || 'http'}
                    onChange={(e) => updateConfig({ default_proxy_protocol: e.target.value })}
                >
                    <option value="http">HTTP</option>
                    <option value="https">HTTPS</option>
                    <option value="socks5">SOCKS5</option>
                </select>
            </div>
            <div className="proxy-settings-field">
                <label className="form-label">{t("proxyHost")}</label>
                <input
                    type="text"
                    className="form-input"
                    spellCheck={false}
                    placeholder={t("proxyHostPlaceholder")}
                    value={config?.default_proxy_host || ''}
                    onChange={(e) => updateConfig({ default_proxy_host: e.target.value })}
                />
            </div>
            <div className="proxy-settings-field proxy-settings-field--port">
                <label className="form-label">{t("proxyPort")}</label>
                <input
                    type="text"
                    className="form-input"
                    spellCheck={false}
                    placeholder={t("proxyPortPlaceholder")}
                    value={config?.default_proxy_port || ''}
                    onChange={(e) => updateConfig({ default_proxy_port: e.target.value })}
                />
            </div>
        </div>

        <div className="proxy-settings-grid proxy-settings-grid--auth">
            <div className="proxy-settings-field">
                <label className="form-label">{t("proxyUsername")}</label>
                <input
                    type="text"
                    className="form-input"
                    spellCheck={false}
                    autoComplete="off"
                    value={config?.default_proxy_username || ''}
                    onChange={(e) => updateConfig({ default_proxy_username: e.target.value })}
                />
            </div>
            <div className="proxy-settings-field">
                <label className="form-label">{t("proxyPassword")}</label>
                <input
                    type="password"
                    className="form-input"
                    autoComplete="new-password"
                    value={config?.default_proxy_password || ''}
                    onChange={(e) => updateConfig({ default_proxy_password: e.target.value })}
                />
            </div>
        </div>

        <div className="proxy-settings-field">
            <label className="form-label">{t("proxyBypass")}</label>
            <textarea
                className="form-input"
                rows={2}
                spellCheck={false}
                placeholder={t("proxyBypassPlaceholder")}
                value={config?.default_proxy_bypass || ''}
                onChange={(e) => updateConfig({ default_proxy_bypass: e.target.value })}
            />
            <div className="proxy-settings-hint">{t("proxyBypassHint")}</div>
        </div>
    </>
);

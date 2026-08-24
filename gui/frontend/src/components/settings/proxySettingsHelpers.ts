import { corelib } from '../../../wailsjs/go/models';

export const buildConfig = (config: corelib.AppConfig | null, patch: Record<string, any>) => new corelib.AppConfig({ ...(config || {}), ...patch });

export const proxyFormPayload = (config: corelib.AppConfig) => ({
    enabled: config.default_proxy_enabled || false,
    protocol: config.default_proxy_protocol || 'http',
    host: config.default_proxy_host || '',
    port: config.default_proxy_port || '',
    username: config.default_proxy_username || '',
    password: config.default_proxy_password || '',
    bypass: config.default_proxy_bypass || '',
    scope_maclaw: config.default_proxy_scope_maclaw || false,
    scope_coding_tools: config.default_proxy_scope_coding_tools || false,
    scope_agent: config.default_proxy_scope_agent || false,
});

export const errorText = (err: unknown) => {
    if (typeof err === 'string') return err;
    if (err && typeof err === 'object' && 'message' in err && typeof (err as { message: unknown }).message === 'string') {
        return (err as { message: string }).message;
    }
    return String(err || '');
};

export type ProviderModelFetchProtocol = 'anthropic' | 'openai';

export function inferProviderModelFetchProtocol(activeTool: string, modelURL: unknown): ProviderModelFetchProtocol {
    if (activeTool === 'claude') return 'anthropic';

    const rawURL = String(modelURL || '').trim().toLowerCase();
    if (!rawURL) return 'openai';

    try {
        const parsed = new URL(rawURL);
        if (parsed.hostname.split('.').some(segment => segment === 'anthropic')) {
            return 'anthropic';
        }
        if (parsed.pathname.split('/').some(segment => segment === 'anthropic')) {
            return 'anthropic';
        }
    } catch {
        if (/(^|\/)anthropic(\/|$)/.test(rawURL)) return 'anthropic';
    }

    return 'openai';
}

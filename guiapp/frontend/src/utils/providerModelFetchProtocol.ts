export type ProviderModelFetchProtocol = 'anthropic' | 'openai';

export function inferProviderModelFetchProtocol(activeTool: string, modelURL: unknown): ProviderModelFetchProtocol {
    const rawURL = String(modelURL || '').trim().toLowerCase();

    // CodeGen is a hybrid gateway: it speaks Anthropic wire protocol for chat
    // (/v1/messages) but its /v1/models endpoint only accepts OpenAI-style
    // Bearer token auth (not Anthropic's x-api-key header). The Anthropic SDK
    // would send x-api-key + anthropic-version headers which CodeGen rejects
    // with 400. Force OpenAI protocol for model discovery on CodeGen endpoints.
    if (rawURL && rawURL.includes('codegen.qianxin-inc.cn')) return 'openai';

    if (activeTool === 'claude') return 'anthropic';

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

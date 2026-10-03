import { describe, expect, it, vi } from 'vitest';

const { probeMock } = vi.hoisted(() => ({ probeMock: vi.fn() }));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    FetchProviderModels: vi.fn(),
    ProbeMaclawLLMProviderModel: (...args: unknown[]) => probeMock(...args),
    TestAndSaveMaclawLLMProviders: vi.fn(),
}));

import { KNOWN_OPENAI_ENDPOINTS, probeProviderModelForShare } from '../LLMConfigPanelShared';

describe('Maclaw provider quick-fill catalog', () => {
    it('keeps Qwen defaults aligned with the built-in GUI provider', () => {
        const qwen = KNOWN_OPENAI_ENDPOINTS.find(provider => provider.name === 'Qwen');

        expect(qwen).toMatchObject({
            url: 'https://dashscope.aliyuncs.com/compatible-mode/v1',
            model: 'qwen3.8-flash',
            context_length: 400000,
        });
    });
});

describe('share model probe', () => {
    it('probes the catalog model by provider name and does not save the provider list', async () => {
        probeMock.mockReset();
        probeMock.mockRejectedValue(new Error('model "default-model" not found'));

        const outcome = await probeProviderModelForShare(
            {
                name: 'WorkBuddy 国际版',
                url: 'https://www.workbuddy.ai/v2',
                key: '',
                model: 'deepseek-v4.1-flash',
            },
            'default-model',
            (en) => en,
        );

        expect(probeMock).toHaveBeenCalledTimes(1);
        expect(probeMock).toHaveBeenCalledWith('WorkBuddy 国际版', 'default-model');
        expect(outcome.ok).toBe(false);
        expect(outcome.error).toContain('default-model');
        expect(outcome.error).not.toContain('deepseek-v4.1-flash');
    });
});

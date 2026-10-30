import { describe, expect, it, vi } from 'vitest';

const { probeMock } = vi.hoisted(() => ({ probeMock: vi.fn() }));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    FetchProviderModels: vi.fn(),
    ProbeMaclawLLMProviderModel: (...args: unknown[]) => probeMock(...args),
    TestAndSaveMaclawLLMProviders: vi.fn(),
}));

import { KNOWN_OPENAI_ENDPOINTS, nextProviderModelChoices, probeProviderModelForShare, providerModelChoices, quickFillModelCatalog } from '../LLMConfigPanelShared';
import { PROVIDER_LOGOS } from '../providerLogos';

describe('Maclaw provider quick-fill catalog', () => {
    it('lists Google AI Studio on the OpenAI-compatible API key base', () => {
        const google = KNOWN_OPENAI_ENDPOINTS.find(provider => provider.name === 'Google AI Studio');

        expect(google).toMatchObject({
            url: 'https://generativelanguage.googleapis.com/v1beta/openai',
            model: 'gemini-3.8-flash',
            protocol: 'openai',
            context_length: 1000000,
            models: ['gemini-3.8-flash', 'gemini-4-argon'],
        });
        expect(google?.auth_type ?? '').toBe('');
        expect(google?.wire_api ?? '').toBe('');
        expect(PROVIDER_LOGOS['Google AI Studio']).toBeTruthy();
    });

    it('lists Google Gemini on the native generateContent base', () => {
        const gemini = KNOWN_OPENAI_ENDPOINTS.find(provider => provider.name === 'Google Gemini');

        expect(gemini).toMatchObject({
            url: 'https://generativelanguage.googleapis.com/v1beta',
            model: 'gemini-3.8-flash',
            protocol: 'google',
            context_length: 1000000,
            models: ['gemini-3.8-flash', 'gemini-4-argon'],
        });
        expect(gemini?.auth_type ?? '').toBe('');
        expect(gemini?.wire_api ?? '').toBe('');
        expect(PROVIDER_LOGOS['Google Gemini']).toBeTruthy();
    });

    it('keeps Qwen defaults aligned with the built-in GUI provider', () => {
        const qwen = KNOWN_OPENAI_ENDPOINTS.find(provider => provider.name === 'Qwen');

        expect(qwen).toMatchObject({
            url: 'https://dashscope.aliyuncs.com/compatible-mode/v1',
            model: 'qwen3.8-flash',
            context_length: 400000,
        });
    });

    it('lists Tencent Token Plan on the mainland OpenAI chat base', () => {
        const tencent = KNOWN_OPENAI_ENDPOINTS.find(provider => provider.name === '腾讯TokenPlan');

        expect(tencent).toMatchObject({
            url: 'https://tokenhub.tencentmaas.com/plan/v3',
            model: 'auto',
            protocol: 'openai',
            context_length: 128000,
        });
        expect(tencent?.wire_api ?? '').toBe('');
        expect(PROVIDER_LOGOS['腾讯TokenPlan']).toBeTruthy();
        expect(PROVIDER_LOGOS['腾讯云']).toBeTruthy();
    });

    it('lists China Telecom Xingchen on the MaaS OpenAI chat base', () => {
        const xingchen = KNOWN_OPENAI_ENDPOINTS.find(provider => provider.name === '星辰大模型');

        expect(xingchen).toMatchObject({
            url: 'https://api-maas.teleai.com.cn/v1',
            model: 'TeleChat3-438B-A24B',
            protocol: 'openai',
            context_length: 256000,
            models: ['TeleChat3-438B-A24B', 'Xing4.0-29B-A4B'],
        });
        expect(PROVIDER_LOGOS['星辰大模型']).toBeTruthy();
        expect(providerModelChoices({
            model: 'Xing4.0-29B-A4B',
            models: ['TeleChat3-438B-A24B', 'Xing4.0-29B-A4B'],
        }).map(model => model.id)).toEqual(['TeleChat3-438B-A24B', 'Xing4.0-29B-A4B']);
    });

    it('replaces a stale model catalog when quick-fill changes provider', () => {
        const xingchen = KNOWN_OPENAI_ENDPOINTS.find(provider => provider.name === '星辰大模型');
        const deepseek = KNOWN_OPENAI_ENDPOINTS.find(provider => provider.name === 'DeepSeek');
        expect(xingchen && deepseek).toBeTruthy();
        expect(quickFillModelCatalog(
            { name: 'DeepSeek', models: ['deepseek-chat', 'deepseek-reasoner'] },
            xingchen!,
        )).toEqual(['TeleChat3-438B-A24B', 'Xing4.0-29B-A4B']);
        expect(quickFillModelCatalog(
            { name: '星辰大模型', models: ['TeleChat3-438B-A24B', 'Xing4.0-29B-A4B'] },
            deepseek!,
        )).toEqual(['deepseek-chat']);
        expect(quickFillModelCatalog(
            { name: 'DeepSeek', models: ['deepseek-chat', 'deepseek-reasoner'] },
            deepseek!,
        )).toEqual(['deepseek-chat', 'deepseek-reasoner']);
        expect(nextProviderModelChoices(
            [{ id: 'old-model', name: 'Old' }],
            { model: 'TeleChat3-438B-A24B', models: ['TeleChat3-438B-A24B', 'Xing4.0-29B-A4B'] },
        ).map(model => model.id)).toEqual(['TeleChat3-438B-A24B', 'Xing4.0-29B-A4B']);
        expect(nextProviderModelChoices(
            [{ id: 'TeleChat3-438B-A24B', name: '旗舰' }, { id: 'Xing4.0-29B-A4B', name: '轻量' }],
            { model: 'TeleChat3-438B-A24B', models: ['TeleChat3-438B-A24B', 'Xing4.0-29B-A4B'] },
        )).toEqual([
            { id: 'TeleChat3-438B-A24B', name: '旗舰' },
            { id: 'Xing4.0-29B-A4B', name: '轻量' },
        ]);
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

import { describe, expect, it } from 'vitest';
import { KNOWN_OPENAI_ENDPOINTS } from '../LLMConfigPanelShared';

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

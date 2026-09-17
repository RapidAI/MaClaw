import { describe, expect, it } from 'vitest';
import { describeAIAssistantError, localizeAIAssistantError, localizeText } from '../aiAssistantI18n';

describe('aiAssistantI18n', () => {
    it('normalizes common locale variants', () => {
        expect(localizeText('en-US', 'English', '\u7b80\u4f53', '\u7e41\u9ad4')).toBe('English');
        expect(localizeText('zh-CN', 'English', '\u7b80\u4f53', '\u7e41\u9ad4')).toBe('\u7b80\u4f53');
        expect(localizeText('zh-TW', 'English', '\u7b80\u4f53', '\u7e41\u9ad4')).toBe('\u7e41\u9ad4');
        expect(localizeText('zh-HK', 'English', '\u7b80\u4f53', '\u7e41\u9ad4')).toBe('\u7e41\u9ad4');
    });

    it('keeps the existing default as simplified Chinese', () => {
        expect(localizeText('', 'English', '\u7b80\u4f53', '\u7e41\u9ad4')).toBe('\u7b80\u4f53');
    });

    it('localizes insufficient-credit errors while preserving amounts', () => {
        const error = 'LLM call failed: insufficient credits for this request: need 14.039 credits, available 9.360';
        expect(localizeAIAssistantError(error, 'zh-Hans')).toBe('LLM 调用失败：本次请求额度不足，需要 14.039 Credits，当前可用 9.360 Credits。');
        expect(localizeAIAssistantError(error, 'zh-Hant')).toBe('LLM 調用失敗：本次請求額度不足，需要 14.039 Credits，目前可用 9.360 Credits。');
        expect(localizeAIAssistantError(error, 'en-US')).toBe(error);
        expect(localizeAIAssistantError('LLM 调用失败：本次请求额度不足，需要 14.039 Credits，当前可用 9.360 Credits。', 'en')).toBe(error);
    });

    it('preserves the held-by-in-flight amount on insufficient-credit errors', () => {
        const error = 'LLM call failed: insufficient credits for this request: need 56.003 credits, available 6.430 (993.570 held by in-flight requests)';
        expect(localizeAIAssistantError(error, 'zh-Hans')).toBe('LLM 调用失败：本次请求额度不足，需要 56.003 Credits，当前可用 6.430 Credits，其中 993.570 Credits 被在途请求冻结。');
        expect(localizeAIAssistantError(error, 'en-US')).toBe(error);
        const localized = 'LLM 调用失败：本次请求额度不足，需要 56.003 Credits，当前可用 6.430 Credits，其中 993.570 Credits 被在途请求冻结。';
        expect(localizeAIAssistantError(localized, 'en')).toBe(error);
        expect(localizeAIAssistantError(localized, 'zh-Hant')).toBe('LLM 調用失敗：本次請求額度不足，需要 56.003 Credits，目前可用 6.430 Credits，其中 993.570 Credits 被在途請求凍結。');
    });

    it('localizes shorter exhausted-credit errors', () => {
        expect(localizeAIAssistantError('LLM call failed: current period credit limit is exhausted', 'zh-Hans')).toBe('LLM 调用失败：当前周期额度已用尽。');
        expect(localizeAIAssistantError('LLM call failed: current period credit limit is exhausted', 'en')).toBe('LLM call failed: current period credit limit is exhausted');
        expect(localizeAIAssistantError('LLM call failed: insufficient credits', 'en')).toBe('LLM call failed: insufficient credits');
    });

    it('localizes the common LLM failure prefix and stable provider details', () => {
        expect(localizeAIAssistantError('LLM call failed: timeout', 'zh-Hans')).toBe('LLM 调用失败：请求超时');
        expect(localizeAIAssistantError('LLM call failed: HTTP 503: service unavailable', 'zh-Hant')).toBe('LLM 調用失敗：HTTP 503: 服務暫不可用');
        expect(localizeAIAssistantError('LLM 调用失败: timeout', 'en')).toBe('LLM call failed: timeout');
        expect(localizeAIAssistantError('LLM call failed: HTTP 429: current period credit limit is exhausted', 'zh-Hans')).toBe('LLM 调用失败：HTTP 429: 当前周期额度已用尽');
    });

    it('translates Hub service denials that arrive pre-localized by the backend', () => {
        expect(localizeAIAssistantError('MaClaw 官方额度已用尽：请兑换额度或切换其他模型提供方。', 'en')).toBe('MaClaw official credits are exhausted. Redeem more credits or switch to another provider.');
        expect(localizeAIAssistantError('MaClaw 官方周期限流：当前周期额度已用尽，约 1 小时 后恢复。', 'en')).toBe('MaClaw official service is period-limited: current-period credits are exhausted; service resumes in about 1 hour.');
        expect(localizeAIAssistantError('MaClaw 官方周期限流：当前周期额度已用尽，约 2 小时 后恢复。', 'zh-Hant')).toBe('MaClaw 官方週期限流：目前週期額度已用盡，約 2 小時 後恢復。');
        expect(localizeAIAssistantError('MaClaw 官方周期限流：当前周期额度已用尽。', 'en')).toBe('MaClaw official service is period-limited: current-period credits are exhausted. Please try again later.');
        expect(localizeAIAssistantError('MaClaw 官方服务商暂不可用：请刷新 Hub 服务状态后重试。', 'en')).toBe('MaClaw official provider is temporarily unavailable. Refresh Hub service status and try again.');
    });

    it('describes credit errors with title, amounts, and an actionable hint', () => {
        expect(describeAIAssistantError('LLM 调用失败：本次请求额度不足，需要 55.074 Credits，当前可用 6.428 Credits。', 'zh-Hans'))
            .toEqual({
                title: '额度不足',
                detail: '本次请求需要 55.074 Credits，当前可用 6.428 Credits。',
                hint: '请兑换额度或切换模型提供方后重试。',
            });
        expect(describeAIAssistantError('LLM call failed: insufficient credits for this request: need 14.039 credits, available 9.360', 'en'))
            .toEqual({
                title: 'Insufficient credits',
                detail: 'This request needs 14.039 credits, but only 9.360 are available.',
                hint: 'Redeem more credits or switch providers, then try again.',
            });
    });

    it('describes credit errors with a held amount when in-flight requests hold credits', () => {
        expect(describeAIAssistantError('LLM call failed: insufficient credits for this request: need 56.003 credits, available 6.430 (993.570 held by in-flight requests)', 'en'))
            .toEqual({
                title: 'Insufficient credits',
                detail: 'This request needs 56.003 credits, but only 6.430 are available (993.570 held by in-flight requests).',
                hint: 'Redeem more credits or switch providers, then try again.',
            });
        expect(describeAIAssistantError('LLM 调用失败：本次请求额度不足，需要 56.003 Credits，当前可用 6.430 Credits，其中 993.570 Credits 被在途请求冻结。', 'zh-Hans'))
            .toEqual({
                title: '额度不足',
                detail: '本次请求需要 56.003 Credits，当前可用 6.430 Credits，其中 993.570 被在途请求冻结。',
                hint: '请兑换额度或切换模型提供方后重试。',
            });
    });

    it('describes exhausted-credit and timeout errors with short titles', () => {
        expect(describeAIAssistantError('LLM 调用失败：当前周期额度已用尽。', 'zh-Hans'))
            .toEqual({ title: '当前周期额度已用尽', hint: '请兑换额度或切换模型提供方后重试。' });
        expect(describeAIAssistantError('LLM 調用失敗：目前授權額度已用盡。', 'zh-Hant'))
            .toEqual({ title: '目前授權額度已用盡', hint: '請兌換額度或切換模型提供方後重試。' });
        expect(describeAIAssistantError('LLM call failed: timeout', 'en'))
            .toEqual({ title: 'Request timed out', hint: 'Please try again later.' });
        expect(describeAIAssistantError('LLM 调用失败：请求超时', 'zh-Hans'))
            .toEqual({ title: '请求超时', hint: '请稍后重试。' });
    });

    it('splits generic LLM failures into prefix title and verbatim detail', () => {
        expect(describeAIAssistantError('LLM 调用失败：HTTP 429: 当前周期额度已用尽', 'zh-Hans'))
            .toEqual({ title: 'LLM 调用失败', detail: 'HTTP 429: 当前周期额度已用尽' });
        expect(describeAIAssistantError('Request failed', 'en'))
            .toEqual({ title: 'Request failed' });
        expect(describeAIAssistantError('  ', 'en')).toEqual({ title: '' });
    });

    it('splits official-service denials at the headline colon without adding a hint', () => {
        expect(describeAIAssistantError('MaClaw 官方额度已用尽：请兑换额度或切换其他模型提供方。', 'zh-Hans'))
            .toEqual({ title: 'MaClaw 官方额度已用尽', detail: '请兑换额度或切换其他模型提供方。' });
        expect(describeAIAssistantError('MaClaw official service is period-limited: current-period credits are exhausted; service resumes in about 1 hour.', 'en'))
            .toEqual({
                title: 'MaClaw official service is period-limited',
                detail: 'current-period credits are exhausted; service resumes in about 1 hour.',
            });
        // No colon: keep the full sentence as the title.
        expect(describeAIAssistantError('MaClaw official credits are exhausted. Redeem more credits or switch to another provider.', 'en'))
            .toEqual({ title: 'MaClaw official credits are exhausted. Redeem more credits or switch to another provider.' });
    });
});

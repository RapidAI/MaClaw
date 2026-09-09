import { describe, expect, it } from 'vitest';
import { localizeAIAssistantError, localizeText } from '../aiAssistantI18n';

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
});

import { describe, expect, it } from 'vitest';
import { localizeText } from '../aiAssistantI18n';

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
});

import { describe, expect, it } from 'vitest';
import { botChatIsNearBottom, botChatLogStamp, pinBotChatToBottom } from './botChatScroll';

describe('botChatScroll', () => {
    it('pins a log to the tail and leaves it when it is already there', () => {
        const el = { scrollHeight: 500, clientHeight: 100, scrollTop: 0 } as HTMLElement;
        expect(pinBotChatToBottom(el)).toBe(true);
        expect(el.scrollTop).toBe(400);
        expect(pinBotChatToBottom(el)).toBe(false);
        expect(pinBotChatToBottom(null)).toBe(false);
    });

    it('treats a short gap above the tail as still following', () => {
        expect(botChatIsNearBottom({ scrollHeight: 500, clientHeight: 100, scrollTop: 340 })).toBe(true);
        expect(botChatIsNearBottom({ scrollHeight: 500, clientHeight: 100, scrollTop: 300 })).toBe(false);
    });

    it('changes the stamp when the visible transcript changes', () => {
        const tail = '结尾二十四字保持不变保持不变保持';
        const first = botChatLogStamp([{ id: 'm1', content: `${'甲'.repeat(40)}${tail}` }]);
        expect(botChatLogStamp([{ id: 'm1', content: `${'甲'.repeat(40)}${tail}` }])).toBe(first);
        expect(botChatLogStamp([{ id: 'm1', content: `${'乙'.repeat(40)}${tail}` }])).not.toBe(first);
        expect(botChatLogStamp([{ id: 'm1', content: '在。', images: [{}] }])).not.toBe(botChatLogStamp([{ id: 'm1', content: '在。' }]));
        expect(botChatLogStamp([{ id: 'm1', content: '安排', phase: 'plan' }])).not.toBe(botChatLogStamp([{ id: 'm1', content: '安排' }]));
        expect(botChatLogStamp([{ id: 'm1', content: '在。', askUser: { options: ['继续'] } }])).not.toBe(botChatLogStamp([{ id: 'm1', content: '在。', askUser: { options: ['停止'] } }]));
    });
});

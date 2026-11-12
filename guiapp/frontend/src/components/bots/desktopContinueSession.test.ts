import { describe, expect, it } from 'vitest';
import { continueSessionForSend, desktopThreadOf, routedLine } from './desktopContinueSession';

describe('continueSessionForSend', () => {
    it('forwards only an explicit choice; otherwise the server decides', () => {
        expect(continueSessionForSend('bot_1')).toBe('');
        expect(continueSessionForSend('bot_1', {})).toBe('');
        expect(continueSessionForSend('bot_1', { session: 'bot_1#t-a' })).toBe('bot_1#t-a');
        expect(continueSessionForSend('bot_1', { session: 'bot_2#t-a' })).toBe('');
        expect(continueSessionForSend('bot_1', { main: true, session: 'bot_1#t-a' })).toBe('bot_1');
    });
    it('knows a thread of this bot', () => {
        expect(desktopThreadOf('bot_1', 'bot_1#t-a')).toBe('bot_1#t-a');
        expect(desktopThreadOf('bot_1', 'bot_1')).toBe('');
        expect(desktopThreadOf('bot_1', 'bot_10#t-a')).toBe('');
    });
    it('words the route line', () => {
        expect(routedLine('zh-Hans', '整理报表')).toBe('接着「整理报表」这件事');
        expect(routedLine('en', '')).toBe('Continuing an earlier task');
    });
});

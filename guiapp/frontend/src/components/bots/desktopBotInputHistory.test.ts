// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from 'vitest';
import {
    DESKTOP_BOT_INPUT_ENTRY_MAX,
    DESKTOP_BOT_INPUT_HISTORY_KEY,
    DESKTOP_BOT_INPUT_HISTORY_MAX,
    botInputHistory,
    rememberBotInput,
} from './desktopBotInputHistory';

const ASSISTANT_PROMPT_HISTORY_KEY = 'ai-assistant-prompt-history';

beforeEach(() => {
    localStorage.clear();
});

describe('desktopBotInputHistory', () => {
    it('stores trimmed commands per account and leaves the assistant history alone', () => {
        localStorage.setItem(ASSISTANT_PROMPT_HISTORY_KEY, JSON.stringify(['助手里的一句']));
        expect(rememberBotInput('alice', '  打开示例网站  ')).toEqual(['打开示例网站']);
        expect(rememberBotInput('alice', '再截一张桌面')).toEqual(['打开示例网站', '再截一张桌面']);
        expect(rememberBotInput(' bob ', '鲍勃的任务')).toEqual(['鲍勃的任务']);
        expect(botInputHistory('alice')).toEqual(['打开示例网站', '再截一张桌面']);
        expect(botInputHistory('bob')).toEqual(['鲍勃的任务']);
        expect(botInputHistory('')).toEqual([]);
        expect(JSON.parse(localStorage.getItem(ASSISTANT_PROMPT_HISTORY_KEY) || '[]')).toEqual(['助手里的一句']);
        expect(localStorage.getItem(DESKTOP_BOT_INPUT_HISTORY_KEY)).toContain('打开示例网站');
        expect(localStorage.getItem(DESKTOP_BOT_INPUT_HISTORY_KEY)).not.toContain('助手里的一句');
    });

    it('keeps one copy of a command sent twice in a row and caps the list', () => {
        rememberBotInput('alice', '打开示例网站');
        expect(rememberBotInput('alice', '打开示例网站')).toEqual(['打开示例网站']);
        rememberBotInput('alice', '再截一张');
        expect(rememberBotInput('alice', '打开示例网站')).toEqual(['打开示例网站', '再截一张', '打开示例网站']);
        for (let i = 0; i < DESKTOP_BOT_INPUT_HISTORY_MAX + 5; i += 1) {
            rememberBotInput('alice', `任务 ${i}`);
        }
        const stored = botInputHistory('alice');
        expect(stored).toHaveLength(DESKTOP_BOT_INPUT_HISTORY_MAX);
        expect(stored[stored.length - 1]).toBe(`任务 ${DESKTOP_BOT_INPUT_HISTORY_MAX + 4}`);
        expect(stored).not.toContain('打开示例网站');
    });

    it('ignores blank text and unreadable storage', () => {
        expect(rememberBotInput('alice', '   ')).toEqual([]);
        localStorage.setItem(DESKTOP_BOT_INPUT_HISTORY_KEY, '{');
        expect(botInputHistory('alice')).toEqual([]);
        localStorage.setItem(DESKTOP_BOT_INPUT_HISTORY_KEY, JSON.stringify({ alice: ['  留下  ', '', 3, '  '] }));
        expect(botInputHistory('alice')).toEqual(['留下']);
    });

    it('drops an oversized line and keeps the newest commands from a long list', () => {
        const huge = '长'.repeat(DESKTOP_BOT_INPUT_ENTRY_MAX + 1);
        expect(rememberBotInput('alice', huge)).toEqual([]);
        expect(localStorage.getItem(DESKTOP_BOT_INPUT_HISTORY_KEY)).toBeNull();
        const aged = Array.from({ length: DESKTOP_BOT_INPUT_HISTORY_MAX + 40 }, (_, index) => `旧 ${index}`);
        localStorage.setItem(DESKTOP_BOT_INPUT_HISTORY_KEY, JSON.stringify({
            alice: ['  ', huge, 4, ...aged],
        }));
        const stored = botInputHistory('alice');
        expect(stored).toHaveLength(DESKTOP_BOT_INPUT_HISTORY_MAX);
        expect(stored[0]).toBe('旧 40');
        expect(stored[stored.length - 1]).toBe(`旧 ${DESKTOP_BOT_INPUT_HISTORY_MAX + 39}`);
        expect(stored).not.toContain(huge);
    });
});

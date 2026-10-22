import { describe, expect, it } from 'vitest';
import { isBareContinue } from './DesktopBotWorkspace';

describe('isBareContinue', () => {
    it('treats a bare continue as resuming, not a new order', () => {
        for (const text of ['继续', '继续。', ' 繼續吧 ', 'continue', 'Go on!', '接着做']) expect(isBareContinue(text)).toBe(true);
    });
    it('keeps real orders as orders', () => {
        for (const text of ['继续写知乎回答', 'continue with the report', '帮我发邮件', '']) expect(isBareContinue(text)).toBe(false);
    });
});

import { runWasInterrupted } from './DesktopBotWorkspace';

describe('runWasInterrupted', () => {
    it('tells an interruption from an ordinary failure', () => {
        expect(runWasInterrupted('run interrupted: service restarted')).toBe(true);
        expect(runWasInterrupted('任务被中断')).toBe(true);
        expect(runWasInterrupted('page not found')).toBe(false);
    });
});

import { desktopBotVariedFallback } from './desktopBotAck';

describe('desktopBotVariedFallback', () => {
    it('keeps the state line, but never repeats what was just said', () => {
        expect(desktopBotVariedFallback('queued', 'zh-Hans', '好，这个等手上的做完我就做。', [])).toBe('好，这个等手上的做完我就做。');
        const next = desktopBotVariedFallback('queued', 'zh-Hans', '好，这个等手上的做完我就做。', ['我: 好，这个等手上的做完我就做。']);
        expect(next).not.toBe('好，这个等手上的做完我就做。');
        const third = desktopBotVariedFallback('queued', 'zh-Hans', '好，这个等手上的做完我就做。', ['我: 好，这个等手上的做完我就做。', `我: ${next}`]);
        expect([next, '好，这个等手上的做完我就做。']).not.toContain(third);
        expect(desktopBotVariedFallback('interrupted', 'en', 'It stopped.', ['me: It stopped.'])).toMatch(/pick it up|resume/);
    });
});

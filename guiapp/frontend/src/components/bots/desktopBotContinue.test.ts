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

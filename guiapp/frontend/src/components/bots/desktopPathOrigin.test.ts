import { describe, expect, it } from 'vitest';
import { classifyBotPath } from './desktopPathOrigin';

describe('classifyBotPath', () => {
    it('keeps a desktop-run home path on the desktop', () => {
        expect(classifyBotPath('/home/desktop/work/bot_a/7c0236e7/snake.cpp', true)).toBe('desktop');
        expect(classifyBotPath('~/Desktop/shot.png', true)).toBe('desktop');
    });

    it('leaves a person-machine path local even inside a desktop run', () => {
        expect(classifyBotPath('/Users/dan/Desktop/report.pdf', true)).toBe('local');
    });

    it('does not treat a home path as a desktop file outside a desktop run', () => {
        expect(classifyBotPath('/home/desktop/work/a.cpp', false)).toBe('local');
    });

    it('keeps a drive path on this machine', () => {
        expect(classifyBotPath('C:\\Users\\dan\\a.txt', true)).toBe('local');
    });
});

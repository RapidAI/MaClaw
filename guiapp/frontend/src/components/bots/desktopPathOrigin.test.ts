import { describe, expect, it } from 'vitest';
import { classifyBotPath } from './desktopPathOrigin';

describe('classifyBotPath', () => {
    it('fetches a work or Desktop path from the desktop', () => {
        expect(classifyBotPath('/home/desktop/work/bot_a/7c0236e7/snake.cpp')).toBe('desktop');
        expect(classifyBotPath('~/Desktop/shot.png')).toBe('desktop');
    });

    it('opens only a path this GUI wrote', () => {
        const saved = 'C:\\Users\\alice\\.maclaw\\data\\bot-files\\bot_1\\shots\\shot-1.png';
        expect(classifyBotPath(saved, { guiPaths: [saved] })).toBe('local');
        expect(classifyBotPath(saved)).toBe('text');
    });

    it('does not open another machine path or a hidden desktop path', () => {
        expect(classifyBotPath('/Users/dan/Desktop/report.pdf')).toBe('text');
        expect(classifyBotPath('/home/desktop/work/bot_a/.secret/key')).toBe('text');
        expect(classifyBotPath('/home/desktop/Documents/a.txt')).toBe('text');
        expect(classifyBotPath('C:\\Users\\dan\\a.txt')).toBe('text');
    });
});

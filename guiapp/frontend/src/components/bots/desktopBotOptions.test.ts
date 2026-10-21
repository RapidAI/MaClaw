import { describe, expect, it } from 'vitest';
import { desktopBotOptionsOf } from './DesktopBotWorkspace';

describe('desktopBotOptionsOf', () => {
    it('keeps 2 to 4 distinct options', () => {
        expect(desktopBotOptionsOf({ options: [' 发笔记 ', '评论', '发笔记', '', '私信', '简介', '多余'] })).toEqual(['发笔记', '评论', '私信', '简介']);
    });
    it('drops a single option or a non-list', () => {
        expect(desktopBotOptionsOf({ options: ['only'] })).toEqual([]);
        expect(desktopBotOptionsOf({ options: 'a/b' })).toEqual([]);
        expect(desktopBotOptionsOf(undefined)).toEqual([]);
    });
});

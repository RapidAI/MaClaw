import { describe, expect, it } from 'vitest';
import { formatBytes, mergeTransfer, type BotLocalTransfer } from './botLocalAccess';

const ev = (id: string, bot = 'b1', done = 0): BotLocalTransfer => ({ id, bot_id: bot, direction: 'to_local', done, state: 'running' });

describe('local relay transfers', () => {
    it('keeps the newest event per id', () => {
        let list = mergeTransfer([], ev('a', 'b1', 1));
        list = mergeTransfer(list, ev('a', 'b1', 5));
        expect(list).toHaveLength(1);
        expect(list[0].done).toBe(5);
    });
    it('bounds each bot separately', () => {
        let list: BotLocalTransfer[] = [];
        for (let i = 0; i < 9; i++) list = mergeTransfer(list, ev(`a${i}`), 3);
        list = mergeTransfer(list, ev('other', 'b2'), 3);
        expect(list.filter(x => x.bot_id === 'b1').map(x => x.id)).toEqual(['a6', 'a7', 'a8']);
        expect(list.some(x => x.id === 'other')).toBe(true);
    });
    it('formats sizes', () => {
        expect(formatBytes(512)).toBe('512 B');
        expect(formatBytes(3 * 1024 * 1024)).toBe('3.0 MB');
    });
});

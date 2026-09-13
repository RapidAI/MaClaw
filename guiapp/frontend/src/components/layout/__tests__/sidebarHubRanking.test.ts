import { describe, expect, it } from 'vitest';
import { systemRankingLabel } from '../sidebarHubRanking';

describe('systemRankingLabel', () => {
    it('keeps the fallback label before a place exists', () => {
        expect(systemRankingLabel('zh-Hans', null, '排名')).toBe('排名');
        expect(systemRankingLabel('zh-Hans', { tokenRank: 0, durationRank: 0 }, '排名')).toBe('排名');
    });

    it('shows the better place as 第n名', () => {
        expect(systemRankingLabel('zh-Hans', { tokenRank: 2, durationRank: 1 }, '排名')).toBe('第1名');
        expect(systemRankingLabel('zh-Hans', { tokenRank: 1, durationRank: 4 }, '排名')).toBe('第1名');
        expect(systemRankingLabel('zh-Hant', { tokenRank: 2, durationRank: 1 }, '排名')).toBe('第1名');
        expect(systemRankingLabel('en', { tokenRank: 2, durationRank: 1 }, 'Rank')).toBe('#1');
    });

    it('uses whichever positive place is available', () => {
        expect(systemRankingLabel('zh-Hans', { tokenRank: 3, durationRank: 0 }, '排名')).toBe('第3名');
        expect(systemRankingLabel('zh-Hans', { tokenRank: 0, durationRank: 5 }, '排名')).toBe('第5名');
    });
});

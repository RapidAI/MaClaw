import { describe, expect, it } from 'vitest';
import { imSubTabOptions } from '../IMSubTabs';

describe('imSubTabOptions', () => {
    it('hides Lansenger by default', () => {
        expect(imSubTabOptions('en').some(tab => tab.key === 'lansenger')).toBe(false);
    });

    it('shows Lansenger for TigerClaw IM settings', () => {
        expect(imSubTabOptions('zh-Hans', { showLansenger: true }).some(tab => tab.key === 'lansenger' && tab.label === '蓝信')).toBe(true);
    });
});

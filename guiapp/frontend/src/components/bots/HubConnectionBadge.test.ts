import { describe, expect, it } from 'vitest';
import { hubBadgeLabel } from './HubConnectionBadge';

describe('hubBadgeLabel', () => {
    it('hides when connected and shows retry state otherwise', () => {
        expect(hubBadgeLabel({ state: 'connected' }, 'zh-Hans')).toBe('');
        expect(hubBadgeLabel({ state: 'connecting', next_retry_ms: 4200 }, 'zh-Hans')).toBe('正在重连 Hub（5 秒后重试）');
        expect(hubBadgeLabel({ state: 'connecting' }, 'en')).toBe('Reconnecting to Hub…');
        expect(hubBadgeLabel({ state: 'offline' }, 'zh-Hans')).toBe('Hub 未连接');
    });
});

import { describe, expect, it } from 'vitest';
import { wakeFailureReason } from './desktopWake';

describe('wakeFailureReason', () => {
    it('names known causes in plain words', () => {
        expect(wakeFailureReason(new Error('maclawsrv request failed: docker service request failed: Post "https://dockerd/v1/desktops/session": context deadline exceeded'))).toBe('等了一分钟电脑仍没有起来');
        expect(wakeFailureReason('docker request failed: no free desktop display')).toBe('没有空闲的屏幕，请稍后再试');
        expect(wakeFailureReason('')).toBe('电脑没有返回画面，请稍后再试');
    });
    it('keeps an unknown error, clipped', () => {
        expect(wakeFailureReason('x'.repeat(200))).toHaveLength(121);
    });
});

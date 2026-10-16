/** @vitest-environment jsdom */
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

const querySecurityEventsMock = vi.fn();

vi.mock('../../../wailsjs/go/main/App', () => ({
    QuerySecurityEvents: (...args: unknown[]) => querySecurityEventsMock(...args),
}));

import { SecurityEventsDialog } from '../SecurityEventsDialog';

const zhTranslations: Record<string, string> = {
    close: '\u5173\u95ed',
    securityEvents: '\u5b89\u5168\u4e8b\u4ef6',
    securityEventsDeniedSummary: '\u6700\u8fd1 7 \u5929\u5171 {count} \u6761\u88ab\u62d2\u7edd\u7684\u64cd\u4f5c',
    securityEventsLoading: '\u52a0\u8f7d\u4e2d...',
    securityEventsLoadFailed: '\u52a0\u8f7d\u5931\u8d25\uff1a',
    securityEventsAllClear: '\u4e00\u5207\u5b89\u5168\uff0c\u6700\u8fd1 7 \u5929\u6ca1\u6709\u88ab\u62d2\u7edd\u7684\u64cd\u4f5c',
    securityEventsTime: '\u65f6\u95f4',
    securityEventsTool: '\u5de5\u5177/\u64cd\u4f5c',
    securityEventsTarget: '\u76ee\u6807',
    securityEventsRemoteIp: '\u8fdc\u7a0b IP',
    securityEventsRisk: '\u98ce\u9669',
    securityEventsReason: '\u62d2\u7edd\u539f\u56e0',
    securityRiskCritical: '\u4e25\u91cd',
    securityRiskHigh: '\u9ad8',
    securityRiskMedium: '\u4e2d',
    securityRiskLow: '\u4f4e',
    securityRiskUnknown: '\u672a\u77e5',
};

const t = (key: string) => zhTranslations[key] ?? key;

describe('SecurityEventsDialog', () => {
    afterEach(() => {
        cleanup();
        vi.clearAllMocks();
    });

    it('renders security event labels through localized translations', async () => {
        querySecurityEventsMock.mockResolvedValue([
            {
                time: '2026-04-28 15:47:26',
                tool_name: 'auto_clawhub_skill_install',
                target: '-',
                remote_ip: '-',
                risk_level: 'critical',
                reason: 'user rejected critical skill',
            },
        ]);

        render(<SecurityEventsDialog open onClose={() => {}} t={t} />);

        await waitFor(() => {
            expect(screen.getByRole('heading', { name: /\u5b89\u5168\u4e8b\u4ef6/ })).toBeTruthy();
            expect(screen.getByText('\u6700\u8fd1 7 \u5929\u5171 1 \u6761\u88ab\u62d2\u7edd\u7684\u64cd\u4f5c')).toBeTruthy();
            expect(screen.getByText('\u65f6\u95f4')).toBeTruthy();
            expect(screen.getByText('\u5de5\u5177/\u64cd\u4f5c')).toBeTruthy();
            expect(screen.getByText('\u98ce\u9669')).toBeTruthy();
            expect(screen.getByText('\u4e25\u91cd')).toBeTruthy();
        });
    });
});

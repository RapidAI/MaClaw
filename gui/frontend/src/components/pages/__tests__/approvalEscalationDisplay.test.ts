import { describe, expect, it } from 'vitest';
import {
    approvalEscalationDataAttr,
    approvalEscalationExhaustedText,
    approvalEscalationRetryText,
} from '../approvalEscalationDisplay';

describe('approvalEscalationDisplay', () => {
    it('formats retry text with attempt counts in Chinese', () => {
        const text = approvalEscalationRetryText(
            {
                escalationPending: true,
                escalationApprovers: ['ve-a', 've-b'],
                escalationAttempts: { 've-a': 3, 've-b': 1 },
            },
            'zh',
        );
        expect(text).toContain('\u5347\u7ea7\u91cd\u6295');
        expect(text).toContain('ve-a\u00d73');
        expect(text).toContain('ve-b\u00d71');
    });

    it('formats retry text in English without pending flag when approvers present', () => {
        const text = approvalEscalationRetryText(
            { escalationApprovers: ['m1'] },
            'en',
        );
        expect(text).toBe('Escalation retry: m1');
    });

    it('returns empty retry text when nothing pending', () => {
        expect(approvalEscalationRetryText({ escalationPending: false }, 'zh')).toBe('');
        expect(approvalEscalationRetryText(undefined, 'en')).toBe('');
    });

    it('formats exhausted peers in Chinese and English', () => {
        const zh = approvalEscalationExhaustedText(
            { escalationExhaustedApprovers: ['x', 'y', 'z', 'w', 'v'] },
            'zh-CN',
        );
        expect(zh).toContain('\u79bb\u7ebf\u8017\u5c3d');
        expect(zh).toContain('+1');
        const en = approvalEscalationExhaustedText(
            { escalationExhaustedApprovers: ['only'] },
            'en',
        );
        expect(en).toBe('Escalation exhausted: only');
    });

    it('maps data-escalation attribute priority pending over exhausted', () => {
        expect(
            approvalEscalationDataAttr({
                escalationPending: true,
                escalationExhaustedApprovers: ['dead'],
            }),
        ).toBe('pending');
        expect(
            approvalEscalationDataAttr({
                escalationExhaustedApprovers: ['dead'],
            }),
        ).toBe('exhausted');
        expect(approvalEscalationDataAttr({})).toBe('');
    });
});

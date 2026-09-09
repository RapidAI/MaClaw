import { describe, expect, it } from 'vitest';
import { canSubmitEmailVerification, emailVerificationCooldownSeconds, normalizeEmailVerificationTarget, sanitizeEmailVerificationCode } from '../emailVerification';

describe('email verification helpers', () => {
    it('normalizes the target consistently', () => {
        expect(normalizeEmailVerificationTarget(' User@Example.COM ')).toBe('user@example.com');
    });

    it('only allows a complete, idle verification request', () => {
        expect(canSubmitEmailVerification({ target: 'user@example.com', code: '123456', codeLength: 6, sending: false, busy: false })).toBe(true);
        expect(canSubmitEmailVerification({ target: 'user@example.com', code: '12345', codeLength: 6, sending: false, busy: false })).toBe(false);
        expect(canSubmitEmailVerification({ target: 'user@example.com', code: '123456', codeLength: 6, sending: true, busy: false })).toBe(false);
        expect(canSubmitEmailVerification({ target: 'user@example.com', code: 'abcdef', codeLength: 6, sending: false, busy: false })).toBe(false);
		expect(canSubmitEmailVerification({ target: 'user@example.com', code: '123', codeLength: 3, sending: false, busy: false })).toBe(false);
		expect(canSubmitEmailVerification({ target: 'user@example.com', code: '123456789', codeLength: 9, sending: false, busy: false })).toBe(false);
    });

    it('keeps digits and enforces the configured length', () => {
        expect(sanitizeEmailVerificationCode('12a 34-567', 6)).toBe('123456');
    });

	it('normalizes server cooldown metadata', () => {
        expect(emailVerificationCooldownSeconds(75.2)).toBe(76);
        expect(emailVerificationCooldownSeconds('invalid')).toBe(60);
        expect(emailVerificationCooldownSeconds(99999)).toBe(3600);
    });
});

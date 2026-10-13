import { describe, expect, it } from 'vitest';
import { connectSoloActive, grantSolo, settingsNavIsSolo } from './settingsSolo';

describe('settings solo grant', () => {
    it('keeps knowledge, IM, and email able to hide the rail without sticking to another tab', () => {
        expect(grantSolo('knowledge', true)).toBe('knowledge');
        expect(grantSolo('im', true)).toBe('im');
        expect(grantSolo('email', true)).toBe('email');
        expect(grantSolo('knowledge', false)).toBe('');
        expect(grantSolo('llm', true)).toBe('');
        expect(settingsNavIsSolo('email', 'email')).toBe(true);
        expect(settingsNavIsSolo('email', 'im')).toBe(false);
        expect(settingsNavIsSolo('', 'knowledge')).toBe(false);
    });

    it('highlights 接入 only for solo IM or mailbox', () => {
        expect(connectSoloActive('settings', 'im')).toBe(true);
        expect(connectSoloActive('settings', 'email')).toBe(true);
        expect(connectSoloActive('settings', 'knowledge')).toBe(false);
        expect(connectSoloActive('settings', '')).toBe(false);
        expect(connectSoloActive('ai', 'email')).toBe(false);
    });
});

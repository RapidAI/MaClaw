import { describe, expect, it } from 'vitest';
import { contactedProfileForExecution, contactedProfileModel, contactedProfileProviderName, foregroundRoleSeparatesTurn, quickModelWriteProfile } from '../contactedModelRoute';

describe('contactedProfileForExecution', () => {
    const summaries = {
        assistant: { provider_name: 'Custom1', model: 'deepseek-v4.1-flash' },
        coding: { provider_name: '智谱编程', model: 'glm-5.3-flash' },
    };

    it('uses the assistant route when a local task has no execution profile', () => {
        const route = contactedProfileForExecution('none', summaries);
        expect(contactedProfileProviderName(route)).toBe('Custom1');
        expect(contactedProfileModel(route)).toBe('deepseek-v4.1-flash');
    });

    it('writes profile-less tasks onto the assistant assignment', () => {
        expect(quickModelWriteProfile('none')).toBe('assistant');
        expect(quickModelWriteProfile(undefined)).toBe('assistant');
        expect(quickModelWriteProfile('assistant')).toBe('assistant');
        expect(quickModelWriteProfile('coding')).toBe('coding');
    });

    it('uses the coding route only for a coding task', () => {
        const route = contactedProfileForExecution('coding', {
            ...summaries,
            foreground: { provider_name: 'MaClaw官方', model: 'auto' },
        });
        expect(contactedProfileProviderName(route)).toBe('智谱编程');
        expect(contactedProfileModel(route)).toBe('glm-5.3-flash');
    });

    it('names the foreground Hub role instead of the assistant profile', () => {
        const route = contactedProfileForExecution('none', {
            ...summaries,
            foreground: { provider_id: 'hub', provider_name: 'MaClaw官方', model: 'auto' },
        });
        expect(contactedProfileProviderName(route)).toBe('MaClaw官方');
        expect(contactedProfileModel(route)).toBe('auto');
    });

    it('separates the foreground role only when its provider or model differs', () => {
        const shared = {
            assistant: { provider_name: 'Custom1', model: 'old-model' },
            foreground: { provider_name: 'Custom1', model: 'old-model' },
        };
        expect(foregroundRoleSeparatesTurn('none', shared)).toBe(false);
        expect(foregroundRoleSeparatesTurn('assistant', shared)).toBe(false);
        expect(foregroundRoleSeparatesTurn('none', {
            ...shared,
            foreground: { provider_name: 'MaClaw官方', model: 'auto' },
        })).toBe(true);
        expect(foregroundRoleSeparatesTurn('coding', {
            ...shared,
            foreground: { provider_name: 'MaClaw官方', model: 'auto' },
        })).toBe(false);
    });

    it('keeps the assistant profile when the foreground role is unresolved', () => {
        const route = contactedProfileForExecution('assistant', {
            ...summaries,
            foreground: { provider_name: '', model: '' },
        });
        expect(contactedProfileProviderName(route)).toBe('Custom1');
        expect(contactedProfileModel(route)).toBe('deepseek-v4.1-flash');
    });
});

import { describe, expect, it } from 'vitest';
import { contactedProfileForExecution, contactedProfileModel, contactedProfileProviderName, quickModelWriteProfile } from '../contactedModelRoute';

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
        const route = contactedProfileForExecution('coding', summaries);
        expect(contactedProfileProviderName(route)).toBe('智谱编程');
        expect(contactedProfileModel(route)).toBe('glm-5.3-flash');
    });
});

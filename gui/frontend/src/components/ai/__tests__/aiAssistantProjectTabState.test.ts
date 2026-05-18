import { beforeEach, describe, expect, it } from 'vitest';
import { loadProjectTabMsgIds, mergeChatMessages, PROJECT_TAB_MSG_IDS_KEY } from '../aiAssistantProjectTabState';

describe('aiAssistantProjectTabState', () => {
    beforeEach(() => {
        localStorage.clear();
    });

    it('loads persisted project-tab message ids from localStorage', () => {
        localStorage.setItem(PROJECT_TAB_MSG_IDS_KEY, JSON.stringify(['m1', 'm2']));

        const ids = loadProjectTabMsgIds();

        expect(ids.has('m1')).toBe(true);
        expect(ids.has('m2')).toBe(true);
        expect(ids.size).toBe(2);
    });

    it('falls back to an empty set for malformed persisted ids', () => {
        localStorage.setItem(PROJECT_TAB_MSG_IDS_KEY, '{not-json');

        expect(loadProjectTabMsgIds().size).toBe(0);
    });

    it('merges chat message groups without duplicate ids', () => {
        const merged = mergeChatMessages(
            [{ id: 'a', role: 'user', content: 'one' }, { id: 'b', role: 'assistant', content: 'two' }],
            [{ id: 'b', role: 'assistant', content: 'duplicate' }, { id: 'c', role: 'user', content: 'three' }],
            undefined,
        );

        expect(merged.map((message) => message.id)).toEqual(['a', 'b', 'c']);
        expect(merged[1].content).toBe('two');
    });
});

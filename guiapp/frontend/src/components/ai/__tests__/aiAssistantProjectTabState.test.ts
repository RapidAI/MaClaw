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

    it('merges chat message groups without duplicate ids and keeps latest message data', () => {
        const merged = mergeChatMessages(
            [{ id: 'a', role: 'user', content: 'one' }, { id: 'b', role: 'assistant', content: 'two' }],
            [{ id: 'b', role: 'assistant', content: 'duplicate' }, { id: 'c', role: 'user', content: 'three' }],
            undefined,
        );

        expect(merged.map((message) => message.id)).toEqual(['a', 'b', 'c']);
        expect(merged[1].content).toBe('duplicate');
    });

    it('replaces a saved project placeholder with the live final assistant response', () => {
        const merged = mergeChatMessages(
            [{ id: 'assistant-1', role: 'assistant', content: '', requestId: 'req-1', sessionKey: 'desktop-user:D:/tasks/weather' }],
            [{ id: 'assistant-1', role: 'assistant', content: 'weather done', requestId: 'req-1', sessionKey: 'desktop-user:D:/tasks/weather', fields: [{ label: 'status', value: 'ok' }] }],
        );

        expect(merged).toHaveLength(1);
        expect(merged[0].content).toBe('weather done');
        expect(merged[0].fields?.[0]?.value).toBe('ok');
    });
});

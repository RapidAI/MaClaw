import { beforeEach, describe, expect, it } from 'vitest';
import { collapseDuplicatePendingUnfinishedSlots, latestPendingUnfinishedStatus, loadProjectTabMsgIds, mergeChatMessages, messageHasPendingUnfinishedSlot, PROJECT_TAB_MSG_IDS_KEY } from '../aiAssistantProjectTabState';

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

    it('places a live row that arrives late beside the round it belongs to', () => {
        const merged = mergeChatMessages(
            [
                { id: 'a', role: 'assistant', content: '', requestId: 'round-a' },
                { id: 'next-user', role: 'user', content: 'next' },
                { id: 'b', role: 'assistant', content: '', requestId: 'round-b' },
            ],
            [
                { id: 'a', role: 'assistant', content: 'done', requestId: 'round-a' },
                { id: 'steer', role: 'user', content: 'guide', requestId: 'round-a' },
                { id: 'next-user', role: 'user', content: 'next' },
                { id: 'b', role: 'assistant', content: 'working', requestId: 'round-b' },
            ],
        );

        expect(merged.map((message) => message.id)).toEqual(['a', 'steer', 'next-user', 'b']);
        expect(merged[0].content).toBe('done');
        expect(merged[3].content).toBe('working');
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

    it('keeps one pending recovery card when startup injection and the reply share a slot', () => {
        const slot = {
            slotID: 'inflight-recovery-1',
            title: '查看驱网服务器状态',
            status: 'interrupted',
            actions: [{ label: '继续上次任务', command: '__resume_unfinished__ inflight-recovery-1', style: 'default' as const }],
        };
        const collapsed = collapseDuplicatePendingUnfinishedSlots([
            { id: 'user', role: 'user', content: '崇州天气，生成格式化pdf', timestamp: 1 },
            { id: 'startup-recovery-inflight-recovery-1', role: 'assistant', content: '检测到未完成任务', timestamp: 2, unfinishedSlot: slot },
            { id: 'reply', role: 'assistant', content: '检测到未完成任务', timestamp: 3, unfinishedSlot: { ...slot } },
        ]);

        expect(collapsed.map(message => message.id)).toEqual(['user', 'reply']);
        expect(latestPendingUnfinishedStatus(collapsed)).toBe('interrupted');
    });

    it('keeps the reply that still has reasoning when a later startup card repeats the slot', () => {
        const slot = {
            slotID: 'inflight-recovery-1',
            status: 'interrupted',
            actions: [{ label: '继续上次任务', command: '__resume_unfinished__ inflight-recovery-1', style: 'default' as const }],
        };
        const messages = [
            { id: 'reply', role: 'assistant' as const, content: '检测到未完成任务：查看驱网服务器状态。', timestamp: 1, reasoning: '查过端口', unfinishedSlot: slot },
            { id: 'startup-recovery-inflight-recovery-1', role: 'assistant' as const, content: '检测到未完成任务：查看驱网服务器状态。', timestamp: 2, unfinishedSlot: { ...slot } },
        ];
        const collapsed = collapseDuplicatePendingUnfinishedSlots(messages);

        expect(collapsed.map(message => message.id)).toEqual(['reply']);
        expect(collapseDuplicatePendingUnfinishedSlots(collapsed)).toBe(collapsed);
    });

    it('keeps the reply when a saved history is merged with the startup card again', () => {
        const slot = {
            slotID: 'inflight-recovery-1',
            status: 'interrupted',
            actions: [{ label: '继续上次任务', command: '__resume_unfinished__ inflight-recovery-1', style: 'default' as const }],
        };
        const user = { id: 'user', role: 'user' as const, content: '崇州天气，生成格式化pdf', timestamp: 2 };
        const startup = { id: 'startup-recovery-inflight-recovery-1', role: 'assistant' as const, content: '检测到未完成任务', timestamp: 1, unfinishedSlot: slot };
        const reply = { id: 'reply', role: 'assistant' as const, content: '检测到未完成任务', timestamp: 3, unfinishedSlot: { ...slot } };
        const once = collapseDuplicatePendingUnfinishedSlots(mergeChatMessages([user, reply], [startup, user, reply]));
        const twice = collapseDuplicatePendingUnfinishedSlots(mergeChatMessages(once, [startup, user, reply]));

        expect(once.map(message => message.id)).toEqual(['user', 'reply']);
        expect(twice.map(message => message.id)).toEqual(['user', 'reply']);
    });

    it('keeps a startup card over an empty placeholder for the same slot', () => {
        const slot = {
            slotID: 'inflight-recovery-1',
            status: 'interrupted',
            actions: [{ label: '继续上次任务', command: '__resume_unfinished__ inflight-recovery-1', style: 'default' as const }],
        };
        const collapsed = collapseDuplicatePendingUnfinishedSlots([
            { id: 'placeholder', role: 'assistant', content: '', timestamp: 4, unfinishedSlot: slot },
            { id: 'startup-recovery-inflight-recovery-1', role: 'assistant', content: '检测到未完成任务', timestamp: 1, unfinishedSlot: { ...slot } },
        ]);

        expect(collapsed.map(message => message.id)).toEqual(['startup-recovery-inflight-recovery-1']);
    });

    it('treats a pending card without a slot id as unfinished', () => {
        expect(latestPendingUnfinishedStatus([
            {
                id: 'card',
                role: 'assistant',
                content: '检测到未完成任务',
                timestamp: 1,
                unfinishedSlot: { status: '', actions: [{ label: '继续上次任务', command: '__resume_unfinished__ ', style: 'default' }] },
            },
        ])).toBe('unfinished');
    });

    it('keeps a resolved recovery card beside a newer pending slot', () => {
        const collapsed = collapseDuplicatePendingUnfinishedSlots([
            {
                id: 'old',
                role: 'assistant',
                content: '已恢复',
                timestamp: 1,
                unfinishedSlot: { slotID: 'slot-a', status: 'resumed', actions: [] },
            },
            {
                id: 'new',
                role: 'assistant',
                content: '检测到未完成任务',
                timestamp: 2,
                unfinishedSlot: {
                    slotID: 'slot-b',
                    status: 'interrupted',
                    actions: [{ label: '继续上次任务', command: '__resume_unfinished__ slot-b', style: 'default' }],
                },
            },
        ]);

        expect(collapsed.map(message => message.id)).toEqual(['old', 'new']);
    });

    it('does not keep the header on an interrupted task after the card is dismissed', () => {
        const message = {
            id: 'card',
            role: 'assistant' as const,
            content: '检测到未完成任务',
            timestamp: 1,
            unfinishedSlot: {
                slotID: 'slot-dismissed',
                status: 'dismissed',
                actions: [{ label: '继续上次任务', command: '__resume_unfinished__ slot-dismissed', style: 'default' as const }],
            },
        };

        expect(messageHasPendingUnfinishedSlot(message)).toBe(false);
        expect(latestPendingUnfinishedStatus([message])).toBe('');
    });

    it('keeps one card after both copies of a slot are dismissed', () => {
        const slot = {
            slotID: 'slot-dismissed',
            status: 'dismissed',
            actions: [] as [],
        };
        const collapsed = collapseDuplicatePendingUnfinishedSlots([
            { id: 'startup-recovery-slot-dismissed', role: 'assistant', content: '', timestamp: 1, unfinishedSlot: slot },
            { id: 'reply', role: 'assistant', content: '', timestamp: 2, unfinishedSlot: { ...slot } },
        ]);

        expect(collapsed.map(message => message.id)).toEqual(['reply']);
    });
});

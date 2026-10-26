import { beforeEach, describe, expect, it } from 'vitest';
import { collapseDuplicatePendingUnfinishedSlots, latestAssistantTurnIncomplete, latestPendingUnfinishedStatus, loadProjectTabMsgIds, mergeChatMessages, mergeProjectRoundHistory, messageHasPendingUnfinishedSlot, PROJECT_TAB_MSG_IDS_KEY, projectHistorySubstantiveDelta } from '../aiAssistantProjectTabState';

describe('aiAssistantProjectTabState', () => {
    beforeEach(() => {
        localStorage.clear();
    });

    it('reads the latest assistant outcome and skips a later system card', () => {
        expect(latestAssistantTurnIncomplete([
            { id: 'a', role: 'assistant' as const, content: '先从 GitHub 上取一份目录清单。', resultStatus: 'incomplete' as const, timestamp: 1 },
            { id: 's', role: 'system' as const, content: '已保存', timestamp: 2 },
        ])).toBe(true);
        expect(latestAssistantTurnIncomplete([
            { id: 'a', role: 'assistant' as const, content: '先从 GitHub 上取一份目录清单。', resultStatus: 'incomplete' as const, timestamp: 1 },
            { id: 'b', role: 'assistant' as const, content: '目录里没有这个文件。', resultStatus: 'completed' as const, timestamp: 2 },
        ])).toBe(false);
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

    it('commits a stage report during a stream and skips a token append', () => {
        const essay = { id: 's0', role: 'assistant' as const, content: '下一轮只执行 S0。', timestamp: 1 };
        const placeholder = { id: 's2', role: 'assistant' as const, content: '', timestamp: 2 };
        const started = { ...placeholder, content: '第' };
        expect(projectHistorySubstantiveDelta([essay, placeholder], [essay, started])).toBe(true);
        expect(projectHistorySubstantiveDelta([essay, started], [
            essay,
            { ...started, content: `${started.content} 3 步` },
        ])).toBe(false);
        expect(projectHistorySubstantiveDelta([essay, { ...placeholder, content: '工作过程'.repeat(40) }], [
            essay,
            { ...placeholder, content: '第 3 步（S2）已完成并归档。', fields: [{ label: 'Turn', value: 'reasoning' }] },
        ])).toBe(true);
        expect(projectHistorySubstantiveDelta([essay], [
            essay,
            { ...placeholder, pendingCodingThoughts: [{ id: 't1', sequence: 1, kind: 'thinking' as const, content: '先核对指标。', timestamp: 2 }] },
        ])).toBe(true);
        expect(projectHistorySubstantiveDelta([essay, placeholder], [essay, placeholder])).toBe(false);
        const startedTool = { id: 'p1', sequence: 2, kind: 'progress' as const, content: 'Coding Agent Event: {"event":"tool_started","detail":"bash"}', timestamp: 2 };
        const finishedTool = { ...startedTool, content: 'Coding Agent Event: {"event":"tool_finished","detail":"bash"}', timestamp: 3 };
        const withTool = { ...placeholder, codingTimeline: [startedTool] };
        expect(projectHistorySubstantiveDelta([essay, withTool], [essay, { ...withTool, codingTimeline: [finishedTool] }])).toBe(true);
        const thought = { id: 't1', sequence: 1, kind: 'thinking' as const, content: '先', timestamp: 1 };
        expect(projectHistorySubstantiveDelta(
            [essay, { ...placeholder, codingTimeline: [thought, startedTool] }],
            [essay, { ...placeholder, codingTimeline: [{ ...thought, content: '先核对指标。' }, startedTool] }],
        )).toBe(false);
    });

    it('keeps a saved coding trail when the fresh row is thinner', () => {
        const trail = [
            { id: 't1', sequence: 1, kind: 'thinking' as const, content: 'Inspect quality.h', timestamp: 2 },
            { id: 'p1', sequence: 2, kind: 'progress' as const, content: 'Coding Agent Event: {"detail":"edit_file"}', timestamp: 3 },
        ];
        const merged = mergeProjectRoundHistory(
            [{ id: 'ack', role: 'assistant', content: '目标已开始', timestamp: 1 }],
            [{ id: 'ack', role: 'assistant', content: '目标已开始', timestamp: 1 }, { id: 'goal', role: 'assistant', content: '', requestId: 'goal-cont-1', timestamp: 2, codingTimeline: trail }],
            [{ id: 'goal', role: 'assistant', content: '步骤已核对', requestId: 'goal-cont-1', timestamp: 2 }],
        );
        expect(merged.map(message => message.id)).toEqual(['ack', 'goal']);
        expect(merged[1].content).toBe('步骤已核对');
        expect(merged[1].codingTimeline).toEqual(trail);
    });

    it('keeps a tool row the later trail skipped when a further event arrived', () => {
        const saved = [
            { id: 't1', sequence: 1, kind: 'thinking' as const, content: 'Inspect quality.h', timestamp: 2 },
            { id: 'p1', sequence: 2, kind: 'progress' as const, content: 'edit_file', timestamp: 3 },
        ];
        const fresh = [
            { id: 't1', sequence: 1, kind: 'thinking' as const, content: 'Inspect quality.h', timestamp: 2 },
            { id: 'p2', sequence: 8, kind: 'progress' as const, content: 'todo_write', timestamp: 4 },
        ];
        const merged = mergeProjectRoundHistory(
            [{ id: 'goal', role: 'assistant', content: '', timestamp: 1, codingTimeline: saved }],
            undefined,
            [{ id: 'goal', role: 'assistant', content: '步骤已核对', timestamp: 2, codingTimeline: fresh }],
        );
        expect(merged[0].content).toBe('步骤已核对');
        expect(merged[0].codingTimeline).toEqual([fresh[0], saved[1], fresh[1]]);
    });

    it('keeps a same-watermark edit instead of a longer earlier trail', () => {
        const saved = [
            { id: 't1', sequence: 1, kind: 'thinking' as const, content: 'old', timestamp: 2 },
            { id: 'p1', sequence: 2, kind: 'progress' as const, content: 'edit_file', timestamp: 3 },
            { id: 't2', sequence: 3, kind: 'thinking' as const, content: 'tail', timestamp: 4 },
        ];
        const fresh = [
            { id: 't1', sequence: 1, lastSequence: 2, kind: 'thinking' as const, content: 'revised', timestamp: 2 },
            { id: 'p2', sequence: 3, kind: 'progress' as const, content: 'todo_write', timestamp: 5 },
        ];
        const merged = mergeProjectRoundHistory(
            [{ id: 'goal', role: 'assistant', content: '', timestamp: 1, codingTimeline: saved }],
            undefined,
            [{ id: 'goal', role: 'assistant', content: '步骤已核对', timestamp: 2, codingTimeline: fresh }],
        );
        expect(merged[0].codingTimeline).toBe(fresh);
    });

    it('keeps an in-place tool finish and a thought only the earlier copy carried further', () => {
        const saved = [
            { id: 't1', sequence: 1, kind: 'thinking' as const, content: 'old', timestamp: 2 },
            { id: 'p1', sequence: 2, kind: 'progress' as const, content: 'edit_file', timestamp: 3 },
            { id: 't2', sequence: 10, kind: 'thinking' as const, content: 'later', timestamp: 6 },
        ];
        const fresh = [
            { id: 't1', sequence: 1, kind: 'thinking' as const, content: 'revised', timestamp: 4 },
            { id: 'p1', sequence: 2, kind: 'progress' as const, content: 'finished', timestamp: 5, eventSequences: [2, 5] },
        ];
        const merged = mergeProjectRoundHistory(
            [{ id: 'goal', role: 'assistant', content: '', timestamp: 1, codingTimeline: saved }],
            undefined,
            [{ id: 'goal', role: 'assistant', content: '步骤已核对', timestamp: 2, codingTimeline: fresh }],
        );
        expect(merged[0].content).toBe('步骤已核对');
        expect(merged[0].codingTimeline).toEqual([fresh[0], fresh[1], saved[2]]);
    });

    it('replaces an earlier tool row when a later copy owns its event sequence', () => {
        const saved = [
            { id: 'p1', sequence: 5, kind: 'progress' as const, content: 'edit_file', timestamp: 3 },
            { id: 't2', sequence: 10, kind: 'thinking' as const, content: 'later', timestamp: 6 },
        ];
        const finished = { id: 'p1', sequence: 2, kind: 'progress' as const, content: 'finished', timestamp: 5, eventSequences: [2, 5] };
        const merged = mergeProjectRoundHistory(
            [{ id: 'goal', role: 'assistant', content: '', timestamp: 1, codingTimeline: saved }],
            undefined,
            [{ id: 'goal', role: 'assistant', content: '步骤已核对', timestamp: 2, codingTimeline: [finished] }],
        );
        expect(merged[0].codingTimeline).toEqual([finished, saved[1]]);
    });

    it('does not paint the same unsequenced row twice', () => {
        const saved = [{ id: 't1', sequence: 0, kind: 'thinking' as const, content: 'old', timestamp: 1 }];
        const fresh = [{ id: 't1', sequence: 0, kind: 'thinking' as const, content: 'revised', timestamp: 2 }];
        const merged = mergeProjectRoundHistory(
            [{ id: 'goal', role: 'assistant', content: '', timestamp: 1, codingTimeline: saved }],
            undefined,
            [{ id: 'goal', role: 'assistant', content: '步骤已核对', timestamp: 2, codingTimeline: fresh }],
        );
        expect(merged[0].codingTimeline).toBe(fresh);
    });

    it('keeps the later row when the same id gains a transport sequence', () => {
        const saved = [
            { id: 't1', sequence: 0, kind: 'thinking' as const, content: 'old', timestamp: 1 },
            { id: 'p1', sequence: 5, kind: 'progress' as const, content: 'edit_file', timestamp: 3 },
        ];
        const fresh = [
            { id: 't1', sequence: 4, kind: 'thinking' as const, content: 'revised', timestamp: 4 },
            { id: 'p1', sequence: 2, kind: 'progress' as const, content: 'finished', timestamp: 5 },
        ];
        const merged = mergeProjectRoundHistory(
            [{ id: 'goal', role: 'assistant', content: '', timestamp: 1, codingTimeline: saved }],
            undefined,
            [{ id: 'goal', role: 'assistant', content: '步骤已核对', timestamp: 2, codingTimeline: fresh }],
        );
        expect(merged[0].codingTimeline).toBe(fresh);
    });

    it('trusts a live row that left the coding trail while the turn is sending', () => {
        const trail = [
            { id: 't1', sequence: 1, kind: 'thinking' as const, content: 'Let me explore the repository.', timestamp: 2 },
            { id: 'p1', sequence: 2, kind: 'progress' as const, content: 'edit_file', timestamp: 3 },
        ];
        const merged = mergeProjectRoundHistory(
            [{ id: 'goal', role: 'assistant', content: '', timestamp: 2, codingTimeline: trail }],
            undefined,
            [{ id: 'goal', role: 'assistant', content: '', reasoning: '先看现有实现。', timestamp: 4 }],
            undefined,
            true,
        );
        expect(merged[0].reasoning).toBe('先看现有实现。');
        expect(merged[0].codingTimeline).toBeUndefined();
    });

    it('keeps an unsequenced row that is a different id', () => {
        const saved = [
            { id: 'n1', sequence: 0, kind: 'thinking' as const, content: 'note', timestamp: 1 },
            { id: 'p1', sequence: 2, kind: 'progress' as const, content: 'edit_file', timestamp: 3 },
        ];
        const fresh = [
            { id: 't1', sequence: 1, kind: 'thinking' as const, content: 'revised', timestamp: 4 },
        ];
        const merged = mergeProjectRoundHistory(
            [{ id: 'goal', role: 'assistant', content: '', timestamp: 1, codingTimeline: saved }],
            undefined,
            [{ id: 'goal', role: 'assistant', content: '步骤已核对', timestamp: 2, codingTimeline: fresh }],
        );
        expect(merged[0].codingTimeline?.map(item => item.id)).toEqual(['n1', 't1', 'p1']);
    });

    it('does not put a saved coding trail back on an error row', () => {
        const trail = [
            { id: 't1', sequence: 1, kind: 'thinking' as const, content: 'Inspect quality.h', timestamp: 2 },
        ];
        const merged = mergeProjectRoundHistory(
            [{ id: 'goal', role: 'assistant', content: '', timestamp: 1, codingTimeline: trail }],
            undefined,
            [{ id: 'goal', role: 'error', content: 'Tool execution failed.', timestamp: 3 }],
        );
        expect(merged[0].role).toBe('error');
        expect(merged[0].content).toBe('Tool execution failed.');
        expect(merged[0].codingTimeline).toBeUndefined();
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

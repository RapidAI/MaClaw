import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
    applyCloudAgentStream,
    applyCloudAgentWatch,
    cloudAgentTrail,
    cloudAgentTrailRequestId,
    resetCloudAgentRounds,
} from '../cloudAgentSession';

const GRANT = 'grant-1';
const INSTANCE = 'inst-1';
const SESSION = `cloud-agent:${INSTANCE}:task-1`;

function frame(event: string, extra: Record<string, unknown> = {}, requestId = 'r1') {
    return {
        grant_id: GRANT,
        session_key: SESSION,
        project_path: '',
        request_id: requestId,
        event,
        proto: 2,
        cursor: 0,
        seq: 0,
        ...extra,
    };
}

let cursor = 0;
function nextFrame(event: string, extra: Record<string, unknown> = {}, requestId = 'r1') {
    cursor += 1;
    return frame(event, { cursor, cursor_from: cursor, ...extra }, requestId);
}

const startedJson = (command: string) => JSON.stringify({
    version: 1, agent: 'coding', event: 'tool_started', phase: 'running', title: '', detail: 'bash', command,
});
const finishedJson = (command: string, outcome = 'success') => JSON.stringify({
    version: 1, agent: 'coding', event: 'tool_finished', phase: 'running', title: '', detail: 'bash', command, outcome,
});

describe('cloudAgentSession trail journal', () => {
    beforeEach(() => {
        resetCloudAgentRounds();
        cursor = 0;
        vi.restoreAllMocks();
    });

    it('builds one interleaved trail with tool_started merged into its tool_finished', () => {
        applyCloudAgentStream(nextFrame('round_started', { display_text: '〔云端〕做个贪吃蛇' }));
        applyCloudAgentStream(nextFrame('thinking', { state: 'delta', text: '先看目录。' }));
        applyCloudAgentStream(nextFrame('progress', { text: `Coding Agent Event: ${startedJson('ls -la')}` }));
        applyCloudAgentStream(nextFrame('progress', { text: `Coding Agent Event: ${finishedJson('ls -la')}` }));
        applyCloudAgentStream(nextFrame('progress', { text: `Coding Agent Event: ${startedJson('g++ main.cpp')}` }));
        applyCloudAgentStream(nextFrame('progress', { text: `Coding Agent Event: ${finishedJson('g++ main.cpp')}` }));
        applyCloudAgentStream(nextFrame('token', { text: '完成。' }));

        const trail = cloudAgentTrail(`${GRANT}\n${SESSION}`);
        expect(trail).not.toBeNull();
        expect(trail!.map(item => item.kind)).toEqual(['thinking', 'progress', 'progress']);
        expect(trail![0].content).toBe('先看目录。');
        // Each call is one row; the finished content replaced the running row.
        expect(trail![1].content).toContain('"event":"tool_finished"');
        expect(trail![1].content).toContain('ls -la');
        expect(trail![2].content).toContain('g++ main.cpp');
        expect(cloudAgentTrailRequestId(`${GRANT}\n${SESSION}`)).toBe('r1');
    });

    it('ignores frames of other sessions and non-trail events', () => {
        applyCloudAgentStream(nextFrame('round_started'));
        const key = `${GRANT}\n${SESSION}`;
        const before = cloudAgentTrail(key);
        applyCloudAgentStream(nextFrame('heartbeat'));
        applyCloudAgentStream(nextFrame('workbench_steps', { workbench: { step_statuses: [] } }));
        expect(cloudAgentTrail(key)).toBe(before);
    });

    it('a new request resets the trail', () => {
        applyCloudAgentStream(nextFrame('round_started', {}, 'r1'));
        applyCloudAgentStream(nextFrame('progress', { text: `Coding Agent Event: ${startedJson('ls -la')}` }, 'r1'));
        expect(cloudAgentTrail(`${GRANT}\n${SESSION}`)).toHaveLength(1);
        applyCloudAgentStream(nextFrame('round_started', { display_text: '〔云端〕再来一轮' }, 'r2'));
        expect(cloudAgentTrail(`${GRANT}\n${SESSION}`)).toBeNull();
        expect(cloudAgentTrailRequestId(`${GRANT}\n${SESSION}`)).toBe('r2');
    });

    it('keeps the frame-built trail when a snapshot of the same settled round arrives', () => {
        applyCloudAgentStream(nextFrame('round_started'));
        applyCloudAgentStream(nextFrame('progress', { text: `Coding Agent Event: ${startedJson('ls -la')}` }));
        applyCloudAgentStream(nextFrame('progress', { text: `Coding Agent Event: ${finishedJson('ls -la')}` }));
        const key = `${GRANT}\n${SESSION}`;
        const built = cloudAgentTrail(key);
        expect(built).toHaveLength(1);

        // The snapshot only carries the last 40 progress rows and splits
        // thinking from progress — it must not shrink the finished trail.
        applyCloudAgentWatch(GRANT, SESSION, {
            session_key: SESSION,
            request_id: 'r1',
            cursor: 100,
            responded: true,
            display_text: '〔云端〕做个贪吃蛇',
            parts: [{ kind: 'progress', seq: 1, text: `Coding Agent Event: ${finishedJson('ls -la')}` }],
        });
        expect(cloudAgentTrail(key)).toBe(built);
    });

    it('seeds the trail from a watch snapshot when this controller joined mid-turn', () => {
        applyCloudAgentWatch(GRANT, SESSION, {
            session_key: SESSION,
            request_id: 'r1',
            cursor: 42,
            responded: false,
            display_text: '〔云端〕做个贪吃蛇',
            parts: [
                { kind: 'thinking', seq: 1, text: '先想一下。', done: true },
                { kind: 'progress', seq: 2, text: `Coding Agent Event: ${startedJson('ls -la')}` },
            ],
        });
        const trail = cloudAgentTrail(`${GRANT}\n${SESSION}`);
        expect(trail!.map(item => item.kind)).toEqual(['thinking', 'progress']);
        expect(trail![0].content).toBe('先想一下。');
    });

    it('seals the open thought when the terminal response lands', () => {
        applyCloudAgentStream(nextFrame('round_started'));
        applyCloudAgentStream(nextFrame('thinking', { state: 'delta', text: '思考中' }));
        applyCloudAgentStream(nextFrame('response', { terminal: { text: '答案' } }));
        const trail = cloudAgentTrail(`${GRANT}\n${SESSION}`);
        expect(trail).not.toBeNull();
        expect(trail![0].kind).toBe('thinking');
    });
});

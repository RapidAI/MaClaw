import { describe, expect, it } from 'vitest';
import {
    appendCodingProgressEvent,
    CodingAgentTrailBuilder,
} from '../codingAgentTimeline';

const started = (command: string, detail = 'bash') =>
    `Coding Agent Event: ${JSON.stringify({ version: 1, agent: 'coding', event: 'tool_started', phase: 'running', title: '', detail, command })}`;
const finished = (command: string, outcome = 'success', extra: Record<string, unknown> = {}) =>
    `Coding Agent Event: ${JSON.stringify({ version: 1, agent: 'coding', event: 'tool_finished', phase: 'running', title: '', detail: 'bash', command, outcome, ...extra })}`;
const audit = () =>
    `Coding Agent Event: ${JSON.stringify({ version: 1, agent: 'coding', event: 'verification_summary', phase: 'running', title: '验证', outcome: 'passed' })}`;

describe('appendCodingProgressEvent (shared trail folding)', () => {
    it('folds tool_finished into its tool_started row — one row per call', () => {
        const afterStart = appendCodingProgressEvent([], started('ls -la'), 1, 1000);
        expect(afterStart).toHaveLength(1);
        const afterFinish = appendCodingProgressEvent(afterStart, finished('ls -la'), 2, 1100);
        expect(afterFinish).toHaveLength(1);
        expect(afterFinish[0].content).toBe(finished('ls -la'));
        // The finished row keeps the started row's arrival position.
        expect(afterFinish[0].sequence).toBe(afterStart[0].sequence);
        expect(afterFinish[0].eventSequences).toContain(2);
    });

    it('keeps distinct calls apart and pairs only matching lifecycles', () => {
        let items = appendCodingProgressEvent([], started('ls -la'), 1, 1000);
        items = appendCodingProgressEvent(items, finished('ls -la'), 2, 1100);
        items = appendCodingProgressEvent(items, started('cat main.go'), 3, 1200);
        items = appendCodingProgressEvent(items, finished('cat main.go'), 4, 1300);
        expect(items).toHaveLength(2);
        // A finished event whose command matches no open start inserts as its own row.
        items = appendCodingProgressEvent(items, finished('make build'), 5, 1400);
        expect(items).toHaveLength(3);
    });

    it('merges tool_progress into the open tool row', () => {
        let items = appendCodingProgressEvent([], started('pip install reportlab'), 1, 1000);
        items = appendCodingProgressEvent(items, 'Coding Agent Event: ' + JSON.stringify({
            version: 1, agent: 'coding', event: 'tool_progress', phase: 'running',
            detail: 'bash', command: 'pip install reportlab', summary: 'Downloading 40%',
        }), 2, 1100);
        expect(items).toHaveLength(1);
        expect(items[0].content).toContain('Downloading 40%');
    });

    it('drops audit rollup banners and ignores non-coding lines', () => {
        let items = appendCodingProgressEvent([], started('ls -la'), 1, 1000);
        const afterAudit = appendCodingProgressEvent(items, audit(), 2, 1100);
        expect(afterAudit).toBe(items);
        const afterStyled = appendCodingProgressEvent(afterAudit, '工具 · 执行命令 (bash)\nls -la', 3, 1200);
        expect(afterStyled).toBe(afterAudit);
    });

    it('returns the same reference when a duplicate delivery repeats', () => {
        const items = appendCodingProgressEvent([], started('ls -la'), 1, 1000);
        expect(appendCodingProgressEvent(items, started('ls -la'), 1, 1100)).toBe(items);
    });

    it('drops an event whose transport sequence is already taken (replay guard)', () => {
        const items = appendCodingProgressEvent([], started('ls -la'), 5, 1000);
        // Cloud cursors are unique per lane, so a taken sequence can only be
        // a replayed frame — fold nothing.
        expect(appendCodingProgressEvent(items, started('cat a.go'), 5, 1100)).toBe(items);
    });
});

describe('CodingAgentTrailBuilder (frame-arrival interleave)', () => {
    it('interleaves thoughts and tool rows in frame order and seals on the next tool', () => {
        const builder = new CodingAgentTrailBuilder();
        builder.thinkingDelta('先看目录结构');
        builder.thinkingDelta('，再编译。');
        builder.thinkingDone();
        builder.progress(started('ls -la'), 4);
        builder.progress(finished('ls -la'), 5);
        builder.thinkingDelta('写实现。');
        builder.progress(started('g++ main.cpp'), 7);
        builder.progress(finished('g++ main.cpp', 'success'), 8);

        const items = builder.snapshot();
        expect(items.map(item => item.kind)).toEqual(['thinking', 'progress', 'thinking', 'progress']);
        expect(items[0].content).toBe('先看目录结构，再编译。');
        // The finished event replaced the running row in place.
        expect(items[1].content).toBe(finished('ls -la'));
        expect(items[3].content).toBe(finished('g++ main.cpp', 'success'));
        expect(builder.hasOpenThought).toBe(false);
    });

    it('keeps the snapshot reference stable while nothing changes', () => {
        const builder = new CodingAgentTrailBuilder();
        builder.progress(started('ls -la'), 1);
        const first = builder.snapshot();
        builder.progress(started('ls -la'), 1);
        expect(builder.snapshot()).toBe(first);
        builder.progress(finished('ls -la'), 2);
        expect(builder.snapshot()).not.toBe(first);
    });

    it('thinkingDone adopts the full text and an unmatched done still records the thought', () => {
        const builder = new CodingAgentTrailBuilder();
        builder.thinkingStarted();
        builder.thinkingDelta('half ');
        builder.thinkingDone('half and complete');
        expect(builder.snapshot()[0].content).toBe('half and complete');

        const orphan = new CodingAgentTrailBuilder();
        orphan.thinkingDone('arrived without deltas');
        expect(orphan.snapshot().map(item => item.content)).toEqual(['arrived without deltas']);
    });

    it('reset clears the trail (snapshot rebuild path)', () => {
        const builder = new CodingAgentTrailBuilder();
        builder.progress(started('ls -la'), 1);
        builder.reset();
        expect(builder.snapshot()).toHaveLength(0);
    });
});

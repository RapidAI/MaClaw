import { describe, expect, it } from 'vitest';
import {
    cloudAgentPreviewChangesFromParts,
    cloudAgentPreviewChangesFromText,
    cloudAgentPreviewCodeFile,
    cloudAgentPreviewDedupeKey,
    cloudAgentPreviewDroppedPaths,
    cloudAgentPreviewPathKey,
    cloudAgentPreviewReadRetries,
    cloudAgentTurnFileChanges,
    cloudAgentWritesOpenPreview,
} from '../cloudAgentPreview';

function event(body: Record<string, unknown>): string {
    return `Coding Agent Event: ${JSON.stringify({ version: 1, agent: 'coding', phase: 'running', ...body })}`;
}

describe('cloudAgentPreviewPathKey', () => {
    it('normalizes slashes, a leading dot, and a trailing slash', () => {
        expect(cloudAgentPreviewPathKey(' .\\src\\snake.cpp/ ')).toBe('src/snake.cpp');
        expect(cloudAgentPreviewPathKey('./src/main.cpp')).toBe('src/main.cpp');
    });
});

describe('cloudAgentWritesOpenPreview', () => {
    it('opens only for a coding instance', () => {
        expect(cloudAgentWritesOpenPreview('coding')).toBe(true);
        expect(cloudAgentWritesOpenPreview(' session ')).toBe(false);
        expect(cloudAgentWritesOpenPreview(undefined)).toBe(false);
    });
});

describe('cloudAgentPreviewChangesFromText', () => {
    it('reads a successful write_file, including created', () => {
        expect(cloudAgentPreviewChangesFromText(event({
            event: 'tool_finished',
            detail: 'write_file',
            outcome: 'success',
            files: ['src/snake.cpp'],
            created: true,
            added: 12,
            removed: 0,
        }))).toEqual([{ path: 'src/snake.cpp', created: true, added: 12, removed: 0 }]);
    });

    it('skips a failed write, a read, and any other tool', () => {
        expect(cloudAgentPreviewChangesFromText(event({
            event: 'tool_finished',
            detail: 'write_file',
            outcome: 'failed',
            files: ['src/snake.cpp'],
        }))).toEqual([]);
        expect(cloudAgentPreviewChangesFromText(event({
            event: 'tool_finished',
            detail: 'read_file',
            outcome: 'success',
            files: ['src/snake.cpp'],
        }))).toEqual([]);
        expect(cloudAgentPreviewChangesFromText(event({
            event: 'tool_finished',
            detail: 'bash',
            outcome: 'success',
        }))).toEqual([]);
        expect(cloudAgentPreviewChangesFromText('plain progress')).toEqual([]);
    });

    it('keeps a deleted diff row so the fold can drop the path', () => {
        expect(cloudAgentPreviewChangesFromText(event({
            event: 'diff_summary',
            phase: 'result',
            files: ['snake.cpp', 'old.cpp'],
            file_changes: [
                { path: 'snake.cpp', added: 12, removed: 0, created: true },
                { path: 'old.cpp', added: 0, removed: 3, deleted: true },
            ],
        }))).toEqual([
            { path: 'snake.cpp', created: true, deleted: false, added: 12, removed: 0 },
            { path: 'old.cpp', created: false, deleted: true, added: 0, removed: 3 },
        ]);
    });
});

describe('cloudAgentPreviewChangesFromParts', () => {
    it('keeps the latest counts, remembers created, and drops a later delete', () => {
        const parts = [
            { kind: 'progress', text: event({ event: 'tool_finished', detail: 'write_file', outcome: 'success', files: ['src/snake.cpp'], created: true, added: 4, removed: 0 }) },
            { kind: 'text', text: 'not a progress line' },
            { kind: 'progress', text: event({ event: 'tool_finished', detail: 'write_file', outcome: 'success', files: ['src/snake.cpp'], added: 9, removed: 1 }) },
            { kind: 'progress', text: event({ event: 'diff_summary', phase: 'result', file_changes: [{ path: 'src/gone.cpp', added: 0, removed: 2, deleted: true }] }) },
            { kind: 'progress', text: event({ event: 'tool_finished', detail: 'write_file', outcome: 'success', files: ['src/keep.h'], added: 2, removed: 0 }) },
            { kind: 'progress', text: event({ event: 'diff_summary', phase: 'result', file_changes: [{ path: 'src/keep.h', added: 0, removed: 2, deleted: true }] }) },
        ];
        expect(cloudAgentPreviewChangesFromParts(parts)).toEqual([
            { path: 'src/snake.cpp', created: true, added: 9, removed: 1 },
        ]);
    });
});

describe('cloudAgentPreviewDroppedPaths', () => {
    it('lists a deleted path until a later write puts it back', () => {
        const write = { kind: 'progress', text: event({ event: 'tool_finished', detail: 'write_file', outcome: 'success', files: ['src/snake.cpp'], created: true, added: 4, removed: 0 }) };
        const deleted = { kind: 'progress', text: event({ event: 'diff_summary', phase: 'result', file_changes: [{ path: 'src/snake.cpp', added: 0, removed: 4, deleted: true }] }) };
        const again = { kind: 'progress', text: event({ event: 'tool_finished', detail: 'write_file', outcome: 'success', files: ['src/snake.cpp'], added: 4, removed: 0 }) };
        expect(cloudAgentPreviewDroppedPaths([write])).toEqual([]);
        expect(cloudAgentPreviewDroppedPaths([write, deleted])).toEqual(['src/snake.cpp']);
        expect(cloudAgentPreviewChangesFromParts([write, deleted])).toEqual([]);
        expect(cloudAgentPreviewDroppedPaths([write, deleted, again])).toEqual([]);
        expect(cloudAgentPreviewChangesFromParts([write, deleted, again])).toEqual([
            { path: 'src/snake.cpp', created: false, added: 4, removed: 0 },
        ]);
    });

    it('drops a written path after a later delete and keeps an untouched delete', () => {
        const parts = [
            { kind: 'progress', text: event({ event: 'diff_summary', phase: 'result', file_changes: [{ path: 'src/gone.cpp', added: 0, removed: 2, deleted: true }] }) },
            { kind: 'progress', text: event({ event: 'tool_finished', detail: 'write_file', outcome: 'success', files: ['src/keep.h'], added: 2, removed: 0 }) },
            { kind: 'progress', text: event({ event: 'diff_summary', phase: 'result', file_changes: [{ path: 'src/keep.h', added: 0, removed: 2, deleted: true }] }) },
        ];
        expect(cloudAgentPreviewDroppedPaths(parts)).toEqual(['src/gone.cpp', 'src/keep.h']);
        expect(cloudAgentPreviewChangesFromParts(parts)).toEqual([]);
    });
});

describe('cloudAgentPreviewReadRetries', () => {
    it('retries a transport failure and not a binary, a directory, or a confirmed miss', () => {
        expect(cloudAgentPreviewReadRetries(new Error('这个文件暂时读取失败'))).toBe(true);
        expect(cloudAgentPreviewReadRetries('')).toBe(true);
        expect(cloudAgentPreviewReadRetries(new Error('binary files cannot be previewed'))).toBe(false);
        expect(cloudAgentPreviewReadRetries(new Error('这是一个目录，不能按文件读取'))).toBe(false);
        expect(cloudAgentPreviewReadRetries(new Error('这个文件已经不在了'))).toBe(false);
        expect(cloudAgentPreviewReadRetries(new Error('path must be relative to the working directory'))).toBe(false);
    });
});

describe('cloudAgentPreviewDedupeKey', () => {
    it('changes when the counts change', () => {
        const left = cloudAgentPreviewDedupeKey('cloud-agent:a', { path: 'src/a.cpp', created: true, added: 1, removed: 0 });
        const right = cloudAgentPreviewDedupeKey('cloud-agent:a', { path: 'src/a.cpp', created: true, added: 2, removed: 0 });
        expect(left).not.toBe(right);
    });
});

describe('cloudAgentPreviewCodeFile', () => {
    const base = {
        projectPath: 'cloud-agent:inst-1',
        path: 'src/snake.cpp',
        content: 'int main(){}\n',
        language: 'cpp',
        updatedAt: 10,
    };

    it('treats a created flag as a new file and does not attach a baseline', () => {
        const file = cloudAgentPreviewCodeFile({ ...base, created: true, original: 'old', added: 12, removed: 0 });
        expect(file.opType).toBe('create');
        expect(file.original).toBeUndefined();
        expect(file.forceOpen).toBe(true);
        expect(file.projectPath).toBe('cloud-agent:inst-1');
        expect(file.fileName).toBe('snake.cpp');
    });

    it('diffs a modify against the returned baseline', () => {
        const file = cloudAgentPreviewCodeFile({ ...base, created: false, original: 'before\n', added: 3, removed: 1 });
        expect(file.opType).toBe('modify');
        expect(file.original).toBe('before\n');
    });

    it('treats a pure insertion with no baseline as a create', () => {
        const file = cloudAgentPreviewCodeFile({ ...base, created: false, added: 4, removed: 0 });
        expect(file.opType).toBe('create');
        expect(file.original).toBeUndefined();
    });

    it('shows a removal with no baseline as a modify and does not invent a diff', () => {
        const file = cloudAgentPreviewCodeFile({ ...base, created: false, added: 1, removed: 2 });
        expect(file.opType).toBe('modify');
        expect(file.original).toBeUndefined();
    });
});

describe('cloudAgentTurnFileChanges', () => {
    const write = (path: string, added: number, removed: number) => event({
        event: 'tool_finished',
        detail: 'write_file',
        outcome: 'success',
        files: [path],
        added,
        removed,
        file_changes: [{ path, added, removed }],
    });

    it('uses the transcript timeline and does not add the same live write again', () => {
        const snake = write('src/snake.cpp', 3, 1);
        const files = cloudAgentTurnFileChanges({
            messages: [{ codingTimeline: [{ kind: 'progress', content: snake }] }],
            live: { parts: [{ kind: 'progress', text: snake }, { kind: 'progress', text: write('src/board.cpp', 4, 0) }] },
        });
        expect(files).toEqual([expect.objectContaining({ path: 'src/snake.cpp', added: 3, removed: 1 })]);
    });

    it('uses the live round when the transcript has no timeline yet', () => {
        const files = cloudAgentTurnFileChanges({
            messages: [{ content: '写好了' }],
            live: { parts: [{ kind: 'progress', text: write('src/snake.cpp', 3, 1) }] },
        });
        expect(files.map(file => file.path)).toEqual(['src/snake.cpp']);
    });
});

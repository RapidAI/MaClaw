import { describe, expect, it } from 'vitest';
import { taskbarInProgressCount } from '../taskbarInProgressCount';

const row = (overrides: Record<string, unknown> = {}) => ({
    id: 'task-1',
    name: 'Build dashboard',
    project_path: 'D:/work/tasks/build',
    ...overrides,
});

describe('taskbarInProgressCount', () => {
    it('counts task rows that are in progress', () => {
        expect(taskbarInProgressCount({
            tasks: [
                row({ active_workflow: { status: 'running' } }),
                row({ id: 'done', project_path: 'D:/work/tasks/done', active_workflow: { status: 'completed' } }),
            ],
        })).toBe(1);
    });

    it('adds bot tasks and running background loops, and skips paused loops', () => {
        expect(taskbarInProgressCount({
            tasks: [],
            botTasks: 2,
            backgroundLoops: [{ status: 'running' }, { status: 'paused' }, { status: 'completed' }],
        })).toBe(3);
    });

    it('does not add a background loop whose remote session is already counted', () => {
        expect(taskbarInProgressCount({
            tasks: [],
            backgroundLoops: [{ status: 'running', session_id: 'sess-1' }],
            remoteSessions: [{ id: 'sess-1', launch_source: 'ai', status: 'running', project_path: 'D:/remote' }],
        })).toBe(1);
    });

    it('keeps a running loop that has no remote session beside a different live session', () => {
        expect(taskbarInProgressCount({
            tasks: [],
            backgroundLoops: [{ status: 'running' }],
            remoteSessions: [{ id: 'sess-2', launch_source: 'ai', status: 'running', project_path: 'D:/remote' }],
        })).toBe(2);
    });

    it('does not treat a parent workspace root as the same project', () => {
        expect(taskbarInProgressCount({
            tasks: [row({ active_workflow: { status: 'running' } })],
            remoteSessions: [{
                id: 'sess-other',
                launch_source: 'ai',
                status: 'running',
                project_path: 'D:/other',
                workspace_root: 'D:/work',
            }],
        })).toBe(2);
    });

    it('does not treat a sibling directory as the same project', () => {
        expect(taskbarInProgressCount({
            tasks: [row({ active_workflow: { status: 'running' } })],
            remoteSessions: [{ launch_source: 'ai', status: 'running', project_path: 'D:/work/tasks/build2' }],
        })).toBe(2);
    });

    it('treats a session inside the running project directory as the same job', () => {
        expect(taskbarInProgressCount({
            tasks: [row({ active_workflow: { status: 'running' } })],
            remoteSessions: [{ id: 'sess-nested', launch_source: 'ai', status: 'running', project_path: 'D:/work/tasks/build/src' }],
            backgroundLoops: [{ status: 'running', session_id: 'sess-nested' }],
        })).toBe(1);
    });

    it('adds a live AI session on a project that has no in-progress row', () => {
        expect(taskbarInProgressCount({
            tasks: [],
            remoteSessions: [{ launch_source: 'ai', status: 'running', project_path: 'D:/remote' }],
        })).toBe(1);
    });

    it('does not count a live AI session twice when its project is already an in-progress row', () => {
        expect(taskbarInProgressCount({
            tasks: [row({ active_workflow: { status: 'running' } })],
            remoteSessions: [{ launch_source: 'ai', status: 'running', project_path: 'D:\\work\\tasks\\build' }],
        })).toBe(1);
    });

    it('still counts a live AI session on a different project', () => {
        expect(taskbarInProgressCount({
            tasks: [row({ active_workflow: { status: 'running' } })],
            remoteSessions: [{ launch_source: 'ai', status: 'running', project_path: 'D:/other' }],
            botTasks: 1,
        })).toBe(3);
    });

    it('counts a stale row once while its live run is busy, including the matching session', () => {
        expect(taskbarInProgressCount({
            tasks: [row({ project_path: 'D:/work/tasks/stale', active_workflow: { status: 'pending' } })],
            signals: { busyTaskRuns: { projectPaths: ['D:/work/tasks/stale'], expertIds: [] } },
            remoteSessions: [{ launch_source: 'ai', status: 'running', project_path: 'D:/work/tasks/stale' }],
        })).toBe(1);
    });

    it('treats a cloud session file as the same job as the running workspace row', () => {
        expect(taskbarInProgressCount({
            tasks: [row({
                project_path: 'D:/work/tasks/math-book',
                working_dir: 'C:/Users/me/.maclaw/data/cloud-workspaces/tenant/cws_math',
                active_workflow: { status: 'running' },
            })],
            remoteSessions: [{
                launch_source: 'ai',
                status: 'running',
                project_path: 'C:/Users/me/.maclaw/data/cloud-workspaces/tenant/cws_math/chapter-1.md',
            }],
        })).toBe(1);
    });

    it('ignores finished sessions, paused task rows, and a non-numeric bot count', () => {
        expect(taskbarInProgressCount({
            tasks: [row({ active_workflow: { status: 'paused' } })],
            botTasks: Number.NaN,
            remoteSessions: [{ launch_source: 'ai', status: 'completed', project_path: 'D:/done' }],
            backgroundLoops: [{ status: 'expired' }],
        })).toBe(0);
        expect(taskbarInProgressCount({ botTasks: Number.POSITIVE_INFINITY })).toBe(0);
        expect(taskbarInProgressCount()).toBe(0);
    });
});

import { describe, expect, it } from 'vitest';
import { presentDialogMessage } from '../../../CustomDialog';
import { defaultTaskDraft, withRunWindow } from '../taskDraft';
import {
    isRunWindowDue,
    runWindowChipLabel,
    runWindowCreatedNotice,
    runWindowCreateRequest,
    runWindowStatusLabel,
    validateRunWindow,
} from '../runWindow';

describe('run window', () => {
    it('defaults to immediate and labels a same-day window', () => {
        expect(runWindowChipLabel(defaultTaskDraft().runWindow, true)).toBe('立即');
        const scheduled = withRunWindow(defaultTaskDraft(), {
            mode: 'scheduled',
            startAt: '2099-03-01T02:00',
            endAt: '2099-03-01T06:30',
        });
        expect(runWindowChipLabel(scheduled.runWindow, true)).toBe('03-01 02:00–06:30');
        expect(runWindowChipLabel({ mode: 'scheduled', startAt: '2099-03-01T02:00', endAt: '' }, true)).toBe('03-01 02:00');
    });

    it('describes when a scheduled task will start and pause', () => {
        const startOnly = runWindowCreatedNotice('夜间整理', '2099-03-01T02:00', '', true);
        expect(startOnly.title).toBe('定时任务已创建');
        expect(startOnly.message).toBe('定时任务「夜间整理」已经创建，将于 2099-03-01 02:00 启动。');
        const withEnd = runWindowCreatedNotice('夜间整理', '2099-03-01T02:00', '2099-03-01T06:30', true);
        expect(withEnd.message).toBe('定时任务「夜间整理」已经创建，将于 2099-03-01 02:00 启动。\n2099-03-01 06:30 如果任务未完成，将暂停。');
        const visible = presentDialogMessage(withEnd.message);
        expect(`${visible.lead}\n${visible.detail || ''}${visible.follow || ''}`).toContain('2099-03-01 02:00');
        expect(`${visible.lead}\n${visible.detail || ''}${visible.follow || ''}`).toContain('2099-03-01 06:30 如果任务未完成，将暂停');
        const quotedName = runWindowCreatedNotice('整理」草稿', '2099-03-01T02:00', '2099-03-01T06:30', true);
        const quotedVisible = presentDialogMessage(quotedName.message);
        const quotedText = `${quotedVisible.lead}\n${quotedVisible.detail || ''}${quotedVisible.follow || ''}`;
        expect(quotedText).toContain('2099-03-01 02:00');
        expect(quotedText).toContain('2099-03-01 06:30 如果任务未完成，将暂停');
    });

    it('rejects a missing start, a past start, and an end that is not later', () => {
        const now = new Date('2099-03-01T00:00');
        expect(validateRunWindow({ mode: 'scheduled', startAt: '', endAt: '' }, now).ok).toBe(false);
        expect(validateRunWindow({ mode: 'scheduled', startAt: '2099-02-01T00:00', endAt: '' }, now).ok).toBe(false);
        expect(validateRunWindow({ mode: 'scheduled', startAt: '2099-03-01T02:00', endAt: '2099-03-01T02:00' }, now).ok).toBe(false);
        expect(validateRunWindow({ mode: 'scheduled', startAt: '2099-03-01T02:00', endAt: '' }, now).ok).toBe(true);
        expect(validateRunWindow(defaultTaskDraft().runWindow, now).ok).toBe(true);
    });

    it('stores the wizard payload without a remote password', () => {
        const draft = withRunWindow({
            ...defaultTaskDraft(),
            taskType: 'coding',
            workspace: {
                kind: 'remote',
                remote: { host: '10.0.0.8', port: 22, user: 'dev', password: 'secret', workDir: '/src' },
            },
        }, { mode: 'scheduled', startAt: '2099-03-01T02:00', endAt: '' });
        const request = runWindowCreateRequest('夜间整理', '夜间整理', '夜间整理', draft);
        expect(request.draft.remoteHost).toBe('10.0.0.8');
        expect(request.draft).not.toHaveProperty('password');
        expect(JSON.stringify(request)).not.toContain('secret');
    });

    it('treats a pending task as due only inside the window', () => {
        const task = {
            id: 'rw-1',
            name: '夜间整理',
            prompt: '整理',
            initialMessage: '整理',
            draft: runWindowCreateRequest('夜间整理', '整理', '整理', defaultTaskDraft()).draft,
            status: 'pending',
            startAt: '2099-03-01T02:00',
            endAt: '2099-03-01T06:00',
            createdAt: '2099-03-01T00:00',
        };
        expect(isRunWindowDue(task, new Date('2099-03-01T01:00'))).toBe(false);
        expect(isRunWindowDue(task, new Date('2099-03-01T02:00'))).toBe(true);
        expect(isRunWindowDue(task, new Date('2099-03-01T06:00'))).toBe(false);
        expect(isRunWindowDue({ ...task, endAt: undefined, status: 'pending' }, new Date('2099-03-02T02:00'))).toBe(true);
        expect(isRunWindowDue({ ...task, status: 'running' }, new Date('2099-03-01T03:00'))).toBe(false);
    });

    it('labels a finished run as done', () => {
        expect(runWindowStatusLabel('running', true)).toBe('执行中');
        expect(runWindowStatusLabel('completed', true)).toBe('已完成');
        expect(runWindowStatusLabel('completed', false)).toBe('Done');
    });
});

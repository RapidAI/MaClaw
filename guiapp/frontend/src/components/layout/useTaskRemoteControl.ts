import { useCallback, useEffect, useRef, useState } from 'react';
import { EventsOn } from '../../../wailsjs/runtime';
import type { TaskManagementItem } from './SidebarTaskManagement';

export type RemotePermission = 'view' | 'operate';

export type RemoteControlMachine = {
    id: string;
    name: string;
    online: boolean;
};

export type RemoteControlGrant = {
    id: string;
    machine_id?: string;
    machine_name?: string;
    account?: string;
    permission: RemotePermission | string;
    status: string;
    same_account?: boolean;
    online?: boolean;
    from_machine?: string;
};

export type RemoteControlState = {
    machine_id: string;
    machines: RemoteControlMachine[];
    grants: RemoteControlGrant[];
    pending: RemoteControlGrant[];
    invites: RemoteControlGrant[];
    controlled: RemoteControlGrant[];
};

export type RemoteTaskMessage = {
    role: string;
    content: string;
};

export type RemoteTaskArtifact = {
    title?: string;
    preview?: string;
    source_type?: string;
};

export type RemoteTaskDetail = {
    task?: TaskManagementItem;
    messages: RemoteTaskMessage[];
    artifacts: RemoteTaskArtifact[];
};

const EMPTY_STATE: RemoteControlState = {
    machine_id: '',
    machines: [],
    grants: [],
    pending: [],
    invites: [],
    controlled: [],
};

function appBinding(name: string): ((...args: unknown[]) => Promise<unknown>) | null {
    const bindings = (window as unknown as { go?: { main?: { App?: Record<string, unknown> } } }).go?.main?.App;
    const fn = bindings?.[name];
    return typeof fn === 'function' ? fn as (...args: unknown[]) => Promise<unknown> : null;
}

async function callBinding<T>(name: string, fallback: T, ...args: unknown[]): Promise<T> {
    const fn = appBinding(name);
    if (!fn) return fallback;
    const value = await fn(...args);
    return (value ?? fallback) as T;
}

function errorText(err: unknown, fallback: string) {
    if (err instanceof Error && err.message.trim()) return err.message;
    if (typeof err === 'string' && err.trim()) return err;
    if (err && typeof err === 'object' && 'message' in err) {
        const message = String((err as { message?: unknown }).message || '').trim();
        if (message) return message;
    }
    return fallback;
}

function sameSnapshot(left: unknown, right: unknown) {
    return JSON.stringify(left) === JSON.stringify(right);
}

function asState(value: RemoteControlState | null | undefined): RemoteControlState {
    return {
        machine_id: value?.machine_id || '',
        machines: value?.machines || [],
        grants: value?.grants || [],
        pending: value?.pending || [],
        invites: value?.invites || [],
        controlled: value?.controlled || [],
    };
}

export function useTaskRemoteControl() {
    const [state, setState] = useState<RemoteControlState>(EMPTY_STATE);
    const [selectedId, setSelectedId] = useState('');
    const [tasks, setTasks] = useState<TaskManagementItem[]>([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState('');
    const [detail, setDetail] = useState<RemoteTaskDetail | null>(null);
    const [detailPath, setDetailPath] = useState('');
    const [creating, setCreating] = useState(false);
    const taskGen = useRef(0);
    const detailGen = useRef(0);
    const tasksForGrant = useRef('');
    const tasksRef = useRef(tasks);
    const detailStamp = useRef('');
    const detailFetchedAt = useRef(0);
    // A later list poll must not wipe an error the user just caused.
    const errorOwner = useRef<'' | 'list' | 'action'>('');
    tasksRef.current = tasks;

    const refresh = useCallback(async () => {
        try {
            const next = asState(await callBinding<RemoteControlState>('ListRemoteControlState', EMPTY_STATE));
            setState(prev => (sameSnapshot(prev, next) ? prev : next));
            setSelectedId(current => (current && next.grants.some(grant => grant.id === current) ? current : ''));
            if (errorOwner.current === 'list') {
                errorOwner.current = '';
                setError('');
            }
        } catch (err) {
            if (errorOwner.current === 'action') return;
            errorOwner.current = 'list';
            setError(errorText(err, '远控状态暂时无法读取'));
        }
    }, []);

    const loadTasks = useCallback(async (grantId: string) => {
        if (!grantId) {
            setTasks([]);
            tasksForGrant.current = '';
            return;
        }
        const gen = ++taskGen.current;
        if (!appBinding('RemoteControlOp')) {
            setTasks([]);
            tasksForGrant.current = '';
            setLoading(false);
            errorOwner.current = 'list';
            setError('远控尚未就绪');
            return;
        }
        if (tasksForGrant.current !== grantId) setLoading(true);
        try {
            const raw = await callBinding<string>('RemoteControlOp', '{}', grantId, 'list_tasks', '');
            if (gen !== taskGen.current) return;
            const parsed = JSON.parse(raw || '{}') as { tasks?: TaskManagementItem[] };
            const nextTasks = Array.isArray(parsed.tasks) ? parsed.tasks : [];
            setTasks(prev => (sameSnapshot(prev, nextTasks) ? prev : nextTasks));
            tasksForGrant.current = grantId;
            if (errorOwner.current === 'list') {
                errorOwner.current = '';
                setError('');
            }
        } catch (err) {
            if (gen !== taskGen.current) return;
            setTasks([]);
            tasksForGrant.current = '';
            errorOwner.current = 'list';
            setError(errorText(err, '对方任务暂时无法读取'));
        } finally {
            if (gen === taskGen.current) setLoading(false);
        }
    }, []);

    const openTask = useCallback(async (grantId: string, projectPath: string) => {
        const gen = ++detailGen.current;
        setDetailPath(projectPath);
        if (!appBinding('RemoteControlOp')) {
            setDetail({ messages: [], artifacts: [] });
            errorOwner.current = 'action';
            setError('远控尚未就绪');
            return;
        }
        try {
            const raw = await callBinding<string>('RemoteControlOp', '{}', grantId, 'get_task', JSON.stringify({ project_path: projectPath }));
            if (gen !== detailGen.current) return;
            const parsed = JSON.parse(raw || '{}') as RemoteTaskDetail;
            const next = {
                task: parsed.task,
                messages: parsed.messages || [],
                artifacts: parsed.artifacts || [],
            };
            setDetail(prev => (sameSnapshot(prev, next) ? prev : next));
            detailStamp.current = `${projectPath}:${parsed.task?.last_activity || ''}`;
            detailFetchedAt.current = Date.now();
        } catch (err) {
            if (gen !== detailGen.current) return;
            setDetail({ messages: [], artifacts: [] });
            errorOwner.current = 'action';
            setError(errorText(err, '会话暂时无法读取'));
        }
    }, []);

    useEffect(() => {
        void refresh();
        const timer = window.setInterval(() => {
            if (document.hidden) return;
            void refresh();
        }, 20000);
        let offNew = () => {};
        let offRevoke = () => {};
        try {
            offNew = EventsOn('notification:new', (payload: unknown) => {
                const record = payload && typeof payload === 'object' ? payload as { source?: string; Source?: string } : {};
                const source = record.source || record.Source || '';
                if (source.startsWith('remote_control')) void refresh();
            });
            offRevoke = EventsOn('notification:revoke', () => { void refresh(); });
        } catch {
            offNew = () => {};
            offRevoke = () => {};
        }
        return () => {
            window.clearInterval(timer);
            offNew();
            offRevoke();
        };
    }, [refresh]);

    const selected = state.grants.find(grant => grant.id === selectedId) || null;
    const online = !!selected && selected.online !== false;
    const operate = selected?.permission === 'operate' && online;

    useEffect(() => {
        if (!selectedId || !online) {
            taskGen.current += 1;
            setTasks([]);
            tasksForGrant.current = '';
            setLoading(false);
            setDetail(null);
            setDetailPath('');
            setCreating(false);
            return;
        }
        if (tasksForGrant.current !== selectedId) {
            setTasks([]);
            tasksForGrant.current = '';
            setLoading(true);
        }
        const tick = () => {
            if (document.hidden) return;
            void loadTasks(selectedId);
        };
        tick();
        const timer = window.setInterval(tick, 12000);
        return () => window.clearInterval(timer);
    }, [loadTasks, online, selectedId]);

    useEffect(() => {
        if (!selectedId || !detailPath || !online) return;
        const timer = window.setInterval(() => {
            if (document.hidden) return;
            const task = tasksRef.current.find(item => item.project_path === detailPath);
            const stamp = `${detailPath}:${task?.last_activity || ''}`;
            // Idle conversations stay put. A running task bumps last activity
            // on the list poll, and a quiet minute still refreshes once.
            if (stamp === detailStamp.current && Date.now() - detailFetchedAt.current < 60000) return;
            void openTask(selectedId, detailPath);
        }, 15000);
        return () => window.clearInterval(timer);
    }, [detailPath, online, openTask, selectedId]);

    const run = useCallback(async (op: string, body: Record<string, unknown>) => {
        if (!selected) return false;
        if (selected.permission !== 'operate' && op !== 'list_tasks' && op !== 'get_task') {
            errorOwner.current = 'action';
            setError('当前权限只能查看');
            return false;
        }
        if (selected.online === false) {
            errorOwner.current = 'action';
            setError('对方离线');
            return false;
        }
        const fn = appBinding('RemoteControlOp');
        if (!fn) {
            errorOwner.current = 'action';
            setError('远控尚未就绪');
            return false;
        }
        try {
            await fn(selected.id, op, JSON.stringify(body));
            errorOwner.current = '';
            setError('');
            await loadTasks(selected.id);
            if (detailPath) await openTask(selected.id, detailPath);
            // The send returns before the other computer stores the turn.
            // Drop the idle stamp so the next poll shows that turn.
            if (op === 'send_message' || op === 'stop_task') {
                detailStamp.current = '';
                detailFetchedAt.current = 0;
            }
            return true;
        } catch (err) {
            errorOwner.current = 'action';
            setError(errorText(err, '远控操作失败'));
            return false;
        }
    }, [detailPath, loadTasks, openTask, selected]);

    const mutate = useCallback(async (name: string, args: unknown[]) => {
        const fn = appBinding(name);
        if (!fn) {
            errorOwner.current = 'action';
            setError('远控尚未就绪');
            return null;
        }
        try {
            const value = await fn(...args);
            errorOwner.current = '';
            setError('');
            await refresh();
            return value;
        } catch (err) {
            errorOwner.current = 'action';
            setError(errorText(err, '远控请求失败'));
            return null;
        }
    }, [refresh]);

    return {
        state,
        selected,
        tasks,
        loading,
        error,
        detail,
        detailPath,
        creating,
        refresh,
        selectLocal: () => {
            detailGen.current += 1;
            errorOwner.current = '';
            setSelectedId('');
            setError('');
            setDetail(null);
            setDetailPath('');
        },
        selectGrant: (id: string) => {
            detailGen.current += 1;
            errorOwner.current = '';
            setSelectedId(id);
            setError('');
            setDetail(null);
            setDetailPath('');
        },
        setCreating,
        grantSameAccount: async (machineID: string, permission: RemotePermission) => {
            const value = await mutate('GrantRemoteControlSameAccount', [machineID, permission]);
            const record = value && typeof value === 'object' ? value as { id?: string; ID?: string } : null;
            const id = record?.id || record?.ID || '';
            if (id) setSelectedId(id);
            return value !== null;
        },
        invite: async (account: string, permission: RemotePermission) => {
            const value = await mutate('InviteRemoteControl', [account, permission]);
            return value !== null;
        },
        accept: async (grantID: string) => {
            const value = await mutate('AcceptRemoteControlInvite', [grantID]);
            return value !== null;
        },
        reject: async (grantID: string) => {
            const value = await mutate('RejectRemoteControlInvite', [grantID]);
            return value !== null;
        },
        ignore: async (grantID: string) => {
            const value = await mutate('IgnoreRemoteControlInvite', [grantID]);
            return value !== null;
        },
        revoke: async (grantID: string) => {
            const value = await mutate('RevokeRemoteControl', [grantID]);
            if (value !== null && selectedId === grantID) setSelectedId('');
            return value !== null;
        },
        openTask: (projectPath: string) => {
            if (selected && selected.online !== false) void openTask(selected.id, projectPath);
        },
        closeDetail: () => { setDetail(null); setDetailPath(''); },
        createTask: async (name: string) => {
            const ok = await run('create_task', { name });
            if (ok) setCreating(false);
            return ok;
        },
        sendMessage: async (projectPath: string, text: string) => run('send_message', { project_path: projectPath, text }),
        stopTask: async (projectPath: string) => run('stop_task', { project_path: projectPath }),
        renameTask: async (projectPath: string, name: string) => run('rename_task', { project_path: projectPath, name }),
        pinTask: async (projectPath: string, pinned: boolean) => run('pin_task', { project_path: projectPath, pinned }),
        operate,
    };
}

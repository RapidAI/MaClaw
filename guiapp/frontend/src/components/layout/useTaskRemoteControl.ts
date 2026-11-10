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
    reasoning_content?: string;
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
    const [errorKind, setErrorKind] = useState<'' | 'state' | 'tasks' | 'action' | 'conversation'>('');
    const [detail, setDetail] = useState<RemoteTaskDetail | null>(null);
    const [detailPath, setDetailPath] = useState('');
    const [creating, setCreating] = useState(false);
    const taskGen = useRef(0);
    const detailGen = useRef(0);
    const tasksForGrant = useRef('');
    const tasksRef = useRef(tasks);
    const selectedIdRef = useRef(selectedId);
    const detailPathRef = useRef(detailPath);
    const detailStamp = useRef('');
    const detailFetchedAt = useRef(0);
    const detailFollowRef = useRef(0);
    const opRefreshGen = useRef(0);
    // A later poll must not wipe an error the user just caused, and a state
    // poll must not clear an error that belongs to the task list.
    const errorOwner = useRef<'' | 'state' | 'tasks' | 'action' | 'conversation'>('');
    const ownError = (kind: '' | 'state' | 'tasks' | 'action' | 'conversation', message: string) => {
        errorOwner.current = kind;
        setErrorKind(kind);
        setError(message);
    };
    const chatErrorHeld = () => errorOwner.current === 'action' || errorOwner.current === 'conversation';
    tasksRef.current = tasks;
    selectedIdRef.current = selectedId;
    detailPathRef.current = detailPath;

    const refresh = useCallback(async () => {
        try {
            const next = asState(await callBinding<RemoteControlState>('ListRemoteControlState', EMPTY_STATE));
            setState(prev => (sameSnapshot(prev, next) ? prev : next));
            setSelectedId(current => {
                if (current && next.grants.some(grant => grant.id === current)) return current;
                if (current) detailGen.current += 1;
                return '';
            });
            if (errorOwner.current === 'state') ownError('', '');
        } catch (err) {
            if (chatErrorHeld() || errorOwner.current === 'tasks') return;
            ownError('state', errorText(err, '远控状态暂时无法读取'));
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
            if (!chatErrorHeld()) ownError('tasks', '远控尚未就绪');
            return;
        }
        if (tasksForGrant.current !== grantId) setLoading(true);
        try {
            const raw = await callBinding<string>('RemoteControlOp', '{}', grantId, 'list_tasks', '');
            if (gen !== taskGen.current || selectedIdRef.current !== grantId) return;
            const parsed = JSON.parse(raw || '{}') as { tasks?: TaskManagementItem[]; error?: string };
            if (parsed.error) throw new Error(parsed.error);
            if (!Array.isArray(parsed.tasks)) throw new Error('对方任务暂时无法读取');
            setTasks(prev => (sameSnapshot(prev, parsed.tasks) ? prev : parsed.tasks || []));
            tasksForGrant.current = grantId;
            if (errorOwner.current === 'tasks' || errorOwner.current === 'state') ownError('', '');
        } catch (err) {
            if (gen !== taskGen.current || selectedIdRef.current !== grantId) return;
            // A failed refresh keeps the list already on screen. Only the first
            // load, which never landed, clears it.
            if (tasksForGrant.current !== grantId) {
                setTasks([]);
                tasksForGrant.current = '';
            }
            if (chatErrorHeld()) return;
            ownError('tasks', err instanceof SyntaxError ? '对方任务暂时无法读取' : errorText(err, '对方任务暂时无法读取'));
        } finally {
            // A list the person already left must not clear the spinner for
            // the machine they are looking at now.
            if (gen === taskGen.current && selectedIdRef.current === grantId) setLoading(false);
        }
    }, []);

    const openTask = useCallback(async (grantId: string, projectPath: string) => {
        // A poll for the previous machine can start after the person has
        // already returned to this computer. Leave the current tab alone.
        if (selectedIdRef.current !== grantId) return;
        const refreshing = detailPathRef.current === projectPath;
        const gen = ++detailGen.current;
        setDetailPath(projectPath);
        if (!appBinding('RemoteControlOp')) {
            if (!refreshing) setDetail({ messages: [], artifacts: [] });
            ownError('conversation', '远控尚未就绪');
            return;
        }
        try {
            const raw = await callBinding<string>('RemoteControlOp', '{}', grantId, 'get_task', JSON.stringify({ project_path: projectPath }));
            if (gen !== detailGen.current || selectedIdRef.current !== grantId) return;
            const parsed = JSON.parse(raw || '{}') as RemoteTaskDetail & { error?: string };
            if (parsed.error) throw new Error(parsed.error);
            if (!Array.isArray(parsed.messages)) throw new Error('会话暂时无法读取');
            const next = {
                task: parsed.task,
                messages: parsed.messages || [],
                artifacts: parsed.artifacts || [],
            };
            setDetail(prev => (sameSnapshot(prev, next) ? prev : next));
            detailStamp.current = `${projectPath}:${parsed.task?.last_activity || ''}`;
            detailFetchedAt.current = Date.now();
            if (errorOwner.current === 'conversation') ownError('', '');
        } catch (err) {
            if (gen !== detailGen.current || selectedIdRef.current !== grantId) return;
            if (!refreshing) setDetail({ messages: [], artifacts: [] });
            ownError('conversation', err instanceof SyntaxError ? '会话暂时无法读取' : errorText(err, '会话暂时无法读取'));
        }
    }, []);

    useEffect(() => {
        void refresh();
        const timer = window.setInterval(() => {
            if (document.hidden) return;
            void refresh();
        }, 20000);
        const onVisible = () => {
            if (document.hidden) return;
            void refresh();
        };
        document.addEventListener('visibilitychange', onVisible);
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
            document.removeEventListener('visibilitychange', onVisible);
            offNew();
            offRevoke();
        };
    }, [refresh]);

    const selected = state.grants.find(grant => grant.id === selectedId) || null;
    const online = !!selected && selected.online !== false;
    const operate = selected?.permission === 'operate' && online;

    useEffect(() => {
        if (!selectedId) {
            taskGen.current += 1;
            detailGen.current += 1;
            setTasks([]);
            tasksForGrant.current = '';
            setLoading(false);
            setDetail(null);
            setDetailPath('');
            setCreating(false);
            return;
        }
        if (!online) {
            setLoading(false);
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
        const onVisible = () => { tick(); };
        document.addEventListener('visibilitychange', onVisible);
        return () => {
            window.clearInterval(timer);
            document.removeEventListener('visibilitychange', onVisible);
        };
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
        const onVisible = () => {
            if (document.hidden) return;
            void openTask(selectedId, detailPath);
        };
        document.addEventListener('visibilitychange', onVisible);
        return () => {
            window.clearInterval(timer);
            document.removeEventListener('visibilitychange', onVisible);
        };
    }, [detailPath, online, openTask, selectedId]);

    const openActivity = selected && detailPath
        ? tasks.find(item => item.project_path === detailPath)?.last_activity || ''
        : '';
    useEffect(() => {
        if (!selectedId || !detailPath || !online || !detailStamp.current) return;
        const stamp = `${detailPath}:${openActivity}`;
        if (stamp === detailStamp.current) return;
        if (Date.now() - detailFetchedAt.current < 800) return;
        void openTask(selectedId, detailPath);
    }, [detailPath, online, openActivity, openTask, selectedId]);

    useEffect(() => () => window.clearTimeout(detailFollowRef.current), []);

    const refreshAfterRemoteOp = useCallback(async (grantId: string, openPath: string, op: string) => {
        // A newer send or stop can start while this refresh is still reading.
        // The older one must not clear the stamp or replace the follow-up.
        const gen = ++opRefreshGen.current;
        await loadTasks(grantId);
        if (gen !== opRefreshGen.current) return;
        if (selectedIdRef.current === grantId && detailPathRef.current === openPath && openPath) {
            await openTask(grantId, openPath);
        }
        if (gen !== opRefreshGen.current) return;
        if ((op === 'send_message' || op === 'stop_task') && selectedIdRef.current === grantId && detailPathRef.current === openPath) {
            detailStamp.current = '';
            detailFetchedAt.current = 0;
            // The other computer stores the turn after the send returns.
            // One more read picks it up without waiting for the 15s poll.
            window.clearTimeout(detailFollowRef.current);
            detailFollowRef.current = window.setTimeout(() => {
                if (gen !== opRefreshGen.current) return;
                if (selectedIdRef.current === grantId && detailPathRef.current === openPath) {
                    void openTask(grantId, openPath);
                }
            }, 1000);
        }
    }, [loadTasks, openTask]);

    const run = useCallback(async (op: string, body: Record<string, unknown>) => {
        if (!selected) return false;
        const grantId = selected.id;
        const openPath = detailPath;
        if (selected.permission !== 'operate' && op !== 'list_tasks' && op !== 'get_task') {
            ownError('action', '当前权限只能查看');
            return false;
        }
        if (selected.online === false) {
            ownError('action', '对方离线');
            return false;
        }
        const fn = appBinding('RemoteControlOp');
        if (!fn) {
            ownError('action', '远控尚未就绪');
            return false;
        }
        try {
            await fn(grantId, op, JSON.stringify(body));
            // The person may have closed the conversation or changed machines
            // while this call was in flight. Do not paint that result back.
            if (selectedIdRef.current !== grantId) return true;
            ownError('', '');
            // The list and conversation refresh after the other computer
            // accepts the operation. Stop waits on this promise, so it must
            // not also wait for those reads.
            void refreshAfterRemoteOp(grantId, openPath, op);
            return true;
        } catch (err) {
            if (selectedIdRef.current !== grantId) return false;
            ownError('action', errorText(err, '远控操作失败'));
            return false;
        }
    }, [detailPath, refreshAfterRemoteOp, selected]);

    const mutate = useCallback(async (name: string, args: unknown[]) => {
        const fn = appBinding(name);
        if (!fn) {
            ownError('action', '远控尚未就绪');
            return null;
        }
        try {
            const value = await fn(...args);
            ownError('', '');
            await refresh();
            return value;
        } catch (err) {
            ownError('action', errorText(err, '远控请求失败'));
            return null;
        }
    }, [refresh]);

    return {
        state,
        selected,
        tasks,
        loading,
        error,
        errorKind,
        detail,
        detailPath,
        creating,
        refresh,
        selectLocal: () => {
            detailGen.current += 1;
            taskGen.current += 1;
            tasksForGrant.current = '';
            ownError('', '');
            setSelectedId('');
            setTasks([]);
            setLoading(false);
            setDetail(null);
            setDetailPath('');
        },
        selectGrant: (id: string) => {
            if (selectedIdRef.current === id) return;
            detailGen.current += 1;
            taskGen.current += 1;
            tasksForGrant.current = '';
            ownError('', '');
            setSelectedId(id);
            setTasks([]);
            setLoading(true);
            setCreating(false);
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
            if (value !== null && selectedIdRef.current === grantID) {
                detailGen.current += 1;
                setSelectedId('');
                setDetail(null);
                setDetailPath('');
            }
            return value !== null;
        },
        openTask: (projectPath: string) => {
            if (selected && selected.online !== false) void openTask(selected.id, projectPath);
        },
        closeDetail: () => {
            detailGen.current += 1;
            setDetail(null);
            setDetailPath('');
        },
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

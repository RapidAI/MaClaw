import { useEffect, useRef, useState } from 'react';
import { EventsOn } from '../../../wailsjs/runtime';
import { localizeText } from '../../i18n';
import {
    applyCloudAgentStream,
    applyCloudAgentWatch,
    expireCloudAgentCards,
    cloudAgentRound,
    cloudAgentSessionKey,
    cloudAgentStoreKey,
    subscribeCloudAgentRounds,
} from './cloudAgentSession';
import { cloudAgentCatalog, cloudAssistantInstanceId, sendShownCloudAgent, stopShownCloudAgent } from './cloudAgentAssistant';
import { noteCloudTurn } from './taskPlace';

type CloudAgentRow = {
    id: string;
    name: string;
    kind: string;
    status: string;
};

type CloudTask = { id: string; title: string };

type Binding = (...args: unknown[]) => Promise<unknown>;

function appBinding(name: string): Binding | null {
    const bindings = (window as unknown as { go?: { main?: { App?: Record<string, unknown> } } }).go?.main?.App;
    const fn = bindings?.[name];
    return typeof fn === 'function' ? fn as Binding : null;
}

function listedInstances(value: unknown): CloudAgentRow[] {
    return cloudAgentCatalog(value).instances.flatMap((item) => {
        if (!item || typeof item !== 'object') return [];
        const row = item as Partial<CloudAgentRow>;
        if (!row.id || !row.kind) return [];
        return [{ id: row.id, name: row.name || '', kind: row.kind, status: row.status || '' }];
    });
}

function parseJSON<T>(raw: unknown, fallback: T): T {
    if (typeof raw !== 'string' || !raw.trim()) return fallback;
    try {
        return JSON.parse(raw) as T;
    } catch {
        return fallback;
    }
}

export function CloudAgentManage({ lang, onClose, onCreated }: { lang: string; onClose: () => void; onCreated?: (row: CloudAgentRow) => void }) {
    const [instances, setInstances] = useState<CloudAgentRow[]>([]);
    const [tasks, setTasks] = useState<CloudTask[]>([]);
    const [live, setLive] = useState('');
    const [error, setError] = useState('');
    const [name, setName] = useState('');
    const [kind, setKind] = useState<'session' | 'coding'>('session');
    const [draft, setDraft] = useState('');
    const [choices, setChoices] = useState<Array<{ label: string; command: string }>>([]);
    const [pickedKind, setPickedKind] = useState('');
    const [gitNote, setGitNote] = useState('');
    const [git, setGit] = useState({
        service: 'github',
        origin: 'https://github.com',
        repo: '',
        authorName: '',
        authorEmail: '',
        token: '',
        source: '',
    });
    const selected = useRef({ grant: '', instance: '', task: '', kind: '' });
    const openGeneration = useRef(0);

    const text = (en: string, zh: string, hant?: string) => localizeText(lang, en, zh, hant);

    const refreshLive = () => {
        const current = selected.current;
        const key = cloudAgentStoreKey(current.grant, cloudAgentSessionKey(current.instance, current.task));
        if (!key) {
            setLive('');
            return;
        }
        const round = cloudAgentRound(key);
        const next: Array<{ label: string; command: string }> = [];
        for (const card of round.cards) {
            if (card.expired) continue;
            for (const action of card.actions || []) {
                if (action.command) next.push({ label: action.label || action.command, command: action.command });
            }
        }
        setChoices(next);
        setLive(round.visible || round.body || round.displayText);
    };

    useEffect(() => {
        const list = appBinding('ListCloudAgents');
        if (!list) return;
        void list().then((rows) => {
            setInstances(listedInstances(rows));
        }).catch((err: unknown) => {
            setError(err instanceof Error ? err.message : String(err));
        });
    }, []);

    useEffect(() => subscribeCloudAgentRounds(() => {
        refreshLive();
    }), []);

    useEffect(() => {
        const off = EventsOn('remote-control:stream', (payload: unknown) => {
            const key = applyCloudAgentStream(payload);
            const current = selected.current;
            const want = cloudAgentStoreKey(current.grant, cloudAgentSessionKey(current.instance, current.task));
            if (key && key === want) refreshLive();
        });
        return () => { if (typeof off === 'function') off(); };
    }, []);

    const fail = (err: unknown) => {
        setError(err instanceof Error ? err.message : String(err));
    };

    const openInstance = async (row: CloudAgentRow) => {
        const generation = ++openGeneration.current;
        setError('');
        setTasks([]);
        setLive('');
        setChoices([]);
        setGitNote('');
        setPickedKind(row.kind);
        selected.current = { grant: '', instance: row.id, task: '', kind: row.kind };
        const open = appBinding('OpenCloudAgentGrant');
        const op = appBinding('RemoteControlOp');
        if (!open || !op) {
            setError(text('Cloud control is unavailable', '云端控制不可用', '雲端控制不可用'));
            return;
        }
        try {
            const grant = String(await open(row.id) || '');
            if (generation !== openGeneration.current) return;
            if (!grant) {
                setError(text('Cloud control is unavailable', '云端控制不可用', '雲端控制不可用'));
                return;
            }
            selected.current = { grant, instance: row.id, task: '', kind: row.kind };
            const raw = await op(grant, 'cloud_list_tasks', '');
            if (generation !== openGeneration.current) return;
            const body = parseJSON<{ tasks?: CloudTask[] }>(raw, {});
            setTasks(Array.isArray(body.tasks) ? body.tasks : []);
            setLive('');
        } catch (err) {
            if (generation === openGeneration.current) fail(err);
        }
    };

    const openTask = async (task: CloudTask) => {
        const op = appBinding('RemoteControlOp');
        const current = selected.current;
        if (!op || !current.grant) return;
        const generation = openGeneration.current;
        setChoices([]);
        setLive('');
        selected.current = { ...current, task: task.id };
        const session = cloudAgentSessionKey(current.instance, task.id);
        const baseline = cloudAgentRound(cloudAgentStoreKey(current.grant, session)).last;
        try {
            const watchRaw = await op(current.grant, 'cloud_watch_task', JSON.stringify({ task_id: task.id, session_key: session }));
            applyCloudAgentWatch(current.grant, session, parseJSON(watchRaw, {}), baseline);
            if (generation !== openGeneration.current || selected.current.instance !== current.instance || selected.current.task !== task.id) return;
            const raw = await op(current.grant, 'cloud_get_task', JSON.stringify({ task_id: task.id }));
            if (generation !== openGeneration.current || selected.current.instance !== current.instance || selected.current.task !== task.id) return;
            const body = parseJSON<{ messages?: string[] }>(raw, {});
            const history = Array.isArray(body.messages) ? body.messages.join('\n') : '';
            const key = cloudAgentStoreKey(current.grant, session);
            const round = cloudAgentRound(key);
            const shown = round.visible || round.body || round.displayText;
            if (shown) setLive(shown);
            else if (history) setLive(history);
            else setLive('');
            refreshLive();
        } catch (err) {
            if (generation === openGeneration.current && selected.current.instance === current.instance) fail(err);
        }
    };

    const createInstance = async () => {
        const create = appBinding('CreateCloudAgent');
        const list = appBinding('ListCloudAgents');
        if (!create || !list) return;
        try {
            const created = await create(name.trim() || text('Cloud task', '云端任务', '雲端任務'), kind) as CloudAgentRow;
            const rows = await list();
            setInstances(listedInstances(rows));
            setName('');
            if (created && created.id) onCreated?.(created);
        } catch (err) {
            fail(err);
        }
    };

    const deliver = async (message: string) => {
        const op = appBinding('RemoteControlOp');
        const current = selected.current;
        const outgoing = message.trim();
        if (!op || !current.grant || !outgoing) return;
        const generation = openGeneration.current;
        const shown = sendShownCloudAgent(current.instance, current.task, outgoing);
        if (shown) {
            setChoices([]);
            setError('');
            const ok = await shown;
            if (!ok || generation !== openGeneration.current || selected.current.instance !== current.instance) return;
            setDraft('');
            refreshLive();
            return;
        }
        setChoices([]);
        setError('');
        try {
            let taskID = current.task;
            if (!taskID) {
                const created = parseJSON<{ task_id?: string }>(
                    await op(current.grant, 'cloud_create_task', JSON.stringify({ title: Array.from(outgoing).slice(0, 48).join('') })),
                    {},
                );
                taskID = String(created.task_id || '').trim();
                if (!taskID) {
                    if (generation === openGeneration.current && selected.current.instance === current.instance) {
                        setError(text('The cloud task was not created', '云端任务没有创建', '雲端任務沒有建立'));
                    }
                    return;
                }
                const session = cloudAgentSessionKey(current.instance, taskID);
                const baseline = cloudAgentRound(cloudAgentStoreKey(current.grant, session)).last;
                if (generation === openGeneration.current && selected.current.instance === current.instance) {
                    selected.current = { ...current, task: taskID };
                    const title = Array.from(outgoing).slice(0, 48).join('');
                    setTasks((prev) => (prev.some((item) => item.id === taskID) ? prev : [...prev, { id: taskID, title }]));
                }
                const watchRaw = await op(current.grant, 'cloud_watch_task', JSON.stringify({ task_id: taskID, session_key: session }));
                applyCloudAgentWatch(current.grant, session, parseJSON(watchRaw, {}), baseline);
            }
            await op(current.grant, 'cloud_send_message', JSON.stringify({
                task_id: taskID,
                text: outgoing,
                client_message_id: `cm_${Date.now()}`,
            }));
            expireCloudAgentCards(cloudAgentStoreKey(current.grant, cloudAgentSessionKey(current.instance, taskID)));
            if (generation !== openGeneration.current || selected.current.instance !== current.instance) return;
            setDraft('');
            refreshLive();
        } catch (err) {
            if (generation === openGeneration.current && selected.current.instance === current.instance) fail(err);
        }
    };

    const connectGit = async () => {
        const connect = appBinding('ConnectCloudAgentGit');
        const current = selected.current;
        if (!connect || current.kind !== 'coding' || !current.instance) return;
        try {
            const view = await connect(current.instance, {
                service: git.service,
                origin: git.origin.trim(),
                repo: git.repo.trim(),
                author_name: git.authorName.trim(),
                author_email: git.authorEmail.trim(),
                token: git.token,
                source: git.source || undefined,
            }) as { repo?: string; token?: string };
            if (view && view.token) {
                setError(text('The token came back', '令牌被返回了', '權杖被返回了'));
                return;
            }
            setGitNote(String(view?.repo || ''));
            setGit((prev) => ({ ...prev, token: '', source: '' }));
        } catch (err) {
            fail(err);
        }
    };

    const fillGitFromAccount = async () => {
        const account = appBinding('CloudAgentGitHubAccount');
        if (!account) return;
        try {
            const view = await account() as {
                connected?: boolean; login?: string; name?: string;
                email?: string; token?: string; repo_scope?: boolean;
            };
            if (!view?.connected || !view.token) {
                setGitNote(text('Connect GitHub in Settings first', '请先在设置中登录 GitHub', '請先在設定中登入 GitHub'));
                return;
            }
            if (!view.repo_scope) {
                setGitNote(text('This token has no repo permission', '该令牌没有仓库权限', '該權杖沒有倉庫權限'));
                return;
            }
            setGit((prev) => ({
                ...prev,
                service: 'github',
                origin: 'https://github.com',
                authorName: String(view.name || view.login || ''),
                authorEmail: String(view.email || ''),
                token: String(view.token),
                source: 'account',
            }));
            setGitNote(text(`Account ${view.login} filled`, `已填入账号 ${view.login}`, `已填入帳號 ${view.login}`));
        } catch (err) {
            fail(err);
        }
    };

    const stop = async () => {
        const op = appBinding('RemoteControlOp');
        const current = selected.current;
        if (!op || !current.grant || !current.task) return;
        const generation = openGeneration.current;
        const shown = stopShownCloudAgent(current.instance, current.task);
        if (shown) {
            await shown;
            return;
        }
        try {
            await op(current.grant, 'cloud_stop_task', JSON.stringify({ task_id: current.task }));
            const session = cloudAgentSessionKey(current.instance, current.task);
            applyCloudAgentStream({
                grant_id: current.grant,
                session_key: session,
                project_path: '',
                event: 'response',
            });
            expireCloudAgentCards(cloudAgentStoreKey(current.grant, session));
            if (cloudAssistantInstanceId() !== current.instance) noteCloudTurn(current.instance, false);
            if (generation !== openGeneration.current || selected.current.instance !== current.instance) return;
            refreshLive();
        } catch (err) {
            if (generation === openGeneration.current && selected.current.instance === current.instance) fail(err);
        }
    };

    return (
        <section
            data-testid="cloud-agent-manage"
            style={{
                position: 'fixed',
                inset: '12vh 18vw auto',
                zIndex: 80,
                background: 'var(--theme-surface, #fff)',
                color: 'var(--theme-text, #172033)',
                borderRadius: 16,
                padding: 16,
                boxShadow: '0 16px 48px rgba(15, 23, 42, 0.18)',
            }}
        >
            <header style={{ display: 'flex', justifyContent: 'space-between', gap: 12 }}>
                <strong>{text('Cloud agents', '云端管理', '雲端管理')}</strong>
                <button type="button" onClick={onClose}>{text('Close', '关闭', '關閉')}</button>
            </header>
            {error ? <p role="alert">{error}</p> : null}
            <div style={{ display: 'flex', gap: 8, marginTop: 12 }}>
                <input
                    aria-label={text('Name', '名称', '名稱')}
                    value={name}
                    onChange={(event) => setName(event.target.value)}
                />
                <select aria-label={text('Kind', '类型', '類型')} value={kind} onChange={(event) => setKind(event.target.value === 'coding' ? 'coding' : 'session')}>
                    <option value="session">{text('Session', '会话', '會話')}</option>
                    <option value="coding">{text('Coding', '编程', '編程')}</option>
                </select>
                <button type="button" onClick={() => { void createInstance(); }}>{text('Create', '创建', '建立')}</button>
            </div>
            <ul>
                {instances.map((row) => (
                    <li key={row.id}>
                        <button type="button" data-testid={`cloud-agent-instance-${row.id}`} onClick={() => { void openInstance(row); }}>
                            {row.name || row.id}
                        </button>
                        <span>{row.kind === 'coding' ? text('Coding', '编程', '編程') : text('Session', '会话', '會話')}</span>
                    </li>
                ))}
            </ul>
            <ul>
                {tasks.map((task) => (
                    <li key={task.id}>
                        <button type="button" data-testid={`cloud-agent-task-${task.id}`} onClick={() => { void openTask(task); }}>
                            {task.title || task.id}
                        </button>
                    </li>
                ))}
            </ul>
            {pickedKind === 'coding' ? (
                <form
                    data-testid="cloud-agent-git"
                    onSubmit={(event) => {
                        event.preventDefault();
                        void connectGit();
                    }}
                    style={{ display: 'flex', flexWrap: 'wrap', gap: 8, marginTop: 12 }}
                >
                    <select aria-label={text('Service', '服务', '服務')} value={git.service} onChange={(event) => setGit((prev) => ({ ...prev, service: event.target.value, source: '' }))}>
                        <option value="github">GitHub</option>
                        <option value="gitlab">GitLab</option>
                    </select>
                    <input aria-label={text('Origin', '地址', '位址')} value={git.origin} onChange={(event) => setGit((prev) => ({ ...prev, origin: event.target.value }))} />
                    <input aria-label={text('Repository', '代码库', '程式庫')} value={git.repo} onChange={(event) => setGit((prev) => ({ ...prev, repo: event.target.value }))} />
                    <input aria-label={text('Author name', '作者', '作者')} value={git.authorName} onChange={(event) => setGit((prev) => ({ ...prev, authorName: event.target.value }))} />
                    <input aria-label={text('Author email', '邮箱', '郵箱')} value={git.authorEmail} onChange={(event) => setGit((prev) => ({ ...prev, authorEmail: event.target.value }))} />
                    <input aria-label={text('Token', '令牌', '權杖')} type="password" value={git.token} onChange={(event) => setGit((prev) => ({ ...prev, token: event.target.value, source: '' }))} />
                    <button type="button" onClick={() => void fillGitFromAccount()}>{text('Use GitHub account', '用 GitHub 账号', '用 GitHub 帳號')}</button>
                    <button type="submit">{text('Connect', '连接', '連接')}</button>
                    {gitNote ? <span>{gitNote}</span> : null}
                </form>
            ) : null}
            <pre data-testid="cloud-agent-live">{live}</pre>
            <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                {choices.map((choice) => (
                    <button
                        key={choice.command}
                        type="button"
                        data-testid={`cloud-agent-choice-${choice.command}`}
                        onClick={() => { void deliver(choice.command); }}
                    >
                        {choice.label}
                    </button>
                ))}
            </div>
            <div style={{ display: 'flex', gap: 8 }}>
                <input
                    aria-label={text('Message', '消息', '訊息')}
                    value={draft}
                    onChange={(event) => setDraft(event.target.value)}
                />
                <button type="button" onClick={() => { void deliver(draft); }}>{text('Send', '发送', '傳送')}</button>
                <button type="button" onClick={() => { void stop(); }}>{text('Stop', '停止', '停止')}</button>
            </div>
        </section>
    );
}

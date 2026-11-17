import { useCallback, useEffect, useState } from 'react';
import { tailText } from '../../i18n/mnTail';

type MaintRule = {
    id?: string;
    repo_url?: string;
    owner?: string;
    repo?: string;
    ghost_dir?: string;
    enabled?: boolean;
    push_branch?: string;
    commands?: Record<string, string>;
    enabled_kinds?: Record<string, boolean>;
    hook_ready?: boolean;
    auth_blocked?: boolean;
    last_error?: string;
    queue_depth?: number;
    intake_ready?: boolean;
};

type GitHubConnection = {
    connected?: boolean;
    login?: string;
    scopes?: string;
    user_code?: string;
    verification_uri?: string;
};

type MaintApp = {
    GetGitHubConnection?: (refresh: boolean) => Promise<GitHubConnection>;
    StartGitHubOAuth?: () => Promise<GitHubConnection>;
    WaitGitHubOAuth?: () => Promise<GitHubConnection>;
    CancelGitHubOAuth?: () => Promise<unknown>;
    LoadConfigForUI?: () => Promise<Record<string, string>>;
    ListGitHubRepoMaintRules?: () => Promise<MaintRule[]>;
    SaveGitHubRepoMaintRule?: (rule: MaintRule) => Promise<MaintRule>;
    DeleteGitHubRepoMaintRule?: (id: string) => Promise<void>;
    SetGitHubRepoMaintEnabled?: (id: string, enabled: boolean, replay: string) => Promise<void>;
    ReplayGitHubRepoMaint?: (id: string) => Promise<void>;
    GitHubAPIRequest?: (method: string, apiPath: string) => Promise<string>;
    SelectWorkingDir?: () => Promise<string>;
};

const EVENTS: Array<{ kind: string; en: string; hans: string; hant: string; issue: boolean }> = [
    { kind: 'issues.opened', en: 'Issue opened', hans: '议题已创建', hant: '議題已建立', issue: true },
    { kind: 'issues.closed', en: 'Issue closed', hans: '议题已关闭', hant: '議題已關閉', issue: true },
    { kind: 'issues.labeled', en: 'Issue labeled', hans: '议题已添加标签', hant: '議題已新增標籤', issue: true },
    { kind: 'issue_comment.created', en: 'New comment', hans: '新评论', hant: '新評論', issue: true },
    { kind: 'pull_request.opened', en: 'Pull request opened', hans: '拉取请求已创建', hant: '拉取請求已建立', issue: false },
    { kind: 'pull_request.merged', en: 'Pull request merged', hans: '已合并', hant: '已合併', issue: false },
    { kind: 'pull_request.review_requested', en: 'Review requested', hans: '已请求审查', hant: '已請求審查', issue: false },
    { kind: 'pull_request_review.submitted', en: 'Review submitted', hans: '已提交审查', hant: '已提交審查', issue: false },
    { kind: 'push', en: 'Push', hans: '新提交', hant: '新提交', issue: false },
    { kind: 'check_run.failure', en: 'Check failed', hans: '检查失败', hant: '檢查失敗', issue: false },
];

function maintApp(): MaintApp | null {
    const app = (window as unknown as { go?: { main?: { App?: MaintApp } } }).go?.main?.App;
    return app ?? null;
}

function text(lang: string, en: string, hans: string, hant: string): string {
    return tailText(lang, en, hans, hant);
}

function scopesHaveRepo(scopes?: string): boolean {
    return String(scopes || '').split(/[\s,]+/).some((part) => part === 'repo');
}

function blankDraft(): MaintRule {
    return {
        repo_url: '',
        ghost_dir: '',
        push_branch: '',
        commands: {},
        enabled_kinds: {},
        enabled: false,
        hook_ready: false,
        intake_ready: true,
    };
}

function issueKindsArmed(rule: MaintRule): boolean {
    const kinds = rule.enabled_kinds || {};
    return EVENTS.some((event) => event.issue && kinds[event.kind]);
}

export function GitHubRepoMaintWorkspace({ lang, onBack }: { lang: string; onBack: () => void }) {
    const [connection, setConnection] = useState<GitHubConnection>({ connected: false });
    const [deviceCode, setDeviceCode] = useState('');
    const [hubReady, setHubReady] = useState(false);
    const [rules, setRules] = useState<MaintRule[]>([]);
    const [draft, setDraft] = useState<MaintRule>(blankDraft());
    const [error, setError] = useState('');
    const [busy, setBusy] = useState(false);
    const [gate, setGate] = useState<{ rule: MaintRule; step: 'public' | 'choice' } | null>(null);

    const reload = useCallback(async () => {
        const app = maintApp();
        if (!app) return;
        if (app.GetGitHubConnection) {
            setConnection(await app.GetGitHubConnection(false));
        }
        if (app.LoadConfigForUI) {
            const cfg = await app.LoadConfigForUI();
            const url = String(cfg?.remote_hub_url || '').trim();
            const id = String(cfg?.remote_machine_id || '').trim();
            const token = String(cfg?.remote_machine_token || '').trim();
            setHubReady(url !== '' && id !== '' && token !== '');
        }
        if (app.ListGitHubRepoMaintRules) {
            const listed = await app.ListGitHubRepoMaintRules();
            setRules(Array.isArray(listed) ? listed : []);
        }
    }, []);

    useEffect(() => {
        let cancelled = false;
        void (async () => {
            try {
                await reload();
            } catch (err) {
                if (!cancelled) setError(err instanceof Error ? err.message : String(err));
            }
        })();
        return () => {
            cancelled = true;
        };
    }, [reload]);

    const connect = async () => {
        const app = maintApp();
        if (!app?.StartGitHubOAuth || !app.WaitGitHubOAuth) {
            setError(text(lang, 'GitHub login is unavailable', 'GitHub 登录不可用', 'GitHub 登入不可用'));
            return;
        }
        setBusy(true);
        setError('');
        try {
            const info = await app.StartGitHubOAuth();
            setDeviceCode(String(info?.user_code || ''));
            const status = await app.WaitGitHubOAuth();
            setConnection(status || { connected: false });
            setDeviceCode('');
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
            setDeviceCode('');
        } finally {
            setBusy(false);
        }
    };

    const pickDir = async () => {
        const app = maintApp();
        if (!app?.SelectWorkingDir) return;
        const dir = await app.SelectWorkingDir();
        if (dir) setDraft((prev) => ({ ...prev, ghost_dir: dir }));
    };

    const save = async () => {
        const kinds = draft.enabled_kinds || {};
        const commands = draft.commands || {};
        for (const event of EVENTS) {
            if (kinds[event.kind] && !String(commands[event.kind] || '').trim()) {
                setError(text(lang, 'An enabled event needs a command', '打开的事件必须填写命令', '打開的事件必須填寫命令'));
                return;
            }
        }
        const app = maintApp();
        if (!app?.SaveGitHubRepoMaintRule) return;
        setBusy(true);
        setError('');
        try {
            const saved = await app.SaveGitHubRepoMaintRule({
                id: draft.id,
                repo_url: draft.repo_url,
                ghost_dir: draft.ghost_dir,
                push_branch: draft.push_branch,
                commands,
                enabled_kinds: kinds,
            });
            setDraft(saved || blankDraft());
            await reload();
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
        } finally {
            setBusy(false);
        }
    };

    const commitEnable = async (rule: MaintRule, replay: string) => {
        const app = maintApp();
        if (!rule.id || !app?.SetGitHubRepoMaintEnabled) return;
        setBusy(true);
        setError('');
        setGate(null);
        try {
            await app.SetGitHubRepoMaintEnabled(rule.id, true, replay);
            await reload();
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
        } finally {
            setBusy(false);
        }
    };

    const requestEnable = async (rule: MaintRule) => {
        if (!connection.connected) {
            setError(text(lang, 'Connect GitHub first', '请先连接 GitHub', '請先連接 GitHub'));
            return;
        }
        if (!scopesHaveRepo(connection.scopes)) {
            setError(text(lang, 'Reconnect GitHub so the token includes repo', '请重新连接 GitHub，令牌需要包含 repo', '請重新連接 GitHub，權杖需要包含 repo'));
            return;
        }
        if (!hubReady) {
            setError(text(lang, 'Connect this desktop to Hub before enabling', '启用前要先连上 Hub', '啟用前要先連上 Hub'));
            return;
        }
        let needsPublic = false;
        if (issueKindsArmed(rule)) {
            needsPublic = true;
            const app = maintApp();
            if (app?.GitHubAPIRequest && rule.owner && rule.repo) {
                try {
                    const raw = await app.GitHubAPIRequest('GET', `/repos/${rule.owner}/${rule.repo}`);
                    const doc = JSON.parse(raw) as { private?: boolean };
                    needsPublic = doc.private !== true;
                } catch {
                    needsPublic = true;
                }
            }
        }
        if (needsPublic) {
            setGate({ rule, step: 'public' });
            return;
        }
        if (!rule.hook_ready) {
            setGate({ rule, step: 'choice' });
            return;
        }
        await commitEnable(rule, '');
    };

    const toggle = async (rule: MaintRule, next: boolean) => {
        if (!next) {
            const app = maintApp();
            if (!rule.id || !app?.SetGitHubRepoMaintEnabled) return;
            setBusy(true);
            setError('');
            try {
                await app.SetGitHubRepoMaintEnabled(rule.id, false, '');
                await reload();
            } catch (err) {
                setError(err instanceof Error ? err.message : String(err));
            } finally {
                setBusy(false);
            }
            return;
        }
        await requestEnable(rule);
    };

    const replay = async (rule: MaintRule) => {
        const app = maintApp();
        if (!rule.id || !app?.ReplayGitHubRepoMaint) return;
        setBusy(true);
        setError('');
        try {
            await app.ReplayGitHubRepoMaint(rule.id);
            await reload();
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
        } finally {
            setBusy(false);
        }
    };

    const intakeReady = rules.length === 0 ? draft.intake_ready !== false : rules.every((rule) => rule.intake_ready !== false);

    return (
        <div className="utilities-page utilities-deploy" data-testid="github-repo-maint-workspace">
            <div className="utilities-page__header utilities-deploy__header">
                <button type="button" className="utilities-deploy__back" data-testid="github-repo-maint-back" onClick={onBack}>
                    {text(lang, 'Back', '返回', '返回')}
                </button>
                <div className="utilities-deploy__hero">
                    <div className="utilities-deploy__hero-text">
                        <h2 className="utilities-page__title">{text(lang, 'GitHub repository maintenance', 'GitHub 仓库自动维护', 'GitHub 倉庫自動維護')}</h2>
                        <p className="utilities-page__subtitle">{text(lang, 'One rule maintains one repository from a local checkout', '一条规则维护一个仓库的本地检出', '一條規則維護一個倉庫的本機檢出')}</p>
                    </div>
                </div>
            </div>
            {error ? <p className="utilities-error" role="alert" data-testid="github-repo-maint-error">{error}</p> : null}
            <section className="utilities-deploy-card" data-testid="github-repo-maint-connection">
                <p>
                    {connection.connected
                        ? text(lang, `Connected as @${connection.login || ''}`, `已连接 GitHub 账号 @${connection.login || ''}`, `已連接 GitHub 帳號 @${connection.login || ''}`)
                        : text(lang, 'GitHub is not connected', '尚未连接 GitHub', '尚未連接 GitHub')}
                </p>
                {!connection.connected || !scopesHaveRepo(connection.scopes) ? (
                    <button type="button" className="utilities-btn utilities-btn--primary" data-testid="github-repo-maint-connect" disabled={busy} onClick={() => { void connect(); }}>
                        {text(lang, 'Connect GitHub', '连接 GitHub', '連接 GitHub')}
                    </button>
                ) : null}
                {deviceCode ? <p data-testid="github-repo-maint-user-code">{deviceCode}</p> : null}
                {!hubReady ? <p data-testid="github-repo-maint-hub-hint">{text(lang, 'Connect this desktop to Hub before enabling', '启用前要先连上 Hub', '啟用前要先連上 Hub')}</p> : null}
            </section>
            <ul className="utilities-deploy-records__list" data-testid="github-repo-maint-rules">
                {rules.map((rule) => (
                    <li key={rule.id} className="utilities-deploy-record" data-testid={`github-repo-maint-rule-${rule.id}`}>
                        <p>{rule.repo_url}</p>
                        <p>{rule.ghost_dir}</p>
                        {rule.last_error ? <p data-testid={`github-repo-maint-error-${rule.id}`}>{rule.last_error}</p> : null}
                        <p data-testid={`github-repo-maint-queue-${rule.id}`}>{text(lang, `Queued: ${rule.queue_depth || 0}`, `排队 ${rule.queue_depth || 0}`, `排隊 ${rule.queue_depth || 0}`)}</p>
                        <p>{rule.hook_ready ? text(lang, 'Webhook is installed', 'Webhook 已挂上', 'Webhook 已掛上') : text(lang, 'Webhook is not installed', 'Webhook 尚未挂上', 'Webhook 尚未掛上')}</p>
                        {rule.auth_blocked ? <p>{text(lang, 'GitHub token was revoked', 'GitHub 令牌已失效', 'GitHub 權杖已失效')}</p> : null}
                        {rule.intake_ready !== false ? (
                            <label>
                                <input
                                    type="checkbox"
                                    data-testid={`github-repo-maint-enable-${rule.id}`}
                                    checked={!!rule.enabled}
                                    disabled={busy}
                                    onChange={(event) => { void toggle(rule, event.target.checked); }}
                                />
                                {text(lang, 'Enabled', '总开关', '總開關')}
                            </label>
                        ) : null}
                        {rule.intake_ready !== false ? (
                            <button type="button" className="utilities-btn" data-testid={`github-repo-maint-replay-${rule.id}`} disabled={busy} onClick={() => { void replay(rule); }}>
                                {text(lang, 'Replay open items', '回放未处理的', '回放未處理的')}
                            </button>
                        ) : null}
                        <button type="button" className="utilities-link" onClick={() => setDraft({ ...blankDraft(), ...rule, commands: { ...(rule.commands || {}) }, enabled_kinds: { ...(rule.enabled_kinds || {}) } })}>
                            {text(lang, 'Edit', '编辑', '編輯')}
                        </button>
                    </li>
                ))}
            </ul>
            <form className="utilities-deploy-card" data-testid="github-repo-maint-editor" onSubmit={(event) => { event.preventDefault(); void save(); }}>
                <label className="utilities-deploy-field">
                    <span>{text(lang, 'Repository URL', '仓库地址', '倉庫網址')}</span>
                    <input className="utilities-input" data-testid="github-repo-maint-repo-url" value={draft.repo_url || ''} onChange={(event) => setDraft({ ...draft, repo_url: event.target.value })} />
                </label>
                <label className="utilities-deploy-field">
                    <span>{text(lang, 'Local checkout', '本地目录', '本機目錄')}</span>
                    <input className="utilities-input" data-testid="github-repo-maint-ghost-dir" value={draft.ghost_dir || ''} onChange={(event) => setDraft({ ...draft, ghost_dir: event.target.value })} />
                </label>
                <button type="button" className="utilities-btn" data-testid="github-repo-maint-pick-dir" onClick={() => { void pickDir(); }}>{text(lang, 'Choose folder', '选择目录', '選擇目錄')}</button>
                {EVENTS.map((event) => (
                    <div key={event.kind}>
                        <label>
                            <input
                                type="checkbox"
                                data-testid={`github-repo-maint-kind-${event.kind}`}
                                checked={!!draft.enabled_kinds?.[event.kind]}
                                onChange={(change) => setDraft({
                                    ...draft,
                                    enabled_kinds: { ...(draft.enabled_kinds || {}), [event.kind]: change.target.checked },
                                })}
                            />
                            {text(lang, event.en, event.hans, event.hant)}
                        </label>
                        <textarea
                            className="utilities-input"
                            data-testid={`github-repo-maint-command-${event.kind}`}
                            value={draft.commands?.[event.kind] || ''}
                            onChange={(change) => setDraft({
                                ...draft,
                                commands: { ...(draft.commands || {}), [event.kind]: change.target.value },
                            })}
                        />
                        {event.kind === 'push' ? (
                            <input
                                className="utilities-input"
                                data-testid="github-repo-maint-push-branch"
                                placeholder={text(lang, 'Empty uses the default branch', '留空则使用默认分支', '留空則使用預設分支')}
                                value={draft.push_branch || ''}
                                onChange={(change) => setDraft({ ...draft, push_branch: change.target.value })}
                            />
                        ) : null}
                    </div>
                ))}
                <button type="submit" className="utilities-btn utilities-btn--primary" data-testid="github-repo-maint-save" disabled={busy}>
                    {text(lang, 'Save', '保存', '儲存')}
                </button>
            </form>
            {gate?.step === 'public' ? (
                <div className="utilities-deploy-card" data-testid="github-repo-maint-public-dialog" role="dialog">
                    <p>{text(lang, 'This is a public repository. Anyone can open an issue or comment, and that text starts a coding task.', '这是公开仓库。任何人都能开议题或写评论，那些文字会自动开工。', '這是公開倉庫。任何人都能開議題或寫評論，那些文字會自動開工。')}</p>
                    <button type="button" className="utilities-btn utilities-btn--primary" data-testid="github-repo-maint-public-confirm" onClick={() => {
                        if (!gate.rule.hook_ready) setGate({ rule: gate.rule, step: 'choice' });
                        else void commitEnable(gate.rule, '');
                    }}>{text(lang, 'Continue', '继续', '繼續')}</button>
                    <button type="button" className="utilities-btn" data-testid="github-repo-maint-public-cancel" onClick={() => setGate(null)}>{text(lang, 'Cancel', '取消', '取消')}</button>
                </div>
            ) : null}
            {gate?.step === 'choice' ? (
                <div className="utilities-deploy-card" data-testid="github-repo-maint-choice-dialog" role="dialog">
                    <p>{text(lang, 'What should the first enable handle?', '第一次启用要处理哪些事件？', '第一次啟用要處理哪些事件？')}</p>
                    <button type="button" className="utilities-btn utilities-btn--primary" data-testid="github-repo-maint-choice-new" onClick={() => { void commitEnable(gate.rule, 'new_only'); }}>
                        {text(lang, 'Only new events', '只处理之后的新事件', '只處理之後的新事件')}
                    </button>
                    <button type="button" className="utilities-btn" data-testid="github-repo-maint-choice-backfill" onClick={() => { void commitEnable(gate.rule, 'backfill'); }}>
                        {text(lang, 'Replay open items', '回放未处理的', '回放未處理的')}
                    </button>
                </div>
            ) : null}
            {intakeReady ? null : <p data-testid="github-repo-maint-intake-off">{text(lang, 'Intake is not ready', '接收尚未就绪', '接收尚未就緒')}</p>}
        </div>
    );
}

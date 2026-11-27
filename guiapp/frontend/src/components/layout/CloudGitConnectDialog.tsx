import { useEffect, useMemo, useRef, useState } from 'react';
import { localizeText } from '../../i18n';
import { refreshCloudGit } from './cloudAgentAssistant';
import { GitRepoCreatePanel, GitPlatformCredentialView, GitRepoCreated } from './GitRepoCreatePanel';
import { noRepoScopeNote } from '../../utils/githubScopes';
import { openSettingsTab } from '../../utils/settingsNavigation';
import './CloudGitConnectDialog.css';

type Binding = (...args: unknown[]) => Promise<unknown>;

function appBinding(name: string): Binding | null {
    const bindings = (window as unknown as { go?: { main?: { App?: Record<string, unknown> } } }).go?.main?.App;
    const fn = bindings?.[name];
    return typeof fn === 'function' ? fn as Binding : null;
}

type AccountView = {
    connected?: boolean;
    login?: string;
    name?: string;
    email?: string;
    origin?: string;
    repo_scope?: boolean;
    github_app_token?: boolean;
};

type RepoView = {
    name: string;
    description?: string;
    web_url?: string;
    default_branch?: string;
    updated_at?: string;
    private?: boolean;
};

type AccountState = 'loading' | 'ready' | 'missing' | 'error';

const defaultOrigin = (service: string) => service === 'gitlab' ? 'https://gitlab.com' : 'https://github.com';

function repoViews(raw: unknown): RepoView[] {
    if (!Array.isArray(raw)) return [];
    const out: RepoView[] = [];
    for (const item of raw) {
        if (!item || typeof item !== 'object') continue;
        const row = item as Record<string, unknown>;
        const name = typeof row.name === 'string' ? row.name.trim() : '';
        if (!name) continue;
        out.push({
            name,
            description: typeof row.description === 'string' ? row.description : '',
            web_url: typeof row.web_url === 'string' ? row.web_url : '',
            default_branch: typeof row.default_branch === 'string' ? row.default_branch : '',
            updated_at: typeof row.updated_at === 'string' ? row.updated_at : '',
            private: row.private === true,
        });
    }
    return out;
}

/**
 * Account-first cloud repository connect. The webview chooses only the
 * repository; the backend injects the settings-account token when it forwards
 * source="account" to maclawsrv. Manual token/origin remains available as the
 * escape hatch, but it is no longer the primary interface.
 */
export function CloudGitConnectDialog({ lang, instanceId, onClose, onConnected }: { lang: string; instanceId: string; onClose: () => void; onConnected?: (repo: string) => void }) {
    const [git, setGit] = useState({
        service: 'github',
        origin: 'https://github.com',
        repo: '',
        authorName: '',
        authorEmail: '',
        token: '',
        source: 'account',
    });
    const [account, setAccount] = useState<AccountView | null>(null);
    const [accountState, setAccountState] = useState<AccountState>('loading');
    const [accountMessage, setAccountMessage] = useState('');
    const [repos, setRepos] = useState<RepoView[]>([]);
    const [repoQuery, setRepoQuery] = useState('');
    const [repoListOpen, setRepoListOpen] = useState(true);
    const [repoSelectionLocked, setRepoSelectionLocked] = useState(false);
    const [reposBusy, setReposBusy] = useState(false);
    const [repoReloadNonce, setRepoReloadNonce] = useState(0);
    const [repoLoadError, setRepoLoadError] = useState('');
    const [error, setError] = useState('');
    const [busy, setBusy] = useState(false);
    const [advancedOpen, setAdvancedOpen] = useState(false);
    const [bound, setBound] = useState<{ service: string; repo: string } | null>(null);
    // Create-new mode: the repository is created on the provider with a
    // picked credential and then bound through the normal connect path.
    // createCreds is the permission probe — creation is offered only when at
    // least one usable credential exists (account with repo scope, or a
    // provider-tagged virtual-repository credential).
    const [repoMode, setRepoMode] = useState<'pick' | 'create'>('pick');
    const [createCreds, setCreateCreds] = useState<GitPlatformCredentialView[]>([]);
    const [createCredsBusy, setCreateCredsBusy] = useState(true);
    const [createCredsError, setCreateCredsError] = useState('');
    // Create+bind failure memory: the repository exists on the provider but
    // the bind failed, so the pick list prefills it and the connect keeps
    // the credential reference (source=credential) it was created with.
    // Cleared as soon as the user edits the repo path or switches service.
    const [retryCredential, setRetryCredential] = useState<{ repo: string; credentialId: string; origin: string } | null>(null);
    const dialogRef = useRef<HTMLDivElement | null>(null);
    const loadGeneration = useRef(0);
    const repoCache = useRef<Partial<Record<string, RepoView[]>>>({});
    const autoAuthor = useRef({ name: '', email: '' });

    const text = (en: string, zh: string, hant?: string) => localizeText(lang, en, zh, hant);
    const isGitLab = git.service === 'gitlab';
    const provider = isGitLab ? 'GitLab' : 'GitHub';

    // Esc closes like the other panel dialogs; opening focuses the repository
    // field. onClose is an inline lambda, so it goes through a ref and the
    // effect runs once — otherwise every parent re-render would steal focus.
    const onCloseRef = useRef(onClose);
    onCloseRef.current = onClose;
    // Live service mirror for one-shot async paths (the mount probe) whose
    // first-render closure would otherwise see a stale git.service.
    const serviceRef = useRef(git.service);
    serviceRef.current = git.service;
    const repoRef = useRef(git.repo);
    repoRef.current = git.repo;
    useEffect(() => {
        const invoker = document.activeElement as HTMLElement | null;
        const onKey = (event: KeyboardEvent) => {
            if (event.key === 'Escape') onCloseRef.current();
        };
        const onPointerDown = (event: MouseEvent) => {
            if (!(event.target as HTMLElement | null)?.closest('.cgc-repo-section')) setRepoListOpen(false);
        };
        window.addEventListener('keydown', onKey);
        document.addEventListener('mousedown', onPointerDown);
        dialogRef.current?.querySelector<HTMLInputElement>('.cgc-repo-input')?.focus();
        return () => {
            window.removeEventListener('keydown', onKey);
            document.removeEventListener('mousedown', onPointerDown);
            invoker?.focus?.();
        };
    }, []);

    // Load the already-connected settings account and its repositories. The
    // local account read never blocks the dialog; only repository discovery is
    // a network operation. A service switch cancels the previous generation.
    useEffect(() => {
        const service = git.service;
        const gitlab = service === 'gitlab';
        const generation = ++loadGeneration.current;
        let cancelled = false;
        const current = () => !cancelled && generation === loadGeneration.current;
        const accountBinding = appBinding(gitlab ? 'CloudAgentGitLabAccount' : 'CloudAgentGitHubAccount');
        const reposBinding = appBinding('CloudAgentGitRepositories');

        setAccount(null);
        setAccountState('loading');
        setAccountMessage('');
        setRepoLoadError('');
        setRepoMode('pick');
        setRetryCredential(null);
        const cachedRepos = repoCache.current[service];
        setRepos(cachedRepos || []);
        setReposBusy(false);
        setCreateCredsError('');
        setCreateCredsBusy(true);

        // The create-repo permission probe runs regardless of the settings
        // account's state: provider-tagged virtual-repository credentials
        // keep creation available even when the account itself cannot list
        // repositories.
        void (async () => {
            const credsBinding = appBinding('GitPlatformCreateCredentials');
            if (!credsBinding) {
                if (current()) setCreateCredsBusy(false);
                return;
            }
            try {
                const rows = await credsBinding(service) as GitPlatformCredentialView[];
                if (current()) setCreateCreds(Array.isArray(rows) ? rows.filter((row) => row && typeof row.kind === 'string') : []);
            } catch (err) {
                // A failed permission probe must not silently swallow the
                // create affordance: the toggle disappears either way, but
                // the user deserves to know it was a local read failure, not
                // "you have no credentials".
                if (current()) {
                    setCreateCreds([]);
                    setCreateCredsError(err instanceof Error ? err.message : String(err));
                }
            } finally {
                if (current()) setCreateCredsBusy(false);
            }
        })();

        void (async () => {
            let view: AccountView | null = null;
            let accountError = '';
            if (accountBinding) {
                try {
                    view = await accountBinding() as AccountView;
                } catch (err) {
                    accountError = err instanceof Error ? err.message : String(err);
                }
            }
            if (!current()) return;

            if (!view?.connected) {
                setAccountState('missing');
                setAccountMessage(accountError || (gitlab
                    ? text('Connect GitLab in Settings → GitHub & GitLab first, or use advanced manual connection.', '请先在 设置 → GitHub & GitLab 中连接 GitLab；也可以展开高级选项手动连接。', '請先在 設定 → GitHub & GitLab 中連接 GitLab；也可以展開進階選項手動連接。')
                    : text('Connect GitHub in Settings → GitHub & GitLab first, or use advanced manual connection.', '请先在 设置 → GitHub & GitLab 中登录 GitHub；也可以展开高级选项手动连接。', '請先在 設定 → GitHub & GitLab 中登入 GitHub；也可以展開進階選項手動連接。')));
                setGit((prev) => prev.service === service && prev.source === 'account' ? { ...prev, source: '' } : prev);
                return;
            }

            setAccount(view);
            if (!view.repo_scope) {
                setAccountState('error');
                setAccountMessage(noRepoScopeNote(lang, gitlab, view.github_app_token));
                setGit((prev) => prev.service === service && prev.source === 'account' ? { ...prev, source: '' } : prev);
                return;
            }

            setAccountState('ready');
            const accountAuthorName = String(view.name || view.login || '');
            const accountAuthorEmail = String(view.email || '');
            if (!accountAuthorName || !accountAuthorEmail) setAdvancedOpen(true);
            setGit((prev) => {
                if (prev.service !== service) return prev;
                const useAccount = prev.source === 'account';
                return {
                    ...prev,
                    origin: useAccount ? (gitlab ? String(view.origin || 'https://gitlab.com') : 'https://github.com') : prev.origin,
                    authorName: !prev.authorName || prev.authorName === autoAuthor.current.name ? accountAuthorName : prev.authorName,
                    authorEmail: !prev.authorEmail || prev.authorEmail === autoAuthor.current.email ? accountAuthorEmail : prev.authorEmail,
                    token: useAccount ? '' : prev.token,
                    source: useAccount ? 'account' : '',
                };
            });
            autoAuthor.current = { name: accountAuthorName, email: accountAuthorEmail };

            if (!reposBinding || cachedRepos) return;
            setReposBusy(true);
            try {
                const rows = repoViews(await reposBinding(service));
                if (!current()) return;
                repoCache.current[service] = rows;
                setRepos(rows);
            } catch (err) {
                if (!current()) return;
                setRepoLoadError(err instanceof Error ? err.message : String(err));
            } finally {
                if (current()) setReposBusy(false);
            }
        })();

        return () => {
            cancelled = true;
        };
    }, [git.service, repoReloadNonce]);

    const filteredRepos = useMemo(() => {
        if (repoSelectionLocked) return repos;
        const query = repoQuery.trim().toLowerCase();
        if (!query) return repos;
        return repos.filter((repo) => (
            repo.name.toLowerCase().includes(query)
            || (repo.description || '').toLowerCase().includes(query)
        ));
    }, [repos, repoQuery, repoSelectionLocked]);

    const fail = (err: unknown) => {
        setError(err instanceof Error ? err.message : String(err));
    };

    const switchService = (service: string) => {
        if (service === git.service) return;
        setError('');
        setRetryCredential(null);
        setRepoQuery('');
        setRepoListOpen(true);
        setRepoSelectionLocked(false);
        setGit((prev) => ({
            ...prev,
            service,
            origin: defaultOrigin(service),
            repo: '',
            token: '',
            source: 'account',
        }));
    };

    // Deep-link into Settings → GitHub & GitLab and let the user sign in
    // there; the dialog closes so reopening starts a fresh account read.
    const goToSettings = () => {
        openSettingsTab('github');
        onCloseRef.current();
    };

    const updatedLabel = (at: string): string => {
        const time = Date.parse(at);
        if (!Number.isFinite(time)) return '';
        const seconds = Math.max(0, Math.round((Date.now() - time) / 1000));
        if (seconds < 60) return text('Updated just now', '刚刚更新', '剛剛更新');
        const minutes = Math.round(seconds / 60);
        if (minutes < 60) return text(`Updated ${minutes} minutes ago`, `${minutes} 分钟前更新`, `${minutes} 分鐘前更新`);
        const hours = Math.round(minutes / 60);
        if (hours < 24) return text(`Updated ${hours} hours ago`, `${hours} 小时前更新`, `${hours} 小時前更新`);
        const days = Math.round(hours / 24);
        if (days < 30) return text(`Updated ${days} days ago`, `${days} 天前更新`, `${days} 天前更新`);
        const months = Math.round(days / 30);
        if (months < 12) return text(`Updated ${months} months ago`, `${months} 个月前更新`, `${months} 個月前更新`);
        const years = Math.round(months / 12);
        return text(`Updated ${years} years ago`, `${years} 年前更新`, `${years} 年前更新`);
    };

    // One-time probe: the dialog defaults to GitHub, but when only GitLab is
    // signed in, opening on GitHub would first show "no connected account".
    // Switch to the usable service before the user reads anything; both reads
    // are local credential-store lookups, and the service effect's generation
    // guard cancels the superseded GitHub load.
    const initialServiceProbed = useRef(false);
    useEffect(() => {
        if (initialServiceProbed.current) return;
        initialServiceProbed.current = true;
        const usable = (view: AccountView | null) => !!view?.connected && view.repo_scope !== false;
        void (async () => {
            const readAccount = async (gitlab: boolean): Promise<AccountView | null> => {
                const binding = appBinding(gitlab ? 'CloudAgentGitLabAccount' : 'CloudAgentGitHubAccount');
                if (!binding) return null;
                try {
                    return await binding() as AccountView;
                } catch {
                    return null;
                }
            };
            const [github, gitlab] = await Promise.all([readAccount(false), readAccount(true)]);
            // Only switch while the user is still on the untouched default —
            // a manual service switch wins, and so does any typed repository
            // path: switching would wipe their input along with the service.
            if (serviceRef.current === 'github' && !repoRef.current.trim() && usable(gitlab) && !usable(github)) switchService('gitlab');
        })();
    }, []);

    // Surface the instance's existing repository binding instead of letting a
    // second connect die on the server's "already has a repository" error.
    // Reconnecting the SAME repository is the supported token-refresh path
    // (srv re-sets identity without re-cloning), so prefilling it is useful,
    // not just informative. A binding on the OTHER service is shown as a note
    // only — repository namespaces do not transfer across GitHub/GitLab.
    useEffect(() => {
        if (!instanceId) return;
        let cancelled = false;
        void (async () => {
            const get = appBinding('GetCloudAgentGit');
            if (!get) return;
            try {
                const view = await get(instanceId) as { connected?: boolean; service?: string; repo?: string };
                if (cancelled || !view?.connected) return;
                const repo = String(view.repo || '').trim();
                const service = String(view.service || '').trim().toLowerCase();
                if (!repo) return;
                setBound({ service, repo });
                if (serviceRef.current !== service || repoRef.current.trim()) return;
                setRepoQuery(repo);
                setRepoSelectionLocked(true);
                setGit((prev) => (prev.repo.trim() ? prev : { ...prev, repo }));
            } catch {
                // An unreachable srv keeps the plain connect flow unchanged.
            }
        })();
        return () => {
            cancelled = true;
        };
    }, [instanceId]);

    const updateRepo = (value: string) => {
        // A connect failure refers to the previous input; editing starts fresh.
        setError('');
        setRetryCredential(null);
        setRepoQuery(value);
        setRepoListOpen(true);
        setRepoSelectionLocked(false);
        setGit((prev) => ({ ...prev, repo: value }));
    };

    const selectRepo = (repo: RepoView) => {
        setError('');
        setRetryCredential(null);
        setRepoQuery(repo.name);
        setRepoListOpen(false);
        setRepoSelectionLocked(true);
        setGit((prev) => ({ ...prev, repo: repo.name }));
    };

    const accountReady = git.source === 'account' && accountState === 'ready';
    const authorReady = !!git.authorName.trim() && !!git.authorEmail.trim();
    const manualReady = !!git.token.trim() && authorReady;
    const retryReady = !!retryCredential && retryCredential.repo === git.repo.trim() && retryCredential.credentialId !== '';
    // instanceId gates here (not inside submit) so a missing id disables the
    // button instead of clicking through to a silent no-op. Pick mode only —
    // the create panel has its own submit affordances.
    const canSubmit = repoMode === 'pick' && !busy && !!instanceId && !!git.repo.trim() && ((accountReady && authorReady) || manualReady || retryReady);
    const selectedRepo = repos.find((repo) => repo.name === git.repo.trim());

    const submit = async () => {
        const connect = appBinding('ConnectCloudAgentGit');
        if (!connect || !instanceId || !canSubmit) return;
        // canSubmit is pick-mode-only, so a form submit while the create panel
        // is up — e.g. Enter with the panel's name not yet ready — can never
        // connect the repo prefilled by an earlier failed create.
        // A prefilled repository created moments ago reconnects with the
        // credential reference it was created with — the settings account
        // may not have access (or may not be connected at all). A retry on
        // the settings-account path (empty credential id) falls through to
        // the normal account connect.
        const retry = retryCredential && retryCredential.repo === git.repo.trim() && retryCredential.credentialId !== '' ? retryCredential : null;
        setBusy(true);
        setError('');
        try {
            const view = await connect(instanceId, {
                service: git.service,
                origin: (retry?.origin || git.origin).trim(),
                repo: git.repo.trim(),
                author_name: git.authorName.trim(),
                author_email: git.authorEmail.trim(),
                token: git.token,
                source: retry ? 'credential' : (git.source || undefined),
                credential_id: retry?.credentialId || undefined,
            }) as { repo?: string; token?: string };
            if (view && view.token) {
                setError(text('The token came back', '令牌被返回了', '權杖被返回了'));
                return;
            }
            onConnected?.(String(view?.repo || git.repo.trim()).trim());
            // refreshCloudGit is internally try/catch'd and generation-guarded;
            // don't hold the dialog open on a slow Hub round-trip.
            void refreshCloudGit(instanceId);
            onClose();
        } catch (err) {
            fail(err);
        } finally {
            setBusy(false);
        }
    };

    // submitCreate binds a freshly created repository. The create step already
    // ran in the panel (it owns credential/namespace/name); the backend
    // injects the picked credential's token (source="credential") or the
    // settings account's (source="account"). A bind failure leaves the
    // repository created on the provider: the dialog switches to the pick
    // list with the repository prefilled and the credential reference kept
    // (retryCredential) so 连接 retries the bind without recreating anything.
    const submitCreate = async (created: GitRepoCreated) => {
        const connect = appBinding('ConnectCloudAgentGit');
        if (!connect || !instanceId) return;
        setBusy(true);
        setError('');
        try {
            const view = await connect(instanceId, {
                service: created.service,
                origin: created.origin || git.origin.trim(),
                repo: created.fullName,
                author_name: git.authorName.trim(),
                author_email: git.authorEmail.trim(),
                token: git.token,
                source: created.credentialId ? 'credential' : (git.source || undefined),
                credential_id: created.credentialId || undefined,
            }) as { repo?: string; token?: string };
            if (view && view.token) {
                setError(text('The token came back', '令牌被返回了', '權杖被返回了'));
                return;
            }
            onConnected?.(String(view?.repo || created.fullName).trim());
            void refreshCloudGit(instanceId);
            onClose();
        } catch (err) {
            fail(err);
            setRepoMode('pick');
            setRetryCredential({ repo: created.fullName, credentialId: created.credentialId, origin: created.origin });
            setGit((prev) => ({ ...prev, repo: created.fullName }));
            setRepoQuery(created.fullName);
            setRepoSelectionLocked(true);
        } finally {
            setBusy(false);
        }
    };

    const manualField = (label: string, key: 'origin' | 'authorName' | 'authorEmail' | 'token', type = 'text', placeholder = '') => (
        <label className="cgc-manual-field">
            <span>{label}</span>
            <input
                aria-label={label}
                type={type}
                value={git[key]}
                placeholder={placeholder}
                disabled={busy}
                onChange={(event) => setGit((prev) => ({
                    ...prev,
                    [key]: event.target.value,
                    ...(key === 'origin' ? { source: '' } : {}),
                    ...(key === 'token' ? { source: event.target.value ? '' : (accountState === 'ready' ? 'account' : '') } : {}),
                }))}
            />
        </label>
    );

    return (
        <div
            role="dialog"
            aria-modal="true"
            data-testid="cloud-git-connect"
            className="cgc-overlay"
            onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}
        >
            <div ref={dialogRef} className="cgc-dialog">
                <header className="cgc-header">
                    <div className="cgc-title-block">
                        <h2>{text('Connect repository', '连接代码库', '連接代碼庫')}</h2>
                        <p>{text('Choose a repository. The connected account is used automatically.', '选择仓库即可连接，系统将自动使用已登录账号。', '選擇倉庫即可連接，系統將自動使用已登入帳號。')}</p>
                    </div>
                    <button type="button" className="cgc-close" aria-label={text('Close', '关闭', '關閉')} onClick={onClose}>×</button>
                </header>

                <form
                    className="cgc-body"
                    onSubmit={(event) => {
                        event.preventDefault();
                        void submit();
                    }}
                >
                    <section className="cgc-section cgc-provider-section" aria-label={text('Git service', '代码平台', '程式碼平台')}>
                        <span className="cgc-label">{text('Service', '代码平台', '程式碼平台')}</span>
                        <div className="cgc-segmented" role="group" aria-label={text('Git service', '代码平台', '程式碼平台')}>
                            <button type="button" aria-pressed={!isGitLab} disabled={busy} onClick={() => switchService('github')}>GitHub</button>
                            <button type="button" aria-pressed={isGitLab} disabled={busy} onClick={() => switchService('gitlab')}>GitLab</button>
                        </div>
                    </section>

                    <section className={`cgc-account cgc-account--${accountState}`} data-testid="cloud-git-account-state">
                        {accountState === 'loading' ? (
                            <>
                                <span className="cgc-account-avatar cgc-account-avatar--loading" aria-hidden="true" />
                                <span className="cgc-account-copy">
                                    <strong>{text('Checking account…', '正在读取账号…', '正在讀取帳號…')}</strong>
                                    <span>{text('Using the connection from Settings', '正在使用设置中的连接', '正在使用設定中的連接')}</span>
                                </span>
                            </>
                        ) : accountState === 'ready' ? (
                            <>
                                <span className="cgc-account-avatar" aria-hidden="true">{(account?.login || provider).slice(0, 1).toUpperCase()}</span>
                                <span className="cgc-account-copy">
                                    <strong>{account?.login ? `@${account.login}` : provider}</strong>
                                    <span>{text('Connected in Settings · authorization will be reused', '已在设置中连接 · 将复用该账号授权', '已在設定中連接 · 將復用該帳號授權')}</span>
                                </span>
                                <span className="cgc-account-badge">{text('Connected', '已连接', '已連接')}</span>
                            </>
                        ) : (
                            <>
                                <span className="cgc-account-avatar cgc-account-avatar--empty" aria-hidden="true">!</span>
                                <span className="cgc-account-copy">
                                    <strong>{accountState === 'error' ? text('Account permission incomplete', '账号权限不完整', '帳號權限不完整') : text('No connected account', '未连接账号', '未連接帳號')}</strong>
                                    <span>{accountMessage}</span>
                                </span>
                                <button type="button" className="cgc-account-action" onClick={goToSettings}>
                                    {accountState === 'error'
                                        ? text('Reconnect at Settings', '去设置重新登录', '去設定重新登入')
                                        : text('Sign in at Settings', '去设置登录', '去設定登入')}
                                </button>
                            </>
                        )}
                    </section>

                    {error ? <p role="alert" className="cgc-alert cgc-alert--error">{error}</p> : null}

                    <section className="cgc-section cgc-repo-section">
                        <div className="cgc-repo-heading">
                            <label className="cgc-label" htmlFor="cloud-git-repo-input">{repoMode === 'create' ? text('New repository', '新建仓库', '新建倉庫') : text('Repository', '选择仓库', '選擇倉庫')}</label>
                            {reposBusy && repoMode === 'pick' ? <span className="cgc-inline-status">{text('Loading repositories…', '正在加载仓库…', '正在載入倉庫…')}</span> : null}
                            {!createCredsBusy && createCreds.length > 0 ? (
                                <div className="cgc-mode-toggle" role="group" aria-label={text('Pick or create', '选择或新建', '選擇或新建')}>
                                    <button type="button" aria-pressed={repoMode === 'pick'} disabled={busy} onClick={() => setRepoMode('pick')}>
                                        {text('Pick existing', '选择仓库', '選擇倉庫')}
                                    </button>
                                    <button type="button" aria-pressed={repoMode === 'create'} disabled={busy} onClick={() => setRepoMode('create')} data-testid="cloud-git-create-toggle">
                                        {text('Create new', '新建仓库', '新建倉庫')}
                                    </button>
                                </div>
                            ) : null}
                        </div>
                        {createCredsError ? (
                            <p className="cgc-empty-note" data-testid="cloud-git-create-error" role="status">
                                {text('Repository creation is unavailable: reading the stored credentials failed.', '新建仓库暂不可用：读取已存凭据失败。', '新建倉庫暫不可用：讀取已存憑據失敗。')}
                                <small>{createCredsError}</small>
                            </p>
                        ) : null}
                        {repoMode === 'create' ? (
                            <GitRepoCreatePanel
                                lang={lang}
                                service={git.service}
                                submitLabel={text('Create and connect', '创建并连接', '建立並連接')}
                                busy={busy}
                                onCreated={(created) => void submitCreate(created)}
                                testIdPrefix="cloud-git-create"
                            />
                        ) : (
                            <>
                                <input
                                    id="cloud-git-repo-input"
                                    className="cgc-repo-input"
                                    aria-label={text('Repository', '仓库', '倉庫')}
                                    value={repoQuery}
                                    disabled={busy}
                                    placeholder={isGitLab ? 'group/project' : 'owner/repo'}
                                    autoComplete="off"
                                    spellCheck={false}
                                    onFocus={() => setRepoListOpen(true)}
                                    onClick={() => setRepoListOpen(true)}
                                    onChange={(event) => updateRepo(event.target.value)}
                                />
                                {selectedRepo ? (
                                    <div className="cgc-selected-meta">
                                        <span>{selectedRepo.private ? text('Private', '私有', '私有') : text('Public', '公开', '公開')}</span>
                                        {selectedRepo.default_branch ? <span>{selectedRepo.default_branch}</span> : null}
                                    </div>
                                ) : null}
                                {repoLoadError ? (
                                    <div className="cgc-alert cgc-alert--warning">
                                        <strong>{text('Repository list unavailable', '无法自动读取仓库列表', '無法自動讀取倉庫列表')}</strong>
                                        <span>{text('You can still type the repository path and connect.', '仍然可以手动输入仓库路径后连接。', '仍然可以手動輸入倉庫路徑後連接。')}</span>
                                        <small>{repoLoadError}</small>
                                        <button
                                            type="button"
                                            className="cgc-retry"
                                            onClick={() => {
                                                delete repoCache.current[git.service];
                                                setRepoReloadNonce((nonce) => nonce + 1);
                                            }}
                                        >
                                            {text('Retry', '重试', '重試')}
                                        </button>
                                    </div>
                                ) : null}
                                {repoListOpen && accountState === 'ready' && repos.length > 0 ? (
                                    <div className="cgc-repo-list" role="listbox" aria-label={text('Available repositories', '可用仓库', '可用倉庫')}>
                                        {filteredRepos.map((repo) => {
                                            const updated = updatedLabel(repo.updated_at || '');
                                            return (
                                                <button
                                                    type="button"
                                                    role="option"
                                                    aria-selected={repo.name === git.repo.trim()}
                                                    key={repo.name}
                                                    className="cgc-repo-option"
                                                    onMouseDown={(event) => event.preventDefault()}
                                                    onClick={() => selectRepo(repo)}
                                                >
                                                    <span className="cgc-repo-option-main">
                                                        <strong>{repo.name}</strong>
                                                        <em>{repo.private ? text('Private', '私有', '私有') : text('Public', '公开', '公開')}</em>
                                                    </span>
                                                    {repo.description ? <span className="cgc-repo-description">{repo.description}</span> : null}
                                                    <span className="cgc-repo-meta">
                                                        {repo.default_branch ? <span>{repo.default_branch}</span> : null}
                                                        {updated ? <span>{updated}</span> : null}
                                                    </span>
                                                </button>
                                            );
                                        })}
                                    </div>
                                ) : null}
                                {repos.length >= 100 ? (
                                    <p className="cgc-empty-note">{text('Only the 100 most recently updated repositories are listed; type the full path if yours is not shown.', '列表最多读取最近更新的 100 个仓库；未显示时可直接输入完整路径。', '列表最多讀取最近更新的 100 個倉庫；未顯示時可直接輸入完整路徑。')}</p>
                                ) : null}
                                {!reposBusy && !repoLoadError && accountState === 'ready' && repos.length === 0 ? (
                                    <p className="cgc-empty-note">{text('No repositories were returned. You can type the path manually.', '没有读取到仓库，可以手动输入路径。', '沒有讀取到倉庫，可以手動輸入路徑。')}</p>
                                ) : null}
                            </>
                        )}
                        {bound ? (
                            <p className="cgc-bound-note" data-testid="cloud-git-bound-note">{text(
                                `This instance is already bound to ${bound.repo}. Reconnecting the same repository refreshes its authorization.`,
                                `该实例已绑定 ${bound.repo}；重新连接同一仓库将刷新授权。`,
                                `該實例已綁定 ${bound.repo}；重新連接同一倉庫將刷新授權。`)}</p>
                        ) : null}
                    </section>

                    {repoMode === 'pick' ? (
                        <section className="cgc-advanced">
                            <button type="button" className="cgc-advanced-toggle" aria-expanded={advancedOpen} onClick={() => setAdvancedOpen((open) => !open)}>
                                {advancedOpen
                                    ? text('Hide manual connection options', '收起手动连接选项', '收起手動連接選項')
                                    : text('Manual token or server settings', '手动令牌或服务器设置', '手動權杖或伺服器設定')}
                            </button>
                            {advancedOpen ? (
                                <div className="cgc-advanced-panel">
                                    {manualField(text('Author name', '提交作者', '提交作者'), 'authorName')}
                                    {manualField(text('Author email', '提交邮箱', '提交郵箱'), 'authorEmail')}
                                    {manualField(text('Origin', '服务地址', '服務位址'), 'origin', 'text', defaultOrigin(git.service))}
                                    {manualField(text('Token', '访问令牌', '存取權杖'), 'token', 'password', accountReady ? text('Leave empty to use the connected account', '留空则使用已连接账号', '留空則使用已連接帳號') : '')}
                                    <p>{text('Manual tokens are used only for this connection and are not shown again.', '手动令牌仅用于本次连接，不会再次显示。', '手動權杖僅用於本次連接，不會再次顯示。')}</p>
                                </div>
                            ) : null}
                        </section>
                    ) : null}

                    <footer className="cgc-footer">
                        <span>{text('After connecting, the agent can commit and push with git_commit / git_push.', '连接后，agent 可使用 git_commit / git_push 提交和推送。', '連接後，agent 可使用 git_commit / git_push 提交和推送。')}</span>
                        <div className="cgc-actions">
                            <button type="button" className="cgc-secondary" disabled={busy} onClick={onClose}>{text('Cancel', '取消', '取消')}</button>
                            {repoMode === 'pick' ? (
                                <button type="submit" className="cgc-primary" disabled={!canSubmit}>
                                    {busy ? text('Connecting…', '连接中…', '連接中…') : text('Connect', '连接', '連接')}
                                </button>
                            ) : null}
                        </div>
                    </footer>
                </form>
            </div>
        </div>
    );
}

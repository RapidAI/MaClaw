import { useEffect, useRef, useState } from 'react';
import { localizeText } from '../../i18n';
import { useGitRepoNameStatus } from './gitRepoNameStatus';
import './GitRepoCreatePanel.css';

type Binding = (...args: unknown[]) => Promise<unknown>;

function appBinding(name: string): Binding | null {
    const bindings = (window as unknown as { go?: { main?: { App?: Record<string, unknown> } } }).go?.main?.App;
    const fn = bindings?.[name];
    return typeof fn === 'function' ? fn as Binding : null;
}

// GitPlatformCredentialView mirrors the Go binding. It carries no secret
// material — the id is the only handle that travels back.
export type GitPlatformCredentialView = {
    id: string;
    kind: string; // "account" | "credential"
    provider?: string; // "github" | "gitlab" | "" (generic)
    name?: string;
    login?: string;
    origin?: string;
};

type GitPlatformNamespaceView = {
    key: string;
    label: string;
    kind: string; // "personal" | "org" | "group"
    namespace_id?: number;
};

// GitPlatformNamespaceList mirrors the Go binding: rows plus an optional
// warning explaining a partial GitHub list (org enumeration failed but the
// personal namespace stays usable).
type GitPlatformNamespaceList = {
    rows: GitPlatformNamespaceView[];
    org_warning?: string;
};

// GitRepoCreated is the created repository handed to the host surface. The
// credentialId carries the picker's choice through to the bind step ('' =
// the settings account); origin is the credential's site root (gitlab
// self-hosted rows differ from the account default and the host's connect
// call must pin it).
export type GitRepoCreated = {
    fullName: string;
    cloneUrl: string;
    webUrl?: string;
    defaultBranch?: string;
    privateRepo: boolean;
    credentialId: string;
    origin: string;
    service: string;
};

/**
 * Shared "create a new GitHub/GitLab repository" panel for the two bind
 * surfaces: the cloud-agent connect dialog and the virtual-repository
 * mapping dialog. The panel owns credential selection (multi-credential:
 * the settings account plus provider-tagged virtual-repository
 * credentials), namespace/org selection, the name field with a debounced
 * availability probe, and the create call. Binding stays with the host:
 * `onCreated` receives the created repository and the chosen credential.
 */
export function GitRepoCreatePanel({ lang, service, onServiceChange, submitLabel, busy, onCreated, testIdPrefix = 'git-repo-create' }: {
    lang: string;
    service: string;
    // onServiceChange renders the GitHub/GitLab segmented toggle inside the
    // panel (virtual-repository surface). Omit it when the host already
    // owns the service switch (cloud connect dialog).
    onServiceChange?: (service: string) => void;
    // submitLabel renders the panel's own create button with that label.
    // The host triggers the follow-up binding inside onCreated.
    submitLabel?: string;
    busy?: boolean;
    onCreated: (created: GitRepoCreated) => void;
    testIdPrefix?: string;
}) {
    const text = (en: string, zh: string, hant?: string) => localizeText(lang, en, zh, hant);
    const [creds, setCreds] = useState<GitPlatformCredentialView[]>([]);
    const [credsBusy, setCredsBusy] = useState(true);
    const [credsError, setCredsError] = useState('');
    const [credentialId, setCredentialId] = useState('');
    const [namespaces, setNamespaces] = useState<GitPlatformNamespaceView[]>([]);
    const [nsBusy, setNsBusy] = useState(false);
    const [nsError, setNsError] = useState('');
    const [nsWarning, setNsWarning] = useState('');
    const [nsKey, setNsKey] = useState('');
    const [name, setName] = useState('');
    const [isPrivate, setIsPrivate] = useState(true);
    const [readme, setReadme] = useState(true);
    const [creating, setCreating] = useState(false);
    const [createError, setCreateError] = useState('');
    const [nsReloadNonce, setNsReloadNonce] = useState(0);
    const credsGeneration = useRef(0);
    const nsGeneration = useRef(0);

    // Credential list per service. The first usable credential (normally the
    // settings account) is the default pick; the list also decides whether
    // creation is offered at all (permission gating lives backend-side).
    useEffect(() => {
        const gen = ++credsGeneration.current;
        const current = () => gen === credsGeneration.current;
        setCreds([]);
        setCredsError('');
        setCredsBusy(true);
        setCredentialId('');
        const binding = appBinding('GitPlatformCreateCredentials');
        void (async () => {
            if (!binding) {
                if (current()) setCredsBusy(false);
                return;
            }
            try {
                const rows = (await binding(service)) as GitPlatformCredentialView[];
                if (!current()) return;
                const list = Array.isArray(rows) ? rows.filter((row) => row && typeof row.kind === 'string') : [];
                setCreds(list);
                setCredentialId(list.length ? String(list[0].id || '') : '');
            } catch (err) {
                if (!current()) return;
                setCreds([]);
                setCredsError(err instanceof Error ? err.message : String(err));
            } finally {
                if (current()) setCredsBusy(false);
            }
        })();
        return () => { credsGeneration.current += 1; };
    }, [service]);

    // Namespaces for the chosen credential. A credential switch or a service
    // switch reloads and re-picks the first namespace (personal first — the
    // backend orders GitHub that way; GitLab rows are sorted here). The
    // guard intentionally does NOT read credsBusy: while the credential list
    // is loading, creds is empty and the membership check below fails, so
    // including credsBusy in the deps would fire a second, wasted fetch
    // when loading completes.
    useEffect(() => {
        const gen = ++nsGeneration.current;
        const current = () => gen === nsGeneration.current;
        setNamespaces([]);
        setNsError('');
        setNsWarning('');
        setNsKey('');
        if (credsError || !creds.some((row) => String(row.id || '') === credentialId)) return;
        const binding = appBinding('GitPlatformNamespaces');
        if (!binding) return;
        setNsBusy(true);
        void (async () => {
            try {
                const payload = (await binding(service, credentialId)) as GitPlatformNamespaceList;
                if (!current()) return;
                const list = (Array.isArray(payload?.rows) ? payload.rows : [])
                    .filter((row) => row && typeof row.key === 'string' && row.key)
                    .sort((a, b) => rankNamespace(a) - rankNamespace(b) || a.key.localeCompare(b.key));
                setNamespaces(list);
                setNsKey(list.length ? list[0].key : '');
                // A partial GitHub list stays usable but must say why the
                // orgs are missing instead of reading as "none".
                setNsWarning(String(payload?.org_warning || ''));
            } catch (err) {
                if (!current()) return;
                setNamespaces([]);
                setNsError(err instanceof Error ? err.message : String(err));
            } finally {
                if (current()) setNsBusy(false);
            }
        })();
        return () => { nsGeneration.current += 1; };
    }, [service, credentialId, credsError, creds, nsReloadNonce]);

    const namespace = namespaces.find((row) => row.key === nsKey) || null;
    const { state: nameState, error: statusError, recheck } = useGitRepoNameStatus({
        service,
        credentialId,
        owner: namespace?.key || '',
        name,
        enabled: !!namespace,
    });

    const credentialLabel = (row: GitPlatformCredentialView): string => {
        if (row.kind === 'account') {
            return `@${row.login || row.name || service}`;
        }
        // Disambiguate the two things a name alone cannot: a legacy generic
        // row (its token's platform only surfaces on first use) and a gitlab
        // self-hosted site when several are stored.
        let label = row.name || row.login || row.id;
        if (!row.provider) {
            label += ` · ${text('generic', '通用', '通用')}`;
        }
        if (row.origin) {
            label += ` · ${row.origin}`;
        }
        return label;
    };

    const namespaceLabel = (row: GitPlatformNamespaceView): string => {
        if (row.kind === 'personal') {
            return `${row.key} · ${text('personal', '个人', '個人')}`;
        }
        if (row.kind === 'org' || row.kind === 'group') {
            return `${row.key} · ${text(row.kind === 'org' ? 'organization' : 'group', row.kind === 'org' ? '组织' : '群组', row.kind === 'org' ? '組織' : '群組')}`;
        }
        return row.key;
    };

    const canCreate = !!namespace && !!name.trim() && nameState.status === 'ok' && !creating && !busy;

    const create = async () => {
        if (!canCreate || !namespace) return;
        setCreating(true);
        setCreateError('');
        try {
            const binding = appBinding('GitPlatformCreateRepo');
            if (!binding) throw new Error(text('Creation binding unavailable', '创建能力不可用', '建立能力不可用'));
            const view = await binding({
                service,
                credential_id: credentialId || undefined,
                owner: namespace.key,
                namespace_id: namespace.namespace_id || undefined,
                namespace_kind: namespace.kind,
                name: name.trim(),
                private: isPrivate,
                init_readme: readme,
            }) as { full_name?: string; web_url?: string; clone_url?: string; default_branch?: string; private?: boolean };
            onCreated({
                fullName: String(view?.full_name || `${namespace.key}/${name.trim()}`),
                cloneUrl: String(view?.clone_url || ''),
                webUrl: String(view?.web_url || ''),
                defaultBranch: String(view?.default_branch || ''),
                privateRepo: view?.private === true || isPrivate,
                credentialId,
                origin: creds.find((row) => String(row.id || '') === credentialId)?.origin || '',
                service,
            });
        } catch (err) {
            setCreateError(err instanceof Error ? err.message : String(err));
            // The authoritative answer is the provider's create failure; refresh
            // the probe so a "taken" verdict from it replaces the stale OK.
            recheck();
        } finally {
            setCreating(false);
        }
    };

    return (
        <div className="grc-panel" data-testid={`${testIdPrefix}-panel`}>
            {onServiceChange ? (
                <div className="grc-row">
                    <span className="grc-label">{text('Platform', '平台', '平臺')}</span>
                    <div className="grc-segmented" role="group" aria-label={text('Platform', '平台', '平臺')}>
                        {/* Creating disables the flip: an in-flight create must
                            not be visually detached from the platform that is
                            receiving it. */}
                        <button type="button" aria-pressed={service !== 'gitlab'} disabled={!!busy || creating} onClick={() => onServiceChange('github')}>GitHub</button>
                        <button type="button" aria-pressed={service === 'gitlab'} disabled={!!busy || creating} onClick={() => onServiceChange('gitlab')}>GitLab</button>
                    </div>
                </div>
            ) : null}

            <div className="grc-row">
                <label className="grc-field">
                    <span className="grc-label">{text('Credential', '凭据', '憑據')}</span>
                    <select
                        data-testid={`${testIdPrefix}-credential`}
                        value={credentialId}
                        disabled={credsBusy || creds.length <= 1 || !!busy}
                        onChange={(event) => { setCreateError(''); setCredentialId(event.target.value); }}
                    >
                        {creds.map((row) => (
                            <option key={row.id || '__account'} value={row.id || ''}>
                                {row.kind === 'account'
                                    ? text(`Connected account @${row.login || ''}`, `已连接账号 @${row.login || ''}`, `已連接帳號 @${row.login || ''}`)
                                    : credentialLabel(row)}
                            </option>
                        ))}
                    </select>
                </label>
                <label className="grc-field">
                    <span className="grc-label">{text('Owner', '归属', '歸屬')}</span>
                    <select
                        data-testid={`${testIdPrefix}-namespace`}
                        value={nsKey}
                        disabled={nsBusy || namespaces.length === 0 || !!busy}
                        onChange={(event) => { setCreateError(''); setNsKey(event.target.value); }}
                    >
                        {namespaces.map((row) => <option key={row.key} value={row.key}>{namespaceLabel(row)}</option>)}
                    </select>
                </label>
            </div>
            {credsBusy ? <p className="grc-note">{text('Loading credentials…', '正在读取凭据…', '正在讀取憑據…')}</p> : null}
            {credsError ? <p className="grc-note grc-note--error">{credsError}</p> : null}
            {!credsBusy && !credsError && creds.length === 0 ? (
                <p className="grc-note">{text('No usable credential for this platform. Add one in the credential manager.', '该平台没有可用凭据，请先在凭据管理中添加。', '該平台沒有可用憑據，請先在憑據管理中新增。')}</p>
            ) : null}
            {nsBusy ? <p className="grc-note">{text('Loading namespaces…', '正在读取归属空间…', '正在讀取歸屬空間…')}</p> : null}
            {nsWarning ? (
                <p className="grc-note" data-testid={`${testIdPrefix}-ns-warning`} role="status">
                    {text('Organization list failed — only your personal namespace is shown. Creation there still works.', '组织列表读取失败，当前仅显示个人命名空间；在其中创建不受影响。', '組織列表讀取失敗，當前僅顯示個人命名空間；在其中建立不受影響。')}
                    <small>{nsWarning}</small>
                </p>
            ) : null}
            {nsError ? (
                <p className="grc-note grc-note--error">
                    {nsError}
                    <button type="button" className="grc-retry" onClick={() => setNsReloadNonce((nonce) => nonce + 1)}>{text('Retry', '重试', '重試')}</button>
                </p>
            ) : null}

            <label className="grc-field">
                <span className="grc-label">{text('Repository name', '仓库名', '倉庫名')}</span>
                <input
                    data-testid={`${testIdPrefix}-name`}
                    value={name}
                    disabled={!!busy || !namespace}
                    placeholder={text('new-repository', '新仓库名', '新倉庫名')}
                    autoComplete="off"
                    spellCheck={false}
                    onChange={(event) => { setCreateError(''); setName(event.target.value); }}
                    onKeyDown={(event) => {
                        // Enter inside the panel submits the creation itself —
                        // the surrounding form's submit path only handles the
                        // pick-existing flow.
                        if (event.key === 'Enter' && canCreate) {
                            event.preventDefault();
                            void create();
                        }
                    }}
                />
            </label>
            {nameState.status === 'checking' ? (
                <p className="grc-availability" data-testid={`${testIdPrefix}-status`}>{text('Checking availability…', '正在检查名称可用性…', '正在檢查名稱可用性…')}</p>
            ) : null}
            {nameState.status === 'ok' && namespace ? (
                <p className="grc-availability grc-availability--ok" data-testid={`${testIdPrefix}-status`} role="status">
                    {text(`「${namespace.key}/${name.trim()}」 is available — you can create and bind it.`, `「${namespace.key}/${name.trim()}」可用，可直接创建并绑定。`, `「${namespace.key}/${name.trim()}」可用，可直接建立並綁定。`)}
                </p>
            ) : null}
            {nameState.status === 'taken' ? (
                <p className="grc-availability grc-availability--bad" data-testid={`${testIdPrefix}-status`} role="alert">
                    {nameState.reason || text('This name is taken.', '该名称已被占用。', '該名稱已被佔用。')}
                </p>
            ) : null}
            {nameState.status === 'invalid' ? (
                <p className="grc-availability grc-availability--bad" data-testid={`${testIdPrefix}-status`} role="alert">
                    {text('Use letters, digits, dot, underscore or hyphen (1-200 characters).', '仅支持字母、数字、点、下划线和连字符（1-200 字符）。', '僅支援字母、數字、點、底線和連字號（1-200 字元）。')}
                </p>
            ) : null}
            {statusError ? (
                <p className="grc-availability grc-availability--bad" data-testid={`${testIdPrefix}-status`} role="alert">
                    {statusError}
                    <button type="button" className="grc-retry" onClick={recheck}>{text('Retry', '重试', '重試')}</button>
                </p>
            ) : null}

            <div className="grc-row grc-row--options">
                <div className="grc-segmented" role="group" aria-label={text('Visibility', '可见性', '可見性')}>
                    <button type="button" aria-pressed={isPrivate} disabled={!!busy || creating} onClick={() => setIsPrivate(true)}>{text('Private', '私有', '私有')}</button>
                    <button type="button" aria-pressed={!isPrivate} disabled={!!busy || creating} onClick={() => setIsPrivate(false)}>{text('Public', '公开', '公開')}</button>
                </div>
                <label className="grc-checkbox">
                    <input type="checkbox" checked={readme} disabled={!!busy || creating} onChange={(event) => setReadme(event.target.checked)} />
                    <span>{text('Initialize with README', '初始化 README', '初始化 README')}</span>
                </label>
            </div>

            {createError ? <p className="grc-note grc-note--error" role="alert">{createError}</p> : null}

            {submitLabel ? (
                <button
                    type="button"
                    className="grc-submit"
                    data-testid={`${testIdPrefix}-submit`}
                    disabled={!canCreate}
                    aria-busy={creating}
                    onClick={() => void create()}
                >
                    {creating ? text('Creating…', '创建中…', '建立中…') : submitLabel}
                </button>
            ) : null}
        </div>
    );
}

// rankNamespace orders the personal namespace first so the default pick is
// the account's own space.
function rankNamespace(row: GitPlatformNamespaceView): number {
    return row.kind === 'personal' ? 0 : 1;
}

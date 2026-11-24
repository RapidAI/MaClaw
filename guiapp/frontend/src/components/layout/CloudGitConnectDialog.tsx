import { useEffect, useRef, useState } from 'react';
import { localizeText } from '../../i18n';
import { refreshCloudGit } from './cloudAgentAssistant';

type Binding = (...args: unknown[]) => Promise<unknown>;

function appBinding(name: string): Binding | null {
    const bindings = (window as unknown as { go?: { main?: { App?: Record<string, unknown> } } }).go?.main?.App;
    const fn = bindings?.[name];
    return typeof fn === 'function' ? fn as Binding : null;
}

/**
 * The coding-instance repository form. ConnectGit clones the repo into the
 * instance workspace on the srv; until then the turn works in the bare
 * workspace and git_commit/git_push stay unavailable.
 */
export function CloudGitConnectDialog({ lang, instanceId, onClose }: { lang: string; instanceId: string; onClose: () => void }) {
    const [git, setGit] = useState({
        service: 'github',
        origin: 'https://github.com',
        repo: '',
        authorName: '',
        authorEmail: '',
        token: '',
        source: '',
    });
    const [error, setError] = useState('');
    const [note, setNote] = useState('');
    const [busy, setBusy] = useState(false);
    const dialogRef = useRef<HTMLDivElement | null>(null);

    const text = (en: string, zh: string, hant?: string) => localizeText(lang, en, zh, hant);

    // Esc closes like the other panel dialogs; opening focuses the first
    // field. onClose is an inline lambda, so it goes through a ref and the
    // effect runs once — otherwise every parent re-render would steal focus
    // back to the first input while the user is typing.
    const onCloseRef = useRef(onClose);
    onCloseRef.current = onClose;
    useEffect(() => {
        const invoker = document.activeElement as HTMLElement | null;
        const onKey = (event: KeyboardEvent) => {
            if (event.key === 'Escape') onCloseRef.current();
        };
        window.addEventListener('keydown', onKey);
        dialogRef.current?.querySelector<HTMLInputElement>('input')?.focus();
        return () => {
            window.removeEventListener('keydown', onKey);
            invoker?.focus?.();
        };
    }, []);

    const fail = (err: unknown) => {
        setError(err instanceof Error ? err.message : String(err));
    };

    const fillFromAccount = async () => {
        const account = appBinding('CloudAgentGitHubAccount');
        if (!account) return;
        try {
            const view = await account() as {
                connected?: boolean; login?: string; name?: string;
                email?: string; token?: string; repo_scope?: boolean;
            };
            if (!view?.connected || !view.token) {
                setNote(text('Connect GitHub in Settings first', '请先在设置中登录 GitHub', '請先在設定中登入 GitHub'));
                return;
            }
            if (!view.repo_scope) {
                setNote(text('This token has no repo permission', '该令牌没有仓库权限', '該權杖沒有倉庫權限'));
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
            setNote(text(`Account ${view.login} filled`, `已填入账号 ${view.login}`, `已填入帳號 ${view.login}`));
        } catch (err) {
            fail(err);
        }
    };

    const submit = async () => {
        const connect = appBinding('ConnectCloudAgentGit');
        if (!connect || !instanceId) return;
        setBusy(true);
        setError('');
        try {
            const view = await connect(instanceId, {
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
            await refreshCloudGit();
            onClose();
        } catch (err) {
            fail(err);
        } finally {
            setBusy(false);
        }
    };

    const field = (label: string, key: keyof typeof git, type = 'text') => (
        <input
            aria-label={label}
            type={type}
            value={git[key]}
            disabled={busy}
            onChange={(event) => setGit((prev) => ({ ...prev, [key]: event.target.value, ...(key === 'token' || key === 'service' ? { source: '' } : {}) }))}
            style={{ padding: '6px 8px', borderRadius: 8, border: '1px solid var(--theme-border, #d0d5dd)', minWidth: 0, width: '100%', boxSizing: 'border-box' }}
        />
    );

    return (
        <div
            role="dialog"
            aria-modal="true"
            data-testid="cloud-git-connect"
            style={{
                position: 'fixed', inset: 0, zIndex: 90,
                background: 'rgba(15, 23, 42, 0.35)',
                display: 'flex', alignItems: 'center', justifyContent: 'center',
            }}
            onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}
        >
            <div
                ref={dialogRef}
                style={{
                    background: 'var(--theme-surface, #fff)',
                    color: 'var(--theme-text, #172033)',
                    borderRadius: 14, padding: 18, width: 420, maxWidth: '90vw',
                    boxShadow: '0 16px 48px rgba(15, 23, 42, 0.2)',
                    display: 'flex', flexDirection: 'column', gap: 10,
                }}
            >
                <header style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 12 }}>
                    <strong>{text('Connect repository', '连接代码库', '連接程式庫')}</strong>
                    <button type="button" onClick={onClose}>{text('Close', '关闭', '關閉')}</button>
                </header>
                <p style={{ margin: 0, fontSize: 12, color: 'var(--theme-text-secondary, #667085)' }}>
                    {text(
                        'The clone lands in the cloud workspace; existing workspace files are merged into it. Commits and pushes then use git_commit/git_push.',
                        '代码库会检出到云端工作区，工作区里已有的文件会并入仓库。之后可以用 git_commit / git_push 提交和推送。',
                        '程式庫會檢出到雲端工作區，工作區裡已有的檔案會併入倉庫。之後可以用 git_commit / git_push 提交和推送。',
                    )}
                </p>
                {error ? <p role="alert" style={{ margin: 0, color: 'var(--theme-danger, #b42318)', fontSize: 12 }}>{error}</p> : null}
                <form
                    onSubmit={(event) => {
                        event.preventDefault();
                        void submit();
                    }}
                    style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}
                >
                    <select
                        aria-label={text('Service', '服务', '服務')}
                        value={git.service}
                        disabled={busy}
                        onChange={(event) => setGit((prev) => ({ ...prev, service: event.target.value, source: '' }))}
                        style={{ padding: '6px 8px', borderRadius: 8, border: '1px solid var(--theme-border, #d0d5dd)' }}
                    >
                        <option value="github">GitHub</option>
                        <option value="gitlab">GitLab</option>
                    </select>
                    {field(text('Origin', '地址', '位址'), 'origin')}
                    <span style={{ gridColumn: '1 / -1' }}>{field(text('Repository (owner/name)', '代码库（owner/name）', '程式庫（owner/name）'), 'repo')}</span>
                    {field(text('Author name', '作者', '作者'), 'authorName')}
                    {field(text('Author email', '邮箱', '郵箱'), 'authorEmail')}
                    <span style={{ gridColumn: '1 / -1' }}>{field(text('Token', '令牌', '權杖'), 'token', 'password')}</span>
                    <button type="button" disabled={busy} onClick={() => void fillFromAccount()}>{text('Use GitHub account', '用 GitHub 账号', '用 GitHub 帳號')}</button>
                    <button type="submit" disabled={busy || !git.repo.trim() || !git.authorName.trim() || !git.authorEmail.trim() || !git.token}>
                        {busy ? text('Connecting…', '连接中…', '連接中…') : text('Connect', '连接', '連接')}
                    </button>
                </form>
                {note ? <span style={{ fontSize: 12, color: 'var(--theme-text-secondary, #667085)' }}>{note}</span> : null}
            </div>
        </div>
    );
}

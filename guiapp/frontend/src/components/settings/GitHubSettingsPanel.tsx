import { useCallback, useEffect, useRef, useState } from 'react';
import { localizeText } from '../../i18n';
import { BrowserOpenURL } from '../../../wailsjs/runtime';
import { corelib, main } from '../../../wailsjs/go/models';
import {
    CancelGitHubOAuth,
    DisconnectGitHub,
    GetGitHubConnection,
    GitHubAPIRequest,
    GitHubCloneRepository,
    ShowItemInFolder,
    StartGitHubOAuth,
    WaitGitHubOAuth,
} from '../../../wailsjs/go/main/App';
import { ConnectionStatusBadge } from './ConnectionStatusBadge';
import { useDialog } from '../CustomDialog';

type GitHubSettingsPanelProps = {
    config: corelib.AppConfig | null;
    lang: string;
    saveRemoteConfigField: (patch: Record<string, any>) => any;
    showToastMessage: (message: string) => void;
};

const t = (lang: string, en: string, hans: string, hant?: string) => localizeText(lang, en, hans, hant);

const GitHubMark = () => (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
        <path d="M12 .5C5.65.5.5 5.65.5 12c0 5.08 3.29 9.39 7.86 10.91.58.11.79-.25.79-.55 0-.27-.01-1.17-.02-2.12-3.2.7-3.88-1.36-3.88-1.36-.52-1.33-1.28-1.68-1.28-1.68-1.04-.71.08-.7.08-.7 1.15.08 1.76 1.18 1.76 1.18 1.03 1.76 2.7 1.25 3.36.96.1-.75.4-1.25.72-1.54-2.55-.29-5.24-1.28-5.24-5.69 0-1.26.45-2.28 1.18-3.09-.12-.29-.51-1.46.11-3.05 0 0 .97-.31 3.17 1.18a11 11 0 0 1 5.77 0c2.2-1.49 3.17-1.18 3.17-1.18.62 1.59.23 2.76.11 3.05.74.81 1.18 1.83 1.18 3.09 0 4.42-2.69 5.39-5.25 5.68.41.35.77 1.05.77 2.12 0 1.53-.01 2.76-.01 3.14 0 .3.2.67.8.55A11.51 11.51 0 0 0 23.5 12C23.5 5.65 18.35.5 12 .5z" />
    </svg>
);

/**
 * GitHub 连接设置页：device-flow 登录、账号状态、API 连通性测试与仓库克隆。
 * token 存在后端 credentials.json（provider "github"），供 skill 搜索、
 * 更新检测与克隆共用。
 */
export const GitHubSettingsPanel = ({ config, lang, saveRemoteConfigField, showToastMessage }: GitHubSettingsPanelProps) => {
    const { showConfirm } = useDialog();
    const [connection, setConnection] = useState<main.GitHubConnectionStatus | null>(null);
    const [deviceInfo, setDeviceInfo] = useState<main.GitHubDeviceInfo | null>(null);
    const [busy, setBusy] = useState(false);
    const [message, setMessage] = useState('');
    const [error, setError] = useState('');
    const [codeCopied, setCodeCopied] = useState(false);
    const [clientIdDraft, setClientIdDraft] = useState('');
    const [advancedOpen, setAdvancedOpen] = useState(false);
    const [cloneUrl, setCloneUrl] = useState('');
    const [cloneDir, setCloneDir] = useState('');
    const [cloneBusy, setCloneBusy] = useState(false);
    const [cloneResult, setCloneResult] = useState<{ ok: boolean; text: string; destDir?: string } | null>(null);

    // A pending device login must die with the panel — navigating away without
    // cancelling would leave the backend polling a code nobody can see.
    const pendingRef = useRef(false);
    // Seed the client-ID draft once — re-seeding on every config object churn
    // would clobber an in-progress edit.
    const seededClientIdRef = useRef(false);

    const loadStatus = useCallback(async (refresh: boolean) => {
        try {
            setConnection(await GetGitHubConnection(refresh));
        } catch {
            /* status read is best-effort; UI shows disconnected */
        }
    }, []);

    useEffect(() => {
        pendingRef.current = deviceInfo != null;
    }, [deviceInfo]);

    useEffect(() => {
        void loadStatus(false);
        return () => {
            if (pendingRef.current) void CancelGitHubOAuth();
        };
    }, [loadStatus]);

    useEffect(() => {
        // Seed only once config has actually arrived — seeding on mount with a
        // still-null config would leave the draft blank forever and hide a
        // previously saved custom client ID.
        if (seededClientIdRef.current || config == null) return;
        seededClientIdRef.current = true;
        setClientIdDraft((config as any)?.github_client_id || '');
    }, [config]);

    const startLogin = () => {
        setBusy(true);
        setError('');
        setMessage('');
        setCodeCopied(false);
        void (async () => {
            try {
                const info = await StartGitHubOAuth();
                setDeviceInfo(info);
                const status = await WaitGitHubOAuth();
                setConnection(status);
                setDeviceInfo(null);
                setMessage(t(lang, `GitHub connected as @${status.login}`, `已连接 GitHub 账号 @${status.login}`, `已連接 GitHub 帳號 @${status.login}`));
            } catch (err: any) {
                const text = String(err?.message || err || '');
                if (!/取消|cancel/i.test(text)) {
                    setError(text || t(lang, 'GitHub login failed', 'GitHub 登录失败', 'GitHub 登入失敗'));
                }
                setDeviceInfo(null);
            } finally {
                setBusy(false);
            }
        })();
    };

    const cancelLogin = () => {
        void CancelGitHubOAuth();
        setDeviceInfo(null);
        setBusy(false);
    };

    const copyCode = async () => {
        if (!deviceInfo?.user_code) return;
        try {
            await navigator.clipboard.writeText(deviceInfo.user_code);
            setCodeCopied(true);
            showToastMessage(t(lang, 'Code copied', '验证码已复制', '驗證碼已複製'));
        } catch {
            showToastMessage(deviceInfo.user_code);
        }
    };

    const disconnect = async () => {
        const ok = await showConfirm(
            t(lang, 'Disconnect GitHub? Skill search, update checks and cloning will lose the account token.', '断开 GitHub 连接？技能搜索、更新检测和代码克隆将失去账号令牌。', '中斷 GitHub 連接？技能搜尋、更新檢測和程式碼複製將失去帳號權杖。'),
            t(lang, 'Disconnect GitHub', '断开 GitHub', '中斷 GitHub'),
        );
        if (!ok) return;
        setBusy(true);
        setError('');
        setMessage('');
        try {
            await DisconnectGitHub();
            setConnection(await GetGitHubConnection(false));
            setMessage(t(lang, 'GitHub disconnected', '已断开 GitHub 连接', '已中斷 GitHub 連接'));
        } catch (err: any) {
            setError(String(err?.message || err || t(lang, 'Disconnect failed', '断开失败', '中斷失敗')));
        } finally {
            setBusy(false);
        }
    };

    const refreshStatus = () => {
        setBusy(true);
        setError('');
        void loadStatus(true)
            .then(() => setMessage(t(lang, 'Status refreshed', '状态已刷新', '狀態已刷新')))
            .finally(() => setBusy(false));
    };

    const testConnection = async () => {
        setBusy(true);
        setError('');
        setMessage('');
        try {
            const raw = await GitHubAPIRequest('GET', '/rate_limit');
            const parsed = JSON.parse(raw);
            const core = parsed?.resources?.core;
            const search = parsed?.resources?.search;
            const parts = [
                core ? `core ${core.remaining}/${core.limit}` : '',
                search ? `search ${search.remaining}/${search.limit}` : '',
            ].filter(Boolean).join(', ');
            setMessage(t(lang, `API reachable — quota ${parts}`, `API 可用 — 限额 ${parts}`, `API 可用 — 限額 ${parts}`));
        } catch (err: any) {
            setError(String(err?.message || err || t(lang, 'GitHub API test failed', 'GitHub API 测试失败', 'GitHub API 測試失敗')));
        } finally {
            setBusy(false);
        }
    };

    const saveClientId = () => {
        const value = clientIdDraft.trim();
        void Promise.resolve(saveRemoteConfigField({ github_client_id: value }))
            .then(() => {
                showToastMessage(t(lang, 'GitHub client ID saved', 'GitHub Client ID 已保存', 'GitHub Client ID 已儲存'));
                void loadStatus(false); // refresh the "effective client id" hint
            })
            .catch((err: any) => setError(String(err?.message || err)));
    };

    const cloneRepo = async () => {
        if (!cloneUrl.trim()) return;
        setCloneBusy(true);
        setCloneResult(null);
        try {
            const res = await GitHubCloneRepository(cloneUrl.trim(), cloneDir.trim());
            setCloneResult({
                ok: true,
                text: (res?.output || '').trim() || t(lang, 'Clone finished', '克隆完成', '複製完成'),
                destDir: res?.dest_dir || '',
            });
        } catch (err: any) {
            setCloneResult({ ok: false, text: String(err?.message || err || t(lang, 'Clone failed', '克隆失败', '複製失敗')) });
        } finally {
            setCloneBusy(false);
        }
    };

    const connected = connection?.connected === true;

    return (
        <>
            <section className="im-settings-card im-settings-channel">
                <p className="im-settings-description">
                    {t(lang,
                        'Connect a GitHub account. The token powers skill search, update checks and code cloning, and private repositories become reachable.',
                        '连接 GitHub 账号。令牌用于技能搜索、更新检测与代码克隆，并可访问私有仓库。',
                        '連接 GitHub 帳號。權杖用於技能搜尋、更新檢測與程式碼複製，並可存取私有儲存庫。')}
                </p>
                <div className="im-settings-toolbar" style={{ alignItems: 'center' }}>
                    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
                        <GitHubMark />
                        <strong>GitHub</strong>
                    </span>
                    <ConnectionStatusBadge status={connected ? 'connected' : 'disconnected'} lang={lang} />
                </div>

                {connected && (
                    <div className="im-settings-toolbar" style={{ alignItems: 'center', gap: 10 }}>
                        {connection?.avatar_url ? (
                            <img src={connection.avatar_url} alt="" width={32} height={32} referrerPolicy="no-referrer" style={{ borderRadius: '50%' }} />
                        ) : null}
                        <span>
                            <strong>@{connection?.login}</strong>
                            {connection?.name ? ` (${connection.name})` : ''}
                        </span>
                        {connection?.html_url ? (
                            <a
                                href={connection.html_url}
                                onClick={(event) => { event.preventDefault(); BrowserOpenURL(connection.html_url!); }}
                            >
                                {t(lang, 'View profile', '查看主页', '檢視主頁')}
                            </a>
                        ) : null}
                    </div>
                )}
                {connected && connection?.scopes ? (
                    <p className="im-settings-description">Scopes: {connection.scopes}</p>
                ) : null}

                {deviceInfo && (
                    <div className="im-settings-card" style={{ marginTop: 8, padding: 12 }}>
                        <p className="im-settings-description">
                            {t(lang,
                                'Open the page below and enter this code to authorize MaClaw:',
                                '打开下面的页面，输入此验证码完成授权：',
                                '打開下面的頁面，輸入此驗證碼完成授權：')}
                        </p>
                        <p style={{ fontSize: 22, fontWeight: 700, letterSpacing: 2, margin: '8px 0' }}>{deviceInfo.user_code}</p>
                        <p className="im-settings-description">
                            <a
                                href={deviceInfo.verification_uri}
                                onClick={(event) => { event.preventDefault(); BrowserOpenURL(deviceInfo.verification_uri); }}
                            >
                                {deviceInfo.verification_uri}
                            </a>
                            {deviceInfo.browser_opened
                                ? ` — ${t(lang, 'opened in your browser', '已在浏览器中打开', '已在瀏覽器中打開')}`
                                : ` — ${t(lang, 'the browser did not open, please open this page manually', '未能自动打开浏览器，请手动打开此页面', '未能自動開啟瀏覽器，請手動開啟此頁面')}`}
                        </p>
                        <div className="im-settings-toolbar">
                            <button type="button" className="im-settings-button" onClick={copyCode}>
                                {codeCopied
                                    ? t(lang, 'Copied', '已复制', '已複製')
                                    : t(lang, 'Copy code', '复制验证码', '複製驗證碼')}
                            </button>
                            <button type="button" className="im-settings-button" onClick={cancelLogin}>
                                {t(lang, 'Cancel', '取消', '取消')}
                            </button>
                            <span className="im-settings-description">
                                {t(lang, 'Waiting for authorization…', '等待授权中…', '等待授權中…')}
                            </span>
                        </div>
                    </div>
                )}

                <div className="im-settings-toolbar">
                    {!connected && !deviceInfo && (
                        <button type="button" className="im-settings-button im-settings-button--accent" disabled={busy} onClick={startLogin}>
                            {busy
                                ? t(lang, 'Starting…', '正在启动…', '正在啟動…')
                                : t(lang, 'Sign in with GitHub', '使用 GitHub 登录', '使用 GitHub 登入')}
                        </button>
                    )}
                    {connected && (
                        <>
                            <button type="button" className="im-settings-button" disabled={busy} onClick={refreshStatus}>
                                {t(lang, 'Refresh status', '刷新状态', '刷新狀態')}
                            </button>
                            <button type="button" className="im-settings-button" disabled={busy} onClick={testConnection}>
                                {t(lang, 'Test API', '测试连接', '測試連接')}
                            </button>
                            <button type="button" className="im-settings-button" disabled={busy} onClick={() => void disconnect()}>
                                {t(lang, 'Disconnect', '断开连接', '中斷連接')}
                            </button>
                        </>
                    )}
                </div>

                {message && <p className="im-settings-description">{message}</p>}
                {error && <p className="im-settings-description" style={{ color: 'var(--theme-danger)' }}>{error}</p>}
            </section>

            <section className="im-settings-card im-settings-channel">
                <p className="im-settings-description">
                    {t(lang,
                        'Clone a repository with the connected account. Leave the directory empty to use the default location.',
                        '使用已连接的账号克隆仓库。目录留空时使用默认位置。',
                        '使用已連接的帳號複製儲存庫。目錄留空時使用預設位置。')}
                </p>
                <div className="im-settings-toolbar" style={{ flexDirection: 'column', alignItems: 'stretch', gap: 8 }}>
                    <input
                        className="form-input"
                        type="text"
                        value={cloneUrl}
                        onChange={(event) => setCloneUrl(event.target.value)}
                        placeholder={t(lang, 'Repository URL or owner/repo', '仓库地址或 owner/repo', '儲存庫地址或 owner/repo')}
                    />
                    <input
                        className="form-input"
                        type="text"
                        value={cloneDir}
                        onChange={(event) => setCloneDir(event.target.value)}
                        placeholder={t(lang, 'Target directory (optional)', '目标目录（可选）', '目標目錄（可選）')}
                    />
                </div>
                <div className="im-settings-toolbar">
                    <button type="button" className="im-settings-button" disabled={cloneBusy || !cloneUrl.trim()} onClick={() => void cloneRepo()}>
                        {cloneBusy
                            ? t(lang, 'Cloning…', '正在克隆…', '正在複製…')
                            : t(lang, 'Clone repository', '克隆仓库', '複製儲存庫')}
                    </button>
                </div>
                {cloneResult && (
                    <>
                        <p className="im-settings-description" style={{ color: cloneResult.ok ? undefined : 'var(--theme-danger)', whiteSpace: 'pre-wrap' }}>
                            {cloneResult.text}
                        </p>
                        {cloneResult.ok && cloneResult.destDir ? (
                            <div className="im-settings-toolbar" style={{ alignItems: 'center' }}>
                                <button type="button" className="im-settings-button" onClick={() => void ShowItemInFolder(cloneResult.destDir!)}>
                                    {t(lang, 'Show in folder', '在文件夹中显示', '在資料夾中顯示')}
                                </button>
                                <span className="im-settings-description" style={{ wordBreak: 'break-all' }}>{cloneResult.destDir}</span>
                            </div>
                        ) : null}
                    </>
                )}
            </section>

            <section className="im-settings-card im-settings-channel">
                <div className="im-settings-toolbar" style={{ alignItems: 'center' }}>
                    <button type="button" className="im-settings-button" onClick={() => setAdvancedOpen((v) => !v)}>
                        {advancedOpen
                            ? t(lang, 'Hide advanced', '收起高级选项', '收起進階選項')
                            : t(lang, 'Advanced', '高级选项', '進階選項')}
                    </button>
                </div>
                {advancedOpen && (
                    <>
                        <p className="im-settings-description">
                            {t(lang,
                                'OAuth Client ID. Leave empty to use the built-in GitHub OAuth app; paste your own OAuth App client ID to customize the authorization page. Device flow must be enabled on the app.',
                                'OAuth Client ID。留空使用内置 GitHub OAuth 应用；粘贴你自己注册的 OAuth App 的 Client ID 可自定义授权页（该应用需开启 Device Flow）。',
                                'OAuth Client ID。留空使用內建 GitHub OAuth 應用；貼上你自己註冊的 OAuth App 的 Client ID 可自訂授權頁（該應用需開啟 Device Flow）。')}
                        </p>
                        <div className="im-settings-toolbar" style={{ flexDirection: 'column', alignItems: 'stretch', gap: 8 }}>
                            <input
                                className="form-input"
                                type="text"
                                value={clientIdDraft}
                                onChange={(event) => setClientIdDraft(event.target.value)}
                                placeholder="Iv1.xxxxxxxxxxxxxxxx"
                            />
                        </div>
                        <div className="im-settings-toolbar">
                            <button type="button" className="im-settings-button" onClick={saveClientId}>
                                {t(lang, 'Save client ID', '保存 Client ID', '儲存 Client ID')}
                            </button>
                            {connection?.client_id ? (
                                <span className="im-settings-description">
                                    {t(lang, 'Effective', '当前生效', '目前生效')}: {connection.client_id}
                                    {connection.custom_client_id ? ` (${t(lang, 'custom', '自定义', '自訂')})` : ` (${t(lang, 'default', '默认', '預設')})`}
                                </span>
                            ) : null}
                        </div>
                    </>
                )}
            </section>
        </>
    );
};

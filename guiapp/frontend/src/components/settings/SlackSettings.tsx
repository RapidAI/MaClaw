import { Dispatch, SetStateAction, useEffect, useState } from 'react';
import { DisconnectSlack, GetSlackConnectionError, GetSlackStatus, LoadConfig, StartSlackLogin } from '../../../wailsjs/go/main/App';
import { corelib } from '../../../wailsjs/go/models';
import { EventsOn } from '../../../wailsjs/runtime';
import { ConnectionStatusBadge } from './ConnectionStatusBadge';
import { textForLang, watchLabel } from './imSettingsShared';

type SlackSettingsProps = {
    config: corelib.AppConfig | null;
    setConfig: Dispatch<SetStateAction<corelib.AppConfig | null>>;
    lang: string;
    saveRemoteConfigField: (patch: Record<string, any>) => any;
    setIMAuditPlatform: Dispatch<SetStateAction<string | null>>;
};

export const SlackSettings = ({
    config,
    setConfig,
    lang,
    setIMAuditPlatform,
}: SlackSettingsProps) => {
    const slack = config as corelib.AppConfig & {
        slack_team_name?: string;
        slack_enabled?: boolean;
        slack_bot_token?: string;
    };
    const [status, setStatus] = useState('disconnected');
    const [busy, setBusy] = useState(false);
    const [message, setMessage] = useState('');
    const [error, setError] = useState('');
    const [detail, setDetail] = useState('');

    useEffect(() => {
        let disposed = false;
        const refresh = () => {
            void GetSlackStatus().then((next) => {
                if (!disposed) setStatus(next || 'disconnected');
            }).catch(() => {});
            void GetSlackConnectionError().then((next) => {
                if (!disposed) setDetail(next || '');
            }).catch(() => {});
        };
        refresh();
        const off = EventsOn('slack-status-changed', () => {
            refresh();
        });
        return () => {
            disposed = true;
            off();
        };
    }, []);

    const connected = status === 'connected' || !!slack?.slack_team_name;
    const login = () => {
        setBusy(true);
        setError('');
        setMessage('');
        void StartSlackLogin()
            .then(async (text) => {
                setMessage(text || textForLang(lang, 'Slack connected', 'Slack 已连接', 'Slack 已連線'));
                const next = await LoadConfig();
                setConfig(next);
                setStatus(await GetSlackStatus());
            })
            .catch((err: any) => {
                setError(String(err?.message || err || textForLang(lang, 'Slack login failed', 'Slack 登录失败', 'Slack 登入失敗')));
            })
            .finally(() => setBusy(false));
    };
    const disconnect = () => {
        setBusy(true);
        setError('');
        setMessage('');
        void DisconnectSlack()
            .then(async () => {
                const next = await LoadConfig();
                setConfig(next);
                setStatus('disconnected');
                setMessage(textForLang(lang, 'Slack disconnected', '已断开 Slack', '已中斷 Slack'));
            })
            .catch((err: any) => {
                setError(String(err?.message || err || textForLang(lang, 'Disconnect failed', '断开失败', '中斷失敗')));
            })
            .finally(() => setBusy(false));
    };

    return (
        <section className="im-settings-card im-settings-channel">
            <p className="im-settings-description">
                {textForLang(
                    lang,
                    'Sign in with Slack in the browser. After it connects, message the bot in Slack and MaClaw replies in that conversation.',
                    '在浏览器里用 Slack 账号登录。连上之后，在 Slack 里给这个机器人发消息，MaClaw 会在同一会话里回复。',
                    '在瀏覽器裡用 Slack 帳號登入。連上之後，在 Slack 裡給這個機器人發訊息，MaClaw 會在同一工作階段裡回覆。',
                )}
            </p>
            <div className="im-settings-toolbar">
                {(slack?.slack_enabled || connected) && <ConnectionStatusBadge status={status} lang={lang} />}
                <button type="button" className="im-settings-button im-settings-button--accent" disabled={busy} onClick={login}>
                    {busy
                        ? textForLang(lang, 'Waiting for Slack…', '正在等待 Slack…', '正在等待 Slack…')
                        : textForLang(lang, 'Sign in and connect', '登录并连接', '登入並連線')}
                </button>
                {(slack?.slack_bot_token || slack?.slack_enabled) && (
                    <button type="button" className="im-settings-button" disabled={busy} onClick={disconnect}>
                        {textForLang(lang, 'Disconnect', '断开', '中斷')}
                    </button>
                )}
                <button type="button" className="im-settings-button im-settings-button--audit" onClick={() => setIMAuditPlatform('slack_local')}>
                    {watchLabel(lang)}
                </button>
            </div>
            {slack?.slack_team_name && (
                <p className="im-settings-description">
                    {textForLang(lang, 'Workspace', '工作区', '工作區')}
                    {': '}
                    {slack.slack_team_name}
                </p>
            )}
            {message && <p className="im-settings-description">{message}</p>}
            {error && <p className="im-settings-description" role="alert">{error}</p>}
            {!error && detail && (status === 'error' || status === 'reconnecting') && (
                <p className="im-settings-description" role="alert">{detail}</p>
            )}
        </section>
    );
};

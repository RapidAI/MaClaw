import { useCallback, useEffect, useRef, useState, type Dispatch, type SetStateAction } from 'react';
import { ListLansengerGroups, LoadConfig, RestartLansenger, SetLansengerGroupIgnored, SetLansengerLocalMode } from '../../../wailsjs/go/main/App';
import { main } from '../../../wailsjs/go/models';
import { ConnectionStatusBadge } from './ConnectionStatusBadge';
import { channelModeLabel, localModeOptions, restartLabel, switchFailedLabel, textForLang, watchLabel } from './imSettingsShared';

type LansengerSettingsProps = {
    config: main.AppConfig | null;
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>;
    lang: string;
    saveRemoteConfigField: (patch: Record<string, any>) => any;
    lansengerStatus: string;
    setLansengerStatus: Dispatch<SetStateAction<string>>;
    lansengerLocalMode: boolean;
    setLansengerLocalModeState: Dispatch<SetStateAction<boolean>>;
    setIMAuditPlatform: Dispatch<SetStateAction<string | null>>;
};

type LansengerGroupRow = {
    group_id?: string;
    name?: string;
    description?: string;
    owner_id?: string;
    owner_name?: string;
    total_members?: number;
    state?: number;
    ignored?: boolean;
    /** Local ignore-list only (not returned by Lansenger group fetch). */
    orphan?: boolean;
};

type LansengerGroupListPayload = {
    total?: number;
    groups?: LansengerGroupRow[];
};

function wailsErrorMessage(err: unknown, fallback: string): string {
    if (err == null) return fallback;
    if (typeof err === 'string') return err || fallback;
    if (typeof err === 'object') {
        const anyErr = err as { message?: unknown; error?: unknown };
        if (typeof anyErr.message === 'string' && anyErr.message.trim()) return anyErr.message;
        if (typeof anyErr.error === 'string' && anyErr.error.trim()) return anyErr.error;
    }
    const s = String(err);
    return s && s !== '[object Object]' ? s : fallback;
}

export const LansengerSettings = ({
    config,
    setConfig,
    lang,
    saveRemoteConfigField,
    lansengerStatus,
    setLansengerStatus,
    lansengerLocalMode,
    setLansengerLocalModeState,
    setIMAuditPlatform,
}: LansengerSettingsProps) => {
    const [groupsOpen, setGroupsOpen] = useState(false);
    const [groupsLoading, setGroupsLoading] = useState(false);
    const [groupsError, setGroupsError] = useState('');
    const [groups, setGroups] = useState<LansengerGroupRow[]>([]);
    const [groupsTotal, setGroupsTotal] = useState(0);
    const [ignoreBusyID, setIgnoreBusyID] = useState('');
    const loadGenRef = useRef(0);

    const loadGroups = useCallback(() => {
        const gen = ++loadGenRef.current;
        setGroupsOpen(true);
        setGroupsLoading(true);
        setGroupsError('');
        ListLansengerGroups()
            .then((result: LansengerGroupListPayload | null) => {
                if (gen !== loadGenRef.current) return;
                const list = Array.isArray(result?.groups) ? result!.groups! : [];
                setGroups(list);
                setGroupsTotal(typeof result?.total === 'number' ? result.total : list.length);
            })
            .catch((err: unknown) => {
                if (gen !== loadGenRef.current) return;
                setGroups([]);
                setGroupsTotal(0);
                setGroupsError(
                    wailsErrorMessage(
                        err,
                        textForLang(lang, 'Failed to load groups', '加载群列表失败', '載入群列表失敗'),
                    ),
                );
            })
            .finally(() => {
                if (gen !== loadGenRef.current) return;
                setGroupsLoading(false);
            });
    }, [lang]);

    const toggleGroupIgnored = useCallback((groupID: string, ignored: boolean) => {
        const id = String(groupID || '').trim();
        if (!id) return;
        setIgnoreBusyID(id);
        // Optimistic UI update so the row flips immediately.
        setGroups((prev) => prev.map((g) => (g.group_id === id ? { ...g, ignored } : g)));
        SetLansengerGroupIgnored(id, ignored)
            .then(() => LoadConfig().then((c: any) => setConfig(c)).catch(() => {}))
            .then(() => {
                if (!ignored) {
                    // Drop ignore-list-only rows once the user re-enables them.
                    setGroups((prev) => prev.filter((g) => !(g.group_id === id && g.orphan)));
                }
            })
            .catch((err: unknown) => {
                // Revert optimistic flip on failure.
                setGroups((prev) => prev.map((g) => (g.group_id === id ? { ...g, ignored: !ignored } : g)));
                alert(wailsErrorMessage(err, textForLang(lang, 'Failed to update ignore list', '更新忽略列表失败', '更新忽略列表失敗')));
            })
            .finally(() => setIgnoreBusyID((cur) => (cur === id ? '' : cur)));
    }, [lang, setConfig]);

    const closeGroupInfo = useCallback(() => {
        // Invalidate in-flight responses so they cannot overwrite a later open.
        loadGenRef.current += 1;
        setGroupsOpen(false);
        setGroupsLoading(false);
        setGroupsError('');
        setIgnoreBusyID('');
    }, []);

    useEffect(() => {
        if (!groupsOpen) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key === 'Escape') {
                e.preventDefault();
                closeGroupInfo();
            }
        };
        window.addEventListener('keydown', onKey);
        return () => window.removeEventListener('keydown', onKey);
    }, [groupsOpen, closeGroupInfo]);

    return (
        <section className="im-settings-card im-settings-channel">
            <p className="im-settings-description">
                {textForLang(lang, 'Configure Lansenger access for TigerClaw Agent messages.', '\u914d\u7f6e\u84dd\u4fe1\u63a5\u5165\uff0c\u7528\u84dd\u4fe1\u4e0e TigerClaw Agent \u5bf9\u8bdd\u3002', '\u914d\u7f6e\u85cd\u4fe1\u63a5\u5165\uff0c\u7528\u85cd\u4fe1\u8207 TigerClaw Agent \u5c0d\u8a71\u3002')}
            </p>
            <div className="im-settings-toolbar">
                <label className="im-settings-toggle">
                    <input
                        type="checkbox"
                        aria-label={textForLang(lang, 'Enable Lansenger', '\u542f\u7528\u84dd\u4fe1', '\u555f\u7528\u85cd\u4fe1')}
                        checked={(config as any)?.lansenger_enabled || false}
                        onChange={(e) => saveRemoteConfigField({ lansenger_enabled: e.target.checked } as any)}
                    />
                    <span>{textForLang(lang, 'Enable Lansenger', '\u542f\u7528\u84dd\u4fe1', '\u555f\u7528\u85cd\u4fe1')}</span>
                </label>
                {(config as any)?.lansenger_enabled && (
                    <>
                        <ConnectionStatusBadge status={lansengerStatus} lang={lang} />
                        <button
                            type="button"
                            className="im-settings-button"
                            onClick={() => RestartLansenger().then(setLansengerStatus).catch((err: any) => {
                                alert(err?.message || err || restartLabel(lang));
                            })}
                        >
                            {restartLabel(lang)}
                        </button>
                    </>
                )}
                <button
                    type="button"
                    className="im-settings-button"
                    onClick={loadGroups}
                    disabled={groupsLoading}
                    title={textForLang(lang, 'List groups this bot has joined', '\u67e5\u770b\u673a\u5668\u4eba\u5df2\u52a0\u5165\u7684\u7fa4', '\u67e5\u770b\u6a5f\u5668\u4eba\u5df2\u52a0\u5165\u7684\u7fa4')}
                >
                    {textForLang(lang, 'Group Info', '\u7fa4\u4fe1\u606f', '\u7fa4\u8cc7\u8a0a')}
                </button>
                <button type="button" className="im-settings-button im-settings-button--audit" onClick={() => setIMAuditPlatform('lansenger')}>
                    {watchLabel(lang)}
                </button>
            </div>
            <div className="im-settings-mode-row">
                <span>{channelModeLabel(lang)}</span>
                <div className="im-settings-segmented">
                    {localModeOptions(lang).map((opt) => (
                        <button
                            key={String(opt.value)}
                            type="button"
                            aria-label={opt.desc}
                            title={opt.desc}
                            data-active={lansengerLocalMode === opt.value}
                            onClick={() => {
                                const prev = lansengerLocalMode;
                                setLansengerLocalModeState(opt.value);
                                SetLansengerLocalMode(opt.value).then(() => {
                                    LoadConfig().then((c: any) => setConfig(c)).catch(() => {});
                                }).catch((err: any) => {
                                    setLansengerLocalModeState(prev);
                                    alert(err?.message || err || switchFailedLabel(lang));
                                });
                            }}
                        >
                            {opt.label}
                        </button>
                    ))}
                </div>
            </div>
            <div className="im-settings-grid im-settings-grid--two">
                <label className="im-settings-field">
                    <span>App ID</span>
                    <input type="text" value={(config as any)?.lansenger_app_id || ''} onChange={(e) => saveRemoteConfigField({ lansenger_app_id: e.target.value } as any)} placeholder="Lansenger App ID" spellCheck={false} />
                </label>
                <label className="im-settings-field">
                    <span>App Secret</span>
                    <input type="password" value={(config as any)?.lansenger_app_secret || ''} onChange={(e) => saveRemoteConfigField({ lansenger_app_secret: e.target.value } as any)} placeholder="Lansenger App Secret" autoComplete="off" />
                </label>
                <label className="im-settings-field">
                    <span>{textForLang(lang, 'Gateway', '\u7f51\u5173', '\u7db2\u95dc')}</span>
                    <input type="text" value={(config as any)?.lansenger_gateway_url || ''} onChange={(e) => saveRemoteConfigField({ lansenger_gateway_url: e.target.value } as any)} placeholder="https://apigw.lx.qianxin.com" spellCheck={false} />
                </label>
                <label className="im-settings-field">
                    <span>{textForLang(lang, 'WS Gateway', 'WS \u7f51\u5173', 'WS \u7db2\u95dc')}</span>
                    <input type="text" value={(config as any)?.lansenger_wss_url || ''} onChange={(e) => saveRemoteConfigField({ lansenger_wss_url: e.target.value } as any)} placeholder={textForLang(lang, 'Optional, usually blank', '\u53ef\u9009\uff0c\u901a\u5e38\u7559\u7a7a', '\u53ef\u9078\uff0c\u901a\u5e38\u7559\u7a7a')} spellCheck={false} />
                </label>
            </div>

            {groupsOpen && (
                <div className="im-groups-modal-overlay" role="presentation" onClick={closeGroupInfo}>
                    <section
                        className="im-groups-modal"
                        role="dialog"
                        aria-modal="true"
                        aria-labelledby="lansenger-groups-dialog-title"
                        onClick={(e) => e.stopPropagation()}
                    >
                        <div className="im-groups-modal__header">
                            <div>
                                <strong id="lansenger-groups-dialog-title">
                                    {textForLang(lang, 'Joined Groups', '\u5df2\u52a0\u5165\u7684\u7fa4', '\u5df2\u52a0\u5165\u7684\u7fa4')}
                                </strong>
                                {!groupsLoading && !groupsError && (
                                    <span className="im-groups-modal__count">
                                        {groups.length > 0 && groups.length < groupsTotal
                                            ? textForLang(
                                                lang,
                                                `Showing ${groups.length} of ${groupsTotal}`,
                                                `已加载 ${groups.length} / 共 ${groupsTotal} 个`,
                                                `已載入 ${groups.length} / 共 ${groupsTotal} 個`,
                                            )
                                            : textForLang(lang, `${groupsTotal} total`, `共 ${groupsTotal} 个`, `共 ${groupsTotal} 個`)}
                                    </span>
                                )}
                            </div>
                            <button
                                type="button"
                                className="im-groups-modal__close"
                                aria-label={textForLang(lang, 'Close', '\u5173\u95ed', '\u95dc\u9589')}
                                onClick={closeGroupInfo}
                            >
                                ×
                            </button>
                        </div>
                        <div className="im-groups-modal__body">
                            {groupsLoading && (
                                <p className="im-groups-modal__status">
                                    {textForLang(lang, 'Loading groups…', '\u6b63\u5728\u52a0\u8f7d\u7fa4\u5217\u8868\u2026', '\u6b63\u5728\u8f09\u5165\u7fa4\u5217\u8868\u2026')}
                                </p>
                            )}
                            {!groupsLoading && groupsError && (
                                <div className="im-groups-modal__error" role="alert">{groupsError}</div>
                            )}
                            {!groupsLoading && !groupsError && groups.length === 0 && (
                                <p className="im-groups-modal__status">
                                    {textForLang(lang, 'No groups found. Invite the bot into a group first.', '\u672a\u67e5\u5230\u7fa4\u3002\u8bf7\u5148\u5c06\u673a\u5668\u4eba\u62c9\u5165\u7fa4\u804a\u3002', '\u672a\u67e5\u5230\u7fa4\u3002\u8acb\u5148\u5c07\u6a5f\u5668\u4eba\u62c9\u5165\u7fa4\u804a\u3002')}
                                </p>
                            )}
                            {!groupsLoading && !groupsError && groups.length > 0 && (
                                <>
                                    <p className="im-groups-modal__hint">
                                        {textForLang(
                                            lang,
                                            '“Ignore” keeps the bot in the group but stops answering there. It does not remove the bot on Lansenger.',
                                            '「不响应」只是本机不再回答该群消息，机器人仍留在蓝信群里（无法通过 API 退群）。',
                                            '「不回應」只是本機不再回答該群訊息，機器人仍留在藍信群裡（無法透過 API 退群）。',
                                        )}
                                    </p>
                                    <div className="im-groups-table-wrap">
                                        <table className="im-groups-table">
                                            <thead>
                                                <tr>
                                                    <th>{textForLang(lang, 'Name', '\u7fa4\u540d\u79f0', '\u7fa4\u540d\u7a31')}</th>
                                                    <th>Group ID</th>
                                                    <th>{textForLang(lang, 'Members', '\u6210\u5458', '\u6210\u54e1')}</th>
                                                    <th>{textForLang(lang, 'Owner', '\u7fa4\u4e3b', '\u7fa4\u4e3b')}</th>
                                                    <th>{textForLang(lang, 'Description', '\u63cf\u8ff0', '\u63cf\u8ff0')}</th>
                                                    <th>{textForLang(lang, 'Response', '\u54cd\u5e94', '\u56de\u61c9')}</th>
                                                </tr>
                                            </thead>
                                            <tbody>
                                                {groups.map((g, idx) => {
                                                    const id = g.group_id || '';
                                                    const owner = g.owner_name || g.owner_id || '—';
                                                    const ignored = !!g.ignored;
                                                    const busy = ignoreBusyID === id;
                                                    return (
                                                        <tr key={id || `row-${idx}`} data-ignored={ignored ? 'true' : undefined}>
                                                            <td className="im-groups-table__name">
                                                                {g.name || id || '—'}
                                                                {ignored ? (
                                                                    <span className="im-groups-badge im-groups-badge--muted">
                                                                        {textForLang(lang, 'Ignored', '\u4e0d\u54cd\u5e94', '\u4e0d\u56de\u61c9')}
                                                                    </span>
                                                                ) : null}
                                                            </td>
                                                            <td className="im-groups-table__id" title={id}>{id || '—'}</td>
                                                            <td>{typeof g.total_members === 'number' ? g.total_members : '—'}</td>
                                                            <td title={g.owner_id || ''}>{owner}</td>
                                                            <td className="im-groups-table__desc">{g.description || '—'}</td>
                                                            <td className="im-groups-table__action">
                                                                <button
                                                                    type="button"
                                                                    className={ignored ? 'im-settings-button im-settings-button--primary' : 'im-settings-button'}
                                                                    disabled={!id || busy || groupsLoading}
                                                                    onClick={() => toggleGroupIgnored(id, !ignored)}
                                                                    title={ignored
                                                                        ? textForLang(lang, 'Resume answering in this group', '\u6062\u590d\u5728\u8be5\u7fa4\u56de\u590d', '\u6062\u5fa9\u5728\u8a72\u7fa4\u56de\u8986')
                                                                        : textForLang(lang, 'Stop answering in this group', '\u4e0d\u518d\u5728\u8be5\u7fa4\u56de\u590d', '\u4e0d\u518d\u5728\u8a72\u7fa4\u56de\u8986')}
                                                                >
                                                                    {busy
                                                                        ? '…'
                                                                        : ignored
                                                                            ? textForLang(lang, 'Resume', '\u6062\u590d\u54cd\u5e94', '\u6062\u5fa9\u56de\u61c9')
                                                                            : textForLang(lang, 'Ignore', '\u4e0d\u54cd\u5e94', '\u4e0d\u56de\u61c9')}
                                                                </button>
                                                            </td>
                                                        </tr>
                                                    );
                                                })}
                                            </tbody>
                                        </table>
                                    </div>
                                </>
                            )}
                        </div>
                        <div className="im-groups-modal__footer">
                            <button
                                type="button"
                                className="im-settings-button"
                                onClick={loadGroups}
                                disabled={groupsLoading}
                            >
                                {textForLang(lang, 'Refresh', '\u5237\u65b0', '\u5237\u65b0')}
                            </button>
                            <button type="button" className="im-settings-button im-settings-button--primary" onClick={closeGroupInfo}>
                                {textForLang(lang, 'Close', '\u5173\u95ed', '\u95dc\u9589')}
                            </button>
                        </div>
                    </section>
                </div>
            )}
        </section>
    );
};

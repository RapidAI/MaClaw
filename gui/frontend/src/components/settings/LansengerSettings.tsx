import { KnowledgeListSources, ListLansengerGroups, LoadConfig, RestartLansenger, SelectVEAllowedDirectory, SetLansengerGroupAllowed, SetLansengerGroupIgnored, SetLansengerLocalMode } from '../../../wailsjs/go/main/App';
import { Suspense, lazy, type Dispatch, type SetStateAction, useCallback, useEffect, useRef, useState } from 'react';
import { corelib, main } from '../../../wailsjs/go/models';
import { ConnectionStatusBadge } from './ConnectionStatusBadge';
import { channelModeLabel, followLabel, localModeOptions, restartLabel, switchFailedLabel, textForLang, watchLabel } from './imSettingsShared';
import { useDialog } from '../CustomDialog';

/** Lazy so settings/IM does not eagerly pull the full watch editor + survey CSS. */
const UtilitiesWatchPanel = lazy(() =>
    import('../pages/UtilitiesWatchPanel')
        .then((m) => ({ default: m.UtilitiesWatchPanel }))
        .catch(() => ({
            default: function WatchPanelLoadFailed({
                isZh,
            }: {
                isZh: boolean;
                onBack: () => void;
                compactHeader?: boolean;
}) {
                return (
                    <p className="im-groups-modal__status" role="alert">
                        {isZh ? '加载盯人面板失败，请重试' : 'Failed to load people-watch panel'}
                    </p>
                );
            },
        })),
);

type LansengerSettingsProps = {
    config: corelib.AppConfig | null;
    setConfig: Dispatch<SetStateAction<corelib.AppConfig | null>>;
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
    /** Allowlist membership (used when group policy is allowlist). */
    allowed?: boolean;
    /** Local ignore/allow list only (not returned by Lansenger group fetch). */
    orphan?: boolean;
};

type LansengerGroupListPayload = {
    total?: number;
    groups?: LansengerGroupRow[];
};

type KnowledgeSourceRow = { id?: string; title?: string; kind?: string; status?: string };

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
    const { showAlert } = useDialog();
    const [groupsOpen, setGroupsOpen] = useState(false);
    const [groupsLoading, setGroupsLoading] = useState(false);
    const [groupsError, setGroupsError] = useState('');
    const [groups, setGroups] = useState<LansengerGroupRow[]>([]);
    const [groupsTotal, setGroupsTotal] = useState(0);
    const [ignoreBusyID, setIgnoreBusyID] = useState('');
    const [watchOpen, setWatchOpen] = useState(false);
    const [permissionSources, setPermissionSources] = useState<KnowledgeSourceRow[]>([]);
    const [permissionSourcesLoading, setPermissionSourcesLoading] = useState(false);
    const [permissionSourcesLoaded, setPermissionSourcesLoaded] = useState(false);
    const [permissionDirectoryBusy, setPermissionDirectoryBusy] = useState(false);
    const loadGenRef = useRef(0);
    const watchDialogRef = useRef<HTMLDivElement | null>(null);
    const watchCloseBtnRef = useRef<HTMLButtonElement | null>(null);
    const followBtnRef = useRef<HTMLButtonElement | null>(null);
    const hadWatchOpenRef = useRef(false);
    const isZh = !lang || lang.startsWith('zh');
    const lansengerConnected = lansengerStatus === 'connected';

    const closeGroupInfo = useCallback(() => {
        // Invalidate in-flight responses so they cannot overwrite a later open.
        loadGenRef.current += 1;
        setGroupsOpen(false);
        setGroupsLoading(false);
        setGroupsError('');
        setIgnoreBusyID('');
    }, []);

    const closeWatch = useCallback(() => setWatchOpen(false), []);

    const openWatch = useCallback(() => {
        if (lansengerStatus !== 'connected') return;
        // Mutual exclusivity with group-info dialog.
        closeGroupInfo();
        setWatchOpen(true);
    }, [closeGroupInfo, lansengerStatus]);

    const loadGroups = useCallback(() => {
        const gen = ++loadGenRef.current;
        setWatchOpen(false);
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

    const groupPolicy = String((config as any)?.lansenger_group_policy || 'open').toLowerCase();
    const isAllowlistPolicy = groupPolicy === 'allowlist' || groupPolicy === 'allow' || groupPolicy === 'whitelist';
    const groupPermissionSourceIDs: string[] = Array.isArray((config as any)?.lansenger_group_knowledge_source_ids) ? (config as any).lansenger_group_knowledge_source_ids : [];
    const groupPermissionDirectories: string[] = Array.isArray((config as any)?.lansenger_group_allowed_directories) ? (config as any).lansenger_group_allowed_directories : [];
    const allowAllDirectories = !!(config as any)?.lansenger_group_allow_all_directories;

    const loadPermissionSources = useCallback(() => {
        if (permissionSourcesLoading || permissionSourcesLoaded) return;
        setPermissionSourcesLoading(true);
        KnowledgeListSources({ limit: 5000, include_disabled: false })
            .then((items: KnowledgeSourceRow[] | null) => setPermissionSources(Array.isArray(items) ? items : []))
            .catch(() => setPermissionSources([]))
            .finally(() => {
                setPermissionSourcesLoaded(true);
                setPermissionSourcesLoading(false);
            });
    }, [permissionSourcesLoaded, permissionSourcesLoading]);

    useEffect(() => {
        if ((config as any)?.lansenger_enabled) loadPermissionSources();
    }, [config, loadPermissionSources]);

    const toggleKnowledgeSource = useCallback((id: string, checked: boolean) => {
        const next = checked
            ? [...new Set([...groupPermissionSourceIDs, id])]
            : groupPermissionSourceIDs.filter((item) => item !== id);
        saveRemoteConfigField({ lansenger_group_knowledge_source_ids: next } as any);
    }, [groupPermissionSourceIDs, saveRemoteConfigField]);

    const addPermissionDirectory = useCallback(async () => {
        setPermissionDirectoryBusy(true);
        try {
            const selected = await SelectVEAllowedDirectory();
            const path = String(selected || '').trim();
            if (!path) return;
            const exists = groupPermissionDirectories.some((item) => item.toLowerCase().replaceAll('\\', '/') === path.toLowerCase().replaceAll('\\', '/'));
            if (!exists) await saveRemoteConfigField({ lansenger_group_allowed_directories: [...groupPermissionDirectories, path] } as any);
        } catch (err) {
            void showAlert(wailsErrorMessage(err, textForLang(lang, 'Could not select directory', '无法选择目录', '無法選擇目錄')));
        } finally {
            setPermissionDirectoryBusy(false);
        }
    }, [groupPermissionDirectories, lang, saveRemoteConfigField, showAlert]);

    const removePermissionDirectory = useCallback((path: string) => {
        saveRemoteConfigField({ lansenger_group_allowed_directories: groupPermissionDirectories.filter((item) => item !== path) } as any);
    }, [groupPermissionDirectories, saveRemoteConfigField]);

    const toggleGroupResponse = useCallback((groupID: string, nextMuted: boolean) => {
        const id = String(groupID || '').trim();
        if (!id) return;
        setIgnoreBusyID(id);
        // Optimistic UI: open policy uses ignored denylist; allowlist uses allowed.
        setGroups((prev) => prev.map((g) => {
            if (g.group_id !== id) return g;
            if (isAllowlistPolicy) {
                return { ...g, allowed: !nextMuted, ignored: false };
            }
            return { ...g, ignored: nextMuted };
        }));
        const apply = isAllowlistPolicy
            ? SetLansengerGroupAllowed(id, !nextMuted)
            : SetLansengerGroupIgnored(id, nextMuted);
        apply
            .then(() => LoadConfig().then((c: any) => {
                // Normalize through AppConfig so group-policy fields are retained
                // the same way as PatchConfigFields responses.
                try {
                    setConfig(new corelib.AppConfig(c));
                } catch {
                    setConfig(c);
                }
            }).catch(() => {}))
            .then(() => {
                if (!nextMuted) {
                    // Drop list-only rows once the user re-enables them.
                    setGroups((prev) => prev.filter((g) => !(g.group_id === id && g.orphan)));
                }
            })
            .catch((err: unknown) => {
                setGroups((prev) => prev.map((g) => {
                    if (g.group_id !== id) return g;
                    if (isAllowlistPolicy) {
                        return { ...g, allowed: nextMuted };
                    }
                    return { ...g, ignored: !nextMuted };
                }));
                void showAlert(wailsErrorMessage(err, textForLang(lang, 'Failed to update group list', '更新群列表失败', '更新群列表失敗')));
            })
            .finally(() => setIgnoreBusyID((cur) => (cur === id ? '' : cur)));
    }, [isAllowlistPolicy, lang, setConfig, showAlert]);

    // One Escape handler for whichever sheet is open (watch stacks above groups).
    // Capture phase so nested inputs / other listeners do not swallow Escape first.
    useEffect(() => {
        if (!groupsOpen && !watchOpen) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== 'Escape') return;
            // Do not dismiss while IME is composing (common for zh input).
            if (e.isComposing || e.keyCode === 229) return;
            e.preventDefault();
            e.stopPropagation();
            if (watchOpen) {
                closeWatch();
                return;
            }
            closeGroupInfo();
        };
        window.addEventListener('keydown', onKey, true);
        return () => window.removeEventListener('keydown', onKey, true);
    }, [groupsOpen, watchOpen, closeGroupInfo, closeWatch]);

    // Lock page scroll while either sheet is open (fixed overlay still lets body scroll underneath).
    useEffect(() => {
        if (!groupsOpen && !watchOpen) return;
        const prev = document.body.style.overflow;
        document.body.style.overflow = 'hidden';
        return () => {
            document.body.style.overflow = prev;
        };
    }, [groupsOpen, watchOpen]);

    // Hide entry and unmount panel if connection drops.
    useEffect(() => {
        if (!lansengerConnected) {
            setWatchOpen(false);
        }
    }, [lansengerConnected]);

    // Focus close control on open; restore to the people-watch button on close.
    useEffect(() => {
        if (watchOpen) {
            hadWatchOpenRef.current = true;
            const id = requestAnimationFrame(() => {
                (watchCloseBtnRef.current || watchDialogRef.current)?.focus();
            });
            return () => cancelAnimationFrame(id);
        }
        if (hadWatchOpenRef.current) {
            hadWatchOpenRef.current = false;
            requestAnimationFrame(() => {
                followBtnRef.current?.focus();
            });
        }
    }, [watchOpen]);

    return (
        <section className="im-settings-card im-settings-channel">
            <p className="im-settings-description">
                {textForLang(lang, 'Configure Lansenger access to chat with the Agent over Lansenger.', '\u914d\u7f6e\u84dd\u4fe1\u63a5\u5165\uff0c\u7528\u84dd\u4fe1\u4e0e Agent \u5bf9\u8bdd\u3002', '\u914d\u7f6e\u85cd\u4fe1\u63a5\u5165\uff0c\u7528\u85cd\u4fe1\u8207 Agent \u5c0d\u8a71\u3002')}
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
                                void showAlert(String(err?.message || err || restartLabel(lang)));
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
                {lansengerConnected && (
                    <button
                        ref={followBtnRef}
                        type="button"
                        className="im-settings-button"
                        data-testid="lansenger-follow-button"
                        aria-label={followLabel(lang)}
                        aria-haspopup="dialog"
                        aria-expanded={watchOpen}
                        onClick={openWatch}
                        title={textForLang(
                            lang,
                            'Watch people: log speech, keyword replies or CLI',
                            '盯人：记录指定成员发言，关键字固定回复或 CLI',
                            '盯人：記錄指定成員發言，關鍵字固定回覆或 CLI',
                        )}
                    >
                        {followLabel(lang)}
                    </button>
                )}
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
                                    void showAlert(String(err?.message || err || switchFailedLabel(lang)));
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

            {(config as any)?.lansenger_enabled && (
                <div className="im-settings-lansenger-group">
                    <label className="im-settings-field im-settings-lansenger-group__policy">
                        <span>{textForLang(lang, 'Group policy', '\u7fa4\u804a\u7b56\u7565', '\u7fa4\u804a\u7b56\u7565')}</span>
                        <select
                            value={(config as any)?.lansenger_group_policy || 'open'}
                            onChange={(e) => saveRemoteConfigField({ lansenger_group_policy: e.target.value } as any)}
                            aria-label={textForLang(lang, 'Group policy', '\u7fa4\u804a\u7b56\u7565', '\u7fa4\u804a\u7b56\u7565')}
                        >
                            <option value="open">{textForLang(lang, 'Open (all groups)', '\u5f00\u653e\uff08\u6240\u6709\u7fa4\uff09', '\u958b\u653e\uff08\u6240\u6709\u7fa4\uff09')}</option>
                            <option value="allowlist">{textForLang(lang, 'Allowlist only', '\u4ec5\u5141\u8bb8\u5217\u8868', '\u50c5\u5141\u8a31\u5217\u8868')}</option>
                            <option value="disabled">{textForLang(lang, 'Disabled', '\u7981\u7528\u7fa4\u804a', '\u7981\u7528\u7fa4\u804a')}</option>
                        </select>
                        {isAllowlistPolicy ? (
                            <span className="im-settings-field-hint" style={{ display: 'block', marginTop: 4, opacity: 0.8, fontSize: 12 }}>
                                {textForLang(
                                    lang,
                                    'Allowlist mode: open Group Info and mark groups as Allowed. Empty allowlist means no group replies.',
                                    '允许列表模式：在「群信息」中将群标记为允许。列表为空时不会回复任何群。',
                                    '允許列表模式：在「群資訊」中將群標記為允許。列表為空時不會回覆任何群。',
                                )}
                            </span>
                        ) : null}
                    </label>
                    {/* Keep toggles in one flex row so auto-fit 3-col grids cannot push "Respond to @all" alone into a third column. */}
                    <div className="im-settings-toggle-row" role="group" aria-label={textForLang(lang, 'Group reply options', '\u7fa4\u56de\u590d\u9009\u9879', '\u7fa4\u56de\u8986\u9078\u9805')}>
                        <label className="im-settings-toggle">
                            <input
                                type="checkbox"
                                checked={(config as any)?.lansenger_require_mention !== false}
                                onChange={(e) => saveRemoteConfigField({ lansenger_require_mention: e.target.checked } as any)}
                            />
                            <span title={textForLang(lang, 'Only respond when @mentioned in groups', '\u7fa4\u804a\u4ec5\u5728 @\u673a\u5668\u4eba \u65f6\u56de\u590d', '\u7fa4\u804a\u50c5\u5728 @\u6a5f\u5668\u4eba \u6642\u56de\u8986')}>
                                {textForLang(lang, 'Require @mention', '\u9700\u8981 @\u63d0\u53ca', '\u9700\u8981 @\u63d0\u53ca')}
                            </span>
                        </label>
                        <label className="im-settings-toggle">
                            <input
                                type="checkbox"
                                checked={!!(config as any)?.lansenger_respond_to_at_all}
                                onChange={(e) => saveRemoteConfigField({ lansenger_respond_to_at_all: e.target.checked } as any)}
                            />
                            <span title={textForLang(lang, 'Also respond to @all when require-mention is on', '\u9700\u8981@\u65f6\u4e5f\u54cd\u5e94 @\u6240\u6709\u4eba', '\u9700\u8981@\u6642\u4e5f\u97ff\u61c9 @\u6240\u6709\u4eba')}>
                                {textForLang(lang, 'Respond to @all', '\u54cd\u5e94 @\u6240\u6709\u4eba', '\u97ff\u61c9 @\u6240\u6709\u4eba')}
                            </span>
                        </label>
                        <label className="im-settings-toggle">
                            <input
                                type="checkbox"
                                checked={!!(config as any)?.lansenger_auto_mention_reply}
                                onChange={(e) => saveRemoteConfigField({ lansenger_auto_mention_reply: e.target.checked } as any)}
                            />
                            <span title={textForLang(lang, 'Auto @ the asker in replies (native reminder API)', '\u56de\u590d\u65f6\u81ea\u52a8 @\u53d1\u9001\u8005\uff08\u539f\u751f reminder\uff09', '\u56de\u8986\u6642\u81ea\u52d5 @\u767c\u9001\u8005')}>
                                {textForLang(lang, 'Auto @ reply', '\u56de\u590d\u81ea\u52a8 @', '\u56de\u8986\u81ea\u52d5 @')}
                            </span>
                        </label>
                        <label className="im-settings-toggle">
                            <input
                                type="checkbox"
                                checked={!!(config as any)?.lansenger_auto_quote_reply}
                                onChange={(e) => saveRemoteConfigField({ lansenger_auto_quote_reply: e.target.checked } as any)}
                            />
                            <span title={textForLang(lang, 'Native quote via refMsgId (preferred over text quote)', '\u4f7f\u7528\u539f\u751f\u5f15\u7528\u56de\u590d\uff08refMsgId\uff09', '\u4f7f\u7528\u539f\u751f\u5f15\u7528\u56de\u8986\uff08refMsgId\uff09')}>
                                {textForLang(lang, 'Auto quote reply', '\u81ea\u52a8\u5f15\u7528\u56de\u590d', '\u81ea\u52d5\u5f15\u7528\u56de\u8986')}
                            </span>
                        </label>
                    </div>

                    <section className="im-group-permissions" aria-labelledby="lansenger-group-permissions-title">
                        <div className="im-group-permissions__header">
                            <div>
                                <strong id="lansenger-group-permissions-title">{textForLang(lang, 'Group permissions', '群聊权限', '群聊權限')}</strong>
                                <p>{textForLang(lang, 'Only these sources and directories are available while the bot replies in a group. Private chats are unaffected.', '机器人在群中回复时仅可使用此处授权的知识库和本地目录；私聊不受影响。', '機器人在群組回覆時僅可使用此處授權的知識庫與本機目錄；私訊不受影響。')}</p>
                            </div>
                        </div>

                        <div className="im-group-permissions__section">
                            <div className="im-group-permissions__label-row">
                                <span>{textForLang(lang, 'Knowledge base scope', '知识库访问范围', '知識庫存取範圍')}</span>
                                <button type="button" className="im-settings-button" onClick={() => { setPermissionSourcesLoaded(false); loadPermissionSources(); }} disabled={permissionSourcesLoading}>
                                    {textForLang(lang, 'Refresh', '刷新', '重新整理')}
                                </button>
                            </div>
                            <p className="im-group-permissions__hint">{textForLang(lang, 'No source selected means the group bot cannot access knowledge.', '未选择任何知识库时，群机器人不能访问知识库。', '未選擇任何知識庫時，群機器人不能存取知識庫。')}</p>
                            <div className="im-group-permissions__sources">
                                {permissionSourcesLoading ? <span>{textForLang(lang, 'Loading…', '加载中…', '載入中…')}</span> : permissionSources.length === 0 ? <span>{textForLang(lang, 'No available knowledge sources', '暂无可授权的知识库来源', '暫無可授權的知識庫來源')}</span> : permissionSources.map((source) => {
                                    const id = String(source.id || '');
                                    if (!id) return null;
                                    return <label key={id} className="im-settings-toggle im-group-permissions__source">
                                        <input type="checkbox" checked={groupPermissionSourceIDs.includes(id)} onChange={(e) => toggleKnowledgeSource(id, e.target.checked)} />
                                        <span><b>{source.title || id}</b><small>{id}{source.kind ? ` · ${source.kind}` : ''}</small></span>
                                    </label>;
                                })}
                            </div>
                        </div>

                        <div className="im-group-permissions__section">
                            <label className="im-settings-toggle">
                                <input type="checkbox" checked={allowAllDirectories} onChange={(e) => saveRemoteConfigField({ lansenger_group_allow_all_directories: e.target.checked } as any)} />
                                <span>{textForLang(lang, 'Allow all directories', '允许所有目录', '允許所有目錄')}</span>
                            </label>
                            <p className="im-group-permissions__hint">{textForLang(lang, 'Disabled by default. Enable only when the group bot may access every local directory.', '默认不启用。仅当允许群机器人访问全部本地目录时才勾选。', '預設不啟用。僅當允許群機器人存取所有本機目錄時才勾選。')}</p>
                            {!allowAllDirectories && <>
                                <div className="im-group-permissions__label-row">
                                    <span>{textForLang(lang, 'Allowed local directories', '可访问的本地目录', '可存取的本機目錄')}</span>
                                    <button type="button" className="im-settings-button" onClick={addPermissionDirectory} disabled={permissionDirectoryBusy}>{textForLang(lang, 'Add directory', '添加目录', '新增目錄')}</button>
                                </div>
                                {groupPermissionDirectories.length === 0 ? <p className="im-group-permissions__empty">{textForLang(lang, 'No local directories are allowed.', '未授权任何本地目录。', '尚未授權任何本機目錄。')}</p> : <ul className="im-group-permissions__directories">
                                    {groupPermissionDirectories.map((path) => <li key={path}><code>{path}</code><button type="button" className="im-settings-button" onClick={() => removePermissionDirectory(path)}>{textForLang(lang, 'Remove', '移除', '移除')}</button></li>)}
                                </ul>}
                            </>}
                        </div>
                    </section>
                </div>
            )}

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
                                        {isAllowlistPolicy
                                            ? textForLang(
                                                lang,
                                                'Allowlist mode: only “Allowed” groups get replies. The bot stays in every Lansenger group either way.',
                                                '允许列表模式：仅「允许」的群会得到回复。机器人仍留在蓝信群中。',
                                                '允許列表模式：僅「允許」的群會得到回覆。機器人仍留在藍信群中。',
                                            )
                                            : textForLang(
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
                                                    // muted = bot will not answer this group under current policy
                                                    const muted = isAllowlistPolicy ? !g.allowed : !!g.ignored;
                                                    const busy = ignoreBusyID === id;
                                                    return (
                                                        <tr key={id || `row-${idx}`} data-ignored={muted ? 'true' : undefined}>
                                                            <td className="im-groups-table__name">
                                                                {g.name || id || '—'}
                                                                {muted ? (
                                                                    <span className="im-groups-badge im-groups-badge--muted">
                                                                        {isAllowlistPolicy
                                                                            ? textForLang(lang, 'Not allowed', '\u672a\u5141\u8bb8', '\u672a\u5141\u8a31')
                                                                            : textForLang(lang, 'Ignored', '\u4e0d\u54cd\u5e94', '\u4e0d\u56de\u61c9')}
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
                                                                    className={muted ? 'im-settings-button im-settings-button--primary' : 'im-settings-button'}
                                                                    disabled={!id || busy || groupsLoading}
                                                                    onClick={() => toggleGroupResponse(id, !muted)}
                                                                    title={muted
                                                                        ? textForLang(lang, 'Resume answering in this group', '\u6062\u590d\u5728\u8be5\u7fa4\u56de\u590d', '\u6062\u5fa9\u5728\u8a72\u7fa4\u56de\u8986')
                                                                        : textForLang(lang, 'Stop answering in this group', '\u4e0d\u518d\u5728\u8be5\u7fa4\u56de\u590d', '\u4e0d\u518d\u5728\u8a72\u7fa4\u56de\u8986')}
                                                                >
                                                                    {busy
                                                                        ? '…'
                                                                        : muted
                                                                            ? (isAllowlistPolicy
                                                                                ? textForLang(lang, 'Allow', '\u5141\u8bb8', '\u5141\u8a31')
                                                                                : textForLang(lang, 'Resume', '\u6062\u590d\u54cd\u5e94', '\u6062\u5fa9\u56de\u61c9'))
                                                                            : (isAllowlistPolicy
                                                                                ? textForLang(lang, 'Disallow', '\u79fb\u51fa\u5141\u8bb8', '\u79fb\u51fa\u5141\u8a31')
                                                                                : textForLang(lang, 'Ignore', '\u4e0d\u54cd\u5e94', '\u4e0d\u56de\u61c9'))}
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

            {watchOpen && lansengerConnected && (
                <div
                    className="im-groups-modal-overlay"
                    role="presentation"
                    onClick={closeWatch}
                >
                    <div
                        ref={watchDialogRef}
                        className="im-groups-modal im-watch-modal"
                        role="dialog"
                        aria-modal="true"
                        aria-labelledby="lansenger-follow-dialog-title"
                        tabIndex={-1}
                        onClick={(e) => e.stopPropagation()}
                    >
                        <div className="im-groups-modal__header im-watch-modal__header">
                            <strong id="lansenger-follow-dialog-title">{followLabel(lang)}</strong>
                            <button
                                ref={watchCloseBtnRef}
                                type="button"
                                className="im-groups-modal__close"
                                aria-label={textForLang(lang, 'Close', '关闭', '關閉')}
                                onClick={closeWatch}
                            >
                                ×
                            </button>
                        </div>
                        <div className="im-watch-modal__body">
                            <Suspense
                                fallback={
                                    <p className="im-groups-modal__status">
                                        {textForLang(lang, 'Loading…', '加载中…', '載入中…')}
                                    </p>
                                }
                            >
                                <UtilitiesWatchPanel isZh={isZh} onBack={closeWatch} compactHeader />
                            </Suspense>
                        </div>
                    </div>
                </div>
            )}
        </section>
    );
};

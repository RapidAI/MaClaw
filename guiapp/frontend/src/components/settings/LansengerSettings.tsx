import { DeleteLansengerBot, GetLansengerBotStatus, KnowledgeListSources, ListExperts, ListLansengerBots, ListLansengerGroups, ListLansengerGroupsForBot, LoadConfig, RestartLansenger, RestartLansengerBot, SaveLansengerBot, SelectVEAllowedDirectory, SelectWorkingDir, SetLansengerBotGroupAllowed, SetLansengerBotGroupFileMaxBytes, SetLansengerBotGroupIgnored, SetLansengerGroupAllowed, SetLansengerGroupFileMaxBytes, SetLansengerGroupIgnored, SetLansengerLocalMode } from '../../../wailsjs/go/main/App';
import { Suspense, lazy, type Dispatch, type SetStateAction, useCallback, useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { corelib, main } from '../../../wailsjs/go/models';
import { parseExpertListJSON, type ExpertDefinition } from '../ai/expertTypes';
import { ConnectionStatusBadge } from './ConnectionStatusBadge';
import { channelModeLabel, followLabel, localModeOptions, restartLabel, switchFailedLabel, textForLang, watchLabel } from './imSettingsShared';
import { useDialog } from '../CustomDialog';
import { EventsOn } from '../../../wailsjs/runtime';
import { usePortalThemeAttributes } from '../../hooks/usePortalThemeAttributes';
import { useSafeBackdropDismiss } from '../../hooks/useSafeBackdropDismiss';

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
    file_max_bytes?: number;
};

type LansengerGroupListPayload = {
    total?: number;
    groups?: LansengerGroupRow[];
};

type KnowledgeSourceRow = { id?: string; title?: string; kind?: string; status?: string };

type LansengerBotProfileView = {
    id: string;
    name: string;
    enabled: boolean;
    app_id: string;
    gateway_url?: string;
    wss_url?: string;
    assistant_mode?: string;
    expert_id?: string;
    initial_prompt?: string;
    working_directory?: string;
    document_directories?: string[];
    knowledge_source_ids?: string[];
    group_policy?: string;
    require_mention?: boolean;
    auto_mention_reply?: boolean;
    auto_quote_reply?: boolean;
    allow_web_search?: boolean;
    allow_all_directories?: boolean;
    allowed_directories?: string[];
    answer_cache?: { enabled?: boolean; ttl_days?: number };
    status?: string;
    status_reason?: string;
    secret_configured?: boolean;
};

type LansengerBotDraft = LansengerBotProfileView & { app_secret?: string };

const answerCacheDefaults = { enabled: true, ttl_days: 0 };
const modalFocusableSelector = 'button:not([disabled]):not([tabindex="-1"]), input:not([disabled]):not([tabindex="-1"]), select:not([disabled]):not([tabindex="-1"]), textarea:not([disabled]):not([tabindex="-1"]), [href]:not([tabindex="-1"])';

function normalizedAnswerCache(value: any) {
    const ttl = Number(value?.ttl_days);
    return {
        enabled: value?.enabled !== false,
        ttl_days: Number.isFinite(ttl) ? Math.max(0, Math.min(365, Math.round(ttl))) : answerCacheDefaults.ttl_days,
    };
}

function botDraftFromView(profile: LansengerBotProfileView): LansengerBotDraft {
    return {
        ...profile,
        document_directories: [...(profile.document_directories || [])],
        knowledge_source_ids: [...(profile.knowledge_source_ids || [])],
        allowed_directories: [...(profile.allowed_directories || [])],
    };
}

function newLansengerBotDraft(): LansengerBotDraft {
    const randomSuffix = typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function'
        ? crypto.randomUUID().slice(0, 8)
        : `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
    const id = `bot-${randomSuffix}`;
    return {
        id, name: '', enabled: false, app_id: '', app_secret: '', assistant_mode: 'general',
        group_policy: 'open', require_mention: true, document_directories: [], knowledge_source_ids: [], allowed_directories: [], answer_cache: answerCacheDefaults,
    };
}

const lansengerBotIDPattern = /^[A-Za-z0-9._-]{1,128}$/;

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
    const [fileLimitBusyID, setFileLimitBusyID] = useState('');
    const [fileLimitDrafts, setFileLimitDrafts] = useState<Record<string, string>>({});
    const [watchOpen, setWatchOpen] = useState(false);
    const [watchBotProfileID, setWatchBotProfileID] = useState('');
    const [permissionSources, setPermissionSources] = useState<KnowledgeSourceRow[]>([]);
    const [permissionSourcesLoading, setPermissionSourcesLoading] = useState(false);
    const [permissionSourcesLoaded, setPermissionSourcesLoaded] = useState(false);
    const [permissionDirectoryBusy, setPermissionDirectoryBusy] = useState(false);
    const [workingDirectorySelecting, setWorkingDirectorySelecting] = useState(false);
    const [bots, setBots] = useState<LansengerBotProfileView[]>([]);
    const [botDraft, setBotDraft] = useState<LansengerBotDraft | null>(null);
    const [botDraftIsNew, setBotDraftIsNew] = useState(false);
    const [botsLoading, setBotsLoading] = useState(false);
    const [botProfilesAvailable, setBotProfilesAvailable] = useState<boolean | null>(null);
    const [botSaving, setBotSaving] = useState(false);
    const [selectedBotStatus, setSelectedBotStatus] = useState('disconnected');
    const [selectedBotStatusReason, setSelectedBotStatusReason] = useState('');
    const [availableExperts, setAvailableExperts] = useState<ExpertDefinition[]>([]);
    const [expertsLoading, setExpertsLoading] = useState(false);
    const [expertsLoadAttempted, setExpertsLoadAttempted] = useState(false);
    const loadGenRef = useRef(0);
    const botDraftIsNewRef = useRef(false);
    const expertsInFlightRef = useRef(false);
    const workingDirectorySelectionInFlightRef = useRef(false);
    const workingDirectorySelectionDraftIDRef = useRef('');
    const mountedRef = useRef(true);
    const groupsDialogRef = useRef<HTMLElement | null>(null);
    const groupsCloseBtnRef = useRef<HTMLButtonElement | null>(null);
    const groupsTriggerRef = useRef<HTMLButtonElement | null>(null);
    const hadGroupsOpenRef = useRef(false);
    const watchDialogRef = useRef<HTMLDivElement | null>(null);
    const watchCloseBtnRef = useRef<HTMLButtonElement | null>(null);
    const followBtnRef = useRef<HTMLButtonElement | null>(null);
    const hadWatchOpenRef = useRef(false);
    const isZh = !lang || lang.startsWith('zh');
    const lansengerConnected = lansengerStatus === 'connected';
    const useBotProfiles = botProfilesAvailable === true;
    const selectedBotIsSaved = !!botDraft && !botDraftIsNew && bots.some((bot) => bot.id === botDraft.id);
    const answerCache = normalizedAnswerCache(botDraft?.answer_cache);
    const answerCacheReuseActive = answerCache.enabled && answerCache.ttl_days > 0;
    const normalizedDraftBotID = botDraft?.id.trim().toLowerCase() || '';
    const newDraftHasDuplicateID = botDraftIsNew && normalizedDraftBotID !== '' && bots.some((bot) => bot.id.trim().toLowerCase() === normalizedDraftBotID);
    const newDraftHasInvalidID = botDraftIsNew && !lansengerBotIDPattern.test(botDraft?.id.trim() || '');
    const newDraftBotIDError = newDraftHasDuplicateID
        ? textForLang(lang, 'This Bot ID is already in use. Choose a different ID.', '该机器人 ID 已被使用，请更换一个 ID。', '此機器人 ID 已被使用，請更換一個 ID。')
        : newDraftHasInvalidID
            ? textForLang(lang, 'Use 1–128 letters, digits, dots, underscores, or hyphens.', '请输入 1–128 个字母、数字、点、下划线或连字符。', '請輸入 1–128 個字母、數字、點、底線或連字號。')
            : '';
    const setNewBotDraftState = useCallback((isNew: boolean) => {
        botDraftIsNewRef.current = isNew;
        setBotDraftIsNew(isNew);
    }, []);

    useEffect(() => {
        mountedRef.current = true;
        return () => {
            mountedRef.current = false;
            workingDirectorySelectionDraftIDRef.current = '';
        };
    }, []);
    const discardBotDraft = useCallback(() => {
        workingDirectorySelectionDraftIDRef.current = '';
        setNewBotDraftState(false);
        setBotDraft(bots.length ? botDraftFromView(bots[0]) : null);
    }, [bots, setNewBotDraftState]);

    const loadBots = useCallback(async () => {
        setBotsLoading(true);
        try {
            const next = await ListLansengerBots() as LansengerBotProfileView[] | null;
            setBotProfilesAvailable(true);
            const list = Array.isArray(next) ? next : [];
            setBots(list);
            setBotDraft((current) => {
                if (current) {
                    const updated = list.find((item) => item.id === current.id);
                    if (updated) {
                        return botDraftFromView(updated);
                    }
                    return botDraftIsNewRef.current ? current : null;
                }
                return list.length ? botDraftFromView(list[0]) : null;
            });
        } catch {
            // Leave legacy settings available if a desktop build predates the API.
            setBotProfilesAvailable(false);
            setBots([]);
        } finally {
            setBotsLoading(false);
        }
    }, []);

    useEffect(() => { void loadBots(); }, [loadBots]);

    // Expert bindings are selected from the current local catalog. The backend
    // independently validates the ID on save, preventing stale UI from binding
    // a bot to an expert that has since been deleted.
    const loadAvailableExperts = useCallback(async () => {
        if (expertsInFlightRef.current) return;
        expertsInFlightRef.current = true;
        setExpertsLoading(true);
        try {
            const raw = await ListExperts();
            const next = parseExpertListJSON(raw);
            setAvailableExperts(next);
            setBotDraft((current) => {
                if (!current || current.assistant_mode !== 'expert' || !current.expert_id) return current;
                return next.some((expert) => expert.id === current.expert_id)
                    ? current
                    : { ...current, expert_id: '' };
            });
        } catch {
            setAvailableExperts([]);
        } finally {
            expertsInFlightRef.current = false;
            setExpertsLoading(false);
            setExpertsLoadAttempted(true);
        }
    }, []);

    useEffect(() => {
        if (botDraft?.assistant_mode === 'expert') void loadAvailableExperts();
    }, [botDraft?.assistant_mode, botDraft?.id, loadAvailableExperts]);

    useEffect(() => {
        if (!botProfilesAvailable || !botDraft?.id || !selectedBotIsSaved) {
            setSelectedBotStatus('disconnected');
            setSelectedBotStatusReason('');
            return;
        }
        const selected = bots.find((bot) => bot.id === botDraft.id);
        if (selected?.status === 'degraded') {
            setSelectedBotStatus('degraded');
            setSelectedBotStatusReason(selected.status_reason || '');
            return;
        }
        let active = true;
        setSelectedBotStatusReason('');
        GetLansengerBotStatus(botDraft.id).then((status) => {
            if (!active) return;
            setSelectedBotStatus(status);
            setSelectedBotStatusReason(status === 'degraded' ? textForLang(lang, 'The selected AI expert is unavailable. Choose another expert and save this bot.', '绑定的 AI 专家已不可用，请重新选择并保存该机器人。', '綁定的 AI 專家已不可用，請重新選擇並儲存此機器人。') : '');
        }).catch(() => { if (active) { setSelectedBotStatus('disconnected'); setSelectedBotStatusReason(''); } });
        return () => { active = false; };
    }, [botDraft?.id, botProfilesAvailable, bots, lang, selectedBotIsSaved]);

    // The app-level status event is deliberately aggregate-only. Subscribe to
    // profile transitions too so the selected bot's Watch entry updates as soon
    // as its own gateway changes state.
    useEffect(() => {
        if (!useBotProfiles || !botDraft?.id || !selectedBotIsSaved) return;
        const botID = botDraft.id;
        const unsubscribe = EventsOn('lansenger-bot-status-changed', (changedID: string, status: string) => {
            if (String(changedID || '').trim() !== botID) return;
            setSelectedBotStatus(String(status || 'disconnected'));
            setSelectedBotStatusReason('');
        });
        return () => {
            unsubscribe();
        };
    }, [botDraft?.id, selectedBotIsSaved, useBotProfiles]);

    const saveBot = useCallback(async () => {
        if (!botDraft || botSaving) return;
        if (newDraftHasDuplicateID || newDraftHasInvalidID) {
            void showAlert(newDraftBotIDError);
            return;
        }
        setBotSaving(true);
        try {
            const saved = await SaveLansengerBot({
                ...botDraft,
                app_secret: botDraft.app_secret || '',
                document_directories: (botDraft.document_directories || []).map((path) => path.trim()).filter(Boolean),
                knowledge_source_ids: (botDraft.knowledge_source_ids || []).map((id) => id.trim()).filter(Boolean),
                allowed_directories: (botDraft.allowed_directories || []).map((path) => path.trim()).filter(Boolean),
            } as corelib.LansengerBotProfile) as unknown as LansengerBotProfileView;
            setNewBotDraftState(false);
            setBotDraft(botDraftFromView(saved));
            await loadBots();
            LoadConfig().then((next: any) => setConfig(next)).catch(() => {});
        } catch (err) {
            void showAlert(wailsErrorMessage(err, textForLang(lang, 'Failed to save bot', '保存机器人失败', '儲存機器人失敗')));
        } finally {
            setBotSaving(false);
        }
    }, [botDraft, botSaving, lang, loadBots, newDraftBotIDError, newDraftHasDuplicateID, newDraftHasInvalidID, setConfig, setNewBotDraftState, showAlert]);

    const deleteBot = useCallback(async () => {
        if (!botDraft || botSaving) return;
        setBotSaving(true);
        try {
            await DeleteLansengerBot(botDraft.id);
            setNewBotDraftState(false);
            setBotDraft(null);
            await loadBots();
        } catch (err) {
            void showAlert(wailsErrorMessage(err, textForLang(lang, 'Failed to delete bot', '删除机器人失败', '刪除機器人失敗')));
        } finally {
            setBotSaving(false);
        }
    }, [botDraft, botSaving, lang, loadBots, setNewBotDraftState, showAlert]);

    const addBotDirectory = useCallback(async () => {
        if (!botDraft) return;
        try {
            const selected = String(await SelectVEAllowedDirectory() || '').trim();
            if (!selected) return;
            setBotDraft((current) => current ? {
                ...current,
                document_directories: [...new Set([...(current.document_directories || []), selected])],
            } : current);
        } catch (err) {
            void showAlert(wailsErrorMessage(err, textForLang(lang, 'Could not select directory', '无法选择目录', '無法選擇目錄')));
        }
    }, [botDraft, lang, showAlert]);

    const addBotAllowedDirectory = useCallback(async () => {
        if (!botDraft) return;
        try {
            const selected = String(await SelectVEAllowedDirectory() || '').trim();
            if (!selected) return;
            setBotDraft((current) => current ? {
                ...current,
                allowed_directories: [...new Set([...(current.allowed_directories || []), selected])],
            } : current);
        } catch (err) {
            void showAlert(wailsErrorMessage(err, textForLang(lang, 'Could not select directory', '无法选择目录', '無法選擇目錄')));
        }
    }, [botDraft, lang, showAlert]);

    const selectBotWorkingDirectory = useCallback(async () => {
        if (!botDraft || workingDirectorySelectionInFlightRef.current) return;
        const draftID = botDraft.id;
        workingDirectorySelectionInFlightRef.current = true;
        workingDirectorySelectionDraftIDRef.current = draftID;
        setWorkingDirectorySelecting(true);
        try {
            const selected = String(await SelectWorkingDir() || '').trim();
            if (!selected) return;
            if (!mountedRef.current || workingDirectorySelectionDraftIDRef.current !== draftID) return;
            setBotDraft((current) => current?.id === draftID
                ? { ...current, working_directory: selected }
                : current);
        } catch (err) {
            void showAlert(wailsErrorMessage(err, textForLang(lang, 'Could not select directory', '无法选择目录', '無法選擇目錄')));
        } finally {
            workingDirectorySelectionInFlightRef.current = false;
            workingDirectorySelectionDraftIDRef.current = '';
            if (mountedRef.current) setWorkingDirectorySelecting(false);
        }
    }, [botDraft, lang, showAlert]);

    const toggleBotKnowledgeSource = useCallback((id: string, checked: boolean) => {
        if (!botDraft) return;
        const current = botDraft.knowledge_source_ids || [];
        setBotDraft({
            ...botDraft,
            knowledge_source_ids: checked ? [...new Set([...current, id])] : current.filter((item) => item !== id),
        });
    }, [botDraft]);

    const closeGroupInfo = useCallback(() => {
        // Invalidate in-flight responses so they cannot overwrite a later open.
        loadGenRef.current += 1;
        setGroupsOpen(false);
        setGroupsLoading(false);
        setGroupsError('');
        setIgnoreBusyID('');
    }, []);

    const closeWatch = useCallback(() => {
        setWatchOpen(false);
        setWatchBotProfileID('');
    }, []);

    const openWatch = useCallback((botProfileID = '') => {
        if ((botProfileID ? selectedBotStatus : lansengerStatus) !== 'connected') return;
        // Mutual exclusivity with group-info dialog.
        closeGroupInfo();
        setWatchBotProfileID(botProfileID);
        setWatchOpen(true);
    }, [closeGroupInfo, lansengerStatus, selectedBotStatus]);
    const portalThemeAttributes = usePortalThemeAttributes();
    const { backdropProps: groupsBackdropProps, dialogProps: groupsDialogProps } = useSafeBackdropDismiss<HTMLElement>(closeGroupInfo);
    const { backdropProps: watchBackdropProps, dialogProps: watchDialogProps } = useSafeBackdropDismiss(closeWatch);

    const loadGroups = useCallback(() => {
        const gen = ++loadGenRef.current;
        setWatchOpen(false);
        setGroupsOpen(true);
        setGroupsLoading(true);
        setGroupsError('');
        const fetchGroups = useBotProfiles && botDraft?.id ? ListLansengerGroupsForBot(botDraft.id) : ListLansengerGroups();
        fetchGroups
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
    }, [botDraft?.id, lang, useBotProfiles]);

    const openGroupInfo = useCallback((trigger: HTMLButtonElement) => {
        groupsTriggerRef.current = trigger;
        loadGroups();
    }, [loadGroups]);

    // A profile owns its own group policy. Reading the legacy config here would
    // make the group dialog display and mutate the wrong policy for non-default
    // bots.
    const groupPolicy = String((useBotProfiles ? botDraft?.group_policy : (config as any)?.lansenger_group_policy) || 'open').toLowerCase();
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
        const apply = useBotProfiles && botDraft?.id
            ? (isAllowlistPolicy ? SetLansengerBotGroupAllowed(botDraft.id, id, !nextMuted) : SetLansengerBotGroupIgnored(botDraft.id, id, nextMuted))
            : (isAllowlistPolicy ? SetLansengerGroupAllowed(id, !nextMuted) : SetLansengerGroupIgnored(id, nextMuted));
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
    }, [botDraft?.id, isAllowlistPolicy, lang, setConfig, showAlert, useBotProfiles]);

    const saveGroupFileLimit = useCallback(async (groupID: string, megabytes: number) => {
        const id = String(groupID || '').trim();
        if (!id) return;
        const maxBytes = Math.max(0, Math.floor(Number.isFinite(megabytes) ? megabytes : 0)) * 1024 * 1024;
        setFileLimitBusyID(id);
        try {
            if (useBotProfiles && botDraft?.id) {
                await SetLansengerBotGroupFileMaxBytes(botDraft.id, id, maxBytes);
            } else {
                await SetLansengerGroupFileMaxBytes(id, maxBytes);
            }
            setGroups((prev) => prev.map((g) => g.group_id === id ? { ...g, file_max_bytes: maxBytes } : g));
            setFileLimitDrafts((prev) => ({ ...prev, [id]: String(maxBytes / (1024 * 1024)) }));
        } catch (err) {
            void showAlert(wailsErrorMessage(err, textForLang(lang, 'Failed to save file limit', '保存文件上限失败', '儲存檔案上限失敗')));
        } finally {
            setFileLimitBusyID('');
        }
    }, [botDraft?.id, lang, showAlert, useBotProfiles]);

    // One Escape handler for whichever sheet is open (watch stacks above groups).
    // Capture phase so nested inputs / other listeners do not swallow Escape first.
    useEffect(() => {
        if (!groupsOpen && !watchOpen) return;
        const onKey = (e: KeyboardEvent) => {
            const activeDialog = watchOpen ? watchDialogRef.current : groupsDialogRef.current;
            // A confirmation/alert opened above this dialog owns keyboard
            // input. Its focus lives outside the portalled parent dialog.
            if (e.target instanceof Node && !activeDialog?.contains(e.target)) return;
            if (e.key === 'Tab') {
                const focusable = activeDialog?.querySelectorAll<HTMLElement>(modalFocusableSelector);
                if (!focusable?.length) return;
                const items = Array.from(focusable);
                const currentIndex = items.indexOf(document.activeElement as HTMLElement);
                const nextIndex = e.shiftKey
                    ? (currentIndex <= 0 ? items.length - 1 : currentIndex - 1)
                    : (currentIndex === items.length - 1 ? 0 : currentIndex + 1);
                e.preventDefault();
                e.stopPropagation();
                items[nextIndex].focus();
                return;
            }
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
        const activeWatchConnected = watchBotProfileID ? selectedBotStatus === 'connected' : lansengerConnected;
        if (watchOpen && !activeWatchConnected) {
            setWatchOpen(false);
            setWatchBotProfileID('');
        }
    }, [lansengerConnected, selectedBotStatus, watchBotProfileID, watchOpen]);

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

    // Give keyboard users a stable entry point and return them to the action
    // that opened Group Info. Do not steal focus when moving directly into
    // the people-watch dialog.
    useEffect(() => {
        if (groupsOpen) {
            hadGroupsOpenRef.current = true;
            const id = requestAnimationFrame(() => {
                (groupsCloseBtnRef.current || groupsDialogRef.current)?.focus();
            });
            return () => cancelAnimationFrame(id);
        }
        if (hadGroupsOpenRef.current && !watchOpen) {
            hadGroupsOpenRef.current = false;
            requestAnimationFrame(() => {
                groupsTriggerRef.current?.focus();
            });
        }
    }, [groupsOpen, watchOpen]);

    return (
        <section className="im-settings-card im-settings-channel">
            <p className="im-settings-description">
                {textForLang(lang, 'Configure Lansenger access to chat with the Agent over Lansenger.', '\u914d\u7f6e\u84dd\u4fe1\u63a5\u5165\uff0c\u7528\u84dd\u4fe1\u4e0e Agent \u5bf9\u8bdd\u3002', '\u914d\u7f6e\u85cd\u4fe1\u63a5\u5165\uff0c\u7528\u85cd\u4fe1\u8207 Agent \u5c0d\u8a71\u3002')}
            </p>
            {useBotProfiles ? (
                <section className="lansenger-bots" aria-label={textForLang(lang, 'Lansenger bots', '蓝信机器人', '藍信機器人')}>
                    <div className="lansenger-bots__header">
                        <div>
                            <strong>{textForLang(lang, 'Bot bindings', '机器人绑定', '機器人綁定')}</strong>
                            <p>{textForLang(lang, 'Every bot has an isolated Agent, queue and chat history.', '每个机器人都有独立 Agent、队列和聊天记录。', '每個機器人都有獨立 Agent、佇列和聊天記錄。')}</p>
                        </div>
                        <button type="button" className="im-settings-button" onClick={() => { setNewBotDraftState(true); setBotDraft(newLansengerBotDraft()); }} disabled={botSaving || (!!botDraft && !selectedBotIsSaved)} title={botDraft && !selectedBotIsSaved ? textForLang(lang, 'Save or discard the current draft before creating another bot.', '请先保存或放弃当前草稿，再创建机器人。', '請先儲存或捨棄目前草稿，再建立機器人。') : undefined}>
                            {textForLang(lang, 'Add bot', '添加机器人', '新增機器人')}
                        </button>
                    </div>
                    {botsLoading ? <p className="im-group-permissions__hint">{textForLang(lang, 'Loading bots…', '正在加载机器人…', '正在載入機器人…')}</p> : (
                        <ul className="lansenger-bots__tabs" aria-label={textForLang(lang, 'Configured bot instances', '已配置机器人实例', '已設定機器人實例')}>
                            <li className="lansenger-bots__list-heading" aria-live="polite">
                                <strong>{textForLang(lang, 'Configured bots', '已配置机器人', '已設定機器人')}</strong>
                                <span>{textForLang(lang, `${bots.length} saved`, `已保存 ${bots.length} 个`, `已儲存 ${bots.length} 個`)}</span>
                            </li>
                            {bots.map((bot) => <li key={bot.id}><button type="button" className="lansenger-bots__instance" data-active={selectedBotIsSaved && botDraft?.id === bot.id} onClick={() => { workingDirectorySelectionDraftIDRef.current = ''; setNewBotDraftState(false); setBotDraft(botDraftFromView(bot)); }} aria-current={selectedBotIsSaved && botDraft?.id === bot.id ? 'true' : undefined} disabled={botDraftIsNew} title={botDraftIsNew ? textForLang(lang, 'Save or discard the current draft before switching bots.', '请先保存或放弃当前草稿，再切换机器人。', '請先儲存或捨棄目前草稿，再切換機器人。') : undefined}>
                                <span className="lansenger-bots__instance-name">{bot.name || bot.id}</span>
                                <small className="lansenger-bots__instance-id">{bot.id}</small>
                                <span className="lansenger-bots__instance-meta">
                                    <span data-state={bot.enabled ? 'enabled' : 'disabled'}>{bot.enabled ? textForLang(lang, 'Enabled', '已启用', '已啟用') : textForLang(lang, 'Disabled', '已停用', '已停用')}</span>
                                    <span>{bot.assistant_mode === 'expert' ? textForLang(lang, 'AI expert', 'AI 专家', 'AI 專家') : textForLang(lang, 'General assistant', '通用助手', '通用助手')}</span>
                                </span>
                            </button></li>)}
                            {botDraft && !selectedBotIsSaved && <li><div role="status" className="lansenger-bots__draft-tab" data-active="true"><span>{textForLang(lang, 'New bot', '新机器人', '新機器人')}</span><small>{textForLang(lang, 'Unsaved draft', '未保存草稿', '未儲存草稿')}</small></div></li>}
                            {bots.length === 0 && !botDraft && <li className="lansenger-bots__empty"><p>{textForLang(lang, 'No bots yet. Add and save one to create the first binding.', '暂无机器人。点击“添加机器人”并保存，即可创建第一个绑定。', '尚無機器人。點選「新增機器人」並儲存，即可建立第一個綁定。')}</p></li>}
                        </ul>
                    )}
                    {botDraft && <div className="lansenger-bots__editor">
                        <div className="lansenger-bots__editor-header">
                            <div>
                                <strong>{selectedBotIsSaved ? `${textForLang(lang, 'Editing bot instance', '\u6b63\u5728\u7f16\u8f91\u673a\u5668\u4eba\u5b9e\u4f8b', '\u6b63\u5728\u7de8\u8f2f\u6a5f\u5668\u4eba\u5be6\u4f8b')}: ${botDraft.name || botDraft.id}` : textForLang(lang, 'New bot instance', '\u65b0\u673a\u5668\u4eba\u5b9e\u4f8b', '\u65b0\u6a5f\u5668\u4eba\u5be6\u4f8b')}</strong>
                                <span>{selectedBotIsSaved ? `${textForLang(lang, 'Instance ID', '\u5b9e\u4f8b ID', '\u5be6\u4f8b ID')}: ${botDraft.id}` : textForLang(lang, 'Unsaved draft — save to create this instance', '\u672a\u4fdd\u5b58\u8349\u7a3f\uff0c\u4fdd\u5b58\u540e\u521b\u5efa\u6b64\u5b9e\u4f8b', '\u672a\u5132\u5b58\u8349\u7a3f\uff0c\u5132\u5b58\u5f8c\u5efa\u7acb\u6b64\u5be6\u4f8b')}</span>
                            </div>
                            <span className="lansenger-bots__editor-state" data-state={botDraft.enabled ? 'enabled' : 'disabled'}>{botDraft.enabled ? textForLang(lang, 'Enabled', '\u5df2\u542f\u7528', '\u5df2\u555f\u7528') : textForLang(lang, 'Disabled', '\u5df2\u505c\u7528', '\u5df2\u505c\u7528')}</span>
                            {!selectedBotIsSaved && <button type="button" className="im-settings-button" onClick={discardBotDraft} disabled={botSaving}>{textForLang(lang, 'Discard draft', '\u653e\u5f03\u8349\u7a3f', '\u6368\u68c4\u8349\u7a3f')}</button>}
                        </div>
                        <div className="im-settings-grid im-settings-grid--two">
                            <label className="im-settings-field"><span>{textForLang(lang, 'Bot ID', '机器人 ID', '機器人 ID')}</span><input value={botDraft.id} disabled={selectedBotIsSaved} aria-invalid={newDraftBotIDError ? true : undefined} aria-describedby={newDraftBotIDError ? 'lansenger-bot-id-error' : undefined} onChange={(e) => setBotDraft({ ...botDraft, id: e.target.value })} spellCheck={false} />{newDraftBotIDError && <small id="lansenger-bot-id-error" className="lansenger-bots__field-error" role="alert">{newDraftBotIDError}</small>}</label>
                            <label className="im-settings-field"><span>{textForLang(lang, 'Name', '名称', '名稱')}</span><input value={botDraft.name} onChange={(e) => setBotDraft({ ...botDraft, name: e.target.value })} /></label>
                            <label className="im-settings-field"><span>App ID</span><input value={botDraft.app_id} onChange={(e) => setBotDraft({ ...botDraft, app_id: e.target.value })} spellCheck={false} /></label>
                            <label className="im-settings-field"><span>App Secret</span><input type="password" value={botDraft.app_secret || ''} onChange={(e) => setBotDraft({ ...botDraft, app_secret: e.target.value })} placeholder={botDraft.secret_configured ? textForLang(lang, 'Configured — leave blank to keep', '已配置，留空则保持不变', '已設定，留空則保持不變') : 'Lansenger App Secret'} autoComplete="off" /></label>
                            <label className="im-settings-field"><span>{textForLang(lang, 'Gateway', '网关', '網關')}</span><input value={botDraft.gateway_url || ''} onChange={(e) => setBotDraft({ ...botDraft, gateway_url: e.target.value })} placeholder="https://apigw.lx.qianxin.com" spellCheck={false} /></label>
                            <label className="im-settings-field"><span>{textForLang(lang, 'WS gateway', 'WS 网关', 'WS 網關')}</span><input value={botDraft.wss_url || ''} onChange={(e) => setBotDraft({ ...botDraft, wss_url: e.target.value })} placeholder={textForLang(lang, 'Optional', '可选', '可選')} spellCheck={false} /></label>
                            <label className="im-settings-field"><span>{textForLang(lang, 'Assistant', '助手类型', '助手類型')}</span><select value={botDraft.assistant_mode || 'general'} onChange={(e) => setBotDraft({ ...botDraft, assistant_mode: e.target.value, expert_id: e.target.value === 'expert' ? botDraft.expert_id || '' : '' })}><option value="general">{textForLang(lang, 'General AI assistant', '通用 AI 助手', '通用 AI 助手')}</option><option value="expert">{textForLang(lang, 'Specific expert', '指定 AI 专家', '指定 AI 專家')}</option></select></label>
                            {botDraft.assistant_mode === 'expert' && <label className="im-settings-field"><span>{textForLang(lang, 'AI expert', 'AI 专家', 'AI 專家')}</span><select value={botDraft.expert_id || ''} onChange={(e) => setBotDraft({ ...botDraft, expert_id: e.target.value })} disabled={expertsLoading || (expertsLoadAttempted && availableExperts.length === 0)}><option value="">{expertsLoading ? textForLang(lang, 'Loading experts…', '正在加载 AI 专家…', '正在載入 AI 專家…') : textForLang(lang, 'Select an available expert', '请选择可用的 AI 专家', '請選擇可用的 AI 專家')}</option>{availableExperts.map((expert) => <option key={expert.id} value={expert.id}>{expert.name || expert.id}{expert.description ? ` — ${expert.description}` : ''}</option>)}</select>{expertsLoadAttempted && !expertsLoading && availableExperts.length === 0 && <small>{textForLang(lang, 'No AI experts are currently available. Create or restore one before enabling this bot.', '当前没有可用的 AI 专家。请先创建或恢复专家，再启用该机器人。', '目前沒有可用的 AI 專家。請先建立或還原專家，再啟用此機器人。')}</small>}</label>}
                            <label className="im-settings-field"><span>{textForLang(lang, 'Group policy', '群聊策略', '群組策略')}</span><select value={botDraft.group_policy || 'open'} onChange={(e) => setBotDraft({ ...botDraft, group_policy: e.target.value })}><option value="open">{textForLang(lang, 'Open', '开放', '開放')}</option><option value="allowlist">{textForLang(lang, 'Allowlist', '仅允许列表', '僅允許列表')}</option><option value="disabled">{textForLang(lang, 'Disabled', '禁用群聊', '停用群組')}</option></select></label>
                            <div className="im-settings-field"><span id="lansenger-bot-working-directory-label">{textForLang(lang, 'Working directory', '工作目录', '工作目錄')}</span><div className="lansenger-bots__working-directory"><input aria-labelledby="lansenger-bot-working-directory-label" value={botDraft.working_directory || ''} onChange={(e) => setBotDraft({ ...botDraft, working_directory: e.target.value })} placeholder={textForLang(lang, 'Optional project/source directory', '可选：项目或源码目录', '可選：專案或原始碼目錄')} spellCheck={false} /><button type="button" className="im-settings-button" onClick={() => void selectBotWorkingDirectory()} disabled={workingDirectorySelecting} aria-label={textForLang(lang, 'Browse working directory', '浏览工作目录', '瀏覽工作目錄')} title={textForLang(lang, 'Browse working directory', '浏览工作目录', '瀏覽工作目錄')}>{workingDirectorySelecting ? textForLang(lang, 'Browsing…', '浏览中…', '瀏覽中…') : textForLang(lang, 'Browse…', '浏览…', '瀏覽…')}</button></div></div>
                            <label className="im-settings-field"><span>{textForLang(lang, 'Initial prompt', '初始提示词', '初始提示詞')}</span><textarea value={botDraft.initial_prompt || ''} onChange={(e) => setBotDraft({ ...botDraft, initial_prompt: e.target.value })} placeholder={textForLang(lang, 'Role, scope and response rules', '角色、范围与回复规则', '角色、範圍與回覆規則')} /></label>
                        </div>
                        <div className="lansenger-bots__documents">
                            <div><strong>{textForLang(lang, 'Help and document directories', '帮助与文档目录', '說明與文件目錄')}</strong><p>{textForLang(lang, 'Directories the expert can use for source code, manuals and retrieval.', '专家可用于源码、软件帮助文档和检索的目录。', '專家可用於原始碼、說明文件及檢索的目錄。')}</p></div>
                            <button type="button" className="im-settings-button" onClick={() => void addBotDirectory()}>{textForLang(lang, 'Add directory', '添加目录', '新增目錄')}</button>
                            {(botDraft.document_directories || []).map((path) => <div className="lansenger-bots__path" key={path}><code>{path}</code><button type="button" className="im-settings-button" onClick={() => setBotDraft({ ...botDraft, document_directories: (botDraft.document_directories || []).filter((item) => item !== path) })}>{textForLang(lang, 'Remove', '移除', '移除')}</button></div>)}
                        </div>
                        <div className="lansenger-bots__documents">
                            <div><strong>{textForLang(lang, 'Local directory boundary', '本地目录授权范围', '本機目錄授權範圍')}</strong><p>{textForLang(lang, 'Document and working directories are included automatically. Add only extra directories this bot must access.', '文档目录和工作目录会自动授权；仅添加该机器人还需访问的额外目录。', '文件目錄與工作目錄會自動授權；只新增此機器人還需存取的額外目錄。')}</p></div>
                            <label className="im-settings-toggle"><input type="checkbox" checked={!!botDraft.allow_all_directories} onChange={(e) => setBotDraft({ ...botDraft, allow_all_directories: e.target.checked })} /><span>{textForLang(lang, 'Allow all directories', '允许所有目录', '允許所有目錄')}</span></label>
                            {!botDraft.allow_all_directories && <>
                                <button type="button" className="im-settings-button" onClick={() => void addBotAllowedDirectory()}>{textForLang(lang, 'Add directory', '添加目录', '新增目錄')}</button>
                                {(botDraft.allowed_directories || []).map((path) => <div className="lansenger-bots__path" key={path}><code>{path}</code><button type="button" className="im-settings-button" onClick={() => setBotDraft({ ...botDraft, allowed_directories: (botDraft.allowed_directories || []).filter((item) => item !== path) })}>{textForLang(lang, 'Remove', '移除', '移除')}</button></div>)}
                            </>}
                        </div>
                        <div className="lansenger-bots__documents">
                            <div><strong>{textForLang(lang, 'Knowledge and web access', '知识库与网络访问', '知識庫與網路存取')}</strong><p>{textForLang(lang, 'These permissions apply when this bot replies in a group.', '这些权限在该机器人群聊回复时生效。', '這些權限在此機器人群組回覆時生效。')}</p></div>
                            <button type="button" className="im-settings-button" onClick={() => { setPermissionSourcesLoaded(false); loadPermissionSources(); }} disabled={permissionSourcesLoading}>{textForLang(lang, 'Refresh knowledge sources', '刷新知识库', '重新整理知識庫')}</button>
                            {permissionSourcesLoading ? <span>{textForLang(lang, 'Loading…', '加载中…', '載入中…')}</span> : permissionSources.map((source) => {
                                const id = String(source.id || '');
                                if (!id) return null;
                                return <label key={id} className="im-settings-toggle"><input type="checkbox" checked={(botDraft.knowledge_source_ids || []).includes(id)} onChange={(e) => toggleBotKnowledgeSource(id, e.target.checked)} /><span>{source.title || id}</span></label>;
                            })}
                            <label className="im-settings-toggle"><input type="checkbox" checked={!!botDraft.allow_web_search} onChange={(e) => setBotDraft({ ...botDraft, allow_web_search: e.target.checked })} /><span>{textForLang(lang, 'Allow public web search', '允许公开网络检索', '允許公開網路檢索')}</span></label>
                        </div>
                        <div className="lansenger-bots__answer-cache">
                            <div className="lansenger-bots__answer-cache-copy">
                                <strong>{textForLang(lang, 'Reply cache for this bot', '当前机器人回复缓存', '目前機器人回覆快取')}</strong>
                                <p>{textForLang(lang, 'This setting applies only to this bot instance. Cached answers remain isolated by bot, and group chats are additionally isolated by group. Refresh requests and follow-up questions always generate a new answer.', '此设置仅对当前机器人实例生效。缓存答案仍按机器人隔离，群聊还会按群组进一步隔离；要求更新或追问时始终重新生成。', '此設定僅對目前機器人實例生效。快取答案仍依機器人隔離，群組聊天還會依群組進一步隔離；要求更新或追問時一律重新產生。')}</p>
                            </div>
                            <div className="lansenger-bots__answer-cache-controls">
                                <label className="im-settings-toggle">
                                    <input type="checkbox" checked={answerCache.enabled} onChange={(e) => setBotDraft({ ...botDraft, answer_cache: { ...answerCache, enabled: e.target.checked } })} />
                                    <span>{textForLang(lang, 'Enable reply cache', '启用回复缓存', '啟用回覆快取')}</span>
                                </label>
                                <label className="lansenger-bots__answer-cache-ttl">
                                    <span>{textForLang(lang, 'Validity (days)', '有效期（天）', '有效期（天）')}</span>
                                    <input type="number" min={0} max={365} inputMode="numeric" disabled={!answerCache.enabled} aria-describedby="lansenger-answer-cache-status" value={answerCache.ttl_days} onChange={(e) => setBotDraft({ ...botDraft, answer_cache: { ...answerCache, ttl_days: Math.max(0, Math.min(365, Number(e.currentTarget.value) || 0)) } })} />
                                </label>
                            </div>
                            <p id="lansenger-answer-cache-status" className="lansenger-bots__answer-cache-status" role="status">{answerCacheReuseActive
                                ? textForLang(lang, `Reply cache is active for ${answerCache.ttl_days} days.`, `回复缓存已启用，有效期为 ${answerCache.ttl_days} 天。`, `回覆快取已啟用，有效期為 ${answerCache.ttl_days} 天。`)
                                : !answerCache.enabled
                                    ? textForLang(lang, 'Reply cache is off. Turn it on, then set the validity to 1–365 days to reuse answers.', '回复缓存已关闭。请先启用，再将有效期设为 1–365 天以复用答案。', '回覆快取已關閉。請先啟用，再將有效期設為 1–365 天以重用答案。')
                                    : textForLang(lang, 'Set the validity to 1–365 days to enable reuse. 0 means do not cache.', '有效期设为 1–365 天即可启用复用；0 表示不缓存。', '有效期設為 1–365 天即可啟用重用；0 表示不快取。')}</p>
                        </div>
                        <div className="im-settings-toggle-row">
                            <label className="im-settings-toggle"><input type="checkbox" checked={!!botDraft.enabled} onChange={(e) => setBotDraft({ ...botDraft, enabled: e.target.checked })} /><span>{textForLang(lang, 'Enable this bot', '启用此机器人', '啟用此機器人')}</span></label>
                            <label className="im-settings-toggle"><input type="checkbox" checked={botDraft.require_mention !== false} onChange={(e) => setBotDraft({ ...botDraft, require_mention: e.target.checked })} /><span>{textForLang(lang, 'Require @ in groups', '群聊需要 @', '群組需要 @')}</span></label>
                            <label className="im-settings-toggle"><input type="checkbox" checked={botDraft.auto_mention_reply !== false} onChange={(e) => setBotDraft({ ...botDraft, auto_mention_reply: e.target.checked })} /><span>{textForLang(lang, 'Default @ the asker', '默认 @ 提问者', '預設 @ 提問者')}</span></label>
                        </div>
                        <div className="lansenger-bots__actions">
                            <button type="button" className="im-settings-button im-settings-button--primary" onClick={() => void saveBot()} disabled={botSaving || !!newDraftBotIDError}>{textForLang(lang, 'Save bot', '保存机器人', '儲存機器人')}</button>
                            <span className="lansenger-bots__status" role={selectedBotStatus === 'degraded' ? 'alert' : undefined}>{textForLang(lang, 'Status', '状态', '狀態')}: {selectedBotStatus}{selectedBotStatusReason ? ` — ${selectedBotStatusReason}` : ''}</span>
                            {selectedBotStatus === 'connected' && (
                                <button
                                    ref={followBtnRef}
                                    type="button"
                                    className="im-settings-button"
                                    data-testid="lansenger-follow-button"
                                    aria-label={followLabel(lang)}
                                    aria-haspopup="dialog"
                                    aria-expanded={watchOpen && watchBotProfileID === botDraft.id}
                                    onClick={() => openWatch(botDraft.id)}
                                >
                                    {followLabel(lang)}
                                </button>
                            )}
                            <button type="button" className="im-settings-button" onClick={() => RestartLansengerBot(botDraft.id).then(setSelectedBotStatus).catch((err: any) => { void showAlert(String(err?.message || err || restartLabel(lang))); })} disabled={!selectedBotIsSaved || !botDraft.enabled}>{restartLabel(lang)}</button>
                            <button type="button" className="im-settings-button" onClick={(event) => openGroupInfo(event.currentTarget)} disabled={!selectedBotIsSaved || groupsLoading}>{textForLang(lang, 'Group info', '群信息', '群資訊')}</button>
                            <button type="button" className="im-settings-button im-settings-button--audit" onClick={() => setIMAuditPlatform(`lansenger:${botDraft.id}`)} disabled={!selectedBotIsSaved}>{textForLang(lang, 'Chat history', '聊天历史', '聊天記錄')}</button>
                            {bots.some((bot) => bot.id === botDraft.id) && <button type="button" className="im-settings-button im-settings-button--danger" onClick={() => void deleteBot()} disabled={botSaving}>{textForLang(lang, 'Delete bot', '删除机器人', '刪除機器人')}</button>}
                        </div>
                    </div>}
                </section>
            ) : <>
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
                    onClick={(event) => openGroupInfo(event.currentTarget)}
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
                        onClick={() => openWatch()}
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
                                <input
                                    type="checkbox"
                                    checked={!!(config as any)?.lansenger_group_allow_web_search}
                                    onChange={(e) => saveRemoteConfigField({ lansenger_group_allow_web_search: e.target.checked } as any)}
                                />
                                <span>{textForLang(lang, 'Allow web research and downloads', '允许网络检索与文件下载', '允許網路檢索與檔案下載')}</span>
                            </label>
                            <p className="im-group-permissions__hint">
                                {textForLang(
                                    lang,
                                    'Disabled by default. When enabled, group replies may use public web_search and web_fetch. Downloads (up to 25 MB, 60 s) are saved in the agent working directory; private-network targets, browser access, sessions, cookies, proxies, custom headers and JS rendering remain unavailable.',
                                    '默认关闭。启用后，群聊回复可使用公开网络的 web_search 和 web_fetch；下载文件（最大 25 MB、60 秒）保存到 Agent 工作目录。内网地址、浏览器访问、会话、Cookie、代理、自定义请求头和 JS 渲染仍不可用。',
                                    '預設關閉。啟用後，群組回覆可使用公開網路的 web_search 與 web_fetch；下載檔案（最大 25 MB、60 秒）儲存在 Agent 工作目錄。內網位址、瀏覽器存取、工作階段、Cookie、Proxy、自訂請求標頭與 JS 渲染仍不可用。',
                                )}
                            </p>
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

            {groupsOpen && createPortal(
                <div
                    className="im-groups-modal-overlay"
                    role="presentation"
                    {...portalThemeAttributes}
                    {...groupsBackdropProps}
                >
                    <section
                        ref={groupsDialogRef}
                        className="im-groups-modal"
                        role="dialog"
                        aria-modal="true"
                        aria-labelledby="lansenger-groups-dialog-title"
                        tabIndex={-1}
                        {...groupsDialogProps}
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
                                ref={groupsCloseBtnRef}
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
                                                    <th>{textForLang(lang, 'File limit', '文件上限', '檔案上限')}</th>
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
                                                            <td className="im-groups-table__file-limit">
                                                                <label>
                                                                    <input type="number" min="0" step="1" inputMode="numeric"
                                                                        value={fileLimitDrafts[id] ?? String(Math.round(Math.max(0, g.file_max_bytes || 0) / (1024 * 1024)))}
                                                                        disabled={!id || fileLimitBusyID === id}
                                                                        aria-label={textForLang(lang, `File limit for ${g.name || id}`, `${g.name || id} 文件大小上限`, `${g.name || id} 檔案大小上限`)}
                                                                        onChange={(e) => setFileLimitDrafts((prev) => ({ ...prev, [id]: e.currentTarget.value }))}
                                                                        onBlur={(e) => void saveGroupFileLimit(id, Number(e.currentTarget.value))}
                                                                        onKeyDown={(e) => { if (e.key === 'Enter') e.currentTarget.blur(); }} />
                                                                    <span>MB</span>
                                                                </label>
                                                                <small>{textForLang(lang, '0 = unlimited', '0 = 不限制', '0 = 不限制')}</small>
                                                            </td>
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
            , document.body)}

            {watchOpen && (watchBotProfileID ? selectedBotStatus === 'connected' : lansengerConnected) && createPortal(
                <div
                    className="im-groups-modal-overlay"
                    role="presentation"
                    {...portalThemeAttributes}
                    {...watchBackdropProps}
                >
                    <div
                        ref={watchDialogRef}
                        className="im-groups-modal im-watch-modal"
                        role="dialog"
                        aria-modal="true"
                        aria-labelledby="lansenger-follow-dialog-title"
                        tabIndex={-1}
                        {...watchDialogProps}
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
                                <UtilitiesWatchPanel isZh={isZh} onBack={closeWatch} botProfileId={watchBotProfileID} compactHeader />
                            </Suspense>
                        </div>
                    </div>
                </div>
            , document.body)}
            </>}
        </section>
    );
};

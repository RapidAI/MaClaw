import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import './UtilitiesPage.css';
import { parseWailsJSON } from './utilitiesParse';
import { useDialog } from '../CustomDialog';
import { tailText } from '../../i18n/mnTail'

type WatchJob = {
    id?: string;
    name: string;
    enabled: boolean;
    group_id: string;
    group_name?: string;
    target_staff_ids: string[];
    target_names?: Record<string, string>;
    record_all: boolean;
    /** targets = only watched people; anyone = whole group keywords */
    keyword_scope?: 'targets' | 'anyone';
    /** Forward every speech from watched targets to owner's IM channels */
    forward_on_target_speech?: boolean;
    /** Owner IM pathways: weixin | lansenger | telegram | qq | hub */
    forward_channels?: string[];
    keywords: KeywordRule[];
};

type IMChannel = {
    id: string;
    label: string;
    online: boolean;
    /** Can proactive-push now (private chat / hub session known). */
    session_ready?: boolean;
    detail?: string;
    enabled?: boolean;
};

type ForwardResult = {
    at?: string;
    job_id?: string;
    reason?: string;
    channel: string;
    ok: boolean;
    error?: string;
    preview?: string;
};

type KeywordRule = {
    id?: string;
    keywords: string[];
    case_sensitive?: boolean;
    record_on_match: boolean;
    reply_text?: string;
    cli_command?: string;
    cli_timeout_sec?: number;
    reply_with_cli_stdout?: boolean;
    /** Package hit and push to job.forward_channels */
    forward_on_match?: boolean;
};

type Member = {
    staff_id: string;
    name?: string;
    last_seen_at?: string;
    source?: string;
};

type GroupRow = { group_id: string; name: string; total_members?: number };
type RosterPayload = { members?: Member[]; directory_available?: boolean; directory_truncated?: boolean; note?: string };

// The member list is a fixed-height scroll container and each card uses
// content-visibility, so rendering up to this many entries stays cheap.
const ROSTER_RENDER_LIMIT = 1000;

/** Deduplicate dynamic import; on failure clear cache so the next call can retry. */
let appModulePromise: Promise<any | null> | null = null;

function getApp(): Promise<any | null> {
    if (!appModulePromise) {
        appModulePromise = import('../../../wailsjs/go/main/App').catch(() => {
            appModulePromise = null;
            return null;
        });
    }
    return appModulePromise;
}

function emptyJob(isZh: boolean | string): WatchJob {
    return {
        name: tailText((typeof isZh === "string" ? isZh : (isZh ? "zh-Hans" : "en")), 'People watch job', '盯人任务'),
        enabled: true,
        group_id: '',
        group_name: '',
        target_staff_ids: [],
        target_names: {},
        record_all: true,
        keyword_scope: 'targets',
        forward_on_target_speech: false,
        forward_channels: [],
        keywords: [
            {
                keywords: [],
                record_on_match: true,
                reply_text: '',
                cli_command: '',
                cli_timeout_sec: 15,
                reply_with_cli_stdout: true,
                forward_on_match: false,
            },
        ],
    };
}

export function UtilitiesWatchPanel({
    isZh,
    onBack,
    botProfileId = '',
    /** When embedded under IM settings modal, hide redundant page title chrome. */
    compactHeader = false,
}: {
    isZh: boolean | string;
    onBack: () => void;
    /** Selected Lansenger bot; empty retains legacy single-bot bindings. */
    botProfileId?: string;
    compactHeader?: boolean;
}) {
    const { showConfirm } = useDialog();
    const lang = typeof isZh === "string" ? isZh : (isZh ? "zh-Hans" : "en");
    const t = useMemo(
        () => ({
    title: tailText(lang, 'People watch', '盯人'),
    back: tailText(lang, 'Close', '关闭'),
    list: tailText(lang, 'Jobs', '任务列表'),
    create: tailText(lang, 'New job', '新建任务'),
    save: tailText(lang, 'Save', '保存'),
    del: tailText(lang, 'Delete', '删除'),
    enabled: tailText(lang, 'Enabled', '启用'),
    name: tailText(lang, 'Name', '名称'),
    group: tailText(lang, 'Lansenger group', '蓝信群'),
    loadGroups: tailText(lang, 'Refresh groups', '刷新群列表'),
    refreshMembers: tailText(lang, 'Refresh members', '刷新成员'),
    filter: tailText(lang, 'Filter by name/id', '按姓名/ID 过滤'),
    memberLimit: tailText(lang, 'Showing the first {count} people for performance. Search to find anyone else.', '为保持流畅，仅显示前 {count} 位；可使用搜索定位其他成员。'),
    directoryLimit: tailText(lang, 'Lansenger returned only part of this directory. Add other staff IDs manually.', '蓝信仅返回了部分成员；可手动添加其他 staffId。'),
    addManual: tailText(lang, 'Add target', '添加盯人对象'),
    manualToggle: tailText(lang, 'Add by staff ID', '手动添加 staffId'),
    manualId: tailText(lang, 'Member staffId', '成员 staffId'),
    manualName: tailText(lang, 'Name (optional)', '姓名（可选）'),
    recordAll: tailText(lang, 'Record all speech from targets (text log)', '记录目标用户全部发言（文本日志）'),
    kwScope: tailText(lang, 'Keyword scope', '关键字匹配范围'),
    kwScopeTargets: tailText(lang, 'Watched people only', '仅盯人对象'),
    kwScopeAnyone: tailText(lang, 'Anyone in the group', '群内任何人'),
    forwardSpeech: tailText(lang, 'Forward watched speech to my IM channels', '盯人对象发言时转发到我的 IM 通道'),
    forwardChannels: tailText(lang, 'Forward to my channels (prefer ready)', '转发到我的通道（勾选可推送通道）'),
    forwardKw: tailText(lang, 'Forward keyword hits to my channels (anyone-scope includes non-targets)', '关键字命中时转发到我的通道（任何人范围下也推非盯人对象）'),
    keywords: tailText(lang, 'Keyword rules', '关键字规则'),
    addKw: tailText(lang, 'Add rule', '添加规则'),
    rule: tailText(lang, 'Rule', '规则'),
    kwList: tailText(lang, 'Keywords (comma-separated)', '关键字（逗号分隔）'),
    reply: tailText(lang, 'Static reply (to source group)', '固定回复（回源群）'),
    cli: tailText(lang, 'CLI command', 'CLI 命令'),
    cliHint: tailText(lang, 'Placeholders: {{date}} {{content}} {{speaker_id}} {{speaker_name}} {{group_id}} {{group_name}} {{keyword}}. Without placeholders, flags are appended. Env LANXIN_WATCH_* is set. Non-empty stdout replies to the source group.', '可用占位符：{{date}} {{content}} {{speaker_id}} {{speaker_name}} {{group_id}} {{group_name}} {{keyword}}。无占位符时自动追加 --date/--content/--speaker-id/--group-id/--keyword。环境变量 LANXIN_WATCH_* 同步注入。stdout 非空则回给用户（源群）。'),
    recordKw: tailText(lang, 'Log on keyword hit', '命中时写入关键字日志'),
    targets: tailText(lang, 'Watched people', '盯人对象'),
    logs: tailText(lang, 'Transcripts', '发言记录'),
    refreshLogs: tailText(lang, 'Refresh logs', '刷新日志'),
    openLog: tailText(lang, 'View', '查看'),
    store: tailText(lang, 'Data directory', '数据目录'),
    note: tailText(lang, 'Forward goes to your own private bot session, not other people. Private-chat the bot once (“ready”); the session is saved so restarting Maclaw still works. Lansenger groups often only push @bot messages. Use Test on a channel to verify.', '说明：转发推到「你自己」与机器人的私聊，不是转给其他人。首次需私聊机器人一次（显示「可推送」）；会话会落盘，关闭并重开 Maclaw 后仍可推。蓝信群消息通常需 @机器人 才会推到本机；测试可点通道旁「测」。'),
    empty: tailText(lang, 'No people-watch jobs yet', '暂无盯人任务'),
    saveOk: tailText(lang, 'Saved', '已保存'),
    pickGroup: tailText(lang, 'Select a group', '请选择群'),
    sessionWarn: tailText(lang, 'Selected channels are not push-ready: private-chat the bot once (session is remembered across restarts), or use Test.', '已选通道尚不可推送：请先用该通道私聊机器人一次（之后会记住，重启仍可用），或点「测」验证。'),
    forwardLog: tailText(lang, 'Recent forwards', '最近转发结果'),
    refreshForward: tailText(lang, 'Refresh', '刷新结果'),
    testSend: tailText(lang, 'Test', '测'),
    testOk: tailText(lang, 'Test sent — check that channel’s private chat', '测试已发送，请查看该通道私聊'),
    chipReady: tailText(lang, '·ready', '·可推送'),
    chipNeedChat: tailText(lang, '·need chat', '·需私聊'),
    chipOnline: tailText(lang, '·on', '·在线'),
    chipOff: tailText(lang, '·off', '·未启用')
}),
        [lang],
    );

    const [jobs, setJobs] = useState<WatchJob[]>([]);
    const [draft, setDraft] = useState<WatchJob | null>(null);
    const [groups, setGroups] = useState<GroupRow[]>([]);
    const [members, setMembers] = useState<Member[]>([]);
    const [memberQuery, setMemberQuery] = useState('');
    const [manualId, setManualId] = useState('');
    const [manualName, setManualName] = useState('');
    const [manualOpen, setManualOpen] = useState(false);
    const [directoryAvailable, setDirectoryAvailable] = useState(true);
    const [rosterNote, setRosterNote] = useState('');
    const [directoryTruncated, setDirectoryTruncated] = useState(false);
    const [membersLoading, setMembersLoading] = useState(false);
    const [logs, setLogs] = useState<string[]>([]);
    const [logContent, setLogContent] = useState('');
    const [storePath, setStorePath] = useState('');
    const [error, setError] = useState('');
    const [hint, setHint] = useState('');
    const [busy, setBusy] = useState(false);
    const [channels, setChannels] = useState<IMChannel[]>([]);
    const [forwardResults, setForwardResults] = useState<ForwardResult[]>([]);
    const aliveRef = useRef(true);
    const hintTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const membersGenRef = useRef(0);
    const logsGenRef = useRef(0);

    // Stable primitive for callback deps (avoid recreating loadJobs when only identity of a string literal would change).
    const apiMissingMsg = tailText(lang, 'People-watch APIs missing (rebuild desktop)', '后端未暴露盯人接口（请重新编译桌面端）');

    useEffect(() => {
        aliveRef.current = true;
        return () => {
            aliveRef.current = false;
            membersGenRef.current += 1;
            logsGenRef.current += 1;
            if (hintTimerRef.current != null) {
                clearTimeout(hintTimerRef.current);
                hintTimerRef.current = null;
            }
        };
    }, []);

    const flashHint = useCallback((msg: string) => {
        setHint(msg);
        if (hintTimerRef.current != null) clearTimeout(hintTimerRef.current);
        hintTimerRef.current = setTimeout(() => {
            if (!aliveRef.current) return;
            setHint('');
            hintTimerRef.current = null;
        }, 2000);
    }, []);

    const loadJobs = useCallback(async () => {
        const app = await getApp();
        if (!aliveRef.current) return;
        if (!app?.ListLansengerWatchJobs) {
            setError(apiMissingMsg);
            return;
        }
        try {
            const raw = app.ListLansengerWatchJobsForBot
                ? await app.ListLansengerWatchJobsForBot(botProfileId)
                : await app.ListLansengerWatchJobs();
            if (!aliveRef.current) return;
            setJobs(parseWailsJSON<WatchJob[]>(raw) || []);
            const extras = await Promise.all([
                app.GetLansengerWatchStorePath ? app.GetLansengerWatchStorePath().catch(() => '') : Promise.resolve(''),
                app.ListLansengerWatchChannelsForBot
                    ? app.ListLansengerWatchChannelsForBot(botProfileId).catch(() => null)
                    : app.ListLansengerWatchChannels
                      ? app.ListLansengerWatchChannels().catch(() => null)
                    : Promise.resolve(null),
            ]);
            if (!aliveRef.current) return;
            setStorePath((extras[0] as string) || '');
            if (extras[1] != null) {
                try {
                    setChannels(parseWailsJSON<IMChannel[]>(extras[1]) || []);
                } catch {
                    /* ignore bad channel payload */
                }
            }
            if (app.ListLansengerWatchForwardResultsForBot || app.ListLansengerWatchForwardResults) {
                try {
                    const fr = app.ListLansengerWatchForwardResultsForBot
                        ? await app.ListLansengerWatchForwardResultsForBot(botProfileId)
                        : await app.ListLansengerWatchForwardResults();
                    if (!aliveRef.current) return;
                    setForwardResults(parseWailsJSON<ForwardResult[]>(fr) || []);
                } catch {
                    /* optional diagnostic API */
                }
            }
        } catch (e: any) {
            if (!aliveRef.current) return;
            setError(e?.message || String(e));
        }
    }, [apiMissingMsg, botProfileId]);

    const refreshChannels = useCallback(async () => {
        const app = await getApp();
        if (!aliveRef.current || (!app?.ListLansengerWatchChannelsForBot && !app?.ListLansengerWatchChannels)) return;
        try {
            const raw = app.ListLansengerWatchChannelsForBot
                ? await app.ListLansengerWatchChannelsForBot(botProfileId)
                : await app.ListLansengerWatchChannels();
            if (!aliveRef.current) return;
            setChannels(parseWailsJSON<IMChannel[]>(raw) || []);
            if (app.ListLansengerWatchForwardResultsForBot || app.ListLansengerWatchForwardResults) {
                const fr = app.ListLansengerWatchForwardResultsForBot
                    ? await app.ListLansengerWatchForwardResultsForBot(botProfileId)
                    : await app.ListLansengerWatchForwardResults();
                if (!aliveRef.current) return;
                setForwardResults(parseWailsJSON<ForwardResult[]>(fr) || []);
            }
        } catch {
            /* ignore */
        }
    }, [botProfileId]);

    const testForwardChannel = useCallback(
        async (channelId: string) => {
            const app = await getApp();
            if (!app?.TestLansengerWatchForwardForBot && !app?.TestLansengerWatchForward) {
                setError(apiMissingMsg);
                return;
            }
            setBusy(true);
            setError('');
            try {
                if (app.TestLansengerWatchForwardForBot) {
                    await app.TestLansengerWatchForwardForBot(botProfileId, channelId);
                } else {
                    await app.TestLansengerWatchForward(channelId);
                }
                if (!aliveRef.current) return;
                flashHint(t.testOk);
                await refreshChannels();
            } catch (e: any) {
                if (!aliveRef.current) return;
                setError(e?.message || String(e));
                await refreshChannels();
            } finally {
                if (aliveRef.current) setBusy(false);
            }
        },
        [apiMissingMsg, botProfileId, flashHint, refreshChannels, t.testOk],
    );

    const loadGroups = useCallback(async () => {
        const app = await getApp();
        if (!aliveRef.current || !app?.ListLansengerGroups) return;
        try {
            const res = await app.ListLansengerGroups();
            if (!aliveRef.current) return;
            const parsed = parseWailsJSON<{ groups?: GroupRow[] }>(res) || res;
            setGroups((parsed?.groups || []) as GroupRow[]);
        } catch (e: any) {
            if (!aliveRef.current) return;
            setError(e?.message || String(e));
        }
    }, []);

    const loadMembers = useCallback(async (groupId: string, q: string) => {
        if (!groupId) {
            setMembers([]);
            setDirectoryAvailable(true);
            setRosterNote('');
            setDirectoryTruncated(false);
            setMembersLoading(false);
            return;
        }
        const gen = ++membersGenRef.current;
        setMembersLoading(true);
        setMembers([]);
        setDirectoryAvailable(true);
        setRosterNote(tailText(lang, 'Loading Lansenger group members…', '正在获取蓝信群成员目录…'));
        setDirectoryTruncated(false);
        const app = await getApp();
        if (!aliveRef.current || gen !== membersGenRef.current) return;
        if (!app?.ListLansengerWatchRoster) {
            setDirectoryAvailable(false);
            setMembersLoading(false);
            setRosterNote(apiMissingMsg);
            setDirectoryTruncated(false);
            return;
        }
        try {
            const raw = app.ListLansengerWatchRosterForBot
                ? await app.ListLansengerWatchRosterForBot(botProfileId, groupId, q || '')
                : await app.ListLansengerWatchRoster(groupId, q || '');
            if (!aliveRef.current || gen !== membersGenRef.current) return;
            const parsed = parseWailsJSON<RosterPayload>(raw);
            if (!parsed || !Array.isArray(parsed.members)) {
                setMembers([]);
                setDirectoryAvailable(false);
                setRosterNote(tailText(lang, 'Member directory returned an invalid response. Add a target manually.', '成员目录返回格式异常，可手动添加盯人对象。'));
                setDirectoryTruncated(false);
                return;
            }
            setMembers(parsed.members);
            setDirectoryAvailable(parsed.directory_available !== false);
            setRosterNote(parsed.note || '');
            setDirectoryTruncated(!!parsed.directory_truncated);
        } catch (e: any) {
            if (!aliveRef.current || gen !== membersGenRef.current) return;
            setMembers([]);
            setDirectoryAvailable(false);
            setRosterNote(e?.message || String(e));
            setDirectoryTruncated(false);
        } finally {
            if (aliveRef.current && gen === membersGenRef.current) setMembersLoading(false);
        }
    }, [apiMissingMsg, botProfileId, isZh]);

    const loadLogs = useCallback(async (jobId?: string) => {
        if (!jobId) {
            setLogs([]);
            return;
        }
        const gen = ++logsGenRef.current;
        const app = await getApp();
        if (!aliveRef.current || gen !== logsGenRef.current || !app?.ListLansengerWatchTranscripts) return;
        try {
            const raw = await app.ListLansengerWatchTranscripts(jobId);
            if (!aliveRef.current || gen !== logsGenRef.current) return;
            setLogs(parseWailsJSON<string[]>(raw) || []);
        } catch (e: any) {
            if (!aliveRef.current || gen !== logsGenRef.current) return;
            setError(e?.message || String(e));
        }
    }, []);

    useEffect(() => {
        void loadJobs();
        void loadGroups();
    }, [loadJobs, loadGroups]);

    // Load the group directory whenever the selected group changes. Filtering
    // stays local so typing in the picker never creates extra backend calls.
    useEffect(() => {
        if (!draft?.group_id) {
            setMembers([]);
            setMembersLoading(false);
            setDirectoryAvailable(true);
            setRosterNote('');
            setDirectoryTruncated(false);
            return;
        }
        void loadMembers(draft.group_id, '');
    }, [draft?.group_id, loadMembers]);

    useEffect(() => {
        if (draft?.id) {
            void loadLogs(draft.id);
        } else {
            setLogs([]);
        }
    }, [draft?.id, loadLogs]);

    const toggleTarget = (staffId: string, name?: string) => {
        if (!draft) return;
        const id = staffId.trim();
        const set = new Set(draft.target_staff_ids || []);
        const names = { ...(draft.target_names || {}) };
        if (set.has(id)) {
            set.delete(id);
            delete names[id];
        } else {
            set.add(id);
            if (name) names[id] = name;
        }
        setDraft({ ...draft, target_staff_ids: Array.from(set), target_names: names });
    };

    const save = async () => {
        if (!draft) return;
        if (!draft.group_id.trim()) {
            setError(t.pickGroup);
            return;
        }
        const hasKeywordRules = (draft.keywords || []).some((k) =>
            (k.keywords || []).some((kw) => String(kw || '').trim().length > 0),
        );
        const hasTargets = (draft.target_staff_ids || []).length > 0;
        // Empty 盯人对象 is valid and must be savable: record/speech-forward simply
        // match nobody. Blocking empty lists previously left old targets on disk
        // while the UI looked "cleared", so removed people kept being forwarded.
        if (!hasKeywordRules && !draft.record_all && !draft.forward_on_target_speech) {
            setError(tailText(lang, 'Enable record, keywords, or speech forward', '请至少启用：记录发言 / 关键字 / 发言转发 之一'));
            return;
        }
        if (
            (draft.forward_on_target_speech || (draft.keywords || []).some((k) => k.forward_on_match)) &&
            !(draft.forward_channels || []).length
        ) {
            setError(tailText(lang, 'Select at least one IM channel for forward', '启用转发时请至少勾选一个 IM 通道'));
            return;
        }
        setBusy(true);
        setError('');
        try {
            const app = await getApp();
            if (!app?.UpsertLansengerWatchJob) {
                setError(apiMissingMsg);
                return;
            }
            // Always send explicit arrays so empty target lists clear disk state.
            // Prune target_names to the remaining ids (removed people leave no name residue).
            const targetIDs = draft.target_staff_ids || [];
            const prunedNames: Record<string, string> = {};
            for (const id of targetIDs) {
                const n = (draft.target_names || {})[id];
                if (n) prunedNames[id] = n;
            }
            const payload: WatchJob = {
                ...draft,
                target_staff_ids: targetIDs,
                target_names: prunedNames,
                forward_channels: draft.forward_channels || [],
                keywords: draft.keywords || [],
            };
            const raw = app.UpsertLansengerWatchJobForBot
                ? await app.UpsertLansengerWatchJobForBot(botProfileId, JSON.stringify(payload))
                : await app.UpsertLansengerWatchJob(JSON.stringify(payload));
            if (!aliveRef.current) return;
            const saved = parseWailsJSON<WatchJob>(raw);
            const base = emptyJob(isZh);
            setDraft({
                ...base,
                ...saved,
                target_staff_ids: saved?.target_staff_ids || [],
                target_names: saved?.target_names || {},
                forward_channels: saved?.forward_channels || [],
                keywords: saved?.keywords?.length ? saved.keywords : base.keywords,
            });
            if (!hasTargets && (draft.record_all || draft.forward_on_target_speech)) {
                flashHint(
                    tailText(lang, 'Saved: no targets — record/speech-forward match nobody', '已保存：未选盯人对象，记录/发言转发不会匹配任何人'),
                );
            } else {
                flashHint(t.saveOk);
            }
            await loadJobs();
        } catch (e: any) {
            if (!aliveRef.current) return;
            setError(e?.message || String(e));
        } finally {
            if (aliveRef.current) setBusy(false);
        }
    };

    const remove = async () => {
        if (!draft?.id) {
            setDraft(null);
            return;
        }
        if (!await showConfirm(tailText(lang, 'Delete this job? Logs are kept.', '确认删除该任务？日志文件会保留。'), tailText(lang, 'Delete job', '删除任务'), { confirmText: tailText(lang, 'Delete', '删除'), cancelText: tailText(lang, 'Cancel', '取消'), confirmVariant: 'danger' })) return;
        setBusy(true);
        setError('');
        try {
            const app = await getApp();
            if (!app?.DeleteLansengerWatchJob) {
                setError(apiMissingMsg);
                return;
            }
            if (app.DeleteLansengerWatchJobForBot) {
                await app.DeleteLansengerWatchJobForBot(botProfileId, draft.id);
            } else {
                await app.DeleteLansengerWatchJob(draft.id);
            }
            if (!aliveRef.current) return;
            setDraft(null);
            await loadJobs();
        } catch (e: any) {
            if (!aliveRef.current) return;
            setError(e?.message || String(e));
        } finally {
            if (aliveRef.current) setBusy(false);
        }
    };

    const openLog = async (path: string) => {
        const app = await getApp();
        if (!app?.ReadLansengerWatchTranscript) return;
        try {
            const text = await app.ReadLansengerWatchTranscript(path);
            if (!aliveRef.current) return;
            setLogContent(text || '');
        } catch (e: any) {
            if (!aliveRef.current) return;
            setError(e?.message || String(e));
        }
    };


    const addManual = async () => {
        if (!draft?.group_id || !manualId.trim()) return;
        const app = await getApp();
        if (!app?.AddLansengerWatchMember) {
            setError(apiMissingMsg);
            return;
        }
        try {
            if (app.AddLansengerWatchMemberForBot) {
                await app.AddLansengerWatchMemberForBot(botProfileId, draft.group_id, manualId.trim(), manualName.trim());
            } else {
                await app.AddLansengerWatchMember(draft.group_id, manualId.trim(), manualName.trim());
            }
            if (!aliveRef.current) return;
            toggleTarget(manualId.trim(), manualName.trim() || manualId.trim());
            setManualId('');
            setManualName('');
            await loadMembers(draft.group_id, '');
        } catch (e: any) {
            if (!aliveRef.current) return;
            setError(e?.message || String(e));
        }
    };

    const updateKw = (idx: number, patch: Partial<KeywordRule>) => {
        if (!draft) return;
        const keywords = [...(draft.keywords || [])];
        keywords[idx] = { ...keywords[idx], ...patch };
        setDraft({ ...draft, keywords });
    };

    const activeChannels = (draft?.forward_channels || []).length;
    const manualForced = !directoryAvailable || directoryTruncated;
    const filteredMembers = useMemo(() => {
        const query = memberQuery.trim().toLocaleLowerCase();
        if (!query) return members;
        return members.filter((member) => `${member.name || ''} ${member.staff_id}`.toLocaleLowerCase().includes(query));
    }, [memberQuery, members]);

    return (
        <div className={`utilities-page${compactHeader ? ' utilities-page--compact' : ''}`} data-testid="watch-page">
            <div className="utilities-page__header utilities-page__header--row">
                <div>
                    {!compactHeader && (
                        <button type="button" className="utilities-link" onClick={onBack}>
                            {t.back}
                        </button>
                    )}
                    {!compactHeader && <h1 className="utilities-page__title">{t.title}</h1>}
                    {!compactHeader && <p className="utilities-page__subtitle">{t.note}</p>}
                    {!compactHeader && storePath ? (
                        <p className="utilities-meta">
                            {t.store}: {storePath}
                        </p>
                    ) : null}
                </div>
                <div className="utilities-actions">
                    <button type="button" className="utilities-btn" onClick={() => setDraft(emptyJob(isZh))}>
                        {t.create}
                    </button>
                </div>
            </div>

            {error ? <div className="utilities-error" role="alert">{error}</div> : null}
            {hint ? <div className="utilities-hint" role="status">{hint}</div> : null}

            <div className="utilities-watch-layout">
                <aside className="utilities-watch-list">
                    <div className="utilities-watch-list__header">
                        <div>
                            <span className="utilities-section-kicker">{tailText(lang, 'People watch jobs', '盯人任务')}</span>
                            <h3>{t.list}</h3>
                        </div>
                        <span className="utilities-watch-list__count">{jobs.length}</span>
                    </div>
                    {jobs.length === 0 ? <div className="utilities-empty">{t.empty}</div> : null}
                    {jobs.map((j, idx) => (
                        <button
                            key={j.id || `${j.group_id || 'g'}-${j.name || 'job'}-${idx}`}
                            type="button"
                            className={`utilities-watch-item${draft?.id && j.id && draft.id === j.id ? ' is-active' : ''}`}
                            aria-current={draft?.id && j.id && draft.id === j.id ? 'page' : undefined}
                            onClick={() => {
                                const base = emptyJob(isZh);
                                setDraft({
                                    ...base,
                                    ...j,
                                    keywords: j.keywords?.length ? j.keywords : base.keywords,
                                });
                            }}
                        >
                            <div className="utilities-watch-item__title">
                                <span
                                    className="utilities-watch-item__dot"
                                    data-on={j.enabled ? 'true' : 'false'}
                                    title={j.enabled ? t.enabled : (tailText(lang, 'Disabled', '已停用'))}
                                    aria-hidden="true"
                                />
                                <span className="utilities-watch-item__name">{j.name || j.id}</span>
                                {!j.enabled ? (
                                    <span className="utilities-watch-item__badge">{tailText(lang, 'Off', '已停用')}</span>
                                ) : null}
                            </div>
                            <div className="utilities-watch-item__meta">
                                {j.group_name || j.group_id}
                                {' · '}
                                {(j.target_staff_ids || []).length}
                                {tailText(lang, ' people', ' 人')}
                            </div>
                        </button>
                    ))}
                </aside>

                {draft ? (
                    <section className="utilities-watch-editor watch-editor">
                        <div className="watch-editor__topbar">
                            <label className="watch-editor__name">
                                <span>{t.name}</span>
                                <input value={draft.name} onChange={(e) => setDraft({ ...draft, name: e.target.value })} />
                            </label>
                            <label className="watch-editor__enabled">
                                <input type="checkbox" checked={!!draft.enabled} onChange={(e) => setDraft({ ...draft, enabled: e.target.checked })} />
                                <span>{draft.enabled ? t.enabled : (tailText(lang, 'Disabled', '已停用'))}</span>
                            </label>
                        </div>

                        <div className="watch-editor__group-row">
                            <label>
                                <span>{t.group}</span>
                                <select
                                    value={draft.group_id}
                                    onChange={(e) => {
                                        const g = groups.find((x) => x.group_id === e.target.value);
                                        setDraft({ ...draft, group_id: e.target.value, group_name: g?.name || '', target_staff_ids: [], target_names: {} });
                                        setMemberQuery('');
                                        setManualId('');
                                        setManualName('');
                                        setManualOpen(false);
                                    }}
                                >
                                    <option value="">{t.pickGroup}</option>
                                    {groups.map((g) => <option key={g.group_id} value={g.group_id}>{g.name || g.group_id}{g.total_members != null ? ` (${g.total_members})` : ''}</option>)}
                                </select>
                            </label>
                            <button type="button" className="utilities-btn utilities-btn--ghost" onClick={() => void loadGroups()}>{t.loadGroups}</button>
                        </div>

                        <div className="watch-editor__section watch-editor__section--targets">
                            <div className="watch-editor__section-heading">
                                <div>
                                    <h3>
                                        {t.targets}
                                        {(draft.target_staff_ids || []).length > 0 ? (
                                            <span className="watch-editor__count">{(draft.target_staff_ids || []).length}</span>
                                        ) : null}
                                    </h3>
                                    <p>{tailText(lang, 'Pick group members to watch; add by staff ID when the directory is unavailable.', '选择要监看的群成员；目录不可用时也可手动添加。')}</p>
                                </div>
                            </div>
                            {(draft.target_staff_ids || []).length > 0 ? (
                                <div className="utilities-chip-row utilities-watch-targets">
                                    {(draft.target_staff_ids || []).map((id) => (
                                        <button key={id} type="button" className="utilities-chip is-on" title={id} onClick={() => toggleTarget(id)}>{(draft.target_names || {})[id] || id} ×</button>
                                    ))}
                                </div>
                            ) : null}
                            <div className="utilities-member-picker">
                                <div className="utilities-member-picker__toolbar">
                                    <input className="utilities-input" aria-label={t.filter} placeholder={t.filter} value={memberQuery} onChange={(e) => setMemberQuery(e.target.value)} />
                                    <button
                                        type="button"
                                        className="utilities-btn utilities-btn--ghost utilities-member-picker__refresh"
                                        disabled={!draft.group_id || membersLoading}
                                        onClick={() => void loadMembers(draft.group_id, memberQuery)}
                                    >
                                        {t.refreshMembers}
                                    </button>
                                </div>
                                {rosterNote && !membersLoading ? <p className="utilities-member-picker__note" role="status">{rosterNote}</p> : null}
                                <div className="utilities-member-list">
                                    {filteredMembers.slice(0, ROSTER_RENDER_LIMIT).map((m) => {
                                        const on = (draft.target_staff_ids || []).includes(m.staff_id);
                                        return <button key={m.staff_id} type="button" className={`utilities-member${on ? ' is-on' : ''}`} aria-pressed={on} onClick={() => toggleTarget(m.staff_id, m.name)}><strong>{m.name || m.staff_id}</strong><span>{m.staff_id}</span></button>;
                                    })}
                                    {membersLoading ? <div className="utilities-empty utilities-empty--member-list" role="status"><strong>{tailText(lang, 'Loading members…', '正在加载成员…')}</strong></div> : filteredMembers.length === 0 ? <div className="utilities-empty utilities-empty--member-list"><strong>{tailText(lang, 'No selectable members yet', '暂无可选成员')}</strong><span>{rosterNote || (tailText(lang, 'Fetching the Lansenger group member directory.', '正在获取蓝信群成员目录。'))}</span></div> : null}
                                    {filteredMembers.length > ROSTER_RENDER_LIMIT ? <p className="utilities-member-picker__limit">{t.memberLimit.replace('{count}', String(ROSTER_RENDER_LIMIT))}</p> : null}
                                </div>
                            </div>
                            {draft.group_id ? (
                                manualForced || manualOpen ? (
                                    <div className="utilities-manual-target">
                                        <p>
                                            {directoryTruncated
                                                ? t.directoryLimit
                                                : !directoryAvailable
                                                  ? (tailText(lang, 'Add a target manually while the group directory is unavailable.', '群成员目录不可用时，可临时手动添加盯人对象。'))
                                                  : (tailText(lang, 'Add a member to the watch list by staff ID.', '输入成员 staffId 直接加入盯人列表。'))}
                                        </p>
                                        <div className="utilities-field__row">
                                            <input className="utilities-input" placeholder={t.manualId} aria-label={t.manualId} value={manualId} onChange={(e) => setManualId(e.target.value)} />
                                            <input className="utilities-input" placeholder={t.manualName} aria-label={t.manualName} value={manualName} onChange={(e) => setManualName(e.target.value)} />
                                            <button type="button" className="utilities-btn utilities-btn--ghost" onClick={() => void addManual()}>{t.addManual}</button>
                                        </div>
                                    </div>
                                ) : (
                                    <button type="button" className="utilities-link utilities-manual-target__toggle" onClick={() => setManualOpen(true)}>
                                        {t.manualToggle}
                                    </button>
                                )
                            ) : null}
                        </div>

                        <div className="watch-editor__section">
                            <div className="watch-editor__section-heading"><div><h3>{tailText(lang, 'Capture & forward', '记录与转发')}</h3><p>{tailText(lang, 'Set what to record or forward.', '设置需要记录或推送的消息。')}</p></div></div>
                            <div className="watch-settings">
                                <div className="watch-settings__pair">
                                    <label className="watch-setting-row"><span><strong>{t.recordAll}</strong><small>{tailText(lang, 'Save full text records from watched people', '保存盯人对象的完整文本记录')}</small></span><input type="checkbox" checked={!!draft.record_all} onChange={(e) => setDraft({ ...draft, record_all: e.target.checked })} /></label>
                                    <label className="watch-setting-row"><span><strong>{t.forwardSpeech}</strong><small>{tailText(lang, 'Push watched speech', '推送盯人对象发言')}</small></span><input type="checkbox" checked={!!draft.forward_on_target_speech} onChange={(e) => setDraft({ ...draft, forward_on_target_speech: e.target.checked })} /></label>
                                </div>
                                <label className="watch-setting-row"><span><strong>{t.kwScope}</strong><small>{tailText(lang, 'Who keyword rules should match', '关键字规则匹配的范围')}</small></span><select value={draft.keyword_scope || 'targets'} onChange={(e) => setDraft({ ...draft, keyword_scope: e.target.value === 'anyone' ? 'anyone' : 'targets' })}><option value="targets">{t.kwScopeTargets}</option><option value="anyone">{t.kwScopeAnyone}</option></select></label>
                                <div className="watch-setting-row watch-setting-row--channels"><span><strong>{t.forwardChannels}{activeChannels > 0 ? ` (${activeChannels})` : ''}</strong><small>{tailText(lang, 'Pushes to your own bot session; Hub = last active bound IM', '推送到你与机器人的私聊会话；Hub = 最近活跃绑定 IM')}</small></span><div className="utilities-chip-row">
                                {(channels.length
                                    ? channels
                                    : [
                                          { id: 'weixin', label: tailText(lang, 'WeChat', '微信'), online: false },
                                          { id: 'lansenger', label: tailText(lang, 'Lansenger', '蓝信'), online: false },
                                          { id: 'hub', label: 'Hub', online: false },
                                      ]
                                ).map((ch) => {
                                    const on = (draft.forward_channels || []).includes(ch.id);
                                    // Missing session_ready (older builds) → treat as not ready when online.
                                    const ready = ch.session_ready === true;
                                    const chipSuffix = ready
                                        ? t.chipReady
                                        : ch.online
                                          ? t.chipNeedChat
                                          : ch.enabled === false
                                            ? t.chipOff
                                            : '';
                                    return (
                                        <span key={ch.id} className="utilities-chip-wrap">
                                            <button
                                                type="button"
                                                className={`utilities-chip${on ? ' is-on' : ''}${on && !ready ? ' is-warn' : ''}`}
                                                aria-pressed={on}
                                                title={ch.detail || ''}
                                                onClick={() => {
                                                    const set = new Set(draft.forward_channels || []);
                                                    if (set.has(ch.id)) set.delete(ch.id);
                                                    else set.add(ch.id);
                                                    setDraft({ ...draft, forward_channels: Array.from(set) });
                                                }}
                                            >
                                                {on ? '✓ ' : ''}
                                                {ch.label}
                                                {chipSuffix}
                                            </button>
                                            {ch.online || ch.enabled !== false ? (
                                                <button
                                                    type="button"
                                                    className="utilities-chip-test"
                                                    disabled={busy}
                                                    title={ch.detail || t.testSend}
                                                    onClick={(e) => {
                                                        e.preventDefault();
                                                        void testForwardChannel(ch.id);
                                                    }}
                                                >
                                                    {t.testSend}
                                                </button>
                                            ) : null}
                                        </span>
                                    );
                                })}
                            </div>
                        </div>
                            {(() => {
                                const wantsForward =
                                    !!draft.forward_on_target_speech ||
                                    (draft.keywords || []).some((k) => k.forward_on_match);
                                const selectedNotReady = (draft.forward_channels || []).some((id) => {
                                    const ch = channels.find((c) => c.id === id);
                                    return !!ch && !!ch.online && ch.session_ready !== true;
                                });
                                return wantsForward && selectedNotReady ? (
                                    <p className="utilities-hint utilities-hint--warn" role="status">
                                        {t.sessionWarn}
                                    </p>
                                ) : null;
                            })()}
                            {forwardResults.length > 0 ? (
                                <div className="watch-forward-log">
                                    <div className="watch-forward-log__head">
                                        <strong>{t.forwardLog}</strong>
                                        <button type="button" className="utilities-link" onClick={() => void refreshChannels()}>
                                            {t.refreshForward}
                                        </button>
                                    </div>
                                    <ul className="watch-forward-log__list">
                                        {forwardResults.slice(0, 8).map((r, i) => (
                                            <li key={`${r.at || ''}-${r.channel}-${i}`} className={r.ok ? 'is-ok' : 'is-err'}>
                                                <span className="watch-forward-log__ch">{r.channel}</span>
                                                <span className="watch-forward-log__st">{r.ok ? 'OK' : r.error || 'ERR'}</span>
                                                {r.preview ? <span className="watch-forward-log__pv">{r.preview}</span> : null}
                                            </li>
                                        ))}
                                    </ul>
                                </div>
                            ) : null}
                            </div>
                        </div>

                        <div className="watch-editor__section">
                            <div className="watch-editor__section-heading watch-editor__section-heading--action">
                                <div>
                                    <h3>{t.keywords}</h3>
                                    <p>{tailText(lang, 'Define logging, replies, or forwards when keywords match.', '设置命中关键字后的记录、回复或转发动作。')}</p>
                                </div>
                                <button
                                    type="button"
                                    className="utilities-btn utilities-btn--ghost"
                                    onClick={() =>
                                        setDraft({
                                            ...draft,
                                            keywords: [
                                                ...(draft.keywords || []),
                                                {
                                                    keywords: [],
                                                    record_on_match: true,
                                                    reply_text: '',
                                                    cli_command: '',
                                                    cli_timeout_sec: 15,
                                                    reply_with_cli_stdout: true,
                                                    // Default on when speech-forward is enabled so keyword hits also notify owner channels.
                                                    forward_on_match: !!draft.forward_on_target_speech,
                                                },
                                            ],
                                        })
                                    }
                                >
                                    {t.addKw}
                                </button>
                            </div>
                            {(draft.keywords || []).map((rule, idx) => (
                                <div key={rule.id || idx} className="utilities-kw-card watch-keyword-rule">
                                    <div className="watch-keyword-rule__head">
                                        <span className="watch-keyword-rule__index">{t.rule} {idx + 1}</span>
                                        <button
                                            type="button"
                                            className="utilities-btn utilities-btn--ghost utilities-btn--quiet-danger watch-keyword-rule__remove"
                                            aria-label={tailText(lang, `Remove keyword rule ${idx + 1}`, `删除关键字规则 ${idx + 1}`)}
                                            onClick={() => {
                                                const keywords = [...(draft.keywords || [])];
                                                keywords.splice(idx, 1);
                                                setDraft({ ...draft, keywords });
                                            }}
                                        >
                                            {tailText(lang, 'Remove', '删除')}
                                        </button>
                                    </div>
                                    <label className="utilities-field">
                                        <span>{t.kwList}</span>
                                        <input
                                            value={(rule.keywords || []).join(', ')}
                                            onChange={(e) =>
                                                updateKw(idx, {
                                                    keywords: e.target.value
                                                        .split(/[,，]/)
                                                        .map((x) => x.trim())
                                                        .filter(Boolean),
                                                })
                                            }
                                        />
                                    </label>
                                    <div className="watch-keyword-rule__toggles">
                                        <label className="utilities-field utilities-field--row">
                                            <input type="checkbox" checked={!!rule.record_on_match} onChange={(e) => updateKw(idx, { record_on_match: e.target.checked })} />
                                            <span>{t.recordKw}</span>
                                        </label>
                                        <label className="utilities-field utilities-field--row">
                                            <input type="checkbox" checked={!!rule.forward_on_match} onChange={(e) => updateKw(idx, { forward_on_match: e.target.checked })} />
                                            <span>{t.forwardKw}</span>
                                        </label>
                                    </div>
                                    <div className="watch-keyword-rule__fields">
                                    <label className="utilities-field">
                                        <span>{t.reply}</span>
                                        <textarea
                                            rows={2}
                                            value={rule.reply_text || ''}
                                            onChange={(e) => updateKw(idx, { reply_text: e.target.value })}
                                        />
                                    </label>
                                    <label className="utilities-field">
                                        <span>{t.cli}</span>
                                        <textarea
                                            rows={2}
                                            value={rule.cli_command || ''}
                                            onChange={(e) => updateKw(idx, { cli_command: e.target.value })}
                                            placeholder={
                                                tailText(lang, 'e.g. python /hooks/watch.py --who={{speaker_id}}', '例: python C:\\hooks\\watch.py --who={{speaker_id}}')
                                            }
                                        />
                                    </label>
                                    </div>
                                    {(rule.cli_command || '').trim() ? (
                                        <div className="watch-keyword-rule__cli-extra">
                                            <label className="utilities-field utilities-field--row">
                                                <input type="checkbox" checked={rule.reply_with_cli_stdout !== false} onChange={(e) => updateKw(idx, { reply_with_cli_stdout: e.target.checked })} />
                                                <span>{tailText(lang, 'Reply with CLI stdout', '用 CLI 标准输出作为回复')}</span>
                                            </label>
                                            <span className="utilities-meta">{t.cliHint}</span>
                                        </div>
                                    ) : null}
                                </div>
                            ))}
                        </div>

                        {draft.id ? (
                            <div className="utilities-watch-section">
                                <div className="utilities-field__row utilities-watch-logs__head">
                                    <h3>{t.logs}</h3>
                                    {storePath ? (
                                        <span className="utilities-meta utilities-watch-logs__store" title={storePath}>
                                            {t.store}: {storePath}
                                        </span>
                                    ) : null}
                                    <button type="button" className="utilities-btn utilities-btn--ghost" onClick={() => void loadLogs(draft.id)}>
                                        {t.refreshLogs}
                                    </button>
                                </div>
                                <ul className="utilities-log-list">
                                    {logs.map((p) => (
                                        <li key={p}>
                                            <code title={p}>{p.split(/[\\/]/).pop() || p}</code>
                                            <button type="button" className="utilities-btn utilities-btn--ghost" onClick={() => void openLog(p)}>
                                                {t.openLog}
                                            </button>
                                        </li>
                                    ))}
                                </ul>
                                {logContent ? (
                                    <pre className="utilities-log-view">{logContent}</pre>
                                ) : null}
                            </div>
                        ) : null}

                        {Boolean(0) && Boolean(isZh) && (
                                    <p className="utilities-meta utilities-watch-files-empty" role="status">{tailText(lang, 'Loading files…', '正在加载文件…')}</p>
                        )}

                        <div className="utilities-actions utilities-watch-actions">
                            <button type="button" className="utilities-btn utilities-btn--danger" disabled={busy} onClick={() => void remove()}>
                                {t.del}
                            </button>
                            <button type="button" className="utilities-btn utilities-btn--primary" disabled={busy} onClick={() => void save()}>
                                {t.save}
                            </button>
                        </div>
                    </section>
                ) : (
                    <section className="utilities-watch-editor utilities-empty utilities-watch-editor--empty">
                        <p>{tailText(lang, 'Select a job on the left to edit, or click "New job" above.', '从左侧选择一个任务进行编辑，或点击上方「新建任务」。')}</p>
                    </section>
                )}
            </div>
        </div>
    );
}

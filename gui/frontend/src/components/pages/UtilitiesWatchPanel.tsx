import { useCallback, useEffect, useMemo, useState } from 'react';
import { parseWailsJSON } from './UtilitiesPage';

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
    detail?: string;
    enabled?: boolean;
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

async function getApp(): Promise<any | null> {
    try {
        return await import('../../../wailsjs/go/main/App');
    } catch {
        return null;
    }
}

function emptyJob(): WatchJob {
    return {
        name: '盯人任务',
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
}: {
    isZh: boolean;
    onBack: () => void;
}) {
    const t = useMemo(
        () =>
            isZh
                ? {
                      title: '盯人',
                      back: '返回',
                      list: '任务列表',
                      create: '新建任务',
                      save: '保存',
                      del: '删除',
                      enabled: '启用',
                      name: '名称',
                      group: '蓝信群',
                      loadGroups: '刷新群列表',
                      members: '群成员（可过滤）',
                      filter: '按姓名/ID 过滤',
                      addManual: '手动添加 staffId',
                      recordAll: '记录目标用户全部发言（文本日志）',
                      kwScope: '关键字匹配范围',
                      kwScopeTargets: '仅关注的人',
                      kwScopeAnyone: '群内任何人',
                      forwardSpeech: '关注对象发言时转发到我的 IM 通道',
                      forwardChannels: '转发到我的通道（勾选在线通道）',
                      forwardKw: '关键字命中时也转发到上述通道',
                      keywords: '关键字规则',
                      addKw: '添加规则',
                      kwList: '关键字（逗号分隔）',
                      reply: '固定回复（回源群）',
                      cli: 'CLI 命令',
                      cliHint:
                          '可用占位符：{{date}} {{content}} {{speaker_id}} {{speaker_name}} {{group_id}} {{group_name}} {{keyword}}。无占位符时自动追加 --date/--content/--speaker-id/--group-id/--keyword。环境变量 LANXIN_WATCH_* 同步注入。stdout 非空则回给用户（源群）。',
                      recordKw: '命中时写入关键字日志',
                      targets: '盯住的人',
                      logs: '发言记录',
                      refreshLogs: '刷新日志',
                      openLog: '查看',
                      store: '数据目录',
                      note: '说明：转发是推到「你自己」绑定的 IM 通道（微信/蓝信/Hub 等），不是转给其他人。需该通道在线且你曾与机器人有过私聊会话。成员列表来自消息学习或手动添加。',
                      empty: '暂无盯人任务',
                      saveOk: '已保存',
                      pickGroup: '请选择群',
                      pickTarget: '请至少选择一个盯人对象（关键字选「任何人」时可暂不选，但建议仍配置关注列表）',
                  }
                : {
                      title: 'Watch',
                      back: 'Back',
                      list: 'Jobs',
                      create: 'New job',
                      save: 'Save',
                      del: 'Delete',
                      enabled: 'Enabled',
                      name: 'Name',
                      group: 'Lansenger group',
                      loadGroups: 'Refresh groups',
                      members: 'Members (filterable)',
                      filter: 'Filter by name/id',
                      addManual: 'Add staffId manually',
                      recordAll: 'Record all speech from targets (text log)',
                      kwScope: 'Keyword scope',
                      kwScopeTargets: 'Watched people only',
                      kwScopeAnyone: 'Anyone in the group',
                      forwardSpeech: 'Forward watched speech to my IM channels',
                      forwardChannels: 'Forward to my channels (online only preferred)',
                      forwardKw: 'Also forward on keyword hit',
                      keywords: 'Keyword rules',
                      addKw: 'Add rule',
                      kwList: 'Keywords (comma-separated)',
                      reply: 'Static reply (to source group)',
                      cli: 'CLI command',
                      cliHint:
                          'Placeholders: {{date}} {{content}} {{speaker_id}} {{speaker_name}} {{group_id}} {{group_name}} {{keyword}}. Without placeholders, flags are appended. Env LANXIN_WATCH_* is set. Non-empty stdout replies to the source group.',
                      recordKw: 'Log on keyword hit',
                      targets: 'Watched people',
                      logs: 'Transcripts',
                      refreshLogs: 'Refresh logs',
                      openLog: 'View',
                      store: 'Data directory',
                      note: 'Forward pushes packaged speech to your own online IM channels (WeChat/Lansenger/Hub), not other people.',
                      empty: 'No watch jobs yet',
                      saveOk: 'Saved',
                      pickGroup: 'Select a group',
                      pickTarget: 'Select at least one target (optional if keyword scope is anyone)',
                  },
        [isZh],
    );

    const [jobs, setJobs] = useState<WatchJob[]>([]);
    const [draft, setDraft] = useState<WatchJob | null>(null);
    const [groups, setGroups] = useState<GroupRow[]>([]);
    const [members, setMembers] = useState<Member[]>([]);
    const [memberQuery, setMemberQuery] = useState('');
    const [manualId, setManualId] = useState('');
    const [manualName, setManualName] = useState('');
    const [logs, setLogs] = useState<string[]>([]);
    const [logContent, setLogContent] = useState('');
    const [storePath, setStorePath] = useState('');
    const [error, setError] = useState('');
    const [hint, setHint] = useState('');
    const [busy, setBusy] = useState(false);
    const [channels, setChannels] = useState<IMChannel[]>([]);

    const loadJobs = useCallback(async () => {
        const app = await getApp();
        if (!app?.ListLansengerWatchJobs) {
            setError(isZh ? '后端未暴露盯人接口（请重新编译桌面端）' : 'Watch APIs missing (rebuild desktop)');
            return;
        }
        const raw = await app.ListLansengerWatchJobs();
        setJobs(parseWailsJSON<WatchJob[]>(raw) || []);
        if (app.GetLansengerWatchStorePath) {
            setStorePath((await app.GetLansengerWatchStorePath()) || '');
        }
        if (app.ListLansengerWatchChannels) {
            try {
                const chRaw = await app.ListLansengerWatchChannels();
                setChannels(parseWailsJSON<IMChannel[]>(chRaw) || []);
            } catch {
                /* ignore */
            }
        }
    }, [isZh]);

    const loadGroups = useCallback(async () => {
        const app = await getApp();
        if (!app?.ListLansengerGroups) return;
        try {
            const res = await app.ListLansengerGroups();
            const parsed = parseWailsJSON<{ groups?: GroupRow[] }>(res) || res;
            setGroups((parsed?.groups || []) as GroupRow[]);
        } catch (e: any) {
            setError(e?.message || String(e));
        }
    }, []);

    const loadMembers = useCallback(
        async (groupId: string, q: string) => {
            if (!groupId) {
                setMembers([]);
                return;
            }
            const app = await getApp();
            if (!app?.ListLansengerWatchRoster) return;
            try {
                const raw = await app.ListLansengerWatchRoster(groupId, q || '');
                const parsed = parseWailsJSON<{ members?: Member[] }>(raw);
                setMembers(parsed?.members || []);
            } catch (e: any) {
                setError(e?.message || String(e));
            }
        },
        [],
    );

    const loadLogs = useCallback(async (jobId?: string) => {
        if (!jobId) {
            setLogs([]);
            return;
        }
        const app = await getApp();
        if (!app?.ListLansengerWatchTranscripts) return;
        const raw = await app.ListLansengerWatchTranscripts(jobId);
        setLogs(parseWailsJSON<string[]>(raw) || []);
    }, []);

    useEffect(() => {
        void loadJobs();
        void loadGroups();
    }, [loadJobs, loadGroups]);

    useEffect(() => {
        if (draft?.group_id) {
            void loadMembers(draft.group_id, memberQuery);
        }
    }, [draft?.group_id, memberQuery, loadMembers]);

    useEffect(() => {
        if (draft?.id) void loadLogs(draft.id);
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
        const scopeAnyone = (draft.keyword_scope || 'targets') === 'anyone';
        const hasKeywordRules = (draft.keywords || []).some((k) => (k.keywords || []).length > 0);
        if ((draft.record_all || draft.forward_on_target_speech || !scopeAnyone) && !draft.target_staff_ids?.length) {
            // targets-scope keywords / record / speech-forward need watch list
            if (!scopeAnyone || draft.record_all || draft.forward_on_target_speech) {
                setError(t.pickTarget);
                return;
            }
        }
        if (!hasKeywordRules && !draft.record_all && !draft.forward_on_target_speech) {
            setError(isZh ? '请至少启用：记录发言 / 关键字 / 发言转发 之一' : 'Enable record, keywords, or speech forward');
            return;
        }
        if (
            (draft.forward_on_target_speech || (draft.keywords || []).some((k) => k.forward_on_match)) &&
            !(draft.forward_channels || []).length
        ) {
            setError(isZh ? '启用转发时请至少勾选一个 IM 通道' : 'Select at least one IM channel for forward');
            return;
        }
        setBusy(true);
        setError('');
        try {
            const app = await getApp();
            const raw = await app.UpsertLansengerWatchJob(JSON.stringify(draft));
            const saved = parseWailsJSON<WatchJob>(raw);
            setDraft(saved);
            setHint(t.saveOk);
            setTimeout(() => setHint(''), 2000);
            await loadJobs();
        } catch (e: any) {
            setError(e?.message || String(e));
        } finally {
            setBusy(false);
        }
    };

    const remove = async () => {
        if (!draft?.id) {
            setDraft(null);
            return;
        }
        if (!window.confirm(isZh ? '确认删除该任务？日志文件会保留。' : 'Delete this job? Logs are kept.')) return;
        setBusy(true);
        try {
            const app = await getApp();
            await app.DeleteLansengerWatchJob(draft.id);
            setDraft(null);
            await loadJobs();
        } catch (e: any) {
            setError(e?.message || String(e));
        } finally {
            setBusy(false);
        }
    };

    const addManual = async () => {
        if (!draft?.group_id || !manualId.trim()) return;
        const app = await getApp();
        await app.AddLansengerWatchMember(draft.group_id, manualId.trim(), manualName.trim());
        toggleTarget(manualId.trim(), manualName.trim());
        setManualId('');
        setManualName('');
        await loadMembers(draft.group_id, memberQuery);
    };

    const openLog = async (path: string) => {
        const app = await getApp();
        const text = await app.ReadLansengerWatchTranscript(path);
        setLogContent(text || '');
    };

    const updateKw = (idx: number, patch: Partial<KeywordRule>) => {
        if (!draft) return;
        const keywords = [...(draft.keywords || [])];
        keywords[idx] = { ...keywords[idx], ...patch };
        setDraft({ ...draft, keywords });
    };

    return (
        <div className="utilities-page" data-testid="watch-page">
            <div className="utilities-page__header utilities-page__header--row">
                <div>
                    <button type="button" className="utilities-link" onClick={onBack}>
                        {t.back}
                    </button>
                    <h1 className="utilities-page__title">{t.title}</h1>
                    <p className="utilities-page__subtitle">{t.note}</p>
                    {storePath ? (
                        <p className="utilities-meta">
                            {t.store}: {storePath}
                        </p>
                    ) : null}
                </div>
                <div className="utilities-actions">
                    <button type="button" className="utilities-btn" onClick={() => setDraft(emptyJob())}>
                        {t.create}
                    </button>
                </div>
            </div>

            {error ? <div className="utilities-error">{error}</div> : null}
            {hint ? <div className="utilities-hint">{hint}</div> : null}

            <div className="utilities-watch-layout">
                <aside className="utilities-watch-list">
                    <h3>{t.list}</h3>
                    {jobs.length === 0 ? <div className="utilities-empty">{t.empty}</div> : null}
                    {jobs.map((j) => (
                        <button
                            key={j.id}
                            type="button"
                            className={`utilities-watch-item${draft?.id === j.id ? ' is-active' : ''}`}
                            onClick={() => setDraft({ ...emptyJob(), ...j, keywords: j.keywords?.length ? j.keywords : emptyJob().keywords })}
                        >
                            <div className="utilities-watch-item__title">
                                {j.enabled ? '●' : '○'} {j.name || j.id}
                            </div>
                            <div className="utilities-watch-item__meta">
                                {j.group_name || j.group_id} · {(j.target_staff_ids || []).length} 人
                            </div>
                        </button>
                    ))}
                </aside>

                {draft ? (
                    <section className="utilities-watch-editor">
                        <label className="utilities-field">
                            <span>{t.name}</span>
                            <input
                                value={draft.name}
                                onChange={(e) => setDraft({ ...draft, name: e.target.value })}
                            />
                        </label>
                        <label className="utilities-field utilities-field--row">
                            <input
                                type="checkbox"
                                checked={!!draft.enabled}
                                onChange={(e) => setDraft({ ...draft, enabled: e.target.checked })}
                            />
                            <span>{t.enabled}</span>
                        </label>
                        <label className="utilities-field">
                            <span>{t.group}</span>
                            <div className="utilities-field__row">
                                <select
                                    value={draft.group_id}
                                    onChange={(e) => {
                                        const g = groups.find((x) => x.group_id === e.target.value);
                                        setDraft({
                                            ...draft,
                                            group_id: e.target.value,
                                            group_name: g?.name || draft.group_name,
                                        });
                                    }}
                                >
                                    <option value="">{t.pickGroup}</option>
                                    {groups.map((g) => (
                                        <option key={g.group_id} value={g.group_id}>
                                            {g.name || g.group_id}
                                            {g.total_members != null ? ` (${g.total_members})` : ''}
                                        </option>
                                    ))}
                                </select>
                                <button type="button" className="utilities-btn utilities-btn--ghost" onClick={() => void loadGroups()}>
                                    {t.loadGroups}
                                </button>
                            </div>
                        </label>

                        <label className="utilities-field utilities-field--row">
                            <input
                                type="checkbox"
                                checked={!!draft.record_all}
                                onChange={(e) => setDraft({ ...draft, record_all: e.target.checked })}
                            />
                            <span>{t.recordAll}</span>
                        </label>

                        <label className="utilities-field">
                            <span>{t.kwScope}</span>
                            <select
                                value={draft.keyword_scope || 'targets'}
                                onChange={(e) =>
                                    setDraft({
                                        ...draft,
                                        keyword_scope: e.target.value === 'anyone' ? 'anyone' : 'targets',
                                    })
                                }
                            >
                                <option value="targets">{t.kwScopeTargets}</option>
                                <option value="anyone">{t.kwScopeAnyone}</option>
                            </select>
                        </label>

                        <label className="utilities-field utilities-field--row">
                            <input
                                type="checkbox"
                                checked={!!draft.forward_on_target_speech}
                                onChange={(e) => setDraft({ ...draft, forward_on_target_speech: e.target.checked })}
                            />
                            <span>{t.forwardSpeech}</span>
                        </label>
                        <div className="utilities-field">
                            <span>{t.forwardChannels}</span>
                            <div className="utilities-chip-row" style={{ marginTop: 6 }}>
                                {(channels.length
                                    ? channels
                                    : [
                                          { id: 'weixin', label: isZh ? '微信' : 'WeChat', online: false },
                                          { id: 'lansenger', label: isZh ? '蓝信' : 'Lansenger', online: false },
                                          { id: 'hub', label: 'Hub', online: false },
                                      ]
                                ).map((ch) => {
                                    const on = (draft.forward_channels || []).includes(ch.id);
                                    return (
                                        <button
                                            key={ch.id}
                                            type="button"
                                            className={`utilities-chip${on ? ' is-on' : ''}`}
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
                                            {ch.online ? (isZh ? ' ·在线' : ' ·on') : ch.enabled === false ? (isZh ? ' ·未启用' : ' ·off') : ''}
                                        </button>
                                    );
                                })}
                            </div>
                            <span className="utilities-meta">
                                {isZh
                                    ? '推送到你自己在该通道与机器人的会话（非转给其他人）。Hub = 最近活跃绑定 IM。'
                                    : 'Pushes to your own bot session on that channel. Hub = last active bound IM.'}
                            </span>
                        </div>

                        <div className="utilities-watch-section">
                            <h3>
                                {t.targets} ({(draft.target_staff_ids || []).length})
                            </h3>
                            <div className="utilities-chip-row">
                                {(draft.target_staff_ids || []).map((id) => (
                                    <button key={id} type="button" className="utilities-chip is-on" onClick={() => toggleTarget(id)}>
                                        {(draft.target_names || {})[id] || id} ×
                                    </button>
                                ))}
                            </div>
                            <h4>{t.members}</h4>
                            <input
                                className="utilities-input"
                                placeholder={t.filter}
                                value={memberQuery}
                                onChange={(e) => setMemberQuery(e.target.value)}
                            />
                            <div className="utilities-member-list">
                                {members.map((m) => {
                                    const on = (draft.target_staff_ids || []).includes(m.staff_id);
                                    return (
                                        <button
                                            key={m.staff_id}
                                            type="button"
                                            className={`utilities-member${on ? ' is-on' : ''}`}
                                            onClick={() => toggleTarget(m.staff_id, m.name)}
                                        >
                                            <strong>{m.name || m.staff_id}</strong>
                                            <span>{m.staff_id}</span>
                                        </button>
                                    );
                                })}
                                {members.length === 0 ? (
                                    <div className="utilities-empty">{isZh ? '暂无成员，可手动添加或等待群消息学习' : 'No members yet'}</div>
                                ) : null}
                            </div>
                            <div className="utilities-field__row">
                                <input
                                    className="utilities-input"
                                    placeholder="staffId"
                                    value={manualId}
                                    onChange={(e) => setManualId(e.target.value)}
                                />
                                <input
                                    className="utilities-input"
                                    placeholder={isZh ? '姓名（可选）' : 'Name (optional)'}
                                    value={manualName}
                                    onChange={(e) => setManualName(e.target.value)}
                                />
                                <button type="button" className="utilities-btn utilities-btn--ghost" onClick={() => void addManual()}>
                                    {t.addManual}
                                </button>
                            </div>
                        </div>

                        <div className="utilities-watch-section">
                            <div className="utilities-field__row">
                                <h3>{t.keywords}</h3>
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
                                                    forward_on_match: false,
                                                },
                                            ],
                                        })
                                    }
                                >
                                    {t.addKw}
                                </button>
                            </div>
                            {(draft.keywords || []).map((rule, idx) => (
                                <div key={rule.id || idx} className="utilities-kw-card">
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
                                    <label className="utilities-field utilities-field--row">
                                        <input
                                            type="checkbox"
                                            checked={!!rule.record_on_match}
                                            onChange={(e) => updateKw(idx, { record_on_match: e.target.checked })}
                                        />
                                        <span>{t.recordKw}</span>
                                    </label>
                                    <label className="utilities-field utilities-field--row">
                                        <input
                                            type="checkbox"
                                            checked={!!rule.forward_on_match}
                                            onChange={(e) => updateKw(idx, { forward_on_match: e.target.checked })}
                                        />
                                        <span>{t.forwardKw}</span>
                                    </label>
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
                                                isZh
                                                    ? '例: python C:\\hooks\\watch.py --who={{speaker_id}}'
                                                    : 'e.g. python /hooks/watch.py --who={{speaker_id}}'
                                            }
                                        />
                                        <span className="utilities-meta">{t.cliHint}</span>
                                    </label>
                                    <label className="utilities-field utilities-field--row">
                                        <input
                                            type="checkbox"
                                            checked={rule.reply_with_cli_stdout !== false}
                                            onChange={(e) => updateKw(idx, { reply_with_cli_stdout: e.target.checked })}
                                        />
                                        <span>{isZh ? '用 CLI 标准输出作为回复' : 'Reply with CLI stdout'}</span>
                                    </label>
                                    <button
                                        type="button"
                                        className="utilities-link"
                                        onClick={() => {
                                            const keywords = [...(draft.keywords || [])];
                                            keywords.splice(idx, 1);
                                            setDraft({ ...draft, keywords });
                                        }}
                                    >
                                        {isZh ? '删除此规则' : 'Remove rule'}
                                    </button>
                                </div>
                            ))}
                        </div>

                        {draft.id ? (
                            <div className="utilities-watch-section">
                                <div className="utilities-field__row">
                                    <h3>{t.logs}</h3>
                                    <button type="button" className="utilities-btn utilities-btn--ghost" onClick={() => void loadLogs(draft.id)}>
                                        {t.refreshLogs}
                                    </button>
                                </div>
                                <ul className="utilities-log-list">
                                    {logs.map((p) => (
                                        <li key={p}>
                                            <button type="button" className="utilities-link" onClick={() => void openLog(p)}>
                                                {t.openLog}
                                            </button>{' '}
                                            <code>{p}</code>
                                        </li>
                                    ))}
                                </ul>
                                {logContent ? (
                                    <pre className="utilities-log-view">{logContent}</pre>
                                ) : null}
                            </div>
                        ) : null}

                        <div className="utilities-actions">
                            <button type="button" className="utilities-btn" disabled={busy} onClick={() => void save()}>
                                {t.save}
                            </button>
                            <button type="button" className="utilities-btn utilities-btn--danger" disabled={busy} onClick={() => void remove()}>
                                {t.del}
                            </button>
                        </div>
                    </section>
                ) : (
                    <section className="utilities-watch-editor utilities-empty">{isZh ? '选择或新建任务' : 'Select or create a job'}</section>
                )}
            </div>
        </div>
    );
}

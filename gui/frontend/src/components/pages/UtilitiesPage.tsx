import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import './UtilitiesPage.css';
import { EventsOn } from '../../../wailsjs/runtime';
import { EVENT_SURVEY_UPDATED } from '../../constants/events';
import {
    buildGroupBreakdown,
    buildPublishChecklist,
    buildSurveyOperatorHelp,
    buildSurveyShareSummary,
    buildUpdateSurveyPayload,
    completionPercent,
    createEmptyQuestion,
    deadlineListBadge,
    deadlineLocalToRFC3339,
    deadlineRFC3339ToLocal,
    detailToEditorQuestions,
    expandResponseAnswers,
    extractTextAnswers,
    filterResponsesByGroup,
    filterResponsesByOption,
    filterResponsesByQuery,
    filterSurveysByQuery,
    filterTextAnswers,
    formatCompletionBadge,
    formatShortCodeForCopy,
    formatSurveyIMCommand,
    isDeadlineExpired,
    moveOptionAt,
    moveQuestionAt,
    newOptionId,
    publishChecklistReady,
    selectArchivableIds,
    selectDeletableIds,
    shouldWarnExportPII,
    sortSurveys,
    surveyStatusBadgeClass,
    surveyStatusLabel,
    type EditorQuestion,
    type SurveyListSort,
} from './utilitiesSurveyEditor';
import { parseWailsJSON } from './utilitiesParse';
import { meetingRecordBaseTitle, meetingRecordCardDesc } from './utilitiesMeetingRecord';

export { parseWailsJSON } from './utilitiesParse';

type View = 'home' | 'survey-list' | 'survey-edit' | 'survey-results';

type SurveySummary = {
    id: string;
    short_code: string;
    title: string;
    status: string;
    description?: string;
    updated_at?: string;
    created_at?: string;
    binding_count?: number;
    question_count?: number;
    response_count?: number;
    settings?: {
        deadline?: string;
        target_count?: number;
        anonymous?: boolean;
    };
};

type SurveyDetail = SurveySummary & {
    questions?: Array<{
        id: string;
        type: string;
        title: string;
        required?: boolean;
        options?: Array<{ id: string; label: string }>;
        min?: number;
        max?: number;
        max_length?: number;
    }>;
    bindings?: Array<{ platform: string; group_id: string; group_name?: string }>;
    settings?: {
        anonymous?: boolean;
        allow_update?: boolean;
        allow_p2p?: boolean;
        target_count?: number;
        deadline?: string;
    };
    /** Present when publish requested announce and some groups failed. */
    announce_failures?: string[];
};

async function getApp(): Promise<any | null> {
    try {
        return await import('../../../wailsjs/go/main/App');
    } catch {
        return null;
    }
}

export const UtilitiesPage = ({
    lang,
    onStartMeetingRecord,
}: {
    lang?: string;
    /** Open a new agent tab and send the meeting-recording command. */
    onStartMeetingRecord?: () => void | Promise<void>;
}) => {
    const isZh = !lang || lang.startsWith('zh');
    const [view, setView] = useState<View>('home');
    const [meetingStarting, setMeetingStarting] = useState(false);
    const [surveys, setSurveys] = useState<SurveySummary[]>([]);
    const [error, setError] = useState('');
    const [hubOk, setHubOk] = useState<boolean | null>(null);
    const [selected, setSelected] = useState<SurveyDetail | null>(null);
    const [draftTitle, setDraftTitle] = useState('');
    const [draftDesc, setDraftDesc] = useState('');
    const [draftAnonymous, setDraftAnonymous] = useState(false);
    const [draftAllowUpdate, setDraftAllowUpdate] = useState(false);
    const [draftAllowP2P, setDraftAllowP2P] = useState(false);
    const [draftDeadlineLocal, setDraftDeadlineLocal] = useState('');
    const [draftTargetCount, setDraftTargetCount] = useState('');
    const [editQuestions, setEditQuestions] = useState<EditorQuestion[]>([
        createEmptyQuestion('single_choice', [], { title: isZh ? '满意吗' : 'Satisfied?', optA: isZh ? '是' : 'Yes', optB: isZh ? '否' : 'No' }),
    ]);
    const [groups, setGroups] = useState<Array<{ group_id: string; name: string }>>([]);
    const [stats, setStats] = useState<any>(null);
    const [responses, setResponses] = useState<any[]>([]);
    const [busy, setBusy] = useState(false);
    const [statusFilter, setStatusFilter] = useState<'all' | 'draft' | 'published' | 'closed' | 'archived'>('all');
    const [listQuery, setListQuery] = useState('');
    const [listSort, setListSort] = useState<SurveyListSort>('updated_desc');
    const [exportHint, setExportHint] = useState('');
    const [copyHint, setCopyHint] = useState('');
    const [selectedIds, setSelectedIds] = useState<string[]>([]);
    const [optionFilter, setOptionFilter] = useState<{ qid: string; oid: string }>({ qid: '', oid: '' });
    const [groupFilter, setGroupFilter] = useState('');
    const [responseQuery, setResponseQuery] = useState('');
    const [textAnswerQuery, setTextAnswerQuery] = useState('');
    const [showHelp, setShowHelp] = useState(false);
    const meetingStartingRef = useRef(false);
    const mountedRef = useRef(true);
    const selectedIdRef = useRef<string | null>(null);
    const viewRef = useRef<View>(view);
    const busyRef = useRef(busy);
    const selectedStatusRef = useRef<string | undefined>(selected?.status);
    selectedIdRef.current = selected?.id || null;
    viewRef.current = view;
    busyRef.current = busy;
    selectedStatusRef.current = selected?.status;

    useEffect(() => {
        mountedRef.current = true;
        return () => {
            mountedRef.current = false;
        };
    }, []);

    const copyText = async (text: string, okMsg: string) => {
        const value = (text || '').trim();
        if (!value) return;
        try {
            if (navigator.clipboard?.writeText) {
                await navigator.clipboard.writeText(value);
            } else {
                const ta = document.createElement('textarea');
                ta.value = value;
                document.body.appendChild(ta);
                ta.select();
                document.execCommand('copy');
                document.body.removeChild(ta);
            }
            setCopyHint(okMsg);
            setTimeout(() => setCopyHint(''), 2000);
        } catch {
            setCopyHint(isZh ? '复制失败' : 'Copy failed');
        }
    };

    const filteredSurveys = useMemo(
        () => sortSurveys(filterSurveysByQuery(surveys, listQuery), listSort),
        [surveys, listQuery, listSort],
    );

    const archivableSelected = useMemo(
        () => selectArchivableIds(filteredSurveys, selectedIds),
        [filteredSurveys, selectedIds],
    );

    const deletableSelected = useMemo(
        () => selectDeletableIds(filteredSurveys, selectedIds),
        [filteredSurveys, selectedIds],
    );

    const choiceFilterOptions = useMemo(() => {
        const qs = selected?.questions || [];
        const out: Array<{ qid: string; qtitle: string; oid: string; olabel: string }> = [];
        for (const q of qs) {
            if (q.type !== 'single_choice' && q.type !== 'multi_choice') continue;
            for (const o of q.options || []) {
                out.push({ qid: q.id, qtitle: q.title, oid: o.id, olabel: o.label });
            }
        }
        return out;
    }, [selected]);

    const qMetaForFilter = useMemo(
        () =>
            (selected?.questions || []).map((q) => ({
                id: q.id,
                type: q.type,
                title: q.title,
                options: q.options,
            })),
        [selected],
    );

    const filteredResponses = useMemo(() => {
        let list = filterResponsesByGroup(responses, groupFilter);
        list = filterResponsesByOption(list, optionFilter.qid, optionFilter.oid);
        list = filterResponsesByQuery(list, qMetaForFilter, responseQuery);
        return list;
    }, [responses, groupFilter, optionFilter, responseQuery, qMetaForFilter]);

    const groupScopedResponses = useMemo(
        () => filterResponsesByGroup(responses, groupFilter),
        [responses, groupFilter],
    );

    const publishChecks = useMemo(() => {
        if (!selected || selected.status !== 'draft') return [];
        // Prefer live editor questions when drafting; bindings from selected
        const qs = editQuestions.map((q) => ({
            title: q.title,
            type: q.type,
            options: (q.options || []).map((o) => ({ label: o.label })),
        }));
        let deadlineISO: string | undefined;
        try {
            deadlineISO = draftDeadlineLocal
                ? deadlineLocalToRFC3339(draftDeadlineLocal)
                : selected.settings?.deadline;
        } catch {
            deadlineISO = selected.settings?.deadline;
        }
        return buildPublishChecklist(
            {
                status: selected.status,
                questions: qs,
                bindings: selected.bindings || [],
                deadline: deadlineISO,
            },
            {
                draft: isZh ? '状态为草稿' : 'Status is draft',
                hasQuestions: isZh ? '至少一道有效题目' : 'At least one valid question',
                choiceOptions: isZh ? '选择题 ≥2 个选项' : 'Choice questions have ≥2 options',
                hasBindings: isZh ? '至少绑定 1 个蓝信群' : 'At least one Lansenger group bound',
                deadlineOk: isZh ? '截止时间未过期' : 'Deadline not already past',
            },
        );
    }, [selected, editQuestions, draftDeadlineLocal, isZh]);

    const canPublishNow = publishChecklistReady(publishChecks);

    const toggleSelect = (id: string) => {
        setSelectedIds((prev) => (prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id]));
    };

    const toggleSelectAllVisible = () => {
        const ids = filteredSurveys.map((s) => s.id);
        const allOn = ids.length > 0 && ids.every((id) => selectedIds.includes(id));
        setSelectedIds(allOn ? selectedIds.filter((id) => !ids.includes(id)) : Array.from(new Set([...selectedIds, ...ids])));
    };

    const batchArchive = async () => {
        const ids = archivableSelected;
        if (ids.length === 0) {
            setError(isZh ? '请勾选草稿或已关闭的问卷' : 'Select draft or closed surveys');
            return;
        }
        if (!window.confirm(isZh ? `确认归档 ${ids.length} 个问卷？` : `Archive ${ids.length} survey(s)?`)) return;
        setBusy(true);
        setError('');
        try {
            const mod = await getApp();
            if (!mod?.ArchiveSurvey) throw new Error(t.hubOffline);
            const results = await Promise.allSettled(ids.map((id: string) => mod.ArchiveSurvey(id)));
            const failures = results
                .map((r, i) => (r.status === 'rejected' ? `${ids[i]}: ${String((r as PromiseRejectedResult).reason?.message || (r as PromiseRejectedResult).reason)}` : ''))
                .filter(Boolean);
            setSelectedIds([]);
            await loadList();
            if (failures.length) {
                setError((isZh ? '部分归档失败：' : 'Some failed: ') + failures.join('; '));
            } else {
                setCopyHint(isZh ? `已归档 ${ids.length} 个` : `Archived ${ids.length}`);
                setTimeout(() => setCopyHint(''), 2500);
            }
        } catch (e: any) {
            setError(String(e?.message || e));
        } finally {
            setBusy(false);
        }
    };

    const batchDelete = async () => {
        const ids = deletableSelected;
        if (ids.length === 0) {
            setError(isZh ? '请勾选草稿或已归档的问卷' : 'Select draft or archived surveys');
            return;
        }
        if (!window.confirm(isZh ? `确认永久删除 ${ids.length} 个问卷？不可恢复。` : `Permanently delete ${ids.length} survey(s)?`)) return;
        setBusy(true);
        setError('');
        try {
            const mod = await getApp();
            if (!mod?.DeleteSurvey) throw new Error(t.hubOffline);
            const results = await Promise.allSettled(ids.map((id: string) => mod.DeleteSurvey(id)));
            const failures = results
                .map((r, i) => (r.status === 'rejected' ? `${ids[i]}: ${String((r as PromiseRejectedResult).reason?.message || (r as PromiseRejectedResult).reason)}` : ''))
                .filter(Boolean);
            setSelectedIds([]);
            if (selected?.id && ids.includes(selected.id)) {
                setSelected(null);
            }
            await loadList();
            if (failures.length) {
                setError((isZh ? '部分删除失败：' : 'Some failed: ') + failures.join('; '));
            } else {
                setCopyHint(isZh ? `已删除 ${ids.length} 个` : `Deleted ${ids.length}`);
                setTimeout(() => setCopyHint(''), 2500);
            }
        } catch (e: any) {
            setError(String(e?.message || e));
        } finally {
            setBusy(false);
        }
    };

    const t = useMemo(() => ({
        title: isZh ? '实用工具' : 'Utilities',
        subtitle: isZh
            ? '面向群场景与日常办公的轻量工具（问卷、会议记录等）'
            : 'Lightweight tools for IM groups and daily work (surveys, meeting notes, …)',
        surveyCard: isZh ? '调查问卷' : 'Surveys',
        surveyDesc: isZh ? '创建问卷、绑定蓝信群、查看结果并导出 Excel' : 'Create surveys, bind Lansenger groups, view results, export Excel',
        meetingCard: meetingRecordBaseTitle(lang),
        meetingDesc: meetingRecordCardDesc(lang),
        open: isZh ? '进入' : 'Open',
        start: lang === 'zh-Hant' ? '開始' : isZh ? '开始' : 'Start',
        starting: lang === 'zh-Hant' ? '啟動中…' : isZh ? '启动中…' : 'Starting…',
        back: isZh ? '返回' : 'Back',
        hubOffline: isZh ? '未连接 Hub。问卷数据保存在 Hub，请先注册/连接 Hub。' : 'Hub is offline. Survey data lives on Hub — connect first.',
        create: isZh ? '新建问卷' : 'New survey',
        save: isZh ? '保存草稿' : 'Save draft',
        publish: isZh ? '发布' : 'Publish',
        bind: isZh ? '绑定选中群' : 'Bind selected group',
        unbind: isZh ? '解绑' : 'Unbind',
        results: isZh ? '结果' : 'Results',
        export: isZh ? '导出 Excel' : 'Export Excel',
        empty: isZh ? '暂无问卷' : 'No surveys yet',
        refresh: isZh ? '刷新' : 'Refresh',
        close: isZh ? '关闭' : 'Close',
        reopen: isZh ? '重新打开' : 'Reopen',
        archive: isZh ? '归档' : 'Archive',
        duplicate: isZh ? '复制' : 'Duplicate',
        delete: isZh ? '删除' : 'Delete',
        announce: isZh ? '群内公告' : 'Announce',
        responses: isZh ? '答卷明细' : 'Responses',
        count: isZh ? '回收份数' : 'Responses',
        addQ: isZh ? '添加题目' : 'Add question',
        addOpt: isZh ? '加选项' : 'Add option',
        remove: isZh ? '删除' : 'Remove',
        deadline: isZh ? '截止时间' : 'Deadline',
        target: isZh ? '目标回收数' : 'Target responses',
        completion: isZh ? '完成率' : 'Completion',
        search: isZh ? '搜索标题或短码…' : 'Search title or code…',
        noMatch: isZh ? '没有匹配的问卷' : 'No matching surveys',
        sort: isZh ? '排序' : 'Sort',
        exporting: isZh ? '正在导出…' : 'Exporting…',
        copyCode: isZh ? '复制短码' : 'Copy code',
        copyCmd: isZh ? '复制命令' : 'Copy /survey',
        copied: isZh ? '已复制' : 'Copied',
        groups: isZh ? '群' : 'groups',
        questions: isZh ? '题' : 'qs',
        batchArchive: isZh ? '批量归档' : 'Archive selected',
        batchDelete: isZh ? '批量删除' : 'Delete selected',
        selectAll: isZh ? '全选可见' : 'Select visible',
        filterOption: isZh ? '按选项筛选答卷' : 'Filter by option',
        allOptions: isZh ? '全部答卷' : 'All responses',
        showing: isZh ? '显示' : 'Showing',
        exportFiltered: isZh ? '导出当前筛选' : 'Export filtered',
        expired: isZh ? '已截止' : 'Expired',
        notExpired: isZh ? '未截止' : 'Open',
        responsesShort: isZh ? '答' : 'resp',
        publishReady: isZh ? '可发布' : 'Ready to publish',
        publishBlocked: isZh ? '发布前请完成以下项' : 'Complete before publish',
        allowP2P: isZh ? '允许私聊填写（凭短码；默认关闭）' : 'Allow P2P fill by short code (default off)',
        searchResponses: isZh ? '搜索答卷…' : 'Search responses…',
        searchTextAnswers: isZh ? '搜索文本答案…' : 'Search text answers…',
        textAnswers: isZh ? '文本答案' : 'Text answers',
        bindings: isZh ? '绑定群' : 'Bound groups',
        liveHint: isZh ? '有新答卷，已自动刷新' : 'New response — refreshed',
        status: isZh ? '状态' : 'Status',
        moveUp: isZh ? '上移' : 'Up',
        moveDown: isZh ? '下移' : 'Down',
        byGroup: isZh ? '按群回收' : 'By group',
        p2pUnknown: isZh ? '私聊/未知群' : 'P2P / unknown',
        exportPiiWarn: isZh
            ? '导出文件将保存在本机，并可能包含答卷人姓名等个人信息。确认继续导出？'
            : 'The export file will be saved locally and may include respondent names (PII). Continue?',
        exportPiiNote: isZh
            ? '提示：非匿名问卷导出会含姓名等 PII，请妥善保管本地文件。'
            : 'Note: non-anonymous exports include names (PII). Keep local files secure.',
        exportAnonNote: isZh
            ? '匿名导出：respondent 列为 anonymous，不含真实姓名。'
            : 'Anonymous export: respondent columns are redacted (anonymous).',
        filterByGroup: isZh ? '点击群筛选答卷；再点取消' : 'Click group to filter; click again to clear',
        allGroups: isZh ? '全部群' : 'All groups',
        copySummary: isZh ? '复制摘要' : 'Copy summary',
        summaryCopied: isZh ? '摘要已复制' : 'Summary copied',
        print: isZh ? '打印' : 'Print',
        emptyHint: isZh ? '创建第一份问卷，绑定蓝信群后即可开始收集。' : 'Create a survey, bind Lansenger groups, then collect responses.',
        shortcutSave: isZh ? 'Ctrl+S 保存草稿' : 'Ctrl+S save draft',
        help: isZh ? '使用说明' : 'How to use',
        hideHelp: isZh ? '收起说明' : 'Hide help',
    }), [isZh, lang]);

    const handleMeetingRecord = useCallback(async () => {
        if (!onStartMeetingRecord || meetingStartingRef.current) return;
        meetingStartingRef.current = true;
        setMeetingStarting(true);
        try {
            await onStartMeetingRecord();
        } catch {
            // Parent surfaces errors (toast). Card re-enables in finally when still mounted.
        } finally {
            meetingStartingRef.current = false;
            // Success navigates away (unmounts this page); skip setState after unmount.
            if (mountedRef.current) setMeetingStarting(false);
        }
    }, [onStartMeetingRecord]);

    const operatorHelp = useMemo(() => buildSurveyOperatorHelp(isZh), [isZh]);

    const loadEditorFromDetail = (detail: SurveyDetail) => {
        setDraftTitle(detail.title || '');
        setDraftDesc(detail.description || '');
        setDraftAnonymous(!!detail.settings?.anonymous);
        setDraftAllowUpdate(!!detail.settings?.allow_update);
        setDraftAllowP2P(!!detail.settings?.allow_p2p);
        setDraftDeadlineLocal(deadlineRFC3339ToLocal(detail.settings?.deadline));
        setDraftTargetCount(
            detail.settings?.target_count && detail.settings.target_count > 0
                ? String(detail.settings.target_count)
                : '',
        );
        setEditQuestions(detailToEditorQuestions(detail));
    };

    const loadList = useCallback(async () => {
        setError('');
        const mod = await getApp();
        if (!mod?.ListSurveys) {
            setHubOk(false);
            setSurveys([]);
            return;
        }
        try {
            const raw = await mod.ListSurveys(JSON.stringify({ status: statusFilter }));
            const res = parseWailsJSON<{ surveys?: SurveySummary[] }>(raw) || {};
            const list = res.surveys || [];
            setSurveys(Array.isArray(list) ? list : []);
            setHubOk(true);
        } catch (e: any) {
            setHubOk(false);
            setSurveys([]);
            setError(String(e?.message || e || 'Hub unavailable'));
        }
    }, [statusFilter]);

    useEffect(() => {
        if (view === 'survey-list' || view === 'survey-edit') {
            void loadList();
        }
    }, [view, loadList]);

    const mapCreateError = (code: string) => {
        if (code === 'choice_needs_two_options') return isZh ? '选择题至少需要 2 个选项' : 'Choice needs 2 options';
        if (code === 'question_title_required') return isZh ? '题目不能为空' : 'Question title required';
        if (code === 'at_least_one_question') return isZh ? '至少一道题' : 'Need at least one question';
        if (code === 'invalid_deadline') return isZh ? '截止时间无效' : 'Invalid deadline';
        if (code === 'invalid_target_count') return isZh ? '目标回收数无效' : 'Invalid target count';
        if (code === 'rating_min_max') return isZh ? '评分题 min 不能大于 max' : 'Rating min cannot exceed max';
        if (code === 'invalid_max_length') return isZh ? '文本最大字数无效' : 'Invalid max length';
        if (code.includes('截止时间已过') || /deadline already passed|deadline.*past/i.test(code)) {
            return isZh ? '截止时间已过，请先修改截止时间再发布' : 'Deadline already past; update deadline before publish';
        }
        return code;
    };

    const settingsForPayload = () => ({
        anonymous: draftAnonymous,
        allow_update: draftAllowUpdate,
        allow_p2p: draftAllowP2P,
        target_count: draftTargetCount,
        deadline_local: draftDeadlineLocal,
    });

    const createSurvey = async () => {
        setBusy(true);
        setError('');
        try {
            const mod = await getApp();
            if (!mod?.CreateSurvey) throw new Error(t.hubOffline);
            let input;
            try {
                input = buildUpdateSurveyPayload({
                    title: draftTitle.trim() || (isZh ? '未命名问卷' : 'Untitled survey'),
                    description: draftDesc,
                    questions: editQuestions,
                    settings: settingsForPayload(),
                });
            } catch (e: any) {
                throw new Error(mapCreateError(String(e?.message || e)));
            }
            const raw = await mod.CreateSurvey(JSON.stringify(input));
            const created = parseWailsJSON<SurveyDetail>(raw);
            if (!created?.id) throw new Error(isZh ? '创建失败：无效响应' : 'Create failed: invalid response');
            setSelected(created);
            loadEditorFromDetail(created);
            setView('survey-edit');
            await loadList();
        } catch (e: any) {
            setError(String(e?.message || e));
        } finally {
            setBusy(false);
        }
    };

    /** Returns false when save fails (so publish can abort). */
    const saveDraft = async (): Promise<boolean> => {
        if (!selected?.id || selected.status !== 'draft') return false;
        setBusy(true);
        setError('');
        try {
            const mod = await getApp();
            if (!mod?.UpdateSurvey) throw new Error(t.hubOffline);
            let payload;
            try {
                payload = buildUpdateSurveyPayload({
                    title: draftTitle,
                    description: draftDesc,
                    questions: editQuestions,
                    settings: settingsForPayload(),
                });
            } catch (e: any) {
                throw new Error(mapCreateError(String(e?.message || e)));
            }
            const raw = await mod.UpdateSurvey(selected.id, JSON.stringify(payload));
            const detail = parseWailsJSON<SurveyDetail>(raw);
            setSelected(detail);
            loadEditorFromDetail(detail);
            await loadList();
            setError('');
            setCopyHint(isZh ? '已保存' : 'Saved');
            setTimeout(() => setCopyHint(''), 2000);
            return true;
        } catch (e: any) {
            setError(String(e?.message || e));
            return false;
        } finally {
            setBusy(false);
        }
    };

    const openSurvey = async (id: string) => {
        setBusy(true);
        setError('');
        try {
            const mod = await getApp();
            const raw = await mod.GetSurvey(id);
            const detail = parseWailsJSON<SurveyDetail>(raw);
            if (!detail?.id) throw new Error(isZh ? '加载失败' : 'Load failed');
            setSelected(detail);
            loadEditorFromDetail(detail);
            setView('survey-edit');
            try {
                const g = await mod.ListLansengerGroups?.();
                const parsed = parseWailsJSON<any>(g);
                const list = parsed?.groups || parsed?.Groups || (Array.isArray(parsed) ? parsed : []);
                setGroups((list || []).map((x: any) => ({
                    group_id: x.group_id || x.GroupID || x.groupId,
                    name: x.name || x.Name || x.group_id,
                })).filter((x: any) => x.group_id));
            } catch {
                setGroups([]);
            }
        } catch (e: any) {
            setError(String(e?.message || e));
        } finally {
            setBusy(false);
        }
    };

    const startNewDraft = () => {
        setSelected(null);
        setDraftTitle('');
        setDraftDesc('');
        setDraftAnonymous(false);
        setDraftAllowUpdate(false);
        setDraftAllowP2P(false);
        setDraftDeadlineLocal('');
        setDraftTargetCount('');
        setEditQuestions([
            createEmptyQuestion('single_choice', [], {
                title: isZh ? '满意吗' : 'Satisfied?',
                optA: isZh ? '是' : 'Yes',
                optB: isZh ? '否' : 'No',
            }),
        ]);
        setView('survey-edit');
        setError('');
    };

    const updateQuestion = (idx: number, patch: Partial<EditorQuestion>) => {
        setEditQuestions((prev) => prev.map((q, i) => (i === idx ? { ...q, ...patch } : q)));
    };

    const updateOption = (qIdx: number, oIdx: number, label: string) => {
        setEditQuestions((prev) =>
            prev.map((q, i) => {
                if (i !== qIdx) return q;
                const options = [...(q.options || [])];
                options[oIdx] = { ...options[oIdx], label };
                return { ...q, options };
            }),
        );
    };

    const reloadSelected = async (id: string) => {
        const mod = await getApp();
        const raw = await mod.GetSurvey(id);
        const detail = parseWailsJSON<SurveyDetail>(raw);
        setSelected(detail);
        if (detail) loadEditorFromDetail(detail);
        return detail;
    };

    const bindGroup = async (groupId: string, name: string) => {
        if (!selected?.id) return;
        setBusy(true);
        setError('');
        try {
            const mod = await getApp();
            await mod.BindSurveyGroups(selected.id, JSON.stringify({
                bindings: [{ platform: 'lansenger', group_id: groupId, group_name: name }],
            }));
            await reloadSelected(selected.id);
        } catch (e: any) {
            setError(String(e?.message || e));
        } finally {
            setBusy(false);
        }
    };

    const unbindGroup = async (platform: string, groupId: string) => {
        if (!selected?.id) return;
        setBusy(true);
        setError('');
        try {
            const mod = await getApp();
            await mod.UnbindSurveyGroup(selected.id, platform || 'lansenger', groupId);
            await reloadSelected(selected.id);
        } catch (e: any) {
            setError(String(e?.message || e));
        } finally {
            setBusy(false);
        }
    };

    const publish = async () => {
        if (!selected?.id) return;
        if (!canPublishNow) {
            setError(isZh ? '发布前请完成检查清单' : 'Complete publish checklist first');
            return;
        }
        // Save draft first so editor questions/settings hit Hub before publish.
        // saveDraft swallows errors into UI state — must check boolean result.
        if (selected.status === 'draft') {
            const saved = await saveDraft();
            if (!saved) return;
        }
        setBusy(true);
        setError('');
        try {
            const mod = await getApp();
            const raw = await mod.PublishSurvey(selected.id, JSON.stringify({ announce: true }));
            const published = parseWailsJSON<SurveyDetail>(raw);
            setSelected(published);
            await loadList();
            if (published?.announce_failures && published.announce_failures.length > 0) {
                setError(
                    (isZh ? '已发布，但部分群公告失败：' : 'Published, but some announces failed: ') +
                        published.announce_failures.join('; '),
                );
            } else {
                setCopyHint(isZh ? '已发布' : 'Published');
                setTimeout(() => setCopyHint(''), 2000);
            }
        } catch (e: any) {
            setError(String(e?.message || e));
        } finally {
            setBusy(false);
        }
    };

    const closeSurvey = async () => {
        if (!selected?.id) return;
        setBusy(true);
        setError('');
        try {
            const mod = await getApp();
            const raw = await mod.CloseSurvey(selected.id);
            setSelected(parseWailsJSON<SurveyDetail>(raw));
            await loadList();
        } catch (e: any) {
            setError(String(e?.message || e));
        } finally {
            setBusy(false);
        }
    };

    const reopenSurvey = async () => {
        if (!selected?.id) return;
        setBusy(true);
        setError('');
        try {
            const mod = await getApp();
            const raw = await mod.ReopenSurvey(selected.id);
            setSelected(parseWailsJSON<SurveyDetail>(raw));
            await loadList();
        } catch (e: any) {
            setError(String(e?.message || e));
        } finally {
            setBusy(false);
        }
    };

    const archiveSurvey = async () => {
        if (!selected?.id) return;
        setBusy(true);
        setError('');
        try {
            const mod = await getApp();
            const raw = await mod.ArchiveSurvey(selected.id);
            setSelected(parseWailsJSON<SurveyDetail>(raw));
            await loadList();
        } catch (e: any) {
            setError(String(e?.message || e));
        } finally {
            setBusy(false);
        }
    };

    const duplicateSurvey = async () => {
        if (!selected?.id) return;
        setBusy(true);
        setError('');
        try {
            const mod = await getApp();
            const raw = await mod.DuplicateSurvey(selected.id);
            const detail = parseWailsJSON<SurveyDetail>(raw);
            setSelected(detail);
            if (detail) loadEditorFromDetail(detail);
            await loadList();
        } catch (e: any) {
            setError(String(e?.message || e));
        } finally {
            setBusy(false);
        }
    };

    const deleteSurvey = async () => {
        if (!selected?.id) return;
        if (!window.confirm(isZh ? '确认删除该问卷？不可恢复。' : 'Delete this survey? This cannot be undone.')) return;
        setBusy(true);
        setError('');
        try {
            const mod = await getApp();
            await mod.DeleteSurvey(selected.id);
            setSelected(null);
            setView('survey-list');
            await loadList();
        } catch (e: any) {
            setError(String(e?.message || e));
        } finally {
            setBusy(false);
        }
    };

    const announce = async () => {
        if (!selected?.id) return;
        setBusy(true);
        setError('');
        try {
            const mod = await getApp();
            const raw = await mod.AnnounceSurveyToBoundGroups(selected.id);
            const res = parseWailsJSON<{ failures?: string[] }>(raw) || {};
            if (res.failures && res.failures.length > 0) {
                setError((isZh ? '部分群公告失败：' : 'Some announces failed: ') + res.failures.join('; '));
            } else {
                setCopyHint(isZh ? '已向绑定群发送公告' : 'Announced to bound groups');
                setTimeout(() => setCopyHint(''), 2500);
            }
        } catch (e: any) {
            setError(String(e?.message || e));
        } finally {
            setBusy(false);
        }
    };

    const openResults = async (opts?: { soft?: boolean }) => {
        const id = selectedIdRef.current || selected?.id;
        if (!id) return;
        if (!opts?.soft) {
            setBusy(true);
            setError('');
        }
        try {
            const mod = await getApp();
            const statsRaw = await mod.GetSurveyStats(id);
            setStats(parseWailsJSON(statsRaw));
            if (mod.ListSurveyResponses) {
                const respRaw = await mod.ListSurveyResponses(id);
                const wrap = parseWailsJSON<{ responses?: any[] }>(respRaw) || {};
                setResponses(Array.isArray(wrap.responses) ? wrap.responses : []);
            } else {
                setResponses([]);
            }
            if (!opts?.soft) {
                setOptionFilter({ qid: '', oid: '' });
                setGroupFilter('');
                setResponseQuery('');
                setTextAnswerQuery('');
            }
            setView('survey-results');
        } catch (e: any) {
            if (!opts?.soft) setError(String(e?.message || e));
        } finally {
            if (!opts?.soft) setBusy(false);
        }
    };

    // Live refresh when IM gateway emits survey-updated after a submission.
    // Debounce burst submits (rate-limit window / multi-user) into one refresh.
    useEffect(() => {
        let off: (() => void) | undefined;
        let timer: ReturnType<typeof setTimeout> | undefined;
        let pendingSid: string | undefined;
        const flush = () => {
            timer = undefined;
            const sid = pendingSid;
            pendingSid = undefined;
            void loadList();
            if (viewRef.current === 'survey-results' && sid && sid === selectedIdRef.current) {
                void openResults({ soft: true });
                setCopyHint(isZh ? '有新答卷，已自动刷新' : 'New response — refreshed');
                setTimeout(() => setCopyHint(''), 2500);
            }
        };
        try {
            off = EventsOn(EVENT_SURVEY_UPDATED, (payload?: { survey_id?: string }) => {
                if (payload?.survey_id) pendingSid = payload.survey_id;
                if (timer) clearTimeout(timer);
                timer = setTimeout(flush, 400);
            });
        } catch {
            // Browser dev without Wails runtime
        }
        return () => {
            if (timer) clearTimeout(timer);
            if (typeof off === 'function') off();
        };
        // eslint-disable-next-line react-hooks/exhaustive-deps -- openResults uses refs for soft refresh
    }, [loadList, isZh]);

    const saveDraftRef = useRef(saveDraft);
    const createSurveyRef = useRef(createSurvey);
    saveDraftRef.current = saveDraft;
    createSurveyRef.current = createSurvey;

    // Keyboard: Ctrl/Cmd+S save or create draft; Esc navigate back.
    useEffect(() => {
        const onKey = (e: KeyboardEvent) => {
            if (busyRef.current) return;
            const key = e.key;
            if ((e.ctrlKey || e.metaKey) && (key === 's' || key === 'S')) {
                if (viewRef.current !== 'survey-edit') return;
                e.preventDefault();
                if (selectedIdRef.current && selectedStatusRef.current === 'draft') {
                    void saveDraftRef.current();
                } else if (!selectedIdRef.current) {
                    void createSurveyRef.current();
                }
                return;
            }
            if (key === 'Escape') {
                if (viewRef.current === 'survey-results') {
                    e.preventDefault();
                    setView('survey-edit');
                    return;
                }
                if (viewRef.current === 'survey-edit') {
                    e.preventDefault();
                    setSelected(null);
                    setView('survey-list');
                    return;
                }
                if (viewRef.current === 'survey-list') {
                    e.preventDefault();
                    setView('home');
                }
            }
        };
        window.addEventListener('keydown', onKey);
        return () => window.removeEventListener('keydown', onKey);
    }, []);

    const exportXlsx = async (filteredOnly: boolean) => {
        if (!selected?.id) return;
        if (shouldWarnExportPII(selected.settings?.anonymous)) {
            if (!window.confirm(t.exportPiiWarn)) return;
        }
        setBusy(true);
        setExportHint(t.exporting);
        setError('');
        try {
            const mod = await getApp();
            let path: string;
            if (filteredOnly && mod.ExportSurveyXLSXFiltered) {
                const body = JSON.stringify({ responses: filteredResponses });
                path = await mod.ExportSurveyXLSXFiltered(selected.id, body);
            } else {
                path = await mod.ExportSurveyXLSX(selected.id);
            }
            const piiTail = shouldWarnExportPII(selected.settings?.anonymous)
                ? (isZh ? '（含 PII，请妥善保管）' : ' (contains PII — keep secure)')
                : '';
            const msg = path
                ? (isZh ? `已导出：${path}${piiTail}` : `Exported: ${path}${piiTail}`)
                : '';
            // Success path uses exportHint only — do not paint success as utilities-error.
            setExportHint(msg);
            setError('');
        } catch (e: any) {
            setExportHint('');
            setError(String(e?.message || e));
        } finally {
            setBusy(false);
        }
    };

    if (view === 'home') {
        return (
            <div className="utilities-page" data-testid="utilities-page">
                <div className="utilities-page__header">
                    <h1 className="utilities-page__title">{t.title}</h1>
                    <p className="utilities-page__subtitle">{t.subtitle}</p>
                </div>
                <div className="utilities-page__grid">
                    <button
                        type="button"
                        className="utilities-tool-card"
                        data-testid="utilities-survey-card"
                        onClick={() => setView('survey-list')}
                    >
                        <div className="utilities-tool-card__icon" aria-hidden>📋</div>
                        <div className="utilities-tool-card__body">
                            <div className="utilities-tool-card__title">{t.surveyCard}</div>
                            <div className="utilities-tool-card__desc">{t.surveyDesc}</div>
                        </div>
                        <span className="utilities-tool-card__cta">{t.open}</span>
                    </button>
                    <button
                        type="button"
                        className={`utilities-tool-card${meetingStarting ? ' is-starting' : ''}`}
                        data-testid="utilities-meeting-card"
                        disabled={meetingStarting || !onStartMeetingRecord}
                        aria-busy={meetingStarting || undefined}
                        aria-label={t.meetingCard}
                        title={t.meetingDesc}
                        onClick={() => { void handleMeetingRecord(); }}
                    >
                        <div className="utilities-tool-card__icon" aria-hidden>🎙️</div>
                        <div className="utilities-tool-card__body">
                            <div className="utilities-tool-card__title">{t.meetingCard}</div>
                            <div className="utilities-tool-card__desc">{t.meetingDesc}</div>
                        </div>
                        <span className="utilities-tool-card__cta">{meetingStarting ? t.starting : t.start}</span>
                    </button>
                </div>
            </div>
        );
    }

    if (view === 'survey-list') {
        return (
            <div className="utilities-page" data-testid="survey-list-page">
                <div className="utilities-page__header utilities-page__header--row">
                    <div>
                        <button type="button" className="utilities-link" onClick={() => setView('home')}>{t.back}</button>
                        <h1 className="utilities-page__title">{t.surveyCard}</h1>
                    </div>
                    <div className="utilities-actions">
                        <select
                            className="utilities-select"
                            value={statusFilter}
                            onChange={(e) => setStatusFilter(e.target.value as typeof statusFilter)}
                            aria-label={isZh ? '状态筛选' : 'Status filter'}
                            data-testid="survey-status-filter"
                        >
                            <option value="all">{isZh ? '全部状态' : 'All'}</option>
                            <option value="draft">{isZh ? '草稿' : 'Draft'}</option>
                            <option value="published">{isZh ? '收集中' : 'Published'}</option>
                            <option value="closed">{isZh ? '已关闭' : 'Closed'}</option>
                            <option value="archived">{isZh ? '已归档' : 'Archived'}</option>
                        </select>
                        <select
                            className="utilities-select"
                            value={listSort}
                            onChange={(e) => setListSort(e.target.value as SurveyListSort)}
                            aria-label={t.sort}
                            data-testid="survey-list-sort"
                        >
                            <option value="updated_desc">{isZh ? '最近更新' : 'Updated ↓'}</option>
                            <option value="updated_asc">{isZh ? '最早更新' : 'Updated ↑'}</option>
                            <option value="title_asc">{isZh ? '标题 A→Z' : 'Title A→Z'}</option>
                            <option value="title_desc">{isZh ? '标题 Z→A' : 'Title Z→A'}</option>
                            <option value="code_asc">{isZh ? '短码' : 'Code'}</option>
                        </select>
                        <button type="button" className="utilities-btn" onClick={() => void loadList()} disabled={busy}>{t.refresh}</button>
                        <button
                            type="button"
                            className="utilities-btn"
                            data-testid="survey-help-toggle"
                            onClick={() => setShowHelp((v) => !v)}
                        >{showHelp ? t.hideHelp : t.help}</button>
                        <button type="button" className="utilities-btn" onClick={toggleSelectAllVisible} disabled={busy || filteredSurveys.length === 0}>{t.selectAll}</button>
                        <button
                            type="button"
                            className="utilities-btn"
                            data-testid="survey-batch-archive"
                            disabled={busy || archivableSelected.length === 0}
                            onClick={() => void batchArchive()}
                        >{t.batchArchive}{archivableSelected.length ? ` (${archivableSelected.length})` : ''}</button>
                        <button
                            type="button"
                            className="utilities-btn utilities-btn--danger"
                            data-testid="survey-batch-delete"
                            disabled={busy || deletableSelected.length === 0}
                            onClick={() => void batchDelete()}
                        >{t.batchDelete}{deletableSelected.length ? ` (${deletableSelected.length})` : ''}</button>
                        <button type="button" className="utilities-btn utilities-btn--primary" onClick={startNewDraft} disabled={busy}>{t.create}</button>
                    </div>
                </div>
                <div className="utilities-search-row">
                    <input
                        className="utilities-search"
                        data-testid="survey-list-search"
                        value={listQuery}
                        onChange={(e) => setListQuery(e.target.value)}
                        placeholder={t.search}
                        spellCheck={false}
                    />
                    {listQuery && (
                        <button type="button" className="utilities-btn" onClick={() => setListQuery('')}>
                            {isZh ? '清除' : 'Clear'}
                        </button>
                    )}
                </div>
                {showHelp && (
                    <div className="utilities-help" data-testid="survey-help-panel">
                        <h3>{t.help}</h3>
                        <ol>
                            {operatorHelp.map((line) => (
                                <li key={line}>{line}</li>
                            ))}
                        </ol>
                    </div>
                )}
                {hubOk === false && <div className="utilities-banner" role="status">{t.hubOffline}</div>}
                {error && <div className="utilities-error">{error}</div>}
                {surveys.length === 0 && hubOk !== false && (
                    <div className="utilities-empty utilities-empty--cta" data-testid="survey-list-empty">
                        <p className="utilities-empty__title">{t.empty}</p>
                        <p className="utilities-meta">{t.emptyHint}</p>
                        <button
                            type="button"
                            className="utilities-btn utilities-btn--primary"
                            disabled={busy || !hubOk}
                            onClick={startNewDraft}
                        >{t.create}</button>
                    </div>
                )}
                {surveys.length > 0 && filteredSurveys.length === 0 && (
                    <div className="utilities-empty">{t.noMatch}</div>
                )}
                {copyHint && <p className="utilities-meta" data-testid="survey-copy-hint">{copyHint}</p>}
                <ul className="utilities-list">
                    {filteredSurveys.map((s) => (
                        <li key={s.id} className="utilities-list-row">
                            <label className="utilities-list-check" onClick={(e) => e.stopPropagation()}>
                                <input
                                    type="checkbox"
                                    checked={selectedIds.includes(s.id)}
                                    onChange={() => toggleSelect(s.id)}
                                    aria-label={`select ${s.title}`}
                                />
                            </label>
                            <button type="button" className="utilities-list-item" onClick={() => void openSurvey(s.id)}>
                                <strong>
                                    {s.title}
                                    <span className={surveyStatusBadgeClass(s.status)} data-testid="survey-status-badge">
                                        {surveyStatusLabel(s.status, isZh)}
                                    </span>
                                    {deadlineListBadge(s.settings?.deadline, { expired: t.expired, open: t.notExpired }) && (
                                        <span className={`utilities-badge ${isDeadlineExpired(s.settings?.deadline) ? 'utilities-badge--danger' : 'utilities-badge--ok'}`}>
                                            {deadlineListBadge(s.settings?.deadline, { expired: t.expired, open: t.notExpired })}
                                        </span>
                                    )}
                                </strong>
                                <span>
                                    {s.short_code}
                                    {typeof s.question_count === 'number' ? ` · ${s.question_count}${t.questions}` : ''}
                                    {typeof s.binding_count === 'number' ? ` · ${s.binding_count}${t.groups}` : ''}
                                    {typeof s.response_count === 'number' ? ` · ${s.response_count}${t.responsesShort}` : ''}
                                    {(() => {
                                        const badge = formatCompletionBadge(Number(s.response_count) || 0, Number(s.settings?.target_count) || 0);
                                        return badge ? (
                                            <span className="utilities-badge utilities-badge--ok" data-testid="survey-list-completion">
                                                {badge}
                                            </span>
                                        ) : null;
                                    })()}
                                    {s.updated_at ? ` · ${new Date(s.updated_at).toLocaleString()}` : ''}
                                </span>
                            </button>
                            <div className="utilities-list-row__actions">
                                <button
                                    type="button"
                                    className="utilities-btn"
                                    title={t.copyCode}
                                    onClick={(e) => {
                                        e.stopPropagation();
                                        void copyText(formatShortCodeForCopy(s.short_code), `${t.copied}: ${formatShortCodeForCopy(s.short_code)}`);
                                    }}
                                >{t.copyCode}</button>
                                <button
                                    type="button"
                                    className="utilities-btn"
                                    title={t.copyCmd}
                                    onClick={(e) => {
                                        e.stopPropagation();
                                        void copyText(formatSurveyIMCommand(s.short_code), `${t.copied}: ${formatSurveyIMCommand(s.short_code)}`);
                                    }}
                                >{t.copyCmd}</button>
                            </div>
                        </li>
                    ))}
                </ul>
            </div>
        );
    }

    if (view === 'survey-results') {
        const count = stats?.response_count ?? responses.length;
        const target = stats?.target_count ?? selected?.settings?.target_count ?? 0;
        const pct = completionPercent(Number(count) || 0, Number(target) || 0);
        const deadline = selected?.settings?.deadline;
        const byQ: any[] = Array.isArray(stats?.by_question) ? stats.by_question : [];
        const bindings = selected?.bindings || [];
        const textQuestions = (selected?.questions || []).filter((q) => q.type === 'text');
        const groupRows = buildGroupBreakdown(responses, bindings, t.p2pUnknown);
        return (
            <div className="utilities-page utilities-page--print" data-testid="survey-results-page">
                <button type="button" className="utilities-link utilities-no-print" onClick={() => setView('survey-edit')}>{t.back}</button>
                <h1 className="utilities-page__title">{t.results}{selected?.title ? ` · ${selected.title}` : ''}</h1>
                {error && <div className="utilities-error">{error}</div>}
                {copyHint && <p className="utilities-meta" data-testid="survey-live-hint">{copyHint}</p>}
                <div className="utilities-results-overview" data-testid="survey-results-overview">
                    <p className="utilities-meta">
                        <strong>{t.status}</strong>: {selected?.status || '—'}
                        {selected?.settings?.anonymous ? (isZh ? ' · 匿名' : ' · anonymous') : ''}
                        {selected?.settings?.allow_p2p ? (isZh ? ' · 私聊可填' : ' · P2P') : ''}
                        {deadline ? ` · ${t.deadline}: ${new Date(deadline).toLocaleString()}` : ''}
                        {isDeadlineExpired(deadline) ? (
                            <span className="utilities-badge utilities-badge--danger">{t.expired}</span>
                        ) : null}
                    </p>
                    {bindings.length > 0 && (
                        <p className="utilities-meta">
                            <strong>{t.bindings}</strong>: {bindings.map((b) => b.group_name || b.group_id).join(' · ')}
                        </p>
                    )}
                    <p className="utilities-meta utilities-export-pii-note" data-testid="survey-export-pii-note">
                        {shouldWarnExportPII(selected?.settings?.anonymous) ? t.exportPiiNote : t.exportAnonNote}
                    </p>
                </div>
                <div className="utilities-stat-tiles">
                    <div className="utilities-stat-tile">
                        <span>{t.count}</span>
                        <strong>{count}</strong>
                    </div>
                    {target > 0 && (
                        <div className="utilities-stat-tile">
                            <span>{t.target}</span>
                            <strong>{target}</strong>
                        </div>
                    )}
                    {pct != null && (
                        <div className="utilities-stat-tile" data-testid="survey-completion-rate">
                            <span>{t.completion}</span>
                            <strong>{pct}%</strong>
                            <div className="utilities-option-bars__track" style={{ marginTop: 6 }}>
                                <div className="utilities-option-bars__fill" style={{ width: `${pct}%` }} />
                            </div>
                        </div>
                    )}
                </div>
                {groupRows.length > 0 && (
                    <div className="utilities-result-block" data-testid="survey-group-breakdown">
                        <h3>{t.byGroup}</h3>
                        <p className="utilities-meta">{t.filterByGroup}</p>
                        <ul className="utilities-option-bars">
                            {groupRows.map((row) => {
                                const active = groupFilter === row.groupId;
                                return (
                                    <li key={row.groupId}>
                                        <button
                                            type="button"
                                            className={`utilities-group-filter-btn${active ? ' is-active' : ''}`}
                                            data-testid={`survey-group-filter-${row.groupId}`}
                                            onClick={() =>
                                                setGroupFilter((prev) => (prev === row.groupId ? '' : row.groupId))
                                            }
                                        >
                                            <div className="utilities-option-bars__label">
                                                <span>{row.groupName}{active ? ' ✓' : ''}</span>
                                                <span>
                                                    {row.count}
                                                    {responses.length > 0 ? ` (${row.percent.toFixed(0)}%)` : ''}
                                                </span>
                                            </div>
                                            <div className="utilities-option-bars__track">
                                                <div
                                                    className="utilities-option-bars__fill"
                                                    style={{ width: `${Math.min(100, row.percent || 0)}%` }}
                                                />
                                            </div>
                                        </button>
                                    </li>
                                );
                            })}
                        </ul>
                        {groupFilter && (
                            <button
                                type="button"
                                className="utilities-btn"
                                data-testid="survey-group-filter-clear"
                                onClick={() => setGroupFilter('')}
                            >
                                {t.allGroups}
                            </button>
                        )}
                    </div>
                )}
                {byQ.map((q) => {
                    const qid = q.question_id || q.QuestionID;
                    const isText = q.type === 'text';
                    const textRows = isText
                        ? filterTextAnswers(extractTextAnswers(qid, groupScopedResponses), textAnswerQuery)
                        : [];
                    return (
                        <div key={qid} className="utilities-result-block">
                            <h3>{q.title || q.Title || qid}</h3>
                            {(q.options || []).length > 0 && (
                                <ul className="utilities-option-bars">
                                    {(q.options || []).map((o: any) => (
                                        <li key={o.option_id || o.OptionID}>
                                            <div className="utilities-option-bars__label">
                                                <span>{o.label || o.Label}</span>
                                                <span>{o.count ?? 0}{typeof o.percent === 'number' ? ` (${o.percent.toFixed(0)}%)` : ''}</span>
                                            </div>
                                            <div className="utilities-option-bars__track">
                                                <div className="utilities-option-bars__fill" style={{ width: `${Math.min(100, o.percent || 0)}%` }} />
                                            </div>
                                        </li>
                                    ))}
                                </ul>
                            )}
                            {q.type === 'rating' && (
                                <p className="utilities-meta">{isZh ? '平均分' : 'Avg'}: {(q.rating_avg ?? 0).toFixed?.(2) ?? q.rating_avg} (n={q.rating_n ?? 0})</p>
                            )}
                            {isText && (
                                <>
                                    <p className="utilities-meta">{t.textAnswers}: {q.text_count ?? textRows.length}</p>
                                    {(q.text_count > 0 || textRows.length > 0 || textAnswerQuery) && (
                                        <div className="utilities-text-answers" data-testid={`survey-text-answers-${qid}`}>
                                            {textQuestions.length > 0 && textQuestions[0]?.id === qid && (
                                                <div className="utilities-search-row">
                                                    <input
                                                        className="utilities-search"
                                                        data-testid="survey-text-answer-search"
                                                        value={textAnswerQuery}
                                                        onChange={(e) => setTextAnswerQuery(e.target.value)}
                                                        placeholder={t.searchTextAnswers}
                                                        spellCheck={false}
                                                    />
                                                </div>
                                            )}
                                            {textRows.length === 0 ? (
                                                <p className="utilities-empty">{isZh ? '无匹配文本' : 'No matching text'}</p>
                                            ) : (
                                                <ul className="utilities-text-answer-list">
                                                    {textRows.map((row) => (
                                                        <li key={row.responseId}>
                                                            <span className="utilities-text-answer-list__who">{row.respondent}</span>
                                                            <span className="utilities-text-answer-list__body">{row.text}</span>
                                                            {row.submittedAt && (
                                                                <span className="utilities-meta">{new Date(row.submittedAt).toLocaleString()}</span>
                                                            )}
                                                        </li>
                                                    ))}
                                                </ul>
                                            )}
                                        </div>
                                    )}
                                </>
                            )}
                        </div>
                    );
                })}
                <h3>{t.responses}</h3>
                <div className="utilities-search-row utilities-no-print">
                    <input
                        className="utilities-search"
                        data-testid="survey-response-search"
                        value={responseQuery}
                        onChange={(e) => setResponseQuery(e.target.value)}
                        placeholder={t.searchResponses}
                        spellCheck={false}
                    />
                    {choiceFilterOptions.length > 0 && (
                        <select
                            className="utilities-select"
                            data-testid="survey-option-filter"
                            value={optionFilter.qid && optionFilter.oid ? `${optionFilter.qid}::${optionFilter.oid}` : ''}
                            onChange={(e) => {
                                const v = e.target.value;
                                if (!v) {
                                    setOptionFilter({ qid: '', oid: '' });
                                    return;
                                }
                                const [qid, oid] = v.split('::');
                                setOptionFilter({ qid, oid });
                            }}
                            aria-label={t.filterOption}
                        >
                            <option value="">{t.allOptions}</option>
                            {choiceFilterOptions.map((o) => (
                                <option key={`${o.qid}::${o.oid}`} value={`${o.qid}::${o.oid}`}>
                                    {o.qtitle}: {o.olabel}
                                </option>
                            ))}
                        </select>
                    )}
                    {(optionFilter.qid || optionFilter.oid || responseQuery || groupFilter) && (
                        <span className="utilities-meta">
                            {t.showing} {filteredResponses.length}/{responses.length}
                        </span>
                    )}
                </div>
                {responses.length === 0 ? (
                    <p className="utilities-empty">{isZh ? '暂无答卷' : 'No responses yet'}</p>
                ) : filteredResponses.length === 0 ? (
                    <p className="utilities-empty">{isZh ? '当前筛选下无答卷' : 'No responses for this filter'}</p>
                ) : (
                    <div className="utilities-response-cards">
                        {filteredResponses.map((r, i) => {
                            const lines = expandResponseAnswers(qMetaForFilter, r.answers);
                            const displayName = selected?.settings?.anonymous
                                ? (isZh ? '匿名' : 'Anonymous')
                                : (r.respondent_name || r.respondent_key || '—');
                            return (
                                <div key={r.id || i} className="utilities-response-card">
                                    <div className="utilities-response-card__meta">
                                        <strong>{displayName}</strong>
                                        <span>{r.submitted_at ? new Date(r.submitted_at).toLocaleString() : '—'}</span>
                                        {r.group_id ? <span>{r.group_id}</span> : null}
                                    </div>
                                    <ul className="utilities-response-card__answers">
                                        {lines.map((line) => (
                                            <li key={line.questionId}>
                                                <span className="utilities-response-card__q">{line.title}</span>
                                                <span className="utilities-response-card__a">{line.display}</span>
                                            </li>
                                        ))}
                                    </ul>
                                </div>
                            );
                        })}
                    </div>
                )}
                {exportHint && <p className="utilities-meta utilities-no-print" data-testid="survey-export-hint">{exportHint}</p>}
                <div className="utilities-actions utilities-no-print">
                    <button type="button" className="utilities-btn" disabled={busy} onClick={() => void openResults()}>{t.refresh}</button>
                    <button
                        type="button"
                        className="utilities-btn"
                        data-testid="survey-copy-summary"
                        disabled={busy}
                        onClick={() => {
                            const summary = buildSurveyShareSummary(
                                {
                                    title: selected?.title,
                                    shortCode: selected?.short_code,
                                    status: selected?.status,
                                    responseCount: Number(count) || 0,
                                    targetCount: Number(target) || 0,
                                    deadline: deadline,
                                    anonymous: selected?.settings?.anonymous,
                                    groups: groupRows.map((g) => ({
                                        groupName: g.groupName,
                                        count: g.count,
                                        percent: g.percent,
                                    })),
                                    questions: byQ.map((q: any) => ({
                                        title: q.title || q.Title || '',
                                        type: q.type,
                                        options: (q.options || []).map((o: any) => ({
                                            label: o.label || o.Label || '',
                                            count: o.count ?? 0,
                                            percent: o.percent,
                                        })),
                                        ratingAvg: q.rating_avg,
                                        ratingN: q.rating_n,
                                        textCount: q.text_count,
                                    })),
                                },
                                isZh,
                            );
                            void copyText(summary, t.summaryCopied);
                        }}
                    >{t.copySummary}</button>
                    <button
                        type="button"
                        className="utilities-btn"
                        data-testid="survey-print"
                        onClick={() => window.print()}
                    >{t.print}</button>
                    <button type="button" className="utilities-btn utilities-btn--primary" disabled={busy} onClick={() => void exportXlsx(false)}>
                        {busy && exportHint === t.exporting ? t.exporting : t.export}
                    </button>
                    {(optionFilter.qid && optionFilter.oid) || responseQuery || groupFilter ? (
                        <button
                            type="button"
                            className="utilities-btn"
                            data-testid="survey-export-filtered"
                            disabled={busy || filteredResponses.length === 0}
                            onClick={() => void exportXlsx(true)}
                        >
                            {t.exportFiltered} ({filteredResponses.length})
                        </button>
                    ) : null}
                </div>
            </div>
        );
    }

    return (
        <div className="utilities-page" data-testid="survey-edit-page">
            <header className="utilities-survey-editor__header">
                <div>
                    <button type="button" className="utilities-link" onClick={() => { setSelected(null); setView('survey-list'); }}>{t.back}</button>
                    <h1 className="utilities-page__title">{selected?.title || t.create}</h1>
                    {(!selected || selected.status === 'draft') && (
                        <p className="utilities-meta utilities-shortcut-hint" data-testid="survey-shortcut-hint">{t.shortcutSave}</p>
                    )}
                </div>
                <button
                    type="button"
                    className="utilities-btn utilities-btn--ghost"
                    data-testid="survey-help-toggle-edit"
                    onClick={() => setShowHelp((v) => !v)}
                >{showHelp ? t.hideHelp : t.help}</button>
            </header>
            <div className="utilities-survey-editor__feedback">
                {copyHint && <p className="utilities-meta">{copyHint}</p>}
                {error && <div className="utilities-error">{error}</div>}
            </div>
            {showHelp && (
                <div className="utilities-help" data-testid="survey-help-panel">
                    <h3>{t.help}</h3>
                    <ol>
                        {operatorHelp.map((line) => (
                            <li key={line}>{line}</li>
                        ))}
                    </ol>
                </div>
            )}
            {selected && (
                <p className="utilities-meta">
                    {selected.short_code} · <span className={surveyStatusBadgeClass(selected.status)}>{surveyStatusLabel(selected.status, isZh)}</span>
                    {selected.settings?.anonymous ? (isZh ? ' · 匿名' : ' · anonymous') : ''}
                    {selected.settings?.deadline
                        ? ` · ${t.deadline} ${new Date(selected.settings.deadline).toLocaleString()}`
                        : ''}
                    {selected.settings?.target_count
                        ? ` · ${t.target} ${selected.settings.target_count}`
                        : ''}
                    {' · '}
                    <button
                        type="button"
                        className="utilities-link"
                        onClick={() => void copyText(formatShortCodeForCopy(selected.short_code), `${t.copied}: ${formatShortCodeForCopy(selected.short_code)}`)}
                    >{t.copyCode}</button>
                    {' · '}
                    <button
                        type="button"
                        className="utilities-link"
                        onClick={() => void copyText(formatSurveyIMCommand(selected.short_code), `${t.copied}: ${formatSurveyIMCommand(selected.short_code)}`)}
                    >{t.copyCmd}</button>
                </p>
            )}
            {selected && (
                <div className="utilities-actions utilities-survey-editor__actions">
                    {selected.status === 'draft' && (
                        <button type="button" className="utilities-btn utilities-btn--primary" disabled={busy} onClick={() => void saveDraft()}>{t.save}</button>
                    )}
                    <button
                        type="button"
                        className="utilities-btn utilities-btn--primary"
                        data-testid="survey-publish"
                        disabled={busy || selected.status !== 'draft' || !canPublishNow}
                        onClick={() => void publish()}
                    >{t.publish}</button>
                    <button type="button" className="utilities-btn" disabled={busy || selected.status !== 'published'} onClick={() => void closeSurvey()}>{t.close}</button>
                    <button type="button" className="utilities-btn" disabled={busy || selected.status !== 'closed'} onClick={() => void reopenSurvey()}>{t.reopen}</button>
                    <button type="button" className="utilities-btn" disabled={busy || (selected.status !== 'draft' && selected.status !== 'closed')} onClick={() => void archiveSurvey()}>{t.archive}</button>
                    <button type="button" className="utilities-btn" disabled={busy} onClick={() => void duplicateSurvey()}>{t.duplicate}</button>
                    <button type="button" className="utilities-btn utilities-btn--danger" disabled={busy || (selected.status !== 'draft' && selected.status !== 'archived')} onClick={() => void deleteSurvey()}>{t.delete}</button>
                    <button type="button" className="utilities-btn" disabled={busy || selected.status !== 'published'} onClick={() => void announce()}>{t.announce}</button>
                    <button type="button" className="utilities-btn" disabled={busy} onClick={() => void openResults()}>{t.results}</button>
                    <button type="button" className="utilities-btn" disabled={busy} onClick={() => void exportXlsx(false)}>{t.export}</button>
                </div>
            )}

            {/* Draft / new survey form */}
            {(!selected || selected.status === 'draft') && (
                <div className="utilities-create-panel utilities-create-panel--wide">
                    <section className="utilities-survey-section utilities-survey-section--details">
                        <div className="utilities-survey-section__heading">
                            <h2>{isZh ? '基本信息' : 'Details'}</h2>
                        </div>
                        <div className="utilities-survey-fields">
                            <label>{isZh ? '标题' : 'Title'}<input value={draftTitle} onChange={(e) => setDraftTitle(e.target.value)} /></label>
                            <label>{isZh ? '说明' : 'Description'}<input value={draftDesc} onChange={(e) => setDraftDesc(e.target.value)} /></label>
                        </div>
                    </section>
                    <section className="utilities-survey-section">
                        <div className="utilities-survey-section__heading">
                            <h2>{isZh ? '收集设置' : 'Collection settings'}</h2>
                        </div>
                        <div className="utilities-survey-settings">
                            <label className="utilities-check">
                                <input type="checkbox" checked={draftAnonymous} onChange={(e) => setDraftAnonymous(e.target.checked)} disabled={!!selected && selected.status !== 'draft'} />
                                {isZh ? '匿名收集（发布后不可改）' : 'Anonymous (locked after publish)'}
                            </label>
                            <label className="utilities-check">
                                <input type="checkbox" checked={draftAllowUpdate} onChange={(e) => setDraftAllowUpdate(e.target.checked)} />
                                {isZh ? '允许修改已提交答卷' : 'Allow update after submit'}
                            </label>
                            <label className="utilities-check">
                                <input type="checkbox" data-testid="survey-allow-p2p" checked={draftAllowP2P} onChange={(e) => setDraftAllowP2P(e.target.checked)} />
                                {t.allowP2P}
                            </label>
                        </div>
                        <div className="utilities-survey-fields utilities-survey-fields--schedule">
                            <label>{t.deadline}
                        <input
                            type="datetime-local"
                            value={draftDeadlineLocal}
                            onChange={(e) => setDraftDeadlineLocal(e.target.value)}
                        />
                        <span className="utilities-meta">{isZh ? '留空表示不截止；仅在提交时校验' : 'Empty = no deadline; checked on submit only'}</span>
                            </label>
                    {draftDeadlineLocal && (
                        <button type="button" className="utilities-btn utilities-btn--ghost utilities-deadline-clear" onClick={() => setDraftDeadlineLocal('')}>
                            {isZh ? '清除截止时间' : 'Clear deadline'}
                        </button>
                    )}
                    <label>{t.target}
                        <input
                            type="number"
                            min={0}
                            placeholder={isZh ? '0 表示不展示完成率' : '0 = hide completion %'}
                            value={draftTargetCount}
                            onChange={(e) => setDraftTargetCount(e.target.value)}
                        />
                    </label>
                        </div>
                    </section>

                    <section className="utilities-survey-section utilities-survey-section--questions">
                    <div className="utilities-survey-section__heading">
                        <h2>{isZh ? '题目' : 'Questions'} <span>{editQuestions.length}</span></h2>
                    </div>
                    <div className="utilities-question-list">
                    {editQuestions.map((q, qi) => (
                        <div key={q.id} className="utilities-q-card" data-testid={`survey-q-${qi}`}>
                            <div className="utilities-q-card__row">
                                <strong>Q{qi + 1}</strong>
                                <select
                                    value={q.type}
                                    onChange={(e) => {
                                        const type = e.target.value as EditorQuestion['type'];
                                        const next = createEmptyQuestion(type, editQuestions.filter((_, i) => i !== qi), { title: q.title });
                                        next.id = q.id;
                                        updateQuestion(qi, next);
                                    }}
                                >
                                    <option value="single_choice">{isZh ? '单选' : 'Single choice'}</option>
                                    <option value="multi_choice">{isZh ? '多选' : 'Multi choice'}</option>
                                    <option value="text">{isZh ? '文本' : 'Text'}</option>
                                    <option value="rating">{isZh ? '评分' : 'Rating'}</option>
                                </select>
                                <label className="utilities-check">
                                    <input type="checkbox" checked={q.required} onChange={(e) => updateQuestion(qi, { required: e.target.checked })} />
                                    {isZh ? '必填' : 'Required'}
                                </label>
                                <button
                                    type="button"
                                    className="utilities-btn"
                                    data-testid={`survey-q-up-${qi}`}
                                    disabled={qi === 0}
                                    title={t.moveUp}
                                    onClick={() => setEditQuestions((prev) => moveQuestionAt(prev, qi, -1))}
                                >↑</button>
                                <button
                                    type="button"
                                    className="utilities-btn"
                                    data-testid={`survey-q-down-${qi}`}
                                    disabled={qi >= editQuestions.length - 1}
                                    title={t.moveDown}
                                    onClick={() => setEditQuestions((prev) => moveQuestionAt(prev, qi, 1))}
                                >↓</button>
                                <button
                                    type="button"
                                    className="utilities-btn"
                                    disabled={editQuestions.length <= 1}
                                    onClick={() => setEditQuestions((prev) => prev.filter((_, i) => i !== qi))}
                                >{t.remove}</button>
                            </div>
                            <label>{isZh ? '题干' : 'Title'}
                                <input value={q.title} onChange={(e) => updateQuestion(qi, { title: e.target.value })} />
                            </label>
                            {(q.type === 'single_choice' || q.type === 'multi_choice') && (
                                <div className="utilities-opts">
                                    {(q.options || []).map((o, oi) => (
                                        <div key={o.id} className="utilities-opts__row">
                                            <span>{oi + 1}.</span>
                                            <input value={o.label} onChange={(e) => updateOption(qi, oi, e.target.value)} />
                                            <button
                                                type="button"
                                                className="utilities-btn"
                                                data-testid={`survey-opt-up-${qi}-${oi}`}
                                                disabled={oi === 0}
                                                title={t.moveUp}
                                                onClick={() =>
                                                    updateQuestion(qi, {
                                                        options: moveOptionAt(q.options || [], oi, -1),
                                                    })
                                                }
                                            >↑</button>
                                            <button
                                                type="button"
                                                className="utilities-btn"
                                                data-testid={`survey-opt-down-${qi}-${oi}`}
                                                disabled={oi >= (q.options || []).length - 1}
                                                title={t.moveDown}
                                                onClick={() =>
                                                    updateQuestion(qi, {
                                                        options: moveOptionAt(q.options || [], oi, 1),
                                                    })
                                                }
                                            >↓</button>
                                            <button
                                                type="button"
                                                className="utilities-btn"
                                                disabled={(q.options || []).length <= 2}
                                                onClick={() =>
                                                    updateQuestion(qi, {
                                                        options: (q.options || []).filter((_, j) => j !== oi),
                                                    })
                                                }
                                            >{t.remove}</button>
                                        </div>
                                    ))}
                                    <button
                                        type="button"
                                        className="utilities-btn"
                                        onClick={() => {
                                            const options = [...(q.options || [])];
                                            options.push({ id: newOptionId(options), label: '' });
                                            updateQuestion(qi, { options });
                                        }}
                                    >{t.addOpt}</button>
                                </div>
                            )}
                            {q.type === 'rating' && (
                                <div className="utilities-q-card__row">
                                    <label>min <input type="number" value={q.min ?? 1} onChange={(e) => updateQuestion(qi, { min: Number(e.target.value) || 1 })} style={{ width: 64 }} /></label>
                                    <label>max <input type="number" value={q.max ?? 5} onChange={(e) => updateQuestion(qi, { max: Number(e.target.value) || 5 })} style={{ width: 64 }} /></label>
                                </div>
                            )}
                            {q.type === 'text' && (
                                <label>{isZh ? '最大字数' : 'Max length'}
                                    <input type="number" value={q.max_length ?? 500} onChange={(e) => updateQuestion(qi, { max_length: Number(e.target.value) || 500 })} />
                                </label>
                            )}
                        </div>
                    ))}
                    </div>
                    <div className="utilities-actions utilities-question-add-actions">
                        <button type="button" className="utilities-btn" onClick={() => setEditQuestions((prev) => [...prev, createEmptyQuestion('single_choice', prev)])}>{t.addQ} · {isZh ? '单选' : 'choice'}</button>
                        <button type="button" className="utilities-btn" onClick={() => setEditQuestions((prev) => [...prev, createEmptyQuestion('text', prev)])}>{t.addQ} · {isZh ? '文本' : 'text'}</button>
                        <button type="button" className="utilities-btn" onClick={() => setEditQuestions((prev) => [...prev, createEmptyQuestion('rating', prev)])}>{t.addQ} · {isZh ? '评分' : 'rating'}</button>
                        <button type="button" className="utilities-btn" onClick={() => setEditQuestions((prev) => [...prev, createEmptyQuestion('multi_choice', prev)])}>{t.addQ} · {isZh ? '多选' : 'multi'}</button>
                    </div>
                    </section>
                    {!selected && (
                        <button type="button" className="utilities-btn utilities-btn--primary" disabled={busy} onClick={() => void createSurvey()}>{t.create}</button>
                    )}
                    {selected?.status === 'draft' && (
                        <button type="button" className="utilities-btn utilities-btn--primary" disabled={busy} onClick={() => void saveDraft()}>{t.save}</button>
                    )}
                </div>
            )}

            {/* Read-only questions when published/closed/archived */}
            {selected?.status === 'draft' && publishChecks.length > 0 && (
                <div className="utilities-checklist" data-testid="survey-publish-checklist">
                    <h3>{canPublishNow ? t.publishReady : t.publishBlocked}</h3>
                    <ul>
                        {publishChecks.map((c) => (
                            <li key={c.id} className={c.ok ? 'ok' : 'bad'}>
                                <span aria-hidden>{c.ok ? '✓' : '!'}</span> {c.label}
                            </li>
                        ))}
                    </ul>
                </div>
            )}

            {selected && selected.status !== 'draft' && (
                <>
                    <h3>{isZh ? '题目（已发布不可改）' : 'Questions (locked)'}</h3>
                    <ul className="utilities-list">
                        {(selected.questions || []).map((q, i) => (
                            <li key={q.id} className="utilities-list-item static">
                                <strong>{i + 1}. {q.title}</strong>
                                <span>{q.type}{q.required ? ' *' : ''}</span>
                                {q.options && q.options.length > 0 && (
                                    <div className="utilities-meta" style={{ width: '100%' }}>
                                        {q.options.map((o, j) => `${j + 1}. ${o.label}`).join(' · ')}
                                    </div>
                                )}
                            </li>
                        ))}
                    </ul>
                </>
            )}

            {selected && (
                <>
                    <h3>{isZh ? '群绑定（蓝信）' : 'Group bindings (Lansenger)'}</h3>
                    <ul className="utilities-list">
                        {(selected.bindings || []).map((b) => (
                            <li key={b.platform + b.group_id} className="utilities-list-item static" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                                <span>{b.group_name || b.group_id} ({b.platform})</span>
                                {(selected.status === 'draft' || selected.status === 'published') && (
                                    <button type="button" className="utilities-btn" disabled={busy} onClick={() => void unbindGroup(b.platform, b.group_id)}>{t.unbind}</button>
                                )}
                            </li>
                        ))}
                    </ul>
                    <div className="utilities-group-pick">
                        {groups.length === 0 && <p className="utilities-meta">{isZh ? '暂无群列表（请配置蓝信并确保有查询权限）' : 'No groups (configure Lansenger)'}</p>}
                        {groups.map((g) => (
                            <button key={g.group_id} type="button" className="utilities-btn" disabled={busy || selected.status === 'closed' || selected.status === 'archived'} onClick={() => void bindGroup(g.group_id, g.name)}>
                                {t.bind}: {g.name}
                            </button>
                        ))}
                    </div>
                </>
            )}
        </div>
    );
};

export default UtilitiesPage;

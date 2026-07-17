/** Pure helpers for multi-question survey draft editing (unit-tested). */

export type EditorOption = { id: string; label: string };
export type EditorQuestion = {
    id: string;
    type: 'single_choice' | 'multi_choice' | 'text' | 'rating';
    title: string;
    required: boolean;
    options?: EditorOption[];
    min?: number;
    max?: number;
    max_length?: number;
};

export function newQuestionId(existing: EditorQuestion[]): string {
    let n = existing.length + 1;
    const ids = new Set(existing.map((q) => q.id));
    while (ids.has(`q${n}`)) n++;
    return `q${n}`;
}

export function newOptionId(options: EditorOption[]): string {
    let n = options.length + 1;
    const ids = new Set(options.map((o) => o.id));
    while (ids.has(`opt_${n}`)) n++;
    return `opt_${n}`;
}

export function createEmptyQuestion(
    type: EditorQuestion['type'],
    existing: EditorQuestion[],
    labels?: { title?: string; optA?: string; optB?: string },
): EditorQuestion {
    const id = newQuestionId(existing);
    if (type === 'text') {
        return { id, type, title: labels?.title || '', required: true, max_length: 500 };
    }
    if (type === 'rating') {
        return { id, type, title: labels?.title || '', required: true, min: 1, max: 5 };
    }
    const optA: EditorOption = { id: 'opt_1', label: labels?.optA || 'A' };
    const optB: EditorOption = { id: 'opt_2', label: labels?.optB || 'B' };
    return {
        id,
        type,
        title: labels?.title || '',
        required: true,
        options: [optA, optB],
    };
}

export function normalizeQuestionsForSave(qs: EditorQuestion[]): EditorQuestion[] {
    return qs.map((q, i) => {
        const base: EditorQuestion = {
            id: q.id || `q${i + 1}`,
            type: q.type,
            title: (q.title || '').trim(),
            required: !!q.required,
        };
        if (q.type === 'single_choice' || q.type === 'multi_choice') {
            base.options = (q.options || [])
                .map((o, j) => ({
                    id: o.id || `opt_${j + 1}`,
                    label: (o.label || '').trim(),
                }))
                .filter((o) => o.label !== '');
            if ((base.options?.length || 0) < 2) {
                throw new Error('choice_needs_two_options');
            }
        }
        if (q.type === 'rating') {
            base.min = q.min ?? 1;
            base.max = q.max ?? 5;
            if (base.min > base.max) {
                throw new Error('rating_min_max');
            }
        }
        if (q.type === 'text') {
            const ml = q.max_length ?? 500;
            if (ml < 0 || ml > 10000) {
                throw new Error('invalid_max_length');
            }
            base.max_length = ml;
        }
        if (!base.title) {
            throw new Error('question_title_required');
        }
        return base;
    });
}

/** Convert datetime-local value (local wall clock) to RFC3339 UTC for Hub. Empty → omit. */
export function deadlineLocalToRFC3339(local: string | undefined | null): string | undefined {
    const s = (local || '').trim();
    if (!s) return undefined;
    const d = new Date(s);
    if (Number.isNaN(d.getTime())) {
        throw new Error('invalid_deadline');
    }
    return d.toISOString();
}

/** Convert Hub deadline (RFC3339 or ISO) to datetime-local input value. */
export function deadlineRFC3339ToLocal(iso: string | undefined | null): string {
    if (!iso) return '';
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return '';
    const pad = (n: number) => String(n).padStart(2, '0');
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export function parseTargetCount(raw: string | number | undefined | null): number {
    if (raw === '' || raw == null) return 0;
    const n = typeof raw === 'number' ? raw : Number(raw);
    if (!Number.isFinite(n) || n < 0 || n > 1_000_000) {
        throw new Error('invalid_target_count');
    }
    return Math.floor(n);
}

/** Completion rate 0–100; null if no target. */
export function completionPercent(responseCount: number, targetCount: number): number | null {
    if (!targetCount || targetCount <= 0) return null;
    return Math.min(100, Math.round((Math.max(0, responseCount) / targetCount) * 100));
}

/** List badge like "3/50 6%"; null if no target. */
export function formatCompletionBadge(responseCount: number, targetCount: number): string | null {
    if (!targetCount || targetCount <= 0) return null;
    const n = Math.max(0, Number(responseCount) || 0);
    const pct = completionPercent(n, targetCount);
    return pct == null ? `${n}/${targetCount}` : `${n}/${targetCount} ${pct}%`;
}

export function buildUpdateSurveyPayload(input: {
    title: string;
    description?: string;
    questions: EditorQuestion[];
    settings?: {
        anonymous?: boolean;
        allow_update?: boolean;
        allow_p2p?: boolean;
        target_count?: number | string;
        /** datetime-local string or empty */
        deadline_local?: string;
        /** already ISO — wins over deadline_local if both set */
        deadline?: string;
    };
}): { title: string; description: string; questions: EditorQuestion[]; settings: Record<string, unknown> } {
    const questions = normalizeQuestionsForSave(input.questions);
    if (questions.length === 0) {
        throw new Error('at_least_one_question');
    }
    const settings: Record<string, unknown> = {
        anonymous: !!input.settings?.anonymous,
        allow_update: !!input.settings?.allow_update,
        allow_p2p: !!input.settings?.allow_p2p,
        target_count: parseTargetCount(input.settings?.target_count),
    };
    let deadlineISO: string | undefined;
    if (input.settings?.deadline) {
        deadlineISO = input.settings.deadline;
    } else if (input.settings?.deadline_local !== undefined) {
        deadlineISO = deadlineLocalToRFC3339(input.settings.deadline_local);
    }
    if (deadlineISO) {
        settings.deadline = deadlineISO;
    } else {
        // Explicit null clears deadline on Hub when JSON includes it — use omit for create;
        // for update Hub SettingsIn with nil pointer leaves old if field missing; send null to clear.
        settings.deadline = null;
    }
    return {
        title: (input.title || '').trim() || 'Untitled',
        description: (input.description || '').trim(),
        questions,
        settings,
    };
}

/** Filter surveys by free-text query against title and short_code (case-insensitive). */
export function filterSurveysByQuery<T extends { title?: string; short_code?: string }>(
    items: T[],
    query: string,
): T[] {
    const q = (query || '').trim().toLowerCase();
    if (!q) return items;
    return items.filter((s) => {
        const title = String(s.title || '').toLowerCase();
        const code = String(s.short_code || '').toLowerCase();
        return title.includes(q) || code.includes(q);
    });
}

export type SurveyListSort = 'updated_desc' | 'updated_asc' | 'title_asc' | 'title_desc' | 'code_asc';

/** Sort surveys; Hub already returns updated_at DESC by default. */
export function sortSurveys<T extends { title?: string; short_code?: string; updated_at?: string; created_at?: string }>(
    items: T[],
    sort: SurveyListSort,
): T[] {
    const arr = [...items];
    const timeOf = (s: T) => {
        const t = Date.parse(String(s.updated_at || s.created_at || ''));
        return Number.isFinite(t) ? t : 0;
    };
    const titleOf = (s: T) => String(s.title || '').toLowerCase();
    const codeOf = (s: T) => String(s.short_code || '').toLowerCase();
    switch (sort) {
        case 'updated_asc':
            return arr.sort((a, b) => timeOf(a) - timeOf(b));
        case 'title_asc':
            return arr.sort((a, b) => titleOf(a).localeCompare(titleOf(b), 'zh'));
        case 'title_desc':
            return arr.sort((a, b) => titleOf(b).localeCompare(titleOf(a), 'zh'));
        case 'code_asc':
            return arr.sort((a, b) => codeOf(a).localeCompare(codeOf(b)));
        case 'updated_desc':
        default:
            return arr.sort((a, b) => timeOf(b) - timeOf(a));
    }
}

export type QuestionMeta = {
    id: string;
    type?: string;
    title?: string;
    options?: Array<{ id: string; label: string }>;
};

/** Format one stored answer for display using option labels / multi order. */
export function formatAnswerForDisplay(q: QuestionMeta | undefined, value: unknown): string {
    if (value == null || value === '') return '—';
    if (!q) return typeof value === 'string' ? value : JSON.stringify(value);
    if (q.type === 'single_choice') {
        const id = String(value);
        const opt = (q.options || []).find((o) => o.id === id);
        return opt?.label || id;
    }
    if (q.type === 'multi_choice') {
        let ids: string[] = [];
        if (Array.isArray(value)) {
            ids = value.map((x) => String(x));
        } else if (typeof value === 'string') {
            try {
                const parsed = JSON.parse(value);
                if (Array.isArray(parsed)) ids = parsed.map(String);
                else ids = [value];
            } catch {
                ids = [value];
            }
        }
        const set = new Set(ids);
        const labels = (q.options || []).filter((o) => set.has(o.id)).map((o) => o.label);
        return labels.length ? labels.join('、') : ids.join('、') || '—';
    }
    if (q.type === 'rating' || q.type === 'text') {
        return String(value);
    }
    return typeof value === 'string' ? value : JSON.stringify(value);
}

/** Draft or closed can be archived (matches Hub Archive rules). */
export function canArchiveSurveyStatus(status: string | undefined | null): boolean {
    return status === 'draft' || status === 'closed';
}

/** Draft or archived can be deleted (matches Hub Delete rules). */
export function canDeleteSurveyStatus(status: string | undefined | null): boolean {
    return status === 'draft' || status === 'archived';
}

/** IDs among selection that are eligible for batch archive. */
export function selectArchivableIds(
    items: Array<{ id: string; status?: string }>,
    selectedIds: string[],
): string[] {
    const set = new Set(selectedIds);
    return items.filter((s) => set.has(s.id) && canArchiveSurveyStatus(s.status)).map((s) => s.id);
}

/** IDs among selection that are eligible for batch delete. */
export function selectDeletableIds(
    items: Array<{ id: string; status?: string }>,
    selectedIds: string[],
): string[] {
    const set = new Set(selectedIds);
    return items.filter((s) => set.has(s.id) && canDeleteSurveyStatus(s.status)).map((s) => s.id);
}

/** True when deadline ISO is in the past (submit would be rejected). */
export function isDeadlineExpired(deadlineISO: string | undefined | null, nowMs: number = Date.now()): boolean {
    if (!deadlineISO) return false;
    const t = Date.parse(deadlineISO);
    if (!Number.isFinite(t)) return false;
    return nowMs >= t;
}

/** List badge label for deadline state. */
export function deadlineListBadge(
    deadlineISO: string | undefined | null,
    labels: { expired: string; open: string },
    nowMs: number = Date.now(),
): string | null {
    if (!deadlineISO) return null;
    return isDeadlineExpired(deadlineISO, nowMs) ? labels.expired : labels.open;
}

export type PublishCheckItem = {
    id: string;
    ok: boolean;
    label: string;
};

/** Preflight checks before Publish (mirrors Hub Publish preconditions). */
export function buildPublishChecklist(
    input: {
        questions?: Array<{ title?: string; type?: string; options?: Array<{ label?: string }> }>;
        bindings?: Array<{ group_id?: string }>;
        status?: string;
        /** RFC3339 deadline; past values block publish (Hub rejects). */
        deadline?: string | null;
    },
    labels: {
        draft: string;
        hasQuestions: string;
        choiceOptions: string;
        hasBindings: string;
        deadlineOk?: string;
    },
    nowMs: number = Date.now(),
): PublishCheckItem[] {
    const qs = input.questions || [];
    const bindings = (input.bindings || []).filter((b) => String(b.group_id || '').trim() !== '');
    const choiceBad = qs.some((q) => {
        if (q.type !== 'single_choice' && q.type !== 'multi_choice') return false;
        const opts = (q.options || []).filter((o) => String(o.label || '').trim() !== '');
        return opts.length < 2;
    });
    const titlesOk = qs.length > 0 && qs.every((q) => String(q.title || '').trim() !== '');
    const deadlineOk = !isDeadlineExpired(input.deadline, nowMs);
    const items: PublishCheckItem[] = [
        { id: 'draft', ok: !input.status || input.status === 'draft', label: labels.draft },
        { id: 'questions', ok: titlesOk, label: labels.hasQuestions },
        { id: 'choice_options', ok: !choiceBad, label: labels.choiceOptions },
        { id: 'bindings', ok: bindings.length >= 1, label: labels.hasBindings },
    ];
    if (labels.deadlineOk) {
        items.push({ id: 'deadline', ok: deadlineOk, label: labels.deadlineOk });
    } else if (!deadlineOk) {
        // Always surface a failed check when deadline is past, even without a label override.
        items.push({ id: 'deadline', ok: false, label: 'deadline not past' });
    }
    return items;
}

export function publishChecklistReady(items: PublishCheckItem[]): boolean {
    return items.length > 0 && items.every((i) => i.ok);
}

/** Whether a response selected a given option on a choice question. */
export function responseMatchesOption(
    answers: unknown,
    questionId: string,
    optionId: string,
): boolean {
    if (!questionId || !optionId) return true;
    let map: Record<string, unknown> = {};
    if (typeof answers === 'string') {
        try {
            map = JSON.parse(answers) || {};
        } catch {
            return false;
        }
    } else if (answers && typeof answers === 'object') {
        map = answers as Record<string, unknown>;
    }
    const v = map[questionId];
    if (v == null) return false;
    if (typeof v === 'string') return v === optionId;
    if (Array.isArray(v)) return v.map(String).includes(optionId);
    return String(v) === optionId;
}

export function filterResponsesByOption<T extends { answers?: unknown }>(
    responses: T[],
    questionId: string,
    optionId: string,
): T[] {
    if (!questionId || !optionId) return responses;
    return responses.filter((r) => responseMatchesOption(r.answers, questionId, optionId));
}

function parseAnswersMap(answers: unknown): Record<string, unknown> {
    if (typeof answers === 'string') {
        try {
            return (JSON.parse(answers) as Record<string, unknown>) || {};
        } catch {
            return {};
        }
    }
    if (answers && typeof answers === 'object') {
        return answers as Record<string, unknown>;
    }
    return {};
}

export type TextAnswerRow = {
    responseId: string;
    text: string;
    respondent: string;
    submittedAt?: string;
};

/** Collect non-empty free-text answers for one text question (design §7.2). */
export function extractTextAnswers(
    questionId: string,
    responses: Array<{
        id?: string;
        answers?: unknown;
        respondent_name?: string;
        respondent_key?: string;
        submitted_at?: string;
    }>,
): TextAnswerRow[] {
    if (!questionId) return [];
    const out: TextAnswerRow[] = [];
    responses.forEach((r, i) => {
        const map = parseAnswersMap(r.answers);
        const raw = map[questionId];
        if (raw == null) return;
        const text = String(raw).trim();
        if (!text) return;
        out.push({
            responseId: r.id || `r${i}`,
            text,
            respondent: String(r.respondent_name || r.respondent_key || '—'),
            submittedAt: r.submitted_at,
        });
    });
    return out;
}

/** Filter text answer rows by free-text query (case-insensitive). */
export function filterTextAnswers(rows: TextAnswerRow[], query: string): TextAnswerRow[] {
    const q = (query || '').trim().toLowerCase();
    if (!q) return rows;
    return rows.filter(
        (row) =>
            row.text.toLowerCase().includes(q) ||
            row.respondent.toLowerCase().includes(q),
    );
}

/**
 * Filter responses by free-text across answer display values, respondent name/key, group.
 * Empty query returns all.
 */
export function filterResponsesByQuery<
    T extends {
        answers?: unknown;
        respondent_name?: string;
        respondent_key?: string;
        group_id?: string;
    },
>(responses: T[], questions: QuestionMeta[], query: string): T[] {
    const q = (query || '').trim().toLowerCase();
    if (!q) return responses;
    return responses.filter((r) => {
        const name = String(r.respondent_name || '').toLowerCase();
        const key = String(r.respondent_key || '').toLowerCase();
        const group = String(r.group_id || '').toLowerCase();
        if (name.includes(q) || key.includes(q) || group.includes(q)) return true;
        const lines = expandResponseAnswers(questions, r.answers);
        return lines.some(
            (line) =>
                line.display.toLowerCase().includes(q) ||
                line.title.toLowerCase().includes(q),
        );
    });
}

/** Normalize short code for clipboard (uppercase, trimmed). */
export function formatShortCodeForCopy(code: string | undefined | null): string {
    return String(code || '').trim().toUpperCase();
}

/** Text suitable for pasting into IM: /survey CODE */
export function formatSurveyIMCommand(code: string | undefined | null): string {
    const c = formatShortCodeForCopy(code);
    return c ? `/survey ${c}` : '';
}

/** Expand answers_json into ordered "题目标题: 显示答案" lines. */
export function expandResponseAnswers(
    questions: QuestionMeta[],
    answers: unknown,
): Array<{ questionId: string; title: string; display: string }> {
    let map: Record<string, unknown> = {};
    if (typeof answers === 'string') {
        try {
            map = JSON.parse(answers) || {};
        } catch {
            map = {};
        }
    } else if (answers && typeof answers === 'object') {
        map = answers as Record<string, unknown>;
    }
    const qs = questions.length ? questions : Object.keys(map).map((id) => ({ id, title: id }));
    return qs.map((q) => ({
        questionId: q.id,
        title: q.title || q.id,
        display: formatAnswerForDisplay(q, map[q.id]),
    }));
}

export function detailToEditorQuestions(detail: { questions?: any[] } | null | undefined): EditorQuestion[] {
    const qs = detail?.questions || [];
    if (!Array.isArray(qs) || qs.length === 0) {
        return [createEmptyQuestion('single_choice', [])];
    }
    return qs.map((q: any, i: number) => ({
        id: q.id || `q${i + 1}`,
        type: (q.type || 'single_choice') as EditorQuestion['type'],
        title: q.title || '',
        required: q.required !== false,
        options: Array.isArray(q.options)
            ? q.options.map((o: any, j: number) => ({ id: o.id || `opt_${j + 1}`, label: o.label || '' }))
            : undefined,
        min: q.min,
        max: q.max,
        max_length: q.max_length,
    }));
}

/** Swap question at index with neighbor (delta -1 up / +1 down). Out of range → no-op copy. */
export function moveQuestionAt<T>(items: T[], index: number, delta: -1 | 1): T[] {
    if (!Array.isArray(items) || items.length === 0) return items ? [...items] : [];
    const j = index + delta;
    if (index < 0 || index >= items.length || j < 0 || j >= items.length) {
        return [...items];
    }
    const next = [...items];
    const tmp = next[index];
    next[index] = next[j];
    next[j] = tmp;
    return next;
}

export type GroupBreakdownRow = {
    groupId: string;
    groupName: string;
    count: number;
    percent: number;
};

/**
 * Per-group response counts for results overview.
 * Includes bound groups with 0; unknown/empty group_id labeled via emptyLabel.
 */
export function buildGroupBreakdown(
    responses: Array<{ group_id?: string }>,
    bindings?: Array<{ group_id?: string; group_name?: string }>,
    emptyLabel: string = 'P2P / unknown',
): GroupBreakdownRow[] {
    const nameById = new Map<string, string>();
    for (const b of bindings || []) {
        const id = String(b.group_id || '').trim();
        if (!id) continue;
        nameById.set(id, String(b.group_name || id).trim() || id);
    }
    const counts = new Map<string, number>();
    for (const id of nameById.keys()) {
        counts.set(id, 0);
    }
    for (const r of responses || []) {
        const id = String(r.group_id || '').trim();
        const key = id || '';
        counts.set(key, (counts.get(key) || 0) + 1);
        if (id && !nameById.has(id)) {
            nameById.set(id, id);
        }
    }
    const total = Math.max(1, (responses || []).length);
    const rows: GroupBreakdownRow[] = [];
    for (const [groupId, count] of counts.entries()) {
        const groupName = groupId
            ? nameById.get(groupId) || groupId
            : emptyLabel;
        rows.push({
            groupId: groupId || '__empty__',
            groupName,
            count,
            percent: (count / total) * 100,
        });
    }
    // When no responses and no bindings, empty list
    if (rows.length === 0) return [];
    rows.sort((a, b) => {
        if (b.count !== a.count) return b.count - a.count;
        return a.groupName.localeCompare(b.groupName, 'zh');
    });
    return rows;
}

/** Non-anonymous exports include respondent name/key — warn before local write (design §9). */
export function shouldWarnExportPII(anonymous: boolean | undefined | null): boolean {
    return !anonymous;
}

/** Sentinel for responses with empty group_id (P2P / unknown). */
export const GROUP_FILTER_EMPTY = '__empty__';

/** Filter by group. Empty filter = all; GROUP_FILTER_EMPTY matches missing group_id. */
export function filterResponsesByGroup<T extends { group_id?: string }>(
    responses: T[],
    groupFilter: string | undefined | null,
): T[] {
    const f = (groupFilter || '').trim();
    if (!f) return responses;
    if (f === GROUP_FILTER_EMPTY) {
        return responses.filter((r) => !String(r.group_id || '').trim());
    }
    return responses.filter((r) => String(r.group_id || '').trim() === f);
}

/** Reorder options (same swap semantics as questions). */
export function moveOptionAt<T>(items: T[], index: number, delta: -1 | 1): T[] {
    return moveQuestionAt(items, index, delta);
}

export type ShareSummaryInput = {
    title?: string;
    shortCode?: string;
    status?: string;
    responseCount: number;
    targetCount?: number;
    deadline?: string;
    anonymous?: boolean;
    groups?: Array<{ groupName: string; count: number; percent?: number }>;
    questions?: Array<{
        title: string;
        type?: string;
        options?: Array<{ label: string; count: number; percent?: number }>;
        ratingAvg?: number;
        ratingN?: number;
        textCount?: number;
    }>;
};

/** CSS modifier classes for list/status pills. */
export function surveyStatusBadgeClass(status: string | undefined | null): string {
    switch (String(status || '').toLowerCase()) {
        case 'published':
            return 'utilities-badge utilities-badge--ok';
        case 'draft':
            return 'utilities-badge utilities-badge--draft';
        case 'closed':
            return 'utilities-badge utilities-badge--muted';
        case 'archived':
            return 'utilities-badge utilities-badge--archived';
        default:
            return 'utilities-badge';
    }
}

/** One section of the operator help panel: a heading plus its lines. */
export type SurveyOperatorHelpGroup = {
    heading: string;
    lines: string[];
};

/** Operator-facing IM command help, grouped for the desktop help panel. */
export function buildSurveyOperatorHelp(isZh: boolean): SurveyOperatorHelpGroup[] {
    if (isZh) {
        return [
            {
                heading: '群内使用',
                lines: [
                    '群内须 @机器人 后发送（问卷已发布且绑定本群）',
                    '/survey <短码> — 开始填写',
                    '/survey <短码> <答案> — 单题快投',
                    '问卷 <短码> 或 调查 <短码> — 同上',
                    '/survey list — 本群进行中的问卷',
                    '/survey status · cancel · help',
                ],
            },
            {
                heading: '答题与修改',
                lines: [
                    '答题中：回复编号/文本；「上一题」回退；「取消」结束',
                    '允许修改时：提交后可 /survey <短码> 再选「修改」',
                ],
            },
            {
                heading: '桌面端',
                lines: [
                    'Ctrl+S 保存草稿；结果页可复制摘要 / 打印 / 导出 Excel',
                ],
            },
        ];
    }
    return [
        {
            heading: 'In a group',
            lines: [
                '@bot first (survey published and bound to the group)',
                '/survey <code> — start',
                '/survey <code> <answer> — single-question fast path',
                '/survey list · status · cancel · help',
            ],
        },
        {
            heading: 'Answering',
            lines: [
                'Send option number/text; 上一题 = prev; 取消 = cancel',
                'If updates allowed: re-run /survey <code> then 修改',
            ],
        },
        {
            heading: 'Desktop',
            lines: [
                'Ctrl+S saves draft; results support summary / print / Excel export',
            ],
        },
    ];
}

/** Localized short status label for badges. */
export function surveyStatusLabel(
    status: string | undefined | null,
    isZh: boolean,
): string {
    const s = String(status || '').toLowerCase();
    if (isZh) {
        switch (s) {
            case 'draft':
                return '草稿';
            case 'published':
                return '收集中';
            case 'closed':
                return '已关闭';
            case 'archived':
                return '已归档';
            default:
                return status || '—';
        }
    }
    switch (s) {
        case 'draft':
            return 'Draft';
        case 'published':
            return 'Open';
        case 'closed':
            return 'Closed';
        case 'archived':
            return 'Archived';
        default:
            return status || '—';
    }
}

/** Plain-text summary suitable for paste into IM / email. */
export function buildSurveyShareSummary(input: ShareSummaryInput, isZh: boolean = true): string {
    const lines: string[] = [];
    const title = (input.title || '').trim() || (isZh ? '未命名问卷' : 'Untitled survey');
    lines.push(title);
    if (input.shortCode) {
        lines.push(formatSurveyIMCommand(input.shortCode));
    }
    const meta: string[] = [];
    if (input.status) meta.push(input.status);
    if (input.anonymous) meta.push(isZh ? '匿名' : 'anonymous');
    meta.push(isZh ? `回收 ${input.responseCount}` : `responses ${input.responseCount}`);
    if (input.targetCount && input.targetCount > 0) {
        const pct = completionPercent(input.responseCount, input.targetCount);
        meta.push(
            isZh
                ? `目标 ${input.targetCount}${pct != null ? ` (${pct}%)` : ''}`
                : `target ${input.targetCount}${pct != null ? ` (${pct}%)` : ''}`,
        );
    }
    if (input.deadline) {
        const d = new Date(input.deadline);
        const ds = Number.isNaN(d.getTime()) ? input.deadline : d.toLocaleString();
        meta.push((isZh ? '截止 ' : 'deadline ') + ds);
    }
    lines.push(meta.join(' · '));

    if (input.groups && input.groups.length > 0) {
        lines.push(isZh ? '【按群】' : '[By group]');
        for (const g of input.groups) {
            const pct =
                typeof g.percent === 'number' && input.responseCount > 0
                    ? ` (${Math.round(g.percent)}%)`
                    : '';
            lines.push(`· ${g.groupName}: ${g.count}${pct}`);
        }
    }

    if (input.questions && input.questions.length > 0) {
        lines.push(isZh ? '【题目】' : '[Questions]');
        input.questions.forEach((q, i) => {
            lines.push(`${i + 1}. ${q.title || '—'}`);
            if (q.options && q.options.length > 0) {
                for (const o of q.options) {
                    const pct = typeof o.percent === 'number' ? ` (${Math.round(o.percent)}%)` : '';
                    lines.push(`   - ${o.label}: ${o.count}${pct}`);
                }
            }
            if (q.type === 'rating' && (q.ratingN || 0) > 0) {
                const avg = typeof q.ratingAvg === 'number' ? q.ratingAvg.toFixed(2) : String(q.ratingAvg ?? '');
                lines.push(isZh ? `   平均 ${avg} (n=${q.ratingN})` : `   avg ${avg} (n=${q.ratingN})`);
            }
            if (q.type === 'text' && (q.textCount || 0) > 0) {
                lines.push(isZh ? `   文本 ${q.textCount} 条` : `   text answers: ${q.textCount}`);
            }
        });
    }
    return lines.join('\n');
}

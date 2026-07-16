import { describe, expect, it } from 'vitest';
import {
    buildUpdateSurveyPayload,
    completionPercent,
    createEmptyQuestion,
    deadlineLocalToRFC3339,
    deadlineRFC3339ToLocal,
    detailToEditorQuestions,
    buildPublishChecklist,
    canArchiveSurveyStatus,
    canDeleteSurveyStatus,
    deadlineListBadge,
    expandResponseAnswers,
    extractTextAnswers,
    filterResponsesByOption,
    filterResponsesByQuery,
    filterSurveysByQuery,
    filterTextAnswers,
    formatAnswerForDisplay,
    formatCompletionBadge,
    formatShortCodeForCopy,
    formatSurveyIMCommand,
    isDeadlineExpired,
    buildGroupBreakdown,
    buildSurveyOperatorHelp,
    buildSurveyShareSummary,
    filterResponsesByGroup,
    GROUP_FILTER_EMPTY,
    moveOptionAt,
    moveQuestionAt,
    normalizeQuestionsForSave,
    parseTargetCount,
    publishChecklistReady,
    selectArchivableIds,
    selectDeletableIds,
    shouldWarnExportPII,
    sortSurveys,
    surveyStatusBadgeClass,
    surveyStatusLabel,
} from '../utilitiesSurveyEditor';

describe('utilitiesSurveyEditor', () => {
    it('creates single_choice with two options', () => {
        const q = createEmptyQuestion('single_choice', [], { title: 'OK?', optA: '是', optB: '否' });
        expect(q.type).toBe('single_choice');
        expect(q.options).toHaveLength(2);
        expect(q.options![0].label).toBe('是');
    });

    it('normalize rejects choice with <2 options', () => {
        expect(() =>
            normalizeQuestionsForSave([
                { id: 'q1', type: 'single_choice', title: 'X', required: true, options: [{ id: 'a', label: 'A' }] },
            ]),
        ).toThrow('choice_needs_two_options');
    });

    it('normalize rejects rating min > max', () => {
        expect(() =>
            normalizeQuestionsForSave([
                { id: 'q1', type: 'rating', title: 'Score', required: true, min: 5, max: 1 },
            ]),
        ).toThrow('rating_min_max');
    });

    it('buildUpdateSurveyPayload multi-question payload', () => {
        const payload = buildUpdateSurveyPayload({
            title: '午餐',
            questions: [
                {
                    id: 'q1',
                    type: 'single_choice',
                    title: '去吗',
                    required: true,
                    options: [
                        { id: 'opt_1', label: '是' },
                        { id: 'opt_2', label: '否' },
                    ],
                },
                { id: 'q2', type: 'text', title: '备注', required: false, max_length: 200 },
                { id: 'q3', type: 'rating', title: '打分', required: true, min: 1, max: 5 },
            ],
            settings: { anonymous: true },
        });
        expect(payload.questions).toHaveLength(3);
        expect(payload.settings.anonymous).toBe(true);
        expect(payload.questions[1].type).toBe('text');
        expect(payload.questions[2].max).toBe(5);
    });

    it('detailToEditorQuestions maps hub detail', () => {
        const qs = detailToEditorQuestions({
            questions: [
                {
                    id: 'q1',
                    type: 'multi_choice',
                    title: '兴趣',
                    required: true,
                    options: [
                        { id: 'a', label: 'A' },
                        { id: 'b', label: 'B' },
                    ],
                },
            ],
        });
        expect(qs[0].type).toBe('multi_choice');
        expect(qs[0].options).toHaveLength(2);
    });

    it('deadline round-trip local <-> RFC3339', () => {
        const local = '2026-12-31T18:30';
        const iso = deadlineLocalToRFC3339(local);
        expect(iso).toMatch(/Z$/);
        const back = deadlineRFC3339ToLocal(iso!);
        // same local wall time when converted back in same TZ
        expect(back).toBe(local);
        expect(deadlineLocalToRFC3339('')).toBeUndefined();
        expect(() => deadlineLocalToRFC3339('not-a-date')).toThrow('invalid_deadline');
    });

    it('parseTargetCount and completionPercent', () => {
        expect(parseTargetCount('')).toBe(0);
        expect(parseTargetCount('12')).toBe(12);
        expect(() => parseTargetCount('-1')).toThrow('invalid_target_count');
        expect(completionPercent(3, 0)).toBeNull();
        expect(completionPercent(3, 10)).toBe(30);
        expect(completionPercent(15, 10)).toBe(100);
    });

    it('filterSurveysByQuery matches title or short_code', () => {
        const items = [
            { id: '1', title: '午餐调查', short_code: 'A3F9K2' },
            { id: '2', title: '团建投票', short_code: 'B1C2D3' },
        ];
        expect(filterSurveysByQuery(items, '')).toHaveLength(2);
        expect(filterSurveysByQuery(items, '午餐')).toEqual([items[0]]);
        expect(filterSurveysByQuery(items, 'b1c2')).toEqual([items[1]]);
        expect(filterSurveysByQuery(items, 'zzz')).toHaveLength(0);
    });

    it('sortSurveys by updated_at and title', () => {
        const items = [
            { id: '1', title: 'B题', short_code: 'Z', updated_at: '2026-01-01T00:00:00Z' },
            { id: '2', title: 'A题', short_code: 'A', updated_at: '2026-06-01T00:00:00Z' },
        ];
        expect(sortSurveys(items, 'updated_desc').map((x) => x.id)).toEqual(['2', '1']);
        expect(sortSurveys(items, 'updated_asc').map((x) => x.id)).toEqual(['1', '2']);
        expect(sortSurveys(items, 'title_asc').map((x) => x.id)).toEqual(['2', '1']);
        expect(sortSurveys(items, 'code_asc').map((x) => x.id)).toEqual(['2', '1']);
    });

    it('formatShortCodeForCopy and IM command', () => {
        expect(formatShortCodeForCopy(' a3f9k2 ')).toBe('A3F9K2');
        expect(formatSurveyIMCommand('a3f9k2')).toBe('/survey A3F9K2');
        expect(formatSurveyIMCommand('')).toBe('');
    });

    it('selectArchivableIds only draft/closed', () => {
        const items = [
            { id: '1', status: 'draft' },
            { id: '2', status: 'published' },
            { id: '3', status: 'closed' },
            { id: '4', status: 'archived' },
        ];
        expect(canArchiveSurveyStatus('draft')).toBe(true);
        expect(canArchiveSurveyStatus('published')).toBe(false);
        expect(selectArchivableIds(items, ['1', '2', '3', '4'])).toEqual(['1', '3']);
        expect(selectArchivableIds(items, ['2'])).toEqual([]);
    });

    it('selectDeletableIds only draft/archived', () => {
        const items = [
            { id: '1', status: 'draft' },
            { id: '2', status: 'published' },
            { id: '3', status: 'closed' },
            { id: '4', status: 'archived' },
        ];
        expect(canDeleteSurveyStatus('archived')).toBe(true);
        expect(canDeleteSurveyStatus('closed')).toBe(false);
        expect(selectDeletableIds(items, ['1', '2', '3', '4'])).toEqual(['1', '4']);
    });

    it('isDeadlineExpired and badge', () => {
        const past = '2020-01-01T00:00:00.000Z';
        const future = '2099-01-01T00:00:00.000Z';
        expect(isDeadlineExpired(past)).toBe(true);
        expect(isDeadlineExpired(future)).toBe(false);
        expect(isDeadlineExpired('')).toBe(false);
        expect(deadlineListBadge(past, { expired: '已截止', open: '未截止' })).toBe('已截止');
        expect(deadlineListBadge(future, { expired: '已截止', open: '未截止' })).toBe('未截止');
    });

    it('buildPublishChecklist blocks missing questions/bindings', () => {
        const labels = {
            draft: 'draft',
            hasQuestions: 'questions',
            choiceOptions: 'options',
            hasBindings: 'bindings',
            deadlineOk: 'deadline',
        };
        const bad = buildPublishChecklist(
            {
                status: 'draft',
                questions: [{ type: 'single_choice', title: 'Q', options: [{ label: 'A' }] }],
                bindings: [],
            },
            labels,
        );
        expect(publishChecklistReady(bad)).toBe(false);
        expect(bad.find((i) => i.id === 'bindings')?.ok).toBe(false);
        expect(bad.find((i) => i.id === 'choice_options')?.ok).toBe(false);

        const good = buildPublishChecklist(
            {
                status: 'draft',
                questions: [
                    {
                        type: 'single_choice',
                        title: 'Q',
                        options: [{ label: 'A' }, { label: 'B' }],
                    },
                ],
                bindings: [{ group_id: 'g1' }],
            },
            labels,
        );
        expect(publishChecklistReady(good)).toBe(true);

        const past = new Date(Date.now() - 3600_000).toISOString();
        const expired = buildPublishChecklist(
            {
                status: 'draft',
                questions: [
                    {
                        type: 'single_choice',
                        title: 'Q',
                        options: [{ label: 'A' }, { label: 'B' }],
                    },
                ],
                bindings: [{ group_id: 'g1' }],
                deadline: past,
            },
            labels,
        );
        expect(publishChecklistReady(expired)).toBe(false);
        expect(expired.find((i) => i.id === 'deadline')?.ok).toBe(false);
    });

    it('filterResponsesByOption for single and multi', () => {
        const rows = [
            { id: 'r1', answers: { q1: 'opt_yes' } },
            { id: 'r2', answers: { q1: 'opt_no' } },
            { id: 'r3', answers: { q1: ['opt_a', 'opt_b'] } },
        ];
        expect(filterResponsesByOption(rows, 'q1', 'opt_yes').map((r) => r.id)).toEqual(['r1']);
        expect(filterResponsesByOption(rows, 'q1', 'opt_b').map((r) => r.id)).toEqual(['r3']);
        expect(filterResponsesByOption(rows, '', '')).toHaveLength(3);
    });

    it('formatAnswerForDisplay and expandResponseAnswers', () => {
        const qs = [
            {
                id: 'q1',
                type: 'single_choice',
                title: '去吗',
                options: [
                    { id: 'opt_yes', label: '是' },
                    { id: 'opt_no', label: '否' },
                ],
            },
            {
                id: 'q2',
                type: 'multi_choice',
                title: '兴趣',
                options: [
                    { id: 'opt_c', label: 'C' },
                    { id: 'opt_a', label: 'A' },
                    { id: 'opt_b', label: 'B' },
                ],
            },
            { id: 'q3', type: 'rating', title: '分' },
        ];
        expect(formatAnswerForDisplay(qs[0], 'opt_yes')).toBe('是');
        // multi follows options array order
        expect(formatAnswerForDisplay(qs[1], ['opt_a', 'opt_c'])).toBe('C、A');
        const lines = expandResponseAnswers(qs, { q1: 'opt_no', q2: ['opt_b'], q3: 4 });
        expect(lines).toHaveLength(3);
        expect(lines[0].display).toBe('否');
        expect(lines[1].display).toBe('B');
        expect(lines[2].display).toBe('4');
    });

    it('buildUpdateSurveyPayload includes deadline and target_count', () => {
        const payload = buildUpdateSurveyPayload({
            title: 'T',
            questions: [
                {
                    id: 'q1',
                    type: 'single_choice',
                    title: 'Q',
                    required: true,
                    options: [
                        { id: 'a', label: 'A' },
                        { id: 'b', label: 'B' },
                    ],
                },
            ],
            settings: {
                deadline_local: '2030-01-15T12:00',
                target_count: '50',
                anonymous: false,
            },
        });
        expect(payload.settings.target_count).toBe(50);
        expect(typeof payload.settings.deadline).toBe('string');
        expect(String(payload.settings.deadline)).toMatch(/2030/);
    });

    it('formatCompletionBadge and text answer helpers', () => {
        expect(formatCompletionBadge(3, 0)).toBeNull();
        expect(formatCompletionBadge(3, 10)).toBe('3/10 30%');
        expect(formatCompletionBadge(15, 10)).toBe('15/10 100%');

        const rows = extractTextAnswers('q_note', [
            { id: 'r1', answers: { q_note: '很好' }, respondent_name: 'Alice' },
            { id: 'r2', answers: { q_note: '  ' }, respondent_name: 'Bob' },
            { id: 'r3', answers: JSON.stringify({ q_note: '一般般' }), respondent_key: 'u3' },
        ]);
        expect(rows).toHaveLength(2);
        expect(rows[0].text).toBe('很好');
        expect(filterTextAnswers(rows, '一').map((r) => r.text)).toEqual(['一般般']);

        const qs = [{ id: 'q1', type: 'text' as const, title: '备注' }];
        const filtered = filterResponsesByQuery(
            [
                { answers: { q1: 'alpha' }, respondent_name: 'Ann' },
                { answers: { q1: 'beta' }, respondent_name: 'Ben' },
            ],
            qs,
            'alp',
        );
        expect(filtered).toHaveLength(1);
        expect(filtered[0].respondent_name).toBe('Ann');
    });

    it('buildUpdateSurveyPayload persists allow_p2p', () => {
        const payload = buildUpdateSurveyPayload({
            title: 'T',
            questions: [
                {
                    id: 'q1',
                    type: 'text',
                    title: 'Q',
                    required: true,
                    max_length: 100,
                },
            ],
            settings: { allow_p2p: true, allow_update: true },
        });
        expect(payload.settings.allow_p2p).toBe(true);
        expect(payload.settings.allow_update).toBe(true);
    });

    it('moveQuestionAt reorders and is a no-op at edges', () => {
        const qs = [{ id: 'a' }, { id: 'b' }, { id: 'c' }];
        expect(moveQuestionAt(qs, 1, -1).map((q) => q.id)).toEqual(['b', 'a', 'c']);
        expect(moveQuestionAt(qs, 0, -1).map((q) => q.id)).toEqual(['a', 'b', 'c']);
        expect(moveQuestionAt(qs, 2, 1).map((q) => q.id)).toEqual(['a', 'b', 'c']);
        expect(moveQuestionAt(qs, 0, 1).map((q) => q.id)).toEqual(['b', 'a', 'c']);
    });

    it('buildGroupBreakdown counts bound groups and empty', () => {
        const rows = buildGroupBreakdown(
            [
                { group_id: 'g1' },
                { group_id: 'g1' },
                { group_id: 'g2' },
                { group_id: '' },
            ],
            [
                { group_id: 'g1', group_name: '研发群' },
                { group_id: 'g2', group_name: '产品群' },
                { group_id: 'g3', group_name: '空群' },
            ],
            '私聊/未知群',
        );
        expect(rows.find((r) => r.groupId === 'g1')?.count).toBe(2);
        expect(rows.find((r) => r.groupId === 'g3')?.count).toBe(0);
        expect(rows.find((r) => r.groupId === '__empty__')?.groupName).toBe('私聊/未知群');
        expect(rows.find((r) => r.groupId === 'g1')?.percent).toBe(50);
    });

    it('shouldWarnExportPII only for non-anonymous', () => {
        expect(shouldWarnExportPII(false)).toBe(true);
        expect(shouldWarnExportPII(undefined)).toBe(true);
        expect(shouldWarnExportPII(true)).toBe(false);
    });

    it('filterResponsesByGroup and GROUP_FILTER_EMPTY', () => {
        const rows = [
            { id: '1', group_id: 'g1' },
            { id: '2', group_id: '' },
            { id: '3', group_id: 'g2' },
            { id: '4' },
        ];
        expect(filterResponsesByGroup(rows, '').map((r) => r.id)).toEqual(['1', '2', '3', '4']);
        expect(filterResponsesByGroup(rows, 'g1').map((r) => r.id)).toEqual(['1']);
        expect(filterResponsesByGroup(rows, GROUP_FILTER_EMPTY).map((r) => r.id)).toEqual(['2', '4']);
    });

    it('moveOptionAt reorders options', () => {
        const opts = [{ id: 'a' }, { id: 'b' }, { id: 'c' }];
        expect(moveOptionAt(opts, 2, -1).map((o) => o.id)).toEqual(['a', 'c', 'b']);
    });

    it('buildSurveyShareSummary includes code and option stats', () => {
        const text = buildSurveyShareSummary(
            {
                title: '午餐调研',
                shortCode: 'A3F9K2',
                status: 'published',
                responseCount: 10,
                targetCount: 20,
                groups: [{ groupName: '研发', count: 6, percent: 60 }],
                questions: [
                    {
                        title: '去吗',
                        type: 'single_choice',
                        options: [
                            { label: '是', count: 7, percent: 70 },
                            { label: '否', count: 3, percent: 30 },
                        ],
                    },
                    { title: '评分', type: 'rating', ratingAvg: 4.2, ratingN: 10 },
                ],
            },
            true,
        );
        expect(text).toContain('午餐调研');
        expect(text).toContain('/survey A3F9K2');
        expect(text).toContain('回收 10');
        expect(text).toContain('研发');
        expect(text).toContain('是: 7');
        expect(text).toContain('平均 4.20');
    });

    it('surveyStatusBadgeClass and surveyStatusLabel', () => {
        expect(surveyStatusBadgeClass('published')).toContain('utilities-badge--ok');
        expect(surveyStatusBadgeClass('draft')).toContain('utilities-badge--draft');
        expect(surveyStatusBadgeClass('closed')).toContain('utilities-badge--muted');
        expect(surveyStatusBadgeClass('archived')).toContain('utilities-badge--archived');
        expect(surveyStatusLabel('published', true)).toBe('收集中');
        expect(surveyStatusLabel('draft', false)).toBe('Draft');
    });

    it('buildSurveyOperatorHelp mentions /survey and list', () => {
        const zh = buildSurveyOperatorHelp(true);
        expect(zh.some((l) => l.includes('/survey'))).toBe(true);
        expect(zh.some((l) => l.includes('list'))).toBe(true);
        const en = buildSurveyOperatorHelp(false);
        expect(en.length).toBeGreaterThan(3);
    });
});

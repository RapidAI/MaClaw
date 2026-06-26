import { describe, expect, it } from 'vitest';
import { applyKnowledgeDomainFilterPayload, applyKnowledgeSearchFilterPayload, applyKnowledgeStructuredSearchPayload, knowledgeCoverageAliasSummary, knowledgeCoverageFilterSummary, knowledgeExecutionActionSourceIDs, knowledgeExecutionFailureDetails, knowledgeExecutionResultSourceIDs, knowledgeExecutionSourceFilterLabel, knowledgeHealthActionConfirmMessage, knowledgeHealthActionExecutable, knowledgeHealthActionExecutionPayload, knowledgeHealthActionManualLabel, knowledgeHealthSummaryModel, knowledgeQualityExecutionContextLabel, knowledgeSourceCoverageOptions, knowledgeSourceCoverageStateValue, knowledgeSourceListPayload, normalizeKnowledgeCoverageOption, normalizeKnowledgeDomainFilter, normalizeKnowledgeFilterToken, normalizeKnowledgeSourceLimit, parseDomainList, parseLabelList, parseURLBatch, resolveKnowledgeCoverageOption } from '../KnowledgeSettingsPanel';

describe('normalizeKnowledgeCoverageOption', () => {
    it('matches backend coverage filter key normalization style', () => {
        expect(normalizeKnowledgeCoverageOption(' Missing-Cards ')).toBe('missing_cards');
        expect(normalizeKnowledgeCoverageOption('missing__cards')).toBe('missing_cards');
        expect(normalizeKnowledgeCoverageOption('missing.cards')).toBe('missing_cards');
        expect(normalizeKnowledgeCoverageOption('missing/cards')).toBe('missing_cards');
        expect(normalizeKnowledgeCoverageOption('missing   facts')).toBe('missing_facts');
        expect(normalizeKnowledgeCoverageOption('HAS_LINKS')).toBe('has_links');
    });
});

describe('knowledgeSourceCoverageOptions', () => {
    it('uses backend capability filters when available', () => {
        expect(knowledgeSourceCoverageOptions({ coverage_filters: ['missing_cards', 'pdf_ocr_needed'] })).toEqual(['missing_cards', 'pdf_ocr_needed']);
    });

    it('normalizes and deduplicates capability filters before rendering options', () => {
        expect(knowledgeSourceCoverageOptions({ coverage_filters: [' Missing-Cards ', '', ' missing cards ', ' HAS_LINKS '] })).toEqual(['missing_cards', 'has_links']);
    });

    it('falls back to built-in filters before capabilities load', () => {
        const options = knowledgeSourceCoverageOptions(null);

        expect(options).toContain('missing_cards');
        expect(options).toContain('has_links');
    });

    it('preserves a normalized selected legacy or alias value when aliases are not available', () => {
        expect(knowledgeSourceCoverageOptions({ coverage_filters: ['missing_cards'] }, ' HasCards ')).toEqual(['missing_cards', 'hascards']);
    });

    it('resolves selected aliases to canonical filters when capabilities expose aliases', () => {
        expect(knowledgeSourceCoverageOptions({
            coverage_filters: ['missing_cards'],
            coverage_aliases: { hascards: 'has_cards' },
        }, ' HasCards ')).toEqual(['missing_cards', 'has_cards']);
    });
});

describe('knowledgeSourceCoverageStateValue', () => {
    it('canonicalizes legacy selected aliases after capabilities load', () => {
        expect(knowledgeSourceCoverageStateValue({
            coverage_aliases: { hascards: 'has_cards' },
        }, ' HasCards ')).toBe('has_cards');
    });

    it('keeps all selected when no coverage filter is active', () => {
        expect(knowledgeSourceCoverageStateValue({
            coverage_aliases: { hascards: 'has_cards' },
        }, 'all')).toBe('all');
        expect(knowledgeSourceCoverageStateValue(null, '')).toBe('all');
    });
});

describe('knowledgeCoverageFilterSummary', () => {
    it('normalizes and deduplicates capability filters without using fallback options', () => {
        expect(knowledgeCoverageFilterSummary({ coverage_filters: [' Missing-Cards ', 'missing cards', ' HAS_LINKS '] })).toEqual(['missing_cards', 'has_links']);
    });

    it('stays empty when capabilities do not expose filters', () => {
        expect(knowledgeCoverageFilterSummary(null)).toEqual([]);
        expect(knowledgeCoverageFilterSummary({})).toEqual([]);
    });

    it('resolves accidental alias filters to canonical filters when aliases are available', () => {
        expect(knowledgeCoverageFilterSummary({
            coverage_filters: ['hasCards', 'needsOCR'],
            coverage_aliases: { hascards: 'has_cards', needsocr: 'pdf_ocr_needed' },
        })).toEqual(['has_cards', 'pdf_ocr_needed']);
    });
});

describe('resolveKnowledgeCoverageOption', () => {
    it('normalizes and resolves aliases to canonical filters', () => {
        expect(resolveKnowledgeCoverageOption({
            coverage_aliases: { ' NeedsOCR ': ' PDF_OCR_NEEDED ' },
        }, 'needs ocr')).toBe('pdf_ocr_needed');
    });
});

describe('knowledgeCoverageAliasSummary', () => {
    it('prioritizes common alias summaries for capability display', () => {
        expect(knowledgeCoverageAliasSummary({
            coverage_aliases: {
                needsocr: 'pdf_ocr_needed',
                rebuildcards: 'missing_cards',
                haslinks: 'has_links',
            },
        })).toEqual([
            'rebuildcards -> missing_cards',
            'needsocr -> pdf_ocr_needed',
            'haslinks -> has_links',
        ]);
    });

    it('normalizes and deduplicates alias summary entries before sorting and display', () => {
        expect(knowledgeCoverageAliasSummary({
            coverage_aliases: {
                ' NeedsOCR ': ' PDF_OCR_NEEDED ',
                needsocr: 'pdf_ocr_needed',
                a: ' missing_nodes ',
            },
        })).toEqual(['needsocr -> pdf_ocr_needed', 'a -> missing_nodes']);
    });

    it('limits noisy alias summaries', () => {
        expect(knowledgeCoverageAliasSummary({
            coverage_aliases: {
                rebuildcards: 'missing_cards',
                c: 'missing_cards',
                a: 'missing_nodes',
                b: 'missing_facts',
            },
        }, 2)).toEqual(['rebuildcards -> missing_cards', 'a -> missing_nodes']);
    });

    it('does not display synthetic compact aliases used only for resolving', () => {
        expect(resolveKnowledgeCoverageOption({
            coverage_aliases: { rebuild_cards: 'missing_cards' },
        }, 'rebuildcards')).toBe('missing_cards');
        expect(knowledgeCoverageAliasSummary({
            coverage_aliases: { rebuild_cards: 'missing_cards' },
        })).toEqual(['rebuild_cards -> missing_cards']);
    });
});

describe('knowledgeSourceListPayload', () => {
    it('uses latest capabilities to resolve coverage aliases in source list payloads', () => {
        expect(knowledgeSourceListPayload({
            coverage_aliases: { hascards: 'has_cards' },
        }, {
            query: '  direct  ',
            kind: 'markdown',
            status: 'distilled',
            coverage: 'hasCards',
            domain: ' https://Docs.Example.com/path?q=1 ',
            labels: ' governed, docs ',
            limit: 5000,
        })).toEqual({
            limit: 5000,
            query: 'direct',
            kind: 'markdown',
            status: 'distilled',
            coverage_filter: 'has_cards',
            domain: 'docs.example.com',
            labels: ['governed', 'docs'],
        });
    });

    it('omits all and empty filters from source list payloads', () => {
        expect(knowledgeSourceListPayload(null, {
            query: ' ',
            kind: 'all',
            status: 'all',
            coverage: 'all',
            domain: '',
            labels: '',
        })).toEqual({ limit: 100 });
    });

    it('normalizes source list limits in payloads', () => {
        expect(knowledgeSourceListPayload(null, { limit: 12.8 })).toEqual({ limit: 12 });
        expect(knowledgeSourceListPayload(null, { limit: 0 })).toEqual({ limit: 100 });
        expect(knowledgeSourceListPayload(null, { limit: -5 })).toEqual({ limit: 100 });
        expect(knowledgeSourceListPayload(null, { limit: 500000 })).toEqual({ limit: 5000 });
    });

    it('normalizes kind and status tokens in source list payloads', () => {
        expect(knowledgeSourceListPayload(null, {
            kind: ' Markdown ',
            status: ' DISTILLED ',
        })).toEqual({ limit: 100, kind: 'markdown', status: 'distilled' });
    });
});

describe('knowledgeHealthSummaryModel', () => {
    it('normalizes the backend health payload for compact display', () => {
        expect(knowledgeHealthSummaryModel({
            status: 'needs_attention',
            score: 72,
            doctor_score: 80,
            quality_avg_score: 64.25,
            quality_grades: { B: 4, A: 8, D: 1, C: 2, F: 9 },
            quality_signals: { missing_cards: 3, missing_facts: 2 },
            doctor_findings: { warning: 2, error: 1 },
            maintenance_actions: [
                { kind: 'rebuild_derived', title: 'Rebuild cards', description: 'Rebuild missing cards.', signals: ['missing_cards'], count: 3 },
                { tool: 'knowledge_link_sources', count: 2 },
            ],
        })).toEqual({
            status: 'needs_attention',
            score: 72,
            doctorScore: 80,
            qualityAvgScore: 64.25,
            gradeEntries: ['F 9', 'A 8', 'B 4', 'C 2'],
            signalEntries: ['missing_cards 3', 'missing_facts 2'],
            findingEntries: ['warning 2', 'error 1'],
            actions: [
                { kind: 'rebuild_derived', title: 'Rebuild cards', description: 'Rebuild missing cards.', signals: ['missing_cards'], count: 3 },
                { tool: 'knowledge_link_sources', count: 2 },
            ],
            actionEntries: ['rebuild_derived 3', 'knowledge_link_sources 2'],
        });
    });

    it('returns null for an empty health payload and clamps invalid numeric fields', () => {
        expect(knowledgeHealthSummaryModel(null)).toBeNull();
        expect(knowledgeHealthSummaryModel({
            score: Number.NaN,
            quality_avg_score: Number.NaN,
        })).toMatchObject({
            status: 'unknown',
            score: 0,
            qualityAvgScore: 0,
        });
    });
});

describe('knowledgeHealthActionExecutionPayload', () => {
    it('builds a single-action maintenance execution request from a health action', () => {
        expect(knowledgeHealthActionExecutionPayload(
            { limit: 100, coverage_filter: 'missing_cards' },
            { kind: 'rebuild_derived_gaps', source_ids: ['s1', 's2'] },
            {
                policy: 'balanced',
                dryRun: false,
                distillMode: 'rules_only',
                maxSourcesPerAction: 50,
                allowSensitiveDisable: true,
                allowDuplicateSuppression: true,
            },
        )).toEqual({
            filter: { limit: 100, coverage_filter: 'missing_cards', source_ids: ['s1', 's2'] },
            policy: 'balanced',
            actions: ['rebuild_derived_gaps'],
            dry_run: false,
            distill_mode: 'rules_only',
            max_sources_per_action: 50,
            allow_sensitive_disable: true,
            allow_duplicate_suppression: true,
        });
    });

    it('defaults to dry-run and rejects actions without a kind', () => {
        expect(knowledgeHealthActionExecutionPayload({}, {})).toBeNull();
        expect(knowledgeHealthActionExecutionPayload({ limit: 25 }, { kind: 'backfill_labels' })).toMatchObject({
            filter: { limit: 25 },
            actions: ['backfill_labels'],
            dry_run: true,
        });
    });

    it('builds scoped payloads for missing-node refresh actions', () => {
        expect(knowledgeHealthActionExecutionPayload(
            { limit: 10, quality_grade: 'poor' },
            { kind: 'refresh_or_reimport_missing_nodes', source_ids: ['s1', 's2', 's3'], executable: true },
            { dryRun: true, policy: 'balanced' },
        )).toMatchObject({
            filter: { limit: 10, quality_grade: 'poor', source_ids: ['s1', 's2', 's3'] },
            policy: 'balanced',
            actions: ['refresh_or_reimport_missing_nodes'],
            dry_run: true,
        });
    });

    it('expands source limits to cover all health action source ids', () => {
        expect(knowledgeHealthActionExecutionPayload(
            { limit: 1 },
            { kind: 'refresh_or_reimport_missing_nodes', source_ids: ['s1', 's2', 's3'], executable: true },
            { maxSourcesPerAction: 2 },
        )).toMatchObject({
            filter: { limit: 3, source_ids: ['s1', 's2', 's3'] },
            actions: ['refresh_or_reimport_missing_nodes'],
            max_sources_per_action: 3,
        });
    });

    it('normalizes noisy health action limits and source ids', () => {
        expect(knowledgeHealthActionExecutionPayload(
            { limit: 'not-a-number' },
            { kind: 'refresh_or_reimport_missing_nodes', source_ids: [' s1 ', '', 's1', 's2'], executable: true },
            { maxSourcesPerAction: Number.NaN },
        )).toMatchObject({
            filter: { limit: 2, source_ids: ['s1', 's2'] },
            actions: ['refresh_or_reimport_missing_nodes'],
            max_sources_per_action: 100,
        });
    });
});

describe('knowledgeHealthActionConfirmMessage', () => {
    it('summarizes write actions before execution', () => {
        expect(knowledgeHealthActionConfirmMessage({
            title: 'Backfill labels',
            kind: 'backfill_labels',
            count: 4,
            signals: ['missing_labels'],
        })).toBe([
            'Run knowledge health action: Backfill labels?',
            'Kind: backfill_labels',
            'Affected sources: 4',
            'Signals: missing_labels',
        ].join('\n'));
    });
});

describe('knowledgeHealthActionExecutable', () => {
    it('honors backend executable flags while allowing legacy actions', () => {
        expect(knowledgeHealthActionExecutable({ kind: 'backfill_labels', executable: true })).toBe(true);
        expect(knowledgeHealthActionExecutable({ kind: 'refresh_or_reimport_missing_nodes', executable: true })).toBe(true);
        expect(knowledgeHealthActionExecutable({ kind: 'refresh_or_reimport_missing_nodes', executable: false })).toBe(false);
        expect(knowledgeHealthActionExecutable({ kind: 'legacy_action' })).toBe(true);
        expect(knowledgeHealthActionExecutable({})).toBe(false);
    });
});

describe('knowledgeHealthActionManualLabel', () => {
    it('includes backend manual reasons when available', () => {
        expect(knowledgeHealthActionManualLabel({ manual_reason: 'requires_refresh_or_reimport_entrypoint' })).toBe('Manual: requires_refresh_or_reimport_entrypoint');
        expect(knowledgeHealthActionManualLabel({})).toBe('Manual');
    });
});

describe('knowledgeQualityExecutionContextLabel', () => {
    it('labels health and plan execution results with source, action, and mode', () => {
        expect(knowledgeQualityExecutionContextLabel({ source: 'health', action: 'backfill_labels', dryRun: true })).toBe('health / backfill_labels / preview');
        expect(knowledgeQualityExecutionContextLabel({ source: 'quality_plan', action: 'all_actions', dryRun: false })).toBe('quality_plan / all_actions / run');
    });

    it('omits empty execution context parts', () => {
        expect(knowledgeQualityExecutionContextLabel(null)).toBe('');
        expect(knowledgeQualityExecutionContextLabel({ action: 'refresh_topic_links' })).toBe('refresh_topic_links / run');
    });
});

describe('knowledgeExecutionResultSourceIDs', () => {
    it('collects unique source IDs from execution-level and per-action results', () => {
        expect(knowledgeExecutionResultSourceIDs({
            source_id: 's0',
            source_ids: ['s1', 's2', 's1'],
            failures: [{ source_id: 's5' }],
            previews: [{ source_id: 's8' }],
            results: [
                { source_id: 's11', source_ids: ['s3', 's2'] },
                { source_ids: ['s4'], result: { source_id: 's12', source_ids: ['s10'], failures: [{ source_id: 's6' }], sources: [{ id: 's7' }], previews: [{ source_id: 's9' }] } },
            ],
        })).toEqual(['s1', 's2', 's0', 's5', 's8', 's11', 's3', 's4', 's12', 's10', 's7', 's6', 's9']);
    });

    it('handles empty execution results and respects limits', () => {
        expect(knowledgeExecutionResultSourceIDs(null)).toEqual([]);
        expect(knowledgeExecutionResultSourceIDs({ source_ids: ['s1', 's2', 's3'] }, 2)).toEqual(['s1', 's2']);
    });
});

describe('knowledgeExecutionActionSourceIDs', () => {
    it('summarizes nested source IDs for one action result', () => {
        expect(knowledgeExecutionActionSourceIDs({
            source_ids: ['s1'],
            result: {
                source_ids: ['s2'],
                failures: [{ source_id: 's3' }],
                previews: [{ source_id: 's4' }],
            },
        })).toEqual(['s1', 's2', 's3', 's4']);
    });
});

describe('knowledgeExecutionFailureDetails', () => {
    it('collects action-level and nested source failures', () => {
        expect(knowledgeExecutionFailureDetails({
            source_id: 'root-source',
            error: 'root failure',
            results: [
                {
                    kind: 'refresh_or_reimport_missing_nodes',
                    source_ids: ['s1'],
                    error: 'refresh_or_reimport_failed',
                    result: {
                        failures: [
                            { source_id: 's1', error: 'source s1 kind "text" is not refreshable' },
                            { source_id: 's1', error: 'source s1 kind "text" is not refreshable' },
                        ],
                        previews: [
                            { source_id: 's3', error: 'preview failed' },
                        ],
                        source_id: 's4',
                        error: 'nested result failed',
                    },
                },
                {
                    kind: 'backfill_labels',
                    failures: [{ sourceID: 's2', message: 'label failure' }],
                },
                {
                    kind: 'bulk_refresh',
                    source_ids: ['s5', 's6'],
                    error: 'bulk failed',
                    result: {
                        source_ids: ['s7', 's8'],
                        error: 'nested bulk failed',
                    },
                },
            ],
        })).toEqual([
            { action: '', sourceID: 'root-source', error: 'root failure' },
            { action: 'refresh_or_reimport_missing_nodes', sourceID: 's1', error: 'source s1 kind "text" is not refreshable' },
            { action: 'refresh_or_reimport_missing_nodes', sourceID: 's3', error: 'preview failed' },
            { action: 'refresh_or_reimport_missing_nodes', sourceID: 's4', error: 'nested result failed' },
            { action: 'refresh_or_reimport_missing_nodes', sourceID: 's1', error: 'refresh_or_reimport_failed' },
            { action: 'backfill_labels', sourceID: 's2', error: 'label failure' },
            { action: 'bulk_refresh', sourceID: 's7', error: 'nested bulk failed' },
            { action: 'bulk_refresh', sourceID: 's8', error: 'nested bulk failed' },
            { action: 'bulk_refresh', sourceID: 's5', error: 'bulk failed' },
            { action: 'bulk_refresh', sourceID: 's6', error: 'bulk failed' },
        ]);
    });

    it('handles empty values and respects limits', () => {
        expect(knowledgeExecutionFailureDetails(null)).toEqual([]);
        expect(knowledgeExecutionFailureDetails({ failures: [{ source_id: 's1', error: 'one' }, { source_id: 's2', error: 'two' }] }, 1)).toEqual([
            { action: '', sourceID: 's1', error: 'one' },
        ]);
    });
});

describe('knowledgeExecutionSourceFilterLabel', () => {
    it('builds a stable label for execution-source filters', () => {
        expect(knowledgeExecutionSourceFilterLabel('health / backfill_labels / preview', 3)).toBe('health / backfill_labels / preview / sources 3');
        expect(knowledgeExecutionSourceFilterLabel('', Number.NaN)).toBe('execution / sources 0');
    });
});

describe('normalizeKnowledgeSourceLimit', () => {
    it('uses positive finite integer limits within a bounded maximum', () => {
        expect(normalizeKnowledgeSourceLimit(250.9)).toBe(250);
        expect(normalizeKnowledgeSourceLimit(Number.NaN, 5000)).toBe(5000);
        expect(normalizeKnowledgeSourceLimit(undefined, 5000)).toBe(5000);
        expect(normalizeKnowledgeSourceLimit(9000, 100, 500)).toBe(500);
        expect(normalizeKnowledgeSourceLimit(0, 9000, 500)).toBe(500);
    });
});

describe('normalizeKnowledgeFilterToken', () => {
    it('trims and lowercases filter tokens', () => {
        expect(normalizeKnowledgeFilterToken(' Markdown ')).toBe('markdown');
        expect(normalizeKnowledgeFilterToken()).toBe('');
    });
});

describe('normalizeKnowledgeDomainFilter', () => {
    it('extracts hostnames from URL filters and lowercases plain domains', () => {
        expect(normalizeKnowledgeDomainFilter(' https://Docs.Example.com/path?q=1 ')).toBe('docs.example.com');
        expect(normalizeKnowledgeDomainFilter('Docs.Example.com/path?q=1')).toBe('docs.example.com');
        expect(normalizeKnowledgeDomainFilter('Example.COM:8443/path')).toBe('example.com');
        expect(normalizeKnowledgeDomainFilter(' Example.COM ')).toBe('example.com');
        expect(normalizeKnowledgeDomainFilter(' https://*.Example.COM./path ')).toBe('example.com');
        expect(normalizeKnowledgeDomainFilter(' *.Example.COM. ')).toBe('example.com');
        expect(normalizeKnowledgeDomainFilter(' https://user:pass@Docs.Example.com/path ')).toBe('docs.example.com');
        expect(normalizeKnowledgeDomainFilter('')).toBe('');
    });
});

describe('applyKnowledgeDomainFilterPayload', () => {
    it('adds normalized domains to payloads', () => {
        const payload: Record<string, unknown> = { query: 'alpha' };

        applyKnowledgeDomainFilterPayload(payload, ' https://Docs.Example.com/path ');

        expect(payload).toEqual({ query: 'alpha', domain: 'docs.example.com' });
    });

    it('leaves payloads unchanged for empty domains', () => {
        const payload: Record<string, unknown> = { query: 'alpha' };

        applyKnowledgeDomainFilterPayload(payload, ' ');

        expect(payload).toEqual({ query: 'alpha' });
    });
});

describe('applyKnowledgeSearchFilterPayload', () => {
    it('applies normalized search filters to payloads', () => {
        const payload: Record<string, unknown> = { query: 'alpha' };

        applyKnowledgeSearchFilterPayload(payload, {
            resultType: ' Card ',
            sourceKind: ' Markdown ',
            domain: ' https://Docs.Example.com/path ',
            sourceID: ' ksrc_1 ',
            labels: 'Governed, docs',
        });

        expect(payload).toEqual({
            query: 'alpha',
            result_types: ['card'],
            source_kinds: ['markdown'],
            domain: 'docs.example.com',
            source_ids: ['ksrc_1'],
            labels: ['governed', 'docs'],
        });
    });

    it('omits empty and all search filters from payloads', () => {
        const payload: Record<string, unknown> = { query: 'alpha' };

        applyKnowledgeSearchFilterPayload(payload, {
            resultType: 'all',
            sourceKind: ' all ',
            domain: ' ',
            sourceID: '',
            labels: '',
        });

        expect(payload).toEqual({ query: 'alpha' });
    });
});

describe('applyKnowledgeStructuredSearchPayload', () => {
    it('builds text column filters for structured table search', () => {
        const payload: Record<string, unknown> = {};

        applyKnowledgeStructuredSearchPayload(payload, {
            query: ' 张三 ',
            sourceID: ' src_1 ',
            sheetName: ' Sheet1 ',
            columnName: ' 部门 ',
            matchMode: ' contains ',
            columnValue: ' 法务 ',
            limit: 20,
            includeDisabled: true,
        });

        expect(payload).toEqual({
            query: '张三',
            source_ids: ['src_1'],
            sheet_names: ['Sheet1'],
            column_contains: { '部门': '法务' },
            limit: 20,
            include_disabled: true,
        });
    });

    it('builds number and date ranges on the selected column', () => {
        const payload: Record<string, unknown> = {};

        applyKnowledgeStructuredSearchPayload(payload, {
            columnName: '金额',
            numberMin: '100',
            numberMax: '200',
            dateStart: '2024-01-01',
            dateEnd: '2024-12-31',
            limit: 500,
        });

        expect(payload).toEqual({
            number_ranges: { '金额': { min: 100, max: 200 } },
            date_ranges: { '金额': { start: '2024-01-01', end: '2024-12-31' } },
            limit: 100,
            include_disabled: false,
        });
    });

    it('accepts comma-formatted number range inputs', () => {
        const payload: Record<string, unknown> = {};

        applyKnowledgeStructuredSearchPayload(payload, {
            columnName: 'amount',
            numberMin: '1,200.5',
            numberMax: '9,999',
        });

        expect(payload).toMatchObject({
            number_ranges: { amount: { min: 1200.5, max: 9999 } },
        });
    });
});

describe('parseDomainList', () => {
    it('normalizes URL, wildcard, port, and duplicate domain entries', () => {
        expect(parseDomainList('*.Example.com\nhttps://Docs.Example.com/path docs.example.com:8443/path https://*.Example.com./docs https://user:pass@Docs.Example.com/secret')).toEqual(['example.com', 'docs.example.com']);
    });

    it('drops empty domain entries', () => {
        expect(parseDomainList('  , ;\n')).toEqual([]);
    });

    it('splits common Chinese domain separators', () => {
        expect(parseDomainList('Example.com，Docs.Example.com；Api.Example.com、example.com')).toEqual(['example.com', 'docs.example.com', 'api.example.com']);
    });
});

describe('parseURLBatch', () => {
    it('splits, trims, and deduplicates URL batches', () => {
        expect(parseURLBatch(' https://a.example.com, https://b.example.com;https://a.example.com\nhttps://c.example.com ')).toEqual([
            'https://a.example.com',
            'https://b.example.com',
            'https://c.example.com',
        ]);
    });

    it('splits common Chinese URL separators', () => {
        expect(parseURLBatch('https://a.example.com，https://b.example.com；https://c.example.com、https://a.example.com')).toEqual([
            'https://a.example.com',
            'https://b.example.com',
            'https://c.example.com',
        ]);
    });
});

describe('parseLabelList', () => {
    it('normalizes, splits, and deduplicates labels', () => {
        expect(parseLabelList(' Governed, docs;GOVERNED\nProject Notes\t docs ')).toEqual(['governed', 'docs', 'project notes']);
    });

    it('keeps spaces inside a single label while dropping empty parts', () => {
        expect(parseLabelList('  product strategy  ,, ;\n')).toEqual(['product strategy']);
    });

    it('collapses repeated whitespace inside labels before deduplicating', () => {
        expect(parseLabelList('Project   Notes, project notes,项目　知识')).toEqual(['project notes', '项目 知识']);
    });

    it('splits common Chinese label separators', () => {
        expect(parseLabelList('治理，文档；项目、知识库')).toEqual(['治理', '文档', '项目', '知识库']);
    });
});

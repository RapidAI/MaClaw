import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { CSSProperties, ReactNode } from 'react';
import { KnowledgeImportDialog } from './KnowledgeImportDialog';
import { ConfirmDialog } from '../modals/ConfirmDialog';
import { DeepCrawlPanel } from './DeepCrawlPanel';
import type { DeepCrawlConfig, DeepCrawlPreviewResult } from './DeepCrawlPanel';
import {
    KnowledgeCapabilities,
    KnowledgeBackfillSourceAutoLabels,
    KnowledgeDeleteSource,
    KnowledgeDiscoverURLs,
    KnowledgeDoctor,
    KnowledgeContextPack,
    KnowledgeDisableSensitiveSources,
    KnowledgeDisableSource,
    KnowledgeDisableSources,
    KnowledgeDisableSourcesByFilter,
    KnowledgeEnableSource,
    KnowledgeEnableSourcesByFilter,
    KnowledgeEntityProfile,
    KnowledgeExecuteSourceQualityMaintenancePlan,
    KnowledgeExplain,
    KnowledgeExportSnapshot,
    KnowledgeFactGraph,
    KnowledgeFactIndex,
    KnowledgeExportSnapshotWithOptions,
    KnowledgeHealth,
    KnowledgeClearAll,
    KnowledgeImportDirectory,
    KnowledgeImportFiles,
    KnowledgeImportSnapshot,
    KnowledgeImportJobStatus,
    KnowledgeListURLDomainPolicies,
    KnowledgeListImportBatches,
    KnowledgeListImportItems,
    KnowledgeListNodesBySource,
    KnowledgeListSourceLinks,
    KnowledgeListSourceLabels,
    KnowledgeListSourceLinkEvents,
    KnowledgeListSourceVersions,
    KnowledgeListCardsBySource,
    KnowledgeListDuplicateCards,
    KnowledgeListFactsBySource,
    KnowledgeListSuppressedCards,
    KnowledgeListSources,
    KnowledgeLinkSources,
    KnowledgeMaintain,
    KnowledgePreviewSourceRefresh,
    KnowledgePreviewSourcesRefreshByFilter,
    KnowledgePreviewSourceTopicLinks,
    KnowledgeRefreshChangedSources,
    KnowledgeRefreshChangedSourcesByFilter,
    KnowledgeRefreshSourceTopicLinks,
    KnowledgeRefreshSourceTopicLinksByFilter,
    KnowledgeRebuildSourceDerived,
    KnowledgeRebuildSourcesDerived,
    KnowledgeRebuildSourcesDerivedByFilter,
    KnowledgeRefreshSource,
    KnowledgeRefreshSources,
    KnowledgeRefreshSourcesByFilter,
    KnowledgeRetryImportBatch,
    KnowledgeRestoreSuppressedCards,
    KnowledgeSaveText,
    KnowledgeSaveURL,
    KnowledgeSaveURLs,
    KnowledgeScanSensitiveContent,
    KnowledgeScanDirectory,
    KnowledgeScanFiles,
    KnowledgeSearch,
    KnowledgeSearchFacets,
    KnowledgeSourceGraph,
    KnowledgeSourceNeighborhood,
    KnowledgeSourcePath,
    KnowledgeSourceDigest,
    KnowledgeSourceTimeline,
    KnowledgeTopicRelevance,
    KnowledgeQualityMaintenancePolicies,
    KnowledgeSourceQualityMaintenancePlan,
    KnowledgeSourceQualityReport,
    KnowledgeStartImportDirectory,
    KnowledgeSuggest,
    KnowledgeSuppressDuplicateCards,
    KnowledgeUnlinkSources,
    KnowledgeUpdateURLDomainPolicies,
    KnowledgeUpdateSourceMetadata,
    KnowledgeUpdateSourceLabels,
    KnowledgeDeepCrawl,
    KnowledgeDeepCrawlPreview,
    SelectKnowledgeDirectory,
    SelectKnowledgeFiles,
} from '../../../wailsjs/go/main/App';
import { colors, radius } from '../remote/styles';

type Props = {
    lang?: string;
};

type Source = {
    id?: string;
    kind?: string;
    uri?: string;
    canonical_uri?: string;
    title?: string;
    status?: string;
    project_path?: string;
    relative_path?: string;
    topic_hint?: string;
    source_trust?: number;
    error_message?: string;
    updated_at?: string;
    node_count?: number;
    card_count?: number;
    fact_count?: number;
    labels?: string[];
};

type SourceLabelSummary = {
    label?: string;
    count?: number;
    source_ids?: string[];
    source_names?: string[];
};

type SourceChangePreview = {
    source_id?: string;
    refreshable?: boolean;
    changed?: boolean;
    hash_changed?: boolean;
    requires_refresh?: boolean;
    old_status?: string;
    new_status?: string;
    old_node_count?: number;
    new_node_count?: number;
    added_nodes?: number;
    removed_nodes?: number;
    unchanged_nodes?: number;
    error?: string;
    samples?: Array<{ kind?: string; title?: string; snippet?: string }>;
};

type KnowledgeNode = {
    id?: string;
    source_id?: string;
    parent_id?: string;
    type?: string;
    title?: string;
    text?: string;
    level?: number;
    page?: number;
    sheet_name?: string;
    row_range?: string;
    col_range?: string;
    xpath?: string;
    offset?: number;
    token_count?: number;
};

type SourceVersion = {
    id?: string;
    source_id?: string;
    kind?: string;
    uri?: string;
    canonical_uri?: string;
    title?: string;
    content_hash?: string;
    status?: string;
    reason?: string;
    fetched_at?: string;
    node_count?: number;
    card_count?: number;
    fact_count?: number;
    created_at?: string;
};

type SourceLink = {
    source_id?: string;
    related_source_id?: string;
    relation?: string;
    score?: number;
    terms?: string[];
    evidence?: string[];
    related_source?: Source;
    created_at?: string;
    updated_at?: string;
};

type SourceTopicLinkBuildResult = {
    source_id?: string;
    scanned?: number;
    candidates?: number;
    linked?: number;
    skipped?: number;
    links?: SourceLink[];
    notes?: string[];
};

type SourceLinkEvent = {
    id?: string;
    source_id?: string;
    related_source_id?: string;
    relation?: string;
    action?: string;
    score?: number;
    terms?: string[];
    evidence?: string[];
    note?: string;
    created_at?: string;
};

type SourceTimelineEvent = {
    id?: string;
    source_id?: string;
    kind?: string;
    action?: string;
    title?: string;
    detail?: string;
    status?: string;
    relation?: string;
    related_source_id?: string;
    score?: number;
    terms?: string[];
    evidence?: string[];
    version_id?: string;
    content_hash?: string;
    node_count?: number;
    card_count?: number;
    fact_count?: number;
    occurred_at?: string;
};

type SourceTimelineResult = {
    source_id?: string;
    count?: number;
    limit?: number;
    events?: SourceTimelineEvent[];
    notes?: string[];
};

type SourceDigestResult = {
    source_id?: string;
    source?: Source;
    title?: string;
    labels?: string[];
    topics?: string[];
    entities?: string[];
    tags?: string[];
    node_count?: number;
    card_count?: number;
    fact_count?: number;
    link_count?: number;
    timeline_count?: number;
    nodes?: KnowledgeNode[];
    cards?: KnowledgeCard[];
    facts?: KnowledgeFact[];
    links?: SourceLink[];
    timeline?: SourceTimelineEvent[];
    notes?: string[];
};

type KnowledgeCard = {
    id?: string;
    source_id?: string;
    node_id?: string;
    title?: string;
    claim?: string;
    summary?: string;
    entities?: string[];
    topics?: string[];
    tags?: string[];
    confidence?: number;
    importance?: number;
    updated_at?: string;
};

type KnowledgeFact = {
    id?: string;
    card_id?: string;
    source_id?: string;
    subject?: string;
    predicate?: string;
    object?: string;
    confidence?: number;
};

type DuplicateCardGroup = {
    key?: string;
    claim?: string;
    count?: number;
    card_ids?: string[];
    source_ids?: string[];
    examples?: string[];
    owner_id?: string;
    tenant_id?: string;
    project_path?: string;
};

type CardSuppression = {
    card_id?: string;
    source_id?: string;
    claim?: string;
    source_title?: string;
    relative_path?: string;
    reason?: string;
    created_at?: string;
};

type SensitiveFinding = {
    kind?: string;
    severity?: string;
    source_id?: string;
    source_title?: string;
    relative_path?: string;
    uri?: string;
    field?: string;
    redacted?: string;
    snippet?: string;
};

type SensitiveScanResult = {
    count?: number;
    max_severity?: string;
    findings?: SensitiveFinding[];
};

type SensitiveIsolationResult = {
    scan?: SensitiveScanResult;
    source_ids?: string[];
    update?: {
        requested?: number;
        updated?: number;
        failed?: number;
        status?: string;
    };
};

type SourceQualityItem = {
    source?: Source;
    score?: number;
    grade?: string;
    signals?: string[];
    actions?: string[];
    sensitive_findings?: number;
    duplicate_claims?: number;
};

type SourceQualityReport = {
    count?: number;
    average_score?: number;
    grades?: Record<string, number>;
    signals?: Record<string, number>;
    actions?: Record<string, number>;
    items?: SourceQualityItem[];
    notes?: string[];
};

type SourceQualityMaintenanceAction = {
    kind?: string;
    title?: string;
    description?: string;
    severity?: string;
    count?: number;
    source_ids?: string[];
    signals?: string[];
    tool?: string;
    args?: Record<string, any>;
};

type SourceQualityMaintenancePlan = {
    quality?: SourceQualityReport;
    count?: number;
    actions?: SourceQualityMaintenanceAction[];
    notes?: string[];
};

type SourceQualityMaintenancePolicy = {
    name?: string;
    title?: string;
    description?: string;
    actions?: string[];
    default_dry_run?: boolean;
    distill_mode?: string;
    max_sources_per_action?: number;
    allow_sensitive_disable?: boolean;
    allow_duplicate_suppression?: boolean;
    query_requires_llm?: boolean;
    may_use_llm_for_structuring?: boolean;
    requires_explicit_write?: boolean;
    notes?: string[];
};

type SourceQualityMaintenanceExecuteResult = {
    plan?: SourceQualityMaintenancePlan;
    dry_run?: boolean;
    count?: number;
    results?: Array<{
        kind?: string;
        requested?: number;
        updated?: number;
        failed?: number;
        skipped?: number;
        dry_run?: boolean;
        source_ids?: string[];
        result?: any;
        failures?: Array<{ source_id?: string; error?: string }>;
        warnings?: string[];
        error?: string;
    }>;
    warnings?: string[];
    notes?: string[];
};

type MaintenanceResult = {
    integrity_ok?: boolean;
    integrity_check?: string;
    optimized_fts?: string[];
    checkpointed?: boolean;
    vacuumed?: boolean;
    warnings?: string[];
    errors?: string[];
};

type ExportResult = {
    output_path?: string;
    format?: string;
    redact_sensitive?: boolean;
    scoped?: boolean;
    source_ids?: string[];
    url_policies?: number;
    sources?: number;
    source_versions?: number;
    source_links?: number;
    source_link_events?: number;
    nodes?: number;
    cards?: number;
    facts?: number;
    bytes?: number;
    generated_at?: string;
};

type SnapshotImportResult = {
    input_path?: string;
    dry_run?: boolean;
    overwrite?: boolean;
    safety_backup_path?: string;
    safety_backup?: ExportResult;
    records?: number;
    would_import?: number;
    imported?: number;
    skipped?: number;
    conflicts?: number;
    unknown_records?: number;
    missing_references?: number;
    failed?: number;
    url_policies?: number;
    sources?: number;
    source_versions?: number;
    source_links?: number;
    source_link_events?: number;
    nodes?: number;
    cards?: number;
    facts?: number;
    conflict_items?: Array<{ line?: number; type?: string; id?: string }>;
    failures?: Array<{ line?: number; type?: string; id?: string; error?: string }>;
};

type FormatCapability = {
    kind?: string;
    extensions?: string[];
    parser?: string;
    search_unit?: string;
    status?: string;
    refreshable?: boolean;
    default_import?: boolean;
    notes?: string;
};

type KnowledgeCapabilitiesResult = {
    default_include_exts?: string[];
    default_auto_labels?: boolean;
    auto_label_rules?: string[];
    formats?: FormatCapability[];
    query_requires_llm?: boolean;
    write_llm_optional?: boolean;
    distill_modes?: string[];
    coverage_filters?: string[];
    coverage_aliases?: Record<string, string>;
    local_indexes?: string[];
    storage_backend?: string;
    search_backend?: string;
};

type ImportResult = {
    batch_id?: string;
    status?: string;
    root_path?: string;
    total_files?: number;
    queued_files?: number;
    duplicate_files?: number;
    skipped_files?: number;
    imported_files?: number;
    failed_files?: number;
    processed_files?: number;
    current_file?: string;
    estimated_bytes?: number;
    warnings?: string[];
};

type URLBatchSaveResult = {
    requested?: number;
    saved?: number;
    failed?: number;
    skipped?: number;
    items?: Array<{ url?: string; source_id?: string; title?: string; status?: string; error?: string }>;
};

type URLDiscoveryResult = {
    requested?: number;
    candidates?: number;
    rejected?: number;
    skipped?: number;
    urls?: string[];
    items?: Array<{ url?: string; host?: string; status?: string; reason?: string }>;
};

type URLDomainPolicy = {
    domain?: string;
    action?: string;
    reason?: string;
    updated_at?: string;
};

type ImportJob = {
    id?: string;
    status?: string;
    error?: string;
    result?: ImportResult;
};

type ImportBatch = {
    id?: string;
    root_path?: string;
    status?: string;
    total_files?: number;
    queued_files?: number;
    imported_files?: number;
    skipped_files?: number;
    failed_files?: number;
    updated_at?: string;
};

type ImportItem = {
    id?: string;
    batch_id?: string;
    source_id?: string;
    file_path?: string;
    relative_path?: string;
    file_hash?: string;
    file_size?: number;
    kind?: string;
    status?: string;
    error_message?: string;
    updated_at?: string;
};

type SearchResult = {
    source?: Source;
    result_type?: string;
    node_id?: string;
    node_title?: string;
    node_type?: string;
    page?: number;
    sheet_name?: string;
    row_range?: string;
    col_range?: string;
    citation?: string;
    card_id?: string;
    card_title?: string;
    fact_id?: string;
    subject?: string;
    predicate?: string;
    object?: string;
    claim?: string;
    summary?: string;
    snippet?: string;
    score?: number;
};

type Citation = {
    label?: string;
    source_title?: string;
    source_kind?: string;
    uri?: string;
    relative_path?: string;
    result_type?: string;
    snippet?: string;
};

type ExplainResult = {
    query?: string;
    count?: number;
    results?: SearchResult[];
    citations?: Citation[];
    notes?: string[];
};

type SearchFacetBucket = {
    label?: string;
    kind?: string;
    count?: number;
    source_id?: string;
    source_kind?: string;
    domain?: string;
    examples?: string[];
};

type SearchFacetsResult = {
    query?: string;
    count?: number;
    result_types?: SearchFacetBucket[];
    source_kinds?: SearchFacetBucket[];
    domains?: SearchFacetBucket[];
    labels?: SearchFacetBucket[];
    sources?: SearchFacetBucket[];
    entities?: SearchFacetBucket[];
    predicates?: SearchFacetBucket[];
    notes?: string[];
};

type TopicRelevanceSource = {
    source?: Source;
    score?: number;
    matched_terms?: string[];
    label_matches?: string[];
    source_hits?: number;
    card_hits?: number;
    fact_hits?: number;
    node_hits?: number;
};

type TopicRelevanceReport = {
    topic_hint?: string;
    query?: string;
    terms?: string[];
    count?: number;
    sources?: TopicRelevanceSource[];
    notes?: string[];
};

type SourceGraphNode = {
    id?: string;
    label?: string;
    kind?: string;
    status?: string;
    topic_hint?: string;
    project_path?: string;
    labels?: string[];
    node_count?: number;
    card_count?: number;
    fact_count?: number;
    degree?: number;
    component_id?: number;
    source_trust?: number;
    relative_path?: string;
    uri?: string;
};

type SourceGraphEdge = {
    id?: string;
    source_id?: string;
    related_source_id?: string;
    relation?: string;
    score?: number;
    terms?: string[];
    evidence?: string[];
};

type SourceGraphComponent = {
    id?: number;
    count?: number;
    edge_count?: number;
    density?: number;
    average_degree?: number;
    top_node_ids?: string[];
    top_labels?: string[];
    terms?: string[];
    isolated?: boolean;
};

type SourceGraphResult = {
    count?: number;
    edge_count?: number;
    focus_source_id?: string;
    depth?: number;
    component_count?: number;
    largest_component_size?: number;
    density?: number;
    nodes?: SourceGraphNode[];
    edges?: SourceGraphEdge[];
    components?: SourceGraphComponent[];
    isolates?: SourceGraphNode[];
    notes?: string[];
};

type SourcePathStep = {
    from_source_id?: string;
    to_source_id?: string;
    relation?: string;
    score?: number;
    terms?: string[];
    evidence?: string[];
};

type SourcePathResult = {
    from_source_id?: string;
    to_source_id?: string;
    found?: boolean;
    hop_count?: number;
    max_depth?: number;
    visited_count?: number;
    searched_edge_count?: number;
    truncated?: boolean;
    nodes?: SourceGraphNode[];
    steps?: SourcePathStep[];
    notes?: string[];
};

type KnowledgeSuggestion = {
    label?: string;
    kind?: string;
    count?: number;
    source_id?: string;
    source_kind?: string;
    domain?: string;
    source_label?: string;
    uri?: string;
    examples?: string[];
};

type KnowledgeSuggestResult = {
    query?: string;
    count?: number;
    items?: KnowledgeSuggestion[];
    notes?: string[];
};

type SearchFilterOverrides = {
    resultType?: string;
    sourceKind?: string;
    domain?: string;
    sourceID?: string;
    labels?: string;
};

type ContextPackItem = {
    label?: string;
    result_type?: string;
    title?: string;
    text?: string;
    source_id?: string;
    citation?: string;
    score?: number;
};

type ContextPackResult = {
    query?: string;
    count?: number;
    character_count?: number;
    items?: ContextPackItem[];
    citations?: Citation[];
    notes?: string[];
};

const sourceKindOptions = ['url', 'pdf', 'pptx', 'docx', 'xlsx', 'csv', 'markdown', 'text', 'conversation', 'workflow_artifact', 'doc', 'xls'];
const sourceStatusOptions = ['pending', 'parsed', 'distilled', 'failed', 'stale', 'disabled'];
const sourceCoverageOptions = ['missing_nodes', 'missing_cards', 'missing_facts', 'missing_links', 'missing_labels', 'pdf_ocr_needed', 'complete', 'has_nodes', 'has_cards', 'has_facts', 'has_links'];
const refreshableSourceKinds = new Set(['url', 'html', 'pdf', 'docx', 'xlsx', 'csv', 'markdown', 'text', 'doc', 'xls']);
const preferredCoverageAliasOrder = ['rebuildcards', 'rebuild_cards', 'rebuildfacts', 'rebuild_facts', 'needsocr', 'needs_ocr', 'haslinks', 'linked', 'missinglabels', 'unlabeled'];

export function normalizeKnowledgeCoverageOption(value: string) {
    return value.trim().toLowerCase().replace(/[-_./]+/g, ' ').split(/\s+/).filter(Boolean).join('_');
}

function normalizedKnowledgeCoverageAliases(capabilities: KnowledgeCapabilitiesResult | null | undefined) {
    const aliases = capabilities?.coverage_aliases;
    const normalizedAliases = new Map<string, string>();
    if (!aliases || typeof aliases !== 'object') return normalizedAliases;
    Object.entries(aliases)
        .filter(([alias, canonical]) => alias.trim() && typeof canonical === 'string' && canonical.trim())
        .forEach(([alias, canonical]) => {
            const normalizedAlias = normalizeKnowledgeCoverageOption(alias);
            const normalizedCanonical = normalizeKnowledgeCoverageOption(canonical);
            normalizedAliases.set(normalizedAlias, normalizedCanonical);
            normalizedAliases.set(normalizedAlias.replaceAll('_', ''), normalizedCanonical);
        });
    return normalizedAliases;
}

function displayKnowledgeCoverageAliases(capabilities: KnowledgeCapabilitiesResult | null | undefined) {
    const aliases = capabilities?.coverage_aliases;
    const normalizedAliases = new Map<string, string>();
    if (!aliases || typeof aliases !== 'object') return normalizedAliases;
    Object.entries(aliases)
        .filter(([alias, canonical]) => alias.trim() && typeof canonical === 'string' && canonical.trim())
        .forEach(([alias, canonical]) => normalizedAliases.set(normalizeKnowledgeCoverageOption(alias), normalizeKnowledgeCoverageOption(canonical)));
    return normalizedAliases;
}

export function resolveKnowledgeCoverageOption(capabilities: KnowledgeCapabilitiesResult | null | undefined, value: string) {
    const normalized = normalizeKnowledgeCoverageOption(value);
    if (!normalized) return '';
    const aliases = normalizedKnowledgeCoverageAliases(capabilities);
    return aliases.get(normalized) || aliases.get(normalized.replaceAll('_', '')) || normalized;
}

export function knowledgeSourceCoverageOptions(capabilities: KnowledgeCapabilitiesResult | null | undefined, selectedCoverage = 'all') {
    const capabilityFilters = Array.isArray(capabilities?.coverage_filters)
        ? capabilities.coverage_filters
            .filter(item => typeof item === 'string' && item.trim())
            .map(item => resolveKnowledgeCoverageOption(capabilities, item))
        : [];
    const base = capabilityFilters.length ? capabilityFilters : sourceCoverageOptions;
    const values = new Set(base);
    const selected = resolveKnowledgeCoverageOption(capabilities, selectedCoverage);
    if (selected !== 'all' && selected) values.add(selected);
    return Array.from(values);
}

export function knowledgeSourceCoverageStateValue(capabilities: KnowledgeCapabilitiesResult | null | undefined, selectedCoverage: string) {
    return resolveKnowledgeCoverageOption(capabilities, selectedCoverage || 'all') || 'all';
}

export function knowledgeCoverageFilterSummary(capabilities: KnowledgeCapabilitiesResult | null | undefined) {
    if (!Array.isArray(capabilities?.coverage_filters)) return [];
    const values = new Set(
        capabilities.coverage_filters
            .filter(item => typeof item === 'string' && item.trim())
            .map(item => resolveKnowledgeCoverageOption(capabilities, item)),
    );
    return Array.from(values);
}

export function knowledgeCoverageAliasSummary(capabilities: KnowledgeCapabilitiesResult | null | undefined, limit = 8) {
    const aliases = capabilities?.coverage_aliases;
    if (!aliases || typeof aliases !== 'object') return [];
    const preferredRank = new Map(preferredCoverageAliasOrder.map((alias, index) => [alias, index]));
    const normalizedAliases = displayKnowledgeCoverageAliases(capabilities);
    return Array.from(normalizedAliases.entries())
        .sort(([left], [right]) => {
            const leftRank = preferredRank.get(left) ?? Number.MAX_SAFE_INTEGER;
            const rightRank = preferredRank.get(right) ?? Number.MAX_SAFE_INTEGER;
            return leftRank - rightRank || left.localeCompare(right);
        })
        .slice(0, Math.max(0, limit))
        .map(([alias, canonical]) => `${alias} -> ${canonical}`);
}

export function normalizeKnowledgeSourceLimit(limit: number | undefined, fallback = 100, max = 5000) {
    const fallbackLimit = Math.max(1, Math.floor(fallback));
    const maxLimit = Math.max(1, Math.floor(max));
    if (!Number.isFinite(limit) || Number(limit) <= 0) return Math.min(fallbackLimit, maxLimit);
    return Math.min(Math.floor(Number(limit)), maxLimit);
}

export function normalizeKnowledgeFilterToken(value?: string) {
    return (value || '').trim().toLowerCase();
}

function normalizeKnowledgeDomainHost(value: string) {
    return value.toLowerCase().replace(/^\*\./, '').replace(/\.+$/, '');
}

export function normalizeKnowledgeDomainFilter(value?: string) {
    const raw = (value || '').trim();
    if (!raw) return '';
    try {
        const parsed = normalizeKnowledgeDomainHost(new URL(raw).hostname);
        if (parsed) return parsed;
    } catch {
        // Try protocol-less URL-like filters below.
    }
    try {
        const parsed = normalizeKnowledgeDomainHost(new URL(`https://${raw}`).hostname);
        if (parsed) return parsed;
    } catch {
        // Fall back to plain domain-like text below.
    }
    return normalizeKnowledgeDomainHost(raw);
}

export function applyKnowledgeDomainFilterPayload(payload: any, domainValue?: string) {
    const domain = normalizeKnowledgeDomainFilter(domainValue);
    if (domain) payload.domain = domain;
}

export function applyKnowledgeSearchFilterPayload(payload: any, filters: {
    resultType?: string;
    sourceKind?: string;
    domain?: string;
    sourceID?: string;
    labels?: string;
} = {}) {
    const resultType = normalizeKnowledgeFilterToken(filters.resultType);
    if (resultType && resultType !== 'all') payload.result_types = [resultType];
    const sourceKind = normalizeKnowledgeFilterToken(filters.sourceKind);
    if (sourceKind && sourceKind !== 'all') payload.source_kinds = [sourceKind];
    applyKnowledgeDomainFilterPayload(payload, filters.domain);
    const sourceID = (filters.sourceID || '').trim();
    if (sourceID) payload.source_ids = [sourceID];
    const labels = parseLabelList(filters.labels || '');
    if (labels.length) payload.labels = labels;
}

export function knowledgeSourceListPayload(capabilities: KnowledgeCapabilitiesResult | null | undefined, filter: {
    query?: string;
    kind?: string;
    status?: string;
    coverage?: string;
    domain?: string;
    labels?: string;
    limit?: number;
}) {
    const payload: any = { limit: normalizeKnowledgeSourceLimit(filter.limit) };
    const query = (filter.query || '').trim();
    if (query) payload.query = query;
    const kind = normalizeKnowledgeFilterToken(filter.kind);
    if (kind && kind !== 'all') payload.kind = kind;
    const status = normalizeKnowledgeFilterToken(filter.status);
    if (status && status !== 'all') payload.status = status;
    const coverageFilter = resolveKnowledgeCoverageOption(capabilities, filter.coverage || 'all');
    if (coverageFilter !== 'all' && coverageFilter) payload.coverage_filter = coverageFilter;
    applyKnowledgeDomainFilterPayload(payload, filter.domain);
    const labels = parseLabelList(filter.labels || '');
    if (labels.length) payload.labels = labels;
    return payload;
}

function topCounts(value: any, limit: number) {
    if (!value || typeof value !== 'object') return [];
    return Object.entries(value)
        .map(([key, count]) => `${key} ${Number(count || 0)}`)
        .sort((left, right) => {
            const leftCount = Number(left.split(' ').pop() || 0);
            const rightCount = Number(right.split(' ').pop() || 0);
            return rightCount - leftCount || left.localeCompare(right);
        })
        .slice(0, limit);
}

export function knowledgeHealthSummaryModel(health: any) {
    if (!health || typeof health !== 'object') return null;
    const score = Number(health.score || 0);
    const qualityAvgScore = Number(health.quality_avg_score || 0);
    const actions = Array.isArray(health.maintenance_actions) ? health.maintenance_actions : [];
    return {
        status: String(health.status || 'unknown'),
        score: Number.isFinite(score) ? score : 0,
        doctorScore: health.doctor_score ?? 0,
        qualityAvgScore: Number.isFinite(qualityAvgScore) ? qualityAvgScore : 0,
        gradeEntries: topCounts(health.quality_grades, 4),
        signalEntries: topCounts(health.quality_signals, 5),
        findingEntries: topCounts(health.doctor_findings, 4),
        actions: actions.slice(0, 5),
        actionEntries: actions.slice(0, 5).map((item: any) => `${item.kind || item.tool || 'action'} ${Number(item.count || 0)}`),
    };
}

export function knowledgeHealthActionExecutionPayload(baseFilter: any, action: any, options: {
    policy?: string;
    dryRun?: boolean;
    distillMode?: string;
    maxSourcesPerAction?: number;
    allowSensitiveDisable?: boolean;
    allowDuplicateSuppression?: boolean;
} = {}) {
    const kind = String(action?.kind || '').trim();
    if (!kind) return null;
    const filter = { ...(baseFilter || {}) };
    const actionSourceIDs = Array.isArray(action?.source_ids)
        ? Array.from(new Set(action.source_ids.map((id: unknown) => String(id || '').trim()).filter(Boolean)))
        : [];
    const sourceCount = actionSourceIDs.length;
    if (sourceCount > 0) {
        filter.source_ids = actionSourceIDs;
        filter.limit = Math.max(normalizeKnowledgeSourceLimit(filter.limit, sourceCount, 5000), sourceCount);
    }
    const maxSourcesPerAction = Math.max(normalizeKnowledgeSourceLimit(options.maxSourcesPerAction, 100, 5000), sourceCount);
    return {
        filter,
        policy: options.policy || '',
        actions: [kind],
        dry_run: options.dryRun !== false,
        distill_mode: options.distillMode || '',
        max_sources_per_action: maxSourcesPerAction,
        allow_sensitive_disable: !!options.allowSensitiveDisable,
        allow_duplicate_suppression: !!options.allowDuplicateSuppression,
    };
}

export function knowledgeQualityExecutionContextLabel(context: { source?: string; action?: string; dryRun?: boolean } | null | undefined) {
    if (!context) return '';
    const source = String(context.source || '').trim();
    const action = String(context.action || '').trim();
    const mode = context.dryRun ? 'preview' : 'run';
    return [source, action, mode].filter(Boolean).join(' / ');
}

export function knowledgeExecutionResultSourceIDs(result: any, limit = 5000) {
    if (!result || typeof result !== 'object') return [];
    const seen = new Set<string>();
    const ids: string[] = [];
    const append = (value: unknown) => {
        const id = String(value || '').trim();
        if (!id || seen.has(id) || ids.length >= limit) return;
        seen.add(id);
        ids.push(id);
    };
    const appendFromObjects = (values: unknown) => {
        if (!Array.isArray(values)) return;
        values.forEach((item: any) => append(item?.source_id || item?.sourceID || item?.id));
    };
    if (Array.isArray(result.source_ids)) {
        result.source_ids.forEach(append);
    }
    append(result.source_id || result.sourceID);
    appendFromObjects(result.sources);
    appendFromObjects(result.failures);
    appendFromObjects(result.previews);
    if (Array.isArray(result.results)) {
        result.results.forEach((item: any) => {
            append(item?.source_id || item?.sourceID);
            if (Array.isArray(item?.source_ids)) item.source_ids.forEach(append);
            appendFromObjects(item?.sources);
            appendFromObjects(item?.failures);
            appendFromObjects(item?.previews);
            append(item?.result?.source_id || item?.result?.sourceID);
            if (Array.isArray(item?.result?.source_ids)) item.result.source_ids.forEach(append);
            appendFromObjects(item?.result?.sources);
            appendFromObjects(item?.result?.failures);
            appendFromObjects(item?.result?.previews);
        });
    }
    return ids;
}

export function knowledgeExecutionActionSourceIDs(action: any, limit = 12) {
    if (!action || typeof action !== 'object') return [];
    return knowledgeExecutionResultSourceIDs({ results: [action] }, limit);
}

export function knowledgeExecutionFailureDetails(result: any, limit = 50) {
    if (!result || typeof result !== 'object') return [];
    const failures: Array<{ action: string; sourceID: string; error: string }> = [];
    const seen = new Set<string>();
    const append = (action: string, sourceID: unknown, error: unknown) => {
        if (failures.length >= limit) return;
        const message = String(error || '').trim();
        if (!message) return;
        const safeAction = String(action || '').trim();
        const safeSourceID = String(sourceID || '').trim();
        const key = `${safeAction}\u0000${safeSourceID}\u0000${message}`;
        if (seen.has(key)) return;
        seen.add(key);
        failures.push({
            action: safeAction,
            sourceID: safeSourceID,
            error: message,
        });
    };
    const collectNestedFailures = (action: string, value: any) => {
        if (!value || typeof value !== 'object') return;
        const nested = Array.isArray(value.failures) ? value.failures : [];
        nested.forEach((failure: any) => append(action, failure?.source_id || failure?.sourceID || failure?.id, failure?.error || failure?.message));
    };
    const collectPreviewErrors = (action: string, value: any) => {
        if (!value || typeof value !== 'object') return;
        const previews = Array.isArray(value.previews) ? value.previews : [];
        previews.forEach((preview: any) => append(action, preview?.source_id || preview?.sourceID || preview?.id, preview?.error || preview?.message));
    };
    const appendErrorForSources = (action: string, value: any, error: unknown) => {
        if (!value || typeof value !== 'object') {
            append(action, '', error);
            return;
        }
        const directID = value.source_id || value.sourceID;
        if (directID) {
            append(action, directID, error);
            return;
        }
        if (Array.isArray(value.source_ids) && value.source_ids.length > 0) {
            value.source_ids.forEach((sourceID: unknown) => append(action, sourceID, error));
            return;
        }
        append(action, '', error);
    };
    if (Array.isArray(result.failures)) {
        result.failures.forEach((failure: any) => append('', failure?.source_id || failure?.sourceID || failure?.id, failure?.error || failure?.message));
    }
    collectPreviewErrors('', result);
    if (result.error) appendErrorForSources('', result, result.error);
    if (Array.isArray(result.results)) {
        result.results.forEach((item: any) => {
            const action = String(item?.kind || '').trim();
            collectNestedFailures(action, item);
            collectNestedFailures(action, item?.result);
            collectPreviewErrors(action, item);
            collectPreviewErrors(action, item?.result);
            if (item?.result?.error) appendErrorForSources(action, item.result, item.result.error);
            if (item?.error) appendErrorForSources(action, item, item.error);
        });
    }
    return failures;
}

export function knowledgeExecutionSourceFilterLabel(context: string, count: number) {
    const safeCount = Math.max(0, Math.floor(Number(count) || 0));
    const safeContext = String(context || '').trim() || 'execution';
    return `${safeContext} / sources ${safeCount}`;
}

export function knowledgeHealthActionConfirmMessage(action: any) {
    const title = String(action?.title || action?.kind || action?.tool || 'action').trim();
    const kind = String(action?.kind || action?.tool || 'action').trim();
    const count = Math.max(0, Math.floor(Number(action?.count || 0)));
    const signals = Array.isArray(action?.signals) ? action.signals.filter(Boolean).join(', ') : '';
    return [
        `Run knowledge health action: ${title || kind}?`,
        kind ? `Kind: ${kind}` : '',
        count > 0 ? `Affected sources: ${count}` : '',
        signals ? `Signals: ${signals}` : '',
    ].filter(Boolean).join('\n');
}

export function knowledgeHealthActionExecutable(action: any) {
    if (action?.executable === false) return false;
    const kind = String(action?.kind || action?.tool || '').trim();
    return !!kind;
}

export function knowledgeHealthActionManualLabel(action: any) {
    const reason = String(action?.manual_reason || '').trim();
    return reason ? `Manual: ${reason}` : 'Manual';
}

type KnowledgeSubTab = 'overview' | 'ingest' | 'search' | 'sources' | 'quality';


export function KnowledgeSettingsPanel({ lang }: Props) {
    const t = (en: string, zhHans: string, zhHant: string = zhHans) => (
        lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en
    );
    const [activeTab, setActiveTab] = useState<KnowledgeSubTab>('overview');
    const [health, setHealth] = useState<any>(null);
    const [capabilities, setCapabilities] = useState<KnowledgeCapabilitiesResult | null>(null);
    const [sources, setSources] = useState<Source[] | null>(null);
    const [searchResults, setSearchResults] = useState<SearchResult[]>([]);
    const [facets, setFacets] = useState<SearchFacetsResult | null>(null);
    const [qualityReport, setQualityReport] = useState<SourceQualityReport | null>(null);
    const [qualityPlan, setQualityPlan] = useState<SourceQualityMaintenancePlan | null>(null);
    const [executionResult, setExecutionResult] = useState<SourceQualityMaintenanceExecuteResult | null>(null);
    const [executionContext, setExecutionContext] = useState<{ source?: string; action?: string; dryRun?: boolean } | null>(null);
    const [importJob, setImportJob] = useState<ImportJob | null>(null);
    const [operationResult, setOperationResult] = useState<any>(null);
    const [successMessage, setSuccessMessage] = useState('');
    const [error, setError] = useState('');
    const [loading, setLoading] = useState(false);
    const [busy, setBusy] = useState('');
    const [textForm, setTextForm] = useState({ title: '', text: '', labels: '', topicHint: '', saveScope: 'project', distillMode: '' });
    const [urlForm, setURLForm] = useState({ urls: '', labels: '', topicHint: '', saveScope: 'project', distillMode: '', autoLabels: true });
    const [fileForm, setFileForm] = useState({ directory: '', labels: '', topicHint: '', saveScope: 'project', distillMode: '', includeExts: '', excludeGlobs: '', recursive: true, autoLabels: true, dryRun: false, maxFileBytes: 10485760 });
    const [selectedFiles, setSelectedFiles] = useState<string[]>([]);
    const [searchForm, setSearchForm] = useState({ query: '', resultType: 'all', sourceKind: 'all', domain: '', sourceID: '', labels: '', limit: 20, includeDisabled: false });
    const [sourceFilter, setSourceFilter] = useState({ query: '', kind: 'all', status: 'all', coverage: 'all', domain: '', labels: '', limit: 100 });
    const [qualityFilter, setQualityFilter] = useState({ query: '', kind: 'all', status: 'all', coverage: 'all', domain: '', labels: '', limit: 100 });
    const [qualityOptions, setQualityOptions] = useState({ policy: 'balanced', dryRun: true, distillMode: '', maxSourcesPerAction: 100, allowSensitiveDisable: false, allowDuplicateSuppression: true });
    const [showImportDialog, setShowImportDialog] = useState(false);
    const [confirmDialog, setConfirmDialog] = useState<{ show: boolean; title: string; message: string; onConfirm: () => void }>({ show: false, title: '', message: '', onConfirm: () => {} });
    const [deepCrawlBusy, setDeepCrawlBusy] = useState(false);

    const confirmT = (key: string) => {
        const isZh = lang?.startsWith('zh') ?? false;
        const map: Record<string, string> = { cancel: isZh ? '取消' : 'Cancel', confirm: isZh ? '确认' : 'Confirm' };
        return map[key] || key;
    };

    /** Map camelCase DeepCrawlConfig to snake_case Go struct for Wails bindings */
    const mapConfigToRequest = useCallback((config: DeepCrawlConfig) => ({
        seed_url: config.seedURL,
        max_depth: config.maxDepth,
        same_domain_only: config.sameDomainOnly,
        save_scope: config.saveScope || '',
        topic_hint: config.topicHint || '',
        labels: config.labels || [],
    }), []);

    const handleDeepCrawlPreview = useCallback(async (config: DeepCrawlConfig): Promise<DeepCrawlPreviewResult | void> => {
        setDeepCrawlBusy(true);
        setError('');
        try {
            const result = await KnowledgeDeepCrawlPreview(mapConfigToRequest(config));
            if (result && result.by_depth) {
                return result as DeepCrawlPreviewResult;
            }
        } catch (err: any) {
            setError(err?.message || String(err));
        } finally {
            setDeepCrawlBusy(false);
        }
    }, [mapConfigToRequest]);

    const handleDeepCrawlStart = useCallback(async (config: DeepCrawlConfig): Promise<void> => {
        setDeepCrawlBusy(true);
        setError('');
        try {
            await KnowledgeDeepCrawl(mapConfigToRequest(config));
        } catch (err: any) {
            setError(err?.message || String(err));
        } finally {
            setDeepCrawlBusy(false);
        }
    }, [mapConfigToRequest]);

    const refresh = async () => {
        setLoading(true);
        setError('');
        try {
            const [healthResult, capabilityResult] = await Promise.all([
                KnowledgeHealth({}),
                KnowledgeCapabilities(),
            ]);
            setHealth(healthResult || null);
            setCapabilities(capabilityResult || null);
        } catch (err: any) {
            setError(err?.message || String(err));
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        void refresh();
    }, []);

    useEffect(() => {
        if (!successMessage) return;
        const timer = setTimeout(() => setSuccessMessage(''), 6000);
        return () => clearTimeout(timer);
    }, [successMessage]);

    const summary = knowledgeHealthSummaryModel(health);
    const sourcePayload = useMemo(() => knowledgeSourceListPayload(capabilities, sourceFilter), [capabilities, sourceFilter]);
    // Invalidate stale results when the query payload changes (filter or capabilities update).
    // sourcePayload is the direct input to KnowledgeListSources — sources is its cached output.
    useEffect(() => { setSources(null); }, [sourcePayload]);
    const sourcePayloadRef = useRef(sourcePayload);
    sourcePayloadRef.current = sourcePayload;
    const qualityPayload = useMemo(() => knowledgeSourceListPayload(capabilities, qualityFilter), [capabilities, qualityFilter]);
    const coverageOptions = useMemo(() => knowledgeSourceCoverageOptions(capabilities, sourceFilter.coverage), [capabilities, sourceFilter.coverage]);
    const qualityCoverageOptions = useMemo(() => knowledgeSourceCoverageOptions(capabilities, qualityFilter.coverage), [capabilities, qualityFilter.coverage]);
    const distillModes = capabilities?.distill_modes?.length ? capabilities.distill_modes : ['rules_only', 'llm_optional'];
    const formatSummary = (capabilities?.formats || []).slice(0, 6).map(format => `${format.kind || 'format'}${format.extensions?.length ? ` (${format.extensions.join(', ')})` : ''}`);
    const aliasSummary = knowledgeCoverageAliasSummary(capabilities, 4);

    const refreshSourceList = async () => {
        const requestPayload = sourcePayload;
        const result = await KnowledgeListSources(requestPayload);
        // Discard stale response if filter changed during the request.
        if (sourcePayloadRef.current !== requestPayload) return [];
        const nextSources = Array.isArray(result) ? result : [];
        setSources(nextSources);
        return nextSources;
    };

    const runTask = async (name: string, task: () => Promise<any>, options: { refreshSources?: boolean; refreshHealth?: boolean } = {}) => {
        setBusy(name);
        setError('');
        setSuccessMessage('');
        if (name !== 'executeQuality') {
            setExecutionResult(null);
            setExecutionContext(null);
        }
        try {
            const result = await task();
            setOperationResult(result ?? { ok: true });
            if (options.refreshSources) await refreshSourceList();
            if (options.refreshHealth) await refresh();
            return result;
        } catch (err: any) {
            setError(err?.message || String(err));
            return null;
        } finally {
            setBusy('');
        }
    };

    const importPayload = () => ({
        root_path: fileForm.directory.trim(),
        topic_hint: fileForm.topicHint.trim(),
        save_scope: fileForm.saveScope,
        distill_mode: fileForm.distillMode,
        labels: parseLabelList(fileForm.labels),
        auto_labels: fileForm.autoLabels,
        recursive: fileForm.recursive,
        include_exts: parseLabelList(fileForm.includeExts).map(ext => ext.startsWith('.') ? ext : `.${ext}`),
        exclude_globs: parseLabelList(fileForm.excludeGlobs),
        max_file_bytes: normalizeKnowledgeSourceLimit(fileForm.maxFileBytes, 10485760, 1024 * 1024 * 1024),
        dry_run: fileForm.dryRun,
    });

    const loadSources = async () => {
        await runTask('sources', async () => {
            const nextSources = await refreshSourceList();
            return { count: nextSources.length };
        });
    };

    const saveText = async () => {
        if (!textForm.text.trim()) {
            setError(t('Text is required.', '请输入文本内容。'));
            return;
        }
        const result = await runTask('saveText', () => KnowledgeSaveText({
            text: textForm.text,
            title: textForm.title.trim(),
            kind: 'text',
            topic_hint: textForm.topicHint.trim(),
            save_scope: textForm.saveScope,
            distill_mode: textForm.distillMode,
            labels: parseLabelList(textForm.labels),
            auto_labels: true,
        }), { refreshSources: true, refreshHealth: true });
        if (result) {
            if (result.save_status === 'duplicate') {
                setSuccessMessage(t('⚠️ Content already exists in knowledge base (updated).', '⚠️ 内容已存在于知识库中（已更新）。'));
            } else {
                setSuccessMessage(t('✅ Text saved to knowledge base successfully.', '✅ 文本已成功保存到知识库。'));
            }
        }
    };

    const saveURLs = async () => {
        const urls = parseURLBatch(urlForm.urls);
        if (!urls.length) {
            setError(t('At least one URL is required.', '请输入至少一个 URL。'));
            return;
        }
        const result = await runTask('saveURLs', () => urls.length === 1
            ? KnowledgeSaveURL(urls[0], urlForm.saveScope, urlForm.topicHint.trim(), urlForm.distillMode, parseLabelList(urlForm.labels), urlForm.autoLabels)
            : KnowledgeSaveURLs(urls, urlForm.saveScope, urlForm.topicHint.trim(), urlForm.distillMode, parseLabelList(urlForm.labels), urlForm.autoLabels), { refreshSources: true, refreshHealth: true });
        if (result) {
            if (urls.length === 1) {
                // Single URL: result is a Source with save_status
                if (result.save_status === 'duplicate') {
                    setSuccessMessage(t('⚠️ URL already exists in knowledge base (updated).', '⚠️ URL 已存在于知识库中（已更新）。'));
                } else {
                    setSuccessMessage(t('✅ URL saved to knowledge base successfully.', '✅ URL 已成功保存到知识库。'));
                }
            } else {
                // Batch URLs: result is URLBatchSaveResult with duplicates count
                const saved = result.saved || 0;
                const duplicates = result.duplicates || 0;
                const skipped = result.skipped || 0;
                const failed = result.failed || 0;
                const fresh = saved - duplicates;
                let msg = '';
                if (saved === 0 && skipped > 0) {
                    msg = t(`⚠️ All ${skipped} URL(s) were duplicates in this batch (skipped).`, `⚠️ 本批次中全部 ${skipped} 个 URL 重复（已跳过）。`);
                } else if (duplicates > 0 && fresh === 0) {
                    msg = t(`⚠️ All ${duplicates} URL(s) already exist in knowledge base (updated).`, `⚠️ 全部 ${duplicates} 个 URL 已存在于知识库中（已更新）。`);
                } else if (duplicates > 0) {
                    msg = t(`✅ ${fresh} URL(s) saved, ${duplicates} already existed (updated).`, `✅ ${fresh} 个 URL 已保存，${duplicates} 个已存在（已更新）。`);
                } else {
                    msg = t(`✅ ${saved} URL(s) saved to knowledge base successfully.`, `✅ ${saved} 个 URL 已成功保存到知识库。`);
                }
                if (failed > 0) {
                    msg += t(` ${failed} failed.`, ` ${failed} 个失败。`);
                }
                setSuccessMessage(msg);
            }
        }
    };

    const chooseFiles = async () => {
        await runTask('chooseFiles', async () => {
            const files = await SelectKnowledgeFiles();
            setSelectedFiles(Array.isArray(files) ? files : []);
            return { files: Array.isArray(files) ? files.length : 0 };
        });
    };

    const importFiles = async () => {
        if (!selectedFiles.length) {
            setError(t('Select files before importing.', '请先选择文件。'));
            return;
        }
        await runTask('importFiles', () => KnowledgeImportFiles(importPayload(), selectedFiles), { refreshSources: true, refreshHealth: true });
    };

    const chooseDirectory = async () => {
        await runTask('chooseDirectory', async () => {
            const directory = await SelectKnowledgeDirectory();
            if (directory) setFileForm(form => ({ ...form, directory }));
            return { directory };
        });
    };

    const startDirectoryImport = async () => {
        if (!fileForm.directory.trim()) {
            setError(t('Select a directory before importing.', '请先选择目录。'));
            return;
        }
        await runTask('importDirectory', async () => {
            const job = await KnowledgeStartImportDirectory(importPayload());
            setImportJob(job || null);
            return job;
        }, { refreshSources: true, refreshHealth: true });
    };

    const runSearch = async () => {
        if (!searchForm.query.trim()) {
            setError(t('Search query is required.', '请输入搜索问题。'));
            return;
        }
        await runTask('search', async () => {
            const payload: any = {
                query: searchForm.query.trim(),
                limit: normalizeKnowledgeSourceLimit(searchForm.limit, 20, 200),
                include_disabled: searchForm.includeDisabled,
            };
            applyKnowledgeSearchFilterPayload(payload, searchForm);
            const [results, facetResult] = await Promise.all([
                KnowledgeSearch(payload),
                KnowledgeSearchFacets(payload),
            ]);
            setSearchResults(Array.isArray(results) ? results : []);
            setFacets(facetResult || null);
            return { count: Array.isArray(results) ? results.length : 0 };
        });
    };

    const loadQuality = async () => {
        await runTask('quality', async () => {
            const [report, plan] = await Promise.all([
                KnowledgeSourceQualityReport(qualityPayload),
                KnowledgeSourceQualityMaintenancePlan(qualityPayload),
            ]);
            setQualityReport(report || null);
            setQualityPlan(plan || null);
            return { sources: report?.count || 0, actions: plan?.count || plan?.actions?.length || 0 };
        });
    };

    const executeQualityAction = async (action?: SourceQualityMaintenanceAction) => {
        const payload = action
            ? knowledgeHealthActionExecutionPayload(qualityPayload, action, qualityOptions)
            : {
                filter: qualityPayload,
                policy: qualityOptions.policy,
                dry_run: qualityOptions.dryRun,
                distill_mode: qualityOptions.distillMode,
                max_sources_per_action: normalizeKnowledgeSourceLimit(qualityOptions.maxSourcesPerAction, 100, 5000),
                allow_sensitive_disable: qualityOptions.allowSensitiveDisable,
                allow_duplicate_suppression: qualityOptions.allowDuplicateSuppression,
            };
        if (!payload) return;
        await runTask('executeQuality', async () => {
            const result = await KnowledgeExecuteSourceQualityMaintenancePlan(payload);
            setExecutionResult(result || null);
            setExecutionContext({ source: action ? 'quality_action' : 'quality_plan', action: action?.kind || 'all_actions', dryRun: qualityOptions.dryRun });
            return result;
        }, { refreshSources: true, refreshHealth: true });
    };

    const toggleSource = async (source: Source) => {
        if (!source.id) return;
        const isDisabled = String(source.status || '').toLowerCase() === 'disabled';
        await runTask(isDisabled ? 'enableSource' : 'disableSource', () => isDisabled ? KnowledgeEnableSource(source.id || '') : KnowledgeDisableSource(source.id || ''), { refreshSources: true, refreshHealth: true });
    };

    const deleteSource = async (source: Source) => {
        if (!source.id) return;
        setConfirmDialog({
            show: true,
            title: t('Delete Source', '删除来源'),
            message: t('Delete this knowledge source?', '确定删除这个知识来源？'),
            onConfirm: async () => {
                setConfirmDialog(prev => ({ ...prev, show: false }));
                await runTask('deleteSource', () => KnowledgeDeleteSource(source.id || ''), { refreshSources: true, refreshHealth: true });
            },
        });
    };

    const handleClearAll = async () => {
        setConfirmDialog({
            show: true,
            title: t('Clear All', '清除全部'),
            message: t('⚠️ This will permanently delete ALL knowledge base content (all imported documents, URLs, cards, facts). This cannot be undone.\n\nAre you sure?',
                '⚠️ 此操作将永久删除知识库中的所有内容（所有导入的文档、URL、卡片、事实），且无法恢复。\n\n确定要继续吗？'),
            onConfirm: () => {
                setConfirmDialog({
                    show: true,
                    title: t('Final Confirmation', '最终确认'),
                    message: t('⚠️ FINAL CONFIRMATION: All knowledge base data will be permanently erased. Proceed?',
                        '⚠️ 最终确认：知识库所有数据将被永久清除。确认执行？'),
                    onConfirm: async () => {
                        setConfirmDialog(prev => ({ ...prev, show: false }));
                        await runTask('clearAll', async () => {
                            await KnowledgeClearAll();
                            return { ok: true };
                        }, { refreshSources: true, refreshHealth: true });
                    },
                });
            },
        });
    };

    useEffect(() => {
        const id = String(importJob?.id || '').trim();
        const status = String(importJob?.status || '').toLowerCase();
        if (!id || !['queued', 'running', 'pending'].includes(status)) return;
        const handle = window.setInterval(() => {
            void KnowledgeImportJobStatus(id)
                .then(job => {
                    setImportJob(job || null);
                    if (job && !['queued', 'running', 'pending'].includes(String(job.status || '').toLowerCase())) {
                        setOperationResult(job);
                        void refreshSourceList();
                        void refresh();
                    }
                })
                .catch(err => setError(err?.message || String(err)));
        }, 1500);
        return () => window.clearInterval(handle);
    }, [importJob?.id, importJob?.status, sourcePayload]);

    return (
        <>
        <section style={panelStyle}>
            <div style={sectionHeaderStyle}>
                <div>
                    <h2 style={titleStyle}>{t('Knowledge Base', '知识库')}</h2>
                    <p style={subtleStyle}>{t('Ingest, search, inspect, and maintain local knowledge sources.', '导入、检索、查看并维护本地知识来源。')}</p>
                </div>
                <div style={{ display: 'flex', gap: 12, alignItems: 'center' }}>
                    <button type="button" style={buttonStyle} onClick={refresh} disabled={loading}>
                        {loading ? t('Refreshing...', '刷新中...') : t('Refresh', '刷新')}
                    </button>
                    <button type="button" style={dangerButtonStyle} onClick={handleClearAll} disabled={!!busy}>
                        {t('Clear All', '清空')}
                    </button>
                </div>
            </div>
            {error ? <div style={errorBoxStyle}>{error}</div> : null}
            {successMessage ? <div style={successBoxStyle}>{successMessage}</div> : null}
            <div style={tabsStyle}>
                {([
                    ['overview', t('Overview', '总览')],
                    ['ingest', t('Ingest', '导入')],
                    ['search', t('Search', '检索')],
                    ['sources', t('Sources', '来源')],
                    ['quality', t('Quality', '质量')],
                ] as Array<[KnowledgeSubTab, string]>).map(([tab, label]) => (
                    <button key={tab} type="button" onClick={() => setActiveTab(tab)} style={tabButtonStyle(activeTab === tab)}>{label}</button>
                ))}
            </div>

            {activeTab === 'overview' && (
                <div style={stackStyle}>
                    <div style={statsGridStyle}>
                        <Stat label={t('Status', '状态')} value={summary?.status || 'Unknown'} />
                        <Stat label={t('Score', '评分')} value={summary?.score ?? 0} />
                        <Stat label={t('Quality', '质量')} value={summary?.qualityAvgScore ?? 0} />
                        <Stat label={t('Actions', '动作')} value={summary?.actions?.length ?? 0} />
                    </div>
                    <div style={twoColumnStyle}>
                        <PanelBlock title={t('Health Signals', '健康信号')}>
                            <KeyValueList values={[...(summary?.gradeEntries || []), ...(summary?.signalEntries || []), ...(summary?.findingEntries || [])]} empty={t('No health signals.', '暂无健康信号。')} />
                        </PanelBlock>
                        <PanelBlock title={t('Capabilities', '能力')}>
                            <KeyValueList values={[
                                capabilities?.storage_backend ? `storage ${capabilities.storage_backend}` : '',
                                capabilities?.search_backend ? `search ${capabilities.search_backend}` : '',
                                ...(formatSummary.length ? formatSummary : []),
                                ...(aliasSummary.length ? aliasSummary : []),
                            ]} empty={t('Capabilities are not loaded yet.', '能力信息尚未加载。')} />
                        </PanelBlock>
                    </div>
                    <PanelBlock title={t('Recommended Maintenance', '建议维护')}>
                        {(summary?.actions || []).length ? (
                            <div style={listStyle}>
                                {(summary?.actions || []).map((action: any, index: number) => (
                                    <div key={`${action.kind || action.tool || index}`} style={rowStyle}>
                                        <div style={rowMainStyle}>
                                            <strong>{action.title || action.kind || action.tool || t('Action', '动作')}</strong>
                                            <span style={mutedLineStyle}>{action.description || [action.kind || action.tool, action.count ? `${action.count} sources` : ''].filter(Boolean).join(' · ')}</span>
                                        </div>
                                        {knowledgeHealthActionExecutable(action) ? (
                                            <button type="button" style={buttonStyle} disabled={!!busy} onClick={() => executeQualityAction(action)}>{t('Preview', '预览')}</button>
                                        ) : <span style={badgeStyle}>{knowledgeHealthActionManualLabel(action)}</span>}
                                    </div>
                                ))}
                            </div>
                        ) : <div style={emptyStyle}>{t('No maintenance action is required.', '暂无需要执行的维护动作。')}</div>}
                    </PanelBlock>
                </div>
            )}

            {activeTab === 'ingest' && (
                <>
                <PanelBlock title={t('Import Documents', '导入文档')}>
                    <div style={documentImportStyle}>
                        <div style={documentImportCopyStyle}>
                            <strong>{t('Add local documents', '添加本地文档')}</strong>
                            <span style={mutedLineStyle}>{t('Import files or directories into your knowledge base', '将文件或目录导入到知识库中')}</span>
                        </div>
                        <button type="button" style={documentImportButtonStyle} onClick={() => setShowImportDialog(true)}>
                            {t('Import Documents', '导入文档')}
                        </button>
                    </div>
                    {importJob && ['running', 'queued', 'pending'].includes(String(importJob.status || '').toLowerCase()) && (
                        <ImportJobSummary t={t} job={importJob} />
                    )}
                </PanelBlock>
                <div style={twoColumnStyle}>
                    <PanelBlock title={t('Save Text', '保存文本')}>
                        <input style={inputStyle} value={textForm.title} onChange={event => setTextForm({ ...textForm, title: event.target.value })} placeholder={t('Title', '标题')} />
                        <textarea style={textareaStyle} value={textForm.text} onChange={event => setTextForm({ ...textForm, text: event.target.value })} placeholder={t('Paste text or notes', '粘贴文本或笔记')} />
                        <MetadataControls t={t} labels={textForm.labels} topicHint={textForm.topicHint} saveScope={textForm.saveScope} distillMode={textForm.distillMode} distillModes={distillModes} onChange={patch => setTextForm({ ...textForm, ...patch })} />
                        <button type="button" style={primaryButtonStyle} disabled={!!busy} onClick={saveText}>{busy === 'saveText' ? t('Saving...', '保存中...') : t('Save Text', '保存文本')}</button>
                    </PanelBlock>
                    <PanelBlock title={t('Save URLs', '保存 URL')}>
                        <textarea style={textareaStyle} value={urlForm.urls} onChange={event => setURLForm({ ...urlForm, urls: event.target.value })} placeholder={t('One or more URLs, separated by line breaks', '一个或多个 URL，可换行分隔')} />
                        <MetadataControls t={t} labels={urlForm.labels} topicHint={urlForm.topicHint} saveScope={urlForm.saveScope} distillMode={urlForm.distillMode} distillModes={distillModes} onChange={patch => setURLForm({ ...urlForm, ...patch })} />
                        <label style={checkboxStyle}><input type="checkbox" checked={urlForm.autoLabels} onChange={event => setURLForm({ ...urlForm, autoLabels: event.target.checked })} /> {t('Auto labels', '自动标签')}</label>
                        <button type="button" style={primaryButtonStyle} disabled={!!busy} onClick={saveURLs}>{busy === 'saveURLs' ? t('Saving...', '保存中...') : t('Save URLs', '保存 URL')}</button>
                    </PanelBlock>
                </div>
                <DeepCrawlPanel
                    lang={lang === 'zh-Hans' || lang === 'zh-Hant' ? undefined : 'en'}
                    onPreview={handleDeepCrawlPreview}
                    onStartCrawl={handleDeepCrawlStart}
                    busy={deepCrawlBusy}
                />
                </>
            )}

            {activeTab === 'search' && (
                <div style={stackStyle}>
                    <PanelBlock title={t('Search', '检索')}>
                        <div style={compactGridStyle}>
                            <input style={inputStyle} value={searchForm.query} onChange={event => setSearchForm({ ...searchForm, query: event.target.value })} placeholder={t('Ask or search knowledge', '输入问题或关键词')} />
                            <select style={inputStyle} value={searchForm.resultType} onChange={event => setSearchForm({ ...searchForm, resultType: event.target.value })}><option value="all">{t('All result types', '全部结果类型')}</option><option value="node">node</option><option value="card">card</option><option value="fact">fact</option></select>
                            <input style={inputStyle} value={searchForm.sourceKind} onChange={event => setSearchForm({ ...searchForm, sourceKind: event.target.value })} placeholder={t('Source kind', '来源类型')} />
                            <input style={inputStyle} value={searchForm.domain} onChange={event => setSearchForm({ ...searchForm, domain: event.target.value })} placeholder={t('Domain', '域名')} />
                            <input style={inputStyle} value={searchForm.labels} onChange={event => setSearchForm({ ...searchForm, labels: event.target.value })} placeholder={t('Labels', '标签')} />
                            <input style={inputStyle} type="number" value={searchForm.limit} onChange={event => setSearchForm({ ...searchForm, limit: Number(event.target.value) })} />
                        </div>
                        <label style={checkboxStyle}><input type="checkbox" checked={searchForm.includeDisabled} onChange={event => setSearchForm({ ...searchForm, includeDisabled: event.target.checked })} /> {t('Include disabled sources', '包含已禁用来源')}</label>
                        <button type="button" style={primaryButtonStyle} disabled={!!busy} onClick={runSearch}>{busy === 'search' ? t('Searching...', '检索中...') : t('Search', '检索')}</button>
                    </PanelBlock>
                    <div style={twoColumnStyle}>
                        <PanelBlock title={`${t('Results', '结果')} (${searchResults.length})`}>
                            <ResultList results={searchResults} empty={t('No results yet.', '暂无检索结果。')} query={searchForm.query} />
                        </PanelBlock>
                        <PanelBlock title={t('Facets', '分面')}>
                            <KeyValueList values={[
                                ...(facets?.result_types || []).map(item => `${item.label || item.kind} ${item.count || 0}`),
                                ...(facets?.source_kinds || []).map(item => `${item.label || item.kind} ${item.count || 0}`),
                                ...(facets?.domains || []).map(item => `${item.domain || item.label} ${item.count || 0}`),
                                ...(facets?.labels || []).map(item => `${item.label} ${item.count || 0}`),
                            ]} empty={t('Run a search to see facets.', '执行检索后显示分面。')} />
                        </PanelBlock>
                    </div>
                </div>
            )}

            {activeTab === 'sources' && (
                <div style={stackStyle}>
                    <SourceFilters t={t} filter={sourceFilter} coverageOptions={coverageOptions} onChange={setSourceFilter} />
                    <div style={inlineActionsStyle}>
                        <button type="button" style={primaryButtonStyle} disabled={!!busy} onClick={loadSources}>{busy === 'sources' ? t('Loading...', '加载中...') : t('Load Sources', '加载来源')}</button>
                        {sources !== null && <span style={mutedLineStyle}>{sources.length >= sourceFilter.limit ? `${sources.length}+ ${t('results (limit reached)', '条结果（已达上限）')}` : `${sources.length} ${t('results', '条结果')}`}</span>}
                        <span style={mutedLineStyle}>{t('Payload', '条件')} {JSON.stringify(sourcePayload)}</span>
                    </div>
                    <div style={listStyle}>
                        {sources === null ? (
                            <div style={emptyStyle}>{t('Click "Load Sources" to query.', '点击「加载来源」按钮进行查询。')}</div>
                        ) : sources.length ? sources.map(source => (
                            <div key={source.id || source.uri || source.title} style={rowStyle}>
                                <div style={rowMainStyle}>
                                    <strong>{source.title || source.relative_path || source.uri || source.id}</strong>
                                    <span style={mutedLineStyle}>{[source.kind, source.status, source.relative_path || source.uri, source.labels?.join(', ')].filter(Boolean).join(' · ')}</span>
                                    <span style={mutedLineStyle}>{[`nodes ${source.node_count || 0}`, `cards ${source.card_count || 0}`, `facts ${source.fact_count || 0}`].join(' · ')}</span>
                                </div>
                                <div style={inlineActionsStyle}>
                                    <button type="button" style={buttonStyle} disabled={!!busy} onClick={() => toggleSource(source)}>{String(source.status || '').toLowerCase() === 'disabled' ? t('Enable', '启用') : t('Disable', '禁用')}</button>
                                    <button type="button" style={dangerButtonStyle} disabled={!!busy} onClick={() => deleteSource(source)}>{t('Delete', '删除')}</button>
                                </div>
                            </div>
                        )) : <div style={emptyStyle}>{t('No sources match the current filters.', '当前筛选条件下没有匹配的来源。')}</div>}
                    </div>
                </div>
            )}

            {activeTab === 'quality' && (
                <div style={stackStyle}>
                    <SourceFilters t={t} filter={qualityFilter} coverageOptions={qualityCoverageOptions} onChange={setQualityFilter} />
                    <div style={compactGridStyle}>
                        <select style={inputStyle} value={qualityOptions.policy} onChange={event => setQualityOptions({ ...qualityOptions, policy: event.target.value })}><option value="balanced">balanced</option><option value="conservative">conservative</option><option value="aggressive">aggressive</option></select>
                        <select style={inputStyle} value={qualityOptions.distillMode} onChange={event => setQualityOptions({ ...qualityOptions, distillMode: event.target.value })}><option value="">{t('Default distill mode', '默认蒸馏模式')}</option>{distillModes.map(mode => <option key={mode} value={mode}>{mode}</option>)}</select>
                        <input style={inputStyle} type="number" value={qualityOptions.maxSourcesPerAction} onChange={event => setQualityOptions({ ...qualityOptions, maxSourcesPerAction: Number(event.target.value) })} />
                    </div>
                    <div style={inlineActionsStyle}>
                        <label style={checkboxStyle}><input type="checkbox" checked={qualityOptions.dryRun} onChange={event => setQualityOptions({ ...qualityOptions, dryRun: event.target.checked })} /> {t('Preview only', '仅预览')}</label>
                        <label style={checkboxStyle}><input type="checkbox" checked={qualityOptions.allowSensitiveDisable} onChange={event => setQualityOptions({ ...qualityOptions, allowSensitiveDisable: event.target.checked })} /> {t('Allow sensitive isolation', '允许敏感隔离')}</label>
                        <label style={checkboxStyle}><input type="checkbox" checked={qualityOptions.allowDuplicateSuppression} onChange={event => setQualityOptions({ ...qualityOptions, allowDuplicateSuppression: event.target.checked })} /> {t('Allow duplicate suppression', '允许重复抑制')}</label>
                    </div>
                    <div style={inlineActionsStyle}>
                        <button type="button" style={primaryButtonStyle} disabled={!!busy} onClick={loadQuality}>{busy === 'quality' ? t('Loading...', '加载中...') : t('Build Report + Plan', '生成报告和计划')}</button>
                        <button type="button" style={buttonStyle} disabled={!!busy || !(qualityPlan?.actions || []).length} onClick={() => executeQualityAction()}>{qualityOptions.dryRun ? t('Preview Plan', '预览计划') : t('Run Plan', '执行计划')}</button>
                    </div>
                    <div style={twoColumnStyle}>
                        <PanelBlock title={t('Quality Report', '质量报告')}>
                            <div style={statsGridStyle}>
                                <Stat label={t('Sources', '来源')} value={qualityReport?.count || 0} />
                                <Stat label={t('Average', '平均分')} value={qualityReport?.average_score || 0} />
                                <Stat label={t('Actions', '动作')} value={qualityPlan?.actions?.length || 0} />
                            </div>
                            <KeyValueList values={[...topCounts(qualityReport?.grades, 4), ...topCounts(qualityReport?.signals, 6), ...topCounts(qualityReport?.actions, 6)]} empty={t('No report yet.', '暂无报告。')} />
                        </PanelBlock>
                        <PanelBlock title={t('Maintenance Plan', '维护计划')}>
                            {(qualityPlan?.actions || []).length ? <div style={listStyle}>{(qualityPlan?.actions || []).map((action, index) => (
                                <div key={`${action.kind || index}`} style={rowStyle}>
                                    <div style={rowMainStyle}>
                                        <strong>{action.title || action.kind}</strong>
                                        <span style={mutedLineStyle}>{[action.description, action.severity, action.count ? `${action.count} sources` : '', action.signals?.join(', ')].filter(Boolean).join(' · ')}</span>
                                    </div>
                                    <button type="button" style={buttonStyle} disabled={!!busy} onClick={() => executeQualityAction(action)}>{qualityOptions.dryRun ? t('Preview', '预览') : t('Run', '执行')}</button>
                                </div>
                            ))}</div> : <div style={emptyStyle}>{t('No plan has been built.', '尚未生成维护计划。')}</div>}
                        </PanelBlock>
                    </div>
                </div>
            )}

            {operationResult ? (
                <PanelBlock title={t('Last Operation', '最近操作')}>
                    {executionResult && operationResult === executionResult ? (
                        <ExecutionSummary t={t} result={executionResult} context={executionContext} />
                    ) : null}
                    {importJob && operationResult?.id === importJob.id ? <ImportJobSummary t={t} job={importJob} /> : null}
                    <details>
                        <summary style={detailsSummaryStyle}>{t('Raw payload', '原始数据')}</summary>
                        <pre style={preStyle}>{JSON.stringify(operationResult, null, 2)}</pre>
                    </details>
                </PanelBlock>
            ) : null}
        </section>
        <KnowledgeImportDialog
            open={showImportDialog}
            onClose={() => setShowImportDialog(false)}
            onJobUpdate={job => { if (job) setImportJob(job); }}
            supportedExts={capabilities?.default_include_exts}
            t={t}
            lang={lang || 'en'}
        />
        {confirmDialog.show && (
            <ConfirmDialog
                title={confirmDialog.title}
                message={confirmDialog.message}
                t={confirmT}
                onCancel={() => setConfirmDialog(prev => ({ ...prev, show: false }))}
                onConfirm={confirmDialog.onConfirm}
            />
        )}
        </>
    );
}

function PanelBlock({ title, children }: { title: string; children: ReactNode }) {
    return <div style={blockStyle}><h3 style={blockTitleStyle}>{title}</h3><div style={blockBodyStyle}>{children}</div></div>;
}

function KeyValueList({ values, empty }: { values: string[]; empty: string }) {
    const cleanValues = values.map(value => String(value || '').trim()).filter(Boolean);
    if (!cleanValues.length) return <div style={emptyStyle}>{empty}</div>;
    return <div style={chipListStyle}>{cleanValues.map(value => <span key={value} style={chipStyle}>{value}</span>)}</div>;
}

const highlightMarkStyle: React.CSSProperties = { background: '#facc15', color: '#1a1a2e', borderRadius: 2, padding: '0 2px' };

function buildHighlightRegex(query: string): RegExp | null {
    if (!query) return null;
    const keywords = query.split(/\s+/).filter(k => k.length > 0);
    if (!keywords.length) return null;
    const escaped = keywords.map(k => k.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'));
    return new RegExp(`(${escaped.join('|')})`, 'gi');
}

function highlightText(text: string, regex: RegExp | null): React.ReactNode {
    if (!regex || !text) return text;
    const parts = text.split(regex);
    if (parts.length <= 1) return text;
    return <>{parts.map((part, i) => {
        if (!part) return null;
        // split with capturing group: odd indices are matches
        return i % 2 === 1 ? <mark key={i} style={highlightMarkStyle}>{part}</mark> : part;
    })}</>;
}

function ResultList({ results, empty, query }: { results: SearchResult[]; empty: string; query?: string }) {
    if (!results.length) return <div style={emptyStyle}>{empty}</div>;
    const regex = buildHighlightRegex((query || '').trim());
    return <div style={listStyle}>{results.map((result, index) => (
        <div key={`${result.result_type || 'result'}-${result.node_id || result.card_id || result.fact_id || index}`} style={rowStyle}>
            <div style={rowMainStyle}>
                <strong>{highlightText(result.card_title || result.node_title || result.subject || result.source?.title || result.source?.relative_path || 'Result', regex)}</strong>
                <span style={mutedLineStyle}>{[result.result_type, result.source?.kind, result.source?.relative_path || result.source?.uri, result.score ? `score ${result.score.toFixed(3)}` : ''].filter(Boolean).join(' · ')}</span>
                <span>{highlightText(result.summary || result.claim || result.snippet || [result.subject, result.predicate, result.object].filter(Boolean).join(' '), regex)}</span>
            </div>
        </div>
    ))}</div>;
}

function ExecutionSummary({ t, result, context }: {
    t: (en: string, zhHans: string, zhHant?: string) => string;
    result: SourceQualityMaintenanceExecuteResult;
    context: { source?: string; action?: string; dryRun?: boolean } | null;
}) {
    const sourceIDs = knowledgeExecutionResultSourceIDs(result, 12);
    const failures = knowledgeExecutionFailureDetails(result, 8);
    const label = knowledgeQualityExecutionContextLabel(context);
    return (
        <div style={stackStyle}>
            <div style={statsGridStyle}>
                <Stat label={t('Mode', '模式')} value={result.dry_run ? t('Preview', '预览') : t('Run', '执行')} />
                <Stat label={t('Actions', '动作')} value={result.results?.length || 0} />
                <Stat label={t('Sources', '来源')} value={result.count || sourceIDs.length || 0} />
                <Stat label={t('Failures', '失败')} value={failures.length} />
            </div>
            {label ? <div style={mutedLineStyle}>{label}</div> : null}
            {sourceIDs.length ? <div style={mutedLineStyle}>{knowledgeExecutionSourceFilterLabel(label, sourceIDs.length)}</div> : null}
            <KeyValueList values={sourceIDs} empty={t('No affected sources reported.', '未报告受影响来源。')} />
            {failures.length ? (
                <div style={listStyle}>
                    {failures.map((failure, index) => (
                        <div key={`${failure.action}-${failure.sourceID}-${index}`} style={failureRowStyle}>
                            <strong>{failure.action || t('Execution', '执行')}</strong>
                            <span style={mutedLineStyle}>{failure.sourceID}</span>
                            <span>{failure.error}</span>
                        </div>
                    ))}
                </div>
            ) : null}
        </div>
    );
}

function ImportJobSummary({ t, job }: {
    t: (en: string, zhHans: string, zhHant?: string) => string;
    job: ImportJob;
}) {
    return (
        <div style={jobStatusStyle}>
            <strong>{t('Directory Import', '目录导入')} {job.id}</strong>
            <span style={mutedLineStyle}>{[job.status, job.result?.root_path, job.result?.current_file, job.error].filter(Boolean).join(' · ')}</span>
            <div style={statsGridStyle}>
                <Stat label={t('Processed', '已处理')} value={job.result?.processed_files || 0} />
                <Stat label={t('Imported', '已导入')} value={job.result?.imported_files || 0} />
                <Stat label={t('Skipped', '已跳过')} value={job.result?.skipped_files || 0} />
                <Stat label={t('Failed', '失败')} value={job.result?.failed_files || 0} />
            </div>
        </div>
    );
}

function MetadataControls({ t, labels, topicHint, saveScope, distillMode, distillModes, onChange }: {
    t: (en: string, zhHans: string, zhHant?: string) => string;
    labels: string;
    topicHint: string;
    saveScope: string;
    distillMode: string;
    distillModes: string[];
    onChange: (patch: any) => void;
}) {
    return (
        <div style={compactGridStyle}>
            <input style={inputStyle} value={labels} onChange={event => onChange({ labels: event.target.value })} placeholder={t('Labels', '标签')} />
            <input style={inputStyle} value={topicHint} onChange={event => onChange({ topicHint: event.target.value })} placeholder={t('Topic hint', '主题提示')} />
            <select style={inputStyle} value={saveScope} onChange={event => onChange({ saveScope: event.target.value })}>
                <option value="project">{t('Project', '项目')}</option>
                <option value="personal">{t('Personal', '个人')}</option>
                <option value="local_only">{t('Local only', '仅本地')}</option>
            </select>
            <select style={inputStyle} value={distillMode} onChange={event => onChange({ distillMode: event.target.value })}>
                <option value="">{t('Default distill mode', '默认蒸馏模式')}</option>
                {distillModes.map(mode => <option key={mode} value={mode}>{mode}</option>)}
            </select>
        </div>
    );
}

function SourceFilters({ t, filter, coverageOptions, onChange }: {
    t: (en: string, zhHans: string, zhHant?: string) => string;
    filter: { query: string; kind: string; status: string; coverage: string; domain: string; labels: string; limit: number };
    coverageOptions: string[];
    onChange: (filter: any) => void;
}) {
    return (
        <PanelBlock title={t('Filters', '筛选')}>
            <div style={compactGridStyle}>
                <input style={inputStyle} value={filter.query} onChange={event => onChange({ ...filter, query: event.target.value })} placeholder={t('Query', '关键词')} />
                <input style={inputStyle} value={filter.kind} onChange={event => onChange({ ...filter, kind: event.target.value })} placeholder={t('Kind', '类型')} />
                <select style={inputStyle} value={filter.status} onChange={event => onChange({ ...filter, status: event.target.value })}>
                    <option value="all">{t('All statuses', '全部状态')}</option>
                    <option value="active">{t('active (non-disabled)', 'active（非禁用）')}</option>
                    <option value="pending">pending</option>
                    <option value="parsed">parsed</option>
                    <option value="distilled">distilled</option>
                    <option value="stale">stale</option>
                    <option value="failed">failed</option>
                    <option value="disabled">disabled</option>
                </select>
                <select style={inputStyle} value={filter.coverage} onChange={event => onChange({ ...filter, coverage: event.target.value })}>
                    {coverageOptions.map(option => <option key={option} value={option}>{option}</option>)}
                </select>
                <input style={inputStyle} value={filter.domain} onChange={event => onChange({ ...filter, domain: event.target.value })} placeholder={t('Domain', '域名')} />
                <input style={inputStyle} value={filter.labels} onChange={event => onChange({ ...filter, labels: event.target.value })} placeholder={t('Labels', '标签')} />
                <input style={inputStyle} type="number" value={filter.limit} onChange={event => onChange({ ...filter, limit: Number(event.target.value) })} />
            </div>
        </PanelBlock>
    );
}

function Stat({ label, value }: { label: string; value: string | number }) {
    return (
        <div style={statStyle}>
            <div style={statLabelStyle}>{label}</div>
            <div style={statValueStyle}>{value}</div>
        </div>
    );
}

const panelStyle: CSSProperties = { display: 'grid', gap: 14, padding: 16, border: `1px solid ${colors.border}`, borderRadius: radius.md, background: colors.surface };
const sectionHeaderStyle: CSSProperties = { display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 12, flexWrap: 'wrap' };
const titleStyle: CSSProperties = { margin: 0, fontSize: 18, fontWeight: 700, color: colors.text };
const subtleStyle: CSSProperties = { margin: '6px 0 0', color: colors.textMuted, fontSize: 13 };
const buttonStyle: CSSProperties = { border: `1px solid ${colors.border}`, borderRadius: radius.sm, padding: '7px 10px', background: colors.surface, color: colors.text, cursor: 'pointer' };
const primaryButtonStyle: CSSProperties = { ...buttonStyle, border: `1px solid ${colors.primary}`, background: colors.primaryLight, color: colors.primaryDark, fontWeight: 700 };
const dangerButtonStyle: CSSProperties = { ...buttonStyle, border: '1px solid #fecaca', color: '#b91c1c' };
const errorBoxStyle: CSSProperties = { border: '1px solid #fecaca', borderRadius: radius.sm, padding: 10, background: '#fef2f2', color: '#b91c1c' };
const successBoxStyle: CSSProperties = { border: '1px solid #bbf7d0', borderRadius: radius.sm, padding: 10, background: '#f0fdf4', color: '#166534' };
const statsGridStyle: CSSProperties = { display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(120px, 1fr))', gap: 10 };
const statStyle: CSSProperties = { border: `1px solid ${colors.borderLight}`, borderRadius: radius.sm, padding: 10, background: colors.surfaceMuted };
const statLabelStyle: CSSProperties = { fontSize: 12, color: colors.textMuted };
const statValueStyle: CSSProperties = { marginTop: 4, fontSize: 18, fontWeight: 700, color: colors.text };
const tabsStyle: CSSProperties = { display: 'flex', gap: 6, flexWrap: 'wrap', borderBottom: `1px solid ${colors.borderLight}`, paddingBottom: 8 };
const tabButtonStyle = (active: boolean): CSSProperties => ({ border: `1px solid ${active ? colors.primary : colors.border}`, borderRadius: radius.sm, padding: '7px 10px', background: active ? colors.primaryLight : colors.surface, color: active ? colors.primaryDark : colors.textMuted, fontWeight: active ? 700 : 600, cursor: 'pointer' });
const stackStyle: CSSProperties = { display: 'grid', gap: 12 };
const twoColumnStyle: CSSProperties = { display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(280px, 1fr))', gap: 12, alignItems: 'start' };
const documentImportStyle: CSSProperties = { display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 16, flexWrap: 'wrap' };
const documentImportCopyStyle: CSSProperties = { minWidth: 220, display: 'grid', gap: 4, color: colors.text, fontSize: 13 };
const documentImportButtonStyle: CSSProperties = { ...primaryButtonStyle, padding: '9px 18px', fontSize: 13, flexShrink: 0 };
const compactGridStyle: CSSProperties = { display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(160px, 1fr))', gap: 8 };
const blockStyle: CSSProperties = { border: `1px solid ${colors.borderLight}`, borderRadius: radius.sm, padding: 12, background: colors.surface };
const blockTitleStyle: CSSProperties = { margin: '0 0 10px', fontSize: 13, fontWeight: 800, color: colors.text };
const blockBodyStyle: CSSProperties = { display: 'grid', gap: 10 };
const inputStyle: CSSProperties = { width: '100%', boxSizing: 'border-box', border: `1px solid ${colors.border}`, borderRadius: radius.sm, padding: '7px 9px', background: colors.surface, color: colors.text, fontSize: 13 };
const textareaStyle: CSSProperties = { ...inputStyle, minHeight: 110, resize: 'vertical', fontFamily: 'inherit' };
const checkboxStyle: CSSProperties = { display: 'inline-flex', alignItems: 'center', gap: 6, color: colors.textMuted, fontSize: 13 };
const inlineActionsStyle: CSSProperties = { display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' };
const listStyle: CSSProperties = { display: 'grid', gap: 8 };
const rowStyle: CSSProperties = { display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 10, border: `1px solid ${colors.borderLight}`, borderRadius: radius.sm, padding: 10, background: colors.surfaceMuted };
const rowMainStyle: CSSProperties = { minWidth: 0, display: 'grid', gap: 4, color: colors.text, fontSize: 13 };
const mutedLineStyle: CSSProperties = { color: colors.textMuted, fontSize: 12, overflowWrap: 'anywhere' };
const emptyStyle: CSSProperties = { color: colors.textMuted, fontSize: 13 };
const chipListStyle: CSSProperties = { display: 'flex', gap: 6, flexWrap: 'wrap' };
const chipStyle: CSSProperties = { border: `1px solid ${colors.borderLight}`, borderRadius: radius.sm, padding: '4px 7px', color: colors.textMuted, background: colors.surfaceMuted, fontSize: 12 };
const badgeStyle: CSSProperties = { ...chipStyle, color: colors.textMuted };
const jobStatusStyle: CSSProperties = { display: 'grid', gap: 4, border: `1px solid ${colors.borderLight}`, borderRadius: radius.sm, padding: 10, background: colors.surfaceMuted, color: colors.text, fontSize: 13 };
const failureRowStyle: CSSProperties = { display: 'grid', gap: 4, border: '1px solid #fecaca', borderRadius: radius.sm, padding: 10, background: '#fef2f2', color: '#7f1d1d', fontSize: 13 };
const detailsSummaryStyle: CSSProperties = { color: colors.textMuted, cursor: 'pointer', fontSize: 12, fontWeight: 700 };
const preStyle: CSSProperties = { margin: 0, maxHeight: 260, overflow: 'auto', border: `1px solid ${colors.borderLight}`, borderRadius: radius.sm, padding: 10, background: colors.surfaceMuted, color: colors.text, fontSize: 12 };

export function parseURLBatch(value: string): string[] {
    return uniqueNormalizedParts(String(value || '').split(/[\s,;，；、]+/), item => item.trim());
}

export function parseDomainList(value: string): string[] {
    return uniqueNormalizedParts(String(value || '').split(/[\s,;，；、]+/), normalizeKnowledgeDomainFilter);
}

export function parseLabelList(value: string) {
    return uniqueNormalizedParts(
        String(value || '').split(/[\r\n\t,;，；、]+/),
        item => item.replace(/\s+/g, ' ').trim().toLowerCase(),
    );
}

function uniqueNormalizedParts(parts: string[], normalize: (value: string) => string): string[] {
    const seen = new Set<string>();
    const result: string[] = [];
    for (const part of parts) {
        const normalized = normalize(part);
        if (!normalized || seen.has(normalized)) continue;
        seen.add(normalized);
        result.push(normalized);
    }
    return result;
}

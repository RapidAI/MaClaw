import { Fragment, useEffect, useMemo, useState } from 'react';
import type { CSSProperties } from 'react';
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
    SelectKnowledgeDirectory,
    SelectKnowledgeFiles,
} from '../../../wailsjs/go/main/App';
import { colors, radius } from '../remote/styles';

type Props = {
    lang: string;
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

const defaultExts = ['.docx', '.doc', '.pdf', '.xlsx', '.xls', '.csv', '.md', '.txt'];
const sourceKindOptions = ['url', 'pdf', 'docx', 'xlsx', 'csv', 'markdown', 'text', 'conversation', 'workflow_artifact', 'doc', 'xls'];
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
    const isZh = lang === 'zh-CN' || lang === 'zh-TW';
    const [health, setHealth] = useState<any>(null);
    const [error, setError] = useState('');
    const [loading, setLoading] = useState(false);

    const refresh = async () => {
        setLoading(true);
        setError('');
        try {
            const result = await KnowledgeHealth({});
            setHealth(result || null);
        } catch (err: any) {
            setError(err?.message || String(err));
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        void refresh();
    }, []);

    const summary = knowledgeHealthSummaryModel(health);

    return (
        <section style={panelStyle}>
            <div style={sectionHeaderStyle}>
                <div>
                    <h2 style={titleStyle}>{isZh ? 'Knowledge Base' : 'Knowledge Base'}</h2>
                    <p style={subtleStyle}>Knowledge settings are available after the panel source is restored.</p>
                </div>
                <button type="button" style={buttonStyle} onClick={refresh} disabled={loading}>
                    {loading ? 'Refreshing...' : 'Refresh'}
                </button>
            </div>
            {error ? <div style={errorBoxStyle}>{error}</div> : null}
            <div style={statsGridStyle}>
                <Stat label="Status" value={summary?.status || 'Unknown'} />
                <Stat label="Score" value={summary?.score ?? 0} />
                <Stat label="Quality" value={summary?.qualityAvgScore ?? 0} />
                <Stat label="Actions" value={summary?.actions?.length ?? 0} />
            </div>
        </section>
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
const errorBoxStyle: CSSProperties = { border: '1px solid #fecaca', borderRadius: radius.sm, padding: 10, background: '#fef2f2', color: '#b91c1c' };
const statsGridStyle: CSSProperties = { display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(120px, 1fr))', gap: 10 };
const statStyle: CSSProperties = { border: `1px solid ${colors.borderLight}`, borderRadius: radius.sm, padding: 10, background: colors.surfaceMuted };
const statLabelStyle: CSSProperties = { fontSize: 12, color: colors.textMuted };
const statValueStyle: CSSProperties = { marginTop: 4, fontSize: 18, fontWeight: 700, color: colors.text };

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

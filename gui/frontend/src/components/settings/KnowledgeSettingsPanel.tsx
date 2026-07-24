import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { KnowledgeImportDialog } from './KnowledgeImportDialog';
import {
    KNOWLEDGE_IMPORT_EXPAND_EVENT,
    consumeKnowledgeImportExpandFlag,
    useKnowledgeImportOptional,
} from './KnowledgeImportContext';
import { ConfirmDialog } from '../modals/ConfirmDialog';
import { DeepCrawlPanel } from './DeepCrawlPanel';
import type { DeepCrawlConfig, DeepCrawlPreviewResult, DeepCrawlRunResult } from './DeepCrawlPanel';
import { buildHubCardStoreURL } from '../../utils/hubCredits';
import {
    LoadConfig,
    PatchConfigFields,
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
    KnowledgeFactGraph,
    KnowledgeFactIndex,
    KnowledgeExportSnapshotWithOptions,
    KnowledgeHealth,
    KnowledgeClearAll,
    KnowledgeImportDirectory,
    KnowledgeImportFiles,
    KnowledgeImportHubShare,
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
    KnowledgeSearchStructured,
    KnowledgeStructuredCatalog,
    KnowledgeSourceGraph,
    KnowledgeSourceNeighborhood,
    KnowledgeSourcePath,
    KnowledgeSourceDigest,
    KnowledgeShareToHub,
    KnowledgeListMyHubShares,
    KnowledgeDeleteHubShare,
    KnowledgeUpdateHubShare,
    OpenFileOrShowInFolder,
    KnowledgeSyncDelete,
    KnowledgeSyncDownload,
    KnowledgeSyncStatus,
    KnowledgeSyncUpload,
    KnowledgeSyncVerifyPassword,
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
    KnowledgeGetImageAssetPaths,
    KnowledgeOpenImageFile,
    GetHubLLMServiceStatus,
    OpenSystemUrl,
    SelectKnowledgeDirectory,
    SelectKnowledgeFiles,
    SelectKnowledgeSnapshotExportPath,
    SelectKnowledgeSnapshotFile,
} from '../../../wailsjs/go/main/App';

type Props = {
    lang?: string;
    showToastMessage?: (message: string, duration?: number) => void;
};

type Source = {
    id?: string;
    batch_id?: string;
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
    table_id?: string;
    row_id?: string;
    cell_id?: string;
    row_index?: number;
    column_name?: string;
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

type StructuredCatalogColumn = {
    column_name?: string;
    normalized_name?: string;
    value_type?: string;
};

type StructuredCatalogTable = {
    id?: string;
    source_id?: string;
    source_title?: string;
    source_kind?: string;
    sheet_name?: string;
    table_title?: string;
    row_count?: number;
    column_count?: number;
    columns?: StructuredCatalogColumn[];
};

type StructuredCatalogResult = {
    count?: number;
    tables?: StructuredCatalogTable[];
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

export function applyKnowledgeStructuredSearchPayload(payload: any, filters: {
    query?: string;
    sourceID?: string;
    sheetName?: string;
    columnName?: string;
    matchMode?: string;
    columnValue?: string;
    numberMin?: string;
    numberMax?: string;
    dateStart?: string;
    dateEnd?: string;
    limit?: number;
    includeDisabled?: boolean;
} = {}) {
    const query = (filters.query || '').trim();
    if (query) payload.query = query;
    const sourceID = (filters.sourceID || '').trim();
    if (sourceID) payload.source_ids = [sourceID];
    const sheetName = (filters.sheetName || '').trim();
    if (sheetName) payload.sheet_names = [sheetName];
    const columnName = (filters.columnName || '').trim();
    const matchMode = normalizeKnowledgeFilterToken(filters.matchMode) || 'equals';
    const columnValue = (filters.columnValue || '').trim();
    if (columnName && columnValue && matchMode === 'contains') {
        payload.column_contains = { [columnName]: columnValue };
    } else if (columnName && columnValue && matchMode === 'equals') {
        payload.column_equals = { [columnName]: columnValue };
    }
    const numberRange: Record<string, number> = {};
    const numberMin = (filters.numberMin || '').trim();
    const numberMax = (filters.numberMax || '').trim();
    const min = parseKnowledgeStructuredNumber(numberMin);
    const max = parseKnowledgeStructuredNumber(numberMax);
    if (numberMin && Number.isFinite(min)) numberRange.min = min;
    if (numberMax && Number.isFinite(max)) numberRange.max = max;
    if (columnName && Object.keys(numberRange).length) payload.number_ranges = { [columnName]: numberRange };
    const dateRange: Record<string, string> = {};
    const dateStart = (filters.dateStart || '').trim();
    const dateEnd = (filters.dateEnd || '').trim();
    if (dateStart) dateRange.start = dateStart;
    if (dateEnd) dateRange.end = dateEnd;
    if (columnName && Object.keys(dateRange).length) payload.date_ranges = { [columnName]: dateRange };
    payload.limit = normalizeKnowledgeSourceLimit(filters.limit, 20, 100);
    payload.include_disabled = !!filters.includeDisabled;
}

function parseKnowledgeStructuredNumber(value: string) {
    return Number(value.replace(/,/g, ''));
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

type KnowledgeSubTab = 'overview' | 'ingest' | 'export' | 'sync' | 'search' | 'sources' | 'quality';

type ExportSourceGroup = {
    id: string;
    label: string;
    meta: string;
    sources: Source[];
};

function SyncIcon() {
    return (
        <svg className="knowledge-button-icon" viewBox="0 0 24 24" aria-hidden="true" focusable="false">
            <path d="M21 12a9 9 0 0 1-15.4 6.4L3 16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
            <path d="M3 16v5h5" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
            <path d="M3 12A9 9 0 0 1 18.4 5.6L21 8" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
            <path d="M21 8V3h-5" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
    );
}

function basenameFromPath(value?: string) {
    const text = String(value || '').trim();
    if (!text) return '';
    const parts = text.split(/[\\/]+/).filter(Boolean);
    return parts[parts.length - 1] || text;
}

function hostnameFromURL(value?: string) {
    const text = String(value || '').trim();
    if (!text) return '';
    try {
        return new URL(text).hostname;
    } catch {
        return '';
    }
}

function knowledgeExportSourceLabel(source: Source) {
    const kind = String(source.kind || '').toLowerCase();
    if (kind.includes('url') || /^https?:\/\//i.test(source.uri || '')) {
        return source.title || hostnameFromURL(source.uri) || source.uri || source.id || 'URL';
    }
    if (source.relative_path) return basenameFromPath(source.relative_path);
    if (source.title) return source.title;
    if (source.uri) return basenameFromPath(source.uri) || source.uri;
    return source.id || 'source';
}

function knowledgeExportSourceMeta(source: Source) {
    const location = source.relative_path || source.uri || source.canonical_uri || '';
    return [
        source.kind,
        source.status,
        location,
        source.labels?.length ? source.labels.join(', ') : '',
        `nodes ${source.node_count || 0}`,
    ].filter(Boolean).join(' · ');
}

function knowledgeExportGroups(sources: Source[], batches: ImportBatch[], t: (en: string, zhHans: string, zhHant?: string) => string): ExportSourceGroup[] {
    const batchMap = new Map<string, ImportBatch>();
    for (const batch of batches || []) {
        const id = String(batch.id || '').trim();
        if (id) batchMap.set(id, batch);
    }

    const grouped = new Map<string, Source[]>();
    const unassigned: Source[] = [];
    for (const source of sources || []) {
        const batchID = String(source.batch_id || '').trim();
        if (batchID && batchMap.has(batchID)) {
            const items = grouped.get(batchID);
            if (items) {
                items.push(source);
            } else {
                grouped.set(batchID, [source]);
            }
        } else {
            unassigned.push(source);
        }
    }

    const groups = Array.from(grouped.entries()).map(([batchID, items]) => {
        const batch = batchMap.get(batchID) || {};
        return {
            id: `batch:${batchID}`,
            label: batch.root_path || batchID,
            meta: [
                t('Import batch', '导入批次'),
                batch.status,
                batch.imported_files !== undefined ? `${batch.imported_files}/${batch.total_files || items.length} ${t('imported', '已导入')}` : '',
                batch.failed_files ? `${batch.failed_files} ${t('failed', '失败')}` : '',
                batch.updated_at,
            ].filter(Boolean).join(' · '),
            sources: items,
        };
    });

    groups.sort((left, right) => left.label.localeCompare(right.label));
    if (unassigned.length) {
        groups.push({
            id: 'unassigned',
            label: t('Ungrouped sources', '未归属来源'),
            meta: t('Manual text, URLs, Hub imports, or older records without an import batch', '手工文本、URL、Hub 导入或没有导入批次的历史记录'),
            sources: unassigned,
        });
    }
    return groups;
}

function knowledgeSyncLocalOfficialActive(status: any) {
    if (!status || typeof status !== 'object') return false;
    if (status.active) return true;
    const grants = [
        ...(Array.isArray(status.active_grants) ? status.active_grants : []),
        ...(Array.isArray(status.credit_grants) ? status.credit_grants : []),
    ];
    const now = Date.now();
    return grants.some((grant: any) => {
        const state = String(grant?.status || (grant?.active === false ? 'queued' : 'active')).toLowerCase();
        if (['expired', 'revoked', 'disabled', 'inactive'].includes(state)) return false;
        const startsAt = Date.parse(String(grant?.starts_at || ''));
        if (Number.isFinite(startsAt) && startsAt > now) return false;
        const expiresAt = Date.parse(String(grant?.expires_at || ''));
        if (Number.isFinite(expiresAt)) return expiresAt > now;
        return grant?.active === true || ['active', 'valid', 'period_limited', 'limited'].includes(state);
    });
}


export function KnowledgeSettingsPanel({ lang, showToastMessage }: Props) {
    const t = (en: string, zhHans: string, zhHant: string = zhHans) => (
        lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en
    );
    const [activeTab, setActiveTab] = useState<KnowledgeSubTab>('overview');
    const [health, setHealth] = useState<any>(null);
    const [capabilities, setCapabilities] = useState<KnowledgeCapabilitiesResult | null>(null);
    const [sources, setSources] = useState<Source[] | null>(null);
    const [searchResults, setSearchResults] = useState<SearchResult[]>([]);
    const [facets, setFacets] = useState<SearchFacetsResult | null>(null);
    const [structuredCatalog, setStructuredCatalog] = useState<StructuredCatalogResult | null>(null);
    const [qualityReport, setQualityReport] = useState<SourceQualityReport | null>(null);
    const [qualityPlan, setQualityPlan] = useState<SourceQualityMaintenancePlan | null>(null);
    const [executionResult, setExecutionResult] = useState<SourceQualityMaintenanceExecuteResult | null>(null);
    const [executionContext, setExecutionContext] = useState<{ source?: string; action?: string; dryRun?: boolean } | null>(null);
    const [importJob, setImportJob] = useState<ImportJob | null>(null);
    const [importDialogKey, setImportDialogKey] = useState(0);
    const [autoRecallEnabled, setAutoRecallEnabled] = useState(true);
    const [autoRecallMinScore, setAutoRecallMinScore] = useState('');
    const [autoRecallSaving, setAutoRecallSaving] = useState(false);
    const [operationResult, setOperationResult] = useState<any>(null);
    const [error, setError] = useState('');
    const [loading, setLoading] = useState(false);
    const [busy, setBusy] = useState('');
    const [textForm, setTextForm] = useState({ title: '', text: '', labels: '', topicHint: '', saveScope: 'project', distillMode: '' });
    const [urlForm, setURLForm] = useState({ urls: '', labels: '', topicHint: '', saveScope: 'project', distillMode: '', autoLabels: true });
    const [fileForm, setFileForm] = useState({ directory: '', labels: '', topicHint: '', saveScope: 'project', distillMode: '', includeExts: '', excludeGlobs: '', recursive: true, autoLabels: true, dryRun: false, maxFileBytes: 10485760 });
    const [exchangeForm, setExchangeForm] = useState({ redactSensitive: true, importPath: '', dryRun: true, overwrite: false, replaceAll: false });
    const [hubShareForm, setHubShareForm] = useState({ hubURL: '', hubToken: '', title: '', description: '', visibilityScope: 'hub', visibilityUsers: '', ttl: '7d', includeDisabled: false });
    const [hubImportForm, setHubImportForm] = useState({ hubURL: '', hubToken: '', knowledgeID: '', shareLink: '', dryRun: true });
    const [syncForm, setSyncForm] = useState({ hubURL: '', hubToken: '', tenantID: '', email: '', hubCenterURL: '', hubID: '', tenantName: '', password: '', passwordConfirm: '', conflictStrategy: '' });
    const [syncStatus, setSyncStatus] = useState<any>(null);
    const [syncConflictResult, setSyncConflictResult] = useState<any>(null);
    const [hubLLMServiceStatus, setHubLLMServiceStatus] = useState<any>(null);
    const [hubShareResult, setHubShareResult] = useState<any>(null);
    const [exportResult, setExportResult] = useState<any>(null);
    const [exportFormat, setExportFormat] = useState<'jsonl' | 'package'>('jsonl');
    const [myShares, setMyShares] = useState<any[] | null>(null);
    const [mySharesTotal, setMySharesTotal] = useState(0);
    const [mySharesLoading, setMySharesLoading] = useState(false);
    const [mySharesError, setMySharesError] = useState('');
    const [mySharesAttempted, setMySharesAttempted] = useState(false);
    const [editingShareID, setEditingShareID] = useState('');
    const [editShareForm, setEditShareForm] = useState({ title: '', description: '', visibilityScope: 'hub', visibilityUsers: '' });
    const [editShareSaving, setEditShareSaving] = useState(false);
    const [exportSources, setExportSources] = useState<Source[] | null>(null);
    const [exportBatches, setExportBatches] = useState<ImportBatch[] | null>(null);
    const [exportSelection, setExportSelection] = useState<Record<string, boolean>>({});
    const [exportListLoading, setExportListLoading] = useState(false);
    const [exportListAttempted, setExportListAttempted] = useState(false);
    const [showHubShareDialog, setShowHubShareDialog] = useState(false);
    const hubShareButtonRef = useRef<HTMLButtonElement | null>(null);
    const hubShareDialogRef = useRef<HTMLElement | null>(null);
    const hubShareDescriptionRef = useRef<HTMLTextAreaElement | null>(null);
    const busyRef = useRef(busy);
    const syncStatusIdentityRef = useRef('');
    const [selectedFiles, setSelectedFiles] = useState<string[]>([]);
    const [searchForm, setSearchForm] = useState({ query: '', resultType: 'all', sourceKind: 'all', domain: '', sourceID: '', labels: '', limit: 20, includeDisabled: false });
    const [searchMode, setSearchMode] = useState<'semantic' | 'structured'>('semantic');
    const [structuredSearchForm, setStructuredSearchForm] = useState({ sheetName: '', columnName: '', matchMode: 'equals', columnValue: '', numberMin: '', numberMax: '', dateStart: '', dateEnd: '' });
    const [sourceFilter, setSourceFilter] = useState({ query: '', kind: 'all', status: 'all', coverage: 'all', domain: '', labels: '', limit: 100 });
    const [qualityFilter, setQualityFilter] = useState({ query: '', kind: 'all', status: 'all', coverage: 'all', domain: '', labels: '', limit: 100 });
    const [qualityOptions, setQualityOptions] = useState({ policy: 'balanced', dryRun: true, distillMode: '', maxSourcesPerAction: 100, allowSensitiveDisable: false, allowDuplicateSuppression: true });
    const [showImportDialog, setShowImportDialog] = useState(false);
    const [confirmDialog, setConfirmDialog] = useState<{ show: boolean; title: string; message: string; onConfirm: () => void }>({ show: false, title: '', message: '', onConfirm: () => {} });
    const [deepCrawlBusy, setDeepCrawlBusy] = useState(false);
    const knowledgeImportGlobal = useKnowledgeImportOptional();

    // Keep global float hidden while the dialog is open; reopen on Expand from any page.
    useEffect(() => {
        knowledgeImportGlobal?.setDialogOpen(showImportDialog);
    }, [showImportDialog, knowledgeImportGlobal]);

    useEffect(() => {
        const openDialog = () => setShowImportDialog(true);
        if (consumeKnowledgeImportExpandFlag()) openDialog();
        window.addEventListener(KNOWLEDGE_IMPORT_EXPAND_EVENT, openDialog);
        return () => window.removeEventListener(KNOWLEDGE_IMPORT_EXPAND_EVENT, openDialog);
    }, []);

    // After global float Dismiss (job cleared), remount dialog so the next open is Step 1.
    const prevGlobalImportJobIdRef = useRef<string>('');
    useEffect(() => {
        const id = String(knowledgeImportGlobal?.job?.id || '').trim();
        const prev = prevGlobalImportJobIdRef.current;
        prevGlobalImportJobIdRef.current = id;
        if (prev && !id && !showImportDialog) {
            setImportJob(null);
            setImportDialogKey(k => k + 1);
        }
    }, [knowledgeImportGlobal?.job?.id, showImportDialog]);

    const notifySuccess = useCallback((message: string) => {
        showToastMessage?.(message, 3000);
    }, [showToastMessage]);

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
        client_run_id: config.clientRunID || '',
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

    const handleDeepCrawlStart = useCallback(async (config: DeepCrawlConfig): Promise<DeepCrawlRunResult | void> => {
        setDeepCrawlBusy(true);
        setError('');
        try {
            return await KnowledgeDeepCrawl(mapConfigToRequest(config)) as DeepCrawlRunResult;
        } catch (err: any) {
            setError(err?.message || String(err));
            throw err;
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
        let cancelled = false;
        void LoadConfig()
            .then((cfg: any) => {
                if (cancelled || !cfg) return;
                // Default on when field is absent (matches AppConfig.IsKnowledgeAutoRecallEnabled).
                setAutoRecallEnabled(cfg.knowledge_auto_recall_enabled !== false);
                const minScore = Number(cfg.knowledge_auto_recall_min_score || 0);
                setAutoRecallMinScore(minScore > 0 ? String(minScore) : '');
                setHubShareForm(prev => ({
                    ...prev,
                    hubURL: prev.hubURL || cfg.remote_hub_url || '',
                    hubToken: prev.hubToken || cfg.remote_viewer_token || '',
                }));
                setHubImportForm(prev => ({
                    ...prev,
                    hubURL: prev.hubURL || cfg.remote_hub_url || '',
                    hubToken: prev.hubToken || cfg.remote_viewer_token || '',
                }));
                setSyncForm(prev => ({
                    ...prev,
                    hubURL: prev.hubURL || cfg.remote_hub_url || '',
                    hubToken: prev.hubToken || cfg.remote_viewer_token || '',
                    tenantID: prev.tenantID || cfg.remote_tenant_id || '',
                    email: prev.email || cfg.remote_email || '',
                    hubCenterURL: prev.hubCenterURL || cfg.remote_hubcenter_url || '',
                    hubID: prev.hubID || cfg.remote_hub_id || '',
                    tenantName: prev.tenantName || cfg.remote_tenant_name || '',
                }));
            })
            .catch(() => {});
        return () => { cancelled = true; };
    }, []);

    const saveAutoRecallSettings = useCallback(async (enabled: boolean, minScoreRaw: string) => {
        setAutoRecallSaving(true);
        setError('');
        try {
            const parsed = Number(String(minScoreRaw || '').trim());
            const minScore = Number.isFinite(parsed) && parsed > 0 ? Math.min(10, parsed) : 0;
            await PatchConfigFields({
                knowledge_auto_recall_enabled: enabled,
                knowledge_auto_recall_min_score: minScore,
            });
            setAutoRecallEnabled(enabled);
            setAutoRecallMinScore(minScore > 0 ? String(minScore) : '');
            notifySuccess(t('Auto-recall settings saved.', '自动召回设置已保存。'));
        } catch (err: any) {
            setError(err?.message || String(err));
        } finally {
            setAutoRecallSaving(false);
        }
    }, [notifySuccess, t]);

    const summary = knowledgeHealthSummaryModel(health);
    const sourcePayload = useMemo(() => knowledgeSourceListPayload(capabilities, sourceFilter), [capabilities, sourceFilter]);
    const knowledgeTabs = useMemo(() => ([
        { id: 'overview' as const, label: t('Overview', '总览'), desc: t('Health, score, and capabilities', '健康、评分与能力') },
        { id: 'ingest' as const, label: t('Ingest', '导入'), desc: t('Text, URL, files, and crawl', '文本、URL、文件与抓取') },
        { id: 'export' as const, label: t('Export', '导出'), desc: t('Snapshots, Hub sharing, and share links', '快照、Hub 分享与分享链接') },
        { id: 'sync' as const, label: t('Sync', '同步'), desc: t('Encrypted manual sync through Hub', '通过 Hub 中转的加密手动同步') },
        { id: 'search' as const, label: t('Search', '检索'), desc: t('Query and facets', '查询与分面') },
        { id: 'sources' as const, label: t('Sources', '来源'), desc: t('Inspect and manage source records', '查看并管理来源') },
        { id: 'quality' as const, label: t('Quality', '质量'), desc: t('Reports and maintenance plans', '报告与维护计划') },
    ]), [t]);
    // Invalidate stale results when the query payload changes (filter or capabilities update).
    // sourcePayload is the direct input to KnowledgeListSources — sources is its cached output.
    useEffect(() => { setSources(null); }, [sourcePayload]);
    const sourcePayloadRef = useRef(sourcePayload);
    sourcePayloadRef.current = sourcePayload;
    const qualityPayload = useMemo(() => knowledgeSourceListPayload(capabilities, qualityFilter), [capabilities, qualityFilter]);
    const coverageOptions = useMemo(() => knowledgeSourceCoverageOptions(capabilities, sourceFilter.coverage), [capabilities, sourceFilter.coverage]);
    const qualityCoverageOptions = useMemo(() => knowledgeSourceCoverageOptions(capabilities, qualityFilter.coverage), [capabilities, qualityFilter.coverage]);
    const exportSourceGroups = useMemo(
        () => knowledgeExportGroups(exportSources || [], exportBatches || [], t),
        [exportSources, exportBatches, t],
    );
    const selectedExportSourceIDs = useMemo(
        () => Object.keys(exportSelection).filter(id => exportSelection[id]),
        [exportSelection],
    );
    const selectedDisabledShareCount = useMemo(() => {
        if (hubShareForm.includeDisabled || !selectedExportSourceIDs.length) return 0;
        const disabledIDs = new Set((exportSources || [])
            .filter(source => String(source.status || '').toLowerCase() === 'disabled')
            .map(source => String(source.id || '').trim())
            .filter(Boolean));
        return selectedExportSourceIDs.filter(id => disabledIDs.has(id)).length;
    }, [exportSources, hubShareForm.includeDisabled, selectedExportSourceIDs]);
    const exportSourceCount = exportSources?.length || 0;
    const exportSelectionLabel = selectedExportSourceIDs.length
        ? t(`${selectedExportSourceIDs.length} selected`, `已选择 ${selectedExportSourceIDs.length} 项`)
        : t('All sources', '全部来源');
    const hubShareDescriptionMissing = !hubShareForm.description.trim();
    const distillModes = capabilities?.distill_modes?.length ? capabilities.distill_modes : ['rules_only', 'llm_optional'];
    const formatSummary = (capabilities?.formats || []).slice(0, 6).map(format => `${format.kind || 'format'}${format.extensions?.length ? ` (${format.extensions.join(', ')})` : ''}`);
    const aliasSummary = knowledgeCoverageAliasSummary(capabilities, 4);
    const structuredColumnSuggestions = useMemo(() => {
        const seen = new Map<string, StructuredCatalogColumn>();
        for (const table of structuredCatalog?.tables || []) {
            for (const column of table.columns || []) {
                const name = String(column.column_name || '').trim();
                if (!name) continue;
                const key = name.toLowerCase();
                if (!seen.has(key)) seen.set(key, column);
            }
        }
        return Array.from(seen.values()).sort((a, b) => String(a.column_name || '').localeCompare(String(b.column_name || '')));
    }, [structuredCatalog]);
    const structuredSheetSuggestions = useMemo(() => {
        const values = new Set<string>();
        for (const table of structuredCatalog?.tables || []) {
            const name = String(table.sheet_name || '').trim();
            if (name) values.add(name);
        }
        return Array.from(values).sort((a, b) => a.localeCompare(b));
    }, [structuredCatalog]);

    const refreshSourceList = async () => {
        const requestPayload = sourcePayload;
        const result = await KnowledgeListSources(requestPayload);
        // Discard stale response if filter changed during the request.
        if (sourcePayloadRef.current !== requestPayload) return [];
        const nextSources = Array.isArray(result) ? result : [];
        setSources(nextSources);
        return nextSources;
    };

    const loadExportSelectionData = useCallback(async () => {
        setExportListAttempted(true);
        setExportListLoading(true);
        setError('');
        try {
            const [nextSources, nextBatches] = await Promise.all([
                KnowledgeListSources({ limit: 5000, include_disabled: true }),
                KnowledgeListImportBatches(200),
            ]);
            const normalizedSources = Array.isArray(nextSources) ? nextSources : [];
            setExportSources(normalizedSources);
            setExportBatches(Array.isArray(nextBatches) ? nextBatches : []);
            const availableIDs = new Set(normalizedSources.map(source => String(source.id || '').trim()).filter(Boolean));
            setExportSelection(prev => Object.fromEntries(
                Object.entries(prev).filter(([id, selected]) => selected && availableIDs.has(id)),
            ));
        } catch (err: any) {
            setError(err?.message || String(err));
        } finally {
            setExportListLoading(false);
        }
    }, []);

    useEffect(() => {
        if (activeTab !== 'export' || exportSources !== null || exportListLoading || exportListAttempted) return;
        void loadExportSelectionData();
    }, [activeTab, exportSources, exportListLoading, exportListAttempted, loadExportSelectionData]);

    useEffect(() => {
        if (activeTab !== 'sync' || busy) return;
        const identityKey = [
            syncForm.hubURL.trim(),
            syncForm.hubToken.trim(),
            syncForm.tenantID.trim(),
            syncForm.email.trim(),
        ].join('\n');
        if (syncStatusIdentityRef.current === identityKey) return;
        syncStatusIdentityRef.current = identityKey;
        void refreshKnowledgeSyncStatus();
    }, [activeTab, busy, syncForm.hubURL, syncForm.hubToken, syncForm.tenantID, syncForm.email, syncStatus]);

    useEffect(() => {
        busyRef.current = busy;
    }, [busy]);

    useEffect(() => {
        if (!showHubShareDialog) return;
        const previousOverflow = document.body.style.overflow;
        document.body.style.overflow = 'hidden';
        hubShareDialogRef.current?.focus();
        const handleKeyDown = (event: KeyboardEvent) => {
            if (event.key === 'Escape' && busyRef.current !== 'shareToHub') {
                setShowHubShareDialog(false);
            }
        };
        window.addEventListener('keydown', handleKeyDown);
        return () => {
            document.body.style.overflow = previousOverflow;
            window.removeEventListener('keydown', handleKeyDown);
            if (hubShareButtonRef.current && document.contains(hubShareButtonRef.current)) {
                hubShareButtonRef.current.focus();
            }
        };
    }, [showHubShareDialog]);

    const runTask = async (name: string, task: () => Promise<any>, options: { refreshSources?: boolean; refreshHealth?: boolean; successMessage?: string | false } = {}) => {
        setBusy(name);
        setError('');
        if (name !== 'executeQuality') {
            setExecutionResult(null);
            setExecutionContext(null);
        }
        try {
            const result = await task();
            setOperationResult(result ?? { ok: true });
            if (options.refreshSources) await refreshSourceList();
            if (options.refreshHealth) await refresh();
            if (options.successMessage !== false) {
                notifySuccess(options.successMessage || t('Operation completed successfully.', '操作成功完成。', '操作成功完成。'));
            }
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

    const exportKnowledgeSnapshot = async () => {
        const outputPath = await SelectKnowledgeSnapshotExportPath(exportFormat);
        if (!outputPath) return;
        setExportResult(null);
        const result = await runTask('exportSnapshot', () => KnowledgeExportSnapshotWithOptions({
            output_path: outputPath,
            format: exportFormat,
            redact_sensitive: exchangeForm.redactSensitive,
            source_ids: selectedExportSourceIDs,
            title: exportFormat === 'package'
                ? t('Knowledge export', '知识导出')
                : '',
            description: exportFormat === 'package'
                ? t('Local knowledge package exported from Maclaw.', '从 Maclaw 导出的本地知识包。')
                : '',
        }), {
            successMessage: exportFormat === 'package'
                ? t('Knowledge package exported.', '知识包已导出。')
                : t('Knowledge snapshot exported.', '知识库快照已导出。'),
        });
        if (result) setExportResult(result);
    };

    const revealExportPath = async (path: string) => {
        const p = String(path || '').trim();
        if (!p) return;
        try {
            await OpenFileOrShowInFolder(p);
        } catch (err: any) {
            setError(err?.message || String(err));
        }
    };

    const openShareURL = async (url: string) => {
        const u = String(url || '').trim();
        if (!u) return;
        try {
            await OpenSystemUrl(u);
        } catch (err: any) {
            setError(err?.message || String(err));
        }
    };

    const visibilityScopeHint = (scope: string) => {
        switch (scope) {
            case 'public':
                return t('Anyone who can browse public Hub knowledge may import this share.', '任何可访问公开知识的 Hub 用户都可能导入此知识。');
            case 'hub':
                return t('Only users on the current Hub can browse and import this share.', '仅当前 Hub 用户可浏览和导入。');
            case 'tenant':
                return t('Only users in the current tenant can browse and import this share.', '仅当前租户用户可浏览和导入。');
            case 'private':
                return t('Only you can browse and import this share.', '仅你自己可浏览和导入。');
            case 'users':
                return t('Only the listed users can browse and import this share. At least one user is required.', '仅列表中的用户可浏览和导入；至少填写 1 个用户。');
            default:
                return '';
        }
    };

    const formatExportBytes = (bytes: number) => {
        if (!Number.isFinite(bytes) || bytes <= 0) return '';
        if (bytes >= 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
        if (bytes >= 1024) return `${Math.max(1, Math.round(bytes / 1024))} KB`;
        return `${bytes} B`;
    };

    const chooseKnowledgeSnapshotImport = async () => {
        const inputPath = await SelectKnowledgeSnapshotFile();
        if (!inputPath) return;
        setExchangeForm(prev => ({ ...prev, importPath: inputPath }));
    };

    const importKnowledgeSnapshot = async (dryRun = exchangeForm.dryRun) => {
        const inputPath = exchangeForm.importPath.trim();
        if (!inputPath) {
            setError(t('Choose a knowledge snapshot before importing.', '请先选择知识库快照文件。'));
            return;
        }
        await runTask(dryRun ? 'importSnapshotDryRun' : 'importSnapshot', () => KnowledgeImportSnapshot({
            input_path: inputPath,
            dry_run: dryRun,
            overwrite: exchangeForm.overwrite,
            replace_all: exchangeForm.replaceAll,
            abort_on_error: true,
            safety_backup_redact: true,
        }), {
            refreshSources: !dryRun,
            refreshHealth: !dryRun,
            successMessage: dryRun
                ? t('Knowledge snapshot checked. Review the result before importing.', '知识库快照已检查，请确认结果后再导入。')
                : t('Knowledge snapshot imported.', '知识库快照已导入。'),
        });
    };

    const importHubShare = async (dryRun = hubImportForm.dryRun) => {
        if (!hubImportForm.shareLink.trim() && !hubImportForm.knowledgeID.trim()) {
            setError(t('Paste a share link or knowledge ID before importing.', '请先粘贴分享链接或知识 ID。'));
            return;
        }
        await runTask(dryRun ? 'importHubShareDryRun' : 'importHubShare', () => KnowledgeImportHubShare({
            hub_url: hubImportForm.hubURL.trim(),
            hub_token: hubImportForm.hubToken.trim(),
            knowledge_id: hubImportForm.knowledgeID.trim(),
            share_link: hubImportForm.shareLink.trim(),
            dry_run: dryRun,
        }), {
            refreshSources: !dryRun,
            refreshHealth: !dryRun,
            successMessage: dryRun
                ? t('Hub knowledge share checked. Review the result before importing.', 'Hub 知识分享已检查，请确认结果后再导入。')
                : t('Hub knowledge share imported.', 'Hub 知识分享已导入。'),
        });
    };

    const shareKnowledgeToHub = async () => {
        const description = hubShareForm.description.trim();
        if (!description) {
            setError(t('Knowledge description is required before sharing.', '分享前必须填写知识描述。'));
            hubShareDescriptionRef.current?.focus();
            return;
        }
        setHubShareResult(null);
        setError('');
        await runTask('shareToHub', async () => {
            const result = await KnowledgeShareToHub({
                hub_url: hubShareForm.hubURL.trim(),
                hub_token: hubShareForm.hubToken.trim(),
                title: hubShareForm.title.trim(),
                description,
                visibility_scope: hubShareForm.visibilityScope,
                visibility_users: parseLabelList(hubShareForm.visibilityUsers),
                ttl: hubShareForm.ttl,
                source_ids: selectedExportSourceIDs,
                redact_sensitive: exchangeForm.redactSensitive,
                include_disabled: hubShareForm.includeDisabled,
            });
            setHubShareResult(result || null);
            setShowHubShareDialog(false);
            void loadMyHubShares();
            return result;
        }, {
            successMessage: t('Knowledge shared to Hub.', '知识已分享到 Hub。'),
        });
    };

    const openHubShareDialog = () => {
        setError('');
        setShowHubShareDialog(true);
    };

    const openHubKnowledgeShares = async () => {
        const hubURL = (hubShareForm.hubURL || hubShareResult?.hub_url || '').trim().replace(/\/+$/, '');
        if (!hubURL) {
            setError(t('Hub URL is required to open shared knowledge.', '需要 Hub 地址才能查看分享。'));
            return;
        }
        const token = hubShareForm.hubToken.trim();
        const tokenHash = token ? `#token=${encodeURIComponent(token)}` : '';
        await OpenSystemUrl(`${hubURL}/hub/knowledge/shares/mine${tokenHash}`);
    };

    const loadMyHubShares = useCallback(async () => {
        setMySharesAttempted(true);
        setMySharesLoading(true);
        setMySharesError('');
        try {
            const result = await KnowledgeListMyHubShares({
                hub_url: hubShareForm.hubURL.trim(),
                hub_token: hubShareForm.hubToken.trim(),
                limit: 20,
            });
            const items = Array.isArray(result?.items) ? result.items : [];
            setMyShares(items);
            setMySharesTotal(Number(result?.total || items.length || 0));
        } catch (err: any) {
            setMyShares(null);
            setMySharesTotal(0);
            setMySharesError(err?.message || String(err));
        } finally {
            setMySharesLoading(false);
        }
    }, [hubShareForm.hubURL, hubShareForm.hubToken]);

    useEffect(() => {
        if (activeTab !== 'export' || mySharesAttempted || mySharesLoading) return;
        // Auto-load once when export tab is opened (Hub URL/token may still be empty → shows error/empty).
        void loadMyHubShares();
    }, [activeTab, mySharesAttempted, mySharesLoading, loadMyHubShares]);

    const openEditMyHubShare = (share: any) => {
        const id = String(share?.knowledge_id || '').trim();
        if (!id) return;
        setEditingShareID(id);
        setEditShareForm({
            title: String(share?.title || ''),
            description: String(share?.description || ''),
            visibilityScope: String(share?.visibility_scope || 'hub'),
            visibilityUsers: '',
        });
        setMySharesError('');
    };

    const saveEditMyHubShare = async () => {
        const id = editingShareID.trim();
        const description = editShareForm.description.trim();
        if (!id) return;
        if (!description) {
            setMySharesError(t('Knowledge description is required before sharing.', '分享前必须填写知识描述。'));
            return;
        }
        setEditShareSaving(true);
        setMySharesError('');
        try {
            const updated = await KnowledgeUpdateHubShare({
                hub_url: hubShareForm.hubURL.trim(),
                hub_token: hubShareForm.hubToken.trim(),
                knowledge_id: id,
                title: editShareForm.title.trim(),
                description,
                visibility_scope: editShareForm.visibilityScope,
                visibility_users: editShareForm.visibilityScope === 'users'
                    ? parseLabelList(editShareForm.visibilityUsers)
                    : [],
            });
            setMyShares(prev => {
                if (!Array.isArray(prev)) return prev;
                return prev.map(item => String(item.knowledge_id || '') === id ? { ...item, ...updated } : item);
            });
            setEditingShareID('');
            notifySuccess(t('Share updated.', '分享已更新。'));
        } catch (err: any) {
            setMySharesError(err?.message || String(err));
        } finally {
            setEditShareSaving(false);
        }
    };

    const deleteMyHubShare = async (knowledgeID: string) => {
        const id = String(knowledgeID || '').trim();
        if (!id) return;
        setConfirmDialog({
            show: true,
            title: t('Delete share', '删除分享'),
            message: t(
                'After deletion the share link can no longer be imported. Your local knowledge base is not deleted.',
                '删除后，分享链接将不可再导入，但不会删除你本地知识库中的内容。',
            ),
            onConfirm: async () => {
                setConfirmDialog(prev => ({ ...prev, show: false }));
                setMySharesError('');
                try {
                    await KnowledgeDeleteHubShare({
                        hub_url: hubShareForm.hubURL.trim(),
                        hub_token: hubShareForm.hubToken.trim(),
                        knowledge_id: id,
                    });
                    notifySuccess(t('Share deleted.', '分享已删除。'));
                    await loadMyHubShares();
                } catch (err: any) {
                    setMySharesError(err?.message || String(err));
                }
            },
        });
    };

    const visibilityLabel = (scope: string) => {
        switch (String(scope || '').toLowerCase()) {
            case 'public': return t('Public internet', '全网公开');
            case 'hub': return t('This Hub public', '本 Hub 公开');
            case 'tenant': return t('Tenant public', '本租户公开');
            case 'private': return t('Only me', '仅自己');
            case 'users': return t('User list', '用户列表可见');
            default: return scope || '-';
        }
    };

    const truncateShareDescription = (text: string, max = 80) => {
        const s = String(text || '').trim();
        if (!s) return '';
        const runes = Array.from(s);
        return runes.length > max ? `${runes.slice(0, max).join('')}…` : s;
    };

    const openKnowledgeSyncCardStore = async () => {
        const storeURL = buildHubCardStoreURL(syncForm.hubURL, syncForm.tenantID, syncForm.email, syncForm.hubToken, syncForm.hubCenterURL, syncForm.hubID, undefined, syncForm.tenantName);
        if (!storeURL) {
            setError(t('Hub URL is required to open the maclaw service card store.', '需要 Hub 地址才能打开 maclaw 服务卡商店。'));
            return;
        }
        await OpenSystemUrl(storeURL);
    };

    const syncPayload = () => ({
        hub_url: syncForm.hubURL.trim(),
        hub_token: syncForm.hubToken.trim(),
        tenant_id: syncForm.tenantID.trim(),
        email: syncForm.email.trim(),
        password: syncForm.password,
        conflict_strategy: syncForm.conflictStrategy,
    });

    const normalizeSyncStatusWithLocalService = (status: any, serviceStatus = hubLLMServiceStatus) => {
        if (!status || typeof status !== 'object') return status || null;
        if (!knowledgeSyncLocalOfficialActive(serviceStatus)) return status;
        return {
            ...status,
            service_status: 'official_active',
            readonly_reason: '',
            retention_days: 0,
            limit_bytes: 500 * 1024 * 1024,
            expires_at: '',
            message: t('maclaw official service is active: sync data has no fixed expiry while the service is valid, with a 500MB server space limit.', 'maclaw 官方服务有效中：同步数据不设固定有效期，服务器空间上限 500MB。'),
        };
    };

    const refreshKnowledgeSyncStatus = async () => {
        await runTask('syncStatus', async () => {
            const [result, serviceStatus] = await Promise.all([
                KnowledgeSyncStatus(syncPayload()),
                GetHubLLMServiceStatus().catch(() => null),
            ]);
            setHubLLMServiceStatus(serviceStatus || null);
            const normalized = normalizeSyncStatusWithLocalService(result, serviceStatus);
            setSyncStatus(normalized || null);
            return normalized;
        }, { successMessage: false });
    };

    const ensureKnowledgeSyncCanWrite = (latestStatus: any) => {
        if (String(latestStatus?.service_status || '') === 'official_expired') {
            throw new Error(t('Upload failed: maclaw official service has expired. Renew maclaw official service before updating sync data.', '上传失败：当前服务已过期，请续费 maclaw 官方服务后再更新同步数据。'));
        }
    };

    const uploadKnowledgeSyncPackage = async (latestStatus: any, verifyExistingPassword: boolean, serviceStatus = hubLLMServiceStatus) => {
        ensureKnowledgeSyncCanWrite(latestStatus);
        if (latestStatus?.has_package && verifyExistingPassword) {
            await KnowledgeSyncVerifyPassword(syncPayload());
        } else if (!latestStatus?.has_package && syncForm.password !== syncForm.passwordConfirm) {
            throw new Error(t('The two sync passwords do not match.', '两次输入的同步密码不一致。'));
        }
        const result = await KnowledgeSyncUpload(syncPayload());
        const normalized = normalizeSyncStatusWithLocalService(result, serviceStatus);
        setSyncStatus(normalized || null);
        return normalized;
    };

    const syncKnowledgeNow = async () => {
        if (!syncForm.password.trim()) {
            setError(t('Enter the sync password before syncing. Hub never stores this password.', '同步前请输入同步密码。Hub 不会保存该密码。'));
            return;
        }
        if (syncReadonly && !knowledgeSyncLocalOfficialActive(hubLLMServiceStatus)) {
            setError(t('Upload failed: maclaw official service has expired. Renew maclaw official service before updating sync data.', '上传失败：当前服务已过期，请续费 maclaw 官方服务后再更新同步数据。'));
            return;
        }
        await runTask('syncNow', async () => {
            setSyncConflictResult(null);
            const [remoteStatus, serviceStatus] = await Promise.all([
                KnowledgeSyncStatus(syncPayload()),
                GetHubLLMServiceStatus().catch(() => null),
            ]);
            setHubLLMServiceStatus(serviceStatus || null);
            const latestStatus = normalizeSyncStatusWithLocalService(remoteStatus, serviceStatus);
            setSyncStatus(latestStatus || null);
            ensureKnowledgeSyncCanWrite(latestStatus);
            if (!latestStatus?.has_package) {
                const uploadResult = await uploadKnowledgeSyncPackage(latestStatus, false, serviceStatus);
                notifySuccess(t('Knowledge sync completed.', '知识库同步完成。'));
                return uploadResult;
            }
            await KnowledgeSyncVerifyPassword(syncPayload());
            const checkResult = await KnowledgeSyncDownload({ ...syncPayload(), conflict_strategy: 'check' });
            setSyncStatus(normalizeSyncStatusWithLocalService(checkResult || latestStatus, serviceStatus) || null);
            if (checkResult?.requires_resolution) {
                setSyncConflictResult(checkResult);
                notifySuccess(t('Knowledge sync checked. Resolve conflicts to continue syncing.', '已检查同步内容，请先处理冲突后继续同步。'));
                return checkResult;
            }
            await KnowledgeSyncDownload({ ...syncPayload(), conflict_strategy: 'import' });
            const uploadResult = await KnowledgeSyncUpload(syncPayload());
            setSyncStatus(normalizeSyncStatusWithLocalService(uploadResult, serviceStatus) || null);
            notifySuccess(t('Knowledge sync completed.', '知识库同步完成。'));
            return uploadResult;
        }, {
            refreshSources: true,
            refreshHealth: true,
            successMessage: false,
        });
    };

    const resolveKnowledgeSyncAndContinue = async (conflictStrategy: 'skip' | 'import') => {
        if (!syncForm.password.trim()) {
            setError(t('Enter the sync password before syncing. Hub never stores this password.', '同步前请输入同步密码。Hub 不会保存该密码。'));
            return;
        }
        await runTask('syncResolve', async () => {
            const serviceStatus = await GetHubLLMServiceStatus().catch(() => hubLLMServiceStatus);
            setHubLLMServiceStatus(serviceStatus || null);
            const latestStatus = normalizeSyncStatusWithLocalService(syncStatus, serviceStatus);
            ensureKnowledgeSyncCanWrite(latestStatus);
            await KnowledgeSyncVerifyPassword(syncPayload());
            const importResult = await KnowledgeSyncDownload({ ...syncPayload(), conflict_strategy: conflictStrategy });
            setSyncStatus(normalizeSyncStatusWithLocalService(importResult, serviceStatus) || null);
            const uploadResult = await KnowledgeSyncUpload(syncPayload());
            setSyncStatus(normalizeSyncStatusWithLocalService(uploadResult, serviceStatus) || null);
            setSyncConflictResult(null);
            return uploadResult;
        }, {
            refreshSources: true,
            refreshHealth: true,
            successMessage: t('Knowledge sync completed.', '知识库同步完成。'),
        });
    };

    const deleteKnowledgeSync = async () => {
        await runTask('syncDelete', async () => {
            const result = await KnowledgeSyncDelete(syncPayload());
            setSyncStatus(result || null);
            return result;
        }, {
            successMessage: t('Cloud sync package deleted.', '云端同步包已删除。'),
        });
    };

    const toggleExportSource = (sourceID?: string) => {
        const id = String(sourceID || '').trim();
        if (!id) return;
        setExportSelection(prev => ({ ...prev, [id]: !prev[id] }));
    };

    const setExportGroupSelection = (group: ExportSourceGroup, selected: boolean) => {
        setExportSelection(prev => {
            const next = { ...prev };
            for (const source of group.sources) {
                const id = String(source.id || '').trim();
                if (id) next[id] = selected;
            }
            return next;
        });
    };

    const clearExportSelection = () => setExportSelection({});

    const copyText = async (value: string) => {
        const text = String(value || '').trim();
        if (!text) return;
        try {
            await navigator.clipboard.writeText(text);
            notifySuccess(t('Copied.', '已复制。'));
        } catch {
            setError(t('Copy failed. Select and copy the link manually.', '复制失败，请手动选择并复制链接。'));
        }
    };

    const loadSources = async () => {
        await runTask('sources', async () => {
            const nextSources = await refreshSourceList();
            return { count: nextSources.length };
        });
    };

    const loadStructuredCatalog = async () => {
        await runTask('structuredCatalog', async () => {
            const payload: any = {
                limit: 500,
                include_disabled: searchForm.includeDisabled,
            };
            if (searchForm.sourceID.trim()) payload.source_ids = [searchForm.sourceID.trim()];
            if (structuredSearchForm.sheetName.trim()) payload.sheet_names = [structuredSearchForm.sheetName.trim()];
            const result = await KnowledgeStructuredCatalog(payload);
            setStructuredCatalog(result || null);
            return { tables: result?.count || 0 };
        }, { successMessage: false });
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
        }), { refreshSources: true, refreshHealth: true, successMessage: false });
        if (result) {
            if (result.save_status === 'duplicate') {
                notifySuccess(t('Content already exists in knowledge base (updated).', '内容已存在于知识库中（已更新）。'));
            } else {
                notifySuccess(t('Text saved to knowledge base successfully.', '文本已成功保存到知识库。'));
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
            : KnowledgeSaveURLs(urls, urlForm.saveScope, urlForm.topicHint.trim(), urlForm.distillMode, parseLabelList(urlForm.labels), urlForm.autoLabels), { refreshSources: true, refreshHealth: true, successMessage: false });
        if (result) {
            if (urls.length === 1) {
                // Single URL: result is a Source with save_status
                if (result.save_status === 'duplicate') {
                    notifySuccess(t('URL already exists in knowledge base (updated).', 'URL 已存在于知识库中（已更新）。'));
                } else {
                    notifySuccess(t('URL saved to knowledge base successfully.', 'URL 已成功保存到知识库。'));
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
                    msg = t(`All ${skipped} URL(s) were duplicates in this batch (skipped).`, `本批次中全部 ${skipped} 个 URL 重复（已跳过）。`);
                } else if (duplicates > 0 && fresh === 0) {
                    msg = t(`All ${duplicates} URL(s) already exist in knowledge base (updated).`, `全部 ${duplicates} 个 URL 已存在于知识库中（已更新）。`);
                } else if (duplicates > 0) {
                    msg = t(`${fresh} URL(s) saved, ${duplicates} already existed (updated).`, `${fresh} 个 URL 已保存，${duplicates} 个已存在（已更新）。`);
                } else {
                    msg = t(`${saved} URL(s) saved to knowledge base successfully.`, `${saved} 个 URL 已成功保存到知识库。`);
                }
                if (failed > 0) {
                    msg += t(` ${failed} failed.`, ` ${failed} 个失败。`);
                }
                notifySuccess(msg);
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
        const structuredHasColumnFilter = !!structuredSearchForm.columnName.trim() && (
            !!structuredSearchForm.columnValue.trim() ||
            !!structuredSearchForm.numberMin.trim() ||
            !!structuredSearchForm.numberMax.trim() ||
            !!structuredSearchForm.dateStart.trim() ||
            !!structuredSearchForm.dateEnd.trim()
        );
        if (searchMode === 'semantic' && !searchForm.query.trim()) {
            setError(t('Search query is required.', '请输入搜索问题。'));
            return;
        }
        if (searchMode === 'structured' && !searchForm.query.trim() && !structuredHasColumnFilter) {
            setError(t('Enter a query or at least one table column filter.', '请输入问题，或至少填写一个表格列筛选条件。'));
            return;
        }
        await runTask('search', async () => {
            const payload: any = {
                limit: normalizeKnowledgeSourceLimit(searchForm.limit, 20, 200),
                include_disabled: searchForm.includeDisabled,
            };
            let results: any[] = [];
            let facetResult: any = null;
            if (searchMode === 'structured') {
                applyKnowledgeStructuredSearchPayload(payload, { ...searchForm, ...structuredSearchForm });
                results = await KnowledgeSearchStructured(payload);
                facetResult = null;
            } else {
                payload.query = searchForm.query.trim();
                applyKnowledgeSearchFilterPayload(payload, searchForm);
                [results, facetResult] = await Promise.all([
                    KnowledgeSearch(payload),
                    KnowledgeSearchFacets(payload),
                ]);
            }
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
            message: t('This will permanently delete ALL knowledge base content (all imported documents, URLs, cards, facts). This cannot be undone.\n\nAre you sure?',
                '此操作将永久删除知识库中的所有内容（所有导入的文档、URL、卡片、事实），且无法恢复。\n\n确定要继续吗？'),
            onConfirm: () => {
                setConfirmDialog({
                    show: true,
                    title: t('Final Confirmation', '最终确认'),
                    message: t('FINAL CONFIRMATION: All knowledge base data will be permanently erased. Proceed?',
                        '最终确认：知识库所有数据将被永久清除。确认执行？'),
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

    useEffect(() => {
        if (searchMode !== 'structured' || structuredCatalog) return;
        void loadStructuredCatalog();
    }, [searchMode, structuredCatalog]);

    const hubShareSourceSummary = (hubShareResult?.source_summary || hubShareResult?.raw?.source_summary || {}) as Record<string, any>;
    const hubShareWarnings = Array.isArray(hubShareSourceSummary.warnings)
        ? hubShareSourceSummary.warnings.map((item: any) => String(item || '').trim()).filter(Boolean)
        : [];
    const hubShareContentSources = Number(hubShareResult?.content_sources || hubShareSourceSummary.content_sources || 0);
    const formatSyncBytes = (value: any) => {
        const bytes = Number(value || 0);
        if (!Number.isFinite(bytes) || bytes <= 0) return '-';
        if (bytes >= 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
        return `${Math.max(1, Math.round(bytes / 1024))} KB`;
    };
    const syncServiceStatus = String(syncStatus?.service_status || 'normal');
    const syncReadonly = syncServiceStatus === 'official_expired' && !knowledgeSyncLocalOfficialActive(hubLLMServiceStatus);
    const syncMessage = syncStatus?.message || (syncReadonly
        ? t('maclaw official service has expired: you can still download existing sync data, but cannot upload a new version. If service is not restored for 7 consecutive days, sync data will be deleted automatically.', 'maclaw 官方服务已过期：你仍可下载已有同步数据，但无法上传新版本。若连续 7 天未恢复服务，同步数据将自动删除。')
        : syncServiceStatus === 'official_active'
            ? t('maclaw official service is active: sync data has no fixed expiry while the service is valid, with a 500MB server space limit.', 'maclaw 官方服务有效中：同步数据不设固定有效期，服务器空间上限 500MB。')
            : t('Temporary sync: sync data is deleted after 7 days, with a 100MB server space limit. Upgrade maclaw official service for 500MB and no fixed expiry while valid.', '当前为临时同步：同步数据将在 7 天后自动删除，服务器空间上限 100MB。升级 maclaw 官方服务后，可获得 500MB 同步空间，并在服务有效期内不设固定有效期。'));
    const syncExpiryLine = syncStatus?.expires_at
        ? `${t('Expires', '到期时间')}: ${syncStatus.expires_at}`
        : syncStatus?.has_package && syncServiceStatus === 'official_active'
            ? t('No fixed expiry while official service is valid.', '官方服务有效期内不设固定有效期。')
            : syncStatus?.has_package && syncReadonly
                ? t('Download is still available now. Renew within 7 days to keep update capability and avoid automatic deletion.', '当前仍可下载。请在 7 天内续费以恢复更新能力并避免自动删除。')
                : syncStatus?.has_package
                    ? t('Temporary package expires 7 days after upload.', '临时同步包会在上传 7 天后到期。')
                    : t('No cloud sync package yet.', '暂无云端同步包。');

    return (
        <>
        <section className="knowledge-panel">
            <div className="knowledge-panel-header">
                <div>
                    <h2 className="knowledge-panel-title">{t('Knowledge Base', '知识库')}</h2>
                    <p className="knowledge-panel-subtitle">{t('Ingest, search, inspect, and maintain local knowledge sources.', '导入、检索、查看并维护本地知识来源。')}</p>
                </div>
                <div className="knowledge-panel-actions">
                    <button type="button" className="knowledge-button knowledge-button--secondary" onClick={refresh} disabled={loading}>
                        {loading ? t('Refreshing...', '刷新中...') : t('Refresh', '刷新')}
                    </button>
                    <button type="button" className="knowledge-button knowledge-button--danger" onClick={handleClearAll} disabled={!!busy}>
                        {t('Clear All', '清空')}
                    </button>
                </div>
            </div>
            {error ? <div className="knowledge-alert knowledge-alert--error">{error}</div> : null}
            <div className="settings-subtab-bar settings-subtab-bar--knowledge" role="tablist" aria-label={t('Knowledge sections', '知识库分区')}>
                {knowledgeTabs.map(tab => (
                    <button
                        key={tab.id}
                        id={`knowledge-tab-${tab.id}`}
                        className="settings-subtab-button"
                        data-active={activeTab === tab.id ? 'true' : undefined}
                        type="button"
                        role="tab"
                        aria-selected={activeTab === tab.id}
                        aria-controls={`knowledge-panel-${tab.id}`}
                        aria-label={tab.label}
                        title={tab.desc}
                        onClick={() => setActiveTab(tab.id)}
                    >
                        <span className="settings-subtab-button__label">{tab.label}</span>
                    </button>
                ))}
            </div>

            {activeTab === 'overview' && (
                <div className="knowledge-stack" role="tabpanel" id="knowledge-panel-overview" aria-labelledby="knowledge-tab-overview">
                    <div className="knowledge-stats-grid">
                        <Stat label={t('Status', '状态')} value={summary?.status || 'Unknown'} />
                        <Stat label={t('Score', '评分')} value={summary?.score ?? 0} />
                        <Stat label={t('Quality', '质量')} value={summary?.qualityAvgScore ?? 0} />
                        <Stat label={t('Actions', '动作')} value={summary?.actions?.length ?? 0} />
                    </div>
                    <PanelBlock title={t('Chat auto-recall', '对话自动召回')}>
                        <span className="knowledge-muted-line">
                            {t(
                                'When enabled, relevant knowledge is silently injected into the AI system prompt on each user message. Manual knowledge_search tools stay available either way.',
                                '开启后，每条用户消息会静默检索知识库并注入系统提示。关闭后仅停用自动召回，knowledge_search 等工具仍可用。',
                            )}
                        </span>
                        <div className="knowledge-checkbox-row" style={{ marginTop: 10 }}>
                            <label className="knowledge-checkbox">
                                <input
                                    type="checkbox"
                                    checked={autoRecallEnabled}
                                    disabled={autoRecallSaving}
                                    onChange={e => {
                                        const next = e.target.checked;
                                        setAutoRecallEnabled(next);
                                        void saveAutoRecallSettings(next, autoRecallMinScore);
                                    }}
                                />
                                {t('Enable knowledge auto-recall', '启用知识库自动召回')}
                            </label>
                        </div>
                        <div className="knowledge-compact-grid" style={{ marginTop: 10, maxWidth: 360 }}>
                            <label className="knowledge-field">
                                <span className="knowledge-field-label">{t('Min score (optional)', '最低分数（可选）')}</span>
                                <input
                                    className="knowledge-input"
                                    type="number"
                                    min={0}
                                    max={10}
                                    step={0.1}
                                    placeholder={t('Default 0.3', '默认 0.3')}
                                    value={autoRecallMinScore}
                                    disabled={autoRecallSaving || !autoRecallEnabled}
                                    onChange={e => setAutoRecallMinScore(e.target.value)}
                                    onBlur={() => {
                                        if (!autoRecallEnabled) return;
                                        void saveAutoRecallSettings(true, autoRecallMinScore);
                                    }}
                                />
                            </label>
                        </div>
                        <span className="knowledge-field-hint">
                            {t(
                                'Higher min score = fewer, stricter injections. Leave empty for the default 0.3 threshold.',
                                '最低分越高，注入越严格、越少。留空使用默认阈值 0.3。',
                            )}
                        </span>
                    </PanelBlock>
                    <div className="knowledge-two-column">
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
                            <div className="knowledge-list">
                                {(summary?.actions || []).map((action: any, index: number) => (
                                    <div key={`${action.kind || action.tool || index}`} className="knowledge-row">
                                        <div className="knowledge-row-main">
                                            <strong>{action.title || action.kind || action.tool || t('Action', '动作')}</strong>
                                            <span className="knowledge-muted-line">{action.description || [action.kind || action.tool, action.count ? `${action.count} sources` : ''].filter(Boolean).join(' · ')}</span>
                                        </div>
                                        {knowledgeHealthActionExecutable(action) ? (
                                            <button type="button" className="knowledge-button knowledge-button--secondary" disabled={!!busy} onClick={() => executeQualityAction(action)}>{t('Preview', '预览')}</button>
                                        ) : <span className="knowledge-chip knowledge-chip--badge">{knowledgeHealthActionManualLabel(action)}</span>}
                                    </div>
                                ))}
                            </div>
                        ) : <div className="knowledge-empty">{t('No maintenance action is required.', '暂无需要执行的维护动作。')}</div>}
                    </PanelBlock>
                </div>
            )}

            {(activeTab === 'ingest' || activeTab === 'export') && (
                <div className="knowledge-stack" role="tabpanel" id={`knowledge-panel-${activeTab}`} aria-labelledby={`knowledge-tab-${activeTab}`}>
                <PanelBlock title={activeTab === 'export' ? t('Export / Share Knowledge Base', '知识库导出 / 分享') : t('Import Knowledge Base', '知识库导入')}>
                    {activeTab === 'export' && (
                    <>
                        <div className="knowledge-exchange-box knowledge-export-share-box">
                            <div className="knowledge-export-head">
                                <div>
                                    <strong>{t('Choose knowledge items', '选择知识条目')}</strong>
                                    <span className="knowledge-muted-line">{t('Select readable knowledge sources once, then export them to a file or share them to Hub. No selection means the whole knowledge base.', '先统一选择知识条目，然后导出为文件或分享到 Hub；不选择则表示整库。')}</span>
                                </div>
                                <div className="knowledge-inline-actions">
                                    <span className="knowledge-chip knowledge-chip--badge">{exportSelectionLabel}</span>
                                    <button type="button" className="knowledge-button knowledge-button--secondary" disabled={exportListLoading || !!busy} onClick={loadExportSelectionData}>
                                        {exportListLoading ? t('Loading...', '加载中...') : t('Refresh List', '刷新列表')}
                                    </button>
                                    <button type="button" className="knowledge-button knowledge-button--secondary" disabled={!selectedExportSourceIDs.length || !!busy} onClick={clearExportSelection}>
                                        {t('Clear Selection', '清空选择')}
                                    </button>
                                </div>
                            </div>
                            <div className="knowledge-export-source-list" aria-label={t('Knowledge item selection for export and sharing', '用于导出和分享的知识条目选择')}>
                                {exportListLoading ? (
                                    <div className="knowledge-empty">{t('Loading existing knowledge sources...', '正在加载现有知识来源...')}</div>
                                ) : exportSources === null && exportListAttempted ? (
                                    <div className="knowledge-empty">{t('Source list could not be loaded. Use Refresh List to try again.', '来源列表未能加载，请点击刷新列表重试。')}</div>
                                ) : exportSources === null ? (
                                    <div className="knowledge-empty">{t('Open the export tab to load existing knowledge sources.', '打开导出页后加载现有知识来源。')}</div>
                                ) : exportSourceCount ? exportSourceGroups.map(group => {
                                    const selectable = group.sources.filter(source => source.id);
                                    const selectedCount = selectable.filter(source => !!exportSelection[String(source.id)]).length;
                                    const allSelected = selectable.length > 0 && selectedCount === selectable.length;
                                    const someSelected = selectedCount > 0 && !allSelected;
                                    return (
                                        <div key={group.id} className="knowledge-export-group">
                                            <div className="knowledge-export-group__head">
                                                <label className="knowledge-checkbox knowledge-export-group__title">
                                                    <input
                                                        type="checkbox"
                                                        checked={allSelected}
                                                        disabled={!selectable.length}
                                                        ref={input => { if (input) input.indeterminate = someSelected; }}
                                                        onChange={event => setExportGroupSelection(group, event.target.checked)}
                                                    />
                                                    <span>{group.label}</span>
                                                </label>
                                                <span className="knowledge-muted-line">{selectedCount}/{selectable.length} {t('selected', '已选')}</span>
                                            </div>
                                            <span className="knowledge-muted-line">{group.meta}</span>
                                            <div className="knowledge-export-source-items">
                                                {group.sources.map(source => {
                                                    const id = String(source.id || '').trim();
                                                    return (
                                                        <label key={id || source.uri || source.title} className="knowledge-export-source-row">
                                                            <input type="checkbox" checked={!!id && !!exportSelection[id]} disabled={!id} onChange={() => toggleExportSource(id)} />
                                                            <span className="knowledge-export-source-row__main">
                                                                <strong>{knowledgeExportSourceLabel(source)}</strong>
                                                                <span className="knowledge-muted-line">{knowledgeExportSourceMeta(source)}</span>
                                                            </span>
                                                        </label>
                                                    );
                                                })}
                                            </div>
                                        </div>
                                    );
                                }) : (
                                    <div className="knowledge-empty">{t('No knowledge sources are available to export yet.', '当前还没有可导出的知识来源。')}</div>
                                )}
                            </div>
                            <div className="knowledge-export-action-panel">
                                <div className="knowledge-hub-share-head">
                                    <div>
                                        <strong>{t('Choose an action', '选择操作')}</strong>
                                        <span className="knowledge-muted-line">{t('Both actions use the same selected items above.', '两个操作都会使用上方同一份已选条目。')}</span>
                                    </div>
                                    <button type="button" className="knowledge-button knowledge-button--secondary" disabled={!!busy} onClick={openHubKnowledgeShares}>
                                        {t('View Shares', '查看分享')}
                                    </button>
                                </div>
                                <div className="knowledge-checkbox-row">
                                    <label className="knowledge-checkbox"><input type="checkbox" checked={exchangeForm.redactSensitive} onChange={event => setExchangeForm({ ...exchangeForm, redactSensitive: event.target.checked })} /> {t('Redact sensitive fields', '脱敏敏感字段')}</label>
                                </div>
                                <div className="knowledge-export-format-row" role="radiogroup" aria-label={t('Export format', '导出格式')}>
                                    <label className="knowledge-checkbox">
                                        <input
                                            type="radio"
                                            name="knowledge-export-format"
                                            checked={exportFormat === 'jsonl'}
                                            onChange={() => setExportFormat('jsonl')}
                                        />
                                        {t('Snapshot JSONL (full restore)', '快照 JSONL（完整还原）')}
                                    </label>
                                    <label className="knowledge-checkbox">
                                        <input
                                            type="radio"
                                            name="knowledge-export-format"
                                            checked={exportFormat === 'package'}
                                            onChange={() => setExportFormat('package')}
                                        />
                                        {t('Exchange package JSON (share/import)', '交换知识包 JSON（分享/导入）')}
                                    </label>
                                </div>
                                <span className="knowledge-field-hint">
                                    {exportFormat === 'package'
                                        ? t('Package JSON matches Hub share packages and is easier for agents to re-import. It may truncate large source bodies.', '知识包 JSON 与 Hub 分享包一致，便于 Agent 再导入；大正文可能被截断。')
                                        : t('JSONL is a full local snapshot (sources/nodes/cards/facts) for backup and replace-all restore.', 'JSONL 是完整本地快照（来源/节点/卡片/事实），适合备份与整库还原。')}
                                </span>
                                <div className="knowledge-action-buttons">
                                    <button type="button" className="knowledge-button knowledge-button--secondary" disabled={!!busy} onClick={exportKnowledgeSnapshot}>
                                        {busy === 'exportSnapshot'
                                            ? t('Exporting...', '导出中...')
                                            : selectedExportSourceIDs.length
                                                ? t('Export Selected to File', '导出已选到文件')
                                                : t('Export Full to File', '导出整库到文件')}
                                    </button>
                                    <button type="button" ref={hubShareButtonRef} className="knowledge-button knowledge-button--primary" disabled={!!busy} onClick={openHubShareDialog}>
                                        {selectedExportSourceIDs.length
                                            ? t('Share Selected to Hub', '分享已选到 Hub')
                                            : t('Share Full to Hub', '分享整库到 Hub')}
                                    </button>
                                </div>
                            </div>
                            {exportResult ? (
                                <div className="knowledge-export-result" role="status">
                                    <div className="knowledge-export-result__head">
                                        <strong>{t('Export successful', '导出成功')}</strong>
                                        <button type="button" className="knowledge-modal-close" aria-label={t('Dismiss', '关闭')} onClick={() => setExportResult(null)}>×</button>
                                    </div>
                                    <div className="knowledge-export-result__path">
                                        <code title={exportResult.output_path || ''}>{exportResult.output_path || '-'}</code>
                                    </div>
                                    <div className="knowledge-export-result__stats">
                                        {[
                                            exportResult.format ? `${t('Format', '格式')} ${exportResult.format}` : '',
                                            exportResult.sources != null ? `${exportResult.sources} ${t('sources', '来源')}` : '',
                                            exportResult.nodes != null ? `${exportResult.nodes} ${t('nodes', '节点')}` : '',
                                            exportResult.cards != null ? `${exportResult.cards} ${t('cards', '卡片')}` : '',
                                            exportResult.facts != null ? `${exportResult.facts} ${t('facts', '事实')}` : '',
                                            formatExportBytes(Number(exportResult.bytes || 0)),
                                            exportResult.redact_sensitive ? t('redacted', '已脱敏') : '',
                                        ].filter(Boolean).join(' · ')}
                                    </div>
                                    <div className="knowledge-inline-actions knowledge-export-result__actions">
                                        <button type="button" className="knowledge-button knowledge-button--secondary" onClick={() => copyText(exportResult.output_path)}>
                                            {t('Copy path', '复制路径')}
                                        </button>
                                        <button type="button" className="knowledge-button knowledge-button--secondary" onClick={() => void revealExportPath(exportResult.output_path)}>
                                            {t('Show in folder', '打开所在文件夹')}
                                        </button>
                                        <button type="button" className="knowledge-button knowledge-button--primary" disabled={!!busy} onClick={openHubShareDialog}>
                                            {t('Share to Hub', '发布到 Hub')}
                                        </button>
                                    </div>
                                </div>
                            ) : null}
                            {showHubShareDialog ? (
                                <div className="knowledge-share-modal-overlay" role="presentation">
                                    <section ref={hubShareDialogRef} className="knowledge-share-modal" role="dialog" aria-modal="true" aria-labelledby="knowledge-share-dialog-title" tabIndex={-1}>
                                        <div className="knowledge-share-modal__header">
                                            <div>
                                                <strong id="knowledge-share-dialog-title">{t('Hub share settings', 'Hub 分享设置')}</strong>
                                                <span className="knowledge-muted-line">{t('Set visibility, expiry, and description before publishing the Hub share.', '发布 Hub 分享前设置可见范围、有效期和描述。')}</span>
                                            </div>
                                            <button type="button" className="knowledge-modal-close" aria-label={t('Close Hub share settings', '关闭 Hub 分享设置')} disabled={busy === 'shareToHub'} onClick={() => setShowHubShareDialog(false)}>×</button>
                                        </div>
                                        <div className="knowledge-share-modal__body">
                                            <div className="knowledge-compact-grid knowledge-hub-share-grid">
                                                <input className="knowledge-input" value={hubShareForm.hubURL} onChange={event => setHubShareForm({ ...hubShareForm, hubURL: event.target.value })} placeholder={t('Hub URL (uses configured Hub if empty)', 'Hub 地址（为空则使用已配置 Hub）')} />
                                                <input className="knowledge-input" value={hubShareForm.title} onChange={event => setHubShareForm({ ...hubShareForm, title: event.target.value })} placeholder={t('Share title (optional)', '分享标题（可选）')} />
                                                <select className="knowledge-input" value={hubShareForm.visibilityScope} onChange={event => setHubShareForm({ ...hubShareForm, visibilityScope: event.target.value })} aria-describedby="knowledge-hub-share-visibility-help">
                                                    <option value="hub">{t('This Hub public', '本 Hub 公开')}</option>
                                                    <option value="public">{t('Public internet', '全网公开')}</option>
                                                    <option value="tenant">{t('Tenant public', '本租户公开')}</option>
                                                    <option value="private">{t('Only me', '仅自己')}</option>
                                                    <option value="users">{t('User list', '用户列表可见')}</option>
                                                </select>
                                                <select className="knowledge-input" value={hubShareForm.ttl} onChange={event => setHubShareForm({ ...hubShareForm, ttl: event.target.value })}>
                                                    <option value="7d">{t('7 days (default)', '7 天（默认）')}</option>
                                                    <option value="month">{t('1 month', '1 个月')}</option>
                                                    <option value="year">{t('1 year', '1 年')}</option>
                                                    <option value="permanent">{t('Permanent', '永久')}</option>
                                                </select>
                                                <input className="knowledge-input" value={hubShareForm.visibilityUsers} onChange={event => setHubShareForm({ ...hubShareForm, visibilityUsers: event.target.value })} placeholder={t('Visible users, emails or IDs', '可见用户，邮箱或 ID')} disabled={hubShareForm.visibilityScope !== 'users'} />
                                            </div>
                                            <span id="knowledge-hub-share-visibility-help" className="knowledge-field-hint">
                                                {visibilityScopeHint(hubShareForm.visibilityScope)}
                                            </span>
                                            <details className="knowledge-advanced-details">
                                                <summary className="knowledge-details-summary">{t('Advanced authentication', '高级认证')}</summary>
                                                <input className="knowledge-input" type="password" value={hubShareForm.hubToken} onChange={event => setHubShareForm({ ...hubShareForm, hubToken: event.target.value })} placeholder={t('Hub token override (optional)', 'Hub 令牌覆盖（可选）')} />
                                                <span className="knowledge-field-hint">{t('Usually not needed. Leave empty to use the configured Hub token.', '通常不需要填写；留空会使用已配置的 Hub 令牌。')}</span>
                                            </details>
                                            <textarea
                                                ref={hubShareDescriptionRef}
                                                id="knowledge-hub-share-description"
                                                className="knowledge-input knowledge-textarea knowledge-textarea--compact"
                                                value={hubShareForm.description}
                                                onChange={event => {
                                                    setHubShareForm({ ...hubShareForm, description: event.target.value });
                                                    if (error) setError('');
                                                }}
                                                placeholder={t('Required knowledge description for readers and Hub management', '必填：给读者和 Hub 管理后台看的知识描述')}
                                                required
                                                aria-invalid={hubShareDescriptionMissing}
                                                aria-describedby="knowledge-hub-share-description-help"
                                            />
                                            <span id="knowledge-hub-share-description-help" className="knowledge-field-hint" data-state={hubShareDescriptionMissing ? 'warning' : 'default'}>
                                                {hubShareDescriptionMissing
                                                    ? t('Required before Hub sharing; visible to readers and Hub knowledge managers.', '分享到 Hub 前必填；读者和 Hub 知识管理后台都会看到。')
                                                    : t('This description helps readers and agents identify the exported knowledge.', '该描述用于帮助读者和 Agent 识别这次导出的知识。')}
                                            </span>
                                            {error ? <div className="knowledge-alert knowledge-alert--error knowledge-share-modal__alert" role="alert">{error}</div> : null}
                                            <div className="knowledge-checkbox-row">
                                                <label className="knowledge-checkbox"><input type="checkbox" checked={hubShareForm.includeDisabled} onChange={event => setHubShareForm({ ...hubShareForm, includeDisabled: event.target.checked })} /> {t('Include disabled sources in Hub share', '分享到 Hub 时包含禁用来源')}</label>
                                                {selectedDisabledShareCount > 0 ? (
                                                    <span className="knowledge-muted-line">{t(`${selectedDisabledShareCount} disabled selected source(s) will be skipped unless enabled here.`, `已选择的 ${selectedDisabledShareCount} 个禁用来源会被跳过，除非在此勾选包含禁用来源。`)}</span>
                                                ) : null}
                                            </div>
                                            <span className="knowledge-muted-line">{selectedExportSourceIDs.length
                                                ? t(`Sharing ${selectedExportSourceIDs.length} selected item(s). Change the scope in the source list behind this dialog.`, `将分享已选择的 ${selectedExportSourceIDs.length} 项；如需调整范围，请关闭弹窗后在来源列表中修改。`)
                                                : t('No item is selected, so sharing will include the whole knowledge base.', '未选择条目，因此将分享整库。')}</span>
                                        </div>
                                        <div className="knowledge-share-modal__footer">
                                            <button type="button" className="knowledge-button knowledge-button--secondary" disabled={busy === 'shareToHub'} onClick={() => setShowHubShareDialog(false)}>
                                                {t('Cancel', '取消')}
                                            </button>
                                            <button type="button" className="knowledge-button knowledge-button--primary" disabled={!!busy} aria-describedby={hubShareDescriptionMissing ? 'knowledge-hub-share-description-help' : undefined} onClick={shareKnowledgeToHub}>
                                                {busy === 'shareToHub'
                                                    ? t('Sharing...', '分享中...')
                                                    : selectedExportSourceIDs.length
                                                        ? t('Publish Selected to Hub', '确认分享已选到 Hub')
                                                        : t('Publish Full to Hub', '确认分享整库到 Hub')}
                                            </button>
                                        </div>
                                    </section>
                                </div>
                            ) : null}
                            {hubShareResult ? (
                                <div className="knowledge-share-result" role="status">
                                    <div className="knowledge-export-result__head">
                                        <strong>{t('Published to Hub', '已发布到 Hub')}</strong>
                                        <button type="button" className="knowledge-modal-close" aria-label={t('Dismiss', '关闭')} onClick={() => setHubShareResult(null)}>×</button>
                                    </div>
                                    <div><strong>{t('Knowledge ID', '知识 ID')}</strong><code>{hubShareResult.knowledge_id || '-'}</code></div>
                                    <div><strong>{t('Share link', '分享链接')}</strong><button type="button" className="knowledge-inline-link-button" onClick={() => copyText(hubShareResult.share_url)}>{hubShareResult.share_url || '-'}</button></div>
                                    <div><strong>{t('Agent import', 'Agent 导入')}</strong><button type="button" className="knowledge-inline-link-button" onClick={() => copyText(hubShareResult.agent_import)}>{hubShareResult.agent_import || '-'}</button></div>
                                    <span className="knowledge-muted-line">{[hubShareResult.source_count ? `${hubShareResult.source_count} sources` : '', hubShareContentSources ? `${hubShareContentSources} importable content sources` : '', hubShareResult.expires_at ? `expires ${hubShareResult.expires_at}` : t('No expiry', '无过期时间')].filter(Boolean).join(' · ')}</span>
                                    <div className="knowledge-inline-actions knowledge-export-result__actions">
                                        <button type="button" className="knowledge-button knowledge-button--secondary" disabled={!hubShareResult.knowledge_id} onClick={() => copyText(hubShareResult.knowledge_id)}>
                                            {t('Copy knowledge ID', '复制知识 ID')}
                                        </button>
                                        <button type="button" className="knowledge-button knowledge-button--secondary" disabled={!hubShareResult.share_url} onClick={() => copyText(hubShareResult.share_url)}>
                                            {t('Copy share link', '复制分享链接')}
                                        </button>
                                        <button type="button" className="knowledge-button knowledge-button--primary" disabled={!hubShareResult.share_url} onClick={() => void openShareURL(hubShareResult.share_url)}>
                                            {t('Open share page', '打开分享页')}
                                        </button>
                                        <button type="button" className="knowledge-button knowledge-button--secondary" onClick={() => void openHubKnowledgeShares()}>
                                            {t('View Shares', '查看分享')}
                                        </button>
                                    </div>
                                    {hubShareWarnings.length ? (
                                        <div className="knowledge-alert knowledge-alert--warning">
                                            <strong>{t('Share warnings', '分享警告')}</strong>
                                            <ul>{hubShareWarnings.slice(0, 5).map((warning, index) => <li key={`${warning}-${index}`}>{warning}</li>)}</ul>
                                        </div>
                                    ) : null}
                                </div>
                            ) : null}

                            <div className="knowledge-my-shares">
                                <div className="knowledge-export-head">
                                    <div>
                                        <strong>{t('My shares', '已分享')}</strong>
                                        <span className="knowledge-muted-line">
                                            {t(
                                                'Shares published under your Hub account. Deleting a share only revokes the link; local knowledge is kept.',
                                                '你在 Hub 上发布的知识分享。删除分享只会失效链接，不会删除本地知识库。',
                                            )}
                                        </span>
                                    </div>
                                    <div className="knowledge-inline-actions">
                                        {mySharesTotal > 0 ? (
                                            <span className="knowledge-chip knowledge-chip--badge">{mySharesTotal} {t('items', '条')}</span>
                                        ) : null}
                                        <button type="button" className="knowledge-button knowledge-button--secondary" disabled={mySharesLoading || !!busy} onClick={() => void loadMyHubShares()}>
                                            {mySharesLoading ? t('Loading...', '加载中...') : t('Refresh', '刷新')}
                                        </button>
                                        <button type="button" className="knowledge-button knowledge-button--secondary" disabled={!!busy} onClick={() => void openHubKnowledgeShares()}>
                                            {t('Open on Hub', '在 Hub 打开')}
                                        </button>
                                    </div>
                                </div>
                                {mySharesError ? (
                                    <div className="knowledge-alert knowledge-alert--warning" role="status">
                                        <strong>{t('Could not load shares', '无法加载已分享列表')}</strong>
                                        <span className="knowledge-muted-line">{mySharesError}</span>
                                        <span className="knowledge-muted-line">
                                            {t('Configure Hub URL and token under Sync, or use Open on Hub.', '请在同步页配置 Hub 地址与令牌，或使用「在 Hub 打开」。')}
                                        </span>
                                    </div>
                                ) : null}
                                {mySharesLoading && !myShares ? (
                                    <div className="knowledge-empty">{t('Loading your Hub shares...', '正在加载已分享条目...')}</div>
                                ) : myShares && myShares.length === 0 ? (
                                    <div className="knowledge-empty">
                                        {t('No shared knowledge yet. Export and share to Hub first.', '还没有已分享的知识条目。先导出并发布到 Hub。')}
                                    </div>
                                ) : myShares && myShares.length > 0 ? (
                                    <div className="knowledge-my-shares-list">
                                        {myShares.map((share: any) => {
                                            const id = String(share.knowledge_id || '').trim();
                                            const shareURL = String(share.share_url || '').trim();
                                            const isEditing = editingShareID === id && id !== '';
                                            return (
                                                <div key={id || shareURL} className="knowledge-my-share-row">
                                                    {isEditing ? (
                                                        <div className="knowledge-my-share-edit">
                                                            <strong>{t('Edit share', '编辑分享')}</strong>
                                                            <input
                                                                className="knowledge-input"
                                                                value={editShareForm.title}
                                                                onChange={e => setEditShareForm(prev => ({ ...prev, title: e.target.value }))}
                                                                placeholder={t('Share title (optional)', '分享标题（可选）')}
                                                                disabled={editShareSaving}
                                                            />
                                                            <textarea
                                                                className="knowledge-input knowledge-textarea knowledge-textarea--compact"
                                                                value={editShareForm.description}
                                                                onChange={e => setEditShareForm(prev => ({ ...prev, description: e.target.value }))}
                                                                placeholder={t('Required knowledge description for readers and Hub management', '必填：给读者和 Hub 管理后台看的知识描述')}
                                                                disabled={editShareSaving}
                                                                rows={3}
                                                            />
                                                            <select
                                                                className="knowledge-input"
                                                                value={editShareForm.visibilityScope}
                                                                onChange={e => setEditShareForm(prev => ({ ...prev, visibilityScope: e.target.value }))}
                                                                disabled={editShareSaving}
                                                                aria-describedby={`knowledge-edit-share-visibility-${id}`}
                                                            >
                                                                <option value="hub">{t('This Hub public', '本 Hub 公开')}</option>
                                                                <option value="public">{t('Public internet', '全网公开')}</option>
                                                                <option value="tenant">{t('Tenant public', '本租户公开')}</option>
                                                                <option value="private">{t('Only me', '仅自己')}</option>
                                                                <option value="users">{t('User list', '用户列表可见')}</option>
                                                            </select>
                                                            <span id={`knowledge-edit-share-visibility-${id}`} className="knowledge-field-hint">
                                                                {visibilityScopeHint(editShareForm.visibilityScope)}
                                                            </span>
                                                            {editShareForm.visibilityScope === 'users' ? (
                                                                <input
                                                                    className="knowledge-input"
                                                                    value={editShareForm.visibilityUsers}
                                                                    onChange={e => setEditShareForm(prev => ({ ...prev, visibilityUsers: e.target.value }))}
                                                                    placeholder={t('Visible users, emails or IDs', '可见用户，邮箱或 ID')}
                                                                    disabled={editShareSaving}
                                                                />
                                                            ) : null}
                                                            <div className="knowledge-my-share-row__actions">
                                                                <button type="button" className="knowledge-button knowledge-button--primary" disabled={editShareSaving || !editShareForm.description.trim()} onClick={() => void saveEditMyHubShare()}>
                                                                    {editShareSaving ? t('Saving...', '保存中...') : t('Save', '保存')}
                                                                </button>
                                                                <button type="button" className="knowledge-button knowledge-button--secondary" disabled={editShareSaving} onClick={() => setEditingShareID('')}>
                                                                    {t('Cancel', '取消')}
                                                                </button>
                                                            </div>
                                                        </div>
                                                    ) : (
                                                        <>
                                                            <div className="knowledge-my-share-row__main">
                                                                <strong>{share.title || id || t('Untitled share', '未命名分享')}</strong>
                                                                {share.description ? (
                                                                    <span className="knowledge-muted-line">{truncateShareDescription(share.description)}</span>
                                                                ) : null}
                                                                <span className="knowledge-muted-line">
                                                                    {[
                                                                        id,
                                                                        visibilityLabel(share.visibility_scope),
                                                                        share.status,
                                                                        share.source_count ? `${share.source_count} ${t('sources', '来源')}` : '',
                                                                        share.import_count != null ? `${share.import_count} ${t('imports', '导入')}` : '',
                                                                        share.updated_at ? `${t('Updated', '更新')} ${share.updated_at}` : '',
                                                                        share.expires_at ? `${t('Expires', '到期')} ${share.expires_at}` : '',
                                                                    ].filter(Boolean).join(' · ')}
                                                                </span>
                                                            </div>
                                                            <div className="knowledge-my-share-row__actions">
                                                                <button type="button" className="knowledge-button knowledge-button--secondary" disabled={!id} onClick={() => copyText(id)}>
                                                                    {t('Copy ID', '复制 ID')}
                                                                </button>
                                                                <button type="button" className="knowledge-button knowledge-button--secondary" disabled={!shareURL} onClick={() => copyText(shareURL)}>
                                                                    {t('Copy link', '复制链接')}
                                                                </button>
                                                                <button type="button" className="knowledge-button knowledge-button--secondary" disabled={!shareURL} onClick={() => void openShareURL(shareURL)}>
                                                                    {t('Open', '打开')}
                                                                </button>
                                                                <button type="button" className="knowledge-button knowledge-button--secondary" disabled={!id || !!busy || editShareSaving} onClick={() => openEditMyHubShare(share)}>
                                                                    {t('Edit', '编辑')}
                                                                </button>
                                                                <button type="button" className="knowledge-button knowledge-button--danger" disabled={!id || !!busy} onClick={() => void deleteMyHubShare(id)}>
                                                                    {t('Delete', '删除')}
                                                                </button>
                                                            </div>
                                                        </>
                                                    )}
                                                </div>
                                            );
                                        })}
                                    </div>
                                ) : null}
                            </div>
                        </div>
                    </>
                    )}
                    {activeTab === 'ingest' && (
                    <>
                        <div className="knowledge-exchange-box">
                            <strong>{t('Import whole knowledge base', '整库导入')}</strong>
                            <span className="knowledge-muted-line">{t('Choose a JSONL snapshot exported from Maclaw GUI. Dry run is enabled by default.', '选择从 Maclaw GUI 导出的 JSONL 快照。默认先预检查。')}</span>
                            <div className="knowledge-file-picker-row">
                                <input className="knowledge-input" value={exchangeForm.importPath} readOnly placeholder={t('No snapshot selected', '未选择快照文件')} />
                                <button type="button" className="knowledge-button knowledge-button--secondary" disabled={!!busy} onClick={chooseKnowledgeSnapshotImport}>{t('Choose', '选择')}</button>
                            </div>
                            <div className="knowledge-checkbox-row">
                                <label className="knowledge-checkbox"><input type="checkbox" checked={exchangeForm.dryRun} onChange={event => setExchangeForm({ ...exchangeForm, dryRun: event.target.checked })} /> {t('Dry run first', '先预检查')}</label>
                                <label className="knowledge-checkbox"><input type="checkbox" checked={exchangeForm.overwrite} onChange={event => setExchangeForm({ ...exchangeForm, overwrite: event.target.checked })} /> {t('Overwrite conflicts', '覆盖冲突')}</label>
                                <label className="knowledge-checkbox"><input type="checkbox" checked={exchangeForm.replaceAll} onChange={event => setExchangeForm({ ...exchangeForm, replaceAll: event.target.checked })} /> {t('Replace all', '替换整库')}</label>
                            </div>
                            <div className="knowledge-panel-actions">
                                <button type="button" className="knowledge-button knowledge-button--secondary" disabled={!!busy || !exchangeForm.importPath.trim()} onClick={() => importKnowledgeSnapshot(true)}>{t('Check Import', '检查导入')}</button>
                                <button type="button" className="knowledge-button knowledge-button--primary" disabled={!!busy || !exchangeForm.importPath.trim()} onClick={() => importKnowledgeSnapshot(false)}>
                                    {busy === 'importSnapshot' ? t('Importing...', '导入中...') : t('Import Snapshot', '导入快照')}
                                </button>
                            </div>
                        </div>
                        <div className="knowledge-exchange-box knowledge-hub-import-box">
                            <strong>{t('Import from Hub share', '从 Hub 分享导入')}</strong>
                            <span className="knowledge-muted-line">{t('Paste a readable share link or a knowledge ID. Dry run checks what can be imported before writing to the local knowledge base.', '粘贴可阅读分享链接或知识 ID。默认先预检查可导入内容，再写入本机知识库。')}</span>
                            <div className="knowledge-compact-grid knowledge-hub-share-grid">
                                <input className="knowledge-input" value={hubImportForm.shareLink} onChange={event => setHubImportForm({ ...hubImportForm, shareLink: event.target.value })} placeholder={t('Share link, e.g. https://hub/hub/knowledge/shares/kn_xxx', '分享链接，例如 https://hub/hub/knowledge/shares/kn_xxx')} />
                                <input className="knowledge-input" value={hubImportForm.knowledgeID} onChange={event => setHubImportForm({ ...hubImportForm, knowledgeID: event.target.value })} placeholder={t('Knowledge ID (when share link is not provided)', '知识 ID（未提供分享链接时使用）')} />
                                <input className="knowledge-input" value={hubImportForm.hubURL} onChange={event => setHubImportForm({ ...hubImportForm, hubURL: event.target.value })} placeholder={t('Hub URL (uses configured Hub if empty)', 'Hub 地址（为空则使用已配置 Hub）')} />
                                <input className="knowledge-input" type="password" value={hubImportForm.hubToken} onChange={event => setHubImportForm({ ...hubImportForm, hubToken: event.target.value })} placeholder={t('Hub token for private/tenant shares', '私有/租户分享所需 Hub 令牌')} />
                            </div>
                            <div className="knowledge-checkbox-row">
                                <label className="knowledge-checkbox"><input type="checkbox" checked={hubImportForm.dryRun} onChange={event => setHubImportForm({ ...hubImportForm, dryRun: event.target.checked })} /> {t('Dry run first', '先预检查')}</label>
                            </div>
                            <div className="knowledge-panel-actions">
                                <button type="button" className="knowledge-button knowledge-button--secondary" disabled={!!busy || (!hubImportForm.shareLink.trim() && !hubImportForm.knowledgeID.trim())} onClick={() => importHubShare(true)}>{t('Check Hub Share', '检查 Hub 分享')}</button>
                                <button type="button" className="knowledge-button knowledge-button--primary" disabled={!!busy || (!hubImportForm.shareLink.trim() && !hubImportForm.knowledgeID.trim())} onClick={() => importHubShare(false)}>
                                    {busy === 'importHubShare' ? t('Importing...', '导入中...') : t('Import Hub Share', '导入 Hub 分享')}
                                </button>
                            </div>
                        </div>
                    </>
                    )}
                </PanelBlock>
                {activeTab === 'ingest' && (
                <>
                <PanelBlock title={t('Import Documents', '导入文档')}>
                    <div className="knowledge-import-card-row">
                        <div className="knowledge-import-card-copy">
                            <strong>{t('Add local documents', '添加本地文档')}</strong>
                            <span className="knowledge-muted-line">{t('Import files or directories into your knowledge base', '将文件或目录导入到知识库中')}</span>
                        </div>
                        <button type="button" className="knowledge-button knowledge-button--primary knowledge-button--prominent" onClick={() => setShowImportDialog(true)}>
                            {t('Import Documents', '导入文档')}
                        </button>
                    </div>
                    {importJob && ['running', 'queued', 'pending'].includes(String(importJob.status || '').toLowerCase()) && (
                        <ImportJobSummary t={t} job={importJob} />
                    )}
                </PanelBlock>
                <div className="knowledge-two-column">
                    <PanelBlock title={t('Save Text', '保存文本')}>
                        <input className="knowledge-input" value={textForm.title} onChange={event => setTextForm({ ...textForm, title: event.target.value })} placeholder={t('Title', '标题')} />
                        <textarea className="knowledge-input knowledge-textarea" value={textForm.text} onChange={event => setTextForm({ ...textForm, text: event.target.value })} placeholder={t('Paste text or notes', '粘贴文本或笔记')} />
                        <MetadataControls t={t} labels={textForm.labels} topicHint={textForm.topicHint} saveScope={textForm.saveScope} distillMode={textForm.distillMode} distillModes={distillModes} onChange={patch => setTextForm({ ...textForm, ...patch })} />
                        <button type="button" className="knowledge-button knowledge-button--primary" disabled={!!busy} onClick={saveText}>{busy === 'saveText' ? t('Saving...', '保存中...') : t('Save Text', '保存文本')}</button>
                    </PanelBlock>
                    <PanelBlock title={t('Save URLs', '保存 URL')}>
                        <textarea className="knowledge-input knowledge-textarea" value={urlForm.urls} onChange={event => setURLForm({ ...urlForm, urls: event.target.value })} placeholder={t('One or more URLs, separated by line breaks', '一个或多个 URL，可换行分隔')} />
                        <MetadataControls t={t} labels={urlForm.labels} topicHint={urlForm.topicHint} saveScope={urlForm.saveScope} distillMode={urlForm.distillMode} distillModes={distillModes} onChange={patch => setURLForm({ ...urlForm, ...patch })} />
                        <label className="knowledge-checkbox"><input type="checkbox" checked={urlForm.autoLabels} onChange={event => setURLForm({ ...urlForm, autoLabels: event.target.checked })} /> {t('Auto labels', '自动标签')}</label>
                        <button type="button" className="knowledge-button knowledge-button--primary" disabled={!!busy} onClick={saveURLs}>{busy === 'saveURLs' ? t('Saving...', '保存中...') : t('Save URLs', '保存 URL')}</button>
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
                </div>
            )}

            {activeTab === 'sync' && (
                <div className="knowledge-stack" role="tabpanel" id="knowledge-panel-sync" aria-labelledby="knowledge-tab-sync">
                    <PanelBlock title={t('Hub Knowledge Sync', 'Hub 知识库同步')}>
                        <div className={`knowledge-alert ${syncReadonly ? 'knowledge-alert--warning' : 'knowledge-alert--info'}`}>
                            <strong>{syncReadonly ? t('Service expired: download only', '服务已过期：仅可下载') : t('Manual encrypted sync', '手动加密同步')}</strong>
                            <span>{syncMessage}</span>
                        </div>
                        <div className="knowledge-stats-grid">
                            <Stat label={t('Service', '服务状态')} value={syncServiceStatus === 'official_active' ? t('Official active', '官方服务有效') : syncReadonly ? t('Official expired', '官方服务过期') : t('Temporary', '临时同步')} />
                            <Stat label={t('Cloud package', '云端同步包')} value={syncStatus?.has_package ? t('Available', '可下载') : t('None', '暂无')} />
                            <Stat label={t('Used space', '已占空间')} value={formatSyncBytes(syncStatus?.stored_size_bytes)} />
                            <Stat label={t('Limit', '空间上限')} value={formatSyncBytes(syncStatus?.limit_bytes)} />
                        </div>
                        <div className="knowledge-two-column">
                            <div className="knowledge-exchange-box">
                                <strong>{t('Connection and password', '连接与密码')}</strong>
                                <span className="knowledge-muted-line">{t('The password encrypts and decrypts the package locally. Hub stores only encrypted bytes and cannot recover the password.', '密码只在本机用于加密和解密。Hub 只保存密文字节，无法找回密码。')}</span>
                                <span className="knowledge-field-hint">
                                    {syncStatus?.has_package
                                        ? t('Updating requires the existing sync password. The old cloud package is decrypted for verification before it is replaced.', '更新时请输入原同步密码。替换前会先用该密码验证旧云端同步包。')
                                        : t('First upload requires entering the sync password twice. If you forget it later, delete the cloud package and upload again.', '首次上传需要输入两次同步密码。以后如果忘记密码，可以删除云端同步包后重新上传。')}
                                </span>
                                <label className="knowledge-field">
                                    <span className="knowledge-field-label">{t('Hub URL', 'Hub 地址')}</span>
                                    <input className="knowledge-input" value={syncForm.hubURL} onChange={event => setSyncForm({ ...syncForm, hubURL: event.target.value })} placeholder={t('Uses configured Hub if empty', '为空则使用已配置 Hub')} />
                                </label>
                                <label className="knowledge-field">
                                    <span className="knowledge-field-label">{t('Sync password', '同步密码')}</span>
                                    <input className="knowledge-input" type="password" value={syncForm.password} onChange={event => setSyncForm({ ...syncForm, password: event.target.value })} placeholder={t('Encrypts and decrypts the sync package locally', '仅在本机加密和解密同步包')} />
                                </label>
                                {!syncStatus?.has_package ? (
                                    <label className="knowledge-field">
                                        <span className="knowledge-field-label">{t('Confirm sync password', '再次输入同步密码')}</span>
                                        <input className="knowledge-input" type="password" value={syncForm.passwordConfirm} onChange={event => setSyncForm({ ...syncForm, passwordConfirm: event.target.value })} placeholder={t('Repeat the sync password for first upload', '首次上传时再次输入同步密码')} />
                                    </label>
                                ) : null}
                                <details className="knowledge-advanced-details">
                                    <summary className="knowledge-details-summary">{t('Advanced authentication', '高级认证')}</summary>
                                    <label className="knowledge-field">
                                        <span className="knowledge-field-label">{t('Hub access token override', 'Hub 访问令牌覆盖')}</span>
                                        <input className="knowledge-input" type="password" value={syncForm.hubToken} onChange={event => setSyncForm({ ...syncForm, hubToken: event.target.value })} placeholder={t('Usually not needed; uses the configured token automatically', '通常不需要填写，会自动使用已配置令牌')} />
                                    </label>
                                </details>
                            </div>
                            <div className="knowledge-exchange-box">
                                <strong>{t('Current cloud state', '当前云端状态')}</strong>
                                <KeyValueList values={[
                                    syncStatus?.package_id ? `${t('Package', '同步包')}: ${syncStatus.package_id}` : '',
                                    syncStatus?.updated_at ? `${t('Updated', '更新时间')}: ${syncStatus.updated_at}` : '',
                                    syncExpiryLine,
                                    syncStatus?.readonly_reason || '',
                                ]} empty={t('Refresh sync status to view cloud state.', '刷新同步状态后查看云端状态。')} />
                            </div>
                        </div>
                        <div className="knowledge-panel-actions">
                            <button type="button" className="knowledge-button knowledge-button--secondary" disabled={!!busy} onClick={refreshKnowledgeSyncStatus}>
                                {busy === 'syncStatus' ? t('Refreshing...', '刷新中...') : t('Refresh Status', '刷新状态')}
                            </button>
                            <button type="button" className="knowledge-button knowledge-button--primary" disabled={!!busy || syncReadonly} onClick={syncKnowledgeNow}>
                                <SyncIcon />
                                {busy === 'syncNow' || busy === 'syncResolve'
                                    ? t('Syncing...', '同步中...')
                                    : t('Sync', '同步')}
                            </button>
                            <button type="button" className="knowledge-button knowledge-button--danger" disabled={!!busy || !syncStatus?.has_package} onClick={deleteKnowledgeSync}>
                                {busy === 'syncDelete' ? t('Deleting...', '删除中...') : t('Delete cloud package', '删除云端同步包')}
                            </button>
                            <button type="button" className="knowledge-button knowledge-button--secondary" disabled={!!busy || (!syncForm.hubURL.trim() && !syncForm.hubCenterURL.trim())} onClick={openKnowledgeSyncCardStore}>
                                {t('Renew maclaw official service', '续费 maclaw 官方服务')}
                            </button>
                        </div>
                        {syncConflictResult?.requires_resolution ? (
                            <div className="knowledge-alert knowledge-alert--warning">
                                <strong>{t('Local conflicts found', '发现本地冲突')}</strong>
                                <span>{t('Choose how to resolve matching local sources before importing the cloud sync package.', '导入云端同步包前，请选择如何处理与本地已有来源匹配的内容。')}</span>
                                <ul>
                                    {(syncConflictResult.conflicts || []).slice(0, 8).map((conflict: any, index: number) => (
                                        <li key={`${conflict.remote_id || conflict.title || index}`}>
                                            {[conflict.title || conflict.uri || conflict.remote_id, conflict.reason].filter(Boolean).join(' · ')}
                                        </li>
                                    ))}
                                </ul>
                                <div className="knowledge-panel-actions">
                                    <button type="button" className="knowledge-button knowledge-button--secondary" disabled={!!busy} onClick={() => resolveKnowledgeSyncAndContinue('skip')}>
                                        {busy === 'syncResolve' ? t('Syncing...', '同步中...') : t('Skip conflicts and sync', '跳过冲突并同步')}
                                    </button>
                                    <button type="button" className="knowledge-button knowledge-button--primary" disabled={!!busy} onClick={() => resolveKnowledgeSyncAndContinue('import')}>
                                        {busy === 'syncResolve' ? t('Syncing...', '同步中...') : t('Import anyway and sync', '仍然导入并同步')}
                                    </button>
                                </div>
                            </div>
                        ) : syncConflictResult ? (
                            <div className="knowledge-alert knowledge-alert--success">
                                <strong>{t('Knowledge sync completed', '知识库同步完成')}</strong>
                                <span>{t('Cloud data and local data have been synchronized.', '云端数据与本地数据已完成同步。')}</span>
                            </div>
                        ) : null}
                    </PanelBlock>
                </div>
            )}

            {activeTab === 'search' && (
                <div className="knowledge-stack" role="tabpanel" id="knowledge-panel-search" aria-labelledby="knowledge-tab-search">
                    <div className="knowledge-search-hero">
                            <div className="knowledge-search-mode-row">
                                <div className="knowledge-search-mode-toggle" role="radiogroup" aria-label={t('Search mode', '检索模式')}>
                                    <button type="button" className="knowledge-mode-button" data-active={searchMode === 'semantic' ? 'true' : undefined} onClick={() => setSearchMode('semantic')}>
                                        {t('Semantic Search', '语义检索')}
                                    </button>
                                    <button type="button" className="knowledge-mode-button" data-active={searchMode === 'structured' ? 'true' : undefined} onClick={() => setSearchMode('structured')}>
                                        {t('Table Filters', '表格筛选')}
                                    </button>
                                </div>
                                <span className="knowledge-muted-line">{searchMode === 'structured'
                                    ? t('Filter Excel/CSV rows by column values, ranges, and optional keywords.', '按列值、范围和可选关键词筛选 Excel/CSV 行。')
                                    : t('Search cards, facts, documents, and table rows with natural language.', '用自然语言检索卡片、事实、文档和表格行。')}</span>
                            </div>
                            <div className="knowledge-search-input-wrapper">
                                <span className="knowledge-search-icon">{searchMode === 'structured' ? 'TABLE' : 'SEARCH'}</span>
                                <input
                                    className="knowledge-input knowledge-search-input--hero"
                                    value={searchForm.query}
                                    onChange={event => setSearchForm({ ...searchForm, query: event.target.value })}
                                    onKeyDown={event => { if (event.key === 'Enter') { event.preventDefault(); void runSearch(); } }}
                                    placeholder={searchMode === 'structured' ? t('Optional keywords inside matched rows...', '可选：在匹配行内继续按关键词筛选...') : t('Search knowledge base...', '搜索知识库...')}
                                    autoFocus
                                />
                                <button type="button" className="knowledge-button knowledge-button--primary knowledge-search-button" disabled={!!busy} onClick={runSearch}>
                                    {busy === 'search' ? t('Searching...', '检索中...') : t('Search', '检索')}
                                </button>
                            </div>
                            <div className="knowledge-search-filters">
                                <select className="knowledge-input knowledge-input--compact" value={searchForm.resultType} onChange={event => setSearchForm({ ...searchForm, resultType: event.target.value })}><option value="all">{t('All types', '全部类型')}</option><option value="node">node</option><option value="card">card</option><option value="fact">fact</option></select>
                                <input className="knowledge-input knowledge-input--compact" value={searchForm.sourceKind} onChange={event => setSearchForm({ ...searchForm, sourceKind: event.target.value })} placeholder={t('Kind', '类型')} />
                                <input className="knowledge-input knowledge-input--compact" value={searchForm.domain} onChange={event => setSearchForm({ ...searchForm, domain: event.target.value })} placeholder={t('Domain', '域名')} />
                                <input className="knowledge-input knowledge-input--compact" value={searchForm.labels} onChange={event => setSearchForm({ ...searchForm, labels: event.target.value })} placeholder={t('Labels', '标签')} />
                                <input className="knowledge-input knowledge-input--compact" type="number" value={searchForm.limit} onChange={event => setSearchForm({ ...searchForm, limit: Number(event.target.value) })} title={t('Max results', '最大结果数')} style={{ width: 60 }} />
                                <label className="knowledge-checkbox"><input type="checkbox" checked={searchForm.includeDisabled} onChange={event => setSearchForm({ ...searchForm, includeDisabled: event.target.checked })} /> {t('Disabled', '含禁用')}</label>
                            </div>
                            {searchMode === 'structured' && (
                                <div className="knowledge-structured-filter-panel" aria-label={t('Structured table filters', '结构化表格筛选')}>
                                    <div className="knowledge-structured-filter-grid">
                                        <input className="knowledge-input" list="knowledge-structured-column-options" value={structuredSearchForm.columnName} onChange={event => setStructuredSearchForm({ ...structuredSearchForm, columnName: event.target.value })} placeholder={t('Column name, e.g. Department', '列名，例如：部门')} />
                                        <datalist id="knowledge-structured-column-options">
                                            {structuredColumnSuggestions.map(column => <option key={`${column.column_name}-${column.value_type || ''}`} value={column.column_name || ''}>{column.value_type || ''}</option>)}
                                        </datalist>
                                        <select className="knowledge-input" value={structuredSearchForm.matchMode} onChange={event => setStructuredSearchForm({ ...structuredSearchForm, matchMode: event.target.value })}>
                                            <option value="equals">{t('Equals', '等于')}</option>
                                            <option value="contains">{t('Contains', '包含')}</option>
                                        </select>
                                        <input className="knowledge-input" value={structuredSearchForm.columnValue} onChange={event => setStructuredSearchForm({ ...structuredSearchForm, columnValue: event.target.value })} placeholder={t('Text value', '文本值')} />
                                        <input className="knowledge-input" list="knowledge-structured-sheet-options" value={structuredSearchForm.sheetName} onChange={event => setStructuredSearchForm({ ...structuredSearchForm, sheetName: event.target.value })} placeholder={t('Sheet name (optional)', 'Sheet 名（可选）')} />
                                        <datalist id="knowledge-structured-sheet-options">
                                            {structuredSheetSuggestions.map(sheet => <option key={sheet} value={sheet} />)}
                                        </datalist>
                                    </div>
                                    <div className="knowledge-structured-filter-grid knowledge-structured-filter-grid--ranges">
                                        <input className="knowledge-input" type="number" value={structuredSearchForm.numberMin} onChange={event => setStructuredSearchForm({ ...structuredSearchForm, numberMin: event.target.value })} placeholder={t('Number min', '数字下限')} />
                                        <input className="knowledge-input" type="number" value={structuredSearchForm.numberMax} onChange={event => setStructuredSearchForm({ ...structuredSearchForm, numberMax: event.target.value })} placeholder={t('Number max', '数字上限')} />
                                        <input className="knowledge-input" type="date" value={structuredSearchForm.dateStart} onChange={event => setStructuredSearchForm({ ...structuredSearchForm, dateStart: event.target.value })} title={t('Date start', '日期起始')} />
                                        <input className="knowledge-input" type="date" value={structuredSearchForm.dateEnd} onChange={event => setStructuredSearchForm({ ...structuredSearchForm, dateEnd: event.target.value })} title={t('Date end', '日期结束')} />
                                    </div>
                                    <div className="knowledge-structured-filter-help">
                                        {t('Tip: use one column at a time for precise row evidence. Text and range filters can be combined on the same column.', '提示：一次使用一个列名以获得精准行证据；同一列可以组合文本值和范围条件。')}
                                        {' '}
                                        {structuredCatalog?.count ? t(`${structuredCatalog.count} table(s) indexed.`, `已索引 ${structuredCatalog.count} 张表。`) : t('No table catalog loaded yet.', '尚未加载表格目录。')}
                                        <button type="button" className="knowledge-inline-link-button" disabled={!!busy} onClick={loadStructuredCatalog}>
                                            {busy === 'structuredCatalog' ? t('Loading catalog...', '加载目录中...') : t('Refresh catalog', '刷新目录')}
                                        </button>
                                    </div>
                                </div>
                            )}
                    </div>
                    <div className="knowledge-two-column">
                        <PanelBlock title={`${t('Results', '结果')} (${searchResults.length})`}>
                            <ResultList results={searchResults} empty={t('No results yet.', '暂无检索结果。')} query={searchForm.query} />
                        </PanelBlock>
                        <PanelBlock title={t('Facets', '分面')}>
                            {searchMode === 'structured' ? (
                                <div className="knowledge-empty">{t('Facets are available for semantic search. Structured table filters return row evidence directly.', '分面用于语义检索；表格筛选会直接返回行级证据。')}</div>
                            ) : <KeyValueList values={[
                                ...(facets?.result_types || []).map(item => `${item.label || item.kind} ${item.count || 0}`),
                                ...(facets?.source_kinds || []).map(item => `${item.label || item.kind} ${item.count || 0}`),
                                ...(facets?.domains || []).map(item => `${item.domain || item.label} ${item.count || 0}`),
                                ...(facets?.labels || []).map(item => `${item.label} ${item.count || 0}`),
                            ]} empty={t('Run a search to see facets.', '执行检索后显示分面。')} />}
                        </PanelBlock>
                    </div>
                </div>
            )}

            {activeTab === 'sources' && (
                <div className="knowledge-stack" role="tabpanel" id="knowledge-panel-sources" aria-labelledby="knowledge-tab-sources">
                    <SourceFilters t={t} filter={sourceFilter} coverageOptions={coverageOptions} onChange={setSourceFilter} />
                    <div className="knowledge-inline-actions">
                        <button type="button" className="knowledge-button knowledge-button--primary" disabled={!!busy} onClick={loadSources}>{busy === 'sources' ? t('Loading...', '加载中...') : t('Load Sources', '加载来源')}</button>
                        {sources !== null && <span className="knowledge-muted-line">{sources.length >= sourceFilter.limit ? `${sources.length}+ ${t('results (limit reached)', '条结果（已达上限）')}` : `${sources.length} ${t('results', '条结果')}`}</span>}
                        <span className="knowledge-muted-line">{t('Payload', '条件')} {JSON.stringify(sourcePayload)}</span>
                    </div>
                    <div className="knowledge-list">
                        {sources === null ? (
                            <div className="knowledge-empty">{t('Click "Load Sources" to query.', '点击「加载来源」按钮进行查询。')}</div>
                        ) : sources.length ? sources.map(source => (
                            <div key={source.id || source.uri || source.title} className="knowledge-row">
                                <div className="knowledge-row-main">
                                    <strong>{source.title || source.relative_path || source.uri || source.id}</strong>
                                    <span className="knowledge-muted-line">{[source.kind, source.status, source.relative_path || source.uri, source.labels?.join(', ')].filter(Boolean).join(' · ')}</span>
                                    <span className="knowledge-muted-line">{[`nodes ${source.node_count || 0}`, `cards ${source.card_count || 0}`, `facts ${source.fact_count || 0}`].join(' · ')}</span>
                                </div>
                                <div className="knowledge-inline-actions">
                                    <button type="button" className="knowledge-button knowledge-button--secondary" disabled={!!busy} onClick={() => toggleSource(source)}>{String(source.status || '').toLowerCase() === 'disabled' ? t('Enable', '启用') : t('Disable', '禁用')}</button>
                                    <button type="button" className="knowledge-button knowledge-button--danger" disabled={!!busy} onClick={() => deleteSource(source)}>{t('Delete', '删除')}</button>
                                </div>
                            </div>
                        )) : <div className="knowledge-empty">{t('No sources match the current filters.', '当前筛选条件下没有匹配的来源。')}</div>}
                    </div>
                </div>
            )}

            {activeTab === 'quality' && (
                <div className="knowledge-stack" role="tabpanel" id="knowledge-panel-quality" aria-labelledby="knowledge-tab-quality">
                    <SourceFilters t={t} filter={qualityFilter} coverageOptions={qualityCoverageOptions} onChange={setQualityFilter} />
                    <div className="knowledge-compact-grid">
                        <select className="knowledge-input" value={qualityOptions.policy} onChange={event => setQualityOptions({ ...qualityOptions, policy: event.target.value })}><option value="balanced">balanced</option><option value="conservative">conservative</option><option value="aggressive">aggressive</option></select>
                        <select className="knowledge-input" value={qualityOptions.distillMode} onChange={event => setQualityOptions({ ...qualityOptions, distillMode: event.target.value })}><option value="">{t('Default distill mode', '默认蒸馏模式')}</option>{distillModes.map(mode => <option key={mode} value={mode}>{mode}</option>)}</select>
                        <input className="knowledge-input" type="number" value={qualityOptions.maxSourcesPerAction} onChange={event => setQualityOptions({ ...qualityOptions, maxSourcesPerAction: Number(event.target.value) })} />
                    </div>
                    <div className="knowledge-inline-actions">
                        <label className="knowledge-checkbox"><input type="checkbox" checked={qualityOptions.dryRun} onChange={event => setQualityOptions({ ...qualityOptions, dryRun: event.target.checked })} /> {t('Preview only', '仅预览')}</label>
                        <label className="knowledge-checkbox"><input type="checkbox" checked={qualityOptions.allowSensitiveDisable} onChange={event => setQualityOptions({ ...qualityOptions, allowSensitiveDisable: event.target.checked })} /> {t('Allow sensitive isolation', '允许敏感隔离')}</label>
                        <label className="knowledge-checkbox"><input type="checkbox" checked={qualityOptions.allowDuplicateSuppression} onChange={event => setQualityOptions({ ...qualityOptions, allowDuplicateSuppression: event.target.checked })} /> {t('Allow duplicate suppression', '允许重复抑制')}</label>
                    </div>
                    <div className="knowledge-inline-actions">
                        <button type="button" className="knowledge-button knowledge-button--primary" disabled={!!busy} onClick={loadQuality}>{busy === 'quality' ? t('Loading...', '加载中...') : t('Build Report + Plan', '生成报告和计划')}</button>
                        <button type="button" className="knowledge-button knowledge-button--secondary" disabled={!!busy || !(qualityPlan?.actions || []).length} onClick={() => executeQualityAction()}>{qualityOptions.dryRun ? t('Preview Plan', '预览计划') : t('Run Plan', '执行计划')}</button>
                    </div>
                    <div className="knowledge-two-column">
                        <PanelBlock title={t('Quality Report', '质量报告')}>
                            <div className="knowledge-stats-grid">
                                <Stat label={t('Sources', '来源')} value={qualityReport?.count || 0} />
                                <Stat label={t('Average', '平均分')} value={qualityReport?.average_score || 0} />
                                <Stat label={t('Actions', '动作')} value={qualityPlan?.actions?.length || 0} />
                            </div>
                            <KeyValueList values={[...topCounts(qualityReport?.grades, 4), ...topCounts(qualityReport?.signals, 6), ...topCounts(qualityReport?.actions, 6)]} empty={t('No report yet.', '暂无报告。')} />
                        </PanelBlock>
                        <PanelBlock title={t('Maintenance Plan', '维护计划')}>
                            {(qualityPlan?.actions || []).length ? <div className="knowledge-list">{(qualityPlan?.actions || []).map((action, index) => (
                                <div key={`${action.kind || index}`} className="knowledge-row">
                                    <div className="knowledge-row-main">
                                        <strong>{action.title || action.kind}</strong>
                                        <span className="knowledge-muted-line">{[action.description, action.severity, action.count ? `${action.count} sources` : '', action.signals?.join(', ')].filter(Boolean).join(' · ')}</span>
                                    </div>
                                    <button type="button" className="knowledge-button knowledge-button--secondary" disabled={!!busy} onClick={() => executeQualityAction(action)}>{qualityOptions.dryRun ? t('Preview', '预览') : t('Run', '执行')}</button>
                                </div>
                            ))}</div> : <div className="knowledge-empty">{t('No plan has been built.', '尚未生成维护计划。')}</div>}
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
                        <summary className="knowledge-details-summary">{t('Raw payload', '原始数据')}</summary>
                        <pre className="knowledge-raw-pre">{JSON.stringify(operationResult, null, 2)}</pre>
                    </details>
                </PanelBlock>
            ) : null}
        </section>
        <KnowledgeImportDialog
            key={importDialogKey}
            open={showImportDialog}
            onClose={() => {
                setShowImportDialog(false);
                knowledgeImportGlobal?.setDialogOpen(false);
            }}
            onJobUpdate={job => {
                if (job) setImportJob(job);
                knowledgeImportGlobal?.publishJob(job);
            }}
            restoreJob={knowledgeImportGlobal?.job || importJob}
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
    return <div className="knowledge-block"><h3 className="knowledge-block-title">{title}</h3><div className="knowledge-block-body">{children}</div></div>;
}

function KeyValueList({ values, empty }: { values: string[]; empty: string }) {
    const cleanValues = values.map(value => String(value || '').trim()).filter(Boolean);
    if (!cleanValues.length) return <div className="knowledge-empty">{empty}</div>;
    return <div className="knowledge-chip-list">{cleanValues.map(value => <span key={value} className="knowledge-chip">{value}</span>)}</div>;
}

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
        return i % 2 === 1 ? <mark key={i} className="knowledge-highlight-mark">{part}</mark> : part;
    })}</>;
}

function ImageResultThumbnail({ sourceID, nodeID, title }: { sourceID: string; nodeID: string; title: string }) {
    const [thumbPath, setThumbPath] = useState('');
    const [originalPath, setOriginalPath] = useState('');

    useEffect(() => {
        if (!sourceID) return;
        // Try with the full node-qualified ID first (embedded images use sourceID_nodeID),
        // then fall back to sourceID alone (standalone images).
        const ids = nodeID ? [sourceID + '_' + nodeID, sourceID] : [sourceID];
        let cancelled = false;
        (async () => {
            for (const id of ids) {
                try {
                    const paths = await KnowledgeGetImageAssetPaths(id);
                    if (cancelled) return;
                    if (paths && (paths.thumb_data_url || paths.original)) {
                        setThumbPath(paths.thumb_data_url || '');
                        setOriginalPath(paths.original || paths.preview || '');
                        return;
                    }
                } catch { /* continue to next id */ }
            }
        })();
        return () => { cancelled = true; };
    }, [sourceID, nodeID]);

    if (!thumbPath) return null;

    const handleClick = async () => {
        if (originalPath) {
            try {
                await KnowledgeOpenImageFile(originalPath);
            } catch (e) {
                console.warn('[knowledge-image] open failed:', e);
            }
        }
    };

    return (
        <button type="button" className="knowledge-image-thumb" onClick={handleClick} title={title + ' (click to open)'}>
            <img src={thumbPath} alt={title} loading="lazy" />
        </button>
    );
}

function ResultList({ results, empty, query }: { results: SearchResult[]; empty: string; query?: string }) {
    if (!results.length) return <div className="knowledge-empty">{empty}</div>;
    const regex = buildHighlightRegex((query || '').trim());
    return <div className="knowledge-list">{results.map((result, index) => {
        const isImage = result.node_type === 'image' || result.source?.kind === 'image';
        const isTableRow = result.result_type === 'table_row';
        const sourceLabel = isTableRow
            ? (result.claim || result.source?.title || result.source?.relative_path || 'Table row')
            : (result.card_title || result.node_title || result.subject || result.source?.title || result.source?.relative_path || 'Result');
        const meta = [
            result.result_type,
            result.source?.kind,
            isTableRow && result.sheet_name ? `sheet ${result.sheet_name}` : '',
            isTableRow && result.row_index ? `row ${result.row_index}` : '',
            result.column_name ? `column ${result.column_name}` : '',
            result.source?.relative_path || result.source?.uri,
            result.score ? `score ${result.score.toFixed(3)}` : '',
        ].filter(Boolean).join(' · ');
        return (
            <div key={`${result.result_type || 'result'}-${result.node_id || result.card_id || result.fact_id || result.row_id || index}`} className={`knowledge-row ${isImage ? 'knowledge-row--image' : ''}`}>
                {isImage && (
                    <ImageResultThumbnail sourceID={result.source?.id || ''} nodeID={result.node_id || ''} title={sourceLabel} />
                )}
                <div className="knowledge-row-main">
                    <strong>
                        {isImage && <span className="knowledge-chip knowledge-chip--image">IMG</span>}
                        {isTableRow && <span className="knowledge-chip knowledge-chip--badge">ROW</span>}
                        {highlightText(sourceLabel, regex)}
                    </strong>
                    <span className="knowledge-muted-line">{meta}</span>
                    <span>{highlightText(result.summary || result.claim || result.snippet || [result.subject, result.predicate, result.object].filter(Boolean).join(' '), regex)}</span>
                </div>
            </div>
        );
    })}</div>;
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
        <div className="knowledge-stack">
            <div className="knowledge-stats-grid">
                <Stat label={t('Mode', '模式')} value={result.dry_run ? t('Preview', '预览') : t('Run', '执行')} />
                <Stat label={t('Actions', '动作')} value={result.results?.length || 0} />
                <Stat label={t('Sources', '来源')} value={result.count || sourceIDs.length || 0} />
                <Stat label={t('Failures', '失败')} value={failures.length} />
            </div>
            {label ? <div className="knowledge-muted-line">{label}</div> : null}
            {sourceIDs.length ? <div className="knowledge-muted-line">{knowledgeExecutionSourceFilterLabel(label, sourceIDs.length)}</div> : null}
            <KeyValueList values={sourceIDs} empty={t('No affected sources reported.', '未报告受影响来源。')} />
            {failures.length ? (
                <div className="knowledge-list">
                    {failures.map((failure, index) => (
                        <div key={`${failure.action}-${failure.sourceID}-${index}`} className="knowledge-failure-row">
                            <strong>{failure.action || t('Execution', '执行')}</strong>
                            <span className="knowledge-muted-line">{failure.sourceID}</span>
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
        <div className="knowledge-job-status">
            <strong>{t('Directory Import', '目录导入')} {job.id}</strong>
            <span className="knowledge-muted-line">{[job.status, job.result?.root_path, job.result?.current_file, job.error].filter(Boolean).join(' · ')}</span>
            <div className="knowledge-stats-grid">
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
        <div className="knowledge-compact-grid">
            <input className="knowledge-input" value={labels} onChange={event => onChange({ labels: event.target.value })} placeholder={t('Labels', '标签')} />
            <input className="knowledge-input" value={topicHint} onChange={event => onChange({ topicHint: event.target.value })} placeholder={t('Topic hint', '主题提示')} />
            <select className="knowledge-input" value={saveScope} onChange={event => onChange({ saveScope: event.target.value })}>
                <option value="project">{t('Project', '项目')}</option>
                <option value="personal">{t('Personal', '个人')}</option>
                <option value="local_only">{t('Local only', '仅本地')}</option>
            </select>
            <select className="knowledge-input" value={distillMode} onChange={event => onChange({ distillMode: event.target.value })}>
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
            <div className="knowledge-compact-grid">
                <input className="knowledge-input" value={filter.query} onChange={event => onChange({ ...filter, query: event.target.value })} placeholder={t('Query', '关键词')} />
                <input className="knowledge-input" value={filter.kind} onChange={event => onChange({ ...filter, kind: event.target.value })} placeholder={t('Kind', '类型')} />
                <select className="knowledge-input" value={filter.status} onChange={event => onChange({ ...filter, status: event.target.value })}>
                    <option value="all">{t('All statuses', '全部状态')}</option>
                    <option value="active">{t('active (non-disabled)', 'active（非禁用）')}</option>
                    <option value="pending">pending</option>
                    <option value="parsed">parsed</option>
                    <option value="distilled">distilled</option>
                    <option value="stale">stale</option>
                    <option value="failed">failed</option>
                    <option value="disabled">disabled</option>
                </select>
                <select className="knowledge-input" value={filter.coverage} onChange={event => onChange({ ...filter, coverage: event.target.value })}>
                    {coverageOptions.map(option => <option key={option} value={option}>{option}</option>)}
                </select>
                <input className="knowledge-input" value={filter.domain} onChange={event => onChange({ ...filter, domain: event.target.value })} placeholder={t('Domain', '域名')} />
                <input className="knowledge-input" value={filter.labels} onChange={event => onChange({ ...filter, labels: event.target.value })} placeholder={t('Labels', '标签')} />
                <input className="knowledge-input" type="number" value={filter.limit} onChange={event => onChange({ ...filter, limit: Number(event.target.value) })} />
            </div>
        </PanelBlock>
    );
}

function Stat({ label, value }: { label: string; value: string | number }) {
    return (
        <div className="knowledge-stat">
            <div className="knowledge-stat-label">{label}</div>
            <div className="knowledge-stat-value">{value}</div>
        </div>
    );
}

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

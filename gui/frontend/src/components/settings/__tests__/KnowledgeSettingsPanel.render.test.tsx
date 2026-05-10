import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { KnowledgeSettingsPanel } from '../KnowledgeSettingsPanel';
import {
    KnowledgeCapabilities,
    KnowledgeHealth,
} from '../../../../wailsjs/go/main/App';

vi.mock('../../../../wailsjs/go/main/App', () => {
    const arrayResult = vi.fn(async () => []);
    const objectResult = vi.fn(async () => ({}));
    const sourceResult = vi.fn(async () => ({ id: 'ksrc_test', title: 'Test source' }));
    const names = [
        'KnowledgeCapabilities',
        'KnowledgeBackfillSourceAutoLabels',
        'KnowledgeDeleteSource',
        'KnowledgeDiscoverURLs',
        'KnowledgeDoctor',
        'KnowledgeContextPack',
        'KnowledgeDisableSensitiveSources',
        'KnowledgeDisableSource',
        'KnowledgeDisableSources',
        'KnowledgeDisableSourcesByFilter',
        'KnowledgeEnableSource',
        'KnowledgeEnableSourcesByFilter',
        'KnowledgeEntityProfile',
        'KnowledgeExecuteSourceQualityMaintenancePlan',
        'KnowledgeExplain',
        'KnowledgeExportSnapshot',
        'KnowledgeFactGraph',
        'KnowledgeFactIndex',
        'KnowledgeExportSnapshotWithOptions',
        'KnowledgeHealth',
        'KnowledgeImportDirectory',
        'KnowledgeImportFiles',
        'KnowledgeImportSnapshot',
        'KnowledgeImportJobStatus',
        'KnowledgeListURLDomainPolicies',
        'KnowledgeListImportBatches',
        'KnowledgeListImportItems',
        'KnowledgeListNodesBySource',
        'KnowledgeListSourceLinks',
        'KnowledgeListSourceLabels',
        'KnowledgeListSourceLinkEvents',
        'KnowledgeListSourceVersions',
        'KnowledgeListCardsBySource',
        'KnowledgeListDuplicateCards',
        'KnowledgeListFactsBySource',
        'KnowledgeListSuppressedCards',
        'KnowledgeListSources',
        'KnowledgeLinkSources',
        'KnowledgeMaintain',
        'KnowledgePreviewSourceRefresh',
        'KnowledgePreviewSourcesRefreshByFilter',
        'KnowledgePreviewSourceTopicLinks',
        'KnowledgeRefreshChangedSources',
        'KnowledgeRefreshChangedSourcesByFilter',
        'KnowledgeRefreshSourceTopicLinks',
        'KnowledgeRefreshSourceTopicLinksByFilter',
        'KnowledgeRebuildSourceDerived',
        'KnowledgeRebuildSourcesDerived',
        'KnowledgeRebuildSourcesDerivedByFilter',
        'KnowledgeRefreshSource',
        'KnowledgeRefreshSources',
        'KnowledgeRefreshSourcesByFilter',
        'KnowledgeRetryImportBatch',
        'KnowledgeRestoreSuppressedCards',
        'KnowledgeSaveURL',
        'KnowledgeSaveURLs',
        'KnowledgeScanSensitiveContent',
        'KnowledgeScanDirectory',
        'KnowledgeScanFiles',
        'KnowledgeSearch',
        'KnowledgeSearchFacets',
        'KnowledgeSourceGraph',
        'KnowledgeSourceNeighborhood',
        'KnowledgeSourcePath',
        'KnowledgeSourceDigest',
        'KnowledgeSourceTimeline',
        'KnowledgeTopicRelevance',
        'KnowledgeQualityMaintenancePolicies',
        'KnowledgeSourceQualityMaintenancePlan',
        'KnowledgeSourceQualityReport',
        'KnowledgeStartImportDirectory',
        'KnowledgeSuggest',
        'KnowledgeSuppressDuplicateCards',
        'KnowledgeUnlinkSources',
        'KnowledgeUpdateURLDomainPolicies',
        'KnowledgeUpdateSourceMetadata',
        'KnowledgeUpdateSourceLabels',
        'SelectKnowledgeDirectory',
        'SelectKnowledgeFiles',
    ];
    const exports = Object.fromEntries(names.map(name => [name, objectResult]));
    return {
        ...exports,
        KnowledgeCapabilities: vi.fn(async () => ({
            distill_modes: ['rules_only'],
            coverage_filters: ['missing_cards', 'has_links'],
            formats: [{ kind: 'markdown', extensions: ['.md'] }],
        })),
        KnowledgeHealth: vi.fn(async () => ({ status: 'ok', score: 100, quality_avg_score: 100, maintenance_actions: [] })),
        KnowledgeListSources: arrayResult,
        KnowledgeSearch: arrayResult,
        KnowledgeSearchFacets: objectResult,
        KnowledgeSourceQualityReport: objectResult,
        KnowledgeSourceQualityMaintenancePlan: objectResult,
        KnowledgeSaveText: sourceResult,
        KnowledgeSaveURL: sourceResult,
        KnowledgeSaveURLs: objectResult,
        SelectKnowledgeFiles: vi.fn(async () => []),
        SelectKnowledgeDirectory: vi.fn(async () => ''),
    };
});

afterEach(() => {
    cleanup();
    vi.clearAllMocks();
});

describe('KnowledgeSettingsPanel component', () => {
    it('renders the restored operational panel instead of the old placeholder', async () => {
        render(<KnowledgeSettingsPanel lang="en" />);

        expect(screen.getByRole('heading', { name: 'Knowledge Base' })).toBeTruthy();
        expect(screen.queryByText(/available after the panel source is restored/i)).toBeNull();
        expect(screen.getByRole('button', { name: 'Overview' })).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Ingest' })).toBeTruthy();
        expect(screen.getAllByRole('button', { name: 'Search' }).length).toBeGreaterThan(0);
        expect(screen.getByRole('button', { name: 'Sources' })).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Quality' })).toBeTruthy();

        await waitFor(() => expect(KnowledgeHealth).toHaveBeenCalled());
        expect(KnowledgeCapabilities).toHaveBeenCalled();
    });
});

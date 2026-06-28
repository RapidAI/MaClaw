import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { KnowledgeSettingsPanel } from '../KnowledgeSettingsPanel';
import {
    KnowledgeCapabilities,
    KnowledgeExportSnapshotWithOptions,
    KnowledgeHealth,
    KnowledgeListImportBatches,
    KnowledgeListSources,
    KnowledgeSaveText,
    KnowledgeShareToHub,
    KnowledgeSearchStructured,
    KnowledgeStructuredCatalog,
    LoadConfig,
    OpenSystemUrl,
    SelectKnowledgeSnapshotExportPath,
} from '../../../../wailsjs/go/main/App';

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn(() => vi.fn()),
    EventsOff: vi.fn(),
}));

vi.mock('../../../../wailsjs/go/main/App', () => {
    const arrayResult = vi.fn(async () => []);
    const objectResult = vi.fn(async () => ({}));
    const sourceResult = vi.fn(async () => ({ id: 'ksrc_test', title: 'Test source' }));
    const names = [
        'LoadConfig',
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
        'KnowledgeFactGraph',
        'KnowledgeFactIndex',
        'KnowledgeExportSnapshotWithOptions',
        'KnowledgeHealth',
        'KnowledgeImportDirectory',
        'KnowledgeImportFiles',
        'KnowledgeImportSnapshot',
        'SelectKnowledgeSnapshotFile',
        'SelectKnowledgeSnapshotExportPath',
        'OpenSystemUrl',
        'KnowledgeShareToHub',
        'KnowledgeImportHubShare',
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
        'KnowledgeSearchStructured',
        'KnowledgeStructuredCatalog',
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
        'KnowledgeDeepCrawl',
        'KnowledgeDeepCrawlPreview',
        'KnowledgeDeepCrawlCancel',
        'KnowledgeSyncDelete',
        'KnowledgeSyncDownload',
        'KnowledgeSyncStatus',
        'KnowledgeSyncUpload',
        'KnowledgeSyncVerifyPassword',
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
        LoadConfig: vi.fn(async () => ({})),
        KnowledgeListSources: arrayResult,
        KnowledgeSearch: arrayResult,
        KnowledgeSearchStructured: arrayResult,
        KnowledgeStructuredCatalog: vi.fn(async () => ({
            count: 1,
            tables: [{
                sheet_name: 'Sheet1',
                columns: [{ column_name: 'Department', value_type: 'string' }],
            }],
        })),
        KnowledgeSearchFacets: objectResult,
        KnowledgeSourceQualityReport: objectResult,
        KnowledgeSourceQualityMaintenancePlan: objectResult,
        KnowledgeSaveText: sourceResult,
        KnowledgeSaveURL: sourceResult,
        KnowledgeSaveURLs: objectResult,
        KnowledgeShareToHub: vi.fn(async () => ({
            knowledge_id: 'kn_ui',
            share_url: 'https://hub.example/hub/knowledge/shares/kn_ui',
            agent_import: 'https://hub.example/api/knowledge/shares/kn_ui?intent=import',
            source_count: 2,
            content_sources: 1,
            warnings: ['source ksrc_big content truncated: package content byte limit reached'],
            expires_at: '2026-07-03T00:00:00Z',
            source_summary: {
                content_sources: 1,
                warnings: ['source ksrc_big content truncated: package content byte limit reached'],
            },
        })),
        KnowledgeSyncStatus: vi.fn(async () => ({
            service_status: 'official_active',
            has_package: false,
            limit_bytes: 524288000,
            message: 'maclaw official service is active',
        })),
        OpenSystemUrl: vi.fn(async () => undefined),
        SelectKnowledgeFiles: vi.fn(async () => []),
        SelectKnowledgeDirectory: vi.fn(async () => ''),
        SelectKnowledgeSnapshotExportPath: vi.fn(async () => ''),
        SelectKnowledgeSnapshotFile: vi.fn(async () => ''),
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
        expect(screen.getByRole('tab', { name: 'Overview' })).toBeTruthy();
        expect(screen.getByRole('tab', { name: 'Ingest' })).toBeTruthy();
        expect(screen.getByRole('tab', { name: 'Search' })).toBeTruthy();
        expect(screen.getByRole('tab', { name: 'Sources' })).toBeTruthy();
        expect(screen.getByRole('tab', { name: 'Quality' })).toBeTruthy();

        await waitFor(() => expect(KnowledgeHealth).toHaveBeenCalled());
        expect(KnowledgeCapabilities).toHaveBeenCalled();
    });

    it('keeps document import as a full-width ingest section', async () => {
        render(<KnowledgeSettingsPanel lang="en" />);

        fireEvent.click(screen.getByRole('tab', { name: 'Ingest' }));

        expect(await screen.findByRole('heading', { name: 'Import Documents' })).toBeTruthy();
        expect(screen.getByText('Add local documents')).toBeTruthy();
        expect(screen.getByText('Import files or directories into your knowledge base')).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Import Documents' })).toBeTruthy();
        expect(screen.getByRole('heading', { name: 'Deep Crawl' })).toBeTruthy();
    });

    it('shows export and Hub sharing in a dedicated export tab', async () => {
        render(<KnowledgeSettingsPanel lang="en" />);

        fireEvent.click(screen.getByRole('tab', { name: 'Export' }));

        expect(await screen.findByRole('heading', { name: 'Export / Share Knowledge Base' })).toBeTruthy();
        expect(screen.getByText('Choose knowledge items')).toBeTruthy();
        expect(screen.getByText('Choose an action')).toBeTruthy();
        expect(screen.getByLabelText('Knowledge item selection for export and sharing')).toBeTruthy();
        expect(screen.getByRole('button', { name: 'View Shares' })).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Export Full to File' })).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Share Full to Hub' })).toBeTruthy();
        expect(screen.queryByPlaceholderText('Required knowledge description for readers and Hub management')).toBeNull();
        const shareButton = screen.getByRole('button', { name: 'Share Full to Hub' });
        fireEvent.click(shareButton);
        const dialog = await screen.findByRole('dialog', { name: 'Hub share settings' });
        expect(dialog).toBeTruthy();
        expect(document.activeElement).toBe(dialog);
        expect(screen.getByPlaceholderText('Required knowledge description for readers and Hub management')).toBeTruthy();
        expect(document.body.style.overflow).toBe('hidden');
        fireEvent.keyDown(window, { key: 'Escape' });
        await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Hub share settings' })).toBeNull());
        expect(document.body.style.overflow).toBe('');
        expect(document.activeElement).toBe(shareButton);
        expect(screen.queryByRole('heading', { name: 'Import Documents' })).toBeNull();
        expect(screen.queryByRole('heading', { name: 'Deep Crawl' })).toBeNull();
    });

    it('opens the Hub shares page with the configured viewer token in the URL hash', async () => {
        vi.mocked(LoadConfig).mockResolvedValueOnce({
            remote_hub_url: 'https://hub.example',
            remote_viewer_token: 'viewer token/with spaces',
        } as any);
        render(<KnowledgeSettingsPanel lang="en" />);

        fireEvent.click(screen.getByRole('tab', { name: 'Export' }));
        fireEvent.click(await screen.findByRole('button', { name: 'View Shares' }));

        await waitFor(() => expect(OpenSystemUrl).toHaveBeenCalledWith(
            'https://hub.example/hub/knowledge/shares/mine#token=viewer%20token%2Fwith%20spaces',
        ));
    });

    it('opens the tenant scoped Hub card store from the sync renewal action', async () => {
        vi.mocked(LoadConfig).mockResolvedValueOnce({
            remote_hub_url: 'https://hub.example/',
            remote_tenant_id: 'tenant acme',
            remote_email: 'dev@example.com',
            remote_viewer_token: 'viewer token',
        } as any);
        render(<KnowledgeSettingsPanel lang="en" />);

        fireEvent.click(screen.getByRole('tab', { name: 'Sync' }));
        fireEvent.click(await screen.findByRole('button', { name: 'Renew maclaw official service' }));

        await waitFor(() => expect(OpenSystemUrl).toHaveBeenCalledWith(
            'https://hub.example/card_store?tenant_id=tenant%20acme&email=dev%40example.com#token=viewer%20token',
        ));
    });

    it('does not auto-retry forever when export source loading fails and allows manual recovery', async () => {
        vi.mocked(KnowledgeListSources)
            .mockRejectedValueOnce(new Error('source list unavailable'))
            .mockResolvedValueOnce([{
                id: 'ksrc_recovered',
                kind: 'file',
                relative_path: 'recovered.md',
                status: 'active',
            }]);
        render(<KnowledgeSettingsPanel lang="en" />);

        fireEvent.click(screen.getByRole('tab', { name: 'Export' }));

        expect(await screen.findByText('source list unavailable')).toBeTruthy();
        expect(screen.getByText('Source list could not be loaded. Use Refresh List to try again.')).toBeTruthy();
        await waitFor(() => expect(KnowledgeListSources).toHaveBeenCalledTimes(1));
        fireEvent.click(screen.getByRole('button', { name: 'Refresh List' }));
        expect(await screen.findByText('recovered.md')).toBeTruthy();
        expect(KnowledgeListSources).toHaveBeenCalledTimes(2);
    });

    it('exports selected readable knowledge sources instead of manual Source IDs', async () => {
        vi.mocked(KnowledgeListImportBatches).mockResolvedValueOnce([{
            id: 'batch_docs',
            root_path: 'D:\\docs\\contracts',
            status: 'completed',
            total_files: 2,
            imported_files: 2,
        }]);
        vi.mocked(KnowledgeListSources).mockResolvedValueOnce([
            {
                id: 'ksrc_file',
                batch_id: 'batch_docs',
                kind: 'file',
                relative_path: 'contracts\\Lease.pdf',
                status: 'active',
                node_count: 4,
            },
            {
                id: 'ksrc_url',
                kind: 'url',
                uri: 'https://example.com/policy',
                title: 'Policy page',
                status: 'active',
                node_count: 2,
            },
        ]);
        vi.mocked(SelectKnowledgeSnapshotExportPath).mockResolvedValueOnce('D:\\tmp\\knowledge.jsonl');
        render(<KnowledgeSettingsPanel lang="en" />);

        fireEvent.click(screen.getByRole('tab', { name: 'Export' }));

        expect(await screen.findByText('D:\\docs\\contracts')).toBeTruthy();
        expect(screen.getByText('Lease.pdf')).toBeTruthy();
        expect(screen.getByText('Policy page')).toBeTruthy();
        fireEvent.click(screen.getByLabelText(/Lease.pdf/i));
        fireEvent.click(screen.getByRole('button', { name: 'Export Selected to File' }));

        await waitFor(() => expect(KnowledgeExportSnapshotWithOptions).toHaveBeenCalledWith(expect.objectContaining({
            output_path: 'D:\\tmp\\knowledge.jsonl',
            source_ids: ['ksrc_file'],
            redact_sensitive: true,
        })));
    });

    it('drops stale selected source IDs after refreshing the export source list', async () => {
        vi.mocked(KnowledgeListSources)
            .mockResolvedValueOnce([{
                id: 'ksrc_old',
                kind: 'file',
                relative_path: 'old.md',
                status: 'active',
            }])
            .mockResolvedValueOnce([{
                id: 'ksrc_new',
                kind: 'file',
                relative_path: 'new.md',
                status: 'active',
            }]);
        render(<KnowledgeSettingsPanel lang="en" />);

        fireEvent.click(screen.getByRole('tab', { name: 'Export' }));

        expect(await screen.findByText('old.md')).toBeTruthy();
        fireEvent.click(screen.getByLabelText(/old.md/i));
        expect(screen.getByRole('button', { name: 'Export Selected to File' })).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: 'Refresh List' }));

        expect(await screen.findByText('new.md')).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Export Full to File' })).toBeTruthy();
    });

    it('keeps selected disabled source IDs scoped while include-disabled remains off', async () => {
        vi.mocked(KnowledgeListSources).mockResolvedValueOnce([
            {
                id: 'ksrc_active',
                kind: 'file',
                relative_path: 'active.md',
                status: 'active',
                node_count: 1,
            },
            {
                id: 'ksrc_disabled',
                kind: 'file',
                relative_path: 'disabled.md',
                status: 'disabled',
                node_count: 1,
            },
        ]);
        render(<KnowledgeSettingsPanel lang="en" />);

        fireEvent.click(screen.getByRole('tab', { name: 'Export' }));

        expect(await screen.findByText('active.md')).toBeTruthy();
        fireEvent.click(screen.getByLabelText(/active.md/i));
        fireEvent.click(screen.getByLabelText(/disabled.md/i));
        fireEvent.click(screen.getByRole('button', { name: 'Share Selected to Hub' }));
        expect(await screen.findByRole('dialog', { name: 'Hub share settings' })).toBeTruthy();
        expect(screen.getByText(/1 disabled selected source/)).toBeTruthy();
        fireEvent.change(screen.getByPlaceholderText('Required knowledge description for readers and Hub management'), { target: { value: 'Share active only' } });
        fireEvent.click(screen.getByRole('button', { name: 'Publish Selected to Hub' }));

        await waitFor(() => expect(KnowledgeShareToHub).toHaveBeenCalledWith(expect.objectContaining({
            source_ids: ['ksrc_active', 'ksrc_disabled'],
            include_disabled: false,
        })));
        await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Hub share settings' })).toBeNull());
    });


    it('shows Hub share content summary and warnings after sharing', async () => {
        render(<KnowledgeSettingsPanel lang="en" />);

        fireEvent.click(screen.getByRole('tab', { name: 'Export' }));
        fireEvent.click(await screen.findByRole('button', { name: 'Share Full to Hub' }));
        expect(await screen.findByRole('dialog', { name: 'Hub share settings' })).toBeTruthy();
        const description = await screen.findByPlaceholderText('Required knowledge description for readers and Hub management') as HTMLTextAreaElement;
        expect(description.required).toBe(true);
        expect(description.getAttribute('aria-invalid')).toBe('true');
        expect(screen.getByText('Advanced authentication')).toBeTruthy();
        expect(screen.queryByText('Selected items for sharing')).toBeNull();
        expect((screen.getByRole('button', { name: 'Publish Full to Hub' }) as HTMLButtonElement).disabled).toBe(false);
        expect(screen.getByText('Required before Hub sharing; visible to readers and Hub knowledge managers.')).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: 'Publish Full to Hub' }));
        expect((await screen.findByRole('alert')).textContent).toContain('Knowledge description is required before sharing.');
        expect(document.activeElement).toBe(description);
        expect(KnowledgeShareToHub).not.toHaveBeenCalled();
        fireEvent.change(description, { target: { value: 'Portable package' } });
        expect(description.getAttribute('aria-invalid')).toBe('false');
        await waitFor(() => expect(screen.queryByRole('alert')).toBeNull());
        fireEvent.click(screen.getByRole('button', { name: 'Publish Full to Hub' }));

        await waitFor(() => expect(KnowledgeShareToHub).toHaveBeenCalledTimes(1));
        await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Hub share settings' })).toBeNull());
        expect(await screen.findByText('Knowledge ID')).toBeTruthy();
        expect(screen.getByText(/1 importable content sources/)).toBeTruthy();
        expect(screen.getByText('Share warnings')).toBeTruthy();
        expect(screen.getAllByText(/content truncated/).length).toBeGreaterThan(0);
    });
    it('sends successful knowledge actions to toast instead of inline success text', async () => {
        const showToastMessage = vi.fn();
        render(<KnowledgeSettingsPanel lang="en" showToastMessage={showToastMessage} />);

        fireEvent.click(screen.getByRole('tab', { name: 'Ingest' }));
        fireEvent.change(await screen.findByPlaceholderText('Paste text or notes'), { target: { value: 'note body' } });
        fireEvent.click(screen.getByRole('button', { name: 'Save Text' }));

        await waitFor(() => expect(KnowledgeSaveText).toHaveBeenCalledTimes(1));
        expect(showToastMessage).toHaveBeenCalledWith('Text saved to knowledge base successfully.', 3000);
        expect(screen.queryByText('Text saved to knowledge base successfully.')).toBeNull();
    });

    it('runs structured table filters through KnowledgeSearchStructured', async () => {
        render(<KnowledgeSettingsPanel lang="en" />);

        fireEvent.click(screen.getByRole('tab', { name: 'Search' }));
        fireEvent.click(await screen.findByRole('button', { name: 'Table Filters' }));
        await waitFor(() => expect(KnowledgeStructuredCatalog).toHaveBeenCalledTimes(1));
        fireEvent.change(screen.getByPlaceholderText('Column name, e.g. Department'), { target: { value: 'Department' } });
        fireEvent.change(screen.getByPlaceholderText('Text value'), { target: { value: 'Legal' } });
        fireEvent.click(screen.getByRole('button', { name: 'Search' }));

        await waitFor(() => expect(KnowledgeSearchStructured).toHaveBeenCalledTimes(1));
        expect(KnowledgeSearchStructured).toHaveBeenCalledWith(expect.objectContaining({
            column_equals: { Department: 'Legal' },
            limit: 20,
            include_disabled: false,
        }));
    });
});

// @vitest-environment jsdom
import { act, fireEvent, render as rtlRender, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { EventsOn } from '../../../../wailsjs/runtime';
import { UtilitiesPage } from '../UtilitiesPage';
import { DialogProvider } from '../../CustomDialog';

vi.mock('../../../../wailsjs/runtime', () => ({ EventsOn: vi.fn(() => vi.fn()), BrowserOpenURL: vi.fn() }));

const render = (ui: React.ReactElement) => rtlRender(<DialogProvider>{ui}</DialogProvider>);

function installBackend() {
    const backend = {
        GetACPHostStatus: vi.fn().mockRejectedValue(new Error('offline')),
        ListExperts: vi.fn().mockResolvedValue('[]'),
        ListVirtualRepositories: vi.fn().mockResolvedValue('[]'),
        GetVCSClientStatus: vi.fn().mockResolvedValue(JSON.stringify({ kind: 'svn', available: false })),
        SelectVirtualRepositoryRoot: vi.fn().mockResolvedValue('D:\\workspace'),
        SaveVirtualRepository: vi.fn().mockImplementation(async (raw: string) => JSON.stringify({ ...JSON.parse(raw), id: 'vrepo_1' })),
		DeleteVirtualRepository: vi.fn().mockResolvedValue(undefined),
        InspectVirtualRepository: vi.fn().mockResolvedValue('[]'),
		ListRepositoryCredentials: vi.fn().mockResolvedValue('[]'),
		SaveRepositoryCredential: vi.fn().mockImplementation(async (raw: string) => JSON.stringify({ ...JSON.parse(raw), id: 'cred_1' })),
		DeleteRepositoryCredential: vi.fn().mockResolvedValue(undefined),
		ListRepositoryCredentialBindings: vi.fn().mockResolvedValue('{}'),
		SetRepositoryCredentialBinding: vi.fn().mockResolvedValue(undefined),
        OpenVirtualRepository: vi.fn(),
		BindVirtualRepositoryRoot: vi.fn(),
		PreviewVirtualRepositoryRootMigration: vi.fn(),
		MigrateVirtualRepositoryRoot: vi.fn(),
        PreviewVirtualRepositoryOperation: vi.fn(),
        StartVirtualRepositoryOperation: vi.fn(),
        GetVirtualRepositoryOperation: vi.fn(),
        CancelVirtualRepositoryOperation: vi.fn(),
        TestRemoteVirtualRepositoryConnection: vi.fn(),
		CreateRemoteVirtualRepositoryRoot: vi.fn().mockResolvedValue(undefined),
        SaveRemoteVirtualRepository: vi.fn(),
        OpenRemoteVirtualRepository: vi.fn(),
        InspectRemoteVirtualRepository: vi.fn().mockResolvedValue('[]'),
        GetVirtualRepositoryDirectoryStats: vi.fn(),
        GetRemoteVirtualRepositoryDirectoryStats: vi.fn(),
		CreateVirtualRepositoryDirectory: vi.fn().mockResolvedValue(undefined),
		CreateRemoteVirtualRepositoryDirectory: vi.fn().mockResolvedValue(undefined),
		CheckoutVirtualRepositoryNode: vi.fn().mockResolvedValue(undefined),
		CheckoutRemoteVirtualRepositoryNode: vi.fn().mockResolvedValue(undefined),
        StartVirtualRepositoryCodingTask: vi.fn(),
		SyncVirtualRepositories: vi.fn().mockResolvedValue(JSON.stringify({ status: 'success', last_synced_at: '2026-07-23T00:00:00Z' })),
		IsVirtualRepositoryBackgroundSyncPending: vi.fn().mockResolvedValue(false),
		GetVirtualRepositoryBackgroundSyncStatus: vi.fn().mockResolvedValue(JSON.stringify({ pending: false, phase: 'idle' })),
    };
    (window as any).go = { main: { App: backend } };
    return backend;
}

describe('Virtual repository utility', () => {
    beforeEach(() => { vi.clearAllMocks(); installBackend(); });
    afterEach(() => { vi.restoreAllMocks(); delete (window as any).go; });

    it('opens from Utilities and creates a root-backed repository', async () => {
        const backend = (window as any).go.main.App;
        render(<UtilitiesPage lang="en" />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        expect(screen.getByTestId('virtual-repository-workspace')).toBeTruthy();
        fireEvent.click(screen.getByText('New virtual repository'));
        fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Product workspace' } });
        fireEvent.click(screen.getByText('Choose'));
        await waitFor(() => expect(backend.SelectVirtualRepositoryRoot).toHaveBeenCalled());
        fireEvent.click(screen.getByText('Save'));
        await waitFor(() => expect(backend.SaveVirtualRepository).toHaveBeenCalled());
        const saved = JSON.parse(backend.SaveVirtualRepository.mock.calls[0][0]);
        expect(saved.root_path).toBe('D:\\workspace');
        expect(saved.nodes).toEqual([]);
    });

	it('gives every action button a functional tooltip title', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		await screen.findByRole('tree', { name: 'Workspace' });
		const workspace = screen.getByTestId('virtual-repository-workspace');
		const buttons = within(workspace).getAllByRole('button');
		expect(buttons.length).toBeGreaterThan(5);
		const missing = buttons
			.filter((button) => !(button.getAttribute('title') || '').trim())
			.map((button) => (button.textContent || button.getAttribute('aria-label') || button.className || 'button').trim().slice(0, 80));
		expect(missing).toEqual([]);
		// Spot-check that short labels still carry a fuller functional description.
		expect(screen.getByTitle('Refresh checkout and change status for all mappings')).toBeTruthy();
		expect(screen.getByTitle('Create a virtual folder at this level')).toBeTruthy();
		expect(screen.getByTitle('Add a Git/SVN/local directory mapping')).toBeTruthy();
		expect(screen.getByTitle('Start a coding task at this repository root')).toBeTruthy();
	});

	it('reports an invalid sync result instead of treating it as successful', async () => {
		const backend = (window as any).go.main.App;
		backend.SyncVirtualRepositories.mockResolvedValue(JSON.stringify({ status: 'unsupported' }));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Sync now'));
		expect((await screen.findByRole('alert')).textContent).toContain('invalid status');
	});

	it('shows a transient cloud revision race as a soft error after the backend retries', async () => {
		const backend = (window as any).go.main.App;
		backend.SyncVirtualRepositories.mockResolvedValue(JSON.stringify({ status: 'conflict', reason: 'revision_race', message: 'Cloud data changed while syncing; try again' }));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Sync now'));
		expect(await screen.findByText('Cloud data changed while syncing; try again')).toBeTruthy();
		expect((await screen.findByRole('alert')).textContent).toContain('Cloud data changed while syncing');
		expect(backend.SyncVirtualRepositories).toHaveBeenCalledTimes(1);
	});

	it('disables manual sync when the backend observes a queued automatic sync', async () => {
		const backend = (window as any).go.main.App;
		backend.SyncVirtualRepositories.mockResolvedValue(JSON.stringify({ status: 'busy' }));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Sync now'));
		const button = await screen.findByRole('button', { name: 'Syncing in the background…' });
		expect(button).toHaveProperty('disabled', true);
	});

	it('rechecks backend sync state after a busy response when its completion event is missed', async () => {
		vi.useFakeTimers();
		try {
			const backend = (window as any).go.main.App;
			backend.SyncVirtualRepositories.mockResolvedValue(JSON.stringify({ status: 'busy' }));
			backend.GetVirtualRepositoryBackgroundSyncStatus
				.mockResolvedValueOnce(JSON.stringify({ pending: false, phase: 'idle' }))
				.mockResolvedValueOnce(JSON.stringify({ pending: false, phase: 'idle' }));
			render(<UtilitiesPage lang="en" />);
			fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
			await act(async () => {});
			fireEvent.click(screen.getByRole('button', { name: 'Sync now' }));
			await act(async () => {});
			expect(screen.getByRole('button', { name: 'Syncing in the background…' })).toHaveProperty('disabled', true);
			await act(async () => { await vi.advanceTimersByTimeAsync(1000); });
			expect(screen.getByRole('button', { name: 'Sync now' })).toHaveProperty('disabled', false);
		} finally {
			vi.useRealTimers();
		}
	});


	it('disables manual sync while an automatic virtual repository sync is pending', async () => {
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		const handler = vi.mocked(EventsOn).mock.calls.find(([event]) => event === 'virtual-repository:background-sync')?.[1] as ((raw: unknown) => void);
		expect(handler).toBeTypeOf('function');
		act(() => handler({ pending: true, phase: 'running' }));
		const button = await screen.findByRole('button', { name: 'Syncing in the background…' });
		expect(button).toHaveProperty('disabled', true);
		act(() => handler({ pending: false, phase: 'idle' }));
		expect(await screen.findByRole('button', { name: 'Sync now' })).toHaveProperty('disabled', false);
	});

	it('keeps the Sync button enabled during a failed-retry wait and shows the error', async () => {
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		const handler = vi.mocked(EventsOn).mock.calls.find(([event]) => event === 'virtual-repository:background-sync')?.[1] as ((raw: unknown) => void);
		act(() => handler({ pending: false, phase: 'retry_wait', message: 'read virtual repository "vrepo-test" for sync: connect SSH', next_retry_at: '2026-07-24T12:00:00Z' }));
		expect(await screen.findByRole('button', { name: 'Sync now' })).toHaveProperty('disabled', false);
		expect((await screen.findByRole('status')).textContent).toMatch(/Automatic sync failed|will retry|vrepo-test/i);
	});

	it('keeps manual sync disabled until the automatic sync state is known', async () => {
		const backend = (window as any).go.main.App;
		let resolveStatus!: (value: string) => void;
		backend.GetVirtualRepositoryBackgroundSyncStatus.mockReturnValue(new Promise((resolve) => { resolveStatus = resolve; }));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		const checking = await screen.findByRole('button', { name: 'Checking sync status…' });
		expect(checking).toHaveProperty('disabled', true);
		fireEvent.click(checking);
		expect(backend.SyncVirtualRepositories).not.toHaveBeenCalled();
		await act(async () => resolveStatus(JSON.stringify({ pending: false, phase: 'idle' })));
		expect(await screen.findByRole('button', { name: 'Sync now' })).toHaveProperty('disabled', false);
	});

	it('does not let an initial sync-state response overwrite a newer background-sync event', async () => {
		const backend = (window as any).go.main.App;
		let resolveStatus!: (value: string) => void;
		backend.GetVirtualRepositoryBackgroundSyncStatus.mockReturnValue(new Promise((resolve) => { resolveStatus = resolve; }));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		const handler = vi.mocked(EventsOn).mock.calls.find(([event]) => event === 'virtual-repository:background-sync')?.[1] as ((raw: unknown) => void);
		expect(handler).toBeTypeOf('function');
		act(() => handler({ pending: true, phase: 'running' }));
		await act(async () => resolveStatus(JSON.stringify({ pending: false, phase: 'idle' })));
		const button = await screen.findByRole('button', { name: 'Syncing in the background…' });
		expect(button).toHaveProperty('disabled', true);
	});

	it('does not let a delayed fallback poll overwrite a newer background-sync event', async () => {
		vi.useFakeTimers();
		try {
			const backend = (window as any).go.main.App;
			let resolvePoll!: (value: string) => void;
			backend.GetVirtualRepositoryBackgroundSyncStatus
				.mockResolvedValueOnce(JSON.stringify({ pending: false, phase: 'idle' }))
				.mockReturnValueOnce(new Promise((resolve) => { resolvePoll = resolve; }));
			backend.SyncVirtualRepositories.mockResolvedValue(JSON.stringify({ status: 'busy' }));
			render(<UtilitiesPage lang="en" />);
			fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
			await act(async () => {});
			fireEvent.click(screen.getByRole('button', { name: 'Sync now' }));
			await act(async () => { await vi.advanceTimersByTimeAsync(1000); });
			const handler = vi.mocked(EventsOn).mock.calls.find(([event]) => event === 'virtual-repository:background-sync')?.[1] as ((raw: unknown) => void);
			act(() => handler({ pending: true, phase: 'running' }));
			await act(async () => resolvePoll(JSON.stringify({ pending: false, phase: 'idle' })));
			expect(screen.getByRole('button', { name: 'Syncing in the background…' })).toHaveProperty('disabled', true);
		} finally {
			vi.useRealTimers();
		}
	});

	it('saves a remote mapping without asking for a relative path', async () => {
        const backend = (window as any).go.main.App;
        const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: '/srv/workspace', remote: { host: 'example.com', user: 'deploy' }, nodes: [] };
        backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
        backend.OpenRemoteVirtualRepository.mockResolvedValue(JSON.stringify(repository));
        render(<UtilitiesPage lang="en" />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        fireEvent.click(await screen.findByText('Workspace'));
        await screen.findByRole('tree', { name: 'Workspace' });
        fireEvent.click(screen.getByLabelText('Add mapping'));
        fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Library' } });
		fireEvent.change(screen.getByLabelText('Repository URL'), { target: { value: 'https://example.com/library.git' } });
		fireEvent.click(screen.getByText('Save'));
		await waitFor(() => expect(backend.SaveRemoteVirtualRepository).toHaveBeenCalled());
		const saved = JSON.parse(backend.SaveRemoteVirtualRepository.mock.calls[0][0]).repository;
		expect(saved.nodes[0].repository.relative_path).toBe('Library');
	});

	it('runs a root-migration preflight and shows the resulting copy scope', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		backend.SelectVirtualRepositoryRoot.mockResolvedValue('D:\\workspace-moved');
		backend.PreviewVirtualRepositoryRootMigration.mockResolvedValue(JSON.stringify({
			repository_id: 'vrepo_1', source_root: 'D:\\workspace', destination_root: 'D:\\workspace-moved', remote: false,
			source_file_count: 12, source_size_bytes: 2048, destination_file_count: 3, destination_size_bytes: 512,
			destination_exists: true, destination_has_manifest: false, can_migrate: true,
		}));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		fireEvent.click(await screen.findByText('Move root'));
		fireEvent.click(screen.getByRole('button', { name: 'Choose destination' }));
		await waitFor(() => expect(backend.SelectVirtualRepositoryRoot).toHaveBeenCalled());
		fireEvent.click(screen.getByRole('button', { name: 'Check migration' }));
		await waitFor(() => expect(backend.PreviewVirtualRepositoryRootMigration).toHaveBeenCalledWith(JSON.stringify({ repository_id: 'vrepo_1', destination_root: 'D:\\workspace-moved' })));
		expect(await screen.findByText('Preflight passed. This repository is ready to move.')).toBeTruthy();
		expect(screen.getByText('12 · 2,048 bytes')).toBeTruthy();
	});

	it('asks the user to bind a synchronized repository that has no local root', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Synced workspace', root_path: '', nodes: [], available: false, unbound: true, error_code: 'location_unavailable' };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.SelectVirtualRepositoryRoot.mockResolvedValue('C:\\workspace');
		backend.BindVirtualRepositoryRoot.mockResolvedValue(JSON.stringify({ ...repository, root_path: 'C:\\workspace', available: true, unbound: false }));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		expect(await screen.findByText('Root directory unavailable')).toBeTruthy();
		fireEvent.click(screen.getByRole('button', { name: 'Set local root' }));
		expect(await screen.findByText('This setting is stored only on this computer and never replaces another device’s location.')).toBeTruthy();
		fireEvent.click(screen.getByRole('button', { name: 'Choose destination' }));
		await waitFor(() => expect(backend.SelectVirtualRepositoryRoot).toHaveBeenCalled());
		fireEvent.click(screen.getByRole('button', { name: 'Bind root' }));
		await waitFor(() => expect(backend.BindVirtualRepositoryRoot).toHaveBeenCalledWith(JSON.stringify({ repository_id: 'vrepo_1', root_path: 'C:\\workspace' })));
	});

	it('reconnects an unavailable local root only to its matching repository', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\offline', nodes: [], available: false, root_repair: true, error_code: 'location_unavailable' };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.SelectVirtualRepositoryRoot.mockResolvedValue('C:\\workspace');
		backend.BindVirtualRepositoryRoot.mockResolvedValue(JSON.stringify({ ...repository, root_path: 'C:\\workspace', available: true, root_repair: false }));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		const listReconnect = await screen.findByRole('button', { name: 'Reconnect local root' });
		expect(listReconnect.getAttribute('title')).toBe('Reconnect the matching local root directory');
		fireEvent.click(listReconnect);
		expect(await screen.findByText('Choose a directory containing the same virtual repository manifest. No directory contents will be initialized or overwritten.')).toBeTruthy();
		fireEvent.click(screen.getByRole('button', { name: 'Choose destination' }));
		await waitFor(() => expect(backend.SelectVirtualRepositoryRoot).toHaveBeenCalled());
		const dialogReconnect = screen.getByRole('button', { name: 'Reconnect root' });
		// Must describe reconnect, not the generic bind-root tip.
		expect(dialogReconnect.getAttribute('title')).toBe('Reconnect the matching local root directory');
		fireEvent.click(dialogReconnect);
		await waitFor(() => expect(backend.BindVirtualRepositoryRoot).toHaveBeenCalledWith(JSON.stringify({ repository_id: 'vrepo_1', root_path: 'C:\\workspace' })));
	});

	it('checks a mapping status immediately after saving and offers checkout', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		backend.InspectVirtualRepository.mockImplementation(async () => JSON.stringify([{ node_id: JSON.parse(backend.SaveVirtualRepository.mock.calls[0][0]).nodes[0].id, kind: 'git', exists: false, is_repository: false, clean: true, error_code: 'not_checked_out' }]));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		fireEvent.click(await screen.findByLabelText('Add mapping'));
		fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Source' } });
		fireEvent.change(screen.getByLabelText('Repository URL'), { target: { value: 'https://example.com/source.git' } });
		fireEvent.click(screen.getByText('Save'));
		await waitFor(() => expect(backend.InspectVirtualRepository).toHaveBeenCalledWith('D:\\workspace'));
		expect(await screen.findByText('Not checked out')).toBeTruthy();
		fireEvent.click(screen.getByText('Checkout repository'));
		const nodeID = JSON.parse(backend.SaveVirtualRepository.mock.calls[0][0]).nodes[0].id;
		await waitFor(() => expect(backend.CheckoutVirtualRepositoryNode).toHaveBeenCalledWith('vrepo_1', nodeID));
	});

	it('recognizes an existing checkout on open and does not offer checkout again', async () => {
		const backend = (window as any).go.main.App;
		const repository = {
			version: 1,
			id: 'vrepo_1',
			name: 'Workspace',
			root_path: 'D:\\workspace',
			nodes: [{ id: 'source', name: 'Source', order: 1, repository: { kind: 'git', relative_path: 'Source', remote_url: 'https://example.com/source.git', enabled: true } }],
		};
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		backend.InspectVirtualRepository.mockResolvedValue(JSON.stringify([{
			node_id: 'source', kind: 'git', path: 'D:\\workspace\\Source', exists: true,
			is_repository: true, clean: true, branch: 'main', remote_url: 'https://example.com/source.git',
		}]));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		await waitFor(() => expect(backend.InspectVirtualRepository).toHaveBeenCalledWith('D:\\workspace'));
		fireEvent.click(await screen.findByRole('treeitem', { name: /Source.*git/i }));
		expect(await screen.findByText('Checked out · Clean')).toBeTruthy();
		expect(screen.queryByRole('button', { name: 'Checkout repository' })).toBeNull();
	});

	it('rejects incomplete status responses instead of hiding checkout', async () => {
		const backend = (window as any).go.main.App;
		const repository = {
			version: 1,
			id: 'vrepo_1',
			name: 'Workspace',
			root_path: 'D:\\workspace',
			nodes: [
				{ id: 'source', name: 'Source', order: 1, repository: { kind: 'git', relative_path: 'Source', remote_url: 'https://example.com/source.git', enabled: true } },
				{ id: 'docs', name: 'Docs', order: 2, repository: { kind: 'git', relative_path: 'Docs', remote_url: 'https://example.com/docs.git', enabled: true } },
			],
		};
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		backend.InspectVirtualRepository.mockResolvedValue(JSON.stringify([{
			node_id: 'source', kind: 'git', path: 'D:\\workspace\\Source', exists: true,
			is_repository: true, clean: true, branch: 'main', remote_url: 'https://example.com/source.git',
		}]));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		await waitFor(() => expect(backend.InspectVirtualRepository).toHaveBeenCalledWith('D:\\workspace'));
		expect((await screen.findByRole('alert')).textContent).toContain('returned a status for an unknown mapping');
		fireEvent.click(await screen.findByRole('treeitem', { name: /Source.*git/i }));
		expect(screen.getByRole('button', { name: 'Checkout repository' })).toBeTruthy();
	});

	it('shows checkout progress until the mapping checkout finishes', async () => {
		const backend = (window as any).go.main.App;
		let finishCheckout!: () => void;
		backend.CheckoutVirtualRepositoryNode.mockReturnValue(new Promise<void>((resolve) => { finishCheckout = resolve; }));
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		backend.InspectVirtualRepository.mockImplementation(async () => JSON.stringify([{ node_id: JSON.parse(backend.SaveVirtualRepository.mock.calls[0][0]).nodes[0].id, kind: 'git', exists: false, is_repository: false, clean: true, error_code: 'not_checked_out' }]));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		fireEvent.click(await screen.findByLabelText('Add mapping'));
		fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Source' } });
		fireEvent.change(screen.getByLabelText('Repository URL'), { target: { value: 'https://example.com/source.git' } });
		fireEvent.click(screen.getByText('Save'));
		const checkout = await screen.findByRole('button', { name: 'Checkout repository' });
		fireEvent.click(checkout);
		fireEvent.click(checkout);
		const pendingCheckout = await screen.findByRole('button', { name: 'Checking out…' });
		expect((pendingCheckout as HTMLButtonElement).disabled).toBe(true);
		expect(pendingCheckout.getAttribute('aria-busy')).toBe('true');
		expect(pendingCheckout.querySelector('.vrepo-button-spinner')).toBeTruthy();
		expect(backend.CheckoutVirtualRepositoryNode).toHaveBeenCalledTimes(1);
		await act(async () => { finishCheckout(); });
		await waitFor(() => expect(screen.getByRole('button', { name: 'Checkout repository' }).getAttribute('aria-busy')).not.toBe('true'));
	});

	it('checks out a new mapping only when explicitly requested after saving', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		backend.SaveVirtualRepository.mockImplementation(async (raw: string) => JSON.stringify({ ...JSON.parse(raw), id: 'vrepo_1' }));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		fireEvent.click(await screen.findByLabelText('Add mapping'));
		fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Source' } });
		fireEvent.change(screen.getByLabelText('Repository URL'), { target: { value: 'https://example.com/source.git' } });
		fireEvent.click(screen.getByLabelText('Checkout after saving'));
		fireEvent.click(screen.getByText('Save'));
		await waitFor(() => expect(backend.CheckoutVirtualRepositoryNode).toHaveBeenCalledWith('vrepo_1', expect.any(String)));
		await waitFor(() => expect(backend.InspectVirtualRepository).toHaveBeenCalledTimes(3));
	});

	it('does not start checkout while saving unless checkout after saving is selected', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		fireEvent.click(await screen.findByLabelText('Add mapping'));
		fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Source' } });
		fireEvent.change(screen.getByLabelText('Repository URL'), { target: { value: 'https://example.com/source.git' } });
		fireEvent.click(screen.getByText('Save'));
		await waitFor(() => expect(backend.SaveVirtualRepository).toHaveBeenCalled());
		await waitFor(() => expect(backend.InspectVirtualRepository).toHaveBeenCalled());
		expect(backend.CheckoutVirtualRepositoryNode).not.toHaveBeenCalled();
	});

	it('does not check out a disabled mapping even when checkout after saving remains selected', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		fireEvent.click(await screen.findByLabelText('Add mapping'));
		fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Source' } });
		fireEvent.change(screen.getByLabelText('Repository URL'), { target: { value: 'https://example.com/source.git' } });
		fireEvent.click(screen.getByLabelText('Checkout after saving'));
		fireEvent.click(screen.getByLabelText('Enabled'));
		expect((screen.getByLabelText('Checkout after saving') as HTMLInputElement).disabled).toBe(true);
		fireEvent.click(screen.getByText('Save'));
		await waitFor(() => expect(backend.SaveVirtualRepository).toHaveBeenCalled());
		expect(backend.CheckoutVirtualRepositoryNode).not.toHaveBeenCalled();
	});

	it('creates a saved local mapping directory even when it is empty', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		fireEvent.click(await screen.findByLabelText('Add mapping'));
		fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Artifacts' } });
		fireEvent.change(screen.getByLabelText('Type'), { target: { value: 'local' } });
		fireEvent.click(screen.getByText('Save'));
		await waitFor(() => expect(backend.CreateVirtualRepositoryDirectory).toHaveBeenCalledWith('D:\\workspace', 'Artifacts'));
	});
	it('reports malformed status responses instead of treating a mapping as healthy', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		backend.InspectVirtualRepository.mockResolvedValue(JSON.stringify([{ node_id: 'source', kind: 'git', exists: 'yes', is_repository: false, clean: true }]));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		fireEvent.click(await screen.findByLabelText('Add mapping'));
		fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Source' } });
		fireEvent.change(screen.getByLabelText('Repository URL'), { target: { value: 'https://example.com/source.git' } });
		fireEvent.click(screen.getByText('Save'));
		expect(await screen.findByText('Inspect virtual repository returned invalid statuses')).toBeTruthy();
		expect(screen.getByRole('button', { name: 'Checkout repository' })).toBeTruthy();
	});

	it('rejects status responses for mappings outside the active repository', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		backend.InspectVirtualRepository.mockResolvedValue(JSON.stringify([{ node_id: 'unknown_node', kind: 'git', exists: true, is_repository: true, clean: true }]));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		fireEvent.click(await screen.findByLabelText('Add mapping'));
		fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Source' } });
		fireEvent.change(screen.getByLabelText('Repository URL'), { target: { value: 'https://example.com/source.git' } });
		fireEvent.click(screen.getByText('Save'));
		expect(await screen.findByText('Inspect virtual repository returned a status for an unknown mapping')).toBeTruthy();
		expect(screen.getByRole('button', { name: 'Checkout repository' })).toBeTruthy();
	});

	it('reports a status refresh failure separately after checkout succeeds', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		backend.InspectVirtualRepository.mockImplementation(async () => {
			const nodeID = JSON.parse(backend.SaveVirtualRepository.mock.calls[0][0]).nodes[0].id;
			return backend.InspectVirtualRepository.mock.calls.length === 1
				? JSON.stringify([{ node_id: nodeID, kind: 'git', exists: false, is_repository: false, clean: true, error_code: 'not_checked_out' }])
				: JSON.stringify([{ node_id: 'unexpected_node', kind: 'git', exists: true, is_repository: true, clean: true }]);
		});
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		fireEvent.click(await screen.findByLabelText('Add mapping'));
		fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Source' } });
		fireEvent.change(screen.getByLabelText('Repository URL'), { target: { value: 'https://example.com/source.git' } });
		fireEvent.click(screen.getByLabelText('Checkout after saving'));
		fireEvent.click(screen.getByText('Save'));
		await waitFor(() => expect(backend.CheckoutVirtualRepositoryNode).toHaveBeenCalledTimes(1));
		expect((await screen.findByRole('alert')).textContent).toContain('checkout completed, but the status refresh failed');
	});

	it('keeps a saved mapping and exposes a retry when checkout after saving fails', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		backend.CheckoutVirtualRepositoryNode.mockRejectedValue(new Error('network unavailable'));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		fireEvent.click(await screen.findByLabelText('Add mapping'));
		fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Source' } });
		fireEvent.change(screen.getByLabelText('Repository URL'), { target: { value: 'https://example.com/source.git' } });
		fireEvent.click(screen.getByLabelText('Checkout after saving'));
		fireEvent.click(screen.getByText('Save'));
		expect((await screen.findByRole('alert')).textContent).toContain('mapping was saved, but checkout failed');
		expect(screen.getByRole('treeitem', { name: /Source.*git/i })).toBeTruthy();
		expect(screen.getByRole('button', { name: 'Checkout repository' })).toBeTruthy();
	});

    it('coalesces rapid directory-picker clicks while creating a repository', async () => {
        const backend = (window as any).go.main.App;
        let resolveRoot!: (path: string) => void;
        backend.SelectVirtualRepositoryRoot.mockReturnValue(new Promise((resolve) => { resolveRoot = resolve; }));
        render(<UtilitiesPage lang="en" />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        fireEvent.click(screen.getByText('New virtual repository'));
        const choose = screen.getByText('Choose');
        fireEvent.click(choose);
        fireEvent.click(choose);
        expect(backend.SelectVirtualRepositoryRoot).toHaveBeenCalledTimes(1);
        expect((await screen.findAllByText('Loading…')).length).toBeGreaterThan(0);
        expect((screen.getByText('Save') as HTMLButtonElement).disabled).toBe(true);
        expect((screen.getByText('Cancel') as HTMLButtonElement).disabled).toBe(true);
        resolveRoot('D:\\workspace');
        await waitFor(() => expect(screen.getByDisplayValue('D:\\workspace')).toBeTruthy());
    });

	    it('does not show a stale refresh error after switching repositories', async () => {
        const backend = (window as any).go.main.App;
        const first = { version: 1, id: 'vrepo_1', name: 'First workspace', root_path: 'D:\\first', nodes: [] };
        const second = { version: 1, id: 'vrepo_2', name: 'Second workspace', root_path: 'D:\\second', nodes: [] };
        backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([first]));
        backend.OpenVirtualRepository.mockImplementation(async (root: string) => JSON.stringify(root === 'D:\\second' ? second : first));
        backend.SelectVirtualRepositoryRoot.mockResolvedValue('D:\\second');
        let rejectRefresh!: (error: Error) => void;
        backend.InspectVirtualRepository
            .mockReturnValueOnce(new Promise((_, reject) => { rejectRefresh = reject; }))
            .mockResolvedValue('[]');

        render(<UtilitiesPage lang="en" />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        fireEvent.click(await screen.findByText('First workspace'));
        const [refresh] = await screen.findAllByText('Refresh status');
        fireEvent.click(refresh);
        await waitFor(() => expect(backend.InspectVirtualRepository).toHaveBeenCalledWith('D:\\first'));
        await act(async () => rejectRefresh(new Error('first workspace is unavailable')));
        fireEvent.click(screen.getByText('Open existing root'));
        expect(await screen.findByRole('tree', { name: 'Second workspace' })).toBeTruthy();
	        expect(screen.queryByText('first workspace is unavailable')).toBeNull();
	    });

		it('does not show a stale automatic inspection error after a newer refresh succeeds', async () => {
			const backend = (window as any).go.main.App;
			const repository = {
				version: 1,
				id: 'vrepo_1',
				name: 'Workspace',
				root_path: 'D:\\workspace',
				nodes: [{ id: 'source', name: 'Source', order: 1, repository: { kind: 'git', relative_path: 'Source', enabled: true } }],
			};
			backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
			backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
			let rejectAutomaticInspection!: (error: Error) => void;
			backend.InspectVirtualRepository
				.mockReturnValueOnce(new Promise((_, reject) => { rejectAutomaticInspection = reject; }))
				.mockResolvedValue(JSON.stringify([{ node_id: 'source', kind: 'git', exists: true, is_repository: true, clean: true }]));

			render(<UtilitiesPage lang="en" />);
			fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
			fireEvent.click(await screen.findByText('Workspace'));
			await waitFor(() => expect(backend.InspectVirtualRepository).toHaveBeenCalledTimes(1));
			fireEvent.click(screen.getByText('Refresh status'));
			await waitFor(() => expect(backend.InspectVirtualRepository).toHaveBeenCalledTimes(2));
			await act(async () => rejectAutomaticInspection(new Error('automatic inspection is unavailable')));

			expect(screen.queryByText('automatic inspection is unavailable')).toBeNull();
		});

	    it('keeps the newest recent-repository list when refreshes resolve out of order', async () => {
        const backend = (window as any).go.main.App;
        let resolveInitial!: (value: string) => void;
        let resolveAfterSave!: (value: string) => void;
        backend.ListVirtualRepositories
            .mockReturnValueOnce(new Promise((resolve) => { resolveInitial = resolve; }))
            .mockReturnValueOnce(new Promise((resolve) => { resolveAfterSave = resolve; }));
        backend.SaveVirtualRepository.mockImplementation(async (raw: string) => JSON.stringify({ ...JSON.parse(raw), id: 'vrepo_1' }));

        render(<UtilitiesPage lang="en" />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        fireEvent.click(screen.getByText('New virtual repository'));
        fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'New workspace' } });
        fireEvent.click(screen.getByText('Choose'));
        await waitFor(() => expect(backend.SelectVirtualRepositoryRoot).toHaveBeenCalled());
        fireEvent.click(screen.getByText('Save'));
        await waitFor(() => expect(backend.ListVirtualRepositories).toHaveBeenCalledTimes(2));

        resolveAfterSave(JSON.stringify([{ id: 'vrepo_1', name: 'New workspace', root_path: 'D:\\workspace' }]));
        await waitFor(() => expect(screen.getAllByRole('button', { name: /New workspace/ }).some((button) => button.classList.contains('vrepo-repository-list__item'))).toBe(true));
        await act(async () => resolveInitial('[]'));
        expect(screen.getAllByRole('button', { name: /New workspace/ }).some((button) => button.classList.contains('vrepo-repository-list__item'))).toBe(true);
    });

    it('does not show stale directory statistics after selecting another mapping', async () => {
        const backend = (window as any).go.main.App;
        const repository = {
            version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', nodes: [
                { id: 'node_1', name: 'First folder', order: 1, repository: { kind: 'local', relative_path: 'first', enabled: true } },
                { id: 'node_2', name: 'Second folder', order: 2, repository: { kind: 'local', relative_path: 'second', enabled: true } },
            ],
        };
        backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
        backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
        let resolveStats!: (value: string) => void;
        backend.GetVirtualRepositoryDirectoryStats.mockReturnValue(new Promise((resolve) => { resolveStats = resolve; }));

        render(<UtilitiesPage lang="en" />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        fireEvent.click(await screen.findByText('Workspace'));
        fireEvent.click(await screen.findByText('First folder'));
        fireEvent.click(screen.getByText('Calculate size'));
        await waitFor(() => expect(backend.GetVirtualRepositoryDirectoryStats).toHaveBeenCalledWith('D:\\workspace', 'First folder'));
        fireEvent.click(screen.getByText('Second folder'));
        await act(async () => resolveStats(JSON.stringify({ file_count: 99, size_bytes: 999 })));
        expect(screen.queryByText('99')).toBeNull();
        expect(screen.queryByText('999 bytes')).toBeNull();
    });

	it('explains SVN recovery options when auto-discovery fails', async () => {
        render(<UtilitiesPage lang="en" />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        expect(await screen.findByText('SVN command line client not found')).toBeTruthy();
        expect(screen.getByText('SVN command line client not found').closest('details')?.open).toBe(false);
        fireEvent.click(screen.getByText('SVN client'));
        const client = screen.getByText('SVN client').closest('details')!;
        expect(within(client).getByText('Search again')).toBeTruthy();
        expect(within(client).getByText('Choose svn')).toBeTruthy();
        expect(within(client).getByText('Installation guide')).toBeTruthy();
    });

	it('manages existing repositories from a searchable list before opening one', async () => {
        const backend = (window as any).go.main.App;
        const local = { version: 1, id: 'local_1', name: 'Product workspace', root_path: 'D:\\product', nodes: [{ id: 'source', name: 'Source', order: 10, repository: { kind: 'git', relative_path: 'src', enabled: true } }] };
        const remote = { version: 1, id: 'remote_1', name: 'Release workspace', root_path: '/srv/release', remote: { host: 'build.example.com', user: 'deploy' }, nodes: [] };
        backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([local, remote]));
        backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(local));
        render(<UtilitiesPage lang="en" />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        expect(await screen.findByRole('complementary', { name: 'Repositories' })).toBeTruthy();
        expect(screen.getByText('2 repositories')).toBeTruthy();
        expect(screen.getByText('1 mappings')).toBeTruthy();
        fireEvent.change(screen.getByPlaceholderText('Search repositories'), { target: { value: 'release' } });
        expect(screen.getByText('1 repositories')).toBeTruthy();
        expect(screen.getByText('Release workspace')).toBeTruthy();
        expect(screen.queryByText('Product workspace')).toBeNull();
        fireEvent.change(screen.getByPlaceholderText('Search repositories'), { target: { value: 'missing' } });
        expect(screen.getByRole('status').textContent).toContain('No virtual repositories match your search');
        fireEvent.change(screen.getByPlaceholderText('Search repositories'), { target: { value: '' } });
        fireEvent.click(screen.getByText('Product workspace'));
        await waitFor(() => expect(backend.OpenVirtualRepository).toHaveBeenCalledWith('D:\\product'));
        expect(await screen.findByRole('tree', { name: 'Product workspace' })).toBeTruthy();
        expect(screen.getByText('Health overview')).toBeTruthy();
	});

		it('keeps Git and SVN tools ahead of sync actions, separate from repository operations', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));

			const toolbar = screen.getByTestId('virtual-repository-workspace').querySelector('.vrepo-actions')!;
			const topRow = Array.from(toolbar.children).slice(0, 3);
			expect(topRow[0].textContent).toContain('Git client');
			expect(topRow[1].textContent).toContain('SVN client');
			const syncActions = topRow[2] as HTMLElement;
			// Setup is a single top-row sibling of Git/SVN; actions are direct button children.
			expect(syncActions.classList.contains('vrepo-actions__setup')).toBe(true);
			expect(Array.from(syncActions.children).every((child) => child.tagName === 'BUTTON')).toBe(true);
			expect(syncActions.textContent).toContain('Sync now');
			expect(syncActions.textContent).toContain('Open existing root');
			expect(syncActions.textContent).toContain('New virtual repository');
			expect(syncActions.textContent).not.toContain('Workspace actions');
			expect(syncActions.textContent).not.toContain('Sync checked-out repositories');
			await waitFor(() => {
				expect(toolbar.children[toolbar.children.length - 1]?.textContent).toContain('Repository actions');
				expect(syncActions.textContent).toContain('Move root');
				expect(syncActions.querySelectorAll(':scope > button').length).toBeGreaterThanOrEqual(4);
			});
	});

	it('uses a concise tool state while keeping the full unavailable-client detail available', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		backend.GetVCSClientStatus.mockImplementation(async (kind: string) => JSON.stringify({ kind, available: false }));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));

		const gitTool = screen.getByText('Git client').closest('details')!;
		expect(within(gitTool).getByText('Not found')).toBeTruthy();
		const gitSummary = within(gitTool).getByText('Not found').closest('summary')!;
		expect(gitSummary.title).toBe('Git client: Git command line client not found');
		expect(gitSummary.getAttribute('aria-label')).toBe('Git client: Git command line client not found');
		fireEvent.click(within(gitTool).getByText('Git client'));
		expect(within(gitTool).getByText('Choose git')).toBeTruthy();
	});

	it('does not reload the currently open repository from the management list', async () => {
        const backend = (window as any).go.main.App;
        const repository = { version: 1, id: 'local_1', name: 'Product workspace', root_path: 'D:\\product', nodes: [] };
        backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
        backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
        render(<UtilitiesPage lang="en" />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        fireEvent.click(await screen.findByText('Product workspace'));
        await waitFor(() => expect(backend.OpenVirtualRepository).toHaveBeenCalledTimes(1));
        fireEvent.click(screen.getAllByText('Product workspace')[0]);
        expect(backend.OpenVirtualRepository).toHaveBeenCalledTimes(1);
    });

	it('removes a repository from the list through a right-click menu after custom confirmation', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'local_1', name: 'Product workspace', root_path: 'D:\\product', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		const repositoryButton = await screen.findByRole('button', { name: /Product workspace/ });
		fireEvent.contextMenu(repositoryButton, { clientX: 120, clientY: 120 });
		const deleteAction = screen.getByRole('menuitem', { name: 'Delete virtual repository' });
		expect(deleteAction).toBeTruthy();
		await waitFor(() => expect(document.activeElement).toBe(deleteAction));
		expect(repositoryButton.getAttribute('aria-expanded')).toBe('true');
		fireEvent.keyDown(deleteAction, { key: 'Escape' });
		await waitFor(() => expect(screen.queryByRole('menu')).toBeNull());
		await waitFor(() => expect(document.activeElement).toBe(repositoryButton));
		fireEvent.contextMenu(repositoryButton, { clientX: 120, clientY: 120 });
		fireEvent.click(screen.getByRole('menuitem', { name: 'Delete virtual repository' }));
		const dialog = await screen.findByRole('dialog', { name: 'Delete virtual repository' });
		expect(dialog.textContent).toContain('does not delete .vrepo or real files');
		fireEvent.click(within(dialog).getByRole('button', { name: 'Remove' }));
		await waitFor(() => expect(backend.DeleteVirtualRepository).toHaveBeenCalledWith('local_1'));
		await waitFor(() => expect(screen.queryByRole('complementary', { name: 'Repositories' })).toBeNull());
	});

	it('offers Git recovery options when the command line client is unavailable', async () => {
		const backend = (window as any).go.main.App;
		backend.GetVCSClientStatus.mockImplementation(async (kind: string) => JSON.stringify({ kind, available: false }));
		backend.VCSClientExecutableHint = vi.fn().mockResolvedValue('D:\\tools\\git.exe');
		backend.SelectVCSClientExecutable = vi.fn().mockResolvedValue('D:\\tools\\git.exe');
		backend.SetVCSClientExecutable = vi.fn().mockResolvedValue(JSON.stringify({ kind: 'git', available: true, executable: 'D:\\tools\\git.exe', version: 'git version 2.50.0', source: 'user' }));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		expect(await screen.findByText('Git command line client not found')).toBeTruthy();
		fireEvent.click(screen.getByText('Git client'));
		const client = screen.getByText('Git client').closest('details')!;
		expect(within(client).getByText('Search again')).toBeTruthy();
		fireEvent.click(within(client).getByText('Choose git'));
		await waitFor(() => expect(backend.VCSClientExecutableHint).toHaveBeenCalledWith('git'));
		await waitFor(() => expect(backend.SelectVCSClientExecutable).toHaveBeenCalledWith('git', 'D:\\tools\\git.exe'));
		await waitFor(() => expect(backend.SetVCSClientExecutable).toHaveBeenCalledWith('git', 'D:\\tools\\git.exe'));
		expect(within(client).getByText('git version 2.50.0', { exact: false })).toBeTruthy();
		expect(within(client).getByText('(user)', { exact: false })).toBeTruthy();
		backend.ResetVCSClientExecutable = vi.fn().mockResolvedValue(JSON.stringify({ kind: 'git', available: false }));
		fireEvent.click(within(client).getByText('Use automatic search'));
		await waitFor(() => expect(backend.ResetVCSClientExecutable).toHaveBeenCalledWith('git'));
		await waitFor(() => expect(within(client).queryByText('Use automatic search')).toBeNull());
	});

	it('prevents duplicate Git client searches while a search is pending', async () => {
		const backend = (window as any).go.main.App;
		let finishSearch!: (value: string) => void;
		backend.GetVCSClientStatus.mockImplementation(async (kind: string) => JSON.stringify({ kind, available: false }));
		backend.SearchVCSClient = vi.fn().mockReturnValue(new Promise<string>((resolve) => { finishSearch = resolve; }));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Git client'));
		const client = screen.getByText('Git client').closest('details')!;
		const search = within(client).getByText('Search again');
		fireEvent.click(search);
		fireEvent.click(search);
		expect(backend.SearchVCSClient).toHaveBeenCalledTimes(1);
		expect((await within(client).findByText('Loading…')) as HTMLButtonElement).toHaveProperty('disabled', true);
		await act(async () => finishSearch(JSON.stringify({ kind: 'git', available: false })));
		await waitFor(() => expect(within(client).getByText('Search again')).toBeTruthy());
	});

	it('keeps a newer Git client search result when the initial status request finishes late', async () => {
		const backend = (window as any).go.main.App;
		let finishInitialStatus!: (value: string) => void;
		backend.GetVCSClientStatus.mockImplementation((kind: string) => kind === 'git'
			? new Promise<string>((resolve) => { finishInitialStatus = resolve; })
			: Promise.resolve(JSON.stringify({ kind, available: false })));
		backend.SearchVCSClient = vi.fn().mockResolvedValue(JSON.stringify({ kind: 'git', available: true, executable: 'D:\\tools\\git.exe', version: 'git version 2.50.0', source: 'user' }));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		await waitFor(() => expect(backend.GetVCSClientStatus).toHaveBeenCalledWith('git'));
		fireEvent.click(await screen.findByText('Git client'));
		const client = screen.getByText('Git client').closest('details')!;
		fireEvent.click(within(client).getByText('Search again'));
		await screen.findByText('git version 2.50.0', { exact: false });
		await act(async () => finishInitialStatus(JSON.stringify({ kind: 'git', available: false })));
		await waitFor(() => expect(within(client).getByText('git version 2.50.0', { exact: false })).toBeTruthy());
	});

	it('does not start a second repository deletion while the first confirmation is pending', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'local_1', name: 'Product workspace', root_path: 'D:\\product', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		const repositoryButton = await screen.findByRole('button', { name: /Product workspace/ });
		fireEvent.contextMenu(repositoryButton, { clientX: 120, clientY: 120 });
		fireEvent.click(screen.getByRole('menuitem', { name: 'Delete virtual repository' }));
		await screen.findByRole('dialog', { name: 'Delete virtual repository' });
		fireEvent.contextMenu(repositoryButton, { clientX: 120, clientY: 120 });
		expect(screen.queryByRole('menu')).toBeNull();
		expect(backend.DeleteVirtualRepository).not.toHaveBeenCalled();
		fireEvent.click(within(screen.getByRole('dialog', { name: 'Delete virtual repository' })).getByRole('button', { name: 'Cancel' }));
	});

	it('clears the active repository when a trimmed matching id is deleted', async () => {
		const backend = (window as any).go.main.App;
		const listed = { version: 1, id: ' local_1 ', name: 'Product workspace', root_path: 'D:\\product', nodes: [] };
		const opened = { ...listed, id: 'local_1' };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([listed]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(opened));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		const repositoryButton = await screen.findByRole('button', { name: /Product workspace/ });
		fireEvent.click(repositoryButton);
		await screen.findByRole('tree', { name: 'Product workspace' });
		fireEvent.contextMenu(repositoryButton, { clientX: 120, clientY: 120 });
		fireEvent.click(screen.getByRole('menuitem', { name: 'Delete virtual repository' }));
		fireEvent.click(within(await screen.findByRole('dialog', { name: 'Delete virtual repository' })).getByRole('button', { name: 'Remove' }));
		await waitFor(() => expect(backend.DeleteVirtualRepository).toHaveBeenCalledWith('local_1'));
		await waitFor(() => expect(screen.queryByRole('tree', { name: 'Product workspace' })).toBeNull());
	});

	it('keeps the repository visible and reports an unavailable delete backend', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'local_1', name: 'Product workspace', root_path: 'D:\\product', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		delete backend.DeleteVirtualRepository;
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		const repositoryButton = await screen.findByRole('button', { name: /Product workspace/ });
		fireEvent.contextMenu(repositoryButton, { clientX: 120, clientY: 120 });
		fireEvent.click(screen.getByRole('menuitem', { name: 'Delete virtual repository' }));
		fireEvent.click(within(await screen.findByRole('dialog', { name: 'Delete virtual repository' })).getByRole('button', { name: 'Remove' }));
		expect((await screen.findByRole('alert')).textContent).toContain('not supported by this version');
		expect(screen.getByRole('button', { name: /Product workspace/ })).toBeTruthy();
	});

    it('passes the preview repository revision when starting an operation', async () => {
        const backend = (window as any).go.main.App;
        const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', updated_at: '2026-07-22T01:02:03Z', nodes: [] };
        backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
        backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
        backend.PreviewVirtualRepositoryOperation.mockResolvedValue(JSON.stringify({ repository_id: repository.id, updated_at: repository.updated_at, targets: [], skipped_local: 0 }));
        backend.StartVirtualRepositoryOperation.mockResolvedValue(JSON.stringify({ job_id: 'job_1', status: 'running', items: [] }));
        backend.GetVirtualRepositoryOperation.mockResolvedValue(JSON.stringify({ job_id: 'job_1', status: 'running', items: [] }));

        render(<UtilitiesPage lang="en" />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        fireEvent.click(await screen.findByText('Workspace'));
        fireEvent.click(await screen.findByText('Push'));
        fireEvent.click(await screen.findByText('Execute'));

        await waitFor(() => expect(backend.StartVirtualRepositoryOperation).toHaveBeenCalled());
        const request = JSON.parse(backend.StartVirtualRepositoryOperation.mock.calls[0][0]);
        expect(request.expected_repository_id).toBe('vrepo_1');
        expect(request.expected_updated_at).toBe('2026-07-22T01:02:03Z');
    });

	it('previews and starts a safe sync for checked-out repositories', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', updated_at: '2026-07-22T01:02:03Z', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		backend.PreviewVirtualRepositoryOperation.mockResolvedValue(JSON.stringify({ repository_id: repository.id, updated_at: repository.updated_at, targets: [], skipped_local: 0 }));
		backend.StartVirtualRepositoryOperation.mockResolvedValue(JSON.stringify({ job_id: 'job_1', action: 'sync', status: 'success', items: [] }));

		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		fireEvent.click(await screen.findByText('Sync checked-out repositories'));
		expect(await screen.findByText(/fast-forward-only pull/i)).toBeTruthy();
		await waitFor(() => expect(backend.PreviewVirtualRepositoryOperation).toHaveBeenCalled());
		expect(JSON.parse(backend.PreviewVirtualRepositoryOperation.mock.calls[0][0]).action).toBe('sync');
		fireEvent.click(await screen.findByText('Execute'));
		await waitFor(() => expect(backend.StartVirtualRepositoryOperation).toHaveBeenCalled());
		expect(JSON.parse(backend.StartVirtualRepositoryOperation.mock.calls[0][0]).action).toBe('sync');
	});

	it('localizes repository operation errors in Chinese', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: '工作区', root_path: 'D:\\workspace', updated_at: '2026-07-22T01:02:03Z', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		backend.PreviewVirtualRepositoryOperation.mockResolvedValue(JSON.stringify({ repository_id: repository.id, updated_at: repository.updated_at, targets: [{ node_id: 'source', name: '源码', kind: 'git' }], skipped_local: 0 }));
		backend.StartVirtualRepositoryOperation.mockResolvedValue(JSON.stringify({ job_id: 'job_1', action: 'sync', status: 'failed', items: [{ node_id: 'source', name: '源码', status: 'failed', error: 'repository has not been checked out' }] }));

		render(<UtilitiesPage lang="zh" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('工作区'));
		fireEvent.click(await screen.findByText('同步已检出仓库'));
		fireEvent.click(await screen.findByText('执行'));
		await waitFor(() => expect(screen.getByRole('region').textContent).toContain('仓库尚未检出，请先完成检出后再执行此操作。'));
	});

    it('locks tree mutations while an operation is running', async () => {
        const backend = (window as any).go.main.App;
        const repository = {
            version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', updated_at: '2026-07-22T01:02:03Z',
            nodes: [{ id: 'node_1', name: 'Source', order: 10, repository: { kind: 'git', relative_path: 'src', remote_url: 'https://example.com/src.git', enabled: true } }],
        };
        backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
        backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
        backend.PreviewVirtualRepositoryOperation.mockResolvedValue(JSON.stringify({ repository_id: repository.id, updated_at: repository.updated_at, targets: [{ node_id: 'node_1', name: 'Source', kind: 'git' }], skipped_local: 0 }));
        backend.StartVirtualRepositoryOperation.mockResolvedValue(JSON.stringify({ job_id: 'job_1', status: 'running', items: [] }));
        backend.GetVirtualRepositoryOperation.mockResolvedValue(JSON.stringify({ job_id: 'job_1', status: 'running', items: [] }));

        render(<UtilitiesPage lang="en" />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        fireEvent.click(await screen.findByText('Workspace'));
        fireEvent.click(await screen.findByText('Push'));
        fireEvent.click(await screen.findByText('Execute'));

        expect(await screen.findByText(/virtual tree and other operations are locked/i)).toBeTruthy();
        expect((screen.getByLabelText('New virtual folder') as HTMLButtonElement).disabled).toBe(true);
        expect((screen.getByLabelText('Add mapping') as HTMLButtonElement).disabled).toBe(true);
        expect((screen.getByText('Repository credentials') as HTMLButtonElement).disabled).toBe(true);
        expect((screen.getByText('Revert') as HTMLButtonElement).disabled).toBe(true);
        expect((screen.getAllByRole('button', { name: /Workspace/ }).find((button) => button.classList.contains('vrepo-repository-list__item')) as HTMLButtonElement).disabled).toBe(true);
    });

    it('supports standard keyboard navigation through visible tree items', async () => {
        const backend = (window as any).go.main.App;
        const repository = {
            version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace',
            nodes: [
                { id: 'group', name: 'Group', order: 10 },
                { id: 'child', parent_id: 'group', name: 'Child', order: 10, repository: { kind: 'git', relative_path: 'child', enabled: true } },
                { id: 'last', name: 'Last', order: 20, repository: { kind: 'local', relative_path: 'last', enabled: true } },
            ],
        };
        backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
        backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
        render(<UtilitiesPage lang="en" />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        fireEvent.click(await screen.findByText('Workspace'));
        const group = await screen.findByRole('treeitem', { name: /Group/ });
        group.focus();
        fireEvent.keyDown(group, { key: 'ArrowDown' });
        await waitFor(() => expect(document.activeElement?.getAttribute('data-vrepo-node-id')).toBe('child'));
        fireEvent.keyDown(document.activeElement as HTMLElement, { key: 'End' });
        await waitFor(() => expect(document.activeElement?.getAttribute('data-vrepo-node-id')).toBe('last'));
        fireEvent.keyDown(document.activeElement as HTMLElement, { key: 'Home' });
        await waitFor(() => expect(document.activeElement?.hasAttribute('data-vrepo-tree-root')).toBe(true));
        fireEvent.keyDown(document.activeElement as HTMLElement, { key: 'ArrowDown' });
        await waitFor(() => expect(document.activeElement?.getAttribute('data-vrepo-node-id')).toBe('group'));
        fireEvent.keyDown(document.activeElement as HTMLElement, { key: 'ArrowRight' });
        await waitFor(() => expect(document.activeElement?.getAttribute('data-vrepo-node-id')).toBe('child'));
        fireEvent.keyDown(document.activeElement as HTMLElement, { key: 'ArrowLeft' });
        await waitFor(() => expect(document.activeElement?.getAttribute('data-vrepo-node-id')).toBe('group'));
        fireEvent.keyDown(document.activeElement as HTMLElement, { key: 'ArrowLeft' });
        expect(group.getAttribute('aria-expanded')).toBe('false');
    });

    it('renders mappings as indented tree children rather than peer rows', async () => {
        const backend = (window as any).go.main.App;
        const repository = {
            version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace',
            nodes: [
                { id: 'bin', name: 'bin', order: 10 },
                { id: 'commonlib', parent_id: 'bin', name: 'commonlib', order: 10, repository: { kind: 'git', relative_path: 'bin/commonlib', enabled: true } },
            ],
        };
        backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
        backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
        render(<UtilitiesPage lang="en" />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        fireEvent.click(await screen.findByText('Workspace'));

        const bin = await screen.findByRole('treeitem', { name: /^bin$/i });
        const commonlib = screen.getByRole('treeitem', { name: /commonlib.*git/i });
        expect(bin.getAttribute('aria-level')).toBe('2');
        expect(commonlib.getAttribute('aria-level')).toBe('3');
        expect(commonlib.querySelector('.vrepo-tree__elbow')).toBeTruthy();
        expect(bin.querySelector('.vrepo-tree__elbow')).toBeNull();
    });

    it('toggles a folder only from its dedicated tree disclosure control', async () => {
        const backend = (window as any).go.main.App;
        const repository = {
            version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace',
            nodes: [
                { id: 'bin', name: 'bin', order: 10 },
                { id: 'commonlib', parent_id: 'bin', name: 'commonlib', order: 10, repository: { kind: 'git', relative_path: 'bin/commonlib', enabled: true } },
            ],
        };
        backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
        backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
        render(<UtilitiesPage lang="en" />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        fireEvent.click(await screen.findByText('Workspace'));

        const bin = await screen.findByRole('treeitem', { name: /^bin$/i });
        fireEvent.doubleClick(bin);
        expect(screen.getByRole('treeitem', { name: /commonlib.*git/i })).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: 'Collapse bin' }));
        expect(screen.queryByRole('treeitem', { name: /commonlib.*git/i })).toBeNull();
        expect(bin.getAttribute('aria-selected')).toBe('false');
        expect(screen.getByRole('button', { name: 'Expand bin' }).getAttribute('tabindex')).toBe('-1');
        fireEvent.click(screen.getByRole('button', { name: 'Expand bin' }));
        expect(await screen.findByRole('treeitem', { name: /commonlib.*git/i })).toBeTruthy();
    });

    it('does not carry a collapsed tree node into another virtual repository with the same node id', async () => {
        const backend = (window as any).go.main.App;
        const first = {
            version: 1, id: 'vrepo_1', name: 'First workspace', root_path: 'D:\\first',
            nodes: [
                { id: 'group', name: 'First group', order: 10 },
                { id: 'first-child', parent_id: 'group', name: 'First child', order: 10, repository: { kind: 'local', relative_path: 'first', enabled: true } },
            ],
        };
        const second = {
            version: 1, id: 'vrepo_2', name: 'Second workspace', root_path: 'D:\\second',
            nodes: [
                { id: 'group', name: 'Second group', order: 10 },
                { id: 'second-child', parent_id: 'group', name: 'Second child', order: 10, repository: { kind: 'local', relative_path: 'second', enabled: true } },
            ],
        };
        backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([first, second]));
        backend.OpenVirtualRepository.mockImplementation(async (root: string) => JSON.stringify(root === second.root_path ? second : first));

        render(<UtilitiesPage lang="en" />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        fireEvent.click(await screen.findByText('First workspace'));
        await screen.findByRole('tree', { name: 'First workspace' });
        fireEvent.click(screen.getByRole('button', { name: 'Collapse First group' }));
        expect(screen.queryByRole('treeitem', { name: /First child.*local/i })).toBeNull();

        const secondRepository = screen.getAllByRole('button', { name: /Second workspace/ })
            .find((button) => button.classList.contains('vrepo-repository-list__item'));
        expect(secondRepository).toBeTruthy();
        fireEvent.click(secondRepository as HTMLButtonElement);
        expect(await screen.findByRole('treeitem', { name: /Second child.*local/i })).toBeTruthy();
    });

    it('keeps legacy nodes with a missing parent reachable from the tree root', async () => {
        const backend = (window as any).go.main.App;
        const repository = {
            version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace',
            nodes: [{ id: 'legacy', parent_id: 'deleted-folder', name: 'Legacy mapping', order: 10, repository: { kind: 'local', relative_path: 'build', enabled: true } }],
        };
        backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
        backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
        render(<UtilitiesPage lang="en" />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        fireEvent.click(await screen.findByText('Workspace'));
        expect(await screen.findByRole('treeitem', { name: /Legacy mapping.*local/i })).toBeTruthy();
    });

    it('keeps cyclic legacy tree nodes reachable from the root', async () => {
        const backend = (window as any).go.main.App;
        const repository = {
            version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace',
            nodes: [
                { id: 'first', parent_id: 'second', name: 'First legacy folder', order: 10 },
                { id: 'second', parent_id: 'first', name: 'Second legacy folder', order: 10 },
            ],
        };
        backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
        backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
        render(<UtilitiesPage lang="en" />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        fireEvent.click(await screen.findByText('Workspace'));
        expect(await screen.findByRole('treeitem', { name: 'First legacy folder' })).toBeTruthy();
        expect(screen.getByRole('treeitem', { name: 'Second legacy folder' })).toBeTruthy();
    });

    it('toggles folders with Enter and Space while the tree item has focus', async () => {
        const backend = (window as any).go.main.App;
        const repository = {
            version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace',
            nodes: [
                { id: 'bin', name: 'bin', order: 10 },
                { id: 'commonlib', parent_id: 'bin', name: 'commonlib', order: 10, repository: { kind: 'git', relative_path: 'bin/commonlib', enabled: true } },
            ],
        };
        backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
        backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
        render(<UtilitiesPage lang="en" />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        fireEvent.click(await screen.findByText('Workspace'));
        const bin = await screen.findByRole('treeitem', { name: /^bin$/i });
        bin.focus();
        fireEvent.keyDown(bin, { key: 'Enter' });
        expect(screen.queryByRole('treeitem', { name: /commonlib.*git/i })).toBeNull();
        fireEvent.keyDown(bin, { key: ' ' });
        expect(await screen.findByRole('treeitem', { name: /commonlib.*git/i })).toBeTruthy();
    });

    it('moves keyboard focus to a tree item selected with the mouse', async () => {
        const backend = (window as any).go.main.App;
        const repository = {
            version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace',
            nodes: [
                { id: 'first', name: 'First folder', order: 10 },
                { id: 'second', name: 'Second folder', order: 20 },
            ],
        };
        backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
        backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
        render(<UtilitiesPage lang="en" />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        fireEvent.click(await screen.findByText('Workspace'));
        const second = await screen.findByRole('treeitem', { name: 'Second folder' });
        fireEvent.click(second);
        expect(document.activeElement).toBe(second);
        expect(second.getAttribute('aria-selected')).toBe('true');
    });

    it('keeps focus and selection on a collapsing folder when its selected child is hidden', async () => {
        const backend = (window as any).go.main.App;
        const repository = {
            version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace',
            nodes: [
                { id: 'bin', name: 'bin', order: 10 },
                { id: 'commonlib', parent_id: 'bin', name: 'commonlib', order: 10, repository: { kind: 'git', relative_path: 'bin/commonlib', enabled: true } },
            ],
        };
        backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
        backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
        render(<UtilitiesPage lang="en" />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        fireEvent.click(await screen.findByText('Workspace'));
        fireEvent.click(await screen.findByRole('treeitem', { name: /commonlib.*git/i }));
        fireEvent.click(screen.getByRole('button', { name: 'Collapse bin' }));
        const bin = screen.getByRole('treeitem', { name: /^bin$/i });
        await waitFor(() => expect(document.activeElement).toBe(bin));
        expect(bin.getAttribute('aria-selected')).toBe('true');
        expect(screen.queryByRole('treeitem', { name: /commonlib.*git/i })).toBeNull();
    });

    it('does not render children beneath a malformed repository mapping node', async () => {
        const backend = (window as any).go.main.App;
        const repository = {
            version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace',
            nodes: [
                { id: 'mapping', name: 'Repository mapping', order: 10, repository: { kind: 'git', relative_path: 'source', enabled: true } },
                { id: 'orphan', parent_id: 'mapping', name: 'Recovered folder', order: 10 },
            ],
        };
        backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
        backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
        render(<UtilitiesPage lang="en" />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        fireEvent.click(await screen.findByText('Workspace'));
        expect((await screen.findByRole('treeitem', { name: 'Recovered folder' })).getAttribute('aria-level')).toBe('2');
    });

    it('does not navigate into a malformed hidden parent with ArrowLeft', async () => {
        const backend = (window as any).go.main.App;
        const repository = {
            version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace',
            nodes: [
                { id: 'mapping', name: 'Repository mapping', order: 10, repository: { kind: 'git', relative_path: 'source', enabled: true } },
                { id: 'orphan', parent_id: 'mapping', name: 'Recovered folder', order: 10 },
            ],
        };
        backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
        backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
        render(<UtilitiesPage lang="en" />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        fireEvent.click(await screen.findByText('Workspace'));
        const recovered = await screen.findByRole('treeitem', { name: 'Recovered folder' });
        recovered.focus();
        fireEvent.keyDown(recovered, { key: 'ArrowLeft' });
        expect(document.activeElement).toBe(recovered);
    });

    it('rejects an edited folder parent that would create a cycle', async () => {
        const backend = (window as any).go.main.App;
        const repository = {
            version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace',
            nodes: [
                { id: 'parent', name: 'Parent', order: 10 },
                { id: 'child', parent_id: 'parent', name: 'Child', order: 10 },
            ],
        };
        backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
        backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
        render(<UtilitiesPage lang="en" />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        fireEvent.click(await screen.findByText('Workspace'));
        fireEvent.click(await screen.findByRole('treeitem', { name: 'Parent' }));
        fireEvent.click(screen.getByText('Edit'));
        fireEvent.change(screen.getByLabelText('Parent folder'), { target: { value: 'child' } });
        fireEvent.click(screen.getByText('Save'));
        expect((await screen.findByRole('alert')).textContent).toContain('cannot be placed inside itself');
        expect(backend.SaveVirtualRepository).not.toHaveBeenCalled();
    });

    it('does not hang when moving a folder into a legacy cyclic branch', async () => {
        const backend = (window as any).go.main.App;
        const repository = {
            version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace',
            nodes: [
                { id: 'source', name: 'Source', order: 10 },
                { id: 'first', parent_id: 'second', name: 'First legacy folder', order: 20 },
                { id: 'second', parent_id: 'first', name: 'Second legacy folder', order: 30 },
            ],
        };
        backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
        backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
        render(<UtilitiesPage lang="en" />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        fireEvent.click(await screen.findByText('Workspace'));
        const source = await screen.findByRole('treeitem', { name: 'Source' });
        const first = screen.getByRole('treeitem', { name: 'First legacy folder' });
        const data = new Map<string, string>();
        const dataTransfer = { effectAllowed: 'none', dropEffect: 'none', setData: (type: string, value: string) => data.set(type, value), getData: (type: string) => data.get(type) || '' };
        fireEvent.dragStart(source, { dataTransfer });
        fireEvent.dragOver(first, { dataTransfer });
        fireEvent.drop(first, { dataTransfer });
        expect(backend.SaveVirtualRepository).not.toHaveBeenCalled();
        expect(screen.getByRole('treeitem', { name: 'Source' })).toBeTruthy();
    });

    it('moves a virtual directory under another directory by drag and drop', async () => {
		const backend = (window as any).go.main.App;
		const repository = {
			version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', updated_at: '2026-07-22T01:02:03Z',
			nodes: [
				{ id: 'source', name: 'Source', order: 10 },
				{ id: 'archive', name: 'Archive', order: 20 },
			],
		};
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		backend.SaveVirtualRepository.mockImplementation(async (raw: string) => JSON.stringify(JSON.parse(raw)));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		const source = await screen.findByRole('treeitem', { name: /Source/ });
		const archive = screen.getByRole('treeitem', { name: /Archive/ });
		const data = new Map<string, string>();
		const dataTransfer = { effectAllowed: 'none', dropEffect: 'none', setData: (type: string, value: string) => data.set(type, value), getData: (type: string) => data.get(type) || '' };
		fireEvent.dragStart(source, { dataTransfer });
		fireEvent.dragOver(archive, { dataTransfer });
		fireEvent.drop(archive, { dataTransfer });
		await waitFor(() => expect(backend.SaveVirtualRepository).toHaveBeenCalled());
		const saved = JSON.parse(backend.SaveVirtualRepository.mock.calls[0][0]);
		expect(saved.nodes.find((node: any) => node.id === 'source').parent_id).toBe('archive');
	});

    it('moves a nested folder back to the virtual tree root by drag and drop', async () => {
        const backend = (window as any).go.main.App;
        const repository = {
            version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', updated_at: '2026-07-22T01:02:03Z',
            nodes: [
                { id: 'parent', name: 'Parent', order: 10 },
                { id: 'child', parent_id: 'parent', name: 'Child', order: 10 },
            ],
        };
        backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
        backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
        backend.SaveVirtualRepository.mockImplementation(async (raw: string) => JSON.stringify(JSON.parse(raw)));
        render(<UtilitiesPage lang="en" />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        fireEvent.click(await screen.findByText('Workspace'));
        const child = await screen.findByRole('treeitem', { name: 'Child' });
        const tree = screen.getByRole('tree', { name: 'Workspace' });
        const data = new Map<string, string>();
        const dataTransfer = { effectAllowed: 'none', dropEffect: 'none', setData: (type: string, value: string) => data.set(type, value), getData: (type: string) => data.get(type) || '' };
        fireEvent.dragStart(child, { dataTransfer });
        fireEvent.dragOver(tree, { dataTransfer });
        fireEvent.drop(tree, { dataTransfer });
        await waitFor(() => expect(backend.SaveVirtualRepository).toHaveBeenCalled());
        const saved = JSON.parse(backend.SaveVirtualRepository.mock.calls[0][0]);
        expect(saved.nodes.find((node: any) => node.id === 'child').parent_id).toBeUndefined();
    });

    it('exposes the virtual repository root as the first navigable tree item', async () => {
        const backend = (window as any).go.main.App;
        const repository = {
            version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace',
            nodes: [{ id: 'folder', name: 'Folder', order: 10 }],
        };
        backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
        backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
        render(<UtilitiesPage lang="en" />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        fireEvent.click(await screen.findByText('Workspace'));

        const root = await screen.findByRole('treeitem', { name: 'Workspace' });
        expect(root.getAttribute('aria-level')).toBe('1');
        fireEvent.keyDown(root, { key: 'ArrowDown' });
        await waitFor(() => expect(document.activeElement?.getAttribute('data-vrepo-node-id')).toBe('folder'));
        fireEvent.keyDown(document.activeElement as HTMLElement, { key: 'ArrowLeft' });
        await waitFor(() => expect(document.activeElement).toBe(root));
    });

    it('creates a remote repository only after explicit host-key trust', async () => {
        const backend = (window as any).go.main.App;
        backend.TestRemoteVirtualRepositoryConnection
			.mockResolvedValueOnce(JSON.stringify({ error_code: 'host_key_untrusted', host_key_algorithm: 'ssh-ed25519', host_key_fingerprint: 'SHA256:testfingerprint' }))
			.mockResolvedValueOnce(JSON.stringify({ connected: true, host_key_trusted: true, root_exists: true }));
        backend.SaveRemoteVirtualRepository.mockImplementation(async (raw: string) => JSON.stringify({ ...JSON.parse(raw).repository, id: 'remote_1' }));
        render(<UtilitiesPage lang="en" />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        fireEvent.click(screen.getByText('New virtual repository'));
        fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Remote workspace' } });
        fireEvent.change(screen.getByLabelText('Location'), { target: { value: 'remote' } });
        fireEvent.change(screen.getByLabelText('Server'), { target: { value: 'example.com' } });
        fireEvent.change(screen.getByLabelText('SSH username'), { target: { value: 'deploy' } });
        fireEvent.change(screen.getByLabelText('SSH password'), { target: { value: 'secret' } });
        fireEvent.change(screen.getByLabelText('Remote root directory'), { target: { value: '/srv/workspace' } });
        fireEvent.click(screen.getByText('Test connection'));
        expect(await screen.findByText(/SHA256:testfingerprint/)).toBeTruthy();
        fireEvent.click(screen.getByLabelText('Trust and save host key'));
		fireEvent.click(screen.getByText('Test connection'));
		await screen.findByText(/Connected/);
        fireEvent.click(screen.getByText('Save'));
        await waitFor(() => expect(backend.SaveRemoteVirtualRepository).toHaveBeenCalled());
        const input = JSON.parse(backend.SaveRemoteVirtualRepository.mock.calls[0][0]);
        expect(input.repository.remote).toEqual({ host: 'example.com', port: 22, user: 'deploy' });
        expect(input.repository.root_path).toBe('/srv/workspace');
        expect(input.trust_host_key).toBe(true);
    });

    it('invalidates a successful remote connection when connection fields change', async () => {
        const backend = (window as any).go.main.App;
        backend.TestRemoteVirtualRepositoryConnection.mockResolvedValue(JSON.stringify({ connected: true, host_key_trusted: true, root_exists: true }));
        render(<UtilitiesPage lang="en" />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        fireEvent.click(screen.getByText('New virtual repository'));
        fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Remote workspace' } });
        fireEvent.change(screen.getByLabelText('Location'), { target: { value: 'remote' } });
        fireEvent.change(screen.getByLabelText('Server'), { target: { value: 'example.com' } });
        fireEvent.change(screen.getByLabelText('SSH username'), { target: { value: 'deploy' } });
        fireEvent.change(screen.getByLabelText('SSH password'), { target: { value: 'secret' } });
        fireEvent.change(screen.getByLabelText('Remote root directory'), { target: { value: '/srv/workspace' } });
        fireEvent.click(screen.getByText('Test connection'));
        expect(await screen.findByText(/Connected/)).toBeTruthy();
        expect((screen.getByText('Save') as HTMLButtonElement).disabled).toBe(false);
        fireEvent.change(screen.getByLabelText('Server'), { target: { value: 'other.example.com' } });
        expect(screen.queryByText(/Connected/)).toBeNull();
        expect((screen.getByText('Save') as HTMLButtonElement).disabled).toBe(true);
    });

	it('offers to create a missing remote root only after confirmation', async () => {
		const backend = (window as any).go.main.App;
		backend.TestRemoteVirtualRepositoryConnection
			.mockResolvedValueOnce(JSON.stringify({ connected: true, host_key_trusted: true, root_exists: false, error_code: 'root_not_found', error: 'SSH connected, but remote root directory does not exist' }))
			.mockResolvedValueOnce(JSON.stringify({ connected: true, host_key_trusted: true, root_exists: true, git_version: 'git version 2.45' }));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(screen.getByText('New virtual repository'));
		fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Remote workspace' } });
		fireEvent.change(screen.getByLabelText('Location'), { target: { value: 'remote' } });
		fireEvent.change(screen.getByLabelText('Server'), { target: { value: 'example.com' } });
		fireEvent.change(screen.getByLabelText('SSH username'), { target: { value: 'deploy' } });
		fireEvent.change(screen.getByLabelText('SSH password'), { target: { value: 'secret' } });
		fireEvent.change(screen.getByLabelText('Remote root directory'), { target: { value: '/srv/new-workspace' } });
		fireEvent.click(screen.getByText('Test connection'));
		expect(await screen.findByText('Create remote root')).toBeTruthy();
		expect((screen.getByText('Save') as HTMLButtonElement).disabled).toBe(true);
		fireEvent.click(screen.getByText('Create remote root'));
		const confirmation = await screen.findByRole('dialog', { name: 'Create remote root' });
		fireEvent.click(within(confirmation).getByRole('button', { name: 'Create remote root' }));
		await waitFor(() => expect(backend.CreateRemoteVirtualRepositoryRoot).toHaveBeenCalledTimes(1));
		await waitFor(() => expect(backend.TestRemoteVirtualRepositoryConnection).toHaveBeenCalledTimes(2));
		expect(screen.queryByRole('dialog', { name: 'Create remote root' })).toBeNull();
		expect(await screen.findByText(/Connected/)).toBeTruthy();
		expect((screen.getByText('Save') as HTMLButtonElement).disabled).toBe(false);
	});

	it('coalesces rapid remote connection tests', async () => {
		const backend = (window as any).go.main.App;
		let resolveTest!: (value: string) => void;
		backend.TestRemoteVirtualRepositoryConnection.mockReturnValue(new Promise((resolve) => { resolveTest = resolve; }));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(screen.getByText('New virtual repository'));
		fireEvent.change(screen.getByLabelText('Location'), { target: { value: 'remote' } });
		fireEvent.change(screen.getByLabelText('Server'), { target: { value: 'example.com' } });
		fireEvent.change(screen.getByLabelText('SSH username'), { target: { value: 'deploy' } });
		fireEvent.change(screen.getByLabelText('SSH password'), { target: { value: 'secret' } });
		fireEvent.change(screen.getByLabelText('Remote root directory'), { target: { value: '/srv/workspace' } });
		const testButton = screen.getByText('Test connection');
		fireEvent.click(testButton);
		fireEvent.click(testButton);
		expect(backend.TestRemoteVirtualRepositoryConnection).toHaveBeenCalledTimes(1);
		resolveTest(JSON.stringify({ connected: true, host_key_trusted: true, root_exists: true }));
		expect(await screen.findByText(/Connected/)).toBeTruthy();
	});

    it('preserves nodes and revision while editing a remote connection', async () => {
        const backend = (window as any).go.main.App;
        const existing = { version: 1, id: 'remote_1', name: 'Remote workspace', root_path: '/srv/workspace', remote: { host: 'example.com', port: 22, user: 'deploy' }, nodes: [{ id: 'build', name: 'Build', repository: { kind: 'local', relative_path: 'build', enabled: true } }], updated_at: '2026-07-22T00:00:00Z' };
        backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([existing]));
        backend.OpenRemoteVirtualRepository.mockResolvedValue(JSON.stringify(existing));
        backend.TestRemoteVirtualRepositoryConnection.mockResolvedValue(JSON.stringify({ connected: true, host_key_trusted: true, root_exists: true }));
        backend.SaveRemoteVirtualRepository.mockImplementation(async (raw: string) => JSON.stringify(JSON.parse(raw).repository));
        render(<UtilitiesPage lang="en" />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        fireEvent.click(await screen.findByText('Remote workspace'));
        await screen.findByText('Edit connection');
        fireEvent.click(screen.getByText('Edit connection'));
        fireEvent.click(screen.getByText('Test connection'));
        await screen.findByText(/Connected/);
        fireEvent.click(screen.getByText('Save'));
        await waitFor(() => expect(backend.SaveRemoteVirtualRepository).toHaveBeenCalled());
        const input = JSON.parse(backend.SaveRemoteVirtualRepository.mock.calls[0][0]);
        expect(input.repository.id).toBe('remote_1');
        expect(input.repository.updated_at).toBe('2026-07-22T00:00:00Z');
        expect(input.repository.nodes).toHaveLength(1);
    });

	it('derives a remote mapping path from its virtual directory', async () => {
		const backend = (window as any).go.main.App;
		const existing = { version: 1, id: 'remote_1', name: 'Remote workspace', root_path: '/srv/workspace', remote: { host: 'example.com', port: 22, user: 'deploy' }, nodes: [], updated_at: '2026-07-22T00:00:00Z' };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([existing]));
		backend.OpenRemoteVirtualRepository.mockResolvedValue(JSON.stringify(existing));
		backend.SaveRemoteVirtualRepository.mockImplementation(async (raw: string) => JSON.stringify(JSON.parse(raw).repository));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Remote workspace'));
		fireEvent.click(await screen.findByLabelText('Add mapping'));
		fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'CLI' } });
		fireEvent.change(screen.getByLabelText('Repository URL'), { target: { value: 'https://example.com/cli.git' } });
		fireEvent.click(screen.getByText('Save'));
		await waitFor(() => expect(backend.SaveRemoteVirtualRepository).toHaveBeenCalledTimes(1));
		const input = JSON.parse(backend.SaveRemoteVirtualRepository.mock.calls[0][0]);
		expect(input.repository.nodes[0].repository.relative_path).toBe('CLI');
	});

	it('validates and saves a mapping before creating its directory', async () => {
		const backend = (window as any).go.main.App;
		const existing = { version: 1, id: 'remote_1', name: 'Remote workspace', root_path: '/srv/workspace', remote: { host: 'example.com', port: 22, user: 'deploy' }, nodes: [], updated_at: '2026-07-22T00:00:00Z' };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([existing]));
		backend.OpenRemoteVirtualRepository.mockResolvedValue(JSON.stringify(existing));
		backend.SaveRemoteVirtualRepository.mockImplementation(async (raw: string) => JSON.stringify(JSON.parse(raw).repository));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Remote workspace'));
		fireEvent.click(await screen.findByLabelText('Add mapping'));
		fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Build' } });
		fireEvent.change(screen.getByLabelText('Type'), { target: { value: 'local' } });
		expect(screen.getByText('The local mapping directory is created automatically when saved')).toBeTruthy();
		fireEvent.click(screen.getByText('Save'));
		await waitFor(() => expect(backend.CreateRemoteVirtualRepositoryDirectory).toHaveBeenCalledWith('remote_1', 'Build'));
		expect(backend.SaveRemoteVirtualRepository.mock.invocationCallOrder[0]).toBeLessThan(backend.CreateRemoteVirtualRepositoryDirectory.mock.invocationCallOrder[0]);
	});

    it('starts a coding task from repository actions and hands it to a new tab', async () => {
        const backend = (window as any).go.main.App;
        const repository = { version: 1, id: 'local_1', name: 'Product workspace', root_path: 'D:\\workspace', nodes: [] };
        backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
        backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
        backend.StartVirtualRepositoryCodingTask.mockResolvedValue({ project_path: 'D:\\tasks\\local_1', task_title: 'Product workspace', agent_mode: 'coding_dev' });
        const onOpen = vi.fn();
        render(<UtilitiesPage lang="en" onOpenVirtualRepositoryTask={onOpen} />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        fireEvent.click(await screen.findByText('Product workspace'));
        const actions = await screen.findByRole('group', { name: 'Repository actions' });
        fireEvent.click(within(actions).getByRole('button', { name: 'Start coding task' }));
        await waitFor(() => expect(backend.StartVirtualRepositoryCodingTask).toHaveBeenCalledWith('local_1'));
        expect(onOpen).toHaveBeenCalledWith(expect.objectContaining({ project_path: 'D:\\tasks\\local_1', agent_mode: 'coding_dev' }));
    });

    it('puts the active repository coding task first in repository actions with a code icon', async () => {
        const backend = (window as any).go.main.App;
        const repository = { version: 1, id: 'local_1', name: 'Product workspace', root_path: 'D:\\workspace', nodes: [] };
        backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
        backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		let resolveLaunch!: (launch: unknown) => void;
		backend.StartVirtualRepositoryCodingTask.mockReturnValue(new Promise((resolve) => { resolveLaunch = resolve; }));
        render(<UtilitiesPage lang="en" />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        fireEvent.click(await screen.findByText('Product workspace'));
        const actions = await screen.findByRole('group', { name: 'Repository actions' });
        const buttons = within(actions).getAllByRole('button');
		expect(buttons[0].textContent).toContain('Start coding task');
		expect(buttons[0].querySelector('svg')).not.toBeNull();
		expect(screen.queryAllByRole('button', { name: 'Start coding task' })).toHaveLength(1);
		fireEvent.click(buttons[0]);
		await waitFor(() => expect(backend.StartVirtualRepositoryCodingTask).toHaveBeenCalledWith('local_1'));
		expect(buttons[0].getAttribute('aria-busy')).toBe('true');
		expect(buttons[0].querySelector('.vrepo-button-spinner')).not.toBeNull();
		resolveLaunch({ project_path: 'D:\\tasks\\local_1', task_title: 'Product workspace', agent_mode: 'coding_dev' });
		await waitFor(() => expect(buttons[0].getAttribute('aria-busy')).toBe('false'));
    });

    it('coalesces rapid recent-repository opening and shows an opening state', async () => {
        const backend = (window as any).go.main.App;
        const recent = { id: 'local_1', name: 'Product workspace', root_path: 'D:\\workspace', nodes: [] };
        backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([recent]));
        let resolveOpen!: (value: string) => void;
        backend.OpenVirtualRepository.mockReturnValue(new Promise((resolve) => { resolveOpen = resolve; }));
        render(<UtilitiesPage lang="en" />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        const open = await screen.findByRole('button', { name: /Product workspace/ });
        fireEvent.click(open);
        fireEvent.click(open);
        expect(backend.OpenVirtualRepository).toHaveBeenCalledTimes(1);
        expect(await screen.findByText('Loading…')).toBeTruthy();
        resolveOpen(JSON.stringify(recent));
        await waitFor(() => expect(screen.getByRole('tree', { name: 'Product workspace' })).toBeTruthy());
    });

    it('coalesces rapid coding-task clicks into one backend launch', async () => {
        const backend = (window as any).go.main.App;
        const repository = { version: 1, id: 'local_1', name: 'Product workspace', root_path: 'D:\\workspace', nodes: [] };
        backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
        backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
        let resolveLaunch!: (launch: unknown) => void;
        backend.StartVirtualRepositoryCodingTask.mockReturnValue(new Promise((resolve) => { resolveLaunch = resolve; }));
        render(<UtilitiesPage lang="en" />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Product workspace'));
		const actions = await screen.findByRole('group', { name: 'Repository actions' });
		const launch = within(actions).getByRole('button', { name: 'Start coding task' });
		fireEvent.click(launch);
		fireEvent.click(launch);
		expect(backend.StartVirtualRepositoryCodingTask).toHaveBeenCalledTimes(1);
		resolveLaunch({ project_path: 'D:\\tasks\\local_1', task_title: 'Product workspace', agent_mode: 'coding_dev' });
		await waitFor(() => expect(launch).toHaveProperty('disabled', false));
    });

	it('coalesces rapid operation execution clicks into one backend job', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', updated_at: '2026-07-22T01:02:03Z', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		backend.PreviewVirtualRepositoryOperation.mockResolvedValue(JSON.stringify({ repository_id: repository.id, updated_at: repository.updated_at, targets: [], skipped_local: 0 }));
		let resolveStart!: (value: string) => void;
		backend.StartVirtualRepositoryOperation.mockReturnValue(new Promise((resolve) => { resolveStart = resolve; }));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		fireEvent.click(await screen.findByText('Push'));
		const execute = await screen.findByText('Execute');
		fireEvent.click(execute);
		fireEvent.click(execute);
		expect(backend.StartVirtualRepositoryOperation).toHaveBeenCalledTimes(1);
		resolveStart(JSON.stringify({ job_id: 'job_1', status: 'running', items: [] }));
	});

	it('rejects a malformed operation start response', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', updated_at: '2026-07-22T01:02:03Z', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		backend.PreviewVirtualRepositoryOperation.mockResolvedValue(JSON.stringify({ repository_id: repository.id, updated_at: repository.updated_at, targets: [], skipped_local: 0 }));
		backend.StartVirtualRepositoryOperation.mockResolvedValue(JSON.stringify({ job_id: 'job_1', status: 'mystery', items: [] }));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		fireEvent.click(await screen.findByText('Push'));
		fireEvent.click(await screen.findByText('Execute'));
		expect(await screen.findByText(/invalid status/i)).toBeTruthy();
	});

	it('rejects malformed operation item responses', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', updated_at: '2026-07-22T01:02:03Z', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		backend.PreviewVirtualRepositoryOperation.mockResolvedValue(JSON.stringify({ repository_id: repository.id, updated_at: repository.updated_at, targets: [], skipped_local: 0 }));
		backend.StartVirtualRepositoryOperation.mockResolvedValue(JSON.stringify({ job_id: 'job_1', status: 'running', items: [{ status: 'mystery' }] }));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		fireEvent.click(await screen.findByText('Push'));
		fireEvent.click(await screen.findByText('Execute'));
		expect(await screen.findByText(/invalid operation items/i)).toBeTruthy();
	});

	it('does not regress a terminal operation when a stale running event arrives', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', updated_at: '2026-07-22T01:02:03Z', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		backend.PreviewVirtualRepositoryOperation.mockResolvedValue(JSON.stringify({ repository_id: repository.id, updated_at: repository.updated_at, targets: [], skipped_local: 0 }));
		backend.StartVirtualRepositoryOperation.mockResolvedValue(JSON.stringify({ job_id: 'job_1', status: 'running', items: [] }));
		backend.GetVirtualRepositoryOperation.mockResolvedValue(JSON.stringify({ job_id: 'job_1', status: 'running', items: [] }));
		backend.GetVirtualRepositoryOperation.mockResolvedValue(JSON.stringify({ job_id: 'job_1', status: 'running', items: [] }));
		backend.GetVirtualRepositoryOperation.mockResolvedValue(JSON.stringify({ job_id: 'job_1', status: 'running', items: [] }));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		fireEvent.click(await screen.findByText('Push'));
		fireEvent.click(await screen.findByText('Execute'));
		await screen.findByText(/Operation: running/i);
		await screen.findByText(/Operation: running/i);
		const handler = vi.mocked(EventsOn).mock.calls.find(([event]) => event === 'virtual-repository:job-updated')?.[1] as ((raw: string) => void);
		expect(handler).toBeTypeOf('function');
		act(() => handler(JSON.stringify({ job_id: 'job_1', status: 'success', items: [{ node_id: 'node_1', name: 'Source', status: 'success' }] })));
		expect(await screen.findByText(/Operation: success/i)).toBeTruthy();
		act(() => handler(JSON.stringify({ job_id: 'job_1', status: 'running', items: [] })));
		expect(screen.getByText(/Operation: success/i)).toBeTruthy();
		expect(screen.queryByText('Cancel')).toBeNull();
	});

	it('does not replace one terminal result with a conflicting stale terminal event', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', updated_at: '2026-07-22T01:02:03Z', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		backend.PreviewVirtualRepositoryOperation.mockResolvedValue(JSON.stringify({ repository_id: repository.id, updated_at: repository.updated_at, targets: [], skipped_local: 0 }));
		backend.StartVirtualRepositoryOperation.mockResolvedValue(JSON.stringify({ job_id: 'job_1', status: 'running', items: [] }));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		fireEvent.click(await screen.findByText('Push'));
		fireEvent.click(await screen.findByText('Execute'));
		await screen.findByText(/Operation: running/i);
		const handler = vi.mocked(EventsOn).mock.calls.find(([event]) => event === 'virtual-repository:job-updated')?.[1] as ((raw: string) => void);
		act(() => handler(JSON.stringify({ job_id: 'job_1', status: 'success', items: [] })));
		expect(await screen.findByText(/Operation: success/i)).toBeTruthy();
		act(() => handler(JSON.stringify({ job_id: 'job_1', status: 'failed', items: [{ node_id: 'node_1', status: 'failed', error: 'stale' }] })));
		expect(screen.getByText(/Operation: success/i)).toBeTruthy();
		expect(screen.queryByText(/stale/)).toBeNull();
	});

	it('retries only push after commit succeeded but push failed', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', updated_at: '2026-07-22T01:02:03Z', nodes: [{ id: 'node_1', name: 'Source', order: 1, repository: { kind: 'git', relative_path: 'src', remote_url: 'https://example.com/src.git', enabled: true } }] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		backend.PreviewVirtualRepositoryOperation.mockResolvedValue(JSON.stringify({ repository_id: repository.id, updated_at: repository.updated_at, targets: [{ node_id: 'node_1', name: 'Source', kind: 'git' }], skipped_local: 0 }));
		backend.StartVirtualRepositoryOperation.mockResolvedValue(JSON.stringify({ job_id: 'job_1', action: 'commit_push', message: 'change', status: 'failed', items: [{ node_id: 'node_1', name: 'Source', status: 'failed', error_code: 'push_failed_after_commit' }] }));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		fireEvent.click(await screen.findByText('Commit & push'));
		fireEvent.change(screen.getByLabelText('Commit message'), { target: { value: 'change' } });
		fireEvent.click(screen.getByText('Preview'));
		fireEvent.click(await screen.findByText('Execute'));
		fireEvent.click(await screen.findByText('Retry failed'));
		expect(await screen.findByRole('heading', { name: 'push' })).toBeTruthy();
		fireEvent.click(screen.getByText('Preview'));
		expect(await screen.findByText(/1 repositories/)).toBeTruthy();
	});

	it('coalesces cancellation clicks while cancellation is pending', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', updated_at: '2026-07-22T01:02:03Z', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		backend.PreviewVirtualRepositoryOperation.mockResolvedValue(JSON.stringify({ repository_id: repository.id, updated_at: repository.updated_at, targets: [], skipped_local: 0 }));
		backend.StartVirtualRepositoryOperation.mockResolvedValue(JSON.stringify({ job_id: 'job_1', status: 'running', items: [] }));
		backend.GetVirtualRepositoryOperation.mockResolvedValue(JSON.stringify({ job_id: 'job_1', status: 'running', items: [] }));
		let resolveCancel!: () => void;
		backend.CancelVirtualRepositoryOperation.mockReturnValue(new Promise<void>((resolve) => { resolveCancel = resolve; }));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		fireEvent.click(await screen.findByText('Push'));
		fireEvent.click(await screen.findByText('Execute'));
		const result = await screen.findByRole('region');
		const cancel = within(result).getByText('Cancel');
		fireEvent.click(cancel);
		fireEvent.click(cancel);
		expect(backend.CancelVirtualRepositoryOperation).toHaveBeenCalledTimes(1);
		resolveCancel();
	});

	it('keeps a saved mapping when the local credential binding fails', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		backend.SaveVirtualRepository.mockImplementation(async (raw: string) => JSON.stringify(JSON.parse(raw)));
		backend.ListRepositoryCredentials.mockResolvedValue(JSON.stringify([{ id: 'cred_1', name: 'GitHub', kind: 'git', username: 'alice' }]));
		backend.SetRepositoryCredentialBinding.mockRejectedValue(new Error('keyring unavailable'));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		fireEvent.click(await screen.findByLabelText('Add mapping'));
		fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Source' } });
		fireEvent.change(screen.getByLabelText('Repository URL'), { target: { value: 'https://example.com/src.git' } });
		const credential = screen.getByLabelText('Repository credentials');
		fireEvent.focus(credential);
		await waitFor(() => expect(screen.getByRole('option', { name: /GitHub/ })).toBeTruthy());
		fireEvent.change(credential, { target: { value: 'cred_1' } });
		fireEvent.click(screen.getByText('Save'));
		expect((await screen.findByRole('alert')).textContent).toMatch(/mapping was saved.*credential binding/i);
		expect(screen.getByRole('treeitem', { name: /Source/ })).toBeTruthy();
		expect(backend.SaveVirtualRepository).toHaveBeenCalledTimes(1);
	});

	it('preserves the mapping credential type when adding a credential from the mapping editor', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		fireEvent.click(await screen.findByLabelText('Add mapping'));
		fireEvent.change(screen.getByLabelText('Type'), { target: { value: 'svn' } });
		fireEvent.click(screen.getByText('Add credential'));
		await waitFor(() => expect(screen.getByRole('heading', { name: 'Add credential' })).toBeTruthy());
		expect((screen.getByLabelText('Type') as HTMLSelectElement).value).toBe('svn');
		fireEvent.change(screen.getByLabelText('Credential name'), { target: { value: 'SVN account' } });
		fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'alice' } });
		fireEvent.change(screen.getByLabelText('Password or token'), { target: { value: 'secret' } });
		fireEvent.click(screen.getByText('Save'));
		await waitFor(() => expect(backend.SaveRepositoryCredential).toHaveBeenCalledTimes(1));
		expect(JSON.parse(backend.SaveRepositoryCredential.mock.calls[0][0])).toMatchObject({ kind: 'svn', name: 'SVN account', username: 'alice' });
		await waitFor(() => expect((screen.getByLabelText('Type') as HTMLSelectElement).value).toBe('svn'));
	});

	it('keeps a successful credential save usable when the credential list cannot refresh', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		backend.ListRepositoryCredentials
			.mockResolvedValueOnce('[]')
			.mockRejectedValue(new Error('credential list unavailable'));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		fireEvent.click(await screen.findByLabelText('Add mapping'));
		fireEvent.click(screen.getByText('Add credential'));
		fireEvent.change(screen.getByLabelText('Credential name'), { target: { value: 'GitHub' } });
		fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'alice' } });
		fireEvent.change(screen.getByLabelText('Password or token'), { target: { value: 'secret' } });
		fireEvent.click(screen.getByText('Save'));
		await waitFor(() => expect(backend.SaveRepositoryCredential).toHaveBeenCalledTimes(1));
		expect((screen.getByLabelText('Repository credentials') as HTMLSelectElement).value).toBe('cred_1');
		expect(screen.queryByRole('alert')).toBeNull();
	});

	it('keeps a credential kind after deleting the credential being edited', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		backend.ListRepositoryCredentials.mockResolvedValue(JSON.stringify([{ id: 'cred_1', name: 'SVN account', kind: 'svn', username: 'alice' }]));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		fireEvent.click(await screen.findByText('Repository credentials'));
		fireEvent.click(await screen.findByText('Edit'));
		expect((screen.getByLabelText('Type') as HTMLSelectElement).value).toBe('svn');
		fireEvent.click(screen.getByText('Remove'));
		fireEvent.click(within(await screen.findByRole('dialog', { name: 'Remove' })).getByRole('button', { name: 'Remove' }));
		await waitFor(() => expect(backend.DeleteRepositoryCredential).toHaveBeenCalledWith('cred_1'));
		fireEvent.change(screen.getByLabelText('Credential name'), { target: { value: 'Replacement SVN account' } });
		fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'bob' } });
		fireEvent.change(screen.getByLabelText('Password or token'), { target: { value: 'secret' } });
		fireEvent.click(screen.getByText('Save'));
		await waitFor(() => expect(backend.SaveRepositoryCredential).toHaveBeenCalledTimes(1));
		expect(JSON.parse(backend.SaveRepositoryCredential.mock.calls[0][0])).toMatchObject({ kind: 'svn', name: 'Replacement SVN account', username: 'bob' });
	});

	it('prevents a second credential deletion while the first is pending', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		backend.ListRepositoryCredentials.mockResolvedValue(JSON.stringify([{ id: 'cred_1', name: 'GitHub', kind: 'git', username: 'alice' }]));
		let resolveDelete!: () => void;
		backend.DeleteRepositoryCredential.mockReturnValue(new Promise<void>((resolve) => { resolveDelete = resolve; }));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		fireEvent.click(await screen.findByText('Repository credentials'));
		fireEvent.click(await screen.findByText('Remove'));
		const confirmation = await screen.findByRole('dialog', { name: 'Remove' });
		const confirm = within(confirmation).getByRole('button', { name: 'Remove' });
		fireEvent.click(confirm);
		await waitFor(() => expect(backend.DeleteRepositoryCredential).toHaveBeenCalledTimes(1));
		expect((screen.getByText('Remove') as HTMLButtonElement).disabled).toBe(true);
		resolveDelete();
		await waitFor(() => expect((screen.getByText('Save') as HTMLButtonElement).disabled).toBe(false));
	});

	it('keeps a successful deletion successful when the credential list cannot refresh', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		backend.ListRepositoryCredentials.mockResolvedValueOnce(JSON.stringify([{ id: 'cred_1', name: 'GitHub', kind: 'git', username: 'alice' }])).mockRejectedValue(new Error('credential list unavailable'));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		fireEvent.click(await screen.findByText('Repository credentials'));
		fireEvent.click(await screen.findByText('Remove'));
		fireEvent.click(within(await screen.findByRole('dialog', { name: 'Remove' })).getByRole('button', { name: 'Remove' }));
		await waitFor(() => expect(backend.DeleteRepositoryCredential).toHaveBeenCalledWith('cred_1'));
		expect(screen.queryByText('GitHub')).toBeNull();
		expect(screen.queryByRole('alert')).toBeNull();
	});

	it('coalesces rapid credential saves into one request', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		let resolveSave!: (value: string) => void;
		backend.SaveRepositoryCredential.mockReturnValue(new Promise((resolve) => { resolveSave = resolve; }));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		fireEvent.click(await screen.findByText('Repository credentials'));
		fireEvent.change(screen.getByLabelText('Credential name'), { target: { value: 'GitHub' } });
		fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'alice' } });
		fireEvent.change(screen.getByLabelText('Password or token'), { target: { value: 'secret' } });
		const save = screen.getByText('Save');
		fireEvent.click(save);
		fireEvent.click(save);
		expect(backend.SaveRepositoryCredential).toHaveBeenCalledTimes(1);
		resolveSave(JSON.stringify({ id: 'cred_1', name: 'GitHub', kind: 'git', username: 'alice' }));
		await waitFor(() => expect((screen.getByText('Save') as HTMLButtonElement).disabled).toBe(false));
	});

	it('does not let a stale credential-list response erase a saved credential', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		let resolveCredentials!: (value: string) => void;
		backend.ListRepositoryCredentials.mockReturnValueOnce(new Promise((resolve) => { resolveCredentials = resolve; }));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		fireEvent.click(await screen.findByText('Repository credentials'));
		fireEvent.change(screen.getByLabelText('Credential name'), { target: { value: 'GitHub' } });
		fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'alice' } });
		fireEvent.change(screen.getByLabelText('Password or token'), { target: { value: 'secret' } });
		fireEvent.click(screen.getByText('Save'));
		await waitFor(() => expect(backend.SaveRepositoryCredential).toHaveBeenCalledTimes(1));
		await act(async () => resolveCredentials('[]'));
		expect(await screen.findByText('GitHub')).toBeTruthy();
	});

	it('keeps a manager-created credential when an older mapping list resolves late', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		let resolveMappingCredentials!: (value: string) => void;
		backend.ListRepositoryCredentials
			.mockReturnValueOnce(new Promise((resolve) => { resolveMappingCredentials = resolve; }))
			.mockResolvedValueOnce('[]');
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		fireEvent.click(await screen.findByLabelText('Add mapping'));
		const mappingCredentials = screen.getByLabelText('Repository credentials');
		fireEvent.focus(mappingCredentials);
		fireEvent.click(screen.getByText('Add credential'));
		fireEvent.change(screen.getByLabelText('Credential name'), { target: { value: 'GitHub' } });
		fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'alice' } });
		fireEvent.change(screen.getByLabelText('Password or token'), { target: { value: 'secret' } });
		fireEvent.click(screen.getByText('Save'));
		await waitFor(() => expect(backend.SaveRepositoryCredential).toHaveBeenCalledTimes(1));
		await act(async () => resolveMappingCredentials('[]'));
		expect(await screen.findByRole('option', { name: /GitHub/ })).toBeTruthy();
	});

	it('loads SVN credentials after the mapping type changes from Git', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		backend.ListRepositoryCredentials
			.mockResolvedValueOnce(JSON.stringify([{ id: 'git_1', name: 'GitHub', kind: 'git', username: 'alice' }]))
			.mockResolvedValueOnce(JSON.stringify([{ id: 'svn_1', name: 'Subversion', kind: 'svn', username: 'bob' }]));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		fireEvent.click(await screen.findByLabelText('Add mapping'));
		const mappingCredentials = screen.getByLabelText('Repository credentials');
		fireEvent.focus(mappingCredentials);
		await waitFor(() => expect(screen.getByRole('option', { name: /GitHub/ })).toBeTruthy());
		fireEvent.change(screen.getByLabelText('Type'), { target: { value: 'svn' } });
		fireEvent.focus(mappingCredentials);
		await waitFor(() => expect(screen.getByRole('option', { name: /Subversion/ })).toBeTruthy());
		expect(backend.ListRepositoryCredentials).toHaveBeenLastCalledWith('svn');
	});

	it('reports malformed credential-list data without treating it as an empty list', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		backend.ListRepositoryCredentials.mockResolvedValue(JSON.stringify([{ id: 'cred_1', name: 'Broken', kind: 'hg', username: 'alice' }]));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		fireEvent.click(await screen.findByText('Repository credentials'));
		expect((await screen.findByRole('alert')).textContent).toMatch(/invalid credentials/i);
	});

	it('does not retain a previous credential list when a later manager refresh fails', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		backend.ListRepositoryCredentials
			.mockResolvedValueOnce(JSON.stringify([{ id: 'cred_1', name: 'GitHub', kind: 'git', username: 'alice' }]))
			.mockRejectedValueOnce(new Error('credential state unavailable'));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		fireEvent.click(await screen.findByText('Repository credentials'));
		expect(await screen.findByText('GitHub')).toBeTruthy();
		fireEvent.click(screen.getByText('Close'));
		fireEvent.click(screen.getByText('Repository credentials'));
		expect((await screen.findByRole('alert')).textContent).toMatch(/credential state unavailable/i);
		expect(screen.queryByText('GitHub')).toBeNull();
	});

	it('rejects duplicate credential ids returned by the backend', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		backend.ListRepositoryCredentials.mockResolvedValue(JSON.stringify([
			{ id: 'cred_1', name: 'GitHub', kind: 'git', username: 'alice' },
			{ id: 'cred_1', name: 'GitLab', kind: 'git', username: 'bob' },
		]));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		fireEvent.click(await screen.findByText('Repository credentials'));
		expect((await screen.findByRole('alert')).textContent).toMatch(/duplicate credential ids/i);
	});

	it('rejects malformed credential metadata returned after save', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		backend.SaveRepositoryCredential.mockResolvedValue(JSON.stringify({ id: 'cred_1', name: 'GitHub', kind: 'mercurial', username: 'alice' }));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		fireEvent.click(await screen.findByText('Repository credentials'));
		fireEvent.change(screen.getByLabelText('Credential name'), { target: { value: 'GitHub' } });
		fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'alice' } });
		fireEvent.change(screen.getByLabelText('Password or token'), { target: { value: 'secret' } });
		fireEvent.click(screen.getByText('Save'));
		expect((await screen.findByRole('alert')).textContent).toMatch(/invalid metadata/i);
	});

	it('does not apply stale credential bindings after switching repositories', async () => {
		const backend = (window as any).go.main.App;
		const first = { version: 1, id: 'vrepo_1', name: 'First workspace', root_path: 'D:\\first', nodes: [{ id: 'node_1', name: 'Source', order: 1, repository: { kind: 'git', relative_path: 'source', remote_url: 'https://example.com/first.git', enabled: true } }] };
		const second = { version: 1, id: 'vrepo_2', name: 'Second workspace', root_path: 'D:\\second', nodes: [{ id: 'node_1', name: 'Source', order: 1, repository: { kind: 'git', relative_path: 'source', remote_url: 'https://example.com/second.git', enabled: true } }] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([first, second]));
		backend.OpenVirtualRepository.mockImplementation(async (root: string) => JSON.stringify(root === first.root_path ? first : second));
		let resolveFirstBindings!: (value: string) => void;
		backend.ListRepositoryCredentialBindings
			.mockReturnValueOnce(new Promise((resolve) => { resolveFirstBindings = resolve; }))
			.mockResolvedValueOnce('{}');
		backend.ListRepositoryCredentials.mockResolvedValue(JSON.stringify([{ id: 'cred_1', name: 'GitHub', kind: 'git', username: 'alice' }]));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('First workspace'));
		fireEvent.click(await screen.findByText('Second workspace'));
		await act(async () => resolveFirstBindings(JSON.stringify({ node_1: 'cred_1' })));
		fireEvent.click(await screen.findByRole('treeitem', { name: /Source/ }));
		fireEvent.click(screen.getByText('Edit'));
		const credentialSelect = screen.getByLabelText('Repository credentials') as HTMLSelectElement;
		fireEvent.focus(credentialSelect);
		await waitFor(() => expect(screen.getByRole('option', { name: /GitHub/ })).toBeTruthy());
		expect(credentialSelect.value).toBe('');
	});

	it('does not apply a delayed credential-list response after leaving the manager', async () => {
		const backend = (window as any).go.main.App;
		const repository = { version: 1, id: 'vrepo_1', name: 'Workspace', root_path: 'D:\\workspace', nodes: [] };
		backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([repository]));
		backend.OpenVirtualRepository.mockResolvedValue(JSON.stringify(repository));
		let resolveCredentials!: (value: string) => void;
		backend.ListRepositoryCredentials.mockReturnValueOnce(new Promise((resolve) => { resolveCredentials = resolve; }));
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(await screen.findByText('Workspace'));
		fireEvent.click(await screen.findByText('Repository credentials'));
		fireEvent.click(screen.getByText('Close'));
		resolveCredentials(JSON.stringify([{ id: 'cred_1', name: 'Delayed account', kind: 'git', username: 'alice' }]));
		await act(async () => undefined);
		expect(screen.queryByText('Credential manager')).toBeNull();
		expect(screen.queryByText('Delayed account')).toBeNull();
	});

	it('does not report a save as successful when the backend response is malformed', async () => {
		const backend = (window as any).go.main.App;
		backend.SaveVirtualRepository.mockResolvedValue('not-json');
		render(<UtilitiesPage lang="en" />);
		fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
		fireEvent.click(screen.getByText('New virtual repository'));
		fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Workspace' } });
		fireEvent.click(screen.getByText('Choose'));
		await waitFor(() => expect(backend.SelectVirtualRepositoryRoot).toHaveBeenCalled());
		fireEvent.click(screen.getByText('Save'));
		expect((await screen.findByRole('alert')).textContent).toMatch(/malformed JSON/i);
		expect(screen.getByRole('heading', { name: 'New virtual repository' })).toBeTruthy();
	});
});

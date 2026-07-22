// @vitest-environment jsdom
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { EventsOn } from '../../../../wailsjs/runtime';
import { UtilitiesPage } from '../UtilitiesPage';

vi.mock('../../../../wailsjs/runtime', () => ({ EventsOn: vi.fn(() => vi.fn()), BrowserOpenURL: vi.fn() }));

function installBackend() {
    const backend = {
        GetACPHostStatus: vi.fn().mockRejectedValue(new Error('offline')),
        ListExperts: vi.fn().mockResolvedValue('[]'),
        ListVirtualRepositories: vi.fn().mockResolvedValue('[]'),
        GetVCSClientStatus: vi.fn().mockResolvedValue(JSON.stringify({ kind: 'svn', available: false })),
        SelectVirtualRepositoryRoot: vi.fn().mockResolvedValue('D:\\workspace'),
        SaveVirtualRepository: vi.fn().mockImplementation(async (raw: string) => JSON.stringify({ ...JSON.parse(raw), id: 'vrepo_1' })),
        InspectVirtualRepository: vi.fn().mockResolvedValue('[]'),
        ListRepositoryCredentials: vi.fn().mockResolvedValue('[]'),
        ListRepositoryCredentialBindings: vi.fn().mockResolvedValue('{}'),
		SetRepositoryCredentialBinding: vi.fn().mockResolvedValue(undefined),
        OpenVirtualRepository: vi.fn(),
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
        StartVirtualRepositoryCodingTask: vi.fn(),
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
        expect(await screen.findByText('Loading…')).toBeTruthy();
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
        backend.InspectVirtualRepository.mockReturnValue(new Promise((_, reject) => { rejectRefresh = reject; }));

        render(<UtilitiesPage lang="en" />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        fireEvent.click(await screen.findByText('First workspace'));
        const refresh = await screen.findByText('Refresh status');
        fireEvent.click(refresh);
        await waitFor(() => expect(backend.InspectVirtualRepository).toHaveBeenCalledWith('D:\\first'));
        await act(async () => rejectRefresh(new Error('first workspace is unavailable')));
        fireEvent.click(screen.getByText('Open existing root'));
        expect(await screen.findByRole('tree', { name: 'Second workspace' })).toBeTruthy();
        expect(screen.queryByText('first workspace is unavailable')).toBeNull();
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
        await waitFor(() => expect(backend.GetVirtualRepositoryDirectoryStats).toHaveBeenCalledWith('D:\\workspace', 'first'));
        fireEvent.click(screen.getByText('Second folder'));
        await act(async () => resolveStats(JSON.stringify({ file_count: 99, size_bytes: 999 })));
        expect(screen.queryByText('99')).toBeNull();
        expect(screen.queryByText('999 bytes')).toBeNull();
    });

    it('explains SVN recovery options when auto-discovery fails', async () => {
        render(<UtilitiesPage lang="en" />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        expect(await screen.findByText('SVN command line client not found')).toBeTruthy();
        expect(screen.getByText('Search again')).toBeTruthy();
        expect(screen.getByText('Choose svn')).toBeTruthy();
        expect(screen.getByText('Installation guide')).toBeTruthy();
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
        expect((screen.getByTitle('New virtual folder') as HTMLButtonElement).disabled).toBe(true);
        expect((screen.getByTitle('Add mapping') as HTMLButtonElement).disabled).toBe(true);
        expect((screen.getByText('Repository credentials') as HTMLButtonElement).disabled).toBe(true);
        expect((screen.getByText('Revert') as HTMLButtonElement).disabled).toBe(true);
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
        await waitFor(() => expect(document.activeElement?.getAttribute('data-vrepo-node-id')).toBe('group'));
        fireEvent.keyDown(document.activeElement as HTMLElement, { key: 'ArrowRight' });
        await waitFor(() => expect(document.activeElement?.getAttribute('data-vrepo-node-id')).toBe('child'));
        fireEvent.keyDown(document.activeElement as HTMLElement, { key: 'ArrowLeft' });
        await waitFor(() => expect(document.activeElement?.getAttribute('data-vrepo-node-id')).toBe('group'));
        fireEvent.keyDown(document.activeElement as HTMLElement, { key: 'ArrowLeft' });
        expect(group.getAttribute('aria-expanded')).toBe('false');
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
		vi.spyOn(window, 'confirm').mockReturnValue(true);
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
		await waitFor(() => expect(backend.CreateRemoteVirtualRepositoryRoot).toHaveBeenCalledTimes(1));
		await waitFor(() => expect(backend.TestRemoteVirtualRepositoryConnection).toHaveBeenCalledTimes(2));
		expect(window.confirm).toHaveBeenCalled();
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

    it('starts a coding task from a recent virtual repository and hands it to a new tab', async () => {
        const backend = (window as any).go.main.App;
        const recent = { id: 'local_1', name: 'Product workspace', root_path: 'D:\\workspace' };
        backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([recent]));
        backend.StartVirtualRepositoryCodingTask.mockResolvedValue({ project_path: 'D:\\tasks\\local_1', task_title: 'Product workspace', agent_mode: 'coding_dev' });
        const onOpen = vi.fn();
        render(<UtilitiesPage lang="en" onOpenVirtualRepositoryTask={onOpen} />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        fireEvent.click(await screen.findByText('Start coding task'));
        await waitFor(() => expect(backend.StartVirtualRepositoryCodingTask).toHaveBeenCalledWith('local_1'));
        expect(onOpen).toHaveBeenCalledWith(expect.objectContaining({ project_path: 'D:\\tasks\\local_1', agent_mode: 'coding_dev' }));
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
        const recent = { id: 'local_1', name: 'Product workspace', root_path: 'D:\\workspace' };
        backend.ListVirtualRepositories.mockResolvedValue(JSON.stringify([recent]));
        let resolveLaunch!: (launch: unknown) => void;
        backend.StartVirtualRepositoryCodingTask.mockReturnValue(new Promise((resolve) => { resolveLaunch = resolve; }));
        render(<UtilitiesPage lang="en" />);
        fireEvent.click(screen.getByTestId('utilities-virtual-repository-card'));
        const launch = await screen.findByText('Start coding task');
        fireEvent.click(launch);
        fireEvent.click(launch);
        expect(backend.StartVirtualRepositoryCodingTask).toHaveBeenCalledTimes(1);
        resolveLaunch({ project_path: 'D:\\tasks\\local_1', task_title: 'Product workspace', agent_mode: 'coding_dev' });
        await waitFor(() => expect((screen.getByText('Start coding task') as HTMLButtonElement).disabled).toBe(false));
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
		fireEvent.click(await screen.findByTitle('Add mapping'));
		fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Source' } });
		fireEvent.change(screen.getByLabelText('Relative path'), { target: { value: 'src' } });
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

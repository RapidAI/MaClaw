import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { BrowserOpenURL, EventsOn } from '../../../wailsjs/runtime';
import './VirtualRepositoryWorkspace.css';

type RepoKind = 'git' | 'svn' | 'local';

type Binding = {
    kind: RepoKind;
    relative_path: string;
    remote_url?: string;
	ref_type?: 'branch' | 'tag';
	ref_name?: string;
    enabled: boolean;
};

type VRepoNode = {
    id: string;
    parent_id?: string;
    name: string;
    order: number;
    repository?: Binding;
};

type VRepo = {
    version: number;
    id?: string;
    name: string;
    root_path: string;
    nodes: VRepoNode[];
    updated_at?: string;
    remote?: { host: string; port?: number; user: string };
};

type NodeStatus = {
    node_id: string;
    kind: RepoKind;
    path?: string;
    exists: boolean;
    is_repository: boolean;
    branch?: string;
    remote_url?: string;
    status?: string;
    clean: boolean;
    error_code?: string;
    error?: string;
};

type ClientStatus = {
    kind: string;
    available: boolean;
    executable?: string;
    version?: string;
    source?: string;
    error?: string;
};

type Credential = {
    id: string;
    name: string;
    kind: 'git' | 'svn';
    username: string;
    scope?: string;
};

type CodingTaskLaunch = {
    project_path: string;
    task_title: string;
    agent_mode: 'coding_dev' | 'remote_coding_dev';
    remote_host?: string;
};

const parseJSON = <T,>(raw: unknown, fallback: T): T => {
    if (typeof raw === 'string') {
        try { return JSON.parse(raw) as T; } catch { return fallback; }
    }
    return (raw as T) ?? fallback;
};

const parseRequiredJSON = <T,>(raw: unknown, label: string): T => {
	if (raw == null || (typeof raw === 'string' && !raw.trim())) throw new Error(`${label} returned an empty response`);
	try {
		return (typeof raw === 'string' ? JSON.parse(raw) : raw) as T;
	} catch {
		throw new Error(`${label} returned malformed JSON`);
	}
};

const operationStatuses = new Set(['running', 'success', 'failed', 'partial_success', 'cancelled']);
const operationItemStatuses = new Set(['success', 'failed', 'cancelled']);
const parseOperationResult = (raw: unknown, label: string, expectedJobID?: string) => {
	const result = parseRequiredJSON<any>(raw, label);
	if (!result || typeof result !== 'object' || typeof result.job_id !== 'string' || !result.job_id.trim()) {
		throw new Error(`${label} returned no job id`);
	}
	if (expectedJobID && result.job_id !== expectedJobID) {
		throw new Error(`${label} returned a different job`);
	}
	if (typeof result.status !== 'string' || !operationStatuses.has(result.status)) {
		throw new Error(`${label} returned an invalid status`);
	}
	if (!Array.isArray(result.items)) {
		throw new Error(`${label} returned invalid operation items`);
	}
	if (result.items.length > 10000 || result.items.some((item: any) =>
		!item || typeof item !== 'object'
		|| typeof item.status !== 'string' || !operationItemStatuses.has(item.status)
		|| (item.node_id != null && typeof item.node_id !== 'string')
		|| (item.name != null && typeof item.name !== 'string')
		|| (item.error != null && typeof item.error !== 'string')
		|| (item.output != null && typeof item.output !== 'string')
		|| (item.error_code != null && typeof item.error_code !== 'string')
		|| (item.duration_ms != null && (typeof item.duration_ms !== 'number' || !Number.isFinite(item.duration_ms) || item.duration_ms < 0)))) {
		throw new Error(`${label} returned invalid operation items`);
	}
	return result;
};

const operationIsTerminal = (status: unknown) => typeof status === 'string' && status !== 'running' && operationStatuses.has(status);
const shouldAcceptOperationResult = (current: any, next: any) => {
	if (!current || current.job_id !== next.job_id) return true;
	if (operationIsTerminal(current.status)) return false;
	if (Array.isArray(current.items) && Array.isArray(next.items) && next.items.length < current.items.length) return false;
	return true;
};

const retryOperationForResult = (result: any) => {
	const failed = (result?.items || []).filter((item: any) => item.status === 'failed' && item.node_id);
	const action = result?.action === 'commit_push' && failed.length > 0 && failed.every((item: any) => item.error_code === 'push_failed_after_commit')
		? 'push'
		: result?.action;
	return { failed, action };
};

const app = () => (window as any)?.go?.main?.App || null;
const nodeId = () => `node_${Date.now()}_${Math.random().toString(36).slice(2, 8)}`;
const errorMessage = (error: unknown) => String((error as any)?.message || error);

export function VirtualRepositoryWorkspace({ isZh, onBack, onOpenCodingTask }: { isZh: boolean; onBack: () => void; onOpenCodingTask?: (launch: CodingTaskLaunch) => void }) {
    const text = isZh ? {
        title: '虚拟仓库', back: '返回实用工具', newRepo: '新建虚拟仓库', openRepo: '打开已有目录',
        recent: '最近使用', empty: '尚未创建虚拟仓库', emptyHint: '选择一个根目录，MaClaw 会在其中创建 .vrepo/manifest.json。',
        name: '名称', root: '根目录', choose: '选择目录', save: '保存', cancel: '取消', refresh: '刷新状态',
        addGroup: '新建虚拟目录', addRepo: '添加映射', credentials: '仓库凭据', svnClient: 'SVN 工具',
        type: '类型', path: '相对目录', remote: '仓库地址', enabled: '启用', parent: '上级目录', rootNode: '（根）', refType: '版本类型', refName: '分支/标签', defaultRef: '默认分支', checkout: '检出仓库', branchRef: '分支', tagRef: '标签',
        edit: '编辑', remove: '删除', openFolder: '打开目录', createDir: '目录不存在时创建', clean: '干净', changed: '有变更',
        local: '纯本地目录', loading: '加载中…', noClient: '未找到 SVN 命令行工具', search: '重新搜索', specify: '指定 svn',
        installSVN: '安装说明',
        foundClient: '已找到', credentialName: '凭据名称', username: '用户名', password: '密码或令牌', scope: '主机/Realm（可选）',
        addCredential: '新增凭据', manageCredentials: '凭据管理', noCredentials: '尚无已保存凭据', passwordHint: '编辑时留空表示不修改',
        deleteIndex: '从最近使用中移除', manifestNote: '删除这里只移除 MaClaw 索引，不会删除 .vrepo 或真实文件。',
        commit: '提交', push: '推送', commitPush: '提交并推送', revert: '还原', execute: '执行', preview: '预览',
        commitMessage: '提交说明', repositories: '个仓库', localSkipped: '个本地目录已跳过', revertWarning: '未提交的已跟踪更改将被丢弃；未跟踪文件会保留。',
        operation: '操作', close: '关闭', retryFailed: '重试失败项', calculateSize: '计算大小', files: '文件数', size: '大小', branch: '分支', status: '状态',
        noCredential: '不使用凭据', anyHost: '任意主机', operationRunningHint: '仓库操作运行期间不能修改虚拟目录树或启动其他操作。',
		location: '位置', localLocation: '本机', remoteLocation: '远程 SSH', editConnection: '编辑连接', startCodingTask: '启动编程任务', startingCodingTask: '正在启动…', server: '服务器', port: '端口', sshUser: 'SSH 用户名', sshPassword: 'SSH 密码', remoteRoot: '远程根目录', testConnection: '测试连接', trustHostKey: '信任并保存主机指纹', hostKeyPrompt: '首次连接，请核对并信任服务器指纹', connected: '连接成功', rootMissingPrompt: 'SSH 已连接，但远程根目录不存在。是否创建该目录？', createRemoteRoot: '创建远程根目录', createRemoteRootConfirm: '确认在远程服务器上创建此根目录？',
		cleanStatus: '仓库干净', changedStatus: '仓库有变更', errorStatus: '仓库状态异常',
    } : {
        title: 'Virtual Repository', back: 'Back to utilities', newRepo: 'New virtual repository', openRepo: 'Open existing root',
        recent: 'Recent', empty: 'No virtual repositories yet', emptyHint: 'Choose a root directory; MaClaw creates .vrepo/manifest.json inside it.',
        name: 'Name', root: 'Root directory', choose: 'Choose', save: 'Save', cancel: 'Cancel', refresh: 'Refresh status',
        addGroup: 'New virtual folder', addRepo: 'Add mapping', credentials: 'Repository credentials', svnClient: 'SVN client',
        type: 'Type', path: 'Relative path', remote: 'Repository URL', enabled: 'Enabled', parent: 'Parent folder', rootNode: '(root)', refType: 'Version type', refName: 'Branch/tag', defaultRef: 'Default branch', checkout: 'Checkout repository', branchRef: 'Branch', tagRef: 'Tag',
        edit: 'Edit', remove: 'Remove', openFolder: 'Open folder', createDir: 'Create directory if missing', clean: 'Clean', changed: 'Changed',
        local: 'Local directory', loading: 'Loading…', noClient: 'SVN command line client not found', search: 'Search again', specify: 'Choose svn',
        installSVN: 'Installation guide',
        foundClient: 'Found', credentialName: 'Credential name', username: 'Username', password: 'Password or token', scope: 'Host/realm (optional)',
        addCredential: 'Add credential', manageCredentials: 'Credential manager', noCredentials: 'No saved credentials', passwordHint: 'Leave blank while editing to keep the secret',
        deleteIndex: 'Remove from recent', manifestNote: 'This only removes the MaClaw index entry; .vrepo and real files remain untouched.',
        commit: 'Commit', push: 'Push', commitPush: 'Commit & push', revert: 'Revert', execute: 'Execute', preview: 'Preview',
        commitMessage: 'Commit message', repositories: 'repositories', localSkipped: 'local directories skipped', revertWarning: 'Uncommitted tracked changes will be discarded. Untracked files are preserved.',
        operation: 'Operation', close: 'Close', retryFailed: 'Retry failed', calculateSize: 'Calculate size', files: 'Files', size: 'Size', branch: 'Branch', status: 'Status',
        noCredential: 'No credential', anyHost: 'any host', operationRunningHint: 'The virtual tree and other operations are locked while a repository operation is running.',
		location: 'Location', localLocation: 'This computer', remoteLocation: 'Remote SSH', editConnection: 'Edit connection', startCodingTask: 'Start coding task', startingCodingTask: 'Starting…', server: 'Server', port: 'Port', sshUser: 'SSH username', sshPassword: 'SSH password', remoteRoot: 'Remote root directory', testConnection: 'Test connection', trustHostKey: 'Trust and save host key', hostKeyPrompt: 'First connection: verify and trust this server fingerprint', connected: 'Connected', rootMissingPrompt: 'SSH is connected, but the remote root does not exist. Create it now?', createRemoteRoot: 'Create remote root', createRemoteRootConfirm: 'Create this root directory on the remote server?',
		cleanStatus: 'Repository is clean', changedStatus: 'Repository has changes', errorStatus: 'Repository status error',
    };

    const [repos, setRepos] = useState<any[]>([]);
    const [repo, setRepo] = useState<VRepo | null>(null);
    const [selectedId, setSelectedId] = useState('');
    const [statuses, setStatuses] = useState<Record<string, NodeStatus>>({});
    const [mode, setMode] = useState<'none' | 'repo' | 'group' | 'mapping' | 'credentials'>('none');
    const [draft, setDraft] = useState<any>({});
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState('');
    const [svn, setSvn] = useState<ClientStatus | null>(null);
    const [credentials, setCredentials] = useState<Credential[]>([]);
    const [credentialBindings, setCredentialBindings] = useState<Record<string, string>>({});
    const [operation, setOperation] = useState<{ action: 'commit' | 'push' | 'commit_push' | 'revert'; message: string; preview?: any } | null>(null);
    const [operationResult, setOperationResult] = useState<any>(null);
	const [operationCancelPending, setOperationCancelPending] = useState(false);
    const [retryNodeIds, setRetryNodeIds] = useState<string[]>([]);
    const [directoryStats, setDirectoryStats] = useState<any>(null);
    const [expanded, setExpanded] = useState<Record<string, boolean>>({});
    const [remoteConnection, setRemoteConnection] = useState<any>(null);
    const [startingRepositoryID, setStartingRepositoryID] = useState('');
    const [openingRepositoryKey, setOpeningRepositoryKey] = useState('');
    const [selectingRoot, setSelectingRoot] = useState(false);
    const operationResultRef = useRef<any>(null);
    const operationDialogRef = useRef<HTMLElement | null>(null);
    const busyRef = useRef(false);
    const codingTaskStartingRef = useRef(false);
	const directoryPickerOpenRef = useRef(false);
	const operationPreviewRequestRef = useRef(0);
	const operationStartingRef = useRef(false);
	const repositorySessionRef = useRef(0);
		const directoryStatsRequestRef = useRef(0);
		const remoteConnectionRequestRef = useRef(0);
    const operationRunning = operationResult?.status === 'running';
    const mutationLocked = busy || operationRunning;
    busyRef.current = busy;

	const setActiveRepository = (nextRepository: VRepo) => {
		repositorySessionRef.current += 1;
		setRepo(nextRepository);
	};

    const loadRecent = useCallback(async () => {
        const backend = app();
        if (!backend?.ListVirtualRepositories) return;
        try { setRepos(parseJSON(await backend.ListVirtualRepositories(), [])); } catch (e: any) { setError(String(e?.message || e)); }
    }, []);

    const loadSVN = useCallback(async (search = false) => {
        const backend = app();
        if (!backend) return;
        try {
            const raw = search && backend.SearchVCSClient ? await backend.SearchVCSClient('svn') : await backend.GetVCSClientStatus?.('svn');
            setSvn(parseJSON(raw, { kind: 'svn', available: false }));
        } catch (e: any) { setSvn({ kind: 'svn', available: false, error: String(e?.message || e) }); setError(String(e?.message || e)); }
    }, []);

    useEffect(() => { void loadRecent(); void loadSVN(); }, [loadRecent, loadSVN]);

    const selectRoot = async () => {
        if (directoryPickerOpenRef.current) return;
        const backend = app();
        directoryPickerOpenRef.current = true;
        setSelectingRoot(true);
        try {
            const path = await backend?.SelectVirtualRepositoryRoot?.(draft.root_path || repo?.root_path || '');
            if (path) setDraft((current: any) => ({ ...current, root_path: path }));
		} catch (e: any) { setError(String(e?.message || e)); }
        finally { directoryPickerOpenRef.current = false; setSelectingRoot(false); }
    };

	const remoteConnectionPayload = () => JSON.stringify({ repository_id: draft.id || '', remote: { host: draft.ssh_host || '', port: Number(draft.ssh_port || 22), user: draft.ssh_user || '' }, root_path: draft.root_path || '', password: draft.ssh_password || '', trust_host_key: !!draft.trust_host_key });

	    const testRemoteConnection = async () => {
		if (busyRef.current) return;
		busyRef.current = true;
		const request = ++remoteConnectionRequestRef.current;
		const payload = remoteConnectionPayload();
	        setBusy(true); setError(''); setRemoteConnection(null);
	        try {
				const status = parseRequiredJSON<any>(await app()?.TestRemoteVirtualRepositoryConnection?.(payload), 'Connection test');
			if (request !== remoteConnectionRequestRef.current) return;
	            setRemoteConnection(status);
	            if (status?.error_code && status.error_code !== 'host_key_untrusted') setError(status.error || status.error_code);
	        } catch (e: any) { if (request === remoteConnectionRequestRef.current) setError(String(e?.message || e)); } finally { busyRef.current = false; setBusy(false); }
	    };

	const createRemoteRoot = async () => {
		if (busyRef.current) return;
		if (!window.confirm(text.createRemoteRootConfirm)) return;
		busyRef.current = true;
		const request = ++remoteConnectionRequestRef.current;
		const payload = remoteConnectionPayload();
		setBusy(true); setError('');
		try {
			await app()?.CreateRemoteVirtualRepositoryRoot?.(payload);
			const status = parseRequiredJSON<any>(await app()?.TestRemoteVirtualRepositoryConnection?.(payload), 'Connection test');
			if (request !== remoteConnectionRequestRef.current) return;
			setRemoteConnection(status);
			if (!status?.root_exists) setError(status?.error || status?.error_code || 'Remote root verification failed');
		} catch (e: any) { if (request === remoteConnectionRequestRef.current) setError(String(e?.message || e)); }
		finally { busyRef.current = false; setBusy(false); }
	};

	    const updateRemoteDraft = (changes: Record<string, unknown>) => {
		remoteConnectionRequestRef.current += 1;
	        setDraft((current: any) => ({ ...current, ...changes, trust_host_key: false }));
        setRemoteConnection(null);
    };

    const openExisting = async () => {
        if (directoryPickerOpenRef.current) return;
        const backend = app();
        directoryPickerOpenRef.current = true;
        setSelectingRoot(true);
        setError('');
        try {
			const root = await backend?.SelectVirtualRepositoryRoot?.('');
            if (!root) return;
			const opened = parseRequiredJSON<VRepo>(await backend.OpenVirtualRepository(root), 'Open virtual repository');
			setActiveRepository(opened); setSelectedId(''); setStatuses({}); setDirectoryStats(null); await loadRecent();
		} catch (e: any) { setError(String(e?.message || e)); }
        finally { directoryPickerOpenRef.current = false; setSelectingRoot(false); }
    };

    const beginNewRepository = () => {
        setMode('repo');
        setRemoteConnection(null);
        setDraft({ name: '', root_path: '', location: 'local' });
    };

    const openRecentRepository = async (item: any) => {
        const key = String(item?.id || item?.root_path || '').trim();
        if (!key || openingRepositoryKey) return;
        setOpeningRepositoryKey(key);
        setError('');
        try {
            const opened = parseRequiredJSON<VRepo>(item.remote
                ? await app()?.OpenRemoteVirtualRepository?.(item.id)
                : await app()?.OpenVirtualRepository?.(item.root_path), 'Open virtual repository');
			setActiveRepository(opened);
            setSelectedId('');
            setStatuses({});
            setDirectoryStats(null);
        } catch (e: any) {
            setError(String(e?.message || e));
        } finally {
            setOpeningRepositoryKey('');
        }
    };

    const startCodingTask = async (item: any) => {
        const id = String(item?.id || '').trim();
        if (!id || codingTaskStartingRef.current) return;
        codingTaskStartingRef.current = true;
        setStartingRepositoryID(id); setError('');
        try {
            const launch = await app()?.StartVirtualRepositoryCodingTask?.(id);
            if (!launch?.project_path) throw new Error(isZh ? '创建编程任务失败' : 'Failed to create coding task');
            onOpenCodingTask?.(launch);
		} catch (e: any) { setError(String(e?.message || e)); }
        finally { codingTaskStartingRef.current = false; setStartingRepositoryID(''); }
    };

    const saveRepo = async (nextRepo: VRepo) => {
        const backend = app();
        setBusy(true); setError('');
        try {
            const saved = nextRepo.remote
				? parseRequiredJSON<VRepo>(await backend.SaveRemoteVirtualRepository(JSON.stringify({ repository: nextRepo, password: draft.ssh_password || '', trust_host_key: !!draft.trust_host_key })), 'Save remote virtual repository')
				: parseRequiredJSON<VRepo>(await backend.SaveVirtualRepository(JSON.stringify(nextRepo)), 'Save virtual repository');
			setActiveRepository(saved); setMode('none'); setDraft({}); await loadRecent();
        } catch (e: any) { setError(String(e?.message || e)); } finally { setBusy(false); }
    };

    const refreshStatus = useCallback(async () => {
        if (!repo) return;
		const repositorySession = repositorySessionRef.current;
        setBusy(true); setError('');
        try {
            const raw = repo.remote ? await app()?.InspectRemoteVirtualRepository?.(repo.id) : await app()?.InspectVirtualRepository(repo.root_path);
            const list = parseJSON<NodeStatus[]>(raw, []);
			if (repositorySession !== repositorySessionRef.current) return;
            setStatuses(Object.fromEntries(list.map((item) => [item.node_id, item])));
		} catch (e: any) {
			if (repositorySession === repositorySessionRef.current) setError(String(e?.message || e));
		} finally {
			if (repositorySession === repositorySessionRef.current) setBusy(false);
		}
    }, [repo]);

    useEffect(() => {
        try {
			return EventsOn('virtual-repository:job-updated', (raw: unknown) => {
				let result: any;
				try { result = parseOperationResult(raw, 'Operation update', operationResultRef.current?.job_id); } catch { return; }
				if (!operationResultRef.current?.job_id) return;
				if (!shouldAcceptOperationResult(operationResultRef.current, result)) return;
                operationResultRef.current = result;
                setOperationResult(result);
                if (result.status !== 'running') {
					setOperationCancelPending(false);
                    setBusy(false);
                    void refreshStatus();
                }
            });
        } catch { return undefined; }
    }, [refreshStatus]);

    const saveNode = async () => {
        if (!repo) return;
        const id = draft.id || nodeId();
        const node: VRepoNode = {
            id, parent_id: draft.parent_id || undefined, name: String(draft.name || '').trim(), order: Number(draft.order || repo.nodes.length * 10 + 10),
            repository: mode === 'mapping' ? {
                kind: draft.kind || 'git', relative_path: String(draft.relative_path || '').trim(),
                remote_url: draft.kind === 'local' ? undefined : String(draft.remote_url || '').trim(),
				ref_type: draft.kind === 'local' || !draft.ref_name ? undefined : (draft.ref_type || 'branch'),
				ref_name: draft.kind === 'local' ? undefined : String(draft.ref_name || '').trim(), enabled: draft.enabled !== false,
            } : undefined,
        };
        setBusy(true); setError('');
        try {
            if (mode === 'mapping' && draft.create_directory) {
                if (repo.remote) await app()?.CreateRemoteVirtualRepositoryDirectory?.(repo.id, node.repository?.relative_path || '');
                else await app()?.CreateVirtualRepositoryDirectory?.(repo.root_path, node.repository?.relative_path || '');
            }
            const nodes = repo.nodes.some((item) => item.id === id) ? repo.nodes.map((item) => item.id === id ? node : item) : [...repo.nodes, node];
            const savePayload = { ...repo, nodes };
			const saved = repo.remote ? parseRequiredJSON<VRepo>(await app()?.SaveRemoteVirtualRepository?.(JSON.stringify({ repository: savePayload })), 'Save remote virtual repository') : parseRequiredJSON<VRepo>(await app()?.SaveVirtualRepository?.(JSON.stringify(savePayload)), 'Save virtual repository');
            if (saved.id && mode === 'mapping' && draft.kind !== 'local') {
                try {
                    await app()?.SetRepositoryCredentialBinding?.(saved.id, id, draft.credential_id || '');
                } catch (bindingError) {
					// The portable manifest is already persisted. Keep the UI aligned with
					// disk state and report the failed machine-local follow-up separately.
					// A manifest rollback is unsafe under concurrent edits and invalid for
					// remote repositories.
					setActiveRepository(saved); setMode('none'); setDraft({}); setSelectedId(id);
					await loadRecent();
					throw new Error(isZh
						? `映射已保存，但凭据绑定更新失败：${errorMessage(bindingError)}`
						: `The mapping was saved, but its credential binding could not be updated: ${errorMessage(bindingError)}`);
                }
                await loadCredentialBindings(saved.id);
            }
			setActiveRepository(saved); setMode('none'); setDraft({}); await loadRecent();
            setSelectedId(id);
        } catch (e: any) { setError(String(e?.message || e)); } finally { setBusy(false); }
    };

    const removeNode = async (id: string) => {
        if (!repo) return;
        const descendants = new Set([id]);
		const childrenByParent = new Map<string, string[]>();
		for (const node of repo.nodes) {
			if (!node.parent_id) continue;
			const children = childrenByParent.get(node.parent_id);
			if (children) children.push(node.id);
			else childrenByParent.set(node.parent_id, [node.id]);
		}
		const queue = [id];
		for (let index = 0; index < queue.length; index++) {
			for (const childID of childrenByParent.get(queue[index]) || []) {
				if (!descendants.has(childID)) {
					descendants.add(childID);
					queue.push(childID);
				}
			}
        }
        const label = repo.nodes.find((node) => node.id === id)?.name || id;
        if (!window.confirm(isZh ? `确认从虚拟目录树移除“${label}”及其子项？不会删除真实文件。` : `Remove “${label}” and its children from the virtual tree? Real files will not be deleted.`)) return;
        await saveRepo({ ...repo, nodes: repo.nodes.filter((node) => !descendants.has(node.id)) });
        setSelectedId('');
    };

    const openMappedFolder = async () => {
        if (!repo || !selected?.repository) return;
        const path = selectedStatus?.path || `${repo.root_path.replace(/[\\/]$/, '')}/${selected.repository.relative_path}`;
        try { await app()?.OpenFileOrShowInFolder?.(path); } catch (e: any) { setError(String(e?.message || e)); }
    };

    const nodesByParent = useMemo(() => {
        const result = new Map<string, VRepoNode[]>();
        for (const node of repo?.nodes || []) {
            const parentID = node.parent_id || '';
            const children = result.get(parentID);
            if (children) children.push(node);
            else result.set(parentID, [node]);
        }
        for (const children of result.values()) {
            children.sort((a, b) => a.order - b.order || a.name.localeCompare(b.name));
        }
        return result;
    }, [repo?.nodes]);

    const visibleNodeIDs = useMemo(() => {
        const ids: string[] = [];
        const visit = (parentID = '') => {
            for (const node of nodesByParent.get(parentID) || []) {
                ids.push(node.id);
                if (expanded[node.id] !== false) visit(node.id);
            }
        };
        visit();
        return ids;
    }, [nodesByParent, expanded]);

    const focusTreeItem = (id: string) => {
        window.requestAnimationFrame(() => {
            const item = Array.from(document.querySelectorAll<HTMLElement>('[data-vrepo-node-id]'))
                .find((candidate) => candidate.dataset.vrepoNodeId === id);
            item?.focus();
        });
    };

    const renderNodes = (parentId = '', depth = 0): React.ReactNode => (nodesByParent.get(parentId) || []).map((node) => {
        const status = statuses[node.id];
        const hasChildren = (nodesByParent.get(node.id)?.length || 0) > 0;
        const isExpanded = expanded[node.id] !== false;
        return <div key={node.id} className="vrepo-tree__branch">
            <button type="button" role="treeitem" data-vrepo-node-id={node.id} tabIndex={selectedId === node.id || (!selectedId && visibleNodeIDs[0] === node.id) ? 0 : -1} aria-level={depth + 1} aria-selected={selectedId === node.id} aria-expanded={hasChildren ? isExpanded : undefined} className={`vrepo-tree__item${selectedId === node.id ? ' is-selected' : ''}`} style={{ paddingLeft: `${12 + depth * 18}px` }} onClick={() => setSelectedId(node.id)} onDoubleClick={() => hasChildren && setExpanded((current) => ({ ...current, [node.id]: !isExpanded }))} onKeyDown={(event) => {
                const index = visibleNodeIDs.indexOf(node.id);
                if (event.key === 'ArrowDown' || event.key === 'ArrowUp' || event.key === 'Home' || event.key === 'End') {
                    event.preventDefault();
                    const nextIndex = event.key === 'Home' ? 0 : event.key === 'End' ? visibleNodeIDs.length - 1 : Math.max(0, Math.min(visibleNodeIDs.length - 1, index + (event.key === 'ArrowDown' ? 1 : -1)));
                    const nextID = visibleNodeIDs[nextIndex];
                    if (nextID) { setSelectedId(nextID); focusTreeItem(nextID); }
                    return;
                }
                if (event.key === 'ArrowRight' && hasChildren) {
                    event.preventDefault();
                    if (!isExpanded) setExpanded((current) => ({ ...current, [node.id]: true }));
                    else {
                        const firstChild = nodesByParent.get(node.id)?.[0]?.id;
                        if (firstChild) { setSelectedId(firstChild); focusTreeItem(firstChild); }
                    }
                }
                if (event.key === 'ArrowLeft') {
                    if (hasChildren && isExpanded) {
                        event.preventDefault(); setExpanded((current) => ({ ...current, [node.id]: false }));
                    } else if (node.parent_id) {
                        event.preventDefault(); setSelectedId(node.parent_id); focusTreeItem(node.parent_id);
                    }
                }
            }}>
				<span className="vrepo-tree__chevron" aria-hidden>{hasChildren ? isExpanded ? '▾' : '▸' : ''}</span>
                <span className="vrepo-tree__kind">{node.repository ? node.repository.kind.toUpperCase() : 'DIR'}</span>
                <span>{node.name}</span>
				{status ? <span className={`vrepo-tree__state ${status.error ? 'is-error' : status.clean ? 'is-clean' : 'is-dirty'}`} role="img" aria-label={status.error ? `${text.errorStatus}: ${status.error}` : status.clean ? text.cleanStatus : text.changedStatus}>{status.error ? '!' : status.clean ? '✓' : '●'}</span> : null}
            </button>
            {isExpanded ? renderNodes(node.id, depth + 1) : null}
        </div>;
    });

    const selected = repo?.nodes.find((node) => node.id === selectedId);
    const selectedStatus = selected ? statuses[selected.id] : undefined;

    const loadCredentialBindings = useCallback(async (repositoryId?: string) => {
        if (!repositoryId) { setCredentialBindings({}); return; }
        try { setCredentialBindings(parseJSON(await app()?.ListRepositoryCredentialBindings?.(repositoryId), {})); } catch { setCredentialBindings({}); }
    }, []);

	useEffect(() => {
		directoryStatsRequestRef.current += 1;
		setDirectoryStats(null);
	}, [selectedId]);

	const loadDirectoryStats = async () => {
		if (!repo || !selected?.repository || selected.repository.kind !== 'local') return;
		const requestID = ++directoryStatsRequestRef.current;
		const repositorySession = repositorySessionRef.current;
		const selectedNodeID = selected.id;
		setError('');
		try {
			const raw = repo.remote
				? await app()?.GetRemoteVirtualRepositoryDirectoryStats?.(repo.id, selected.repository.relative_path)
				: await app()?.GetVirtualRepositoryDirectoryStats?.(repo.root_path, selected.repository.relative_path);
			if (requestID === directoryStatsRequestRef.current && repositorySession === repositorySessionRef.current && selectedNodeID === selectedId) {
				setDirectoryStats(parseJSON(raw, null));
			}
		} catch (e: any) {
			if (requestID === directoryStatsRequestRef.current && repositorySession === repositorySessionRef.current && selectedNodeID === selectedId) {
				setError(String(e?.message || e));
			}
		}
	};

    const startCredentialManager = async () => {
        setMode('credentials'); setDraft({});
        try { setCredentials(parseJSON(await app()?.ListRepositoryCredentials?.(''), [])); } catch (e: any) { setError(String(e?.message || e)); }
    };

    const deleteCredential = async (credential: Credential) => {
        if (!window.confirm(isZh ? `确认删除凭据“${credential.name}”？所有仓库绑定将同时解除。` : `Delete credential “${credential.name}”? All repository bindings to it will be removed.`)) return;
        setBusy(true); setError('');
        try {
            await app()?.DeleteRepositoryCredential?.(credential.id);
            setCredentials(parseJSON(await app()?.ListRepositoryCredentials?.(''), []));
            if (repo?.id) await loadCredentialBindings(repo.id);
            if (draft.id === credential.id) setDraft({});
        } catch (e: any) { setError(String(e?.message || e)); } finally { setBusy(false); }
    };

    useEffect(() => { void loadCredentialBindings(repo?.id); }, [repo?.id, loadCredentialBindings]);

    const saveCredential = async () => {
        setBusy(true); setError('');
        try {
            await app()?.SaveRepositoryCredential?.(JSON.stringify(draft));
            setDraft({}); setCredentials(parseJSON(await app()?.ListRepositoryCredentials?.(''), []));
        } catch (e: any) { setError(String(e?.message || e)); } finally { setBusy(false); }
    };

    const specifySVN = async () => {
        const backend = app();
        try {
            const path = await backend?.SelectVCSClientExecutable?.('svn');
            if (!path) return;
            setSvn(parseJSON(await backend.SetVCSClientExecutable('svn', path), { kind: 'svn', available: false }));
        } catch (e: any) { setError(String(e?.message || e)); }
    };

    const previewOperation = async (action: 'commit' | 'push' | 'commit_push' | 'revert') => {
        if (!repo) return;
		const requestID = ++operationPreviewRequestRef.current;
        setRetryNodeIds([]);
        const next = { action, message: '', preview: undefined as any };
        setOperation(next); operationResultRef.current = null; setOperationResult(null); setError('');
        if (action === 'commit' || action === 'commit_push') return;
        try {
			const preview = parseRequiredJSON<any>(await app()?.PreviewVirtualRepositoryOperation?.(JSON.stringify({ root_path: repo.root_path, repository_id: repo.id, node_id: selectedId, action })), 'Operation preview');
			if (operationPreviewRequestRef.current === requestID) setOperation({ ...next, preview });
		} catch (e: any) { if (operationPreviewRequestRef.current === requestID) setError(String(e?.message || e)); }
    };

    const loadOperationPreview = async () => {
        if (!repo || !operation) return;
		const requestID = ++operationPreviewRequestRef.current;
		setBusy(true); setError('');
        try {
			const preview = parseRequiredJSON<any>(await app()?.PreviewVirtualRepositoryOperation?.(JSON.stringify({ root_path: repo.root_path, repository_id: repo.id, node_id: selectedId, node_ids: retryNodeIds, action: operation.action, message: operation.message })), 'Operation preview');
			if (operationPreviewRequestRef.current === requestID) setOperation({ ...operation, preview });
		} catch (e: any) {
			if (operationPreviewRequestRef.current === requestID) setError(String(e?.message || e));
		} finally {
			if (operationPreviewRequestRef.current === requestID) setBusy(false);
		}
    };

    const executeOperation = async () => {
		if (!repo || !operation || operationStartingRef.current) return;
		operationStartingRef.current = true;
        setBusy(true); setError('');
        try {
			const result = parseOperationResult(await app()?.StartVirtualRepositoryOperation?.(JSON.stringify({ root_path: repo.root_path, repository_id: repo.id, node_id: selectedId, node_ids: retryNodeIds, action: operation.action, message: operation.message, expected_repository_id: operation.preview?.repository_id, expected_updated_at: operation.preview?.updated_at })), 'Start operation');
			operationResultRef.current = result;
			setOperationResult(result); setOperation(null); setRetryNodeIds([]);
			if (result.status !== 'running') {
				setBusy(false);
				setOperationCancelPending(false);
				void refreshStatus();
			}
		} catch (e: any) { setError(String(e?.message || e)); setBusy(false); }
		finally { operationStartingRef.current = false; }
    };

    useEffect(() => {
        if (!operationResult?.job_id || operationResult.status !== 'running') return;
        const jobID = operationResult.job_id;
		let pollInFlight = false;
        const timer = window.setInterval(async () => {
			if (pollInFlight) return;
			pollInFlight = true;
			try {
				const result = parseOperationResult(await app()?.GetVirtualRepositoryOperation?.(jobID), 'Operation status', jobID);
				if (operationResultRef.current?.job_id !== jobID) return;
				if (!shouldAcceptOperationResult(operationResultRef.current, result)) return;
                operationResultRef.current = result;
                setOperationResult(result);
				if (result.status !== 'running') { setBusy(false); setOperationCancelPending(false); window.clearInterval(timer); void refreshStatus(); }
			} catch (e: any) {
				if (operationResultRef.current?.job_id === jobID) {
					setError(String(e?.message || e));
				}
			}
			finally { pollInFlight = false; }
        }, 750);
        return () => window.clearInterval(timer);
    }, [operationResult?.job_id, operationResult?.status]);

	const cancelOperation = async () => {
		if (!operationResult?.job_id || operationCancelPending) return;
		setOperationCancelPending(true);
		setError('');
		try {
			await app()?.CancelVirtualRepositoryOperation?.(operationResult.job_id);
		} catch (e: any) {
			setOperationCancelPending(false);
			setError(String(e?.message || e));
		}
	};

    useEffect(() => {
        if (!operation) return;
        const previousFocus = document.activeElement as HTMLElement | null;
        const dialog = operationDialogRef.current;
        window.setTimeout(() => dialog?.querySelector<HTMLElement>('textarea, button:not(:disabled)')?.focus(), 0);
        const onKeyDown = (event: KeyboardEvent) => {
            if (event.key === 'Escape' && !busyRef.current) {
                event.preventDefault();
                setOperation(null);
				operationPreviewRequestRef.current++;
                setRetryNodeIds([]);
                return;
            }
            if (event.key !== 'Tab' || !dialog) return;
            const focusable = Array.from(dialog.querySelectorAll<HTMLElement>('button:not(:disabled), textarea:not(:disabled), input:not(:disabled), select:not(:disabled)'));
            if (!focusable.length) return;
            const first = focusable[0];
            const last = focusable[focusable.length - 1];
            if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
            else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
        };
        window.addEventListener('keydown', onKeyDown, true);
        return () => {
            window.removeEventListener('keydown', onKeyDown, true);
            previousFocus?.focus();
        };
    }, [operation]);

    return <div className="utilities-page vrepo-page" data-testid="virtual-repository-workspace">
        <header className="vrepo-header">
            <div><button type="button" className="utilities-link" onClick={onBack}>{text.back}</button><h1>{text.title}</h1>{repo?.remote ? <p className="vrepo-remote-location">{repo.remote.user}@{repo.remote.host}:{repo.remote.port || 22} · {repo.root_path}</p> : null}</div>
            {repo && mode === 'none' ? <div className="vrepo-actions">
                <div className="vrepo-actions__setup">
                    <button type="button" className="secondary" disabled={mutationLocked || selectingRoot} onClick={beginNewRepository}>{text.newRepo}</button>
                    <button type="button" className="secondary" disabled={mutationLocked || selectingRoot} onClick={() => void openExisting()}>{selectingRoot ? text.loading : text.openRepo}</button>
                    {repo?.remote ? <button type="button" className="secondary" disabled={mutationLocked} onClick={() => { setMode('repo'); setRemoteConnection(null); setDraft({ ...repo, location: 'remote', ssh_host: repo.remote?.host || '', ssh_port: repo.remote?.port || 22, ssh_user: repo.remote?.user || '', ssh_password: '' }); }}>{text.editConnection}</button> : null}
                    {repo ? <button type="button" className="secondary" disabled={mutationLocked || !!startingRepositoryID} onClick={() => void startCodingTask(repo)}>{startingRepositoryID === repo.id ? text.startingCodingTask : text.startCodingTask}</button> : null}
                    {repo ? <button type="button" className="secondary" disabled={mutationLocked} onClick={() => void startCredentialManager()}>{text.credentials}</button> : null}
                </div>
                {repo ? <div className="vrepo-actions__operation">
                    <button type="button" className="secondary" disabled={mutationLocked} onClick={() => void refreshStatus()}>{text.refresh}</button>
                    <button type="button" className="secondary" disabled={mutationLocked} onClick={() => void previewOperation('commit')}>{text.commit}</button>
                    <button type="button" className="secondary" disabled={mutationLocked} onClick={() => void previewOperation('push')}>{text.push}</button>
                    <button type="button" disabled={mutationLocked} onClick={() => void previewOperation('commit_push')}>{text.commitPush}</button>
                    <button type="button" className="danger" disabled={mutationLocked} onClick={() => void previewOperation('revert')}>{text.revert}</button>
                </div> : null}
            </div> : null}
        </header>
        {error ? <p className="utilities-error" role="alert">{error}</p> : null}
        {!repo?.remote ? <div className="vrepo-client-status">
            <strong>{text.svnClient}</strong>
            {svn?.available ? <span>{text.foundClient}: {svn.version} · <code>{svn.executable}</code> ({svn.source})</span> : <span>{text.noClient}</span>}
            <button type="button" className="utilities-link" disabled={mutationLocked} onClick={() => void loadSVN(true)}>{text.search}</button>
            <button type="button" className="utilities-link" disabled={mutationLocked} onClick={() => void specifySVN()}>{text.specify}</button>
            {!svn?.available ? <button type="button" className="utilities-link" onClick={() => { try { BrowserOpenURL('https://subversion.apache.org/packages.html'); } catch { window.open('https://subversion.apache.org/packages.html', '_blank', 'noopener'); } }}>{text.installSVN}</button> : null}
        </div> : null}

        {!repo && mode !== 'repo' ? <section className="vrepo-empty">
            <div className="vrepo-empty__intro">
                <span className="vrepo-empty__mark" aria-hidden="true">V</span>
                <div><h2>{text.empty}</h2><p>{text.emptyHint}</p></div>
            </div>
            <div className="vrepo-empty__actions">
                <button type="button" disabled={mutationLocked || selectingRoot} onClick={beginNewRepository}>{text.newRepo}</button>
                <button type="button" className="secondary" disabled={mutationLocked || selectingRoot} onClick={() => void openExisting()}>{selectingRoot ? text.loading : text.openRepo}</button>
            </div>
            {repos.length ? <><h3>{text.recent}</h3><div className="vrepo-recent">{repos.map((item) => { const itemKey = String(item.id || item.root_path || ''); const opening = openingRepositoryKey === itemKey; return <div className="vrepo-recent__row" key={item.id || item.root_path}><button className="vrepo-recent__open" type="button" disabled={!!openingRepositoryKey || selectingRoot} onClick={() => void openRecentRepository(item)}><strong>{item.name}</strong><span>{opening ? text.loading : item.root_path}</span></button><button className="secondary vrepo-recent__task" type="button" disabled={!!startingRepositoryID || !!openingRepositoryKey || selectingRoot} onClick={() => void startCodingTask(item)}>{startingRepositoryID === item.id ? text.startingCodingTask : text.startCodingTask}</button></div>; })}</div></> : null}
        </section> : null}

        {mode === 'repo' ? <section className="vrepo-editor">
            <h2>{draft.id ? text.editConnection : text.newRepo}</h2>
            <label>{text.name}<input value={draft.name || ''} onChange={(e) => setDraft({ ...draft, name: e.target.value })} /></label>
	            <label>{text.location}<select value={draft.location || 'local'} onChange={(e) => { remoteConnectionRequestRef.current += 1; setDraft({ ...draft, location: e.target.value }); setRemoteConnection(null); }}><option value="local">{text.localLocation}</option><option value="remote">{text.remoteLocation}</option></select></label>
            {draft.location === 'remote' ? <>
                <div className="vrepo-inline"><label>{text.server}<input value={draft.ssh_host || ''} onChange={(e) => updateRemoteDraft({ ssh_host: e.target.value })} /></label><label>{text.port}<input type="number" min="1" max="65535" value={draft.ssh_port || 22} onChange={(e) => updateRemoteDraft({ ssh_port: e.target.value })} /></label></div>
                <label>{text.sshUser}<input value={draft.ssh_user || ''} onChange={(e) => updateRemoteDraft({ ssh_user: e.target.value })} /></label>
                <label>{text.sshPassword}<input type="password" autoComplete="new-password" value={draft.ssh_password || ''} placeholder={draft.id ? text.passwordHint : ''} onChange={(e) => updateRemoteDraft({ ssh_password: e.target.value })} /></label>
                <label>{text.remoteRoot}<input value={draft.root_path || ''} placeholder="/srv/workspace" onChange={(e) => updateRemoteDraft({ root_path: e.target.value })} /></label>
                {remoteConnection?.error_code === 'host_key_untrusted' ? <div className="vrepo-host-key"><strong>{text.hostKeyPrompt}</strong><code>{remoteConnection.host_key_algorithm} {remoteConnection.host_key_fingerprint}</code><label className="vrepo-check"><input type="checkbox" checked={!!draft.trust_host_key} onChange={(e) => setDraft({ ...draft, trust_host_key: e.target.checked })} />{text.trustHostKey}</label></div> : null}
				{remoteConnection?.connected && remoteConnection?.error_code === 'root_not_found' ? <div className="vrepo-root-missing" role="alert"><span>{text.rootMissingPrompt}</span><button type="button" disabled={busy} onClick={() => void createRemoteRoot()}>{text.createRemoteRoot}</button></div> : null}
                {remoteConnection?.connected && remoteConnection?.root_exists ? <p className="vrepo-connection-ok">{text.connected} · Git {remoteConnection.git_version || '—'} · SVN {remoteConnection.svn_version || '—'}</p> : null}
                <button type="button" className="secondary" disabled={busy} onClick={() => void testRemoteConnection()}>{text.testConnection}</button>
            </> : <label>{text.root}<div className="vrepo-inline"><input value={draft.root_path || ''} readOnly /><button type="button" className="secondary" disabled={busy || selectingRoot} onClick={() => void selectRoot()}>{selectingRoot ? text.loading : text.choose}</button></div></label>}
	            <div className="vrepo-form-actions"><button type="button" onClick={() => void saveRepo({ ...draft, version: draft.version || 1, name: draft.name || '', root_path: draft.root_path || '', remote: draft.location === 'remote' ? { host: draft.ssh_host || '', port: Number(draft.ssh_port || 22), user: draft.ssh_user || '' } : undefined, nodes: draft.nodes || [] })} disabled={busy || selectingRoot || (draft.location === 'remote' && !(remoteConnection?.connected && remoteConnection?.host_key_trusted && remoteConnection?.root_exists && !remoteConnection?.error_code))}>{text.save}</button><button type="button" className="secondary" disabled={selectingRoot} onClick={() => setMode('none')}>{text.cancel}</button></div>
        </section> : null}

        {repo && mode !== 'repo' && mode !== 'credentials' ? <div className="vrepo-layout">
            <aside className="vrepo-tree" aria-label={repo.name}>
				<div className="vrepo-tree__head"><button type="button" className="vrepo-tree__root" aria-pressed={!selectedId} onClick={() => setSelectedId('')}><strong>{repo.name}</strong><span>{repo.root_path}</span></button><div><button type="button" aria-label={text.addGroup} title={text.addGroup} disabled={mutationLocked} onClick={() => { setMode('group'); setDraft({ parent_id: selected?.repository ? selected.parent_id : selectedId }); }}>+DIR</button><button type="button" aria-label={text.addRepo} title={text.addRepo} disabled={mutationLocked} onClick={() => { setMode('mapping'); setDraft({ parent_id: selected?.repository ? selected.parent_id : selectedId, kind: 'git', enabled: true }); }}>+MAP</button></div></div>
                <div role="tree" aria-label={repo.name}>{renderNodes()}</div>
            </aside>
            <main className="vrepo-detail">
                {(mode === 'group' || mode === 'mapping') ? <section className="vrepo-editor">
                    <h2>{mode === 'group' ? text.addGroup : text.addRepo}</h2>
                    <label>{text.name}<input value={draft.name || ''} onChange={(e) => setDraft({ ...draft, name: e.target.value })} /></label>
                    <label>{text.parent}<select value={draft.parent_id || ''} onChange={(e) => setDraft({ ...draft, parent_id: e.target.value })}><option value="">{text.rootNode}</option>{repo.nodes.filter((n) => !n.repository).map((n) => <option key={n.id} value={n.id}>{n.name}</option>)}</select></label>
                    {mode === 'mapping' ? <>
                        <label>{text.type}<select value={draft.kind || 'git'} onChange={(e) => setDraft({ ...draft, kind: e.target.value, credential_id: '' })}><option value="git">Git</option><option value="svn">SVN</option><option value="local">{text.local}</option></select></label>
                        <label>{text.path}<input value={draft.relative_path || ''} onChange={(e) => setDraft({ ...draft, relative_path: e.target.value })} placeholder="build/release" /></label>
                        {draft.kind !== 'local' ? <><label>{text.remote}<input value={draft.remote_url || ''} onChange={(e) => setDraft({ ...draft, remote_url: e.target.value })} /></label><label>{text.refType}<select value={draft.ref_type || 'branch'} onChange={(e) => setDraft({ ...draft, ref_type: e.target.value })}><option value="branch">{text.branchRef}</option><option value="tag">{text.tagRef}</option></select></label><label>{text.refName}<input value={draft.ref_name || ''} placeholder={text.defaultRef} onChange={(e) => setDraft({ ...draft, ref_name: e.target.value })} /></label><label>{text.credentials}<select value={draft.credential_id || credentialBindings[draft.id] || ''} onFocus={async () => { if (!credentials.length) setCredentials(parseJSON(await app()?.ListRepositoryCredentials?.(draft.kind || ''), [])); }} onChange={(e) => setDraft({ ...draft, credential_id: e.target.value })}><option value="">{text.noCredential}</option>{credentials.filter((item) => item.kind === draft.kind).map((item) => <option key={item.id} value={item.id}>{item.name} · {item.username}</option>)}</select></label></> : null}
                        <label className="vrepo-check"><input type="checkbox" checked={!!draft.create_directory} onChange={(e) => setDraft({ ...draft, create_directory: e.target.checked })} />{text.createDir}</label>
                        <label className="vrepo-check"><input type="checkbox" checked={draft.enabled !== false} onChange={(e) => setDraft({ ...draft, enabled: e.target.checked })} />{text.enabled}</label>
                    </> : null}
                    <div className="vrepo-form-actions"><button type="button" onClick={() => void saveNode()} disabled={busy}>{text.save}</button><button type="button" className="secondary" onClick={() => setMode('none')}>{text.cancel}</button></div>
                </section> : selected ? <section>
                    <div className="vrepo-detail__title"><div><span className="vrepo-detail__kind">{selected.repository?.kind.toUpperCase() || 'DIR'}</span><h2>{selected.name}</h2></div><div><button type="button" className="secondary" disabled={mutationLocked} onClick={() => { setMode(selected.repository ? 'mapping' : 'group'); setDraft({ ...selected, ...(selected.repository || {}) }); }}>{text.edit}</button><button type="button" className="danger" disabled={mutationLocked} onClick={() => void removeNode(selected.id)}>{text.remove}</button></div></div>
					{selected.repository ? <><dl className="vrepo-facts"><dt>{text.path}</dt><dd>{selectedStatus?.path || selected.repository.relative_path}</dd>{selected.repository.remote_url ? <><dt>{text.remote}</dt><dd>{selectedStatus?.remote_url || selected.repository.remote_url}</dd></> : null}{selected.repository.ref_name ? <><dt>{selected.repository.ref_type === 'tag' ? text.tagRef : text.branchRef}</dt><dd>{selected.repository.ref_name}</dd></> : null}{selectedStatus?.branch ? <><dt>{text.branch}</dt><dd>{selectedStatus.branch}</dd></> : null}<dt>{text.status}</dt><dd>{selectedStatus?.error || (selectedStatus ? selectedStatus.clean ? text.clean : text.changed : '—')}</dd>{directoryStats && selected.repository.kind === 'local' ? <><dt>{text.files}</dt><dd>{directoryStats.file_count}</dd><dt>{text.size}</dt><dd>{directoryStats.size_bytes.toLocaleString()} bytes</dd></> : null}</dl>{!repo.remote ? <button type="button" className="secondary" onClick={() => void openMappedFolder()}>{text.openFolder}</button> : null}{selected.repository.kind !== 'local' && selectedStatus?.error_code === 'not_checked_out' ? <button type="button" onClick={async () => { try { setBusy(true); if (repo.remote) await app()?.CheckoutRemoteVirtualRepositoryNode?.(repo.id, selected.id); else await app()?.CheckoutVirtualRepositoryNode?.(repo.id, selected.id); await refreshStatus(); } catch (e: any) { setError(errorMessage(e)); } finally { setBusy(false); } }}>{text.checkout}</button> : null}{selected.repository.kind === 'local' ? <button type="button" className="secondary" onClick={() => void loadDirectoryStats()}>{text.calculateSize}</button> : null}</> : null}
                    {selectedStatus?.status ? <pre className="vrepo-status-output">{selectedStatus.status}</pre> : null}
                </section> : <section className="vrepo-placeholder"><h2>{repo.name}</h2><p>.vrepo/manifest.json</p></section>}
            </main>
        </div> : null}

        {mode === 'credentials' ? <section className="vrepo-credentials">
            <div className="vrepo-detail__title"><h2>{text.manageCredentials}</h2><button type="button" className="secondary" onClick={() => { setMode('none'); setDraft({}); }}>{text.cancel}</button></div>
            <div className="vrepo-credential-layout"><div>{credentials.length ? credentials.map((credential) => <div className="vrepo-credential-row" key={credential.id}><div><strong>{credential.name}</strong><span>{credential.kind.toUpperCase()} · {credential.username} · {credential.scope || text.anyHost}</span></div><div><button type="button" className="secondary" disabled={mutationLocked} onClick={() => setDraft(credential)}>{text.edit}</button><button type="button" className="danger" disabled={mutationLocked} onClick={() => void deleteCredential(credential)}>{text.remove}</button></div></div>) : <p>{text.noCredentials}</p>}</div>
                <div className="vrepo-editor"><h3>{draft.id ? text.edit : text.addCredential}</h3><label>{text.credentialName}<input value={draft.name || ''} onChange={(e) => setDraft({ ...draft, name: e.target.value })} /></label><label>{text.type}<select value={draft.kind || 'git'} disabled={!!draft.id} onChange={(e) => setDraft({ ...draft, kind: e.target.value })}><option value="git">Git</option><option value="svn">SVN</option></select></label><label>{text.username}<input value={draft.username || ''} onChange={(e) => setDraft({ ...draft, username: e.target.value })} /></label><label>{text.password}<input type="password" autoComplete="new-password" value={draft.secret || ''} placeholder={draft.id ? text.passwordHint : ''} onChange={(e) => setDraft({ ...draft, secret: e.target.value })} /></label><label>{text.scope}<input value={draft.scope || ''} onChange={(e) => setDraft({ ...draft, scope: e.target.value })} /></label><button type="button" onClick={() => void saveCredential()} disabled={busy}>{text.save}</button></div>
            </div>
        </section> : null}
        {operation ? <div className="vrepo-operation-backdrop" role="presentation"><section ref={operationDialogRef} className="vrepo-operation" role="dialog" aria-modal="true" aria-labelledby="vrepo-operation-title">
            <h2 id="vrepo-operation-title">{operation.action.replace('_', ' & ')}</h2>
            {(operation.action === 'commit' || operation.action === 'commit_push') ? <label>{text.commitMessage}<textarea value={operation.message} onChange={(e) => setOperation({ ...operation, message: e.target.value, preview: undefined })} /></label> : null}
            {!operation.preview ? <button type="button" onClick={() => void loadOperationPreview()} disabled={(operation.action === 'commit' || operation.action === 'commit_push') && !operation.message.trim()}>{text.preview}</button> : <>
                <p>{operation.preview.targets?.length || 0} {text.repositories} · {operation.preview.skipped_local || 0} {text.localSkipped}</p>
                <div className="vrepo-operation__targets">{(operation.preview.targets || []).map((target: any) => <div key={target.node_id}><strong>{target.name}</strong><span>{target.kind.toUpperCase()} · {target.changed ? text.changed : text.clean}{target.error ? ` · ${target.error}` : ''}</span></div>)}</div>
                {operation.action === 'revert' ? <p className="vrepo-operation__danger">{text.revertWarning}</p> : null}
                <button type="button" className={operation.action === 'revert' ? 'danger' : ''} disabled={operation.preview.blocked || busy} onClick={() => void executeOperation()}>{busy ? text.loading : text.execute}</button>
            </>}
			<button type="button" className="secondary" disabled={busy} onClick={() => { operationPreviewRequestRef.current++; setOperation(null); setRetryNodeIds([]); }}>{text.cancel}</button>
        </section></div> : null}
		{operationResult ? <section className="vrepo-operation-result" role="region" aria-live="polite" aria-atomic="false"><div className="vrepo-detail__title"><div><h2>{text.operation}: {operationResult.status}</h2>{operationRunning ? <p>{text.operationRunningHint}</p> : null}</div><div>{operationRunning ? <button type="button" className="danger" disabled={operationCancelPending} onClick={() => void cancelOperation()}>{operationCancelPending ? text.loading : text.cancel}</button> : <button type="button" className="secondary" onClick={() => { operationResultRef.current = null; setOperationResult(null); setOperationCancelPending(false); }}>{text.close}</button>}</div></div>{(operationResult.items || []).map((item: any) => <div key={item.node_id || item.name} className={item.status === 'failed' ? 'is-error' : ''}><strong>{item.name || text.operation}</strong><span>{item.status}{item.error_code ? ` · ${item.error_code}` : ''}{item.error ? ` · ${item.error}` : ''}</span></div>)}{operationResult.status !== 'cancelled' && !operationRunning && operationResult.items?.some((item: any) => item.status === 'failed' && item.node_id) ? <button type="button" onClick={() => { const retry = retryOperationForResult(operationResult); setRetryNodeIds(retry.failed.map((item: any) => item.node_id)); setOperation({ action: retry.action, message: operationResult.message || '' }); operationResultRef.current = null; setOperationResult(null); setOperationCancelPending(false); }}>{text.retryFailed}</button> : null}</section> : null}
    </div>;
}

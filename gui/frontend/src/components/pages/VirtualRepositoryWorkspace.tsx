import { useCallback, useDeferredValue, useEffect, useMemo, useRef, useState } from 'react';
import { BrowserOpenURL, EventsOn } from '../../../wailsjs/runtime';
import { useDialog } from '../CustomDialog';
import type { CodingTaskLaunch as SharedCodingTaskLaunch } from '../ai/codingTaskLaunch';
import './VirtualRepositoryWorkspace.css';

type RepoKind = 'git' | 'svn' | 'local';

type Binding = {
    kind: RepoKind;
    relative_path: string;
	description?: string;
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

const isCredential = (value: unknown): value is Credential => {
	if (!value || typeof value !== 'object') return false;
	const credential = value as Record<string, unknown>;
	return typeof credential.id === 'string' && credential.id.trim().length > 0
		&& typeof credential.name === 'string' && credential.name.trim().length > 0
		&& (credential.kind === 'git' || credential.kind === 'svn')
		&& typeof credential.username === 'string' && credential.username.trim().length > 0
		&& (credential.scope == null || typeof credential.scope === 'string');
};

const parseCredentials = (raw: unknown, label: string): Credential[] => {
	const value = parseRequiredJSON<unknown>(raw, label);
	if (!Array.isArray(value) || value.some((credential) => !isCredential(credential))) {
		throw new Error(`${label} returned invalid credentials`);
	}
	const ids = new Set<string>();
	if (value.some((credential) => {
		if (ids.has(credential.id)) return true;
		ids.add(credential.id);
		return false;
	})) throw new Error(`${label} returned duplicate credential ids`);
	return value;
};

const isNodeStatus = (value: unknown): value is NodeStatus => {
	if (!value || typeof value !== 'object') return false;
	const status = value as Record<string, unknown>;
	return typeof status.node_id === 'string' && status.node_id.trim().length > 0
		&& (status.kind === 'git' || status.kind === 'svn' || status.kind === 'local')
		&& typeof status.exists === 'boolean'
		&& typeof status.is_repository === 'boolean'
		&& typeof status.clean === 'boolean'
		&& ['path', 'branch', 'remote_url', 'status', 'error_code', 'error'].every((key) => status[key] == null || typeof status[key] === 'string');
};

const parseNodeStatuses = (raw: unknown, label: string): NodeStatus[] => {
	const value = parseRequiredJSON<unknown>(raw, label);
	if (!Array.isArray(value) || value.some((status) => !isNodeStatus(status))) {
		throw new Error(`${label} returned invalid statuses`);
	}
	const nodeIDs = new Set<string>();
	if (value.some((status) => {
		if (nodeIDs.has(status.node_id)) return true;
		nodeIDs.add(status.node_id);
		return false;
	})) throw new Error(`${label} returned duplicate node statuses`);
	return value;
};

const statusNeedsAttention = (status: NodeStatus) => Boolean(status.error_code || status.error);

type CodingTaskLaunch = {
    project_path: SharedCodingTaskLaunch['projectPath'];
    task_title: SharedCodingTaskLaunch['taskTitle'];
    agent_mode: NonNullable<SharedCodingTaskLaunch['agentMode']>;
    remote_host?: SharedCodingTaskLaunch['remoteHost'];
};

type RepositoryContextMenu = {
	item: any;
	key: string;
	x: number;
	y: number;
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
// The virtual directory tree is the source of truth for a mapping's checkout
// location. The persisted relative path is derived for backend compatibility.
const relativePathsForTree = (nodes: VRepoNode[]) => {
	const byID = new Map(nodes.map((node) => [node.id, node]));
	const paths = new Map<string, string>();
	const resolving = new Set<string>();
	const resolve = (nodeID: string): string => {
		const cached = paths.get(nodeID);
		if (cached !== undefined) return cached;
		const node = byID.get(nodeID);
		if (!node?.name?.trim() || resolving.has(nodeID)) return '';
		resolving.add(nodeID);
		const parentPath = node.parent_id ? resolve(node.parent_id) : '';
		resolving.delete(nodeID);
		const value = node.parent_id && !parentPath ? '' : parentPath ? `${parentPath}/${node.name.trim()}` : node.name.trim();
		paths.set(nodeID, value);
		return value;
	};
	for (const node of nodes) resolve(node.id);
	return paths;
};

const relativePathForTreeNode = (nodes: VRepoNode[], nodeID: string) => relativePathsForTree(nodes).get(nodeID) || '';

const withTreeDerivedMappingPaths = (repository: VRepo): VRepo => ({
	...repository,
	nodes: (() => {
		const paths = relativePathsForTree(repository.nodes);
		return repository.nodes.map((node) => node.repository ? {
			...node,
				repository: { ...node.repository, relative_path: paths.get(node.id) || '' },
		} : node);
	})(),
});

const treeNodeIcon = (kind?: RepoKind) => {
	if (kind === 'git') return <svg viewBox="0 0 16 16" aria-hidden><path d="M6 3.5 3.5 6 8 10.5l4.5-4.5-2.5-2.5M8 10.5v3M6 3.5v3M8 13.5a1 1 0 1 0 0 2 1 1 0 0 0 0-2M6 5.5a1 1 0 1 0 0 2 1 1 0 0 0 0-2" /></svg>;
	if (kind === 'svn') return <svg viewBox="0 0 16 16" aria-hidden><path d="M3 4.5h6l1.5 1.5H13v6.5H3zM3 4.5v8M5.5 8h5M5.5 10.5h3" /></svg>;
	if (kind === 'local') return <svg viewBox="0 0 16 16" aria-hidden><path d="M2.5 4h4l1.2 1.5h5.8v7H2.5zM2.5 6h11" /></svg>;
	return <svg viewBox="0 0 16 16" aria-hidden><path d="M2.5 4h4l1.2 1.5h5.8v7H2.5z" /></svg>;
};

// Return true when following a prospective parent chain would either reach the
// node itself or encounter a pre-existing cycle in a malformed manifest.
const wouldCreateTreeCycle = (nodes: VRepoNode[], nodeID: string, parentID: string) => {
	const byID = new Map(nodes.map((node) => [node.id, node]));
	const visited = new Set<string>();
	for (let currentID = parentID; currentID;) {
		if (currentID === nodeID || visited.has(currentID)) return true;
		visited.add(currentID);
		currentID = byID.get(currentID)?.parent_id || '';
	}
	return false;
};

export function VirtualRepositoryWorkspace({ isZh, onBack, onOpenCodingTask }: { isZh: boolean; onBack: () => void; onOpenCodingTask?: (launch: CodingTaskLaunch) => void }) {
	const { showConfirm } = useDialog();
    const text = isZh ? {
        title: '虚拟仓库', back: '返回实用工具', newRepo: '新建虚拟仓库', openRepo: '打开已有目录',
        recent: '最近使用', repositoryList: '仓库', searchRepositories: '搜索仓库', repositoryCount: '个仓库', noSearchResults: '没有匹配的虚拟仓库', selectRepository: '选择一个虚拟仓库', selectRepositoryHint: '从左侧打开仓库，查看目录映射、健康状态和操作记录。', localRepository: '本机', remoteRepository: '远程 SSH', mappings: '个映射', health: '健康概览', healthy: '正常', needsAttention: '需处理', pendingStatus: '尚未检查', lastOpened: '最近打开', repositoryActions: '仓库操作',
        empty: '尚未创建虚拟仓库', emptyHint: '选择一个根目录，MaClaw 会在其中创建 .vrepo/manifest.json。',
        name: '名称', root: '根目录', choose: '选择目录', save: '保存', cancel: '取消', refresh: '刷新状态',
        addGroup: '新建虚拟目录', addRepo: '添加映射', credentials: '仓库凭据', svnClient: 'SVN 工具',
	        type: '类型', path: '目录位置', remote: '仓库地址', enabled: '启用', parent: '上级目录', rootNode: '（根）', refType: '版本类型', refName: '分支/标签', defaultRef: '默认分支', checkout: '检出仓库', checkoutAfterSave: '保存后立即检出', notCheckedOut: '尚未检出', branchRef: '分支', tagRef: '标签',
        edit: '编辑', remove: '删除', openFolder: '打开目录', createDir: '目录不存在时创建', clean: '干净', changed: '有变更',
        local: '纯本地目录', loading: '加载中…', noClient: '未找到 SVN 命令行工具', search: '重新搜索', specify: '指定 svn',
        installSVN: '安装说明',
        foundClient: '已找到', credentialName: '凭据名称', username: '用户名', password: '密码或令牌', scope: '主机/Realm（可选）',
        addCredential: '新增凭据', manageCredentials: '凭据管理', noCredentials: '尚无已保存凭据', passwordHint: '编辑时留空表示不修改',
		deleteIndex: '从最近使用中移除', deleteRepository: '删除虚拟仓库', deleteRepositoryConfirm: '确认从 MaClaw 列表中移除“{name}”？\n\n这不会删除 .vrepo 目录或真实文件，但会解除本机凭据绑定和 SSH 密码保存。', manifestNote: '删除这里只移除 MaClaw 索引，不会删除 .vrepo 或真实文件。',
		syncNow: '立即同步', syncing: '正在同步…', syncReady: '自动同步已开启', syncSuccess: '已同步', syncConflict: '同步冲突', syncConflictMessage: '“{name}”同时在本机和另一台设备上被修改。是否保留本机版本？', syncConflictCloud: '是否改为采用 Hub 版本？选择“否”会保留两个版本，并将本机版本另存为副本。', useLocal: '保留本机', useCloud: '采用 Hub', keepCopy: '保留副本', syncUnavailable: '同步需要先连接并注册 Hub。',
        commit: '提交', push: '推送', commitPush: '提交并推送', revert: '还原', execute: '执行', preview: '预览',
        commitMessage: '提交说明', repositories: '个仓库', localSkipped: '个本地目录已跳过', revertWarning: '未提交的已跟踪更改将被丢弃；未跟踪文件会保留。',
        operation: '操作', close: '关闭', retryFailed: '重试失败项', calculateSize: '计算大小', files: '文件数', size: '大小', branch: '分支', status: '状态',
        noCredential: '不使用凭据', anyHost: '任意主机', operationRunningHint: '仓库操作运行期间不能修改虚拟目录树或启动其他操作。',
		location: '位置', localLocation: '本机', remoteLocation: '远程 SSH', editConnection: '编辑连接', startCodingTask: '启动编程任务', startingCodingTask: '正在启动…', server: '服务器', port: '端口', sshUser: 'SSH 用户名', sshPassword: 'SSH 密码', remoteRoot: '远程根目录', testConnection: '测试连接', trustHostKey: '信任并保存主机指纹', hostKeyPrompt: '首次连接，请核对并信任服务器指纹', connected: '连接成功', rootMissingPrompt: 'SSH 已连接，但远程根目录不存在。是否创建该目录？', createRemoteRoot: '创建远程根目录', createRemoteRootConfirm: '确认在远程服务器上创建此根目录？',
		cleanStatus: '仓库干净', changedStatus: '仓库有变更', errorStatus: '仓库状态异常',
    } : {
        title: 'Virtual Repository', back: 'Back to utilities', newRepo: 'New virtual repository', openRepo: 'Open existing root',
        recent: 'Recent', repositoryList: 'Repositories', searchRepositories: 'Search repositories', repositoryCount: 'repositories', noSearchResults: 'No virtual repositories match your search', selectRepository: 'Select a virtual repository', selectRepositoryHint: 'Open a repository from the list to review mappings, health, and operations.', localRepository: 'Local', remoteRepository: 'Remote SSH', mappings: 'mappings', health: 'Health overview', healthy: 'Healthy', needsAttention: 'Needs attention', pendingStatus: 'Not checked', lastOpened: 'Last opened', repositoryActions: 'Repository actions',
        empty: 'No virtual repositories yet', emptyHint: 'Choose a root directory; MaClaw creates .vrepo/manifest.json inside it.',
        name: 'Name', root: 'Root directory', choose: 'Choose', save: 'Save', cancel: 'Cancel', refresh: 'Refresh status',
        addGroup: 'New virtual folder', addRepo: 'Add mapping', credentials: 'Repository credentials', svnClient: 'SVN client',
	        type: 'Type', path: 'Directory location', remote: 'Repository URL', enabled: 'Enabled', parent: 'Parent folder', rootNode: '(root)', refType: 'Version type', refName: 'Branch/tag', defaultRef: 'Default branch', checkout: 'Checkout repository', checkoutAfterSave: 'Checkout after saving', notCheckedOut: 'Not checked out', branchRef: 'Branch', tagRef: 'Tag',
        edit: 'Edit', remove: 'Remove', openFolder: 'Open folder', createDir: 'Create directory if missing', clean: 'Clean', changed: 'Changed',
        local: 'Local directory', loading: 'Loading…', noClient: 'SVN command line client not found', search: 'Search again', specify: 'Choose svn',
        installSVN: 'Installation guide',
        foundClient: 'Found', credentialName: 'Credential name', username: 'Username', password: 'Password or token', scope: 'Host/realm (optional)',
        addCredential: 'Add credential', manageCredentials: 'Credential manager', noCredentials: 'No saved credentials', passwordHint: 'Leave blank while editing to keep the secret',
		deleteIndex: 'Remove from recent', deleteRepository: 'Delete virtual repository', deleteRepositoryConfirm: 'Remove “{name}” from the MaClaw list?\n\nThis does not delete .vrepo or real files, but it removes local credential bindings and the saved SSH password.', manifestNote: 'This only removes the MaClaw index entry; .vrepo and real files remain untouched.',
		syncNow: 'Sync now', syncing: 'Syncing…', syncReady: 'Automatic sync is on', syncSuccess: 'Synced', syncConflict: 'Sync conflict', syncConflictMessage: '“{name}” changed both here and on another device. Keep this computer’s version?', syncConflictCloud: 'Use the Hub version instead? Choosing “No” keeps both by saving this computer’s version as a copy.', useLocal: 'Keep local', useCloud: 'Use Hub', keepCopy: 'Keep copy', syncUnavailable: 'Connect and register with Hub before syncing.',
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
	const [credentialReturnToMapping, setCredentialReturnToMapping] = useState(false);
	const [mappingDraftBeforeCredentials, setMappingDraftBeforeCredentials] = useState<Record<string, unknown> | null>(null);
    const [operation, setOperation] = useState<{ action: 'commit' | 'push' | 'commit_push' | 'revert'; message: string; preview?: any } | null>(null);
    const [operationResult, setOperationResult] = useState<any>(null);
	const [operationCancelPending, setOperationCancelPending] = useState(false);
    const [retryNodeIds, setRetryNodeIds] = useState<string[]>([]);
    const [directoryStats, setDirectoryStats] = useState<any>(null);
    const [expanded, setExpanded] = useState<Record<string, boolean>>({});
    const [remoteConnection, setRemoteConnection] = useState<any>(null);
    const [startingRepositoryID, setStartingRepositoryID] = useState('');
	const [openingRepositoryKey, setOpeningRepositoryKey] = useState('');
	const [repositoryQuery, setRepositoryQuery] = useState('');
	const deferredRepositoryQuery = useDeferredValue(repositoryQuery);
	const [removedRepositoryIDs, setRemovedRepositoryIDs] = useState<Set<string>>(() => new Set());
	const [repositoryContextMenu, setRepositoryContextMenu] = useState<RepositoryContextMenu | null>(null);
	const [syncStatus, setSyncStatus] = useState<'idle' | 'syncing' | 'success' | 'conflict' | 'error'>('idle');
	const [syncMessage, setSyncMessage] = useState('');
	    const [selectingRoot, setSelectingRoot] = useState(false);
	const [draggedNodeID, setDraggedNodeID] = useState('');
	const [dropTargetID, setDropTargetID] = useState<string | null>(null);
	const operationResultRef = useRef<any>(null);
	const operationDialogRef = useRef<HTMLElement | null>(null);
	const busyRef = useRef(false);
	const credentialSaveInFlightRef = useRef(false);
	const credentialDeleteInFlightRef = useRef(false);
	const credentialManagerRequestRef = useRef(0);
	const mappingCredentialRequestRef = useRef(0);
	const credentialBindingsRequestRef = useRef(0);
	const repositoryDeletionRef = useRef(false);
    const codingTaskStartingRef = useRef(false);
	const directoryPickerOpenRef = useRef(false);
	const operationPreviewRequestRef = useRef(0);
	const operationStartingRef = useRef(false);
	const repositorySessionRef = useRef(0);
	const recentRepositoriesRequestRef = useRef(0);
		const directoryStatsRequestRef = useRef(0);
		const remoteConnectionRequestRef = useRef(0);
		const repositoryContextMenuRef = useRef<HTMLDivElement | null>(null);
		const repositoryContextMenuActionRef = useRef<HTMLButtonElement | null>(null);
    const operationRunning = operationResult?.status === 'running';
    const mutationLocked = busy || operationRunning;
    busyRef.current = busy;

	// The recent list can become large on shared workstations. Prepare its search
	// text and mapping count only when the backend data changes, then defer the
	// filtering work so an input event is never held up by a long list render.
	const repositoryRecords = useMemo(() => repos.filter((item) => !removedRepositoryIDs.has(String(item?.id || '').trim())).map((item, index) => ({
		item,
		key: String(item?.id || item?.root_path || `repository-${index}`),
		searchText: [item?.name, item?.root_path, item?.remote?.host, item?.remote?.user]
			.map((value) => String(value || '').toLocaleLowerCase())
			.join('\u0000'),
		mappingCount: Array.isArray(item?.nodes)
			? item.nodes.reduce((count: number, node: VRepoNode) => count + Number(Boolean(node.repository)), 0)
			: 0,
	})), [repos, removedRepositoryIDs]);

	const filteredRepositories = useMemo(() => {
		const query = deferredRepositoryQuery.trim().toLocaleLowerCase();
		return query ? repositoryRecords.filter((record) => record.searchText.includes(query)) : repositoryRecords;
	}, [repositoryRecords, deferredRepositoryQuery]);

	const repositoryList = repositoryRecords.length ? <aside className="vrepo-repository-list" aria-label={text.repositoryList}>
		<div className="vrepo-repository-list__head"><strong>{text.repositoryList}</strong><span aria-live="polite">{filteredRepositories.length} {text.repositoryCount}</span></div>
		<label className="vrepo-repository-list__search"><span className="sr-only">{text.searchRepositories}</span><input value={repositoryQuery} onChange={(event) => setRepositoryQuery(event.target.value)} placeholder={text.searchRepositories} /></label>
		<div className="vrepo-repository-list__items">
			{filteredRepositories.map(({ item, key: itemKey, mappingCount }) => {
				const opening = openingRepositoryKey === itemKey;
				const active = !!repo && (repo.id === item.id || (!repo.id && repo.root_path === item.root_path));
					return <div className="vrepo-repository-list__record" key={itemKey} onContextMenu={(event) => openRepositoryContextMenu(event, item, itemKey)}>
						<button className={`vrepo-repository-list__item${active ? ' is-active' : ''}`} type="button" data-vrepo-repository-key={itemKey} aria-pressed={active} aria-haspopup="menu" aria-expanded={repositoryContextMenu?.key === itemKey} disabled={!!openingRepositoryKey || selectingRoot || mutationLocked} onClick={() => void openRecentRepository(item)} onKeyDown={(event) => {
							if (event.key !== 'ContextMenu' && !(event.shiftKey && event.key === 'F10')) return;
							event.preventDefault();
							const bounds = event.currentTarget.getBoundingClientRect();
							openRepositoryContextMenu(event, item, itemKey, { x: bounds.left + Math.min(bounds.width / 2, 40), y: bounds.top + Math.min(bounds.height, 36) });
						}}>
						<span className="vrepo-repository-list__item-name"><strong>{item.name}</strong><em>{item.remote ? text.remoteRepository : text.localRepository}</em></span>
						<span className="vrepo-repository-list__item-path">{opening ? text.loading : item.remote ? `${item.remote.user}@${item.remote.host}` : item.root_path}</span>
						<span className="vrepo-repository-list__item-meta">{mappingCount} {text.mappings}</span>
					</button>
					<button className="vrepo-repository-list__task utilities-link" type="button" disabled={!!startingRepositoryID || !!openingRepositoryKey || selectingRoot || mutationLocked} onClick={() => void startCodingTask(item)}>{startingRepositoryID === item.id ? text.startingCodingTask : text.startCodingTask}</button>
				</div>;
			})}
			{!filteredRepositories.length ? <p className="vrepo-repository-list__empty" role="status">{text.noSearchResults}</p> : null}
		</div>
	</aside> : null;

	const setActiveRepository = (nextRepository: VRepo, resetTreeState = false) => {
		repositorySessionRef.current += 1;
		if (resetTreeState) {
			setExpanded({});
			setDraggedNodeID('');
			setDropTargetID(null);
		}
		setRepo(nextRepository);
	};

	const repositoryIDsMatch = (left: unknown, right: unknown) => String(left || '').trim() === String(right || '').trim();

	const openRepositoryContextMenu = (event: { preventDefault: () => void; clientX?: number; clientY?: number }, item: any, key: string, position?: { x: number; y: number }) => {
		event.preventDefault();
		if (repositoryDeletionRef.current || mutationLocked || selectingRoot) return;
		const x = position?.x ?? event.clientX ?? 0;
		const y = position?.y ?? event.clientY ?? 0;
		setRepositoryContextMenu({
			item,
			key,
			x: Math.max(8, Math.min(x, window.innerWidth - 210)),
			y: Math.max(8, Math.min(y, window.innerHeight - 56)),
		});
	};

	const closeRepositoryContextMenu = (restoreFocus = false) => {
		const key = repositoryContextMenu?.key;
		setRepositoryContextMenu(null);
		if (restoreFocus && key) {
			window.requestAnimationFrame(() => {
				Array.from(document.querySelectorAll<HTMLElement>('[data-vrepo-repository-key]'))
					.find((item) => item.dataset.vrepoRepositoryKey === key)
					?.focus();
			});
		}
	};

	const loadRecent = useCallback(async () => {
		const backend = app();
		if (!backend?.ListVirtualRepositories) return;
		const requestID = ++recentRepositoriesRequestRef.current;
		try {
			const recent = parseJSON(await backend.ListVirtualRepositories(), []);
			if (requestID === recentRepositoriesRequestRef.current) {
				setRepos(recent);
				// A deleted entry stays hidden while a lagging index read can still
				// return it. Once a read confirms it is gone, release the temporary
				// tombstone so reopening the same root can appear in the future.
				const returnedIDs = new Set(recent.map((item: any) => String(item?.id || '').trim()).filter(Boolean));
				setRemovedRepositoryIDs((current) => {
					const pending = Array.from(current).filter((id) => returnedIDs.has(id));
					return pending.length === current.size ? current : new Set(pending);
				});
			}
		} catch (e: any) {
			if (requestID === recentRepositoriesRequestRef.current) setError(String(e?.message || e));
		}
	}, []);

	const runRepositorySync = async (initialResolutions: Record<string, string> = {}) => {
		if (syncStatus === 'syncing') return;
		const backend = app();
		if (!backend?.SyncVirtualRepositories) {
			setSyncStatus('error');
			setSyncMessage(text.syncUnavailable);
			return;
		}
		setSyncStatus('syncing');
		setSyncMessage('');
		try {
			let resolutions = initialResolutions;
			for (;;) {
				const result = parseRequiredJSON<any>(await backend.SyncVirtualRepositories(JSON.stringify(resolutions)), 'Virtual repository sync');
				if (result?.status === 'success') {
					setSyncStatus('success');
					setSyncMessage(result?.last_synced_at ? `${text.syncSuccess} · ${new Date(result.last_synced_at).toLocaleString()}` : text.syncSuccess);
					await loadRecent();
					return;
				}
				if (result?.status !== 'conflict') throw new Error(result?.message || 'Virtual repository sync returned an invalid status');
				const conflicts = Array.isArray(result.conflicts) ? result.conflicts : [];
				if (!conflicts.length) {
					setSyncStatus('conflict');
					setSyncMessage(result?.message || text.syncConflict);
					return;
				}
				const next = { ...resolutions };
				for (const conflict of conflicts) {
					const name = String(conflict?.name || conflict?.id || text.syncConflict);
					const keepLocal = await showConfirm(text.syncConflictMessage.replace('{name}', name), text.syncConflict, { confirmText: text.useLocal, cancelText: text.useCloud, confirmVariant: 'danger' });
					if (keepLocal) { next[String(conflict.id)] = 'local'; continue; }
					if (String(conflict.kind) === 'repository') {
						const useCloud = await showConfirm(text.syncConflictCloud, text.syncConflict, { confirmText: text.useCloud, cancelText: text.keepCopy });
						next[String(conflict.id)] = useCloud ? 'cloud' : 'copy';
					} else {
						next[String(conflict.id)] = 'cloud';
					}
				}
				resolutions = next;
			}
		} catch (e: any) {
			setSyncStatus('error');
			setSyncMessage(errorMessage(e));
		}
	};

	const deleteRecentRepository = async (item: any) => {
		if (repositoryDeletionRef.current || busyRef.current || operationResultRef.current?.status === 'running') return;
		const id = String(item?.id || '').trim();
		closeRepositoryContextMenu();
		if (!id) {
			setError(isZh ? '该虚拟仓库缺少标识，无法从列表中删除。' : 'This virtual repository has no identifier and cannot be removed from the list.');
			return;
		}
		repositoryDeletionRef.current = true;
		const name = String(item?.name || id);
		const confirmed = await showConfirm(text.deleteRepositoryConfirm.replace('{name}', name), text.deleteRepository, {
			confirmText: text.remove,
			cancelText: text.cancel,
			confirmVariant: 'danger',
		});
		if (!confirmed || busyRef.current || operationResultRef.current?.status === 'running') {
			repositoryDeletionRef.current = false;
			return;
		}
		busyRef.current = true;
		setBusy(true);
		setError('');
		try {
			const backend = app();
			if (!backend?.DeleteVirtualRepository) {
				throw new Error(isZh ? '当前版本不支持删除虚拟仓库。' : 'Deleting virtual repositories is not supported by this version.');
			}
			await backend.DeleteVirtualRepository(id);
			setRemovedRepositoryIDs((current) => new Set(current).add(id));
			const deletingActiveRepository = repositoryIDsMatch(repo?.id, id);
			if (deletingActiveRepository) {
				repositorySessionRef.current += 1;
				setRepo(null);
				setSelectedId('');
				setStatuses({});
				setDirectoryStats(null);
				setCredentialBindings({});
			}
			setRepos((current) => current.filter((candidate) => !repositoryIDsMatch(candidate?.id, id)));
			void loadRecent();
		} catch (e: any) {
			setError(errorMessage(e));
		} finally {
			repositoryDeletionRef.current = false;
			busyRef.current = false;
			setBusy(false);
		}
	};

	useEffect(() => {
		if (!repositoryContextMenu) return;
		const focusID = window.setTimeout(() => repositoryContextMenuActionRef.current?.focus(), 0);
		const dismiss = (event: MouseEvent) => {
			if (!repositoryContextMenuRef.current?.contains(event.target as Node)) closeRepositoryContextMenu();
		};
		const dismissOnEscape = (event: KeyboardEvent) => {
			if (event.key === 'Escape') {
				event.preventDefault();
				closeRepositoryContextMenu(true);
			}
		};
		window.addEventListener('mousedown', dismiss, true);
		window.addEventListener('keydown', dismissOnEscape, true);
		return () => {
			window.clearTimeout(focusID);
			window.removeEventListener('mousedown', dismiss, true);
			window.removeEventListener('keydown', dismissOnEscape, true);
		};
	}, [repositoryContextMenu]);

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
		if (!await showConfirm(text.createRemoteRootConfirm, text.createRemoteRoot, { confirmText: text.createRemoteRoot, cancelText: text.cancel })) return;
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
			setActiveRepository(opened, true); setSelectedId(''); setStatuses({}); setDirectoryStats(null); await loadRecent();
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
        const isCurrent = !!repo && (repo.id === item?.id || (!repo.id && repo.root_path === item?.root_path));
        if (!key || openingRepositoryKey || mutationLocked || isCurrent) return;
        setOpeningRepositoryKey(key);
        setError('');
        try {
            const opened = parseRequiredJSON<VRepo>(item.remote
                ? await app()?.OpenRemoteVirtualRepository?.(item.id)
                : await app()?.OpenVirtualRepository?.(item.root_path), 'Open virtual repository');
			setActiveRepository(opened, true);
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
			const normalizedRepo = withTreeDerivedMappingPaths(nextRepo);
			const saved = normalizedRepo.remote
				? parseRequiredJSON<VRepo>(await backend.SaveRemoteVirtualRepository(JSON.stringify({ repository: normalizedRepo, password: draft.ssh_password || '', trust_host_key: !!draft.trust_host_key })), 'Save remote virtual repository')
				: parseRequiredJSON<VRepo>(await backend.SaveVirtualRepository(JSON.stringify(normalizedRepo)), 'Save virtual repository');
			setActiveRepository(saved); setMode('none'); setDraft({}); await loadRecent();
        } catch (e: any) { setError(String(e?.message || e)); } finally { setBusy(false); }
    };

	const inspectRepositoryStatuses = useCallback(async (targetRepository: VRepo, repositorySession = repositorySessionRef.current) => {
		const backend = app();
		const inspect = targetRepository.remote ? backend?.InspectRemoteVirtualRepository : backend?.InspectVirtualRepository;
		if (typeof inspect !== 'function') throw new Error(isZh ? '当前版本不支持检查虚拟仓库状态。' : 'This version does not support virtual repository status checks.');
		if (targetRepository.remote && !targetRepository.id) throw new Error(isZh ? '远程虚拟仓库 ID 缺失。' : 'Remote virtual repository id is missing.');
		const raw = targetRepository.remote
			? await inspect(targetRepository.id)
			: await inspect(targetRepository.root_path);
		const list = parseNodeStatuses(raw, 'Inspect virtual repository');
		const mappingKinds = new Map(targetRepository.nodes
			.filter((node) => !!node.repository)
			.map((node) => [node.id, node.repository!.kind]));
		if (list.some((status) => mappingKinds.get(status.node_id) !== status.kind)) {
			throw new Error('Inspect virtual repository returned a status for an unknown mapping');
		}
		const nextStatuses = Object.fromEntries(list.map((item) => [item.node_id, item]));
		if (repositorySession === repositorySessionRef.current) setStatuses(nextStatuses);
		return nextStatuses;
	}, []);

	const refreshStatus = useCallback(async () => {
		if (!repo) return;
		const repositorySession = repositorySessionRef.current;
		setBusy(true); setError('');
		try {
			await inspectRepositoryStatuses(repo, repositorySession);
		} catch (e: any) {
			if (repositorySession === repositorySessionRef.current) setError(String(e?.message || e));
		} finally {
			if (repositorySession === repositorySessionRef.current) setBusy(false);
		}
	}, [repo, inspectRepositoryStatuses]);

	const checkoutRepositoryNode = async (targetRepository: VRepo, targetNodeID: string) => {
		if (!targetRepository.id) throw new Error(isZh ? '虚拟仓库 ID 缺失，无法检出。' : 'The virtual repository id is missing, so checkout cannot start.');
		const checkout = targetRepository.remote ? app()?.CheckoutRemoteVirtualRepositoryNode : app()?.CheckoutVirtualRepositoryNode;
		if (typeof checkout !== 'function') throw new Error(isZh ? '当前版本不支持检出虚拟仓库。' : 'This version does not support virtual repository checkout.');
		await checkout(targetRepository.id, targetNodeID);
	};

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
        const parentID = String(draft.parent_id || '').trim();
		if (mode === 'mapping' && !String(draft.name || '').trim()) {
			setError(isZh ? '映射目录名称不能为空。' : 'A mapping directory name is required.');
			return;
		}
        const parent = parentID ? repo.nodes.find((item) => item.id === parentID) : undefined;
        if (parentID && (!parent || parent.repository || parentID === id)) {
            setError(isZh ? '请选择有效的虚拟目录作为上级目录。' : 'Choose a valid virtual folder as the parent.');
            return;
        }
        if (parentID && wouldCreateTreeCycle(repo.nodes, id, parentID)) {
            setError(isZh ? '不能将目录移动到自身或其子目录中。' : 'A folder cannot be placed inside itself or one of its descendants.');
            return;
        }
        const node: VRepoNode = {
            id, parent_id: parentID || undefined, name: String(draft.name || '').trim(), order: Number(draft.order || repo.nodes.length * 10 + 10),
            repository: mode === 'mapping' ? {
				kind: draft.kind || 'git', relative_path: '', description: String(draft.description || '').trim() || undefined,
                remote_url: draft.kind === 'local' ? undefined : String(draft.remote_url || '').trim(),
				ref_type: draft.kind === 'local' || !draft.ref_name ? undefined : (draft.ref_type || 'branch'),
				ref_name: draft.kind === 'local' ? undefined : String(draft.ref_name || '').trim(), enabled: draft.enabled !== false,
            } : undefined,
	        };
	        setBusy(true); setError('');
	        try {
	            const nodes = repo.nodes.some((item) => item.id === id) ? repo.nodes.map((item) => item.id === id ? node : item) : [...repo.nodes, node];
	            const savePayload = withTreeDerivedMappingPaths({ ...repo, nodes });
			const saved = repo.remote ? parseRequiredJSON<VRepo>(await app()?.SaveRemoteVirtualRepository?.(JSON.stringify({ repository: savePayload })), 'Save remote virtual repository') : parseRequiredJSON<VRepo>(await app()?.SaveVirtualRepository?.(JSON.stringify(savePayload)), 'Save virtual repository');
			let directoryCreationError: unknown;
			if (mode === 'mapping' && draft.create_directory) {
				try {
					const mappedPath = saved.nodes.find((item) => item.id === id)?.repository?.relative_path || '';
					if (saved.remote) await app()?.CreateRemoteVirtualRepositoryDirectory?.(saved.id, mappedPath);
					else await app()?.CreateVirtualRepositoryDirectory?.(saved.root_path, mappedPath);
				} catch (creationError) {
					directoryCreationError = creationError;
				}
			}
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
			const savedRepositorySession = repositorySessionRef.current;
			setSelectedId(id);
			if (mode === 'mapping') {
				try {
					await inspectRepositoryStatuses(saved, savedRepositorySession);
				} catch (inspectError) {
					// An inspection failure is distinct from an unchecked-out repository
					// or a missing local directory. Keep that diagnostic visible so users
					// can take the appropriate next action without losing the mapping.
					if (savedRepositorySession === repositorySessionRef.current) {
						setStatuses((current) => ({ ...current, [id]: {
							node_id: id, kind: draft.kind || 'git', exists: false, is_repository: false,
							clean: true, error_code: 'inspection_failed', error: errorMessage(inspectError),
						} }));
					}
				}
				if (draft.kind !== 'local' && draft.enabled !== false && draft.checkout_after_save) {
					try {
						await checkoutRepositoryNode(saved, id);
					} catch (checkoutError) {
						if (savedRepositorySession === repositorySessionRef.current) {
							setError(isZh
								? `映射已保存，但检出失败：${errorMessage(checkoutError)}`
								: `The mapping was saved, but checkout failed: ${errorMessage(checkoutError)}`);
						}
						return;
					}
					try {
						await inspectRepositoryStatuses(saved, savedRepositorySession);
					} catch (inspectError) {
						if (savedRepositorySession === repositorySessionRef.current) {
							setError(isZh
								? `映射已保存，检出已完成，但状态刷新失败：${errorMessage(inspectError)}`
								: `The mapping was saved and checkout completed, but the status refresh failed: ${errorMessage(inspectError)}`);
						}
					}
				}
			}
			if (directoryCreationError) {
				throw new Error(isZh
					? `映射已保存，但目录创建失败：${errorMessage(directoryCreationError)}`
					: `The mapping was saved, but its directory could not be created: ${errorMessage(directoryCreationError)}`);
			}
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
        if (!await showConfirm(isZh ? `确认从虚拟目录树移除“${label}”及其子项？不会删除真实文件。` : `Remove “${label}” and its children from the virtual tree? Real files will not be deleted.`, text.remove, { confirmText: text.remove, cancelText: text.cancel, confirmVariant: 'danger' })) return;
        await saveRepo({ ...repo, nodes: repo.nodes.filter((node) => !descendants.has(node.id)) });
        setSelectedId('');
    };

    const openMappedFolder = async () => {
        if (!repo || !selected?.repository) return;
        const path = selectedStatus?.path || `${repo.root_path.replace(/[\\/]$/, '')}/${selected.repository.relative_path || relativePathForTreeNode(repo.nodes, selected.id)}`;
        try { await app()?.OpenFileOrShowInFolder?.(path); } catch (e: any) { setError(String(e?.message || e)); }
    };

		const nodesByParent = useMemo(() => {
        const result = new Map<string, VRepoNode[]>();
        const nodes = repo?.nodes || [];
        const nodesByID = new Map(nodes.map((node) => [node.id, node]));
        for (const node of repo?.nodes || []) {
            // Old manifests can retain a missing parent or a cyclic parent chain after
            // an interrupted edit. Make those entries visible at the root instead of
            // allowing a malformed relation to hide them from the tree.
            let parentID = node.parent_id || '';
            const ancestors = new Set<string>([node.id]);
            let parentChainIsValid = true;
            while (parentID) {
                if (ancestors.has(parentID) || !nodesByID.has(parentID)) {
                    parentChainIsValid = false;
                    break;
                }
                ancestors.add(parentID);
                parentID = nodesByID.get(parentID)?.parent_id || '';
            }
            const directParent = node.parent_id ? nodesByID.get(node.parent_id) : undefined;
            parentID = parentChainIsValid && directParent && !directParent.repository ? directParent.id : '';
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

    const displayParentByNodeID = useMemo(() => {
        const parents = new Map<string, string>();
        for (const [parentID, children] of nodesByParent) {
            for (const child of children) parents.set(child.id, parentID);
        }
        return parents;
    }, [nodesByParent]);

    const focusTreeItem = (id: string) => {
        window.requestAnimationFrame(() => {
            const item = Array.from(document.querySelectorAll<HTMLElement>('[data-vrepo-node-id]'))
                .find((candidate) => candidate.dataset.vrepoNodeId === id);
            item?.focus();
        });
    };

    const focusTreeRoot = () => {
        window.requestAnimationFrame(() => document.querySelector<HTMLElement>('[data-vrepo-tree-root]')?.focus());
    };

    const toggleTreeNode = (nodeID: string, isExpanded: boolean) => {
        if (isExpanded) {
            const descendants = new Set<string>();
            const visit = (parentID: string) => {
                for (const child of nodesByParent.get(parentID) || []) {
                    descendants.add(child.id);
                    visit(child.id);
                }
            };
            visit(nodeID);
            if (descendants.has(selectedId)) {
                setSelectedId(nodeID);
                focusTreeItem(nodeID);
            }
        }
        setExpanded((current) => ({ ...current, [nodeID]: !isExpanded }));
    };

	const canMoveTreeNode = (nodeID: string, parentID: string) => {
		if (!repo || mutationLocked || !nodeID || nodeID === parentID) return false;
		const moving = repo.nodes.find((node) => node.id === nodeID);
		const parent = parentID ? repo.nodes.find((node) => node.id === parentID) : undefined;
		if (!moving || (parentID && (!parent || parent.repository))) return false;
		if (parentID && wouldCreateTreeCycle(repo.nodes, nodeID, parentID)) return false;
		return (moving.parent_id || '') !== parentID;
	};

	const moveTreeNode = async (nodeID: string, parentID: string) => {
		if (!repo || mutationLocked || nodeID === parentID) return;
		const moving = repo.nodes.find((node) => node.id === nodeID);
		const parent = parentID ? repo.nodes.find((node) => node.id === parentID) : undefined;
		if (!moving || (parentID && (!parent || parent.repository))) return;
		if (parentID && wouldCreateTreeCycle(repo.nodes, nodeID, parentID)) return;
		if (!canMoveTreeNode(nodeID, parentID)) return;
		const siblings = repo.nodes.filter((node) => (node.parent_id || '') === parentID && node.id !== nodeID);
		const nextOrder = siblings.reduce((maximum, node) => Math.max(maximum, node.order || 0), 0) + 10;
		const nextRepo = withTreeDerivedMappingPaths({ ...repo, nodes: repo.nodes.map((node) => node.id === nodeID ? { ...node, parent_id: parentID || undefined, order: nextOrder } : node) });
		setBusy(true); setError('');
		try {
			const saved = repo.remote
				? parseRequiredJSON<VRepo>(await app()?.SaveRemoteVirtualRepository?.(JSON.stringify({ repository: nextRepo })), 'Save remote virtual repository')
				: parseRequiredJSON<VRepo>(await app()?.SaveVirtualRepository?.(JSON.stringify(nextRepo)), 'Save virtual repository');
			setActiveRepository(saved);
			setSelectedId(nodeID);
			if (parentID) setExpanded((current) => ({ ...current, [parentID]: true }));
			await loadRecent();
		} catch (e: any) { setError(errorMessage(e)); }
		finally { setBusy(false); setDraggedNodeID(''); setDropTargetID(null); }
	};

    const renderNodes = (parentId = '', depth = 0, ancestorHasNext: boolean[] = []): React.ReactNode => (nodesByParent.get(parentId) || []).map((node, index, siblings) => {
        const status = statuses[node.id];
        const hasChildren = (nodesByParent.get(node.id)?.length || 0) > 0;
        const isExpanded = expanded[node.id] !== false;
        const isLast = index === siblings.length - 1;
		const nodeLabel = node.repository ? `${node.name} · ${node.repository.kind.toUpperCase()}` : node.name;
        return <div key={node.id} className="vrepo-tree__branch" role="none">
	            <div role="treeitem" aria-label={nodeLabel} draggable={!mutationLocked} data-vrepo-node-id={node.id} tabIndex={selectedId === node.id ? 0 : -1} aria-level={depth + 1} aria-selected={selectedId === node.id} aria-expanded={hasChildren ? isExpanded : undefined} className={`vrepo-tree__item${selectedId === node.id ? ' is-selected' : ''}${draggedNodeID === node.id ? ' is-dragging' : ''}${dropTargetID === node.id ? ' is-drop-target' : ''}`} onDragStart={(event) => { if (mutationLocked) { event.preventDefault(); return; } setDraggedNodeID(node.id); event.dataTransfer.effectAllowed = 'move'; event.dataTransfer.setData('text/plain', node.id); }} onDragEnd={() => { setDraggedNodeID(''); setDropTargetID(null); }} onDragOver={(event) => { if (!node.repository && canMoveTreeNode(draggedNodeID, node.id)) { event.preventDefault(); event.dataTransfer.dropEffect = 'move'; setDropTargetID(node.id); } }} onDragLeave={(event) => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setDropTargetID((current) => current === node.id ? null : current); }} onDrop={(event) => { event.preventDefault(); const sourceID = draggedNodeID || event.dataTransfer.getData('text/plain'); void moveTreeNode(sourceID, node.id); }} onClick={(event) => { event.currentTarget.focus(); setSelectedId(node.id); }} onKeyDown={(event) => {
                const index = visibleNodeIDs.indexOf(node.id);
                if (event.key === 'ArrowDown' || event.key === 'ArrowUp' || event.key === 'Home' || event.key === 'End') {
                    event.preventDefault();
					if (event.key === 'Home' || (event.key === 'ArrowUp' && index === 0)) { setSelectedId(''); focusTreeRoot(); return; }
					const nextIndex = event.key === 'End' ? visibleNodeIDs.length - 1 : Math.max(0, Math.min(visibleNodeIDs.length - 1, index + (event.key === 'ArrowDown' ? 1 : -1)));
                    const nextID = visibleNodeIDs[nextIndex];
                    if (nextID) { setSelectedId(nextID); focusTreeItem(nextID); }
                    return;
                }
                if ((event.key === 'Enter' || event.key === ' ') && hasChildren) {
                    event.preventDefault();
                    toggleTreeNode(node.id, isExpanded);
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
                        event.preventDefault(); toggleTreeNode(node.id, isExpanded);
                    } else {
                        const displayParentID = displayParentByNodeID.get(node.id);
                        if (!displayParentID) { event.preventDefault(); setSelectedId(''); focusTreeRoot(); return; }
                        event.preventDefault(); setSelectedId(displayParentID); focusTreeItem(displayParentID);
                    }
                }
	            }}>
                    <span className="vrepo-tree__guides" aria-hidden>{ancestorHasNext.map((hasNext, guideIndex) => <span key={guideIndex} className={hasNext ? 'is-active' : ''} />)}</span>
					{depth > 1 ? <span className={`vrepo-tree__elbow${isLast ? ' is-last' : ''}`} aria-hidden /> : null}
			{hasChildren ? <button type="button" tabIndex={-1} className="vrepo-tree__chevron" aria-label={`${isExpanded ? (isZh ? '折叠' : 'Collapse') : (isZh ? '展开' : 'Expand')} ${node.name}`} aria-expanded={isExpanded} onClick={(event) => { event.stopPropagation(); toggleTreeNode(node.id, isExpanded); }}>{isExpanded ? '▾' : '▸'}</button> : <span className="vrepo-tree__chevron" aria-hidden />}
                    <span className={`vrepo-tree__icon${node.repository ? ` is-${node.repository.kind}` : ''}`}>{treeNodeIcon(node.repository?.kind)}</span>
                    <span className={`vrepo-tree__label${selectedId === node.id ? ' is-selected' : ''}${draggedNodeID === node.id ? ' is-dragging' : ''}${dropTargetID === node.id ? ' is-drop-target' : ''}`} title={node.repository?.description || undefined}>{node.name}</span>
				{status ? <span className={`vrepo-tree__state ${status.error ? 'is-error' : status.clean ? 'is-clean' : 'is-dirty'}`} role="img" aria-label={status.error ? `${text.errorStatus}: ${status.error}` : status.clean ? text.cleanStatus : text.changedStatus}>{status.error ? '!' : status.clean ? '✓' : '●'}</span> : null}
	            </div>
		{isExpanded ? renderNodes(node.id, depth + 1, [...ancestorHasNext, depth > 0 && !isLast]) : null}
        </div>;
    });

    const selected = repo?.nodes.find((node) => node.id === selectedId);
    const selectedStatus = selected ? statuses[selected.id] : undefined;

    const loadCredentialBindings = useCallback(async (repositoryId?: string) => {
        const requestID = ++credentialBindingsRequestRef.current;
        if (!repositoryId) { setCredentialBindings({}); return; }
        try {
            const nextBindings = parseJSON(await app()?.ListRepositoryCredentialBindings?.(repositoryId), {});
            if (requestID === credentialBindingsRequestRef.current) setCredentialBindings(nextBindings);
        } catch {
            if (requestID === credentialBindingsRequestRef.current) setCredentialBindings({});
        }
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
			const relativePath = relativePathForTreeNode(repo.nodes, selected.id);
			const raw = repo.remote
				? await app()?.GetRemoteVirtualRepositoryDirectoryStats?.(repo.id, relativePath)
				: await app()?.GetVirtualRepositoryDirectoryStats?.(repo.root_path, relativePath);
			if (requestID === directoryStatsRequestRef.current && repositorySession === repositorySessionRef.current && selectedNodeID === selectedId) {
				setDirectoryStats(parseJSON(raw, null));
			}
		} catch (e: any) {
			if (requestID === directoryStatsRequestRef.current && repositorySession === repositorySessionRef.current && selectedNodeID === selectedId) {
				setError(String(e?.message || e));
			}
		}
	};

	const startCredentialManager = async (returnToMapping = false) => {
		mappingCredentialRequestRef.current += 1;
		const requestID = ++credentialManagerRequestRef.current;
		const mappingDraft = returnToMapping ? { ...draft } : null;
		setCredentialReturnToMapping(returnToMapping);
		setMappingDraftBeforeCredentials(mappingDraft);
		setMode('credentials');
		// A failed refresh must not expose credentials from a prior manager or
		// mapping session as if they were current machine-local state.
		setCredentials([]);
		// The credential form is a separate draft. Preserve the mapping's selected
		// repository kind when it launched this flow; otherwise the select can show
		// its Git fallback while the request serializes no kind at all.
		setDraft({ kind: mappingDraft?.kind === 'svn' ? 'svn' : 'git' });
		setError('');
		try {
			const nextCredentials = parseCredentials(await app()?.ListRepositoryCredentials?.(''), 'List repository credentials');
			if (requestID === credentialManagerRequestRef.current) setCredentials(nextCredentials);
		} catch (e: any) {
			if (requestID === credentialManagerRequestRef.current) setError(String(e?.message || e));
		}
    };

	const loadMappingCredentials = async (kind: RepoKind) => {
		const requestID = ++mappingCredentialRequestRef.current;
		try {
			const nextCredentials = parseCredentials(await app()?.ListRepositoryCredentials?.(kind), 'List repository credentials');
			if (requestID === mappingCredentialRequestRef.current) setCredentials(nextCredentials);
		} catch (e: any) {
			if (requestID === mappingCredentialRequestRef.current) setError(String(e?.message || e));
		}
	};

	const deleteCredential = async (credential: Credential) => {
		if (credentialDeleteInFlightRef.current) return;
		if (!await showConfirm(isZh ? `确认删除凭据“${credential.name}”？所有仓库绑定将同时解除。` : `Delete credential “${credential.name}”? All repository bindings to it will be removed.`, text.remove, { confirmText: text.remove, cancelText: text.cancel, confirmVariant: 'danger' })) return;
		if (credentialDeleteInFlightRef.current) return;
		credentialDeleteInFlightRef.current = true;
		// A list request started before this mutation may resolve afterwards with a
		// stale snapshot. Invalidate it before applying the authoritative result.
		credentialManagerRequestRef.current += 1;
		mappingCredentialRequestRef.current += 1;
		setBusy(true); setError('');
		try {
			await app()?.DeleteRepositoryCredential?.(credential.id);
			// The delete call is authoritative. Do not turn a successful deletion into
			// an apparent failure merely because the follow-up list read is unavailable.
			setCredentials((current) => current.filter((item) => item.id !== credential.id));
			if (repo?.id) await loadCredentialBindings(repo.id);
			if (draft.id === credential.id) setDraft({ kind: credential.kind });
		} catch (e: any) { setError(String(e?.message || e)); } finally { credentialDeleteInFlightRef.current = false; setBusy(false); }
	};

    useEffect(() => { void loadCredentialBindings(repo?.id); }, [repo?.id, loadCredentialBindings]);

	const saveCredential = async () => {
		if (credentialSaveInFlightRef.current) return;
		credentialSaveInFlightRef.current = true;
		// Preserve the metadata returned by SaveRepositoryCredential instead of
		// allowing an older ListRepositoryCredentials response to overwrite it.
		credentialManagerRequestRef.current += 1;
		mappingCredentialRequestRef.current += 1;
		setBusy(true); setError('');
		try {
			// Keep the serialized value aligned with the select's visual fallback.
			// This also protects a newly cleared credential draft from omitting kind.
			const kind = draft.kind === 'svn' ? 'svn' : 'git';
			const saved = parseRequiredJSON<unknown>(await app()?.SaveRepositoryCredential?.(JSON.stringify({ ...draft, kind })), 'Save repository credential');
			if (!isCredential(saved)) throw new Error('Save repository credential returned invalid metadata');
			// Saving returns the full metadata needed by this view, so update the local
			// list directly. A separate list read can fail or briefly return stale data.
			setCredentials((current) => [saved, ...current.filter((item) => item.id !== saved.id)]);
			if (credentialReturnToMapping) {
				setMode('mapping');
				setDraft({ ...(mappingDraftBeforeCredentials || {}), credential_id: saved.id });
				setCredentialReturnToMapping(false);
				setMappingDraftBeforeCredentials(null);
			} else setDraft({ kind });
		} catch (e: any) { setError(String(e?.message || e)); } finally { credentialSaveInFlightRef.current = false; setBusy(false); }
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
            {mode === 'none' && (repo || repos.length > 0) ? <div className="vrepo-actions">
				<div className="vrepo-actions__setup">
					<button type="button" className="secondary" disabled={mutationLocked || syncStatus === 'syncing'} onClick={() => void runRepositorySync()}>{syncStatus === 'syncing' ? text.syncing : text.syncNow}</button>
					<button type="button" className="secondary" disabled={mutationLocked || selectingRoot} onClick={() => void openExisting()}>{selectingRoot ? text.loading : text.openRepo}</button>
                    <button type="button" disabled={mutationLocked || selectingRoot} onClick={beginNewRepository}>{text.newRepo}</button>
                    {repo?.remote ? <button type="button" className="secondary" disabled={mutationLocked} onClick={() => { setMode('repo'); setRemoteConnection(null); setDraft({ ...repo, location: 'remote', ssh_host: repo.remote?.host || '', ssh_port: repo.remote?.port || 22, ssh_user: repo.remote?.user || '', ssh_password: '' }); }}>{text.editConnection}</button> : null}
                    {repo ? <button type="button" className="secondary" disabled={mutationLocked || !!startingRepositoryID} onClick={() => void startCodingTask(repo)}>{startingRepositoryID === repo.id ? text.startingCodingTask : text.startCodingTask}</button> : null}
                </div>
                {repo ? <div className="vrepo-actions__operation">
                    <button type="button" className="secondary" disabled={mutationLocked} onClick={() => void refreshStatus()}>{text.refresh}</button>
                    <button type="button" className="secondary" disabled={mutationLocked} onClick={() => void previewOperation('commit')}>{text.commit}</button>
                    <button type="button" className="secondary" disabled={mutationLocked} onClick={() => void previewOperation('push')}>{text.push}</button>
                    <button type="button" disabled={mutationLocked} onClick={() => void previewOperation('commit_push')}>{text.commitPush}</button>
                    <button type="button" className="danger" disabled={mutationLocked} onClick={() => void previewOperation('revert')}>{text.revert}</button>
                    <button type="button" className="secondary" disabled={mutationLocked} onClick={() => void startCredentialManager()}>{text.credentials}</button>
                </div> : null}
            </div> : null}
		</header>
		{syncStatus !== 'idle' ? <p className={`vrepo-sync-status is-${syncStatus}`} role={syncStatus === 'error' ? 'alert' : 'status'}>{syncStatus === 'syncing' ? text.syncing : syncMessage || text.syncReady}</p> : null}
		{error ? <p className="utilities-error" role="alert">{error}</p> : null}
		{repositoryContextMenu ? <div ref={repositoryContextMenuRef} className="vrepo-repository-menu" role="menu" aria-label={repositoryContextMenu.item?.name || text.repositoryList} style={{ left: repositoryContextMenu.x, top: repositoryContextMenu.y }}>
			<button ref={repositoryContextMenuActionRef} type="button" role="menuitem" className="danger" disabled={mutationLocked} onClick={() => void deleteRecentRepository(repositoryContextMenu.item)}>{text.deleteRepository}</button>
		</div> : null}
        <details className="vrepo-client-status">
            <summary><strong>{text.svnClient}</strong><span className={svn?.available ? 'is-ready' : 'is-missing'}>{svn?.available ? text.foundClient : text.noClient}</span></summary>
            <div className="vrepo-client-status__body">
                {svn?.available ? <span>{svn.version} · <code>{svn.executable}</code> ({svn.source})</span> : null}
                <button type="button" className="utilities-link" disabled={mutationLocked} onClick={() => void loadSVN(true)}>{text.search}</button>
                <button type="button" className="utilities-link" disabled={mutationLocked} onClick={() => void specifySVN()}>{text.specify}</button>
                {!svn?.available ? <button type="button" className="utilities-link" onClick={() => { try { BrowserOpenURL('https://subversion.apache.org/packages.html'); } catch { window.open('https://subversion.apache.org/packages.html', '_blank', 'noopener'); } }}>{text.installSVN}</button> : null}
            </div>
        </details>

        {!repos.length && !repo && mode !== 'repo' ? <section className="vrepo-empty">
            <div className="vrepo-empty__intro">
                <span className="vrepo-empty__mark" aria-hidden="true">V</span>
                <div><h2>{text.empty}</h2><p>{text.emptyHint}</p></div>
            </div>
			<div className="vrepo-empty__actions">
				<button type="button" className="secondary" disabled={mutationLocked || syncStatus === 'syncing'} onClick={() => void runRepositorySync()}>{syncStatus === 'syncing' ? text.syncing : text.syncNow}</button>
				<button type="button" disabled={mutationLocked || selectingRoot} onClick={beginNewRepository}>{text.newRepo}</button>
                <button type="button" className="secondary" disabled={mutationLocked || selectingRoot} onClick={() => void openExisting()}>{selectingRoot ? text.loading : text.openRepo}</button>
            </div>
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

		{(repositoryRecords.length || repo) && mode !== 'repo' && mode !== 'credentials' ? <div className="vrepo-management-shell">
            {repositoryList}
            {repo ? <div className="vrepo-layout">
            <aside className="vrepo-tree" aria-label={repo.name}>
				<div className="vrepo-tree__head"><div><strong>{text.repositoryList}</strong><span>{repo.root_path}</span></div><div><button type="button" aria-label={text.addGroup} title={text.addGroup} disabled={mutationLocked} onClick={() => { setMode('group'); setDraft({ parent_id: selected?.repository ? selected.parent_id : selectedId }); }}>+DIR</button><button type="button" aria-label={text.addRepo} title={text.addRepo} disabled={mutationLocked} onClick={() => { if (selected && !selected.repository) { setMode('mapping'); setDraft({ ...selected, kind: 'git', enabled: true }); return; } setMode('mapping'); setDraft({ parent_id: selected?.repository ? selected.parent_id : selectedId, kind: 'git', enabled: true }); }}>+MAP</button></div></div>
                <div role="tree" aria-label={repo.name} className={dropTargetID === '' ? 'is-root-drop-target' : ''} onDragOver={(event) => { if (event.target === event.currentTarget && canMoveTreeNode(draggedNodeID, '')) { event.preventDefault(); event.dataTransfer.dropEffect = 'move'; setDropTargetID(''); } }} onDragLeave={(event) => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setDropTargetID((current) => current === '' ? null : current); }} onDrop={(event) => { if (event.target !== event.currentTarget) return; event.preventDefault(); const sourceID = draggedNodeID || event.dataTransfer.getData('text/plain'); void moveTreeNode(sourceID, ''); }}>
					<div role="treeitem" aria-level={1} aria-selected={!selectedId} aria-expanded="true" data-vrepo-tree-root tabIndex={!selectedId ? 0 : -1} className={`vrepo-tree__item vrepo-tree__root-item${!selectedId ? ' is-selected' : ''}${dropTargetID === '' ? ' is-drop-target' : ''}`} onClick={(event) => { event.currentTarget.focus(); setSelectedId(''); }} onKeyDown={(event) => { const firstNodeID = visibleNodeIDs[0]; const lastNodeID = visibleNodeIDs[visibleNodeIDs.length - 1]; if ((event.key === 'ArrowDown' || event.key === 'ArrowRight') && firstNodeID) { event.preventDefault(); setSelectedId(firstNodeID); focusTreeItem(firstNodeID); } else if (event.key === 'End' && lastNodeID) { event.preventDefault(); setSelectedId(lastNodeID); focusTreeItem(lastNodeID); } }} onDragOver={(event) => { if (canMoveTreeNode(draggedNodeID, '')) { event.preventDefault(); event.dataTransfer.dropEffect = 'move'; setDropTargetID(''); } }} onDragLeave={(event) => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setDropTargetID((current) => current === '' ? null : current); }} onDrop={(event) => { event.preventDefault(); const sourceID = draggedNodeID || event.dataTransfer.getData('text/plain'); void moveTreeNode(sourceID, ''); }}>
                        <span className="vrepo-tree__chevron" aria-hidden>▾</span><span className="vrepo-tree__icon">{treeNodeIcon()}</span><span className={`vrepo-tree__label${!selectedId ? ' is-selected' : ''}${dropTargetID === '' ? ' is-drop-target' : ''}`}>{repo.name}</span>
					</div>
					{renderNodes('', 1)}
				</div>
            </aside>
            <main className="vrepo-detail">
                {(mode === 'group' || mode === 'mapping') ? <section className="vrepo-editor">
                    <h2>{mode === 'group' ? text.addGroup : text.addRepo}</h2>
                    <label>{text.name}<input value={draft.name || ''} onChange={(e) => setDraft({ ...draft, name: e.target.value })} /></label>
                    <label>{text.parent}<select value={draft.parent_id || ''} onChange={(e) => setDraft({ ...draft, parent_id: e.target.value })}><option value="">{text.rootNode}</option>{repo.nodes.filter((n) => !n.repository).map((n) => <option key={n.id} value={n.id}>{n.name}</option>)}</select></label>
                    {mode === 'mapping' ? <>
	                        <label>{isZh ? '仓库说明（可选）' : 'Repository description (optional)'}<input value={draft.description || ''} onChange={(e) => setDraft({ ...draft, description: e.target.value })} /></label>
                        <label>{text.type}<select value={draft.kind || 'git'} onChange={(e) => setDraft({ ...draft, kind: e.target.value, credential_id: '' })}><option value="git">Git</option><option value="svn">SVN</option><option value="local">{text.local}</option></select></label>
							{draft.kind !== 'local' ? <><label>{text.remote}<input value={draft.remote_url || ''} onChange={(e) => setDraft({ ...draft, remote_url: e.target.value })} /></label><label>{text.refType}<select value={draft.ref_type || 'branch'} onChange={(e) => setDraft({ ...draft, ref_type: e.target.value })}><option value="branch">{text.branchRef}</option><option value="tag">{text.tagRef}</option></select></label><label>{text.refName}<input value={draft.ref_name || ''} placeholder={text.defaultRef} onChange={(e) => setDraft({ ...draft, ref_name: e.target.value })} /></label><label>{text.credentials}<div className="vrepo-inline"><select value={draft.credential_id || credentialBindings[draft.id] || ''} onFocus={() => { const kind = draft.kind === 'svn' ? 'svn' : 'git'; if (!credentials.some((item) => item.kind === kind)) void loadMappingCredentials(kind); }} onChange={(e) => setDraft({ ...draft, credential_id: e.target.value })}><option value="">{text.noCredential}</option>{credentials.filter((item) => item.kind === draft.kind).map((item) => <option key={item.id} value={item.id}>{item.name} · {item.username}</option>)}</select><button type="button" className="secondary vrepo-credential-add" onClick={() => void startCredentialManager(true)}>{text.addCredential}</button></div></label></> : null}
						{draft.kind !== 'local' ? <label className="vrepo-check"><input type="checkbox" disabled={draft.enabled === false} checked={!!draft.checkout_after_save} onChange={(e) => setDraft({ ...draft, checkout_after_save: e.target.checked })} />{text.checkoutAfterSave}</label> : null}
	                        <label className="vrepo-check"><input type="checkbox" checked={!!draft.create_directory} onChange={(e) => setDraft({ ...draft, create_directory: e.target.checked })} />{text.createDir}</label>
                        <label className="vrepo-check"><input type="checkbox" checked={draft.enabled !== false} onChange={(e) => setDraft({ ...draft, enabled: e.target.checked })} />{text.enabled}</label>
                    </> : null}
                    <div className="vrepo-form-actions"><button type="button" onClick={() => void saveNode()} disabled={busy}>{text.save}</button><button type="button" className="secondary" onClick={() => setMode('none')}>{text.cancel}</button></div>
                </section> : selected ? <section>
                    <div className="vrepo-detail__title"><div><span className="vrepo-detail__kind">{selected.repository?.kind.toUpperCase() || 'DIR'}</span><h2>{selected.name}</h2></div><div><button type="button" className="secondary" disabled={mutationLocked} onClick={() => { setMode(selected.repository ? 'mapping' : 'group'); setDraft({ ...selected, ...(selected.repository || {}) }); }}>{text.edit}</button><button type="button" className="danger" disabled={mutationLocked} onClick={() => void removeNode(selected.id)}>{text.remove}</button></div></div>
					{selected.repository ? <><dl className="vrepo-facts">{selected.repository.remote_url ? <><dt>{text.remote}</dt><dd>{selectedStatus?.remote_url || selected.repository.remote_url}</dd></> : null}{selected.repository.ref_name ? <><dt>{selected.repository.ref_type === 'tag' ? text.tagRef : text.branchRef}</dt><dd>{selected.repository.ref_name}</dd></> : null}{selectedStatus?.branch ? <><dt>{text.branch}</dt><dd>{selectedStatus.branch}</dd></> : null}<dt>{text.status}</dt><dd>{selectedStatus?.error_code === 'not_checked_out' ? text.notCheckedOut : selectedStatus?.error || (selectedStatus ? selectedStatus.clean ? text.clean : text.changed : text.pendingStatus)}</dd>{directoryStats && selected.repository.kind === 'local' ? <><dt>{text.files}</dt><dd>{directoryStats.file_count}</dd><dt>{text.size}</dt><dd>{directoryStats.size_bytes.toLocaleString()} bytes</dd></> : null}</dl>{!repo.remote ? <button type="button" className="secondary" onClick={() => void openMappedFolder()}>{text.openFolder}</button> : null}{selected.repository.kind !== 'local' ? <button type="button" disabled={mutationLocked} onClick={async () => { try { setBusy(true); setError(''); await checkoutRepositoryNode(repo, selected.id); await refreshStatus(); } catch (e: any) { setError(errorMessage(e)); } finally { setBusy(false); } }}>{text.checkout}</button> : null}{selected.repository.kind === 'local' ? <button type="button" className="secondary" onClick={() => void loadDirectoryStats()}>{text.calculateSize}</button> : null}</> : null}
                    {selectedStatus?.status ? <pre className="vrepo-status-output">{selectedStatus.status}</pre> : null}
						</section> : <section className="vrepo-overview"><div className="vrepo-overview__title"><div><span>{repo.remote ? text.remoteRepository : text.localRepository}</span><h2>{repo.name}</h2><p>{repo.root_path}</p></div></div><div className="vrepo-health"><div><span>{text.health}</span><strong>{Object.values(statuses).some(statusNeedsAttention) ? text.needsAttention : Object.keys(statuses).length ? text.healthy : text.pendingStatus}</strong></div><div><span>{text.mappings}</span><strong>{repo.nodes.filter((node) => node.repository).length}</strong></div><div><span>{text.location}</span><strong>{repo.remote ? text.remoteRepository : text.localRepository}</strong></div></div><div className="vrepo-overview__next"><h3>{text.repositoryActions}</h3><p>{text.selectRepositoryHint}</p></div></section>}
            </main>
        </div> : <section className="vrepo-management-placeholder"><h2>{text.selectRepository}</h2><p>{text.selectRepositoryHint}</p></section>}
        </div> : null}

		{mode === 'credentials' ? <section className="vrepo-credentials">
			<div className="vrepo-detail__title"><h2>{text.manageCredentials}</h2><button type="button" className="secondary" disabled={busy} onClick={() => { credentialManagerRequestRef.current += 1; const mappingDraft = mappingDraftBeforeCredentials; setMode(credentialReturnToMapping ? 'mapping' : 'none'); setCredentialReturnToMapping(false); setMappingDraftBeforeCredentials(null); setDraft(mappingDraft || {}); }}>{text.cancel}</button></div>
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

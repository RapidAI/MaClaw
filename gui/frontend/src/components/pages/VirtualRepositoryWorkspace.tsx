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
	available?: boolean;
	unbound?: boolean;
	root_repair?: boolean;
	error_code?: string;
	error?: string;
};

type RootMigrationPreview = {
    repository_id: string;
    source_root: string;
    destination_root: string;
    remote: boolean;
    source_file_count: number;
    source_size_bytes: number;
    destination_file_count: number;
    destination_size_bytes: number;
    destination_exists: boolean;
    destination_has_manifest: boolean;
    can_migrate: boolean;
    reason?: string;
};

const isRootMigrationPreview = (value: unknown): value is RootMigrationPreview => {
	if (!value || typeof value !== 'object') return false;
	const preview = value as Record<string, unknown>;
	return typeof preview.repository_id === 'string' && typeof preview.source_root === 'string' && typeof preview.destination_root === 'string'
		&& typeof preview.remote === 'boolean' && typeof preview.source_file_count === 'number' && Number.isFinite(preview.source_file_count)
		&& typeof preview.source_size_bytes === 'number' && Number.isFinite(preview.source_size_bytes)
		&& typeof preview.destination_file_count === 'number' && Number.isFinite(preview.destination_file_count)
		&& typeof preview.destination_size_bytes === 'number' && Number.isFinite(preview.destination_size_bytes)
		&& typeof preview.destination_exists === 'boolean' && typeof preview.destination_has_manifest === 'boolean'
		&& typeof preview.can_migrate === 'boolean' && (preview.reason == null || typeof preview.reason === 'string');
};

const parseRootMigrationPreview = (raw: unknown, label: string): RootMigrationPreview => {
	const preview = parseRequiredJSON<unknown>(raw, label);
	if (!isRootMigrationPreview(preview)) throw new Error(`${label} returned invalid migration details`);
	return preview;
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

type ChangeFile = {
	path: string;
	original_path?: string;
	index_status: string;
	worktree_status: string;
};

type ChangeCommit = {
	hash: string;
	short_hash: string;
	parents: string[];
	author: string;
	date: string;
	subject: string;
	decorations?: string;
};

type RepositoryChanges = {
	node_id: string;
	branch?: string;
	head?: string;
	files: ChangeFile[];
	files_truncated?: boolean;
	commits: ChangeCommit[];
	diff?: string;
};

type ClientStatus = {
    kind: string;
    available: boolean;
    executable?: string;
    version?: string;
    source?: string;
    error?: string;
};

type VCSClientAction = 'git-search' | 'git-select' | 'git-reset' | 'svn-search' | 'svn-select' | 'svn-reset' | '';
type VCSClientKind = 'git' | 'svn';

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

const isRepositoryChanges = (value: unknown): value is RepositoryChanges => {
	if (!value || typeof value !== 'object') return false;
	const changes = value as Record<string, unknown>;
	return typeof changes.node_id === 'string'
		&& (changes.branch == null || typeof changes.branch === 'string')
		&& (changes.head == null || typeof changes.head === 'string')
		&& (changes.files_truncated == null || typeof changes.files_truncated === 'boolean')
		&& (changes.diff == null || typeof changes.diff === 'string')
		&& Array.isArray(changes.files) && changes.files.every((file) => file && typeof file === 'object'
			&& typeof file.path === 'string' && typeof file.index_status === 'string' && typeof file.worktree_status === 'string'
			&& (file.original_path == null || typeof file.original_path === 'string'))
		&& Array.isArray(changes.commits) && changes.commits.every((commit) => commit && typeof commit === 'object'
			&& typeof commit.hash === 'string' && typeof commit.short_hash === 'string' && Array.isArray(commit.parents)
			&& commit.parents.every((parent: unknown) => typeof parent === 'string')
			&& typeof commit.author === 'string' && typeof commit.date === 'string' && typeof commit.subject === 'string'
			&& (commit.decorations == null || typeof commit.decorations === 'string'));
};

const parseRepositoryChanges = (raw: unknown, label: string): RepositoryChanges => {
	const value = parseRequiredJSON<unknown>(raw, label);
	if (!isRepositoryChanges(value)) throw new Error(`${label} returned invalid change details`);
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

const repositoryContextMenuWidth = 232;
const repositoryContextMenuHeight = 92;

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
type BackgroundSyncPhase = 'idle' | 'queued' | 'running' | 'retry_wait' | 'failed' | 'conflict';
const BACKGROUND_SYNC_PHASES: ReadonlySet<string> = new Set(['idle', 'queued', 'running', 'retry_wait', 'failed', 'conflict']);
const localizeVRepoError = (value: unknown, isZh: boolean) => {
	const message = errorMessage(value).trim();
	if (!isZh || !message) return message;
	const lower = message.toLowerCase();
	if (lower.includes('cloud data changed while syncing')) return '云端数据在同步过程中被其他设备更新。请稍后再点「立即同步」；若仍失败，请确认仅有一台设备在同时同步。';
	if (lower.includes('automatic virtual repository synchronization is in progress')) return '后台正在同步虚拟仓库配置，请稍后再试。';
	if (lower.includes('hub url not configured') || lower.includes('hub token not configured') || lower.includes('machine id missing')) return '同步需要先连接并注册 Hub。';
	if (lower.includes('read virtual repository') && lower.includes('for sync')) return message.replace(/^read virtual repository /i, '读取虚拟仓库 ').replace(/ for sync:/i, ' 以同步时失败：');
	if (lower.includes('checkout target already exists and is not empty')) return '检出目录已存在且不为空。请先选择其他目录，或清理该目录后再检出。';
	if (lower.includes('repository has not been checked out')) return '仓库尚未检出，请先完成检出后再执行此操作。';
	if (lower.includes('nothing to commit')) return '没有可提交的更改。';
	if (lower.includes('non-fast-forward') || lower.includes('rejected')) return '推送被远端拒绝。请先同步更新并处理冲突后再推送。';
	if (lower.includes('authentication failed') || lower.includes('authorization failed') || lower.includes('could not authenticate') || lower.includes('access denied')) return '仓库认证失败，请检查凭据和访问权限。';
	// Match VCS working-copy conflicts only. Do not use a bare "conflict" substring:
	// Hub revision races and merge prompts also contain that word in English UI text.
	if (lower.includes('working copy has conflicts')) return '工作副本存在冲突，请处理冲突后再继续。';
	if (lower.includes('not a git working tree')) return '目录不是有效的 Git 工作副本。';
	if (lower.includes('not a working copy')) return '目录不是有效的 SVN 工作副本。';
	return message;
};
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

// Shared modal shell for the workspace's editors. Renders a dimmed backdrop and
// a centered dialog with a titled header, a scrollable body, and an action
// footer. Escape and backdrop presses close the dialog unless it is busy, and
// focus returns to the control that opened it.
type VRepoDialogProps = {
    title: React.ReactNode;
    titleId: string;
    onClose: () => void;
    closeLabel: string;
    closeDisabled?: boolean;
    className?: string;
    footer?: React.ReactNode;
    children: React.ReactNode;
};

function VRepoDialog({ title, titleId, onClose, closeLabel, closeDisabled = false, className = '', footer, children }: VRepoDialogProps) {
    const dialogRef = useRef<HTMLElement | null>(null);
    const pressStartedOnBackdropRef = useRef(false);
    const onCloseRef = useRef(onClose);
    onCloseRef.current = onClose;
    const closeDisabledRef = useRef(closeDisabled);
    closeDisabledRef.current = closeDisabled;

    useEffect(() => {
        const previousFocus = document.activeElement as HTMLElement | null;
        const dialog = dialogRef.current;
        const focusTimer = window.setTimeout(() => {
            const target = dialog?.querySelector<HTMLElement>('.vrepo-dialog__body input:not(:disabled), .vrepo-dialog__body select:not(:disabled), .vrepo-dialog__body textarea:not(:disabled)')
                || dialog?.querySelector<HTMLElement>('.vrepo-dialog__body button:not(:disabled), .vrepo-dialog__footer button:not(:disabled)')
                || dialog?.querySelector<HTMLElement>('button:not(:disabled)');
            target?.focus();
        }, 0);
        const onKeyDown = (event: KeyboardEvent) => {
            if (event.key === 'Escape') {
                if (closeDisabledRef.current) return;
                // A CustomDialog confirm/prompt layers above this dialog and owns
                // Escape; do not close the editor out from under it.
                if (document.querySelector('.modal-backdrop')) return;
                event.preventDefault();
                onCloseRef.current();
                return;
            }
            if (event.key !== 'Tab' || !dialog) return;
            const focusable = Array.from(dialog.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled)'));
            if (!focusable.length) return;
            const first = focusable[0];
            const last = focusable[focusable.length - 1];
            if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
            else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
        };
        window.addEventListener('keydown', onKeyDown, true);
        return () => {
            window.clearTimeout(focusTimer);
            window.removeEventListener('keydown', onKeyDown, true);
            previousFocus?.focus();
        };
    }, []);

    return <div className="vrepo-dialog-backdrop" role="presentation" onMouseDown={(event) => { pressStartedOnBackdropRef.current = event.target === event.currentTarget; }} onClick={(event) => {
        const shouldClose = pressStartedOnBackdropRef.current && event.target === event.currentTarget;
        pressStartedOnBackdropRef.current = false;
        if (shouldClose && !closeDisabledRef.current) onCloseRef.current();
    }}>
        <section ref={dialogRef} className={`vrepo-dialog${className ? ` ${className}` : ''}`} role="dialog" aria-modal="true" aria-labelledby={titleId}>
            <header className="vrepo-dialog__header">
                <h2 id={titleId}>{title}</h2>
                <button type="button" className="vrepo-dialog__close" aria-label={closeLabel} title={closeLabel} disabled={closeDisabled} onClick={onClose}>×</button>
            </header>
            <div className="vrepo-dialog__body">{children}</div>
            {footer ? <footer className="vrepo-dialog__footer">{footer}</footer> : null}
        </section>
    </div>;
}

export function VirtualRepositoryWorkspace({ isZh, onBack, onOpenCodingTask }: { isZh: boolean; onBack: () => void; onOpenCodingTask?: (launch: CodingTaskLaunch) => void }) {
	const { showConfirm } = useDialog();
	    const text = isZh ? {
        title: '虚拟仓库', back: '返回实用工具', newRepo: '新建虚拟仓库', openRepo: '打开已有目录',
        recent: '最近使用', repositoryList: '仓库', searchRepositories: '搜索仓库', repositoryCount: '个仓库', noSearchResults: '没有匹配的虚拟仓库', selectRepository: '选择一个虚拟仓库', selectRepositoryHint: '从左侧打开仓库，查看目录映射、健康状态和操作记录。', localRepository: '本机', remoteRepository: '远程 SSH', mappings: '个映射', health: '健康概览', healthy: '正常', needsAttention: '需处理', pendingStatus: '尚未检查', lastOpened: '最近打开', repositoryActions: '仓库操作',
        empty: '尚未创建虚拟仓库', emptyHint: '选择一个根目录，MaClaw 会在其中创建 .vrepo/manifest.json。',
        name: '名称', root: '根目录', choose: '选择目录', save: '保存', cancel: '取消', refresh: '刷新状态',
		addGroup: '新建虚拟目录', addRepo: '添加映射', editGroup: '编辑虚拟目录', editMapping: '编辑映射', credentials: '仓库凭据', gitClient: 'Git 工具', svnClient: 'SVN 工具',
	        type: '类型', path: '目录位置', remote: '仓库地址', enabled: '启用', parent: '上级目录', rootNode: '（根）', refType: '版本类型', refName: '分支/标签', defaultRef: '默认分支', checkout: '检出仓库', checkingOut: '正在检出…', checkoutAfterSave: '保存后立即检出', notCheckedOut: '尚未检出', checkedOut: '已检出', checkedOutClean: '已检出 · 干净', checkedOutChanged: '已检出 · 有变更', branchRef: '分支', tagRef: '标签',
        edit: '编辑', remove: '删除', openFolder: '打开目录', createDir: '目录不存在时创建', localDirectoryCreated: '本地映射保存后会自动创建目录', clean: '干净', changed: '有变更',
	        local: '纯本地目录', loading: '加载中…', noGitClient: '未找到 Git 命令行工具', noClient: '未找到 SVN 命令行工具', clientNotFound: '未找到', search: '重新搜索', resetClient: '恢复自动搜索', specifyGit: '指定 git', specify: '指定 svn',
        installSVN: '安装说明',
        foundClient: '已找到', credentialName: '凭据名称', username: '用户名', password: '密码或令牌', scope: '主机/Realm（可选）',
        addCredential: '新增凭据', manageCredentials: '凭据管理', noCredentials: '尚无已保存凭据', passwordHint: '编辑时留空表示不修改',
		deleteIndex: '从最近使用中移除', deleteRepository: '删除虚拟仓库', deleteRepositoryConfirm: '确认从 MaClaw 列表中移除“{name}”？\n\n这不会删除 .vrepo 目录或真实文件，但会解除本机凭据绑定和 SSH 密码保存。', manifestNote: '删除这里只移除 MaClaw 索引，不会删除 .vrepo 或真实文件。',
		syncNow: '立即同步', syncing: '正在同步…', checkingSync: '正在检查同步状态…', backgroundSyncing: '后台正在同步…', backgroundSyncRetry: '自动同步失败，将自动重试', backgroundSyncFailed: '自动同步已暂停', backgroundSyncConflict: '自动同步发现冲突，请点「立即同步」处理', repairRemoteConnection: '修复远程连接', syncReady: '自动同步已开启', syncSuccess: '已同步', syncConflict: '同步冲突', syncConflictMessage: '“{name}”同时在本机和另一台设备上被修改。是否保留本机版本？', syncConflictCloud: '是否改为采用 Hub 版本？选择“否”会保留两个版本，并将本机版本另存为副本。', useLocal: '保留本机', useCloud: '采用 Hub', keepCopy: '保留副本', syncUnavailable: '同步需要先连接并注册 Hub。',
		commit: '提交', push: '推送', syncWorkingCopies: '同步已检出仓库', syncWorkingCopiesTitle: '同步已检出仓库', syncWorkingCopiesHint: 'Git 将执行仅快进更新；SVN 将执行 update。存在本地改动或冲突时不会自动合并。', commitPush: '提交并推送', revert: '还原', execute: '执行', preview: '预览',
		commitMessage: '提交说明', repositories: '个仓库', localSkipped: '个本地目录已跳过', revertWarning: '未提交的已跟踪更改将被丢弃；未跟踪文件会保留。',
        operation: '操作', close: '关闭', retryFailed: '重试失败项', calculateSize: '计算大小', files: '文件数', size: '大小', branch: '分支', status: '状态',
        noCredential: '不使用凭据', anyHost: '任意主机', operationRunningHint: '仓库操作运行期间不能修改虚拟目录树或启动其他操作。',
			location: '位置', localLocation: '本机', remoteLocation: '远程 SSH', editConnection: '编辑连接', repairConnectionHint: '此处仅用于验证并修复 SSH 连接；连接恢复后请关闭窗口并点击「立即同步」。', moveRoot: '迁移根目录', moveRootTitle: '迁移仓库根目录', moveRootHint: '复制文件、验证仓库清单后才会切换到新位置；旧根目录会保留，方便你确认后自行清理。', currentRoot: '当前根目录', destinationRoot: '新根目录', rootManagedByMigration: '已有仓库的根目录由“迁移根目录”操作管理。', inspectMigration: '检查迁移', migrationReady: '预检通过，可以迁移。', migrationConflict: '目标目录不能包含另一个虚拟仓库，且不能与源目录重叠。已有的同名文件也会阻止迁移。', sourceFiles: '源文件', destinationFiles: '目标文件', migrateNow: '开始迁移', migrating: '正在迁移…', migrationComplete: '迁移完成。旧根目录仍被保留。', chooseDestination: '选择目标目录', previewAgain: '重新检查', chooseNewRoot: '请选择新的仓库根目录。', locationUnavailable: '根目录不可用', locationUnavailableHint: '此仓库来自其它设备，尚未在本机设置根目录。请选择一个空目录，或包含同一虚拟仓库的目录。', setLocalRoot: '设置本机根目录', bindLocalRootTitle: '设置本机仓库根目录', bindLocalRootHint: '此设置仅保存在本机，不会覆盖其它设备的目录位置。', bindLocalRoot: '绑定根目录', bindingRoot: '正在绑定…', reconnectLocalRoot: '重新连接本机根目录', reconnectRoot: '重新连接根目录', reconnectRootTitle: '重新连接本机根目录', reconnectRootHint: '请选择包含同一虚拟仓库清单的目录；不会初始化或覆盖目录内容。', reconnectRootUnavailableHint: '原本机根目录不可用。只能重新连接到匹配的仓库。', rootRepairListHint: '原本机根目录不可用。请选择包含同一虚拟仓库的目录以重新连接。', startCodingTask: '启动编程任务', startingCodingTask: '正在启动…', server: '服务器', port: '端口', sshUser: 'SSH 用户名', sshPassword: 'SSH 密码', remoteRoot: '远程根目录', testConnection: '测试连接', trustHostKey: '首次创建时设置信任主机密钥', hostKeyPrompt: '首次连接，请核对并信任服务器指纹', hostKeyChangedPrompt: '服务器主机密钥与已保存指纹不一致。请先独立核对下方指纹；确认后移除旧记录，再重新测试并明确保存新密钥。', removeSavedHostKey: '移除已保存密钥', removeSavedHostKeyConfirm: '这将移除该远程仓库的已保存 SSH 主机密钥，不会信任下方的新密钥。请先通过独立渠道核对指纹；移除后必须重新测试并明确保存新密钥。', connected: '连接成功', rootMissingPrompt: 'SSH 已连接，但远程根目录不存在。是否创建该目录？', createRemoteRoot: '创建远程根目录', createRemoteRootConfirm: '确认在远程服务器上创建此根目录？',
		cleanStatus: '仓库干净', changedStatus: '仓库有变更', errorStatus: '仓库状态异常', changes: '变更', changesTitle: 'Git 变更', changesHint: '查看工作区文件变更与最近提交关系；此页面只读，不会修改仓库。', refreshChanges: '刷新变更', loadingChanges: '正在读取变更…', noChanges: '工作区没有未提交的变更', changesTruncated: '仅显示前 2,000 个变更文件；请使用 Git 客户端查看完整列表。', changedFiles: '文件变更', recentCommits: '最近提交', selectChange: '选择一个文件查看差异', noDiff: '该文件没有可显示的文本差异', conflict: '冲突', staged: '已暂存', modified: '已修改', untracked: '未跟踪', renamed: '重命名', deleted: '已删除', graph: '提交图', changesUnavailable: '仅已检出的 Git 映射可查看变更', closeChanges: '关闭变更',
    } : {
        title: 'Virtual Repository', back: 'Back to utilities', newRepo: 'New virtual repository', openRepo: 'Open existing root',
        recent: 'Recent', repositoryList: 'Repositories', searchRepositories: 'Search repositories', repositoryCount: 'repositories', noSearchResults: 'No virtual repositories match your search', selectRepository: 'Select a virtual repository', selectRepositoryHint: 'Open a repository from the list to review mappings, health, and operations.', localRepository: 'Local', remoteRepository: 'Remote SSH', mappings: 'mappings', health: 'Health overview', healthy: 'Healthy', needsAttention: 'Needs attention', pendingStatus: 'Not checked', lastOpened: 'Last opened', repositoryActions: 'Repository actions',
        empty: 'No virtual repositories yet', emptyHint: 'Choose a root directory; MaClaw creates .vrepo/manifest.json inside it.',
        name: 'Name', root: 'Root directory', choose: 'Choose', save: 'Save', cancel: 'Cancel', refresh: 'Refresh status',
		addGroup: 'New virtual folder', addRepo: 'Add mapping', editGroup: 'Edit virtual folder', editMapping: 'Edit mapping', credentials: 'Repository credentials', gitClient: 'Git client', svnClient: 'SVN client',
	        type: 'Type', path: 'Directory location', remote: 'Repository URL', enabled: 'Enabled', parent: 'Parent folder', rootNode: '(root)', refType: 'Version type', refName: 'Branch/tag', defaultRef: 'Default branch', checkout: 'Checkout repository', checkingOut: 'Checking out…', checkoutAfterSave: 'Checkout after saving', notCheckedOut: 'Not checked out', checkedOut: 'Checked out', checkedOutClean: 'Checked out · Clean', checkedOutChanged: 'Checked out · Changed', branchRef: 'Branch', tagRef: 'Tag',
        edit: 'Edit', remove: 'Remove', openFolder: 'Open folder', createDir: 'Create directory if missing', localDirectoryCreated: 'The local mapping directory is created automatically when saved', clean: 'Clean', changed: 'Changed',
	        local: 'Local directory', loading: 'Loading…', noGitClient: 'Git command line client not found', noClient: 'SVN command line client not found', clientNotFound: 'Not found', search: 'Search again', resetClient: 'Use automatic search', specifyGit: 'Choose git', specify: 'Choose svn',
        installSVN: 'Installation guide',
        foundClient: 'Found', credentialName: 'Credential name', username: 'Username', password: 'Password or token', scope: 'Host/realm (optional)',
        addCredential: 'Add credential', manageCredentials: 'Credential manager', noCredentials: 'No saved credentials', passwordHint: 'Leave blank while editing to keep the secret',
		deleteIndex: 'Remove from recent', deleteRepository: 'Delete virtual repository', deleteRepositoryConfirm: 'Remove “{name}” from the MaClaw list?\n\nThis does not delete .vrepo or real files, but it removes local credential bindings and the saved SSH password.', manifestNote: 'This only removes the MaClaw index entry; .vrepo and real files remain untouched.',
		syncNow: 'Sync now', syncing: 'Syncing…', checkingSync: 'Checking sync status…', backgroundSyncing: 'Syncing in the background…', backgroundSyncRetry: 'Automatic sync failed; will retry', backgroundSyncFailed: 'Automatic sync paused', backgroundSyncConflict: 'Automatic sync found conflicts — click Sync now to resolve', repairRemoteConnection: 'Repair remote connection', syncReady: 'Automatic sync is on', syncSuccess: 'Synced', syncConflict: 'Sync conflict', syncConflictMessage: '“{name}” changed both here and on another device. Keep this computer’s version?', syncConflictCloud: 'Use the Hub version instead? Choosing “No” keeps both by saving this computer’s version as a copy.', useLocal: 'Keep local', useCloud: 'Use Hub', keepCopy: 'Keep copy', syncUnavailable: 'Connect and register with Hub before syncing.',
		commit: 'Commit', push: 'Push', syncWorkingCopies: 'Sync checked-out repositories', syncWorkingCopiesTitle: 'Sync checked-out repositories', syncWorkingCopiesHint: 'Git uses fast-forward-only pull; SVN uses update. Local changes and conflicts are never merged automatically.', commitPush: 'Commit & push', revert: 'Revert', execute: 'Execute', preview: 'Preview',
		commitMessage: 'Commit message', repositories: 'repositories', localSkipped: 'local directories skipped', revertWarning: 'Uncommitted tracked changes will be discarded. Untracked files are preserved.',
        operation: 'Operation', close: 'Close', retryFailed: 'Retry failed', calculateSize: 'Calculate size', files: 'Files', size: 'Size', branch: 'Branch', status: 'Status',
        noCredential: 'No credential', anyHost: 'any host', operationRunningHint: 'The virtual tree and other operations are locked while a repository operation is running.',
			location: 'Location', localLocation: 'This computer', remoteLocation: 'Remote SSH', editConnection: 'Edit connection', repairConnectionHint: 'This dialog only verifies and repairs the SSH connection. After it succeeds, close it and click Sync now.', moveRoot: 'Move root', moveRootTitle: 'Move repository root', moveRootHint: 'Files are copied and the manifest is verified before switching to the new location. The old root is kept for review.', currentRoot: 'Current root', destinationRoot: 'New root', rootManagedByMigration: 'An existing repository root is managed by the Move root action.', inspectMigration: 'Check migration', migrationReady: 'Preflight passed. This repository is ready to move.', migrationConflict: 'The destination cannot contain another virtual repository or overlap the source. Existing files with the same path also block the move.', sourceFiles: 'Source files', destinationFiles: 'Destination files', migrateNow: 'Start migration', migrating: 'Migrating…', migrationComplete: 'Migration complete. The old root was kept.', chooseDestination: 'Choose destination', previewAgain: 'Check again', chooseNewRoot: 'Choose a new repository root.', locationUnavailable: 'Root directory unavailable', locationUnavailableHint: 'This repository came from another device and has no root directory on this computer yet. Choose an empty directory, or one containing this virtual repository.', setLocalRoot: 'Set local root', bindLocalRootTitle: 'Set local repository root', bindLocalRootHint: 'This setting is stored only on this computer and never replaces another device’s location.', bindLocalRoot: 'Bind root', bindingRoot: 'Binding…', reconnectLocalRoot: 'Reconnect local root', reconnectRoot: 'Reconnect root', reconnectRootTitle: 'Reconnect local repository root', reconnectRootHint: 'Choose a directory containing the same virtual repository manifest. No directory contents will be initialized or overwritten.', reconnectRootUnavailableHint: 'The previous local root is unavailable. Reconnect only to the matching repository.', rootRepairListHint: 'Local root unavailable — choose the matching repository to reconnect it.', startCodingTask: 'Start coding task', startingCodingTask: 'Starting…', server: 'Server', port: 'Port', sshUser: 'SSH username', sshPassword: 'SSH password', remoteRoot: 'Remote root directory', testConnection: 'Test connection', trustHostKey: 'Trust and save host key', hostKeyPrompt: 'First connection: verify and trust this server fingerprint', hostKeyChangedPrompt: 'The server host key differs from the saved fingerprint. Verify the fingerprint independently, then remove the old saved key, test again, and explicitly save the new key.', removeSavedHostKey: 'Remove saved key', removeSavedHostKeyConfirm: 'This removes the saved SSH host key for this remote repository; it does not trust the newly observed key. Verify the fingerprint independently first. You must test again and explicitly save the new key.', connected: 'Connected', rootMissingPrompt: 'SSH is connected, but the remote root does not exist. Create it now?', createRemoteRoot: 'Create remote root', createRemoteRootConfirm: 'Create this root directory on the remote server?',
		cleanStatus: 'Repository is clean', changedStatus: 'Repository has changes', errorStatus: 'Repository status error', changes: 'Changes', changesTitle: 'Git changes', changesHint: 'Review working-tree files and recent commit relationships. This view is read-only.', refreshChanges: 'Refresh changes', loadingChanges: 'Loading changes…', noChanges: 'The working tree has no uncommitted changes', changesTruncated: 'Showing the first 2,000 changed files. Use a Git client for the full list.', changedFiles: 'Changed files', recentCommits: 'Recent commits', selectChange: 'Select a file to view its diff', noDiff: 'This file has no text diff to display', conflict: 'Conflict', staged: 'Staged', modified: 'Modified', untracked: 'Untracked', renamed: 'Renamed', deleted: 'Deleted', graph: 'Commit graph', changesUnavailable: 'Changes are available for checked-out Git mappings only', closeChanges: 'Close changes',
    };

	// Hover/title strings for toolbar and dialog actions. Kept separate from
	// button labels so short labels can still carry a fuller functional hint.
	const tips = isZh ? {
		back: '返回实用工具页面',
		searchGit: '在系统中重新搜索 Git 可执行文件',
		specifyGit: '手动选择 Git 可执行文件',
		resetClient: '清除手动路径并恢复自动搜索',
		searchSvn: '在系统中重新搜索 SVN 可执行文件',
		specifySvn: '手动选择 SVN 可执行文件',
		installSvn: '打开 SVN 官方安装说明页面',
		syncConfig: '与 Hub 同步虚拟仓库配置',
		openRepo: '打开本机已有虚拟仓库根目录',
		newRepo: '创建新的虚拟仓库',
		moveRoot: '将仓库迁移到新的根目录位置',
		editConnection: '编辑远程 SSH 连接信息',
		startCoding: '以当前仓库根目录启动编程任务',
		refresh: '刷新各映射的检出与变更状态',
		syncWC: '对已检出仓库执行 pull/update（仅快进）',
		commit: '预览并提交已检出仓库的本地更改',
		push: '预览并推送已提交更改到远端',
		commitPush: '预览后提交本地更改并推送到远端',
		revert: '预览并丢弃未提交的已跟踪更改',
		credentials: '管理 Git/SVN 登录凭据',
		deleteRepo: '从列表移除此仓库（不删除磁盘文件）',
		openRecent: '打开此虚拟仓库',
		setLocalRoot: '为本机绑定仓库根目录',
		reconnectRoot: '重新连接匹配的本机根目录',
		expand: '展开',
		collapse: '折叠',
		addGroup: '在当前层级新建虚拟目录',
		addRepo: '添加 Git/SVN/本地目录映射',
		editNode: '编辑所选目录或映射',
		removeNode: '删除所选目录或映射',
		openFolder: '在文件管理器中打开映射目录',
		checkout: '将远程仓库检出到映射路径',
		calcSize: '统计本地目录文件数与占用空间',
		cancel: '取消并关闭',
		save: '保存当前更改',
		chooseRoot: '选择本机目录',
		chooseDest: '选择目标目录',
		testConn: '测试远程 SSH 连接',
		createRemoteRoot: '在远程服务器创建根目录',
		inspectMigration: '检查迁移是否可行',
		migrateNow: '开始迁移到新根目录',
		bindRoot: '绑定所选目录为本机根目录',
		previewOp: '预览将受影响的仓库',
		executeOp: '确认并执行操作',
		retryFailed: '仅重试失败项',
		addCredential: '新增仓库登录凭据',
		editCredential: '编辑此凭据',
		removeCredential: '删除此凭据',
	} : {
		back: 'Return to the utilities page',
		searchGit: 'Search the system for the Git executable',
		specifyGit: 'Manually choose the Git executable',
		resetClient: 'Clear the manual path and use automatic search',
		searchSvn: 'Search the system for the SVN executable',
		specifySvn: 'Manually choose the SVN executable',
		installSvn: 'Open the official SVN installation guide',
		syncConfig: 'Sync virtual repository config with Hub',
		openRepo: 'Open an existing virtual repository root',
		newRepo: 'Create a new virtual repository',
		moveRoot: 'Move the repository to a new root directory',
		editConnection: 'Edit the remote SSH connection',
		startCoding: 'Start a coding task at this repository root',
		refresh: 'Refresh checkout and change status for all mappings',
		syncWC: 'Pull/update checked-out repositories (fast-forward only)',
		commit: 'Preview and commit local changes in checked-out repositories',
		push: 'Preview and push committed changes to remotes',
		commitPush: 'Preview, commit local changes, then push',
		revert: 'Preview and discard uncommitted tracked changes',
		credentials: 'Manage Git/SVN login credentials',
		deleteRepo: 'Remove this repository from the list (files stay on disk)',
		openRecent: 'Open this virtual repository',
		setLocalRoot: 'Bind a local root directory for this repository',
		reconnectRoot: 'Reconnect the matching local root directory',
		expand: 'Expand',
		collapse: 'Collapse',
		addGroup: 'Create a virtual folder at this level',
		addRepo: 'Add a Git/SVN/local directory mapping',
		editNode: 'Edit the selected folder or mapping',
		removeNode: 'Remove the selected folder or mapping',
		openFolder: 'Open the mapped folder in the file manager',
		checkout: 'Check out the remote repository into the mapped path',
		calcSize: 'Count files and size for the local directory',
		cancel: 'Cancel and close',
		save: 'Save current changes',
		chooseRoot: 'Choose a local directory',
		chooseDest: 'Choose the destination directory',
		testConn: 'Test the remote SSH connection',
		createRemoteRoot: 'Create the root directory on the remote server',
		inspectMigration: 'Check whether the migration can proceed',
		migrateNow: 'Start migration to the new root',
		bindRoot: 'Bind the chosen directory as the local root',
		previewOp: 'Preview repositories that will be affected',
		executeOp: 'Confirm and run the operation',
		retryFailed: 'Retry only the failed items',
		addCredential: 'Add a repository credential',
		editCredential: 'Edit this credential',
		removeCredential: 'Delete this credential',
	};

    const [repos, setRepos] = useState<any[]>([]);
    const [repo, setRepo] = useState<VRepo | null>(null);
    const [selectedId, setSelectedId] = useState('');
	const [statuses, setStatuses] = useState<Record<string, NodeStatus>>({});
	const [changes, setChanges] = useState<RepositoryChanges | null>(null);
	const [changesNodeID, setChangesNodeID] = useState('');
	const [changesFilePath, setChangesFilePath] = useState('');
	const [changesLoading, setChangesLoading] = useState(false);
	    const [mode, setMode] = useState<'none' | 'repo' | 'group' | 'mapping' | 'credentials' | 'migration' | 'root-binding'>('none');
    const [draft, setDraft] = useState<any>({});
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState('');
    const [git, setGit] = useState<ClientStatus | null>(null);
    const [svn, setSvn] = useState<ClientStatus | null>(null);
	const [vcsClientAction, setVcsClientAction] = useState<VCSClientAction>('');
	const [credentials, setCredentials] = useState<Credential[]>([]);
	const [credentialBindings, setCredentialBindings] = useState<Record<string, string>>({});
	const [credentialReturnToMapping, setCredentialReturnToMapping] = useState(false);
	const [mappingDraftBeforeCredentials, setMappingDraftBeforeCredentials] = useState<Record<string, unknown> | null>(null);
	const [operation, setOperation] = useState<{ action: 'sync' | 'commit' | 'push' | 'commit_push' | 'revert'; message: string; preview?: any } | null>(null);
    const [operationResult, setOperationResult] = useState<any>(null);
	const [operationCancelPending, setOperationCancelPending] = useState(false);
    const [retryNodeIds, setRetryNodeIds] = useState<string[]>([]);
    const [directoryStats, setDirectoryStats] = useState<any>(null);
    const [expanded, setExpanded] = useState<Record<string, boolean>>({});
	    const [remoteConnection, setRemoteConnection] = useState<any>(null);
		const [migrationDestination, setMigrationDestination] = useState('');
		const [migrationPreview, setMigrationPreview] = useState<RootMigrationPreview | null>(null);
		const [migrationComplete, setMigrationComplete] = useState(false);
		const [rootBindingDestination, setRootBindingDestination] = useState('');
    const [startingRepositoryID, setStartingRepositoryID] = useState('');
	const [openingRepositoryKey, setOpeningRepositoryKey] = useState('');
	const [repositoryQuery, setRepositoryQuery] = useState('');
	const deferredRepositoryQuery = useDeferredValue(repositoryQuery);
	const [removedRepositoryIDs, setRemovedRepositoryIDs] = useState<Set<string>>(() => new Set());
	const [repositoryContextMenu, setRepositoryContextMenu] = useState<RepositoryContextMenu | null>(null);
	const [syncStatus, setSyncStatus] = useState<'idle' | 'syncing' | 'success' | 'conflict' | 'error'>('idle');
	const [syncMessage, setSyncMessage] = useState('');
	const [backgroundSyncPending, setBackgroundSyncPending] = useState(false);
	const [backgroundSyncStateReady, setBackgroundSyncStateReady] = useState(false);
	const [backgroundSyncPhase, setBackgroundSyncPhase] = useState<BackgroundSyncPhase>('idle');
	const [backgroundSyncDetail, setBackgroundSyncDetail] = useState('');
	const [backgroundSyncRepairRepositoryID, setBackgroundSyncRepairRepositoryID] = useState('');
	const [backgroundSyncNextRetryAt, setBackgroundSyncNextRetryAt] = useState('');
	const [checkoutNodeID, setCheckoutNodeID] = useState('');
	    const [selectingRoot, setSelectingRoot] = useState(false);
	const [draggedNodeID, setDraggedNodeID] = useState('');
	const [dropTargetID, setDropTargetID] = useState<string | null>(null);
	const operationResultRef = useRef<any>(null);
	const busyRef = useRef(false);
	const credentialSaveInFlightRef = useRef(false);
	const credentialDeleteInFlightRef = useRef(false);
	const credentialManagerRequestRef = useRef(0);
	const mappingCredentialRequestRef = useRef(0);
	const credentialBindingsRequestRef = useRef(0);
	const repositoryDeletionRef = useRef(false);
	const checkoutInFlightRef = useRef(false);
	const vcsClientActionRef = useRef<VCSClientAction>('');
	const vcsClientStatusRequestRef = useRef<Record<VCSClientKind, number>>({ git: 0, svn: 0 });
    const codingTaskStartingRef = useRef(false);
	const directoryPickerOpenRef = useRef(false);
	const operationPreviewRequestRef = useRef(0);
	const operationStartingRef = useRef(false);
	const operationRefreshRequestRef = useRef(0);
	const statusInspectionRequestRef = useRef(0);
	const changesRequestRef = useRef(0);
	const repositorySessionRef = useRef(0);
	const recentRepositoriesRequestRef = useRef(0);
	const directoryStatsRequestRef = useRef(0);
		const remoteConnectionRequestRef = useRef(0);
		const repositoryContextMenuRef = useRef<HTMLDivElement | null>(null);
		const repositoryContextMenuActionRef = useRef<HTMLButtonElement | null>(null);
	const workspaceMountedRef = useRef(false);
	const backgroundSyncRequestRef = useRef(0);

	useEffect(() => {
		workspaceMountedRef.current = true;
		return () => {
			workspaceMountedRef.current = false;
			// Let an already-dispatched inspection finish harmlessly without writing
			// into a workspace that has since been removed from the tree.
			operationRefreshRequestRef.current += 1;
			statusInspectionRequestRef.current += 1;
		};
	}, []);

	const applyBackgroundSyncStatus = (payload: unknown) => {
		if (!payload || typeof payload !== 'object') {
			if (typeof payload === 'boolean') {
				setBackgroundSyncPending(payload);
				setBackgroundSyncPhase(payload ? 'running' : 'idle');
				if (!payload) {
					setBackgroundSyncDetail('');
					setBackgroundSyncRepairRepositoryID('');
					setBackgroundSyncNextRetryAt('');
				}
			}
			return;
		}
		const status = payload as { pending?: unknown; phase?: unknown; message?: unknown; repair_repository_id?: unknown; next_retry_at?: unknown };
		let pending = status.pending === true;
		const phaseRaw = String(status.phase || '').trim();
		const phase: BackgroundSyncPhase = BACKGROUND_SYNC_PHASES.has(phaseRaw)
			? phaseRaw as BackgroundSyncPhase
			: (pending ? 'running' : 'idle');
		// Mirror backend snapshot coherence: UI-blocking phases always disable the
		// button; wait/fail/conflict never do — even if a transitional event races.
		if (phase === 'queued' || phase === 'running') pending = true;
		else if (phase === 'retry_wait' || phase === 'failed' || phase === 'conflict') pending = false;
		setBackgroundSyncPending(pending);
		setBackgroundSyncPhase(phase);
		setBackgroundSyncDetail(String(status.message || '').trim());
		setBackgroundSyncRepairRepositoryID(typeof status.repair_repository_id === 'string' ? status.repair_repository_id.trim() : '');
		setBackgroundSyncNextRetryAt(String(status.next_retry_at || '').trim());
	};

	const parseBackgroundSyncStatusPayload = (raw: unknown): { status: unknown; pending: boolean } => {
		if (typeof raw === 'string') {
			try {
				const status = JSON.parse(raw);
				return { status, pending: status?.pending === true };
			} catch {
				return { status: { pending: false, phase: 'idle' }, pending: false };
			}
		}
		if (raw && typeof raw === 'object') {
			return { status: raw, pending: (raw as { pending?: unknown }).pending === true };
		}
		return { status: { pending: false, phase: 'idle' }, pending: false };
	};

	useEffect(() => {
		const backend = app();
		let unsubscribe: (() => void) | undefined;
		try {
			// Subscribe before reading the initial state. Otherwise a scheduled sync
			// can begin after the read has taken its snapshot but before this page is
			// listening, which incorrectly re-enables the manual button.
			unsubscribe = EventsOn('virtual-repository:background-sync', (payload: unknown) => {
				if (!workspaceMountedRef.current || !payload || typeof payload !== 'object') return;
				// A live event is newer than the startup query. Prevent a late
				// false response from re-enabling the manual button mid-sync.
				backgroundSyncRequestRef.current += 1;
				applyBackgroundSyncStatus(payload);
				setBackgroundSyncStateReady(true);
			});
		} catch { /* The initial query below keeps the control usable if events are unavailable. */ }
		const requestID = ++backgroundSyncRequestRef.current;
		const loadStatus = async () => {
			try {
				if (typeof backend?.GetVirtualRepositoryBackgroundSyncStatus === 'function') {
					const raw = await backend.GetVirtualRepositoryBackgroundSyncStatus();
					if (!workspaceMountedRef.current || requestID !== backgroundSyncRequestRef.current) return;
					applyBackgroundSyncStatus(parseBackgroundSyncStatusPayload(raw).status);
					setBackgroundSyncStateReady(true);
					return;
				}
				const pending = await backend?.IsVirtualRepositoryBackgroundSyncPending?.();
				if (!workspaceMountedRef.current || requestID !== backgroundSyncRequestRef.current) return;
				applyBackgroundSyncStatus(pending === true);
				setBackgroundSyncStateReady(true);
			} catch {
				if (workspaceMountedRef.current && requestID === backgroundSyncRequestRef.current) setBackgroundSyncStateReady(true);
			}
		};
		void loadStatus();
		return unsubscribe;
	}, []);

	useEffect(() => {
		if (!backgroundSyncPending) return;
		const backend = app();
		const getStatus = backend?.GetVirtualRepositoryBackgroundSyncStatus;
		const getPending = backend?.IsVirtualRepositoryBackgroundSyncPending;
		if (typeof getStatus !== 'function' && typeof getPending !== 'function') return;
		let disposed = false;
		let timer: number | undefined;
		const check = async () => {
			const requestID = backgroundSyncRequestRef.current;
			try {
				if (typeof getStatus === 'function') {
					const raw = await getStatus();
					if (disposed || !workspaceMountedRef.current || requestID !== backgroundSyncRequestRef.current) return;
					const parsed = parseBackgroundSyncStatusPayload(raw);
					applyBackgroundSyncStatus(parsed.status);
					setBackgroundSyncStateReady(true);
					if (parsed.pending) timer = window.setTimeout(() => { void check(); }, 1000);
					return;
				}
				const pending = await getPending();
				// A newer event or busy response is authoritative. Do not let this
				// older poll re-enable the manual action over a newly queued sync.
				if (disposed || !workspaceMountedRef.current || requestID !== backgroundSyncRequestRef.current) return;
				applyBackgroundSyncStatus(pending === true);
				setBackgroundSyncStateReady(true);
				if (pending === true) timer = window.setTimeout(() => { void check(); }, 1000);
			} catch {
				// Events normally provide the immediate completion signal. If an event
				// was lost, keep checking rather than leaving the manual action disabled.
				if (!disposed) timer = window.setTimeout(() => { void check(); }, 2000);
			}
		};
		// Do not query in the same turn as a busy response: let the queued job
		// establish its backend-visible state first.
		timer = window.setTimeout(() => { void check(); }, 1000);
		return () => {
			disposed = true;
			if (timer !== undefined) window.clearTimeout(timer);
		};
	}, [backgroundSyncPending]);

    const operationRunning = operationResult?.status === 'running';
    const mutationLocked = busy || operationRunning;
    busyRef.current = busy;

	const backgroundStatusVisible = backgroundSyncPhase === 'retry_wait' || backgroundSyncPhase === 'failed' || backgroundSyncPhase === 'conflict';
	const syncButtonLabel = syncStatus === 'syncing'
		? text.syncing
		: !backgroundSyncStateReady
			? text.checkingSync
			: backgroundSyncPending
				? text.backgroundSyncing
				: text.syncNow;
	const formatBackgroundSyncDetail = () => {
		const detail = localizeVRepoError(backgroundSyncDetail, isZh);
		const sep = isZh ? '：' : ': ';
		if (backgroundSyncPhase === 'retry_wait') {
			const when = backgroundSyncNextRetryAt ? (() => { try { return new Date(backgroundSyncNextRetryAt).toLocaleString(); } catch { return backgroundSyncNextRetryAt; } })() : '';
			const base = detail ? `${text.backgroundSyncRetry}${sep}${detail}` : text.backgroundSyncRetry;
			return when ? `${base} · ${when}` : base;
		}
		if (backgroundSyncPhase === 'failed') {
			return detail ? `${text.backgroundSyncFailed}${sep}${detail}` : text.backgroundSyncFailed;
		}
		if (backgroundSyncPhase === 'conflict') {
			return detail ? `${text.backgroundSyncConflict}${sep}${detail}` : text.backgroundSyncConflict;
		}
		return detail;
	};
	const syncStatusBannerClass = syncStatus === 'idle'
		? (backgroundSyncPending ? 'syncing' : backgroundSyncPhase === 'retry_wait' ? 'retry' : backgroundSyncPhase === 'failed' || backgroundSyncPhase === 'conflict' ? 'error' : 'syncing')
		: syncStatus;
	const syncStatusBannerText = syncStatus === 'syncing'
		? text.syncing
		: !backgroundSyncStateReady
			? text.checkingSync
			: backgroundSyncPending
				? text.backgroundSyncing
				: backgroundStatusVisible
					? formatBackgroundSyncDetail()
					: (syncMessage || text.syncReady);
	const backgroundSyncRepairRepository = useMemo(() => {
		if (backgroundSyncPhase !== 'retry_wait' && backgroundSyncPhase !== 'failed') return null;
		if (backgroundSyncRepairRepositoryID) {
			return repos.find((item) => item?.remote && String(item?.id || '').trim() === backgroundSyncRepairRepositoryID) || null;
		}
		// Compatibility for desktop builds predating repair_repository_id. Avoid
		// guessing when duplicate display names exist; the repair action must
		// always target one known remote endpoint.
		const message = backgroundSyncDetail.trim();
		// Sync failures identify the unavailable repository as
		// `read virtual repository "name" for sync: ...`. Only offer repair for
		// a known remote entry, so a generic Hub or local-root problem never opens
		// an unrelated connection editor.
		const match = /read virtual repository\s+["“]([^"”]+)["”]\s+for sync:/i.exec(message);
		if (!match) return null;
		const name = match[1].trim();
		const matches = repos.filter((item) => item?.remote && String(item?.name || '').trim() === name);
		return matches.length === 1 ? matches[0] : null;
	}, [backgroundSyncDetail, backgroundSyncPhase, backgroundSyncRepairRepositoryID, repos]);
	const openBackgroundSyncRepair = () => {
		const target = backgroundSyncRepairRepository;
		if (!target || mutationLocked) return;
		setRemoteConnection(null);
		// The recent-repository index deliberately omits the remote manifest's
		// revision. Saving that list entry after a successful repair therefore
		// fails the backend compare-and-swap check. Connection repair only needs
		// a probe, so keep this draft separate from a saveable repository edit.
		setDraft({ ...target, location: 'remote', repair_only: true, ssh_host: target.remote?.host || '', ssh_port: target.remote?.port || 22, ssh_user: target.remote?.user || '', ssh_password: '' });
		setMode('repo');
	};
	const clearBackgroundSyncRepairForRepository = (repositoryID: unknown) => {
		if (String(repositoryID || '').trim() !== backgroundSyncRepairRepositoryID) return;
		setBackgroundSyncRepairRepositoryID('');
		setBackgroundSyncDetail('');
		setBackgroundSyncNextRetryAt('');
		setBackgroundSyncPhase('idle');
	};

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
					return <div className="vrepo-repository-list__record" key={itemKey} onContextMenu={(event) => (item.unbound || item.root_repair) ? event.preventDefault() : openRepositoryContextMenu(event, item, itemKey)}>
						<button className={`vrepo-repository-list__item${active ? ' is-active' : ''}`} type="button" title={`${tips.openRecent}${item.name ? ` · ${item.name}` : ''}`} data-vrepo-repository-key={itemKey} aria-pressed={active} aria-haspopup="menu" aria-expanded={repositoryContextMenu?.key === itemKey} disabled={!!openingRepositoryKey || selectingRoot || mutationLocked || item.unbound || item.root_repair} onClick={() => void openRecentRepository(item)} onKeyDown={(event) => {
							if (event.key !== 'ContextMenu' && !(event.shiftKey && event.key === 'F10')) return;
							event.preventDefault();
							const bounds = event.currentTarget.getBoundingClientRect();
							if (!item.unbound && !item.root_repair) openRepositoryContextMenu(event, item, itemKey, { x: bounds.left + Math.min(bounds.width / 2, 40), y: bounds.top + Math.min(bounds.height, 36) });
						}}>
						<span className="vrepo-repository-list__item-name"><strong>{item.name}</strong><span className="vrepo-repository-list__item-label"><em>{item.remote ? text.remoteRepository : text.localRepository}</em>{item.unbound || item.root_repair ? <span className="vrepo-repository-list__unavailable" role="status">{text.locationUnavailable}</span> : null}</span></span>
						<span className="vrepo-repository-list__item-path">{opening ? text.loading : item.unbound ? text.locationUnavailableHint : item.root_repair ? text.rootRepairListHint : item.remote ? `${item.remote.user}@${item.remote.host}` : item.root_path}</span>
						<span className="vrepo-repository-list__item-meta">{mappingCount} {text.mappings}</span>
					</button>
					{item.unbound || item.root_repair ? <button type="button" className="vrepo-repository-list__task secondary" title={item.root_repair ? tips.reconnectRoot : tips.setLocalRoot} disabled={mutationLocked || selectingRoot} onClick={() => openRootBinding(item)}>{item.root_repair ? text.reconnectLocalRoot : text.setLocalRoot}</button> : null}
				</div>;
			})}
			{!filteredRepositories.length ? <p className="vrepo-repository-list__empty" role="status">{text.noSearchResults}</p> : null}
		</div>
	</aside> : null;

	const setActiveRepository = (nextRepository: VRepo, resetTreeState = false) => {
		repositorySessionRef.current += 1;
		changesRequestRef.current += 1;
		setChanges(null);
		setChangesNodeID('');
		setChangesFilePath('');
		setChangesLoading(false);
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
			x: Math.max(8, Math.min(x, window.innerWidth - repositoryContextMenuWidth - 8)),
			y: Math.max(8, Math.min(y, window.innerHeight - repositoryContextMenuHeight - 8)),
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
		if (syncStatus === 'syncing' || backgroundSyncPending || !backgroundSyncStateReady) return;
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
				if (result?.status === 'busy') {
					// The backend received a click in the narrow interval before its
					// background-sync event reached this page. Mirror that authoritative
					// state immediately. Invalidate any older initial/poll response that
					// may otherwise re-enable the button before the completion event.
					backgroundSyncRequestRef.current += 1;
					applyBackgroundSyncStatus({ pending: true, phase: 'running', message: result?.message || '' });
					setSyncStatus('idle');
					setSyncMessage('');
					return;
				}
				// The backend clears a failed/retry background state as soon as a
				// manual sync takes over. Mirror that acknowledgement here as well:
				// an older desktop can lose the accompanying Wails event, which used
				// to leave a stale repair button beside a successful manual sync.
				applyBackgroundSyncStatus({ pending: false, phase: 'idle' });
				if (result?.status === 'success') {
					setSyncStatus('success');
					setSyncMessage(result?.last_synced_at ? `${text.syncSuccess} · ${new Date(result.last_synced_at).toLocaleString()}` : text.syncSuccess);
					await loadRecent();
					return;
				}
				if (result?.status !== 'conflict') throw new Error(result?.message || 'Virtual repository sync returned an invalid status');
				const conflicts = Array.isArray(result.conflicts) ? result.conflicts : [];
				// Pure Hub revision race after the backend's bounded retries.
				// Prefer structured reason; empty conflicts remains a fallback for older backends.
				if (result?.reason === 'revision_race' || !conflicts.length) {
					setSyncStatus('error');
					setSyncMessage(localizeVRepoError(result?.message || text.syncConflict, isZh));
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
			setSyncMessage(localizeVRepoError(e, isZh));
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
		const requestID = ++vcsClientStatusRequestRef.current.svn;
        try {
            const raw = search && backend.SearchVCSClient ? await backend.SearchVCSClient('svn') : await backend.GetVCSClientStatus?.('svn');
			if (requestID === vcsClientStatusRequestRef.current.svn) setSvn(parseJSON(raw, { kind: 'svn', available: false }));
        } catch (e: any) { if (requestID === vcsClientStatusRequestRef.current.svn) { setSvn({ kind: 'svn', available: false, error: String(e?.message || e) }); setError(String(e?.message || e)); } }
    }, []);

    const loadGit = useCallback(async (search = false) => {
        const backend = app();
        if (!backend) return;
		const requestID = ++vcsClientStatusRequestRef.current.git;
        try {
            const raw = search && backend.SearchVCSClient ? await backend.SearchVCSClient('git') : await backend.GetVCSClientStatus?.('git');
			if (requestID === vcsClientStatusRequestRef.current.git) setGit(parseJSON(raw, { kind: 'git', available: false }));
        } catch (e: any) { if (requestID === vcsClientStatusRequestRef.current.git) { setGit({ kind: 'git', available: false, error: String(e?.message || e) }); setError(String(e?.message || e)); } }
    }, []);

    useEffect(() => { void loadRecent(); void loadGit(); void loadSVN(); }, [loadGit, loadRecent, loadSVN]);

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

	const selectMigrationDestination = async () => {
		if (!repo || repo.remote || directoryPickerOpenRef.current) return;
		directoryPickerOpenRef.current = true;
		setSelectingRoot(true);
		try {
			const destination = await app()?.SelectVirtualRepositoryRoot?.(migrationDestination || repo.root_path);
			if (destination) {
				setMigrationDestination(destination);
				setMigrationPreview(null);
				setMigrationComplete(false);
			}
		} catch (e: any) { setError(errorMessage(e)); }
		finally { directoryPickerOpenRef.current = false; setSelectingRoot(false); }
	};

	const openRootBinding = (item: VRepo) => {
		if (!item?.id || (!item.unbound && !item.root_repair) || item.remote || mutationLocked) return;
		setRepo(item);
		setRootBindingDestination('');
		setError('');
		setMode('root-binding');
	};

	const selectRootBindingDestination = async () => {
		if (directoryPickerOpenRef.current) return;
		directoryPickerOpenRef.current = true;
		setSelectingRoot(true);
		try {
			const destination = await app()?.SelectVirtualRepositoryRoot?.(rootBindingDestination);
			if (destination) setRootBindingDestination(destination);
		} catch (e: any) { setError(errorMessage(e)); }
		finally { directoryPickerOpenRef.current = false; setSelectingRoot(false); }
	};

	const bindLocalRoot = async () => {
		if (!repo?.id || (!repo.unbound && !repo.root_repair) || !rootBindingDestination.trim() || busyRef.current) return;
		setBusy(true); setError('');
		try {
			const bound = parseRequiredJSON<VRepo>(await app()?.BindVirtualRepositoryRoot?.(JSON.stringify({ repository_id: repo.id, root_path: rootBindingDestination })), 'Local root binding');
			setActiveRepository(bound, true);
			setMode('none');
			await loadRecent();
			void inspectRepositoryStatusesInBackground(bound, repositorySessionRef.current);
		} catch (e: any) { setError(errorMessage(e)); }
		finally { setBusy(false); }
	};

	const openRootMigration = () => {
		if (!repo || mutationLocked) return;
		setError('');
		setMigrationDestination('');
		setMigrationPreview(null);
		setMigrationComplete(false);
		setMode('migration');
	};

	const migrationPayload = () => JSON.stringify({ repository_id: repo?.id || '', destination_root: migrationDestination });

	const previewRootMigration = async () => {
		if (!repo || busyRef.current) return;
		if (!migrationDestination.trim()) { setError(text.chooseNewRoot); return; }
		setBusy(true); setError(''); setMigrationPreview(null); setMigrationComplete(false);
		try {
			const preview = parseRootMigrationPreview(await app()?.PreviewVirtualRepositoryRootMigration?.(migrationPayload()), 'Migration preflight');
			setMigrationPreview(preview);
			if (!preview.can_migrate) setError(preview.reason || text.migrationConflict);
		} catch (e: any) { setError(errorMessage(e)); }
		finally { setBusy(false); }
	};

	const migrateRoot = async () => {
		if (!repo || !migrationPreview?.can_migrate || busyRef.current) return;
		const confirmed = await showConfirm(
			isZh ? `将“${repo.name}”迁移到\n${migrationPreview.destination_root}\n\n文件会先复制并校验，成功后才切换仓库位置。旧根目录不会自动删除。` : `Move “${repo.name}” to\n${migrationPreview.destination_root}\n\nFiles are copied and verified before the repository location switches. The old root is not deleted.`,
			text.moveRoot,
			{ confirmText: text.migrateNow, cancelText: text.cancel },
		);
		if (!confirmed) return;
		setBusy(true); setError('');
		try {
			const migrated = parseRequiredJSON<VRepo>(await app()?.MigrateVirtualRepositoryRoot?.(migrationPayload()), 'Root migration');
			setActiveRepository(migrated, true);
			setMigrationComplete(true);
			setMigrationPreview(null);
			await loadRecent();
			void inspectRepositoryStatusesInBackground(migrated, repositorySessionRef.current);
		} catch (e: any) { setError(errorMessage(e)); }
		finally { setBusy(false); }
	};

	const remoteConnectionPayload = (trustHostKey = !!draft.trust_host_key) => JSON.stringify({ repository_id: draft.id || '', remote: { host: draft.ssh_host || '', port: Number(draft.ssh_port || 22), user: draft.ssh_user || '' }, root_path: draft.root_path || '', password: draft.ssh_password || '', trust_host_key: trustHostKey });

	    const testRemoteConnection = async (trustHostKey = !!draft.trust_host_key) => {
		if (busyRef.current) return;
		busyRef.current = true;
		const request = ++remoteConnectionRequestRef.current;
		const payload = remoteConnectionPayload(trustHostKey);
	        setBusy(true); setError(''); setRemoteConnection(null);
		try {
			const backend = app();
			const response = draft.repair_only
				? await backend?.RepairRemoteVirtualRepositoryConnection?.(payload)
				: await backend?.TestRemoteVirtualRepositoryConnection?.(payload);
			const status = parseRequiredJSON<any>(response, 'Connection test');
			if (request !== remoteConnectionRequestRef.current) return;
	            setRemoteConnection(status);
	            if (status?.error_code && status.error_code !== 'host_key_untrusted' && status.error_code !== 'host_key_changed') setError(status.error || status.error_code);
	        } catch (e: any) { if (request === remoteConnectionRequestRef.current) setError(String(e?.message || e)); } finally { busyRef.current = false; setBusy(false); }
	    };

	const resetRemoteHostKey = async () => {
		if (!draft.id || busyRef.current) return;
		const confirmed = await showConfirm(text.removeSavedHostKeyConfirm, text.removeSavedHostKey, { confirmText: text.removeSavedHostKey, cancelText: text.cancel, confirmVariant: 'danger' });
		if (!confirmed) return;
		busyRef.current = true;
		setBusy(true); setError('');
		let reset = false;
		try {
			await app()?.ResetRemoteVirtualRepositoryHostKey?.(draft.id);
			setDraft((current: any) => ({ ...current, trust_host_key: false }));
			setRemoteConnection(null);
			reset = true;
		} catch (e: any) { setError(String(e?.message || e)); }
		finally { busyRef.current = false; setBusy(false); }
		if (reset) await testRemoteConnection(false);
	};

	const createRemoteRoot = async () => {
		if (draft.repair_only) return;
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
			setActiveRepository(opened, true); setSelectedId(''); setStatuses({}); setDirectoryStats(null);
			const repositorySession = repositorySessionRef.current;
			setError('');
			inspectRepositoryStatusesInBackground(opened, repositorySession);
			await loadRecent();
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
			const repositorySession = repositorySessionRef.current;
			setError('');
			inspectRepositoryStatusesInBackground(opened, repositorySession);
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
			setActiveRepository(saved); clearBackgroundSyncRepairForRepository(saved.id); setMode('none'); setDraft({}); await loadRecent();
        } catch (e: any) { setError(String(e?.message || e)); } finally { setBusy(false); }
    };

	const inspectRepositoryStatuses = useCallback(async (targetRepository: VRepo, repositorySession = repositorySessionRef.current) => {
		const requestID = ++statusInspectionRequestRef.current;
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
		if (list.length !== mappingKinds.size || list.some((status) => mappingKinds.get(status.node_id) !== status.kind)) {
			throw new Error('Inspect virtual repository returned a status for an unknown mapping');
		}
		const nextStatuses = Object.fromEntries(list.map((item) => [item.node_id, item]));
		if (workspaceMountedRef.current && requestID === statusInspectionRequestRef.current && repositorySession === repositorySessionRef.current) {
			setStatuses(nextStatuses);
		}
		return nextStatuses;
	}, []);

	const inspectRepositoryStatusesInBackground = useCallback((targetRepository: VRepo, repositorySession = repositorySessionRef.current) => {
		const requestID = statusInspectionRequestRef.current + 1;
		void inspectRepositoryStatuses(targetRepository, repositorySession).catch((inspectionError) => {
			if (
				workspaceMountedRef.current
				&& requestID === statusInspectionRequestRef.current
				&& repositorySession === repositorySessionRef.current
			) {
				setError(localizeVRepoError(inspectionError, isZh));
			}
		});
	}, [inspectRepositoryStatuses, isZh]);

	const refreshStatus = useCallback(async () => {
		if (!repo) return;
		const repositorySession = repositorySessionRef.current;
		setBusy(true); setError('');
		try {
			await inspectRepositoryStatuses(repo, repositorySession);
		} catch (e: any) {
			if (workspaceMountedRef.current && repositorySession === repositorySessionRef.current) setError(String(e?.message || e));
		} finally {
			if (workspaceMountedRef.current && repositorySession === repositorySessionRef.current) setBusy(false);
		}
	}, [repo, inspectRepositoryStatuses]);

	const refreshStatusAfterOperation = useCallback(async () => {
		const requestID = ++operationRefreshRequestRef.current;
		if (!repo) return;
		const repositorySession = repositorySessionRef.current;
		try {
			await inspectRepositoryStatuses(repo, repositorySession);
		} catch (e: any) {
			if (workspaceMountedRef.current && requestID === operationRefreshRequestRef.current && repositorySession === repositorySessionRef.current) {
				setError(String(e?.message || e));
			}
		}
	}, [repo, inspectRepositoryStatuses]);

	const loadChanges = useCallback(async (targetRepository: VRepo, nodeID: string, filePath = '', repositorySession = repositorySessionRef.current) => {
		const backend = app();
		if (typeof backend?.GetVirtualRepositoryChanges !== 'function') throw new Error(isZh ? '当前版本不支持查看 Git 变更。' : 'This version does not support Git changes.');
		const requestID = ++changesRequestRef.current;
		setChangesLoading(true);
		try {
			const raw = await backend.GetVirtualRepositoryChanges(JSON.stringify({
				repository_id: targetRepository.remote ? targetRepository.id || '' : '',
				root_path: targetRepository.remote ? '' : targetRepository.root_path,
				node_id: nodeID,
				file_path: filePath,
			}));
			const next = parseRepositoryChanges(raw, 'Virtual repository changes');
			if (next.node_id !== nodeID) throw new Error('Virtual repository changes returned a different mapping');
			if (workspaceMountedRef.current && requestID === changesRequestRef.current && repositorySession === repositorySessionRef.current) {
				setChanges(next);
				setChangesNodeID(nodeID);
				setChangesFilePath(filePath);
			}
		} finally {
			if (workspaceMountedRef.current && requestID === changesRequestRef.current && repositorySession === repositorySessionRef.current) setChangesLoading(false);
		}
	}, [isZh]);

	const openChanges = useCallback(async () => {
		const selectedMapping = repo?.nodes.find((node) => node.id === selectedId);
		if (!repo || !selectedMapping?.repository || selectedMapping.repository.kind !== 'git' || !statuses[selectedMapping.id]?.is_repository) {
			setError(text.changesUnavailable);
			return;
		}
		setError('');
		try {
			await loadChanges(repo, selectedMapping.id, '', repositorySessionRef.current);
		} catch (changeError) {
			setError(localizeVRepoError(changeError, isZh));
		}
	}, [repo, selectedId, statuses, loadChanges, text.changesUnavailable, isZh]);

	const selectChangeFile = useCallback(async (filePath: string) => {
		if (!repo || !changesNodeID) return;
		setError('');
		try {
			await loadChanges(repo, changesNodeID, filePath, repositorySessionRef.current);
		} catch (changeError) {
			setError(localizeVRepoError(changeError, isZh));
		}
	}, [repo, changesNodeID, loadChanges, isZh]);

	const checkoutRepositoryNode = async (targetRepository: VRepo, targetNodeID: string) => {
		if (!targetRepository.id) throw new Error(isZh ? '虚拟仓库 ID 缺失，无法检出。' : 'The virtual repository id is missing, so checkout cannot start.');
		const checkout = targetRepository.remote ? app()?.CheckoutRemoteVirtualRepositoryNode : app()?.CheckoutVirtualRepositoryNode;
		if (typeof checkout !== 'function') throw new Error(isZh ? '当前版本不支持检出虚拟仓库。' : 'This version does not support virtual repository checkout.');
		await checkout(targetRepository.id, targetNodeID);
	};

	const checkoutSelectedRepositoryNode = async (targetRepository: VRepo, targetNodeID: string) => {
		// State updates do not take effect until the next render. Keep a synchronous
		// guard too, so a double click in the same event turn cannot start two clones.
		if (checkoutInFlightRef.current || busyRef.current || mutationLocked) return;
		const repositorySession = repositorySessionRef.current;
		checkoutInFlightRef.current = true;
		setCheckoutNodeID(targetNodeID);
		setBusy(true);
		setError('');
		try {
			await checkoutRepositoryNode(targetRepository, targetNodeID);
			// Refresh precisely the repository that was checked out. Calling the
			// render-bound refreshStatus here could instead inspect a repository the
			// user selected while the clone was still running.
			if (workspaceMountedRef.current && repositorySession === repositorySessionRef.current) {
				await inspectRepositoryStatuses(targetRepository, repositorySession);
			}
		} catch (e: any) {
			if (workspaceMountedRef.current && repositorySession === repositorySessionRef.current) setError(localizeVRepoError(e, isZh));
		} finally {
			checkoutInFlightRef.current = false;
			// A later operation may have started while the checkout was awaiting its
			// final status refresh. Do not clear that operation's busy/progress state.
			if (workspaceMountedRef.current && repositorySession === repositorySessionRef.current) {
				setCheckoutNodeID('');
				if (operationResultRef.current?.status !== 'running') setBusy(false);
			}
		}
	};

	useEffect(() => {
        try {
			return EventsOn('virtual-repository:job-updated', (raw: unknown) => {
				if (!workspaceMountedRef.current) return;
				let result: any;
				try { result = parseOperationResult(raw, 'Operation update', operationResultRef.current?.job_id); } catch { return; }
				if (!operationResultRef.current?.job_id) return;
				if (!shouldAcceptOperationResult(operationResultRef.current, result)) return;
                operationResultRef.current = result;
                setOperationResult(result);
                if (result.status !== 'running') {
					setOperationCancelPending(false);
                    setBusy(false);
                    void refreshStatusAfterOperation();
                }
            });
        } catch { return undefined; }
	}, [refreshStatusAfterOperation]);

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
			// Local mappings are directories rather than checkouts. Always materialize
			// them after saving so an intentionally empty local mapping is usable.
			if (mode === 'mapping' && (draft.create_directory || draft.kind === 'local')) {
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
			{hasChildren ? <button type="button" tabIndex={-1} className="vrepo-tree__chevron" aria-label={`${isExpanded ? tips.collapse : tips.expand} ${node.name}`} title={`${isExpanded ? tips.collapse : tips.expand} ${node.name}`} aria-expanded={isExpanded} onClick={(event) => { event.stopPropagation(); toggleTreeNode(node.id, isExpanded); }}>{isExpanded ? '▾' : '▸'}</button> : <span className="vrepo-tree__chevron" aria-hidden />}
                    <span className={`vrepo-tree__icon${node.repository ? ` is-${node.repository.kind}` : ''}`}>{treeNodeIcon(node.repository?.kind)}</span>
                    <span className={`vrepo-tree__label${selectedId === node.id ? ' is-selected' : ''}${draggedNodeID === node.id ? ' is-dragging' : ''}${dropTargetID === node.id ? ' is-drop-target' : ''}`} title={node.repository?.description || undefined}>{node.name}</span>
				{status ? <span className={`vrepo-tree__state ${status.error ? 'is-error' : status.clean ? 'is-clean' : 'is-dirty'}`} role="img" aria-label={status.error ? `${text.errorStatus}: ${status.error}` : status.clean ? text.cleanStatus : text.changedStatus}>{status.error ? '!' : status.clean ? '✓' : '●'}</span> : null}
	            </div>
		{isExpanded ? renderNodes(node.id, depth + 1, [...ancestorHasNext, depth > 0 && !isLast]) : null}
        </div>;
    });

    const selected = repo?.nodes.find((node) => node.id === selectedId);
    const selectedStatus = selected ? statuses[selected.id] : undefined;
	const checkingOut = Boolean(selected && checkoutNodeID === selected.id);
	const selectedIsCheckedOut = Boolean(selectedStatus?.is_repository);
	const selectedStatusLabel = selectedStatus?.error_code === 'not_checked_out'
		? text.notCheckedOut
		: selectedStatus?.error
			? localizeVRepoError(selectedStatus.error, isZh)
			: selectedIsCheckedOut
				? selectedStatus?.clean ? text.checkedOutClean : text.checkedOutChanged
				: selectedStatus ? selectedStatus.clean ? text.clean : text.changed : text.pendingStatus;
	const changesOpen = Boolean(changesNodeID && changesNodeID === selected?.id);
	const changeLabel = (file: ChangeFile) => {
		if (file.index_status === '?' && file.worktree_status === '?') return text.untracked;
		if (['DD', 'AU', 'UD', 'UA', 'DU', 'AA', 'UU'].includes(`${file.index_status}${file.worktree_status}`)) return text.conflict;
		if (file.index_status === 'R' || file.worktree_status === 'R' || file.index_status === 'C' || file.worktree_status === 'C') return text.renamed;
		if (file.index_status === 'D' || file.worktree_status === 'D') return text.deleted;
		if (file.index_status !== ' ') return text.staged;
		return text.modified;
	};

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

    const specifiedClientPath = async (kind: 'git' | 'svn') => {
        const backend = app();
        try {
            return await backend?.VCSClientExecutableHint?.(kind) || '';
        } catch { return ''; }
    };

	const beginVCSClientAction = (action: Exclude<VCSClientAction, ''>) => {
		if (vcsClientActionRef.current) return false;
		vcsClientActionRef.current = action;
		setVcsClientAction(action);
		return true;
	};

	const finishVCSClientAction = (action: Exclude<VCSClientAction, ''>) => {
		if (vcsClientActionRef.current !== action) return;
		vcsClientActionRef.current = '';
		setVcsClientAction('');
	};

	const vcsClientActionInFlight = vcsClientAction !== '';

    const specifySVN = async () => {
		const action = 'svn-select' as const;
		if (mutationLocked || !beginVCSClientAction(action)) return;
        const backend = app();
		const requestID = ++vcsClientStatusRequestRef.current.svn;
		setError('');
        try {
            const path = await backend?.SelectVCSClientExecutable?.('svn', await specifiedClientPath('svn'));
            if (!path) return;
			const raw = await backend.SetVCSClientExecutable('svn', path);
			if (requestID === vcsClientStatusRequestRef.current.svn) setSvn(parseJSON(raw, { kind: 'svn', available: false }));
		} catch (e: any) { if (requestID === vcsClientStatusRequestRef.current.svn) setError(String(e?.message || e)); } finally { finishVCSClientAction(action); }
    };

    const specifyGit = async () => {
		const action = 'git-select' as const;
		if (mutationLocked || !beginVCSClientAction(action)) return;
        const backend = app();
		const requestID = ++vcsClientStatusRequestRef.current.git;
		setError('');
        try {
            const path = await backend?.SelectVCSClientExecutable?.('git', await specifiedClientPath('git'));
            if (!path) return;
			const raw = await backend.SetVCSClientExecutable('git', path);
			if (requestID === vcsClientStatusRequestRef.current.git) setGit(parseJSON(raw, { kind: 'git', available: false }));
		} catch (e: any) { if (requestID === vcsClientStatusRequestRef.current.git) setError(String(e?.message || e)); } finally { finishVCSClientAction(action); }
    };

	const searchVCSClient = async (kind: 'git' | 'svn') => {
		const action: Exclude<VCSClientAction, ''> = kind === 'git' ? 'git-search' : 'svn-search';
		if (mutationLocked || !beginVCSClientAction(action)) return;
		setError('');
		try {
			if (kind === 'git') await loadGit(true);
			else await loadSVN(true);
		} finally { finishVCSClientAction(action); }
	};

	const resetVCSClientExecutable = async (kind: 'git' | 'svn') => {
		const action: Exclude<VCSClientAction, ''> = kind === 'git' ? 'git-reset' : 'svn-reset';
		if (mutationLocked || !beginVCSClientAction(action)) return;
		const backend = app();
		const requestID = ++vcsClientStatusRequestRef.current[kind];
		setError('');
		try {
			const raw = await backend?.ResetVCSClientExecutable?.(kind);
			const fallback = { kind, available: false };
			if (requestID === vcsClientStatusRequestRef.current[kind]) {
				if (kind === 'git') setGit(parseJSON(raw, fallback));
				else setSvn(parseJSON(raw, fallback));
			}
		} catch (e: any) { if (requestID === vcsClientStatusRequestRef.current[kind]) setError(String(e?.message || e)); } finally { finishVCSClientAction(action); }
	};

	const previewOperation = async (action: 'sync' | 'commit' | 'push' | 'commit_push' | 'revert') => {
        if (!repo) return;
		const requestID = ++operationPreviewRequestRef.current;
        setRetryNodeIds([]);
        const next = { action, message: '', preview: undefined as any };
        setOperation(next); operationResultRef.current = null; setOperationResult(null); setError('');
        if (action === 'commit' || action === 'commit_push') return;
        try {
			const preview = parseRequiredJSON<any>(await app()?.PreviewVirtualRepositoryOperation?.(JSON.stringify({ root_path: repo.root_path, repository_id: repo.id, node_id: selectedId, action })), 'Operation preview');
			if (operationPreviewRequestRef.current === requestID) setOperation({ ...next, preview });
		} catch (e: any) { if (operationPreviewRequestRef.current === requestID) setError(localizeVRepoError(e, isZh)); }
    };

    const loadOperationPreview = async () => {
        if (!repo || !operation) return;
		const requestID = ++operationPreviewRequestRef.current;
		setBusy(true); setError('');
        try {
			const preview = parseRequiredJSON<any>(await app()?.PreviewVirtualRepositoryOperation?.(JSON.stringify({ root_path: repo.root_path, repository_id: repo.id, node_id: selectedId, node_ids: retryNodeIds, action: operation.action, message: operation.message })), 'Operation preview');
			if (operationPreviewRequestRef.current === requestID) setOperation({ ...operation, preview });
		} catch (e: any) {
			if (operationPreviewRequestRef.current === requestID) setError(localizeVRepoError(e, isZh));
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
                void refreshStatusAfterOperation();
			}
		} catch (e: any) { setError(localizeVRepoError(e, isZh)); setBusy(false); }
		finally { operationStartingRef.current = false; }
    };

    useEffect(() => {
        if (!operationResult?.job_id || operationResult.status !== 'running') return;
        const jobID = operationResult.job_id;
		let pollInFlight = false;
		let disposed = false;
        const timer = window.setInterval(async () => {
			if (pollInFlight) return;
			pollInFlight = true;
			try {
				const result = parseOperationResult(await app()?.GetVirtualRepositoryOperation?.(jobID), 'Operation status', jobID);
				if (disposed || !workspaceMountedRef.current) return;
				if (operationResultRef.current?.job_id !== jobID) return;
				if (!shouldAcceptOperationResult(operationResultRef.current, result)) return;
                operationResultRef.current = result;
                setOperationResult(result);
				if (result.status !== 'running') { setBusy(false); setOperationCancelPending(false); window.clearInterval(timer); void refreshStatusAfterOperation(); }
			} catch (e: any) {
				if (!disposed && workspaceMountedRef.current && operationResultRef.current?.job_id === jobID) {
					setError(localizeVRepoError(e, isZh));
				}
			}
			finally { pollInFlight = false; }
        }, 750);
		return () => { disposed = true; window.clearInterval(timer); };
	}, [operationResult?.job_id, operationResult?.status, refreshStatusAfterOperation]);

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

	const closeOperation = () => {
		operationPreviewRequestRef.current++;
		setOperation(null);
		setRetryNodeIds([]);
	};

	const closeCredentialManager = () => {
		credentialManagerRequestRef.current += 1;
		const mappingDraft = mappingDraftBeforeCredentials;
		setMode(credentialReturnToMapping ? 'mapping' : 'none');
		setCredentialReturnToMapping(false);
		setMappingDraftBeforeCredentials(null);
		setDraft(mappingDraft || {});
	};

	// Editors render as modal dialogs that cover the page-level error banner, so
	// the same banner is repeated inside the open dialog to keep failures visible.
	const dialogOpen = mode !== 'none' || operation !== null;

	const openSvnInstallGuide = () => {
		try { BrowserOpenURL('https://subversion.apache.org/packages.html'); }
		catch { window.open('https://subversion.apache.org/packages.html', '_blank', 'noopener'); }
	};

	const syncConfigDisabled = mutationLocked || syncStatus === 'syncing' || backgroundSyncPending || !backgroundSyncStateReady;
	const rootSelectDisabled = mutationLocked || selectingRoot;
	const vcsClientBusy = mutationLocked || vcsClientActionInFlight;

	const renderVCSClientStatus = (kind: 'git' | 'svn', compact: boolean) => {
		const client = kind === 'git' ? git : svn;
		const label = kind === 'git' ? text.gitClient : text.svnClient;
		const missingDetail = kind === 'git' ? text.noGitClient : text.noClient;
		const statusLabel = client?.available ? text.foundClient : (compact ? text.clientNotFound : missingDetail);
		const summaryTitle = `${label}: ${client?.available ? text.foundClient : missingDetail}`;
		const searchAction = kind === 'git' ? 'git-search' : 'svn-search';
		const selectAction = kind === 'git' ? 'git-select' : 'svn-select';
		const resetAction = kind === 'git' ? 'git-reset' : 'svn-reset';
		const specifyLabel = kind === 'git' ? text.specifyGit : text.specify;
		const searchTip = kind === 'git' ? tips.searchGit : tips.searchSvn;
		const specifyTip = kind === 'git' ? tips.specifyGit : tips.specifySvn;
		const className = compact
			? `vrepo-client-status vrepo-client-status--compact${kind === 'svn' ? ' vrepo-client-status--end-aligned' : ''}`
			: 'vrepo-client-status';
		return <details className={className}>
			<summary aria-label={summaryTitle} title={summaryTitle}><strong>{label}</strong><span className={client?.available ? 'is-ready' : 'is-missing'}>{statusLabel}</span></summary>
			<div className="vrepo-client-status__body">
				{client?.available ? <span>{client.version} · <code>{client.executable}</code> ({client.source})</span> : null}
				<button type="button" className="utilities-link" title={searchTip} disabled={vcsClientBusy} aria-busy={vcsClientAction === searchAction} onClick={() => void searchVCSClient(kind)}>{vcsClientAction === searchAction ? text.loading : text.search}</button>
				<button type="button" className="utilities-link" title={specifyTip} disabled={vcsClientBusy} aria-busy={vcsClientAction === selectAction} onClick={() => void (kind === 'git' ? specifyGit() : specifySVN())}>{vcsClientAction === selectAction ? text.loading : specifyLabel}</button>
				{client?.source === 'user' ? <button type="button" className="utilities-link" title={tips.resetClient} disabled={vcsClientBusy} aria-busy={vcsClientAction === resetAction} onClick={() => void resetVCSClientExecutable(kind)}>{vcsClientAction === resetAction ? text.loading : text.resetClient}</button> : null}
				{kind === 'svn' && !client?.available ? <button type="button" className="utilities-link" title={tips.installSvn} disabled={vcsClientActionInFlight} onClick={openSvnInstallGuide}>{text.installSVN}</button> : null}
			</div>
		</details>;
	};

	// Shared by the header toolbar and the empty-state panel (order differs).
	const renderSetupCoreActions = (order: 'header' | 'empty') => {
		const syncBtn = <button type="button" className="secondary" disabled={syncConfigDisabled} title={tips.syncConfig} onClick={() => void runRepositorySync()}>{syncButtonLabel}</button>;
		const openBtn = <button type="button" className="secondary" disabled={rootSelectDisabled} title={tips.openRepo} onClick={() => void openExisting()}>{selectingRoot ? text.loading : text.openRepo}</button>;
		const newBtn = <button type="button" disabled={rootSelectDisabled} title={tips.newRepo} onClick={beginNewRepository}>{text.newRepo}</button>;
		return order === 'header' ? <>{syncBtn}{openBtn}{newBtn}</> : <>{syncBtn}{newBtn}{openBtn}</>;
	};

	const errorBanner = error ? <p className="utilities-error" role="alert">{localizeVRepoError(error, isZh)}</p> : null;

    return <div className="utilities-page vrepo-page" data-testid="virtual-repository-workspace">
        <header className="vrepo-header">
            <div><button type="button" className="utilities-link" title={tips.back} onClick={onBack}>{text.back}</button><h1>{text.title}</h1>{repo?.remote ? <p className="vrepo-remote-location">{repo.remote.user}@{repo.remote.host}:{repo.remote.port || 22} · {repo.root_path}</p> : null}</div>
			{(repo || repos.length > 0) ? <div className="vrepo-actions">
				{renderVCSClientStatus('git', true)}
				{renderVCSClientStatus('svn', true)}
				<div className="vrepo-actions__setup">
					{renderSetupCoreActions('header')}
					{repo ? <button type="button" className="secondary" disabled={mutationLocked} title={tips.moveRoot} onClick={openRootMigration}>{text.moveRoot}</button> : null}
					{repo?.remote ? <button type="button" className="secondary" disabled={mutationLocked} title={tips.editConnection} onClick={() => { setMode('repo'); setRemoteConnection(null); setDraft({ ...repo, location: 'remote', ssh_host: repo.remote?.host || '', ssh_port: repo.remote?.port || 22, ssh_user: repo.remote?.user || '', ssh_password: '' }); }}>{text.editConnection}</button> : null}
				</div>
                {repo ? <div className="vrepo-actions__group vrepo-actions__operation" role="group" aria-labelledby="vrepo-repository-actions">
					<span id="vrepo-repository-actions" className="vrepo-actions__label">{text.repositoryActions}</span>
					<div className="vrepo-actions__buttons">
					<button type="button" className={`secondary vrepo-coding-task-button${startingRepositoryID === repo.id ? ' is-loading' : ''}`} title={tips.startCoding} disabled={mutationLocked || !!startingRepositoryID} aria-busy={startingRepositoryID === repo.id} onClick={() => void startCodingTask(repo)}>
						<svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
							<path d="m8.5 8-4 4 4 4M15.5 8l4 4-4 4M13.5 5.5l-3 13" />
						</svg>
						{startingRepositoryID === repo.id ? <span className="vrepo-button-spinner" aria-hidden="true" /> : null}
						<span>{startingRepositoryID === repo.id ? text.startingCodingTask : text.startCodingTask}</span>
					</button>
                    <button type="button" className="secondary" disabled={mutationLocked} title={tips.refresh} onClick={() => void refreshStatus()}>{text.refresh}</button>
					<button type="button" className="secondary" disabled={mutationLocked} title={tips.syncWC} onClick={() => void previewOperation('sync')}>{text.syncWorkingCopies}</button>
                    <button type="button" className="secondary" disabled={mutationLocked} title={tips.commit} onClick={() => void previewOperation('commit')}>{text.commit}</button>
                    <button type="button" className="secondary" disabled={mutationLocked} title={tips.push} onClick={() => void previewOperation('push')}>{text.push}</button>
                    <button type="button" disabled={mutationLocked} title={tips.commitPush} onClick={() => void previewOperation('commit_push')}>{text.commitPush}</button>
                    <button type="button" className="danger" disabled={mutationLocked} title={tips.revert} onClick={() => void previewOperation('revert')}>{text.revert}</button>
                    <button type="button" className="secondary" disabled={mutationLocked} title={tips.credentials} onClick={() => void startCredentialManager()}>{text.credentials}</button>
                </div>
				</div> : null}
            </div> : null}
		</header>
		{syncStatus !== 'idle' || backgroundSyncPending || !backgroundSyncStateReady || backgroundStatusVisible ? <div className={`vrepo-sync-status is-${syncStatusBannerClass}`} role={syncStatus === 'error' || backgroundSyncPhase === 'failed' || backgroundSyncPhase === 'conflict' ? 'alert' : 'status'}><span>{syncStatusBannerText}</span>{backgroundSyncRepairRepository ? <button type="button" className="secondary vrepo-sync-status__repair" disabled={mutationLocked} title={text.repairRemoteConnection} onClick={openBackgroundSyncRepair}>{text.repairRemoteConnection}</button> : null}</div> : null}
		{!dialogOpen && errorBanner}
		{!repo && !repos.length ? <>
			{renderVCSClientStatus('git', false)}
			{renderVCSClientStatus('svn', false)}
		</> : null}
		{repositoryContextMenu ? <div ref={repositoryContextMenuRef} className="vrepo-repository-menu" role="menu" aria-label={repositoryContextMenu.item?.name || text.repositoryList} style={{ left: repositoryContextMenu.x, top: repositoryContextMenu.y }}>
			<div className="vrepo-repository-menu__context"><span>{text.repositoryList}</span><strong>{repositoryContextMenu.item?.name}</strong></div>
			<button ref={repositoryContextMenuActionRef} type="button" role="menuitem" className="vrepo-repository-menu__delete" disabled={mutationLocked} title={tips.deleteRepo} onClick={() => void deleteRecentRepository(repositoryContextMenu.item)}><svg viewBox="0 0 16 16" aria-hidden="true"><path d="M3 4.5h10M6.25 2.5h3.5M5 4.5l.6 8h4.8l.6-8M6.75 7v3M9.25 7v3" /></svg><span>{text.deleteRepository}</span></button>
		</div> : null}

        {!repos.length && !repo ? <section className="vrepo-empty">
            <div className="vrepo-empty__intro">
                <span className="vrepo-empty__mark" aria-hidden="true">V</span>
                <div><h2>{text.empty}</h2><p>{text.emptyHint}</p></div>
            </div>
			<div className="vrepo-empty__actions">
				{renderSetupCoreActions('empty')}
            </div>
        </section> : null}

        {mode === 'repo' ? <VRepoDialog title={draft.repair_only ? text.repairRemoteConnection : draft.id ? text.editConnection : text.newRepo} titleId="vrepo-repo-dialog-title" className="vrepo-dialog--repo" closeLabel={text.close} closeDisabled={busy || selectingRoot} onClose={() => setMode('none')} footer={<>
			<button type="button" className="secondary" title={tips.cancel} disabled={busy || selectingRoot} onClick={() => setMode('none')}>{text.cancel}</button>
			{draft.repair_only ? null : <button type="button" title={tips.save} onClick={() => void saveRepo({ ...draft, version: draft.version || 1, name: draft.name || '', root_path: draft.root_path || '', remote: draft.location === 'remote' ? { host: draft.ssh_host || '', port: Number(draft.ssh_port || 22), user: draft.ssh_user || '' } : undefined, nodes: draft.nodes || [] })} disabled={busy || selectingRoot || (draft.location === 'remote' && !(remoteConnection?.connected && remoteConnection?.host_key_trusted && remoteConnection?.root_exists && !remoteConnection?.error_code))}>{text.save}</button>}
		</>}>
			{errorBanner}
			<div className="vrepo-editor">
			{draft.repair_only ? <p className="vrepo-repair-hint" role="status">{text.repairConnectionHint}</p> : <><label>{text.name}<input value={draft.name || ''} onChange={(e) => setDraft({ ...draft, name: e.target.value })} /></label>
	            <label>{text.location}<select value={draft.location || 'local'} onChange={(e) => { remoteConnectionRequestRef.current += 1; setDraft({ ...draft, location: e.target.value }); setRemoteConnection(null); }}><option value="local">{text.localLocation}</option><option value="remote">{text.remoteLocation}</option></select></label></>}
            {draft.location === 'remote' ? <>
                <div className="vrepo-inline"><label>{text.server}<input value={draft.ssh_host || ''} readOnly={!!draft.repair_only} onChange={(e) => updateRemoteDraft({ ssh_host: e.target.value })} /></label><label>{text.port}<input type="number" min="1" max="65535" value={draft.ssh_port || 22} readOnly={!!draft.repair_only} onChange={(e) => updateRemoteDraft({ ssh_port: e.target.value })} /></label></div>
                <label>{text.sshUser}<input value={draft.ssh_user || ''} readOnly={!!draft.repair_only} onChange={(e) => updateRemoteDraft({ ssh_user: e.target.value })} /></label>
                <label>{text.sshPassword}<input type="password" autoComplete="new-password" value={draft.ssh_password || ''} placeholder={draft.id ? text.passwordHint : ''} onChange={(e) => updateRemoteDraft({ ssh_password: e.target.value })} /></label>
				<label>{text.remoteRoot}<input value={draft.root_path || ''} placeholder="/srv/workspace" readOnly={!!draft.id || !!draft.repair_only} onChange={(e) => updateRemoteDraft({ root_path: e.target.value })} />{draft.id ? <small>{text.rootManagedByMigration}</small> : null}</label>
                {remoteConnection?.error_code === 'host_key_untrusted' ? <div className="vrepo-host-key"><strong>{text.hostKeyPrompt}</strong><code>{remoteConnection.host_key_algorithm} {remoteConnection.host_key_fingerprint}</code><label className="vrepo-check"><input type="checkbox" checked={!!draft.trust_host_key} onChange={(e) => setDraft({ ...draft, trust_host_key: e.target.checked })} />{text.trustHostKey}</label></div> : null}
				{remoteConnection?.error_code === 'host_key_changed' ? <div className="vrepo-host-key" role="alert"><strong>{text.hostKeyChangedPrompt}</strong><code>{remoteConnection.host_key_algorithm} {remoteConnection.host_key_fingerprint}</code><button type="button" className="danger" disabled={busy || !draft.id} onClick={() => void resetRemoteHostKey()}>{text.removeSavedHostKey}</button></div> : null}
				{!draft.repair_only && remoteConnection?.connected && remoteConnection?.error_code === 'root_not_found' ? <div className="vrepo-root-missing" role="alert"><span>{text.rootMissingPrompt}</span><button type="button" disabled={busy} title={tips.createRemoteRoot} onClick={() => void createRemoteRoot()}>{text.createRemoteRoot}</button></div> : null}
                {remoteConnection?.connected && remoteConnection?.root_exists ? <p className="vrepo-connection-ok">{text.connected} · Git {remoteConnection.git_version || '—'} · SVN {remoteConnection.svn_version || '—'}</p> : null}
                <button type="button" className="secondary" disabled={busy} title={tips.testConn} onClick={() => void testRemoteConnection()}>{text.testConnection}</button>
            </> : <label>{text.root}<div className="vrepo-inline"><input value={draft.root_path || ''} readOnly /><button type="button" className="secondary" disabled={busy || selectingRoot} title={tips.chooseRoot} onClick={() => void selectRoot()}>{selectingRoot ? text.loading : text.choose}</button></div></label>}
			</div>
        </VRepoDialog> : null}

		{mode === 'migration' && repo ? <VRepoDialog title={text.moveRootTitle} titleId="vrepo-migration-dialog-title" className="vrepo-dialog--migration" closeLabel={text.close} closeDisabled={busy || selectingRoot} onClose={() => setMode('none')} footer={<>
			<button type="button" className="secondary" title={tips.cancel} disabled={busy || selectingRoot} onClick={() => setMode('none')}>{text.cancel}</button>
			{migrationComplete ? <button type="button" title={text.close} onClick={() => setMode('none')}>{text.close}</button> : <button type="button" title={tips.migrateNow} disabled={busy || !migrationPreview?.can_migrate} onClick={() => void migrateRoot()}>{busy ? text.migrating : text.migrateNow}</button>}
		</>}>
			{errorBanner}
			<div className="vrepo-migration">
				<p className="vrepo-migration__hint">{text.moveRootHint}</p>
				<label>{text.currentRoot}<code>{repo.root_path}</code></label>
				{repo.remote ? <label>{text.destinationRoot}<input value={migrationDestination} placeholder="/srv/workspace" onChange={(event) => { setMigrationDestination(event.target.value); setMigrationPreview(null); setMigrationComplete(false); }} /></label> : <label>{text.destinationRoot}<div className="vrepo-inline"><input value={migrationDestination} readOnly /><button type="button" className="secondary" disabled={busy || selectingRoot} title={tips.chooseDest} onClick={() => void selectMigrationDestination()}>{selectingRoot ? text.loading : text.chooseDestination}</button></div></label>}
				{!migrationComplete ? <button type="button" className="secondary" disabled={busy || !migrationDestination.trim()} title={tips.inspectMigration} onClick={() => void previewRootMigration()}>{busy ? text.loading : migrationPreview ? text.previewAgain : text.inspectMigration}</button> : <p className="vrepo-connection-ok" role="status">{text.migrationComplete}</p>}
				{migrationPreview ? <div className={`vrepo-migration__preview${migrationPreview.can_migrate ? ' is-ready' : ' is-blocked'}`}>
					<strong>{migrationPreview.can_migrate ? text.migrationReady : migrationPreview.reason || text.migrationConflict}</strong>
					<dl><dt>{text.sourceFiles}</dt><dd>{migrationPreview.source_file_count.toLocaleString()} · {migrationPreview.source_size_bytes.toLocaleString()} bytes</dd><dt>{text.destinationFiles}</dt><dd>{migrationPreview.destination_exists ? `${migrationPreview.destination_file_count.toLocaleString()} · ${migrationPreview.destination_size_bytes.toLocaleString()} bytes` : '—'}</dd></dl>
				</div> : null}
			</div>
		</VRepoDialog> : null}

		{mode === 'root-binding' && repo ? <VRepoDialog title={repo.root_repair ? text.reconnectRootTitle : text.bindLocalRootTitle} titleId="vrepo-root-binding-dialog-title" className="vrepo-dialog--migration" closeLabel={text.close} closeDisabled={busy || selectingRoot} onClose={() => setMode('none')} footer={<>
			<button type="button" className="secondary" title={tips.cancel} disabled={busy || selectingRoot} onClick={() => setMode('none')}>{text.cancel}</button>
			<button type="button" title={repo.root_repair ? tips.reconnectRoot : tips.bindRoot} disabled={busy || !rootBindingDestination.trim()} onClick={() => void bindLocalRoot()}>{busy ? text.bindingRoot : repo.root_repair ? text.reconnectRoot : text.bindLocalRoot}</button>
		</>}>
			{errorBanner}
			<div className="vrepo-migration">
				<p className="vrepo-migration__hint">{repo.root_repair ? text.reconnectRootHint : text.bindLocalRootHint}</p>
				<p className="vrepo-migration__hint">{repo.root_repair ? text.reconnectRootUnavailableHint : text.locationUnavailableHint}</p>
				<label>{text.destinationRoot}<div className="vrepo-inline"><input value={rootBindingDestination} readOnly /><button type="button" className="secondary" disabled={busy || selectingRoot} title={tips.chooseDest} onClick={() => void selectRootBindingDestination()}>{selectingRoot ? text.loading : text.chooseDestination}</button></div></label>
			</div>
		</VRepoDialog> : null}

		{(repositoryRecords.length || repo) ? <div className="vrepo-management-shell">
            {repositoryList}
            {repo ? <div className="vrepo-layout">
            <aside className="vrepo-tree" aria-label={repo.name}>
				<div className="vrepo-tree__head"><div><strong>{text.repositoryList}</strong><span>{repo.root_path}</span></div><div><button type="button" aria-label={text.addGroup} title={tips.addGroup} disabled={mutationLocked} onClick={() => { setMode('group'); setDraft({ parent_id: selected?.repository ? selected.parent_id : selectedId }); }}>+DIR</button><button type="button" aria-label={text.addRepo} title={tips.addRepo} disabled={mutationLocked} onClick={() => { if (selected && !selected.repository) { setMode('mapping'); setDraft({ ...selected, kind: 'git', enabled: true }); return; } setMode('mapping'); setDraft({ parent_id: selected?.repository ? selected.parent_id : selectedId, kind: 'git', enabled: true }); }}>+MAP</button></div></div>
                <div role="tree" aria-label={repo.name} className={dropTargetID === '' ? 'is-root-drop-target' : ''} onDragOver={(event) => { if (event.target === event.currentTarget && canMoveTreeNode(draggedNodeID, '')) { event.preventDefault(); event.dataTransfer.dropEffect = 'move'; setDropTargetID(''); } }} onDragLeave={(event) => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setDropTargetID((current) => current === '' ? null : current); }} onDrop={(event) => { if (event.target !== event.currentTarget) return; event.preventDefault(); const sourceID = draggedNodeID || event.dataTransfer.getData('text/plain'); void moveTreeNode(sourceID, ''); }}>
					<div role="treeitem" aria-level={1} aria-selected={!selectedId} aria-expanded="true" data-vrepo-tree-root tabIndex={!selectedId ? 0 : -1} className={`vrepo-tree__item vrepo-tree__root-item${!selectedId ? ' is-selected' : ''}${dropTargetID === '' ? ' is-drop-target' : ''}`} onClick={(event) => { event.currentTarget.focus(); setSelectedId(''); }} onKeyDown={(event) => { const firstNodeID = visibleNodeIDs[0]; const lastNodeID = visibleNodeIDs[visibleNodeIDs.length - 1]; if ((event.key === 'ArrowDown' || event.key === 'ArrowRight') && firstNodeID) { event.preventDefault(); setSelectedId(firstNodeID); focusTreeItem(firstNodeID); } else if (event.key === 'End' && lastNodeID) { event.preventDefault(); setSelectedId(lastNodeID); focusTreeItem(lastNodeID); } }} onDragOver={(event) => { if (canMoveTreeNode(draggedNodeID, '')) { event.preventDefault(); event.dataTransfer.dropEffect = 'move'; setDropTargetID(''); } }} onDragLeave={(event) => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setDropTargetID((current) => current === '' ? null : current); }} onDrop={(event) => { event.preventDefault(); const sourceID = draggedNodeID || event.dataTransfer.getData('text/plain'); void moveTreeNode(sourceID, ''); }}>
                        <span className="vrepo-tree__chevron" aria-hidden>▾</span><span className="vrepo-tree__icon">{treeNodeIcon()}</span><span className={`vrepo-tree__label${!selectedId ? ' is-selected' : ''}${dropTargetID === '' ? ' is-drop-target' : ''}`}>{repo.name}</span>
					</div>
					{renderNodes('', 1)}
				</div>
            </aside>
            <main className="vrepo-detail">
                {selected ? <section>
                    <div className="vrepo-detail__title"><div><span className="vrepo-detail__kind">{selected.repository?.kind.toUpperCase() || 'DIR'}</span><h2>{selected.name}</h2></div><div><button type="button" className="secondary" title={tips.editNode} disabled={mutationLocked} onClick={() => { setMode(selected.repository ? 'mapping' : 'group'); setDraft({ ...selected, ...(selected.repository || {}) }); }}>{text.edit}</button><button type="button" className="danger" title={tips.removeNode} disabled={mutationLocked} onClick={() => void removeNode(selected.id)}>{text.remove}</button></div></div>
					{selected.repository ? <><dl className="vrepo-facts">{selected.repository.remote_url ? <><dt>{text.remote}</dt><dd>{selectedStatus?.remote_url || selected.repository.remote_url}</dd></> : null}{selected.repository.ref_name ? <><dt>{selected.repository.ref_type === 'tag' ? text.tagRef : text.branchRef}</dt><dd>{selected.repository.ref_name}</dd></> : null}{selectedStatus?.branch ? <><dt>{text.branch}</dt><dd>{selectedStatus.branch}</dd></> : null}<dt>{text.status}</dt><dd>{selectedStatusLabel}</dd>{directoryStats && selected.repository.kind === 'local' ? <><dt>{text.files}</dt><dd>{directoryStats.file_count}</dd><dt>{text.size}</dt><dd>{directoryStats.size_bytes.toLocaleString()} bytes</dd></> : null}</dl><div className="vrepo-detail__actions">{!repo.remote ? <button type="button" className="secondary" title={tips.openFolder} onClick={() => void openMappedFolder()}>{text.openFolder}</button> : null}{selected.repository.kind !== 'local' && !selectedIsCheckedOut ? <button type="button" className={checkingOut ? 'is-loading' : undefined} disabled={mutationLocked} aria-busy={checkingOut} title={tips.checkout} onClick={() => void checkoutSelectedRepositoryNode(repo, selected.id)}>{checkingOut ? <span className="vrepo-button-spinner" aria-hidden="true" /> : null}<span>{checkingOut ? text.checkingOut : text.checkout}</span></button> : null}{selected.repository.kind === 'git' && selectedIsCheckedOut ? <button type="button" className="secondary" disabled={changesLoading} aria-busy={changesLoading} onClick={() => void openChanges()}>{changesLoading ? text.loadingChanges : text.changes}</button> : null}{selected.repository.kind === 'local' ? <button type="button" className="secondary" title={tips.calcSize} onClick={() => void loadDirectoryStats()}>{text.calculateSize}</button> : null}</div></> : null}
					{changesOpen ? <section className="vrepo-changes" aria-label={text.changesTitle}><header className="vrepo-changes__head"><div><h3>{text.changesTitle}</h3><p>{text.changesHint}</p></div><div><button type="button" className="secondary" disabled={changesLoading} onClick={() => void openChanges()}>{text.refreshChanges}</button><button type="button" className="secondary" onClick={() => { changesRequestRef.current += 1; setChanges(null); setChangesNodeID(''); setChangesFilePath(''); }}>{text.closeChanges}</button></div></header><div className="vrepo-changes__summary"><span>{changes?.branch || selectedStatus?.branch || 'HEAD'}</span><span>{changes?.head ? changes.head.slice(0, 8) : ''}</span></div><div className="vrepo-changes__grid"><section><h4>{text.changedFiles}</h4>{changes?.files_truncated ? <p className="vrepo-changes__notice" role="status">{text.changesTruncated}</p> : null}{changes?.files.length ? <div className="vrepo-change-list">{changes.files.map((file) => { const label = changeLabel(file); return <button type="button" key={`${file.path}\u0000${file.original_path || ''}`} className={changesFilePath === file.path ? 'is-selected' : ''} onClick={() => void selectChangeFile(file.path)}><span><strong>{file.path}</strong>{file.original_path ? <em>{file.original_path}</em> : null}</span><small className={label === text.conflict ? 'is-conflict' : undefined}>{label}</small></button>; })}</div> : <p className="vrepo-changes__empty">{text.noChanges}</p>}</section><section><h4>{text.recentCommits}</h4><div className="vrepo-commit-graph">{changes?.commits.map((commit) => <div key={commit.hash}><span className="vrepo-commit-graph__rail" aria-hidden>●</span><p><strong>{commit.subject}</strong><small>{commit.short_hash} · {commit.author} · {commit.date}{commit.decorations ? ` · ${commit.decorations}` : ''}</small></p></div>)}</div></section></div><section className="vrepo-diff"><h4>{changesFilePath || text.selectChange}</h4>{changesFilePath ? changes?.diff ? <pre>{changes.diff}</pre> : <p className="vrepo-changes__empty">{text.noDiff}</p> : <p className="vrepo-changes__empty">{text.selectChange}</p>}</section></section> : null}
					{selectedStatus?.status ? <pre className="vrepo-status-output">{selectedStatus.status}</pre> : null}
						</section> : <section className="vrepo-overview"><div className="vrepo-overview__title"><div><span>{repo.remote ? text.remoteRepository : text.localRepository}</span><h2>{repo.name}</h2><p>{repo.root_path}</p></div></div><div className="vrepo-health"><div><span>{text.health}</span><strong>{Object.values(statuses).some(statusNeedsAttention) ? text.needsAttention : Object.keys(statuses).length ? text.healthy : text.pendingStatus}</strong></div><div><span>{text.mappings}</span><strong>{repo.nodes.filter((node) => node.repository).length}</strong></div><div><span>{text.location}</span><strong>{repo.remote ? text.remoteRepository : text.localRepository}</strong></div></div><div className="vrepo-overview__next"><h3>{text.repositoryActions}</h3><p>{text.selectRepositoryHint}</p></div></section>}
            </main>
        </div> : <section className="vrepo-management-placeholder"><h2>{text.selectRepository}</h2><p>{text.selectRepositoryHint}</p></section>}
        </div> : null}

		{(mode === 'group' || mode === 'mapping') && repo ? <VRepoDialog title={draft.id ? (mode === 'group' ? text.editGroup : text.editMapping) : (mode === 'group' ? text.addGroup : text.addRepo)} titleId="vrepo-node-dialog-title" className="vrepo-dialog--node" closeLabel={text.close} closeDisabled={busy} onClose={() => setMode('none')} footer={<>
			<button type="button" className="secondary" title={tips.cancel} disabled={busy} onClick={() => setMode('none')}>{text.cancel}</button>
			<button type="button" title={tips.save} onClick={() => void saveNode()} disabled={busy}>{text.save}</button>
		</>}>
			{errorBanner}
			<div className="vrepo-editor">
                    <label>{text.name}<input value={draft.name || ''} onChange={(e) => setDraft({ ...draft, name: e.target.value })} /></label>
                    <label>{text.parent}<select value={draft.parent_id || ''} onChange={(e) => setDraft({ ...draft, parent_id: e.target.value })}><option value="">{text.rootNode}</option>{repo.nodes.filter((n) => !n.repository).map((n) => <option key={n.id} value={n.id}>{n.name}</option>)}</select></label>
                    {mode === 'mapping' ? <>
	                        <label>{isZh ? '仓库说明（可选）' : 'Repository description (optional)'}<input value={draft.description || ''} onChange={(e) => setDraft({ ...draft, description: e.target.value })} /></label>
                        <label>{text.type}<select value={draft.kind || 'git'} onChange={(e) => setDraft({ ...draft, kind: e.target.value, credential_id: '' })}><option value="git">Git</option><option value="svn">SVN</option><option value="local">{text.local}</option></select></label>
							{draft.kind !== 'local' ? <><label>{text.remote}<input value={draft.remote_url || ''} onChange={(e) => setDraft({ ...draft, remote_url: e.target.value })} /></label><label>{text.refType}<select value={draft.ref_type || 'branch'} onChange={(e) => setDraft({ ...draft, ref_type: e.target.value })}><option value="branch">{text.branchRef}</option><option value="tag">{text.tagRef}</option></select></label><label>{text.refName}<input value={draft.ref_name || ''} placeholder={text.defaultRef} onChange={(e) => setDraft({ ...draft, ref_name: e.target.value })} /></label><label>{text.credentials}<div className="vrepo-inline"><select value={draft.credential_id || credentialBindings[draft.id] || ''} onFocus={() => { const kind = draft.kind === 'svn' ? 'svn' : 'git'; if (!credentials.some((item) => item.kind === kind)) void loadMappingCredentials(kind); }} onChange={(e) => setDraft({ ...draft, credential_id: e.target.value })}><option value="">{text.noCredential}</option>{credentials.filter((item) => item.kind === draft.kind).map((item) => <option key={item.id} value={item.id}>{item.name} · {item.username}</option>)}</select><button type="button" className="secondary vrepo-credential-add" title={tips.addCredential} onClick={() => void startCredentialManager(true)}>{text.addCredential}</button></div></label></> : null}
						{draft.kind !== 'local' ? <label className="vrepo-check"><input type="checkbox" disabled={draft.enabled === false} checked={!!draft.checkout_after_save} onChange={(e) => setDraft({ ...draft, checkout_after_save: e.target.checked })} />{text.checkoutAfterSave}</label> : null}
	                        {draft.kind === 'local' ? <p className="vrepo-local-directory-note">{text.localDirectoryCreated}</p> : <label className="vrepo-check"><input type="checkbox" checked={!!draft.create_directory} onChange={(e) => setDraft({ ...draft, create_directory: e.target.checked })} />{text.createDir}</label>}
                        <label className="vrepo-check"><input type="checkbox" checked={draft.enabled !== false} onChange={(e) => setDraft({ ...draft, enabled: e.target.checked })} />{text.enabled}</label>
                    </> : null}
			</div>
		</VRepoDialog> : null}

		{mode === 'credentials' ? <VRepoDialog title={text.manageCredentials} titleId="vrepo-credentials-dialog-title" className="vrepo-dialog--credentials" closeLabel={text.close} closeDisabled={busy} onClose={closeCredentialManager} footer={<button type="button" className="secondary" title={text.close} disabled={busy} onClick={closeCredentialManager}>{text.close}</button>}>
			{errorBanner}
            <div className="vrepo-credential-layout"><div>{credentials.length ? credentials.map((credential) => <div className="vrepo-credential-row" key={credential.id}><div><strong>{credential.name}</strong><span>{credential.kind.toUpperCase()} · {credential.username} · {credential.scope || text.anyHost}</span></div><div><button type="button" className="secondary" title={tips.editCredential} disabled={mutationLocked} onClick={() => setDraft(credential)}>{text.edit}</button><button type="button" className="danger" title={tips.removeCredential} disabled={mutationLocked} onClick={() => void deleteCredential(credential)}>{text.remove}</button></div></div>) : <p>{text.noCredentials}</p>}</div>
                <div className="vrepo-editor"><h3>{draft.id ? text.edit : text.addCredential}</h3><label>{text.credentialName}<input value={draft.name || ''} onChange={(e) => setDraft({ ...draft, name: e.target.value })} /></label><label>{text.type}<select value={draft.kind || 'git'} disabled={!!draft.id} onChange={(e) => setDraft({ ...draft, kind: e.target.value })}><option value="git">Git</option><option value="svn">SVN</option></select></label><label>{text.username}<input value={draft.username || ''} onChange={(e) => setDraft({ ...draft, username: e.target.value })} /></label><label>{text.password}<input type="password" autoComplete="new-password" value={draft.secret || ''} placeholder={draft.id ? text.passwordHint : ''} onChange={(e) => setDraft({ ...draft, secret: e.target.value })} /></label><label>{text.scope}<input value={draft.scope || ''} onChange={(e) => setDraft({ ...draft, scope: e.target.value })} /></label><button type="button" title={tips.save} onClick={() => void saveCredential()} disabled={busy}>{text.save}</button></div>
            </div>
        </VRepoDialog> : null}
        {operation ? <VRepoDialog title={operation.action === 'sync' ? text.syncWorkingCopiesTitle : operation.action.replace('_', ' & ')} titleId="vrepo-operation-title" className="vrepo-operation" closeLabel={text.close} closeDisabled={busy} onClose={closeOperation} footer={<>
			<button type="button" className="secondary" title={tips.cancel} disabled={busy} onClick={closeOperation}>{text.cancel}</button>
			{!operation.preview ? <button type="button" title={tips.previewOp} onClick={() => void loadOperationPreview()} disabled={(operation.action === 'commit' || operation.action === 'commit_push') && !operation.message.trim()}>{text.preview}</button> : <button type="button" className={operation.action === 'revert' ? 'danger' : ''} title={tips.executeOp} disabled={operation.preview.blocked || busy} onClick={() => void executeOperation()}>{busy ? text.loading : text.execute}</button>}
		</>}>
			{errorBanner}
            {operation.action === 'sync' ? <p className="vrepo-operation__hint">{text.syncWorkingCopiesHint}</p> : null}
            {(operation.action === 'commit' || operation.action === 'commit_push') ? <label>{text.commitMessage}<textarea value={operation.message} onChange={(e) => setOperation({ ...operation, message: e.target.value, preview: undefined })} /></label> : null}
            {operation.preview ? <>
                <p>{operation.preview.targets?.length || 0} {text.repositories} · {operation.preview.skipped_local || 0} {text.localSkipped}</p>
                <div className="vrepo-operation__targets">{(operation.preview.targets || []).map((target: any) => <div key={target.node_id}><strong>{target.name}</strong><span>{target.kind.toUpperCase()} · {target.changed ? text.changed : text.clean}{target.error ? ` · ${target.error}` : ''}</span></div>)}</div>
                {operation.action === 'revert' ? <p className="vrepo-operation__danger">{text.revertWarning}</p> : null}
            </> : null}
        </VRepoDialog> : null}
		{operationResult ? <section className="vrepo-operation-result" role="region" aria-live="polite" aria-atomic="false"><div className="vrepo-detail__title"><div><h2>{text.operation}: {operationResult.status}</h2>{operationRunning ? <p>{text.operationRunningHint}</p> : null}</div><div>{operationRunning ? <button type="button" className="danger" disabled={operationCancelPending} title={tips.cancel} onClick={() => void cancelOperation()}>{operationCancelPending ? text.loading : text.cancel}</button> : <button type="button" className="secondary" title={text.close} onClick={() => { operationResultRef.current = null; setOperationResult(null); setOperationCancelPending(false); }}>{text.close}</button>}</div></div>{(operationResult.items || []).map((item: any) => <div key={item.node_id || item.name} className={item.status === 'failed' ? 'is-error' : ''}><strong>{item.name || text.operation}</strong><span>{item.status}{item.error_code ? ` · ${item.error_code}` : ''}{item.error ? ` · ${localizeVRepoError(item.error, isZh)}` : ''}</span></div>)}{operationResult.status !== 'cancelled' && !operationRunning && operationResult.items?.some((item: any) => item.status === 'failed' && item.node_id) ? <button type="button" onClick={() => { const retry = retryOperationForResult(operationResult); setRetryNodeIds(retry.failed.map((item: any) => item.node_id)); setOperation({ action: retry.action, message: operationResult.message || '' }); operationResultRef.current = null; setOperationResult(null); setOperationCancelPending(false); }} title={tips.retryFailed}>{text.retryFailed}</button> : null}</section> : null}
    </div>;
}

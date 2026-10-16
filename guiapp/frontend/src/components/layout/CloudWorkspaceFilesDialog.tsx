import { useCallback, useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { SyncCloudWorkspaceFiles } from '../../../wailsjs/go/main/App';
import { useSafeBackdropDismiss } from '../../hooks/useSafeBackdropDismiss';
import { localizeText } from '../../i18n';
import { scrubCloudWorkspaceError } from '../ai/codingTaskMode';
import { CodePreviewWorkspace } from '../ai/CodePreviewWorkspace';
import type { CodePreviewTheme } from '../ai/FileTabBar';
import { extractErrorMessage } from '../ai/participantAddError';
import './cloudOverview.css';

const lightFileTheme: CodePreviewTheme = {
    bg: '#ffffff',
    text: '#1f2937',
    textMuted: '#64748b',
    border: '#e4e4e4',
    lineNumBg: '#f5f5f5',
    lineNumText: '#94a3b8',
    tabBg: '#f5f5f5',
    tabActiveBg: '#ffffff',
    tabActiveText: '#111827',
    tabHoverBg: '#eeeeee',
    diffAddBg: 'rgba(79, 127, 111, 0.12)',
    diffAddText: '#4f7f6f',
    diffDeleteBg: 'rgba(196, 61, 52, 0.10)',
    diffDeleteText: '#c43d34',
    syntaxKeyword: '#2f6fbc',
    syntaxString: '#4f7f6f',
    syntaxComment: '#64748b',
    syntaxNumber: '#2f6fbc',
    syntaxFunction: '#334155',
    syntaxType: '#2f6fbc',
    syntaxOperator: '#334155',
};

const darkFileTheme: CodePreviewTheme = {
    bg: '#0f1720',
    text: '#d7dee8',
    textMuted: '#a0abbe',
    border: '#263447',
    lineNumBg: '#111b27',
    lineNumText: '#8494a8',
    tabBg: '#111b27',
    tabActiveBg: '#162233',
    tabActiveText: '#edf3f9',
    tabHoverBg: '#1a293b',
    diffAddBg: 'rgba(122, 168, 154, 0.16)',
    diffAddText: '#b8d7cf',
    diffDeleteBg: 'rgba(196, 61, 52, 0.12)',
    diffDeleteText: '#e07a72',
    syntaxKeyword: '#9bc2ea',
    syntaxString: '#b8d7cf',
    syntaxComment: '#8d9eb2',
    syntaxNumber: '#b7d3ef',
    syntaxFunction: '#d7dee8',
    syntaxType: '#b7d3ef',
    syntaxOperator: '#c3ccd8',
};

const textForLang = localizeText;
const FILES_DIALOG_Z_INDEX = 100000;

const portalThemeMode = (themeMode?: 'light' | 'dark') => (
    themeMode || document.getElementById('App')?.getAttribute('data-ai-theme') || undefined
);

const cachePathFromSync = (value: unknown): string => {
    if (!value || typeof value !== 'object') return '';
    const row = value as Record<string, unknown>;
    for (const key of ['local_path', 'LocalPath']) {
        const item = row[key];
        if (typeof item === 'string' && item.trim()) return item.trim();
    }
    return '';
};

export function CloudWorkspaceFilesDialog({
    lang,
    themeMode,
    workspaceId,
    workspaceName,
    onClose,
}: {
    lang: string;
    themeMode?: 'light' | 'dark';
    workspaceId: string;
    workspaceName: string;
    onClose: () => void;
}) {
    const [localPath, setLocalPath] = useState('');
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState('');
    const [retryToken, setRetryToken] = useState(0);
    const dialogRef = useRef<HTMLDivElement | null>(null);
    const refreshRef = useRef<(() => void) | null>(null);
    const refreshRequestRef = useRef(0);
    const [treeRefreshing, setTreeRefreshing] = useState(false);
    const [syncing, setSyncing] = useState(false);
    const [refreshError, setRefreshError] = useState('');
    const handleRefreshReady = useCallback((refresh: () => void, busy: boolean) => {
        refreshRef.current = refresh;
        setTreeRefreshing(prev => (prev === busy ? prev : busy));
    }, []);
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(onClose);
    const title = (workspaceName || workspaceId).trim() || workspaceId;
    const previewTheme = portalThemeMode(themeMode) === 'dark' ? darkFileTheme : lightFileTheme;

    useEffect(() => {
        let cancelled = false;
        refreshRequestRef.current += 1;
        const load = async () => {
            setLoading(true);
            setError('');
            setRefreshError('');
            setSyncing(false);
            setLocalPath('');
            const fallback = textForLang(lang, 'Could not load cloud workspace files.', '无法加载云端工作区文件。', '無法載入雲端工作區檔案。');
            try {
                if (typeof SyncCloudWorkspaceFiles !== 'function') {
                    throw new Error(fallback);
                }
                const prepared = await SyncCloudWorkspaceFiles(workspaceId);
                if (cancelled) return;
                const path = cachePathFromSync(prepared);
                if (!path) throw new Error(fallback);
                setLocalPath(path);
            } catch (err) {
                if (cancelled) return;
                const scrubbed = scrubCloudWorkspaceError(extractErrorMessage(err), fallback);
                setError(scrubbed || fallback);
            } finally {
                if (!cancelled) setLoading(false);
            }
        };
        void load();
        return () => {
            cancelled = true;
            // A newer refresh owns the counter. Always bump so that refresh
            // cannot apply after this load is cancelled or the dialog unmounts.
            refreshRequestRef.current += 1;
        };
    }, [lang, retryToken, workspaceId]);

    // 刷新 must pull again. The tree callback only re-reads the cache from the
    // last SyncCloudWorkspaceFiles, and an unlinked workspace has no live writer.
    const refreshCloudFiles = useCallback(() => {
        const request = ++refreshRequestRef.current;
        const fallback = textForLang(lang, 'Could not refresh cloud workspace files.', '无法刷新云端工作区文件。', '無法重新整理雲端工作區檔案。');
        setSyncing(true);
        setRefreshError('');
        void (async () => {
            try {
                if (typeof SyncCloudWorkspaceFiles !== 'function') throw new Error(fallback);
                const prepared = await SyncCloudWorkspaceFiles(workspaceId);
                if (request !== refreshRequestRef.current) return;
                const path = cachePathFromSync(prepared);
                if (!path) throw new Error(fallback);
                if (path !== localPath) {
                    setLocalPath(path);
                    return;
                }
                refreshRef.current?.();
            } catch (err) {
                if (request !== refreshRequestRef.current) return;
                const scrubbed = scrubCloudWorkspaceError(extractErrorMessage(err), fallback);
                setRefreshError(scrubbed || fallback);
            } finally {
                if (request === refreshRequestRef.current) setSyncing(false);
            }
        })();
    }, [lang, localPath, workspaceId]);

    useEffect(() => {
        const focusTimer = window.setTimeout(() => dialogRef.current?.focus(), 0);
        return () => {
            window.clearTimeout(focusTimer);
            document.getElementById('task-cloud-overview-panel')?.focus();
        };
    }, [workspaceId]);

    // Cleanup runs before the focus effect above, so the overview is focusable again.
    useEffect(() => {
        const panel = document.getElementById('task-cloud-overview-panel');
        if (!panel) return undefined;
        panel.setAttribute('inert', '');
        return () => { panel.removeAttribute('inert'); };
    }, []);

    return createPortal(
        <div
            className="modal-backdrop"
            data-testid="task-cloud-overview-files-dialog"
            data-ai-theme={portalThemeMode(themeMode)}
            data-ai-dark-scheme={document.getElementById('App')?.getAttribute('data-ai-dark-scheme') || undefined}
            data-ai-light-scheme={document.getElementById('App')?.getAttribute('data-ai-light-scheme') || undefined}
            style={{ zIndex: FILES_DIALOG_Z_INDEX }}
            {...backdropProps}
        >
            <div
                ref={dialogRef}
                className="modal-content mc-cloud-files"
                role="dialog"
                aria-modal="true"
                aria-labelledby="task-cloud-overview-files-title"
                tabIndex={-1}
                {...dialogProps}
            >
                <div className="modal-header">
                    <h3 id="task-cloud-overview-files-title">{title}</h3>
                    <div className="mc-cloud-files__header-actions">
                        {!loading && !error ? (
                            <button
                                type="button"
                                className="mc-cloud-files__refresh"
                                aria-label={textForLang(lang, 'Refresh cloud files', '刷新云端文件', '重新整理雲端檔案')}
                                title={textForLang(lang, 'Refresh', '刷新', '重新整理')}
                                aria-busy={syncing || treeRefreshing}
                                onClick={refreshCloudFiles}
                            >
                                {textForLang(lang, 'Refresh', '刷新', '重新整理')}
                            </button>
                        ) : null}
                        <button type="button" className="btn-close" aria-label={textForLang(lang, 'Close', '关闭', '關閉')} onClick={onClose}>X</button>
                    </div>
                </div>
                <p className="mc-cloud-files__hint">
                    {textForLang(lang, 'Right-click a file or folder to delete it. Deletion also removes the cloud copy.', '右键文件或文件夹可以删除，删除会同时移除云端副本。', '右鍵檔案或資料夾可以刪除，刪除會同時移除雲端副本。')}
                </p>
                {!loading && !error && refreshError ? (
                    <p className="mc-cloud-files__refresh-error" role="alert" data-testid="task-cloud-overview-files-refresh-error">{refreshError}</p>
                ) : null}
                <div className="mc-cloud-files__body">
                    {loading ? (
                        <div className="mc-cloud-files__status" role="status" data-testid="task-cloud-overview-files-loading">
                            {textForLang(lang, 'Syncing cloud files…', '正在同步云端文件…', '正在同步雲端檔案…')}
                        </div>
                    ) : error ? (
                        <div className="mc-cloud-files__status" role="alert" data-testid="task-cloud-overview-files-error">
                            <span>{error}</span>
                            <button type="button" className="mc-cloud-overview__action" onClick={() => setRetryToken(token => token + 1)}>
                                {textForLang(lang, 'Retry', '重试', '重試')}
                            </button>
                        </div>
                    ) : (
                        <CodePreviewWorkspace
                            projectPath={localPath}
                            cloudMode
                            manageFiles
                            hideHeader
                            lang={lang}
                            theme={previewTheme}
                            onRefreshReady={handleRefreshReady}
                            onOpenFile={() => {}}
                        />
                    )}
                </div>
            </div>
        </div>,
        document.body,
    );
}

import { useCallback, useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { EventsOn } from '../../../wailsjs/runtime';
import { useSafeBackdropDismiss } from '../../hooks/useSafeBackdropDismiss';
import { localizeText } from '../../i18n';
import { CodePreviewWorkspace } from '../ai/CodePreviewWorkspace';
import { GlyphClose, GlyphCloudDrive, GlyphRefresh } from './cloudDriveGlyphs';
import { cloudFilesDarkTheme, cloudFilesLightTheme, portalThemeMode } from './CloudWorkspaceFilesDialog';
import type { CloudAgentListItem } from './taskPlace';
import './cloudOverview.css';

const textForLang = localizeText;
const FILES_DIALOG_Z_INDEX = 100000;

// manageFiles mode never calls this, but keep one stable reference so the
// workspace's openFile callback deps do not churn on every dialog render.
const noopOpenFile = () => {};

/**
 * Live file browser inside a cloud agent instance. Unlike the cloud workspace
 * dialog there is no sync: the tree reads through remotecontrol cloud ops and
 * the project path is the instance's "cloud-agent:<id>" session key, which the
 * desktop's workbench bindings dispatch to the ops.
 */
export function CloudAgentFilesDialog({
    lang,
    themeMode,
    item,
    onClose,
}: {
    lang: string;
    themeMode?: 'light' | 'dark';
    item: CloudAgentListItem;
    onClose: () => void;
}) {
    const dialogRef = useRef<HTMLDivElement | null>(null);
    const refreshRef = useRef<(() => void) | null>(null);
    const [treeRefreshing, setTreeRefreshing] = useState(false);
    const handleRefreshReady = useCallback((refresh: () => void, busy: boolean) => {
        refreshRef.current = refresh;
        setTreeRefreshing(prev => (prev === busy ? prev : busy));
    }, []);
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(onClose);
    const title = (item.name || item.id).trim() || item.id;
    const projectPath = `cloud-agent:${item.id}`;
    const previewTheme = portalThemeMode(themeMode) === 'dark' ? cloudFilesDarkTheme : cloudFilesLightTheme;

    useEffect(() => {
        const focusTimer = window.setTimeout(() => dialogRef.current?.focus(), 0);
        return () => window.clearTimeout(focusTimer);
    }, [item.id]);

    // Workspace mutations from the task context menu (upload, delete) reach
    // the open dialog through this event; a missing payload id refreshes too.
    useEffect(() => {
        let off: (() => void) | undefined;
        try {
            off = EventsOn('cloud-agent-files-changed', (payload: unknown) => {
                const id = typeof payload === 'object' && payload !== null
                    ? String((payload as { instance_id?: string }).instance_id || '')
                    : '';
                if (!id || id === item.id) refreshRef.current?.();
            });
        } catch {
            off = undefined;
        }
        return () => off?.();
    }, [item.id]);

    return createPortal(
        <div
            className="modal-backdrop"
            data-testid="cloud-agent-files-dialog"
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
                aria-labelledby="cloud-agent-files-title"
                tabIndex={-1}
                {...dialogProps}
            >
                <div className="mc-cloud-files__head">
                    <span className="mc-cloud-files__head-icon" aria-hidden="true">
                        <GlyphCloudDrive size={20} />
                    </span>
                    <div className="mc-cloud-files__head-copy">
                        <h3 id="cloud-agent-files-title">{title}</h3>
                        <p className="mc-cloud-files__head-sub">
                            {textForLang(lang, 'Files in this cloud task. Right-click a file or folder to open, download, or delete it.', '云端任务里的文件。右键文件或文件夹可以打开、下载或删除。', '雲端任務裡的檔案。右鍵檔案或資料夾可以開啟、下載或刪除。')}
                        </p>
                    </div>
                    <div className="mc-cloud-files__head-actions">
                        <button
                            type="button"
                            className="mc-cloud-files__refresh"
                            aria-label={textForLang(lang, 'Refresh cloud files', '刷新云端文件', '重新整理雲端檔案')}
                            title={textForLang(lang, 'Refresh', '刷新', '重新整理')}
                            aria-busy={treeRefreshing}
                            onClick={() => refreshRef.current?.()}
                        >
                            <GlyphRefresh size={14} />
                            {textForLang(lang, 'Refresh', '刷新', '重新整理')}
                        </button>
                        <button
                            type="button"
                            className="mc-cloud-files__close"
                            aria-label={textForLang(lang, 'Close', '关闭', '關閉')}
                            title={textForLang(lang, 'Close', '关闭', '關閉')}
                            onClick={onClose}
                        >
                            <GlyphClose size={16} />
                        </button>
                    </div>
                </div>
                <div className="mc-cloud-files__body">
                    <CodePreviewWorkspace
                        projectPath={projectPath}
                        cloudMode
                        manageFiles
                        hideHeader
                        lang={lang}
                        theme={previewTheme}
                        onRefreshReady={handleRefreshReady}
                        onOpenFile={noopOpenFile}
                    />
                </div>
            </div>
        </div>,
        document.body,
    );
}

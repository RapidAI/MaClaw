import { useEffect, useMemo, useRef, useState, type CSSProperties, type KeyboardEvent as ReactKeyboardEvent } from 'react';
import { createPortal } from 'react-dom';
import { localizeText } from '../../i18n';
import { useSafeBackdropDismiss } from '../../hooks/useSafeBackdropDismiss';
import { extractErrorMessage } from '../ai/participantAddError';
import { nextDefaultCloudWorkspaceName } from '../ai/task-config/WorkspacePickerPopover';

/** Same helper the task list uses, so "zh-CN"/"zh_CN"/"ZH" all resolve. */
const textForLang = localizeText;

const getPortalThemeMode = (themeMode?: string) => (
    themeMode || document.getElementById('App')?.getAttribute('data-ai-theme') || undefined
);
const getPortalDarkScheme = () => document.getElementById('App')?.getAttribute('data-ai-dark-scheme') || undefined;
const getPortalLightScheme = () => document.getElementById('App')?.getAttribute('data-ai-light-scheme') || undefined;

export type TaskTransferDirection = 'to-cloud' | 'to-local';

export type TaskTransferCloudWorkspace = {
    id: string;
    name: string;
    /** Title of the task already bound to this workspace; bound workspaces cannot host a second task. */
    boundTaskTitle?: string;
};

export type TaskTransferTarget =
    | { kind: 'cloud'; workspaceId: string; workspaceName: string }
    | { kind: 'local'; dir: string };

export type TaskWorkspaceTransferDialogProps = {
    open: boolean;
    lang: string;
    themeMode?: string;
    direction: TaskTransferDirection;
    /** Title of the source task; also the title of the task created at the target. */
    taskName: string;
    cloudWorkspaces?: TaskTransferCloudWorkspace[];
    /** Local folder chosen for a "转移到本地" transfer. */
    localDir?: string;
    busy: boolean;
    error: string;
    /** Non-empty when a new cloud workspace cannot be created right now. */
    createDisabledReason?: string;
    /**
     * Creates a cloud workspace with the given name and returns it. A null
     * result means the creation failed and the dialog keeps the editor open.
     */
    onCreateCloudWorkspace?: (name: string) => Promise<TaskTransferCloudWorkspace | null | undefined>;
    onPickLocalDir?: () => void | Promise<void>;
    onConfirm: (target: TaskTransferTarget) => void | Promise<void>;
    onClose: () => void;
};

const workspaceRowStyle = (selected: boolean, disabled: boolean): CSSProperties => ({
    display: 'flex',
    alignItems: 'center',
    gap: '8px',
    width: '100%',
    boxSizing: 'border-box',
    padding: '8px 10px',
    borderRadius: '8px',
    border: selected ? '1px solid var(--theme-primary)' : '1px solid var(--theme-border)',
    background: selected
        ? 'color-mix(in srgb, var(--theme-primary) 12%, var(--theme-surface))'
        : 'var(--theme-surface)',
    color: selected ? 'var(--theme-primary)' : 'var(--theme-text-primary)',
    textAlign: 'left',
    cursor: disabled ? 'not-allowed' : 'pointer',
    opacity: disabled ? 0.55 : 1,
});

/** Chinese and English both need Enter to submit, but a live IME must not. */
const isImeComposing = (event: ReactKeyboardEvent<HTMLInputElement>) =>
    event.nativeEvent.isComposing || event.keyCode === 229;

/** Stable identity so an omitted prop cannot invalidate memos on every render. */
const NO_CLOUD_WORKSPACES: TaskTransferCloudWorkspace[] = [];

/**
 * Modal for moving a task between the local machine and a cloud workspace.
 *
 * The dialog only collects the destination: a cloud workspace (existing free
 * one, or a newly created one) for "转移到云端", a local folder for
 * "转移到本地". The caller owns the actual transfer so it can drive the
 * existing task-creation path and the follow-up "delete the source task?"
 * confirmation.
 */
export function TaskWorkspaceTransferDialog({
    open,
    lang,
    themeMode,
    direction,
    taskName,
    cloudWorkspaces: cloudWorkspacesProp,
    localDir = '',
    busy,
    error,
    createDisabledReason = '',
    onCreateCloudWorkspace,
    onPickLocalDir,
    onConfirm,
    onClose,
}: TaskWorkspaceTransferDialogProps) {
    const toCloud = direction === 'to-cloud';
    const cloudWorkspaces = cloudWorkspacesProp ?? NO_CLOUD_WORKSPACES;
    /** Only an explicit user pick; the default is derived so a changing list
     * cannot trigger an extra state update from an effect. */
    const [pickedId, setPickedId] = useState('');
    const [creatingName, setCreatingName] = useState<string | null>(null);
    const [createError, setCreateError] = useState('');
    const [creatingBusy, setCreatingBusy] = useState(false);
    const creatingRef = useRef(false);
    const createInputRef = useRef<HTMLInputElement | null>(null);
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(onClose, { enabled: !busy && !creatingBusy });

    const freeWorkspaces = useMemo(
        () => cloudWorkspaces.filter(row => !row.boundTaskTitle),
        [cloudWorkspaces],
    );
    // A workspace that became bound, or the list itself, may change under us,
    // so the effective selection is re-derived instead of trusted.
    const selectedId = freeWorkspaces.some(row => row.id === pickedId)
        ? pickedId
        : (freeWorkspaces[0]?.id || '');

    // Reset per open so a previous transfer's selection cannot leak into the
    // next one.
    useEffect(() => {
        if (open) return;
        setPickedId('');
        setCreatingName(null);
        setCreateError('');
        setCreatingBusy(false);
        creatingRef.current = false;
    }, [open]);

    useEffect(() => {
        if (creatingName === null) return;
        const input = createInputRef.current;
        input?.focus();
        input?.select();
    }, [creatingName]);

    useEffect(() => {
        if (!open) return;
        const onKeyDown = (event: KeyboardEvent) => {
            if (event.key !== 'Escape' || event.isComposing || event.keyCode === 229) return;
            if (busy || creatingRef.current) return;
            // A nested confirm dialog (the follow-up "delete the source task?"
            // prompt) owns Escape while it is up.
            if (document.querySelector('.custom-dialog')) return;
            event.preventDefault();
            event.stopImmediatePropagation();
            onClose();
        };
        window.addEventListener('keydown', onKeyDown, true);
        return () => window.removeEventListener('keydown', onKeyDown, true);
    }, [busy, onClose, open]);

    if (!open) return null;

    const title = toCloud
        ? textForLang(lang, `Move “${taskName}” to the cloud`, `转移到云端「${taskName}」`, `轉移到雲端「${taskName}」`)
        : textForLang(lang, `Move “${taskName}” to this computer`, `转移到本地「${taskName}」`, `轉移到本機「${taskName}」`);
    const hint = toCloud
        ? textForLang(
            lang,
            'A cloud workspace task with the same title is created in the chosen workspace, and this task\'s files are copied into it.',
            '将在所选云端工作区创建一条同名任务，并把该任务的文件复制进去。',
            '將在所選雲端工作區建立一條同名任務，並把該任務的檔案複製進去。',
        )
        : textForLang(
            lang,
            'A local task with the same title is created for the chosen folder, and the cloud files are copied into it.',
            '将为所选目录创建一条同名本地任务，并把云端文件复制进去。',
            '將為所選目錄建立一條同名本機任務，並把雲端檔案複製進去。',
        );
    const confirmLabel = toCloud
        ? textForLang(lang, 'Move to cloud', '转移到云端', '轉移到雲端')
        : textForLang(lang, 'Move to this computer', '转移到本地', '轉移到本機');

    const beginCreate = () => {
        if (creatingBusy || !onCreateCloudWorkspace || createDisabledReason) return;
        setCreateError('');
        setCreatingName(nextDefaultCloudWorkspaceName(cloudWorkspaces.map(row => row.name)));
    };

    const submitCreate = async () => {
        const name = (creatingName ?? '').trim();
        if (!name || !onCreateCloudWorkspace || creatingRef.current) return;
        creatingRef.current = true;
        setCreatingBusy(true);
        setCreateError('');
        try {
            const created = await onCreateCloudWorkspace(name);
            if (created?.id) {
                setPickedId(created.id);
                setCreatingName(null);
                return;
            }
            // A null result means the caller refused (another cloud action owns
            // the busy lock); keep the editor open so the name is not lost.
            setCreateError(textForLang(
                lang,
                'Another cloud action is still running. Try again in a moment.',
                '另一个云端操作正在执行，请稍后再试。',
                '另一個雲端操作正在執行，請稍後再試。',
            ));
        } catch (err) {
            setCreateError(extractErrorMessage(err) || textForLang(lang, 'Failed to create cloud workspace', '新建云端工作区失败', '新建雲端工作區失敗'));
        } finally {
            creatingRef.current = false;
            setCreatingBusy(false);
        }
    };

    const confirm = () => {
        if (busy || creatingBusy) return;
        if (toCloud) {
            const picked = freeWorkspaces.find(row => row.id === selectedId);
            if (!picked) return;
            void onConfirm({ kind: 'cloud', workspaceId: picked.id, workspaceName: picked.name });
            return;
        }
        const dir = localDir.trim();
        if (!dir) return;
        void onConfirm({ kind: 'local', dir });
    };

    const confirmDisabled = busy
        || creatingBusy
        || (toCloud ? !freeWorkspaces.some(row => row.id === selectedId) : !localDir.trim());

    return createPortal(
        <div
            className="modal-backdrop cwshare-backdrop"
            data-testid="task-transfer-dialog"
            data-direction={direction}
            data-ai-theme={getPortalThemeMode(themeMode)}
            data-ai-dark-scheme={getPortalDarkScheme()}
            data-ai-light-scheme={getPortalLightScheme()}
            {...backdropProps}
        >
            <div
                className="modal-content cwshare-dialog"
                role="dialog"
                aria-modal="true"
                aria-labelledby="task-transfer-title"
                {...dialogProps}
            >
                <div className="modal-header">
                    <h3 id="task-transfer-title" className="cwshare-title">{title}</h3>
                    <button
                        type="button"
                        className="btn-close"
                        data-testid="task-transfer-close"
                        aria-label={textForLang(lang, 'Close', '关闭', '關閉')}
                        disabled={busy || creatingBusy}
                        onClick={onClose}
                    >X</button>
                </div>
                <div className="modal-body cwshare-body">
                    <div className="cwshare-hint">{hint}</div>

                    {toCloud ? (
                        <div>
                            <div className="cwshare-field-label">{textForLang(lang, 'Cloud workspace', '云端工作区', '雲端工作區')}</div>
                            <div role="radiogroup" aria-label={textForLang(lang, 'Cloud workspace', '云端工作区', '雲端工作區')} style={{ display: 'flex', flexDirection: 'column', gap: '6px' }}>
                                {cloudWorkspaces.map(row => {
                                    const bound = !!row.boundTaskTitle;
                                    const selected = !bound && row.id === selectedId;
                                    return (
                                        <button
                                            key={row.id}
                                            type="button"
                                            role="radio"
                                            aria-checked={selected}
                                            data-testid={`task-transfer-workspace-${row.id}`}
                                            aria-disabled={bound}
                                            disabled={busy || creatingBusy || bound}
                                            title={bound
                                                ? textForLang(
                                                    lang,
                                                    `Already holds the task “${row.boundTaskTitle}”. Each workspace hosts one task.`,
                                                    `已有任务「${row.boundTaskTitle}」，每个工作区只能承载一条任务。`,
                                                    `已有任務「${row.boundTaskTitle}」，每個工作區只能承載一條任務。`,
                                                )
                                                : row.name}
                                            onClick={() => { if (!bound) setPickedId(row.id); }}
                                            style={workspaceRowStyle(selected, bound)}
                                        >
                                            <span style={{ minWidth: 0, flex: '1 1 auto', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', fontWeight: 600 }}>{row.name}</span>
                                            {bound ? (
                                                <span data-testid={`task-transfer-workspace-bound-${row.id}`} style={{ flexShrink: 0, fontSize: '0.66rem', color: 'var(--theme-text-muted)' }}>
                                                    {textForLang(lang, 'in use', '使用中', '使用中')}
                                                </span>
                                            ) : null}
                                            {selected ? <span aria-hidden="true" style={{ flexShrink: 0 }}>✓</span> : null}
                                        </button>
                                    );
                                })}
                                {freeWorkspaces.length === 0 ? (
                                    <div data-testid="task-transfer-workspace-empty" className="cwshare-empty">
                                        {cloudWorkspaces.length === 0
                                            ? textForLang(lang, 'No cloud workspace yet. Create one below.', '还没有云端工作区，可在下方新建。', '還沒有雲端工作區，可在下方新建。')
                                            : textForLang(
                                                lang,
                                                'Every cloud workspace already hosts a task. Create another one below, or free one up first.',
                                                '每个云端工作区都已承载任务，可在下方再新建一个，或先腾出一个。',
                                                '每個雲端工作區都已承載任務，可在下方再新建一個，或先騰出一個。',
                                            )}
                                    </div>
                                ) : null}
                            </div>
                        </div>
                    ) : (
                        <div>
                            <div className="cwshare-field-label">{textForLang(lang, 'Local folder', '本地目录', '本機目錄')}</div>
                            <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                                <span
                                    data-testid="task-transfer-local-dir"
                                    className="cwshare-url"
                                    style={{ flex: '1 1 auto', minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
                                >{localDir || textForLang(lang, 'No folder selected', '尚未选择目录', '尚未選擇目錄')}</span>
                                <button
                                    type="button"
                                    className="btn-secondary"
                                    data-testid="task-transfer-pick-dir"
                                    disabled={busy || creatingBusy || !onPickLocalDir}
                                    onClick={() => { void onPickLocalDir?.(); }}
                                >{textForLang(lang, 'Choose folder…', '选择目录…', '選擇目錄…')}</button>
                            </div>
                        </div>
                    )}

                    {toCloud ? (
                        <div>
                            <div className="cwshare-field-label">{textForLang(lang, 'New cloud workspace', '新建云端工作区', '新建雲端工作區')}</div>
                            {createDisabledReason ? (
                                <div data-testid="task-transfer-create-disabled" className="cwshare-hint">{createDisabledReason}</div>
                            ) : creatingName === null ? (
                                <button
                                    type="button"
                                    className="btn-secondary"
                                    data-testid="task-transfer-create-workspace"
                                    disabled={busy || creatingBusy || !onCreateCloudWorkspace}
                                    onClick={beginCreate}
                                >{textForLang(lang, 'New cloud workspace…', '新建云端工作区…', '新建雲端工作區…')}</button>
                            ) : (
                                <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                                    <input
                                        ref={createInputRef}
                                        data-testid="task-transfer-create-name"
                                        aria-label={textForLang(lang, 'Workspace name', '工作区名称', '工作區名稱')}
                                        placeholder={textForLang(lang, 'Workspace name', '工作区名称', '工作區名稱')}
                                        className="cwshare-input"
                                        style={{ flex: '1 1 auto', minWidth: 0 }}
                                        readOnly={creatingBusy}
                                        value={creatingName}
                                        onChange={event => setCreatingName(event.target.value)}
                                        onKeyDown={event => {
                                            event.stopPropagation();
                                            if (isImeComposing(event) || event.key !== 'Enter') return;
                                            event.preventDefault();
                                            void submitCreate();
                                        }}
                                    />
                                    <button
                                        type="button"
                                        className="btn-primary"
                                        data-testid="task-transfer-create-submit"
                                        disabled={creatingBusy || !(creatingName ?? '').trim()}
                                        onClick={() => { void submitCreate(); }}
                                    >
                                        {creatingBusy
                                            ? textForLang(lang, 'Creating…', '正在创建…', '正在建立…')
                                            : textForLang(lang, 'Create', '创建', '建立')}
                                    </button>
                                </div>
                            )}
                        </div>
                    ) : null}

                    {error ? <div role="alert" data-testid="task-transfer-error" className="cwshare-alert cwshare-alert--error">{error}</div> : null}
                    {createError ? <div role="alert" data-testid="task-transfer-create-error" className="cwshare-alert cwshare-alert--error">{createError}</div> : null}
                </div>
                <div className="modal-footer cwshare-footer">
                    <button type="button" className="btn-secondary" data-testid="task-transfer-cancel" disabled={busy || creatingBusy} onClick={onClose}>
                        {textForLang(lang, 'Cancel', '取消', '取消')}
                    </button>
                    <button type="button" className="btn-primary" data-testid="task-transfer-confirm" disabled={confirmDisabled} onClick={confirm}>
                        {busy ? textForLang(lang, 'Moving…', '正在转移…', '正在轉移…') : confirmLabel}
                    </button>
                </div>
            </div>
        </div>,
        document.body,
    );
}

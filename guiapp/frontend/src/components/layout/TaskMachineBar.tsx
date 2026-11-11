import { useEffect, useRef, useState } from 'react';
import { localizeText } from '../../i18n';
import type { RemotePermission, useTaskRemoteControl } from './useTaskRemoteControl';
import { bindRemoteAssistantActions, publishRemoteAssistantSession } from './remoteAssistantSession';
import { useSafeBackdropDismiss } from '../../hooks/useSafeBackdropDismiss';
import { useModalFocusTrap } from '../../hooks/useModalFocusTrap';

type RemoteControl = ReturnType<typeof useTaskRemoteControl>;

function confirmKeyIsIme(event: { key: string; nativeEvent: KeyboardEvent; keyCode: number }) {
    return event.key === 'Enter' && (event.nativeEvent.isComposing || event.keyCode === 229);
}

function permissionLabel(lang: string, permission: string) {
    return permission === 'operate'
        ? localizeText(lang, 'Can operate', '可操作', '可操作')
        : localizeText(lang, 'View only', '仅查看', '僅查看');
}

// Mounted only while the add-machine dialog is open: the focus trap belongs to
// the dialog's lifetime, so its mount-only effects can move focus in and hand
// it back to the "+" button on the way out.
function AddMachineDialog({
    lang,
    remote,
    permission,
    onPermissionChange,
    account,
    onAccountChange,
    acting,
    runAction,
    onClose,
}: {
    lang: string;
    remote: RemoteControl;
    permission: RemotePermission;
    onPermissionChange: (permission: RemotePermission) => void;
    account: string;
    onAccountChange: (account: string) => void;
    acting: boolean;
    runAction: (work: () => Promise<unknown>) => Promise<void>;
    onClose: () => void;
}) {
    const { backdropProps, dialogProps } = useSafeBackdropDismiss<HTMLDivElement>(onClose);
    const dialogRef = useRef<HTMLDivElement | null>(null);
    // An in-flight grant/invite still lands after an impatient close, so keep
    // the dialog up (same contract as the bot settings dialogs' submit guard).
    const { onKeyDown } = useModalFocusTrap(dialogRef, { onEscape: () => { if (!acting) onClose(); } });
    const remoteActivity = !!remote.selected || remote.state.pending.length > 0 || remote.state.controlled.length > 0;
    const machines = [...remote.state.machines]
        // This computer is the viewer, not a target. A stale or duplicate
        // machine record for it must never become a grant option.
        .filter(machine => machine.id && machine.id !== remote.state.machine_id)
        .sort((left, right) =>
            Number(right.online) - Number(left.online)
            || (left.name || left.id).localeCompare(right.name || right.id, lang, { sensitivity: 'base' })
            || left.id.localeCompare(right.id));

    return (
        <div className="modal-backdrop" data-testid="task-machine-add-panel" {...backdropProps}>
            <div className="modal-content mc-task-machine-dialog" ref={dialogRef} role="dialog" aria-modal="true" aria-label={localizeText(lang, 'Add a machine', '添加机器', '新增機器')} tabIndex={-1} onKeyDown={onKeyDown} {...dialogProps}>
                <div className="modal-header">
                    <h3>{localizeText(lang, 'Add a machine', '添加机器', '新增機器')}</h3>
                    <button
                        type="button"
                        className="btn-close"
                        aria-label={localizeText(lang, 'Close', '关闭', '關閉')}
                        data-testid="task-machine-add-close"
                        onClick={onClose}
                    >
                        ×
                    </button>
                </div>
                <div className="modal-body">
                    <div className="mc-task-machine-add">
                        {remote.error && !remoteActivity ? (
                            <p className="mc-task-machine-error" data-testid="task-machine-error" role="alert">{remote.error}</p>
                        ) : null}
                        <div className="mc-task-machine-permission" role="group" aria-label={localizeText(lang, 'Permission', '权限', '權限')}>
                            <label className={`mc-task-machine-perm-option${permission === 'view' ? ' is-selected' : ''}`}>
                                <input className="mc-task-machine-perm-radio" type="radio" name="remote-permission" checked={permission === 'view'} onChange={() => onPermissionChange('view')} />
                                <svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="1.5" aria-hidden="true">
                                    <path d="M1.7 8S4.2 3.8 8 3.8 14.3 8 14.3 8 11.8 12.2 8 12.2 1.7 8 1.7 8Z" />
                                    <circle cx="8" cy="8" r="2.1" />
                                </svg>
                                <span>{permissionLabel(lang, 'view')}</span>
                            </label>
                            <label className={`mc-task-machine-perm-option${permission === 'operate' ? ' is-selected' : ''}`}>
                                <input className="mc-task-machine-perm-radio" type="radio" name="remote-permission" checked={permission === 'operate'} onChange={() => onPermissionChange('operate')} />
                                <svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinejoin="round" aria-hidden="true">
                                    <path d="M5 2.5l7.5 4.6-3.3.8L8 11.5 5 2.5z" />
                                </svg>
                                <span>{permissionLabel(lang, 'operate')}</span>
                            </label>
                        </div>
                        <section className="mc-task-machine-own">
                            <h3>{localizeText(lang, 'Other computers on this account', '同帐号的其它机器', '同帳號的其它機器')}</h3>
                            {remote.state.machines.length === 0
                                ? <p className="mc-task-machine-own-empty">{localizeText(lang, 'No other computer is registered on this account.', '这个帐号下没有其它已注册的电脑。', '這個帳號下沒有其它已註冊的電腦。')}</p>
                                : machines.map(machine => {
                                    const granted = remote.state.grants.find(grant => grant.machine_id === machine.id);
                                    return (
                                    <button
                                        key={machine.id}
                                        type="button"
                                        className="mc-task-machine-own-item"
                                        data-online={machine.online ? 'true' : 'false'}
                                        data-testid={`task-machine-own-${machine.id}`}
                                        title={granted ? localizeText(lang, 'Click again to update the permission', '再次点击会更新权限', '再次點擊會更新權限') : undefined}
                                        disabled={acting}
                                        onClick={() => {
                                            void runAction(async () => {
                                                const ok = await remote.grantSameAccount(machine.id, permission);
                                                if (ok !== false) onClose();
                                            });
                                        }}
                                    >
                                        <span className="mc-task-machine-own-dot" aria-hidden="true" />
                                        <span className="mc-task-machine-own-text">
                                            <span className="mc-task-machine-own-name">{machine.name || machine.id}</span>
                                            <span className="mc-task-machine-own-status">{' · '}{machine.online ? localizeText(lang, 'Online', '在线', '在線') : localizeText(lang, 'Offline', '离线', '離線')}</span>
                                        </span>
                                        {granted ? <span className="mc-task-machine-own-badge">{permissionLabel(lang, granted.permission)}</span> : null}
                                        <span className="mc-task-machine-own-chevron" aria-hidden="true">›</span>
                                    </button>
                                    );
                                })}
                        </section>
                        <form
                            className="mc-task-machine-invite"
                            onSubmit={(event) => {
                                event.preventDefault();
                                const value = account.trim();
                                if (!value || acting) return;
                                void runAction(async () => {
                                    const ok = await remote.invite(value, permission);
                                    if (ok !== false) { onAccountChange(''); onClose(); }
                                });
                            }}
                        >
                            <h3>{localizeText(lang, 'Another account', '其它帐号', '其它帳號')}</h3>
                            <div className="mc-task-machine-invite-field">
                                <svg viewBox="0 0 16 16" width="13" height="13" fill="none" stroke="currentColor" strokeWidth="1.3" aria-hidden="true">
                                    <rect x="1.8" y="3.2" width="12.4" height="9.6" rx="1.6" />
                                    <path d="m2.5 4.5 5.5 4 5.5-4" />
                                </svg>
                                <input
                                    data-testid="task-machine-account"
                                    value={account}
                                    placeholder={localizeText(lang, 'Phone or email', '手机号或邮箱', '手機號或郵箱')}
                                    onKeyDown={(event) => { if (confirmKeyIsIme(event)) event.preventDefault(); }}
                                    onChange={(event) => onAccountChange(event.target.value)}
                                />
                            </div>
                            <button type="submit" className="mc-task-machine-primary" data-testid="task-machine-invite" disabled={acting}>{localizeText(lang, 'Send invite', '发送邀请', '傳送邀請')}</button>
                        </form>
                    </div>
                </div>
            </div>
        </div>
    );
}

export function TaskMachineBar({
    lang,
    remote,
    onCloseGrant,
}: {
    lang: string;
    remote: RemoteControl;
    onCloseGrant: (grantId: string, name: string) => void;
}) {
    const [adding, setAdding] = useState(false);
    const [permission, setPermission] = useState<RemotePermission>('view');
    const [account, setAccount] = useState('');
    const [createName, setCreateName] = useState('');
    const [acting, setActing] = useState(false);
    const actingRef = useRef(false);
    // The create box stays mounted across machine tabs. Don't carry a name
    // typed for one computer onto the next.
    useEffect(() => { setCreateName(''); }, [remote.selected?.id]);
    const selected = remote.selected;
    const detail = remote.detail;
    const detailPath = remote.detailPath;
    const detailLoading = remote.detailLoading;
    const detailBusy = remote.detailBusy;
    const remoteTasks = remote.tasks;
    const operate = remote.operate;
    const error = remote.error;
    const sendMessage = remote.sendMessage;
    const stopTask = remote.stopTask;
    const closeDetail = remote.closeDetail;
    const chatError = (remote.errorKind === 'action' || remote.errorKind === 'conversation') && !!detailPath ? error : '';
    useEffect(() => {
        if (!selected || !detailPath) {
            publishRemoteAssistantSession(null);
            bindRemoteAssistantActions(null);
            return;
        }
        publishRemoteAssistantSession({
            grantId: selected.id,
            machineName: selected.machine_name || selected.machine_id || '',
            projectPath: detailPath,
            taskName: detail?.task?.name
                || remoteTasks.find(item => item.project_path === detailPath)?.name
                || detailPath,
            canSend: !!operate && selected.online !== false,
            offline: selected.online === false,
            error: chatError,
            // An offline grant never starts a fetch, so an empty transcript
            // must not read as "still loading" forever.
            loading: selected.online !== false && (detailLoading || !detail),
            busy: !!detailBusy && selected.online !== false,
            messages: (detail?.messages || []).map(message => {
                const content = (message.content || '').trim();
                const reasoning = (message.reasoning_content || '').trim();
                return {
                    role: message.role,
                    content: content || reasoning,
                    reasoning: content ? reasoning : '',
                };
            }).filter(message => message.content.trim() !== ''),
        });
        bindRemoteAssistantActions({
            send: text => sendMessage(detailPath, text),
            stop: () => stopTask(detailPath),
            close: closeDetail,
        });
    }, [chatError, closeDetail, detail, detailBusy, detailLoading, detailPath, operate, remoteTasks, selected, sendMessage, stopTask]);
    useEffect(() => () => {
        publishRemoteAssistantSession(null);
        bindRemoteAssistantActions(null);
    }, []);
    const runAction = async (work: () => Promise<unknown>) => {
        if (actingRef.current) return;
        actingRef.current = true;
        setActing(true);
        try {
            await work();
        } finally {
            actingRef.current = false;
            setActing(false);
        }
    };
    const viewOnly = !!selected && selected.permission !== 'operate';
    const remoteActivity = !!selected || remote.state.pending.length > 0 || remote.state.controlled.length > 0;
    // A failure belongs next to the form the person is using. The tab row
    // only keeps it when that form is not open, so the same text is not shown twice.
    const errorInDetail = !!chatError;
    const errorInCreate = !!(remote.error && selected && remote.creating && !errorInDetail);

    return (
        <div className="mc-task-machine-bar" data-testid="task-machine-bar">
            <div className="mc-task-machine-tabs">
                <div className="mc-task-machine-tablist" role="tablist" aria-label={localizeText(lang, 'Machines', '机器', '機器')}>
                <button
                    type="button"
                    role="tab"
                    aria-selected={!selected}
                    className={`mc-task-machine-tab${!selected ? ' is-active' : ''}`}
                    data-testid="task-machine-tab-local"
                    onClick={remote.selectLocal}
                >
                    {localizeText(lang, 'This computer', '本机', '本機')}
                </button>
                {remote.state.grants.map(grant => (
                    <span key={grant.id} className={`mc-task-machine-tab-wrap${selected?.id === grant.id ? ' is-active' : ''}`}>
                        <button
                            type="button"
                            role="tab"
                            aria-selected={selected?.id === grant.id}
                            className={`mc-task-machine-tab${selected?.id === grant.id ? ' is-active' : ''}`}
                            data-testid={`task-machine-tab-${grant.id}`}
                            title={grant.online === false ? localizeText(lang, 'Offline', '离线', '離線') : permissionLabel(lang, grant.permission)}
                            onClick={() => remote.selectGrant(grant.id)}
                        >
                            <span>{grant.machine_name || grant.machine_id}</span>
                            {grant.online === false ? <i data-testid={`task-machine-offline-${grant.id}`}>{localizeText(lang, 'Offline', '离线', '離線')}</i> : null}
                        </button>
                        <button
                            type="button"
                            className="mc-task-machine-tab-close"
                            aria-label={localizeText(lang, `End remote control of ${grant.machine_name || grant.machine_id}`, `结束对${grant.machine_name || grant.machine_id}的远控`, `結束對${grant.machine_name || grant.machine_id}的遠控`)}
                            data-testid={`task-machine-close-${grant.id}`}
                            onClick={() => onCloseGrant(grant.id, grant.machine_name || grant.machine_id || '')}
                        >
                            ×
                        </button>
                    </span>
                ))}
                </div>
                {remote.error && remoteActivity && !errorInDetail && !errorInCreate ? (
                    <span className="mc-task-machine-error" data-testid="task-machine-error" role="alert" title={remote.error}>{remote.error}</span>
                ) : null}
                <button
                    type="button"
                    className="mc-task-machine-tab mc-task-machine-tab--add"
                    data-testid="task-machine-add"
                    aria-expanded={adding}
                    aria-haspopup="dialog"
                    aria-label={localizeText(lang, 'Add a machine', '添加机器', '新增機器')}
                    onClick={() => {
                        if (!adding) void remote.refresh();
                        setAdding(open => !open);
                    }}
                >
                    +
                </button>
            </div>
            {selected ? (
                <div className="mc-task-machine-note" data-testid="task-machine-note" role="status">
                    {selected.online === false
                        ? localizeText(lang, 'This computer is offline. Its tasks stay there and cannot be used from here.', '对方离线。任务仍在那台电脑上，这里暂时不能使用。', '對方離線。任務仍在那台電腦上，這裡暫時不能使用。')
                        : viewOnly
                            ? localizeText(lang, 'View only. Tasks run on that computer.', '仅查看。任务在对方电脑上运行。', '僅查看。任務在對方電腦上運行。')
                            : localizeText(lang, 'You can operate. Tasks run on that computer.', '可操作。任务在对方电脑上运行。', '可操作。任務在對方電腦上運行。')}
                </div>
            ) : null}
            {remote.state.pending.length > 0 || remote.state.controlled.length > 0 ? (
                <div className="mc-task-machine-add">
                    {remote.state.pending.length > 0 ? (
                        <section data-testid="task-machine-pending">
                            <h3>{localizeText(lang, 'Waiting for the other person', '等待对方处理', '等待對方處理')}</h3>
                            {remote.state.pending.map(invite => (
                                <div key={invite.id} className="mc-task-machine-row">
                                    <span className="mc-task-machine-row-text">
                                        <span className="mc-task-machine-row-dot" data-state="pending" aria-hidden="true" />
                                        {invite.account} · {permissionLabel(lang, invite.permission)}
                                    </span>
                                    <button type="button" data-testid={`task-machine-cancel-${invite.id}`} disabled={acting} onClick={() => void runAction(() => remote.revoke(invite.id))}>{localizeText(lang, 'Cancel', '取消', '取消')}</button>
                                </div>
                            ))}
                        </section>
                    ) : null}
                    {remote.state.controlled.length > 0 ? (
                        <section data-testid="task-machine-controlled">
                            <h3>{localizeText(lang, 'This computer is being controlled', '这台电脑正在被远控', '這台電腦正在被遠控')}</h3>
                            {remote.state.controlled.map(item => (
                                <div key={item.id} className="mc-task-machine-row">
                                    <span className="mc-task-machine-row-text">
                                        <span className="mc-task-machine-row-dot" data-state="active" aria-hidden="true" />
                                        {item.from_machine} · {permissionLabel(lang, item.permission)}
                                    </span>
                                    <button type="button" data-testid={`task-machine-end-${item.id}`} disabled={acting} onClick={() => void runAction(() => remote.revoke(item.id))}>{localizeText(lang, 'End', '结束', '結束')}</button>
                                </div>
                            ))}
                        </section>
                    ) : null}
                </div>
            ) : null}
            {selected && remote.operate && remote.creating ? (
                <form
                    className="mc-task-machine-create"
                    data-testid="task-machine-create"
                    onSubmit={(event) => {
                        event.preventDefault();
                        const name = createName.trim();
                        if (!name || acting) return;
                        void runAction(async () => {
                            const ok = await remote.createTask(name);
                            if (ok) setCreateName('');
                        });
                    }}
                >
                    {errorInCreate ? <p className="mc-task-machine-error" data-testid="task-machine-error" role="alert">{remote.error}</p> : null}
                    <input
                        data-testid="task-machine-create-name"
                        value={createName}
                        placeholder={localizeText(lang, 'Task name on that computer', '在对方电脑上的任务名', '在對方電腦上的任務名')}
                        onKeyDown={(event) => { if (confirmKeyIsIme(event)) event.preventDefault(); }}
                        onChange={(event) => setCreateName(event.target.value)}
                    />
                    <button type="submit" disabled={acting}>{localizeText(lang, 'Create there', '在对方新建', '在對方新建')}</button>
                </form>
            ) : null}
            {adding ? (
                <AddMachineDialog
                    lang={lang}
                    remote={remote}
                    permission={permission}
                    onPermissionChange={setPermission}
                    account={account}
                    onAccountChange={setAccount}
                    acting={acting}
                    runAction={runAction}
                    onClose={() => setAdding(false)}
                />
            ) : null}
        </div>
    );
}

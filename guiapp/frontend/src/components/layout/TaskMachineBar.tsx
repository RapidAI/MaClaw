import { useCallback, useEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from 'react';
import { localizeText } from '../../i18n';
import type { RemotePermission, useTaskRemoteControl } from './useTaskRemoteControl';
import { bindRemoteAssistantActions, publishRemoteAssistantSession, bindRemoteTaskOpener } from './remoteAssistantSession';
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

// A granted tab shows a compact handle — a few full computer names would
// eat the whole strip. The tooltip (and aria-label) still carries it.
function machineTabShortLabel(lang: string, index: number) {
    return localizeText(lang, `M${index + 1}`, `机器${index + 1}`, `機器${index + 1}`);
}

// Matches the prefers-reduced-motion block in the shell styles: smooth
// scrolling is motion too.
function prefersReducedMotion() {
    return typeof window !== 'undefined' && typeof window.matchMedia === 'function'
        && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
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
    const [acting, setActing] = useState(false);
    const actingRef = useRef(false);
    const selected = remote.selected;
    const detail = remote.detail;
    const detailPath = remote.detailPath;
    const detailLoading = remote.detailLoading;
    const detailBusy = remote.detailBusy;
    const awaitingReply = remote.awaitingReply;
    const awaitingStop = remote.awaitingStop;
    const remoteTasks = remote.tasks;
    const operate = remote.operate;
    const error = remote.error;
    const sendMessage = remote.sendMessage;
    const stopTask = remote.stopTask;
    const closeDetail = remote.closeDetail;
    const chatError = (remote.errorKind === 'action' || remote.errorKind === 'conversation') && !!detailPath ? error : '';
    // Local wall time for "elapsed" on the live panel. It resets when the run
    // ends or the open task changes, so it never survives into another run.
    const busySinceRef = useRef<{ path: string; at: number }>({ path: '', at: 0 });
    useEffect(() => {
        if (!selected || !detailPath) {
            busySinceRef.current = { path: '', at: 0 };
            publishRemoteAssistantSession(null);
            bindRemoteAssistantActions(null);
            return;
        }
        if (detailBusy) {
            if (busySinceRef.current.path !== detailPath) {
                busySinceRef.current = { path: detailPath, at: Date.now() };
            }
        } else {
            busySinceRef.current = { path: '', at: 0 };
        }
        // The task-list row carries the fresher heartbeat; the detail
        // snapshot only updates when the conversation poll lands.
        const openTaskRow = remoteTasks.find(item => item.project_path === detailPath);
        publishRemoteAssistantSession({
            grantId: selected.id,
            machineName: selected.machine_name || selected.machine_id || '',
            projectPath: detailPath,
            taskName: detail?.task?.name
                || openTaskRow?.name
                || detailPath,
            canSend: !!operate && selected.online !== false,
            offline: selected.online === false,
            error: chatError,
            // An offline grant never starts a fetch, so an empty transcript
            // must not read as "still loading" forever.
            loading: selected.online !== false && (detailLoading || !detail),
            busy: !!detailBusy && selected.online !== false,
            activity: {
                awaitingReply: !!awaitingReply && selected.online !== false,
                stopping: !!awaitingStop && selected.online !== false,
                // The list row polls on its own cadence and often lands the
                // workflow snapshot before get_task does; use it as fallback.
                workflow: detail?.task?.active_workflow ?? openTaskRow?.active_workflow ?? null,
                workflowType: detail?.task?.workflow_type || openTaskRow?.workflow_type || '',
                lastActivityAt: openTaskRow?.last_activity || detail?.task?.last_activity || '',
                busySince: detailBusy && busySinceRef.current.at ? busySinceRef.current.at : undefined,
            },
            // Keep thinking in `reasoning` so it renders as a 思考过程 block
            // like a local task. A turn that is still thinking has no content
            // yet; promoting the trail to content painted it as plain prose.
            messages: (detail?.messages || []).map(message => {
                const content = (message.content || '').trim();
                const reasoning = (message.reasoning_content || '').trim();
                return { role: message.role, content, reasoning };
            }).filter(message => message.content !== '' || (message.reasoning || '') !== ''),
        });
        bindRemoteAssistantActions({
            send: text => sendMessage(detailPath, text),
            stop: () => stopTask(detailPath),
            close: closeDetail,
        });
    }, [awaitingReply, awaitingStop, chatError, closeDetail, detail, detailBusy, detailLoading, detailPath, operate, remoteTasks, selected, sendMessage, stopTask]);
    useEffect(() => () => {
        publishRemoteAssistantSession(null);
        bindRemoteAssistantActions(null);
    }, []);
    // The new-task wizard can create on any controlled machine; it asks this
    // bridge to switch to that machine's tab and open the fresh task. Bound
    // once: remote's identity changes every render, so dispatch through a ref.
    const remoteRef = useRef(remote);
    remoteRef.current = remote;
    useEffect(() => {
        bindRemoteTaskOpener((grantId, projectPath) => remoteRef.current.openTaskForGrant(grantId, projectPath));
        return () => bindRemoteTaskOpener(null);
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
    // The tab strip scrolls horizontally with the scrollbar hidden, so the
    // edges fade in wherever more tabs are waiting and the active tab walks
    // itself into view when it is selected.
    const tablistRef = useRef<HTMLDivElement | null>(null);
    const activeTabRef = useRef<HTMLButtonElement | null>(null);
    const [tabFades, setTabFades] = useState({ left: false, right: false, overflow: false });
    const updateTabFades = useCallback(() => {
        const el = tablistRef.current;
        if (!el) return;
        const maxScroll = el.scrollWidth - el.clientWidth;
        const next = { left: el.scrollLeft > 1, right: el.scrollLeft < maxScroll - 1, overflow: maxScroll > 1 };
        // Scroll events fire per frame; skip the render when nothing moved.
        setTabFades(prev => prev.left === next.left && prev.right === next.right && prev.overflow === next.overflow ? prev : next);
    }, []);
    const scrollTabs = useCallback((direction: -1 | 1) => {
        const el = tablistRef.current;
        if (!el) return;
        el.scrollBy({ left: direction * Math.max(80, el.clientWidth * 0.7), behavior: prefersReducedMotion() ? 'auto' : 'smooth' });
    }, []);
    // A tablist moves focus with the arrow keys; activation stays on
    // click/Enter. Focus scrolls an off-screen tab into view on its own.
    const onTabsKeyDown = useCallback((event: ReactKeyboardEvent<HTMLDivElement>) => {
        if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return;
        const tabs = Array.from(event.currentTarget.querySelectorAll<HTMLElement>('[role="tab"]'));
        const index = tabs.indexOf(document.activeElement as HTMLElement);
        if (index < 0) return;
        event.preventDefault();
        const step = event.key === 'ArrowRight' ? 1 : -1;
        tabs[(index + step + tabs.length) % tabs.length]?.focus();
    }, []);
    useEffect(() => {
        updateTabFades();
        const el = tablistRef.current;
        if (!el || typeof ResizeObserver === 'undefined') return;
        const observer = new ResizeObserver(() => updateTabFades());
        observer.observe(el);
        return () => observer.disconnect();
    }, [remote.state.grants, updateTabFades]);
    useEffect(() => {
        const el = activeTabRef.current;
        if (!el || typeof el.scrollIntoView !== 'function') return;
        el.scrollIntoView({ behavior: prefersReducedMotion() ? 'auto' : 'smooth', inline: 'nearest', block: 'nearest' });
    }, [selected?.id]);
    // Vertical wheel input scrolls the strip sideways (trackpads and wheels
    // send deltaY; React's onWheel is passive, so this needs a raw listener).
    useEffect(() => {
        const el = tablistRef.current;
        if (!el) return;
        const onWheel = (event: WheelEvent) => {
            if (event.deltaY && !event.deltaX) {
                event.preventDefault();
                el.scrollLeft += event.deltaY;
                updateTabFades();
            }
        };
        el.addEventListener('wheel', onWheel, { passive: false });
        return () => el.removeEventListener('wheel', onWheel);
    }, [updateTabFades]);

    return (
        <div className="mc-task-machine-bar" data-testid="task-machine-bar">
            {/* The local tab lives outside the scroll strip so it can never
                be scrolled away; the ‹ › buttons only move the grant tabs. */}
            <div className="mc-task-machine-tabs" role="tablist" aria-label={localizeText(lang, 'Machines', '机器', '機器')} onKeyDown={onTabsKeyDown}>
                <button
                    type="button"
                    role="tab"
                    aria-selected={!selected}
                    className={`mc-task-machine-tab${!selected ? ' is-active' : ''}`}
                    data-testid="task-machine-tab-local"
                    onClick={remote.selectLocal}
                >
                    {localizeText(lang, 'MyPC', '本机', '本機')}
                </button>
                {tabFades.overflow ? (
                    <button
                        type="button"
                        className="mc-task-machine-tab-nav mc-task-machine-tab-nav--left"
                        data-testid="task-machine-scroll-left"
                        aria-label={localizeText(lang, 'Scroll machines left', '向左滚动机器标签', '向左滾動機器標籤')}
                        disabled={!tabFades.left}
                        onClick={() => scrollTabs(-1)}
                    >
                        ‹
                    </button>
                ) : null}
                <div className="mc-task-machine-tablist-wrap">
                    <div className="mc-task-machine-tablist" ref={tablistRef} onScroll={updateTabFades}>
                    {remote.state.grants.map((grant, index) => {
                        const machineName = grant.machine_name || grant.machine_id || '';
                        const tabStatus = grant.online === false
                            ? localizeText(lang, 'Offline', '离线', '離線')
                            : permissionLabel(lang, grant.permission);
                        const shortLabel = machineTabShortLabel(lang, index);
                        // A grant from another account shares the machine name
                        // alone — the account disambiguates whose machine it is.
                        const fullName = grant.account ? `${machineName} (${grant.account})` : machineName;
                        return (
                        <span key={grant.id} className={`mc-task-machine-tab-wrap${selected?.id === grant.id ? ' is-active' : ''}`}>
                            <button
                                type="button"
                                role="tab"
                                aria-selected={selected?.id === grant.id}
                                aria-label={fullName || shortLabel}
                                className={`mc-task-machine-tab${selected?.id === grant.id ? ' is-active' : ''}`}
                                data-testid={`task-machine-tab-${grant.id}`}
                                data-offline={grant.online === false ? 'true' : undefined}
                                title={fullName ? `${fullName} · ${tabStatus}` : tabStatus}
                                ref={selected?.id === grant.id ? activeTabRef : undefined}
                                onClick={() => remote.selectGrant(grant.id)}
                            >
                                <span
                                    className="mc-task-machine-tab-dot"
                                    data-online={grant.online === false ? 'false' : 'true'}
                                    role={grant.online === false ? 'img' : undefined}
                                    aria-label={grant.online === false ? localizeText(lang, 'Offline', '离线', '離線') : undefined}
                                    data-testid={grant.online === false ? `task-machine-offline-${grant.id}` : undefined}
                                />
                                <span>{shortLabel}</span>
                            </button>
                            <button
                                type="button"
                                className="mc-task-machine-tab-close"
                                aria-label={localizeText(lang, `End remote control of ${machineName || shortLabel}`, `结束对${machineName || shortLabel}的远控`, `結束對${machineName || shortLabel}的遠控`)}
                                title={localizeText(lang, 'End remote control', '结束远控', '結束遠控')}
                                data-testid={`task-machine-close-${grant.id}`}
                                onClick={() => onCloseGrant(grant.id, machineName || shortLabel)}
                            >
                                ×
                            </button>
                        </span>
                        );
                    })}
                    </div>
                    <span className="mc-task-machine-tab-fade mc-task-machine-tab-fade--left" data-show={tabFades.left ? 'true' : 'false'} aria-hidden="true" />
                    <span className="mc-task-machine-tab-fade mc-task-machine-tab-fade--right" data-show={tabFades.right ? 'true' : 'false'} aria-hidden="true" />
                </div>
                {tabFades.overflow ? (
                    <button
                        type="button"
                        className="mc-task-machine-tab-nav mc-task-machine-tab-nav--right"
                        data-testid="task-machine-scroll-right"
                        aria-label={localizeText(lang, 'Scroll machines right', '向右滚动机器标签', '向右滾動機器標籤')}
                        disabled={!tabFades.right}
                        onClick={() => scrollTabs(1)}
                    >
                        ›
                    </button>
                ) : null}
                {remote.error && remoteActivity && !errorInDetail ? (
                    <span className="mc-task-machine-error" data-testid="task-machine-error" role="alert" title={remote.error}>{remote.error}</span>
                ) : null}
                <button
                    type="button"
                    className="mc-task-machine-tab mc-task-machine-tab--add"
                    data-testid="task-machine-add"
                    aria-expanded={adding}
                    aria-haspopup="dialog"
                    aria-label={localizeText(lang, 'Add a machine', '添加机器', '新增機器')}
                    title={localizeText(lang, 'Add a machine', '添加机器', '新增機器')}
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

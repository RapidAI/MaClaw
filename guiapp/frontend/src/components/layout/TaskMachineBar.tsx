import { useRef, useState } from 'react';
import { localizeText } from '../../i18n';
import type { RemotePermission, useTaskRemoteControl } from './useTaskRemoteControl';

type RemoteControl = ReturnType<typeof useTaskRemoteControl>;

function permissionLabel(lang: string, permission: string) {
    return permission === 'operate'
        ? localizeText(lang, 'Can operate', '可操作', '可操作')
        : localizeText(lang, 'View only', '仅查看', '僅查看');
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
    const [draft, setDraft] = useState('');
    const [acting, setActing] = useState(false);
    const actingRef = useRef(false);
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
    const selected = remote.selected;
    const viewOnly = !!selected && selected.permission !== 'operate';

    return (
        <div className="mc-task-machine-bar" data-testid="task-machine-bar">
            <div className="mc-task-machine-tabs" role="tablist" aria-label={localizeText(lang, 'Machines', '机器', '機器')}>
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
                {remote.error ? (
                    <span className="mc-task-machine-error" data-testid="task-machine-error" role="alert" title={remote.error}>{remote.error}</span>
                ) : null}
                <button
                    type="button"
                    className="mc-task-machine-tab mc-task-machine-tab--add"
                    data-testid="task-machine-add"
                    aria-expanded={adding}
                    aria-label={localizeText(lang, 'Add a machine', '添加机器', '新增機器')}
                    onClick={() => setAdding(open => !open)}
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
                                <p key={invite.id}>
                                    {invite.account} · {permissionLabel(lang, invite.permission)}
                                    <button type="button" data-testid={`task-machine-cancel-${invite.id}`} disabled={acting} onClick={() => void runAction(() => remote.revoke(invite.id))}>{localizeText(lang, 'Cancel', '取消', '取消')}</button>
                                </p>
                            ))}
                        </section>
                    ) : null}
                    {remote.state.controlled.length > 0 ? (
                        <section data-testid="task-machine-controlled">
                            <h3>{localizeText(lang, 'This computer is being controlled', '这台电脑正在被远控', '這台電腦正在被遠控')}</h3>
                            {remote.state.controlled.map(item => (
                                <p key={item.id}>
                                    {item.from_machine} · {permissionLabel(lang, item.permission)}
                                    <button type="button" data-testid={`task-machine-end-${item.id}`} disabled={acting} onClick={() => void runAction(() => remote.revoke(item.id))}>{localizeText(lang, 'End', '结束', '結束')}</button>
                                </p>
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
                    <input
                        data-testid="task-machine-create-name"
                        value={createName}
                        placeholder={localizeText(lang, 'Task name on that computer', '在对方电脑上的任务名', '在對方電腦上的任務名')}
                        onChange={(event) => setCreateName(event.target.value)}
                    />
                    <button type="submit" disabled={acting}>{localizeText(lang, 'Create there', '在对方新建', '在對方新建')}</button>
                </form>
            ) : null}
            {remote.detail && remote.detailPath ? (
                <section className="mc-task-machine-detail" data-testid="task-machine-detail">
                    <header>
                        <strong>{remote.detail.task?.name || remote.detailPath}</strong>
                        <button type="button" data-testid="task-machine-detail-close" onClick={remote.closeDetail}>{localizeText(lang, 'Close', '关闭', '關閉')}</button>
                    </header>
                    <div className="mc-task-machine-messages">
                        {remote.detail.messages.length === 0
                            ? <p>{localizeText(lang, 'No conversation yet', '还没有会话', '還沒有工作階段')}</p>
                            : remote.detail.messages.map((message, index) => (
                                <p key={`${message.role}-${index}`}><b>{message.role}</b> {message.content}</p>
                            ))}
                    </div>
                    {remote.detail.artifacts.length > 0 ? (
                        <ul className="mc-task-machine-artifacts" data-testid="task-machine-artifacts">
                            {remote.detail.artifacts.map((artifact, index) => (
                                <li key={`${artifact.title || 'artifact'}-${index}`}>{artifact.title || artifact.preview}</li>
                            ))}
                        </ul>
                    ) : null}
                    {remote.operate ? (
                        <form
                            className="mc-task-machine-send"
                            onSubmit={(event) => {
                                event.preventDefault();
                                const text = draft.trim();
                                if (!text || acting) return;
                                void runAction(async () => {
                                    const ok = await remote.sendMessage(remote.detailPath, text);
                                    if (ok !== false) setDraft('');
                                });
                            }}
                        >
                            <input
                                data-testid="task-machine-send-input"
                                value={draft}
                                placeholder={localizeText(lang, 'Message to run on that computer', '发到对方电脑上运行', '發到對方電腦上運行')}
                                onChange={(event) => setDraft(event.target.value)}
                            />
                            <button type="submit" data-testid="task-machine-send" disabled={acting}>{localizeText(lang, 'Send', '发送', '傳送')}</button>
                            <button type="button" data-testid="task-machine-stop" onClick={() => void remote.stopTask(remote.detailPath)}>{localizeText(lang, 'Stop', '停止', '停止')}</button>
                        </form>
                    ) : null}
                </section>
            ) : null}
            {adding ? (
                <div className="mc-task-machine-add" data-testid="task-machine-add-panel">
                    <div className="mc-task-machine-permission" role="group" aria-label={localizeText(lang, 'Permission', '权限', '權限')}>
                        <label><input type="radio" name="remote-permission" checked={permission === 'view'} onChange={() => setPermission('view')} />{permissionLabel(lang, 'view')}</label>
                        <label><input type="radio" name="remote-permission" checked={permission === 'operate'} onChange={() => setPermission('operate')} />{permissionLabel(lang, 'operate')}</label>
                    </div>
                    <section>
                        <h3>{localizeText(lang, 'Other computers on this account', '同帐号的其它机器', '同帳號的其它機器')}</h3>
                        {remote.state.machines.length === 0
                            ? <p>{localizeText(lang, 'No other computer is registered on this account.', '这个帐号下没有其它已注册的电脑。', '這個帳號下沒有其它已註冊的電腦。')}</p>
                            : remote.state.machines.map(machine => {
                                const granted = remote.state.grants.find(grant => grant.machine_id === machine.id);
                                return (
                                <button
                                    key={machine.id}
                                    type="button"
                                    data-testid={`task-machine-own-${machine.id}`}
                                    title={granted ? localizeText(lang, 'Click again to update the permission', '再次点击会更新权限', '再次點擊會更新權限') : undefined}
                                    disabled={acting}
                                    onClick={() => {
                                        void runAction(async () => {
                                            const ok = await remote.grantSameAccount(machine.id, permission);
                                            if (ok !== false) setAdding(false);
                                        });
                                    }}
                                >
                                    {machine.name || machine.id} · {machine.online ? localizeText(lang, 'Online', '在线', '在線') : localizeText(lang, 'Offline', '离线', '離線')}
                                    {granted ? ` · ${permissionLabel(lang, granted.permission)}` : ''}
                                </button>
                                );
                            })}
                    </section>
                    <form
                        onSubmit={(event) => {
                            event.preventDefault();
                            const value = account.trim();
                            if (!value || acting) return;
                            void runAction(async () => {
                                const ok = await remote.invite(value, permission);
                                if (ok !== false) { setAccount(''); setAdding(false); }
                            });
                        }}
                    >
                        <h3>{localizeText(lang, 'Another account', '其它帐号', '其它帳號')}</h3>
                        <input
                            data-testid="task-machine-account"
                            value={account}
                            placeholder={localizeText(lang, 'Phone or email', '手机号或邮箱', '手機號或郵箱')}
                            onChange={(event) => setAccount(event.target.value)}
                        />
                        <button type="submit" data-testid="task-machine-invite" disabled={acting}>{localizeText(lang, 'Send invite', '发送邀请', '傳送邀請')}</button>
                    </form>
                </div>
            ) : null}
        </div>
    );
}

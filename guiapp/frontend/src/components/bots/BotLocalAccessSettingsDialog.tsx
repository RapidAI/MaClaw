import { useEffect, useId, useRef, useState, type ReactNode } from 'react';
import { useSafeBackdropDismiss } from '../../hooks/useSafeBackdropDismiss';
import { useModalFocusTrap } from '../../hooks/useModalFocusTrap';
import { localizeText } from '../../i18n';
import { GetBotLocalAccess, PickBotLocalAccessFolder, SaveBotLocalAccess } from '../../../wailsjs/go/main/App';
import { localPermission, type BotLocalAccessSettings, type BotLocalAccessState, type BotLocalPermission } from './botLocalAccess';

function words(lang: string) {
    const L = (en: string, zh: string, hant?: string) => localizeText(lang, en, zh, hant);
    return {
        title: L('Access to this computer', '本地访问'),
        hint: L(
            'Bots run in the cloud. Through this app they can read and save files on this computer and reach hosts on its network (for example an intranet GPU server). Permission below chooses whether that asks you first. Existing files are never overwritten.',
            'Bot 在云端运行。经由本机 MaClaw 中转，它们可以读写这台电脑上的文件，也可以访问这台电脑所在网络里的机器（例如内网 GPU 服务器）。下面的权限决定要不要先询问。已有文件不会被覆盖。',
            'Bot 在雲端執行。經由本機 MaClaw 中轉，它們可以讀寫這台電腦上的檔案，也可以存取這台電腦所在網路裡的機器（例如內網 GPU 伺服器）。下面的權限決定要不要先詢問。已有檔案不會被覆蓋。'),
        permission: L('Permission', '权限', '權限'),
        ask: L('Ask', '询问', '詢問'),
        askHint: L(
            'Ask before a bot writes a file here or reaches a new folder or host. This is the default.',
            '写入本地文件，或访问新的文件夹和主机之前先询问。这是默认。',
            '寫入本機檔案，或存取新的資料夾和主機之前先詢問。這是預設。',
        ),
        full: L('Full access', '完全权限', '完全權限'),
        fullHint: L(
            'Do not ask. Bots can write files on this computer and reach its files and network directly. The size limit still applies, and existing files are still never overwritten.',
            '不再询问。Bot 可以直接写入本地文件，并访问这台电脑上的文件和网络。单个文件上限仍然有效，已有文件仍不会被覆盖。',
            '不再詢問。Bot 可以直接寫入本機檔案，並存取這台電腦上的檔案和網路。單一檔案上限仍然有效，已有檔案仍不會被覆蓋。',
        ),
        linkOn: L('Connected: bots can reach this computer now.', '已连接：Bot 现在可以经由本机中转。', '已連線：Bot 現在可以經由本機中轉。'),
        linkOff: L('Not connected to the server yet.', '尚未连上服务器。', '尚未連上伺服器。'),
        files: L('Allow bots to use files on this computer', '允许 Bot 使用本机文件', '允許 Bot 使用本機檔案'),
        folders: L('Allowed folders', '允许的文件夹', '允許的資料夾'),
        foldersHint: L('Unchecked folders are not the default place to write. Bots save into MaClaw/<bot name>. Ask confirms every other place; Full access writes there without asking.', '未勾选「可写」的不是默认写入位置。Bot 默认保存到 MaClaw/<Bot 名称>。询问时，其他位置会先问你；完全权限则直接写入。', '未勾選「可寫」的不是預設寫入位置。Bot 預設儲存到 MaClaw/<Bot 名稱>。詢問時，其他位置會先問你；完全權限則直接寫入。'),
        write: L('write', '可写', '可寫'),
        remove: L('Remove', '移除', '移除'),
        add: L('Add folder…', '添加文件夹…', '新增資料夾…'),
        maxSize: L('Max file size (MB)', '单个文件上限（MB）', '單一檔案上限（MB）'),
        network: L('Allow bots to reach this computer’s network', '允许 Bot 经本机访问内网', '允許 Bot 經本機存取內網'),
        netAllow: L('Pre-approved hosts (one per line)', '预先允许的主机（每行一个）', '預先允許的主機（每行一個）'),
        netHint: L('e.g. 192.168.1.0/24, 10.0.0.5:22, gpu.lab:22,8888. Ask confirms a host that is not listed. Full access connects without asking.', '例如 192.168.1.0/24、10.0.0.5:22、gpu.lab:22,8888。询问时，未列出的主机会先问你；完全权限则直接连接。', '例如 192.168.1.0/24、10.0.0.5:22、gpu.lab:22,8888。詢問時，未列出的主機會先問你；完全權限則直接連線。'),
        idle: L('Close idle tunnels after (minutes)', '隧道空闲多久后关闭（分钟）', '通道閒置多久後關閉（分鐘）'),
        confirm: L('Wait for my answer (seconds)', '询问等待时间（秒）', '詢問等待時間（秒）'),
        approved: (n: number) => L(`${n} remembered “always allow” answers`, `已记住 ${n} 条「始终允许」`, `已記住 ${n} 條「始終允許」`),
        clear: L('Forget them', '全部清除', '全部清除'),
        save: L('Save', '保存', '儲存'),
        cancel: L('Cancel', '取消', '取消'),
        close: L('Close', '关闭', '關閉'),
        loading: L('Loading…', '加载中…', '載入中…'),
    };
}

type Props = {
    heading: string;
    tabs?: ReactNode;
    lang: string;
    onClose: () => void;
};

/** The 本地访问 tab of the Bot 管理 Settings dialog. The policy lives on this computer. */
export function BotLocalAccessSettingsDialog({ heading, tabs, lang, onClose }: Props) {
    const w = words(lang);
    const titleId = useId();
    const [state, setState] = useState<BotLocalAccessState | null>(null);
    const [draft, setDraft] = useState<BotLocalAccessSettings | null>(null);
    const [netText, setNetText] = useState('');
    const [saving, setSaving] = useState(false);
    const [notice, setNotice] = useState('');
    const dialogRef = useRef<HTMLDivElement | null>(null);
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(onClose, { enabled: !saving });
    const { onKeyDown } = useModalFocusTrap(dialogRef, { onEscape: () => { if (!saving) onClose(); } });

    useEffect(() => {
        let live = true;
        GetBotLocalAccess().then((s: BotLocalAccessState) => {
            if (!live || !s) return;
            setState(s);
            setDraft({ ...s.settings, permission: localPermission(s.settings?.permission) });
            setNetText((s.settings.net_allow || []).join('\n'));
        }).catch((error: unknown) => setNotice(error instanceof Error ? error.message : String(error)));
        return () => { live = false; };
    }, []);

    const patch = (p: Partial<BotLocalAccessSettings>) => setDraft(d => (d ? { ...d, ...p } : d));
    const roots = draft?.roots || [];
    const permission = localPermission(draft?.permission);
    const choosePermission = (next: BotLocalPermission) => patch({ permission: next });

    const addFolder = async () => {
        try {
            const picked = await PickBotLocalAccessFolder();
            if (picked && !roots.some(r => r.path === picked)) patch({ roots: [...roots, { path: picked, write: false }] });
        } catch (error) {
            setNotice(error instanceof Error ? error.message : String(error));
        }
    };

    const save = async () => {
        if (!draft || saving) return;
        setSaving(true);
        setNotice('');
        try {
            const next = { ...draft, permission, net_allow: netText.split('\n').map(s => s.trim()).filter(Boolean) };
            const saved: BotLocalAccessState = await SaveBotLocalAccess(next);
            setState(saved);
            onClose();
        } catch (error) {
            setNotice(error instanceof Error ? error.message : String(error));
        } finally {
            setSaving(false);
        }
    };

    return (
        <div className="modal-backdrop custom-dialog-backdrop bot-llm-dialog-backdrop" {...backdropProps} onKeyDown={onKeyDown}>
            <div className="modal-content custom-dialog bot-llm-dialog" ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby={titleId} data-testid="bot-local-dialog" tabIndex={-1} {...dialogProps}>
                <div className="bot-llm-dialog__head">
                    <div className="modal-header custom-dialog__header">
                        <div className="custom-dialog__heading">
                            <h3 id={titleId} className="custom-dialog__title">{heading}</h3>
                        </div>
                        <button type="button" className="btn-close custom-dialog__close" aria-label={w.close} disabled={saving} onClick={onClose}>
                            <svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true"><path d="M3.2 3.2 10.8 10.8M10.8 3.2 3.2 10.8" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" /></svg>
                        </button>
                    </div>
                    {tabs}
                </div>
                <div className="modal-body custom-dialog__body">
                    <h4 className="bot-llm-dialog__section">{w.title}</h4>
                    <p className="bot-llm-dialog__hint">{w.hint}</p>
                    {state ? <p className="bot-llm-dialog__status" data-testid="bot-local-link">{state.connected ? w.linkOn : `${w.linkOff}${state.link_error ? ` (${state.link_error})` : ''}`}</p> : null}
                    {!draft ? <p className="bot-llm-dialog__subhint">{w.loading}</p> : (
                        <>
                            <div className="bot-local-dialog__permission" role="radiogroup" aria-labelledby={`${titleId}-permission`} data-testid="bot-local-permission">
                                <p className="bot-llm-dialog__label" id={`${titleId}-permission`}>{w.permission}</p>
                                {(['ask', 'full'] as const).map(value => (
                                    <div
                                        className={`bot-llm-dialog__option${permission === value ? ' is-selected' : ''}${saving ? ' is-disabled' : ''}`}
                                        key={value}
                                        onClick={() => { if (!saving) choosePermission(value); }}
                                    >
                                        <label className="bot-llm-dialog__radio">
                                            <input
                                                type="radio"
                                                name="bot-local-permission"
                                                value={value}
                                                checked={permission === value}
                                                disabled={saving}
                                                data-testid={`bot-local-permission-${value}`}
                                                onChange={() => choosePermission(value)}
                                            />
                                            <span>{value === 'ask' ? w.ask : w.full}</span>
                                        </label>
                                        <p className="bot-llm-dialog__subhint">{value === 'ask' ? w.askHint : w.fullHint}</p>
                                    </div>
                                ))}
                            </div>
                            <label className="bot-llm-dialog__radio">
                                <input type="checkbox" checked={draft.files_enabled} disabled={saving} data-testid="bot-local-files" onChange={e => patch({ files_enabled: e.target.checked })} />
                                <span>{w.files}</span>
                            </label>
                            <div className={`bot-local-dialog__group${draft.files_enabled ? '' : ' is-disabled'}`}>
                                <p className="bot-llm-dialog__label">{w.folders}</p>
                                <p className="bot-llm-dialog__subhint">{w.foldersHint}</p>
                                <ul className="bot-local-dialog__roots" data-testid="bot-local-roots">
                                    {roots.map((r, i) => (
                                        <li key={r.path} className="bot-local-dialog__root">
                                            <span className="bot-local-dialog__path" title={r.path}>{r.path}</span>
                                            <label className="bot-local-dialog__write">
                                                <input type="checkbox" checked={r.write} disabled={saving || !draft.files_enabled} onChange={e => patch({ roots: roots.map((x, j) => (j === i ? { ...x, write: e.target.checked } : x)) })} />
                                                {w.write}
                                            </label>
                                            <button type="button" className="btn-secondary bot-local-dialog__remove" disabled={saving || !draft.files_enabled} onClick={() => patch({ roots: roots.filter((_, j) => j !== i) })}>{w.remove}</button>
                                        </li>
                                    ))}
                                </ul>
                                <button type="button" className="btn-secondary" disabled={saving || !draft.files_enabled} data-testid="bot-local-add" onClick={() => { void addFolder(); }}>{w.add}</button>
                                <label className="bot-local-dialog__field">
                                    <span>{w.maxSize}</span>
                                    <input type="number" className="form-input" min={1} max={4096} value={draft.max_transfer_mb} disabled={saving || !draft.files_enabled} onChange={e => patch({ max_transfer_mb: Number(e.target.value) || 0 })} />
                                </label>
                            </div>
                            <label className="bot-llm-dialog__radio">
                                <input type="checkbox" checked={draft.network_enabled} disabled={saving} data-testid="bot-local-network" onChange={e => patch({ network_enabled: e.target.checked })} />
                                <span>{w.network}</span>
                            </label>
                            <div className={`bot-local-dialog__group${draft.network_enabled ? '' : ' is-disabled'}`}>
                                <label className="bot-local-dialog__field bot-local-dialog__field--block">
                                    <span>{w.netAllow}</span>
                                    <textarea className="form-input" rows={3} value={netText} disabled={saving || !draft.network_enabled} data-testid="bot-local-net-allow" onChange={e => setNetText(e.target.value)} />
                                </label>
                                <p className="bot-llm-dialog__subhint">{w.netHint}</p>
                                <label className="bot-local-dialog__field">
                                    <span>{w.idle}</span>
                                    <input type="number" className="form-input" min={1} value={draft.tunnel_idle_minutes} disabled={saving || !draft.network_enabled} onChange={e => patch({ tunnel_idle_minutes: Number(e.target.value) || 0 })} />
                                </label>
                            </div>
                            {permission === 'ask' ? (
                                <label className="bot-local-dialog__field">
                                    <span>{w.confirm}</span>
                                    <input type="number" className="form-input" min={10} max={600} value={draft.confirm_seconds} disabled={saving} data-testid="bot-local-confirm" onChange={e => patch({ confirm_seconds: Number(e.target.value) || 0 })} />
                                </label>
                            ) : null}
                            {(draft.approved || []).length > 0 ? (
                                <p className="bot-llm-dialog__subhint">
                                    {w.approved((draft.approved || []).length)}{' '}
                                    <button type="button" className="btn-link" data-testid="bot-local-clear" disabled={saving} onClick={() => patch({ approved: [] })}>{w.clear}</button>
                                </p>
                            ) : null}
                        </>
                    )}
                    {notice ? <p className="bot-llm-dialog__notice" role="status">{notice}</p> : null}
                </div>
                <div className="modal-footer custom-dialog__footer">
                    <button type="button" className="btn-secondary" onClick={onClose} disabled={saving}>{w.cancel}</button>
                    <button type="button" className="btn-primary" data-testid="bot-local-save" onClick={() => { void save(); }} disabled={saving || !draft}>{w.save}</button>
                </div>
            </div>
        </div>
    );
}

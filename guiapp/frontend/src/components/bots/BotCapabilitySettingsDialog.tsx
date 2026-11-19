import { useEffect, useId, useRef, useState, type ReactNode } from 'react';
import { useSafeBackdropDismiss } from '../../hooks/useSafeBackdropDismiss';
import { useModalFocusTrap } from '../../hooks/useModalFocusTrap';
import { localizeText } from '../../i18n';
import { GetDesktopBotCapabilityPage, ImportDesktopBotMCP, UpdateDesktopBotCapabilities } from '../../../wailsjs/go/main/App';

export type DesktopBotMCPRow = {
    id: string;
    name: string;
    transport: string;
    url?: string;
    command?: string;
    disabled?: boolean;
    secret_set?: boolean;
};

export type DesktopBotSkillRow = {
    name: string;
    description?: string;
    enabled?: boolean;
    local?: boolean;
    on_server?: boolean;
};

export type DesktopBotCapabilityPage = {
    mcp?: DesktopBotMCPRow[];
    skills?: DesktopBotSkillRow[];
    warning?: string;
};

function words(lang: string) {
    const L = (en: string, zh: string, hant?: string) => localizeText(lang, en, zh, hant);
    return {
        title: L('MCP and skills', 'MCP 与技能', 'MCP 與技能'),
        hint: L(
            'Saved on the server for every bot of this account. HTTP servers are called from the cloud. A command-based server runs in the account cloud desktop, with its command and env saved there, so this computer can be off. Skill text is uploaded too.',
            '保存在服务器上，这个账号创建的每个 Bot 都用同一份。HTTP 由云端调用。本机命令和它的环境变量会存到服务器，在这个账号的云端桌面里运行，这台电脑关机也不影响。技能正文也会上传。',
            '儲存在伺服器上，這個帳號建立的每個 Bot 都用同一份。HTTP 由雲端呼叫。本機命令和它的環境變數會存到伺服器，在這個帳號的雲端桌面裡執行，這台電腦關機也不影響。技能正文也會上傳。',
        ),
        mcp: L('MCP', 'MCP', 'MCP'),
        copy: L('Copy from this MaClaw', '从本机 MaClaw 复制', '從本機 MaClaw 複製'),
        add: L('Add JSON', '添加 JSON', '新增 JSON'),
        addHint: L('Paste an mcpServers JSON block.', '粘贴 mcpServers JSON。', '貼上 mcpServers JSON。'),
        saveJSON: L('Add', '添加', '新增'),
        cancel: L('Cancel', '取消', '取消'),
        remove: L('Remove', '移除', '移除'),
        off: L('Off', '停用', '停用'),
        on: L('On', '启用', '啟用'),
        secret: L('key stored', '已保存密钥', '已儲存金鑰'),
        stdio: L('cloud desktop', '云端桌面', '雲端桌面'),
        emptyMCP: L('No MCP yet.', '还没有 MCP。', '還沒有 MCP。'),
        skills: L('Skills', '技能', '技能'),
        emptySkills: L('No skills yet.', '还没有技能。', '還沒有技能。'),
        use: L('Bots can use this', 'Bot 可以使用', 'Bot 可以使用'),
        serverOnly: L('On the account, not installed here', '已在账号上，这台电脑没有安装', '已在帳號上，這台電腦沒有安裝'),
        newSkill: L('New skill', '新安装技能', '新安裝技能'),
        skillName: L('Name', '名称', '名稱'),
        skillDesc: L('What it is for', '用途', '用途'),
        skillBody: L('Instructions', '说明', '說明'),
        install: L('Install for bots', '安装并给 Bot 用', '安裝並給 Bot 用'),
        close: L('Close', '关闭', '關閉'),
        loading: L('Loading…', '加载中…', '載入中…'),
        saved: L('Saved. Every bot on this account can use it.', '已保存。这个账号的每个 Bot 都可以用。', '已儲存。這個帳號的每個 Bot 都可以用。'),
    };
}

type Props = {
    heading: string;
    tabs?: ReactNode;
    lang: string;
    onClose: () => void;
};

/** MCP and skills for every desktop bot of this account. */
export function BotCapabilitySettingsDialog({ heading, tabs, lang, onClose }: Props) {
    const w = words(lang);
    const titleId = useId();
    const [page, setPage] = useState<DesktopBotCapabilityPage | null>(null);
    const [busy, setBusy] = useState(false);
    const [notice, setNotice] = useState('');
    const [jsonOpen, setJsonOpen] = useState(false);
    const [jsonText, setJsonText] = useState('');
    const [skillOpen, setSkillOpen] = useState(false);
    const [skillName, setSkillName] = useState('');
    const [skillDesc, setSkillDesc] = useState('');
    const [skillBody, setSkillBody] = useState('');
    const dialogRef = useRef<HTMLDivElement | null>(null);
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(onClose, { enabled: !busy });
    const { onKeyDown } = useModalFocusTrap(dialogRef, { onEscape: () => { if (!busy) onClose(); } });

    useEffect(() => {
        let live = true;
        GetDesktopBotCapabilityPage().then((next: DesktopBotCapabilityPage) => {
            if (!live) return;
            setPage(next || { mcp: [], skills: [] });
            if (next?.warning) setNotice(next.warning);
        }).catch((error: unknown) => {
            if (live) setNotice(error instanceof Error ? error.message : String(error));
        });
        return () => { live = false; };
    }, []);

    const run = async (work: () => Promise<DesktopBotCapabilityPage>) => {
        if (busy) return;
        setBusy(true);
        setNotice('');
        try {
            const next = await work();
            setPage(next || { mcp: [], skills: [] });
            setNotice(next?.warning || w.saved);
            setJsonOpen(false);
            setJsonText('');
            setSkillOpen(false);
            setSkillName('');
            setSkillDesc('');
            setSkillBody('');
        } catch (error) {
            setNotice(error instanceof Error ? error.message : String(error));
        } finally {
            setBusy(false);
        }
    };

    const mcp = page?.mcp || [];
    const skills = page?.skills || [];

    return (
        <div className="modal-backdrop custom-dialog-backdrop bot-llm-dialog-backdrop" {...backdropProps} onKeyDown={onKeyDown}>
            <div className="modal-content custom-dialog bot-llm-dialog" ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby={titleId} data-testid="bot-capability-dialog" tabIndex={-1} {...dialogProps}>
                <div className="bot-llm-dialog__head">
                    <div className="modal-header custom-dialog__header">
                        <div className="custom-dialog__heading">
                            <h3 id={titleId} className="custom-dialog__title">{heading}</h3>
                        </div>
                        <button type="button" className="modal-close" aria-label={w.close} disabled={busy} onClick={onClose}>×</button>
                    </div>
                    {tabs}
                </div>
                <div className="custom-dialog__body bot-llm-dialog__body">
                    <p className="bot-llm-dialog__hint">{w.hint}</p>
                    {notice ? <p className="bot-llm-dialog__hint" data-testid="bot-capability-notice" role="status">{notice}</p> : null}
                    {!page ? <p className="bot-llm-dialog__hint">{w.loading}</p> : null}
                    <section className="bot-llm-dialog__section">
                        <h4 className="bot-cap__title">{w.mcp}</h4>
                        <div className="bot-cap__actions">
                            <button type="button" data-testid="bot-capability-copy" disabled={busy} onClick={() => { void run(() => ImportDesktopBotMCP('gui')); }}>{w.copy}</button>
                            <button type="button" data-testid="bot-capability-add-json" disabled={busy} onClick={() => setJsonOpen(v => !v)}>{w.add}</button>
                        </div>
                        {jsonOpen ? (
                            <div className="bot-cap__form">
                                <p className="bot-llm-dialog__hint">{w.addHint}</p>
                                <textarea data-testid="bot-capability-json" value={jsonText} onChange={e => setJsonText(e.target.value)} rows={6} spellCheck={false} />
                                <button type="button" data-testid="bot-capability-json-save" disabled={busy || !jsonText.trim()} onClick={() => { void run(() => ImportDesktopBotMCP(jsonText)); }}>{w.saveJSON}</button>
                            </div>
                        ) : null}
                        {page && mcp.length === 0 ? <p className="bot-llm-dialog__hint">{w.emptyMCP}</p> : null}
                        <ul className="bot-cap__list">
                            {mcp.map(row => (
                                <li key={row.id} data-testid={`bot-capability-mcp-${row.id}`}>
                                    <div>
                                        <strong>{row.name}</strong>
                                        <span className="bot-cap__meta">{row.transport === 'stdio' ? w.stdio : (row.url || row.transport)}{row.secret_set ? ` · ${w.secret}` : ''}</span>
                                    </div>
                                    <div className="bot-cap__actions">
                                        <button type="button" data-testid={`bot-capability-mcp-toggle-${row.id}`} disabled={busy} onClick={() => { void run(() => UpdateDesktopBotCapabilities({ set_disabled: { [row.id]: !row.disabled } })); }}>{row.disabled ? w.on : w.off}</button>
                                        <button type="button" data-testid={`bot-capability-mcp-remove-${row.id}`} disabled={busy} onClick={() => { void run(() => UpdateDesktopBotCapabilities({ remove_mcp: [row.id] })); }}>{w.remove}</button>
                                    </div>
                                </li>
                            ))}
                        </ul>
                    </section>
                    <section className="bot-llm-dialog__section">
                        <h4 className="bot-cap__title">{w.skills}</h4>
                        <button type="button" data-testid="bot-capability-new-skill" disabled={busy} onClick={() => setSkillOpen(v => !v)}>{w.newSkill}</button>
                        {skillOpen ? (
                            <div className="bot-cap__form">
                                <label>{w.skillName}<input data-testid="bot-capability-skill-name" value={skillName} onChange={e => setSkillName(e.target.value)} /></label>
                                <label>{w.skillDesc}<input data-testid="bot-capability-skill-desc" value={skillDesc} onChange={e => setSkillDesc(e.target.value)} /></label>
                                <label>{w.skillBody}<textarea data-testid="bot-capability-skill-body" value={skillBody} onChange={e => setSkillBody(e.target.value)} rows={6} /></label>
                                <button type="button" data-testid="bot-capability-skill-save" disabled={busy || !skillName.trim() || !skillBody.trim()} onClick={() => { void run(() => UpdateDesktopBotCapabilities({ add_skills: [{ name: skillName.trim(), description: skillDesc.trim(), body: skillBody }] })); }}>{w.install}</button>
                            </div>
                        ) : null}
                        {page && skills.length === 0 ? <p className="bot-llm-dialog__hint">{w.emptySkills}</p> : null}
                        <ul className="bot-cap__list">
                            {skills.map(row => (
                                <li key={row.name} data-testid={`bot-capability-skill-${row.name}`}>
                                    <label>
                                        <input
                                            type="checkbox"
                                            data-testid={`bot-capability-skill-toggle-${row.name}`}
                                            checked={!!row.enabled}
                                            disabled={busy || (!row.local && !row.enabled)}
                                            onChange={() => {
                                                void run(() => UpdateDesktopBotCapabilities(row.enabled
                                                    ? { remove_skills: [row.name] }
                                                    : { enable_skills: [row.name] }));
                                            }}
                                        />
                                        <span>
                                            <strong>{row.name}</strong>
                                            {row.description ? <span className="bot-cap__meta">{row.description}</span> : null}
                                            {!row.local && row.on_server ? <span className="bot-cap__meta">{w.serverOnly}</span> : null}
                                        </span>
                                    </label>
                                    <span className="bot-cap__meta">{row.enabled ? w.use : ''}</span>
                                </li>
                            ))}
                        </ul>
                    </section>
                </div>
            </div>
        </div>
    );
}

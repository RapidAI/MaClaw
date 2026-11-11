import { useEffect, useState } from 'react';
import { EventsOn } from '../../../wailsjs/runtime';
import { AnswerBotLocalAccessPrompt, GetPendingBotLocalAccessPrompts } from '../../../wailsjs/go/main/App';
import { localizeText } from '../../i18n';
import { formatBytes, type BotLocalAccessPrompt as Prompt } from './botLocalAccess';

/**
 * The question a cloud bot raises before it touches something new on this
 * computer. Mounted once at the app root so it shows on any page. No answer
 * before the deadline counts as no.
 */
export function BotLocalAccessPrompt({ lang }: { lang: string }) {
    const [queue, setQueue] = useState<Prompt[]>([]);
    const [now, setNow] = useState(() => Date.now());
    const [seen] = useState(() => new Map<string, number>());

    useEffect(() => {
        const add = (p: Prompt) => {
            if (!p?.id) return;
            if (!seen.has(p.id)) seen.set(p.id, Date.now());
            setQueue(q => (q.some(x => x.id === p.id) ? q : [...q, p]));
        };
        GetPendingBotLocalAccessPrompts().then((list: Prompt[]) => (list || []).forEach(add)).catch(() => {});
        const offAsk = EventsOn('bot-local-access-prompt', add);
        const offDone = EventsOn('bot-local-access-prompt-done', (id: string) => setQueue(q => q.filter(x => x.id !== id)));
        const tick = window.setInterval(() => setNow(Date.now()), 1000);
        return () => { offAsk(); offDone(); window.clearInterval(tick); };
    }, [seen]);

    const p = queue[0];
    if (!p) return null;
    const L = (en: string, zh: string, hant?: string) => localizeText(lang, en, zh, hant);
    const who = p.bot_name || 'Bot';
    const action = p.kind === 'network'
        ? L(`${who} wants to connect to ${p.target} through this computer`, `${who} 想经由这台电脑连接 ${p.target}`, `${who} 想經由這台電腦連線 ${p.target}`)
        : p.kind === 'write'
            ? L(`${who} wants to save a file here: ${p.target}`, `${who} 想把文件保存到：${p.target}`, `${who} 想把檔案儲存到：${p.target}`)
            : p.kind === 'list'
                ? L(`${who} wants to see the folder ${p.target}`, `${who} 想查看文件夹 ${p.target}`, `${who} 想查看資料夾 ${p.target}`)
                : L(`${who} wants to copy ${p.target} to its cloud desktop`, `${who} 想把 ${p.target} 复制到云端电脑`, `${who} 想把 ${p.target} 複製到雲端電腦`);
    const notes: string[] = [];
    if (p.outside) notes.push(L('This is outside the folders you allowed.', '这个位置不在你允许的文件夹里。', '這個位置不在你允許的資料夾裡。'));
    if (p.local) notes.push(L('This is a service on this computer itself.', '这是本机自身的服务。', '這是本機自身的服務。'));
    else if (p.kind === 'network' && !p.private) notes.push(L('This is a public internet address.', '这是公网地址。', '這是公網位址。'));
    if (p.size) notes.push(formatBytes(p.size));
    const left = Math.max(0, Math.ceil(((seen.get(p.id) || now) + (p.seconds || 120) * 1000 - now) / 1000));
    const answer = (a: 'deny' | 'once' | 'always') => {
        setQueue(q => q.filter(x => x.id !== p.id));
        void AnswerBotLocalAccessPrompt(p.id, a).catch(() => {});
    };
    return (
        <div className="modal-backdrop custom-dialog-backdrop bot-local-prompt-backdrop" data-testid="bot-local-prompt">
            <div className="modal-content custom-dialog bot-local-prompt" role="alertdialog" aria-modal="true" aria-label={action}>
                <div className="modal-header custom-dialog__header">
                    <h3 className="custom-dialog__title">{L('Allow access to this computer?', '允许访问这台电脑吗？', '允許存取這台電腦嗎？')}</h3>
                </div>
                <div className="modal-body custom-dialog__body">
                    <p className="bot-local-prompt__action">{action}</p>
                    {p.remote ? <p className="bot-llm-dialog__subhint">{L('Cloud side:', '云端位置：', '雲端位置：')} {p.remote}</p> : null}
                    {notes.length ? <p className="bot-llm-dialog__subhint">{notes.join(' · ')}</p> : null}
                    <p className="bot-llm-dialog__subhint">{L(`No answer in ${left}s counts as no.`, `${left} 秒内不回答视为拒绝。`, `${left} 秒內不回答視為拒絕。`)}{queue.length > 1 ? ` (+${queue.length - 1})` : ''}</p>
                </div>
                <div className="modal-footer custom-dialog__footer">
                    <button type="button" className="btn-secondary" data-testid="bot-local-deny" onClick={() => answer('deny')}>{L('Deny', '拒绝', '拒絕')}</button>
                    <button type="button" className="btn-secondary" data-testid="bot-local-once" onClick={() => answer('once')}>{L('Allow once', '允许本次', '允許本次')}</button>
                    <button type="button" className="btn-primary" data-testid="bot-local-always" onClick={() => answer('always')}>{L('Always allow', '始终允许', '始終允許')}</button>
                </div>
            </div>
        </div>
    );
}

import { BotAvatar } from '../BotAvatar';
import type { BotAppearance } from '../botAppearance';
import { useState } from 'react';
import { BotMessageBody, botMessageIsStructured } from '../botMessageBody';
import { botName, type BotRef, type SaidItem } from './botCollab';

// SaidFileChip opens one file a server-started turn said with its message
// (Hub keeps the bytes on the said note). A failure shows on the chip.
function SaidFileChip({ noteId, name, onOpenFile }: { noteId: string; name: string; onOpenFile?: (noteId: string, name: string) => Promise<void> }) {
    const [error, setError] = useState('');
    return (
        <button
            type="button"
            className="desktop-bot-chat__file-chip bot-said__file"
            data-testid={`bot-said-file-${noteId}-${name}`}
            title={error || name}
            disabled={!onOpenFile}
            onClick={() => { if (onOpenFile) onOpenFile(noteId, name).then(() => setError('')).catch(e => setError(String((e as Error)?.message || e))); }}
        >
            📎 {name}{error ? ` (${error})` : ''}
        </button>
    );
}

// First non-empty line. The rest stays behind the disclosure. This does
// not look for log phrases; the server already marked the message process.
export function processNoteSummary(text: string): string {
    const line = String(text || '').split('\n').map(part => part.trim()).find(Boolean) || '';
    return line.length > 96 ? `${line.slice(0, 96)}…` : line;
}

/** A process note is one collapsed row. It is not a chat bubble. */
export function DesktopBotProcessNote({ id, text, lang = 'zh-Hans' }: { id: string; text: string; lang?: string }) {
    const summary = processNoteSummary(text) || (lang.startsWith('zh') ? '过程' : 'Progress');
    return (
        <details className="desktop-bot-chat__process" data-message-kind="process" data-testid={`desktop-bot-process-${id}`}>
            <summary>{summary}</summary>
            <pre className="desktop-bot-chat__process-body">{text}</pre>
        </details>
    );
}

/** Messages bots said to the person directly, shown in the chat with the speaker's name and avatar. */
export function BotSaidInline({ items, bots, lookOf, lang = 'zh-Hans', onOpenFile }: { items: SaidItem[]; bots: BotRef[]; lookOf: (bot: { id: string; title: string }) => BotAppearance; lang?: string; onOpenFile?: (noteId: string, name: string) => Promise<void> }) {
    if (!items.length) return null;
    return (
        <>
            {items.map(item => {
                const bot = bots.find(b => b.id === item.from) || { id: item.from, title: item.from };
                const process = item.message_kind === 'process';
                return (
                    <div key={item.id} className="desktop-bot-chat__entry desktop-bot-chat__entry--assistant bot-said" data-message-role="said" data-message-kind={item.message_kind || 'outcome'} data-testid={`bot-said-${item.id}`}>
                        <span className="bot-said__who">
                            <BotAvatar className="desktop-bot-chat__notice-avatar" title={bot.title} {...lookOf(bot)} />
                            {botName(bots, item.from, lang)}
                            {item.created_at ? <time dateTime={item.created_at}>{new Date(item.created_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</time> : null}
                        </span>
                        {process ? <DesktopBotProcessNote id={item.id} text={item.text} lang={lang} /> : (
                            botMessageIsStructured(item.text)
                                ? <div className="desktop-bot-chat__bubble desktop-bot-chat__bubble--assistant"><BotMessageBody text={item.text} /></div>
                                : <div className="desktop-bot-chat__bubble desktop-bot-chat__bubble--assistant is-plain">{item.text}</div>
                        )}
                        {item.files && item.files.length > 0 ? (
                            <div className="bot-said__files">
                                {item.files.map(f => <SaidFileChip key={f.sha256 || f.name} noteId={item.id} name={f.name} onOpenFile={onOpenFile} />)}
                            </div>
                        ) : null}
                    </div>
                );
            })}
        </>
    );
}

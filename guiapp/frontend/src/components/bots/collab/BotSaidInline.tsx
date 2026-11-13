import { BotAvatar } from '../BotAvatar';
import type { BotAppearance } from '../botAppearance';
import { useEffect, useState } from 'react';
import { BotMessageBody, botMessageIsStructured } from '../botMessageBody';
import { botName, type BotRef, type SaidItem } from './botCollab';

export type SaidImage = { mime: string; data: string };

function saidFileIsImage(file: { name?: string; mime?: string }): boolean {
    const mime = String(file.mime || '').toLowerCase();
    if (mime === 'image/png' || mime === 'image/jpeg' || mime === 'image/jpg') return true;
    return /\.(png|jpe?g)$/i.test(file.name || '');
}

// SaidImageInline paints a png or jpeg from a said note. The bytes are
// fetched the same way a staged screenshot is: not left as a file chip.
function SaidImageInline({ noteId, name, mime, loadImage }: { noteId: string; name: string; mime?: string; loadImage?: (noteId: string, name: string) => Promise<SaidImage | null> }) {
    const [src, setSrc] = useState('');
    useEffect(() => {
        if (!loadImage) return;
        let live = true;
        void loadImage(noteId, name).then(image => {
            const kind = String(image?.mime || mime || '').toLowerCase();
            const data = String(image?.data || '').replace(/\s+/g, '');
            if (!live || (kind !== 'image/png' && kind !== 'image/jpeg') || !data) return;
            setSrc(`data:${kind};base64,${data}`);
        }).catch(() => undefined);
        return () => { live = false; };
    }, [noteId, name, mime, loadImage]);
    if (!src) return <span className="bot-said__image-pending">{name}</span>;
    return <img className="bot-said__image" data-testid={`bot-said-image-${noteId}-${name}`} alt={name} src={src} />;
}

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

/** Messages bots said to the person directly, shown in the chat with the speaker's name and avatar. */
export function BotSaidInline({ items, bots, lookOf, lang = 'zh-Hans', onOpenFile, loadImage }: { items: SaidItem[]; bots: BotRef[]; lookOf: (bot: { id: string; title: string }) => BotAppearance; lang?: string; onOpenFile?: (noteId: string, name: string) => Promise<void>; loadImage?: (noteId: string, name: string) => Promise<SaidImage | null> }) {
    if (!items.length) return null;
    return (
        <>
            {items.map(item => {
                const bot = bots.find(b => b.id === item.from) || { id: item.from, title: item.from };
                return (
                    <div key={item.id} className="desktop-bot-chat__entry desktop-bot-chat__entry--assistant bot-said" data-message-role="said" data-testid={`bot-said-${item.id}`}>
                        <span className="bot-said__who">
                            <BotAvatar className="desktop-bot-chat__notice-avatar" title={bot.title} {...lookOf(bot)} />
                            {botName(bots, item.from, lang)}
                            {item.created_at ? <time dateTime={item.created_at}>{new Date(item.created_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</time> : null}
                        </span>
                        {botMessageIsStructured(item.text)
                            ? <div className="desktop-bot-chat__bubble desktop-bot-chat__bubble--assistant"><BotMessageBody text={item.text} /></div>
                            : <div className="desktop-bot-chat__bubble desktop-bot-chat__bubble--assistant is-plain">{item.text}</div>}
                        {item.files && item.files.length > 0 ? (
                            <div className="bot-said__files">
                                {item.files.map(f => saidFileIsImage(f)
                                    ? <SaidImageInline key={f.sha256 || f.name} noteId={item.id} name={f.name} mime={f.mime} loadImage={loadImage} />
                                    : <SaidFileChip key={f.sha256 || f.name} noteId={item.id} name={f.name} onOpenFile={onOpenFile} />)}
                            </div>
                        ) : null}
                    </div>
                );
            })}
        </>
    );
}

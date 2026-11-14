import { BotAvatar } from '../BotAvatar';
import type { BotAppearance } from '../botAppearance';
import { useCallback, useEffect, useState } from 'react';
import { BotMessageBody, botMessageIsStructured } from '../botMessageBody';
import { botName, type BotRef, type SaidItem } from './botCollab';

// Bytes of one file on a said note. The chat fetches them to preview a picture.
export type SaidFileBytes = { mime?: string; MIME?: string; data?: string };

const IMAGE_MIME = new Set(['image/png', 'image/jpeg', 'image/gif', 'image/webp']);
// A said file is at most 8 MB. Anything larger stays a chip instead of a data URL.
const SAID_PICTURE_DATA_MAX = 12_000_000;

function saidMime(raw: string): string {
    const mime = raw.toLowerCase().split(';')[0].trim();
    return mime === 'image/jpg' ? 'image/jpeg' : mime;
}

// A screenshot said as its own message arrives as a file (png/jpeg). The
// chat shows that picture, not a download chip.
export function saidFileIsImage(file: { name?: string; mime?: string; MIME?: string }): boolean {
    const mime = saidMime(String(file.mime || file.MIME || ''));
    // A stated type wins. A report named report.png with a pdf type stays a file.
    if (mime) return IMAGE_MIME.has(mime);
    return /\.(png|jpe?g|gif|webp)$/i.test(String(file.name || ''));
}

// saidPictureMime is the type to put on the preview. An empty fetch type
// falls back to the file name; a stated non-image type does not.
export function saidPictureMime(name: string, raw: string): string {
    const mime = saidMime(raw);
    if (IMAGE_MIME.has(mime)) return mime;
    if (mime) return '';
    if (/\.jpe?g$/i.test(name)) return 'image/jpeg';
    if (/\.gif$/i.test(name)) return 'image/gif';
    if (/\.webp$/i.test(name)) return 'image/webp';
    if (/\.png$/i.test(name)) return 'image/png';
    return '';
}

function saidPictureData(data: string): boolean {
    if (!data || data.length > SAID_PICTURE_DATA_MAX || data.length % 4 !== 0) return false;
    return /^[A-Za-z0-9+/]+={0,2}$/.test(data);
}

// saidPictureSource is the img src for one fetched file, or '' when the
// bytes are not a picture the chat can show.
export function saidPictureSource(name: string, file: SaidFileBytes | null | undefined): string {
    const mime = saidPictureMime(name, String(file?.mime || file?.MIME || ''));
    const data = String(file?.data || '').replace(/\s+/g, '');
    if (!mime || !saidPictureData(data)) return '';
    return `data:${mime};base64,${data}`;
}

// "文件：snake-desktop.png（181 KB）" is the label the sender puts on a
// file-only message. Once the picture is on screen that line is noise.
export function saidTextIsOnlyFileLabel(text: string, files: { name: string }[]): boolean {
    const t = text.trim();
    if (!t) return true;
    return files.some(file => {
        const name = file.name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
        return new RegExp(`^(?:文件|截图)：${name}(?:（[^）]*）|\\([^)]*\\))?$`).test(t);
    });
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

function SaidPicture({ noteId, name, loadFile, onOpenShot, openLabel, onReady, onFail }: {
    noteId: string;
    name: string;
    loadFile: (noteId: string, name: string) => Promise<SaidFileBytes | null | undefined>;
    onOpenShot?: (src: string, trigger: HTMLButtonElement) => void;
    openLabel: string;
    onReady: (name: string) => void;
    onFail: (name: string) => void;
}) {
    const [src, setSrc] = useState('');
    useEffect(() => {
        let live = true;
        loadFile(noteId, name).then(file => {
            if (!live) return;
            const next = saidPictureSource(name, file);
            if (!next) {
                onFail(name);
                return;
            }
            setSrc(next);
            onReady(name);
        }).catch(() => { if (live) onFail(name); });
        return () => { live = false; };
    }, [noteId, name, loadFile, onReady, onFail]);
    if (!src) return null;
    return (
        <button
            type="button"
            className="desktop-bot-chat__shot-open"
            data-testid={`bot-said-image-${noteId}-${name}`}
            title={openLabel}
            aria-label={openLabel}
            onClick={event => onOpenShot?.(src, event.currentTarget)}
        >
            <img
                className="desktop-bot-chat__shot"
                data-testid="desktop-bot-screenshot"
                alt={name}
                decoding="async"
                draggable={false}
                src={src}
                onError={() => onFail(name)}
            />
        </button>
    );
}

function SaidEntry({ item, bots, lookOf, lang, onOpenFile, loadFile, onOpenShot, openLabel }: {
    item: SaidItem;
    bots: BotRef[];
    lookOf: (bot: { id: string; title: string }) => BotAppearance;
    lang: string;
    onOpenFile?: (noteId: string, name: string) => Promise<void>;
    loadFile?: (noteId: string, name: string) => Promise<SaidFileBytes | null | undefined>;
    onOpenShot?: (src: string, trigger: HTMLButtonElement) => void;
    openLabel: string;
}) {
    const files = item.files || [];
    const pictures = loadFile ? files.filter(saidFileIsImage) : [];
    const [ready, setReady] = useState<Record<string, boolean>>({});
    const [failed, setFailed] = useState<Record<string, boolean>>({});
    const onReady = useCallback((name: string) => {
        setReady(prev => (prev[name] ? prev : { ...prev, [name]: true }));
    }, []);
    const onFail = useCallback((name: string) => {
        setFailed(prev => (prev[name] ? prev : { ...prev, [name]: true }));
    }, []);
    // The "文件：" line stays until the picture is actually on screen, so a
    // slow fetch does not leave an empty gap under the name.
    const pictureShown = pictures.length > 0 && pictures.every(file => ready[file.name] && !failed[file.name]);
    const hideLabel = pictureShown && files.every(saidFileIsImage) && saidTextIsOnlyFileLabel(item.text || '', pictures);
    const bot = bots.find(b => b.id === item.from) || { id: item.from, title: item.from };
    const chips = files.filter(file => !pictures.some(picture => picture.name === file.name) || failed[file.name]);
    return (
        <div className="desktop-bot-chat__entry desktop-bot-chat__entry--assistant bot-said" data-message-role="said" data-testid={`bot-said-${item.id}`}>
            <span className="bot-said__who">
                <BotAvatar className="desktop-bot-chat__notice-avatar" title={bot.title} {...lookOf(bot)} />
                {botName(bots, item.from, lang)}
                {item.created_at ? <time dateTime={item.created_at}>{new Date(item.created_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</time> : null}
            </span>
            {!hideLabel && item.text ? (
                botMessageIsStructured(item.text)
                    ? <div className="desktop-bot-chat__bubble desktop-bot-chat__bubble--assistant"><BotMessageBody text={item.text} /></div>
                    : <div className="desktop-bot-chat__bubble desktop-bot-chat__bubble--assistant is-plain">{item.text}</div>
            ) : null}
            {pictures.filter(file => !failed[file.name]).map(file => (
                <SaidPicture key={file.sha256 || file.name} noteId={item.id} name={file.name} loadFile={loadFile as NonNullable<typeof loadFile>} onOpenShot={onOpenShot} openLabel={openLabel} onReady={onReady} onFail={onFail} />
            ))}
            {chips.length > 0 ? (
                <div className="bot-said__files">
                    {chips.map(file => <SaidFileChip key={file.sha256 || file.name} noteId={item.id} name={file.name} onOpenFile={onOpenFile} />)}
                </div>
            ) : null}
        </div>
    );
}

/** Messages bots said to the person directly, shown in the chat with the speaker's name and avatar. */
export function BotSaidInline({ items, bots, lookOf, lang = 'zh-Hans', onOpenFile, loadFile, onOpenShot, openLabel = '查看图片' }: {
    items: SaidItem[];
    bots: BotRef[];
    lookOf: (bot: { id: string; title: string }) => BotAppearance;
    lang?: string;
    onOpenFile?: (noteId: string, name: string) => Promise<void>;
    loadFile?: (noteId: string, name: string) => Promise<SaidFileBytes | null | undefined>;
    onOpenShot?: (src: string, trigger: HTMLButtonElement) => void;
    openLabel?: string;
}) {
    if (!items.length) return null;
    return (
        <>
            {items.map(item => (
                <SaidEntry key={item.id} item={item} bots={bots} lookOf={lookOf} lang={lang} onOpenFile={onOpenFile} loadFile={loadFile} onOpenShot={onOpenShot} openLabel={openLabel} />
            ))}
        </>
    );
}

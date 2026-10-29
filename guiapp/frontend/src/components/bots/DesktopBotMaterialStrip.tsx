import { useRef } from 'react';
import { localizeText } from '../../i18n';
import { formatMaterialSize, materialFileName, type MaterialItem } from './desktopBotMaterials';

function words(lang: string) {
    return {
        attach: localizeText(lang, 'Attach files', '添加素材'),
        attached: localizeText(lang, 'Attached files', '已附加文件'),
        remove: localizeText(lang, 'Remove', '移除'),
        tooLarge: localizeText(lang, 'Over 200 MB', '超过 200 MB'),
        failed: localizeText(lang, 'Upload failed', '上传失败'),
    };
}

function materialKindLabel(name: string): string {
    const raw = (name.match(/\.([^./\\]+)$/)?.[1] || '').trim();
    if (!raw) return 'FILE';
    return raw.slice(0, 4).toUpperCase();
}

export function materialErrorText(code: string | undefined, lang: string): string {
    const w = words(lang);
    return code === 'too_large' ? w.tooLarge : w.failed;
}

// The attach button and the files waiting to go with the next message.
export function DesktopBotMaterialStrip({ items, lang, disabled, onAttach, onRemove }: {
    items: MaterialItem[];
    lang: string;
    disabled?: boolean;
    onAttach: (files: File[]) => void;
    onRemove: (id: string) => void;
}) {
    const input = useRef<HTMLInputElement>(null);
    const w = words(lang);
    return (
        <div className="desktop-bot-materials" data-testid="desktop-bot-materials">
            {items.length > 0 && (
                <div className="desktop-bot-materials__strip" role="list" aria-label={w.attached}>
                    {items.map(item => {
                        const kind = materialKindLabel(item.name);
                        const sizeLabel = formatMaterialSize(item.size);
                        const meta = item.status === 'uploading'
                            ? `${item.size > 0 ? Math.floor((item.sent / item.size) * 100) : 0}%`
                            : item.status === 'error'
                                ? materialErrorText(item.error, lang)
                                : sizeLabel;
                        const titled = item.path || item.name;
                        return (
                            <span key={item.id} className={`desktop-bot-materials__item is-${item.status}`} role="listitem" data-testid="desktop-bot-material" title={item.status === 'done' ? `${titled} · ${sizeLabel}` : titled}>
                                <span className={`desktop-bot-materials__badge${kind.length > 3 ? ' is-long' : ''}`} aria-hidden="true">{kind}</span>
                                <span className="desktop-bot-materials__name">{item.name}</span>
                                <span className={`desktop-bot-materials__meta${item.status === 'done' ? ' is-quiet' : ''}`}>{meta}</span>
                                <button type="button" className="desktop-bot-materials__remove" data-testid="desktop-bot-material-remove" aria-label={`${w.remove} ${item.name}`} title={`${w.remove} ${item.name}`} onClick={() => onRemove(item.id)}>×</button>
                            </span>
                        );
                    })}
                </div>
            )}
            <button
                type="button"
                className="desktop-bot-materials__attach"
                data-testid="desktop-bot-attach"
                disabled={disabled}
                aria-label={w.attach}
                title={w.attach}
                onClick={() => input.current?.click()}
            >
                <svg width="13" height="13" viewBox="0 0 24 24" aria-hidden="true" focusable="false">
                    <path fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" d="m21.44 11.05-9.19 9.19a6 6 0 0 1-8.49-8.49l9.19-9.19a4 4 0 0 1 5.66 5.66l-9.2 9.19a2 2 0 0 1-2.83-2.83l8.49-8.48" />
                </svg>
            </button>
            <input
                ref={input}
                type="file"
                multiple
                hidden
                data-testid="desktop-bot-attach-input"
                onChange={event => {
                    const files = Array.from(event.target.files || []);
                    event.target.value = '';
                    if (files.length) onAttach(files);
                }}
            />
        </div>
    );
}

// Read-only chips for the files a sent message carried.
export function DesktopBotMaterialChips({ paths }: { paths: string[] }) {
    if (paths.length === 0) return null;
    return (
        <span className="desktop-bot-materials__sent" data-testid="desktop-bot-message-materials">
            {paths.map(path => (
                <span key={path} className="desktop-bot-materials__item is-done" title={path}>📄 {materialFileName(path)}</span>
            ))}
        </span>
    );
}

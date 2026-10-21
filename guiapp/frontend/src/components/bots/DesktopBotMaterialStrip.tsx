import { useRef } from 'react';
import { formatMaterialSize, materialFileName, type MaterialItem } from './desktopBotMaterials';

function words(lang: string) {
    const en = String(lang).toLowerCase().startsWith('en');
    return {
        attach: en ? 'Attach files' : '添加素材',
        remove: en ? 'Remove' : '移除',
        tooLarge: en ? 'Over 200 MB' : '超过 200 MB',
        failed: en ? 'Upload failed' : '上传失败',
    };
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
            <button
                type="button"
                className="desktop-bot-chat__action desktop-bot-materials__attach"
                data-testid="desktop-bot-attach"
                disabled={disabled}
                aria-label={w.attach}
                title={w.attach}
                onClick={() => input.current?.click()}
            >
                📎
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
            {items.map(item => (
                <span key={item.id} className={`desktop-bot-materials__item is-${item.status}`} data-testid="desktop-bot-material" title={item.path || item.name}>
                    <span className="desktop-bot-materials__name">{item.name}</span>
                    <span className="desktop-bot-materials__meta">
                        {item.status === 'uploading'
                            ? `${item.size > 0 ? Math.floor((item.sent / item.size) * 100) : 0}%`
                            : item.status === 'error'
                                ? materialErrorText(item.error, lang)
                                : formatMaterialSize(item.size)}
                    </span>
                    <button type="button" className="desktop-bot-materials__remove" data-testid="desktop-bot-material-remove" aria-label={w.remove} onClick={() => onRemove(item.id)}>×</button>
                </span>
            ))}
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

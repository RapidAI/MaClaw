import { useEffect, useState } from 'react';
import { EventsOn } from '../../../wailsjs/runtime';
import { localizeText } from '../../i18n';
import { formatBytes, mergeTransfer, type BotLocalTransfer } from './botLocalAccess';

/** File chips for transfers between this computer and the selected bot's cloud desktop. */
export function DesktopBotLocalTransfers({ botId, lang }: { botId: string; lang: string }) {
    const [items, setItems] = useState<BotLocalTransfer[]>([]);
    useEffect(() => {
        const off = EventsOn('desktop-bot-local-transfer', (ev: BotLocalTransfer) => {
            if (ev?.id) setItems(list => mergeTransfer(list, ev));
        });
        return () => off();
    }, []);
    const mine = items.filter(item => item.bot_id === botId && item.direction !== 'tunnel');
    if (mine.length === 0) return null;
    const L = (en: string, zh: string, hant?: string) => localizeText(lang, en, zh, hant);
    return (
        <div className="desktop-bot-local-transfers" role="list" data-testid="desktop-bot-local-transfers">
            {mine.map(item => {
                const arrow = item.direction === 'to_cloud' ? L('to cloud', '传到云端', '傳到雲端') : L('to this computer', '存到本机', '存到本機');
                const pct = item.size ? Math.min(100, Math.floor((item.done / item.size) * 100)) : 0;
                const meta = item.state === 'running' ? `${pct}%` : item.state === 'failed' ? (item.error || L('failed', '失败', '失敗')) : formatBytes(item.size || item.done);
                return (
                    <span key={item.id} role="listitem" className={`desktop-bot-materials__item is-${item.state === 'running' ? 'uploading' : item.state === 'failed' ? 'error' : 'done'}`} title={item.local_path || item.name} data-testid="desktop-bot-local-transfer">
                        <span className="desktop-bot-materials__badge" aria-hidden="true">{item.direction === 'to_cloud' ? '↑' : '↓'}</span>
                        <span className="desktop-bot-materials__name">{item.name}</span>
                        <span className="desktop-bot-materials__meta">{arrow} · {meta}</span>
                    </span>
                );
            })}
        </div>
    );
}

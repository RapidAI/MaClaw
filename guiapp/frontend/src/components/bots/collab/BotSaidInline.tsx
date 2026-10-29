import { BotAvatar } from '../BotAvatar';
import type { BotAppearance } from '../botAppearance';
import { botName, type BotRef, type SaidItem } from './botCollab';

/** Messages bots said to the person directly, shown in the chat with the speaker's name and avatar. */
export function BotSaidInline({ items, bots, lookOf, lang = 'zh-Hans' }: { items: SaidItem[]; bots: BotRef[]; lookOf: (bot: { id: string; title: string }) => BotAppearance; lang?: string }) {
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
                        <div className="desktop-bot-chat__bubble desktop-bot-chat__bubble--assistant is-plain">{item.text}</div>
                    </div>
                );
            })}
        </>
    );
}

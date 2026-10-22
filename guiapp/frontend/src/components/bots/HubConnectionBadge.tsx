import { useEffect, useRef, useState } from 'react';
import { GetHubConnectionState, ReconnectHub } from '../../../wailsjs/go/main/App';
import { EventsOn } from '../../../wailsjs/runtime';

export interface HubLinkState {
    state: string;
    attempts?: number;
    next_retry_ms?: number;
    last_error?: string;
}

export function hubBadgeLabel(st: HubLinkState | null, lang: string): string {
    if (!st || st.state === 'connected') return '';
    const en = String(lang || '').toLowerCase().startsWith('en');
    const hant = String(lang || '').includes('Hant');
    if (st.state === 'connecting') {
        const secs = Math.ceil((st.next_retry_ms || 0) / 1000);
        if (en) return secs > 0 ? `Reconnecting to Hub (retry in ${secs}s)` : 'Reconnecting to Hub…';
        if (hant) return secs > 0 ? `正在重連 Hub（${secs} 秒後重試）` : '正在重連 Hub…';
        return secs > 0 ? `正在重连 Hub（${secs} 秒后重试）` : '正在重连 Hub…';
    }
    if (en) return 'Hub not connected';
    return hant ? 'Hub 未連接' : 'Hub 未连接';
}

// Shows the Hub link only when it is not up, and asks for a reconnect when
// the network returns, the window comes back, or the badge is tapped.
export function HubConnectionBadge({ lang }: { lang: string }) {
    const [st, setSt] = useState<HubLinkState | null>(null);
    const [, tick] = useState(0);
    const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
    useEffect(() => {
        let alive = true;
        const refresh = () => {
            if (timer.current) clearTimeout(timer.current);
            timer.current = setTimeout(() => {
                Promise.resolve().then(() => GetHubConnectionState()).then(next => { if (alive && next) setSt(next); }).catch(() => undefined);
            }, 150);
        };
        const kick = (why: string) => {
            Promise.resolve().then(() => ReconnectHub(why)).then(next => { if (alive && next) setSt(next); }).catch(() => undefined);
        };
        refresh();
        const off = EventsOn('hub-connection-state', refresh);
        const online = () => kick('online');
        const visible = () => { if (document.visibilityState === 'visible') kick('visible'); };
        window.addEventListener('online', online);
        document.addEventListener('visibilitychange', visible);
        const clock = window.setInterval(() => tick(n => n + 1), 1000);
        return () => {
            alive = false;
            if (timer.current) clearTimeout(timer.current);
            if (typeof off === 'function') off();
            window.removeEventListener('online', online);
            document.removeEventListener('visibilitychange', visible);
            window.clearInterval(clock);
        };
    }, []);
    const label = hubBadgeLabel(st, lang);
    if (!label) return null;
    return (
        <button
            type="button"
            className={'desktop-bot-workspace__hub-link is-' + (st?.state || 'offline')}
            data-testid="desktop-bot-hub-link"
            title={st?.last_error || label}
            onClick={() => { void Promise.resolve().then(() => ReconnectHub('tap')).then(next => next && setSt(next)).catch(() => undefined); }}
        >
            {label}
        </button>
    );
}

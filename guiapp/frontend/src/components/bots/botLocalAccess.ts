// Types and helpers for the local relay (bot access to this computer).

export type BotLocalAccessRoot = { path: string; write: boolean };

export type BotLocalAccessSettings = {
    files_enabled: boolean;
    roots: BotLocalAccessRoot[] | null;
    max_transfer_mb: number;
    network_enabled: boolean;
    net_allow: string[] | null;
    tunnel_idle_minutes: number;
    confirm_seconds: number;
    approved: string[] | null;
};

export type BotLocalAccessState = {
    settings: BotLocalAccessSettings;
    connected: boolean;
    home: string;
    link_error?: string;
};

export type BotLocalAccessPrompt = {
    id: string;
    bot_id: string;
    bot_name: string;
    kind: 'read' | 'write' | 'list' | 'network' | string;
    target: string;
    remote?: string;
    outside?: boolean;
    private?: boolean;
    local?: boolean;
    size?: number;
    seconds: number;
};

export type BotLocalTransfer = {
    id: string;
    bot_id: string;
    bot_name?: string;
    run_id?: string;
    direction: 'to_cloud' | 'to_local' | 'tunnel' | 'download' | string;
    name?: string;
    local_path?: string;
    remote?: string;
    target?: string;
    size?: number;
    done: number;
    state: 'running' | 'done' | 'failed' | 'closed' | string;
    error?: string;
};

export function formatBytes(n: number | undefined): string {
    const v = Number(n || 0);
    if (v >= 1 << 30) return `${(v / (1 << 30)).toFixed(2)} GB`;
    if (v >= 1 << 20) return `${(v / (1 << 20)).toFixed(1)} MB`;
    if (v >= 1 << 10) return `${(v / (1 << 10)).toFixed(1)} KB`;
    return `${v} B`;
}

/** Keeps the newest event per transfer id and at most `limit` entries per bot. */
export function mergeTransfer(list: BotLocalTransfer[], ev: BotLocalTransfer, limit = 6): BotLocalTransfer[] {
    const rest = list.filter(item => item.id !== ev.id);
    const next = [...rest, ev];
    const mine = next.filter(item => item.bot_id === ev.bot_id);
    if (mine.length <= limit) return next;
    const drop = new Set(mine.slice(0, mine.length - limit).map(item => item.id));
    return next.filter(item => !drop.has(item.id));
}

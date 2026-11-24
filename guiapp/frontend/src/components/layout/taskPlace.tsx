import { useSyncExternalStore } from 'react';
import { localizeText } from '../../i18n';

/** Where a task lives. Cloud workspace rows stay local; only cloud agents are cloud. */
export type TaskPlace = 'local' | 'cloud';

export type CloudAgentListItem = {
    id: string;
    name: string;
    kind: string;
    status: string;
    updatedAt: string;
};

const OPEN_STAMP_KEY = 'maclaw.cloud-agent-opened.v1';
const ANCIENT = Date.parse('2000-01-01T00:00:00Z');

const stampListeners = new Set<() => void>();
let stampRevision = 0;

function notifyStamps() {
    stampRevision += 1;
    stampListeners.forEach(listener => listener());
}

/** Epoch milliseconds, or 0 when the value is missing or a zero time. */
export function taskPlaceStamp(value: unknown): number {
    const parsed = typeof value === 'number' ? value : Date.parse(String(value || ''));
    if (!Number.isFinite(parsed) || parsed < ANCIENT) return 0;
    return parsed;
}

export function readCloudOpenStamps(): Record<string, number> {
    if (typeof localStorage === 'undefined') return {};
    try {
        const parsed = JSON.parse(localStorage.getItem(OPEN_STAMP_KEY) || '{}') as unknown;
        if (!parsed || typeof parsed !== 'object') return {};
        const stamps: Record<string, number> = {};
        for (const [id, value] of Object.entries(parsed)) {
            const stamp = taskPlaceStamp(value);
            if (id && stamp > 0) stamps[id] = stamp;
        }
        return stamps;
    } catch {
        return {};
    }
}

/** Opening a cloud task lifts it in 最近任务 even when the server stamp is older. */
export function rememberCloudOpen(id: string, at = Date.now()): number {
    const key = id.trim();
    if (!key) return 0;
    const stamps = readCloudOpenStamps();
    stamps[key] = at;
    try {
        localStorage.setItem(OPEN_STAMP_KEY, JSON.stringify(stamps));
    } catch {
        // Private mode still keeps the in-memory revision for this view.
    }
    notifyStamps();
    return at;
}

export type CloudTaskPlaceSnapshot = {
    /** Hub 已为这个账号开通云端任务。没开通时新建任务的位置里不出现云端。 */
    enabled: boolean;
    /** 任务栏当前停在云端页签。新建任务的位置默认跟着它。 */
    selected: boolean;
};

let cloudTaskPlace: CloudTaskPlaceSnapshot = { enabled: false, selected: false };
const cloudTaskPlaceListeners = new Set<() => void>();

function sameCloudTaskPlace(a: CloudTaskPlaceSnapshot, b: CloudTaskPlaceSnapshot): boolean {
    return a.enabled === b.enabled && a.selected === b.selected;
}

function notifyCloudTaskPlace() {
    cloudTaskPlaceListeners.forEach(listener => listener());
}

/** Sidebar publishes this after the Hub list and when the 云端 tab changes. */
export function publishCloudTaskPlace(next: CloudTaskPlaceSnapshot) {
    if (sameCloudTaskPlace(cloudTaskPlace, next)) return;
    cloudTaskPlace = { enabled: next.enabled === true, selected: next.selected === true };
    notifyCloudTaskPlace();
}

export function cloudTaskPlaceSnapshot(): CloudTaskPlaceSnapshot {
    return cloudTaskPlace;
}

export function useCloudTaskPlace(): CloudTaskPlaceSnapshot {
    return useSyncExternalStore(
        (listener: () => void) => {
            cloudTaskPlaceListeners.add(listener);
            return () => cloudTaskPlaceListeners.delete(listener);
        },
        () => cloudTaskPlace,
        () => cloudTaskPlace,
    );
}

/** Test isolation. Production starts disabled until the sidebar publishes the Hub list. */
export function resetCloudTaskPlaceForTests() {
    if (!cloudTaskPlace.enabled && !cloudTaskPlace.selected) return;
    cloudTaskPlace = { enabled: false, selected: false };
    notifyCloudTaskPlace();
}

export function useCloudOpenRevision(): number {
    return useSyncExternalStore(
        (listener: () => void) => {
            stampListeners.add(listener);
            return () => stampListeners.delete(listener);
        },
        () => stampRevision,
        () => 0,
    );
}

/** Last observed turn. Absent means the list status is still the authority. */
const turnListeners = new Set<() => void>();
let turnNotes = new Map<string, boolean>();
let turnVersion = 0;

function notifyTurns() {
    turnVersion += 1;
    turnListeners.forEach(listener => listener());
}

/** Record whether this instance's turn is in flight. The same value does not notify. */
export function noteCloudTurn(id: string, running: boolean): void {
    const key = id.trim();
    if (!key || turnNotes.get(key) === running) return;
    const next = new Map(turnNotes);
    next.set(key, running);
    turnNotes = next;
    notifyTurns();
}

/** In-flight when we have seen it, otherwise the list row's own status. */
export function cloudTurnRunning(id: string, listStatus: string): boolean {
    const noted = turnNotes.get(id);
    if (noted !== undefined) return noted;
    return listStatus === 'running';
}

export function useCloudTurnRunning(id: string, listStatus: string): boolean {
    useSyncExternalStore(
        (listener: () => void) => {
            turnListeners.add(listener);
            return () => turnListeners.delete(listener);
        },
        () => turnVersion,
        () => 0,
    );
    return cloudTurnRunning(id, listStatus);
}

export function resetCloudTurnNotes(): void {
    if (turnNotes.size === 0) return;
    turnNotes = new Map();
    notifyTurns();
}

function textOf(row: Record<string, unknown>, ...keys: string[]): string {
    for (const key of keys) {
        const value = row[key];
        if (typeof value === 'string' && value.trim()) return value.trim();
    }
    return '';
}

/** One cloud instance from ListCloudAgents. A deleted row is not a task. */
export function asCloudAgentListItem(value: unknown): CloudAgentListItem | null {
    if (!value || typeof value !== 'object') return null;
    const row = value as Record<string, unknown>;
    const id = textOf(row, 'id', 'ID');
    if (!id) return null;
    const status = textOf(row, 'status', 'Status');
    if (status === 'deleted') return null;
    return {
        id,
        name: textOf(row, 'name', 'Name'),
        kind: textOf(row, 'kind', 'Kind'),
        status,
        updatedAt: textOf(row, 'updated_at', 'UpdatedAt', 'created_at', 'CreatedAt'),
    };
}

/** Recency for the mixed band. A zero server stamp still counts after this desktop opens it. */
export function cloudAgentRecency(item: CloudAgentListItem, stamps: Record<string, number>): number {
    return Math.max(taskPlaceStamp(item.updatedAt), taskPlaceStamp(stamps[item.id]));
}

export type RecentMix = {
    /** Local entry keys and cloud ids that fit in the band, newest first. */
    order: Record<string, number>;
};

/** Interleave local entries and cloud instances by time. The cap counts both. */
export function mixRecentEntries(
    local: ReadonlyArray<{ key: string; at: number }>,
    cloud: ReadonlyArray<{ id: string; at: number }>,
    cap: number,
): RecentMix {
    if (cap <= 0) return { order: {} };
    const ranked: Array<{ key: string; at: number }> = [];
    for (const item of local) {
        if (item.at > 0 && item.key) ranked.push({ key: item.key, at: item.at });
    }
    for (const item of cloud) {
        if (item.at > 0 && item.id) ranked.push({ key: `cloud:${item.id}`, at: item.at });
    }
    ranked.sort((a, b) => b.at - a.at);
    const order: Record<string, number> = {};
    ranked.slice(0, cap).forEach((item, index) => {
        order[item.key] = index;
    });
    return { order };
}

const ICON_PROPS = {
    fill: 'none',
    stroke: 'currentColor',
    strokeWidth: 1.7,
    strokeLinecap: 'round' as const,
    strokeLinejoin: 'round' as const,
};

/**
 * Place mark for 最近任务.
 * Local is a laptop. Cloud is a cloud outline with no download arrow —
 * the arrow belongs to 云端工作区.
 */
export function TaskPlaceIcon({ place, lang }: { place: TaskPlace; lang: string }) {
    const label = place === 'cloud'
        ? localizeText(lang, 'Cloud task', '云端任务', '雲端任務')
        : localizeText(lang, 'Local task', '本地任务', '本機任務');
    const color = place === 'cloud'
        ? 'color-mix(in srgb, var(--theme-primary) 72%, var(--theme-text-primary))'
        : 'var(--theme-text-muted)';
    return (
        <span
            role="img"
            aria-label={label}
            title={label}
            data-testid={`task-place-icon-${place}`}
            data-place={place}
            style={{ flexShrink: 0, width: 16, height: 16, display: 'inline-flex', alignItems: 'center', justifyContent: 'center', color, alignSelf: 'center' }}
        >
            <svg width="16" height="16" viewBox="0 0 24 24" aria-hidden="true" focusable="false">
                {place === 'local' ? (
                    <>
                        <rect {...ICON_PROPS} x="5" y="3.5" width="14" height="10" rx="1.4" />
                        <path {...ICON_PROPS} d="M3 18.5h18l-2-4H5l-2 4z" />
                    </>
                ) : (
                    <path {...ICON_PROPS} d="M7.2 17.5h9.4a3.5 3.5 0 0 0 .35-7 5.2 5.2 0 0 0-10-.35 3.1 3.1 0 0 0 .25 7.35z" />
                )}
            </svg>
        </span>
    );
}

export function cloudAgentKindLabel(kind: string, lang: string): string {
    return kind === 'coding'
        ? localizeText(lang, 'Coding', '编程', '編程')
        : localizeText(lang, 'Session', '会话', '會話');
}

const ROW_STYLE = {
    display: 'flex',
    flexDirection: 'row' as const,
    alignItems: 'flex-start' as const,
    gap: '6px',
    padding: '3px 8px',
    borderRadius: '8px',
    cursor: 'pointer',
};

/** A cloud instance drawn like a task row. The cloud mark is its type icon. */
export function CloudAgentListRow({
    item,
    lang,
    active,
    order,
    timeLabel,
    onOpen,
}: {
    item: CloudAgentListItem;
    lang: string;
    active: boolean;
    order?: number;
    timeLabel?: string;
    onOpen: () => void;
}) {
    const title = item.name || item.id;
    const kind = cloudAgentKindLabel(item.kind, lang);
    const running = useCloudTurnRunning(item.id, item.status);
    const kindLine = running
        ? `${kind} · ${localizeText(lang, 'In progress', '进行中', '進行中')}`
        : kind;
    return (
        <div data-testid="cloud-agent-task-row" data-cloud-agent={item.id} style={order == null ? undefined : { order }}>
            <div
                className={`sidebar-task-row${active ? ' is-active' : ''}`}
                role="button"
                tabIndex={0}
                aria-current={active ? 'true' : undefined}
                title={localizeText(lang, 'Click to open.', '单击打开', '單擊開啟')}
                style={{
                    ...ROW_STYLE,
                    ...(active ? { background: 'var(--theme-surface)', boxShadow: 'inset 3px 0 0 var(--theme-primary)' } : {}),
                }}
                onClick={onOpen}
                onKeyDown={(event) => {
                    if (event.key !== 'Enter' && event.key !== ' ') return;
                    event.preventDefault();
                    onOpen();
                }}
            >
                <TaskPlaceIcon place="cloud" lang={lang} />
                <span className="stsm-row-body">
                    <span className="mc-sidebar-task-title-row">
                        <span className="stsm-row-title">{title}</span>
                        {timeLabel ? <time className="mc-sidebar-task-time">{timeLabel}</time> : null}
                    </span>
                    <span
                        data-testid="cloud-agent-kind"
                        className="mc-task-working-dir"
                        style={{ display: 'flex', alignItems: 'center', marginTop: 1, color: 'var(--theme-text-secondary)', fontSize: 12, lineHeight: 1.35 }}
                    >
                        {kindLine}
                    </span>
                </span>
            </div>
        </div>
    );
}

import { useSyncExternalStore } from 'react';
import { EventsOn } from '../../../wailsjs/runtime';
import { localizeText } from '../../i18n';
import {
    applyCloudAgentStream,
    applyCloudAgentWatch,
    cloudAgentRound,
    cloudAgentSessionKey,
    cloudAgentStoreKey,
} from './cloudAgentSession';
import {
    bindRemoteAssistantActions,
    publishRemoteAssistantSession,
    type RemoteAssistantTranscriptMessage,
    type RemoteLivePart,
    type RemoteLiveRound,
} from './remoteAssistantSession';
import { asCloudAgentListItem, rememberCloudOpen, type CloudAgentListItem } from './taskPlace';

type Binding = (...args: unknown[]) => Promise<unknown>;

type Owner = {
    generation: number;
    lang: string;
    item: CloudAgentListItem;
    grant: string;
    taskId: string;
    history: string[];
    historyRequest: string;
    error: string;
};

let generation = 0;
let owner: Owner | null = null;
let streamOff: (() => void) | null = null;
const listeners = new Set<() => void>();

function binding(name: string): Binding | null {
    const app = (window as unknown as { go?: { main?: { App?: Record<string, unknown> } } }).go?.main?.App;
    const fn = app?.[name];
    return typeof fn === 'function' ? fn as Binding : null;
}

function notify() {
    listeners.forEach(listener => listener());
}

export function cloudAssistantActive(): boolean {
    return owner != null;
}

export function cloudAssistantInstanceId(): string {
    return owner?.item.id || '';
}

export function useCloudAssistantId(): string {
    return useSyncExternalStore(
        (listener: () => void) => {
            listeners.add(listener);
            return () => listeners.delete(listener);
        },
        cloudAssistantInstanceId,
        () => '',
    );
}

function parseJSON<T>(raw: unknown, fallback: T): T {
    if (typeof raw !== 'string' || !raw.trim()) return fallback;
    try {
        return JSON.parse(raw) as T;
    } catch {
        return fallback;
    }
}

function historyMessages(lines: string[]): RemoteAssistantTranscriptMessage[] {
    const messages: RemoteAssistantTranscriptMessage[] = [];
    for (const line of lines) {
        const split = line.indexOf('\n');
        const role = (split < 0 ? line : line.slice(0, split)).trim() || 'assistant';
        const content = split < 0 ? '' : line.slice(split + 1);
        if (!content.trim()) continue;
        messages.push({ role, content });
    }
    return messages;
}

function stripOrigin(text: string): string {
    return text.replace(/^〔[^〕]*〕/, '').trim();
}

/** The same live round a remote task projects: draft body, thinking, and progress. */
function cloudLive(round: ReturnType<typeof cloudAgentRound> | null): RemoteLiveRound | null {
    if (!round) return null;
    const text = (round.visible || round.body || '').trim();
    const thinking = round.thinking.some(block => block.text);
    if (!round.requestId && !text && !thinking && round.timeline.length === 0) return null;
    const parts: RemoteLivePart[] = [];
    round.thinking.forEach((block, index) => {
        parts.push({ kind: 'thinking', seq: index + 1, text: block.text, done: block.done });
    });
    if (text) parts.push({ kind: 'text', seq: parts.length + 1, text });
    for (const row of round.timeline) {
        const label = row.text || row.title;
        if (label) parts.push({ kind: 'progress', seq: parts.length + 1, text: label });
    }
    return {
        requestId: round.requestId,
        displayText: round.displayText || '',
        parts,
        streamDone: round.responded,
        responded: round.responded,
        baseline: -1,
        lastEventAt: Date.now(),
    };
}

function publishOwner() {
    const current = owner;
    if (!current) return;
    const text = (en: string, zh: string, hant?: string) => localizeText(current.lang, en, zh, hant);
    const sessionKey = cloudAgentSessionKey(current.item.id, current.taskId);
    const storeKey = cloudAgentStoreKey(current.grant, sessionKey);
    const round = storeKey ? cloudAgentRound(storeKey) : null;
    const messages = historyMessages(current.history);
    const prompt = stripOrigin(round?.displayText || '');
    if (prompt && !messages.some(message => message.role === 'user' && message.content.trim() === prompt)) {
        messages.push({ role: 'user', content: prompt });
    }
    const liveText = (round?.visible || round?.body || '').trim();
    const reasoning = (round?.thinking || []).map(block => block.text).filter(Boolean).join('\n\n');
    const last = messages[messages.length - 1];
    if (liveText && last && last.role !== 'user' && last.content.trim() === liveText) {
        if (reasoning && !last.reasoning) last.reasoning = reasoning;
    } else if (liveText || reasoning) {
        messages.push({
            role: 'assistant',
            content: liveText,
            reasoning: reasoning || undefined,
            remoteLive: true,
            liveThinking: !!round?.thinking.some(block => !block.done),
        });
    }
    const card = round?.cards.find(item => !item.expired && (item.actions?.length || item.text || item.confirmationId));
    if (card) {
        const host = messages[messages.length - 1];
        const actions = (card.actions || []).filter(action => action.command);
        const confirmation = card.confirmationId ? { id: card.confirmationId, summary: card.text } : undefined;
        if (host && host.role !== 'user') {
            host.actions = actions;
            host.confirmation = confirmation;
            if (!host.content.trim() && card.text) host.content = card.text;
        } else {
            messages.push({
                role: card.asError ? 'error' : 'assistant',
                content: card.text || liveText,
                actions,
                confirmation,
            });
        }
    }
    for (const note of round?.notices || []) {
        if (note.text) messages.push({ role: note.asError ? 'error' : 'assistant', content: note.text });
    }
    const live = cloudLive(round);
    publishRemoteAssistantSession({
        grantId: current.grant || current.item.id,
        machineName: text('Cloud', '云端', '雲端'),
        projectPath: `cloud-agent:${current.item.id}`,
        taskName: current.item.name || current.item.id,
        canSend: !!current.grant,
        offline: false,
        error: current.error,
        loading: !current.grant && !current.error,
        busy: current.item.status === 'running' || (!!live && !live.responded),
        live,
        messages,
    });
    bindRemoteAssistantActions({
        send: textToSend => sendCloud(current.generation, textToSend),
        stop: () => stopCloud(current.generation),
        close: closeCloudAssistant,
    });
}

function ensureStream() {
    if (streamOff) return;
    const off = EventsOn('remote-control:stream', (payload: unknown) => {
        const key = applyCloudAgentStream(payload);
        const current = owner;
        if (!current || !key) return;
        const want = cloudAgentStoreKey(current.grant, cloudAgentSessionKey(current.item.id, current.taskId));
        if (key !== want) return;
        if (current.error) current.error = '';
        publishOwner();
        const round = cloudAgentRound(key);
        if (round.responded && round.requestId && current.historyRequest !== round.requestId) {
            current.historyRequest = round.requestId;
            void reloadHistory(current.generation);
        }
    });
    streamOff = typeof off === 'function' ? off : () => {};
}

async function reloadHistory(gen: number) {
    const current = owner;
    const op = binding('RemoteControlOp');
    if (!current || current.generation !== gen || !op || !current.grant || !current.taskId) return;
    try {
        const raw = await op(current.grant, 'cloud_get_task', JSON.stringify({ task_id: current.taskId }));
        if (!owner || owner.generation !== gen) return;
        const body = parseJSON<{ messages?: string[] }>(raw, {});
        owner.history = Array.isArray(body.messages) ? body.messages : [];
        publishOwner();
    } catch {
        // The live round still shows what already arrived.
    }
}

/** Drop the cloud view without clearing a session the caller is about to publish. */
export function releaseCloudAssistant() {
    if (!owner) return;
    generation += 1;
    owner = null;
    notify();
}

/** Test isolation. Production close goes through closeCloudAssistant. */
export function resetCloudAssistantForTests() {
    generation += 1;
    owner = null;
    if (streamOff) {
        const off = streamOff;
        streamOff = null;
        off();
    }
    notify();
    publishRemoteAssistantSession(null);
    bindRemoteAssistantActions(null);
}

/** Leave the cloud task and return the main panel to the local conversation. */
export function closeCloudAssistant() {
    if (!owner) return;
    generation += 1;
    owner = null;
    notify();
    publishRemoteAssistantSession(null);
    bindRemoteAssistantActions(null);
}

async function sendCloud(gen: number, text: string): Promise<boolean> {
    const outgoing = text.trim();
    const current = owner;
    const op = binding('RemoteControlOp');
    if (!current || current.generation !== gen || !op || !current.grant || !outgoing) return false;
    try {
        let taskId = current.taskId;
        if (!taskId) {
            const created = parseJSON<{ task_id?: string }>(
                await op(current.grant, 'cloud_create_task', JSON.stringify({
                    title: Array.from(outgoing).slice(0, 48).join(''),
                })),
                {},
            );
            taskId = String(created.task_id || '').trim();
            if (!owner || owner.generation !== gen || !taskId) return false;
            owner.taskId = taskId;
            const session = cloudAgentSessionKey(current.item.id, taskId);
            const watchRaw = await op(current.grant, 'cloud_watch_task', JSON.stringify({ task_id: taskId, session_key: session }));
            applyCloudAgentWatch(current.grant, session, parseJSON(watchRaw, {}));
        }
        if (!owner || owner.generation !== gen) return false;
        await op(current.grant, 'cloud_send_message', JSON.stringify({
            task_id: owner.taskId,
            text: outgoing,
            client_message_id: `cm_${Date.now()}`,
        }));
        owner.history = [...owner.history, `user\n${outgoing}`];
        owner.error = '';
        publishOwner();
        return true;
    } catch (err) {
        if (!owner || owner.generation !== gen) return false;
        owner.error = err instanceof Error ? err.message : String(err);
        publishOwner();
        return false;
    }
}

async function stopCloud(gen: number): Promise<boolean> {
    const current = owner;
    const op = binding('RemoteControlOp');
    if (!current || current.generation !== gen || !op || !current.grant || !current.taskId) return false;
    try {
        await op(current.grant, 'cloud_stop_task', JSON.stringify({ task_id: current.taskId }));
        return true;
    } catch {
        return false;
    }
}

/**
 * Open one cloud instance in the same panel a remote task uses.
 * List position is the only local/cloud distinction; send, watch, and stop stay here.
 */
export async function openCloudAgent(item: CloudAgentListItem, lang: string): Promise<void> {
    const gen = ++generation;
    rememberCloudOpen(item.id);
    owner = { generation: gen, lang, item, grant: '', taskId: '', history: [], historyRequest: '', error: '' };
    notify();
    ensureStream();
    publishOwner();
    const open = binding('OpenCloudAgentGrant');
    const op = binding('RemoteControlOp');
    if (!open || !op) {
        if (!owner || owner.generation !== gen) return;
        owner.error = localizeText(lang, 'Cloud control is unavailable', '云端控制不可用', '雲端控制不可用');
        publishOwner();
        return;
    }
    try {
        const grant = String(await open(item.id) || '').trim();
        if (!owner || owner.generation !== gen) return;
        if (!grant) {
            owner.error = localizeText(lang, 'Cloud control is unavailable', '云端控制不可用', '雲端控制不可用');
            publishOwner();
            return;
        }
        owner.grant = grant;
        const listed = parseJSON<{ tasks?: Array<{ id?: string }> }>(
            await op(grant, 'cloud_list_tasks', ''),
            {},
        );
        if (!owner || owner.generation !== gen) return;
        const tasks = Array.isArray(listed.tasks) ? listed.tasks : [];
        const taskId = String(tasks[tasks.length - 1]?.id || '').trim();
        owner.taskId = taskId;
        if (taskId) {
            const session = cloudAgentSessionKey(item.id, taskId);
            const watchRaw = await op(grant, 'cloud_watch_task', JSON.stringify({ task_id: taskId, session_key: session }));
            if (!owner || owner.generation !== gen) return;
            applyCloudAgentWatch(grant, session, parseJSON(watchRaw, {}));
            const raw = await op(grant, 'cloud_get_task', JSON.stringify({ task_id: taskId }));
            if (!owner || owner.generation !== gen) return;
            const body = parseJSON<{ messages?: string[] }>(raw, {});
            owner.history = Array.isArray(body.messages) ? body.messages : [];
        }
        publishOwner();
    } catch (err) {
        if (!owner || owner.generation !== gen) return;
        owner.error = err instanceof Error ? err.message : String(err);
        publishOwner();
    }
}

export function cloudAgentsFromList(value: unknown): CloudAgentListItem[] {
    if (!Array.isArray(value)) return [];
    return value.map(asCloudAgentListItem).filter((item): item is CloudAgentListItem => item != null);
}

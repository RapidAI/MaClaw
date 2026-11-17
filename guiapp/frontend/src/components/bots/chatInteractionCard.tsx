import { useEffect, useState } from 'react';

export type ChatInteractionChoice = { id: string; label: string };
export type ChatInteractionField = { id: string; label: string };
export type ChatInteractionShot = {
    id: string;
    captured_at?: string;
    lease_id?: string;
    desktop_mode?: string;
    bot_id?: string;
    task_id?: string;
};

// ChatInteraction is the server card. Image bytes are not part of it.
export type ChatInteraction = {
    id: string;
    task_id: string;
    run_id: string;
    bot_id: string;
    kind: string;
    question: string;
    choices?: ChatInteractionChoice[];
    fields?: ChatInteractionField[];
    operation?: { kind?: string; version?: number; hash?: string };
    consent_hash?: string;
    target?: { url?: string; origin?: string; title?: string };
    handoff?: string;
    spotlight?: boolean;
    shot?: ChatInteractionShot;
    lease_id?: string;
    desktop_mode?: string;
    status?: string;
};

export type ChatInteractionDecision = {
    task_id: string;
    bot_id: string;
    choice_ids?: string[];
    fields?: Record<string, string>;
    operation_version: number;
    payload_hash: string;
    consent_hash: string;
    decline?: boolean;
    target_origin?: string;
    target_url?: string;
    shot_id?: string;
    captured_at?: string;
    lease_id?: string;
};

export type ChatDesktopOpen = {
    botId: string;
    requestId: string;
    runId: string;
    leaseId: string;
    capturedAt: string;
    shotId: string;
};

const SECRET_FIELD = /password|passwd|otp|cvv|pin|secret|card/i;

export function parseChatInteraction(raw: unknown): ChatInteraction | undefined {
    const data = typeof raw === 'string' ? parseJSON(raw) : raw;
    if (!data || typeof data !== 'object') return undefined;
    const card = data as Record<string, unknown>;
    const id = text(card.id);
    const task = text(card.task_id);
    const run = text(card.run_id);
    const bot = text(card.bot_id);
    const kind = text(card.kind);
    const question = text(card.question);
    if (!id || !task || !run || !bot || !kind || !question) return undefined;
    const operation = card.operation && typeof card.operation === 'object' ? card.operation as Record<string, unknown> : undefined;
    return {
        id,
        task_id: task,
        run_id: run,
        bot_id: bot,
        kind,
        question,
        choices: choicesOf(card.choices),
        fields: fieldsOf(card.fields),
        operation: operation ? {
            kind: text(operation.kind),
            version: Number(operation.version) || 0,
            hash: text(operation.hash),
        } : undefined,
        consent_hash: text(card.consent_hash),
        target: targetOf(card.target),
        handoff: text(card.handoff) || 'full_page',
        spotlight: false,
        shot: shotOf(card.shot),
        lease_id: text(card.lease_id),
        desktop_mode: text(card.desktop_mode),
        status: text(card.status) || 'pending',
    };
}

export function interactionDecision(card: ChatInteraction, choiceIds: string[], fields?: Record<string, string>, decline = false): ChatInteractionDecision | undefined {
    if (!card.operation?.hash || !card.operation.version || !card.consent_hash) return undefined;
    const cleanFields: Record<string, string> = {};
    for (const [key, value] of Object.entries(fields || {})) {
        if (SECRET_FIELD.test(key) || SECRET_FIELD.test(value)) return undefined;
        cleanFields[key] = value;
    }
    return {
        task_id: card.task_id,
        bot_id: card.bot_id,
        choice_ids: choiceIds,
        fields: Object.keys(cleanFields).length ? cleanFields : undefined,
        operation_version: card.operation.version,
        payload_hash: card.operation.hash,
        consent_hash: card.consent_hash,
        decline,
        target_origin: card.target?.origin,
        target_url: card.target?.url,
        shot_id: card.shot?.id,
        captured_at: card.shot?.captured_at,
        lease_id: card.shot?.lease_id || card.lease_id,
    };
}

export function desktopOpenFrom(card: ChatInteraction): ChatDesktopOpen | undefined {
    const lease = card.shot?.lease_id || card.lease_id || '';
    if (!card.bot_id || !lease) return undefined;
    return {
        botId: card.bot_id,
        requestId: card.id,
        runId: card.run_id,
        leaseId: lease,
        capturedAt: card.shot?.captured_at || '',
        shotId: card.shot?.id || '',
    };
}

function parseJSON(raw: string): unknown {
    try {
        return JSON.parse(raw);
    } catch {
        return undefined;
    }
}

function text(value: unknown): string {
    return typeof value === 'string' ? value.trim() : '';
}

function choicesOf(raw: unknown): ChatInteractionChoice[] | undefined {
    if (!Array.isArray(raw)) return undefined;
    const out: ChatInteractionChoice[] = [];
    for (const item of raw) {
        if (!item || typeof item !== 'object') continue;
        const row = item as Record<string, unknown>;
        const id = text(row.id);
        const label = text(row.label);
        if (id && label) out.push({ id, label });
    }
    return out.length ? out : undefined;
}

function fieldsOf(raw: unknown): ChatInteractionField[] | undefined {
    if (!Array.isArray(raw)) return undefined;
    const out: ChatInteractionField[] = [];
    for (const item of raw) {
        if (!item || typeof item !== 'object') continue;
        const row = item as Record<string, unknown>;
        const id = text(row.id);
        const label = text(row.label);
        if (!id || !label || SECRET_FIELD.test(id) || SECRET_FIELD.test(label)) continue;
        out.push({ id, label });
    }
    return out.length ? out : undefined;
}

function targetOf(raw: unknown): ChatInteraction['target'] | undefined {
    if (!raw || typeof raw !== 'object') return undefined;
    const row = raw as Record<string, unknown>;
    const url = text(row.url);
    const origin = text(row.origin);
    const title = text(row.title);
    if (!url && !origin && !title) return undefined;
    return { url, origin, title };
}

function shotOf(raw: unknown): ChatInteractionShot | undefined {
    if (!raw || typeof raw !== 'object') return undefined;
    const row = raw as Record<string, unknown>;
    const id = text(row.id);
    if (!id) return undefined;
    return {
        id,
        captured_at: text(row.captured_at),
        lease_id: text(row.lease_id),
        desktop_mode: text(row.desktop_mode),
        bot_id: text(row.bot_id),
        task_id: text(row.task_id),
    };
}

function statusLabel(status: string): string {
    switch (status) {
        case 'processing':
            return '正在核对页面上的实际结果';
        case 'cancelled':
        case 'declined':
            return '已取消';
        case 'done':
            return '已有核对结果';
        default:
            return '待你处理';
    }
}

export function ChatInteractionCard({
    card,
    shotData,
    loadShot,
    lang,
    onDecide,
    onOpenDesktop,
}: {
    card: ChatInteraction;
    shotData?: string;
    loadShot?: (card: ChatInteraction) => Promise<string | undefined>;
    lang: string;
    onDecide: (decision: ChatInteractionDecision) => void;
    onOpenDesktop: (open: ChatDesktopOpen) => void;
}) {
    const [values, setValues] = useState<Record<string, string>>({});
    const [picked, setPicked] = useState<string[]>([]);
    const [loaded, setLoaded] = useState('');
    useEffect(() => {
        if (shotData || !loadShot || !card.shot?.id) return undefined;
        let live = true;
        loadShot(card).then(data => {
            if (live && data && !data.startsWith('http')) setLoaded(data);
        }).catch(() => undefined);
        return () => { live = false; };
    }, [card, loadShot, shotData]);
    const zh = lang.startsWith('zh');
    const open = desktopOpenFrom(card);
    const picture = shotData || loaded;
    const showShot = Boolean(picture && card.shot?.id && !picture.startsWith('http'));
    const submit = (choiceIds: string[], decline = false) => {
        const decision = interactionDecision(card, choiceIds, values, decline);
        if (decision) onDecide(decision);
    };
    return (
        <div className="desktop-bot-chat__inquiry" data-testid="chat-interaction-card">
            <div className="desktop-bot-chat__inquiry-prompt" data-testid="chat-interaction-question">{card.question}</div>
            <div data-testid="chat-interaction-status">{statusLabel(card.status || 'pending')}</div>
            {card.target?.origin ? (
                <div data-testid="chat-interaction-origin">{card.target.origin}</div>
            ) : null}
            {card.kind === 'native_handoff' ? (
                <div data-testid="chat-interaction-handoff">{zh ? '在这台电脑上操作。点截图打开这一任务的电脑，不会打开别的桌面。' : 'Use this computer. The screenshot opens this task’s computer.'}</div>
            ) : null}
            {showShot && open ? (
                <button
                    type="button"
                    data-testid="chat-interaction-shot"
                    onClick={() => onOpenDesktop(open)}
                >
                    <img alt={zh ? '这一任务的屏幕' : 'This task’s screen'} src={picture} />
                </button>
            ) : null}
            {card.fields?.map(field => (
                <label key={field.id}>
                    {field.label}
                    <input
                        data-testid={`chat-interaction-field-${field.id}`}
                        value={values[field.id] || ''}
                        onChange={event => setValues(current => ({ ...current, [field.id]: event.target.value }))}
                    />
                </label>
            ))}
            <div className="desktop-bot-chat__inquiry-options">
                {card.choices?.map(choice => (
                    <button
                        type="button"
                        key={choice.id}
                        data-testid={`chat-interaction-choice-${choice.id}`}
                        onClick={() => {
                            if (card.kind === 'multi_choice') {
                                setPicked(current => current.includes(choice.id) ? current.filter(id => id !== choice.id) : [...current, choice.id]);
                                return;
                            }
                            submit([choice.id], choice.id === 'decline' || choice.id === 'cancel');
                        }}
                    >{choice.label}</button>
                ))}
            </div>
            {card.kind === 'multi_choice' || card.kind === 'text' ? (
                <button
                    type="button"
                    data-testid="chat-interaction-submit"
                    onClick={() => submit(picked, false)}
                >{zh ? '提交' : 'Submit'}</button>
            ) : null}
        </div>
    );
}

import { EventsOn } from '../../../wailsjs/runtime';
import { PostDesktopBotCross } from '../../../wailsjs/go/main/App';
import { desktopBotLine } from './desktopBotAck';
import { desktopTurnFromPayload, isDesktopBotFiller } from './visibleDesktopTurn';
import {
    appendDesktopBotReport,
    applySupplementOutcomes,
    desktopBotFiles,
    desktopBotImages,
    desktopBotLocalPaths,
    dropPendingBotReply,
    markDesktopBotNotice,
    messagesForBot,
    notePendingBotDesktop,
    personaForBot,
    saveBotMessages,
    settlePendingBotReply,
    takeDesktopBotCrossRun,
    type BotPhase,
    type DesktopBotAsk,
} from './desktopBots';

function oneLine(text: string): string {
    const line = text.trim().split(/\r?\n/).find(part => part.trim()) || '';
    const chars = Array.from(line.trim());
    if (chars.length <= 80) return chars.join('');
    return chars.slice(0, 80).join('');
}

function reportedTurnPhase(data: Record<string, unknown>): BotPhase | undefined {
    const phase = String(data.phase || data.Phase || '').trim();
    if (phase === 'execute' || phase === 'plan') return phase;
    return undefined;
}

export function reportDesktopBotCrossResult(userId: string, botId: string, text: string) {
    const run = takeDesktopBotCrossRun(userId, botId);
    const body = (text || '').trim();
    if (!run || !body) return;
    markDesktopBotNotice(userId, `${run.noteId}:result:${botId}`);
    void PostDesktopBotCross(botId, 'result', '', body, '', run.noteId).then(posted => {
        const id = String(posted?.notes?.[0]?.id || '').trim();
        if (id) markDesktopBotNotice(userId, `${id}:target`);
    }).catch(() => undefined);
}

export function desktopBotRecentChat(ownerId: string, botId: string): string[] {
    return messagesForBot(ownerId, botId)
        .filter(item => !item.pending && (item.content || '').trim())
        .slice(-6)
        .map(item => `${item.role === 'user' ? '你' : '我'}: ${oneLine(item.content).slice(0, 160)}`);
}

let resultLang = 'en';
export function setDesktopBotResultLang(lang: string) {
    if (lang) resultLang = lang;
}

export function rewordFailedTurn(userId: string, botId: string, messageId: string, shown: string, cause: string) {
    if (!messageId || !shown) return;
    const stored = messagesForBot(userId, botId);
    const idx = stored.findIndex(item => item.id === messageId);
    const earlier = stored.slice(0, idx < 0 ? stored.length : idx).reverse().find(item => item.role === 'user')?.content || '';
    const recent = desktopBotRecentChat(userId, botId).slice(0, -1);
    void desktopBotLine(runWasInterrupted(cause) ? 'interrupted' : 'error', {
        lang: resultLang,
        message: oneLine(earlier).slice(0, 160),
        persona: personaForBot(userId, botId),
        recent,
        status: { stop_reason: oneLine(cause).slice(0, 240), last_task: oneLine(earlier).slice(0, 160) || undefined },
    }, shown).then(line => {
        if (!line || line === shown) return;
        const now = messagesForBot(userId, botId);
        if (!now.some(item => item.id === messageId && item.content === shown)) return;
        saveBotMessages(userId, botId, now.map(item => item.id === messageId ? { ...item, content: line } : item));
    });
}

export function runWasInterrupted(raw: string): boolean {
    return /interrupted|service restarted|中断|中斷|被打断|重启/i.test(raw || '');
}

export type DesktopBotScreenEvent = { url: string; reported: boolean; userControl: boolean; reason: string; cleared: boolean };

export function screenOf(data: Record<string, unknown>): DesktopBotScreenEvent {
    const url = String(data.novnc_url || data.NovncURL || data.desktop_handoff_url || data.DesktopHandoffURL || '');
    const reported = ['user_control', 'desktop_user_control', 'UserControl', 'DesktopUserControl', 'desktop_handoff', 'DesktopHandoff'].some(key => Object.prototype.hasOwnProperty.call(data, key));
    const userControl = data.user_control === true || data.desktop_user_control === true || data.UserControl === true || data.DesktopUserControl === true || data.desktop_handoff === true || data.DesktopHandoff === true;
    const reason = String(data.attention_reason || data.desktop_attention_reason || data.AttentionReason || '').trim();
    const cleared = data.cleared === true || data.Cleared === true;
    return { url, reported, userControl, reason, cleared };
}

export function askFrom(data: Record<string, unknown>): DesktopBotAsk | undefined {
    const inputType = String(data.ask_user_input_type || data.AskUserInputType || '').trim();
    const secretName = String(data.ask_user_secret_name || data.AskUserSecretName || '').trim();
    const question = String(data.ask_user_question || data.AskUserQuestion || '').trim();
    let options: string[] | undefined;
    const raw = data.ask_user_options_json ?? data.AskUserOptionsJSON;
    if (typeof raw === 'string' && raw.trim()) {
        try {
            const parsed = JSON.parse(raw) as unknown;
            if (Array.isArray(parsed)) {
                const kept = parsed.map(item => String(item).trim()).filter(item => item !== '' && !isDesktopBotFiller(item));
                if (kept.length) options = kept;
            }
        } catch {
            options = undefined;
        }
    }
    if (!inputType && !secretName && !question && !(options && options.length)) return undefined;
    return { inputType, secretName, question, options };
}

let desktopBotResultBound = false;

export function bindDesktopBotResultStore() {
    if (desktopBotResultBound) return;
    desktopBotResultBound = true;
    EventsOn('ai-assistant-response', handleDesktopBotResult);
    EventsOn('desktop-bot-view', handleDesktopBotView);
    EventsOn('desktop-bot-report', handleDesktopBotReport);
    EventsOn('desktop-bot-supplement-state', handleDesktopBotSupplementState);
}

export function handleDesktopBotSupplementState(payload: unknown) {
    let data: Record<string, unknown>;
    try {
        data = (typeof payload === 'string' ? JSON.parse(payload) : payload) as Record<string, unknown>;
    } catch {
        return;
    }
    if (!data || typeof data !== 'object') return;
    const sessionKey = String(data.session_key || data.SessionKey || '');
    const split = sessionKey.lastIndexOf(':');
    const botId = String(data.bot_id || data.BotID || '').trim() || (split > 0 ? sessionKey.slice(split + 1) : '');
    if (!botId) return;
    const userId = split > 0 ? sessionKey.slice(0, split) : '';
    if (!userId) return;
    const runId = String(data.run_id || data.RunID || '').trim();
    if (!runId) return;
    const items = Array.isArray(data.items) ? data.items as Array<{ id?: string; state?: string; content?: string }> : [];
    applySupplementOutcomes(userId, botId, runId, items);
}

export function handleDesktopBotView(payload: unknown) {
    let data: Record<string, unknown>;
    try {
        data = (typeof payload === 'string' ? JSON.parse(payload) : payload) as Record<string, unknown>;
    } catch {
        return;
    }
    if (!data || typeof data !== 'object') return;
    const requestId = String(data.request_id || data.RequestID || '');
    if (!requestId.startsWith('desktop-bot-')) return;
    const sessionKey = String(data.session_key || data.SessionKey || '');
    const split = sessionKey.lastIndexOf(':');
    if (split <= 0) return;
    const screen = screenOf(data);
    if (screen.cleared || (!screen.userControl && screen.reason === '')) return;
    notePendingBotDesktop(sessionKey.slice(0, split), sessionKey.slice(split + 1), {
        handoffUrl: screen.url,
        userControl: screen.userControl,
        attentionReason: screen.reason,
        reported: screen.reported,
        requestId,
    });
}

export function handleDesktopBotResult(payload: unknown) {
    let data: Record<string, unknown>;
    try {
        data = (typeof payload === 'string' ? JSON.parse(payload) : payload) as Record<string, unknown>;
    } catch {
        return;
    }
    if (!data || typeof data !== 'object') return;
    const requestId = String(data.request_id || data.RequestID || '');
    if (!requestId.startsWith('desktop-bot-')) return;
    const sessionKey = String(data.session_key || data.SessionKey || '');
    const split = sessionKey.lastIndexOf(':');
    if (split <= 0) return;
    const result = responseText(data);
    const screen = screenOf(data);
    const attached = attachedFiles(data, result.failed);
    const resultUser = sessionKey.slice(0, split);
    const resultBot = sessionKey.slice(split + 1);
    const images = result.failed ? undefined : imagesFrom(data);
    const shotPaths = result.failed ? undefined : shotPathsFrom(data);
    const ask = askFrom(data);
    if (!turnHasVisibleResult(result, ask, images, shotPaths, attached)) {
        dropPendingBotReply(resultUser, resultBot, requestId);
        return;
    }
    const shown = sentenceForTurn(result, ask);
    const settledId = settlePendingBotReply(resultUser, resultBot, {
        content: shown,
        failed: result.failed,
        handoffUrl: screen.userControl || screen.reason ? screen.url : '',
        userControl: screen.userControl,
        attentionReason: screen.reason,
        askUser: ask,
        requestId,
        images,
        shotPaths,
        files: attached.files,
        localPaths: attached.localPaths,
    });
    if (!result.silent) reportDesktopBotCrossResult(resultUser, resultBot, result.content);
    if (result.failed && !ask && settledId) rewordFailedTurn(resultUser, resultBot, settledId, shown, String(result.content || ''));
}

export function handleDesktopBotReport(payload: unknown) {
    let data: Record<string, unknown>;
    try {
        data = (typeof payload === 'string' ? JSON.parse(payload) : payload) as Record<string, unknown>;
    } catch {
        return;
    }
    if (!data || typeof data !== 'object') return;
    const schedule = data.schedule_report === true || data.ScheduleReport === true;
    const delivery = data.delivery_report === true || data.DeliveryReport === true;
    if (!schedule && !delivery) return;
    const requestId = String(data.request_id || data.RequestID || '');
    if (schedule && !requestId.startsWith('desktop-bot-sched-')) return;
    if (delivery && !requestId.startsWith('desktop-bot-deliver-')) return;
    const sessionKey = String(data.session_key || data.SessionKey || '');
    const split = sessionKey.lastIndexOf(':');
    if (split <= 0) return;
    const result = responseText(data);
    const screen = screenOf(data);
    const attached = attachedFiles(data, result.failed);
    const images = result.failed ? undefined : imagesFrom(data);
    const shotPaths = result.failed ? undefined : shotPathsFrom(data);
    const ask = askFrom(data);
    if (!turnHasVisibleResult(result, ask, images, shotPaths, attached)) return;
    const reportUser = sessionKey.slice(0, split);
    const reportBot = sessionKey.slice(split + 1);
    const sentence = sentenceForTurn(result, ask);
    appendDesktopBotReport(reportUser, reportBot, {
        content: delivery ? deliveryAnswering(sentence, String(data.task || data.Task || ''), messagesForBot(reportUser, reportBot)) : sentence,
        failed: result.failed,
        handoffUrl: screen.userControl || screen.reason ? screen.url : '',
        userControl: screen.userControl,
        attentionReason: screen.reason,
        askUser: ask,
        requestId,
        images,
        shotPaths,
        files: attached.files,
        localPaths: attached.localPaths,
        phase: reportedTurnPhase(data),
    });
}

export function imagesFrom(data: Record<string, unknown>) {
    return desktopBotImages(data.desktop_images ?? data.DesktopImages);
}

export function shotPathsFrom(data: Record<string, unknown>) {
    return desktopBotLocalPaths(data.desktop_shot_paths ?? data.DesktopShotPaths);
}

function filesFrom(data: Record<string, unknown>) {
    return desktopBotFiles(data.desktop_files ?? data.DesktopFiles);
}

function localPathsFrom(data: Record<string, unknown>) {
    return desktopBotLocalPaths(data.local_file_paths ?? data.LocalFilePaths ?? data.local_file_path ?? data.LocalFilePath);
}

export function attachedFiles(data: Record<string, unknown>, failed: boolean) {
    if (failed) return { localPaths: undefined, files: undefined };
    const localPaths = localPathsFrom(data);
    return { localPaths, files: localPaths?.length ? undefined : filesFrom(data) };
}

function desktopTurnHasAttachment(images: unknown[] | undefined, shotPaths: string[] | undefined, attached: { files?: unknown[]; localPaths?: string[] }) {
    return (images && images.length > 0) || (shotPaths && shotPaths.length > 0) || (attached.files && attached.files.length > 0) || (attached.localPaths && attached.localPaths.length > 0);
}

export function responseText(payload: Record<string, unknown>) {
    const view = desktopTurnFromPayload(payload);
    return { content: view.sentence, failed: view.failed, silent: !view.bubble };
}

export function askKeepsTurn(ask: DesktopBotAsk | undefined): boolean {
    if (!ask) return false;
    const questionText = (ask.question || '').trim();
    if (questionText && !isDesktopBotFiller(questionText)) return true;
    if ((ask.secretName || '').trim()) return true;
    return choiceLine(ask) !== '';
}

function choiceLine(ask: DesktopBotAsk | undefined): string {
    return (ask?.options || [])
        .map(item => (item || '').trim())
        .filter(item => item && !isDesktopBotFiller(item))
        .join('\n');
}

export function sentenceForTurn(result: { content: string; failed?: boolean }, ask: DesktopBotAsk | undefined): string {
    if (result.content && !result.failed) return result.content;
    const question = (ask?.question || '').trim();
    if (question && !isDesktopBotFiller(question)) return question;
    if (!result.failed) {
        const choices = choiceLine(ask);
        if (choices) return choices;
        const secret = (ask?.secretName || '').trim();
        if (secret && !isDesktopBotFiller(secret)) return secret;
    }
    return result.content || '';
}

export function turnHasVisibleResult(result: { silent: boolean }, ask: DesktopBotAsk | undefined, images: unknown[] | undefined, shotPaths: string[] | undefined, attached: { files?: unknown[]; localPaths?: string[] }) {
    return !result.silent || askKeepsTurn(ask) || desktopTurnHasAttachment(images, shotPaths, attached);
}

/**
 * A delivered result belongs to the run that produced it, not to whatever the
 * person said last. When its own request is not the latest thing they said,
 * the result names that request, so a run started elsewhere (another window, a
 * script, a missed result) never reads as the answer to the newest message.
 */
export function deliveryAnswering(sentence: string, task: string, messages: Array<{ role: string; content: string }>): string {
    const asked = oneLineTask(task);
    if (!asked || !sentence) return sentence;
    let lastUser = '';
    for (let i = messages.length - 1; i >= 0; i--) {
        if (messages[i].role === 'user') { lastUser = oneLineTask(messages[i].content); break; }
    }
    if (lastUser && lastUser === asked) return sentence;
    const quoted = asked.length > 40 ? `${asked.slice(0, 40)}…` : asked;
    return `「${quoted}」\n${sentence}`;
}

function oneLineTask(text: string): string {
    return (text || '').replace(/\s+/g, ' ').trim();
}

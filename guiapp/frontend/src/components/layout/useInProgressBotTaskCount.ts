import { useEffect, useState } from 'react';
import { currentBotAccessEnabled, subscribeBotGrant } from '../bots/botOpenGate';
import { BOT_TRANSCRIPT_EVENT } from '../bots/desktopBots';
import { currentLocalManagementTasks, subscribeLocalManagementTasks, tasksForBotMonitor } from '../bots/localManagementTasks';
import { startVisibleInterval } from '../../utils/visibleInterval';

// Mirrors the rail's Bot grant. Starts from the last noted value so the
// status card does not wait for another DesktopBotAccess round trip.
export function useBotAccessEnabled(): boolean {
    const [enabled, setEnabled] = useState(currentBotAccessEnabled);
    useEffect(() => {
        setEnabled(current => {
            const next = currentBotAccessEnabled();
            return current === next ? current : next;
        });
        return subscribeBotGrant(setEnabled);
    }, []);
    return enabled;
}

const BOT_TASK_COUNT_POLL_MS = 2000;

type BotTaskSlot = {
    count: number;
    listeners: Set<(count: number) => void>;
    stop: () => void;
};

// One poll and one transcript listener per account. The rail, the status
// card, and the taskbar badge all read this count; separate timers would
// each re-walk the transcript.
const botTaskSlots = new Map<string, BotTaskSlot>();

function publishBotTaskCount(slot: BotTaskSlot, userId: string) {
    const next = tasksForBotMonitor(userId).length;
    if (slot.count === next) return;
    slot.count = next;
    slot.listeners.forEach(listener => listener(next));
}

function subscribeBotTaskCount(userId: string, listener: (count: number) => void): () => void {
    let slot = botTaskSlots.get(userId);
    if (!slot) {
        const created: BotTaskSlot = {
            count: tasksForBotMonitor(userId).length,
            listeners: new Set(),
            stop: () => {},
        };
        const onTranscript = (event: Event) => {
            const detail = (event as CustomEvent<{ userId?: string }>).detail;
            if (detail?.userId && detail.userId !== userId) return;
            publishBotTaskCount(created, userId);
        };
        const stopInterval = startVisibleInterval(() => publishBotTaskCount(created, userId), BOT_TASK_COUNT_POLL_MS);
        const stopLocal = subscribeLocalManagementTasks(() => publishBotTaskCount(created, userId));
        window.addEventListener(BOT_TRANSCRIPT_EVENT, onTranscript);
        created.stop = () => {
            stopInterval();
            stopLocal();
            window.removeEventListener(BOT_TRANSCRIPT_EVENT, onTranscript);
        };
        slot = created;
        botTaskSlots.set(userId, slot);
    }
    slot.listeners.add(listener);
    const next = tasksForBotMonitor(userId).length;
    if (next !== slot.count) publishBotTaskCount(slot, userId);
    else listener(slot.count);
    return () => {
        const current = botTaskSlots.get(userId);
        if (!current) return;
        current.listeners.delete(listener);
        if (current.listeners.size === 0) {
            current.stop();
            botTaskSlots.delete(userId);
        }
    };
}

// In-progress bot tasks for the rail badge and the taskbar overlay. Without
// a Bot grant the badge still counts a desktop install or update running
// on this computer.
export function useInProgressBotTaskCount(userId: string, enabled: boolean): number {
    const [count, setCount] = useState(0);
    useEffect(() => {
        if (!enabled) {
            const publishLocal = () => {
                const next = currentLocalManagementTasks().length;
                setCount(current => current === next ? current : next);
            };
            return subscribeLocalManagementTasks(publishLocal);
        }
        return subscribeBotTaskCount(userId, next => {
            setCount(current => current === next ? current : next);
        });
    }, [userId, enabled]);
    return count;
}

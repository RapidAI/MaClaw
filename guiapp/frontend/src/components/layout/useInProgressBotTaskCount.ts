import { useEffect, useState } from 'react';
import { currentBotAccessEnabled, subscribeBotGrant } from '../bots/botOpenGate';
import { currentInProgressBotTasks } from '../bots/desktopBots';
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

// In-progress bot tasks for the rail badge. The count stays at 0 until Hub
// has granted Bot access, matching the Bot tab on the task monitor.
export function useInProgressBotTaskCount(userId: string, enabled: boolean): number {
    const [count, setCount] = useState(0);
    useEffect(() => {
        if (!enabled) {
            setCount(current => current === 0 ? current : 0);
            return;
        }
        let cancelled = false;
        const load = () => {
            if (cancelled) return;
            const next = currentInProgressBotTasks(userId).length;
            setCount(current => current === next ? current : next);
        };
        load();
        const stop = startVisibleInterval(load, BOT_TASK_COUNT_POLL_MS);
        return () => {
            cancelled = true;
            stop();
        };
    }, [userId, enabled]);
    return count;
}

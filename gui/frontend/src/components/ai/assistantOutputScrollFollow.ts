export function planConversationFollow(opts: {
    activityKey?: string;
    prevActivityKey?: string;
    userScrolledUp: boolean;
    messageCount: number;
    prevMessageCount: number;
}): { nextActivityKey?: string; nextMessageCount: number; settleFrames?: number } {
    const activityChanged = !!opts.activityKey && opts.activityKey !== opts.prevActivityKey;
    if (opts.userScrolledUp) {
        return { nextActivityKey: opts.activityKey, nextMessageCount: opts.messageCount };
    }
    if (activityChanged || opts.messageCount !== opts.prevMessageCount) {
        return { nextActivityKey: opts.activityKey, nextMessageCount: opts.messageCount, settleFrames: 1 };
    }
    return { nextActivityKey: opts.activityKey, nextMessageCount: opts.prevMessageCount, settleFrames: 0 };
}

export function runPinnedScroll(
    applyScroll: (behavior: ScrollBehavior) => void,
    cancelScheduled: () => void,
    setRaf: (id: number | null) => void,
    behavior: ScrollBehavior,
    force: boolean,
    userScrolledUp: boolean,
    settleFrames: number,
): void {
    const scroll = () => {
        if (!force && userScrolledUp) return;
        applyScroll(behavior);
    };
    const scheduleSettledScroll = (remainingFrames: number) => {
        if (remainingFrames <= 0 || typeof requestAnimationFrame !== "function") return;
        setRaf(requestAnimationFrame(() => {
            setRaf(null);
            scroll();
            scheduleSettledScroll(remainingFrames - 1);
        }));
    };
    cancelScheduled();
    scroll();
    scheduleSettledScroll(settleFrames);
}

export interface BotOpenDecision {
    open: boolean;
    leave: boolean;
    toast: boolean;
}

// A newer navigation cancels an in-flight Bot open. Denial only leaves the
// Bot page when that page is the one still showing.
export function botOpenDecision(epoch: number, request: number, enabled: boolean, currentTab: string): BotOpenDecision {
    if (request !== epoch) return { open: false, leave: false, toast: false };
    if (enabled) return { open: true, leave: false, toast: false };
    return { open: false, leave: currentTab === 'bots', toast: true };
}

// Leaving the Bot page, or opening any other page, cancels an in-flight Bot open.
// Opening Bot itself keeps the epoch so the access check can still land.
export function navigationEpoch(epoch: number, nextTab: string, currentTab: string): number {
    if (nextTab === 'bots' || nextTab === currentTab) return epoch;
    return epoch + 1;
}

type BotAccessListener = (enabled: boolean) => void;
const botAccessListeners = new Set<BotAccessListener>();
let botAccessReadGeneration = 0;

// Every Hub access read takes a generation. A newer read, or a published
// answer, retires any poll that started earlier.
export function beginBotAccessRead(): number {
    botAccessReadGeneration += 1;
    return botAccessReadGeneration;
}

export function isCurrentBotAccessRead(id: number): boolean {
    return id === botAccessReadGeneration;
}

// The rail and the click confirmation share one answer. Publishing retires
// older reads so a late poll cannot enable Bot after a denial. A denial hides
// the rail entry entirely.
export function publishBotAccess(enabled: boolean) {
    botAccessReadGeneration += 1;
    botAccessListeners.forEach(listener => listener(enabled));
}

export function subscribeBotAccess(listener: BotAccessListener) {
    botAccessListeners.add(listener);
    return () => { botAccessListeners.delete(listener); };
}

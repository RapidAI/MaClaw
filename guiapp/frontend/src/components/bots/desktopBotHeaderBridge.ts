import { useSyncExternalStore } from 'react';

// 群聊按钮从桌面 Bot 聊天的悬浮标题条移到了应用顶栏（MainTopHeader，bots
// 页签）。顶栏拿不到 workspace 内部的 selected / collab.features.rooms 状态，
// 这里用最小模块桥接：DesktopBotWorkspace 挂载时登记群聊入口与回调，卸载
// 时撤销；登记只发生在 bots 页签，所以 MainTopHeader 以「有登记」为开关，
// 不需要再比较 navTab。
type Listener = () => void;
const listeners = new Set<Listener>();
let roomsVisible = false;
let openRooms: (() => void) | null = null;

function emit() {
    listeners.forEach(listener => listener());
}

/** DesktopBotWorkspace 的当前状态顶栏可直接消费；null 撤销登记。
 * 只有可见性变化才通知订阅者：workspace 每次刷新 bots 数组都会带着新的
 * open 回调重新登记，回调身份变化不应触发顶栏重渲染。 */
export function noteDesktopBotRooms(next: { visible: boolean; open: () => void } | null) {
    const visible = !!next?.visible;
    const open = next?.open ?? null;
    const visibilityChanged = roomsVisible !== visible;
    roomsVisible = visible;
    openRooms = open;
    if (visibilityChanged) emit();
}

export function currentDesktopBotRoomsVisible(): boolean {
    return roomsVisible;
}

export function openDesktopBotRoomsFromHeader(): void {
    openRooms?.();
}

export function subscribeDesktopBotRooms(listener: Listener): () => void {
    listeners.add(listener);
    return () => {
        listeners.delete(listener);
    };
}

/** 顶栏的群聊按钮可见性。 */
export function useDesktopBotRoomsVisible(): boolean {
    return useSyncExternalStore(subscribeDesktopBotRooms, currentDesktopBotRoomsVisible);
}

export function resetDesktopBotHeaderBridgeForTests(): void {
    roomsVisible = false;
    openRooms = null;
    listeners.clear();
}

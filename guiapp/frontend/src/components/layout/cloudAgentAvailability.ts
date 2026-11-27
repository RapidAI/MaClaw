import { useSyncExternalStore } from 'react';
import { cloudAgentCatalog, onCloudAgentListRefresh } from './cloudAgentAssistant';
import { startVisibleInterval } from '../../utils/visibleInterval';

/**
 * Hub 云端 Agent 开关（ListCloudAgents 的 enabled 字段），给任务栏之外的
 * 读者用 —— 目前是设置里的 云端 Agent 页签。任务栏自己也在轮询同一个
 * 接口（它还要 instances），它的回答经由 noteCloudAgentsEnabled 汇入这
 * 里的唯一真相；本模块的轮询负责任务栏不存在的时候（面板折叠、停在
 * 设置页、启动深链直接进设置）开关仍然保鲜。
 */

// 任务栏活着时它 30s 一拉并随时汇入，这里的节奏只服务于任务栏缺席的
// 场景，60s 对设置页签的可见性足够 —— 避免稳态下同一接口被拉两次。
const POLL_MS = 60000;

type EnabledListener = (enabled: boolean) => void;
const listeners = new Set<EnabledListener>();
let enabled = false;
/** 还没有任何一次成功回答时开关视为关 —— 与任务栏云端位置同规则。 */
let noted = false;
/** 只有最新一次 fetch 可以写值；轮询停止时自增以作废在途请求。 */
let fetchGeneration = 0;
let stopPolling: (() => void) | null = null;

function listBinding(): (() => Promise<unknown>) | null {
    const list = (window as unknown as { go?: { main?: { App?: { ListCloudAgents?: () => Promise<unknown> } } } })
        .go?.main?.App?.ListCloudAgents;
    return typeof list === 'function' ? list : null;
}

function fetchOnce() {
    const list = listBinding();
    if (!list) return;
    const gen = ++fetchGeneration;
    void list().then((rows) => {
        if (gen !== fetchGeneration) return;
        noteCloudAgentsEnabled(cloudAgentCatalog(rows).enabled);
    }).catch(() => {
        // 被拒绝的调用不是 Hub 的回答（未登录、瞬时网络问题）：
        // 与任务栏同规则，保留上一次的回答直到下一次真正返回。
    });
}

/** Hub 的回答（任务栏的轮询或本模块的轮询）。相同值不重复通知。 */
export function noteCloudAgentsEnabled(next: boolean) {
    if (noted && enabled === next) return;
    enabled = next;
    noted = true;
    listeners.forEach(listener => listener(enabled));
}

export function currentCloudAgentsEnabled(): boolean {
    return enabled;
}

export function subscribeCloudAgentsEnabled(listener: EnabledListener): () => void {
    listeners.add(listener);
    if (listeners.size === 1 && !stopPolling) {
        fetchOnce();
        const stopTimer = startVisibleInterval(fetchOnce, POLL_MS);
        const offRefresh = onCloudAgentListRefresh(fetchOnce);
        stopPolling = () => {
            stopPolling = null;
            offRefresh();
            stopTimer();
        };
    }
    return () => {
        listeners.delete(listener);
        if (listeners.size === 0) {
            fetchGeneration += 1;
            stopPolling?.();
        }
    };
}

/** The Hub switch as reactive state. */
export function useCloudAgentsEnabled(): boolean {
    return useSyncExternalStore(
        subscribeCloudAgentsEnabled,
        () => enabled,
        () => enabled,
    );
}

/** Test isolation. Production starts closed until Hub answers. */
export function resetCloudAgentsEnabledForTests() {
    fetchGeneration += 1;
    if (noted) {
        noted = false;
        enabled = false;
    }
}

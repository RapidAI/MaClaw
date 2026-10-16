import type { AIAssistantInitStatus } from "./useAIAssistant";

const initStatusLabels: Record<AIAssistantInitStatus, { en: string; zh: string }> = {
    connecting: { en: "Connecting to Hub...", zh: "\u6b63\u5728\u8fde\u63a5 Hub..." },
    loading: { en: "Loading components...", zh: "\u6b63\u5728\u52a0\u8f7d\u7ec4\u4ef6..." },
    warming: { en: "Warming up...", zh: "\u6b63\u5728\u9884\u70ed..." },
    ready: { en: "Ready", zh: "\u5c31\u7eea" },
    degraded: { en: "Ready (Hub offline)", zh: "\u5c31\u7eea\uff08Hub \u79bb\u7ebf\uff09" },
};

export function getAssistantInitLabel(status: AIAssistantInitStatus | undefined, lang: string) {
    const statusKey = status ?? "connecting";
    return initStatusLabels[statusKey][lang === "en" ? "en" : "zh"];
}
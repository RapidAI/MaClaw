import type { ChatMessage } from "./useAIAssistant";
import { parseCodingAgentProgress } from "./CodingAgentProgressStatus";

export function compactCodingAgentProgressMessages(messages: ChatMessage[]): ChatMessage[] {
    let latestCodingIndex = -1;
    const isCodingProgress = messages.map(message => !!parseCodingAgentProgress(message.content || ""));
    for (let i = messages.length - 1; i >= 0; i--) {
        if (isCodingProgress[i]) {
            latestCodingIndex = i;
            break;
        }
    }
    if (latestCodingIndex < 0) return messages;
    return messages.filter((_message, index) => index === latestCodingIndex || !isCodingProgress[index]);
}

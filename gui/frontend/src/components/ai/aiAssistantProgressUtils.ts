import type { ChatMessage } from "./useAIAssistant";

const TOOL_PROGRESS_PREFIXES = [
    "⚙️", "🛠️", "🖥️", "🚀", "📎", "📄", "🔍", "📂",
    "✏️", "💾", "📸", "🔊", "📝", "📖", "🔗", "🌐", "🧠", "📦",
];

/** Find the latest tool-specific progress message (e.g. "⚙️ 正在执行 weather-query...") */
export function findLatestToolProgressText(progressMessages: ChatMessage[], sending: boolean): string {
    if (!sending || progressMessages.length === 0) return "";
    for (let i = progressMessages.length - 1; i >= 0; i--) {
        const msg = progressMessages[i];
        if (msg.role === "progress" && msg.content) {
            if (TOOL_PROGRESS_PREFIXES.some(p => msg.content!.startsWith(p))) {
                return msg.content;
            }
        }
    }
    return "";
}

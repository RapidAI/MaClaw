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
            if (isToolProgressMessage(msg)) {
                return msg.content;
            }
        }
    }
    return "";
}

export function isToolProgressMessage(msg: ChatMessage): boolean {
    const content = msg.content?.trimStart() || "";
    if (msg.role !== "progress") return false;
    const withoutPrefix = stripToolProgressPrefix(content).trim();
    return withoutPrefix !== content && isRunningToolStatus(withoutPrefix);
}

export function formatToolProgressStatus(text: string, lang: string): string {
    let cleaned = stripToolProgressPrefix(text.trim()).trim();
    cleaned = cleaned
        .replace(/\s*[（(](?:可继续输入|you can type ahead)[）)]\s*$/i, "")
        .replace(/\s*(?:\.\.\.|…)\s*$/, "")
        .trim();
    const runningAction = lang === "en" ? "Running" : "\u6b63\u5728\u6267\u884c";
    const startingAction = lang === "en" ? "Starting" : "\u6b63\u5728\u542f\u52a8";
    const skillMatch = cleaned.match(/(\u6b63\u5728\u6267\u884c|\u6b63\u5728\u542f\u52a8)\s*Skill[\u300c"]([^\u300d"]+)[\u300d"]?/);
    if (skillMatch?.[2]) return `${isStartingToolAction(skillMatch[1]) ? startingAction : runningAction} ${skillMatch[2].trim()}`;
    const toolPathMatch = cleaned.match(/^(\u6b63\u5728\u6267\u884c|\u6b63\u5728\u542f\u52a8|running|executing|starting|launching)\s+(?:Shell|Skill)\s*\/\s*([^/]+?)(?:\s*\/.*)?$/i);
    if (toolPathMatch?.[2]) return `${isStartingToolAction(toolPathMatch[1]) ? startingAction : runningAction} ${toolPathMatch[2].trim()}`;
    const englishSkillMatch = cleaned.match(/(running|executing|starting|launching)\s+Skill\s*["“]?([^"”]+)["”]?/i);
    if (englishSkillMatch?.[2]) return `${isStartingToolAction(englishSkillMatch[1]) ? "Starting" : "Running"} ${englishSkillMatch[2].trim()}`;
    return cleaned || (lang === "en" ? "Working" : "\u6b63\u5728\u6267\u884c");
}

function stripToolProgressPrefix(text: string): string {
    for (const prefix of TOOL_PROGRESS_PREFIXES) {
        if (text.startsWith(prefix)) return text.slice(prefix.length);
    }
    return text;
}

function isRunningToolStatus(text: string): boolean {
    return /^(?:\u6b63\u5728\u6267\u884c|\u6b63\u5728\u542f\u52a8)\s*(?:Skill[\u300c"]|(?:Shell|Skill)\s*\/)/.test(text)
        || /^(?:running|executing|starting|launching)\s+(?:Skill\b|(?:Shell|Skill)\s*\/)/i.test(text);
}

function isStartingToolAction(action: string): boolean {
    return /starting|launching|\u542f\u52a8/i.test(action);
}

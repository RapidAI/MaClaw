import { localizeText } from "../../i18n";
import { stripLeadingEmojiCluster } from "./aiAssistantProgressUtils";

/** Live activity shown on the reasoning-panel summary while a round is in flight. */
export type AssistantLiveActivityKind =
    | "thinking"
    | "preparing"
    | "syncing_context"
    | "analyzing"
    | "accessing_model"
    | "calling_tool"
    | "running_command"
    | "reading_file"
    | "writing_file"
    | "editing_file"
    | "listing_dir"
    | "searching_files"
    | "searching_content"
    | "searching_web"
    | "fetching_page"
    | "using_browser"
    | "running_skill"
    | "generating_pdf"
    | "accessing_memory"
    | "remote_exec"
    | "screenshot"
    | "tts"
    | "asr"
    | "opening"
    | "delegating";

const TOOL_KIND_BY_NAME: Record<string, AssistantLiveActivityKind> = {
    bash: "running_command",
    run_terminal: "running_command",
    shell: "running_command",
    powershell: "running_command",
    read_file: "reading_file",
    read_files: "reading_file",
    read_code: "reading_file",
    read: "reading_file",
    write_file: "writing_file",
    write: "writing_file",
    create_file: "writing_file",
    edit_file: "editing_file",
    edit_lines: "editing_file",
    edit: "editing_file",
    str_replace: "editing_file",
    apply_patch: "editing_file",
    list_directory: "listing_dir",
    list_dir: "listing_dir",
    search_files: "searching_files",
    search: "searching_files",
    glob: "searching_files",
    git_diff: "searching_content",
    ripgrep: "searching_content",
    grep_search: "searching_content",
    grep: "searching_content",
    web_search: "searching_web",
    web_fetch: "fetching_page",
    download_file: "fetching_page",
    open_page: "fetching_page",
    fetch_url: "fetching_page",
    fetch_page: "fetching_page",
    browse_page: "fetching_page",
    http_get: "fetching_page",
    browser: "using_browser",
    computer_click: "using_browser",
    computer_use: "using_browser",
    session_search: "searching_content",
    search_code: "searching_content",
    run_skill: "running_skill",
    manage_skill: "running_skill",
    generate_pdf: "generating_pdf",
    memory: "accessing_memory",
    ssh: "remote_exec",
    screenshot: "screenshot",
    tts: "tts",
    asr: "asr",
    open: "opening",
    delegate_task: "delegating",
};

const ACTION_PHRASE_KIND: Array<[RegExp, AssistantLiveActivityKind]> = [
    [/执行命令|命令进度|run command|command progress/i, "running_command"],
    [/读取文件|read file/i, "reading_file"],
    [/写入文件|write file/i, "writing_file"],
    [/编辑文件|edit file/i, "editing_file"],
    [/列出目录|list directory/i, "listing_dir"],
    [/搜索文件|search files/i, "searching_files"],
    [/检索内容|search content/i, "searching_content"],
    [/搜索网络|web search/i, "searching_web"],
    [/访问网页|提取网页|open page|fetch(?:ing)? pages?/i, "fetching_page"],
    [/操作浏览器|using browser|^browser$/i, "using_browser"],
    [/执行技能|技能进度|run skill|skill progress/i, "running_skill"],
    [/生成\s*pdf|generate pdf/i, "generating_pdf"],
    [/访问记忆|access memory/i, "accessing_memory"],
    [/远程执行|远程操作|连接服务器|remote exec|remote operation|connect server/i, "remote_exec"],
    [/截取屏幕|^screenshot$/i, "screenshot"],
    [/生成语音|generate speech/i, "tts"],
    [/语音转写|transcribe audio/i, "asr"],
    [/委派任务|delegate task/i, "delegating"],
    [/^打开$|^open$/i, "opening"],
    [/调用工具|call tool|script progress|脚本进度|处理中|^working$/i, "calling_tool"],
];

const IM_TOOL_STATUS_PREFIX = /^(?:工具\s*[·•]\s*|Tool\s*[·•]\s*)(.+)$/i;
const CODING_EVENT_PREFIX = "Coding Agent Event:";
const RUNNING_TOOLS_LINE = /^(?:\u6b63\u5728\u6267\u884c\u5de5\u5177|running tools?)/i;
const RUNNING_SKILL_OR_SHELL_LINE = /^(?:\u6b63\u5728\u6267\u884c|\u6b63\u5728\u542f\u52a8|running|executing|starting|launching)\s+(?:skill\b|(?:shell|skill)\s*\/)/i;

const MODEL_ACTIVITY_KINDS: ReadonlySet<AssistantLiveActivityKind> = new Set(["accessing_model"]);
const TOOL_ACTIVITY_KINDS: ReadonlySet<AssistantLiveActivityKind> = new Set([
    "calling_tool",
    "running_command",
    "reading_file",
    "writing_file",
    "editing_file",
    "listing_dir",
    "searching_files",
    "searching_content",
    "searching_web",
    "fetching_page",
    "using_browser",
    "running_skill",
    "generating_pdf",
    "accessing_memory",
    "remote_exec",
    "screenshot",
    "tts",
    "asr",
    "opening",
    "delegating",
]);

export function assistantLiveActivityLabel(kind: AssistantLiveActivityKind, lang: string): string {
    switch (kind) {
        case "thinking":
            return localizeText(lang, "Thinking", "正在思考", "正在思考");
        case "preparing":
            return localizeText(lang, "Preparing", "正在准备", "正在準備");
        case "syncing_context":
            return localizeText(lang, "Syncing context", "正在同步上下文", "正在同步上下文");
        case "analyzing":
            return localizeText(lang, "Analyzing the task", "正在分析任务", "正在分析任務");
        case "accessing_model":
            return localizeText(lang, "Contacting the model", "正在访问模型", "正在訪問模型");
        case "running_command":
            return localizeText(lang, "Running command", "正在执行命令", "正在執行命令");
        case "reading_file":
            return localizeText(lang, "Reading files", "正在读取文件", "正在讀取檔案");
        case "writing_file":
            return localizeText(lang, "Writing files", "正在写入文件", "正在寫入檔案");
        case "editing_file":
            return localizeText(lang, "Editing files", "正在编辑文件", "正在編輯檔案");
        case "listing_dir":
            return localizeText(lang, "Listing directory", "正在列出目录", "正在列出目錄");
        case "searching_files":
            return localizeText(lang, "Searching files", "正在搜索文件", "正在搜尋檔案");
        case "searching_content":
            return localizeText(lang, "Searching content", "正在检索内容", "正在檢索內容");
        case "searching_web":
            return localizeText(lang, "Searching the web", "正在搜索网络", "正在搜尋網路");
        case "fetching_page":
            return localizeText(lang, "Fetching pages", "正在提取网页", "正在擷取網頁");
        case "using_browser":
            return localizeText(lang, "Using browser", "正在操作浏览器", "正在操作瀏覽器");
        case "running_skill":
            return localizeText(lang, "Running skill", "正在执行技能", "正在執行技能");
        case "generating_pdf":
            return localizeText(lang, "Generating PDF", "正在生成 PDF", "正在產生 PDF");
        case "accessing_memory":
            return localizeText(lang, "Accessing memory", "正在访问记忆", "正在存取記憶");
        case "remote_exec":
            return localizeText(lang, "Remote operation", "正在远程操作", "正在遠端操作");
        case "screenshot":
            return localizeText(lang, "Capturing screen", "正在截取屏幕", "正在擷取螢幕");
        case "tts":
            return localizeText(lang, "Generating speech", "正在生成语音", "正在產生語音");
        case "asr":
            return localizeText(lang, "Transcribing audio", "正在语音转写", "正在語音轉寫");
        case "opening":
            return localizeText(lang, "Opening", "正在打开", "正在開啟");
        case "delegating":
            return localizeText(lang, "Delegating", "正在委派任务", "正在委派任務");
        case "calling_tool":
        default:
            return localizeText(lang, "Calling tools", "正在调用工具", "正在呼叫工具");
    }
}

/** Plain-text object after the live action, e.g. "MaClaw官方 auto 模型" or "ssh 工具". */
export function assistantLiveActivityObject(
    kind: AssistantLiveActivityKind | null | undefined,
    lang: string,
    ctx?: {
        providerName?: string;
        modelId?: string;
        isHubService?: boolean;
        toolName?: string;
    },
): string {
    if (!kind) return "";
    if (MODEL_ACTIVITY_KINDS.has(kind)) {
        return formatLiveModelObject(lang, ctx?.providerName, ctx?.modelId, ctx?.isHubService);
    }
    if (TOOL_ACTIVITY_KINDS.has(kind)) {
        const toolName = normalizeLiveToolName(ctx?.toolName);
        if (!toolName || isImpliedToolName(kind, toolName)) return "";
        return formatLiveToolObject(lang, toolName);
    }
    return "";
}

const IMPLIED_TOOL_NAMES: Partial<Record<AssistantLiveActivityKind, readonly string[]>> = {
    searching_web: ["web_search"],
    fetching_page: ["web_fetch"],
    reading_file: ["read_file", "read_files", "read_code", "read"],
    writing_file: ["write_file", "write", "create_file"],
    editing_file: ["edit_file", "edit_lines", "edit", "str_replace", "apply_patch"],
    listing_dir: ["list_directory", "list_dir"],
    generating_pdf: ["generate_pdf"],
    accessing_memory: ["memory"],
    screenshot: ["screenshot"],
    tts: ["tts"],
    asr: ["asr"],
};

function isImpliedToolName(kind: AssistantLiveActivityKind, toolName: string): boolean {
    return (IMPLIED_TOOL_NAMES[kind] || []).includes(toolName.toLowerCase());
}

export function formatLiveModelObject(lang: string, providerName?: string, modelId?: string, isHubService?: boolean): string {
    const provider = isHubService
        ? localizeText(lang, "MaClaw official", "MaClaw官方", "MaClaw官方")
        : String(providerName || "").trim();
    const model = String(modelId || "").trim();
    const name = [provider, model].filter(Boolean).join(" ");
    if (!name) return "";
    return localizeText(lang, `${name} model`, `${name} 模型`, `${name} 模型`);
}

export function formatLiveToolObject(lang: string, toolName?: string): string {
    const name = normalizeLiveToolName(toolName);
    if (!name) return "";
    return localizeText(lang, name, `${name} 工具`, `${name} 工具`);
}

export function extractInFlightToolName(opts: {
    codingProgress?: { event?: string; detail?: string } | null;
    progressMessages?: Array<{ content?: string }>;
    reasoningText?: string;
}): string {
    const codingEvent = (opts.codingProgress?.event || "").trim().toLowerCase();
    if (codingEvent === "tool_started") {
        const fromCoding = normalizeLiveToolName(opts.codingProgress?.detail || "");
        if (fromCoding) return fromCoding;
    }
    // Only the latest progress line is current. Walking older named tools
    // would keep showing web_fetch after the turn had already moved on.
    const latest = latestProgressText(opts.progressMessages);
    const fromProgress = extractToolNameFromProgressText(latest);
    if (fromProgress) return fromProgress;
    return extractToolNameFromProgressText(opts.reasoningText || "");
}

function normalizeLiveToolName(name?: string): string {
    const raw = String(name || "").trim();
    if (!raw) return "";
    const token = raw.match(/^[a-zA-Z][a-zA-Z0-9_-]*$/)?.[0] || "";
    return token;
}

function extractToolNameFromProgressText(text: string): string {
    const trimmed = (text || "").trim();
    if (!trimmed) return "";
    const coding = parseCodingToolEvent(trimmed);
    if ((coding?.event || "").trim().toLowerCase() === "tool_started") {
        return normalizeLiveToolName(coding?.detail || "");
    }
    const lines = trimmed.split(/\r?\n/);
    for (let i = lines.length - 1; i >= 0; i--) {
        const firstLine = stripLeadingEmojiCluster(lines[i] || "").trim()
            .replace(/^\[Status\]\s*/i, "")
            .replace(/^\u2022\s*/, "");
        if (!firstLine) continue;
        const isToolStatus = RUNNING_TOOLS_LINE.test(firstLine)
            || /正在调用工具|calling tools?/i.test(firstLine)
            || IM_TOOL_STATUS_PREFIX.test(firstLine);
        if (!isToolStatus) return "";
        const named = firstLine.match(/[:\uff1a]\s*([a-zA-Z][a-zA-Z0-9_-]*)/);
        if (named?.[1]) {
            const token = normalizeLiveToolName(named[1]);
            if (token) return token;
        }
        const prefixed = firstLine.match(IM_TOOL_STATUS_PREFIX);
        if (prefixed?.[1]) return normalizeLiveToolName(prefixed[1].trim());
        return "";
    }
    return "";
}

export function assistantLiveActivityFromToolName(name: string): AssistantLiveActivityKind {
    const raw = (name || "").trim().toLowerCase();
    if (!raw) return "calling_tool";
    const bare = raw.startsWith("ssh_") ? raw.slice(4) : raw;
    return TOOL_KIND_BY_NAME[bare] || TOOL_KIND_BY_NAME[raw] || "calling_tool";
}

export function resolveAssistantLiveActivity(opts: {
    streaming: boolean;
    busy: boolean;
    hasReasoning?: boolean;
    reasoningText?: string;
    progressMessages?: Array<{ content?: string }>;
    codingProgress?: { event?: string; detail?: string } | null;
}): AssistantLiveActivityKind | null {
    if (!opts.busy && !opts.streaming) return null;
    const codingEvent = (opts.codingProgress?.event || "").trim().toLowerCase();
    // An in-flight tool wins even if a few reasoning tokens are still flushing.
    if (codingEvent === "tool_started") {
        return assistantLiveActivityFromToolName(opts.codingProgress?.detail || "");
    }
    const latestText = latestProgressText(opts.progressMessages);
    if (isInFlightToolProgressText(latestText)) {
        return parseLiveActivityFromProgressText(latestText) || "calling_tool";
    }
    if (codingEvent === "tool_finished" && opts.streaming) return "thinking";
    const fromStatus = liveActivityFromReasoningStatus(opts.reasoningText)
        || liveActivityFromStatusText(latestText);
    const hasModelThought = opts.hasReasoning || reasoningHasModelThought(opts.reasoningText);
    // Status-only milestones (environment / context / analyze) stay on the
    // header until the model actually writes thought tokens.
    if (opts.streaming && hasModelThought) return "thinking";
    if (fromStatus) return fromStatus;
    if (opts.streaming) return "accessing_model";
    const fromProgress = latestText ? parseLiveActivityFromProgressText(latestText) : null;
    if (fromProgress) return fromProgress;
    // After the model has already produced a thought trail, a silent busy
    // gap is the tool round — show that step on the thinking header.
    // Before any model prose, the round is still contacting the model.
    return hasModelThought ? "calling_tool" : "accessing_model";
}

/** Thought text that should drive the live header: chat reasoning plus coding-timeline thoughts. */
export function assistantLiveReasoningSource(msg: {
    role?: string;
    content?: string;
    reasoning?: string;
    codingTimeline?: Array<{ kind?: string; content?: string }>;
} | undefined): string {
    if (!msg || msg.role !== "assistant" || (msg.content || "").trim()) return "";
    const timelineThoughts = (msg.codingTimeline || [])
        .filter((item) => item.kind === "thinking")
        .map((item) => String(item.content || "").trim())
        .filter(Boolean);
    return [msg.reasoning || "", ...timelineThoughts].filter(Boolean).join("\n");
}

export function reasoningHasModelThought(reasoning?: string): boolean {
    if (!reasoning?.trim()) return false;
    return reasoning.split(/\r?\n/).some((line) => {
        const text = line.trim();
        if (!text) return false;
        if (text.startsWith("\u2022 ") || text.startsWith("[Status]")) return false;
        return true;
    });
}

function liveActivityFromReasoningStatus(reasoning?: string): AssistantLiveActivityKind | null {
    if (!reasoning) return null;
    const lines = reasoning.split(/\r?\n/);
    for (let i = lines.length - 1; i >= 0; i--) {
        const text = lines[i].trim();
        if (!text) continue;
        if (text.startsWith("\u2022 ") || text.startsWith("[Status]")) {
            const kind = liveActivityFromStatusText(text);
            if (kind) return kind;
        }
    }
    return null;
}

function liveActivityFromStatusText(text: string): AssistantLiveActivityKind | null {
    const body = (text || "")
        .trim()
        .replace(/^\[Status\]\s*/i, "")
        .replace(/^\u2022\s*/, "")
        .trim();
    if (!body) return null;
    if (/正在执行工具|正在调用工具|running tools?/i.test(body)) return "calling_tool";
    if (/模型请求|等待响应|访问模型|准备模型请求|contacting the model|building the request/i.test(body)) return "accessing_model";
    if (/同步会话|同步.*上下文|conversation context/i.test(body)) return "syncing_context";
    if (/分析任务|analyzing the task/i.test(body)) return "analyzing";
    if (/准备执行|执行环境|已接收任务|task received|preparing the execution|environment is ready/i.test(body)) return "preparing";
    return null;
}

/** Last assistant owns the live header only while it is still the in-flight round. */
export function assistantMessageOwnsLiveActivity(
    msg: { role?: string; content?: string } | undefined,
    streaming: boolean,
    isNewestMessage = true,
): boolean {
    if (!msg || msg.role !== "assistant" || !isNewestMessage) return false;
    if (streaming) return true;
    return !(msg.content || "").trim();
}

/**
 * Coding timeline: the live sheen belongs on the last thought only when that
 * thought is also the last timeline item (still thinking). Tool/edit steps
 * after it get a trailing live header instead of relabeling an earlier thought.
 */
export function codingTimelineLiveThoughtIndex(
    timeline: Array<{ kind?: string }> | undefined,
    ownsLive: boolean,
): number {
    if (!ownsLive || !timeline?.length) return -1;
    const last = timeline.length - 1;
    return timeline[last]?.kind === "thinking" ? last : -1;
}

const GENERIC_CODING_LIVE_KINDS: ReadonlySet<AssistantLiveActivityKind> = new Set([
    "thinking",
    "preparing",
    "syncing_context",
    "analyzing",
    "accessing_model",
]);

/** Coding timeline + plan already show generic thinking; do not add a second bar. */
export function isGenericCodingLiveKind(kind?: AssistantLiveActivityKind | null): boolean {
    return !!kind && GENERIC_CODING_LIVE_KINDS.has(kind);
}

/** Live header that is not already attached to a coding timeline thought. */
export function resolveStandaloneLiveActivityLabel(opts: {
    liveLabel?: string;
    liveKind?: AssistantLiveActivityKind | null;
    coding: boolean;
    lastAssistantOwnsLive: boolean;
    lastMessageRole?: string;
    timelineOwnsLiveThought?: boolean;
}): string | undefined {
    if (!opts.liveLabel) return undefined;
    if (opts.coding) {
        // Timeline thoughts and the plan card already cover generic thinking.
        // Require an explicit tool kind so a missing kind cannot resurrect the
        // extra "正在思考" bar between the transcript and the plan.
        if (!opts.liveKind || isGenericCodingLiveKind(opts.liveKind) || opts.timelineOwnsLiveThought) return undefined;
        return opts.liveLabel;
    }
    if (!opts.lastAssistantOwnsLive && opts.lastMessageRole !== "assistant") return opts.liveLabel;
    return undefined;
}

function latestProgressText(messages?: Array<{ content?: string }>): string {
    if (!messages?.length) return "";
    for (let i = messages.length - 1; i >= 0; i--) {
        const text = (messages[i]?.content || "").trim();
        if (text) return text;
    }
    return "";
}

/** True for an in-progress execution line, not a lingering IM tool-status card. */
export function isInFlightToolProgressText(text: string): boolean {
    const trimmed = (text || "").trim();
    if (!trimmed) return false;
    const coding = parseCodingToolEvent(trimmed);
    if ((coding?.event || "").trim().toLowerCase() === "tool_started") return true;
    const firstLine = stripLeadingEmojiCluster((trimmed.split(/\r?\n/)[0] || "")).trim();
    if (RUNNING_TOOLS_LINE.test(firstLine)) return true;
    return RUNNING_SKILL_OR_SHELL_LINE.test(firstLine);
}

export function parseLiveActivityFromProgressText(text: string): AssistantLiveActivityKind | null {
    const trimmed = (text || "").trim();
    if (!trimmed) return null;
    const coding = parseCodingToolEvent(trimmed);
    if (coding) return liveActivityFromCodingEvent(coding.event, coding.detail);
    const firstLine = stripLeadingEmojiCluster((trimmed.split(/\r?\n/)[0] || "")).trim();
    const runningTools = parseRunningToolsLine(firstLine);
    if (runningTools) return runningTools;
    if (RUNNING_SKILL_OR_SHELL_LINE.test(firstLine)) {
        return /shell/i.test(firstLine) ? "running_command" : "running_skill";
    }
    const prefixed = firstLine.match(IM_TOOL_STATUS_PREFIX);
    if (!prefixed?.[1]) return null;
    return liveActivityFromActionPhrase(prefixed[1].trim());
}

function parseRunningToolsLine(firstLine: string): AssistantLiveActivityKind | null {
    if (!RUNNING_TOOLS_LINE.test(firstLine)) return null;
    const named = firstLine.match(/[:\uff1a]\s*([a-zA-Z][a-zA-Z0-9_-]*)/);
    return named?.[1] ? assistantLiveActivityFromToolName(named[1]) : "calling_tool";
}

function parseCodingToolEvent(text: string): { event: string; detail: string } | null {
    if (!text.startsWith(CODING_EVENT_PREFIX)) return null;
    try {
        const raw = JSON.parse(text.slice(CODING_EVENT_PREFIX.length).trim()) as Record<string, unknown>;
        if (raw?.agent !== "coding") return null;
        return {
            event: typeof raw.event === "string" ? raw.event : "",
            detail: typeof raw.detail === "string" ? raw.detail : "",
        };
    } catch {
        return null;
    }
}

function liveActivityFromCodingEvent(event: string, detail: string): AssistantLiveActivityKind | null {
    const normalized = (event || "").trim().toLowerCase();
    if (normalized === "tool_started") return assistantLiveActivityFromToolName(detail);
    if (normalized === "tool_finished") return "thinking";
    return null;
}

function liveActivityFromActionPhrase(phrase: string): AssistantLiveActivityKind | null {
    const text = phrase.replace(/\s+/g, " ").trim();
    if (!text) return null;
    for (const [pattern, kind] of ACTION_PHRASE_KIND) {
        if (pattern.test(text)) return kind;
    }
    return "calling_tool";
}

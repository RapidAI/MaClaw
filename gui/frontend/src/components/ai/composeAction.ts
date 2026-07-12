import type { AssistantInputIconName } from "./aiAssistantPanelTheme";

/** Compose-mode actions: free-form input is wrapped as a slash command on send. */
export type ComposeAction = "goal" | "btw";

/** One-shot slash commands fired immediately from the + menu (no input needed). */
export type FireSlashCommand = "/memory" | "/compress" | "/help";

/** Local UI actions from the + menu (not slash commands). */
export type PlusMenuActionId = "newConversation";

/** Default /loop template inserted into the input for the user to complete. */
export const LOOP_COMMAND_TEMPLATE = `/loop "go test ./..." `;

export type PlusMenuItemKind = "action" | "compose" | "template" | "fire";

export interface PlusMenuItemDef {
    id: string;
    kind: PlusMenuItemKind;
    /** Icon that matches the command meaning. */
    icon: AssistantInputIconName;
    /** Stable test id. */
    testId: string;
    labelZh: string;
    labelEn: string;
    hintZh?: string;
    hintEn?: string;
    /** For local UI actions (e.g. start new conversation). */
    actionId?: PlusMenuActionId;
    /** For compose items. */
    composeAction?: ComposeAction;
    /** For template items — text inserted into the input. */
    template?: string;
    /** For fire items — exact command string to send. */
    fireCommand?: FireSlashCommand;
    /** When true, item is disabled while the agent is busy (inputLocked). */
    disableWhenBusy?: boolean;
}

const COMPOSE_PREFIX: Record<ComposeAction, string> = {
    goal: "/goal",
    btw: "/btw",
};

/** Ordered + menu entries (actions first, then compose/template, then fire-and-forget). */
export const PLUS_MENU_ITEMS: PlusMenuItemDef[] = [
    {
        id: "newConversation",
        kind: "action",
        icon: "messagePlus",
        testId: "ai-plus-menu-new-conversation",
        labelZh: "新建任务会话",
        labelEn: "New session",
        hintZh: "清空当前会话，回到任务入口",
        hintEn: "Clear the current session and return to task entry",
        actionId: "newConversation",
        disableWhenBusy: true,
    },
    {
        id: "goal",
        kind: "compose",
        icon: "target",
        testId: "ai-plus-menu-goal",
        labelZh: "目标",
        labelEn: "Goal",
        hintZh: "/goal 持久化长时目标",
        hintEn: "/goal long-running objective",
        composeAction: "goal",
    },
    {
        id: "btw",
        kind: "compose",
        icon: "search",
        testId: "ai-plus-menu-btw",
        labelZh: "旁路查询",
        labelEn: "Side query",
        hintZh: "/btw 不打断主任务",
        hintEn: "/btw without interrupting",
        composeAction: "btw",
    },
    {
        id: "loop",
        kind: "template",
        icon: "repeat",
        testId: "ai-plus-menu-loop",
        labelZh: "验证循环",
        labelEn: "Verify loop",
        hintZh: "/loop 验证命令 + 目标",
        hintEn: "/loop verify cmd + goal",
        template: LOOP_COMMAND_TEMPLATE,
    },
    {
        id: "memory",
        kind: "fire",
        icon: "brain",
        testId: "ai-plus-menu-memory",
        labelZh: "记忆状态",
        labelEn: "Memory",
        hintZh: "/memory",
        hintEn: "/memory",
        fireCommand: "/memory",
    },
    {
        id: "compress",
        kind: "fire",
        icon: "compress",
        testId: "ai-plus-menu-compress",
        labelZh: "压缩对话历史",
        labelEn: "Compress chat history",
        hintZh: "/compress 压缩当前对话上下文",
        hintEn: "/compress summarize current chat context",
        fireCommand: "/compress",
    },
    {
        id: "help",
        kind: "fire",
        icon: "helpCircle",
        testId: "ai-plus-menu-help",
        labelZh: "帮助",
        labelEn: "Help",
        hintZh: "/help",
        hintEn: "/help",
        fireCommand: "/help",
    },
];

/** Pre-split groups so the menu does not re-filter on every render. */
export const PLUS_MENU_ACTION_ITEMS = PLUS_MENU_ITEMS.filter((item) => item.kind === "action");
export const PLUS_MENU_COMPOSE_TEMPLATE_ITEMS = PLUS_MENU_ITEMS.filter(
    (item) => item.kind === "compose" || item.kind === "template",
);
export const PLUS_MENU_FIRE_ITEMS = PLUS_MENU_ITEMS.filter((item) => item.kind === "fire");

const COMPOSE_ITEM_BY_ACTION = new Map(
    PLUS_MENU_ITEMS
        .filter((item): item is PlusMenuItemDef & { composeAction: ComposeAction } => item.kind === "compose" && !!item.composeAction)
        .map((item) => [item.composeAction, item]),
);

/** True when text is a `/btw` slash command (case-insensitive). */
export function isBtwCommandText(text: string): boolean {
    return /^\/btw(?:\s|$)/i.test(text.trim());
}

/** Strip the `/btw` prefix; returns "" for bare `/btw`. */
export function btwQueryFromText(text: string): string {
    const trimmed = text.trim();
    const match = trimmed.match(/^\/btw(?:\s+(.*))?$/i);
    if (!match) return trimmed;
    return (match[1] || "").trim();
}

function hasComposePrefix(text: string, prefix: string): boolean {
    const lower = text.toLowerCase();
    const prefixLower = prefix.toLowerCase();
    return lower === prefixLower || lower.startsWith(`${prefixLower} `);
}

/**
 * When the user is in a compose mode, free-form input becomes the argument of
 * the corresponding slash command.
 *
 * - empty input stays empty (caller should not send)
 * - text that already starts with the matching prefix is left unchanged
 * - otherwise the text is prefixed as `/cmd <body>`
 */
export function applyComposeActionToText(text: string, action: ComposeAction | null | undefined): string {
    const trimmed = text.trim();
    if (!trimmed || !action) return trimmed;
    const prefix = COMPOSE_PREFIX[action];
    if (!prefix) return trimmed;
    if (hasComposePrefix(trimmed, prefix)) return trimmed;
    return `${prefix} ${trimmed}`;
}

const COMPOSE_PLACEHOLDER: Record<ComposeAction, { zh: string; en: string }> = {
    goal: {
        zh: "描述你要持续推进的目标...",
        en: "Describe the long-running goal to pursue...",
    },
    btw: {
        zh: "输入旁路查询问题（不打断当前任务）...",
        en: "Ask a side question (won't interrupt the main task)...",
    },
};

export function getComposeActionPlaceholder(action: ComposeAction | null | undefined, isZh: boolean): string | null {
    if (!action) return null;
    const entry = COMPOSE_PLACEHOLDER[action];
    return entry ? (isZh ? entry.zh : entry.en) : null;
}

export function getComposeActionLabel(action: ComposeAction, isZh: boolean): string {
    const item = COMPOSE_ITEM_BY_ACTION.get(action);
    if (!item) return action;
    return isZh ? item.labelZh : item.labelEn;
}

export function getComposeActionIcon(action: ComposeAction): AssistantInputIconName {
    return COMPOSE_ITEM_BY_ACTION.get(action)?.icon ?? "target";
}

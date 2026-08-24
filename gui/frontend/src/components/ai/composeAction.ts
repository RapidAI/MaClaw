import type { AssistantInputIconName } from "./aiAssistantPanelTheme";
import {
    isInstallActionAllowed,
    isInstallCLIBinaryPrefix,
    isKnownInstallCommand,
    normalizeInstallCommand,
} from "./installCommandAllowlist";

/** Compose-mode actions: free-form input is prefixed with the selected command on send. */
export type ComposeAction = "goal" | "btw" | "moa" | "computer";

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
    moa: "/moa",
    computer: "@computer",
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
        id: "moa",
        kind: "compose",
        icon: "layers",
        testId: "ai-plus-menu-moa",
        labelZh: "多模型会诊",
        labelEn: "MoA council",
        hintZh: "/moa 多视角综合（单次）",
        hintEn: "/moa multi-model synthesis (one-shot)",
        composeAction: "moa",
    },
    {
        id: "computer",
        kind: "compose",
        icon: "monitor",
        testId: "ai-plus-menu-computer",
        labelZh: "桌面操控",
        labelEn: "Computer Use",
        hintZh: "让助手操作本机桌面",
        hintEn: "Let the assistant operate the local desktop",
        composeAction: "computer",
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

/**
 * Skill / MCP / Plugin install-or-search commands for the AI assistant panel.
 * Handled immediately by the backend (no agent loop) — same class as /help.
 *
 * Allowlist source of truth (shared with Go via go:embed):
 *   ./installCommandAllowlist.json
 *
 * Matches:
 *   /skill search|install|list|remove ...
 *   /mcp search|install|list|remove|add ...
 *   /plugin marketplace|add|remove|search|installed|list ...
 *   maclaw-tui skill|mcp|plugin ...
 */
/** Strip a leading ASCII or fullwidth slash from a single token. */
function peelInstallCommandSlash(tok: string): string {
    const t = tok.trim();
    if (t.startsWith("/") || t.startsWith("／")) return t.slice(1).trim();
    return t;
}

/**
 * Split like shell fields: whitespace outside quotes, quotes stripped.
 * Mirrors Go installCommandFields().
 */
export function installCommandFields(s: string): string[] {
    const fields: string[] = [];
    let cur = "";
    let quote: string | null = null;
    for (const ch of s) {
        if (quote) {
            if (ch === quote) {
                quote = null;
            } else {
                cur += ch;
            }
            continue;
        }
        if (ch === '"' || ch === "'") {
            quote = ch;
            continue;
        }
        if (ch === " " || ch === "\t" || ch === "\n" || ch === "\r") {
            if (cur) {
                fields.push(cur);
                cur = "";
            }
            continue;
        }
        cur += ch;
    }
    if (cur) fields.push(cur);
    return fields;
}

/**
 * Normalize install command text for send/classify parity with Go:
 * strip BOM, optional CLI binary (path) prefix, and leading / or ／.
 * Returns null when the text is not an install command.
 */
export function normalizeInstallCommandText(text: string): string | null {
    // Strip BOM (some IM / paste sources include U+FEFF).
    let trimmed = text.trim().replace(/^\uFEFF/, "").trim();
    if (!trimmed) return null;

    // Leading slash on the whole line (／skill … or /skill …).
    let hasLeadingSlash = false;
    if (trimmed.startsWith("/") || trimmed.startsWith("／")) {
        hasLeadingSlash = true;
        trimmed = trimmed.slice(1).trim();
    }

    let fields = installCommandFields(trimmed);
    if (fields.length === 0) return null;

    let hasBin = false;
    if (isInstallCLIBinaryPrefix(fields[0])) {
        hasBin = true;
        fields = fields.slice(1);
    }
    if (fields.length === 0) return null;
    // Free-form chat without slash or CLI binary prefix is never an install command.
    if (!hasLeadingSlash && !hasBin) return null;

    // After a binary prefix, still allow `maclaw-tui /skill list`.
    fields[0] = peelInstallCommandSlash(fields[0]);
    if (!fields[0]) return null;

    const cmdToken = fields[0];
    const cmd = normalizeInstallCommand(cmdToken);
    const args = fields.slice(1);

    if (args.length === 0) {
        if (!isKnownInstallCommand(cmdToken)) return null;
    } else if (!isInstallActionAllowed(cmd, args)) {
        return null;
    }

    // Canonical slash form for backend/history (drop binary prefix; aliases → canonical).
    // Re-quote args that contain whitespace so round-trip paste stays one token.
    const rendered = [cmd, ...args.map(renderInstallArg)].filter(Boolean);
    return `/${rendered.join(" ")}`;
}

function renderInstallArg(arg: string): string {
    if (!arg) return arg;
    // Match Go installCommandFields (no backslash escapes — quotes are delimiters only).
    if (!/[\s"']/.test(arg)) return arg;
    if (!arg.includes('"')) return `"${arg}"`;
    if (!arg.includes("'")) return `'${arg}'`;
    // Both quote types present: drop doubles and wrap (rare for install targets).
    return `"${arg.replace(/"/g, "")}"`;
}

export function isInstallCommandText(text: string): boolean {
    return normalizeInstallCommandText(text) !== null;
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
    return lower === prefixLower || (lower.startsWith(prefixLower) && /^\s/u.test(text.slice(prefix.length)));
}

function hasOtherAtMention(text: string): boolean {
    const match = /^@([\p{L}][\p{L}\p{N}_-]*)(?:\s|$)/u.exec(text);
    return !!match && match[1].toLowerCase() !== "computer";
}

/**
 * When the user is in a compose mode, free-form input becomes the argument of
 * the corresponding command or explicit Computer Use mention.
 *
 * - empty input stays empty (caller should not send)
 * - text that already starts with the matching prefix is left unchanged
 * - install/CLI commands and other leading-slash commands are never wrapped
 *   (avoids `/goal /skill list` when goal compose mode is active)
 * - other explicit @mentions are never overwritten by Computer Use mode
 * - otherwise the text is prefixed as `/cmd <body>` or `@computer <body>`
 */
export function applyComposeActionToText(text: string, action: ComposeAction | null | undefined): string {
    // Strip BOM so paste from IM clients matches backend splitInstallCommand.
    const trimmed = text.trim().replace(/^\uFEFF/, "").trim();
    if (!trimmed || !action) return trimmed;
    // Install / marketplace commands must stay intact under any compose mode.
    // Prefer the canonical slash form (aliases / fullwidth / CLI prefix peeled).
    const install = normalizeInstallCommandText(trimmed);
    if (install) return install;
    const prefix = COMPOSE_PREFIX[action];
    if (!prefix) return trimmed;
    if (hasComposePrefix(trimmed, prefix)) return trimmed;
    // Keep an explicit destination intact only in Computer Use mode. Other
    // compose modes still need to wrap their input (for example, `/goal
    // @teammate review this`) so this mode cannot silently bypass them.
    if (action === "computer" && hasOtherAtMention(trimmed)) return trimmed;
    // Do not wrap other slash commands (/help, /memory, mistyped /skill, …).
    // Fullwidth slash (／) is treated the same as ASCII "/" (Chinese IM keyboards).
    if (trimmed.startsWith("/") || trimmed.startsWith("／")) return trimmed;
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
    moa: {
        zh: "描述需要多模型会诊的问题或方案...",
        en: "Describe a hard problem for multi-model council review...",
    },
    computer: {
        zh: "描述需要在桌面上完成的操作...",
        en: "Describe the task to perform on the desktop...",
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

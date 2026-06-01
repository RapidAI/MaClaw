// Tool name constants to avoid repeated string arrays
export const TOOL_NAMES = ['claude', 'gemini', 'codex', 'opencode', 'codebuddy', 'cursor', 'iflow', 'kilo'] as const;
export const SKILL_TOOLS = ['claude', 'gemini', 'codex'] as const;
export const isToolTab = (tab: string): boolean => (TOOL_NAMES as readonly string[]).includes(tab);
export const isSkillTool = (tab: string): boolean => (SKILL_TOOLS as readonly string[]).includes(tab);

export const TOOL_LABELS: Record<string, string> = {
    claude: 'Claude Code',
    gemini: 'Gemini CLI',
    codex: 'OpenAI Codex',
    opencode: 'OpenCode AI',
    codebuddy: 'CodeBuddy',
    cursor: 'Cursor Agent',
    iflow: 'iFlow CLI',
    kilo: 'Kilo Code',
};

export const getToolLabel = (tool: string): string => TOOL_LABELS[tool] || tool;

export const getVisibleToolOptions = (config: any): { id: string; name: string }[] => (
    TOOL_NAMES
        .filter((id) => {
            const key = `show_${id}`;
            return config?.[key] !== false;
        })
        .map((id) => ({ id, name: getToolLabel(id) }))
);

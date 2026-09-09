// Tool name constants to avoid repeated string arrays
export const TOOL_NAMES = ['claude', 'codex', 'opencode', 'codebuddy', 'iflow', 'kilo'] as const;
export const SKILL_TOOLS = ['claude', 'codex'] as const;
export const isToolTab = (tab: string): boolean => (TOOL_NAMES as readonly string[]).includes(tab);
export const isSkillTool = (tab: string): boolean => (SKILL_TOOLS as readonly string[]).includes(tab);
export const DEFAULT_TOOL = 'claude';
export const normalizeToolTab = (tab?: string | null): typeof TOOL_NAMES[number] => {
    const normalized = (tab || '').trim().toLowerCase();
    return isToolTab(normalized) ? normalized as typeof TOOL_NAMES[number] : DEFAULT_TOOL;
};

export const TOOL_LABELS: Record<string, string> = {
    claude: 'Claude Code',
    codex: 'OpenAI Codex',
    opencode: 'OpenCode',
    codebuddy: 'CodeBuddy',
    iflow: 'iFlow CLI',
    kilo: 'Kilo Code',
};

export const getToolLabel = (tool: string): string => TOOL_LABELS[tool] || tool;

export const getAllToolOptions = (): { id: string; name: string }[] => (
    TOOL_NAMES.map((id) => ({ id, name: getToolLabel(id) }))
);

export const getVisibleToolOptions = (config: any): { id: string; name: string }[] => {
    const allTools = getAllToolOptions();
    const visibleTools = allTools.filter((tool) => {
        const key = `show_${tool.id}`;
        return config?.[key] !== false;
    });

    return visibleTools.length > 1 ? visibleTools : allTools;
};

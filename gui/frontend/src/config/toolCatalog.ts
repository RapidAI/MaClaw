// Tool name constants to avoid repeated string arrays
export const TOOL_NAMES = ['claude', 'gemini', 'codex', 'opencode', 'codebuddy', 'cursor', 'iflow', 'kilo'] as const;
export const SKILL_TOOLS = ['claude', 'gemini', 'codex'] as const;
export const isToolTab = (tab: string): boolean => (TOOL_NAMES as readonly string[]).includes(tab);
export const isSkillTool = (tab: string): boolean => (SKILL_TOOLS as readonly string[]).includes(tab);

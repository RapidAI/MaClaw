import claudecodeIcon from '../../assets/images/claudecode.png';
import codebuddyIcon from '../../assets/images/Codebuddy.png';
import codexIcon from '../../assets/images/Codex.png';
import iflowIcon from '../../assets/images/iflow.png';
import opencodeIcon from '../../assets/images/opencode.png';
import kiloIcon from '../../assets/images/KiloCode.png';
import { getToolLabel, getVisibleToolOptions, normalizeToolTab } from '../../config/toolCatalog';

type SidebarToolSelectorProps = {
    activeTool: string;
    toolDropdownOpen: boolean;
    setToolDropdownOpen: (updater: (prev: boolean) => boolean) => void;
    config: any;
    switchTool: (tool: string) => void;
    visible?: boolean;
};

const toolIcons: Record<string, string> = {
    claude: claudecodeIcon,
    codex: codexIcon,
    opencode: opencodeIcon,
    codebuddy: codebuddyIcon,
    iflow: iflowIcon,
    kilo: kiloIcon,
};

const sidebarToolSelectorLabels = ['Claude Code', 'CodeBuddy', 'Kilo Code'];

/** Premium decorative divider shown when coding tool entry is hidden. */
function PremiumDivider() {
    return (
        <div style={{ flexShrink: 0, padding: '12px 16px' }}>
            <div style={{
                height: '3px',
                borderRadius: '1.5px',
                background: 'linear-gradient(90deg, transparent 0%, color-mix(in srgb, var(--theme-primary) 15%, transparent) 10%, color-mix(in srgb, var(--theme-primary) 40%, var(--theme-border)) 30%, var(--theme-primary) 50%, color-mix(in srgb, var(--theme-primary) 40%, var(--theme-border)) 70%, color-mix(in srgb, var(--theme-primary) 15%, transparent) 90%, transparent 100%)',
                boxShadow: '0 1px 3px color-mix(in srgb, var(--theme-primary) 20%, transparent), inset 0 0.5px 0 rgba(255,255,255,0.15)',
                opacity: 0.7,
            }} />
        </div>
    );
}

export const SidebarToolSelector = ({
    activeTool,
    toolDropdownOpen,
    setToolDropdownOpen,
    config,
    switchTool,
    visible = true,
}: SidebarToolSelectorProps) => {
    if (!visible) {
        return <PremiumDivider />;
    }

    const safeActiveTool = normalizeToolTab(activeTool);
    const visibleTools = getVisibleToolOptions(config);
    const tools = visibleTools.some((tool) => tool.id === safeActiveTool)
        ? visibleTools
        : [{ id: safeActiveTool, name: getToolLabel(safeActiveTool) }, ...visibleTools];
    const activeToolIcon = toolIcons[safeActiveTool];

    return (
        <div style={{ flexShrink: 0, borderBottom: '1px solid var(--theme-border)' }}>
            <button
                type="button"
                aria-expanded={toolDropdownOpen}
                onClick={() => setToolDropdownOpen(prev => !prev)}
                style={{ display: 'flex', alignItems: 'center', width: '100%', height: '58px', padding: '0 18px', gap: '12px', cursor: 'pointer', border: 0, background: 'transparent', color: 'inherit', textAlign: 'left' }}
            >
                {activeToolIcon
                    ? <img src={activeToolIcon} style={{ width: '18px', height: '18px', flexShrink: 0 }} alt="" />
                    : <span style={{ width: '8px', height: '8px', borderRadius: '50%', background: 'var(--theme-primary)', flexShrink: 0 }} />}
                <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', fontSize: '0.82rem', fontWeight: 700, color: 'var(--theme-text-primary)', flex: 1 }}>{getToolLabel(safeActiveTool)}</span>
                <span style={{ fontSize: '0.72rem', opacity: 0.55, flexShrink: 0 }}>{toolDropdownOpen ? '\u25B4' : '\u25BE'}</span>
            </button>
            {toolDropdownOpen && (
                <div role="group" aria-label="Coding tools" data-ui-guard-labels={sidebarToolSelectorLabels.join(', ')} style={{ padding: '0 8px 8px' }}>
                    {tools.map(tool => (
                        <button
                            type="button"
                            aria-current={safeActiveTool === tool.id ? 'true' : undefined}
                            key={tool.id}
                            onClick={() => switchTool(tool.id)}
                            style={{ display: 'flex', alignItems: 'center', width: '100%', gap: '8px', padding: '7px 10px', borderRadius: '6px', cursor: 'pointer', border: 0, fontSize: '0.82rem', color: 'var(--theme-text-primary)', background: safeActiveTool === tool.id ? 'color-mix(in srgb, var(--theme-primary) 16%, transparent)' : 'transparent', fontWeight: safeActiveTool === tool.id ? 700 : 500, textAlign: 'left' }}
                        >
                            {toolIcons[tool.id] && <img src={toolIcons[tool.id]} style={{ width: '16px', height: '16px' }} alt="" />}
                            <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', flex: 1 }}>{tool.name}</span>
                            {safeActiveTool === tool.id && <span style={{ fontSize: '0.7rem', opacity: 0.65 }}>{'\u2713'}</span>}
                        </button>
                    ))}
                </div>
            )}
        </div>
    );
};

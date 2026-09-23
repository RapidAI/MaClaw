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
        <div className="sts-divider-wrap">
            <div className="sts-divider" />
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
        <div className="sts-root">
            <button
                type="button"
                aria-expanded={toolDropdownOpen}
                onClick={() => setToolDropdownOpen(prev => !prev)}
                style={{ display: 'flex', alignItems: 'center', width: '100%', height: '58px', padding: '0 18px', gap: '12px', cursor: 'pointer', border: 0, background: 'transparent', color: 'inherit', textAlign: 'left' }}
            >
                {activeToolIcon
                    ? <img src={activeToolIcon} className="sts-tool-icon" alt="" />
                    : <span className="sts-tool-dot" />}
                <span className="sts-tool-name">{getToolLabel(safeActiveTool)}</span>
                <span className="sts-tool-caret">{toolDropdownOpen ? '\u25B4' : '\u25BE'}</span>
            </button>
            {toolDropdownOpen && (
                <div role="group" aria-label="Coding tools" data-ui-guard-labels={sidebarToolSelectorLabels.join(', ')} className="sts-menu">
                    {tools.map(tool => (
                        <button
                            type="button"
                            aria-current={safeActiveTool === tool.id ? 'true' : undefined}
                            key={tool.id}
                            onClick={() => switchTool(tool.id)}
                            style={{ display: 'flex', alignItems: 'center', width: '100%', gap: '8px', padding: '7px 10px', borderRadius: '6px', cursor: 'pointer', border: 0, fontSize: '0.82rem', color: 'var(--theme-text-primary)', background: safeActiveTool === tool.id ? 'color-mix(in srgb, var(--theme-primary) 16%, transparent)' : 'transparent', fontWeight: safeActiveTool === tool.id ? 700 : 500, textAlign: 'left' }}
                        >
                            {toolIcons[tool.id] && <img src={toolIcons[tool.id]} className="sts-menu-icon" alt="" />}
                            <span className="sts-menu-name">{tool.name}</span>
                            {safeActiveTool === tool.id && <span className="sts-menu-check">OK</span>}
                        </button>
                    ))}
                </div>
            )}
        </div>
    );
};

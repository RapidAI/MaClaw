import claudecodeIcon from '../../assets/images/claudecode.png';
import codebuddyIcon from '../../assets/images/Codebuddy.png';
import codexIcon from '../../assets/images/Codex.png';
import geminiIcon from '../../assets/images/gemincli.png';
import iflowIcon from '../../assets/images/iflow.png';
import opencodeIcon from '../../assets/images/opencode.png';
import kiloIcon from '../../assets/images/KiloCode.png';

type SidebarToolSelectorProps = {
    activeTool: string;
    toolDropdownOpen: boolean;
    setToolDropdownOpen: (updater: (prev: boolean) => boolean) => void;
    config: any;
    switchTool: (tool: string) => void;
};

export const SidebarToolSelector = ({
    activeTool,
    toolDropdownOpen,
    setToolDropdownOpen,
    config,
    switchTool,
}: SidebarToolSelectorProps) => (
    <div style={{ flexShrink: 0, borderBottom: '1px solid var(--theme-border)' }}>
        <div onClick={() => setToolDropdownOpen(prev => !prev)} style={{ display: 'flex', alignItems: 'center', height: '58px', padding: '0 18px', gap: '12px', cursor: 'pointer' }}>
            <span style={{ color: '#f97316', fontSize: '1rem', lineHeight: 1 }}>✺</span>
            <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', fontSize: '0.82rem', fontWeight: 700, color: 'var(--theme-text-primary)', flex: 1 }}>{activeTool === 'claude' ? 'Claude Code' : activeTool === 'gemini' ? 'Gemini CLI' : activeTool === 'codex' ? 'CodeX' : activeTool === 'opencode' ? 'OpenCode' : activeTool === 'codebuddy' ? 'CodeBuddy' : activeTool === 'cursor' ? 'Cursor Agent' : activeTool === 'iflow' ? 'iFlow CLI' : activeTool === 'kilo' ? 'Kilo Code' : activeTool}</span>
            <span style={{ fontSize: '0.72rem', opacity: 0.55, flexShrink: 0 }}>{toolDropdownOpen ? '▲' : '▼'}</span>
        </div>
        {toolDropdownOpen && (
            <div style={{ padding: '0 8px 8px' }}>
                {([{ id: 'claude', name: 'Claude Code', icon: claudecodeIcon }, ...(config?.show_gemini !== false ? [{ id: 'gemini', name: 'Gemini CLI', icon: geminiIcon }] : []), ...(config?.show_codex !== false ? [{ id: 'codex', name: 'CodeX', icon: codexIcon }] : []), ...(config?.show_opencode !== false ? [{ id: 'opencode', name: 'OpenCode', icon: opencodeIcon }] : []), ...(config?.show_codebuddy !== false ? [{ id: 'codebuddy', name: 'CodeBuddy', icon: codebuddyIcon }] : []), ...(config?.show_iflow !== false ? [{ id: 'iflow', name: 'iFlow CLI', icon: iflowIcon }] : []), ...(config?.show_kilo !== false ? [{ id: 'kilo', name: 'Kilo Code', icon: kiloIcon }] : [])] as { id: string; name: string; icon: string }[]).map(tool => (
                    <div key={tool.id} onClick={() => switchTool(tool.id)} style={{ display: 'flex', alignItems: 'center', gap: '8px', padding: '7px 10px', borderRadius: '6px', cursor: 'pointer', fontSize: '0.82rem', color: 'var(--theme-text-primary)', background: activeTool === tool.id ? 'color-mix(in srgb, var(--theme-primary) 16%, transparent)' : 'transparent', fontWeight: activeTool === tool.id ? 700 : 500 }}>
                        <img src={tool.icon} style={{ width: '16px', height: '16px' }} alt="" />
                        <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', flex: 1 }}>{tool.name}</span>
                        {activeTool === tool.id && <span style={{ fontSize: '0.7rem', opacity: 0.65 }}>✓</span>}
                    </div>
                ))}
            </div>
        )}
    </div>
);

// Icons for the sidebar collapse cluster shown at the leading edge of the
// window-chrome headers (task pane + nav rail toggles). Mirrored via
// scaleX(-1) when collapsed so the filled side points at the reveal edge,
// matching the WorkBuddy/VS Code panel-toggle affordance.

interface SidebarToggleIconProps {
    collapsed?: boolean;
}

const mirrorStyle = (collapsed?: boolean): React.CSSProperties | undefined => (
    collapsed ? { transform: 'scaleX(-1)' } : undefined
);

/** Panel-left glyph for the task pane (任务列表) toggle. */
export const TaskPaneToggleIcon = ({ collapsed }: SidebarToggleIconProps) => (
    <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true" focusable="false" style={mirrorStyle(collapsed)}>
        <rect x="3" y="4.5" width="18" height="15" rx="2.5" fill="none" stroke="currentColor" strokeWidth="1.8" />
        <path d="M9.4 4.5v15" fill="none" stroke="currentColor" strokeWidth="1.8" />
        <rect x="4.9" y="6.4" width="2.8" height="11.2" rx="1" fill="currentColor" opacity="0.55" />
    </svg>
);

/** Narrow rail glyph for the nav rail (功能栏) toggle. */
export const NavRailToggleIcon = ({ collapsed }: SidebarToggleIconProps) => (
    <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true" focusable="false" style={mirrorStyle(collapsed)}>
        <rect x="3" y="4.5" width="18" height="15" rx="2.5" fill="none" stroke="currentColor" strokeWidth="1.8" />
        <path d="M8 4.5v15" fill="none" stroke="currentColor" strokeWidth="1.8" />
        <circle cx="5.5" cy="8.4" r="1.1" fill="currentColor" opacity="0.55" />
        <circle cx="5.5" cy="12" r="1.1" fill="currentColor" opacity="0.55" />
        <circle cx="5.5" cy="15.6" r="1.1" fill="currentColor" opacity="0.55" />
    </svg>
);

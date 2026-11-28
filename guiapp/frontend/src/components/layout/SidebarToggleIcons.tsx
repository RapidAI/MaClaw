// Icons for the sidebar collapse cluster shown at the right end of the
// task-pane search row (and in the window-chrome headers while collapsed).
// Concrete glyphs on purpose: a checklist for the 任务列表 pane and a function
// grid for the 功能栏 nav rail, so the two toggles tell themselves apart at
// 16px. Expanded/collapsed state is carried by the aria-pressed styling
// (theme color vs muted gray), not by icon geometry.

/** Checklist glyph for the task pane (任务列表) toggle. */
export const TaskPaneToggleIcon = () => (
    <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false">
        <path d="m3.4 5.9 2 2 3.3-3.5" />
        <path d="m3.4 11.9 2 2 3.3-3.5" />
        <path d="m3.4 17.9 2 2 3.3-3.5" />
        <path d="M12.4 6.2h8.2" />
        <path d="M12.4 12.2h8.2" />
        <path d="M12.4 18.2h8.2" />
    </svg>
);

/** Function-grid glyph for the nav rail (功能栏) toggle. */
export const NavRailToggleIcon = () => (
    <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinejoin="round" aria-hidden="true" focusable="false">
        <rect x="3.4" y="3.4" width="7.4" height="7.4" rx="1.8" />
        <rect x="13.2" y="3.4" width="7.4" height="7.4" rx="1.8" />
        <rect x="3.4" y="13.2" width="7.4" height="7.4" rx="1.8" />
        <rect x="13.2" y="13.2" width="7.4" height="7.4" rx="1.8" />
    </svg>
);

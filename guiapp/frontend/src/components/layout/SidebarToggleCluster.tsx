import { localizeText } from '../../i18n';
import { NavRailToggleIcon, TaskPaneToggleIcon } from './SidebarToggleIcons';

export interface SidebarTogglesState {
    /** 任务列表 currently collapsed. */
    paneCollapsed: boolean;
    /** 功能栏 (nav rail) currently collapsed. */
    railCollapsed: boolean;
    onTogglePane: () => void;
    onToggleRail: () => void;
}

interface SidebarToggleClusterProps {
    /** The task pane only exists on the AI workbench tab; other tabs toggle the rail only. */
    showPaneToggle?: boolean;
    paneCollapsed?: boolean;
    railCollapsed?: boolean;
    onTogglePane?: () => void;
    onToggleRail?: () => void;
    lang?: string;
}

/**
 * The collapse/expand button pair pinned to the leading edge of every window
 * chrome header (assistant title bar, task execution header, main top
 * header). Lives OUTSIDE the collapsible columns so the restore affordance
 * survives both columns being hidden — with both collapsed it sits at the
 * window's top-left corner, like WorkBuddy's 收起侧边栏 button.
 *
 * The headers it renders into are window-drag regions: plain <button>s are
 * already excluded from drag arming (WINDOW_DRAG_EXCLUDE_SELECTOR), but
 * double-click must not bubble or the header toggles maximize.
 */
export function SidebarToggleCluster({ showPaneToggle = true, paneCollapsed, railCollapsed, onTogglePane, onToggleRail, lang }: SidebarToggleClusterProps) {
    const paneLabel = localizeText(lang, paneCollapsed ? 'Show task list' : 'Collapse task list', paneCollapsed ? '展开任务列表' : '收起任务列表', paneCollapsed ? '展開任務列表' : '收起任務列表');
    const railLabel = localizeText(lang, railCollapsed ? 'Show function bar' : 'Collapse function bar', railCollapsed ? '展开功能栏' : '收起功能栏', railCollapsed ? '展開功能欄' : '收起功能欄');
    const keepHeaderBehavior = (event: React.MouseEvent) => {
        // Keep the header's double-click-to-maximize from firing on the buttons.
        event.stopPropagation();
    };
    return (
        <div className="sb-toggle-cluster" data-testid="sidebar-toggle-cluster" role="group" aria-label={localizeText(lang, 'Sidebar display', '侧栏显示', '側欄顯示')} data-window-no-drag>
            {showPaneToggle && (
                <button
                    type="button"
                    className="sb-toggle-btn"
                    data-testid="task-pane-toggle"
                    aria-pressed={!paneCollapsed}
                    aria-label={paneLabel}
                    title={`${paneLabel} (Ctrl+B)`}
                    onMouseDown={keepHeaderBehavior}
                    onDoubleClick={keepHeaderBehavior}
                    onClick={(event) => { event.stopPropagation(); onTogglePane?.(); }}
                >
                    <TaskPaneToggleIcon collapsed={paneCollapsed} />
                </button>
            )}
            <button
                type="button"
                className="sb-toggle-btn"
                data-testid="nav-rail-toggle"
                aria-pressed={!railCollapsed}
                aria-label={railLabel}
                title={`${railLabel} (Ctrl+Shift+B)`}
                onMouseDown={keepHeaderBehavior}
                onDoubleClick={keepHeaderBehavior}
                onClick={(event) => { event.stopPropagation(); onToggleRail?.(); }}
            >
                <NavRailToggleIcon collapsed={railCollapsed} />
            </button>
        </div>
    );
}

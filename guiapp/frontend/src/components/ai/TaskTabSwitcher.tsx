import { useEffect, useRef, useState } from "react";
import type { AITab } from "./AITabTypes";
import { getAITabDisplayTitle } from "./AITabItem";
import { normalizeProjectSessionPath } from "./aiAssistantPanelSessionUtils";
import { cloudWorkspaceIdFromPath, cloudWorkspaceIdFromTaskFields, visibleTaskRows } from "./codingTaskMode";
import { sanitizeProjectTabTitle } from "./useAITabManager";
import type { TaskManagementItem } from "../layout/SidebarTaskManagement";

type TaskTabSwitcherProps = {
    tabs: AITab[];
    activeTabId: string;
    lang: string;
    onActivate: (tabId: string) => void;
    onClose: (tabId: string) => void;
    /** Sidebar-visible task rows; when provided the menu mirrors the task list instead of only open tabs. */
    tasks?: TaskManagementItem[];
    onOpenTask?: (projectPath: string, task?: TaskManagementItem) => void;
};

const label = (lang: string, en: string, zh: string, hant = zh) =>
    lang.startsWith("en") ? en : lang.startsWith("zh-Hant") ? hant : zh;

/** Compact per-type glyph; color inherits from the surrounding row. */
function TabTypeIcon({ tab }: { tab: AITab }) {
    if (tab.type === "expert" && tab.expertIcon) {
        return <span className="mc-task-tab-switcher__icon" aria-hidden="true">{tab.expertIcon}</span>;
    }
    const path = tab.type === "local" ? (
        // Sparkle — local AI assistant main session
        <path d="M8 1l1.5 4.5L14 7l-4.5 1.5L8 13l-1.5-4.5L2 7l4.5-1.5L8 1z" fill="currentColor" />
    ) : tab.type === "ve" || tab.type === "group" ? (
        // Chat bubble — digital employee / group conversation
        <>
            <rect x="2" y="3" width="12" height="8" rx="2" stroke="currentColor" strokeWidth="1.3" />
            <path d="M5.5 11.5 5 14l3-2" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" strokeLinejoin="round" fill="none" />
        </>
    ) : tab.type === "expert" ? (
        // Person — expert session
        <>
            <circle cx="8" cy="5.5" r="2.5" stroke="currentColor" strokeWidth="1.3" />
            <path d="M3.5 13.5c.8-2.4 2.5-3.6 4.5-3.6s3.7 1.2 4.5 3.6" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" fill="none" />
        </>
    ) : tab.cloudWorkspaceId ? (
        // Cloud — cloud workspace task
        <path d="M4.5 12a2.5 2.5 0 1 1 .4-4.95A3.5 3.5 0 0 1 11.6 8.2 2.3 2.3 0 0 1 11 12.7H4.5Z" stroke="currentColor" strokeWidth="1.2" strokeLinejoin="round" fill="none" />
    ) : tab.agentMode === "remote_coding_dev" ? (
        // Terminal — remote coding environment
        <>
            <rect x="2.5" y="3.5" width="11" height="9" rx="1.5" stroke="currentColor" strokeWidth="1.3" />
            <path d="M5 7h3M5 9.5h5" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" />
        </>
    ) : tab.agentMode === "coding_dev" ? (
        // Code brackets — local coding environment
        <>
            <path d="M5.5 4.5 2.5 8l3 3.5" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" strokeLinejoin="round" />
            <path d="m10.5 4.5 3 3.5-3 3.5" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" strokeLinejoin="round" />
        </>
    ) : (
        // Document — ordinary task session
        <>
            <rect x="3" y="2" width="10" height="12" rx="1.5" stroke="currentColor" strokeWidth="1.3" />
            <line x1="5.5" y1="5.5" x2="10.5" y2="5.5" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" />
            <line x1="5.5" y1="8" x2="10.5" y2="8" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" />
        </>
    );
    return (
        <svg className="mc-task-tab-switcher__icon" width="12" height="12" viewBox="0 0 16 16" fill="none" aria-hidden="true">{path}</svg>
    );
}

type SwitcherRow = {
    key: string;
    testId: string;
    title: string;
    iconTab: AITab;
    active: boolean;
    closeTabId?: string;
    /** Task-list row without an open tab; selecting it opens the task. */
    unopened?: boolean;
    onSelect: () => void;
};

/**
 * Compact task switcher for the task execution header. With only `tabs` it
 * lists every assistant tab; when the sidebar task list is passed via `tasks`
 * it mirrors that list instead — opened tasks switch, unopened ones open via
 * `onOpenTask`, and leftover open tabs no task claims stay reachable at the
 * end. The list renders only while open so task titles never duplicate header
 * text in the DOM.
 */
export function TaskTabSwitcher({ tabs, activeTabId, lang, onActivate, onClose, tasks, onOpenTask }: TaskTabSwitcherProps) {
    const [open, setOpen] = useState(false);
    const rootRef = useRef<HTMLDetailsElement>(null);

    useEffect(() => {
        if (!open) return;
        const handler = (event: globalThis.MouseEvent) => {
            if (rootRef.current && !rootRef.current.contains(event.target as Node)) setOpen(false);
        };
        document.addEventListener("mousedown", handler);
        return () => document.removeEventListener("mousedown", handler);
    }, [open]);

    const tabRow = (tab: AITab): SwitcherRow => ({
        key: tab.id,
        testId: `task-tab-switcher-item-${tab.id}`,
        title: getAITabDisplayTitle(tab, lang),
        iconTab: tab,
        active: tab.id === activeTabId,
        closeTabId: tab.closable ? tab.id : undefined,
        onSelect: () => onActivate(tab.id),
    });

    let rows: SwitcherRow[];
    if (!tasks) {
        rows = tabs.map(tabRow);
    } else {
        rows = [];
        const matchedTabIds = new Set<string>();
        const localTab = tabs.find(tab => tab.type === "local");
        if (localTab) rows.push(tabRow(localTab));
        visibleTaskRows(tasks).forEach((task, index) => {
            const taskPath = normalizeProjectSessionPath(task.project_path || "");
            const wsId = cloudWorkspaceIdFromTaskFields(task);
            const tab = tabs.find(candidate => candidate.type !== "local" && !matchedTabIds.has(candidate.id) && (
                (!!wsId && (String(candidate.cloudWorkspaceId || "").trim() || cloudWorkspaceIdFromPath(candidate.projectPath)) === wsId)
                || (!!taskPath && normalizeProjectSessionPath(candidate.projectPath || "") === taskPath)
            )) || null;
            if (tab) matchedTabIds.add(tab.id);
            const title = sanitizeProjectTabTitle(String(task.name || "").trim(), task.project_path);
            const key = String(task.id || task.project_path || index);
            if (tab) {
                rows.push({ ...tabRow(tab), title });
            } else {
                rows.push({
                    key: `task-${key}`,
                    testId: `task-tab-switcher-task-${key}`,
                    title,
                    iconTab: { id: `task-${key}`, type: "project", title, projectPath: task.project_path, cloudWorkspaceId: wsId || undefined, closable: false },
                    active: false,
                    unopened: true,
                    onSelect: () => onOpenTask?.(task.project_path, task),
                });
            }
        });
        for (const tab of tabs) {
            if (tab.type === "local" || matchedTabIds.has(tab.id)) continue;
            rows.push(tabRow(tab));
        }
    }

    return (
        <details
            ref={rootRef}
            className="mc-task-tab-switcher"
            open={open}
            onKeyDown={event => {
                if (event.key !== "Escape" || !open) return;
                event.stopPropagation();
                setOpen(false);
                // Return focus to the trigger so keyboard users keep their place.
                rootRef.current?.querySelector("summary")?.focus();
            }}
        >
            <summary
                className="task-tab-switcher-btn"
                data-testid="task-tab-switcher-btn"
                aria-expanded={open}
                aria-haspopup="menu"
                aria-label={label(lang, "Switch task", "切换任务", "切換任務")}
                title={label(lang, "Switch task", "切换任务", "切換任務")}
                onClick={event => { event.preventDefault(); setOpen(current => !current); }}
            >
                {label(lang, "Tasks", "任务", "任務")} ({rows.length})
            </summary>
            {open && (
                <div className="mc-task-tab-switcher__popover" role="menu" aria-label={label(lang, "Open tasks", "打开的任务", "開啟的任務")}>
                    {rows.map(row => (
                        <div
                            key={row.key}
                            role="menuitem"
                            tabIndex={0}
                            className={`mc-task-tab-switcher__item${row.unopened ? " mc-task-tab-switcher__item--unopened" : ""}`}
                            data-testid={row.testId}
                            data-active={row.active || undefined}
                            onClick={() => { setOpen(false); row.onSelect(); }}
                            onKeyDown={event => {
                                if (event.key !== "Enter" && event.key !== " ") return;
                                // The close button handles its own keys; without this guard a
                                // keyboard press on × would bubble here and activate instead.
                                if ((event.target as HTMLElement).closest(".mc-task-tab-switcher__close")) {
                                    // Keep Space from scrolling the popover before keyup fires click.
                                    if (event.key === " ") event.preventDefault();
                                    return;
                                }
                                event.preventDefault();
                                setOpen(false);
                                row.onSelect();
                            }}
                        >
                            <span className="mc-task-tab-switcher__check" aria-hidden="true">{row.active ? "✓" : ""}</span>
                            <TabTypeIcon tab={row.iconTab} />
                            <span className="mc-task-tab-switcher__title" title={row.title}>{row.title}</span>
                            {row.closeTabId ? (
                                <button
                                    type="button"
                                    className="mc-task-tab-switcher__close"
                                    data-testid={`task-tab-switcher-close-${row.closeTabId}`}
                                    aria-label={label(lang, `Close ${row.title}`, `关闭 ${row.title}`, `關閉 ${row.title}`)}
                                    title={label(lang, "Close task", "关闭任务", "關閉任務")}
                                    onClick={event => { event.stopPropagation(); onClose(row.closeTabId as string); }}
                                >×</button>
                            ) : null}
                        </div>
                    ))}
                </div>
            )}
        </details>
    );
}

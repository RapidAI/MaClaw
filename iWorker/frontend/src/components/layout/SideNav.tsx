import { useState } from 'react';
import type { DiWorkerTab, HistoryTaskItem } from '../../types';
import { IconCheck, IconChevronRight, IconClaw, IconExpert, IconFilter, IconNewTask, IconSearch, IconSettings, IconSkills } from './SidebarIcons';

type NavItem = {
  id: DiWorkerTab;
  label: string;
  hint: string;
  icon: React.ReactNode;
};

type Props = {
  activeTab: DiWorkerTab;
  roleName: string;
  roleDescription: string;
  recentTasks?: HistoryTaskItem[];
  onChange: (tab: DiWorkerTab) => void;
};

const primaryItems: NavItem[] = [
  { id: 'home', label: 'Talk', hint: 'voice / IM', icon: <IconNewTask /> },
  { id: 'new-task', label: 'Task Space', hint: 'structured work', icon: <IconClaw /> },
  { id: 'colleagues', label: 'Partners', hint: 'iWorkers + humans', icon: <IconExpert /> },
  { id: 'history', label: 'Skills & Work', hint: 'tools and evidence', icon: <IconSkills /> },
];

export function SideNav({ activeTab, roleName, roleDescription, recentTasks = [], onChange }: Props) {
  const [searchQuery, setSearchQuery] = useState('');
  const visibleTasks = recentTasks
    .filter((task) => !searchQuery || task.title.toLowerCase().includes(searchQuery.toLowerCase()) || task.description?.toLowerCase().includes(searchQuery.toLowerCase()))
    .slice(0, 8);
  const avatarLabel = roleName.trim().charAt(0) || 'i';

  return (
    <aside className="dw-side-nav iw-sidebar">
      <div className="dw-side-nav-shell iw-sidebar-shell">
        <div className="iw-sidebar-brand">
          <span className="iw-brand-mark" aria-hidden="true">i</span>
          <div>
            <h1>iWorker</h1>
            <p>digital employee body</p>
          </div>
        </div>

        <div className="iw-sidebar-search-row">
          <label className="iw-sidebar-search">
            <IconSearch />
            <input
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="Search tasks"
              spellCheck={false}
            />
          </label>
          <button type="button" className="iw-sidebar-icon-button" onClick={() => onChange('settings')} aria-label="Open settings">
            <IconFilter />
          </button>
        </div>

        <nav className="iw-sidebar-nav" aria-label="iWorker navigation">
          {primaryItems.map((item) => (
            <button key={item.id} type="button" className={item.id === activeTab ? 'is-active' : ''} onClick={() => onChange(item.id)} aria-label={item.label}>
              <span className="iw-sidebar-nav-icon" aria-hidden="true">{item.icon}</span>
              <span className="iw-sidebar-nav-copy">
                <strong>{item.label}</strong>
                <small>{item.hint}</small>
              </span>
            </button>
          ))}
        </nav>

        <section className="iw-sidebar-section" aria-label="Recent work">
          <div className="iw-sidebar-section-title">Recent work</div>
          <div className="iw-sidebar-recent-list">
            {visibleTasks.length === 0 ? (
              <div className="iw-sidebar-empty">No matching work yet.</div>
            ) : visibleTasks.map((task) => (
              <button key={task.id} type="button" onClick={() => onChange('history')}>
                <span className="iw-sidebar-check" aria-hidden="true"><IconCheck /></span>
                <span className="iw-sidebar-task-copy">
                  <strong>{task.title}</strong>
                  <small>{task.owner} · {task.updatedAt}</small>
                </span>
              </button>
            ))}
          </div>
        </section>

        <div className="iw-sidebar-footer">
          <button type="button" className={`iw-sidebar-settings${activeTab === 'settings' ? ' is-active' : ''}`} onClick={() => onChange('settings')} aria-label="Open settings">
            <span className="iw-sidebar-nav-icon" aria-hidden="true"><IconSettings /></span>
            <span className="iw-sidebar-nav-copy">
              <strong>Settings</strong>
              <small>Center, memory, routing</small>
            </span>
          </button>

          <button type="button" className="iw-sidebar-profile" onClick={() => onChange('settings')}>
            <span className="iw-sidebar-avatar">{avatarLabel}</span>
            <span>
              <strong>{roleName}</strong>
              <small>{roleDescription.slice(0, 36)}</small>
            </span>
            <IconChevronRight />
          </button>
        </div>
      </div>
    </aside>
  );
}

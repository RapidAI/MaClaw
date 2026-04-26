import { useState } from 'react';
import type { DiWorkerTab, HistoryTaskItem } from '../../types';
import { IconClaw, IconExpert, IconSkills, IconSettings, IconSearch, IconCheck, IconChevronRight, IconFilter, IconNewTask } from './SidebarIcons';

type NavItem = {
  id: DiWorkerTab;
  label: string;
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
  { id: 'home', label: 'Talk', icon: <IconNewTask /> },
  { id: 'new-task', label: 'Task', icon: <IconClaw /> },
  { id: 'colleagues', label: 'Partners', icon: <IconExpert /> },
  { id: 'history', label: 'Tools', icon: <IconSkills /> },
];

export function SideNav({ activeTab, roleName, roleDescription, recentTasks = [], onChange }: Props) {
  const [searchQuery, setSearchQuery] = useState('');
  const visibleTasks = recentTasks
    .filter((task) => !searchQuery || task.title.toLowerCase().includes(searchQuery.toLowerCase()) || task.description?.toLowerCase().includes(searchQuery.toLowerCase()))
    .slice(0, 8);

  return (
    <aside className="dw-side-nav">
      <div className="dw-side-nav-shell">
        <div className="dw-brand dw-brand-compact" style={{ padding: '10px 8px 6px' }}>
          <div className="dw-brand-row dw-brand-row-compact" style={{ gap: '8px' }}>
            <span className="dw-nav-icon" style={{ width: '28px', height: '28px', borderRadius: '8px', fontSize: '14px', background: '#1f2937', color: '#fff' }} aria-hidden="true">i</span>
            <div style={{ display: 'flex', alignItems: 'baseline', gap: '4px' }}>
              <h1 style={{ fontSize: '14px' }}>iWorker</h1>
              <span style={{ fontSize: '10px', color: '#9ca3af', fontWeight: 400 }}>body node</span>
            </div>
          </div>
        </div>

        <div style={{ padding: '0 6px 6px', display: 'flex', gap: '4px', alignItems: 'center' }}>
          <div style={{
            flex: 1, display: 'flex', alignItems: 'center', gap: '6px',
            padding: '5px 8px', borderRadius: '6px',
            border: '1px solid #dde4ec', background: '#f3f5f8',
            color: '#9ca3af', fontSize: '12px',
          }}>
            <IconSearch />
            <input
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="Search tasks"
              spellCheck={false}
              style={{ border: 'none', background: 'transparent', outline: 'none', fontSize: '12px', color: '#1f2937', width: '100%', padding: 0 }}
            />
          </div>
          <button
            type="button"
            onClick={() => onChange('settings')}
            style={{
              display: 'flex', alignItems: 'center', justifyContent: 'center',
              width: '26px', height: '26px', borderRadius: '6px',
              border: '1px solid #dde4ec', background: '#f3f5f8',
              color: '#6b7280', cursor: 'pointer', flexShrink: 0,
            }}
            aria-label="Open settings"
          >
            <IconFilter />
          </button>
        </div>

        <nav className="dw-nav-list dw-nav-list-compact" style={{ padding: '0 4px' }}>
          {primaryItems.map((item) => (
            <button
              key={item.id}
              type="button"
              className={item.id === activeTab ? 'active' : ''}
              onClick={() => onChange(item.id)}
              aria-label={item.label}
              style={{ borderRadius: '6px' }}
            >
              <span className="dw-nav-icon" style={{ width: '28px', height: '28px', borderRadius: '8px', fontSize: '13px' }} aria-hidden="true">
                {item.icon}
              </span>
              <span className="dw-nav-copy dw-nav-copy-compact">
                <span>{item.label}</span>
              </span>
            </button>
          ))}
        </nav>

        <div style={{ padding: '10px 10px 4px', fontSize: '11px', fontWeight: 700, color: '#9ca3af', letterSpacing: '0.04em' }}>
          RECENT WORK
        </div>
        <div style={{ flex: 1, overflowY: 'auto', padding: '0 6px', minHeight: 0 }}>
          {visibleTasks.map((task) => (
            <button
              key={task.id}
              type="button"
              onClick={() => onChange('history')}
              style={{
                width: '100%', textAlign: 'left', display: 'flex', alignItems: 'flex-start', gap: '6px',
                padding: '5px 6px', borderRadius: '6px', border: 'none', background: 'transparent',
                color: '#4b5563', fontSize: '12px', lineHeight: '1.4', cursor: 'pointer',
              }}
              onMouseEnter={(e) => { e.currentTarget.style.background = '#f3f5f8'; }}
              onMouseLeave={(e) => { e.currentTarget.style.background = 'transparent'; }}
            >
              <span style={{ flexShrink: 0, marginTop: '2px', color: '#22c55e' }}><IconCheck /></span>
              <span style={{ flex: 1, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{task.title}</span>
              <span style={{ flexShrink: 0, fontSize: '10px', color: '#9ca3af', whiteSpace: 'nowrap' }}>{task.updatedAt}</span>
            </button>
          ))}
        </div>

        <div className="dw-side-footer-stack" style={{ gap: '4px', padding: '0 4px 4px' }}>
          <button
            type="button"
            className={`dw-side-settings-entry${activeTab === 'settings' ? ' is-active' : ''}`}
            onClick={() => onChange('settings')}
            aria-label="Open settings"
            style={{ borderRadius: '6px', padding: '6px 8px' }}
          >
            <span className="dw-nav-icon dw-nav-icon-small" style={{ borderRadius: '6px' }} aria-hidden="true">
              <IconSettings />
            </span>
            <span className="dw-nav-copy dw-nav-copy-compact">
              <span>Settings</span>
            </span>
          </button>

          <div style={{
            display: 'flex', alignItems: 'center', gap: '8px',
            padding: '8px 6px', borderTop: '1px solid #e5e7eb',
          }}>
            <span style={{
              width: '28px', height: '28px', borderRadius: '50%',
              background: '#dcfce7', display: 'flex', alignItems: 'center', justifyContent: 'center',
              fontSize: '12px', fontWeight: 700, color: '#166534', flexShrink: 0,
            }}>
              {roleName.charAt(0)}
            </span>
            <div style={{ flex: 1, minWidth: 0 }}>
              <div style={{ fontSize: '12px', fontWeight: 600, color: '#111827', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{roleName}</div>
              <div style={{ fontSize: '10px', color: '#9ca3af', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{roleDescription.slice(0, 24)}</div>
            </div>
            <span style={{ color: '#9ca3af', cursor: 'pointer', display: 'flex' }} onClick={() => onChange('settings')}>
              <IconChevronRight />
            </span>
          </div>
        </div>
      </div>
    </aside>
  );
}

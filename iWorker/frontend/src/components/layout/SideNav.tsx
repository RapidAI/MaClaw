import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { ReactNode } from 'react';
import type { DiWorkerTab, HistoryTaskItem } from '../../types';
import { IconCheck, IconChevronRight, IconClaw, IconExpert, IconFilter, IconNewTask, IconSearch, IconSettings, IconSkills } from './SidebarIcons';
import { LanguageSwitcher } from './LanguageSwitcher';

type NavItem = {
  id: DiWorkerTab;
  labelKey: string;
  hintKey: string;
  icon: ReactNode;
};

type Props = {
  activeTab: DiWorkerTab;
  roleName: string;
  roleDescription: string;
  recentTasks?: HistoryTaskItem[];
  interventionSummary?: { count: number; cachedCount: number; title: string };
  onChange: (tab: DiWorkerTab) => void;
};

const primaryItems: NavItem[] = [
  { id: 'home', labelKey: 'nav.home', hintKey: 'navHint.home', icon: <IconNewTask /> },
  { id: 'new-task', labelKey: 'nav.newTask', hintKey: 'navHint.newTask', icon: <IconClaw /> },
  { id: 'colleagues', labelKey: 'nav.colleagues', hintKey: 'navHint.colleagues', icon: <IconExpert /> },
  { id: 'history', labelKey: 'nav.history', hintKey: 'navHint.history', icon: <IconSkills /> },
];

export function SideNav({ activeTab, roleName, roleDescription, recentTasks = [], interventionSummary, onChange }: Props) {
  const { t } = useTranslation();
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
            <p>{t('sidebar.tagline')}</p>
          </div>
          <LanguageSwitcher />
        </div>

        <div className="iw-sidebar-search-row">
          <label className="iw-sidebar-search">
            <IconSearch />
            <input
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder={t('sidebar.search')}
              spellCheck={false}
            />
          </label>
          <button type="button" className="iw-sidebar-icon-button" onClick={() => onChange('settings')} aria-label={t('sidebar.openSettings')}>
            <IconFilter />
          </button>
        </div>

        {interventionSummary?.count ? (
          <button type="button" className="iw-sidebar-intervention" onClick={() => onChange('home')} aria-label={t('sidebar.openInterventionWorkbench')}>
            <span>{t('sidebar.intervention')}</span>
            <strong>{interventionSummary.title}</strong>
            <small>{interventionSummary.cachedCount ? t('sidebar.cachedNotice', { count: interventionSummary.cachedCount }) : t('sidebar.waitingNotice', { count: interventionSummary.count })}</small>
          </button>
        ) : null}

        <nav className="iw-sidebar-nav" aria-label={t('sidebar.navigation')}>
          {primaryItems.map((item) => {
            const label = t(item.labelKey);
            return (
              <button key={item.id} type="button" className={item.id === activeTab ? 'is-active' : ''} onClick={() => onChange(item.id)} aria-label={label}>
                <span className="iw-sidebar-nav-icon" aria-hidden="true">{item.icon}</span>
                <span className="iw-sidebar-nav-copy">
                  <strong>{label}</strong>
                  <small>{t(item.hintKey)}</small>
                </span>
                {item.id === 'home' && interventionSummary?.count ? <span className="iw-sidebar-alert-badge" aria-label={t('sidebar.humanInputNeeded', { count: interventionSummary.count })}>{interventionSummary.count}</span> : null}
              </button>
            );
          })}
        </nav>

        <section className="iw-sidebar-section" aria-label={t('sidebar.recentWork')}>
          <div className="iw-sidebar-section-title">{t('sidebar.recentWork')}</div>
          <div className="iw-sidebar-recent-list">
            {visibleTasks.length === 0 ? (
              <div className="iw-sidebar-empty">{t('sidebar.emptyRecent')}</div>
            ) : visibleTasks.map((task) => (
              <button key={task.id} type="button" onClick={() => onChange('history')}>
                <span className="iw-sidebar-check" aria-hidden="true"><IconCheck /></span>
                <span className="iw-sidebar-task-copy">
                  <strong>{task.title}</strong>
                  <small>{t('sidebar.updated', { owner: task.owner, time: task.updatedAt })}</small>
                </span>
              </button>
            ))}
          </div>
        </section>

        <div className="iw-sidebar-footer">
          <button type="button" className={'iw-sidebar-settings' + (activeTab === 'settings' ? ' is-active' : '')} onClick={() => onChange('settings')} aria-label={t('sidebar.openSettings')}>
            <span className="iw-sidebar-nav-icon" aria-hidden="true"><IconSettings /></span>
            <span className="iw-sidebar-nav-copy">
              <strong>{t('sidebar.settings')}</strong>
              <small>{t('sidebar.settingsHint')}</small>
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

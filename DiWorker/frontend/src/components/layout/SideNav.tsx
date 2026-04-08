import type { DiWorkerTab } from '../../types';

type NavItem = {
  id: DiWorkerTab;
  label: string;
  icon: string;
};

type Props = {
  activeTab: DiWorkerTab;
  roleName: string;
  roleDescription: string;
  onChange: (tab: DiWorkerTab) => void;
};

const items: NavItem[] = [
  { id: 'home', label: '首页', icon: '⌂' },
  { id: 'colleagues', label: '同事', icon: '◫' },
  { id: 'new-task', label: '新建', icon: '+' },
  { id: 'history', label: '记录', icon: '≣' },
];

export function SideNav({ activeTab, roleName, roleDescription, onChange }: Props) {
  return (
    <aside className="dw-side-nav">
      <div className="dw-side-nav-shell">
        <div className="dw-brand dw-brand-compact">
          <div className="dw-brand-row dw-brand-row-compact">
            <div>
              <h1>DiWorker</h1>
            </div>
          </div>
        </div>
        <nav className="dw-nav-list dw-nav-list-compact">
          {items.map((item) => (
            <button
              key={item.id}
              type="button"
              className={item.id === activeTab ? 'active' : ''}
              onClick={() => onChange(item.id)}
              aria-label={item.id === 'colleagues' ? '找同事' : item.id === 'new-task' ? '新建任务' : item.id === 'history' ? '历史任务' : item.label}
            >
              <span className="dw-nav-icon" aria-hidden="true">{item.icon}</span>
              <span className="dw-nav-copy dw-nav-copy-compact">
                <span>{item.label}</span>
              </span>
            </button>
          ))}
        </nav>
        <div className="dw-side-footer-stack">
          <section className="dw-side-role-card" aria-label="当前角色信息">
            <div className="dw-side-role-head">
              <span className="dw-nav-icon dw-nav-icon-small" aria-hidden="true">◈</span>
              <div>
                <strong>{roleName}</strong>
                <p>{roleDescription}</p>
              </div>
            </div>
          </section>
          <button type="button" className={`dw-side-settings-entry${activeTab === 'settings' ? ' is-active' : ''}`} onClick={() => onChange('settings')} aria-label="打开配置界面">
            <span className="dw-nav-icon dw-nav-icon-small" aria-hidden="true">⚙</span>
            <span className="dw-nav-copy dw-nav-copy-compact">
              <span>配置中心</span>
            </span>
          </button>
          <div className="dw-side-footer dw-side-footer-compact">
            <span>本地工作区</span>
          </div>
        </div>
      </div>
    </aside>
  );
}

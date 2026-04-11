import { useTranslation } from 'react-i18next';
import type { CenterTab } from '../../types';

const tabIds: CenterTab[] = [
  'overview', 'employees', 'communications', 'workflows',
  'knowledge', 'packages', 'models', 'compute', 'security',
  'delivery', 'usage', 'im', 'auth', 'settings',
];

type Props = {
  activeTab: CenterTab;
  onChange: (tab: CenterTab) => void;
};

export function SideNav({ activeTab, onChange }: Props) {
  const { t } = useTranslation();

  return (
    <aside className="center-side">
      <div className="center-brand">
        <div className="mini">iWorkerCenter</div>
        <h1>{t('app.title')}</h1>
      </div>
      <nav className="center-nav">
        {tabIds.map((id) => (
          <button
            key={id}
            type="button"
            className={id === activeTab ? 'active' : ''}
            onClick={() => onChange(id)}
          >
            <span>{t(`nav.${id}`)}</span>
          </button>
        ))}
      </nav>
    </aside>
  );
}

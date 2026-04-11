import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { checkSetup } from './api/auth';
import { clearToken } from './api/client';
import { LoginPage } from './pages/LoginPage';
import { SetupPage } from './pages/SetupPage';
import { OverviewPage } from './pages/OverviewPage';
import { CentersPage } from './pages/CentersPage';
import { LicensesPage } from './pages/LicensesPage';
import { SettingsPage } from './pages/SettingsPage';
import { ComputePowerPage } from './pages/ComputePowerPage';
import { LanguageSwitcher } from './components/LanguageSwitcher';

type Tab = 'overview' | 'centers' | 'licenses' | 'compute' | 'settings';
const tabs: Tab[] = ['overview', 'centers', 'licenses', 'compute', 'settings'];

export default function App() {
  const { t } = useTranslation();
  const [phase, setPhase] = useState<'loading' | 'setup' | 'login' | 'app'>('loading');
  const [activeTab, setActiveTab] = useState<Tab>('overview');

  useEffect(() => {
    checkSetup()
      .then(s => setPhase(s.setup ? 'login' : 'setup'))
      .catch(() => setPhase('login'));
  }, []);

  if (phase === 'loading') return <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100vh', color: '#888' }}>{t('app.loading')}</div>;
  if (phase === 'setup') return <SetupPage onDone={() => setPhase('login')} />;
  if (phase === 'login') return <LoginPage onLogin={() => setPhase('app')} />;

  const content = (() => {
    switch (activeTab) {
      case 'overview': return <OverviewPage />;
      case 'centers': return <CentersPage />;
      case 'licenses': return <LicensesPage />;
      case 'compute': return <ComputePowerPage />;
      case 'settings': return <SettingsPage />;
    }
  })();

  const handleLogout = () => { clearToken(); setPhase('login'); };

  return (
    <div className="app-shell">
      <aside className="side">
        <div className="brand">
          <div className="mini">iWorkerCloud</div>
          <h1>{t('app.title')}</h1>
        </div>
        <nav className="nav">
          {tabs.map(id => (
            <button key={id} className={id === activeTab ? 'active' : ''} onClick={() => setActiveTab(id)}>
              {t(`nav.${id}`)}
            </button>
          ))}
        </nav>
      </aside>
      <main className="main">
        <div className="top-bar">
          <div>
            <h2>{t(`nav.${activeTab}`)}</h2>
            <p>{t(`subtitle.${activeTab}`)}</p>
          </div>
          <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
            <LanguageSwitcher />
            <button className="btn-ghost" onClick={handleLogout}>{t('common.logout')}</button>
          </div>
        </div>
        {content}
      </main>
    </div>
  );
}

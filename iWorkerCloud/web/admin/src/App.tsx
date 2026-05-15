import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { checkSetup } from './api/auth';
import { AUTH_EXPIRED_EVENT, clearToken, getToken } from './api/client';
import { LoginPage } from './pages/LoginPage';
import { SetupPage } from './pages/SetupPage';
import { OverviewPage } from './pages/OverviewPage';
import { CentersPage } from './pages/CentersPage';
import { LicensesPage } from './pages/LicensesPage';
import { SettingsPage } from './pages/SettingsPage';
import { ComputePowerPage } from './pages/ComputePowerPage';
import { CapabilityMarketPage } from './pages/CapabilityMarketPage';
import { LanguageSwitcher } from './components/LanguageSwitcher';

type Tab = 'overview' | 'centers' | 'licenses' | 'compute' | 'capabilitymarket' | 'settings';
const tabs: Tab[] = ['overview', 'centers', 'licenses', 'compute', 'capabilitymarket', 'settings'];

/** Legacy tab aliases that redirect to their new names */
const tabAliases: Record<string, Tab> = {
  skills: 'capabilitymarket',
  skillmarket: 'capabilitymarket',
};

function isTab(value: string): value is Tab {
  return tabs.includes(value as Tab);
}

function readTabFromHash(): Tab {
  const raw = window.location.hash.replace(/^#\/?/, '');
  if (isTab(raw)) return raw;
  // Support legacy hash values with redirect
  if (raw in tabAliases) {
    const target = tabAliases[raw];
    window.location.hash = target;
    return target;
  }
  return 'overview';
}

export default function App() {
  const { t } = useTranslation();
  const [phase, setPhase] = useState<'loading' | 'setup' | 'login' | 'app'>('loading');
  const [activeTab, setActiveTab] = useState<Tab>(() => readTabFromHash());
  const [loginNoticeKey, setLoginNoticeKey] = useState('');

  useEffect(() => {
    checkSetup()
      .then(s => setPhase(s.setup ? (getToken() ? 'app' : 'login') : 'setup'))
      .catch(() => setPhase('login'));
  }, []);

  useEffect(() => {
    const handleExpired = () => {
      setLoginNoticeKey('login.sessionExpired');
      setPhase('login');
    };
    window.addEventListener(AUTH_EXPIRED_EVENT, handleExpired);
    return () => window.removeEventListener(AUTH_EXPIRED_EVENT, handleExpired);
  }, []);

  useEffect(() => {
    const handleHashChange = () => setActiveTab(readTabFromHash());
    window.addEventListener('hashchange', handleHashChange);
    return () => window.removeEventListener('hashchange', handleHashChange);
  }, []);

  const selectTab = (tab: Tab) => {
    setActiveTab(tab);
    if (window.location.hash !== '#' + tab) window.location.hash = tab;
  };

  if (phase === 'loading') return <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100vh', color: '#888' }}>{t('app.loading')}</div>;
  if (phase === 'setup') return <SetupPage onDone={() => { setLoginNoticeKey('setup.success'); setPhase('login'); }} />;
  if (phase === 'login') return <LoginPage noticeKey={loginNoticeKey} onLogin={() => { setLoginNoticeKey(''); setPhase('app'); }} />;

  const content = (() => {
    switch (activeTab) {
      case 'overview': return <OverviewPage />;
      case 'centers': return <CentersPage />;
      case 'licenses': return <LicensesPage />;
      case 'compute': return <ComputePowerPage />;
      case 'capabilitymarket': return <CapabilityMarketPage />;
      case 'settings': return <SettingsPage />;
    }
  })();

  const handleLogout = () => {
    clearToken();
    setLoginNoticeKey('login.loggedOut');
    setPhase('login');
  };

  return (
    <div className="app-shell">
      <aside className="side">
        <div className="brand">
          <div className="mini">iWorkerCloud</div>
          <h1>{t('app.title')}</h1>
        </div>
        <nav className="nav">
          {tabs.map(id => (
            <button key={id} className={id === activeTab ? 'active' : ''} onClick={() => selectTab(id)}>
              <span>{t(`nav.${id}`)}</span>
              <small>{t(`subtitle.${id}`)}</small>
            </button>
          ))}
        </nav>
        <div className="side-note">
          <strong>{t('app.sideNoteTitle')}</strong>
          <br />
          <span>{t('app.sideNoteDesc')}</span>
        </div>
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

import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { SideNav } from './components/layout/SideNav';
import { TopHeader } from './components/layout/TopHeader';
import { LanguageSwitcher } from './components/layout/LanguageSwitcher';
import { AccountSettingsPage } from './pages/AccountSettingsPage';
import { AuthPage } from './pages/AuthPage';
import { CommunicationsPage } from './pages/CommunicationsPage';
import { DeliveryPage } from './pages/DeliveryPage';
import { EmployeesPage } from './pages/EmployeesPage';
import { IMSettingsPage } from './pages/IMSettingsPage';
import { KnowledgePage } from './pages/KnowledgePage';
import { LoginPage } from './pages/LoginPage';
import { ModelRoutingPage } from './pages/ModelRoutingPage';
import { ComputePowerPage } from './pages/ComputePowerPage';
import { OverviewPage } from './pages/OverviewPage';
import { PackagesPage } from './pages/PackagesPage';
import { SecurityPage } from './pages/SecurityPage';
import { SetupTenantPage } from './pages/SetupTenantPage';
import { UsagePage } from './pages/UsagePage';
import { WorkflowsPage } from './pages/WorkflowsPage';
import type { CenterTab } from './types';

export default function App() {
  const { t } = useTranslation();
  const [authenticated, setAuthenticated] = useState(false);
  const [checking, setChecking] = useState(true);
  const [needsSetup, setNeedsSetup] = useState(false);
  const [activeTab, setActiveTab] = useState<CenterTab>('overview');

  useEffect(() => {
    Promise.all([
      fetch('/auth/tenant-status').then(r => r.ok ? r.json() : null).catch(() => null),
      fetch('/auth/check').then(r => { if (r.ok) setAuthenticated(true); }).catch(() => {}),
    ]).then(([tenantStatus]) => {
      if (tenantStatus && tenantStatus.needs_setup) {
        setNeedsSetup(true);
      }
    }).finally(() => setChecking(false));
  }, []);

  // useMemo MUST be called before any conditional returns (React hooks rules)
  const content = useMemo(() => {
    switch (activeTab) {
      case 'employees': return <EmployeesPage />;
      case 'models': return <ModelRoutingPage />;
      case 'compute': return <ComputePowerPage />;
      case 'overview': return <OverviewPage />;
      case 'communications': return <CommunicationsPage />;
      case 'workflows': return <WorkflowsPage />;
      case 'knowledge': return <KnowledgePage />;
      case 'packages': return <PackagesPage />;
      case 'security': return <SecurityPage />;
      case 'delivery': return <DeliveryPage />;
      case 'usage': return <UsagePage />;
      case 'im': return <IMSettingsPage />;
      case 'auth': return <AuthPage />;
      case 'settings': return <AccountSettingsPage />;
      default: return null;
    }
  }, [activeTab]);

  if (checking) {
    return (
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100vh', color: '#888' }}>
        {t('app.loading')}
      </div>
    );
  }

  if (needsSetup) {
    return <SetupTenantPage onSetupComplete={() => setNeedsSetup(false)} />;
  }

  if (!authenticated) {
    return <LoginPage onLogin={() => setAuthenticated(true)} />;
  }

  return (
    <div className="center-shell">
      <SideNav activeTab={activeTab} onChange={setActiveTab} />
      <main className="center-main">
        <TopHeader
          title={t(`nav.${activeTab}`)}
          subtitle={t(`subtitle.${activeTab}`)}
        />
        <LanguageSwitcher />
        {content}
      </main>
    </div>
  );
}

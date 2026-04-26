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
import type { AssetNavigationTarget, CenterTab, CommunicationsNavigationTarget, OverviewNavigationTarget } from './types';

export default function App() {
  const { t } = useTranslation();
  const [authenticated, setAuthenticated] = useState(false);
  const [checking, setChecking] = useState(true);
  const [needsSetup, setNeedsSetup] = useState(false);
  const [activeTab, setActiveTab] = useState<CenterTab>('overview');
  const [communicationsTarget, setCommunicationsTarget] = useState<CommunicationsNavigationTarget | null>(null);
  const [overviewTarget, setOverviewTarget] = useState<OverviewNavigationTarget | null>(null);
  const [knowledgeTarget, setKnowledgeTarget] = useState<AssetNavigationTarget | null>(null);
  const [packagesTarget, setPackagesTarget] = useState<AssetNavigationTarget | null>(null);
  const [workflowsTarget, setWorkflowsTarget] = useState<AssetNavigationTarget | null>(null);

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

  const handleNavigateToCommunications = (target: CommunicationsNavigationTarget) => {
    setCommunicationsTarget(target);
    setActiveTab('communications');
  };

  const handleNavigateToOverview = (target: OverviewNavigationTarget) => {
    setOverviewTarget(target);
    setActiveTab('overview');
  };

  const handleNavigateBackToOverview = (target?: AssetNavigationTarget | null) => {
    setOverviewTarget(target ? {
      role_code: target.role_code,
      source: target.source || 'asset_review',
    } : null);
    setActiveTab('overview');
  };

  const handleNavigateToTab = (tab: CenterTab, target?: AssetNavigationTarget) => {
    if (tab === 'knowledge') {
      setKnowledgeTarget(target || null);
    }
    if (tab === 'packages') {
      setPackagesTarget(target || null);
    }
    if (tab === 'workflows') {
      setWorkflowsTarget(target || null);
    }
    setActiveTab(tab);
  };

  const content = useMemo(() => {
    switch (activeTab) {
      case 'employees': return <EmployeesPage />;
      case 'models': return <ModelRoutingPage />;
      case 'compute': return <ComputePowerPage />;
      case 'overview': return <OverviewPage navigationTarget={overviewTarget} onNavigationHandled={() => setOverviewTarget(null)} onNavigateToCommunications={handleNavigateToCommunications} onNavigateToTab={handleNavigateToTab} />;
      case 'communications': return <CommunicationsPage navigationTarget={communicationsTarget} onNavigationHandled={() => setCommunicationsTarget(null)} onNavigateToOverview={handleNavigateToOverview} />;
      case 'workflows': return <WorkflowsPage navigationTarget={workflowsTarget} onNavigationHandled={() => setWorkflowsTarget(null)} onNavigateToOverview={handleNavigateBackToOverview} onNavigateToTab={handleNavigateToTab} />;
      case 'knowledge': return <KnowledgePage navigationTarget={knowledgeTarget} onNavigationHandled={() => setKnowledgeTarget(null)} onNavigateToOverview={handleNavigateBackToOverview} onNavigateToTab={handleNavigateToTab} />;
      case 'packages': return <PackagesPage navigationTarget={packagesTarget} onNavigationHandled={() => setPackagesTarget(null)} onNavigateToOverview={handleNavigateBackToOverview} onNavigateToTab={handleNavigateToTab} />;
      case 'security': return <SecurityPage />;
      case 'delivery': return <DeliveryPage />;
      case 'usage': return <UsagePage />;
      case 'im': return <IMSettingsPage />;
      case 'auth': return <AuthPage />;
      case 'settings': return <AccountSettingsPage />;
      default: return null;
    }
  }, [activeTab, communicationsTarget, overviewTarget, knowledgeTarget, packagesTarget, workflowsTarget]);

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

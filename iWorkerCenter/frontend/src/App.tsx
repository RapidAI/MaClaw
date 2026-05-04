import { useCallback, useEffect, useRef, useState } from 'react';
import { fetchBootstrapStatus, isBootstrapComplete, type BootstrapStatus } from './api/bootstrap';
import { installTenantFetchInterceptor, rememberTenantID } from './api/tenantFetch';
import { SideNav } from './components/layout/SideNav';
import { TopHeader } from './components/layout/TopHeader';
import { AccountSettingsPage } from './pages/AccountSettingsPage';
import { AuthPage } from './pages/AuthPage';
import { BootstrapPage } from './pages/BootstrapPage';
import { CloudRegistrationPage } from './pages/CloudRegistrationPage';
import { CommunicationsPage } from './pages/CommunicationsPage';
import { DeliveryPage } from './pages/DeliveryPage';
import { EmployeesPage } from './pages/EmployeesPage';
import { IMSettingsPage } from './pages/IMSettingsPage';
import { KnowledgePage } from './pages/KnowledgePage';
import { LoginPage } from './pages/LoginPage';
import { ModelRoutingPage } from './pages/ModelRoutingPage';
import { OverviewPage } from './pages/OverviewPage';
import { PackagesPage } from './pages/PackagesPage';
import { SecurityPage } from './pages/SecurityPage';
import { SetupTenantPage } from './pages/SetupTenantPage';
import { UsagePage } from './pages/UsagePage';
import { WorkflowsPage } from './pages/WorkflowsPage';
import type { CenterTab } from './types';

type AuthCheck = { username?: string; email?: string; tenant_id?: string };

const meta: Record<CenterTab, { title: string; subtitle: string }> = {
  overview: { title: '总览', subtitle: '查看数字员工中心的运行状态、告警和关键待办。' },
  bootstrap: { title: '单位初始化', subtitle: '为当前租户创建启动计划、组织骨架、首批 iWorker 和首次运行任务。' },
  employees: { title: '数字员工', subtitle: '管理 iWorker 身份、角色、能力偏好和模型策略。' },
  communications: { title: '员工通讯', subtitle: '查看数字员工之间的协作记录、请求流转和人工介入。' },
  workflows: { title: '流程设计', subtitle: '编排任务如何在数字员工、技能和人工节点之间流转。' },
  knowledge: { title: '经验共享', subtitle: '沉淀组织经验，并按公司、部门和个人范围复用。' },
  packages: { title: '能力包', subtitle: '管理技能与 MCP 能力包的来源、版本、安装和下发状态。' },
  models: { title: '模型调度', subtitle: '统一配置默认模型、备用模型和路由规则。' },
  cloud: { title: '云端注册', subtitle: '连接 iWorkerCloud，提交注册信息、确认授权并验证心跳状态。' },
  security: { title: '安全规则', subtitle: '下发统一治理规则，并保留审计与策略命中记录。' },
  delivery: { title: '下发管理', subtitle: '查看配置、能力包和 MCP 向 iWorker 客户端的下发状态。' },
  usage: { title: '使用情况', subtitle: '跟踪数字员工调用量、任务量和资源趋势。' },
  im: { title: 'IM 管理', subtitle: '配置飞书、钉钉、企业微信等企业通讯入口。' },
  auth: { title: '认证管理', subtitle: '管理 LDAP、本地账号和预留的 OIDC/OAuth 认证适配器。' },
  settings: { title: '账号设置', subtitle: '管理管理员邮箱、登录密码和租户模式。' },
};

const isWails = typeof window !== 'undefined' && typeof (window as Window & { go?: unknown }).go !== 'undefined';

installTenantFetchInterceptor();

export default function App() {
  const [authenticated, setAuthenticated] = useState(isWails);
  const [checking, setChecking] = useState(!isWails);
  const [needsSetup, setNeedsSetup] = useState(false);
  const [activeTab, setActiveTab] = useState<CenterTab>('overview');
  const [currentTenantId, setCurrentTenantId] = useState(isWails ? 'default' : '');
  const [bootstrapStatus, setBootstrapStatus] = useState<BootstrapStatus | null>(null);
  const [bootstrapWizardOpen, setBootstrapWizardOpen] = useState(false);
  const dismissedBootstrapTenants = useRef<Set<string>>(new Set());

  const evaluateBootstrapForTenant = useCallback(async (allowPrompt: boolean) => {
    try {
      const status = await fetchBootstrapStatus();
      setBootstrapStatus(status);
      setCurrentTenantId(status.tenant_id || 'default');
      rememberTenantID(status.tenant_id || 'default');
      const tenantID = status.tenant_id || 'default';
      const dismissed = dismissedBootstrapTenants.current.has(tenantID);
      if (allowPrompt && !isBootstrapComplete(status) && !dismissed) {
        setActiveTab('bootstrap');
        setBootstrapWizardOpen(true);
      }
      return status;
    } catch {
      setBootstrapStatus(null);
      return null;
    }
  }, []);

  const markAuthenticated = useCallback((tenantID?: string) => {
    setAuthenticated(true);
    if (tenantID) { setCurrentTenantId(tenantID); rememberTenantID(tenantID); }
    setBootstrapWizardOpen(false);
    void evaluateBootstrapForTenant(true);
  }, [evaluateBootstrapForTenant]);

  useEffect(() => {
    if (isWails) {
      void evaluateBootstrapForTenant(false);
      return;
    }
    Promise.all([
      fetch('/auth/tenant-status').then(r => r.ok ? r.json() : null).catch(() => null),
      fetch('/auth/check').then(async r => r.ok ? await r.json() as AuthCheck : null).catch(() => null),
    ]).then(([tenantStatus, auth]) => {
      if (tenantStatus && tenantStatus.needs_setup) {
        setNeedsSetup(true);
        return;
      }
      if (auth?.tenant_id) {
        setAuthenticated(true);
        setCurrentTenantId(auth.tenant_id);
        rememberTenantID(auth.tenant_id);
      }
    }).finally(() => setChecking(false));
  }, [evaluateBootstrapForTenant]);

  useEffect(() => {
    if (authenticated && !needsSetup) {
      void evaluateBootstrapForTenant(true);
    }
  }, [authenticated, needsSetup, currentTenantId, evaluateBootstrapForTenant]);

  const closeBootstrapWizard = () => {
    const tenantID = bootstrapStatus?.tenant_id || currentTenantId || 'default';
    setBootstrapWizardOpen(false);
    dismissedBootstrapTenants.current.add(tenantID);
  };

  const handleBootstrapChanged = (status: BootstrapStatus | null) => {
    setBootstrapStatus(status);
    if (status?.tenant_id) { setCurrentTenantId(status.tenant_id); rememberTenantID(status.tenant_id); }
    if (isBootstrapComplete(status)) setBootstrapWizardOpen(false);
  };

  if (checking) return <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100vh', color: '#888' }}>加载中...</div>;
  if (needsSetup) return <SetupTenantPage onSetupComplete={(tenantID) => { setNeedsSetup(false); setAuthenticated(false); if (tenantID) { setCurrentTenantId(tenantID); rememberTenantID(tenantID); } setActiveTab('bootstrap'); setBootstrapWizardOpen(false); }} />;
  if (!authenticated) return <LoginPage onLogin={(tenantID) => markAuthenticated(tenantID)} />;

  const renderContent = () => {
    switch (activeTab) {
      case 'bootstrap': return <BootstrapPage wizardOpen={bootstrapWizardOpen} onWizardClose={closeBootstrapWizard} onBootstrapChanged={handleBootstrapChanged} />;
      case 'employees': return <EmployeesPage />;
      case 'models': return <ModelRoutingPage />;
      case 'cloud': return <CloudRegistrationPage />;
      case 'overview': return <OverviewPage onNavigate={setActiveTab} />;
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
  };

  return (
    <div className="center-shell">
      <SideNav activeTab={activeTab} onChange={setActiveTab} />
      <main className="center-main">
        <TopHeader title={meta[activeTab].title} subtitle={meta[activeTab].subtitle} tenantId={currentTenantId || bootstrapStatus?.tenant_id} bootstrapReady={isBootstrapComplete(bootstrapStatus)} />
        {renderContent()}
      </main>
    </div>
  );
}

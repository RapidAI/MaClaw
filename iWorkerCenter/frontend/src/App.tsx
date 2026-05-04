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
import { useI18n } from './i18n';
import type { CenterTab } from './types';

type AuthCheck = { username?: string; email?: string; tenant_id?: string };

const meta: Record<CenterTab, { title: { zh: string; en: string }; subtitle: { zh: string; en: string } }> = {
  overview: { title: { zh: '总览', en: 'Overview' }, subtitle: { zh: '查看数字员工中心的运行状态、告警和关键待办。', en: 'Review iWorkerCenter runtime status, alerts, and key actions.' } },
  bootstrap: { title: { zh: '单位初始化', en: 'Bootstrap' }, subtitle: { zh: '为当前租户创建启动计划、组织骨架、首批 iWorker 和初始任务。', en: 'Create the tenant bootstrap plan, organization structure, first iWorkers, and initial work.' } },
  employees: { title: { zh: '数字员工', en: 'iWorkers' }, subtitle: { zh: '管理 iWorker 身份、角色、能力偏好和模型策略。', en: 'Manage iWorker identities, roles, capability preferences, and model policies.' } },
  communications: { title: { zh: '员工通讯', en: 'Communications' }, subtitle: { zh: '查看数字员工之间的协作记录、请求流转和人工介入。', en: 'Review collaboration records, request handoffs, and human interventions.' } },
  workflows: { title: { zh: '流程设计', en: 'Workflows' }, subtitle: { zh: '编排任务如何在数字员工、技能和人工节点之间流转。', en: 'Orchestrate tasks across iWorkers, skills, and human checkpoints.' } },
  knowledge: { title: { zh: '经验共享', en: 'Knowledge' }, subtitle: { zh: '沉淀组织经验，并按公司、部门和个人范围复用。', en: 'Capture organization knowledge and reuse it by company, team, and person.' } },
  packages: { title: { zh: '能力包', en: 'Packages' }, subtitle: { zh: '管理技能与 MCP 能力包的来源、版本、安装和下发状态。', en: 'Manage skill and MCP packages, versions, installation, and delivery status.' } },
  models: { title: { zh: '模型调度', en: 'Model Routing' }, subtitle: { zh: '统一配置默认模型、备用模型和路由规则。', en: 'Configure default models, fallback models, and routing rules.' } },
  cloud: { title: { zh: '云端注册', en: 'Cloud Registration' }, subtitle: { zh: '连接 iWorkerCloud，提交注册信息、确认授权并验证心跳状态。', en: 'Connect to iWorkerCloud, submit registration details, confirm authorization, and verify heartbeat status.' } },
  security: { title: { zh: '安全规则', en: 'Security' }, subtitle: { zh: '下发统一治理规则，并保留审计与策略命中记录。', en: 'Distribute governance rules and keep audit and policy-hit records.' } },
  delivery: { title: { zh: '下发管理', en: 'Delivery' }, subtitle: { zh: '查看配置、能力包和 MCP 向 iWorker 客户端的下发状态。', en: 'Track configuration, package, and MCP delivery to iWorker clients.' } },
  usage: { title: { zh: '使用情况', en: 'Usage' }, subtitle: { zh: '跟踪数字员工调用量、任务量和资源趋势。', en: 'Track iWorker calls, workload, and resource trends.' } },
  im: { title: { zh: 'IM 管理', en: 'IM Settings' }, subtitle: { zh: '配置飞书、钉钉、企业微信等企业通讯入口。', en: 'Configure enterprise messaging entrances such as Feishu, DingTalk, and WeCom.' } },
  auth: { title: { zh: '认证管理', en: 'Authentication' }, subtitle: { zh: '管理 LDAP、本地账号和预留的 OIDC/OAuth 认证适配器。', en: 'Manage LDAP, local accounts, and reserved OIDC/OAuth adapters.' } },
  settings: { title: { zh: '账号设置', en: 'Account Settings' }, subtitle: { zh: '管理管理员邮箱、登录密码和租户模式。', en: 'Manage admin email, login password, and tenant mode.' } },
};

const isWails = typeof window !== 'undefined' && typeof (window as Window & { go?: unknown }).go !== 'undefined';

installTenantFetchInterceptor();

export default function App() {
  const { t } = useI18n();
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
        <TopHeader title={t(meta[activeTab].title.zh, meta[activeTab].title.en)} subtitle={t(meta[activeTab].subtitle.zh, meta[activeTab].subtitle.en)} tenantId={currentTenantId || bootstrapStatus?.tenant_id} bootstrapReady={isBootstrapComplete(bootstrapStatus)} />
        {renderContent()}
      </main>
    </div>
  );
}

import { useEffect, useState } from 'react';
import { SideNav } from './components/layout/SideNav';
import { TopHeader } from './components/layout/TopHeader';
import { AccountSettingsPage } from './pages/AccountSettingsPage';
import { AuthPage } from './pages/AuthPage';
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

const meta: Record<CenterTab, { title: string; subtitle: string }> = {
  overview: { title: '总览', subtitle: '查看数字员工中心的运行状态、告警和关键待办。' },
  employees: { title: '数字员工', subtitle: '管理 iWorker 身份、角色、能力偏好和模型策略。' },
  communications: { title: '员工通讯', subtitle: '查看数字员工之间的协作记录、请求流转和人工介入。' },
  workflows: { title: '流程设计', subtitle: '编排任务如何在不同数字员工、技能和人工节点之间流转。' },
  knowledge: { title: '经验共享', subtitle: '沉淀组织经验，并按公司、部门和个人范围复用。' },
  packages: { title: '能力包', subtitle: '管理技能与 MCP 能力包的来源、版本、安装和下发状态。' },
  models: { title: '模型调度', subtitle: '统一配置默认模型、备用模型和路由规则。' },
  cloud: { title: '云端注册', subtitle: '连接 iWorkerCloud，提交注册信息、确认授权并验证心跳状态。' },
  security: { title: '安全规则', subtitle: '下发统一治理规则，并保留审计与策略命中记录。' },
  delivery: { title: '下发管理', subtitle: '查看配置、能力包和 MCP 向 iWorker 客户端的下发状态。' },
  usage: { title: '使用情况', subtitle: '跟踪数字员工调用量、任务量和资源趋势。' },
  im: { title: 'IM 管理', subtitle: '配置飞书、钉钉、企业微信等企业通讯入口。' },
  auth: { title: '认证管理', subtitle: '管理 LDAP、本地账号和预留的 OIDC/OAuth 认证适配器。' },
  settings: { title: '账户设置', subtitle: '管理管理员邮箱、登录密码和租户模式。' },
};

// In Wails mode, skip login (desktop app handles auth).
const isWails = typeof window !== 'undefined' && typeof (window as Window & { go?: unknown }).go !== 'undefined';

export default function App() {
  const [authenticated, setAuthenticated] = useState(isWails);
  const [checking, setChecking] = useState(!isWails);
  const [needsSetup, setNeedsSetup] = useState(false);
  const [activeTab, setActiveTab] = useState<CenterTab>('overview');

  // Check tenant status and existing session on mount (HTTP mode only)
  useEffect(() => {
    if (isWails) return;
    Promise.all([
      fetch('/auth/tenant-status').then(r => r.ok ? r.json() : null).catch(() => null),
      fetch('/auth/check').then(r => { if (r.ok) setAuthenticated(true); }).catch(() => {}),
    ]).then(([tenantStatus]) => {
      if (tenantStatus && tenantStatus.needs_setup) {
        setNeedsSetup(true);
      }
    }).finally(() => setChecking(false));
  }, []);

  if (checking) {
    return <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100vh', color: '#888' }}>加载中...</div>;
  }

  if (needsSetup) {
    return <SetupTenantPage onSetupComplete={() => { setNeedsSetup(false); }} />;
  }

  if (!authenticated) {
    return <LoginPage onLogin={() => setAuthenticated(true)} />;
  }

  const renderContent = () => {
    switch (activeTab) {
      case 'employees': return <EmployeesPage />;
      case 'models': return <ModelRoutingPage />;
      case 'cloud': return <CloudRegistrationPage />;
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
  };

  return (
    <div className="center-shell">
      <SideNav activeTab={activeTab} onChange={setActiveTab} />
      <main className="center-main">
        <TopHeader title={meta[activeTab].title} subtitle={meta[activeTab].subtitle} />
        {renderContent()}
      </main>
    </div>
  );
}

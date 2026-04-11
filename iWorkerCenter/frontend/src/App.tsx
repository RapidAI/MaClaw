import { useMemo, useState } from 'react';
import { SideNav } from './components/layout/SideNav';
import { TopHeader } from './components/layout/TopHeader';
import { CommunicationsPage } from './pages/CommunicationsPage';
import { DeliveryPage } from './pages/DeliveryPage';
import { EmployeesPage } from './pages/EmployeesPage';
import { KnowledgePage } from './pages/KnowledgePage';
import { ModelRoutingPage } from './pages/ModelRoutingPage';
import { OverviewPage } from './pages/OverviewPage';
import { PackagesPage } from './pages/PackagesPage';
import { SecurityPage } from './pages/SecurityPage';
import { UsagePage } from './pages/UsagePage';
import { WorkflowsPage } from './pages/WorkflowsPage';
import type { CenterTab } from './types';

const meta: Record<CenterTab, { title: string; subtitle: string }> = {
  overview: { title: '总览', subtitle: '帮助管理员快速了解数字员工中心的整体运行情况。' },
  employees: { title: '数字员工', subtitle: '管理身份、角色、能力偏好和模型策略。' },
  communications: { title: '员工通讯', subtitle: '查看数字员工之间的协作记录和请求流转。' },
  workflows: { title: '流程设计', subtitle: '配置事务如何在不同数字员工之间流转。' },
  knowledge: { title: '经验共享', subtitle: '沉淀经验并支持不同员工复用。' },
  packages: { title: '能力包', subtitle: '管理能力包来源、版本和分发状态。' },
  models: { title: '模型调度', subtitle: '统一配置默认模型、备用模型与路由规则。' },
  security: { title: '安全规则', subtitle: '下发统一治理规则并保留审计入口。' },
  delivery: { title: '下发管理', subtitle: '查看配置和能力向客户端下发的状态。' },
  usage: { title: '使用情况', subtitle: '跟踪数字员工使用量和趋势变化。' },
};

export default function App() {
  const [activeTab, setActiveTab] = useState<CenterTab>('overview');

  const content = useMemo(() => {
    switch (activeTab) {
      case 'employees':
        return <EmployeesPage />;
      case 'models':
        return <ModelRoutingPage />;
      case 'overview':
        return <OverviewPage />;
      case 'communications':
        return <CommunicationsPage />;
      case 'workflows':
        return <WorkflowsPage />;
      case 'knowledge':
        return <KnowledgePage />;
      case 'packages':
        return <PackagesPage />;
      case 'security':
        return <SecurityPage />;
      case 'delivery':
        return <DeliveryPage />;
      case 'usage':
        return <UsagePage />;
      default:
        return null;
    }
  }, [activeTab]);

  return (
    <div className="center-shell">
      <SideNav activeTab={activeTab} onChange={setActiveTab} />
      <main className="center-main">
        <TopHeader title={meta[activeTab].title} subtitle={meta[activeTab].subtitle} />
        {content}
      </main>
    </div>
  );
}

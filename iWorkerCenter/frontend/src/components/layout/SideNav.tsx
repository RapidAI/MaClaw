import { useI18n } from '../../i18n';
import type { CenterTab } from '../../types';

type NavItem = { id: CenterTab; label: { zh: string; en: string }; hint: { zh: string; en: string } };
type Props = { activeTab: CenterTab; onChange: (tab: CenterTab) => void };

const items: NavItem[] = [
  { id: 'overview', label: { zh: '总览', en: 'Overview' }, hint: { zh: '运行概览与告警', en: 'Runtime and alerts' } },
  { id: 'bootstrap', label: { zh: '单位初始化', en: 'Bootstrap' }, hint: { zh: '新租户启动向导', en: 'Tenant setup wizard' } },
  { id: 'employees', label: { zh: '数字员工', en: 'iWorkers' }, hint: { zh: '身份、角色与策略', en: 'Identity, roles, policies' } },
  { id: 'communications', label: { zh: '员工通讯', en: 'Communications' }, hint: { zh: '协作记录与请求流转', en: 'Collaboration and handoffs' } },
  { id: 'workflows', label: { zh: '流程设计', en: 'Workflows' }, hint: { zh: '编排任务流转', en: 'Task orchestration' } },
  { id: 'knowledge', label: { zh: '经验共享', en: 'Knowledge' }, hint: { zh: '经验沉淀与复用', en: 'Knowledge reuse' } },
  { id: 'packages', label: { zh: '能力包', en: 'Packages' }, hint: { zh: '技能与 MCP 下发', en: 'Skill and MCP delivery' } },
  { id: 'models', label: { zh: '模型调度', en: 'Models' }, hint: { zh: '模型策略与路由', en: 'Model policy and routing' } },
  { id: 'cloud', label: { zh: '云端注册', en: 'Cloud Registration' }, hint: { zh: '连接 iWorkerCloud', en: 'Connect iWorkerCloud' } },
  { id: 'security', label: { zh: '安全规则', en: 'Security' }, hint: { zh: '统一治理与审计', en: 'Governance and audit' } },
  { id: 'delivery', label: { zh: '下发管理', en: 'Delivery' }, hint: { zh: '配置和能力下发', en: 'Configuration delivery' } },
  { id: 'usage', label: { zh: '使用情况', en: 'Usage' }, hint: { zh: '统计与趋势', en: 'Metrics and trends' } },
  { id: 'im', label: { zh: 'IM 管理', en: 'IM Settings' }, hint: { zh: '飞书/钉钉/企微', en: 'Feishu/DingTalk/WeCom' } },
  { id: 'auth', label: { zh: '认证管理', en: 'Authentication' }, hint: { zh: 'LDAP/本地/OIDC', en: 'LDAP/local/OIDC' } },
  { id: 'settings', label: { zh: '账号设置', en: 'Account Settings' }, hint: { zh: '资料、密码与租户模式', en: 'Profile, password, tenancy' } },
];

export function SideNav({ activeTab, onChange }: Props) {
  const { t } = useI18n();
  return (
    <aside className="center-side">
      <div className="center-brand">
        <div className="mini">iWorkerCenter</div>
        <h1>{t('数字员工中心', 'Digital Workforce Center')}</h1>
        <p>{t('组织管理、协作观察、能力分发与规则控制台。', 'Console for organization management, collaboration, capability delivery, and governance.')}</p>
      </div>
      <nav className="center-nav">
        {items.map((item) => (
          <button key={item.id} type="button" className={item.id === activeTab ? 'active' : ''} onClick={() => onChange(item.id)}>
            <span>{t(item.label.zh, item.label.en)}</span>
            <small>{t(item.hint.zh, item.hint.en)}</small>
          </button>
        ))}
      </nav>
      <div className="center-hint">
        <strong>{t('管理提示', 'Operator Note')}</strong>
        <p>{t('Cloud 只负责注册、授权、算力和能力市场协调；企业业务、员工协作和 MCP 下发由本 Center 本地管理。', 'Cloud only coordinates registration, authorization, compute, and capability marketplace services. Business workflows, employee collaboration, and MCP delivery remain managed locally by this Center.')}</p>
      </div>
    </aside>
  );
}

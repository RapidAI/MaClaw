import type { CenterTab } from '../../types';

type NavItem = { id: CenterTab; label: string; hint: string };
type Props = { activeTab: CenterTab; onChange: (tab: CenterTab) => void };

const items: NavItem[] = [
  { id: 'overview', label: '总览', hint: '运行概览与告警' },
  { id: 'bootstrap', label: '单位初始化', hint: '新租户启动计划' },
  { id: 'employees', label: '数字员工', hint: '身份、角色与策略' },
  { id: 'communications', label: '员工通讯', hint: '协作记录与请求流转' },
  { id: 'groupDiscussion', label: '群组讨论', hint: 'MaClaw 专家会诊' },
  { id: 'workflows', label: '流程设计', hint: '编排任务流转' },
  { id: 'knowledge', label: '经验共享', hint: '经验沉淀与复用' },
  { id: 'packages', label: '能力包', hint: '技能与 MCP 下发' },
  { id: 'models', label: '模型调度', hint: '模型策略与路由' },
  { id: 'cloud', label: '云端注册', hint: '连接 iWorkerCloud' },
  { id: 'security', label: '安全规则', hint: '统一治理与审计' },
  { id: 'delivery', label: '下发管理', hint: '配置和能力下发' },
  { id: 'usage', label: '使用情况', hint: '统计与趋势' },
  { id: 'im', label: 'IM 管理', hint: '飞书/钉钉/企微' },
  { id: 'auth', label: '认证管理', hint: 'LDAP/本地/OIDC' },
  { id: 'settings', label: '账号设置', hint: '资料、密码与租户模式' },
];

export function SideNav({ activeTab, onChange }: Props) {
  return (
    <aside className="center-side">
      <div className="center-brand">
        <div className="mini">iWorkerCenter</div>
        <h1>数字员工中心</h1>
        <p>组织管理、协作观察、能力分发与规则控制台。</p>
      </div>
      <nav className="center-nav">
        {items.map((item) => (
          <button key={item.id} type="button" className={item.id === activeTab ? 'active' : ''} onClick={() => onChange(item.id)}>
            <span>{item.label}</span>
            <small>{item.hint}</small>
          </button>
        ))}
      </nav>
      <div className="center-hint">
        <strong>管理提示</strong>
        <p>Cloud 只负责注册、授权、算力和能力市场协调；企业业务、员工协作和 MCP 下发由本 Center 本地管理。</p>
      </div>
    </aside>
  );
}

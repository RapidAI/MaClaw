import type { CenterTab } from '../../types';

type NavItem = {
  id: CenterTab;
  label: string;
  hint: string;
};

type Props = {
  activeTab: CenterTab;
  onChange: (tab: CenterTab) => void;
};

const items: NavItem[] = [
  { id: 'overview', label: '总览', hint: '运行概览和告警' },
  { id: 'employees', label: '数字员工', hint: '人员与角色配置' },
  { id: 'communications', label: '员工通讯', hint: '查看协作记录' },
  { id: 'workflows', label: '流程设计', hint: '编排事务流转' },
  { id: 'knowledge', label: '经验共享', hint: '经验沉淀与复用' },
  { id: 'packages', label: '能力包', hint: '管理能力分发' },
  { id: 'models', label: '模型调度', hint: '模型策略与路由' },
  { id: 'security', label: '安全规则', hint: '统一治理和审计' },
  { id: 'delivery', label: '下发管理', hint: '查看下发状态' },
  { id: 'usage', label: '使用情况', hint: '统计和趋势' },
];

export function SideNav({ activeTab, onChange }: Props) {
  return (
    <aside className="center-side">
      <div className="center-brand">
        <div className="mini">iWokerCenter</div>
        <h1>数字员工中心</h1>
        <p>组织管理、协作观察、能力分发与规则控制台。</p>
      </div>
      <nav className="center-nav">
        {items.map((item) => (
          <button
            key={item.id}
            type="button"
            className={item.id === activeTab ? 'active' : ''}
            onClick={() => onChange(item.id)}
          >
            <span>{item.label}</span>
            <small>{item.hint}</small>
          </button>
        ))}
      </nav>
      <div className="center-hint">
        <strong>管理提示</strong>
        <p>第一阶段先搭好管理台骨架，后续逐页接真实数据与操作。</p>
      </div>
    </aside>
  );
}

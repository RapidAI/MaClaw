import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';

const rows = [
  { rule: '跨范围协作审批', scope: '全中心', hits: '12', status: '生效中' },
  { rule: '敏感输出审查', scope: '办公同事', hits: '6', status: '生效中' },
  { rule: '能力包风险拦截', scope: '能力包管理', hits: '2', status: '待复核' },
];

const reviews = [
  { title: '敏感输出审查最近命中 6 次', desc: '主要集中在办公同事的外发内容', tag: '重点关注' },
  { title: '能力包风险拦截待复核', desc: '当前仍有 2 条记录等待管理员确认', tag: '待处理' },
];

const governance = [
  { title: '跨范围协作审批继续维持全中心生效', desc: '当前作为统一兜底规则，保障跨角色协作前先完成确认', tag: '持续生效' },
  { title: '敏感输出审查准备补充复核记录留痕', desc: '后续可继续接入更完整的审计链路和处理结果回看', tag: '待补齐' },
];

export function SecurityPage() {
  return (
    <div className="center-page-stack">
      <SectionCard title="安全规则" desc="统一查看规则列表、命中情况和审批状态。">
        <DataTable
          columns={[
            { key: 'rule', label: '规则名称' },
            { key: 'scope', label: '作用范围' },
            { key: 'hits', label: '最近命中次数' },
            { key: 'status', label: '状态' },
          ]}
          rows={rows}
        />
      </SectionCard>
      <SectionCard title="最近复核" desc="补充安全规则命中后的复核重点和待处理事项。">
        <div className="item-list">
          {reviews.map((item) => (
            <div key={item.title} className="item-row">
              <strong>{item.title}</strong>
              <p>{item.desc}</p>
              <span className="badge warn">{item.tag}</span>
            </div>
          ))}
        </div>
      </SectionCard>
      <SectionCard title="治理计划" desc="补充当前持续生效的治理重点和后续待补链路。">
        <div className="item-list">
          {governance.map((item) => (
            <div key={item.title} className="item-row">
              <strong>{item.title}</strong>
              <p>{item.desc}</p>
              <span className="badge info">{item.tag}</span>
            </div>
          ))}
        </div>
      </SectionCard>
    </div>
  );
}

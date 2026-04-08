import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';

const rows = [
  { topic: '异常复盘模板', source: '生产团队', updated: '今天 09:20', status: '已共享' },
  { topic: '周报写作范式', source: '办公同事', updated: '昨天 18:40', status: '审核中' },
  { topic: '质量分析口径', source: '质量团队', updated: '周一 14:10', status: '已引用' },
];

const references = [
  { title: '办公同事引用周报写作范式', desc: '最近 24 小时被复用 8 次', tag: '高频复用' },
  { title: '生产同事使用异常复盘模板', desc: '已形成标准输出格式', tag: '已沉淀' },
];

export function KnowledgePage() {
  return (
    <div className="center-page-stack">
      <SectionCard title="经验共享" desc="沉淀经验主题、来源和当前复用状态。">
        <DataTable
          columns={[
            { key: 'topic', label: '经验主题' },
            { key: 'source', label: '来源' },
            { key: 'updated', label: '最近更新' },
            { key: 'status', label: '状态' },
          ]}
          rows={rows}
        />
      </SectionCard>
      <SectionCard title="最近复用" desc="查看经验内容如何被不同数字员工引用。">
        <div className="item-list">
          {references.map((item) => (
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

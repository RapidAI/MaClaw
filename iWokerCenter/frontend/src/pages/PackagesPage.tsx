import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';

const rows = [
  { name: '周报汇总', source: '系统内置', version: 'v0.9', status: '已上架' },
  { name: '异常归档', source: '运营维护', version: 'v0.6', status: '待审核' },
  { name: '质量分析', source: '质量团队', version: 'v1.1', status: '已下发' },
];

const bindings = [
  { title: '周报汇总已绑定办公同事', desc: '当前作为高频任务默认能力包，最近 7 天调用最稳定', tag: '已生效' },
  { title: '异常归档等待审核后上架', desc: '审核通过后将补充绑定到生产同事和质检同事', tag: '待处理' },
];

export function PackagesPage() {
  return (
    <div className="center-page-stack">
      <SectionCard title="能力包列表" desc="管理能力包来源、版本、审核状态和绑定关系。">
        <DataTable
          columns={[
            { key: 'name', label: '能力包名称' },
            { key: 'source', label: '来源' },
            { key: 'version', label: '版本' },
            { key: 'status', label: '状态' },
          ]}
          rows={rows}
        />
      </SectionCard>
      <SectionCard title="绑定与审核观察" desc="补充能力包当前绑定对象和审核进展。">
        <div className="item-list">
          {bindings.map((item) => (
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

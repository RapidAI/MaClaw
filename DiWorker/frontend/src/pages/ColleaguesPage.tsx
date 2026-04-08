import { colleagues } from '../mock/colleagues';
import { ColleagueCard } from '../components/home/ColleagueCard';

type Props = {
  selectedColleagueName: string;
  onPickColleagueTask: (task: string, colleagueName: string) => void;
};

const quickMatches = [
  { task: '写通知', owner: '小迪' },
  { task: '汇总数据', owner: '阿宁' },
  { task: '异常说明', owner: '老陈' },
];

export function ColleaguesPage({ selectedColleagueName, onPickColleagueTask }: Props) {
  return (
    <div className="dw-page-stack">
      <section className="card dw-page-panel">
        <div className="dw-panel-header">
          <div>
            <span className="eyebrow">找同事</span>
            <h2>找同事</h2>
          </div>
          <small>{selectedColleagueName ? `当前已选：${selectedColleagueName}` : '按名字、角色和会做的事帮助你快速找到人'}</small>
        </div>
        <div className="dw-task-layout">
          <div className="dw-task-main">
            <div className="card-subtle dw-editor-section">
              <div className="section-head with-gap">
                <div>
                  <span className="eyebrow">成员列表</span>
                  <h3>可协作同事</h3>
                </div>
                <small>从成员面板里选定协作对象后，可直接带入新建任务继续处理。</small>
              </div>
              <div className="dw-colleague-grid">
                {colleagues.map((colleague) => (
                  <ColleagueCard key={colleague.id} colleague={colleague} onSelect={() => onPickColleagueTask(colleague.tasks[0], colleague.name)} />
                ))}
              </div>
            </div>
          </div>
          <aside className="dw-task-side">
            <div className="card-subtle dw-side-panel-block">
              <label>当前状态</label>
              <strong>{selectedColleagueName || '暂未选择同事'}</strong>
              <p>从这里选中的同事会直接带到新建任务页继续处理。</p>
            </div>
            <div className="card-subtle dw-side-panel-block">
              <label>快速匹配</label>
              <div className="dw-handoff-list">
                {quickMatches.map((item) => (
                  <div key={item.task} className="dw-handoff-item">
                    <strong>{item.task}</strong>
                    <p>推荐同事：{item.owner}</p>
                  </div>
                ))}
              </div>
            </div>
            <div className="card-subtle dw-side-panel-block">
              <label>选择建议</label>
              <p>如果你已经知道任务目标，优先按任务类型选人；如果还不明确，可先随便选一位同事开始描述。</p>
            </div>
          </aside>
        </div>
      </section>
    </div>
  );
}

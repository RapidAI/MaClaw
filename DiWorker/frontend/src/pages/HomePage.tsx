import { colleagues } from '../mock/colleagues';
import { ColleagueCard } from '../components/home/ColleagueCard';
import { FocusPanel } from '../components/home/FocusPanel';
import { QuickInputComposer } from '../components/home/QuickInputComposer';
import { QuickTaskChips } from '../components/home/QuickTaskChips';
import { RecentTaskList } from '../components/home/RecentTaskList';
import { WelcomeHero } from '../components/home/WelcomeHero';
import { WorkbenchPreview } from '../components/home/WorkbenchPreview';
import { quickTasks } from '../mock/tasks';
import type { HistoryTaskItem } from '../types';

type Props = {
  draft: string;
  selectedTask: string;
  selectedColleagueName: string;
  recentTasks: HistoryTaskItem[];
  onDraftChange: (value: string) => void;
  onPickTask: (task: string, colleagueName?: string) => void;
  onOpenNewTask: () => void;
  onOpenRecentTask: (task: HistoryTaskItem) => void;
};

export function HomePage({ draft, selectedTask, selectedColleagueName, recentTasks, onDraftChange, onPickTask, onOpenNewTask, onOpenRecentTask }: Props) {
  return (
    <div className="dw-page-stack dw-home-page">
      <div className="dw-home-layout">
        <div className="dw-home-main">
          <WelcomeHero
            greeting="开始今天的任务处理"
            hint="从主页直接发起任务、继续最近记录，或先挑选合适的协作同事。"
            selectedTask={selectedTask}
            selectedColleagueName={selectedColleagueName}
          />
          <div className="dw-home-main-scroll">
            <QuickInputComposer
              value={draft}
              selectedTask={selectedTask}
              selectedColleagueName={selectedColleagueName}
              onChange={onDraftChange}
              onPickTask={onPickTask}
              onOpenNewTask={onOpenNewTask}
            />
            <div className="dw-home-main-lists">
              <QuickTaskChips tasks={quickTasks} onPick={onPickTask} />
              <RecentTaskList tasks={recentTasks} onOpenTask={onOpenRecentTask} />
            </div>
          </div>
        </div>
        <aside className="card dw-home-inspector">
          <div className="dw-home-inspector-scroll">
            <FocusPanel selectedTask={selectedTask} selectedColleagueName={selectedColleagueName} onOpenNewTask={onOpenNewTask} />
            <WorkbenchPreview selectedTask={selectedTask} draft={draft} />
            <div className="dw-inspector-section dw-colleague-panel-card-native">
              <div className="dw-inspector-rowhead">
                <div>
                  <h3>可直接分派</h3>
                  <p>{selectedColleagueName ? `当前已选：${selectedColleagueName}` : '从这里快速进入协作流程'}</p>
                </div>
              </div>
              <div className="dw-colleague-panel-list dw-colleague-panel-list-compact">
                {colleagues.map((colleague) => (
                  <ColleagueCard
                    key={colleague.id}
                    compact
                    isSelected={selectedColleagueName === colleague.name}
                    colleague={colleague}
                    onSelect={() => onPickTask(colleague.tasks[0], colleague.name)}
                  />
                ))}
              </div>
            </div>
          </div>
        </aside>
      </div>
    </div>
  );
}

import { useTranslation } from 'react-i18next';

type Props = {
  value: string;
  selectedTask: string;
  selectedColleagueName: string;
  onChange: (value: string) => void;
  onPickTask: (task: string) => void;
  onOpenNewTask: () => void;
};

const quickModes = [
  { key: 'report', label: ['汇报', 'Report'], task: '异常说明' },
  { key: 'brief', label: ['纪要', 'Brief'], task: '会议纪要' },
  { key: 'table', label: ['表格', 'Table'], task: '整理表格' },
  { key: 'notice', label: ['通知', 'Notice'], task: '写通知' },
];

const quickCategories = [
  { key: 'writeReport', title: ['写汇报', 'Write report'], task: '异常说明' },
  { key: 'meetingBrief', title: ['做纪要', 'Meeting brief'], task: '会议纪要' },
  { key: 'organizeTable', title: ['整理表格', 'Organize table'], task: '整理表格' },
];

export function QuickInputComposer({ value, selectedTask, selectedColleagueName, onChange, onPickTask, onOpenNewTask }: Props) {
  const { t } = useTranslation();
  const activeModeTask = quickModes.find((item) => item.task === selectedTask)?.task || '';
  const taskTitle = selectedTask || t('quickInput.directTask', 'Direct task input');
  const colleagueText = selectedColleagueName
    ? t('quickInput.selectedColleague', 'Selected: {{name}}', { name: selectedColleagueName })
    : t('quickInput.autoMatch', 'Auto match');

  return (
    <section className="card dw-composer dw-home-composer dw-home-composer-panel">
      <div className="dw-home-composer-strip">
        <div className="dw-home-mode-bar">
          {quickModes.map((item) => (
            <button
              key={item.key}
              type="button"
              className={`dw-home-mode-tab${activeModeTask === item.task ? ' is-active' : ''}`}
              onClick={() => onPickTask(item.task)}
            >
              {t(`quickInput.modes.${item.key}`, item.label[1])}
            </button>
          ))}
        </div>
        <span className="dw-toolbar-meta">{colleagueText}</span>
      </div>
      <div className="dw-home-composer-body">
        <div className="dw-home-composer-editor">
          <div className="dw-home-composer-prompt dw-home-composer-prompt-compact">
            <strong>{taskTitle}</strong>
            <span>{t('quickInput.promptDetail', 'Clarify the goal, scope, and expected result.')}</span>
          </div>
          <textarea
            value={value}
            onChange={(event) => onChange(event.target.value)}
            placeholder={t('quickInput.placeholder', 'Example: organize today\'s production exceptions and generate a short report summary.')}
            rows={6}
          />
        </div>
        <aside className="dw-home-composer-sidebar">
          <div className="dw-home-sidebar-inline">
            <label>{t('quickInput.quickBringIn', 'Quick bring-in')}</label>
            <div className="dw-home-composer-categories">
              {quickCategories.map((item) => (
                <button key={item.key} type="button" className="dw-home-category-button" onClick={() => onPickTask(item.task)}>
                  <strong>{t(`quickInput.categories.${item.key}`, item.title[1])}</strong>
                </button>
              ))}
            </div>
          </div>
          <div className="dw-home-sidebar-inline dw-home-sidebar-inline-status">
            <label>{t('quickInput.current', 'Current')}</label>
            <strong>{selectedTask || t('quickInput.unspecifiedTask', 'Unspecified')}</strong>
            <p>{selectedColleagueName ? t('quickInput.withColleague', 'With {{name}}', { name: selectedColleagueName }) : t('quickInput.noColleague', 'No colleague selected')}</p>
          </div>
        </aside>
      </div>
      <div className="dw-composer-actions dw-home-composer-actions">
        <button type="button" className="secondary" onClick={onOpenNewTask}>{t('quickInput.open', 'Open')}</button>
        <button type="button" className="primary" onClick={onOpenNewTask}>{t('quickInput.start', 'Start')}</button>
      </div>
    </section>
  );
}

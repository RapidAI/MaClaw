import { useState, useRef, useEffect } from 'react';
import { quickTasks as defaultQuickTasks } from '../mock/tasks';
import type { HistoryTaskItem } from '../types';
import { IconSearch, IconChevronRight } from '../components/layout/SidebarIcons';

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

type WorkMode = 'code' | 'office';

const skillChips = [
  { icon: '📄', label: '文档处理' },
  { icon: '🎬', label: '视频生成' },
  { icon: '🔍', label: '深度研究' },
  { icon: '💰', label: '金融服务' },
  { icon: '📊', label: '数据分析' },
  { icon: '📈', label: '数据可视化' },
  { icon: '📑', label: '幻灯片' },
  { icon: '📁', label: '产品管理' },
];

export function HomePage({ draft, selectedTask, selectedColleagueName, recentTasks, onDraftChange, onPickTask, onOpenNewTask }: Props) {
  const [workMode, setWorkMode] = useState<WorkMode>('office');
  const [quickTasks, setQuickTasks] = useState<string[]>(defaultQuickTasks);
  const chipScrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const welcomeLoader = (window as Window & {
      go?: {
        main?: {
          App?: {
            GetWelcomeData?: () => Promise<{ quick_tasks?: string[] }>;
          };
        };
      };
    }).go?.main?.App?.GetWelcomeData;

    if (!welcomeLoader) {
      return;
    }

    welcomeLoader()
      .then((data: { quick_tasks?: string[] }) => {
        if (data?.quick_tasks && data.quick_tasks.length > 0) {
          setQuickTasks(data.quick_tasks);
        }
      })
      .catch(() => {});
  }, []);

  const handleSubmit = () => {
    if (draft.trim()) {
      onOpenNewTask();
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSubmit();
    }
  };

  return (
    <div style={{
      display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center',
      height: '100%', padding: '40px 24px 24px', overflow: 'auto',
      background: '#f9fafb',
    }}>
      {/* Mascot / Logo area */}
      <div style={{ marginBottom: '20px', opacity: 0.85 }}>
        <div style={{
          width: '80px', height: '80px', borderRadius: '20px',
          background: '#f3f4f6', border: '1px solid #e5e7eb',
          display: 'flex', alignItems: 'center', justifyContent: 'center',
          fontSize: '36px', margin: '0 auto',
        }}>
          🤖
        </div>
      </div>

      {/* Slogan */}
      <h2 style={{ fontSize: '22px', fontWeight: 700, color: '#111827', margin: '0 0 6px', textAlign: 'center' }}>
        Claw Your Ideas Into Reality
      </h2>
      <p style={{ fontSize: '13px', color: '#9ca3af', margin: '0 0 24px', textAlign: 'center' }}>
        Triggered Anywhere, Completed Locally
      </p>

      {/* Mode toggle tabs */}
      <div style={{
        display: 'flex', alignItems: 'center', gap: '0',
        marginBottom: '20px',
        background: '#f3f4f6', borderRadius: '8px', padding: '3px',
        border: '1px solid #e5e7eb',
      }}>
        <span style={{ fontSize: '13px', color: '#6b7280', padding: '0 10px', fontWeight: 500 }}>开始</span>
        <button
          type="button"
          onClick={() => setWorkMode('code')}
          style={{
            display: 'flex', alignItems: 'center', gap: '6px',
            padding: '6px 14px', borderRadius: '6px', border: 'none',
            background: workMode === 'code' ? '#ffffff' : 'transparent',
            color: workMode === 'code' ? '#111827' : '#6b7280',
            fontWeight: 600, fontSize: '13px', cursor: 'pointer',
            boxShadow: workMode === 'code' ? '0 1px 2px rgba(0,0,0,0.06)' : 'none',
          }}
        >
          <span style={{ fontSize: '14px' }}>{'<>'}</span> 代码开发
        </button>
        <button
          type="button"
          onClick={() => setWorkMode('office')}
          style={{
            display: 'flex', alignItems: 'center', gap: '6px',
            padding: '6px 14px', borderRadius: '6px', border: 'none',
            background: workMode === 'office' ? '#1f2937' : 'transparent',
            color: workMode === 'office' ? '#ffffff' : '#6b7280',
            fontWeight: 600, fontSize: '13px', cursor: 'pointer',
            boxShadow: workMode === 'office' ? '0 1px 3px rgba(0,0,0,0.12)' : 'none',
          }}
        >
          <span style={{ fontSize: '14px' }}>💬</span> 日常办公
        </button>
        <span style={{ fontSize: '13px', color: '#6b7280', padding: '0 10px', fontWeight: 500 }}>任务</span>
      </div>

      {/* Skill category chips — horizontal scrollable */}
      <div style={{ position: 'relative', width: '100%', maxWidth: '640px', marginBottom: '24px' }}>
        <div
          ref={chipScrollRef}
          style={{
            display: 'flex', gap: '8px', overflowX: 'auto', padding: '2px 4px',
            scrollbarWidth: 'none', msOverflowStyle: 'none',
          }}
        >
          {(workMode === 'office' ? skillChips : quickTasks.map((t) => ({ icon: '⚡', label: t }))).map((chip) => (
            <button
              key={chip.label}
              type="button"
              onClick={() => onPickTask(chip.label)}
              style={{
                display: 'flex', alignItems: 'center', gap: '5px',
                padding: '6px 12px', borderRadius: '16px',
                border: '1px solid #e5e7eb', background: '#ffffff',
                color: '#374151', fontSize: '12px', fontWeight: 500,
                whiteSpace: 'nowrap', cursor: 'pointer', flexShrink: 0,
                transition: 'border-color 0.15s, background 0.15s',
              }}
              onMouseEnter={(e) => { e.currentTarget.style.borderColor = '#d1d5db'; e.currentTarget.style.background = '#f9fafb'; }}
              onMouseLeave={(e) => { e.currentTarget.style.borderColor = '#e5e7eb'; e.currentTarget.style.background = '#ffffff'; }}
            >
              <span>{chip.icon}</span> {chip.label}
            </button>
          ))}
        </div>
        {/* Scroll hint arrow */}
        <div style={{
          position: 'absolute', right: 0, top: 0, bottom: 0, width: '32px',
          background: 'linear-gradient(90deg, transparent, #f9fafb)',
          display: 'flex', alignItems: 'center', justifyContent: 'flex-end',
          pointerEvents: 'none',
        }}>
          <span style={{ color: '#9ca3af', pointerEvents: 'auto', cursor: 'pointer' }}
            onClick={() => chipScrollRef.current?.scrollBy({ left: 120, behavior: 'smooth' })}
          >
            <IconChevronRight style={{ width: '16px', height: '16px' }} />
          </span>
        </div>
      </div>

      {/* Input area */}
      <div style={{
        width: '100%', maxWidth: '640px',
        background: '#ffffff', borderRadius: '12px',
        border: '1px solid #e5e7eb',
        boxShadow: '0 1px 3px rgba(0,0,0,0.04)',
        overflow: 'hidden',
      }}>
        {/* Toolbar icons */}
        <div style={{
          display: 'flex', alignItems: 'center', gap: '8px',
          padding: '8px 12px 0',
          color: '#9ca3af', fontSize: '14px',
        }}>
          <span style={{ cursor: 'pointer' }} title="链接">🔗</span>
          <span style={{ cursor: 'pointer' }} title="附件">📎</span>
        </div>

        {/* Text input */}
        <textarea
          value={draft}
          onChange={(e) => onDraftChange(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder="输入问题..."
          rows={3}
          style={{
            width: '100%', border: 'none', outline: 'none', resize: 'none',
            padding: '8px 12px 12px', fontSize: '14px', lineHeight: '1.6',
            color: '#1f2937', background: 'transparent',
            fontFamily: 'inherit',
          }}
        />

        {/* Bottom bar */}
        <div style={{
          display: 'flex', alignItems: 'center', justifyContent: 'space-between',
          padding: '6px 12px 8px', borderTop: '1px solid #f3f4f6',
        }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
            <button type="button" style={{
              display: 'flex', alignItems: 'center', gap: '4px',
              padding: '4px 10px', borderRadius: '6px',
              border: '1px solid #e5e7eb', background: '#f9fafb',
              color: '#374151', fontSize: '12px', fontWeight: 600, cursor: 'pointer',
            }}>
              📦 Craft <span style={{ fontSize: '10px', color: '#9ca3af' }}>▾</span>
            </button>
            <button type="button" style={{
              display: 'flex', alignItems: 'center', gap: '4px',
              padding: '4px 10px', borderRadius: '6px',
              border: 'none', background: 'transparent',
              color: '#6b7280', fontSize: '12px', fontWeight: 500, cursor: 'pointer',
            }}>
              ✂️ Skills
            </button>
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
            <button type="button" style={{
              display: 'flex', alignItems: 'center', gap: '4px',
              padding: '4px 10px', borderRadius: '6px',
              border: 'none', background: 'transparent',
              color: '#9ca3af', fontSize: '12px', cursor: 'pointer',
            }}>
              📁 选择文件夹 <span style={{ fontSize: '10px' }}>▾</span>
            </button>
            <span style={{ color: '#d1d5db', fontSize: '16px' }}>⚠️</span>
          </div>
        </div>
      </div>

      {/* Footer hint */}
      <p style={{ fontSize: '11px', color: '#d1d5db', marginTop: '12px', textAlign: 'center' }}>
        内容由 AI 生成，请核实重要信息。
      </p>
    </div>
  );
}

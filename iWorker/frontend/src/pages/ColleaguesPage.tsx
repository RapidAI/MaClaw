import { useState, useEffect } from 'react';
import { colleagues as mockColleagues } from '../mock/colleagues';
import type { Colleague } from '../types';

type Props = {
  selectedColleagueName: string;
  onPickColleagueTask: (task: string, colleagueName: string) => void;
};

const hasWails = () => typeof window !== 'undefined' && typeof (window as Window & { go?: unknown }).go !== 'undefined';

function buildCategories(list: Colleague[]) {
  return [
    { label: '全部', count: list.length },
    { label: '办公', count: list.filter((c) => c.role.includes('办公')).length },
    { label: '数据', count: list.filter((c) => c.role.includes('数据')).length },
    { label: '生产', count: list.filter((c) => c.role.includes('生产')).length },
    { label: '质量', count: list.filter((c) => c.role.includes('质量')).length },
  ];
}

const avatarColors = ['#f3e8ff', '#fef3c7', '#dbeafe', '#dcfce7', '#fce7f3', '#e0e7ff'];
const roleColors = ['#7c3aed', '#d97706', '#2563eb', '#16a34a', '#db2777', '#4f46e5'];

export function ColleaguesPage({ selectedColleagueName, onPickColleagueTask }: Props) {
  const [colleagues, setColleagues] = useState<Colleague[]>(mockColleagues);
  const [activeCategory, setActiveCategory] = useState('全部');

  useEffect(() => {
    if (!hasWails()) return;
    (window as any).go.main.App.FetchColleagues()
      .then((cols: Colleague[]) => {
        if (Array.isArray(cols) && cols.length > 0) {
          setColleagues(cols);
        }
      })
      .catch(() => {});
  }, []);

  const categories = buildCategories(colleagues);

  const filtered = activeCategory === '全部'
    ? colleagues
    : colleagues.filter((c) => c.role.includes(activeCategory));

  return (
    <div style={{ display: 'flex', gap: '16px', height: '100%', overflow: 'hidden' }}>
      {/* Main content */}
      <div style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
        {/* Header */}
        <div style={{ marginBottom: '16px', flexShrink: 0 }}>
          <h2 style={{ fontSize: '18px', fontWeight: 700, color: '#111827', margin: '0 0 4px' }}>同事中心</h2>
          <p style={{ fontSize: '13px', color: '#9ca3af', margin: 0 }}>按分类浏览同事，召唤他们为你服务</p>
        </div>

        {/* Category tabs */}
        <div style={{ display: 'flex', gap: '0', marginBottom: '16px', flexShrink: 0, borderBottom: '2px solid #f3f4f6' }}>
          {categories.map((cat) => (
            <button
              key={cat.label}
              type="button"
              onClick={() => setActiveCategory(cat.label)}
              style={{
                padding: '8px 16px', border: 'none', background: 'transparent',
                fontSize: '13px', fontWeight: activeCategory === cat.label ? 700 : 500,
                color: activeCategory === cat.label ? '#6366f1' : '#6b7280',
                borderBottom: activeCategory === cat.label ? '2px solid #6366f1' : '2px solid transparent',
                marginBottom: '-2px', cursor: 'pointer',
                transition: 'color 0.15s, border-color 0.15s',
              }}
            >
              {cat.label}{cat.count > 0 ? ` (${cat.count})` : ''}
            </button>
          ))}
        </div>

        {/* Card grid */}
        <div style={{
          flex: 1, overflow: 'auto', paddingRight: '4px',
        }}>
          <div style={{
            display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))',
            gap: '12px',
          }}>
            {filtered.map((colleague, idx) => {
              const bgColor = avatarColors[idx % avatarColors.length];
              const textColor = roleColors[idx % roleColors.length];
              return (
                <div
                  key={colleague.id}
                  style={{
                    background: '#ffffff', borderRadius: '12px',
                    border: selectedColleagueName === colleague.name ? '2px solid #6366f1' : '1px solid #e5e7eb',
                    padding: '20px', textAlign: 'center',
                    cursor: 'pointer',
                    transition: 'border-color 0.15s, box-shadow 0.15s',
                  }}
                  onClick={() => onPickColleagueTask(colleague.tasks[0], colleague.name)}
                  onMouseEnter={(e) => { e.currentTarget.style.boxShadow = '0 2px 8px rgba(0,0,0,0.06)'; }}
                  onMouseLeave={(e) => { e.currentTarget.style.boxShadow = 'none'; }}
                >
                  {/* Avatar */}
                  <div style={{
                    width: '64px', height: '64px', borderRadius: '50%',
                    background: bgColor, margin: '0 auto 10px',
                    display: 'flex', alignItems: 'center', justifyContent: 'center',
                    fontSize: '24px', fontWeight: 800, color: textColor,
                    border: `3px solid ${bgColor}`,
                    boxShadow: `0 0 0 2px ${textColor}20`,
                  }}>
                    {colleague.name.charAt(0)}
                  </div>

                  {/* Name */}
                  <div style={{ fontSize: '15px', fontWeight: 700, color: '#111827', marginBottom: '4px' }}>
                    {colleague.name}
                  </div>

                  {/* Role badge */}
                  <div style={{
                    display: 'inline-block', padding: '2px 10px', borderRadius: '12px',
                    background: `${textColor}10`, color: textColor,
                    fontSize: '12px', fontWeight: 600, marginBottom: '8px',
                  }}>
                    {colleague.role}
                  </div>

                  {/* Description */}
                  <p style={{
                    fontSize: '12px', color: '#6b7280', margin: '0',
                    lineHeight: '1.5', maxWidth: '200px', marginLeft: 'auto', marginRight: 'auto',
                  }}>
                    {colleague.description}
                  </p>
                </div>
              );
            })}
          </div>
        </div>
      </div>

      {/* Right sidebar — ranking / info */}
      <aside style={{
        width: '220px', flexShrink: 0,
        display: 'flex', flexDirection: 'column', gap: '12px',
      }}>
        {/* Ranking card */}
        <div style={{
          background: '#faf5ff', borderRadius: '12px', border: '1px solid #e9d5ff',
          padding: '16px',
        }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '8px' }}>
            <span style={{ fontSize: '18px' }}>🏆</span>
            <strong style={{ fontSize: '14px', color: '#7c3aed' }}>同事排行榜</strong>
          </div>
          <p style={{ fontSize: '12px', color: '#a78bfa', margin: 0 }}>
            基于使用频率与好评率排名
          </p>
        </div>

        {/* Ranking list */}
        <div style={{
          background: '#ffffff', borderRadius: '12px', border: '1px solid #e5e7eb',
          padding: '12px', flex: 1,
        }}>
          {colleagues.slice(0, 4).map((c, i) => (
            <div
              key={c.id}
              style={{
                display: 'flex', alignItems: 'center', gap: '8px',
                padding: '6px 4px',
                borderBottom: i < 3 ? '1px solid #f3f4f6' : 'none',
              }}
            >
              <span style={{
                width: '20px', height: '20px', borderRadius: '50%',
                background: i === 0 ? '#fbbf24' : i === 1 ? '#d1d5db' : i === 2 ? '#f59e0b' : '#e5e7eb',
                display: 'flex', alignItems: 'center', justifyContent: 'center',
                fontSize: '10px', fontWeight: 700, color: i < 3 ? '#fff' : '#6b7280',
                flexShrink: 0,
              }}>
                {i + 1}
              </span>
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ fontSize: '12px', fontWeight: 600, color: '#111827' }}>{c.name}</div>
                <div style={{ fontSize: '10px', color: '#9ca3af' }}>{c.role}</div>
              </div>
            </div>
          ))}
        </div>
      </aside>
    </div>
  );
}

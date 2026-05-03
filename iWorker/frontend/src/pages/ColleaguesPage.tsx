import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { colleagues as fallbackColleagues } from '../mock/colleagues';
import type { Colleague } from '../types';

type Props = {
  selectedColleagueName: string;
  onPickColleagueTask: (task: string, colleagueName: string) => void;
};

type Category = 'all' | 'office' | 'data' | 'operations' | 'quality';
type LoadState = 'loading' | 'center' | 'fallback' | 'empty';

const hasWails = () => typeof window !== 'undefined' && typeof (window as Window & { go?: unknown }).go !== 'undefined';

const categories: Array<{ id: Category; tokens: string[]; labelKey: string }> = [
  { id: 'all', tokens: [], labelKey: 'colleaguesPage.categories.all' },
  { id: 'office', tokens: ['office', 'admin', 'assistant', 'writing'], labelKey: 'colleaguesPage.categories.office' },
  { id: 'data', tokens: ['data', 'table', 'analysis'], labelKey: 'colleaguesPage.categories.data' },
  { id: 'operations', tokens: ['ops', 'operation', 'production', 'delivery'], labelKey: 'colleaguesPage.categories.operations' },
  { id: 'quality', tokens: ['quality', 'qa', 'risk'], labelKey: 'colleaguesPage.categories.quality' },
];

const palette = [
  { bg: '#f3e8ff', text: '#7c3aed' },
  { bg: '#fef3c7', text: '#d97706' },
  { bg: '#dbeafe', text: '#2563eb' },
  { bg: '#dcfce7', text: '#16a34a' },
  { bg: '#fce7f3', text: '#db2777' },
  { bg: '#e0e7ff', text: '#4f46e5' },
];

const normalize = (value: string) => value.toLowerCase().replace(/[_-]/g, ' ');
const taskLabel = (colleague: Colleague) => colleague.tasks?.[0] || colleague.strengths?.[0] || colleague.description || colleague.role;

function matchesCategory(colleague: Colleague, category: Category) {
  if (category === 'all') return true;
  const config = categories.find(item => item.id === category);
  if (!config) return true;
  const haystack = normalize([colleague.name, colleague.role, colleague.description, ...(colleague.strengths || []), ...(colleague.tasks || [])].join(' '));
  return config.tokens.some(token => haystack.includes(token));
}

function categoryCount(list: Colleague[], category: Category) {
  return category === 'all' ? list.length : list.filter(item => matchesCategory(item, category)).length;
}

export function ColleaguesPage({ selectedColleagueName, onPickColleagueTask }: Props) {
  const { t } = useTranslation();
  const [colleagues, setColleagues] = useState<Colleague[]>(fallbackColleagues);
  const [activeCategory, setActiveCategory] = useState<Category>('all');
  const [loadState, setLoadState] = useState<LoadState>('fallback');

  const loadColleagues = async () => {
    if (!hasWails()) {
      setLoadState('fallback');
      return;
    }
    setLoadState('loading');
    try {
      const cols = await (window as any).go.main.App.FetchColleagues();
      if (Array.isArray(cols) && cols.length > 0) {
        setColleagues(cols);
        setLoadState('center');
        return;
      }
      setColleagues([]);
      setLoadState('empty');
    } catch {
      setColleagues(fallbackColleagues);
      setLoadState('fallback');
    }
  };

  useEffect(() => { void loadColleagues(); }, []);

  const filtered = useMemo(() => colleagues.filter((item) => matchesCategory(item, activeCategory)), [activeCategory, colleagues]);
  const topColleagues = useMemo(() => colleagues.slice(0, 4), [colleagues]);
  const statusText = loadState === 'center'
    ? t('colleaguesPage.sourceCenter')
    : loadState === 'loading'
      ? t('colleaguesPage.loading')
      : loadState === 'empty'
        ? t('colleaguesPage.noCenterWorkers')
        : t('colleaguesPage.sourceFallback');

  return (
    <div className="iw-colleague-page">
      <main className="iw-colleague-main">
        <header className="iw-colleague-header">
          <div>
            <h2>{t('colleaguesPage.title')}</h2>
            <p>{t('colleaguesPage.subtitle')}</p>
          </div>
          <button type="button" onClick={() => { void loadColleagues(); }} disabled={loadState === 'loading'}>{loadState === 'loading' ? t('colleaguesPage.loading') : t('colleaguesPage.refresh')}</button>
        </header>

        <div className="iw-colleague-tabs" role="tablist" aria-label={t('colleaguesPage.categoryAria')}>
          {categories.map((cat) => (
            <button key={cat.id} type="button" role="tab" aria-selected={activeCategory === cat.id} className={activeCategory === cat.id ? 'is-active' : ''} onClick={() => setActiveCategory(cat.id)}>
              {t(cat.labelKey)}{categoryCount(colleagues, cat.id) > 0 ? ' (' + categoryCount(colleagues, cat.id) + ')' : ''}
            </button>
          ))}
        </div>

        <section className="iw-colleague-grid" aria-label={t('colleaguesPage.gridAria')}>
          {filtered.length === 0 ? <div className="iw-colleague-empty">{t('colleaguesPage.empty')}</div> : filtered.map((colleague, index) => {
            const colors = palette[index % palette.length];
            return (
              <button key={colleague.id} type="button" className={selectedColleagueName === colleague.name ? 'iw-colleague-card is-selected' : 'iw-colleague-card'} onClick={() => onPickColleagueTask(taskLabel(colleague), colleague.name)}>
                <span className="iw-colleague-avatar" style={{ background: colors.bg, color: colors.text, boxShadow: '0 0 0 2px ' + colors.text + '22' }}>{colleague.name.charAt(0).toUpperCase()}</span>
                <strong>{colleague.name}</strong>
                <em style={{ background: colors.text + '12', color: colors.text }}>{colleague.role}</em>
                <p>{colleague.description}</p>
                <small>{taskLabel(colleague)}</small>
              </button>
            );
          })}
        </section>
      </main>

      <aside className="iw-colleague-side">
        <section className="iw-colleague-insight">
          <span>{t('colleaguesPage.source')}</span>
          <strong>{statusText}</strong>
          <p>{t('colleaguesPage.sourceHint')}</p>
        </section>
        <section className="iw-colleague-insight is-list">
          <span>{t('colleaguesPage.quickCall')}</span>
          {topColleagues.length === 0 ? <p>{t('colleaguesPage.empty')}</p> : topColleagues.map((colleague, index) => (
            <button key={colleague.id} type="button" onClick={() => onPickColleagueTask(taskLabel(colleague), colleague.name)}>
              <b>{index + 1}</b>
              <span><strong>{colleague.name}</strong><small>{colleague.role}</small></span>
            </button>
          ))}
        </section>
      </aside>
    </div>
  );
}

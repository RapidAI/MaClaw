import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';
import { listMemories, type Memory } from '../api/memories';
import type { AssetNavigationTarget, CenterTab, OverviewNavigationTarget } from '../types';

type KnowledgePageProps = {
  navigationTarget?: AssetNavigationTarget | null;
  onNavigationHandled?: () => void;
  onNavigateToOverview?: (target?: OverviewNavigationTarget | null) => void;
  onNavigateToTab: (tab: CenterTab, target?: AssetNavigationTarget) => void;
};

type AssetStageCard = {
  id: 'knowledge' | 'packages' | 'workflows';
  eyebrow: string;
  title: string;
  tone: 'ok' | 'info' | 'warn';
  summary: string;
  detail: string;
  statLine: string[];
  actionLabel: string;
};

export function KnowledgePage({ navigationTarget, onNavigationHandled, onNavigateToOverview, onNavigateToTab }: KnowledgePageProps) {
  const { t } = useTranslation();
  const [items, setItems] = useState<Memory[]>([]);
  const [loading, setLoading] = useState(true);
  const [focusedTarget, setFocusedTarget] = useState<AssetNavigationTarget | null>(null);

  useEffect(() => {
    listMemories()
      .then((nextItems) => {
        setItems(nextItems);
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    if (!navigationTarget) {
      return;
    }
    setFocusedTarget(navigationTarget);
    onNavigationHandled?.();
  }, [navigationTarget, onNavigationHandled]);

  const filteredItems = useMemo(() => {
    if (!focusedTarget) {
      return items;
    }
    return items.filter((item) => {
      if (focusedTarget.draft_id && item.id === focusedTarget.draft_id) {
        return true;
      }
      if (focusedTarget.draft_name && item.title === focusedTarget.draft_name) {
        return true;
      }
      if (focusedTarget.role_label && item.title.toLowerCase().includes(focusedTarget.role_label.toLowerCase())) {
        return true;
      }
      if (focusedTarget.role_code && item.title.toLowerCase().includes(focusedTarget.role_code.toLowerCase())) {
        return true;
      }
      return false;
    });
  }, [items, focusedTarget]);

  const primaryItem = filteredItems[0] || null;
  const roleLabel = focusedTarget?.role_label || focusedTarget?.role_code || 'Recovery deposition';
  const stageTarget = focusedTarget || undefined;

  const stageCards = useMemo<AssetStageCard[]>(() => [
    {
      id: 'knowledge',
      eyebrow: 'Stage 1',
      title: primaryItem ? `${roleLabel} memory is in review` : 'Review the recovery memory',
      tone: primaryItem ? 'info' : 'warn',
      summary: primaryItem
        ? `The organization already has a candidate memory draft for ${primaryItem.title}.`
        : 'No matching recovery memory is currently visible for this context.',
      detail: 'This is where the exception is converted into durable language and reusable organizational memory.',
      statLine: [
        `${filteredItems.length} matching memory asset${filteredItems.length === 1 ? '' : 's'}`,
        `${items.length} total knowledge item${items.length === 1 ? '' : 's'}`,
        focusedTarget?.draft_name || 'Context-bound review',
      ],
      actionLabel: 'Current stage',
    },
    {
      id: 'packages',
      eyebrow: 'Stage 2',
      title: 'Promote memory into capability',
      tone: focusedTarget ? 'info' : 'ok',
      summary: 'Move from reviewed wording into an approved reusable capability package.',
      detail: 'Capability review is where the organization stops treating this as a one-off note and starts treating it as a reusable operating asset.',
      statLine: [
        'Approve package after memory review',
        'Connect role context to reusable handling',
        'Prepare workflow rollout',
      ],
      actionLabel: 'Open packages',
    },
    {
      id: 'workflows',
      eyebrow: 'Stage 3',
      title: 'Publish operating standard',
      tone: focusedTarget ? 'info' : 'ok',
      summary: 'Turn the reviewed package into a live workflow standard the center can execute automatically.',
      detail: 'This is the point where institutional memory becomes active policy instead of a reviewed but inactive draft.',
      statLine: [
        'Publish workflow when package is approved',
        'Switch role to policy monitoring',
        'Close the exception loop',
      ],
      actionLabel: 'Open workflows',
    },
  ], [filteredItems.length, focusedTarget, items.length, primaryItem, roleLabel]);

  const rows = filteredItems.map((m) => ({
    title: m.title,
    source: m.source,
    created: m.created_at,
  }));

  return (
    <div className="center-page-stack">
      <div className="asset-cockpit-grid">
        {stageCards.map((card) => (
          <section key={card.id} className={`card section-card asset-cockpit-card ${card.id === 'knowledge' ? 'asset-cockpit-card-active' : ''}`}>
            <div className="asset-cockpit-head">
              <div>
                <div className="mini light">{card.eyebrow}</div>
                <h3>{card.title}</h3>
              </div>
              <span className={`badge ${card.tone}`}>{card.id === 'knowledge' ? 'Current' : 'Next'}</span>
            </div>
            <strong>{card.summary}</strong>
            <p>{card.detail}</p>
            <div className="asset-cockpit-stats">
              {card.statLine.map((item) => <span key={item}>{item}</span>)}
            </div>
            <div className="executive-action-row">
              {card.id === 'knowledge' ? (
                <button type="button" className="executive-link-button" onClick={() => onNavigateToOverview?.({ role_code: focusedTarget?.role_code, source: 'knowledge_review' })}>
                  Return to overview
                </button>
              ) : (
                <button type="button" className="executive-assign-button" onClick={() => onNavigateToTab(card.id, stageTarget)}>
                  {card.actionLabel}
                </button>
              )}
            </div>
          </section>
        ))}
      </div>

      <SectionCard title={t('nav.knowledge')} desc={loading ? t('common.loading') : `${rows.length}`}>
        {focusedTarget ? (
          <div className="item-row asset-review-row">
            <span className="badge info">Institutional memory review</span>
            <strong>{roleLabel}</strong>
            <p>{focusedTarget.draft_name ? `Showing the memory draft expected for ${focusedTarget.draft_name}.` : 'Showing knowledge assets related to the current recovery deposition context.'}</p>
            {primaryItem ? (
              <>
                <p>{`Primary memory: ${primaryItem.title}`}</p>
                <p>This draft should capture the recovery logic clearly enough that the next similar exception can be handled by the system rather than re-explained by people.</p>
              </>
            ) : (
              <p>No matching memory draft is currently visible for this recovery context.</p>
            )}
            <div className="executive-action-row">
              <button
                type="button"
                className="executive-link-button"
                onClick={() => onNavigateToOverview?.({ role_code: focusedTarget.role_code, source: 'knowledge_review' })}
              >
                Return to overview
              </button>
              <button
                type="button"
                className="executive-assign-button"
                onClick={() => onNavigateToTab('packages', stageTarget)}
              >
                Continue to packages
              </button>
            </div>
          </div>
        ) : null}
        <DataTable
          columns={[
            { key: 'title', label: 'Title' },
            { key: 'source', label: 'Source' },
            { key: 'created', label: 'Created' },
          ]}
          rows={rows}
        />
      </SectionCard>
    </div>
  );
}

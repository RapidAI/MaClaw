import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';
import { listMemories, type Memory } from '../api/memories';
import type { AssetNavigationTarget, OverviewNavigationTarget } from '../types';

type KnowledgePageProps = {
  navigationTarget?: AssetNavigationTarget | null;
  onNavigationHandled?: () => void;
  onNavigateToOverview?: (target?: OverviewNavigationTarget | null) => void;
};

export function KnowledgePage({ navigationTarget, onNavigationHandled, onNavigateToOverview }: KnowledgePageProps) {
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

  const rows = filteredItems.map((m) => ({
    title: m.title,
    source: m.source,
    created: m.created_at,
  }));

  return (
    <div className="center-page-stack">
      <SectionCard title={t('nav.knowledge')} desc={loading ? t('common.loading') : `${rows.length}`}>
        {focusedTarget ? (
          <div className="item-row">
            <span className="badge info">Focused from overview</span>
            <strong>{focusedTarget.role_label || focusedTarget.role_code || 'Recovery deposition'}</strong>
            <p>{focusedTarget.draft_name ? `Showing the knowledge draft expected for ${focusedTarget.draft_name}.` : 'Showing knowledge assets related to the current recovery deposition context.'}</p>
            <p>This page is the review surface for the recovery memory. Once the wording is acceptable, move on to the package and workflow rollout steps.</p>
            <div className="executive-action-row">
              <button
                type="button"
                className="executive-link-button"
                onClick={() => onNavigateToOverview?.({ role_code: focusedTarget.role_code, source: 'knowledge_review' })}
              >
                Return to overview
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

import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';
import { listWorkflows, publishWorkflow, type WorkflowDef } from '../api/workflows';
import type { AssetNavigationTarget, OverviewNavigationTarget } from '../types';

type WorkflowsPageProps = {
  navigationTarget?: AssetNavigationTarget | null;
  onNavigationHandled?: () => void;
  onNavigateToOverview?: (target?: OverviewNavigationTarget | null) => void;
};

export function WorkflowsPage({ navigationTarget, onNavigationHandled, onNavigateToOverview }: WorkflowsPageProps) {
  const { t } = useTranslation();
  const [items, setItems] = useState<WorkflowDef[]>([]);
  const [loading, setLoading] = useState(true);
  const [focusedTarget, setFocusedTarget] = useState<AssetNavigationTarget | null>(null);
  const [message, setMessage] = useState('');
  const [publishing, setPublishing] = useState(false);

  const loadItems = async () => {
    setLoading(true);
    try {
      const nextItems = await listWorkflows();
      setItems(nextItems);
    } catch {
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void loadItems();
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
      if (focusedTarget.draft_name && item.name === focusedTarget.draft_name) {
        return true;
      }
      if (focusedTarget.role_label && item.name.toLowerCase().includes(focusedTarget.role_label.toLowerCase())) {
        return true;
      }
      if (focusedTarget.role_code && item.name.toLowerCase().includes(focusedTarget.role_code.toLowerCase())) {
        return true;
      }
      return false;
    });
  }, [items, focusedTarget]);

  const primaryItem = filteredItems[0] || null;
  const canPublish = Boolean(primaryItem && primaryItem.status !== 'published');

  const rows = filteredItems.map((w) => ({
    name: w.name,
    description: w.description,
    status: w.status,
  }));

  const handlePublish = async () => {
    if (!primaryItem) {
      return;
    }
    try {
      setPublishing(true);
      setMessage('');
      await publishWorkflow(primaryItem.id);
      await loadItems();
      setMessage(`Published workflow ${primaryItem.name}. Return to the overview to verify that the organization has switched into policy monitoring.`);
    } catch (error) {
      setMessage(error instanceof Error ? error.message : 'Failed to publish the workflow.');
    } finally {
      setPublishing(false);
    }
  };

  return (
    <div className="center-page-stack">
      <SectionCard title={t('nav.workflows')} desc={loading ? t('common.loading') : `${rows.length}`}>
        {focusedTarget ? (
          <div className="item-row">
            <span className="badge info">Focused from overview</span>
            <strong>{focusedTarget.role_label || focusedTarget.role_code || 'Recovery deposition'}</strong>
            <p>{focusedTarget.draft_name ? `Showing the workflow draft expected for ${focusedTarget.draft_name}.` : 'Showing workflow definitions related to the current recovery deposition context.'}</p>
            {primaryItem ? <p>{`Primary workflow status: ${primaryItem.status}`}</p> : <p>No matching workflow draft is currently visible for this recovery context.</p>}
            <div className="executive-action-row">
              <button
                type="button"
                className="executive-assign-button"
                disabled={!canPublish || publishing}
                onClick={() => void handlePublish()}
              >
                {publishing ? 'Publishing...' : primaryItem?.status === 'published' ? 'Workflow published' : 'Publish workflow'}
              </button>
              <button
                type="button"
                className="executive-link-button"
                onClick={() => onNavigateToOverview?.({ role_code: focusedTarget.role_code, source: 'workflow_review' })}
              >
                Return to overview
              </button>
            </div>
            {message ? <p>{message}</p> : null}
          </div>
        ) : null}
        <DataTable
          columns={[
            { key: 'name', label: 'Name' },
            { key: 'description', label: 'Description' },
            { key: 'status', label: 'Status' },
          ]}
          rows={rows}
        />
      </SectionCard>
    </div>
  );
}

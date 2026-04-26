import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';
import { listWorkflows, publishWorkflow, type WorkflowDef } from '../api/workflows';
import type { AssetNavigationTarget, CenterTab, OverviewNavigationTarget } from '../types';

type WorkflowsPageProps = {
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

export function WorkflowsPage({ navigationTarget, onNavigationHandled, onNavigateToOverview, onNavigateToTab }: WorkflowsPageProps) {
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
  const roleLabel = focusedTarget?.role_label || focusedTarget?.role_code || 'Recovery deposition';
  const stageTarget = focusedTarget || undefined;

  const stageCards = useMemo<AssetStageCard[]>(() => [
    {
      id: 'knowledge',
      eyebrow: 'Stage 1',
      title: 'Review recovery memory',
      tone: 'ok',
      summary: 'Memory review preserves the language and decision logic behind the recovery path.',
      detail: 'Every published workflow standard should be traceable back to a reviewed memory draft.',
      statLine: [
        'Start from reviewed memory',
        'Retain human-readable rationale',
        'Support future role updates',
      ],
      actionLabel: 'Open knowledge',
    },
    {
      id: 'packages',
      eyebrow: 'Stage 2',
      title: 'Approve capability package',
      tone: 'ok',
      summary: 'Capability approval confirms the recovery pattern is reusable enough to become a formal package.',
      detail: 'Workflow rollout should follow package approval so the published standard is backed by a reviewed capability asset.',
      statLine: [
        'Approve before rollout',
        'Bind role context to package',
        'Prepare enforcement layer',
      ],
      actionLabel: 'Open packages',
    },
    {
      id: 'workflows',
      eyebrow: 'Stage 3',
      title: primaryItem ? `${primaryItem.name} is ready for publication` : 'Publish workflow standard',
      tone: primaryItem ? (primaryItem.status === 'published' ? 'ok' : 'info') : 'warn',
      summary: primaryItem
        ? `Primary workflow is currently ${primaryItem.status}.`
        : 'No matching workflow draft is currently visible for this recovery context.',
      detail: 'This is where the organization stops reviewing the recovery and starts enforcing it as policy through live workflow execution.',
      statLine: [
        `${filteredItems.length} matching workflow${filteredItems.length === 1 ? '' : 's'}`,
        `${items.length} total workflow definition${items.length === 1 ? '' : 's'}`,
        primaryItem?.status || 'awaiting publication',
      ],
      actionLabel: 'Current stage',
    },
  ], [filteredItems.length, items.length, primaryItem]);

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
      <div className="asset-cockpit-grid">
        {stageCards.map((card) => (
          <section key={card.id} className={`card section-card asset-cockpit-card ${card.id === 'workflows' ? 'asset-cockpit-card-active' : ''}`}>
            <div className="asset-cockpit-head">
              <div>
                <div className="mini light">{card.eyebrow}</div>
                <h3>{card.title}</h3>
              </div>
              <span className={`badge ${card.tone}`}>{card.id === 'workflows' ? 'Current' : 'Prior'}</span>
            </div>
            <strong>{card.summary}</strong>
            <p>{card.detail}</p>
            <div className="asset-cockpit-stats">
              {card.statLine.map((item) => <span key={item}>{item}</span>)}
            </div>
            <div className="executive-action-row">
              {card.id === 'workflows' ? (
                <button type="button" className="executive-link-button" onClick={() => onNavigateToOverview?.({ role_code: focusedTarget?.role_code, source: 'workflow_review' })}>
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

      <SectionCard title={t('nav.workflows')} desc={loading ? t('common.loading') : `${rows.length}`}>
        {focusedTarget ? (
          <div className="item-row asset-review-row">
            <span className="badge info">Workflow standard rollout</span>
            <strong>{roleLabel}</strong>
            <p>{focusedTarget.draft_name ? `Showing the workflow draft expected for ${focusedTarget.draft_name}.` : 'Showing workflow definitions related to the current recovery deposition context.'}</p>
            {primaryItem ? <p>{`Primary workflow status: ${primaryItem.status}`}</p> : <p>No matching workflow draft is currently visible for this recovery context.</p>}
            <p>This is the publication gate where reviewed recovery logic becomes live organizational policy and can run without waiting for fresh board intervention.</p>
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
                onClick={() => onNavigateToTab('packages', stageTarget)}
              >
                Review packages again
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

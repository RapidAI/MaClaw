import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';
import { approveCapability, listCapabilities, type Capability } from '../api/capabilities';
import type { AssetNavigationTarget, CenterTab, OverviewNavigationTarget } from '../types';

type PackagesPageProps = {
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

export function PackagesPage({ navigationTarget, onNavigationHandled, onNavigateToOverview, onNavigateToTab }: PackagesPageProps) {
  const { t } = useTranslation();
  const [items, setItems] = useState<Capability[]>([]);
  const [loading, setLoading] = useState(true);
  const [focusedTarget, setFocusedTarget] = useState<AssetNavigationTarget | null>(null);
  const [message, setMessage] = useState('');
  const [approving, setApproving] = useState(false);

  const loadItems = async () => {
    setLoading(true);
    try {
      const nextItems = await listCapabilities();
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
  const canApprove = Boolean(primaryItem && primaryItem.status !== 'approved');
  const roleLabel = focusedTarget?.role_label || focusedTarget?.role_code || 'Recovery deposition';
  const stageTarget = focusedTarget || undefined;

  const stageCards = useMemo<AssetStageCard[]>(() => [
    {
      id: 'knowledge',
      eyebrow: 'Stage 1',
      title: 'Review recovery memory',
      tone: 'ok',
      summary: 'Make sure the recovery path is described clearly before promoting it into a reusable capability.',
      detail: 'The memory stage preserves reasoning and language. It is the source material for the capability package.',
      statLine: [
        'Memory first',
        'Clarify role context',
        'Preserve exception language',
      ],
      actionLabel: 'Open knowledge',
    },
    {
      id: 'packages',
      eyebrow: 'Stage 2',
      title: primaryItem ? `${primaryItem.name} is under package review` : 'Approve the capability package',
      tone: primaryItem ? (primaryItem.status === 'approved' ? 'ok' : 'info') : 'warn',
      summary: primaryItem
        ? `${primaryItem.name} currently sits in status ${primaryItem.status}.`
        : 'No matching capability package is currently visible for this recovery context.',
      detail: 'This is where the organization decides whether the recovery logic is mature enough to become a reusable packaged capability.',
      statLine: [
        `${filteredItems.length} matching capability package${filteredItems.length === 1 ? '' : 's'}`,
        `${items.length} total package${items.length === 1 ? '' : 's'}`,
        primaryItem?.status || 'awaiting review',
      ],
      actionLabel: 'Current stage',
    },
    {
      id: 'workflows',
      eyebrow: 'Stage 3',
      title: 'Publish workflow standard',
      tone: primaryItem?.status === 'approved' ? 'info' : 'ok',
      summary: 'Once the package is approved, the next move is to publish the workflow so the center can execute it under policy.',
      detail: 'A capability package is a reviewed asset. A workflow publication is what turns that asset into active organizational behavior.',
      statLine: [
        'Package approval before rollout',
        'Policy follows package review',
        'Shift into monitoring mode',
      ],
      actionLabel: 'Open workflows',
    },
  ], [filteredItems.length, items.length, primaryItem]);

  const rows = filteredItems.map((c) => ({
    name: c.name,
    description: c.description,
    status: c.status,
  }));

  const handleApprove = async () => {
    if (!primaryItem) {
      return;
    }
    try {
      setApproving(true);
      setMessage('');
      await approveCapability(primaryItem.id);
      await loadItems();
      setMessage(`Approved capability package ${primaryItem.name}. The organization can now use this recovery handling package as a formal reusable asset.`);
    } catch (error) {
      setMessage(error instanceof Error ? error.message : 'Failed to approve the capability package.');
    } finally {
      setApproving(false);
    }
  };

  return (
    <div className="center-page-stack">
      <div className="asset-cockpit-grid">
        {stageCards.map((card) => (
          <section key={card.id} className={`card section-card asset-cockpit-card ${card.id === 'packages' ? 'asset-cockpit-card-active' : ''}`}>
            <div className="asset-cockpit-head">
              <div>
                <div className="mini light">{card.eyebrow}</div>
                <h3>{card.title}</h3>
              </div>
              <span className={`badge ${card.tone}`}>{card.id === 'packages' ? 'Current' : 'Next'}</span>
            </div>
            <strong>{card.summary}</strong>
            <p>{card.detail}</p>
            <div className="asset-cockpit-stats">
              {card.statLine.map((item) => <span key={item}>{item}</span>)}
            </div>
            <div className="executive-action-row">
              {card.id === 'packages' ? (
                <button type="button" className="executive-link-button" onClick={() => onNavigateToOverview?.({ role_code: focusedTarget?.role_code, source: 'package_review' })}>
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

      <SectionCard title={t('nav.packages')} desc={loading ? t('common.loading') : `${rows.length}`}>
        {focusedTarget ? (
          <div className="item-row asset-review-row">
            <span className="badge info">Capability package review</span>
            <strong>{roleLabel}</strong>
            <p>{focusedTarget.draft_name ? `Showing the capability package expected for ${focusedTarget.draft_name}.` : 'Showing capability packages related to the current recovery deposition context.'}</p>
            {primaryItem ? <p>{`Primary package status: ${primaryItem.status}`}</p> : <p>No matching package draft is currently visible for this recovery context.</p>}
            <p>This is the approval gate where the organization decides the recovery logic is reusable enough to be promoted into a formal operating package.</p>
            <div className="executive-action-row">
              <button
                type="button"
                className="executive-assign-button"
                disabled={!canApprove || approving}
                onClick={() => void handleApprove()}
              >
                {approving ? 'Approving...' : primaryItem?.status === 'approved' ? 'Package approved' : 'Approve package'}
              </button>
              <button
                type="button"
                className="executive-link-button"
                onClick={() => onNavigateToTab('knowledge', stageTarget)}
              >
                Review knowledge again
              </button>
              <button
                type="button"
                className="executive-link-button"
                onClick={() => onNavigateToTab('workflows', stageTarget)}
              >
                Continue to workflows
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

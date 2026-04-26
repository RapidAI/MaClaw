import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';
import { approveCapability, listCapabilities, type Capability } from '../api/capabilities';
import type { AssetNavigationTarget, OverviewNavigationTarget } from '../types';

type PackagesPageProps = {
  navigationTarget?: AssetNavigationTarget | null;
  onNavigationHandled?: () => void;
  onNavigateToOverview?: (target?: OverviewNavigationTarget | null) => void;
};

export function PackagesPage({ navigationTarget, onNavigationHandled, onNavigateToOverview }: PackagesPageProps) {
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
      <SectionCard title={t('nav.packages')} desc={loading ? t('common.loading') : `${rows.length}`}>
        {focusedTarget ? (
          <div className="item-row">
            <span className="badge info">Focused from overview</span>
            <strong>{focusedTarget.role_label || focusedTarget.role_code || 'Recovery deposition'}</strong>
            <p>{focusedTarget.draft_name ? `Showing the capability package expected for ${focusedTarget.draft_name}.` : 'Showing capability packages related to the current recovery deposition context.'}</p>
            {primaryItem ? <p>{`Primary package status: ${primaryItem.status}`}</p> : <p>No matching package draft is currently visible for this recovery context.</p>}
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
                onClick={() => onNavigateToOverview?.({ role_code: focusedTarget.role_code, source: 'package_review' })}
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

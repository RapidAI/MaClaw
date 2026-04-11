import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';
import { listEndpoints } from '../api/models';

export function ModelRoutingPage() {
  const { t } = useTranslation();
  const [rows, setRows] = useState<Array<Record<string, string>>>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    listEndpoints()
      .then((items) => {
        setRows(items.map((e) => ({
          name: e.name,
          protocol: e.protocol,
          model: e.model,
          costTier: e.cost_tier,
          enabled: e.enabled ? '✓' : '✗',
        })));
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  return (
    <div className="center-page-stack">
      <SectionCard title={t('nav.models')} desc={loading ? t('common.loading') : `${rows.length}`}>
        <DataTable
          columns={[
            { key: 'name', label: 'Name' },
            { key: 'protocol', label: 'Protocol' },
            { key: 'model', label: 'Model' },
            { key: 'costTier', label: 'Cost Tier' },
            { key: 'enabled', label: 'Enabled' },
          ]}
          rows={rows}
        />
      </SectionCard>
    </div>
  );
}

import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';
import { listCapabilities } from '../api/capabilities';

export function PackagesPage() {
  const { t } = useTranslation();
  const [rows, setRows] = useState<Array<Record<string, string>>>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    listCapabilities()
      .then((items) => {
        setRows(items.map((c) => ({
          name: c.name,
          description: c.description,
          status: c.status,
        })));
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  return (
    <div className="center-page-stack">
      <SectionCard title={t('nav.packages')} desc={loading ? t('common.loading') : `${rows.length}`}>
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

import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';
import { listCollaborations } from '../api/collaboration';

export function CommunicationsPage() {
  const { t } = useTranslation();
  const [rows, setRows] = useState<Array<Record<string, string>>>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    listCollaborations()
      .then((items) => {
        setRows(items.map((c) => ({
          title: c.title,
          from: c.from_colleague,
          to: c.to_colleague,
          status: c.status,
        })));
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  return (
    <div className="center-page-stack">
      <SectionCard title={t('nav.communications')} desc={loading ? t('common.loading') : `${rows.length}`}>
        <DataTable
          columns={[
            { key: 'title', label: 'Title' },
            { key: 'from', label: 'From' },
            { key: 'to', label: 'To' },
            { key: 'status', label: 'Status' },
          ]}
          rows={rows}
        />
      </SectionCard>
    </div>
  );
}

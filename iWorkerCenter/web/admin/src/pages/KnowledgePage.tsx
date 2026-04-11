import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';
import { listMemories } from '../api/memories';

export function KnowledgePage() {
  const { t } = useTranslation();
  const [rows, setRows] = useState<Array<Record<string, string>>>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    listMemories()
      .then((items) => {
        setRows(items.map((m) => ({
          title: m.title,
          source: m.source,
          created: m.created_at,
        })));
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  return (
    <div className="center-page-stack">
      <SectionCard title={t('nav.knowledge')} desc={loading ? t('common.loading') : `${rows.length}`}>
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

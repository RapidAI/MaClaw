import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';
import { listPolicies } from '../api/security';

export function SecurityPage() {
  const { t } = useTranslation();
  const [rows, setRows] = useState<Array<Record<string, string>>>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    listPolicies()
      .then((items) => {
        setRows(items.map((p) => ({
          name: p.name,
          ruleType: p.rule_type,
          pattern: p.pattern,
          action: p.action,
          enabled: p.enabled ? '✓' : '✗',
        })));
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  return (
    <div className="center-page-stack">
      <SectionCard title={t('nav.security')} desc={loading ? t('common.loading') : `${rows.length}`}>
        <DataTable
          columns={[
            { key: 'name', label: 'Name' },
            { key: 'ruleType', label: 'Type' },
            { key: 'pattern', label: 'Pattern' },
            { key: 'action', label: 'Action' },
            { key: 'enabled', label: 'Enabled' },
          ]}
          rows={rows}
        />
      </SectionCard>
    </div>
  );
}

import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { listCenters } from '../api/centers';
import { listLicenses } from '../api/licenses';

export function OverviewPage() {
  const { t } = useTranslation();
  const [stats, setStats] = useState({ total: 0, pending: 0, activeLic: 0 });

  useEffect(() => {
    Promise.all([listCenters().catch(() => []), listLicenses().catch(() => [])])
      .then(([centers, licenses]) => {
        setStats({
          total: centers.length,
          pending: centers.filter(c => c.status === 'pending').length,
          activeLic: licenses.filter(l => !l.revoked_at).length,
        });
      });
  }, []);

  return (
    <div>
      <div className="metrics">
        <div className="metric"><label>{t('nav.centers')}</label><strong>{stats.total}</strong></div>
        <div className="metric"><label>{t('centers.pending')}</label><strong>{stats.pending}</strong></div>
        <div className="metric"><label>{t('licenses.valid')}</label><strong>{stats.activeLic}</strong></div>
      </div>
    </div>
  );
}

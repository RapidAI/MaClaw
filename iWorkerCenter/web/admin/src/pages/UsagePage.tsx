import { useTranslation } from 'react-i18next';
import { SectionCard } from '../components/cards/SectionCard';

export function UsagePage() {
  const { t } = useTranslation();

  return (
    <div className="center-page-stack">
      <SectionCard title={t('nav.usage')} desc={t('subtitle.usage')}>
        <p style={{ padding: 16, color: '#888' }}>{t('common.noData')}</p>
      </SectionCard>
    </div>
  );
}

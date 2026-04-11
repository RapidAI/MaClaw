import { useTranslation } from 'react-i18next';

export function PlaceholderPage({ tabKey }: { tabKey: string }) {
  const { t } = useTranslation();
  return (
    <div className="center-page-stack">
      <div className="card" style={{ padding: 32, textAlign: 'center', color: '#888' }}>
        <h3>{t(`nav.${tabKey}`)}</h3>
        <p>{t(`subtitle.${tabKey}`)}</p>
      </div>
    </div>
  );
}

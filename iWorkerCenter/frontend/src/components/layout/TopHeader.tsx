import { useI18n } from '../../i18n';

type Props = {
  title: string;
  subtitle: string;
  tenantId?: string;
  bootstrapReady?: boolean;
};

export function TopHeader({ title, subtitle, tenantId, bootstrapReady }: Props) {
  const { language, setLanguage, t } = useI18n();
  return (
    <header className="center-top card">
      <div>
        <div className="mini light">Admin Console</div>
        <h2>{title}</h2>
        <p>{subtitle}</p>
      </div>
      <div className="center-top-actions">
        {tenantId ? <span className="badge info">{t('租户', 'Tenant')} {tenantId}</span> : null}
        <span className={bootstrapReady ? 'badge ok' : 'badge warn'}>{bootstrapReady ? t('已初始化', 'Bootstrapped') : t('待初始化', 'Pending Bootstrap')}</span>
        <button type="button" className="ghost" onClick={() => setLanguage(language === 'en' ? 'zh' : 'en')}>{language === 'en' ? '中文' : 'EN'}</button>
        <button type="button" className="ghost" onClick={() => window.location.reload()}>{t('刷新数据', 'Refresh')}</button>
      </div>
    </header>
  );
}

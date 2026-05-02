import { useTranslation } from 'react-i18next';

export function LanguageSwitcher() {
  const { i18n, t } = useTranslation();
  const current = i18n.resolvedLanguage || i18n.language || 'zh';
  const isChinese = current.startsWith('zh');
  const nextLanguage = isChinese ? 'en' : 'zh';

  return (
    <button
      type="button"
      className="iw-language-switcher"
      onClick={() => { void i18n.changeLanguage(nextLanguage); }}
      aria-label={t('app.language', 'Language')}
      title={t('app.language', 'Language')}
    >
      {isChinese ? 'EN' : '\u4e2d\u6587'}
    </button>
  );
}

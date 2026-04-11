import { useTranslation } from 'react-i18next';

export function LanguageSwitcher() {
  const { i18n } = useTranslation();
  const toggle = () => i18n.changeLanguage(i18n.language === 'zh' ? 'en' : 'zh');
  return (
    <button className="btn-ghost" onClick={toggle} style={{ minWidth: 48 }}>
      {i18n.language === 'zh' ? 'EN' : '中文'}
    </button>
  );
}

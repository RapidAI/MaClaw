import { useTranslation } from 'react-i18next';

export function LanguageSwitcher() {
  const { i18n } = useTranslation();
  const current = i18n.resolvedLanguage || i18n.language || 'zh';
  const isChinese = current.startsWith('zh');
  const toggle = () => {
    void i18n.changeLanguage(isChinese ? 'en' : 'zh');
  };

  return (
    <button className="btn-ghost" onClick={toggle} style={{ minWidth: 48 }}>
      {isChinese ? 'EN' : '\u4e2d\u6587'}
    </button>
  );
}

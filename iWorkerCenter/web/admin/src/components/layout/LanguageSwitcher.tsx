import { useTranslation } from 'react-i18next';

export function LanguageSwitcher() {
  const { i18n } = useTranslation();

  const toggle = () => {
    const next = i18n.language === 'zh' ? 'en' : 'zh';
    i18n.changeLanguage(next);
  };

  return (
    <button
      type="button"
      onClick={toggle}
      style={{
        position: 'fixed',
        top: 12,
        right: 16,
        padding: '4px 12px',
        fontSize: 13,
        border: '1px solid #ddd',
        borderRadius: 4,
        background: '#fff',
        cursor: 'pointer',
        zIndex: 100,
      }}
    >
      {i18n.language === 'zh' ? 'EN' : '中文'}
    </button>
  );
}

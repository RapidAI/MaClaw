import { useTranslation } from 'react-i18next';

export function LanguageSwitcher() {
  const { i18n } = useTranslation();
  const current = i18n.resolvedLanguage || i18n.language || 'zh';
  const isChinese = current.startsWith('zh');
  const toggle = () => {
    void i18n.changeLanguage(isChinese ? 'en' : 'zh');
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
      {isChinese ? 'EN' : '\u4e2d\u6587'}
    </button>
  );
}

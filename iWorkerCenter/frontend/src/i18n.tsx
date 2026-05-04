import { createContext, useContext, useMemo, useState, type ReactNode } from 'react';

export type Language = 'zh' | 'en';

type I18nContextValue = {
  language: Language;
  setLanguage: (language: Language) => void;
  t: (zh: string, en: string) => string;
};

const defaultLanguage = (): Language => {
  if (typeof window === 'undefined') return 'zh';
  const saved = window.localStorage.getItem('iworkercenter.language');
  return saved === 'en' ? 'en' : 'zh';
};

const I18nContext = createContext<I18nContextValue>({ language: 'zh', setLanguage: () => undefined, t: (zh) => zh });

export function I18nProvider({ children }: { children: ReactNode }) {
  const [language, setLanguageState] = useState<Language>(() => defaultLanguage());
  const value = useMemo<I18nContextValue>(() => ({
    language,
    setLanguage: (next) => {
      setLanguageState(next);
      if (typeof window !== 'undefined') window.localStorage.setItem('iworkercenter.language', next);
    },
    t: (zh, en) => language === 'en' ? en : zh,
  }), [language]);
  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

export function useI18n() {
  return useContext(I18nContext);
}

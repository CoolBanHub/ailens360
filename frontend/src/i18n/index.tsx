import { createContext, useContext, useEffect, useMemo, useState } from 'react';
import { Locale, LocaleKey, localeMap } from './locales';

interface Ctx {
  locale: Locale;
  setLocale: (l: Locale) => void;
  t: (key: LocaleKey, vars?: Record<string, string | number>) => string;
}

const I18nContext = createContext<Ctx | null>(null);

const STORAGE_KEY = 'ailens_locale';

function detect(): Locale {
  try {
    const saved = localStorage.getItem(STORAGE_KEY) as Locale | null;
    if (saved === 'zh' || saved === 'en') return saved;
  } catch { /* */ }
  const browser = (typeof navigator !== 'undefined' ? navigator.language : '').toLowerCase();
  return browser.startsWith('zh') ? 'zh' : 'en';
}

export function I18nProvider({ children }: { children: React.ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>(detect());

  useEffect(() => {
    try { localStorage.setItem(STORAGE_KEY, locale); } catch { /* */ }
    if (typeof document !== 'undefined') {
      document.documentElement.lang = locale === 'zh' ? 'zh-CN' : 'en';
    }
  }, [locale]);

  const value = useMemo<Ctx>(() => {
    const dict = localeMap[locale];
    return {
      locale,
      setLocale: setLocaleState,
      t: (key, vars) => {
        let s = dict[key] ?? key;
        if (vars) for (const [k, v] of Object.entries(vars)) s = s.replaceAll(`{${k}}`, String(v));
        return s;
      },
    };
  }, [locale]);

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

export function useI18n() {
  const ctx = useContext(I18nContext);
  if (!ctx) throw new Error('useI18n must be used inside <I18nProvider>');
  return ctx;
}

/** Shortcut: const t = useT(); t('some.key') */
export function useT() {
  return useI18n().t;
}

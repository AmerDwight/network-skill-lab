import i18next from "i18next";
import { initReactI18next } from "react-i18next";

import en from "./locales/en.json";
import zhTW from "./locales/zh-TW.json";

export const languages = ["zh-TW", "en"] as const;

export type Language = (typeof languages)[number];

export const defaultLanguage: Language = "zh-TW";

export const storageKey = "nsl.lang";

function isLanguage(value: string): value is Language {
  return (languages as readonly string[]).includes(value);
}

export function readStoredLanguage(): string | null {
  try {
    return window.localStorage.getItem(storageKey);
  } catch {
    return null;
  }
}

export function storeLanguage(language: Language): void {
  try {
    window.localStorage.setItem(storageKey, language);
  } catch {
    return;
  }
}

export function resolveLanguage(
  stored: string | null,
  browserLanguages: readonly string[],
): Language {
  if (stored !== null && isLanguage(stored)) {
    return stored;
  }
  const preferred = browserLanguages[0];
  if (preferred === undefined) {
    return defaultLanguage;
  }
  return preferred.toLowerCase().startsWith("zh") ? "zh-TW" : "en";
}

function browserLanguages(): readonly string[] {
  if (navigator.languages.length > 0) {
    return navigator.languages;
  }
  return navigator.language ? [navigator.language] : [];
}

void i18next.use(initReactI18next).init({
  resources: {
    "zh-TW": { translation: zhTW },
    en: { translation: en },
  },
  lng: resolveLanguage(readStoredLanguage(), browserLanguages()),
  fallbackLng: defaultLanguage,
  keySeparator: false,
  interpolation: { escapeValue: false },
});

export function currentLanguage(): Language {
  return isLanguage(i18next.language) ? i18next.language : defaultLanguage;
}

export async function changeLanguage(language: Language): Promise<void> {
  storeLanguage(language);
  await i18next.changeLanguage(language);
}

export default i18next;

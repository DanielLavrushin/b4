import i18n from "i18next";
import { initReactI18next } from "react-i18next";

import en from "./en.json";
import ru from "./ru.json";

const SUPPORTED = ["en", "ru"] as const;
export type Lang = (typeof SUPPORTED)[number];

export const isSupportedLang = (v: unknown): v is Lang =>
  typeof v === "string" && (SUPPORTED as readonly string[]).includes(v);

const STORAGE_KEY = "b4hub-language";

const initial: Lang = (() => {
  try {
    const cached = localStorage.getItem(STORAGE_KEY);
    if (isSupportedLang(cached)) return cached;
  } catch {
    return "en";
  }
  return navigator.language.toLowerCase().startsWith("ru") ? "ru" : "en";
})();

i18n.on("languageChanged", (lng: string) => {
  document.documentElement.lang = lng;
});

void i18n.use(initReactI18next).init({
  resources: {
    en: { translation: en },
    ru: { translation: ru },
  },
  lng: initial,
  fallbackLng: "en",
  interpolation: {
    escapeValue: false,
  },
});

export const setLanguage = (lang: string) => {
  if (!isSupportedLang(lang) || i18n.language === lang) return;
  void i18n.changeLanguage(lang);
  try {
    localStorage.setItem(STORAGE_KEY, lang);
  } catch {
    return;
  }
};

export default i18n;

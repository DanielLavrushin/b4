import i18n from "i18next";
import { initReactI18next } from "react-i18next";

import en from "./en.json";
import ru from "./ru.json";

const SUPPORTED = ["en", "ru"] as const;
type Lang = (typeof SUPPORTED)[number];

export const isSupportedLang = (v: unknown): v is Lang =>
  typeof v === "string" && (SUPPORTED as readonly string[]).includes(v);

const initial: Lang = (() => {
  const cached = localStorage.getItem("b4-language");
  return isSupportedLang(cached) ? cached : "en";
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
  if (!isSupportedLang(lang)) return;
  if (i18n.language === lang) return;
  void i18n.changeLanguage(lang);
  localStorage.setItem("b4-language", lang);
};

export default i18n;

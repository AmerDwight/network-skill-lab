import { useTranslation } from "react-i18next";

import { languages } from "../i18n";
import { useAppStore } from "../store/app";
import { useAuthStore } from "../store/auth";

export function LanguageToggle() {
  const { t } = useTranslation();
  const language = useAppStore((state) => state.language);
  const setLanguage = useAuthStore((state) => state.setLanguage);

  return (
    <div className="language-toggle" aria-label={t("language.label")}>
      {languages.map((value) => (
        <button
          key={value}
          type="button"
          className="language-toggle__button"
          aria-pressed={value === language}
          onClick={() => void setLanguage(value)}
        >
          {t(`language.${value}`)}
        </button>
      ))}
    </div>
  );
}

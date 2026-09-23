import { useTranslation } from "react-i18next";

import { useAppStore } from "../store/app";

import { HealthIndicator } from "./HealthIndicator";
import { LanguageToggle } from "./LanguageToggle";
import { Nav } from "./Nav";

export function Header() {
  const { t } = useTranslation();
  const health = useAppStore((state) => state.health);

  return (
    <header className="header">
      <div className="header__brand">
        <h1 className="header__title">{t("app.title")}</h1>
        <p className="header__tagline">{t("app.tagline")}</p>
        <Nav />
      </div>
      <div className="header__status">
        <HealthIndicator state={health} />
        <LanguageToggle />
      </div>
    </header>
  );
}

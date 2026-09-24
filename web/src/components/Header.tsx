import { useTranslation } from "react-i18next";
import { Link } from "react-router";

import { useAppStore } from "../store/app";
import { useAuthStore } from "../store/auth";

import { HealthIndicator } from "./HealthIndicator";
import { LanguageToggle } from "./LanguageToggle";
import { Nav } from "./Nav";

function Session() {
  const { t } = useTranslation();
  const me = useAuthStore((state) => state.me);
  const logout = useAuthStore((state) => state.logout);

  if (me === null) {
    return null;
  }

  return (
    <div className="session">
      <span className="session__user">{me.username}</span>
      <span className="badge badge--role">{t(`role.${me.role}`)}</span>
      {me.role === "admin" ? (
        <Link className="session__link" to="/admin">
          {t("nav.admin")}
        </Link>
      ) : null}
      <button
        type="button"
        className="session__logout"
        onClick={() => void logout()}
      >
        {t("action.logout")}
      </button>
    </div>
  );
}

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
        <Session />
        <HealthIndicator state={health} />
        <LanguageToggle />
      </div>
    </header>
  );
}

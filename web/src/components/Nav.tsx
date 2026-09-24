import { useTranslation } from "react-i18next";
import { NavLink } from "react-router";

const links = [
  { to: "/", key: "nav.labs" },
  { to: "/docs", key: "nav.docs" },
  { to: "/tracks", key: "nav.tracks" },
  { to: "/history", key: "nav.history" },
];

export function Nav() {
  const { t } = useTranslation();

  return (
    <nav className="nav" aria-label={t("nav.label")}>
      {links.map((link) => (
        <NavLink
          key={link.to}
          to={link.to}
          end={link.to === "/"}
          className={({ isActive }) =>
            isActive ? "nav__link nav__link--active" : "nav__link"
          }
        >
          {t(link.key)}
        </NavLink>
      ))}
    </nav>
  );
}

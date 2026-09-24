import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Navigate, Outlet, useLocation } from "react-router";

import { loginHref } from "../lib/paths";
import { useAuthStore } from "../store/auth";

export function AuthGate() {
  const { t } = useTranslation();
  const location = useLocation();
  const status = useAuthStore((state) => state.status);
  const load = useAuthStore((state) => state.load);

  useEffect(() => {
    void load();
  }, [load]);

  if (status === "unknown") {
    return (
      <div className="page">
        <p className="state">{t("auth.checking")}</p>
      </div>
    );
  }

  if (status === "anonymous") {
    return (
      <Navigate
        replace
        to={loginHref(`${location.pathname}${location.search}`)}
      />
    );
  }

  return <Outlet />;
}

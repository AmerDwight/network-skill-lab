import type { FormEvent } from "react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Navigate, useSearchParams } from "react-router";

import { LanguageToggle } from "../components/LanguageToggle";
import { safeNext } from "../lib/paths";
import { useAuthStore } from "../store/auth";

const knownErrors = [
  "invalid_credentials",
  "user_disabled",
  "too_many_attempts",
];

function errorKey(code: string): string {
  return knownErrors.includes(code)
    ? `login.error.${code}`
    : "login.error.unknown";
}

export function LoginPage() {
  const { t } = useTranslation();
  const [searchParams] = useSearchParams();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const status = useAuthStore((state) => state.status);
  const noUsers = useAuthStore((state) => state.noUsers);
  const loginError = useAuthStore((state) => state.loginError);
  const submitting = useAuthStore((state) => state.submitting);
  const load = useAuthStore((state) => state.load);
  const login = useAuthStore((state) => state.login);

  useEffect(() => {
    void load();
  }, [load]);

  const next = safeNext(searchParams.get("next"));

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    await login(username, password);
  }

  if (status === "authenticated") {
    return <Navigate replace to={next} />;
  }

  return (
    <div className="page page--login">
      <header className="login__header">
        <h1 className="header__title">{t("app.title")}</h1>
        <LanguageToggle />
      </header>

      <main className="main">
        <h2 className="main__heading">{t("login.heading")}</h2>

        {noUsers ? <p className="login__hint">{t("login.noUsers")}</p> : null}

        {loginError !== null ? (
          <div className="banner banner--error">
            <span>{t(errorKey(loginError))}</span>
          </div>
        ) : null}

        <form
          className="login__form"
          onSubmit={(event) => void handleSubmit(event)}
        >
          <label className="field" htmlFor="login-username">
            <span className="field__label">{t("login.username")}</span>
            <input
              id="login-username"
              className="field__input"
              name="username"
              autoComplete="username"
              value={username}
              onChange={(event) => setUsername(event.target.value)}
            />
          </label>

          <label className="field" htmlFor="login-password">
            <span className="field__label">{t("login.password")}</span>
            <input
              id="login-password"
              className="field__input"
              name="password"
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
            />
          </label>

          <button
            type="submit"
            className="button button--primary"
            disabled={submitting}
          >
            {submitting ? t("login.submitting") : t("login.submit")}
          </button>
        </form>
      </main>
    </div>
  );
}

import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Link, useNavigate } from "react-router";

import type { LabSummary } from "../api/types";
import { Header } from "../components/Header";
import { useAppStore } from "../store/app";

function LabCard({
  lab,
  disabled,
  starting,
  onStart,
}: {
  lab: LabSummary;
  disabled: boolean;
  starting: boolean;
  onStart: () => void;
}) {
  const { t } = useTranslation();

  return (
    <li className="lab-card">
      <h3 className="lab-card__title">{lab.title}</h3>
      <dl className="lab-card__meta">
        <dt>{t("lab.topic")}</dt>
        <dd>{lab.topic}</dd>
        <dt>{t("lab.level")}</dt>
        <dd>{t("lab.level.value", { level: lab.level })}</dd>
        <dt>{t("lab.duration")}</dt>
        <dd>{t("lab.duration.value", { minutes: lab.estimated_minutes })}</dd>
        <dt>{t("lab.modes")}</dt>
        <dd>
          {lab.modes
            .map((mode) => t(`lab.mode.${mode}`, { defaultValue: mode }))
            .join(", ")}
        </dd>
      </dl>
      <button
        type="button"
        className="button button--primary"
        disabled={disabled || starting}
        onClick={onStart}
      >
        {starting ? t("attempt.starting") : t("action.start")}
      </button>
    </li>
  );
}

export function LabListPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const labs = useAppStore((state) => state.labs);
  const attempt = useAppStore((state) => state.attempt);
  const attemptError = useAppStore((state) => state.attemptError);
  const startingLabId = useAppStore((state) => state.startingLabId);
  const loadHealth = useAppStore((state) => state.loadHealth);
  const loadLabs = useAppStore((state) => state.loadLabs);
  const loadCurrentAttempt = useAppStore((state) => state.loadCurrentAttempt);
  const startAttempt = useAppStore((state) => state.startAttempt);

  useEffect(() => {
    void loadHealth();
    void loadLabs();
    void loadCurrentAttempt();
  }, [loadHealth, loadLabs, loadCurrentAttempt]);

  async function handleStart(labId: string) {
    const started = await startAttempt(labId);
    if (started) {
      await navigate(`/attempts/${started.id}`);
    }
  }

  return (
    <div className="page">
      <Header />
      <main className="main">
        <h2 className="main__heading">{t("labs.heading")}</h2>

        {attempt ? (
          <div className="banner banner--info">
            <span>{t("attempt.active")}</span>
            <Link
              className="button button--primary"
              to={`/attempts/${attempt.id}`}
            >
              {t("action.continue")}
            </Link>
          </div>
        ) : null}

        {attemptError ? (
          <div className="banner banner--error">
            <span>{t("attempt.error")}</span>
            <span className="banner__detail">{attemptError}</span>
          </div>
        ) : null}

        {labs.status === "loading" ? (
          <p className="state">{t("labs.loading")}</p>
        ) : null}

        {labs.status === "error" ? (
          <div className="banner banner--error">
            <span>{t("labs.error")}</span>
            <span className="banner__detail">{labs.message}</span>
            <button
              type="button"
              className="button"
              onClick={() => void loadLabs()}
            >
              {t("action.retry")}
            </button>
          </div>
        ) : null}

        {labs.status === "ok" && labs.labs.length === 0 ? (
          <p className="state">{t("labs.empty")}</p>
        ) : null}

        {labs.status === "ok" && labs.labs.length > 0 ? (
          <ul className="lab-list">
            {labs.labs.map((lab) => (
              <LabCard
                key={lab.id}
                lab={lab}
                disabled={attempt !== null}
                starting={startingLabId === lab.id}
                onStart={() => void handleStart(lab.id)}
              />
            ))}
          </ul>
        ) : null}
      </main>
    </div>
  );
}

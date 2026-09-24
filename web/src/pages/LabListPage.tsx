import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Link, useSearchParams } from "react-router";

import type { LabSummary } from "../api/types";
import { Header } from "../components/Header";
import { RunnerBusyNotice } from "../components/RunnerBusyNotice";
import { TopicTree } from "../components/TopicTree";
import { labHref } from "../lib/paths";
import { useAppStore } from "../store/app";

function LabCard({ lab }: { lab: LabSummary }) {
  const { t } = useTranslation();

  return (
    <li className="lab-card">
      <Link className="lab-card__link" to={labHref(lab.id)}>
        <h3 className="lab-card__title">{lab.title}</h3>
        <dl className="lab-card__meta">
          <dt>{t("lab.level")}</dt>
          <dd>{t("lab.level.value", { level: lab.level })}</dd>
          <dt>{t("lab.duration")}</dt>
          <dd>{t("lab.duration.value", { minutes: lab.estimated_minutes })}</dd>
          <dt>{t("lab.modes")}</dt>
          <dd>{lab.modes.map((mode) => t(`lab.mode.${mode}`)).join(", ")}</dd>
          <dt>{t("lab.relatedDocs")}</dt>
          <dd>
            {t("lab.relatedDocs.value", { docs: lab.related_docs.length })}
          </dd>
        </dl>
        {lab.has_hidden_checkpoints ? (
          <span className="badge badge--hidden">{t("lab.hidden")}</span>
        ) : null}
      </Link>
    </li>
  );
}

export function LabListPage() {
  const { t } = useTranslation();
  const [searchParams] = useSearchParams();
  const topic = searchParams.get("topic");
  const language = useAppStore((state) => state.language);
  const topics = useAppStore((state) => state.topics);
  const labs = useAppStore((state) => state.labs);
  const attempt = useAppStore((state) => state.attempt);
  const attemptError = useAppStore((state) => state.attemptError);
  const runnerBusy = useAppStore((state) => state.runnerBusy);
  const loadHealth = useAppStore((state) => state.loadHealth);
  const loadTopics = useAppStore((state) => state.loadTopics);
  const loadLabs = useAppStore((state) => state.loadLabs);
  const loadCurrentAttempt = useAppStore((state) => state.loadCurrentAttempt);

  useEffect(() => {
    void loadHealth();
    void loadCurrentAttempt();
  }, [loadHealth, loadCurrentAttempt]);

  useEffect(() => {
    void loadTopics();
  }, [loadTopics, language]);

  useEffect(() => {
    void loadLabs(topic ?? undefined);
  }, [loadLabs, topic, language]);

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

        {runnerBusy !== null ? <RunnerBusyNotice busy={runnerBusy} /> : null}

        {attemptError ? (
          <div className="banner banner--error">
            <span>{t("attempt.error")}</span>
            <span className="banner__detail">{attemptError}</span>
          </div>
        ) : null}

        <div className="browser">
          <aside className="browser__sidebar" aria-label={t("topics.heading")}>
            <h3 className="browser__heading">{t("topics.heading")}</h3>
            {topics.status === "loading" ? (
              <p className="state">{t("topics.loading")}</p>
            ) : null}
            {topics.status === "error" ? (
              <div className="banner banner--error">
                <span>{t("topics.error")}</span>
                <span className="banner__detail">{topics.message}</span>
                <button
                  type="button"
                  className="button"
                  onClick={() => void loadTopics()}
                >
                  {t("action.retry")}
                </button>
              </div>
            ) : null}
            {topics.status === "ok" ? (
              <TopicTree topics={topics.topics} selected={topic} />
            ) : null}
          </aside>

          <section className="browser__content">
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
                  onClick={() => void loadLabs(topic ?? undefined)}
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
                  <LabCard key={lab.id} lab={lab} />
                ))}
              </ul>
            ) : null}
          </section>
        </div>
      </main>
    </div>
  );
}

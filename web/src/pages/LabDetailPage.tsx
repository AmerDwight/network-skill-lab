import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Link, useNavigate, useParams, useSearchParams } from "react-router";

import type { LabDetail, LabMode } from "../api/types";
import { labModes } from "../api/types";
import { Header } from "../components/Header";
import { docHref, topicHref } from "../lib/paths";
import { useAppStore } from "../store/app";

function parseMode(value: string | null): LabMode | null {
  return labModes.find((mode) => mode === value) ?? null;
}

function ModeStart({
  mode,
  preselected,
  disabled,
  starting,
  onStart,
}: {
  mode: LabMode;
  preselected: boolean;
  disabled: boolean;
  starting: boolean;
  onStart: () => void;
}) {
  const { t } = useTranslation();

  return (
    <li className={preselected ? "mode mode--preselected" : "mode"}>
      <h4 className="mode__title">{t(`lab.mode.${mode}`)}</h4>
      <p className="mode__description">{t(`lab.mode.${mode}.description`)}</p>
      <button
        type="button"
        className={preselected ? "button button--primary" : "button"}
        disabled={disabled || starting}
        onClick={onStart}
      >
        {starting ? t("attempt.starting") : t("action.start")}
      </button>
    </li>
  );
}

function LabView({ lab }: { lab: LabDetail }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const preselected = parseMode(searchParams.get("mode"));
  const attempt = useAppStore((state) => state.attempt);
  const starting = useAppStore((state) => state.starting);
  const startAttempt = useAppStore((state) => state.startAttempt);

  async function handleStart(mode: LabMode) {
    const started = await startAttempt(lab.id, mode);
    if (started) {
      await navigate(`/attempts/${started.id}`);
    }
  }

  return (
    <article className="lab-detail">
      <h2 className="lab-detail__title">{lab.title}</h2>

      <dl className="lab-detail__meta">
        <dt>{t("lab.topic")}</dt>
        <dd>
          <Link to={topicHref(lab.topic)}>{lab.topic_title}</Link>
        </dd>
        <dt>{t("lab.level")}</dt>
        <dd>{t("lab.level.value", { level: lab.level })}</dd>
        <dt>{t("lab.duration")}</dt>
        <dd>{t("lab.duration.value", { minutes: lab.estimated_minutes })}</dd>
      </dl>

      <section className="lab-detail__section">
        <h3 className="lab-detail__heading">{t("lab.nodes")}</h3>
        <ul className="lab-detail__nodes">
          {lab.nodes.map((node) => (
            <li key={node.name}>
              <span className="lab-detail__node">{node.name}</span>
              <span className="lab-detail__role">{node.role}</span>
            </li>
          ))}
        </ul>
      </section>

      <section className="lab-detail__section">
        <h3 className="lab-detail__heading">{t("lab.relatedDocs")}</h3>
        {lab.related_docs.length === 0 ? (
          <p className="state">{t("lab.relatedDocs.empty")}</p>
        ) : (
          <ul className="lab-detail__docs">
            {lab.related_docs.map((doc) => (
              <li key={doc.id}>
                <Link to={docHref(doc.id)}>{doc.title}</Link>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="lab-detail__section">
        <h3 className="lab-detail__heading">{t("lab.checkpoints")}</h3>
        <ul className="lab-detail__checkpoints">
          {lab.checkpoints.map((checkpoint) => (
            <li key={checkpoint.id}>{checkpoint.title}</li>
          ))}
        </ul>
        {lab.has_hidden_checkpoints ? (
          <span className="badge badge--hidden">{t("lab.hidden")}</span>
        ) : null}
      </section>

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

      <section className="lab-detail__section">
        <h3 className="lab-detail__heading">{t("lab.modes")}</h3>
        <ul className="mode-list">
          {lab.modes.map((mode) => (
            <ModeStart
              key={mode}
              mode={mode}
              preselected={preselected === mode}
              disabled={attempt !== null}
              starting={starting?.labId === lab.id && starting.mode === mode}
              onStart={() => void handleStart(mode)}
            />
          ))}
        </ul>
      </section>
    </article>
  );
}

export function LabDetailPage() {
  const { t } = useTranslation();
  const { id } = useParams();
  const labId = id ?? "";
  const language = useAppStore((state) => state.language);
  const lab = useAppStore((state) => state.lab);
  const attemptError = useAppStore((state) => state.attemptError);
  const loadHealth = useAppStore((state) => state.loadHealth);
  const loadLab = useAppStore((state) => state.loadLab);
  const loadCurrentAttempt = useAppStore((state) => state.loadCurrentAttempt);

  useEffect(() => {
    void loadHealth();
    void loadCurrentAttempt();
  }, [loadHealth, loadCurrentAttempt]);

  useEffect(() => {
    void loadLab(labId);
  }, [loadLab, labId, language]);

  return (
    <div className="page">
      <Header />
      <main className="main">
        {lab.status === "loading" ? (
          <p className="state">{t("lab.loading")}</p>
        ) : null}

        {lab.status === "error" ? (
          <div className="banner banner--error">
            <span>{t("lab.error")}</span>
            <span className="banner__detail">{lab.message}</span>
            <button
              type="button"
              className="button"
              onClick={() => void loadLab(labId)}
            >
              {t("action.retry")}
            </button>
          </div>
        ) : null}

        {attemptError ? (
          <div className="banner banner--error">
            <span>{t("attempt.error")}</span>
            <span className="banner__detail">{attemptError}</span>
          </div>
        ) : null}

        {lab.status === "ok" ? <LabView lab={lab.lab} /> : null}

        <Link className="button" to="/">
          {t("action.back")}
        </Link>
      </main>
    </div>
  );
}

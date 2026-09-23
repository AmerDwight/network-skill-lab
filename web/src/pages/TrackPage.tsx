import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Link, useParams } from "react-router";

import type { Track } from "../api/types";
import { Header } from "../components/Header";
import { stepHref } from "../lib/paths";
import { nextStep, stepIcons } from "../lib/track";
import { useAppStore } from "../store/app";
import { useTracksStore } from "../store/tracks";

function TrackView({ track }: { track: Track }) {
  const { t } = useTranslation();
  const next = nextStep(track);

  return (
    <article className="track">
      <h2 className="track__title">{track.title}</h2>

      <ol className="track__steps">
        {track.steps.map((step) => (
          <li
            key={`${step.kind}:${step.ref}:${step.mode ?? ""}`}
            className={
              step.completed ? "track-step track-step--done" : "track-step"
            }
          >
            <span
              className="track-step__icon"
              aria-label={t(`track.kind.${step.kind}`)}
            >
              {stepIcons[step.kind]}
            </span>
            <Link className="track-step__link" to={stepHref(step)}>
              {step.title}
            </Link>
            {step.mode ? (
              <span className="badge badge--mode">
                {t(`lab.mode.${step.mode}`)}
              </span>
            ) : null}
            {step.completed ? (
              <span
                className="track-step__done"
                aria-label={t("track.completed")}
              >
                ✔
              </span>
            ) : null}
          </li>
        ))}
      </ol>

      {next === null ? (
        <p className="state">{t("track.done")}</p>
      ) : (
        <Link className="button button--primary" to={stepHref(next)}>
          {t("track.next", { title: next.title })}
        </Link>
      )}
    </article>
  );
}

export function TrackPage() {
  const { t } = useTranslation();
  const { id } = useParams();
  const trackId = id ?? "";
  const language = useAppStore((state) => state.language);
  const track = useTracksStore((state) => state.track);
  const loadTrack = useTracksStore((state) => state.loadTrack);

  useEffect(() => {
    void loadTrack(trackId);
  }, [loadTrack, trackId, language]);

  return (
    <div className="page">
      <Header />
      <main className="main">
        {track.status === "loading" ? (
          <p className="state">{t("track.loading")}</p>
        ) : null}

        {track.status === "error" ? (
          <div className="banner banner--error">
            <span>{t("track.error")}</span>
            <span className="banner__detail">{track.message}</span>
            <button
              type="button"
              className="button"
              onClick={() => void loadTrack(trackId)}
            >
              {t("action.retry")}
            </button>
          </div>
        ) : null}

        {track.status === "ok" ? <TrackView track={track.track} /> : null}

        <Link className="button" to="/tracks">
          {t("action.backToTracks")}
        </Link>
      </main>
    </div>
  );
}

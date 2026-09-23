import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";

import { Header } from "../components/Header";
import { useAppStore } from "../store/app";
import { useTracksStore } from "../store/tracks";

export function TrackListPage() {
  const { t } = useTranslation();
  const language = useAppStore((state) => state.language);
  const tracks = useTracksStore((state) => state.tracks);
  const loadTracks = useTracksStore((state) => state.loadTracks);

  useEffect(() => {
    void loadTracks();
  }, [loadTracks, language]);

  return (
    <div className="page">
      <Header />
      <main className="main">
        <h2 className="main__heading">{t("tracks.heading")}</h2>

        {tracks.status === "loading" ? (
          <p className="state">{t("tracks.loading")}</p>
        ) : null}

        {tracks.status === "error" ? (
          <div className="banner banner--error">
            <span>{t("tracks.error")}</span>
            <span className="banner__detail">{tracks.message}</span>
            <button
              type="button"
              className="button"
              onClick={() => void loadTracks()}
            >
              {t("action.retry")}
            </button>
          </div>
        ) : null}

        {tracks.status === "ok" && tracks.tracks.length === 0 ? (
          <p className="state">{t("tracks.empty")}</p>
        ) : null}

        {tracks.status === "ok" && tracks.tracks.length > 0 ? (
          <ul className="track-list">
            {tracks.tracks.map((track) => (
              <li key={track.id} className="track-card">
                <Link
                  className="track-card__link"
                  to={`/tracks/${encodeURIComponent(track.id)}`}
                >
                  <h3 className="track-card__title">{track.title}</h3>
                  <p className="track-card__meta">
                    {t("track.steps.value", { steps: track.steps })}
                    {" · "}
                    {t("track.progress", {
                      completed: track.completed,
                      total: track.steps,
                    })}
                  </p>
                  <progress
                    className="track-card__progress"
                    max={track.steps}
                    value={track.completed}
                  >
                    {track.completed}/{track.steps}
                  </progress>
                </Link>
              </li>
            ))}
          </ul>
        ) : null}
      </main>
    </div>
  );
}

import { Suspense, lazy, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { castUrl, listRecordings } from "../api/client";
import type { RecordingInfo } from "../api/types";
import { messageOf } from "../lib/errors";
import { formatBytes, formatTimeOfDay } from "../lib/format";

const CastPlayer = lazy(() => import("./CastPlayer"));

export const speeds = [1, 2, 4] as const;

type RecordingsState =
  | { status: "loading" }
  | { status: "ok"; recordings: RecordingInfo[] }
  | { status: "error"; message: string };

function Recording({
  attemptId,
  recording,
}: {
  attemptId: string;
  recording: RecordingInfo;
}) {
  const { t } = useTranslation();
  const [speed, setSpeed] = useState(1);
  const selectId = `speed-${recording.id}`;

  return (
    <figure className="recording">
      <figcaption className="recording__caption">
        <span className="recording__name">
          {recording.node} · {recording.tab}
        </span>
        <span className="recording__meta">
          {t("recordings.started")} {formatTimeOfDay(recording.started_at)}
          {" · "}
          {t("recordings.ended")} {formatTimeOfDay(recording.ended_at) ?? "—"}
          {" · "}
          {formatBytes(recording.bytes)}
        </span>
        <label className="recording__speed" htmlFor={selectId}>
          <span className="field__label">{t("recordings.speed")}</span>
          <select
            id={selectId}
            className="field__input"
            value={speed}
            onChange={(event) => setSpeed(Number(event.target.value))}
          >
            {speeds.map((value) => (
              <option key={value} value={value}>
                {t("recordings.speed.value", { speed: value })}
              </option>
            ))}
          </select>
        </label>
      </figcaption>
      <Suspense
        fallback={<p className="state">{t("recordings.playerLoading")}</p>}
      >
        <CastPlayer src={castUrl(attemptId, recording.id)} speed={speed} />
      </Suspense>
    </figure>
  );
}

export function RecordingsTab({ attemptId }: { attemptId: string }) {
  const { t } = useTranslation();
  const [state, setState] = useState<RecordingsState>({ status: "loading" });

  useEffect(() => {
    let cancelled = false;
    listRecordings(attemptId)
      .then((recordings) => {
        if (!cancelled) {
          setState({ status: "ok", recordings });
        }
      })
      .catch((error: unknown) => {
        if (!cancelled) {
          setState({ status: "error", message: messageOf(error) });
        }
      });
    return () => {
      cancelled = true;
    };
  }, [attemptId]);

  if (state.status === "loading") {
    return <p className="state">{t("recordings.loading")}</p>;
  }

  if (state.status === "error") {
    return (
      <div className="banner banner--error">
        <span>{t("recordings.error")}</span>
        <span className="banner__detail">{state.message}</span>
      </div>
    );
  }

  if (state.recordings.length === 0) {
    return <p className="state">{t("recordings.empty")}</p>;
  }

  return (
    <div className="recordings">
      {state.recordings.map((recording) => (
        <Recording
          key={recording.id}
          attemptId={attemptId}
          recording={recording}
        />
      ))}
    </div>
  );
}

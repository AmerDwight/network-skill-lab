import { useState } from "react";
import { useTranslation } from "react-i18next";

import type { AttemptCheckpoint, CheckpointStatus } from "../api/types";

const checkpointIcons: Record<CheckpointStatus, string> = {
  pending: "○",
  pass: "✔",
  fail: "✘",
  error: "!",
};

function passedAtLabel(value: string | null): string | null {
  if (value === null) {
    return null;
  }
  const time = new Date(value);
  if (Number.isNaN(time.getTime())) {
    return value;
  }
  return time.toLocaleTimeString();
}

export interface TicketPanelProps {
  title: string;
  ticket: string;
  checkpoints: AttemptCheckpoint[];
  abandoning: boolean;
  onAbandon: () => void;
}

export function TicketPanel({
  title,
  ticket,
  checkpoints,
  abandoning,
  onAbandon,
}: TicketPanelProps) {
  const { t } = useTranslation();
  const [confirming, setConfirming] = useState(false);

  return (
    <aside className="panel">
      <h1 className="panel__title">{title}</h1>
      <p className="panel__ticket">{ticket}</p>

      <h2 className="panel__heading">{t("attempt.checkpoints")}</h2>
      <ul className="checkpoints">
        {checkpoints.map((checkpoint) => {
          const passedAt = passedAtLabel(checkpoint.first_passed_at);
          return (
            <li
              key={checkpoint.id}
              className={`checkpoint checkpoint--${checkpoint.status}`}
            >
              <span
                className="checkpoint__icon"
                aria-label={t(`checkpoint.${checkpoint.status}`)}
              >
                {checkpointIcons[checkpoint.status]}
              </span>
              <span className="checkpoint__title">{checkpoint.title}</span>
              {passedAt !== null ? (
                <span className="checkpoint__time">{passedAt}</span>
              ) : null}
            </li>
          );
        })}
      </ul>

      {confirming ? (
        <div className="panel__confirm">
          <span>{t("attempt.abandon.confirm")}</span>
          <button
            type="button"
            className="button button--danger"
            disabled={abandoning}
            onClick={onAbandon}
          >
            {t("attempt.abandon.yes")}
          </button>
          <button
            type="button"
            className="button"
            disabled={abandoning}
            onClick={() => setConfirming(false)}
          >
            {t("action.cancel")}
          </button>
        </div>
      ) : (
        <button
          type="button"
          className="button"
          onClick={() => setConfirming(true)}
        >
          {t("attempt.abandon")}
        </button>
      )}
    </aside>
  );
}

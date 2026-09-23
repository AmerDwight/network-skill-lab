import { useTranslation } from "react-i18next";

import type { AttemptCheckpoint } from "../api/types";
import { checkpointIcons, formatTimeOfDay } from "../lib/format";

export interface TicketPanelProps {
  ticket: string;
  checkpoints: AttemptCheckpoint[];
}

export function TicketPanel({ ticket, checkpoints }: TicketPanelProps) {
  const { t } = useTranslation();

  return (
    <div className="ticket">
      <p className="panel__ticket">{ticket}</p>

      {checkpoints.length > 0 ? (
        <>
          <h2 className="panel__heading">{t("attempt.checkpoints")}</h2>
          <ul className="checkpoints">
            {checkpoints.map((checkpoint) => {
              const passedAt = formatTimeOfDay(checkpoint.first_passed_at);
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
        </>
      ) : null}
    </div>
  );
}

import { useTranslation } from "react-i18next";

import type { SubmitResult } from "../api/types";
import { checkpointIcons } from "../lib/format";

export interface SubmitPanelProps {
  submitting: boolean;
  submitCount: number;
  result: SubmitResult | null;
  error: string | null;
  onSubmit: () => void;
}

export function SubmitPanel({
  submitting,
  submitCount,
  result,
  error,
  onSubmit,
}: SubmitPanelProps) {
  const { t } = useTranslation();

  return (
    <div className="submit">
      <button
        type="button"
        className="button button--primary"
        disabled={submitting}
        onClick={onSubmit}
      >
        {submitting ? t("attempt.submitting") : t("attempt.submit")}
      </button>
      <p className="submit__count">
        {t("attempt.submitCount", { submits: submitCount })}
      </p>

      {error !== null ? (
        <div className="banner banner--error">
          <span>{t("attempt.submitError")}</span>
          <span className="banner__detail">{error}</span>
        </div>
      ) : null}

      {result !== null && !result.passed ? (
        <div className="submit__summary">
          <h2 className="panel__heading">{t("attempt.submitResult")}</h2>
          <ul className="checkpoints">
            {result.checkpoints.map((checkpoint) => (
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
              </li>
            ))}
          </ul>
          {result.hidden_failed > 0 ? (
            <p className="submit__hidden">
              {t("attempt.submitHiddenFailed", {
                checks: result.hidden_failed,
              })}
            </p>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

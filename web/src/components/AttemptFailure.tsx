import { useTranslation } from "react-i18next";
import { Link } from "react-router";

export const runnerDisconnected = "runner disconnected";

export function AttemptFailure({ message }: { message: string }) {
  const { t } = useTranslation();

  return (
    <div className="attempt__error">
      <h2>{t("attempt.failed")}</h2>
      <p className="state">
        {message === runnerDisconnected
          ? t("attempt.runnerDisconnected")
          : message}
      </p>
      <Link className="button" to="/">
        {t("action.back")}
      </Link>
    </div>
  );
}

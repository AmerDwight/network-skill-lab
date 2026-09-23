import { useTranslation } from "react-i18next";
import { Link, useParams } from "react-router";

export function AttemptPage() {
  const { t } = useTranslation();
  const { id } = useParams();

  return (
    <main className="main main--placeholder">
      <h1>{t("attempt.heading")}</h1>
      <p>
        {t("attempt.id")}: <code>{id}</code>
      </p>
      <p className="state">{t("attempt.placeholder")}</p>
      <Link to="/">{t("action.back")}</Link>
    </main>
  );
}

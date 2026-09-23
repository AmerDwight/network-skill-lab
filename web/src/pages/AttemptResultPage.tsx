import { useTranslation } from "react-i18next";
import { Link, useParams } from "react-router";

export function AttemptResultPage() {
  const { t } = useTranslation();
  const { id } = useParams();

  return (
    <main className="main main--placeholder">
      <h1>{t("result.heading")}</h1>
      <p>
        {t("attempt.id")}: <code>{id}</code>
      </p>
      <p className="state">{t("result.placeholder")}</p>
      <Link to="/">{t("action.back")}</Link>
    </main>
  );
}

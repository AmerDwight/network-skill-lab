import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import Markdown from "react-markdown";
import { Link, useParams } from "react-router";

import { Header } from "../components/Header";
import { useAppStore } from "../store/app";
import { useDocsStore } from "../store/docs";

export function DocPage() {
  const { t } = useTranslation();
  const params = useParams();
  const docId = params["*"] ?? "";
  const language = useAppStore((state) => state.language);
  const doc = useDocsStore((state) => state.doc);
  const marking = useDocsStore((state) => state.marking);
  const markError = useDocsStore((state) => state.markError);
  const loadDoc = useDocsStore((state) => state.loadDoc);
  const markRead = useDocsStore((state) => state.markRead);

  useEffect(() => {
    void loadDoc(docId);
  }, [loadDoc, docId, language]);

  return (
    <div className="page">
      <Header />
      <main className="main">
        {doc.status === "loading" ? (
          <p className="state">{t("doc.loading")}</p>
        ) : null}

        {doc.status === "error" ? (
          <div className="banner banner--error">
            <span>{t("doc.error")}</span>
            <span className="banner__detail">{doc.message}</span>
            <button
              type="button"
              className="button"
              onClick={() => void loadDoc(docId)}
            >
              {t("action.retry")}
            </button>
          </div>
        ) : null}

        {doc.status === "ok" ? (
          <article className="doc">
            <h2 className="doc__title">{doc.doc.title}</h2>
            <p className="doc__topic">{doc.doc.topic}</p>
            <div className="markdown">
              <Markdown>{doc.doc.body}</Markdown>
            </div>
            {markError ? (
              <div className="banner banner--error">
                <span>{t("doc.markError")}</span>
                <span className="banner__detail">{markError}</span>
              </div>
            ) : null}
            <button
              type="button"
              className="button button--primary"
              disabled={doc.doc.completed || marking}
              onClick={() => void markRead(doc.doc.id)}
            >
              {doc.doc.completed ? t("doc.read") : t("doc.markRead")}
            </button>
          </article>
        ) : null}

        <Link className="button" to="/docs">
          {t("action.backToDocs")}
        </Link>
      </main>
    </div>
  );
}

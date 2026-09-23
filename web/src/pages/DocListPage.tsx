import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";

import type { DocSummary } from "../api/types";
import { Header } from "../components/Header";
import { docHref } from "../lib/paths";
import { useAppStore } from "../store/app";
import { useDocsStore } from "../store/docs";

function groupByTopic(docs: DocSummary[]): [string, DocSummary[]][] {
  const groups = new Map<string, DocSummary[]>();
  for (const doc of docs) {
    const group = groups.get(doc.topic);
    if (group === undefined) {
      groups.set(doc.topic, [doc]);
    } else {
      group.push(doc);
    }
  }
  return [...groups];
}

export function DocListPage() {
  const { t } = useTranslation();
  const language = useAppStore((state) => state.language);
  const docs = useDocsStore((state) => state.docs);
  const loadDocs = useDocsStore((state) => state.loadDocs);

  useEffect(() => {
    void loadDocs();
  }, [loadDocs, language]);

  return (
    <div className="page">
      <Header />
      <main className="main">
        <h2 className="main__heading">{t("docs.heading")}</h2>

        {docs.status === "loading" ? (
          <p className="state">{t("docs.loading")}</p>
        ) : null}

        {docs.status === "error" ? (
          <div className="banner banner--error">
            <span>{t("docs.error")}</span>
            <span className="banner__detail">{docs.message}</span>
            <button
              type="button"
              className="button"
              onClick={() => void loadDocs()}
            >
              {t("action.retry")}
            </button>
          </div>
        ) : null}

        {docs.status === "ok" && docs.docs.length === 0 ? (
          <p className="state">{t("docs.empty")}</p>
        ) : null}

        {docs.status === "ok"
          ? groupByTopic(docs.docs).map(([topic, group]) => (
              <section key={topic} className="doc-group">
                <h3 className="doc-group__title">{topic}</h3>
                <ul className="doc-group__list">
                  {group.map((doc) => (
                    <li key={doc.id}>
                      <Link to={docHref(doc.id)}>{doc.title}</Link>
                    </li>
                  ))}
                </ul>
              </section>
            ))
          : null}
      </main>
    </div>
  );
}

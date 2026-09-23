import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import Markdown from "react-markdown";

import { getDoc } from "../api/client";
import type { DocRef } from "../api/types";
import { messageOf } from "../lib/errors";
import { useAppStore } from "../store/app";
import type { DocState } from "../store/docs";

export interface DocsTabProps {
  docs: DocRef[];
  selected: string | null;
  error: string | null;
  onSelect: (id: string) => void;
}

export function DocsTab({ docs, selected, error, onSelect }: DocsTabProps) {
  const { t } = useTranslation();
  const language = useAppStore((state) => state.language);
  const [loaded, setLoaded] = useState<{ id: string; state: DocState } | null>(
    null,
  );

  useEffect(() => {
    if (selected === null) {
      return;
    }
    let active = true;
    getDoc(selected)
      .then((body) => {
        if (active) {
          setLoaded({ id: selected, state: { status: "ok", doc: body } });
        }
      })
      .catch((failure: unknown) => {
        if (active) {
          setLoaded({
            id: selected,
            state: { status: "error", message: messageOf(failure) },
          });
        }
      });
    return () => {
      active = false;
    };
  }, [selected, language]);

  const doc: DocState =
    loaded !== null && loaded.id === selected
      ? loaded.state
      : { status: "loading" };

  if (error !== null) {
    return (
      <div className="banner banner--error">
        <span>{t("lab.error")}</span>
        <span className="banner__detail">{error}</span>
      </div>
    );
  }

  if (docs.length === 0) {
    return <p className="state">{t("lab.relatedDocs.empty")}</p>;
  }

  return (
    <div className="panel-docs">
      <div className="panel-docs__tabs" role="tablist">
        {docs.map((item) => (
          <button
            key={item.id}
            type="button"
            role="tab"
            aria-selected={item.id === selected}
            className={
              item.id === selected ? "panel-tab panel-tab--active" : "panel-tab"
            }
            onClick={() => onSelect(item.id)}
          >
            {item.title}
          </button>
        ))}
      </div>

      {doc.status === "loading" ? (
        <p className="state">{t("doc.loading")}</p>
      ) : null}

      {doc.status === "error" ? (
        <div className="banner banner--error">
          <span>{t("doc.error")}</span>
          <span className="banner__detail">{doc.message}</span>
        </div>
      ) : null}

      {doc.status === "ok" ? (
        <div className="panel-docs__body markdown">
          <Markdown>{doc.doc.body}</Markdown>
        </div>
      ) : null}
    </div>
  );
}

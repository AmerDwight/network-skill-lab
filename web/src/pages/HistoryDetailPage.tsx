import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams } from "react-router";

import { CommandsTab } from "../components/CommandsTab";
import { Header } from "../components/Header";
import { RecordingsTab } from "../components/RecordingsTab";
import { ResultView } from "../components/ResultView";
import { useAuthStore } from "../store/auth";
import { useHistoryStore } from "../store/history";

const tabs = ["result", "commands", "recordings"] as const;

type Tab = (typeof tabs)[number];

function DeleteButton({ attemptId }: { attemptId: string }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const deleting = useHistoryStore((state) => state.deleting);
  const deleteAttempt = useHistoryStore((state) => state.deleteAttempt);

  async function handleDelete() {
    if (!window.confirm(t("history.delete.confirm"))) {
      return;
    }
    if (await deleteAttempt(attemptId)) {
      await navigate("/history");
    }
  }

  return (
    <button
      type="button"
      className="button button--danger"
      disabled={deleting === attemptId}
      onClick={() => void handleDelete()}
    >
      {t("history.delete")}
    </button>
  );
}

export function HistoryDetailPage() {
  const { t } = useTranslation();
  const { id } = useParams();
  const attemptId = id ?? "";
  const [tab, setTab] = useState<Tab>("result");
  const [retries, setRetries] = useState(0);
  const me = useAuthStore((state) => state.me);
  const deleteError = useHistoryStore((state) => state.deleteError);

  return (
    <div className="page">
      <Header />
      <main className="main">
        <div className="history-detail__header">
          <h2 className="main__heading">{t("history.detail.heading")}</h2>
          {me !== null && me.role === "admin" ? (
            <DeleteButton attemptId={attemptId} />
          ) : null}
        </div>

        {deleteError !== null ? (
          <div className="banner banner--error">
            <span>{t("history.delete.error")}</span>
            <span className="banner__detail">{deleteError}</span>
          </div>
        ) : null}

        <div className="panel__tabs" role="tablist">
          {tabs.map((name) => (
            <button
              key={name}
              type="button"
              role="tab"
              aria-selected={tab === name}
              className={
                tab === name ? "panel-tab panel-tab--active" : "panel-tab"
              }
              onClick={() => setTab(name)}
            >
              {t(`history.tab.${name}`)}
            </button>
          ))}
        </div>

        <div className="history-detail__panel">
          {tab === "result" ? (
            <ResultView
              key={`${attemptId}:${retries}`}
              attemptId={attemptId}
              onRetry={() => setRetries((value) => value + 1)}
            />
          ) : null}
          {tab === "commands" ? (
            <CommandsTab key={attemptId} attemptId={attemptId} />
          ) : null}
          {tab === "recordings" ? (
            <RecordingsTab key={attemptId} attemptId={attemptId} />
          ) : null}
        </div>
      </main>
    </div>
  );
}

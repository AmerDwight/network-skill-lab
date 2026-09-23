import type { ReactNode } from "react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import Markdown from "react-markdown";
import { Link, useParams } from "react-router";

import { ApiError, getResult } from "../api/client";
import type { Result, ResultCheckpoint } from "../api/types";
import { checkpointIcons, formatElapsed, formatTimeOfDay } from "../lib/format";

type ResultState =
  | { kind: "loading" }
  | { kind: "ready"; result: Result }
  | { kind: "missing" }
  | { kind: "unfinished" }
  | { kind: "failed"; message: string };

function stateForError(error: unknown): ResultState {
  if (error instanceof ApiError) {
    if (error.status === 404) {
      return { kind: "missing" };
    }
    if (error.status === 409) {
      return { kind: "unfinished" };
    }
  }
  return {
    kind: "failed",
    message: error instanceof Error ? error.message : String(error),
  };
}

function Solution({
  markdown,
  defaultOpen,
}: {
  markdown: string;
  defaultOpen: boolean;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(defaultOpen);

  return (
    <details
      className="solution"
      open={open}
      onToggle={(event) => setOpen(event.currentTarget.open)}
    >
      <summary className="solution__summary">{t("result.solution")}</summary>
      <div className="markdown">
        <Markdown>{markdown}</Markdown>
      </div>
    </details>
  );
}

function orderCheckpoints(checkpoints: ResultCheckpoint[]): ResultCheckpoint[] {
  return [
    ...checkpoints.filter((checkpoint) => checkpoint.visible),
    ...checkpoints.filter((checkpoint) => !checkpoint.visible),
  ];
}

function ResultNotice({ children }: { children: ReactNode }) {
  const { t } = useTranslation();

  return (
    <main className="main main--placeholder">
      <h1>{t("result.heading")}</h1>
      {children}
    </main>
  );
}

function ResultView({
  attemptId,
  onRetry,
}: {
  attemptId: string;
  onRetry: () => void;
}) {
  const { t } = useTranslation();
  const [state, setState] = useState<ResultState>({ kind: "loading" });

  useEffect(() => {
    let cancelled = false;
    getResult(attemptId)
      .then((result) => {
        if (!cancelled) {
          setState({ kind: "ready", result });
        }
      })
      .catch((error: unknown) => {
        if (!cancelled) {
          setState(stateForError(error));
        }
      });
    return () => {
      cancelled = true;
    };
  }, [attemptId]);

  if (state.kind === "loading") {
    return (
      <ResultNotice>
        <p className="state">{t("result.loading")}</p>
      </ResultNotice>
    );
  }

  if (state.kind === "missing") {
    return (
      <ResultNotice>
        <p className="state">{t("result.notFound")}</p>
        <Link className="button" to="/">
          {t("action.back")}
        </Link>
      </ResultNotice>
    );
  }

  if (state.kind === "unfinished") {
    return (
      <ResultNotice>
        <p className="state">{t("result.unfinished")}</p>
        <Link className="button button--primary" to={`/attempts/${attemptId}`}>
          {t("action.openAttempt")}
        </Link>
      </ResultNotice>
    );
  }

  if (state.kind === "failed") {
    return (
      <ResultNotice>
        <div className="banner banner--error">
          <span>{t("result.error")}</span>
          <span className="banner__detail">{state.message}</span>
          <button type="button" className="button" onClick={onRetry}>
            {t("action.retry")}
          </button>
        </div>
        <Link className="button" to="/">
          {t("action.back")}
        </Link>
      </ResultNotice>
    );
  }

  const { result } = state;

  return (
    <main className="main result">
      <header className="result__header">
        <h1 className="result__title">{result.lab.title}</h1>
        <span className={`badge badge--${result.status}`}>
          {t(`status.${result.status}`)}
        </span>
      </header>

      <dl className="result__summary">
        <div className="result__metric">
          <dt>{t("result.elapsed")}</dt>
          <dd className="result__number">{formatElapsed(result.elapsed_ms)}</dd>
        </div>
        <div className="result__metric">
          <dt>{t("result.commands")}</dt>
          <dd className="result__number">{result.command_count}</dd>
        </div>
        {result.submit_count > 0 && (
          <div className="result__metric">
            <dt>{t("result.submissions")}</dt>
            <dd className="result__number">{result.submit_count}</dd>
          </div>
        )}
      </dl>

      <table className="result-table">
        <thead>
          <tr>
            <th scope="col">{t("result.checkpoint")}</th>
            <th scope="col">{t("result.status")}</th>
            <th scope="col">{t("result.passedAt")}</th>
          </tr>
        </thead>
        <tbody>
          {orderCheckpoints(result.checkpoints).map((checkpoint) => (
            <tr key={checkpoint.id}>
              <td>
                <span className="result-table__checkpoint">
                  {checkpoint.title}
                  {!checkpoint.visible && (
                    <span className="badge badge--hidden">
                      {t("result.hidden")}
                    </span>
                  )}
                </span>
              </td>
              <td
                className={`result-table__icon result-table__icon--${checkpoint.status}`}
              >
                <span aria-label={t(`checkpoint.${checkpoint.status}`)}>
                  {checkpointIcons[checkpoint.status]}
                </span>
              </td>
              <td className="result-table__time">
                {formatTimeOfDay(checkpoint.first_passed_at) ?? "—"}
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      <Solution
        markdown={result.solution}
        defaultOpen={result.status === "passed"}
      />

      <Link className="button" to="/">
        {t("action.back")}
      </Link>
    </main>
  );
}

export function AttemptResultPage() {
  const { id } = useParams();
  const attemptId = id ?? "";
  const [retries, setRetries] = useState(0);

  return (
    <ResultView
      key={`${attemptId}:${retries}`}
      attemptId={attemptId}
      onRetry={() => setRetries((value) => value + 1)}
    />
  );
}

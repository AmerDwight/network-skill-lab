import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import { getLab } from "../api/client";
import type { DocRef } from "../api/types";
import { messageOf } from "../lib/errors";
import { tutorialStepViews } from "../lib/tutorial";
import { useAttemptStore } from "../store/attempt";

import { DocsTab } from "./DocsTab";
import { SubmitPanel } from "./SubmitPanel";
import { TicketPanel } from "./TicketPanel";
import { TutorialSteps } from "./TutorialSteps";

const submitSummaryMs = 5000;

type PanelTab = "ticket" | "docs";

export interface AttemptPanelProps {
  attemptId: string;
  abandoning: boolean;
  onAbandon: () => void;
}

export function AttemptPanel({
  attemptId,
  abandoning,
  onAbandon,
}: AttemptPanelProps) {
  const { t } = useTranslation();
  const attempt = useAttemptStore((state) => state.attempt);
  const checkpointOrder = useAttemptStore((state) => state.checkpointOrder);
  const checkpoints = useAttemptStore((state) => state.checkpoints);
  const submitting = useAttemptStore((state) => state.submitting);
  const submitCount = useAttemptStore((state) => state.submitCount);
  const submitResult = useAttemptStore((state) => state.submitResult);
  const submitError = useAttemptStore((state) => state.submitError);
  const submit = useAttemptStore((state) => state.submit);
  const clearSubmitResult = useAttemptStore((state) => state.clearSubmitResult);

  const [tab, setTab] = useState<PanelTab>("ticket");
  const [confirming, setConfirming] = useState(false);
  const [relatedDocs, setRelatedDocs] = useState<DocRef[]>([]);
  const [docsError, setDocsError] = useState<string | null>(null);
  const [selectedDoc, setSelectedDoc] = useState<string | null>(null);

  const mode = attempt?.mode ?? "guided";
  const labId = attempt?.lab_id;

  useEffect(() => {
    if (labId === undefined) {
      return;
    }
    let active = true;
    getLab(labId)
      .then((lab) => {
        if (!active) {
          return;
        }
        setRelatedDocs(lab.related_docs);
        setSelectedDoc((current) => current ?? lab.related_docs[0]?.id ?? null);
      })
      .catch((failure: unknown) => {
        if (active) {
          setDocsError(messageOf(failure));
        }
      });
    return () => {
      active = false;
    };
  }, [labId]);

  useEffect(() => {
    if (submitResult === null || submitResult.passed) {
      return;
    }
    const timer = setTimeout(() => clearSubmitResult(), submitSummaryMs);
    return () => clearTimeout(timer);
  }, [submitResult, clearSubmitResult]);

  const steps = useMemo(
    () => tutorialStepViews(attempt?.tutorial_steps ?? [], checkpoints),
    [attempt, checkpoints],
  );

  const visibleCheckpoints =
    mode === "guided"
      ? checkpointOrder
          .map((id) => checkpoints[id])
          .filter((checkpoint) => checkpoint !== undefined)
      : [];

  function content() {
    if (tab === "docs") {
      return (
        <DocsTab
          docs={relatedDocs}
          selected={selectedDoc}
          error={docsError}
          onSelect={setSelectedDoc}
        />
      );
    }
    if (mode === "tutorial") {
      return <TutorialSteps steps={steps} />;
    }
    return (
      <>
        <TicketPanel
          ticket={attempt?.ticket ?? ""}
          checkpoints={visibleCheckpoints}
        />
        {mode === "real" ? (
          <SubmitPanel
            submitting={submitting}
            submitCount={submitCount}
            result={submitResult}
            error={submitError}
            onSubmit={() => void submit(attemptId)}
          />
        ) : null}
      </>
    );
  }

  return (
    <aside className="panel">
      <h1 className="panel__title">
        {attempt?.lab.title ?? t("attempt.heading")}
      </h1>

      <div className="panel__tabs" role="tablist">
        <button
          type="button"
          role="tab"
          aria-selected={tab === "ticket"}
          className={
            tab === "ticket" ? "panel-tab panel-tab--active" : "panel-tab"
          }
          onClick={() => setTab("ticket")}
        >
          {mode === "tutorial"
            ? t("attempt.tab.steps")
            : t("attempt.tab.ticket")}
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={tab === "docs"}
          className={
            tab === "docs" ? "panel-tab panel-tab--active" : "panel-tab"
          }
          onClick={() => setTab("docs")}
        >
          {t("attempt.tab.docs")}
        </button>
      </div>

      {content()}

      {confirming ? (
        <div className="panel__confirm">
          <span>{t("attempt.abandon.confirm")}</span>
          <button
            type="button"
            className="button button--danger"
            disabled={abandoning}
            onClick={onAbandon}
          >
            {t("attempt.abandon.yes")}
          </button>
          <button
            type="button"
            className="button"
            disabled={abandoning}
            onClick={() => setConfirming(false)}
          >
            {t("action.cancel")}
          </button>
        </div>
      ) : (
        <button
          type="button"
          className="button"
          onClick={() => setConfirming(true)}
        >
          {t("attempt.abandon")}
        </button>
      )}
    </aside>
  );
}

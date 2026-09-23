import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Link, useNavigate, useParams } from "react-router";

import { AttemptPanel } from "../components/AttemptPanel";
import { ProvisioningPanel } from "../components/ProvisioningPanel";
import { StatusBar } from "../components/StatusBar";
import { TerminalWorkspace } from "../components/TerminalWorkspace";
import { isResultStatus, useAttemptStore } from "../store/attempt";
import { useAttemptEvents } from "../ws/events";

const timerIntervalMs = 250;

export function AttemptPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { id } = useParams();
  const attemptId = id ?? "";

  const attempt = useAttemptStore((state) => state.attempt);
  const status = useAttemptStore((state) => state.status);
  const errorMessage = useAttemptStore((state) => state.errorMessage);
  const provisioningStep = useAttemptStore((state) => state.provisioningStep);
  const precheckAttempt = useAttemptStore((state) => state.precheckAttempt);
  const eventsConnection = useAttemptStore((state) => state.eventsConnection);
  const terminals = useAttemptStore((state) => state.terminals);
  const loading = useAttemptStore((state) => state.loading);
  const loadError = useAttemptStore((state) => state.loadError);
  const eventError = useAttemptStore((state) => state.eventError);
  const abandoning = useAttemptStore((state) => state.abandoning);
  const displayedMs = useAttemptStore((state) => state.displayedMs);
  const load = useAttemptStore((state) => state.load);
  const reset = useAttemptStore((state) => state.reset);
  const tick = useAttemptStore((state) => state.tick);
  const abandon = useAttemptStore((state) => state.abandon);

  useEffect(() => {
    void load(attemptId);
    return () => reset();
  }, [attemptId, load, reset]);

  useAttemptEvents(attemptId);

  useEffect(() => {
    const timer = setInterval(() => tick(), timerIntervalMs);
    return () => clearInterval(timer);
  }, [tick]);

  useEffect(() => {
    if (isResultStatus(status)) {
      void navigate(`/attempts/${attemptId}/result`, { replace: true });
    }
  }, [status, attemptId, navigate]);

  async function handleAbandon() {
    if (await abandon(attemptId)) {
      await navigate(`/attempts/${attemptId}/result`, { replace: true });
    }
  }

  return (
    <div className="attempt">
      <AttemptPanel
        attemptId={attemptId}
        abandoning={abandoning}
        onAbandon={() => void handleAbandon()}
      />

      <section className="attempt__workspace">
        {eventError !== null ? (
          <div className="banner banner--error">{eventError}</div>
        ) : null}

        {loadError !== null ? (
          <div className="banner banner--error">
            <span>{t("attempt.loadError")}</span>
            <span className="banner__detail">{loadError}</span>
            <Link className="button" to="/">
              {t("action.back")}
            </Link>
          </div>
        ) : null}

        {loading ? <p className="state">{t("attempt.loading")}</p> : null}

        {status === "provisioning" ? (
          <ProvisioningPanel
            step={provisioningStep}
            nodes={attempt?.nodes ?? []}
            precheckAttempt={precheckAttempt}
          />
        ) : null}

        {status === "error" ? (
          <div className="attempt__error">
            <h2>{t("attempt.failed")}</h2>
            <p className="state">{errorMessage}</p>
            <Link className="button" to="/">
              {t("action.back")}
            </Link>
          </div>
        ) : null}

        {status === "running" && attempt !== null ? (
          <TerminalWorkspace attemptId={attemptId} nodes={attempt.nodes} />
        ) : null}
      </section>

      <StatusBar
        status={status}
        elapsedMs={displayedMs}
        nodes={attempt?.nodes ?? []}
        terminals={terminals}
        eventsConnection={eventsConnection}
      />
    </div>
  );
}

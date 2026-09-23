import { useTranslation } from "react-i18next";

import type { HealthState } from "../store/app";

function flagLabel(value: boolean, present: string, absent: string): string {
  return value ? present : absent;
}

export function HealthIndicator({ state }: { state: HealthState }) {
  const { t } = useTranslation();

  if (state.status === "loading") {
    return (
      <p className="health health--pending">
        <span className="health__dot" />
        {t("health.checking")}
      </p>
    );
  }

  if (state.status === "error") {
    return (
      <p className="health health--error">
        <span className="health__dot" />
        {t("health.unreachable")}
        <span className="health__detail">{state.message}</span>
      </p>
    );
  }

  const { health } = state;
  const tone = health.ok ? "ok" : "error";

  return (
    <p className={`health health--${tone}`}>
      <span className="health__dot" />
      {health.ok ? t("health.ok") : t("health.down")}
      {health.error ? (
        <span className="health__detail">{health.error}</span>
      ) : null}
      <span className="health__detail">
        {t("health.docker")}:{" "}
        {flagLabel(health.docker, t("health.present"), t("health.absent"))}
      </span>
      <span className="health__detail">
        {t("health.image")} ({health.image_name}):{" "}
        {flagLabel(health.image, t("health.present"), t("health.absent"))}
      </span>
      <span className="health__detail">
        {t("health.memory")}:{" "}
        {t("health.memory.value", { megabytes: health.mem_available_mb })}
      </span>
    </p>
  );
}

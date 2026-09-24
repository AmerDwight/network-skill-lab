import { useTranslation } from "react-i18next";

import type { RunnerBusy } from "../api/types";

export function RunnerBusyNotice({ busy }: { busy: RunnerBusy }) {
  const { t } = useTranslation();

  return (
    <div className="banner banner--error">
      <span>{t("attempt.runnerBusy")}</span>
      <span className="banner__detail">
        {t("attempt.runnerBusy.detail", {
          active: busy.sandboxes_active,
          max: busy.sandboxes_max,
        })}
      </span>
    </div>
  );
}

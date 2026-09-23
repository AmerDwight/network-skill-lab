import { useTranslation } from "react-i18next";

import type { ProvisioningStep } from "../api/types";
import { provisioningSteps } from "../api/types";

export interface ProvisioningPanelProps {
  step: ProvisioningStep | null;
}

export function ProvisioningPanel({ step }: ProvisioningPanelProps) {
  const { t } = useTranslation();
  const current = step ?? provisioningSteps[0];

  return (
    <div className="provisioning">
      <h2 className="provisioning__title">{t("provisioning.heading")}</h2>
      <ol className="provisioning__steps">
        {provisioningSteps.map((name) => (
          <li
            key={name}
            className={
              name === current
                ? "provisioning__step provisioning__step--current"
                : "provisioning__step"
            }
            aria-current={name === current ? "step" : undefined}
          >
            {t(`provisioning.${name}`)}
          </li>
        ))}
      </ol>
    </div>
  );
}

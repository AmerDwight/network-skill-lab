import { useTranslation } from "react-i18next";

import type { LabNode, ProvisioningStep } from "../api/types";

export function hasK3sRole(nodes: LabNode[]): boolean {
  return nodes.some((node) => node.role.startsWith("k3s-"));
}

function stepsFor(
  step: ProvisioningStep | null,
  k3s: boolean,
  precheckAttempt: number | null,
): ProvisioningStep[] {
  const steps: ProvisioningStep[] = ["networks", "containers", "bootstrap"];
  if (k3s || step === "k3s") {
    steps.push("k3s");
  }
  steps.push("setup");
  if (precheckAttempt !== null) {
    steps.push("precheck");
  }
  return steps;
}

export interface ProvisioningPanelProps {
  step: ProvisioningStep | null;
  nodes: LabNode[];
  precheckAttempt: number | null;
}

export function ProvisioningPanel({
  step,
  nodes,
  precheckAttempt,
}: ProvisioningPanelProps) {
  const { t } = useTranslation();
  const steps = stepsFor(step, hasK3sRole(nodes), precheckAttempt);
  const current = step ?? steps[0];

  return (
    <div className="provisioning">
      <h2 className="provisioning__title">{t("provisioning.heading")}</h2>
      <ol className="provisioning__steps">
        {steps.map((name) => (
          <li
            key={name}
            className={
              name === current
                ? "provisioning__step provisioning__step--current"
                : "provisioning__step"
            }
            aria-current={name === current ? "step" : undefined}
          >
            {name === "precheck" &&
            precheckAttempt !== null &&
            precheckAttempt > 1
              ? t("provisioning.precheck.attempt", { attempt: precheckAttempt })
              : t(`provisioning.${name}`)}
          </li>
        ))}
      </ol>
    </div>
  );
}

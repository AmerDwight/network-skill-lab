import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import Markdown from "react-markdown";

import { checkpointIcons } from "../lib/format";
import type { TutorialStepView } from "../lib/tutorial";
import { currentStepIndex } from "../lib/tutorial";

function stateOf(step: TutorialStepView, current: boolean): string {
  if (current) {
    return "current";
  }
  return step.status === "pass" ? "done" : "later";
}

export interface TutorialStepsProps {
  steps: TutorialStepView[];
}

export function TutorialSteps({ steps }: TutorialStepsProps) {
  const { t } = useTranslation();
  const current = currentStepIndex(steps);
  const currentStep = useRef<HTMLLIElement>(null);

  useEffect(() => {
    currentStep.current?.scrollIntoView({ block: "nearest" });
  }, [current]);

  return (
    <ol className="steps">
      {steps.map((step, index) => {
        const isCurrent = index === current;
        return (
          <li
            key={step.checkpoint}
            ref={isCurrent ? currentStep : null}
            className={`step step--${stateOf(step, isCurrent)}`}
            aria-current={isCurrent ? "step" : undefined}
          >
            <div className="step__header">
              <span className="step__number">{index + 1}</span>
              <span className="step__title">{step.title}</span>
              {step.status === "pass" ? (
                <span className="step__icon" aria-label={t("checkpoint.pass")}>
                  {checkpointIcons.pass}
                </span>
              ) : null}
            </div>
            {isCurrent ? (
              <div className="markdown step__instruction">
                <Markdown>{step.instruction}</Markdown>
              </div>
            ) : null}
          </li>
        );
      })}
    </ol>
  );
}

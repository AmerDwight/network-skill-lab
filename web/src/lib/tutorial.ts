import type {
  AttemptCheckpoint,
  CheckpointStatus,
  TutorialStep,
} from "../api/types";

export interface TutorialStepView {
  checkpoint: string;
  instruction: string;
  title: string;
  status: CheckpointStatus;
}

export function tutorialStepViews(
  steps: TutorialStep[],
  checkpoints: Record<string, AttemptCheckpoint>,
): TutorialStepView[] {
  return steps.map((step) => {
    const checkpoint = checkpoints[step.checkpoint];
    return {
      checkpoint: step.checkpoint,
      instruction: step.instruction,
      title: checkpoint?.title ?? step.checkpoint,
      status: checkpoint?.status ?? "pending",
    };
  });
}

export function currentStepIndex(steps: TutorialStepView[]): number {
  return steps.findIndex((step) => step.status !== "pass");
}

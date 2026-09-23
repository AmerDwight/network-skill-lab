import type { Track, TrackStep } from "../api/types";

export const stepIcons: Record<TrackStep["kind"], string> = {
  doc: "▤",
  lab: "⌨",
};

export function nextStep(track: Track): TrackStep | null {
  return track.steps.find((step) => !step.completed) ?? null;
}

export function completedSteps(track: Track): number {
  return track.steps.filter((step) => step.completed).length;
}

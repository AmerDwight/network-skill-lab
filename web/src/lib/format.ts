import type { CheckpointStatus } from "../api/types";

export const checkpointIcons: Record<CheckpointStatus, string> = {
  pending: "○",
  pass: "✔",
  fail: "✘",
  error: "!",
};

export function formatElapsed(milliseconds: number): string {
  const totalSeconds = Math.max(0, Math.floor(milliseconds / 1000));
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  return `${String(minutes).padStart(2, "0")}:${String(seconds).padStart(2, "0")}`;
}

export function formatTimeOfDay(value: string | null): string | null {
  if (value === null) {
    return null;
  }
  const time = new Date(value);
  if (Number.isNaN(time.getTime())) {
    return value;
  }
  return time.toLocaleTimeString();
}

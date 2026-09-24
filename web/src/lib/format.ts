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

const byteUnits = ["B", "KB", "MB", "GB", "TB"];

export function formatBytes(bytes: number): string {
  let value = Math.max(0, bytes);
  let unit = 0;
  while (value >= 1024 && unit < byteUnits.length - 1) {
    value /= 1024;
    unit += 1;
  }
  const rounded = unit === 0 ? String(value) : value.toFixed(1);
  return `${rounded} ${byteUnits[unit]}`;
}

export function formatDate(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleDateString();
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

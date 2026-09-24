import type { AttemptStatus } from "../api/types";

const active: readonly AttemptStatus[] = ["provisioning", "running"];

export function isTerminal(status: AttemptStatus): boolean {
  return !active.includes(status);
}

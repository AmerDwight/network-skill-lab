import { create } from "zustand";

import { abandonAttempt, getAttempt, submitAttempt } from "../api/client";
import type {
  Attempt,
  AttemptCheckpoint,
  AttemptEvent,
  AttemptStatus,
  ProvisioningStep,
  SubmitResult,
} from "../api/types";
import { messageOf } from "../lib/errors";
import type { ConnectionState } from "../ws/socket";
import type { TerminalState } from "../ws/terminal";

export function isFinishedStatus(status: AttemptStatus | null): boolean {
  return (
    status === "passed" ||
    status === "abandoned" ||
    status === "expired" ||
    status === "error"
  );
}

export function isResultStatus(status: AttemptStatus | null): boolean {
  return status === "passed" || status === "abandoned" || status === "expired";
}

export interface AttemptState {
  attempt: Attempt | null;
  status: AttemptStatus | null;
  errorMessage: string;
  checkpointOrder: string[];
  checkpoints: Record<string, AttemptCheckpoint>;
  provisioningStep: ProvisioningStep | null;
  precheckAttempt: number | null;
  eventsConnection: ConnectionState;
  terminals: Record<string, TerminalState>;
  loading: boolean;
  loadError: string | null;
  eventError: string | null;
  abandoning: boolean;
  submitting: boolean;
  submitCount: number;
  submitResult: SubmitResult | null;
  submitError: string | null;
  elapsedMs: number;
  syncedAt: number | null;
  displayedMs: number;
  load: (id: string) => Promise<void>;
  refresh: (id: string) => Promise<void>;
  applyEvent: (event: AttemptEvent) => void;
  setEventsConnection: (state: ConnectionState) => void;
  setTerminalState: (key: string, state: TerminalState | null) => void;
  tick: () => void;
  abandon: (id: string) => Promise<boolean>;
  submit: (id: string) => Promise<void>;
  clearSubmitResult: () => void;
  reset: () => void;
}

export const initialState = {
  attempt: null,
  status: null,
  errorMessage: "",
  checkpointOrder: [] as string[],
  checkpoints: {} as Record<string, AttemptCheckpoint>,
  provisioningStep: null as ProvisioningStep | null,
  precheckAttempt: null as number | null,
  eventsConnection: "closed" as ConnectionState,
  terminals: {} as Record<string, TerminalState>,
  loading: false,
  loadError: null,
  eventError: null,
  abandoning: false,
  submitting: false,
  submitCount: 0,
  submitResult: null as SubmitResult | null,
  submitError: null as string | null,
  elapsedMs: 0,
  syncedAt: null,
  displayedMs: 0,
};

function displayFor(
  status: AttemptStatus | null,
  elapsedMs: number,
  syncedAt: number,
): number {
  return status === "running" ? elapsedMs + (Date.now() - syncedAt) : elapsedMs;
}

function fromAttempt(attempt: Attempt) {
  const syncedAt = Date.now();
  return {
    attempt,
    status: attempt.status,
    errorMessage: attempt.error_message,
    checkpointOrder: attempt.checkpoints.map((checkpoint) => checkpoint.id),
    checkpoints: Object.fromEntries(
      attempt.checkpoints.map((checkpoint) => [checkpoint.id, checkpoint]),
    ),
    provisioningStep: null as ProvisioningStep | null,
    precheckAttempt: null as number | null,
    submitCount: attempt.submit_count,
    elapsedMs: attempt.elapsed_ms,
    syncedAt,
    displayedMs: displayFor(attempt.status, attempt.elapsed_ms, syncedAt),
    loadError: null,
    loading: false,
  };
}

export const useAttemptStore = create<AttemptState>()((set, get) => ({
  ...initialState,

  load: async (id) => {
    set({ loading: true, loadError: null });
    try {
      set(fromAttempt(await getAttempt(id)));
    } catch (error) {
      set({ loading: false, loadError: messageOf(error) });
    }
  },

  refresh: async (id) => {
    try {
      const attempt = await getAttempt(id);
      set({
        ...fromAttempt(attempt),
        provisioningStep: get().provisioningStep,
        precheckAttempt: get().precheckAttempt,
      });
    } catch (error) {
      set({ loadError: messageOf(error) });
    }
  },

  applyEvent: (event) => {
    const state = get();
    switch (event.type) {
      case "status": {
        const syncedAt = Date.now();
        set({
          status: event.status,
          errorMessage: event.error_message,
          elapsedMs: event.elapsed_ms,
          syncedAt,
          displayedMs: displayFor(event.status, event.elapsed_ms, syncedAt),
          provisioningStep:
            event.status === "provisioning" ? state.provisioningStep : null,
        });
        return;
      }
      case "provisioning": {
        set({
          provisioningStep: event.step,
          precheckAttempt:
            event.step === "precheck"
              ? (event.attempt ?? 1)
              : state.precheckAttempt,
        });
        return;
      }
      case "checkpoint": {
        if (state.attempt?.mode === "real") {
          return;
        }
        const previous = state.checkpoints[event.id];
        const checkpoint: AttemptCheckpoint = {
          id: event.id,
          title: previous?.title ?? event.id,
          status: event.status,
          first_passed_at: event.first_passed_at,
        };
        set({
          checkpoints: { ...state.checkpoints, [event.id]: checkpoint },
          checkpointOrder:
            previous === undefined
              ? [...state.checkpointOrder, event.id]
              : state.checkpointOrder,
        });
        return;
      }
      case "tick": {
        const syncedAt = Date.now();
        set({
          elapsedMs: event.elapsed_ms,
          syncedAt,
          displayedMs: displayFor(state.status, event.elapsed_ms, syncedAt),
        });
        return;
      }
      case "submit": {
        set({ submitCount: event.submit_count });
        return;
      }
      case "error": {
        set({ eventError: event.message });
        return;
      }
    }
  },

  setEventsConnection: (state) => set({ eventsConnection: state }),

  setTerminalState: (key, state) => {
    const terminals = { ...get().terminals };
    if (state === null) {
      delete terminals[key];
    } else {
      terminals[key] = state;
    }
    set({ terminals });
  },

  tick: () => {
    const { status, elapsedMs, syncedAt } = get();
    if (syncedAt === null) {
      return;
    }
    set({ displayedMs: displayFor(status, elapsedMs, syncedAt) });
  },

  abandon: async (id) => {
    set({ abandoning: true });
    try {
      set({ ...fromAttempt(await abandonAttempt(id)), abandoning: false });
      return true;
    } catch (error) {
      set({ abandoning: false, loadError: messageOf(error) });
      return false;
    }
  },

  submit: async (id) => {
    set({ submitting: true, submitError: null, submitResult: null });
    try {
      const result = await submitAttempt(id);
      set({
        submitting: false,
        submitResult: result,
        submitCount: result.submit_count,
      });
    } catch (error) {
      set({ submitting: false, submitError: messageOf(error) });
    }
  },

  clearSubmitResult: () => set({ submitResult: null }),

  reset: () => set(initialState),
}));

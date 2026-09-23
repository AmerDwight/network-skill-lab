import { create } from "zustand";

import {
  ApiError,
  createAttempt,
  getCurrentAttempt,
  getHealth,
  listLabs,
} from "../api/client";
import type { Attempt, Health, LabSummary } from "../api/types";
import { changeLanguage, currentLanguage } from "../i18n";
import type { Language } from "../i18n";

export type HealthState =
  | { status: "loading" }
  | { status: "ok"; health: Health }
  | { status: "error"; message: string };

export type LabsState =
  | { status: "loading" }
  | { status: "ok"; labs: LabSummary[] }
  | { status: "error"; message: string };

export interface AppState {
  language: Language;
  health: HealthState;
  labs: LabsState;
  attempt: Attempt | null;
  startingLabId: string | null;
  attemptError: string | null;
  setLanguage: (language: Language) => Promise<void>;
  loadHealth: () => Promise<void>;
  loadLabs: () => Promise<void>;
  loadCurrentAttempt: () => Promise<void>;
  startAttempt: (labId: string) => Promise<Attempt | null>;
}

export const initialState = {
  language: currentLanguage(),
  health: { status: "loading" } as HealthState,
  labs: { status: "loading" } as LabsState,
  attempt: null,
  startingLabId: null,
  attemptError: null,
};

function messageOf(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export const useAppStore = create<AppState>()((set, get) => ({
  ...initialState,

  setLanguage: async (language) => {
    await changeLanguage(language);
    set({ language });
    await get().loadLabs();
  },

  loadHealth: async () => {
    set({ health: { status: "loading" } });
    try {
      set({ health: { status: "ok", health: await getHealth() } });
    } catch (error) {
      set({ health: { status: "error", message: messageOf(error) } });
    }
  },

  loadLabs: async () => {
    set({ labs: { status: "loading" } });
    try {
      set({ labs: { status: "ok", labs: await listLabs() } });
    } catch (error) {
      set({ labs: { status: "error", message: messageOf(error) } });
    }
  },

  loadCurrentAttempt: async () => {
    try {
      set({ attempt: await getCurrentAttempt(), attemptError: null });
    } catch (error) {
      set({ attempt: null, attemptError: messageOf(error) });
    }
  },

  startAttempt: async (labId) => {
    set({ startingLabId: labId, attemptError: null });
    try {
      const attempt = await createAttempt(labId);
      set({ attempt, startingLabId: null });
      return attempt;
    } catch (error) {
      const message = messageOf(error);
      set({ startingLabId: null });
      if (error instanceof ApiError && error.status === 409) {
        await get().loadCurrentAttempt();
      }
      set({ attemptError: message });
      return null;
    }
  },
}));

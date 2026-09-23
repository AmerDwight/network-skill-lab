import { create } from "zustand";

import {
  ApiError,
  createAttempt,
  getCurrentAttempt,
  getHealth,
  getLab,
  listLabs,
  listTopics,
} from "../api/client";
import type {
  Attempt,
  Health,
  LabDetail,
  LabMode,
  LabSummary,
  TopicNode,
} from "../api/types";
import { changeLanguage, currentLanguage } from "../i18n";
import type { Language } from "../i18n";
import { messageOf } from "../lib/errors";

export type HealthState =
  | { status: "loading" }
  | { status: "ok"; health: Health }
  | { status: "error"; message: string };

export type TopicsState =
  | { status: "loading" }
  | { status: "ok"; topics: TopicNode[] }
  | { status: "error"; message: string };

export type LabsState =
  | { status: "loading" }
  | { status: "ok"; labs: LabSummary[] }
  | { status: "error"; message: string };

export type LabState =
  | { status: "loading" }
  | { status: "ok"; lab: LabDetail }
  | { status: "error"; message: string };

export interface StartRequest {
  labId: string;
  mode: LabMode;
}

export interface AppState {
  language: Language;
  health: HealthState;
  topics: TopicsState;
  labs: LabsState;
  lab: LabState;
  attempt: Attempt | null;
  starting: StartRequest | null;
  attemptError: string | null;
  setLanguage: (language: Language) => Promise<void>;
  loadHealth: () => Promise<void>;
  loadTopics: () => Promise<void>;
  loadLabs: (topic?: string) => Promise<void>;
  loadLab: (id: string) => Promise<void>;
  loadCurrentAttempt: () => Promise<void>;
  startAttempt: (labId: string, mode: LabMode) => Promise<Attempt | null>;
}

export const initialState = {
  language: currentLanguage(),
  health: { status: "loading" } as HealthState,
  topics: { status: "loading" } as TopicsState,
  labs: { status: "loading" } as LabsState,
  lab: { status: "loading" } as LabState,
  attempt: null,
  starting: null,
  attemptError: null,
};

export const useAppStore = create<AppState>()((set, get) => ({
  ...initialState,

  setLanguage: async (language) => {
    await changeLanguage(language);
    set({ language });
  },

  loadHealth: async () => {
    set({ health: { status: "loading" } });
    try {
      set({ health: { status: "ok", health: await getHealth() } });
    } catch (error) {
      set({ health: { status: "error", message: messageOf(error) } });
    }
  },

  loadTopics: async () => {
    set({ topics: { status: "loading" } });
    try {
      set({ topics: { status: "ok", topics: await listTopics() } });
    } catch (error) {
      set({ topics: { status: "error", message: messageOf(error) } });
    }
  },

  loadLabs: async (topic) => {
    set({ labs: { status: "loading" } });
    try {
      set({ labs: { status: "ok", labs: await listLabs(topic) } });
    } catch (error) {
      set({ labs: { status: "error", message: messageOf(error) } });
    }
  },

  loadLab: async (id) => {
    set({ lab: { status: "loading" } });
    try {
      set({ lab: { status: "ok", lab: await getLab(id) } });
    } catch (error) {
      set({ lab: { status: "error", message: messageOf(error) } });
    }
  },

  loadCurrentAttempt: async () => {
    try {
      set({ attempt: await getCurrentAttempt(), attemptError: null });
    } catch (error) {
      set({ attempt: null, attemptError: messageOf(error) });
    }
  },

  startAttempt: async (labId, mode) => {
    set({ starting: { labId, mode }, attemptError: null });
    try {
      const attempt = await createAttempt(labId, mode);
      set({ attempt, starting: null });
      return attempt;
    } catch (error) {
      const message = messageOf(error);
      set({ starting: null });
      if (error instanceof ApiError && error.status === 409) {
        await get().loadCurrentAttempt();
      }
      set({ attemptError: message });
      return null;
    }
  },
}));

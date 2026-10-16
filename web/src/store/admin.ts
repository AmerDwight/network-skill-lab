import { create } from "zustand";

import {
  ApiError,
  adminAbandon,
  createRunner as requestCreateRunner,
  createUser as requestCreateUser,
  getAdminStats,
  listAdminAttempts,
  listRunners,
  listUsers,
  revokeRunner as requestRevokeRunner,
  updateUser as requestUpdateUser,
} from "../api/client";
import type {
  AdminAttempt,
  AdminRunner,
  AdminStats,
  AdminUser,
  CreateRunnerRequest,
  CreatedRunner,
  CreateUserRequest,
  UpdateUserRequest,
} from "../api/types";
import { messageOf } from "../lib/errors";

export type UsersState =
  | { status: "loading" }
  | { status: "ok"; users: AdminUser[] }
  | { status: "error"; message: string };

export type StatsState =
  | { status: "loading" }
  | { status: "ok"; stats: AdminStats }
  | { status: "error"; message: string };

export type AttemptsState =
  | { status: "loading" }
  | { status: "ok"; attempts: AdminAttempt[] }
  | { status: "error"; message: string };

export type RunnersState =
  | { status: "loading" }
  | { status: "ok"; runners: AdminRunner[] }
  | { status: "error"; message: string };

export interface ActionError {
  code: string;
  message: string;
}

export interface AdminState {
  users: UsersState;
  stats: StatsState;
  attempts: AttemptsState;
  runners: RunnersState;
  createdRunner: CreatedRunner | null;
  pending: string | null;
  actionError: ActionError | null;
  runnerError: ActionError | null;
  load: () => Promise<void>;
  loadUsers: () => Promise<void>;
  loadStats: () => Promise<void>;
  loadAttempts: () => Promise<void>;
  loadRunners: () => Promise<void>;
  createUser: (input: CreateUserRequest) => Promise<boolean>;
  updateUser: (id: string, patch: UpdateUserRequest) => Promise<boolean>;
  abandonAttempt: (id: string) => Promise<boolean>;
  createRunner: (input: CreateRunnerRequest) => Promise<boolean>;
  revokeRunner: (id: string) => Promise<boolean>;
  dismissCreatedRunner: () => void;
  reset: () => void;
}

export const initialState = {
  users: { status: "loading" } as UsersState,
  stats: { status: "loading" } as StatsState,
  attempts: { status: "loading" } as AttemptsState,
  runners: { status: "loading" } as RunnersState,
  createdRunner: null as CreatedRunner | null,
  pending: null as string | null,
  actionError: null as ActionError | null,
  runnerError: null as ActionError | null,
};

function actionErrorOf(error: unknown): ActionError {
  return {
    code: error instanceof ApiError ? error.code : "unknown",
    message: messageOf(error),
  };
}

export const useAdminStore = create<AdminState>()((set, get) => ({
  ...initialState,

  load: async () => {
    await Promise.all([
      get().loadUsers(),
      get().loadStats(),
      get().loadAttempts(),
      get().loadRunners(),
    ]);
  },

  loadUsers: async () => {
    set({ users: { status: "loading" } });
    try {
      set({ users: { status: "ok", users: await listUsers() } });
    } catch (error) {
      set({ users: { status: "error", message: messageOf(error) } });
    }
  },

  loadStats: async () => {
    set({ stats: { status: "loading" } });
    try {
      set({ stats: { status: "ok", stats: await getAdminStats() } });
    } catch (error) {
      set({ stats: { status: "error", message: messageOf(error) } });
    }
  },

  loadAttempts: async () => {
    set({ attempts: { status: "loading" } });
    try {
      set({ attempts: { status: "ok", attempts: await listAdminAttempts() } });
    } catch (error) {
      set({ attempts: { status: "error", message: messageOf(error) } });
    }
  },

  loadRunners: async () => {
    set({ runners: { status: "loading" } });
    try {
      set({ runners: { status: "ok", runners: await listRunners() } });
    } catch (error) {
      set({ runners: { status: "error", message: messageOf(error) } });
    }
  },

  createUser: async (input) => {
    set({ pending: "create", actionError: null });
    try {
      await requestCreateUser(input);
      set({ pending: null });
      await get().loadUsers();
      return true;
    } catch (error) {
      set({ pending: null, actionError: actionErrorOf(error) });
      return false;
    }
  },

  updateUser: async (id, patch) => {
    set({ pending: id, actionError: null });
    try {
      const updated = await requestUpdateUser(id, patch);
      const { users } = get();
      set({
        pending: null,
        users:
          users.status === "ok"
            ? {
                status: "ok",
                users: users.users.map((user) =>
                  user.id === id ? updated : user,
                ),
              }
            : users,
      });
      return true;
    } catch (error) {
      set({ pending: null, actionError: actionErrorOf(error) });
      return false;
    }
  },

  abandonAttempt: async (id) => {
    set({ pending: id, actionError: null });
    try {
      await adminAbandon(id);
      set({ pending: null });
      await Promise.all([get().loadAttempts(), get().loadStats()]);
      return true;
    } catch (error) {
      set({ pending: null, actionError: actionErrorOf(error) });
      return false;
    }
  },

  createRunner: async (input) => {
    set({ pending: "create-runner", runnerError: null, createdRunner: null });
    try {
      const created = await requestCreateRunner(input);
      set({ pending: null, createdRunner: created });
      await get().loadRunners();
      return true;
    } catch (error) {
      set({ pending: null, runnerError: actionErrorOf(error) });
      return false;
    }
  },

  revokeRunner: async (id) => {
    set({ pending: id, runnerError: null });
    try {
      await requestRevokeRunner(id);
      set({ pending: null });
      await Promise.all([get().loadRunners(), get().loadStats()]);
      return true;
    } catch (error) {
      set({ pending: null, runnerError: actionErrorOf(error) });
      return false;
    }
  },

  dismissCreatedRunner: () => set({ createdRunner: null }),

  reset: () => set(initialState),
}));

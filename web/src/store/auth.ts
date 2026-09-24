import { create } from "zustand";

import {
  ApiError,
  getMe,
  login as requestLogin,
  logout as requestLogout,
  updateMe,
} from "../api/client";
import { setUnauthorizedListener } from "../api/session";
import type { Me, UserLocale } from "../api/types";
import type { Language } from "../i18n";

import { useAppStore } from "./app";
import { resetUserScopedStores } from "./reset";

export type AuthStatus = "unknown" | "anonymous" | "authenticated";

export function languageForLocale(locale: UserLocale): Language {
  return locale === "zh" ? "zh-TW" : "en";
}

export function localeForLanguage(language: Language): UserLocale {
  return language === "zh-TW" ? "zh" : "en";
}

function codeOf(error: unknown): string {
  return error instanceof ApiError ? error.code : "unknown";
}

export interface AuthState {
  me: Me | null;
  status: AuthStatus;
  noUsers: boolean;
  loginError: string | null;
  submitting: boolean;
  revalidating: boolean;
  load: () => Promise<void>;
  login: (username: string, password: string) => Promise<boolean>;
  logout: () => Promise<void>;
  setLanguage: (language: Language) => Promise<void>;
  revalidate: () => Promise<void>;
  clearSession: () => void;
}

export const initialState = {
  me: null as Me | null,
  status: "unknown" as AuthStatus,
  noUsers: false,
  loginError: null as string | null,
  submitting: false,
  revalidating: false,
};

export const useAuthStore = create<AuthState>()((set, get) => ({
  ...initialState,

  load: async () => {
    try {
      set({ me: await getMe(), status: "authenticated", noUsers: false });
    } catch (error) {
      set({
        me: null,
        status: "anonymous",
        noUsers: codeOf(error) === "no_users",
      });
    }
  },

  login: async (username, password) => {
    set({ submitting: true, loginError: null });
    try {
      const me = await requestLogin(username, password);
      set({
        me,
        status: "authenticated",
        submitting: false,
        loginError: null,
        noUsers: false,
      });
      await useAppStore.getState().setLanguage(languageForLocale(me.locale));
      return true;
    } catch (error) {
      set({ submitting: false, loginError: codeOf(error) });
      return false;
    }
  },

  logout: async () => {
    await requestLogout().catch(() => undefined);
    get().clearSession();
  },

  setLanguage: async (language) => {
    await useAppStore.getState().setLanguage(language);
    if (get().status !== "authenticated") {
      return;
    }
    const me = await updateMe({ locale: localeForLanguage(language) }).catch(
      () => null,
    );
    if (me !== null) {
      set({ me });
    }
  },

  revalidate: async () => {
    if (get().revalidating) {
      return;
    }
    set({ revalidating: true });
    const me = await getMe().catch(() => null);
    set({ revalidating: false });
    if (me !== null) {
      set({ me, status: "authenticated" });
    }
  },

  clearSession: () => {
    set({ me: null, status: "anonymous", loginError: null });
    resetUserScopedStores();
  },
}));

setUnauthorizedListener(() => {
  useAuthStore.getState().clearSession();
});

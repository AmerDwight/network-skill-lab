import { beforeEach, describe, expect, it, vi } from "vitest";

import * as client from "../api/client";
import { ApiError } from "../api/client";
import { notifyUnauthorized } from "../api/session";
import type { AdminUser, Attempt, LabSummary, Me } from "../api/types";
import i18next from "../i18n";

import { initialState as adminInitialState, useAdminStore } from "./admin";
import { initialState as appInitialState, useAppStore } from "./app";
import { initialState, useAuthStore } from "./auth";

vi.mock("../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof client>();
  return {
    ApiError: actual.ApiError,
    getMe: vi.fn(),
    login: vi.fn(),
    logout: vi.fn(),
    updateMe: vi.fn(),
  };
});

const getMe = vi.mocked(client.getMe);
const login = vi.mocked(client.login);
const logout = vi.mocked(client.logout);
const updateMe = vi.mocked(client.updateMe);

const me: Me = {
  id: "u-alice",
  username: "alice",
  role: "user",
  locale: "en",
  created_at: "2026-09-12T09:30:00Z",
};

const lab: LabSummary = {
  id: "net-ip-01-link-down",
  title: "Server lost connectivity",
  topic: "net/ip",
  level: 2,
  modes: ["tutorial", "guided", "real"],
  estimated_minutes: 10,
  related_docs: [],
  has_hidden_checkpoints: false,
};

const attempt: Attempt = {
  id: "01JATTEMPT",
  lab_id: lab.id,
  mode: "guided",
  status: "running",
  error_message: "",
  lab,
  ticket: "the server cannot reach the gateway",
  nodes: [{ name: "host", role: "linux" }],
  checkpoints: [],
  checkpoints_hidden: false,
  tutorial_steps: null,
  submit_count: 0,
  elapsed_ms: 0,
  started_at: null,
  ended_at: null,
  server_time: "2026-09-23T00:00:00Z",
  created_at: "2026-09-23T00:00:00Z",
};

const adminUser: AdminUser = {
  ...me,
  disabled_at: null,
  attempts: 3,
};

beforeEach(async () => {
  vi.clearAllMocks();
  useAuthStore.setState(initialState);
  useAppStore.setState(appInitialState);
  useAdminStore.setState(adminInitialState);
  await i18next.changeLanguage("zh-TW");
});

describe("load", () => {
  it("keeps the session when the server knows the user", async () => {
    getMe.mockResolvedValue(me);

    await useAuthStore.getState().load();

    expect(useAuthStore.getState().status).toBe("authenticated");
    expect(useAuthStore.getState().me).toEqual(me);
  });

  it("falls back to anonymous on a 401", async () => {
    getMe.mockRejectedValue(new ApiError(401, "unauthorized", "sign in"));

    await useAuthStore.getState().load();

    expect(useAuthStore.getState().status).toBe("anonymous");
    expect(useAuthStore.getState().me).toBeNull();
    expect(useAuthStore.getState().noUsers).toBe(false);
  });

  it("remembers that the server has no account yet", async () => {
    getMe.mockRejectedValue(new ApiError(401, "no_users", "no account"));

    await useAuthStore.getState().load();

    expect(useAuthStore.getState().noUsers).toBe(true);
  });
});

describe("login", () => {
  it("stores the user and takes the ui language from the locale", async () => {
    login.mockResolvedValue({ ...me, locale: "zh" });

    await expect(
      useAuthStore.getState().login("alice", "alice123"),
    ).resolves.toBe(true);

    expect(login).toHaveBeenCalledWith("alice", "alice123");
    expect(useAuthStore.getState().status).toBe("authenticated");
    expect(useAppStore.getState().language).toBe("zh-TW");
  });

  it("maps an en locale onto the english ui", async () => {
    login.mockResolvedValue(me);

    await useAuthStore.getState().login("alice", "alice123");

    expect(useAppStore.getState().language).toBe("en");
  });

  it("keeps the error code of a refused login", async () => {
    login.mockRejectedValue(
      new ApiError(401, "invalid_credentials", "wrong password"),
    );

    await expect(useAuthStore.getState().login("alice", "nope")).resolves.toBe(
      false,
    );

    expect(useAuthStore.getState().loginError).toBe("invalid_credentials");
    expect(useAuthStore.getState().status).toBe("unknown");
  });
});

describe("logout", () => {
  it("drops the session even when the request fails", async () => {
    useAuthStore.setState({ me, status: "authenticated" });
    logout.mockRejectedValue(new Error("offline"));

    await useAuthStore.getState().logout();

    expect(logout).toHaveBeenCalledOnce();
    expect(useAuthStore.getState().me).toBeNull();
    expect(useAuthStore.getState().status).toBe("anonymous");
  });
});

describe("setLanguage", () => {
  it("writes the locale back while signed in", async () => {
    useAuthStore.setState({ me, status: "authenticated" });
    updateMe.mockResolvedValue({ ...me, locale: "zh" });

    await useAuthStore.getState().setLanguage("zh-TW");

    expect(updateMe).toHaveBeenCalledWith({ locale: "zh" });
    expect(useAppStore.getState().language).toBe("zh-TW");
    expect(useAuthStore.getState().me?.locale).toBe("zh");
  });

  it("only changes the ui while signed out", async () => {
    useAuthStore.setState({ status: "anonymous" });

    await useAuthStore.getState().setLanguage("en");

    expect(updateMe).not.toHaveBeenCalled();
    expect(useAppStore.getState().language).toBe("en");
  });
});

describe("revalidate", () => {
  it("asks the server once while a check is in flight", async () => {
    getMe.mockResolvedValue(me);

    await Promise.all([
      useAuthStore.getState().revalidate(),
      useAuthStore.getState().revalidate(),
    ]);

    expect(getMe).toHaveBeenCalledOnce();
    expect(useAuthStore.getState().status).toBe("authenticated");
  });
});

describe("the unauthorized subscriber", () => {
  it("clears the session when the api reports a 401", () => {
    useAuthStore.setState({ me, status: "authenticated" });

    notifyUnauthorized();

    expect(useAuthStore.getState().me).toBeNull();
    expect(useAuthStore.getState().status).toBe("anonymous");
  });
});

describe("user-scoped caches", () => {
  function fillCaches() {
    useAuthStore.setState({ me, status: "authenticated" });
    useAppStore.setState({ attempt, language: "en" });
    useAdminStore.setState({ users: { status: "ok", users: [adminUser] } });
  }

  it("drops them on logout", async () => {
    fillCaches();
    logout.mockResolvedValue(undefined);

    await useAuthStore.getState().logout();

    expect(useAppStore.getState().attempt).toBeNull();
    expect(useAdminStore.getState().users).toEqual({ status: "loading" });
  });

  it("drops them when the api reports a 401", () => {
    fillCaches();

    notifyUnauthorized();

    expect(useAppStore.getState().attempt).toBeNull();
    expect(useAdminStore.getState().users).toEqual({ status: "loading" });
  });

  it("keeps the ui language", async () => {
    fillCaches();
    logout.mockResolvedValue(undefined);

    await useAuthStore.getState().logout();

    expect(useAppStore.getState().language).toBe("en");
  });
});

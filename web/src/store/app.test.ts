import { beforeEach, describe, expect, it, vi } from "vitest";

import * as client from "../api/client";
import { ApiError } from "../api/client";
import type { Attempt, Health, LabSummary } from "../api/types";

import { initialState, useAppStore } from "./app";

vi.mock("../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof client>();
  return {
    ApiError: actual.ApiError,
    getHealth: vi.fn(),
    listLabs: vi.fn(),
    getCurrentAttempt: vi.fn(),
    createAttempt: vi.fn(),
  };
});

const getHealth = vi.mocked(client.getHealth);
const listLabs = vi.mocked(client.listLabs);
const getCurrentAttempt = vi.mocked(client.getCurrentAttempt);
const createAttempt = vi.mocked(client.createAttempt);

const health: Health = {
  ok: true,
  docker: true,
  image: true,
  image_name: "nsl/node",
  mem_available_mb: 2048,
  error: "",
};

const lab: LabSummary = {
  id: "net-ip-01-link-down",
  title: "Server lost connectivity",
  topic: "net/ip",
  level: 2,
  modes: ["guided"],
  estimated_minutes: 10,
};

const attempt: Attempt = {
  id: "01JATTEMPT",
  lab_id: lab.id,
  mode: "guided",
  status: "provisioning",
  elapsed_ms: 0,
  server_time: "2026-09-23T00:00:00Z",
  created_at: "2026-09-23T00:00:00Z",
};

beforeEach(() => {
  vi.clearAllMocks();
  useAppStore.setState(initialState);
});

describe("loadHealth", () => {
  it("stores the health payload", async () => {
    getHealth.mockResolvedValue(health);

    await useAppStore.getState().loadHealth();

    expect(useAppStore.getState().health).toEqual({ status: "ok", health });
  });

  it("stores the error message", async () => {
    getHealth.mockRejectedValue(new ApiError(404, "not_found", "no route"));

    await useAppStore.getState().loadHealth();

    expect(useAppStore.getState().health).toEqual({
      status: "error",
      message: "no route",
    });
  });
});

describe("loadLabs", () => {
  it("stores the lab list", async () => {
    listLabs.mockResolvedValue([lab]);

    await useAppStore.getState().loadLabs();

    expect(useAppStore.getState().labs).toEqual({
      status: "ok",
      labs: [lab],
    });
  });

  it("stores the error message", async () => {
    listLabs.mockRejectedValue(new ApiError(404, "not_found", "no route"));

    await useAppStore.getState().loadLabs();

    expect(useAppStore.getState().labs).toEqual({
      status: "error",
      message: "no route",
    });
  });
});

describe("loadCurrentAttempt", () => {
  it("keeps the attempt null when there is none", async () => {
    getCurrentAttempt.mockResolvedValue(null);

    await useAppStore.getState().loadCurrentAttempt();

    expect(useAppStore.getState().attempt).toBeNull();
    expect(useAppStore.getState().attemptError).toBeNull();
  });

  it("stores the running attempt", async () => {
    getCurrentAttempt.mockResolvedValue(attempt);

    await useAppStore.getState().loadCurrentAttempt();

    expect(useAppStore.getState().attempt).toEqual(attempt);
  });

  it("stores the error message", async () => {
    getCurrentAttempt.mockRejectedValue(
      new ApiError(404, "not_found", "no route"),
    );

    await useAppStore.getState().loadCurrentAttempt();

    expect(useAppStore.getState().attempt).toBeNull();
    expect(useAppStore.getState().attemptError).toBe("no route");
  });
});

describe("startAttempt", () => {
  it("returns and stores the created attempt", async () => {
    createAttempt.mockResolvedValue(attempt);

    await expect(useAppStore.getState().startAttempt(lab.id)).resolves.toEqual(
      attempt,
    );
    expect(createAttempt).toHaveBeenCalledWith(lab.id);
    expect(useAppStore.getState().attempt).toEqual(attempt);
    expect(useAppStore.getState().startingLabId).toBeNull();
  });

  it("loads the running attempt on 409", async () => {
    createAttempt.mockRejectedValue(
      new ApiError(409, "attempt_in_progress", "already running"),
    );
    getCurrentAttempt.mockResolvedValue(attempt);

    await expect(
      useAppStore.getState().startAttempt(lab.id),
    ).resolves.toBeNull();
    expect(getCurrentAttempt).toHaveBeenCalledOnce();
    expect(useAppStore.getState().attempt).toEqual(attempt);
    expect(useAppStore.getState().attemptError).toBe("already running");
  });

  it("does not look for a running attempt on other errors", async () => {
    createAttempt.mockRejectedValue(new ApiError(404, "not_found", "no route"));

    await expect(
      useAppStore.getState().startAttempt(lab.id),
    ).resolves.toBeNull();
    expect(getCurrentAttempt).not.toHaveBeenCalled();
    expect(useAppStore.getState().attemptError).toBe("no route");
    expect(useAppStore.getState().startingLabId).toBeNull();
  });
});

describe("setLanguage", () => {
  it("switches the language and reloads the labs", async () => {
    listLabs.mockResolvedValue([lab]);

    await useAppStore.getState().setLanguage("en");

    expect(useAppStore.getState().language).toBe("en");
    expect(listLabs).toHaveBeenCalledOnce();

    await useAppStore.getState().setLanguage("zh-TW");
  });
});

import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import * as client from "./api/client";
import { ApiError } from "./api/client";
import { App } from "./App";
import { initialState, useAppStore } from "./store/app";
import { initialState as authInitialState, useAuthStore } from "./store/auth";

vi.mock("./api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof client>();
  return {
    ApiError: actual.ApiError,
    getHealth: vi.fn(),
    listTopics: vi.fn(),
    listLabs: vi.fn(),
    getLab: vi.fn(),
    getCurrentAttempt: vi.fn(),
    createAttempt: vi.fn(),
    runnerBusyOf: actual.runnerBusyOf,
    getMe: vi.fn(),
    login: vi.fn(),
    logout: vi.fn(),
    updateMe: vi.fn(),
  };
});

const getMe = vi.mocked(client.getMe);
const getHealth = vi.mocked(client.getHealth);
const listTopics = vi.mocked(client.listTopics);
const listLabs = vi.mocked(client.listLabs);
const getCurrentAttempt = vi.mocked(client.getCurrentAttempt);

beforeEach(() => {
  vi.clearAllMocks();
  useAppStore.setState(initialState);
  useAuthStore.setState(authInitialState);
  getMe.mockResolvedValue({
    id: "u-alice",
    username: "alice",
    role: "user",
    locale: "zh",
    created_at: "2026-09-12T09:30:00Z",
  });
});

describe("App", () => {
  it("renders the project name", async () => {
    getHealth.mockResolvedValue({
      ok: true,
      docker: true,
      image: true,
      image_name: "nsl/node",
      instance: "test",
      mem_available_mb: 2048,
      error: "",
    });
    listTopics.mockResolvedValue([]);
    listLabs.mockResolvedValue([]);
    getCurrentAttempt.mockResolvedValue(null);

    render(<App />);

    expect(
      await screen.findByRole("heading", { name: "network-skill-lab" }),
    ).toBeDefined();
  });

  it("shows an error state when the api is not there", async () => {
    const error = new ApiError(404, "not_found", "no route for GET /api/labs");
    getHealth.mockRejectedValue(error);
    listTopics.mockRejectedValue(error);
    listLabs.mockRejectedValue(error);
    getCurrentAttempt.mockRejectedValue(error);

    render(<App />);

    const shown = await screen.findAllByText("no route for GET /api/labs");

    expect(shown.length).toBeGreaterThan(0);
  });
});

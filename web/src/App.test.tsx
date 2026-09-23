import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import * as client from "./api/client";
import { ApiError } from "./api/client";
import { App } from "./App";
import { initialState, useAppStore } from "./store/app";

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
  };
});

const getHealth = vi.mocked(client.getHealth);
const listTopics = vi.mocked(client.listTopics);
const listLabs = vi.mocked(client.listLabs);
const getCurrentAttempt = vi.mocked(client.getCurrentAttempt);

beforeEach(() => {
  vi.clearAllMocks();
  useAppStore.setState(initialState);
});

describe("App", () => {
  it("renders the project name", async () => {
    getHealth.mockResolvedValue({
      ok: true,
      docker: true,
      image: true,
      image_name: "nsl/node",
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

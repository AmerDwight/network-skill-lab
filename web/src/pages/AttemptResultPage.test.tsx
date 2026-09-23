import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import * as client from "../api/client";
import { ApiError } from "../api/client";
import type { Result } from "../api/types";
import i18next from "../i18n";

import { AttemptResultPage } from "./AttemptResultPage";

vi.mock("../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof client>();
  return { ApiError: actual.ApiError, getResult: vi.fn() };
});

const getResult = vi.mocked(client.getResult);

const attemptId = "01JB0000000000000000000000";
const passedAt = "2026-09-23T08:15:30.000Z";

function makeResult(overrides: Partial<Result> = {}): Result {
  return {
    attempt_id: attemptId,
    status: "passed",
    lab: {
      id: "net-ip-01-link-down",
      title: "Server lost connectivity",
      topic: "net/ip",
      level: 2,
      modes: ["guided"],
      estimated_minutes: 10,
    },
    elapsed_ms: 254000,
    command_count: 17,
    checkpoints: [
      {
        id: "link-up",
        title: "The link is up",
        status: "pass",
        first_passed_at: passedAt,
      },
      {
        id: "ping-ok",
        title: "The gateway answers",
        status: "pending",
        first_passed_at: null,
      },
    ],
    solution: "## Fix\n```sh\nip link set eth0 up\n```\n",
    ...overrides,
  };
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={[`/attempts/${attemptId}/result`]}>
      <Routes>
        <Route path="/attempts/:id/result" element={<AttemptResultPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

function solutionDetails(): HTMLDetailsElement {
  const summary = screen.getByText("Solution");
  const details = summary.closest("details");
  if (details === null) {
    throw new Error("the solution section is missing");
  }
  return details;
}

beforeEach(async () => {
  vi.clearAllMocks();
  await i18next.changeLanguage("en");
});

afterEach(() => {
  cleanup();
});

describe("AttemptResultPage", () => {
  it("shows a loading state while the result is on its way", () => {
    getResult.mockReturnValue(new Promise<Result>(() => {}));

    renderPage();

    expect(screen.getByText("Loading the result")).toBeDefined();
  });

  it("renders a passed result with the solution expanded", async () => {
    getResult.mockResolvedValue(makeResult());

    renderPage();

    expect(
      await screen.findByRole("heading", { name: "Server lost connectivity" }),
    ).toBeDefined();
    expect(getResult).toHaveBeenCalledWith(attemptId);
    expect(screen.getByText("Passed")).toBeDefined();
    expect(screen.getByText("04:14")).toBeDefined();
    expect(screen.getByText("17")).toBeDefined();
    expect(screen.getByText("The link is up")).toBeDefined();
    expect(
      screen.getByText(new Date(passedAt).toLocaleTimeString()),
    ).toBeDefined();
    expect(screen.getByText("—")).toBeDefined();
    expect(solutionDetails().open).toBe(true);
    expect(document.querySelector(".markdown pre code")?.textContent).toContain(
      "ip link set eth0 up",
    );
  });

  it("renders every checkpoint as a table row", async () => {
    getResult.mockResolvedValue(makeResult());

    renderPage();

    await screen.findByRole("table");

    expect(screen.getAllByRole("row")).toHaveLength(3);
    expect(screen.getByLabelText("Passed")).toBeDefined();
    expect(screen.getByLabelText("Pending")).toBeDefined();
  });

  it("collapses the solution for an abandoned attempt", async () => {
    getResult.mockResolvedValue(makeResult({ status: "abandoned" }));

    renderPage();

    expect(await screen.findByText("Abandoned")).toBeDefined();
    expect(solutionDetails().open).toBe(false);
  });

  it("reports an attempt that does not exist", async () => {
    getResult.mockRejectedValue(new ApiError(404, "not_found", "no attempt"));

    renderPage();

    expect(
      await screen.findByText("This attempt does not exist."),
    ).toBeDefined();
    expect(screen.getByRole("link", { name: "Back to labs" })).toBeDefined();
  });

  it("links back to an attempt that is still running", async () => {
    getResult.mockRejectedValue(
      new ApiError(409, "attempt_running", "not finished"),
    );

    renderPage();

    expect(
      await screen.findByText("This attempt is still running."),
    ).toBeDefined();
    expect(
      screen
        .getByRole("link", { name: "Go to the attempt" })
        .getAttribute("href"),
    ).toBe(`/attempts/${attemptId}`);
  });

  it("offers a retry after any other failure", async () => {
    getResult.mockRejectedValueOnce(new ApiError(500, "internal", "boom"));
    getResult.mockResolvedValueOnce(makeResult());

    renderPage();

    expect(await screen.findByText("Could not load the result")).toBeDefined();
    expect(screen.getByText("boom")).toBeDefined();

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));

    expect(
      await screen.findByRole("heading", { name: "Server lost connectivity" }),
    ).toBeDefined();
    expect(getResult).toHaveBeenCalledTimes(2);
  });
});

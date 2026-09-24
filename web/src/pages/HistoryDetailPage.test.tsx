import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import * as client from "../api/client";
import { ApiError } from "../api/client";
import type { Me, Result } from "../api/types";
import i18next from "../i18n";
import { initialState as appInitialState, useAppStore } from "../store/app";
import { initialState as authInitialState, useAuthStore } from "../store/auth";
import {
  initialState as historyInitialState,
  useHistoryStore,
} from "../store/history";

import { HistoryDetailPage } from "./HistoryDetailPage";

vi.mock("asciinema-player", () => ({
  create: vi.fn(() => ({ dispose: vi.fn() })),
}));

vi.mock("../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof client>();
  return {
    ApiError: actual.ApiError,
    castUrl: actual.castUrl,
    getMe: vi.fn(),
    logout: vi.fn(),
    updateMe: vi.fn(),
    getResult: vi.fn(),
    getCommands: vi.fn(),
    listRecordings: vi.fn(),
    adminDeleteAttempt: vi.fn(),
  };
});

const getResult = vi.mocked(client.getResult);
const getCommands = vi.mocked(client.getCommands);
const listRecordings = vi.mocked(client.listRecordings);
const adminDeleteAttempt = vi.mocked(client.adminDeleteAttempt);

const attemptId = "01JHIST0";

const alice: Me = {
  id: "u-alice",
  username: "alice",
  role: "user",
  locale: "en",
  created_at: "2026-09-12T09:30:00Z",
};

const admin: Me = { ...alice, id: "u-admin", username: "admin", role: "admin" };

const result: Result = {
  attempt_id: attemptId,
  status: "passed",
  lab: {
    id: "net-ip-01-link-down",
    title: "Server lost connectivity",
    topic: "net/ip",
    level: 2,
    modes: ["guided"],
    estimated_minutes: 10,
    related_docs: [],
    has_hidden_checkpoints: false,
  },
  elapsed_ms: 254000,
  command_count: 17,
  checkpoints: [
    {
      id: "link-up",
      title: "The link is up",
      status: "pass",
      first_passed_at: "2026-09-24T09:01:00Z",
      visible: true,
    },
  ],
  submit_count: 0,
  solution: "## Fix\n",
};

function renderPage() {
  return render(
    <MemoryRouter initialEntries={[`/history/${attemptId}`]}>
      <Routes>
        <Route path="/history/:id" element={<HistoryDetailPage />} />
        <Route path="/history" element={<p>history list</p>} />
      </Routes>
    </MemoryRouter>,
  );
}

beforeEach(async () => {
  vi.clearAllMocks();
  useAppStore.setState(appInitialState);
  useHistoryStore.setState(historyInitialState);
  useAuthStore.setState({
    ...authInitialState,
    me: alice,
    status: "authenticated",
  });
  await i18next.changeLanguage("en");
  getResult.mockResolvedValue(result);
  getCommands.mockResolvedValue([
    {
      id: "cmd-0",
      node: "host",
      ts: "2026-09-24T09:00:00Z",
      user: "root",
      cwd: "/root",
      command: "ip link",
      exit_code: 0,
    },
  ]);
  listRecordings.mockResolvedValue([
    {
      id: "rec-host-1",
      node: "host",
      tab: "1",
      started_at: "2026-09-24T09:00:00Z",
      ended_at: "2026-09-24T09:05:00Z",
      bytes: 1024,
    },
  ]);
});

afterEach(() => {
  cleanup();
});

describe("HistoryDetailPage", () => {
  it("opens on the result tab", async () => {
    renderPage();

    expect(
      await screen.findByRole("heading", { name: "Server lost connectivity" }),
    ).toBeDefined();
    expect(getResult).toHaveBeenCalledWith(attemptId);
    expect(getCommands).not.toHaveBeenCalled();
  });

  it("shows the still running notice with a link to the attempt", async () => {
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

  it("loads the commands when that tab is picked", async () => {
    renderPage();

    await screen.findByRole("heading", { name: "Server lost connectivity" });
    fireEvent.click(screen.getByRole("tab", { name: "Commands" }));

    expect(
      await screen.findByRole("table", { name: "Commands" }),
    ).toBeDefined();
    expect(getCommands).toHaveBeenCalledWith(attemptId, { limit: 100 });
  });

  it("loads the recordings when that tab is picked", async () => {
    renderPage();

    await screen.findByRole("heading", { name: "Server lost connectivity" });
    fireEvent.click(screen.getByRole("tab", { name: "Recordings" }));

    expect(await screen.findByText("host · 1")).toBeDefined();
    expect(listRecordings).toHaveBeenCalledWith(attemptId);
  });

  it("gives a plain user no delete button", async () => {
    renderPage();

    await screen.findByRole("heading", { name: "Server lost connectivity" });

    expect(screen.queryByRole("button", { name: "Delete attempt" })).toBeNull();
  });

  it("deletes the attempt for an admin and goes back to the list", async () => {
    useAuthStore.setState({ me: admin });
    vi.stubGlobal("confirm", vi.fn().mockReturnValue(true));
    adminDeleteAttempt.mockResolvedValue(undefined);

    renderPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Delete attempt" }),
    );

    await waitFor(() => {
      expect(adminDeleteAttempt).toHaveBeenCalledWith(attemptId);
    });
    expect(await screen.findByText("history list")).toBeDefined();
    vi.unstubAllGlobals();
  });

  it("keeps the attempt when the confirmation is declined", async () => {
    useAuthStore.setState({ me: admin });
    vi.stubGlobal("confirm", vi.fn().mockReturnValue(false));

    renderPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Delete attempt" }),
    );

    expect(adminDeleteAttempt).not.toHaveBeenCalled();
    vi.unstubAllGlobals();
  });
});

import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import * as client from "../api/client";
import type { AdminUser, HistoryItem, Me } from "../api/types";
import i18next from "../i18n";
import {
  initialState as adminInitialState,
  useAdminStore,
} from "../store/admin";
import { initialState as appInitialState, useAppStore } from "../store/app";
import { initialState as authInitialState, useAuthStore } from "../store/auth";
import {
  historyPageSize,
  initialState as historyInitialState,
  useHistoryStore,
} from "../store/history";

import { HistoryPage } from "./HistoryPage";

vi.mock("../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof client>();
  return {
    ApiError: actual.ApiError,
    getMe: vi.fn(),
    login: vi.fn(),
    logout: vi.fn(),
    updateMe: vi.fn(),
    getHealth: vi.fn(),
    listUsers: vi.fn(),
    listHistory: vi.fn(),
    adminDeleteAttempt: vi.fn(),
  };
});

const listHistory = vi.mocked(client.listHistory);
const listUsers = vi.mocked(client.listUsers);

const alice: Me = {
  id: "u-alice",
  username: "alice",
  role: "user",
  locale: "en",
  created_at: "2026-09-12T09:30:00Z",
};

const admin: Me = { ...alice, id: "u-admin", username: "admin", role: "admin" };

const users: AdminUser[] = [
  { ...admin, disabled_at: null, attempts: 4 },
  { ...alice, disabled_at: null, attempts: 1 },
];

function makeItem(overrides: Partial<HistoryItem> = {}): HistoryItem {
  return {
    id: "01JHIST0",
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
    mode: "guided",
    status: "passed",
    elapsed_ms: 254000,
    submit_count: 2,
    command_count: 17,
    created_at: "2026-09-24T09:00:00Z",
    ended_at: "2026-09-24T09:04:14Z",
    user: { id: "u-alice", username: "alice" },
    ...overrides,
  };
}

function renderPage() {
  return render(
    <MemoryRouter>
      <HistoryPage />
    </MemoryRouter>,
  );
}

function rowFor(title: string): HTMLElement {
  const cell = screen.getByText(title).closest("tr");
  if (cell === null) {
    throw new Error(`no row for ${title}`);
  }
  return cell;
}

beforeEach(async () => {
  vi.clearAllMocks();
  useAdminStore.setState(adminInitialState);
  useAppStore.setState(appInitialState);
  useHistoryStore.setState(historyInitialState);
  useAuthStore.setState({
    ...authInitialState,
    me: alice,
    status: "authenticated",
  });
  await i18next.changeLanguage("en");
  listUsers.mockResolvedValue(users);
});

afterEach(() => {
  cleanup();
});

describe("HistoryPage", () => {
  it("shows a row per attempt with its badges and counters", async () => {
    listHistory.mockResolvedValue([makeItem()]);

    renderPage();

    await screen.findByRole("table", { name: "History" });
    const row = rowFor("Server lost connectivity");
    expect(within(row).getByText("Guided")).toBeDefined();
    expect(within(row).getByText("Passed")).toBeDefined();
    expect(
      within(row).getByText(
        new Date("2026-09-24T09:00:00Z").toLocaleDateString(),
      ),
    ).toBeDefined();
    expect(within(row).getByText("04:14")).toBeDefined();
    expect(within(row).getByText("2")).toBeDefined();
    expect(within(row).getByText("17")).toBeDefined();
  });

  it("links a finished attempt to its detail page", async () => {
    listHistory.mockResolvedValue([makeItem()]);

    renderPage();

    const link = await screen.findByRole("link", {
      name: "Server lost connectivity",
    });
    expect(link.getAttribute("href")).toBe("/history/01JHIST0");
  });

  it("sends the owner of a running attempt to the attempt itself", async () => {
    listHistory.mockResolvedValue([makeItem({ status: "running" })]);

    renderPage();

    const link = await screen.findByRole("link", {
      name: "Server lost connectivity",
    });
    expect(link.getAttribute("href")).toBe("/attempts/01JHIST0");
  });

  it("leaves someone else's running attempt unclickable", async () => {
    useAuthStore.setState({ me: admin });
    listHistory.mockResolvedValue([makeItem({ status: "running" })]);

    renderPage();

    await screen.findByRole("table", { name: "History" });
    expect(
      screen.queryByRole("link", { name: "Server lost connectivity" }),
    ).toBeNull();
    expect(screen.getByText("Running")).toBeDefined();
  });

  it("offers no user selector to a plain user", async () => {
    listHistory.mockResolvedValue([]);

    renderPage();

    await screen.findByText("No attempt yet.");
    expect(screen.queryByLabelText("User")).toBeNull();
    expect(listUsers).not.toHaveBeenCalled();
  });

  it("lets an admin switch to another user", async () => {
    useAuthStore.setState({ me: admin });
    listHistory.mockResolvedValue([makeItem()]);

    renderPage();

    const select = await screen.findByLabelText("User");
    expect(await screen.findByRole("option", { name: "alice" })).toBeDefined();
    expect((select as HTMLSelectElement).value).toBe("");

    fireEvent.change(select, { target: { value: "u-alice" } });

    await waitFor(() => {
      expect(listHistory).toHaveBeenLastCalledWith({
        limit: historyPageSize,
        user_id: "u-alice",
      });
    });
  });

  it("appends the next page on demand", async () => {
    const first = Array.from({ length: historyPageSize }, (_, index) =>
      makeItem({
        id: `01JHIST${index}`,
        created_at: `2026-09-24T0${index % 9}:00:00Z`,
      }),
    );
    listHistory.mockResolvedValueOnce(first);

    renderPage();

    const button = await screen.findByRole("button", { name: "Load more" });
    listHistory.mockResolvedValueOnce([
      makeItem({
        id: "01JHISTLAST",
        lab: { ...makeItem().lab, title: "A pod keeps restarting" },
      }),
    ]);
    fireEvent.click(button);

    expect(await screen.findByText("A pod keeps restarting")).toBeDefined();
    expect(screen.queryByRole("button", { name: "Load more" })).toBeNull();
  });

  it("offers a retry after a failure", async () => {
    listHistory.mockRejectedValueOnce(new client.ApiError(500, "x", "boom"));
    listHistory.mockResolvedValueOnce([makeItem()]);

    renderPage();

    expect(await screen.findByText("boom")).toBeDefined();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));

    expect(await screen.findByText("Server lost connectivity")).toBeDefined();
  });
});

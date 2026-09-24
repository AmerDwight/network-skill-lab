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
import { ApiError } from "../api/client";
import type { AdminAttempt, AdminUser, Me } from "../api/types";
import i18next from "../i18n";
import {
  initialState as adminInitialState,
  useAdminStore,
} from "../store/admin";
import { initialState as appInitialState, useAppStore } from "../store/app";
import { initialState as authInitialState, useAuthStore } from "../store/auth";

import { AdminPage } from "./AdminPage";

vi.mock("../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof client>();
  return {
    ApiError: actual.ApiError,
    getMe: vi.fn(),
    login: vi.fn(),
    logout: vi.fn(),
    updateMe: vi.fn(),
    listUsers: vi.fn(),
    createUser: vi.fn(),
    updateUser: vi.fn(),
    listAdminAttempts: vi.fn(),
    adminAbandon: vi.fn(),
    getAdminStats: vi.fn(),
  };
});

const listUsers = vi.mocked(client.listUsers);
const createUser = vi.mocked(client.createUser);
const updateUser = vi.mocked(client.updateUser);
const listAdminAttempts = vi.mocked(client.listAdminAttempts);
const adminAbandon = vi.mocked(client.adminAbandon);
const getAdminStats = vi.mocked(client.getAdminStats);

const admin: Me = {
  id: "u-admin",
  username: "admin",
  role: "admin",
  locale: "en",
  created_at: "2026-09-01T08:00:00Z",
};

const users: AdminUser[] = [
  { ...admin, disabled_at: null, attempts: 4 },
  {
    id: "u-alice",
    username: "alice",
    role: "user",
    locale: "en",
    created_at: "2026-09-12T09:30:00Z",
    disabled_at: null,
    attempts: 1,
  },
];

const attempts: AdminAttempt[] = [
  {
    id: "01JATTEMPT",
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
    status: "running",
    elapsed_ms: 65000,
    created_at: "2026-09-24T10:00:00Z",
    user: { id: "u-alice", username: "alice" },
  },
];

function usersTable(): HTMLElement {
  return screen.getByRole("table", { name: "Users" });
}

function rowFor(username: string): HTMLElement {
  const cell = within(usersTable()).getByRole("cell", { name: username });
  const row = cell.closest("tr");
  if (row === null) {
    throw new Error(`no row for ${username}`);
  }
  return row;
}

function renderPage() {
  return render(
    <MemoryRouter>
      <AdminPage />
    </MemoryRouter>,
  );
}

beforeEach(async () => {
  vi.clearAllMocks();
  useAdminStore.setState(adminInitialState);
  useAppStore.setState(appInitialState);
  useAuthStore.setState({
    ...authInitialState,
    me: admin,
    status: "authenticated",
  });
  await i18next.changeLanguage("en");
  listUsers.mockResolvedValue(users);
  listAdminAttempts.mockResolvedValue(attempts);
  getAdminStats.mockResolvedValue({
    sandboxes_active: 1,
    sandboxes_max: 3,
    recordings_bytes: 734003200,
    attempts: 12,
  });
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("AdminPage", () => {
  it("turns a plain user away", async () => {
    useAuthStore.setState({ me: { ...admin, role: "user" } });

    renderPage();

    expect(
      await screen.findByText("This page is for administrators only."),
    ).toBeDefined();
    expect(listUsers).not.toHaveBeenCalled();
  });

  it("shows the users, the stats and the attempts in progress", async () => {
    renderPage();

    expect(await screen.findByRole("table", { name: "Users" })).toBeDefined();
    expect(within(rowFor("alice")).getByText("Active")).toBeDefined();
    expect(screen.getByText("1 / 3")).toBeDefined();
    expect(screen.getByText("700.0 MB")).toBeDefined();
    expect(
      screen.getByRole("cell", { name: "Server lost connectivity" }),
    ).toBeDefined();
  });

  it("leaves the admin no way to change its own account", async () => {
    renderPage();

    await screen.findByRole("table", { name: "Users" });

    for (const button of within(rowFor("admin")).getAllByRole("button")) {
      expect(button.hasAttribute("disabled")).toBe(true);
    }
    for (const button of within(rowFor("alice")).getAllByRole("button")) {
      expect(button.hasAttribute("disabled")).toBe(false);
    }
  });

  it("disables another user", async () => {
    updateUser.mockResolvedValue({
      ...users[1],
      disabled_at: "2026-09-24T11:00:00Z",
    });
    renderPage();

    await screen.findByRole("table", { name: "Users" });
    fireEvent.click(
      within(rowFor("alice")).getByRole("button", { name: "Disable" }),
    );

    await waitFor(() => {
      expect(updateUser).toHaveBeenCalledWith("u-alice", { disabled: true });
    });
    expect(await within(rowFor("alice")).findByText("Disabled")).toBeDefined();
  });

  it("changes the role of another user", async () => {
    updateUser.mockResolvedValue({ ...users[1], role: "admin" });
    renderPage();

    await screen.findByRole("table", { name: "Users" });
    fireEvent.click(
      within(rowFor("alice")).getByRole("button", { name: "Make admin" }),
    );

    await waitFor(() => {
      expect(updateUser).toHaveBeenCalledWith("u-alice", { role: "admin" });
    });
  });

  it("resets a password from the prompt", async () => {
    vi.stubGlobal("prompt", vi.fn().mockReturnValue("a-longer-secret"));
    updateUser.mockResolvedValue(users[1]);
    renderPage();

    await screen.findByRole("table", { name: "Users" });
    fireEvent.click(
      within(rowFor("alice")).getByRole("button", { name: "Reset password" }),
    );

    await waitFor(() => {
      expect(updateUser).toHaveBeenCalledWith("u-alice", {
        password: "a-longer-secret",
      });
    });
  });

  it("refuses a password that is too short", async () => {
    vi.stubGlobal("prompt", vi.fn().mockReturnValue("short"));
    renderPage();

    await screen.findByRole("table", { name: "Users" });
    fireEvent.click(
      within(rowFor("alice")).getByRole("button", { name: "Reset password" }),
    );

    expect(await screen.findByText("Use at least 8 characters.")).toBeDefined();
    expect(updateUser).not.toHaveBeenCalled();
  });

  it("checks the new user against the naming rules before asking the server", async () => {
    renderPage();

    await screen.findByRole("table", { name: "Users" });
    fireEvent.change(screen.getByLabelText("Username"), {
      target: { value: "Bob!" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "long-enough" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));

    expect(
      await screen.findByText("Use 3 to 32 characters from a-z, 0-9, _ and -."),
    ).toBeDefined();
    expect(createUser).not.toHaveBeenCalled();
  });

  it("creates a user the server accepts", async () => {
    createUser.mockResolvedValue({ ...users[1], username: "bob" });
    renderPage();

    await screen.findByRole("table", { name: "Users" });
    fireEvent.change(screen.getByLabelText("Username"), {
      target: { value: "bob" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "long-enough" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));

    await waitFor(() => {
      expect(createUser).toHaveBeenCalledWith({
        username: "bob",
        password: "long-enough",
        role: "user",
      });
    });
  });

  it("reports a name the server already knows", async () => {
    createUser.mockRejectedValue(new ApiError(409, "username_taken", "alice"));
    renderPage();

    await screen.findByRole("table", { name: "Users" });
    fireEvent.change(screen.getByLabelText("Username"), {
      target: { value: "alice" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "long-enough" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));

    expect(await screen.findByText("That username is taken.")).toBeDefined();
  });

  it("force abandons an attempt after a confirmation", async () => {
    vi.stubGlobal("confirm", vi.fn().mockReturnValue(true));
    adminAbandon.mockResolvedValue({} as never);
    renderPage();

    await screen.findByRole("table", { name: "Users" });
    fireEvent.click(screen.getByRole("button", { name: "Force abandon" }));

    await waitFor(() => {
      expect(adminAbandon).toHaveBeenCalledWith("01JATTEMPT");
    });
  });

  it("keeps the attempt when the confirmation is declined", async () => {
    vi.stubGlobal("confirm", vi.fn().mockReturnValue(false));
    renderPage();

    await screen.findByRole("table", { name: "Users" });
    fireEvent.click(screen.getByRole("button", { name: "Force abandon" }));

    expect(adminAbandon).not.toHaveBeenCalled();
  });
});

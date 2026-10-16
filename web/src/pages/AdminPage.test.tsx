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
import type { AdminAttempt, AdminRunner, AdminUser, Me } from "../api/types";
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
    listRunners: vi.fn(),
    createRunner: vi.fn(),
    revokeRunner: vi.fn(),
  };
});

const listUsers = vi.mocked(client.listUsers);
const createUser = vi.mocked(client.createUser);
const updateUser = vi.mocked(client.updateUser);
const listAdminAttempts = vi.mocked(client.listAdminAttempts);
const adminAbandon = vi.mocked(client.adminAbandon);
const getAdminStats = vi.mocked(client.getAdminStats);
const listRunners = vi.mocked(client.listRunners);
const createRunner = vi.mocked(client.createRunner);
const revokeRunner = vi.mocked(client.revokeRunner);

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

const runners: AdminRunner[] = [
  {
    id: "",
    name: "local",
    capabilities: ["container"],
    capacity: 3,
    active: 1,
    connected: true,
    last_seen_at: null,
    version: "",
    revoked_at: null,
  },
  {
    id: "r-lab",
    name: "lab-01",
    capabilities: ["container", "vm"],
    capacity: 2,
    active: 0,
    connected: true,
    last_seen_at: "2026-10-16T08:00:00.000Z",
    version: "v0.4.0",
    revoked_at: null,
  },
  {
    id: "r-old",
    name: "old-01",
    capabilities: ["container"],
    capacity: 1,
    active: 0,
    connected: false,
    last_seen_at: null,
    version: "",
    revoked_at: "2026-10-01T08:00:00.000Z",
  },
];

function runnerRow(name: string): HTMLElement {
  const table = screen.getByRole("table", { name: "Runners" });
  const row = within(table).getByRole("cell", { name }).closest("tr");
  if (row === null) {
    throw new Error(`no row for runner ${name}`);
  }
  return row;
}

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
  listRunners.mockResolvedValue(runners);
  getAdminStats.mockResolvedValue({
    sandboxes_active: 1,
    sandboxes_max: 3,
    recordings_bytes: 734003200,
    attempts: 12,
    oldest_attempt_at: "2026-08-02T09:15:00.000Z",
    runners: [],
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
      screen.getByText(
        new Date("2026-08-02T09:15:00.000Z").toLocaleDateString(),
      ),
    ).toBeDefined();
    expect(
      screen.getByRole("cell", { name: "Server lost connectivity" }),
    ).toBeDefined();
  });

  it("says there is no oldest record before any attempt", async () => {
    getAdminStats.mockResolvedValue({
      sandboxes_active: 0,
      sandboxes_max: 3,
      recordings_bytes: 0,
      attempts: 0,
      oldest_attempt_at: null,
      runners: [],
    });
    renderPage();

    expect(await screen.findByText("None yet")).toBeDefined();
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

  it("lists the runners with their capacity and connection", async () => {
    renderPage();

    await screen.findByRole("table", { name: "Runners" });

    const local = runnerRow("local");
    expect(within(local).getByText("Built in")).toBeDefined();
    expect(within(local).queryByRole("button")).toBeNull();

    const lab = runnerRow("lab-01");
    expect(within(lab).getByText("Container, VM")).toBeDefined();
    expect(within(lab).getByText("Connected")).toBeDefined();
    expect(within(lab).getByText("v0.4.0")).toBeDefined();
    expect(within(lab).getByRole("button", { name: "Revoke" })).toBeDefined();

    const old = runnerRow("old-01");
    expect(within(old).getByText("Revoked")).toBeDefined();
    expect(within(old).getByText("Never")).toBeDefined();
    expect(within(old).queryByRole("button")).toBeNull();
  });

  it("creates a runner and shows its token once with the command", async () => {
    createRunner.mockResolvedValue({
      id: "r-new",
      name: "lab-02",
      token: "s3cret-token",
    });
    renderPage();

    await screen.findByRole("table", { name: "Runners" });
    fireEvent.change(screen.getByLabelText("Runner name"), {
      target: { value: "lab-02" },
    });
    fireEvent.click(screen.getByRole("checkbox", { name: "VM" }));
    fireEvent.change(screen.getByLabelText("Sandboxes at once"), {
      target: { value: "2" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create token" }));

    await waitFor(() => {
      expect(createRunner).toHaveBeenCalledWith({
        name: "lab-02",
        capabilities: ["container", "vm"],
        capacity: 2,
      });
    });
    expect(await screen.findByText("s3cret-token")).toBeDefined();
    expect(
      screen.getByText("Copy it now; it is shown only once."),
    ).toBeDefined();
    expect(
      screen.getByText(
        /^nsl runner --url wss?:\/\/[^ ]+\/ws\/runner --name lab-02 --token-file \.\/lab-02\.token$/,
      ),
    ).toBeDefined();
    expect(listRunners).toHaveBeenCalledTimes(2);

    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    expect(screen.queryByText("s3cret-token")).toBeNull();
  });

  it("checks the runner name before asking the server", async () => {
    renderPage();

    await screen.findByRole("table", { name: "Runners" });
    fireEvent.change(screen.getByLabelText("Runner name"), {
      target: { value: "local" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create token" }));

    expect(await screen.findByText(/local is reserved/)).toBeDefined();
    expect(createRunner).not.toHaveBeenCalled();
  });

  it("reports a runner name the server already knows", async () => {
    createRunner.mockRejectedValue(new ApiError(409, "runner_exists", "x"));
    renderPage();

    await screen.findByRole("table", { name: "Runners" });
    fireEvent.change(screen.getByLabelText("Runner name"), {
      target: { value: "lab-01" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create token" }));

    expect(
      await screen.findByText("A runner with that name already exists."),
    ).toBeDefined();
  });

  it("revokes a runner after a confirmation", async () => {
    vi.stubGlobal("confirm", vi.fn().mockReturnValue(true));
    revokeRunner.mockResolvedValue();
    renderPage();

    await screen.findByRole("table", { name: "Runners" });
    fireEvent.click(
      within(runnerRow("lab-01")).getByRole("button", { name: "Revoke" }),
    );

    await waitFor(() => {
      expect(revokeRunner).toHaveBeenCalledWith("r-lab");
    });
  });

  it("keeps the runner when the revoke is declined", async () => {
    vi.stubGlobal("confirm", vi.fn().mockReturnValue(false));
    renderPage();

    await screen.findByRole("table", { name: "Runners" });
    fireEvent.click(
      within(runnerRow("lab-01")).getByRole("button", { name: "Revoke" }),
    );

    expect(revokeRunner).not.toHaveBeenCalled();
  });

  it("keeps the attempt when the confirmation is declined", async () => {
    vi.stubGlobal("confirm", vi.fn().mockReturnValue(false));
    renderPage();

    await screen.findByRole("table", { name: "Users" });
    fireEvent.click(screen.getByRole("button", { name: "Force abandon" }));

    expect(adminAbandon).not.toHaveBeenCalled();
  });
});

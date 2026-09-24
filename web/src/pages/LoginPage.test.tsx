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
import type { Me } from "../api/types";
import i18next from "../i18n";
import { initialState as appInitialState, useAppStore } from "../store/app";
import { initialState, useAuthStore } from "../store/auth";

import { LoginPage } from "./LoginPage";

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

const me: Me = {
  id: "u-alice",
  username: "alice",
  role: "user",
  locale: "en",
  created_at: "2026-09-12T09:30:00Z",
};

function renderPage(entry = "/login") {
  return render(
    <MemoryRouter initialEntries={[entry]}>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="/tracks" element={<p>the tracks page</p>} />
        <Route path="/" element={<p>the lab list</p>} />
      </Routes>
    </MemoryRouter>,
  );
}

function signIn() {
  fireEvent.change(screen.getByLabelText("Username"), {
    target: { value: "alice" },
  });
  fireEvent.change(screen.getByLabelText("Password"), {
    target: { value: "alice123" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
}

beforeEach(async () => {
  vi.clearAllMocks();
  useAuthStore.setState(initialState);
  useAppStore.setState(appInitialState);
  await i18next.changeLanguage("en");
  getMe.mockRejectedValue(new ApiError(401, "unauthorized", "sign in"));
});

afterEach(() => {
  cleanup();
});

describe("LoginPage", () => {
  it("signs in and returns to the path that was asked for", async () => {
    login.mockResolvedValue(me);
    renderPage("/login?next=%2Ftracks");

    signIn();

    expect(await screen.findByText("the tracks page")).toBeDefined();
    expect(login).toHaveBeenCalledWith("alice", "alice123");
  });

  it("ignores a next that points off the site", async () => {
    login.mockResolvedValue(me);
    renderPage("/login?next=https%3A%2F%2Felsewhere.example");

    signIn();

    expect(await screen.findByText("the lab list")).toBeDefined();
  });

  it.each([
    ["invalid_credentials", 401, "Wrong username or password."],
    ["user_disabled", 403, "This account is disabled."],
    [
      "too_many_attempts",
      429,
      "Too many failed attempts. Try again in a minute.",
    ],
    ["unexpected", 500, "Could not sign in."],
  ])("shows the message for %s", async (code, status, message) => {
    login.mockRejectedValue(new ApiError(status, code, "server text"));
    renderPage();

    signIn();

    expect(await screen.findByText(message)).toBeDefined();
  });

  it("points at the cli when the server has no account yet", async () => {
    getMe.mockRejectedValue(new ApiError(401, "no_users", "no account"));
    renderPage();

    expect(
      await screen.findByText(
        "No account exists yet. Run nsl user add on the server to create the first one.",
      ),
    ).toBeDefined();
  });

  it("switches the language without a session", async () => {
    renderPage();

    fireEvent.click(screen.getByRole("button", { name: "繁體中文" }));

    await waitFor(() => {
      expect(screen.getByRole("heading", { name: "登入" })).toBeDefined();
    });
  });
});

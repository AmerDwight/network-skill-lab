import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import * as client from "../api/client";
import { ApiError } from "../api/client";
import type { Me } from "../api/types";
import i18next from "../i18n";
import { initialState, useAuthStore } from "../store/auth";

import { AuthGate } from "./AuthGate";

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

const me: Me = {
  id: "u-alice",
  username: "alice",
  role: "user",
  locale: "en",
  created_at: "2026-09-12T09:30:00Z",
};

function LoginStub() {
  const location = useLocation();
  return <p>{`login ${location.search}`}</p>;
}

function renderGate(entry: string) {
  return render(
    <MemoryRouter initialEntries={[entry]}>
      <Routes>
        <Route path="/login" element={<LoginStub />} />
        <Route element={<AuthGate />}>
          <Route path="/labs/:id" element={<p>the lab page</p>} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );
}

beforeEach(async () => {
  vi.clearAllMocks();
  useAuthStore.setState(initialState);
  await i18next.changeLanguage("en");
});

afterEach(() => {
  cleanup();
});

describe("AuthGate", () => {
  it("shows a loading state while the session is unknown", () => {
    getMe.mockReturnValue(new Promise(() => undefined));

    renderGate("/labs/net-ip-01");

    expect(screen.getByText("Checking your session")).toBeDefined();
  });

  it("renders the route once the user is known", async () => {
    getMe.mockResolvedValue(me);

    renderGate("/labs/net-ip-01");

    expect(await screen.findByText("the lab page")).toBeDefined();
  });

  it("sends an anonymous visitor to the login page with the path to return to", async () => {
    getMe.mockRejectedValue(new ApiError(401, "unauthorized", "sign in"));

    renderGate("/labs/net-ip-01?mode=real");

    expect(
      await screen.findByText("login ?next=%2Flabs%2Fnet-ip-01%3Fmode%3Dreal"),
    ).toBeDefined();
  });
});

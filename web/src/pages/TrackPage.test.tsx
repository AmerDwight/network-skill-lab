import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import * as client from "../api/client";
import type { Track } from "../api/types";
import i18next from "../i18n";
import { initialState as appInitialState, useAppStore } from "../store/app";
import { initialState, useTracksStore } from "../store/tracks";

import { TrackPage } from "./TrackPage";

vi.mock("../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof client>();
  return {
    ApiError: actual.ApiError,
    getHealth: vi.fn(),
    listTopics: vi.fn(),
    listLabs: vi.fn(),
    getLab: vi.fn(),
    getCurrentAttempt: vi.fn(),
    createAttempt: vi.fn(),
    listTracks: vi.fn(),
    getTrack: vi.fn(),
  };
});

const getHealth = vi.mocked(client.getHealth);
const getTrack = vi.mocked(client.getTrack);

const track: Track = {
  id: "network-basics",
  title: "Network basics",
  steps: [
    {
      kind: "doc",
      ref: "net/ip/guide",
      title: "IP guide",
      mode: null,
      completed: true,
    },
    {
      kind: "lab",
      ref: "net-ip-01-link-down",
      title: "Link down",
      mode: "tutorial",
      completed: true,
    },
    {
      kind: "lab",
      ref: "net-ip-01-link-down",
      title: "Link down",
      mode: "guided",
      completed: false,
    },
    {
      kind: "doc",
      ref: "net/dns/guide",
      title: "DNS guide",
      mode: null,
      completed: false,
    },
  ],
};

function renderPage() {
  return render(
    <MemoryRouter initialEntries={[`/tracks/${track.id}`]}>
      <Routes>
        <Route path="/tracks/:id" element={<TrackPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

beforeEach(async () => {
  vi.clearAllMocks();
  useAppStore.setState(appInitialState);
  useTracksStore.setState(initialState);
  await i18next.changeLanguage("en");
  getHealth.mockResolvedValue({
    ok: true,
    docker: true,
    image: true,
    image_name: "nsl/node",
    mem_available_mb: 2048,
    error: "",
  });
  getTrack.mockResolvedValue(track);
});

afterEach(() => {
  cleanup();
});

describe("TrackPage", () => {
  it("lists the steps in order with their kind, mode and progress", async () => {
    renderPage();

    await screen.findByRole("heading", { name: "Network basics" });

    expect(getTrack).toHaveBeenCalledWith(track.id);
    const steps = screen.getAllByRole("listitem");
    expect(steps.map((step) => step.textContent?.includes("✔"))).toEqual([
      true,
      true,
      false,
      false,
    ]);
    expect(screen.getAllByLabelText("Doc")).toHaveLength(2);
    expect(screen.getAllByLabelText("Lab")).toHaveLength(2);
    expect(screen.getAllByText("Tutorial")).toHaveLength(1);
    expect(
      screen.getByRole("link", { name: "IP guide" }).getAttribute("href"),
    ).toBe("/docs/net/ip/guide");
  });

  it("points next at the first step that is not completed", async () => {
    renderPage();

    const next = await screen.findByRole("link", { name: /^Next:/ });

    expect(next.textContent).toBe("Next: Link down");
    expect(next.getAttribute("href")).toBe(
      "/labs/net-ip-01-link-down?mode=guided",
    );
  });

  it("says the track is done once every step is completed", async () => {
    getTrack.mockResolvedValue({
      ...track,
      steps: track.steps.map((step) => ({ ...step, completed: true })),
    });
    renderPage();

    expect(await screen.findByText("Every step is done.")).toBeDefined();
    expect(screen.queryByRole("link", { name: /^Next:/ })).toBeNull();
  });
});

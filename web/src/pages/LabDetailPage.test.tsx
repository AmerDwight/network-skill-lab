import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import * as client from "../api/client";
import type { Attempt, LabDetail, LabMode } from "../api/types";
import i18next from "../i18n";
import { initialState, useAppStore } from "../store/app";

import { LabDetailPage } from "./LabDetailPage";

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
  };
});

const getHealth = vi.mocked(client.getHealth);
const getLab = vi.mocked(client.getLab);
const getCurrentAttempt = vi.mocked(client.getCurrentAttempt);
const createAttempt = vi.mocked(client.createAttempt);

const lab: LabDetail = {
  id: "net-ip-01-link-down",
  title: "Server lost connectivity",
  topic: "net/ip",
  topic_title: "IP and links",
  level: 2,
  modes: ["tutorial", "guided", "real"],
  estimated_minutes: 10,
  related_docs: [{ id: "net/ip/guide", title: "IP guide" }],
  has_hidden_checkpoints: true,
  nodes: [
    { name: "host", role: "linux" },
    { name: "gw", role: "router" },
  ],
  checkpoints: [{ id: "link-up", title: "The link is up" }],
};

const attempt: Attempt = {
  id: "01JATTEMPT",
  lab_id: lab.id,
  mode: "guided",
  status: "provisioning",
  error_message: "",
  lab,
  ticket: "the server cannot reach the gateway",
  nodes: lab.nodes,
  checkpoints: [],
  elapsed_ms: 0,
  started_at: null,
  ended_at: null,
  server_time: "2026-09-23T00:00:00Z",
  created_at: "2026-09-23T00:00:00Z",
};

function renderPage(entry = `/labs/${lab.id}`) {
  return render(
    <MemoryRouter initialEntries={[entry]}>
      <Routes>
        <Route path="/labs/:id" element={<LabDetailPage />} />
        <Route path="/attempts/:id" element={<p>the attempt page</p>} />
      </Routes>
    </MemoryRouter>,
  );
}

function startButtonFor(mode: string): HTMLElement {
  const item = screen.getByRole("heading", { name: mode }).closest("li");
  if (item === null) {
    throw new Error(`no start block for ${mode}`);
  }
  return within(item).getByRole("button");
}

beforeEach(async () => {
  vi.clearAllMocks();
  useAppStore.setState(initialState);
  await i18next.changeLanguage("en");
  getHealth.mockResolvedValue({
    ok: true,
    docker: true,
    image: true,
    image_name: "nsl/node",
    mem_available_mb: 2048,
    error: "",
  });
  getCurrentAttempt.mockResolvedValue(null);
  getLab.mockResolvedValue(lab);
});

afterEach(() => {
  cleanup();
});

describe("LabDetailPage", () => {
  it("shows the lab, its nodes, docs and visible checkpoints", async () => {
    renderPage();

    expect(
      await screen.findByRole("heading", { name: "Server lost connectivity" }),
    ).toBeDefined();
    expect(getLab).toHaveBeenCalledWith(lab.id);
    expect(
      screen.getByRole("link", { name: "IP and links" }).getAttribute("href"),
    ).toBe("/?topic=net%2Fip");
    expect(screen.getByText("host")).toBeDefined();
    expect(screen.getByText("router")).toBeDefined();
    expect(
      screen.getByRole("link", { name: "IP guide" }).getAttribute("href"),
    ).toBe("/docs/net/ip/guide");
    expect(screen.getByText("The link is up")).toBeDefined();
    expect(screen.getByText("Has hidden checkpoints")).toBeDefined();
  });

  it("offers one start button per mode with a description", async () => {
    renderPage();

    await screen.findByRole("heading", { name: "Tutorial" });

    for (const mode of ["Tutorial", "Guided", "Real"]) {
      expect(startButtonFor(mode).textContent).toBe("Start");
    }
    expect(
      screen.getByText("See the checkpoints turn green while you work."),
    ).toBeDefined();
  });

  it.each<[string, LabMode]>([
    ["Tutorial", "tutorial"],
    ["Guided", "guided"],
    ["Real", "real"],
  ])("starts %s in its own mode", async (label, mode) => {
    createAttempt.mockResolvedValue({ ...attempt, mode });
    renderPage();

    await screen.findByRole("heading", { name: label });
    fireEvent.click(startButtonFor(label));

    expect(await screen.findByText("the attempt page")).toBeDefined();
    expect(createAttempt).toHaveBeenCalledWith(lab.id, mode);
  });

  it("marks the mode asked for by the query as preselected", async () => {
    renderPage(`/labs/${lab.id}?mode=real`);

    await screen.findByRole("heading", { name: "Real" });

    expect(startButtonFor("Real").closest("li")?.className).toContain(
      "mode--preselected",
    );
    expect(startButtonFor("Guided").closest("li")?.className).not.toContain(
      "mode--preselected",
    );
  });

  it("disables every mode while another attempt is running", async () => {
    getCurrentAttempt.mockResolvedValue(attempt);
    renderPage();

    expect(
      await screen.findByText("A lab is already in progress."),
    ).toBeDefined();
    for (const mode of ["Tutorial", "Guided", "Real"]) {
      expect(startButtonFor(mode).hasAttribute("disabled")).toBe(true);
    }
    expect(createAttempt).not.toHaveBeenCalled();
  });
});

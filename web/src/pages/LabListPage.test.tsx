import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import * as client from "../api/client";
import type { LabSummary, TopicNode } from "../api/types";
import i18next from "../i18n";
import { initialState, useAppStore } from "../store/app";

import { LabListPage } from "./LabListPage";

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
    runnerBusyOf: actual.runnerBusyOf,
  };
});

const getHealth = vi.mocked(client.getHealth);
const listTopics = vi.mocked(client.listTopics);
const listLabs = vi.mocked(client.listLabs);
const getCurrentAttempt = vi.mocked(client.getCurrentAttempt);

const topics: TopicNode[] = [
  {
    id: "net",
    title: "Networking",
    labs: 2,
    docs: 1,
    children: [
      { id: "net/ip", title: "IP and links", labs: 1, docs: 1, children: [] },
      { id: "net/dns", title: "DNS", labs: 1, docs: 0, children: [] },
    ],
  },
  { id: "k3s", title: "Kubernetes", labs: 1, docs: 0, children: [] },
];

const labs: LabSummary[] = [
  {
    id: "net-ip-01-link-down",
    title: "Server lost connectivity",
    topic: "net/ip",
    level: 2,
    modes: ["tutorial", "guided"],
    estimated_minutes: 10,
    related_docs: [{ id: "net/ip/guide", title: "IP guide" }],
    has_hidden_checkpoints: true,
  },
];

function renderPage(entry = "/") {
  return render(
    <MemoryRouter initialEntries={[entry]}>
      <Routes>
        <Route path="/" element={<LabListPage />} />
      </Routes>
    </MemoryRouter>,
  );
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
  listTopics.mockResolvedValue(topics);
  listLabs.mockResolvedValue(labs);
});

afterEach(() => {
  cleanup();
});

describe("LabListPage topic tree", () => {
  it("shows the roots with their counts and all topics on top", async () => {
    renderPage();

    const all = await screen.findByRole("link", { name: /All topics/ });

    expect(all.getAttribute("href")).toBe("/");
    expect(all.getAttribute("aria-current")).toBe("page");
    expect(all.textContent).toContain("3");
    expect(
      screen.getByRole("link", { name: /Networking/ }).textContent,
    ).toContain("2");
    expect(listLabs).toHaveBeenCalledWith(undefined);
  });

  it("keeps the children collapsed until the topic is expanded", async () => {
    renderPage();

    await screen.findByRole("link", { name: /Networking/ });

    expect(screen.queryByRole("link", { name: /IP and links/ })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Expand Networking" }));

    expect(screen.getByRole("link", { name: /IP and links/ })).toBeDefined();

    fireEvent.click(
      screen.getByRole("button", { name: "Collapse Networking" }),
    );

    expect(screen.queryByRole("link", { name: /IP and links/ })).toBeNull();
  });

  it("expands the ancestors of the selected topic and asks for its labs", async () => {
    renderPage("/?topic=net%2Fdns");

    const selected = await screen.findByRole("link", { name: /DNS/ });

    expect(selected.getAttribute("aria-current")).toBe("page");
    expect(
      screen
        .getByRole("link", { name: /All topics/ })
        .getAttribute("aria-current"),
    ).toBeNull();
    expect(listLabs).toHaveBeenCalledWith("net/dns");
  });

  it("reloads the labs of the topic that was clicked", async () => {
    renderPage();

    fireEvent.click(await screen.findByRole("link", { name: /Kubernetes/ }));

    expect(listLabs).toHaveBeenLastCalledWith("k3s");
  });
});

describe("LabListPage lab cards", () => {
  it("links each card to the lab detail page", async () => {
    renderPage();

    const card = await screen.findByRole("link", {
      name: /Server lost connectivity/,
    });

    expect(card.getAttribute("href")).toBe("/labs/net-ip-01-link-down");
    expect(card.textContent).toContain("L2");
    expect(card.textContent).toContain("10 min");
    expect(card.textContent).toContain("Tutorial, Guided");
    expect(card.textContent).toContain("1 docs");
    expect(card.textContent).toContain("Has hidden checkpoints");
  });

  it("has no start button of its own", async () => {
    renderPage();

    await screen.findByRole("link", { name: /Server lost connectivity/ });

    expect(screen.queryByRole("button", { name: "Start" })).toBeNull();
  });
});

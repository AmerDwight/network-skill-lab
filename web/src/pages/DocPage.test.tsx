import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import * as client from "../api/client";
import { ApiError } from "../api/client";
import type { Doc } from "../api/types";
import i18next from "../i18n";
import { initialState as appInitialState, useAppStore } from "../store/app";
import { initialState, useDocsStore } from "../store/docs";

import { DocPage } from "./DocPage";

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
    listDocs: vi.fn(),
    getDoc: vi.fn(),
    markDocRead: vi.fn(),
  };
});

const getHealth = vi.mocked(client.getHealth);
const getDoc = vi.mocked(client.getDoc);
const markDocRead = vi.mocked(client.markDocRead);

const doc: Doc = {
  id: "net/ip/guide",
  title: "IP guide",
  topic: "net/ip",
  body: "# IP guide\n\nRun `ip link`.\n",
  completed: false,
};

function renderPage() {
  return render(
    <MemoryRouter initialEntries={[`/docs/${doc.id}`]}>
      <Routes>
        <Route path="/docs/*" element={<DocPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

beforeEach(async () => {
  vi.clearAllMocks();
  useAppStore.setState(appInitialState);
  useDocsStore.setState(initialState);
  await i18next.changeLanguage("en");
  getHealth.mockResolvedValue({
    ok: true,
    docker: true,
    image: true,
    image_name: "nsl/node",
    instance: "test",
    mem_available_mb: 2048,
    error: "",
  });
  getDoc.mockResolvedValue(doc);
});

afterEach(() => {
  cleanup();
});

describe("DocPage", () => {
  it("renders the markdown body of the doc the path points at", async () => {
    renderPage();

    expect(
      await screen.findByRole("heading", { name: "IP guide", level: 2 }),
    ).toBeDefined();
    expect(getDoc).toHaveBeenCalledWith("net/ip/guide");
    expect(document.querySelector(".markdown code")?.textContent).toBe(
      "ip link",
    );
  });

  it("marks the doc as read and keeps the button disabled afterwards", async () => {
    markDocRead.mockResolvedValue();
    renderPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Mark as read" }),
    );

    const read = await screen.findByRole("button", { name: "Read" });

    expect(markDocRead).toHaveBeenCalledWith(doc.id);
    expect(read.hasAttribute("disabled")).toBe(true);
  });

  it("shows a doc that is already read as read", async () => {
    getDoc.mockResolvedValue({ ...doc, completed: true });
    renderPage();

    const read = await screen.findByRole("button", { name: "Read" });

    expect(read.hasAttribute("disabled")).toBe(true);
    expect(screen.queryByRole("button", { name: "Mark as read" })).toBeNull();
  });

  it("keeps the button usable when marking fails", async () => {
    markDocRead.mockRejectedValue(new ApiError(400, "invalid_kind", "lab"));
    renderPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Mark as read" }),
    );

    expect(
      await screen.findByText("Could not mark the doc as read"),
    ).toBeDefined();
    expect(
      screen
        .getByRole("button", { name: "Mark as read" })
        .hasAttribute("disabled"),
    ).toBe(false);
  });
});

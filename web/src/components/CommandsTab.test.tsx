import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import * as client from "../api/client";
import type { CommandEntry } from "../api/types";
import i18next from "../i18n";

import { CommandsTab, commandsPageSize } from "./CommandsTab";

vi.mock("../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof client>();
  return { ApiError: actual.ApiError, getCommands: vi.fn() };
});

const getCommands = vi.mocked(client.getCommands);

const attemptId = "01JHIST0";

function makeEntry(index: number, node = "host"): CommandEntry {
  return {
    id: `cmd-${index}`,
    node,
    ts: "2026-09-24T09:00:00Z",
    user: "root",
    cwd: "/root",
    command: `ip link ${index}`,
    exit_code: index % 2,
  };
}

function fullPage(): CommandEntry[] {
  return Array.from({ length: commandsPageSize }, (_, index) =>
    makeEntry(index),
  );
}

function commandCells(): string[] {
  return screen
    .getAllByRole("row")
    .slice(1)
    .map((row) => row.children[4].textContent ?? "");
}

beforeEach(async () => {
  vi.clearAllMocks();
  await i18next.changeLanguage("en");
});

afterEach(() => {
  cleanup();
});

describe("CommandsTab", () => {
  it("lists the commands with their node, directory and exit code", async () => {
    getCommands.mockResolvedValue([makeEntry(0), makeEntry(1, "gw")]);

    render(<CommandsTab attemptId={attemptId} />);

    await screen.findByRole("table", { name: "Commands" });
    expect(getCommands).toHaveBeenCalledWith(attemptId, {
      limit: commandsPageSize,
    });
    expect(commandCells()).toEqual(["ip link 0", "ip link 1"]);
    expect(screen.getAllByText("/root")).toHaveLength(2);
    expect(screen.getByRole("cell", { name: "gw" })).toBeDefined();
  });

  it("keeps only the chosen node", async () => {
    getCommands.mockResolvedValue([
      makeEntry(0),
      makeEntry(1, "gw"),
      makeEntry(2),
    ]);

    render(<CommandsTab attemptId={attemptId} />);

    await screen.findByRole("table", { name: "Commands" });
    fireEvent.change(screen.getByLabelText("Node"), {
      target: { value: "gw" },
    });

    expect(commandCells()).toEqual(["ip link 1"]);

    fireEvent.change(screen.getByLabelText("Node"), {
      target: { value: "" },
    });

    expect(commandCells()).toEqual(["ip link 0", "ip link 1", "ip link 2"]);
  });

  it("offers every node that appears in the loaded commands", async () => {
    getCommands.mockResolvedValue([makeEntry(0), makeEntry(1, "gw")]);

    render(<CommandsTab attemptId={attemptId} />);

    await screen.findByRole("table", { name: "Commands" });

    expect(
      screen.getAllByRole("option").map((option) => option.textContent),
    ).toEqual(["All nodes", "gw", "host"]);
  });

  it("appends the next page using the last id as the cursor", async () => {
    getCommands.mockResolvedValueOnce(fullPage());

    render(<CommandsTab attemptId={attemptId} />);

    const button = await screen.findByRole("button", { name: "Load more" });
    getCommands.mockResolvedValueOnce([makeEntry(999)]);
    fireEvent.click(button);

    expect(await screen.findByText("ip link 999")).toBeDefined();
    expect(getCommands).toHaveBeenLastCalledWith(attemptId, {
      limit: commandsPageSize,
      after: `cmd-${commandsPageSize - 1}`,
    });
    expect(screen.queryByRole("button", { name: "Load more" })).toBeNull();
  });

  it("says so when the attempt recorded nothing", async () => {
    getCommands.mockResolvedValue([]);

    render(<CommandsTab attemptId={attemptId} />);

    expect(await screen.findByText("No command was recorded.")).toBeDefined();
  });

  it("reports a failure to load", async () => {
    getCommands.mockRejectedValue(new client.ApiError(500, "internal", "boom"));

    render(<CommandsTab attemptId={attemptId} />);

    expect(
      await screen.findByText("Could not load the commands"),
    ).toBeDefined();
    expect(screen.getByText("boom")).toBeDefined();
  });
});

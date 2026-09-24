import { beforeEach, describe, expect, it, vi } from "vitest";

import * as client from "../api/client";
import { ApiError } from "../api/client";
import type { HistoryItem } from "../api/types";

import { historyPageSize, initialState, useHistoryStore } from "./history";

vi.mock("../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof client>();
  return {
    ApiError: actual.ApiError,
    listHistory: vi.fn(),
    adminDeleteAttempt: vi.fn(),
  };
});

const listHistory = vi.mocked(client.listHistory);
const adminDeleteAttempt = vi.mocked(client.adminDeleteAttempt);

function makeItem(index: number, userId = "u-alice"): HistoryItem {
  return {
    id: `01JHIST${index}`,
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
    elapsed_ms: 120000,
    submit_count: 0,
    command_count: 12,
    created_at: `2026-09-${String(28 - index).padStart(2, "0")}T09:00:00Z`,
    ended_at: `2026-09-${String(28 - index).padStart(2, "0")}T09:02:00Z`,
    user: { id: userId, username: userId === "u-alice" ? "alice" : "admin" },
  };
}

function fullPage(offset = 0): HistoryItem[] {
  return Array.from({ length: historyPageSize }, (_, index) =>
    makeItem(offset + index),
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  useHistoryStore.setState(initialState);
});

describe("load", () => {
  it("asks for the first page of the caller's own attempts", async () => {
    listHistory.mockResolvedValue([makeItem(0)]);

    await useHistoryStore.getState().load();

    expect(listHistory).toHaveBeenCalledWith({ limit: historyPageSize });
    expect(useHistoryStore.getState().list).toEqual({
      status: "ok",
      items: [makeItem(0)],
      hasMore: false,
    });
  });

  it("reports more pages when the page comes back full", async () => {
    listHistory.mockResolvedValue(fullPage());

    await useHistoryStore.getState().load();

    const { list } = useHistoryStore.getState();
    expect(list.status === "ok" && list.hasMore).toBe(true);
  });

  it("keeps the error message", async () => {
    listHistory.mockRejectedValue(new ApiError(500, "internal", "boom"));

    await useHistoryStore.getState().load();

    expect(useHistoryStore.getState().list).toEqual({
      status: "error",
      message: "boom",
    });
  });
});

describe("loadMore", () => {
  it("passes the created_at of the last item as the cursor", async () => {
    const first = fullPage();
    listHistory.mockResolvedValueOnce(first);
    await useHistoryStore.getState().load();

    listHistory.mockResolvedValueOnce([makeItem(99)]);
    await useHistoryStore.getState().loadMore();

    expect(listHistory).toHaveBeenLastCalledWith({
      limit: historyPageSize,
      before: first[first.length - 1].created_at,
    });
    const { list } = useHistoryStore.getState();
    expect(list.status === "ok" && list.items).toHaveLength(
      historyPageSize + 1,
    );
    expect(list.status === "ok" && list.hasMore).toBe(false);
  });

  it("does nothing when the last page was already reached", async () => {
    listHistory.mockResolvedValue([makeItem(0)]);
    await useHistoryStore.getState().load();
    listHistory.mockClear();

    await useHistoryStore.getState().loadMore();

    expect(listHistory).not.toHaveBeenCalled();
  });

  it("keeps the loaded items when the next page fails", async () => {
    listHistory.mockResolvedValueOnce(fullPage());
    await useHistoryStore.getState().load();

    listHistory.mockRejectedValueOnce(new ApiError(500, "internal", "boom"));
    await useHistoryStore.getState().loadMore();

    const { list, moreError } = useHistoryStore.getState();
    expect(moreError).toBe("boom");
    expect(list.status === "ok" && list.items).toHaveLength(historyPageSize);
  });
});

describe("selectUser", () => {
  it("reloads the first page for the chosen user", async () => {
    listHistory.mockResolvedValue([makeItem(0, "u-admin")]);

    await useHistoryStore.getState().selectUser("u-admin");

    expect(listHistory).toHaveBeenCalledWith({
      limit: historyPageSize,
      user_id: "u-admin",
    });
    expect(useHistoryStore.getState().userId).toBe("u-admin");
  });

  it("drops the user filter when going back to self", async () => {
    listHistory.mockResolvedValue([makeItem(0)]);
    await useHistoryStore.getState().selectUser("u-admin");

    await useHistoryStore.getState().selectUser(null);

    expect(listHistory).toHaveBeenLastCalledWith({ limit: historyPageSize });
    expect(useHistoryStore.getState().userId).toBeNull();
  });

  it("carries the chosen user into the next page", async () => {
    listHistory.mockResolvedValueOnce(fullPage());
    await useHistoryStore.getState().selectUser("u-admin");

    listHistory.mockResolvedValueOnce([]);
    await useHistoryStore.getState().loadMore();

    expect(listHistory).toHaveBeenLastCalledWith({
      limit: historyPageSize,
      user_id: "u-admin",
      before: makeItem(historyPageSize - 1).created_at,
    });
  });
});

describe("deleteAttempt", () => {
  it("drops the attempt from the loaded list", async () => {
    listHistory.mockResolvedValue([makeItem(0), makeItem(1)]);
    await useHistoryStore.getState().load();
    adminDeleteAttempt.mockResolvedValue(undefined);

    await expect(
      useHistoryStore.getState().deleteAttempt("01JHIST0"),
    ).resolves.toBe(true);

    const { list } = useHistoryStore.getState();
    expect(list.status === "ok" && list.items.map((item) => item.id)).toEqual([
      "01JHIST1",
    ]);
  });

  it("keeps the error message when the server refuses", async () => {
    adminDeleteAttempt.mockRejectedValue(
      new ApiError(409, "attempt_running", "still running"),
    );

    await expect(
      useHistoryStore.getState().deleteAttempt("01JHIST0"),
    ).resolves.toBe(false);
    expect(useHistoryStore.getState().deleteError).toBe("still running");
  });
});

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import * as client from "../api/client";
import { ApiError } from "../api/client";
import type { Attempt } from "../api/types";

import {
  initialState,
  isFinishedStatus,
  isResultStatus,
  useAttemptStore,
} from "./attempt";

vi.mock("../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof client>();
  return {
    ApiError: actual.ApiError,
    getAttempt: vi.fn(),
    abandonAttempt: vi.fn(),
  };
});

const getAttempt = vi.mocked(client.getAttempt);
const abandonAttempt = vi.mocked(client.abandonAttempt);

const attempt: Attempt = {
  id: "01JATTEMPT",
  lab_id: "net-ip-01-link-down",
  mode: "guided",
  status: "running",
  error_message: "",
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
  ticket: "the server cannot reach the gateway",
  nodes: [
    { name: "host", role: "linux" },
    { name: "gw", role: "router" },
  ],
  checkpoints: [
    {
      id: "link-up",
      title: "The link is up",
      status: "pending",
      first_passed_at: null,
    },
    {
      id: "ping-ok",
      title: "The gateway answers",
      status: "pending",
      first_passed_at: null,
    },
  ],
  elapsed_ms: 5000,
  started_at: "2026-09-23T00:00:00.000Z",
  ended_at: null,
  server_time: "2026-09-23T00:00:05.000Z",
  created_at: "2026-09-23T00:00:00.000Z",
};

beforeEach(() => {
  vi.clearAllMocks();
  useAttemptStore.setState(initialState);
  vi.spyOn(Date, "now").mockReturnValue(1_000_000);
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("load", () => {
  it("spreads the attempt over the store", async () => {
    getAttempt.mockResolvedValue(attempt);

    await useAttemptStore.getState().load(attempt.id);

    const state = useAttemptStore.getState();
    expect(state.attempt).toEqual(attempt);
    expect(state.status).toBe("running");
    expect(state.checkpointOrder).toEqual(["link-up", "ping-ok"]);
    expect(state.checkpoints["link-up"]?.status).toBe("pending");
    expect(state.elapsedMs).toBe(5000);
    expect(state.syncedAt).toBe(1_000_000);
    expect(state.loading).toBe(false);
  });

  it("stores the error message", async () => {
    getAttempt.mockRejectedValue(new ApiError(404, "not_found", "no attempt"));

    await useAttemptStore.getState().load("missing");

    expect(useAttemptStore.getState().loadError).toBe("no attempt");
    expect(useAttemptStore.getState().loading).toBe(false);
  });
});

describe("applyEvent", () => {
  beforeEach(async () => {
    getAttempt.mockResolvedValue(attempt);
    await useAttemptStore.getState().load(attempt.id);
  });

  it("applies a status event with its timer base", () => {
    vi.mocked(Date.now).mockReturnValue(1_002_000);

    useAttemptStore.getState().applyEvent({
      type: "status",
      status: "running",
      error_message: "",
      elapsed_ms: 7000,
      server_time: "2026-09-23T00:00:07.000Z",
    });

    const state = useAttemptStore.getState();
    expect(state.elapsedMs).toBe(7000);
    expect(state.syncedAt).toBe(1_002_000);
    expect(state.displayedMs).toBe(7000);
  });

  it("keeps the error message of a failed attempt", () => {
    useAttemptStore.getState().applyEvent({
      type: "status",
      status: "error",
      error_message: "docker is gone",
      elapsed_ms: 7000,
      server_time: "2026-09-23T00:00:07.000Z",
    });

    expect(useAttemptStore.getState().status).toBe("error");
    expect(useAttemptStore.getState().errorMessage).toBe("docker is gone");
  });

  it("tracks the provisioning step and clears it once running", () => {
    useAttemptStore.getState().applyEvent({
      type: "provisioning",
      step: "bootstrap",
    });

    expect(useAttemptStore.getState().provisioningStep).toBe("bootstrap");

    useAttemptStore.getState().applyEvent({
      type: "status",
      status: "running",
      error_message: "",
      elapsed_ms: 0,
      server_time: "2026-09-23T00:00:00.000Z",
    });

    expect(useAttemptStore.getState().provisioningStep).toBeNull();
  });

  it("flips a known checkpoint and keeps its title", () => {
    useAttemptStore.getState().applyEvent({
      type: "checkpoint",
      id: "link-up",
      status: "pass",
      first_passed_at: "2026-09-23T00:00:09.000Z",
    });

    const checkpoint = useAttemptStore.getState().checkpoints["link-up"];
    expect(checkpoint).toEqual({
      id: "link-up",
      title: "The link is up",
      status: "pass",
      first_passed_at: "2026-09-23T00:00:09.000Z",
    });
    expect(useAttemptStore.getState().checkpointOrder).toEqual([
      "link-up",
      "ping-ok",
    ]);
  });

  it("appends an unknown checkpoint", () => {
    useAttemptStore.getState().applyEvent({
      type: "checkpoint",
      id: "extra",
      status: "fail",
      first_passed_at: null,
    });

    expect(useAttemptStore.getState().checkpointOrder).toEqual([
      "link-up",
      "ping-ok",
      "extra",
    ]);
  });

  it("resyncs the timer on a tick", () => {
    vi.mocked(Date.now).mockReturnValue(1_010_000);

    useAttemptStore.getState().applyEvent({
      type: "tick",
      elapsed_ms: 15000,
      server_time: "2026-09-23T00:00:15.000Z",
    });

    expect(useAttemptStore.getState().elapsedMs).toBe(15000);
    expect(useAttemptStore.getState().displayedMs).toBe(15000);
  });

  it("stores an error event", () => {
    useAttemptStore.getState().applyEvent({
      type: "error",
      message: "checker crashed",
    });

    expect(useAttemptStore.getState().eventError).toBe("checker crashed");
  });
});

describe("tick", () => {
  it("adds the local delta while running", async () => {
    getAttempt.mockResolvedValue(attempt);
    await useAttemptStore.getState().load(attempt.id);

    vi.mocked(Date.now).mockReturnValue(1_002_500);
    useAttemptStore.getState().tick();

    expect(useAttemptStore.getState().displayedMs).toBe(7500);
  });

  it("freezes once the attempt is no longer running", async () => {
    getAttempt.mockResolvedValue({ ...attempt, status: "passed" });
    await useAttemptStore.getState().load(attempt.id);

    vi.mocked(Date.now).mockReturnValue(1_002_500);
    useAttemptStore.getState().tick();

    expect(useAttemptStore.getState().displayedMs).toBe(5000);
  });

  it("does not count without a server sync", () => {
    vi.mocked(Date.now).mockReturnValue(1_002_500);
    useAttemptStore.getState().tick();

    expect(useAttemptStore.getState().displayedMs).toBe(0);
  });
});

describe("abandon", () => {
  it("stores the abandoned attempt", async () => {
    abandonAttempt.mockResolvedValue({ ...attempt, status: "abandoned" });

    await expect(useAttemptStore.getState().abandon(attempt.id)).resolves.toBe(
      true,
    );
    expect(useAttemptStore.getState().status).toBe("abandoned");
    expect(useAttemptStore.getState().abandoning).toBe(false);
  });

  it("reports a failure", async () => {
    abandonAttempt.mockRejectedValue(
      new ApiError(409, "attempt_finished", "already finished"),
    );

    await expect(useAttemptStore.getState().abandon(attempt.id)).resolves.toBe(
      false,
    );
    expect(useAttemptStore.getState().loadError).toBe("already finished");
  });
});

describe("terminal status flags", () => {
  it("navigates away only for result statuses", () => {
    expect(isResultStatus("passed")).toBe(true);
    expect(isResultStatus("abandoned")).toBe(true);
    expect(isResultStatus("expired")).toBe(true);
    expect(isResultStatus("error")).toBe(false);
    expect(isResultStatus("running")).toBe(false);
    expect(isResultStatus(null)).toBe(false);
  });

  it("stops the events socket for every terminal status", () => {
    expect(isFinishedStatus("error")).toBe(true);
    expect(isFinishedStatus("passed")).toBe(true);
    expect(isFinishedStatus("provisioning")).toBe(false);
    expect(isFinishedStatus(null)).toBe(false);
  });
});

describe("terminal connection states", () => {
  it("records and clears each tab", () => {
    useAttemptStore.getState().setTerminalState("host:main", "open");
    expect(useAttemptStore.getState().terminals).toEqual({
      "host:main": "open",
    });

    useAttemptStore.getState().setTerminalState("host:main", null);
    expect(useAttemptStore.getState().terminals).toEqual({});
  });
});

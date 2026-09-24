import { beforeEach, describe, expect, it, vi } from "vitest";

import * as client from "../api/client";
import { ApiError } from "../api/client";
import type {
  Attempt,
  Health,
  LabDetail,
  LabSummary,
  TopicNode,
} from "../api/types";

import { initialState, useAppStore } from "./app";

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
const getLab = vi.mocked(client.getLab);
const getCurrentAttempt = vi.mocked(client.getCurrentAttempt);
const createAttempt = vi.mocked(client.createAttempt);

const health: Health = {
  ok: true,
  docker: true,
  image: true,
  image_name: "nsl/node",
  instance: "test",
  mem_available_mb: 2048,
  error: "",
};

const topics: TopicNode[] = [
  {
    id: "net",
    title: "Networking",
    labs: 1,
    docs: 1,
    children: [{ id: "net/ip", title: "IP", labs: 1, docs: 1, children: [] }],
  },
];

const lab: LabSummary = {
  id: "net-ip-01-link-down",
  title: "Server lost connectivity",
  topic: "net/ip",
  level: 2,
  modes: ["tutorial", "guided", "real"],
  estimated_minutes: 10,
  related_docs: [{ id: "net/ip/guide", title: "IP guide" }],
  has_hidden_checkpoints: false,
};

const labDetail: LabDetail = {
  ...lab,
  nodes: [{ name: "host", role: "linux" }],
  checkpoints: [{ id: "link-up", title: "The link is up" }],
  topic_title: "IP",
};

const attempt: Attempt = {
  id: "01JATTEMPT",
  lab_id: lab.id,
  mode: "guided",
  status: "provisioning",
  error_message: "",
  lab,
  ticket: "the server cannot reach the gateway",
  nodes: [{ name: "host", role: "linux" }],
  checkpoints: [],
  checkpoints_hidden: false,
  tutorial_steps: null,
  submit_count: 0,
  elapsed_ms: 0,
  started_at: null,
  ended_at: null,
  server_time: "2026-09-23T00:00:00Z",
  created_at: "2026-09-23T00:00:00Z",
};

beforeEach(() => {
  vi.clearAllMocks();
  useAppStore.setState(initialState);
});

describe("loadHealth", () => {
  it("stores the health payload", async () => {
    getHealth.mockResolvedValue(health);

    await useAppStore.getState().loadHealth();

    expect(useAppStore.getState().health).toEqual({ status: "ok", health });
  });

  it("stores the error message", async () => {
    getHealth.mockRejectedValue(new ApiError(404, "not_found", "no route"));

    await useAppStore.getState().loadHealth();

    expect(useAppStore.getState().health).toEqual({
      status: "error",
      message: "no route",
    });
  });
});

describe("loadTopics", () => {
  it("stores the topic tree", async () => {
    listTopics.mockResolvedValue(topics);

    await useAppStore.getState().loadTopics();

    expect(useAppStore.getState().topics).toEqual({ status: "ok", topics });
  });

  it("stores the error message", async () => {
    listTopics.mockRejectedValue(new ApiError(404, "not_found", "no route"));

    await useAppStore.getState().loadTopics();

    expect(useAppStore.getState().topics).toEqual({
      status: "error",
      message: "no route",
    });
  });
});

describe("loadLabs", () => {
  it("stores the lab list", async () => {
    listLabs.mockResolvedValue([lab]);

    await useAppStore.getState().loadLabs();

    expect(listLabs).toHaveBeenCalledWith(undefined);
    expect(useAppStore.getState().labs).toEqual({
      status: "ok",
      labs: [lab],
    });
  });

  it("passes the selected topic on", async () => {
    listLabs.mockResolvedValue([lab]);

    await useAppStore.getState().loadLabs("net/ip");

    expect(listLabs).toHaveBeenCalledWith("net/ip");
  });

  it("stores the error message", async () => {
    listLabs.mockRejectedValue(new ApiError(404, "not_found", "no route"));

    await useAppStore.getState().loadLabs();

    expect(useAppStore.getState().labs).toEqual({
      status: "error",
      message: "no route",
    });
  });
});

describe("loadLab", () => {
  it("stores the lab detail", async () => {
    getLab.mockResolvedValue(labDetail);

    await useAppStore.getState().loadLab(lab.id);

    expect(getLab).toHaveBeenCalledWith(lab.id);
    expect(useAppStore.getState().lab).toEqual({
      status: "ok",
      lab: labDetail,
    });
  });

  it("stores the error message", async () => {
    getLab.mockRejectedValue(new ApiError(404, "not_found", "no lab"));

    await useAppStore.getState().loadLab(lab.id);

    expect(useAppStore.getState().lab).toEqual({
      status: "error",
      message: "no lab",
    });
  });
});

describe("loadCurrentAttempt", () => {
  it("keeps the attempt null when there is none", async () => {
    getCurrentAttempt.mockResolvedValue(null);

    await useAppStore.getState().loadCurrentAttempt();

    expect(useAppStore.getState().attempt).toBeNull();
    expect(useAppStore.getState().attemptError).toBeNull();
  });

  it("stores the running attempt", async () => {
    getCurrentAttempt.mockResolvedValue(attempt);

    await useAppStore.getState().loadCurrentAttempt();

    expect(useAppStore.getState().attempt).toEqual(attempt);
  });

  it("stores the error message", async () => {
    getCurrentAttempt.mockRejectedValue(
      new ApiError(404, "not_found", "no route"),
    );

    await useAppStore.getState().loadCurrentAttempt();

    expect(useAppStore.getState().attempt).toBeNull();
    expect(useAppStore.getState().attemptError).toBe("no route");
  });
});

describe("startAttempt", () => {
  it("returns and stores the created attempt", async () => {
    createAttempt.mockResolvedValue(attempt);

    await expect(
      useAppStore.getState().startAttempt(lab.id, "real"),
    ).resolves.toEqual(attempt);
    expect(createAttempt).toHaveBeenCalledWith(lab.id, "real");
    expect(useAppStore.getState().attempt).toEqual(attempt);
    expect(useAppStore.getState().starting).toBeNull();
  });

  it("loads the running attempt on 409", async () => {
    createAttempt.mockRejectedValue(
      new ApiError(409, "attempt_in_progress", "already running"),
    );
    getCurrentAttempt.mockResolvedValue(attempt);

    await expect(
      useAppStore.getState().startAttempt(lab.id, "guided"),
    ).resolves.toBeNull();
    expect(getCurrentAttempt).toHaveBeenCalledOnce();
    expect(useAppStore.getState().attempt).toEqual(attempt);
    expect(useAppStore.getState().attemptError).toBe("already running");
  });

  it("does not look for a running attempt on other errors", async () => {
    createAttempt.mockRejectedValue(new ApiError(404, "not_found", "no route"));

    await expect(
      useAppStore.getState().startAttempt(lab.id, "guided"),
    ).resolves.toBeNull();
    expect(getCurrentAttempt).not.toHaveBeenCalled();
    expect(useAppStore.getState().attemptError).toBe("no route");
    expect(useAppStore.getState().starting).toBeNull();
  });
});

describe("setLanguage", () => {
  it("switches the language", async () => {
    await useAppStore.getState().setLanguage("en");

    expect(useAppStore.getState().language).toBe("en");

    await useAppStore.getState().setLanguage("zh-TW");

    expect(useAppStore.getState().language).toBe("zh-TW");
  });
});

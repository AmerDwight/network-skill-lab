import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import * as client from "../api/client";
import type {
  Attempt,
  AttemptCheckpoint,
  Doc,
  LabDetail,
  LabMode,
  SubmitResult,
} from "../api/types";
import i18next from "../i18n";
import { initialState, useAttemptStore } from "../store/attempt";

import { AttemptPanel } from "./AttemptPanel";

vi.mock("../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof client>();
  return {
    ApiError: actual.ApiError,
    getLab: vi.fn(),
    getDoc: vi.fn(),
    getAttempt: vi.fn(),
    abandonAttempt: vi.fn(),
    submitAttempt: vi.fn(),
  };
});

const getLab = vi.mocked(client.getLab);
const getDoc = vi.mocked(client.getDoc);
const submitAttempt = vi.mocked(client.submitAttempt);

const lab: LabDetail = {
  id: "net-ip-01-link-down",
  title: "Server lost connectivity",
  topic: "net/ip",
  topic_title: "IP and links",
  level: 2,
  modes: ["tutorial", "guided", "real"],
  estimated_minutes: 10,
  related_docs: [
    { id: "net/ip/guide", title: "IP guide" },
    { id: "net/ip/playbook", title: "Playbook" },
  ],
  has_hidden_checkpoints: true,
  nodes: [{ name: "host", role: "linux" }],
  checkpoints: [{ id: "link-up", title: "The link is up" }],
};

const checkpoints: AttemptCheckpoint[] = [
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
];

function attemptFor(mode: LabMode): Attempt {
  return {
    id: "01JATTEMPT",
    lab_id: lab.id,
    mode,
    status: "running",
    error_message: "",
    lab,
    ticket: "the server cannot reach the gateway",
    nodes: lab.nodes,
    checkpoints: mode === "real" ? [] : checkpoints,
    checkpoints_hidden: mode === "real",
    tutorial_steps:
      mode === "tutorial"
        ? [
            { checkpoint: "link-up", instruction: "# Bring the link up" },
            { checkpoint: "ping-ok", instruction: "# Ping the gateway" },
          ]
        : null,
    submit_count: 0,
    elapsed_ms: 0,
    started_at: null,
    ended_at: null,
    server_time: "2026-09-23T00:00:00Z",
    created_at: "2026-09-23T00:00:00Z",
  };
}

function doc(id: string, body: string): Doc {
  return { id, title: id, topic: "net/ip", body, completed: false };
}

function setAttempt(mode: LabMode) {
  const attempt = attemptFor(mode);
  useAttemptStore.setState({
    ...initialState,
    attempt,
    status: attempt.status,
    checkpointOrder: attempt.checkpoints.map((checkpoint) => checkpoint.id),
    checkpoints: Object.fromEntries(
      attempt.checkpoints.map((checkpoint) => [checkpoint.id, checkpoint]),
    ),
  });
}

function renderPanel() {
  return render(
    <AttemptPanel
      attemptId="01JATTEMPT"
      abandoning={false}
      onAbandon={() => undefined}
    />,
  );
}

const failedSubmit: SubmitResult = {
  passed: false,
  checkpoints: [
    { id: "link-up", title: "The link is up", status: "pass" },
    { id: "ping-ok", title: "The gateway answers", status: "fail" },
  ],
  hidden_failed: 2,
  submit_count: 1,
};

const passedSubmit: SubmitResult = {
  passed: true,
  checkpoints: [
    { id: "link-up", title: "The link is up", status: "pass" },
    { id: "ping-ok", title: "The gateway answers", status: "pass" },
  ],
  hidden_failed: 0,
  submit_count: 2,
};

beforeEach(async () => {
  vi.clearAllMocks();
  await i18next.changeLanguage("en");
  HTMLElement.prototype.scrollIntoView = vi.fn();
  getLab.mockResolvedValue(lab);
  getDoc.mockResolvedValue(doc("net/ip/guide", "# The IP guide"));
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe("guided mode", () => {
  it("shows the ticket and the live checkpoints", async () => {
    setAttempt("guided");
    renderPanel();

    expect(await screen.findByRole("tab", { name: "Ticket" })).toBeDefined();
    expect(
      screen.getByText("the server cannot reach the gateway"),
    ).toBeDefined();
    expect(screen.getByText("The link is up")).toBeDefined();
    expect(screen.getByText("The gateway answers")).toBeDefined();
    expect(screen.queryByRole("button", { name: "Submit" })).toBeNull();
  });
});

describe("tutorial mode", () => {
  it("expands the current step only and marks the passed ones", async () => {
    setAttempt("tutorial");
    renderPanel();

    expect(await screen.findByRole("tab", { name: "Steps" })).toBeDefined();
    expect(screen.getByText("Bring the link up")).toBeDefined();
    expect(screen.queryByText("Ping the gateway")).toBeNull();

    const items = screen.getAllByRole("listitem");
    expect(items[0]?.getAttribute("aria-current")).toBe("step");
    expect(items[1]?.className).toContain("step--later");
  });

  it("advances to the next step when the current one passes", async () => {
    setAttempt("tutorial");
    renderPanel();

    await screen.findByRole("tab", { name: "Steps" });
    act(() => {
      useAttemptStore.getState().applyEvent({
        type: "checkpoint",
        id: "link-up",
        status: "pass",
        first_passed_at: "2026-09-23T00:01:00Z",
      });
    });

    expect(screen.getByText("Ping the gateway")).toBeDefined();
    expect(screen.queryByText("Bring the link up")).toBeNull();
    const items = screen.getAllByRole("listitem");
    expect(items[0]?.className).toContain("step--done");
    expect(items[1]?.getAttribute("aria-current")).toBe("step");
    expect(HTMLElement.prototype.scrollIntoView).toHaveBeenCalled();
  });

  it("keeps the check of a step passed out of order", async () => {
    setAttempt("tutorial");
    renderPanel();

    await screen.findByRole("tab", { name: "Steps" });
    act(() => {
      useAttemptStore.getState().applyEvent({
        type: "checkpoint",
        id: "ping-ok",
        status: "pass",
        first_passed_at: "2026-09-23T00:01:00Z",
      });
    });

    const items = screen.getAllByRole("listitem");
    expect(items[0]?.getAttribute("aria-current")).toBe("step");
    expect(items[1]?.className).toContain("step--done");
  });
});

describe("real mode", () => {
  it("shows only the ticket, the submit button and the count", async () => {
    setAttempt("real");
    renderPanel();

    expect(await screen.findByRole("button", { name: "Submit" })).toBeDefined();
    expect(
      screen.getByText("the server cannot reach the gateway"),
    ).toBeDefined();
    expect(screen.getByText("Submitted 0 times")).toBeDefined();
    expect(screen.queryByText("The gateway answers")).toBeNull();
  });

  it("summarises a failed submit and then passes", async () => {
    setAttempt("real");
    submitAttempt.mockResolvedValueOnce(failedSubmit);
    submitAttempt.mockResolvedValueOnce(passedSubmit);
    renderPanel();

    const button = await screen.findByRole("button", { name: "Submit" });
    await act(async () => {
      fireEvent.click(button);
    });

    expect(screen.getByText("Last submission")).toBeDefined();
    expect(screen.getByText("2 hidden checks failed")).toBeDefined();
    expect(screen.getByText("Submitted 1 times")).toBeDefined();

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Submit" }));
    });

    expect(screen.queryByText("Last submission")).toBeNull();
    expect(screen.getByText("Submitted 2 times")).toBeDefined();
  });

  it("hides the summary after five seconds", async () => {
    setAttempt("real");
    submitAttempt.mockResolvedValue(failedSubmit);
    vi.useFakeTimers();
    renderPanel();

    await act(async () => {
      await Promise.resolve();
    });
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Submit" }));
    });

    expect(screen.getByText("Last submission")).toBeDefined();

    act(() => {
      vi.advanceTimersByTime(5000);
    });

    expect(screen.queryByText("Last submission")).toBeNull();
  });

  it("disables the button while a submit is in flight", async () => {
    setAttempt("real");
    let release: () => void = () => undefined;
    submitAttempt.mockReturnValue(
      new Promise<SubmitResult>((resolve) => {
        release = () => resolve(failedSubmit);
      }),
    );
    renderPanel();

    const button = await screen.findByRole("button", { name: "Submit" });
    act(() => {
      fireEvent.click(button);
    });

    expect(
      screen
        .getByRole("button", { name: "Submitting" })
        .hasAttribute("disabled"),
    ).toBe(true);

    await act(async () => {
      release();
    });

    expect(screen.getByRole("button", { name: "Submit" })).toBeDefined();
  });
});

describe("docs tab", () => {
  it("lists the related docs and renders the selected one", async () => {
    setAttempt("guided");
    renderPanel();

    fireEvent.click(await screen.findByRole("tab", { name: "Docs" }));

    expect(await screen.findByRole("tab", { name: "IP guide" })).toBeDefined();
    expect(getLab).toHaveBeenCalledWith(lab.id);
    expect(getLab).toHaveBeenCalledTimes(1);
    expect(await screen.findByText("The IP guide")).toBeDefined();

    getDoc.mockResolvedValue(doc("net/ip/playbook", "# The playbook"));
    fireEvent.click(screen.getByRole("tab", { name: "Playbook" }));

    expect(await screen.findByText("The playbook")).toBeDefined();
    expect(getDoc).toHaveBeenLastCalledWith("net/ip/playbook");
  });

  it("keeps the selected doc when the tab is opened again", async () => {
    setAttempt("guided");
    renderPanel();

    fireEvent.click(await screen.findByRole("tab", { name: "Docs" }));
    getDoc.mockResolvedValue(doc("net/ip/playbook", "# The playbook"));
    fireEvent.click(await screen.findByRole("tab", { name: "Playbook" }));
    await screen.findByText("The playbook");

    fireEvent.click(screen.getByRole("tab", { name: "Ticket" }));
    fireEvent.click(screen.getByRole("tab", { name: "Docs" }));

    expect(await screen.findByText("The playbook")).toBeDefined();
  });

  it("says when the lab has no related docs", async () => {
    setAttempt("guided");
    getLab.mockResolvedValue({ ...lab, related_docs: [] });
    renderPanel();

    fireEvent.click(await screen.findByRole("tab", { name: "Docs" }));

    expect(await screen.findByText("No related docs")).toBeDefined();
  });
});

import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { Player } from "asciinema-player";
import { create } from "asciinema-player";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import * as client from "../api/client";
import type { RecordingInfo } from "../api/types";
import i18next from "../i18n";

import { RecordingsTab } from "./RecordingsTab";

vi.mock("asciinema-player", () => ({ create: vi.fn() }));

vi.mock("../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof client>();
  return {
    ApiError: actual.ApiError,
    listRecordings: vi.fn(),
    castUrl: actual.castUrl,
  };
});

const listRecordings = vi.mocked(client.listRecordings);
const createPlayer = vi.mocked(create);

const attemptId = "01JHIST0";

const recordings: RecordingInfo[] = [
  {
    id: "rec-host-1",
    node: "host",
    tab: "1",
    started_at: "2026-09-24T09:00:00Z",
    ended_at: "2026-09-24T09:05:00Z",
    bytes: 48213,
  },
  {
    id: "rec-gw-1",
    node: "gw",
    tab: "1",
    started_at: "2026-09-24T09:01:00Z",
    ended_at: null,
    bytes: 1024,
  },
];

beforeEach(async () => {
  vi.clearAllMocks();
  createPlayer.mockImplementation(
    () => ({ dispose: vi.fn() }) as unknown as Player,
  );
  await i18next.changeLanguage("en");
});

afterEach(() => {
  cleanup();
});

describe("RecordingsTab", () => {
  it("renders one player per recording with its caption", async () => {
    listRecordings.mockResolvedValue(recordings);

    const { container } = render(<RecordingsTab attemptId={attemptId} />);

    expect(await screen.findByText("host · 1")).toBeDefined();
    expect(screen.getByText("gw · 1")).toBeDefined();
    await waitFor(() => {
      expect(container.querySelectorAll(".cast-player")).toHaveLength(2);
    });
    expect(createPlayer).toHaveBeenCalledTimes(2);
    expect(createPlayer.mock.calls.map((call) => call[0])).toEqual([
      `/api/attempts/${attemptId}/recordings/rec-host-1/cast`,
      `/api/attempts/${attemptId}/recordings/rec-gw-1/cast`,
    ]);
    expect(screen.getByText(/47\.1 KB/)).toBeDefined();
  });

  it("restarts one player at the chosen speed", async () => {
    listRecordings.mockResolvedValue([recordings[0]]);

    render(<RecordingsTab attemptId={attemptId} />);

    await screen.findByText("host · 1");
    await waitFor(() => {
      expect(createPlayer).toHaveBeenCalledTimes(1);
    });

    fireEvent.change(screen.getByLabelText("Speed"), {
      target: { value: "4" },
    });

    await waitFor(() => {
      expect(createPlayer).toHaveBeenCalledTimes(2);
    });
    expect(createPlayer.mock.calls[1][2]?.speed).toBe(4);
  });

  it("says so when nothing was recorded", async () => {
    listRecordings.mockResolvedValue([]);

    render(<RecordingsTab attemptId={attemptId} />);

    expect(await screen.findByText("No recording was kept.")).toBeDefined();
    expect(createPlayer).not.toHaveBeenCalled();
  });

  it("reports a failure to list the recordings", async () => {
    listRecordings.mockRejectedValue(
      new client.ApiError(500, "internal", "boom"),
    );

    render(<RecordingsTab attemptId={attemptId} />);

    expect(
      await screen.findByText("Could not load the recordings"),
    ).toBeDefined();
    expect(screen.getByText("boom")).toBeDefined();
  });
});

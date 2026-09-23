import { beforeEach, describe, expect, it, vi } from "vitest";

import * as client from "../api/client";
import { ApiError } from "../api/client";
import type { Track, TrackSummary } from "../api/types";

import { initialState, useTracksStore } from "./tracks";

vi.mock("../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof client>();
  return {
    ApiError: actual.ApiError,
    listTracks: vi.fn(),
    getTrack: vi.fn(),
  };
});

const listTracks = vi.mocked(client.listTracks);
const getTrack = vi.mocked(client.getTrack);

const summary: TrackSummary = {
  id: "network-basics",
  title: "Network basics",
  steps: 2,
  completed: 1,
};

const track: Track = {
  id: summary.id,
  title: summary.title,
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
      title: "Server lost connectivity",
      mode: "guided",
      completed: false,
    },
  ],
};

beforeEach(() => {
  vi.clearAllMocks();
  useTracksStore.setState(initialState);
});

describe("loadTracks", () => {
  it("stores the track list", async () => {
    listTracks.mockResolvedValue([summary]);

    await useTracksStore.getState().loadTracks();

    expect(useTracksStore.getState().tracks).toEqual({
      status: "ok",
      tracks: [summary],
    });
  });

  it("stores the error message", async () => {
    listTracks.mockRejectedValue(new ApiError(404, "not_found", "no route"));

    await useTracksStore.getState().loadTracks();

    expect(useTracksStore.getState().tracks).toEqual({
      status: "error",
      message: "no route",
    });
  });
});

describe("loadTrack", () => {
  it("stores the track", async () => {
    getTrack.mockResolvedValue(track);

    await useTracksStore.getState().loadTrack(track.id);

    expect(getTrack).toHaveBeenCalledWith(track.id);
    expect(useTracksStore.getState().track).toEqual({ status: "ok", track });
  });

  it("stores the error message", async () => {
    getTrack.mockRejectedValue(new ApiError(404, "not_found", "no track"));

    await useTracksStore.getState().loadTrack(track.id);

    expect(useTracksStore.getState().track).toEqual({
      status: "error",
      message: "no track",
    });
  });
});

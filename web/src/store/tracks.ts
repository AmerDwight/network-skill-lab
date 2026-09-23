import { create } from "zustand";

import { getTrack, listTracks } from "../api/client";
import type { Track, TrackSummary } from "../api/types";
import { messageOf } from "../lib/errors";

export type TracksState =
  | { status: "loading" }
  | { status: "ok"; tracks: TrackSummary[] }
  | { status: "error"; message: string };

export type TrackState =
  | { status: "loading" }
  | { status: "ok"; track: Track }
  | { status: "error"; message: string };

export interface TracksStoreState {
  tracks: TracksState;
  track: TrackState;
  loadTracks: () => Promise<void>;
  loadTrack: (id: string) => Promise<void>;
}

export const initialState = {
  tracks: { status: "loading" } as TracksState,
  track: { status: "loading" } as TrackState,
};

export const useTracksStore = create<TracksStoreState>()((set) => ({
  ...initialState,

  loadTracks: async () => {
    set({ tracks: { status: "loading" } });
    try {
      set({ tracks: { status: "ok", tracks: await listTracks() } });
    } catch (error) {
      set({ tracks: { status: "error", message: messageOf(error) } });
    }
  },

  loadTrack: async (id) => {
    set({ track: { status: "loading" } });
    try {
      set({ track: { status: "ok", track: await getTrack(id) } });
    } catch (error) {
      set({ track: { status: "error", message: messageOf(error) } });
    }
  },
}));

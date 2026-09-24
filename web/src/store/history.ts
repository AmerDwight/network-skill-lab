import { create } from "zustand";

import { adminDeleteAttempt, listHistory } from "../api/client";
import type { HistoryItem } from "../api/types";
import { messageOf } from "../lib/errors";

export const historyPageSize = 20;

export type HistoryListState =
  | { status: "loading" }
  | { status: "ok"; items: HistoryItem[]; hasMore: boolean }
  | { status: "error"; message: string };

export interface HistoryState {
  list: HistoryListState;
  userId: string | null;
  loadingMore: boolean;
  moreError: string | null;
  deleting: string | null;
  deleteError: string | null;
  load: () => Promise<void>;
  loadMore: () => Promise<void>;
  selectUser: (userId: string | null) => Promise<void>;
  deleteAttempt: (id: string) => Promise<boolean>;
  reset: () => void;
}

export const initialState = {
  list: { status: "loading" } as HistoryListState,
  userId: null as string | null,
  loadingMore: false,
  moreError: null as string | null,
  deleting: null as string | null,
  deleteError: null as string | null,
};

function query(userId: string | null, before?: string) {
  return {
    limit: historyPageSize,
    ...(userId === null ? {} : { user_id: userId }),
    ...(before === undefined ? {} : { before }),
  };
}

export const useHistoryStore = create<HistoryState>()((set, get) => ({
  ...initialState,

  load: async () => {
    set({ list: { status: "loading" }, moreError: null });
    const { userId } = get();
    try {
      const items = await listHistory(query(userId));
      set({
        list: {
          status: "ok",
          items,
          hasMore: items.length === historyPageSize,
        },
      });
    } catch (error) {
      set({ list: { status: "error", message: messageOf(error) } });
    }
  },

  loadMore: async () => {
    const { list, userId, loadingMore } = get();
    if (loadingMore || list.status !== "ok" || !list.hasMore) {
      return;
    }
    const last = list.items.at(-1);
    if (last === undefined) {
      return;
    }
    set({ loadingMore: true, moreError: null });
    try {
      const items = await listHistory(query(userId, last.created_at));
      const current = get().list;
      set({
        loadingMore: false,
        list:
          current.status === "ok"
            ? {
                status: "ok",
                items: [...current.items, ...items],
                hasMore: items.length === historyPageSize,
              }
            : current,
      });
    } catch (error) {
      set({ loadingMore: false, moreError: messageOf(error) });
    }
  },

  selectUser: async (userId) => {
    set({ userId });
    await get().load();
  },

  deleteAttempt: async (id) => {
    set({ deleting: id, deleteError: null });
    try {
      await adminDeleteAttempt(id);
      const { list } = get();
      set({
        deleting: null,
        list:
          list.status === "ok"
            ? {
                ...list,
                items: list.items.filter((item) => item.id !== id),
              }
            : list,
      });
      return true;
    } catch (error) {
      set({ deleting: null, deleteError: messageOf(error) });
      return false;
    }
  },

  reset: () => set(initialState),
}));

import { create } from "zustand";

import { getDoc, listDocs, markDocRead } from "../api/client";
import type { Doc, DocSummary } from "../api/types";
import { messageOf } from "../lib/errors";

export type DocsState =
  | { status: "loading" }
  | { status: "ok"; docs: DocSummary[] }
  | { status: "error"; message: string };

export type DocState =
  | { status: "loading" }
  | { status: "ok"; doc: Doc }
  | { status: "error"; message: string };

export interface DocsStoreState {
  docs: DocsState;
  doc: DocState;
  marking: boolean;
  markError: string | null;
  loadDocs: () => Promise<void>;
  loadDoc: (id: string) => Promise<void>;
  markRead: (id: string) => Promise<void>;
}

export const initialState = {
  docs: { status: "loading" } as DocsState,
  doc: { status: "loading" } as DocState,
  marking: false,
  markError: null,
};

export const useDocsStore = create<DocsStoreState>()((set, get) => ({
  ...initialState,

  loadDocs: async () => {
    set({ docs: { status: "loading" } });
    try {
      set({ docs: { status: "ok", docs: await listDocs() } });
    } catch (error) {
      set({ docs: { status: "error", message: messageOf(error) } });
    }
  },

  loadDoc: async (id) => {
    set({ doc: { status: "loading" }, markError: null });
    try {
      set({ doc: { status: "ok", doc: await getDoc(id) } });
    } catch (error) {
      set({ doc: { status: "error", message: messageOf(error) } });
    }
  },

  markRead: async (id) => {
    set({ marking: true, markError: null });
    try {
      await markDocRead(id);
      const { doc } = get();
      set({
        marking: false,
        doc:
          doc.status === "ok" && doc.doc.id === id
            ? { status: "ok", doc: { ...doc.doc, completed: true } }
            : doc,
      });
    } catch (error) {
      set({ marking: false, markError: messageOf(error) });
    }
  },
}));

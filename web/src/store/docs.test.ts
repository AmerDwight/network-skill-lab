import { beforeEach, describe, expect, it, vi } from "vitest";

import * as client from "../api/client";
import { ApiError } from "../api/client";
import type { Doc } from "../api/types";

import { initialState, useDocsStore } from "./docs";

vi.mock("../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof client>();
  return {
    ApiError: actual.ApiError,
    listDocs: vi.fn(),
    getDoc: vi.fn(),
    markDocRead: vi.fn(),
  };
});

const listDocs = vi.mocked(client.listDocs);
const getDoc = vi.mocked(client.getDoc);
const markDocRead = vi.mocked(client.markDocRead);

const doc: Doc = {
  id: "net/ip/guide",
  title: "IP guide",
  topic: "net/ip",
  body: "# IP guide\n",
  completed: false,
};

beforeEach(() => {
  vi.clearAllMocks();
  useDocsStore.setState(initialState);
});

describe("loadDocs", () => {
  it("stores the doc list", async () => {
    listDocs.mockResolvedValue([doc]);

    await useDocsStore.getState().loadDocs();

    expect(useDocsStore.getState().docs).toEqual({
      status: "ok",
      docs: [doc],
    });
  });

  it("stores the error message", async () => {
    listDocs.mockRejectedValue(new ApiError(404, "not_found", "no route"));

    await useDocsStore.getState().loadDocs();

    expect(useDocsStore.getState().docs).toEqual({
      status: "error",
      message: "no route",
    });
  });
});

describe("loadDoc", () => {
  it("stores the doc", async () => {
    getDoc.mockResolvedValue(doc);

    await useDocsStore.getState().loadDoc(doc.id);

    expect(getDoc).toHaveBeenCalledWith(doc.id);
    expect(useDocsStore.getState().doc).toEqual({ status: "ok", doc });
  });

  it("stores the error message", async () => {
    getDoc.mockRejectedValue(new ApiError(404, "not_found", "no doc"));

    await useDocsStore.getState().loadDoc(doc.id);

    expect(useDocsStore.getState().doc).toEqual({
      status: "error",
      message: "no doc",
    });
  });
});

describe("markRead", () => {
  it("marks the loaded doc as completed", async () => {
    getDoc.mockResolvedValue(doc);
    markDocRead.mockResolvedValue();

    await useDocsStore.getState().loadDoc(doc.id);
    await useDocsStore.getState().markRead(doc.id);

    expect(markDocRead).toHaveBeenCalledWith(doc.id);
    expect(useDocsStore.getState().doc).toEqual({
      status: "ok",
      doc: { ...doc, completed: true },
    });
    expect(useDocsStore.getState().marking).toBe(false);
  });

  it("leaves another loaded doc alone", async () => {
    getDoc.mockResolvedValue(doc);
    markDocRead.mockResolvedValue();

    await useDocsStore.getState().loadDoc(doc.id);
    await useDocsStore.getState().markRead("net/dns/guide");

    expect(useDocsStore.getState().doc).toEqual({ status: "ok", doc });
  });

  it("stores the error message", async () => {
    getDoc.mockResolvedValue(doc);
    markDocRead.mockRejectedValue(new ApiError(400, "invalid_kind", "lab"));

    await useDocsStore.getState().loadDoc(doc.id);
    await useDocsStore.getState().markRead(doc.id);

    expect(useDocsStore.getState().markError).toBe("lab");
    expect(useDocsStore.getState().doc).toEqual({ status: "ok", doc });
  });
});

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import i18next from "../i18n";

import {
  ApiError,
  createAttempt,
  getCurrentAttempt,
  getHealth,
  getLab,
  getResult,
  listLabs,
} from "./client";

const fetchMock = vi.fn<typeof fetch>();

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function lastUrl(): string {
  return String(fetchMock.mock.calls.at(-1)?.[0]);
}

beforeEach(async () => {
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
  await i18next.changeLanguage("zh-TW");
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("getHealth", () => {
  it("returns the parsed body and asks for the current language", async () => {
    const health = {
      ok: true,
      docker: true,
      image: true,
      image_name: "nsl/node",
      mem_available_mb: 4096,
      error: "",
    };
    fetchMock.mockResolvedValue(jsonResponse(200, health));

    await expect(getHealth()).resolves.toEqual(health);
    expect(lastUrl()).toBe("/api/health?lang=zh-TW");
  });

  it("follows a language change", async () => {
    await i18next.changeLanguage("en");
    fetchMock.mockResolvedValue(jsonResponse(200, {}));

    await getHealth();

    expect(lastUrl()).toBe("/api/health?lang=en");
  });
});

describe("listLabs", () => {
  it("returns the lab summaries", async () => {
    const labs = [
      {
        id: "net-ip-01-link-down",
        title: "伺服器連不到對外",
        topic: "net/ip",
        level: 2,
        modes: ["guided"],
        estimated_minutes: 10,
      },
    ];
    fetchMock.mockResolvedValue(jsonResponse(200, labs));

    await expect(listLabs()).resolves.toEqual(labs);
    expect(lastUrl()).toBe("/api/labs?lang=zh-TW");
  });
});

describe("getLab", () => {
  it("encodes the lab id", async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, { id: "a/b" }));

    await getLab("a/b");

    expect(lastUrl()).toBe("/api/labs/a%2Fb?lang=zh-TW");
  });
});

describe("createAttempt", () => {
  it("posts the lab id and mode", async () => {
    const attempt = {
      id: "01J",
      lab_id: "net-ip-01-link-down",
      mode: "guided",
      status: "provisioning",
      elapsed_ms: 0,
      server_time: "2026-09-23T00:00:00Z",
      created_at: "2026-09-23T00:00:00Z",
    };
    fetchMock.mockResolvedValue(jsonResponse(201, attempt));

    await expect(createAttempt("net-ip-01-link-down")).resolves.toEqual(
      attempt,
    );
    const init = fetchMock.mock.calls.at(-1)?.[1];
    expect(init?.method).toBe("POST");
    expect(init?.body).toBe(
      JSON.stringify({ lab_id: "net-ip-01-link-down", mode: "guided" }),
    );
  });

  it("raises an ApiError on 409", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(409, {
        error: { code: "attempt_in_progress", message: "already running" },
      }),
    );

    await expect(createAttempt("lab")).rejects.toMatchObject({
      status: 409,
      code: "attempt_in_progress",
      message: "already running",
    });
  });
});

describe("getCurrentAttempt", () => {
  it("returns null on 204", async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }));

    await expect(getCurrentAttempt()).resolves.toBeNull();
  });

  it("returns the attempt on 200", async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, { id: "01J" }));

    await expect(getCurrentAttempt()).resolves.toMatchObject({ id: "01J" });
  });
});

describe("getResult", () => {
  it("asks for the result of an attempt", async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, { attempt_id: "01J" }));

    await expect(getResult("01J")).resolves.toMatchObject({
      attempt_id: "01J",
    });
    expect(lastUrl()).toBe("/api/attempts/01J/result?lang=zh-TW");
  });

  it("raises an ApiError on 409 while the attempt is unfinished", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(409, {
        error: { code: "attempt_running", message: "not finished" },
      }),
    );

    await expect(getResult("01J")).rejects.toMatchObject({
      status: 409,
      code: "attempt_running",
    });
  });
});

describe("error handling", () => {
  it("maps the 404 error body onto ApiError", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(404, {
        error: { code: "not_found", message: "no route for GET /api/labs" },
      }),
    );

    const error = await listLabs().catch((caught: unknown) => caught);

    expect(error).toBeInstanceOf(ApiError);
    expect(error).toMatchObject({
      status: 404,
      code: "not_found",
      message: "no route for GET /api/labs",
    });
  });

  it("falls back when the error body is not the expected shape", async () => {
    fetchMock.mockResolvedValue(new Response("<html>", { status: 500 }));

    await expect(getHealth()).rejects.toMatchObject({
      status: 500,
      code: "unknown",
      message: "request failed with status 500",
    });
  });
});

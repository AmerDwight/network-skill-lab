import { currentLanguage } from "../i18n";

import type {
  Attempt,
  CreateAttemptRequest,
  Health,
  Lab,
  LabMode,
  LabSummary,
  Result,
} from "./types";

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

function withLanguage(path: string): string {
  const separator = path.includes("?") ? "&" : "?";
  return `${path}${separator}lang=${encodeURIComponent(currentLanguage())}`;
}

function parseError(status: number, body: unknown): ApiError {
  const fallback = new ApiError(
    status,
    "unknown",
    `request failed with status ${status}`,
  );
  if (typeof body !== "object" || body === null) {
    return fallback;
  }
  const { error } = body as { error?: { code?: unknown; message?: unknown } };
  if (
    error === undefined ||
    typeof error.code !== "string" ||
    typeof error.message !== "string"
  ) {
    return fallback;
  }
  return new ApiError(status, error.code, error.message);
}

async function send(path: string, init?: RequestInit): Promise<Response> {
  const response = await fetch(withLanguage(path), init);
  if (response.ok) {
    return response;
  }
  let body: unknown = null;
  try {
    body = await response.json();
  } catch {
    body = null;
  }
  throw parseError(response.status, body);
}

async function json<T>(response: Response): Promise<T> {
  return (await response.json()) as T;
}

export async function getHealth(): Promise<Health> {
  return json<Health>(await send("/api/health"));
}

export async function listLabs(): Promise<LabSummary[]> {
  return json<LabSummary[]>(await send("/api/labs"));
}

export async function getLab(id: string): Promise<Lab> {
  return json<Lab>(await send(`/api/labs/${encodeURIComponent(id)}`));
}

export async function createAttempt(
  labId: string,
  mode: LabMode = "guided",
): Promise<Attempt> {
  const body: CreateAttemptRequest = { lab_id: labId, mode };
  const response = await send("/api/attempts", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  return json<Attempt>(response);
}

export async function getCurrentAttempt(): Promise<Attempt | null> {
  const response = await send("/api/attempts/current");
  if (response.status === 204) {
    return null;
  }
  return json<Attempt>(response);
}

export async function getAttempt(id: string): Promise<Attempt> {
  return json<Attempt>(await send(`/api/attempts/${encodeURIComponent(id)}`));
}

export async function abandonAttempt(id: string): Promise<Attempt> {
  const response = await send(
    `/api/attempts/${encodeURIComponent(id)}/abandon`,
    { method: "POST" },
  );
  return json<Attempt>(response);
}

export async function getResult(id: string): Promise<Result> {
  const response = await send(`/api/attempts/${encodeURIComponent(id)}/result`);
  return json<Result>(response);
}
